# Contribuindo com o MyMediaDB

Obrigado pelo interesse! O MyMediaDB é software livre sob a licença
[MIT](LICENSE). Este guia resume **como o projeto trabalha** — leia antes de
abrir um PR grande, porque o processo é deliberadamente orientado a decisão e a
documentação, não só a código.

> **Status:** `Fase 0 — documentação em draft`. Neste momento o trabalho é
> majoritariamente de **fundação**: decidir e escrever o modelo antes de codar.

## Como o projeto pensa

Cinco princípios governam qualquer contribuição (detalhe no [README](README.md)):

1. **O dono é a autoridade.** O banco local é a fonte de verdade; dado externo é
   sugestão. Toda edição do usuário é um **override** que vence sempre.
2. **O banco é maior que a biblioteca.** Cataloga o que se tem *e* o que ainda
   não se tem, via `status` — nunca gambiarra.
3. **Universal e extensível sem tocar no núcleo.** Verticais e integrações vivem
   em `extension_data` *namespaced*; não se adicionam colunas por tipo.
4. **Local-first e leve.** Um binário, uma imagem, memória mínima. Fontes
   externas são enriquecimento opcional.
5. **Não-destrutivo.** Nada apaga dado do dono automaticamente; purga é sempre
   explícita.

Um PR que contraria um desses princípios provavelmente será recusado — abra uma
discussão antes.

## Decisões primeiro, código depois

Decisões de fundação são registradas e rastreadas, não improvisadas no código:

- A **[agenda de decisões](docs/00-agenda-de-decisoes.md)** é o backlog vivo: cada
  item nasce `aberto`, passa por discussão e vira `documentado`.
- Decisões fechadas viram **[ADRs](docs/adr/)** (formato leve) e são promovidas às
  docs de spec (`docs/03`, `docs/04`, …).
- O desenvolvimento é dirigido por **[briefings](briefings/)** — ordens de serviço
  autocontidas, uma por feature/fix.

Antes de propor mudança estrutural (schema, resolver, API), verifique se ela toca
uma decisão já fechada. Se tocar, o caminho é **propor um novo ADR que supere o
anterior**, não editar a spec direto.

## Fluxo de contribuição

1. **Abra uma issue** descrevendo o problema/ideia antes de codar algo grande.
2. **Faça um fork e um branch** a partir de `master` (ex.: `feat/scan-incremental`).
3. **Siga a stack e o estilo** do código vizinho (Go no backend, Vite/React no
   front — ver [docs/03](docs/03-arquitetura.md)).
4. **Escreva em português brasileiro** nas docs e mensagens de commit, com acentuação
   correta (o projeto é mantido nesse idioma).
5. **Mensagens de commit** no formato `tipo: resumo` (ex.: `docs:`, `feat:`,
   `fix:`, `refactor:`), no imperativo.
6. **Abra o PR** referenciando a issue e, quando aplicável, o ADR/briefing.

## Desenvolvimento com hot reload

A "bancada de trabalho" (briefing [0004](briefings/0004-ambiente-de-desenvolvimento-hot-reload.md))
sobe front e back lado a lado, cada um com recarga em tempo real. Só precisa de
Docker + Compose v2:

```sh
docker compose -f compose.dev.yaml up --build
```

Sobem dois serviços:

| Serviço | Porta | O que roda | O que recarrega |
|---|---|---|---|
| **web** | http://localhost:5173 | Vite dev server (`npm run dev --host`) | edite um `.tsx`/`.css` → **HMR** troca o módulo na tela, sem recarregar a página |
| **api** | http://localhost:8080 | binário Go sob o [air](https://github.com/air-verse/air) | edite um `.go` → **rebuild + restart** automático em segundos |

Abra **http://localhost:5173**: o front chama `/api` relativo e o **proxy do
Vite** encaminha para o serviço `api` (mesmo contrato de produção, **sem CORS**).
O alvo do proxy vem de `VITE_PROXY_TARGET=http://api:8080` (definido no compose,
porque dentro do container `localhost` não é o serviço `api`); rodando
`npm run dev` fora do Docker, o default `http://localhost:8080` continua valendo.

