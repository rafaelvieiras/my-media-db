# 0028 — Testes end-to-end: validar o fluxo inteiro (UI + API) de forma rápida e programática

> Hoje a cobertura é UNIT (Go + Vitest) mais smoke tests MANUAIS via curl a cada
> feature. Falta a camada do meio/topo: validar as JORNADAS reais ponta a ponta —
> principalmente a interface — de forma rápida, programática e determinística, que
> cresça junto com cada implementação em vez de depender só de teste unitário e de
> validação manual. Este briefing desenha esse caminho em camadas, reusando o que a
> arquitetura já oferece.

| | |
|---|---|
| **ID** | 0028 |
| **Tipo** | infra/qualidade |
| **Fase (roadmap)** | Fase 3 — extensão pós-revisão |
| **Docs relacionadas** | [05-api](../docs/05-api.md), [ADR-0010](../docs/adr), [ADR-0012](../docs/adr) |
| **Status** | Fases 1 e 2 IMPLEMENTADAS (Camada 1 Go + Camada 2 Playwright, com cobertura de binário e front); Fase 3 planejada |
| **Depende de** | 0015 (enrich), 0025/0026/0027 (features a cobrir) |

---

## 1. Objetivo

- Validar o SISTEMA INTEIRO (SPA + API + banco) nas jornadas críticas, não só unidades.
- **Rápido e programático**: roda em CI/pre-push e localmente, sem passos manuais.
- **Determinístico**: nunca bate no TMDB real; relógio e dados controlados.
- **Incremental**: cada feature nova ganha 1 jornada; o custo marginal por jornada é baixo.
- **Barato de manter**: poucas jornadas de "caminho feliz + erros-chave", não exaustivo
  (o exaustivo continua nos unit tests).

Não-objetivos: substituir os unit tests (eles seguem sendo o grosso da cobertura);
testar visual/pixel (fora de escopo); cobrir toda combinação (só as jornadas de valor).

## 2. O que a arquitetura JÁ facilita (por que isto é barato aqui)

- **SQLite CGO-free (`modernc`)**: o binário compila e roda em qualquer runner sem
  dependência nativa; DB temporário por execução = um `MYMEDIADB_DB_PATH` novo.
- **Binário único servindo SPA + API no mesmo origin**: um processo pra subir, sem
  CORS/duplo servidor.
- **Provider é interface injetada** (`server.WithSearchProvider`) — só 4 métodos
  (`Nome`, `Buscar`, `BuscarDetalhe`, `MapearDetalhe`) + `ColecaoProvider.BuscarColecao`.
  Já existe um `fakeProvider`/`fakeColProvider` nos testes de enrich como ponto de partida.
- **baseURL do client TMDB é injetável** (`tmdb.New(cred, httpClient, baseURL)`) — dá pra
  apontar o client REAL a um TMDB mock local (fidelidade do mapeamento), se quisermos.
- **Seletores estáveis**: a UI usa `aria-label` em pt-BR e `data-testid` (ex.:
  `poster-placeholder`) — bons âncoras pra Playwright/RTL, sem depender de texto frágil.
- **openapi.yaml é a espinha do contrato** (o `api-types.ts` é gerado dele) — os E2E
  contra o binário real detectam deriva front↔back que os mocks à mão não pegam.

## 3. Decisão-chave: tornar a fonte externa DETERMINÍSTICA

É o que define quase toda a complexidade. Três opções, em ordem de custo:

1. **Fake provider de fixtures atrás de um flag** (RECOMENDADO como base). Ex.: env
   `MYMEDIADB_FAKE_PROVIDER=1` faz o `main` injetar um provider que serve payloads de
   `testdata/` (Matrix 603, coleção 10, similares, etc.) em vez do TMDB. Determinístico,
   sem rede, sem segredo. Reusa o padrão do `fakeProvider` existente.
   - **Imagens determinísticas**: o fake devolve `poster_url`/`backdrop_url` apontando a
     um host de fixtures local (ou uma imagem embutida) — o proxy/cache (0024) baixa dali,
     sem tocar o CDN do TMDB.
