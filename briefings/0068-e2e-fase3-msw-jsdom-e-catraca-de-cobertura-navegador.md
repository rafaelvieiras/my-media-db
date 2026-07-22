# 0068 — E2E Fase 3 (MSW + jsdom) e travar a cobertura de navegador (Camada 2) na catraca

> O briefing 0028 fechou as Fases 1 (Go/API) e 2 (Playwright) com um checklist de 21
> jornadas e duas coberturas de navegador (binário 52,8%, front 78,9%) que hoje são só
> TERMÔMETRO — reportadas no console/artefato, não travadas. Este briefing resolve os
> dois follow-ups que ficaram em aberto no §10 do 0028: (a) a Fase 3 opcional (MSW +
> jsdom, integração rápida por commit) e (b) travar as duas métricas de cobertura de
> navegador como ratchet blocking na catraca, igual ao unitário.

| | |
|---|---|
| **ID** | 0068 |
| **Tipo** | infra/qualidade |
| **Fase (roadmap)** | Transversal — infraestrutura operacional |
| **Docs relacionadas** | [0028](0028-testes-e2e.md), [0061](0061-catraca-sensivel-ao-diff-e-log-sob-demanda.md), [ADR-0016](../docs/adr) |
| **Status** | done |
| **Depende de** | 0028, 0061 |

---

## 1. Objetivo

Duas entregas independentes (arquivos disjuntos — dá para implementar em paralelo):

- **A. Fase 3 (MSW + jsdom)**: uma camada NOVA de teste — árvore de rota real (TanStack
  Router) montada em jsdom, rede mockada por MSW (contrato real do `openapi.yaml` via
  `api-types.ts`), sem browser. Cobre jornadas MULTI-TELA (navegação entre rotas) que os
  testes de componente isolado (Vitest + `stub-fetch.ts`) não alcançam, e roda a cada
  `npm test` (rápida, ms), pegando regressão de UI antes do Playwright.
- **B. Travar a cobertura de navegador (Camada 2) na catraca**: as duas métricas
  reportadas hoje só no console do `make e2e-cov` (binário via `GOCOVERDIR`+
  `go tool covdata`; front via `monocart-coverage-reports`) viram métricas `blocking`
  no `baseline.json`, comparadas num passo novo do job `.github/workflows/e2e.yml`
  (job separado da catraca unitária, continua não bloqueando por mudança de código
  que não seja de UI — só falha se as JORNADAS regredirem em cobertura).

Não-objetivo: NÃO migrar os testes unitários existentes que usam `stub-fetch.ts`/
`criarFetch` para MSW — os dois padrões coexistem (unitário = fetch stub simples;
integração de rota = MSW). Não expandir o checklist §7.1 do 0028 (fica como está).

## 2. Entrega A — MSW + jsdom (`web/`)

### 2.1. Dependência e estrutura

- Adicionar `msw` como devDependency em `web/package.json` (versão mais recente da
  major 2.x — API `setupServer`/`http`/`HttpResponse`).
- Novo diretório `web/src/test/msw/`:
  - `handlers.ts` — handlers MSW por endpoint, tipados contra `web/src/lib/api-types.ts`
    (nunca à mão solto — reusa os tipos gerados, para deriva de contrato quebrar o
    type-check, não silenciosamente). Cobrir POR ORA só os endpoints exercidos pelas
    3 jornadas do §2.3 abaixo (import, busca, override) — extensível por handler
    conforme entram novas jornadas de integração, mesma filosofia incremental do 0028.
  - `server.ts` — `setupServer(...handlers)`; exporta `server` para os testes poderem
    `server.use(...)` overrides pontuais (ex.: simular 502 numa jornada de erro).
- `web/src/test/setup.ts` (arquivo de setup global do Vitest já existente): adicionar
  `beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))`,
  `afterEach(() => server.resetHandlers())`, `afterAll(() => server.close())`. Rota não
  mockada estoura erro (mesma filosofia de "falha alto" do `stub-fetch.ts` atual, não
  fica silenciosa).

### 2.2. Harness de árvore de rota

- Novo `web/src/test/route-tree-harness.tsx`: monta o `router` REAL de
  `web/src/router.tsx` com um `memoryHistory` apontando para um path inicial (parâmetro),
  dentro do `QueryClientProvider` (reusa o padrão de `renderComEspioes` de
  `harness.tsx`, mas para a árvore inteira em vez de um componente isolado). Função
  `renderRota(pathInicial: string)` devolve `{ router, queryClient }` e usa
  `await router.load()` / `waitFor` para aguardar a navegação assentar antes das
  asserções (evita flakiness).