`Ctrl+C` (ou `docker compose -f compose.dev.yaml down`) derruba os dois de forma
limpa. O `air` e o `Dockerfile.dev` são dependências **só de dev** — não entram
na imagem de produção (briefing [0003](briefings/0003-empacotamento-dockerfile-multistage.md)).

> **File watching:** se ao salvar um arquivo o HMR (front) ou o rebuild (back)
> não disparar — comum em bind mounts de alguns sistemas de arquivos —, ligue
> polling: no front, `server.watch.usePolling = true` em `web/vite.config.ts`.

## Quality gate e hooks de git

O projeto tem uma **catraca de qualidade unificada** (Go + TS) — um único
`baseline.json` na raiz que só permite as métricas **melhorarem**. Ela roda no
`pre-push` local e no PR (ver [briefing 0005](briefings/0005-quality-gate-catraca-unificada.md)
e [ADR-0016](docs/adr/0016-quality-gate-catraca-unificada.md)).

**Instale os hooks uma vez** (usa [lefthook](https://github.com/evilmartians/lefthook)):

```sh
lefthook install
# lefthook via: go install github.com/evilmartians/lefthook@latest
#          ou:  npm install -g lefthook   (ou npx lefthook install)
```

Isso ativa três ganchos:

- **`pre-commit`** — auto-formata os arquivos staged (`gofmt -w` / `prettier --write`)
  e os re-adiciona. Precisa só de `gofmt` (vem com o Go) e do `prettier` (no `web/`).
- **`pre-push`** — roda a catraca (`node scripts/quality-gate.mjs --local`) e
  **aborta o push** em regressão bloqueante.
- **`commit-msg`** — valida Conventional Commits (`tipo: resumo`, em pt-BR).

**Ferramentas do `pre-push`.** A catraca mede o Go com ferramentas que precisam
estar no `PATH`. Se faltar alguma, a métrica é **pulada com aviso** (não quebra),
mas o ideal é tê-las todas:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
# garanta que "$(go env GOPATH)/bin" está no seu PATH
```

> **Nota:** `govulncheck` é zero-tolerância. Mantenha o toolchain Go atualizado
> (o `go.mod` fixa `toolchain go1.26.5`, que já corrige as vulns de stdlib conhecidas).

**Ferramentas do lado web.** O front tem **ESLint** (`npm run lint`) e **vitest** com
cobertura v8 (`npm run test:coverage`, que gera `web/coverage/coverage-summary.json`).
A catraca lê a **cobertura de linhas** (`web_coverage_lines`, bloqueante — só sobe) e as
**issues de ESLint** (`web_lint_issues`, aviso). Por isso o `pre-push` e o CI exigem um
`npm install` em `web/` **antes** de rodar o gate — sem `node_modules` no `web/`, o
vitest e o ESLint não rodam e essas métricas são **puladas** (nunca quebram o gate, mas
deixam de proteger o front):

```sh
cd web && npm install    # instala eslint + vitest (devDeps) uma vez
npm run lint             # lint do front (0 = limpo)
npm run test:coverage    # testes + cobertura → web/coverage/coverage-summary.json
```

**Comandos à mão** (Makefile na raiz):

| Comando | O que faz |
|---|---|
| `make fmt` | Formata Go (`gofmt`) e web (`prettier`) |
| `make gate-local` | Roda a catraca com saída humana |
| `make gate` | Roda a catraca em modo CI (Markdown) |
| `make baseline` | Regenera e **avança** o `baseline.json` (só quando você genuinamente melhorou as métricas) |

## Segurança

**Segredos nunca no código.** Chaves de API e credenciais entram por
**variável de ambiente / config**, jamais commitadas. Se encontrar um segredo no
histórico, reporte de forma privada (não abra issue pública) para que seja
**rotacionado**.

## Licença da sua contribuição

Ao contribuir, você concorda em licenciar o seu trabalho sob a **licença MIT** do
projeto.
