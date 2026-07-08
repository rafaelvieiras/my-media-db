# 0003 — Empacotamento: Dockerfile multi-stage (imagem única)

> Uma imagem que builda o front, compila o binário Go com o `dist` embutido e roda
> tudo num container enxuto.

| | |
|---|---|
| **ID** | 0003 |
| **Tipo** | chore |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único |
| **Status** | draft |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [ADR-0014](../docs/adr/0014-stack-de-implementacao.md) |
| **Depende de** | [0001](0001-backend-esqueleto-servidor.md), [0002](0002-frontend-scaffold-integracao-api.md) |

---

## 1. Objetivo

Entregar o **formato de distribuição** do produto: *uma imagem, um processo* (ver
[03](../docs/03-arquitetura.md)). Fecha a Fase 1 provando que front + API viajam
juntos num container que qualquer um sobe com um `docker run`.

## 2. Jornada do usuário

1. O operador roda `docker build -t mymediadb .`.
2. Depois `docker run -p 8080:8080 mymediadb`.
3. Abre `http://localhost:8080` → o SPA carrega e mostra **"API online"** (mesmo origin).
4. `GET /api/v1/health` responde `200`. O `HEALTHCHECK` do container fica *healthy*.

## 3. Escopo

**Dentro:**
- **Dockerfile multi-stage**: (1) Node builda `web/` → `dist`; (2) Go compila o
  binário com o `dist` embutido; (3) imagem final mínima só com o binário.
- `.dockerignore` (não copiar `node_modules`, `.git`, artefatos locais).
- Imagem final enxuta, **usuário não-root**, `EXPOSE 8080`, `HEALTHCHECK`.

**Fora (explicitamente):**
- `docker-compose` com volumes de mídia (Fase 2, quando houver scan).
- CI/publicação da imagem em registry; build multi-arch (follow-up).
- Empacotar `ffprobe` (só necessário quando o scan chegar, na Fase 2).

## 4. Contrato técnico

### Estágios
1. **`node:lts` (builder-web):** copia `web/`, `npm ci`, `npm run build` →
   `internal/webui/dist`.
2. **`golang` (builder-go):** copia o código + o `dist` do estágio 1; compila com
   **`CGO_ENABLED=0`** (binário estático — permitido pelo driver CGO-free do
   [ADR-0014](../docs/adr/0014-stack-de-implementacao.md)); injeta `version` via
   `-ldflags`.
3. **Final:** base mínima (**`gcr.io/distroless/static`** ou `alpine`), copia só o
   binário, `USER` não-root, `EXPOSE 8080`, `HEALTHCHECK` batendo em
   `/api/v1/health`, `ENTRYPOINT` no binário.

### Arquivos afetados
- Criar `Dockerfile`, `.dockerignore`.

## 5. Critérios de aceite

- [ ] `docker build .` conclui sem erro, sem depender de artefatos locais pré-buildados.
- [ ] `docker run -p 8080:8080` serve o SPA em `/` **e** `health` em `/api/v1/health`
      no mesmo *origin*.
- [ ] A imagem final **não** contém toolchain Node/Go (só o binário + base mínima).
- [ ] O container roda como **usuário não-root**.
- [ ] `docker inspect` mostra o `HEALTHCHECK` reportando *healthy*.

## 6. Notas de implementação / decisões

- `CGO_ENABLED=0` casa com `modernc.org/sqlite` ([ADR-0014](../docs/adr/0014-stack-de-implementacao.md))
  e habilita a imagem `distroless/static` (sem libc).
- `HEALTHCHECK` numa imagem distroless (sem shell/curl) usa o próprio binário com um
  subcomando `healthcheck`, ou um pequeno probe estático — decidir na implementação.

## 7. Fora deste briefing / follow-ups

- Adicionar **`ffprobe`** à imagem final quando o scan entrar (Fase 2) — provavelmente
  troca a base `static` por uma com o FFmpeg disponível.
- `docker-compose` de exemplo com volumes de mídia (Fase 2).
- Pipeline de CI e publicação multi-arch (ARM incluso) da imagem.
