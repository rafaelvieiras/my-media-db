# 0027 — Interno-first em similares/busca + editar external_ids + item manual

> Três frentes que aprofundam a "centralização interno-first" (0026) e abrem a
> catalogação manual: (A) similares e busca externa também linkam ao item interno
> quando já importado; (B) o dono passa a EDITAR a identidade externa (external_ids)
> da entity, para preparar sincronização futura (ids mudam, itens são fundidos/
> apagados na fonte); (C) criar um ITEM LOCAL manual (movie|tv) sem fonte — que, com
> (B), pode receber depois o id da fonte e sincronizar.

| | |
|---|---|
| **ID** | 0027 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — extensão pós-revisão |
| **Docs relacionadas** | [05-api](../docs/05-api.md), [03-modelo-dados](../docs/03-modelo-dados.md) |
| **Status** | backend feito; frontend em implementação |
| **Depende de** | [0025](0025-colecao-entity-interna.md), [0026](0026-centralizacao-interno-first.md) |

---

## Feature A — similares e busca externa interno-aware (estende 0026)

Reusa os helpers `Server.idInternoItem` (id tmdb → entity) do 0026. Campo aditivo
`internal_id: string|null` em:
- `ExternalSimilar` — cada similar do detalhe externo. `richfields.Similar` ganhou
  `InternalID`; `SimilaresDe` segue PURA (nil), e quem tem DB preenche: em
  `montarDetalheExterno` (handler) e em `resolverEntity` PÓS `resolver.Resolver` (loop
  sobre `resolvida.Similar`, mantendo o resolver puro).
- `ExternalResult` — cada resultado da busca externa. `mapearExternos` virou método
  `s.mapearExternos(ctx, res)` e anota o `internal_id` por resultado.

Só `tmdb` resolve (dedup por coluna gerada); N pequeno, lookup indexado. A UI mostra
"na biblioteca" e linka ao interno em vez de reimportar.

## Feature B — editar external_ids (`PUT /entities/{id}/external-ids`)

Corpo `{"external_ids": {"tmdb":"603","imdb":"tt0133093"}}` (mapa COMPLETO; PUT
substitui). Grava via `AtualizarExternalIds` (já existia); as colunas geradas
external_id_tmdb/imdb RE-DERIVAM do JSON — fixar o id prepara a sincronização. Valor
vazio REMOVE aquela fonte; chave (source) vazia → 422. 404 entity inexistente; 400
corpo inválido; 200 com a entity RESOLVIDA. **NÃO re-enriquece** (o dono chama
`/enrich` depois — só fixa a identidade). Handler em `internal/server/external_ids.go`.

## Feature C — criar item local manual (`POST /entities`)

Corpo `{type, title, original_title?, description?, release_date?}`, `type ∈ {movie, tv}`
(coleção tem `POST /collections`). Cria `entity` LOCAL sem external_ids, status DEFAULT.
201 com a entity resolvida; 422 type inválido/título vazio; 400 corpo inválido. Handler
em `internal/server/items.go`; o helper `nsOpcional` (campo opcional → NULL quando
vazio) é compartilhado com o `createCollection` (0025). Sem migração/sqlc novo (reusa
`CriarEntity`).

## Frontend (fica com os forks de UI)

- Similares (`SimilaresRail`) e resultados de busca externa interno-aware (reusa o
  padrão do `ExternalResultCard.internalId` do 0026 + selo "na biblioteca").
- Editor de external_ids na página da entity (linhas source→id, salvar via PUT).
- "Novo item" (form type+título) na home, análogo ao "Nova coleção".

## Decisões / follow-ups

- `internal_id` só para `tmdb`; outras fontes = null (sem dedup).
- `PUT external-ids` não valida o formato do id por fonte (aceita qualquer string) nem
  re-enriquece — refinos futuros.
- Item manual não valida o formato de `release_date` (ISO recomendado para a ordenação
  por ano) — refino futuro.
