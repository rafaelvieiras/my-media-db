# ADR-0016 — Quality gate: catraca unificada (ratchet) Go + TS

- **Status:** Aceita
- **Data:** 2026-07-08
- **Agenda:** [QG1](../00-agenda-de-decisoes.md) · **Briefing:** [0005](../../briefings/0005-quality-gate-catraca-unificada.md)

## Contexto

O MyMediaDB é um monorepo **poliglota**: backend Go + frontend TypeScript. Antes de
escrever a primeira linha, o dono quis um **quality gate** que valide qualidade de
código e cobertura a cada entrega, rodando tanto **localmente antes do push** quanto
**no PR**.

Existe o [boatman](https://github.com/rafaelvieiras/boatman) (projeto do próprio dono):
um CLI zero-dependência que instala uma **catraca (ratchet)** de qualidade em projetos
JS/TS — grava um `baseline.json`, recompara a cada PR e **bloqueia regressão** (nunca
se piora em silêncio; melhorias avançam o baseline). Problema: os coletores de métrica
do boatman são **cravados em ferramentas JS** (vitest/jest coverage, ESLint, jscpd,
npm audit, Stryker) e **não enxergam Go** — o backend ficaria com cobertura não medida.

## Decisão

**Uma catraca unificada, um `baseline.json`, cobrindo Go + TS** (Opção A entre
unificado / dois gates separados / estender o boatman upstream).

1. **Runner em Node, partindo do boatman.** Roda-se `boatman init` para gerar a metade
   JS (script standalone `quality-gate.mjs` + workflow + baseline) e **estende-se o
   script gerado** para também ler os relatórios do toolchain Go. O gate é ferramenta
   de **dev/CI** — não vai no binário de produção, então não fere o princípio de
   *single binary* CGO-free ([ADR-0014](0014-stack-de-implementacao.md)).
2. **Coletores Go adicionados** ao mesmo baseline:
   - Lint → **golangci-lint** (meta-linter).
   - Formatação → **gofmt/gofumpt** (modo check).
   - Cobertura → **`go test -coverprofile`** → `go tool cover`.
   - Vulnerabilidades → **govulncheck**.
   - Complexidade → **gocyclo/gocognit** (não-bloqueante).
3. **Hooks locais com lefthook** (não husky). Binário Go único, poliglota: roda tarefa
   Go e tarefa JS lado a lado, sem exigir Node só para os hooks existirem — melhor
   encaixe num repo Go+TS que o husky (Node-only) que o boatman gera por padrão.
4. **Duas execuções:** `pre-push` local (feedback cedo) + workflow de PR (trava o
   merge e comenta o relatório).

Regras da catraca preservadas do boatman: cobertura/duplicação/vulns críticas
**bloqueiam**; lint/complexidade/mutation **avisam**; melhorias exigem avançar o
baseline conscientemente.

## Consequências

- Um único "verde/vermelho" honesto para o projeto inteiro (Go incluso) — sem falso
  "tudo verde" com o backend sem medição.
- Reaproveita o boatman onde ele é forte; a metade Go é código próprio no repo (o
  script gerado é standalone e "seu", como componentes shadcn).
- **Acoplamento evitado:** o progresso do MyMediaDB não espera o boatman virar
  poliglota. Promover os coletores Go para o boatman upstream fica como **follow-up**
  (Opção C), não como pré-requisito.
- Custo: manter uma extensão local do script gerado quando o boatman evoluir; e exigir
  Node no ambiente de CI/dev para rodar a catraca (já presente pelo frontend).
- O baseline só é significativo quando há código a medir: bootstrap com baseline
  inicial ao surgir o scaffold ([0001](../../briefings/0001-backend-esqueleto-servidor.md)/[0002](../../briefings/0002-frontend-scaffold-integracao-api.md)).
