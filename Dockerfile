# syntax=docker/dockerfile:1

# Multi-stage packaging of MyMediaDB: builds the SPA (Vite), compiles the Go
# binary with the SPA embedded via //go:embed and ships a lean final image.
# The build does NOT depend on locally pre-built artifacts: the real dist is
# generated in stage 1 and copied into the Go module before compilation.
#
# Multi-architecture strategy (amd64 + arm64 + arm/v7, Raspberry Pi target):
# CROSS-COMPILATION, not QEMU emulation. The builder-web and builder-go stages
# ALWAYS run on the runner's native arch (--platform=$BUILDPLATFORM): the Vite
# dist is arch-neutral (HTML/JS/CSS) and the Go binary (CGO_ENABLED=0, pure-Go
# SQLite via modernc) cross-compiles trivially to the target GOOS/GOARCH/GOARM.
# That avoids emulating `npm run build`/`go build` under QEMU, which would be
# slow and risk OOM on armv7. Only the `final` stage (apk add ffmpeg) is
# really built for the TARGET platform, because it needs the native ffmpeg.

# Binary version, injected via -ldflags. Override with --build-arg VERSION=...
ARG VERSION=docker

# ---------------------------------------------------------------------------
# Stage 1 — builder-web: generates the production SPA with Vite.
# --platform=$BUILDPLATFORM: forces it to run on the runner's native arch, never
# emulated — the generated dist is arch-neutral, so there is no reason to build
# it through QEMU (pointlessly slower).
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:24-slim AS builder-web
WORKDIR /app/web

# Dependency layer: the manifest rarely changes, so it makes good use of the cache.
COPY web/package.json web/package-lock.json ./
RUN npm ci

# Front-end source and build. vite.config.ts points build.outDir to
# ../internal/webui/dist, so the output goes to /app/internal/webui/dist.
COPY web/ ./
RUN npm run build

# ---------------------------------------------------------------------------
# Stage 2 — builder-go: compiles the static binary with the SPA embedded.
# --platform=$BUILDPLATFORM: the Go toolchain also runs on the runner's native
# arch (avoids QEMU for `go build` itself); what determines the arch of the
# final BINARY is the cross-compile via GOOS/GOARCH/GOARM below, not the
# platform of the stage. Go version pinned to 1.26.6 (exact patch, the same
# version fixed in CI because of stdlib vulns/build determinism).
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26.11-alpine AS builder-go
WORKDIR /app

# Go dependency layer.
COPY go.mod go.sum ./
RUN go mod download

# Application Go code.
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Replaces the versioned placeholder with the REAL dist coming from stage 1, so
# that //go:embed all:dist captures the production SPA.
COPY --from=builder-web /app/internal/webui/dist/ ./internal/webui/dist/

# Static compilation (no CGO) with the version injected and a lean binary (-s -w),
# CROSS-COMPILED for the TARGET platform. TARGETOS/TARGETARCH/TARGETVARIANT
# are filled in automatically by BuildKit according to the target --platform
# passed to `docker build`/`buildx build` (e.g.: linux/arm/v7 → TARGETOS=linux,
# TARGETARCH=arm, TARGETVARIANT=v7). GOARM=${TARGETVARIANT#v} extracts the "7" from
# "v7" for the Raspberry Pi armv7; on amd64/arm64 the variant comes empty and
# GOARM="" is simply ignored by the Go toolchain.
ARG VERSION
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build \
    -ldflags "-X main.version=${VERSION} -s -w" \
    -o /mymediadb ./cmd/mymediadb

# ---------------------------------------------------------------------------
# Stage 3 — final: minimal image, without the Node/Go toolchain.
# NO --platform=$BUILDPLATFORM here on purpose: this stage is built
# for the TARGET platform (Docker's default behavior), because `apk add
# ffmpeg` must bring the package for the right arch (amd64/arm64/armv7) — only the
# Go binary copied from builder-go already comes cross-compiled for that arch.
# ---------------------------------------------------------------------------
FROM alpine:3.22 AS final

