# 0001 — Backend Go: esqueleto do servidor HTTP

> Um binário Go que sobe, responde `/api/v1/health` e serve o SPA embutido.

| | |
|---|---|
| **ID** | 0001 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único |
| **Status** | done |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [ADR-0014](../docs/adr/0014-stack-de-implementacao.md) |
| **Depende de** | — |

---

## 1. Objetivo

Estabelecer a **base física** de todo o produto: o processo Go único que serve a
API e o frontend no mesmo *origin* (ver [03](../docs/03-arquitetura.md)). Sem esse
esqueleto, nenhuma feature de biblioteca (Fase 2) tem onde rodar. O entregável é um
"hello world" de infraestrutura: sobe, responde saúde, e devolve o SPA para
qualquer rota não-API.

## 2. Jornada do usuário

Ainda não há UI de usuário final; a "jornada" é do **operador/dev**:

1. Roda o binário (`go run ./cmd/mymediadb` em dev, ou o binário compilado).
2. O servidor sobe na porta `8080` (configurável por env `PORT`).
3. `GET /api/v1/health` → `200` com JSON `{"status":"ok","version":"…"}`.
4. `GET /` ou qualquer rota não-`/api` → devolve o `index.html` do SPA (para o
   roteamento client-side funcionar em deep-links).
5. `GET /api/rota-inexistente` → `404` **JSON** (nunca o `index.html`, para não
   mascarar erro de API como página).
6. `Ctrl+C` (SIGINT/SIGTERM) → *graceful shutdown* (para de aceitar, drena, sai).

## 3. Escopo

**Dentro:**
- Servidor HTTP com **chi** ([ADR-0014](../docs/adr/0014-stack-de-implementacao.md)).
- `GET /api/v1/health` (JSON).
- Embed do SPA (`//go:embed`) + servir estáticos + **fallback de rota** para `index.html`.
- Config de porta por env; logging básico de requests; graceful shutdown.

**Fora (explicitamente):**
- SQLite / persistência e pragmas WAL (entram na Fase 2, ver [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md)).
- Qualquer rota `/api/v1/*` além de `health`; toda a `/api/tmdb`.
- Auth, TLS (fica a cargo de reverse proxy), CORS elaborado.

## 4. Contrato técnico

### Layout de diretórios (alinha com 0002 e 0003)

Por o `//go:embed` **não** acessar diretórios acima do pacote, o `dist` do Vite é
gerado **dentro** do pacote que o embute:

```
cmd/mymediadb/main.go          — entrypoint (flags, PORT, start/shutdown)
internal/server/server.go      — construção do router chi + middlewares
internal/server/health.go      — handler GET /api/v1/health
internal/server/static.go      — embed do SPA + fallback de rota
internal/webui/embed.go        — //go:embed all:dist  (var embedded fs.FS)
internal/webui/dist/index.html — placeholder commitado (para compilar sem build do front)
go.mod / go.sum
```

### API
- `GET /api/v1/health` → `200 application/json`: `{"status":"ok","version":"<ldflags>"}`.
- Prefixo `/api/*` que não casa rota → `404 application/json` `{"error":"not found"}`.

### Backend (Go)
- Módulo Go; router **chi**; middlewares de logger e recoverer.
- `internal/webui`: `//go:embed all:dist` + `fs.Sub(embedded, "dist")`.
- `static.go`: serve `assets/*` do FS embutido; qualquer outra rota **não-`/api`**
  responde `index.html` (SPA fallback). `/api/*` nunca cai no fallback.
- Versão injetada por `-ldflags "-X main.version=<v>"`; default `"dev"`.
- **CGO desabilitado** (coerente com o driver escolhido; ver ADR-0014).

### Arquivos afetados
- Criar todos os listados no layout acima.

## 5. Critérios de aceite

- [ ] `CGO_ENABLED=0 go build ./...` compila **sem** o front buildado (usa o placeholder).
- [ ] `curl localhost:8080/api/v1/health` → `200` e JSON com `status:"ok"`.
- [ ] `curl localhost:8080/qualquer/deep/link` → devolve o `index.html`.
- [ ] `curl localhost:8080/api/naoexiste` → `404` **JSON** (não HTML).
- [ ] `PORT=9090` muda a porta de escuta.
- [ ] SIGINT encerra com graceful shutdown (sem matar conexões em voo abruptamente).

## 6. Notas de implementação / decisões

- **chi** e **CGO-free** vêm do [ADR-0014](../docs/adr/0014-stack-de-implementacao.md).
- A distinção `/api/*` (404 JSON) × rota SPA (index.html) é deliberada: sem ela, um
  bug de API vira "página em branco" e some do radar.
- O placeholder `dist/index.html` commitado permite compilar/rodar o backend antes
  de o frontend existir — desacopla 0001 de 0002 no desenvolvimento.

## 7. Fora deste briefing / follow-ups

- Persistência SQLite + pragmas WAL e os dois pools de conexão (primeiro briefing da
  Fase 2 que tocar dados; ADR-0010).
- Cliente OpenAPI-tipado passa a valer quando houver rotas além de `health`.
