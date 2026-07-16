# 0061 — Catraca sensível ao diff + fast-mode + log completo sob demanda

> A catraca passa a rodar só os coletores do lado (Go/Web) realmente tocado pelo
> diff, ganha um modo `--fast` mais barato pra iteração, e passa a gravar a
> saída completa de qualquer coletor que falhar em `reports/`, sem depender de
> nenhum agente "decidir" o que rodar.

| | |
|---|---|
| **ID** | 0061 |
| **Tipo** | chore |
| **Fase (roadmap)** | Infraestrutura interna (fora das fases de produto do roadmap) |
| **Status** | done |
| **Docs relacionadas** | — |
| **Depende de** | — |

---

## 1. Objetivo

O gate de qualidade (`scripts/quality-gate.mjs`, chamado por `make gate-local`
no `pre-push` e por `node scripts/quality-gate.mjs` no CI) roda **todos** os
coletores — Go e Web — em **toda** invocação, independente do que mudou no
diff. Editar um único `.md` em `briefings/` dispara: `go test ./...` completo,
`go test ./internal/e2e/...` sem cache, golangci-lint, govulncheck (rede),
gocyclo, `vitest` completo do `web/`, `tsc -b --force` (rebuild TS do zero,
sempre), `jscpd` no `web/src` inteiro, `eslint` completo e `npm audit` (rede).

Isso encarece cada iteração de implementação: agentes de validação chegam a
rodar o gate completo 2-4 vezes por briefing (uma vez, corrige regressão,
roda de novo pra confirmar), pagando o custo do monorepo inteiro a cada
rodada mesmo quando só um punhado de arquivos de um lado mudou.

Este briefing resolve isso com regras **mecânicas** (baseadas em `git diff`),
não com instrução para o agente "usar juízo" sobre o que rodar — o objetivo
explícito é fechar o máximo de decisão possível em código determinístico, e
só deixar em aberto (via instrução em `AGENTS.md`) o que não dá pra resolver
em código sem custo desproporcional.

Um efeito colateral encontrado durante a análise (não é o foco original, mas
é do mesmo arquivo e da mesma motivação — "log enxuto no sucesso, detalhado
na falha"): quando `go test`/`vitest` **falham de verdade** (não é erro de
setup, é teste quebrando), o gate hoje só imprime `stderr`, que pros
executores de teste do Go/vitest costuma vir **vazio ou inútil** — o
`--- FAIL:` com a asserção que quebrou vai pro `stdout`, que é descartado sem
nunca ser mostrado. Isso está corrigido neste briefing também (seção 4.4).

## 2. Fluxo (dev/agente) — antes × depois

**Antes:**
1. Agente muda 3 arquivos em `internal/server/` e 3 em `web/src/`.
2. Roda `make gate-local` pra checar → paga Go completo + Web completo.
3. Acha uma regressão de duplicação no Web, corrige.
4. Roda `make gate-local` de novo "pra confirmar" → paga Go completo (que nem
   mudou desde a última rodada) + Web completo outra vez.
5. Se um teste quebra no meio disso, o warning mostra `stderr` (quase sempre
   vazio) e não o `--- FAIL:` real — o agente tem que re-rodar `go test -v`
   por fora pra descobrir o que quebrou, gastando mais uma rodada verbosa.

**Depois:**
1. Mesmo diff (3 arquivos Go + 3 Web).
2. Durante a correção, roda `make gate-fast` (mesmo filtro por diff, só os
   coletores baratos) — mais rápido, sem golangci-lint/govulncheck/gocyclo/
   jscpd/npm audit.
3. Corrige a regressão apontada.
4. Roda `make gate-local` **uma vez**, completo, antes do commit — continua
   pagando Go + Web (o diff tocou os dois lados, isso é esperado e correto).
5. Se um push tocar só `briefings/0062-outro.md`, o gate nem chama os
   coletores — imprime a razão do skip e passa trivialmente.
6. Se um teste quebrar, o terminal mostra as últimas ~40 linhas do
   stdout+stderr real do `go test`/`vitest`, e o arquivo completo fica em
   `reports/<metrica>-failure.log` pra quem precisar do resto.

## 3. Escopo

**Dentro:**
- Filtro por diff (lado Go × lado Web × "nem um nem outro") em
  `scripts/quality-gate.mjs`, usado tanto por `--local` quanto pelo modo CI.
