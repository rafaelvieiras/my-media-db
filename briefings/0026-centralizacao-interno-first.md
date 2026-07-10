# 0026 — Centralização interno-first: priorizar o que já está na biblioteca

> Hoje as telas do EXTERNO (página da coleção, banda de coleção, cards de parte)
> tratam todo item como "não salvo": oferecem Importar e navegam para o detalhe
> EXTERNO mesmo quando o item JÁ FOI importado. O dono quer o oposto: se existe
> uma entity interna correspondente, a UI deve mostrar "na biblioteca", linkar para
> o destino INTERNO (`/entities/{id}`) e NÃO oferecer reimportar. Feedback direto do
> dono: abrir a coleção de um filme importado mostra o próprio filme como "importar"
> e leva ao TMDB, não ao item interno.

| | |
|---|---|
| **ID** | 0026 |
| **Tipo** | feature/fix |
| **Fase (roadmap)** | Fase 3 — extensão pós-revisão |
| **Docs relacionadas** | [05-api](../docs/05-api.md), [04-resolver](../docs/04-resolver.md) |
| **Status** | backend feito; frontend em implementação |
| **Depende de** | [0024](0024-paridade-detalhe-entity-rico.md), [0025](0025-colecao-entity-interna.md) |

---

## 1. Objetivo

Resolver externo `(source, source_id)` → entity INTERNA e refletir isso na UI:
- **Página da coleção externa** (`/collection/$source/$id`): cada PARTE já importada
  vira card "na biblioteca" que linka para `/entities/{id}` (sem botão Importar); a
  COLEÇÃO, se já importada, troca "Importar coleção" por "Ver coleção" (link interno).
- **Banda de coleção** (no detalhe do externo E no detalhe da entity): quando a
  coleção já é entity, o card linka para `/entities/{id}` interno, não para a página
  externa.
- **Excluir coleção**: a página da entity coleção ganha ação de exclusão (o endpoint
  `DELETE /entities/{id}` já existe — purga com CASCADE das relações, não dos filmes).

## 2. Backend — resolução externo→interno (sem migração) — FEITO

Só a fonte `tmdb` deduplica hoje (colunas geradas `external_id_tmdb`; ver 0015). Para
outras fontes o `internal_id` sai `null` (degrada — sem lookup). Reusa as queries
existentes: `ObterEntityPorTmdb` (filme/tv) e `ObterColecaoPorTmdb` (coleção, 0025).

Helpers no `Server` (`internal/server/interno.go`, retornam `*string`, nil quando não
há entity, source≠tmdb, ou erro — a leitura nunca cai por causa deles):
- `idInternoItem(ctx, source, sourceID)` → `ObterEntityPorTmdb`.
- `idInternoColecao(ctx, source, sourceID)` → `ObterColecaoPorTmdb`.

Contrato de API (campo ADITIVO, opcional, `null` quando ausente):
- `ExternalCollectionDetail.internal_id` — id interno da coleção, se importada.
- `ExternalCollectionPart.internal_id` — id interno da parte, se importada.
- `ExternalCollection.internal_id` (o objeto da BANDA, presente em `ExternalDetail`
  E em `ResolvedEntity`) — id interno da coleção, se importada.
  - `richfields.Collection` ganha `InternalID *string` (`ColecaoDe` segue PURA —
    devolve nil; quem tem DB preenche): setado em `montarDetalheExterno` (handler) e,
    para a entity, em `resolverEntity` DEPOIS de `resolver.Resolver` (o resolver é
    função pura, não acessa banco — o server pós-processa `.Collection.InternalID`).

`similar` NÃO ganha `internal_id` nesta rodada (o dono não pediu; evita rajada de
lookups por N miniaturas) — follow-up.

## 3. Frontend

- **`ExternalResultCard`** ganha prop opcional `internalId?: string | null`. Quando
  presente: pôster+título linkam para `/entities/$id` (interno), selo "Na biblioteca",
  e o botão Importar some. Sem ela, comportamento atual (externo + Importar). Reuso
  vale para a página da coleção (e, de graça, para futuros usos como a busca).
- **Página da coleção**: passa `parte.internal_id` ao card; o header mostra
  "Ver coleção" (Link para `/entities/{internal_id}`) quando `data.internal_id`, senão
  "Importar coleção".
- **`ColecaoBand`**: quando `colecao.internal_id`, linka `/entities/{internal_id}`;
  senão `/collection/$source/$id` (atual).
- **Excluir coleção**: na página `/entities/{id}` com `type==='collection'`, ação de
  exclusão (confirmação em duas etapas) → `purgeEntity(id)` → invalida `['entities']`
  e navega para a home.

## 4. Decisões / follow-ups

- `internal_id` só para `tmdb` (única fonte com dedup); outras fontes = null.
- Card "na biblioteca" reusa o pôster EXTERNO da parte (direto) — não busca o pôster
  interno (evita N idas); trocar pela arte interna é refino futuro.
- `similar` interno-aware e a busca externa interno-aware = follow-ups.
