# Deploy

A practical guide to putting MyMediaDB into production: a single Docker image,
published on the GitHub Container Registry and kept up to date on its own.

## 1. Overview

MyMediaDB runs as a single Docker image (Go + embedded SPA, `Dockerfile`
at the root), published on the GitHub Container Registry as
`ghcr.io/rafaelvieiras/my-media-db` — a **public** image, so no registry
login is needed to pull it.

The image is **multi-arch**: `linux/amd64`, `linux/arm64` and `linux/arm/v7`. Docker
picks the right variant on its own from the multi-arch manifest, with no
extra configuration on the target.

End-to-end flow:

1. Push to `master` (or a `v*` tag, or a manual trigger) → the workflow
   `.github/workflows/release.yml` builds the image for the 3 architectures and
   publishes it to ghcr with the `:latest` tag (plus short `sha` and, on tags, semver).
2. On the target (a home server, a Raspberry Pi, etc.), `compose.yaml` brings up the
   app **and** a Watchtower that keeps an eye on the registry.
3. Every 5 minutes Watchtower checks whether `:latest` changed; if it did, it pulls the
   new image, recreates the container and cleans up the old image.

There is no manual deploy step in day-to-day work: after the initial setup, a push
to master reaches the target by itself.

## 2. Prerequisites

- Docker + Docker Compose (the `docker compose` plugin) installed on the target.
- Network access to `ghcr.io` — the image `ghcr.io/rafaelvieiras/my-media-db`
  is public, so pulling it requires no credentials at all (the CI uses the
  workflow's automatic token to publish; the target needs nothing).

## 3. Bring it up on a test host

```bash
# 1) Template of optional credentials (TMDB/fanart.tv/TVDB)
cp .env.example .env
# ... edit .env and fill in whatever you have (you can leave everything empty)

# 2) Pull the published image
docker compose -f compose.yaml pull

# 3) Bring up app + Watchtower
docker compose -f compose.yaml up -d
```

Reach it at `http://localhost:8080` (the `8080:8080` mapping is already defined in the
compose file). To follow the app logs:

```bash
docker compose -f compose.yaml logs -f app
```

## 4. Auto-update

The `watchtower` service in `compose.yaml` watches containers with the label
`com.centurylinklabs.watchtower.enable=true` (only `app` has that label) and,
every `WATCHTOWER_POLL_INTERVAL=300` seconds (5 min), checks whether the `:latest` tag
changed on ghcr. If it did, it pulls the new image, recreates the `mymediadb`
container with the same volumes/env, and removes the old image
(`WATCHTOWER_CLEANUP=true`) so it does not pile up garbage on disk.

To force an update right away, without waiting for the poll:

```bash
docker compose -f compose.yaml pull && docker compose -f compose.yaml up -d
```

Or, alternatively, restart only Watchtower (it checks as soon as it comes up):

```bash
docker compose -f compose.yaml restart watchtower
```

## 5. Raspberry Pi (1GB RAM)

The same steps from sections 3–4 apply to the Pi: copy `compose.yaml`,
`.env.example` (renamed to `.env`) and run `docker compose -f compose.yaml up -d`.

- The multi-arch manifest makes `:latest` resolve automatically to the
  Pi's architecture — **32-bit** → `linux/arm/v7`; **64-bit** → `linux/arm64`.
  There is nothing to choose manually.
- `WATCHTOWER_CLEANUP=true` is essential here: without it, every update leaves
  the old image on the SD card, which is small.
- The Go binary (static, no CGO) + pure-Go SQLite (modernc) run
  comfortably in 1GB of RAM — there is no JVM, Node, or build toolchain in the
  final image (just the binary + ffmpeg).

## 6. Troubleshooting

| Symptom | Probable cause | Action |
|---|---|---|
| `manifest unknown` / `denied` on `pull` | Wrong image reference or tag | Confirm `ghcr.io/rafaelvieiras/my-media-db:latest` and retry |
| `exec format error` when starting the container | Manifest arch does not match the host | It should not happen with the 3 published targets (amd64/arm64/armv7); check `docker version` → `Server.Arch` |
| Port 8080 already in use | Another service occupying the port on the host | Change the mapping in `compose.yaml` (e.g. `"8081:8080"`) |
| App comes up but external search (TMDB/fanart.tv/TVDB) does not work | `.env` empty or missing the keys | Fill `.env` from `.env.example` — it is optional, but without a key the search stays off |
