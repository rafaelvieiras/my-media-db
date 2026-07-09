# 0004 — Ambiente de desenvolvimento: compose com hot reload

> Uma "bancada de trabalho" em Docker que sobe front e back lado a lado, cada um com
> recarga em tempo real — salvar um arquivo reflete na hora, sem rebuild.

| | |
|---|---|
| **ID** | 0004 |
| **Tipo** | chore |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único |
| **Status** | done |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [ADR-0014](../docs/adr/0014-stack-de-implementacao.md) |
| **Depende de** | [0001](0001-backend-esqueleto-servidor.md), [0002](0002-frontend-scaffold-integracao-api.md) |

---

## 1. Objetivo

Dar um **loop de desenvolvimento rápido**. O [0003](0003-empacotamento-dockerfile-multistage.md)
produz a imagem de produção (imutável, imprópria para iterar). Este briefing entrega o
oposto: um ambiente onde **salvar um arquivo reflete imediatamente** — HMR no front e
recompilação automática no back — para não haver atrito no dia a dia da Fase 2 em diante.
Prioridade declarada: **o front**.

## 2. Jornada do usuário

A "jornada" é do **dev**:

1. Roda `docker compose up` (arquivo de dev) na raiz do repo.
2. Sobem dois serviços: **web** (Vite dev server na `5173`) e **api** (binário Go na
   `8080`, sob um *watcher*).
3. O dev abre `http://localhost:5173`. O proxy do Vite encaminha `/api` → serviço `api`
   (mesmo contrato relativo da produção, **sem CORS**).
4. Edita um `.tsx` → a tela **atualiza na hora** (HMR), sem recarregar a página nem
   perder estado.
5. Edita um `.go` → o **air** recompila e reinicia o binário em segundos; o próximo
   request já usa o código novo.
6. `Ctrl+C` derruba os dois serviços de forma limpa.

## 3. Escopo

**Dentro:**
- Arquivo **`compose.dev.yaml`** (ou `docker-compose.dev.yml`) com dois serviços:
  - **web**: imagem `node`, monta `web/` como volume, roda `npm run dev`, expõe `5173`.
  - **api**: imagem `golang`, monta o código Go como volume, roda **air**, expõe `8080`.
- **`.air.toml`** configurando o watcher (o quê observar, comando de build, binário de saída).
- Proxy do Vite (`server.proxy`) apontando `/api` → serviço `api` (definido no
  [0002](0002-frontend-scaffold-integracao-api.md); aqui só se garante o host correto no compose).
- README curto de "como desenvolver" (subir, portas, o que recarrega).

**Fora (explicitamente):**
- A imagem/pipeline de **produção** — é o [0003](0003-empacotamento-dockerfile-multistage.md).
- Volumes de **mídia** e o scan (Fase 2).
- Banco/persistência, seeds, migrações (Fase 2).
- Perfis de compose para staging/CI.

## 4. Contrato técnico

### Serviço `web` (front, HMR)
- Base `node:lts`; `working_dir /app`; volume `./web:/app` + volume anônimo para
  `/app/node_modules` (não deixar o `node_modules` do host mascarar o do container).
- `command: npm run dev -- --host` — `--host` (ou `server.host: true`) é necessário para
  o dev server ser acessível de fora do container.
- Porta `5173:5173`. Se o file-watching não disparar no bind mount da plataforma, ligar
  `usePolling` no Vite (`server.watch.usePolling`) — decidir na implementação.
- O HMR usa WebSocket na própria `5173`; a porta mapeada já cobre.

### Serviço `api` (back, air)
- Base `golang`; volume com o código Go; **air** como processo principal.
- `.air.toml`: observa `**/*.go`, ignora `web/`, `internal/webui/dist/`, `tmp/`;
  `full_bin`/`cmd` compilando `./cmd/mymediadb` com `CGO_ENABLED=0`.
- Porta `8080:8080`. Env `PORT` opcional (default do [0001](0001-backend-esqueleto-servidor.md)).
- **air é dependência só de dev**: não entra na imagem de produção ([0003](0003-empacotamento-dockerfile-multistage.md)).

### Rede / contrato dev = prod
- O front chama **`/api` relativo**; o proxy do Vite resolve para o serviço `api`. Não
  há CORS — dev espelha o mesmo *origin* relativo da produção.
- `VITE_API_BASE` permanece **vazio** no compose (é apenas *override* para casos remotos).

### Arquivos afetados
- Criar `compose.dev.yaml`, `.air.toml`.
- `vite.config.ts`: garantir `server.host` e o `server.proxy` (base vinda do 0002).
- `.dockerignore` / `.gitignore`: incluir `tmp/` do air.

## 5. Critérios de aceite

- [ ] `docker compose -f compose.dev.yaml up` sobe **web** (`5173`) e **api** (`8080`).
- [ ] Abrir `http://localhost:5173` mostra **"API online"** (front via proxy → api, sem CORS).
- [ ] Editar um `.tsx` reflete na tela **sem recarregar a página** (HMR ativo).
- [ ] Editar um `.go` dispara **rebuild + restart** automático do binário (air), sem ação manual.
- [ ] Nenhum artefato deste ambiente vaza para a imagem de produção ([0003](0003-empacotamento-dockerfile-multistage.md)).
- [ ] `Ctrl+C` / `compose down` encerra os dois serviços de forma limpa.

## 6. Notas de implementação / decisões

- **Proxy do Vite em vez de CORS** (decisão de sessão, 2026-07-08): o front usa caminho
  relativo idêntico ao de produção e o Vite encaminha `/api` ao back. Mantém o
  [0001](0001-backend-esqueleto-servidor.md) sem CORS e faz **dev espelhar prod**.
  Alternativa descartada: `VITE_API_BASE=http://localhost:8080` cross-origin, que
  exigiria CORS no Go e faria dev divergir de prod.
- **air** como watcher do Go (decisão de sessão, 2026-07-08): mais difundido, config
  simples via `.air.toml`. Alternativa descartada: `wgo` (menor, mas menos exemplos).
- Volume anônimo em `/app/node_modules` é o padrão para não sobrepor as deps do container
  com as do host (que podem ser de outra arquitetura).
- `usePolling` pode ser necessário conforme o sistema de arquivos do host — deixar
  documentado e ligar se o watch não disparar. Na implementação **não foi necessário**
  (ext4 local dispara HMR e air nativamente); fica documentado como fallback no CONTRIBUTING.
- **Alvo do proxy configurável** (decisão de implementação): dentro do container `web`,
  `localhost:8080` não é o serviço `api`. O `vite.config.ts` passou a usar
  `process.env.VITE_PROXY_TARGET || 'http://localhost:8080'`; o compose define
  `VITE_PROXY_TARGET=http://api:8080`. Assim o `npm run dev` local segue idêntico e o
  compose resolve pelo DNS do compose — sem divergir dev de prod.
- **`-buildvcs=false` no `.air.toml`** (necessário): com o repo bind-mounted, o git
  dentro do container acusa "dubious ownership" e o `go build` falha no VCS stamping;
  em dev não se precisa do stamp. O `api` usa um `Dockerfile.dev` que só instala o
  **air** (módulo atual `github.com/air-verse/air`), com caches Go em volumes nomeados.

## 7. Fora deste briefing / follow-ups

- Serviço de **banco/seed** no compose quando a persistência entrar (Fase 2).
- Volume de **mídia** montado para exercitar o scan (Fase 2).
- Perfil de compose para **testes/CI** e, eventualmente, staging.