### 2.3. Jornadas de integração (Fase 3) — 3 para começar

Localização: `web/src/routes/__msw__/*.test.tsx` (pasta nova, ao lado de `routes/`,
sinalizando "teste de árvore", não de componente). Uma jornada por arquivo:

1. `busca-importar.test.tsx` — jornada #1/#2 do checklist 0028 §7.1 em miniatura:
   `/search` → mock de busca TMDB devolve 1 resultado não-importado → clique em
   "Importar" → navega para `/entities/{id}` → título resolvido aparece.
2. `enrich-nao-destrutivo.test.tsx` — jornada #6 (crítica, ADR-0012): entity já
   importada com override de título + tag deletada → editar → "Salvar e
   re-enriquecer" → mock de enrich devolve dado novo da fonte → título editado
   PERMANECE, tag removida não volta.
3. `erro-fonte-indisponivel.test.tsx` — jornada #17: mock do enrich devolve 502 →
   mensagem de erro inline aparece na tela (não crash, não navegação perdida).

Cada teste roda dentro do `npm test` normal (Vitest+jsdom), portanto conta para
`web_coverage_lines` (métrica já existente na catraca) — nenhuma métrica nova por
conta da Entrega A.

## 3. Entrega B — travar a cobertura de navegador na catraca

### 3.1. Novas métricas em `scripts/quality-gate.mjs`

Em `METRIC_DEFS`, dois registros novos com `side: 'e2e'` (novo valor de `side`, só
para agrupamento no relatório — NÃO entra no laço `['go','web']` de
`collectMetrics`, ver §3.3):

```js
e2e_binary_coverage_lines: { label: 'E2E: cobertura do binário (navegador)', unit: '%', side: 'e2e', lowerIsBetter: false, blocking: true },
e2e_front_coverage_lines:  { label: 'E2E: cobertura do front (navegador)',   unit: '%', side: 'e2e', lowerIsBetter: false, blocking: true },
```

### 3.2. Persistir os números (hoje só vão pro console)

- `e2e/global-teardown.ts`, função `coberturaBinario()`: já grava
  `e2e/.artifacts/e2e-binary-coverage.txt` (perfil textfmt) — o coletor (§3.4) roda
  `go tool cover -func` sobre ESSE arquivo e pega a linha `total:`, nenhuma mudança
  necessária aqui.
- `e2e/global-teardown.ts`, função `coberturaFront()`: HOJE só faz
  `console.log` do `resultado.summary.lines.pct`. Adicionar, logo após, persistir em
  `e2e/.artifacts/e2e-front-coverage.json` com o formato `{ "linesPct": <number> }`
  (usar `writeFileSync`/`JSON.stringify`). É o único arquivo novo que este teardown
  passa a escrever.

### 3.3. Coletores + modo `--e2e`

- CLI: novo flag `--e2e` (`const isE2E = process.argv.includes('--e2e')`), análogo a
  `--fast`/`--full`/`--generate-baseline` já existentes.
- `collectMetrics(touches, opts)` ganha um terceiro parâmetro implícito: quando
  `isE2E` é true, o laço roda **só** `for (const side of ['e2e'])` (não `['go','web']`)
  — ou seja, em modo `--e2e` os coletores Go/Web NÃO rodam (não recompila/testa o
  projeto inteiro, só lê os dois artefatos que o `make e2e-cov` já deixou prontos).
  Fora do modo `--e2e` (uso normal, `--local`/CI/`--fast`), o laço continua
  `['go','web']` como hoje e as chaves `e2e_*` simplesmente não entram em `metrics`
  (ficam `undefined` → `compareMetrics` já trata `undefined ?? null` como "N/A",
  mesmo caminho usado hoje para lado pulado por diff — nenhuma lógica nova ali).
- Dois coletores novos, registrados em `COLLECTORS`:
  - `collectE2EBinaryCoverage()`: lê `e2e/.artifacts/e2e-binary-coverage.txt`; se não
    existir, `logFailure(...)` + `return null` (mesmo padrão dos coletores atuais
    quando o artefato falta). Se existir, roda
    `go tool cover -func=<path>` (via `run(...)`, mesmo helper já usado no arquivo),
    pega a linha que começa com `total:` e extrai o percentual (`parseFloat`).
  - `collectE2EFrontCoverage()`: lê `e2e/.artifacts/e2e-front-coverage.json`
    (`{ linesPct }`); ausente/inválido → `null` (mesmo padrão).

### 3.4. `--generate-baseline` nunca mais sobrescreve o lado que não coletou