2. **Mock TMDB HTTP + baseURL apontado a ele**. Mais fiel (exercita o client+mapeamento
   REAIS do TMDB), custo um pouco maior (subir o mock, montar as respostas cruas). Viável
   porque o baseURL já é injetável. Bom para uma camada de contrato do provider.
3. **Record/replay (cassetes)**. Grava respostas reais uma vez e reproduz. Mais infra;
   só se quisermos fidelidade sem manter fixtures à mão.

**O fake provider é a PEÇA-CHAVE**: construído uma vez, alimenta as três camadas abaixo.
Começar por (1); (2) entra depois como camada de contrato do provider, se necessário.

## 3.1. Medir a COBERTURA do E2E (existe solução de mercado?)

Pergunta do dono: dá para ter um "% coberto pelo E2E", como a catraca tem no unitário?
**Sim** — e o nosso stack é particularmente favorável. Pesquisa (jul/2026):

- **Backend Go — cobertura do BINÁRIO real (Go 1.20+; temos 1.26)**: compilar com
  `go build -cover` gera um binário instrumentado; rodá-lo com `GOCOVERDIR=<dir>` faz
  cada execução gravar arquivos `covcounter`/`covmeta` naquele diretório; depois
  `go tool covdata textfmt -i=<dir> -o=perfil.txt` (ou `percent -i=<dir>`) extrai o
  perfil/percentual. Ou seja, a **Camada 2 (Playwright contra o binário) pode emitir a
  cobertura do backend** exercida pelas jornadas de navegador — a mesma métrica do
  unitário, só que da jornada real. `go tool covdata merge` funde vários diretórios
  (várias execuções/jornadas) num só. Ref.: [go.dev/blog/integration-test-coverage].
- **Camada 1 — cobertura como métrica dedicada na catraca (fazer agora)**: o
  `go test ./... -coverprofile` da catraca mede cobertura POR-PACOTE (cada pacote pelos
  SEUS testes), então um harness E2E num pacote à parte NÃO creditaria os pacotes que
  ele exercita. A forma correta de ter o "% do app coberto pelas JORNADAS" é uma métrica
  SEPARADA: `go test ./internal/e2e/... -coverpkg=./internal/... -coverprofile=e2e.out`
  → `go tool cover -func` → total %. Isso responde direto "quanto das linhas do app as
  jornadas tocam", sem misturar com o ratchet unitário (semânticas distintas). Entra na
  catraca como `e2e_coverage_lines` (ratchet próprio, só sobe).
- **Frontend — cobertura do navegador**: duas vias maduras — (a) instrumentar o build
  com `vite-plugin-istanbul`/`babel-plugin-istanbul` (o app expõe `window.__coverage__`,
  o Playwright lê após cada teste e o `nyc` reporta linha/branch do TSX real); (b)
  cobertura V8 nativa do Chromium (`page.coverage.start/stopJSCoverage`) convertida por
  `v8-to-istanbul`. A ferramenta de referência hoje é **`monocart-coverage-reports`**
  (gera V8 ou Istanbul, LCOV/HTML, aplica source maps e **funde** unit+E2E num relatório
  só). Refs.: [playwright.dev/docs/api/class-coverage], [npm monocart-coverage-reports].
- **Fusão unit+E2E**: dá para somar num número só (`go tool covdata merge` no backend;
  `nyc merge`/monocart no frontend). Mas a recomendação aqui é **NÃO fundir com o ratchet
  unitário**: manter `e2e_coverage_lines` como métrica própria (o E2E cobre JORNADAS, o
  unit cobre RAMOS — fundir esconde buraco de ramo atrás de jornada e vice-versa).

Ordem pragmática: começar pela métrica da Camada 1 (só `-coverpkg`, sem infra nova);
a cobertura do binário (GOCOVERDIR) e a do navegador (monocart) entram com a Camada 2.

## 4. As camadas (troféu de testes)

### Camada 1 — E2E black-box do BACKEND (fazer primeiro; custo baixo)
Um teste Go que sobe o `Server` REAL via `httptest.NewServer` com o fake provider +
`WithAssetsDir(tmp)` + DB temporário e exercita **jornadas de API inteiras** (as mesmas
que hoje valido por curl). Rápido (dentro do `go test`), determinístico, trivial de
estender (uma função por feature), e entra na catraca existente.
- Cobre: import/enrich, resolução por camadas, coleção+membros, `internal_id`,
  external_ids, criar item, ciclo "setar id → enrich".
