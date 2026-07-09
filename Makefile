# Makefile — atalhos humanos do MyMediaDB.
#
# O quality gate propriamente dito é `node scripts/quality-gate.mjs` (chamado
# direto pelo lefthook e pelo workflow de CI). Estes alvos são conveniência.

.DEFAULT_GOAL := help
.PHONY: help fmt gate gate-local baseline hooks

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

baseline: ## Regenera e avança o baseline.json (Go + TS)
	node scripts/quality-gate.mjs --generate-baseline

hooks: ## Instala os hooks de git do lefthook
	lefthook install
