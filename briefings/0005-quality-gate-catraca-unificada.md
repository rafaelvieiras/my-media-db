# 0005 — Quality gate: catraca unificada (ratchet) Go + TS

> Uma catraca de qualidade única para o monorepo: mede Go e TS, grava um baseline,
> roda no `pre-push` local e no PR, e **bloqueia regressão**.

| | |
|---|---|
| **ID** | 0005 |
| **Tipo** | chore |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único (infra transversal) |
| **Status** | draft |
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
- **lefthook** para os hooks locais (`pre-push` roda o gate; `commit-msg` valida
  Conventional Commits — o projeto usa commits em pt-BR com prefixo convencional).
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
| `go_coverage_lines` | `go test -coverprofile` → `go tool cover` | Sim (queda) |
| `go_lint_issues` | golangci-lint (`--out-format json`) | Aviso (ou bloqueia — decidir) |
| `go_vuln_high` | govulncheck | Zero-tolerância p/ críticas |
| `go_gofmt_unformatted` | `gofmt -l` (contagem) | Sim (deve ser 0) |
| `go_cyclo_violations` | gocyclo | Aviso |

### Coleta Go (feeds do gate)
- `go test ./... -coverprofile=reports/go-coverage.out` → converter para % de linhas.
- `golangci-lint run --out-format json > reports/golangci.json` (contar issues).
- `govulncheck -json ./... > reports/govulncheck.json` (contar por severidade).
- `gofmt -l .` → nº de arquivos não formatados (0 = ok).
- `gocyclo -over 10 .` → nº de funções acima do limite.
- O `quality-gate.mjs` estendido lê esses arquivos e mescla com as métricas JS.

### lefthook
- `lefthook.yml`: `pre-push` → `npm run quality:gate:local` (ou script equivalente que
  rode as duas metades); `commit-msg` → validação de Conventional Commits.
- Instalação via `lefthook install` documentada no CONTRIBUTING.

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
- [ ] `quality:baseline` regenera e avança o `baseline.json` (Go + TS).
- [ ] O gate **não** entra na imagem de produção ([0003](0003-empacotamento-dockerfile-multistage.md)).

## 6. Notas de implementação / decisões

- Arquitetura, runner e hooks vêm do [ADR-0016](../docs/adr/0016-quality-gate-catraca-unificada.md):
  catraca **unificada**, runner **Node partindo do boatman**, hooks com **lefthook**.
- O script gerado pelo boatman é **standalone e "seu"** (não importa nada do boatman em
  runtime), então estendê-lo com coletores Go é o caminho previsto — não um hack.
- **Bloqueante vs. aviso** para `go_lint_issues`: começar como **aviso** (catraca
  incremental, à la complexidade do boatman) e endurecer depois, para não travar o
  scaffold. Decidir o limiar na implementação.
- Baseline inicial tirado sobre o scaffold: cobertura provavelmente baixa no começo — a
  catraca garante que só suba dali pra frente.

## 7. Fora deste briefing / follow-ups

- **Opção C** do ADR-0016: promover os coletores Go para o **boatman upstream**,
  tornando-o poliglota (beneficia a comunidade e outros projetos).
- **Mutation testing** nos dois lados quando a base de testes amadurecer.
- Gate de **bundle-size** do frontend e de **tamanho da imagem** Docker (Fase 2+).