- NÃO cobre: renderização/roteamento reais da UI (é o papel da Camada 2).

### Camada 2 — E2E de NAVEGADOR (Playwright) contra o binário (FEITA)
Build do web → binário com o fake provider → sobe → Playwright dirige um Chromium real.
É o que garante que front e back concordam de verdade (DOM, roteamento TanStack, fetch
reais). Roda sob demanda (`make e2e`), não a cada commit.
- **Projeto ISOLADO em `e2e/`** (não sob `web/`): tem seu próprio `package.json` e usa
  `npm install` (nunca `npm ci`), para NÃO tocar o `web/package-lock.json` estrito (o
  Docker faz `npm ci` lá; a versão do npm local é < 11.16). O lockfile do `e2e/` é
  gitignored de propósito.
- Orquestração: `global-setup` builda o front + `go build -cover` o binário e o sobe com
  `MYMEDIADB_FAKE_PROVIDER=1` + DB/assets temporários + `GOCOVERDIR`, com health check;
  `global-teardown` manda SIGTERM (o binário grava os contadores ao sair) e extrai a
  cobertura. `E2E_BASE_URL` reusa um servidor externo; `E2E_SKIP_BUILD=1` pula os builds.
- Jornadas atuais: **15 testes em 7 specs** cobrindo o inventário completo de fluxos do
  usuário (ver 7.1). web-first assertions, seletores por papel/aria/texto, chrome sempre
  em pt-BR. Isolamento por `resetBiblioteca` no `beforeEach` (zera entities via API);
  workers=1, um banco compartilhado.
- **Cobertura do binário** (backend, via `GOCOVERDIR` + `go tool covdata`): **52,4%** do
  binário exercido pelas jornadas (por-pacote + total). **Cobertura do front** (via
  `monocart-coverage-reports` lendo a cobertura V8 do Chromium, mapeada ao TSX por
  sourcemap INLINE que o vite emite só quando `E2E_COVERAGE=1`): **78,2%** das linhas.
  Rode ambas com `make e2e-cov`. São **termômetro** (reportadas no console, observadas ao
  crescer a suíte), NÃO ratcheadas na catraca — o contrato de completude é o checklist 7.1.

### Camada 3 — integração jsdom + MSW (opcional; UI rápida por commit)
Trocar os stubs de fetch manuais (hoje `stub-fetch.ts`) por **MSW** e montar árvores de
rota inteiras no Vitest/jsdom. Rápido (ms), sem browser, pega regressão de UI cedo e
reduz a deriva de contrato dos mocks à mão. Roda junto do `npm test` atual.

## 6. Determinismo e pegadinhas (específicas deste repo)

- **Relógio**: testes ancoram no relógio REAL, nunca em data fixa (o `strftime('now')` do
  banco anda; ver o débito histórico de TTL). Fake determinístico + `-count=1`.
- **Cache do `go test`**: a Camada 1 deve rodar com `-count=1` na catraca (o cache já
  mascarou falha antes).
- **Imagens/proxy**: fake serve imagem fixa/local; nada de CDN em E2E.
- **Isolamento de dados**: DB e assetsDir temporários por execução (nasce limpo, migra do
  zero — o schema é versionado por goose).
- **Portas/servidor**: subir em porta efêmera; parar por porta (`lsof -ti:PORT | kill`),
  nunca por `pkill -f <padrao>` (mata o próprio shell — pegadinha conhecida).
- **Flakiness do browser**: manter a Camada 2 enxuta; assertions que esperam estado, não
  tempo.

## 7. Jornadas iniciais a codificar (o que já valido à mão)

Camada 1 (API) e as principais também na Camada 2 (UI):
1. Importar filme (tmdb) → entity resolvida com título/ano/pôster.
2. Importar coleção → entity `collection` + membros ordenados por ano.
3. Criar coleção manual → adicionar/remover membro (picker) → excluir coleção (membros
   sobrevivem).
