# syntax=docker/dockerfile:1

# Empacotamento multi-stage do MyMediaDB: builda o SPA (Vite), compila o binário
# Go com o SPA embutido via //go:embed e entrega uma imagem final enxuta.
# O build NÃO depende de artefatos locais pré-buildados: o dist real é gerado
# no estágio 1 e copiado para dentro do módulo Go antes da compilação.

# Versão do binário, injetada via -ldflags. Sobrescreva com --build-arg VERSION=...
ARG VERSION=docker

# ---------------------------------------------------------------------------
# Estágio 1 — builder-web: gera o SPA de produção com Vite.
# ---------------------------------------------------------------------------
FROM node:lts-slim AS builder-web
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
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder-go
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

# Compilação estática (sem CGO) com versão injetada e binário enxuto (-s -w).
ARG VERSION
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.version=${VERSION} -s -w" \
    -o /mymediadb ./cmd/mymediadb

# ---------------------------------------------------------------------------
# Estágio 3 — final: imagem mínima, sem toolchain Node/Go.
# ---------------------------------------------------------------------------
FROM alpine:3.22 AS final

# Usuário não-root dedicado.
RUN addgroup -S mymediadb && adduser -S -G mymediadb -H -h /nonexistent mymediadb

# Apenas o binário compilado.
COPY --from=builder-go /mymediadb /mymediadb

USER mymediadb

EXPOSE 8080

# Healthcheck usando o wget do busybox (presente no alpine). start-period folgado
# mas curto o suficiente para virar healthy rápido no teste.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/api/v1/health || exit 1

ENTRYPOINT ["/mymediadb"]
