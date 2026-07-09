# 0005 — Quality gate: catraca unificada (ratchet) Go + TS

> Uma catraca de qualidade única para o monorepo: mede Go e TS, grava um baseline,
> roda no `pre-push` local e no PR, e **bloqueia regressão**.

| | |
|---|---|
| **ID** | 0005 |
| **Tipo** | chore |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único (infra transversal) |
| **Status** | done |
| **Docs relacionadas** | [ADR-0016](../docs/adr/0016-quality-gate-catraca-unificada.md), [ADR-0014](../docs/adr/0014-stack-de-implementacao.md), [CONTRIBUTING](../CONTRIBUTING.md) |
| **Depende de** | [0001](0001-backend-esqueleto-servidor.md), [0002](0002-frontend-scaffold-integracao-api.md) (para haver código a medir) |

---

## 1. Objetivo

Instalar o **quality gate** do projeto **antes** de a implementação real começar, para
que toda entrega dali em diante seja validada em qualidade de código e cobertura. O
mecanismo é a **catraca (ratchet)** do [boatman](https://github.com/rafaelvieiras/boatman):
grava as métricas atuais num `baseline.json` e, a cada push/PR, recompara — **piorou,
bloqueia; melhorou, avança o baseline**. Nunca se regride em silêncio. A diferença aqui
é que o gate é **unificado e poliglota**: mede o backend **Go** e o frontend **TS** num
único baseline ([ADR-0016](../docs/adr/0016-quality-gate-catraca-unificada.md)).

## 2. Jornada do usuário

A "jornada" é do **dev/mantenedor**:

1. Termina uma mudança e roda `git push`. O **lefthook** dispara o `pre-push`.
2. O gate roda local (`--local`, saída humana no terminal): lint, cobertura e
   duplicação dos **dois** lados; mostra melhorias/regressões vs. o baseline.
3. Se algo bloqueante regrediu (ex.: cobertura caiu), o push é **abortado** — feedback
   antes de abrir o PR.
4. Ao abrir o PR, o **workflow de CI** repete o gate, **trava o merge** em regressão e
   posta um **comentário** com a tabela de métricas (atual × baseline × delta).
5. Quando o dev genuinamente melhorou as métricas, roda `quality:baseline`, commita o
   novo `baseline.json` e a catraca avança.

## 3. Escopo

**Dentro:**
- Rodar **`boatman init`** apontando para o frontend (`/web`) — gera a metade JS:
  `quality-gate.mjs`, scripts npm (`quality:gate`, `quality:baseline`, …), baseline.
- **Estender o `quality-gate.mjs` gerado** para coletar métricas **Go** no mesmo
  `baseline.json` (ver Contrato).
- **Coletores Go**: golangci-lint, `go test -coverprofile`, govulncheck, gofmt-check,
  gocyclo (complexidade, não-bloqueante).
- **lefthook** para os hooks locais: `pre-commit` **auto-formata** (gofmt/prettier),
  `pre-push` roda o gate, `commit-msg` valida Conventional Commits (commits em pt-BR
  com prefixo convencional).
- **Comando de formatação** explícito (`make fmt` / `npm run format`) para rodar à mão.
- **Workflow de PR** (GitHub Actions) rodando o gate unificado + comentário no PR.
- `baseline.json` inicial commitado, tirado sobre o scaffold (0001+0002).
- Config das ferramentas: `.golangci.yml`, ajuste do `eslint.config` se preciso.

**Fora (explicitamente):**
- **Mutation testing** (Stryker no JS, go-mutesting no Go) — go-mutesting é imaturo;
  fica como follow-up opcional, não-bloqueante.
- Promover os coletores Go para o **boatman upstream** (Opção C do ADR-0016) — follow-up.
- Publicação de badges/coverage em serviço externo (Codecov etc.).
- Gates de performance/bundle-size (quando houver o que medir, Fase 2+).

## 4. Contrato técnico

### Métricas no baseline unificado
Mantém as chaves do boatman e **acrescenta** as de Go (prefixo `go_` / `web_` para
desambiguar), por exemplo:

| Métrica | Fonte | Bloqueia? |
|---|---|---|
| `web_coverage_lines` | vitest coverage-summary.json | Sim (queda) |
| `web_eslint_errors` | ESLint json | Aviso |
| `web_duplicate_percent` | jscpd | Sim (aumento) |
| `web_audit_critical` | npm audit | Zero-tolerância |
| `go_coverage_lines` | `go test -coverprofile` → `go tool cover` | **Catraca pura** (queda bloqueia; sem piso absoluto no início) |
| `go_lint_issues` | golangci-lint (`--out-format json`) | **Aviso** (catraca incremental; endurece depois) |
| `go_vuln_high` | govulncheck | Zero-tolerância p/ críticas |
| `go_gofmt_unformatted` | `gofmt -l` (contagem) | **Sim, bloqueia** (deve ser 0) |
| `go_cyclo_violations` | gocyclo | Aviso |

> **Cobertura = catraca pura** (Go e TS): o baseline parte de onde o scaffold estiver e
> só pode subir; **nenhum piso absoluto** definido agora (chutá-lo com o projeto vazio
> travaria o começo) — adicionar piso é follow-up quando a base amadurecer.

### Coleta Go (feeds do gate)
- `go test ./... -coverprofile=reports/go-coverage.out` → converter para % de linhas.
- `golangci-lint run --out-format json > reports/golangci.json` (contar issues).
- `govulncheck -json ./... > reports/govulncheck.json` (contar por severidade).
- `gofmt -l .` → nº de arquivos não formatados (0 = ok).
- `gocyclo -over 10 .` → nº de funções acima do limite.
- O `quality-gate.mjs` estendido lê esses arquivos e mescla com as métricas JS.

### lefthook
- `lefthook.yml` com três ganchos:
  - **`pre-commit` (auto-formatação, rápido):** `gofmt -w`/`gofumpt -w` nos `.go`
    staged + `prettier --write` nos arquivos do `/web` staged, com **`stage_fixed: true`**
    (re-adiciona os arquivos corrigidos). Resolve a formatação **automaticamente** —
    ver §6, decisão do fluxo LLM.
  - **`pre-push` (gate, pesado):** roda o gate unificado em modo local
    (`--local`) — lint, cobertura, duplicação, gofmt-check dos dois lados; aborta o
    push em regressão bloqueante.
  - **`commit-msg`:** validação de Conventional Commits (mensagens em pt-BR com prefixo
    convencional, ver [CONTRIBUTING](../CONTRIBUTING.md)).
- Instalação via `lefthook install` documentada no CONTRIBUTING.
- **Nota de ambiente:** o `pre-push` exige as ferramentas Go instaladas localmente
  (`golangci-lint`, `govulncheck`, `gocyclo`); documentar no CONTRIBUTING. O `pre-commit`
  só precisa de `gofmt` (vem com o Go) e `prettier` (já no `/web`).

### CI (GitHub Actions)
- Job que: instala Node + Go, roda as ferramentas dos dois lados gerando os `reports/`,
  executa o gate em modo CI (Markdown), **falha** em regressão bloqueante e **comenta**
  o relatório no PR (comentário sticky).

### Arquivos afetados
- Gerados/editados: `scripts/quality-gate.mjs` (estendido), `scripts/count-lint.mjs`,
  `baseline.json`, `.github/workflows/quality-gate.yml`, `web/package.json` (scripts).
- Criados: `lefthook.yml`, `.golangci.yml`, config de gocyclo se necessário.
- `.gitignore`: `reports/`, `tmp/`.

## 5. Critérios de aceite

- [ ] `boatman init` gerou a metade JS e ela roda sozinha no `/web`.
- [ ] O gate estendido lê métricas **Go e TS** no **mesmo** `baseline.json`.
- [ ] `git push` com regressão bloqueante (ex.: cobertura Go caindo) é **abortado** pelo `pre-push`.
- [ ] O workflow de PR **falha** em regressão bloqueante e **comenta** a tabela de métricas.
- [ ] `commit-msg` rejeita mensagem fora do padrão Conventional Commits.
- [ ] `pre-commit` **auto-formata** arquivos staged (gofmt/prettier) e os re-adiciona,
      sem depender de editor; um commit de código desformatado sai formatado.
- [ ] `gofmt -l` no gate retorna 0 (formatação bloqueante como rede de segurança).
- [ ] `quality:baseline` regenera e avança o `baseline.json` (Go + TS).
- [ ] O gate **não** entra na imagem de produção ([0003](0003-empacotamento-dockerfile-multistage.md)).

## 6. Notas de implementação / decisões

- Arquitetura, runner e hooks vêm do [ADR-0016](../docs/adr/0016-quality-gate-catraca-unificada.md):
  catraca **unificada**, runner **Node partindo do boatman**, hooks com **lefthook**.
- O script gerado pelo boatman é **standalone e "seu"** (não importa nada do boatman em
  runtime), então estendê-lo com coletores Go é o caminho previsto — não um hack.
- **`go_lint_issues` = aviso no começo** (decisão de sessão, 2026-07-08): catraca
  incremental (à la complexidade do boatman) que **avisa** quando aumenta sem travar,
  para não brigar com o linter no scaffold; **endurece para bloqueante depois**, quando
  a dívida estiver zerada.
- **Formatação bloqueante + auto-fix no `pre-commit`** (decisão de sessão, 2026-07-08).
  Contexto: o código é **~95% gerado por LLM** e **não passa por um editor** com
  format-on-save, então não se pode depender do editor. Solução em camadas: (1) o
  `pre-commit` do lefthook roda `gofmt -w`/`prettier --write` e re-adiciona os arquivos
  (`stage_fixed`), formatando **automaticamente**; (2) o gate mantém `gofmt -l == 0`
  como métrica **bloqueante**, de rede de segurança caso o hook seja pulado; (3) há
  comando explícito (`make fmt` / `npm run format`) para rodar à mão. Assim a
  formatação é rígida **sem** atrito para o fluxo com LLM.
- **Cobertura = catraca pura** (decisão de sessão, 2026-07-08): sem piso absoluto no
  início — só proibir queda a partir do baseline do scaffold. Piso absoluto vira
  follow-up quando a base de testes amadurecer.
- Baseline inicial tirado sobre o scaffold: cobertura provavelmente baixa no começo — a
  catraca garante que só suba dali pra frente.
- **Escopo do lado TS enxuto** (decisão de sessão, 2026-07-08): o scaffold não tem
  vitest nem ESLint (o `create-vite` trouxe **oxlint**, considerado imaturo). Decisão:
  **remover o oxlint** do `web/` e não construir coletores customizados em cima dele;
  o lado TS entra na catraca só com **duplicação (jscpd)** e **`npm audit` crítico**.
  Cobertura e lint do front ficam como follow-up (entram com ESLint + vitest quando as
  telas da Fase 2 amadurecerem). O lado Go entra completo.
- **Gate na raiz, sem `package.json` de raiz.** A catraca é `scripts/quality-gate.mjs`
  (na raiz), chamada direto por `node ...` no lefthook e no CI; os atalhos humanos ficam
  num **`Makefile`** (`make fmt|gate|gate-local|baseline|hooks`), que encaixa melhor num
  repo poliglota que um `package.json` de raiz.
- **`toolchain go1.26.5` no `go.mod`** (decisão de implementação): com o Go 1.26.4 o
  `govulncheck` reportava `GO-2026-5856` (vuln alcançável de `crypto/tls` via
  `ListenAndServe`), corrigida em `crypto/tls@go1.26.5`. Como `go_vuln_high` é
  zero-tolerância, o baseline nasceria vermelho; a remediação prescrita é subir o
  toolchain. O workflow usa `go-version: 1.26.x` (≥ 1.26.5) pelo mesmo motivo.
- **`web/package-lock.json` deve ser gerado com npm ≥ 11.16** (node:lts). Um lock gerado
  por npm mais antigo (ou via `--package-lock-only`) fica dessincronizado (faltam peers
  `@emnapi/*`) e o `npm ci` do CI **falha**. Regenerar com `npm install` sob node:lts.

## 7. Fora deste briefing / follow-ups

- **Opção C** do ADR-0016: promover os coletores Go para o **boatman upstream**,
  tornando-o poliglota (beneficia a comunidade e outros projetos).
- **Mutation testing** nos dois lados quando a base de testes amadurecer.
- **Piso absoluto de cobertura** (além da catraca) quando a base de testes amadurecer.
- **Endurecer `go_lint_issues` para bloqueante** quando a dívida de lint estiver zerada.
- Gate de **bundle-size** do frontend e de **tamanho da imagem** Docker (Fase 2+).