4. Criar item manual (sem fonte) → editar `external_ids` (setar tmdb) → **"Salvar e
   re-enriquecer"** → dados vêm da fonte pelo id fixado.
5. Interno-first: item já importado aparece como "Na biblioteca" (busca, similares,
   partes de coleção) e leva ao INTERNO, sem "Importar".
6. **Não-destrutividade do enrich** (crítico p/ ADR-0012): editar título (override) e
   uma tag (delta add/remove) → re-enriquecer → título editado PERMANECE, tag removida
   não volta, tag nova da fonte aparece. (Codifica a garantia do resolver de 3 camadas.)
7. Erros-chave: enrich sem id da fonte → 422 inline; fonte indisponível → 502/503.

## 7.1. Inventário de fluxos do usuário (checklist — o termômetro)

A intenção do dono: **garantir que todo fluxo que o usuário já vê/usa esteja preso por um
teste de ponta a ponta**, para que uma implementação nova não quebre em silêncio o que
funcionava. A cobertura E2E (52,4% binário, 78,2% front) é o **termômetro** — sobe quando
a suíte cresce, sinaliza regressão quando cai — mas o **contrato de completude é este
checklist**: cada linha é uma tela/ação que o usuário exercita, mapeada ao spec que a
prende. Os números de cobertura NÃO são ratcheados (jornadas de navegador são mais
lentas/variáveis); o checklist é revisado a cada feature nova.

**Regra de governança (a garantia que o dono pediu):** toda feature nova voltada ao
usuário precisa **adicionar ou estender uma jornada** neste inventário antes de fechar.
Se um fluxo passa a existir sem linha aqui, ele está desprotegido — a revisão de PR deve
cobrar a jornada. É isto que impede "novas implementações quebrarem fluxos de outras
partes" sem ninguém perceber.

| # | Fluxo do usuário | Tela/ação | Spec (`e2e/tests/`) |
|---|---|---|---|
| 1 | Buscar e importar filme | `/search` → "Importar" → detalhe interno resolvido (pt-BR) | `journeys.spec.ts` |
| 2 | Interno-first na busca | importado = "Na biblioteca" + link interno; não-importado = "Importar" + link externo | `journeys.spec.ts` |
| 3 | Biblioteca lista o importado | `/` grid com card linkando ao interno | `journeys.spec.ts` |
| 4 | Detalhe externo → importar | `/external/tmdb/movie/604` → "Importar" leva ao interno | `external.spec.ts` |
| 5 | Coleção externa → importar coleção | `/collection/tmdb/2344` → entity `collection` + membros | `external.spec.ts` |
| 6 | Enrich não-destrutivo (ADR-0012) | editar título (override) + tag (delta) → re-enriquecer → edições sobrevivem | `editing.spec.ts` |
| 7 | Item manual → fixar external-ids → re-enriquecer | dados vêm da fonte pelo id fixado | `editing.spec.ts` |
| 8 | Criar item manual | "Novo item" → "Título do item" → "Criar item" → aparece na biblioteca | `manual.spec.ts` |
| 9 | Coleção manual (ciclo) | criar → adicionar/remover membro (picker) → excluir (membros sobrevivem) | `manual.spec.ts` |
| 10 | Galeria de arte (ciclo) | enviar pôster → preferir → remover, na seção "Arte" da entity | `art.spec.ts` |
| 11 | Trocar idioma de leitura | combobox re-resolve o título exibido (en ↔ pt-BR) | `settings.spec.ts` |
| 12 | Configurações de idioma | `/settings`: principal/fallback, permitidos, salvar | `settings.spec.ts` |
| 13 | Datasource (CRUD) | `/library`: adicionar path → aparece; remover → some | `library.spec.ts` |
| 14 | Revisão de draft (curadoria) | scan de nome não-identificado → `/review` → "Corrigir" (busca) → sai da fila | `library.spec.ts` |
| 15 | Órfã (curadoria) | arquivo catalogado some → rescan → `/orphans` → "Purgar" (confirmação 2 passos) | `library.spec.ts` |