- Precedência determinística de "arquivo de infra do próprio gate mudou ⇒
  força rodada completa dos dois lados", pra nunca criar um ponto cego onde
  uma mudança no próprio gate escapa de validação.
- Novo modo `--fast` (coletores baratos, mesmo filtro por diff).
- Novo modo `--full` (ignora o filtro, roda tudo — escape hatch manual).
- Wiring no `Makefile` (`gate-fast`, `gate-local-full`) e no
  `.github/workflows/quality-gate.yml` (passar a base do diff certa pro
  `push` e pro `pull_request`).
- Gravação da saída completa de qualquer coletor que **falhar em rodar**
  (não "achou issues" — "não conseguiu produzir o resultado") em
  `reports/<chave>-failure.log`, com stdout **e** stderr, e exibição das
  últimas ~40 linhas no terminal/Markdown em vez de só as primeiras linhas
  do stderr.
- Atualização do `AGENTS.md` (texto exato na seção 4 deste briefing).

**Fora (explicitamente):**
- Paralelizar coletores Go/Web entre si (ganho de wall-clock, ortogonal).
- Cache de resultados caros (govulncheck/npm audit) por hash de
  lockfile — desnecessário, o filtro por lado já cobre a maior parte do
  ganho com uma regra só.
- Granularidade abaixo de "lado" (por pacote Go, por diretório do `web/`,
  etc.) — quebraria a comparabilidade da cobertura com `baseline.json`, que
  é sempre um percentual sobre o repositório inteiro daquele lado.
- Mexer no workflow `e2e.yml` (Playwright) — ferramenta e execução
  diferentes, não analisado aqui.
- Mudar `npx tsc -b --force` — fica como está (ver §6).
- Reduzir a verbosidade de comandos que os **subagentes rodam por fora do
  gate** (ex.: `go test -v` solto durante debug) — isso é comportamento de
  prompt/instrução, não deste script; parcialmente endereçado pela
  atualização do `AGENTS.md` (§4.5), mas sem mudança de código associada.

## 4. Contrato técnico

### 4.1 Classificação de arquivos (regra fixa, sem exceção por julgamento)

Três buckets, checados nesta ordem de precedência sobre a lista de arquivos
do diff:

1. **Infra do gate** (regex fixo — qualquer arquivo desta lista força
   `touchesGo = touchesWeb = true`, independente do resto do diff):
   `scripts/quality-gate.mjs`, `lefthook.yml`, `.golangci.yml`,
   `baseline.json`, `Makefile`, `.github/workflows/*.yml` (qualquer arquivo
   dentro dessa pasta).
2. **Lado Go**: arquivo casa com `^(cmd/|internal/|go\.mod$|go\.sum$)`.
   → `touchesGo = true`.
3. **Lado Web**: arquivo casa com `^web/`. → `touchesWeb = true`.

Qualquer arquivo que não caia em nenhum bucket (ex.: `briefings/*.md`,
`docs/*.md`, `README.md`, `AGENTS.md`, imagens soltas na raiz,
`compose*.yaml`, `Dockerfile`) **não seta nenhuma flag**. Se, depois de
percorrer todo o diff, nem `touchesGo` nem `touchesWeb` ficaram `true`, **nenhum
coletor roda** — não é um caso especial, é a consequência natural das duas
flags falsas (`collectMetrics` já retorna tudo `null` pros dois lados, e o
comparador/impressor já trata `null` como "pulada" sem regressão — reaproveitar
esse caminho existente, não criar um `if (nadaTocado) return early` paralelo).

