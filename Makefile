# Makefile — atalhos humanos do MyMediaDB.
#
# O quality gate propriamente dito é `node scripts/quality-gate.mjs` (chamado
# direto pelo lefthook e pelo workflow de CI). Estes alvos são conveniência.

.DEFAULT_GOAL := help
.PHONY: help fmt gate gate-local gate-fast gate-local-full baseline baseline-e2e hooks e2e e2e-cov e2e-install

help: ## Lista os alvos disponíveis
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

fmt: ## Auto-formata Go (gofmt) e web (prettier)
	gofmt -w cmd internal
	cd web && npx --yes prettier --write .

gate: ## Roda a catraca em modo CI (saída Markdown)
	node scripts/quality-gate.mjs

gate-local: ## Roda a catraca em modo local (tabela humana; usada no pre-push)
	node scripts/quality-gate.mjs --local

gate-fast: ## Catraca em modo rápido (filtro por diff, sem coletores caros)
	node scripts/quality-gate.mjs --local --fast

gate-local-full: ## Catraca completa, ignorando o filtro por diff (uso manual)
	node scripts/quality-gate.mjs --local --full

baseline: ## Regenera e avança o baseline.json (Go + TS)
	node scripts/quality-gate.mjs --generate-baseline

baseline-e2e: ## Avanca a catraca de cobertura de navegador (Camada 2): rode 'make e2e-cov' antes
	node scripts/quality-gate.mjs --generate-baseline --e2e

hooks: ## Instala os hooks de git do lefthook
	lefthook install

e2e-install: ## Instala o Playwright do projeto e2e/ (npm install + browser)
	cd e2e && npm install && npm run install:browser

e2e: ## Roda as jornadas de navegador (Camada 2): builda front+binario com o fake
	cd e2e && npm run e2e

e2e-cov: ## Como `e2e`, mas coleta cobertura do binario (GOCOVERDIR) e do front (monocart)
	cd e2e && E2E_COVERAGE=1 npm run e2e