Hoje `generateBaseline()` escreve `baseline.json` do zero a cada chamada. Mudar para
**sempre mesclar por cima do `baseline.json` existente** (se houver): ler o arquivo
atual, `const anteriores = existing?.metrics ?? {}`, e gravar
`metrics: { ...anteriores, ...collectMetrics(...) }`. Isso é necessário porque
`--generate-baseline --e2e` (novo uso, §3.5) só recolhe o lado `e2e` — sem a mesclagem,
zeraria as chaves `go_*`/`web_*` do baseline vigente. Regra única e sempre ativa (não
um caso especial do modo `--e2e`) — mais simples e sem novo ramo de decisão.

### 3.5. Nome do relatório em modo `--e2e`

`printMarkdown` grava hoje sempre em `reports/quality-gate-report.md` (linha fixa). Em
modo `--e2e`, gravar em `reports/e2e-quality-gate-report.md` em vez disso (evita
confusão entre os dois relatórios quando ambos os jobs de CI publicam artefato) —
`const reportFile = isE2E ? 'e2e-quality-gate-report.md' : 'quality-gate-report.md'`.

### 3.6. Makefile

Novo alvo:

```makefile
baseline-e2e: ## Avanca a catraca de cobertura de navegador (Camada 2): rode 'make e2e-cov' antes
	node scripts/quality-gate.mjs --generate-baseline --e2e
```

### 3.7. CI — `.github/workflows/e2e.yml`

Depois do step "Run E2E (com cobertura)" (que já roda `make e2e-cov` e deixa os dois
artefatos prontos), dois steps novos:

```yaml
      - name: Travar cobertura de navegador na catraca
        id: e2e-gate
        run: node scripts/quality-gate.mjs --e2e
        continue-on-error: true

      - name: Comment e2e coverage table on PR
        if: github.event_name == 'pull_request'
        uses: marocchino/sticky-pull-request-comment@v3
        with:
          header: e2e-quality-gate
          recreate: true
          path: reports/e2e-quality-gate-report.md

      - name: Fail on blocking regression (cobertura de navegador)
        if: steps.e2e-gate.outcome == 'failure'
        run: |
          echo "Cobertura de navegador regrediu vs baseline. Veja reports/e2e-quality-gate-report.md."
          exit 1
```

Isso ainda vale a nota do cabeçalho do arquivo ("marque como não-obrigatório na
proteção de branch se não quiser bloquear") — a decisão de branch protection é do
dono, fora do escopo do código.

### 3.8. `baseline.json` — captura inicial

Depois que A e B estiverem implementados: rodar `make e2e-cov` uma vez localmente
(gera os dois artefatos novos) e então `make baseline-e2e` (grava as duas métricas
`e2e_*` pela primeira vez, mesclando por cima do `baseline.json` vigente). Isso é
passo de VALIDAÇÃO/fechamento, não de implementação — feito depois que os dois
agentes terminarem, antes do commit final.

## 4. Ordem de execução

Duas entregas em arquivos disjuntos — **implementar A e B em paralelo**:

- Agente 1 (web): Entrega A completa (§2) — `web/package.json`, `web/src/test/msw/`,
  `web/src/test/route-tree-harness.tsx`, `web/src/routes/__msw__/*.test.tsx`.
- Agente 2 (infra/catraca): Entrega B completa (§3, exceto §3.8) — `scripts/quality-gate.mjs`,
  `e2e/global-teardown.ts`, `Makefile`, `.github/workflows/e2e.yml`.

Depois de ambos prontos: rodar `npm test` (web), `make gate-local` (raiz), `make
e2e-cov` + `make baseline-e2e` (captura inicial §3.8), revisar `baseline.json`,
commitar e — só com confirmação explícita — dar push (dispara CI/CD + deploy).

## 5. Critérios de aceite

- `npm test` em `web/` inclui as 3 jornadas de integração novas, todas verdes,
  rodando junto do restante da suíte (sem browser).
- `node scripts/quality-gate.mjs --e2e` roda e IMPRIME as duas métricas `e2e_*`
  lendo os artefatos do `make e2e-cov` anterior, sem re-executar Playwright.
- `node scripts/quality-gate.mjs` (modo normal, sem `--e2e`) continua idêntico ao
  comportamento de hoje — as chaves `e2e_*` não aparecem nem afetam o resultado.
- `baseline.json` ganha `e2e_binary_coverage_lines`/`e2e_front_coverage_lines` sem
  perder nenhuma chave `go_*`/`web_*` existente.
- `.github/workflows/e2e.yml` falha (exit 1) se uma PR reduzir a cobertura de
  navegador do binário ou do front vs. o baseline travado.