# RUNTIME dependencies:
#   ffmpeg     → scanner ffprobe (ADR-0014). Without it the scan still
#                runs, but degrades (empty mediainfo).
#   sqlite     → CLI used by scripts/backup.sh for `VACUUM INTO` (consistent
#                snapshot of the WAL database with no downtime). The app itself
#                uses the pure-Go modernc driver and does NOT depend on this
#                binary; it is only for the backup.
#   python3    → runtime for the yt-dlp binary downloaded dynamically at runtime
#                (ADR-0025): the `internal/ytdlp` package downloads the
#                "yt-dlp" asset (platform-independent Python zipapp, shebang
#                `#!/usr/bin/env python3`) from the official release, NEVER the
#                per-OS standalone (`yt-dlp_linux` etc., a PyInstaller build linked
#                to glibc) --
#                this image is Alpine/musl and the standalone fails at runtime with
#                "no such file or directory" (not even `gcompat` fixes it: it fails
#                further on because of a glibc symbol missing from the embedded
#                libpython). Found in the first production validation of the
#                yt-dlp integration.
#   chromaprint → fpcalc from the acoustic fingerprint step 4.5
#                 (internal/fingerprint): subprocess, the same pattern as ffprobe
#                 above -- absence degrades (the scanner skips the step, logs a
#                 warning), it never takes the scan down. Package confirmed in
#                 Alpine 3.22's `community` repo (x86_64/aarch64/armhf/armv7);
#                 it installs /usr/bin/fpcalc directly, with no separate
#                 `-tools` package.
# --no-cache keeps the image lean.
RUN apk add --no-cache ffmpeg sqlite python3 chromaprint su-exec

# Dedicated non-root user. The entrypoint may re-id it through PUID/PGID at boot (Unraid
# convention) and always ends up running the binary through su-exec with those ids.
RUN addgroup -S -g 101 mymediadb && adduser -S -u 100 -G mymediadb -H -h /nonexistent mymediadb

# Only the compiled binary.
COPY --from=builder-go /mymediadb /mymediadb

# Persistent data directory (SQLite + downloaded artwork). Created here and its
# ownership set to the image user: when a named volume is
# mounted on /data for the first time, Docker copies the owner and permissions
# of the image directory to the new volume. Without this chown the volume would
# be born root:root and the non-root process would fail to create the database
# ("unable to open database file"). The `VOLUME` declares the mount point: without
# -v, Docker backs it with an anonymous volume, so what the app writes there
# survives a recreated container. That only holds because the ENV below actually
# points the database and the artwork at /data.
RUN mkdir -p /data && chown mymediadb:mymediadb /data
VOLUME /data

# Point the data at the volume above. The binary's own default is
# tmp/mymediadb.sqlite -- a development path that, with the working directory at
# the filesystem root, resolves to /tmp: the writable layer, which is gone the
# next time the container is recreated. Measured, not assumed: a plain
# `docker run -v host:/data` left the database in /tmp and /data empty.
ENV MYMEDIADB_DB_PATH=/data/mymediadb.sqlite \
    MYMEDIADB_ASSETS_DIR=/data/assets

# The entrypoint starts as root ONLY to fix the ownership of /data and drop to PUID:PGID
# (99:100 by default); with `--user` it skips all of that and execs directly.
COPY --chmod=755 docker/entrypoint.sh /entrypoint.sh

EXPOSE 8080

# Healthcheck using busybox wget (present in alpine). start-period loose enough
# but short enough to turn healthy fast in the test.
# The port comes from PORT (with the application's 8080 default) because the
# shell form of HEALTHCHECK expands the variable: hardcoding 8080 left the
# container forever 'unhealthy' in any environment running on another port.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider "http://localhost:${PORT:-8080}/api/v1/health" || exit 1

ENTRYPOINT ["/entrypoint.sh"]
