# 0003 — Empacotamento: Dockerfile multi-stage (imagem única)

> Uma imagem que builda o front, compila o binário Go com o `dist` embutido e roda
> tudo num container enxuto.

| | |
|---|---|
| **ID** | 0003 |
| **Tipo** | chore |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único |
| **Status** | ready |
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
3. **Final:** base **`alpine`**, copia só o binário, `USER` não-root, `EXPOSE 8080`,
   `HEALTHCHECK` batendo em `/api/v1/health` via o **`wget` do busybox** (já presente no
   alpine — sem shell script extra nem subcomando no binário), `ENTRYPOINT` no binário.

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

- `CGO_ENABLED=0` casa com `modernc.org/sqlite` ([ADR-0014](../docs/adr/0014-stack-de-implementacao.md));
  o binário é estático e roda no alpine (musl) sem depender de glibc.
- **Base `alpine` escolhida** (decisão de sessão, 2026-07-08) em vez de
  `distroless/static`. Motivo: o alpine traz shell e `apk`, o que (a) permite o
  `HEALTHCHECK` usar o `wget` do busybox sem tocar no binário e (b) facilita adicionar
  o **`ffprobe`** na Fase 2 (`apk add ffmpeg`) **sem trocar de base**. Trade-off aceito:
  imagem um pouco maior e superfície maior que a distroless. Alternativa descartada:
  distroless/static (menor/mais segura, mas exigiria subcomando `healthcheck` no
  binário e troca de base quando o FFmpeg entrasse).
- `HEALTHCHECK`: `wget -q --spider http://localhost:8080/api/v1/health` (busybox), com
  `--start-period` folgado para o boot. Sem subcomando dedicado no binário — o 0001
  permanece enxuto.

## 7. Fora deste briefing / follow-ups

- Adicionar **`ffprobe`** à imagem final quando o scan entrar (Fase 2) — com o alpine é
  só `apk add ffmpeg`, **sem trocar de base** (motivo da escolha do alpine, ver §6).
- `docker-compose` de exemplo com volumes de mídia (Fase 2).
- Pipeline de CI e publicação multi-arch (ARM incluso) da imagem.
