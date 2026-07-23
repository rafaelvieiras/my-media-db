# 0003 — Empacotamento: Dockerfile multi-stage (imagem única)

> Uma imagem que builda o front, compila o binário Go com o `dist` embutido e roda
> tudo num container enxuto.

| | |
|---|---|
| **ID** | 0003 |
| **Tipo** | chore |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único |
| **Status** | done |
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
- **Builder web = `node:lts-slim`** (decisão de implementação, 2026-07-08). O
  `package-lock.json` gerado no scaffold do 0002 estava num estado não-normalizado que
  o `npm ci` do npm ≥ 11.16 (empacotado no `node:lts`) rejeitava (`Missing:
  @emnapi/core … from lock file`). Correção de raiz: **regenerar o lock** (`npm install`
  limpo o normaliza) — com ele o `npm ci` passa em `node:lts` sem pin. Preferido a pinar
  um patch exato do Node (que fixaria o npm antigo e mascararia o lock quebrado). O
  builder é estágio descartado, então usar a base LTS cheia/slim não afeta a imagem final.
- **Atualização (2026-07-23, decisão do dono):** `node:lts-slim`/`node:lts` trocados por
  `node:24-slim` (Dockerfile) e `node:24` (`compose.dev.yaml`) — fixa o **major**
  (continua flutuando em patch/minor dentro do 24.x, mesmo comportamento do
  `node-version: '24'` já usado em `.github/workflows/*.yml`). Motivo: o dono pediu o
  pin explícito após o mesmo sintoma (`Missing: @emnapi/… from lock file`) se repetir
  no briefing 0070 — mesmo com o lockfile correto, `node:lts` deixa a versão do Node
  (logo do npm embutido) à mercê de quando a imagem é *pulled*, e alinhar Docker+CI+local
  no mesmo major reduz a superfície de divergência. A correção de raiz continua sendo
  regenerar o lock com um npm ≥ 11.16 (ver briefing 0005 §"lockfile npm estrito");
  o pin de major é reforço, não substitui isso.
- Imagem final medida: **~16 MB** (alpine + binário estático ~7,6 MB).

## 7. Fora deste briefing / follow-ups

- Adicionar **`ffprobe`** à imagem final quando o scan entrar (Fase 2) — com o alpine é
  só `apk add ffmpeg`, **sem trocar de base** (motivo da escolha do alpine, ver §6).
- `docker-compose` de exemplo com volumes de mídia (Fase 2).
- Pipeline de CI e publicação multi-arch (ARM incluso) da imagem.