**Snapshot da cobertura no fechamento do inventário** (referência, não trava): binário
52,4% (por `go tool covdata`), front 78,2% linhas / 55,1% branches / 71,8% funções (por
monocart V8). Pacotes de menor cobertura de navegador — candidatos naturais a próximas
jornadas: `mediainfo` (21%), `richfields` (46%), `enrich` (50%), `server` (55%). O
provider `tmdb` fica em 0% de propósito (o fake o substitui nas jornadas).

## 8. Integração com CI / catraca / lefthook

- Camada 1 roda no `go test ./... -count=1` que a catraca já executa (sem infra nova).
- Camada 2 (Playwright) vira um job separado (mais lento) — pre-push e/ou CI noturno; não
  bloqueia cada commit. Um alvo `npm run e2e` que builda+sobe+roda.
- Camada 3 roda dentro do `npm test` atual.
- Métricas: o ratchet unitário (`go_coverage_lines`/`web_coverage_lines`) fica como está —
  o E2E NÃO o substitui. Além dele, a catraca ganha `e2e_coverage_lines`: `go test
  ./internal/e2e/... -coverpkg=./internal/... -coverprofile` → total % (quanto do app as
  JORNADAS tocam), ratchet próprio (só sobe), blocking. Semânticas distintas de propósito
  (jornada × ramo). A cobertura do binário (Camada 2, via GOCOVERDIR) e a do navegador
  (monocart) entram como métricas próprias quando a Camada 2 existir.

## 9. Plano faseado

- **Fase 1** (EM ENTREGA) — fake provider de fixtures (`internal/provider/fake`, ligado
  por `MYMEDIADB_FAKE_PROVIDER=1` no `main`) + Camada 1 (harness `internal/e2e` de
  jornadas de API em Go sobre `httptest.NewServer`) migrando os smoke tests manuais +
  métrica `e2e_coverage_lines` na catraca (`-coverpkg`). Valor imediato, baixo risco.
- **Fase 2** (FEITA) — projeto `e2e/` com Playwright: global-setup builda o front + o
  binário `-cover` e o sobe com o fake; **inventário completo de jornadas** (15 testes /
  7 specs, ver 7.1); cobertura do binário via `GOCOVERDIR` + `go tool covdata` (52,4%) e
  do front via `monocart-coverage-reports` lendo a cobertura V8 do Chromium com sourcemap
  inline (78,2% linhas). `make e2e` / `make e2e-cov`. Imagens: `MYMEDIADB_FAKE_IMAGE_BASE` (hoje sem pôster nas jornadas de
  navegador — placeholders; ligar quando uma jornada precisar de imagem determinística).
- **Fase 3** (opcional) — MSW + montagem de rotas no Vitest para UI rápida por commit.

## 10. Decisões em aberto / follow-ups

- Fake provider por fixtures (1) x mock TMDB HTTP (2): começar por (1); avaliar (2) como
  camada de contrato do provider TMDB.
- ~~Runner do Playwright no ambiente/CI (browser disponível?)~~ RESOLVIDO: Chromium
  disponível; `@playwright/test` + `monocart` num projeto `e2e/` isolado (`npm install`,
  sem tocar o lockfile do web). Falta rodar no CI (job separado, não-bloqueante).
- Fixtures ficam em código (`internal/provider/fake/catalogo.go`), não em `testdata/` —
  simples e tipado; migrar para arquivos se crescerem muito.
- Cobertura do binário/front (Camada 2): hoje SÓ reportadas (console), não ratcheadas na
  catraca (jornadas de navegador são mais lentas/variáveis). Avaliar travar como métricas
  próprias quando a suíte crescer e rodar no CI.
- Estratégia de dados para jornadas que dependem de estado prévio (seed via API no início
  de cada jornada, mantendo isolamento).
- `e2e_coverage_lines` blocking já na Fase 1 x começar reportando (não-blocking) e travar
  depois: escolhido BLOCKING com baseline capturado no 1º verde (ratchet só sobe), igual
  ao unit — cresce a cada jornada nova.
- Cobertura do binário (GOCOVERDIR) exige o binário `-cover`; decidir na Fase 2 se o alvo
  `npm run e2e` já emite o perfil e se ele vira métrica na catraca ou só relatório.
