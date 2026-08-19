# syntax=docker/dockerfile:1

# Empacotamento multi-stage do MyMediaDB: builda o SPA (Vite), compila o binário
# Go com o SPA embutido via //go:embed e entrega uma imagem final enxuta.
# O build NÃO depende de artefatos locais pré-buildados: o dist real é gerado
# no estágio 1 e copiado para dentro do módulo Go antes da compilação.
#
# Estratégia multi-arquitetura (amd64 + arm64 + arm/v7, alvo Raspberry Pi):
# CROSS-COMPILAÇÃO, não emulação QEMU. Os estágios builder-web e builder-go
# rodam SEMPRE na arch nativa do runner (--platform=$BUILDPLATFORM): o dist
# do Vite é arch-neutro (HTML/JS/CSS) e o binário Go (CGO_ENABLED=0, SQLite
# puro-Go via modernc) cross-compila trivialmente para GOOS/GOARCH/GOARM
# alvo. Isso evita emular `npm run build`/`go build` sob QEMU, o que seria
# lento e arriscaria OOM no armv7. Só o estágio `final` (apk add ffmpeg) é
# buildado para a plataforma ALVO de verdade, pois precisa do ffmpeg nativo.

# Versão do binário, injetada via -ldflags. Sobrescreva com --build-arg VERSION=...
ARG VERSION=docker

# ---------------------------------------------------------------------------
# Estágio 1 — builder-web: gera o SPA de produção com Vite.
# --platform=$BUILDPLATFORM: força rodar na arch nativa do runner, nunca
# emulada — o dist gerado é arch-neutro, então não há motivo pra buildar
# via QEMU (mais lento à toa).
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:24-slim AS builder-web
WORKDIR /app/web

# Camada de dependências: só o manifest muda raramente, aproveita cache.
COPY web/package.json web/package-lock.json ./
RUN npm ci

# Código-fonte do front e build. O vite.config.ts aponta build.outDir para
# ../internal/webui/dist, então a saída vai para /app/internal/webui/dist.
COPY web/ ./
RUN npm run build

# ---------------------------------------------------------------------------
# Estágio 2 — builder-go: compila o binário estático com o SPA embutido.
# --platform=$BUILDPLATFORM: o toolchain Go também roda na arch nativa do
# runner (evita QEMU pro `go build` em si); quem determina a arch do
# BINÁRIO final é o cross-compile via GOOS/GOARCH/GOARM abaixo, não a
# plataforma do estágio. Versão do Go pinada em 1.26.6 (patch exato, mesma
# versão fixada no CI por vuln de stdlib/determinismo do build).
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine AS builder-go
WORKDIR /app

# Camada de dependências Go.
COPY go.mod go.sum ./
RUN go mod download

# Código Go da aplicação.
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Sobrescreve o placeholder versionado pelo dist REAL vindo do estágio 1, para
# que o //go:embed all:dist capture o SPA de produção.
COPY --from=builder-web /app/internal/webui/dist/ ./internal/webui/dist/

# Compilação estática (sem CGO) com versão injetada e binário enxuto (-s -w),
# CROSS-COMPILADA para a plataforma ALVO. TARGETOS/TARGETARCH/TARGETVARIANT
# são preenchidos automaticamente pelo BuildKit conforme o --platform alvo
# passado ao `docker build`/`buildx build` (ex.: linux/arm/v7 → TARGETOS=linux,
# TARGETARCH=arm, TARGETVARIANT=v7). GOARM=${TARGETVARIANT#v} extrai o "7" de
# "v7" para o armv7 do Raspberry Pi; em amd64/arm64 a variant vem vazia e
# GOARM="" é simplesmente ignorado pelo toolchain Go.
ARG VERSION
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build \
    -ldflags "-X main.version=${VERSION} -s -w" \
    -o /mymediadb ./cmd/mymediadb

# ---------------------------------------------------------------------------
# Estágio 3 — final: imagem mínima, sem toolchain Node/Go.
# SEM --platform=$BUILDPLATFORM aqui de propósito: este estágio é buildado
# para a plataforma ALVO (comportamento padrão do Docker), pois o `apk add
# ffmpeg` precisa trazer o pacote da arch certa (amd64/arm64/armv7) — só o
# binário Go copiado de builder-go já vem cross-compilado para essa arch.
# ---------------------------------------------------------------------------
FROM alpine:3.22 AS final

# Dependências de RUNTIME:
#   ffmpeg  → ffprobe do scanner (ADR-0014, briefing 0009). Sem ele o scan ainda
#             roda, mas degrada (mediainfo vazio).
#   sqlite  → CLI usado por scripts/backup.sh para `VACUUM INTO` (snapshot
#             consistente do banco WAL sem downtime). O app em si usa o driver
#             Go puro modernc e NÃO depende deste binário; é só para o backup.
#   python3 → runtime do binário yt-dlp baixado dinamicamente em runtime
#             (briefing 0101, ADR-0025): o pacote `internal/ytdlp` baixa o
#             asset "yt-dlp" (zipapp Python plataforma-independente, shebang
#             `#!/usr/bin/env python3`) da release oficial, NUNCA o standalone
#             por SO (`yt-dlp_linux` etc., build PyInstaller ligado a glibc) --
#             esta imagem é Alpine/musl e o standalone falha em runtime com
#             "no such file or directory" (nem `gcompat` resolve: falha mais
#             adiante por símbolo glibc ausente na libpython embutida). Achado
#             na 1ª validação em produção do briefing 0101.
# --no-cache mantém a imagem enxuta.
RUN apk add --no-cache ffmpeg sqlite python3

# Usuário não-root dedicado.
RUN addgroup -S mymediadb && adduser -S -G mymediadb -H -h /nonexistent mymediadb

# Apenas o binário compilado.
COPY --from=builder-go /mymediadb /mymediadb

# Diretório de dados persistente (SQLite + arte baixada). Criado e com dono
# ajustado para o usuário non-root ANTES do `USER`: quando um volume nomeado é
# montado em /data na primeira vez, o Docker copia dono e permissões do
# diretório da imagem para o volume novo. Sem este chown o volume nasceria
# root:root e o processo non-root falharia ao criar o banco ("unable to open
# database file"). O `VOLUME` declara o ponto de montagem (mesmo sem -v, um
# volume anônimo garante que os dados não se percam no filesystem efêmero).
# Em produção o compose aponta MYMEDIADB_DB_PATH/MYMEDIADB_ASSETS_DIR para /data.
RUN mkdir -p /data && chown mymediadb:mymediadb /data
VOLUME /data

USER mymediadb

EXPOSE 8080

# Healthcheck usando o wget do busybox (presente no alpine). start-period folgado
# mas curto o suficiente para virar healthy rápido no teste.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/api/v1/health || exit 1

ENTRYPOINT ["/mymediadb"]
