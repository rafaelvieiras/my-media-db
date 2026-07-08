# ADR-0011 — `external_ids` autoritativo, `media_external` cache

- **Status:** Aceita
- **Data:** 2026-07-07
- **Agenda:** [MD4](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

O mesmo id externo aparece em `entity.external_ids` e em
`media_external.(source, source_id)`. Sem eleger a autoridade, as duas cópias podem
divergir.

## Decisão

**`entity.external_ids` é o mapa de identidade autoritativo** — todos os ids
conhecidos (buscados ou não); é onde o match grava e o lookup lê. **`media_external`
é cache de payload** por fonte buscada, com `source_id` **subordinado** (invariante:
`media_external.source_id == external_ids[source]`). **Conflito → a identidade
vence**: a linha de cache divergente é obsoleta (re-busca/descarta). O dono corrige
**a identidade** e **nunca edita `media_external` à mão**. Mesma lógica para
`person`/`company`.

## Consequências

- Identidade (qual registro externo) e valor editorial (`media_external` como base
  do resolver) ficam como eixos distintos.
- Lookup por id externo via coluna gerada + índice.
- Uma pessoa adicionada pelo dono nasce **local** (`external_ids` vazio) — usado em
  [ADR-0013](0013-override-de-relacoes-por-operacoes.md).