Não crie um sub-filtro por métrica individual dentro de um lado (ex.: "só
rodar `npm audit` se o lockfile mudou"). Granularidade é por lado, ponto —
reaproveite o campo `side: 'go' | 'web'` que já existe em `METRIC_DEFS` pra
decidir quais chaves pular; não crie uma lista paralela de "quais métricas
são do lado X".

### 4.2 Base do diff (de onde vem a lista de arquivos mudados)

Uma função `getChangedFiles()` que devolve `string[]` ou `null`
(`null` = "não foi possível determinar a base com segurança" → tratar como
se **ambos** os lados tivessem sido tocados, ou seja, rodar tudo. Esse é o
único fallback: na dúvida, roda tudo, nunca pula tudo por engano).

Ordem de resolução da base:

1. **CI**: se a env var `GATE_DIFF_BASE` estiver setada e for um SHA válido
   (`git cat-file -e <sha>` sai 0), usa `git diff --name-only <base>...HEAD`.
   O workflow (`quality-gate.yml`) passa essa env var assim:
   - em `pull_request`: `${{ github.event.pull_request.base.sha }}`
   - em `push`: `${{ github.event.before }}` — que vem zerado
     (`0000000000000000000000000000000000000000`) no primeiro push de uma
     branch nova; nesse caso o SHA falha o `cat-file -e` e cai no fallback
     (rodar tudo), que é o comportamento correto (não dá pra saber o que
     mudou sem uma base real).
2. **Local (`--local`/`pre-push`)**: sem env var. Tenta, nesta ordem, e usa a
   primeira que resolver:
   a. `git merge-base HEAD @{upstream}` (branch atual tem upstream
      configurado);
   b. `git merge-base HEAD origin/master` (se existir);
   c. `git merge-base HEAD origin/main` (se existir);
   d. nenhuma resolveu → `null` (roda tudo).
3. **`--generate-baseline`**: nunca chama `getChangedFiles()` — sempre roda
   os dois lados completos, sem exceção. O baseline nunca pode ser gerado
   parcial.

### 4.3 Novos modos de CLI

- `--fast`: aplica o mesmo filtro por lado de `--local`, mas dentro de cada
  lado tocado só roda os coletores baratos: `go_coverage_lines` (o `go test`
  normal, não o de E2E) e `go_gofmt_unformatted` do lado Go;
  `web_coverage_lines` e `web_typecheck_errors` do lado Web. Pula
  `e2e_coverage_lines`, `go_lint_issues`, `go_vuln_high`,
  `go_cyclo_violations`, `web_duplicate_percent`, `web_lint_issues`,
  `web_audit_critical` mesmo que o lado correspondente tenha sido tocado.
  Saída no formato humano (igual `--local`), mas com um aviso de que é modo
  rápido — não substitui a rodada completa antes do push.
- `--full`: ignora o resultado de `getChangedFiles()` como se sempre fosse
  `null` — roda os dois lados completos. Serve de escape hatch manual; não é
  pra uso automático em hook nem obrigatório pra agentes.

### 4.4 Log completo sob demanda nos coletores que falham ao rodar

Vale para todo coletor que hoje só imprime `stderr` truncado quando não
consegue produzir seu resultado (procure por todos os `warn(...); if
(...stderr...) warn(...slice(0, N)...)` no arquivo — são ~9 ocorrências:
`collectGoCoverage`, `collectE2ECoverage`, `collectGolangciLint`,
`collectGovulncheck`, `collectWebDuplication`, `collectWebAuditCritical`,
`collectWebTypecheck`, `collectWebLint`, `collectWebCoverage`). Trocar o
padrão em todos, uniformemente, por:

1. Concatenar `stdout + '\n' + stderr` do comando que falhou.
2. Gravar esse conteúdo completo em `reports/<chave-da-metrica>-failure.log`
   (ex.: `reports/go_coverage_lines-failure.log`), sobrescrevendo a rodada
   anterior.
3. Imprimir no `warn()` só as **últimas** ~40 linhas desse conteúdo (não as
   primeiras — o resumo/falha final costuma estar no fim, não no início) mais
   uma linha apontando o caminho do arquivo completo.

Isso é o mesmo tratamento pra todos os 9 pontos — não decidir caso a caso se
"esse aqui merece o arquivo e esse outro não". O caminho de sucesso (coletor
rodou e produziu resultado, com ou sem issues encontradas) **não muda** — já
está enxuto hoje (só a tabela final), não mexer.

### 4.5 `Makefile`

Novos alvos, ao lado dos existentes:
```
gate-fast:       ## Catraca em modo rápido (filtro por diff, sem coletores caros)
	node scripts/quality-gate.mjs --local --fast

gate-local-full: ## Catraca completa, ignorando o filtro por diff (uso manual)
	node scripts/quality-gate.mjs --local --full
```
`gate` (modo CI) e `gate-local` (modo humano, usado no `pre-push`) continuam
existindo com os mesmos comandos — o filtro por diff passa a ser
comportamento **padrão** deles, não um flag novo que alguém precisa lembrar
de passar.

### 4.6 `.github/workflows/quality-gate.yml`

Adicionar a env var `GATE_DIFF_BASE` no step que roda o gate (ou no `job`
inteiro), computada a partir do evento:
```yaml
env:
  GATE_DIFF_BASE: ${{ github.event_name == 'pull_request' && github.event.pull_request.base.sha || github.event.before }}
```
O `fetch-depth: 0` do checkout já presente continua necessário (é o que
permite o `git diff`/`git merge-base` enxergar o histórico completo).

### 4.7 `AGENTS.md` — texto a atualizar

Duas mudanças no arquivo, seção "Práticas operacionais":

1. No bullet atual sobre a catraca (por volta da linha 84-86), acrescentar
   que ela é sensível a diff e que "pulada" no lado não tocado é esperado, e
   adicionar um bullet novo logo abaixo sobre `make gate-fast` vs
   `make gate-local`:

   > - Catraca: `make gate-local`. Ela só deixa as métricas **melhorarem**
   >   (ratchet) e agora é **sensível ao diff** — só roda os coletores do
   >   lado (Go/Web) que seu diff realmente tocou; "pulada" nas métricas do
   >   outro lado é esperado, não é bug. **NUNCA rode `make baseline`** — só
   >   o dono avança o baseline. gocyclo é aviso não-bloqueante (mantenha
   >   funções novas ≤10 mesmo assim).
   > - Durante a implementação (iterando numa correção), use
   >   `make gate-fast` — mesmo filtro por diff, sem golangci-lint/
   >   govulncheck/gocyclo/jscpd/npm audit. Reserve o `make gate-local`
   >   completo para **uma** rodada final, logo antes do commit/push — não
   >   repita o gate completo "pra confirmar".
   > - Se um coletor da catraca falhar (não "achou issues" — falhou de
   >   rodar), o terminal mostra só as últimas linhas; a saída completa fica
   >   em `reports/<metrica>-failure.log` — leia esse arquivo antes de
   >   tentar adivinhar a causa pela saída truncada.

2. No bullet de "Escala e contexto" (por volta da linha 110-111), que hoje
   diz "a saída verbosa da catraca é o maior consumidor de contexto do pai",
   trocar por algo que reflita a causa real depois desta mudança:

   > - Delegue pesquisa/implementação/validação a **subagentes** e receba
   >   resumos — rodar a catraca **completa** repetidas vezes dentro de uma
   >   única implementação é o maior desperdício de tempo/contexto; prefira
   >   `make gate-fast` ao iterar e reserve `make gate-local` completo para
   >   o final (ver acima).

### Arquivos afetados
- `scripts/quality-gate.mjs` (filtro por diff, `--fast`, `--full`, log de
  falha em arquivo).
- `Makefile` (`gate-fast`, `gate-local-full`).
- `.github/workflows/quality-gate.yml` (`GATE_DIFF_BASE`).
- `AGENTS.md` (texto da seção 4.7 acima).
- `.gitignore`: confirmar que `reports/` já está ignorado (se não estiver,
  adicionar — os `*-failure.log` não podem ir pro commit).

## 5. Critérios de aceite

- [ ] Diff tocando só `briefings/*.md` (ou outro arquivo fora dos buckets Go/
      Web/infra) não roda nenhum coletor; gate passa e imprime a razão do
      skip.
- [ ] Diff tocando só `web/**` pula os 6 coletores do lado Go
      (`go_coverage_lines`, `e2e_coverage_lines`, `go_gofmt_unformatted`,
      `go_lint_issues`, `go_vuln_high`, `go_cyclo_violations`) e roda os 5 do
      lado Web normalmente.
- [ ] Diff tocando só `internal/**`/`cmd/**` pula os 5 coletores do lado Web
      e roda os 6 do lado Go normalmente.
- [ ] Diff tocando `scripts/quality-gate.mjs`, `lefthook.yml`,
      `.golangci.yml`, `baseline.json`, `Makefile` ou qualquer arquivo em
      `.github/workflows/` força rodada completa dos dois lados, mesmo sem
      nenhum outro arquivo de código no diff.
- [ ] `make gate-fast` existe, aplica o mesmo filtro por diff, e só roda
      `go_coverage_lines`/`go_gofmt_unformatted`/`web_coverage_lines`/
      `web_typecheck_errors` (pulando os outros 7, mesmo com o lado tocado).
- [ ] `make gate-local-full` força rodada completa dos dois lados, ignorando
      o filtro por diff.
- [ ] Em CI, `push` usa `github.event.before` como base e `pull_request` usa
      `github.event.pull_request.base.sha`; primeiro push de uma branch nova
      (`before` zerado) cai no fallback de "rodar tudo" sem quebrar o
      workflow.
- [ ] Local, sem upstream configurado e sem `origin/master`/`origin/main`
      resolvível, cai no fallback de "rodar tudo" sem crashar.
- [ ] Um `go test` que falha de verdade (teste quebrado, não erro de setup)
      grava stdout+stderr completos em
      `reports/go_coverage_lines-failure.log` e mostra as últimas ~40 linhas
      no terminal — incluindo o bloco `--- FAIL:` da asserção, não só um
      `stderr` vazio.
- [ ] O mesmo vale pros outros 8 pontos listados em 4.4 (mesmo tratamento
      uniforme).
- [ ] `make baseline` (`--generate-baseline`) continua sempre rodando os
      dois lados completos, ignorando qualquer filtro.
- [ ] `AGENTS.md` atualizado com o texto exato de 4.7 (ou equivalente
      revisado, mantendo o conteúdo semântico).
- [ ] Nenhuma métrica nova entra em `METRIC_DEFS`; `baseline.json` não muda
      de schema; o ratchet continua funcionando como antes para quem roda
      `--full`/toca os dois lados.
- [ ] `reports/` confirmado no `.gitignore`.

## 6. Notas de implementação / decisões

- **Granularidade é por lado (Go/Web), nunca por pacote/arquivo individual.**
  A cobertura (`go_coverage_lines`, `web_coverage_lines`) é sempre um
  percentual sobre o repositório inteiro daquele lado — rodar só o pacote
  tocado quebraria a comparabilidade com `baseline.json`. Reaproveite o
  campo `side` já existente em `METRIC_DEFS`.
- **`npx tsc -b --force` fica como está — não mexer.** O comentário já
  existente no arquivo (`collectWebTypecheck`) explica que o `--force` é
  proteção deliberada contra o `.tsbuildinfo` mascarar uma fixture
  desatualizada (já mordeu o projeto antes, ver `catraca-cobre-typecheck-web`
  na memória do projeto). O custo (rebuild TS completo sempre que o lado Web
  roda) é aceito como o preço dessa proteção; ninguém precisa "decidir" isso
  de novo durante a implementação.
- **Fallback universal é sempre "rodar tudo".** Qualquer incerteza sobre a
  base do diff (sem upstream, sem origin/master, SHA inválido, primeiro push
  de uma branch nova, erro ao rodar `git diff`) resolve para `null` →
  `touchesGo = touchesWeb = true`. Nunca o contrário — um bug aqui que
  resolvesse pra "pular tudo" seria silencioso e perigoso.
- **`--generate-baseline` está fora do filtro por definição** — baseline
  parcial corromperia o ratchet pros dois lados.
- Este briefing não introduz nenhuma métrica nova nem muda o formato de
  `baseline.json` — é só sobre *quando* cada coletor existente roda e *o
  que* é logado quando um deles falha ao rodar.

## 7. Fora deste briefing / follow-ups

- Paralelizar a execução dos coletores Go e Web (hoje sequenciais) — ganho
  de wall-clock ortogonal a este briefing, pode virar um briefing separado
  se a rodada completa continuar lenta mesmo com o filtro por diff.
- Cache de coletores caros (`govulncheck`, `npm audit`) por hash de
  `go.sum`/`package-lock.json` entre rodadas consecutivas — não vale a
  complexidade dado que o filtro por lado já remove a maior parte do custo
  desnecessário.
- Filtro/otimização do workflow `e2e.yml` (Playwright) — ferramenta e
  execução diferentes da catraca, precisa de análise própria.
- Reduzir a verbosidade de comandos rodados pelos subagentes **fora** do
  gate (ex.: `go test -v` solto durante debug manual) — é instrução de
  prompt, endereçado parcialmente pelo `AGENTS.md` atualizado (§4.7), sem
  mudança de código correspondente.
