# 0021 — UI de idiomas: admin de config + seletor de leitura

> A superfície do multi-idioma para o dono: uma tela de **Configurações** para definir
> principal/fallback/permitidos, e um **seletor de idioma** na leitura que passa o
> `?lang=` — mostrando, via proveniência, quando um campo caiu no fallback.

| | |
|---|---|
| **ID** | 0021 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md), [ADR-0019](../docs/adr/0019-resolucao-por-idioma-fallback-proveniencia.md), [05-api](../docs/05-api.md) |
| **Status** | draft |
| **Depende de** | [0019](0019-fundacao-multi-idioma-settings-resolver.md) |

---

## 1. Objetivo

Dar ao gestor o controle dos idiomas e ao leitor a escolha do idioma. Consome os
endpoints do [0019](0019-fundacao-multi-idioma-settings-resolver.md) (settings +
`?lang=` + `resolved_languages`).

## 2. Escopo (a detalhar quando aterrissar)

**Dentro:**
- **Tela de Configurações → Idiomas**: define principal, fallback e permitidos
  (multi-select), com validação (principal/fallback ⊆ permitidos). Salva via
  `PUT /settings/languages`; invalida as queries afetadas.
- **Seletor de idioma** na leitura (grid/detalhe): passa `?lang=`; default = principal.
  Persistência leve da escolha (ex.: querystring/local).
- **Indicador de proveniência**: quando `resolved_languages[campo] !== lang` (caiu no
  fallback), sinalizar discretamente ("sinopse em inglês") — a base para o dono depois
  acrescentar a tradução no editor ([0016](0016-editor-override-ui.md)).
- Testes vitest (settings form, seletor, indicador) + validação Playwright.

**Fora:** o editor de override por idioma ([0016](0016-editor-override-ui.md)); a
edição de tags por idioma (junto do 0016 revisado).

## 3. Notas

- Reusar shadcn/ui (form/select/badge). O seletor de idioma pode morar no `AppHeader`.
- Lista de idiomas permitidos alimenta tanto o admin quanto o seletor (fonte única).
