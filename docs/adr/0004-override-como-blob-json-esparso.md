# ADR-0004 — Override como blob JSON esparso

- **Status:** Aceita
- **Data:** 2026-07-06
- **Agenda:** [R4](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

O armazenamento do override podia ser EAV `(entity_id, field, value)` ou um blob
JSON esparso. O esboço original da doc 04 usava EAV, que dificulta validar por
schema e representar merges de lista.

## Decisão

**Blob JSON esparso**: `media_override(entity_id, override_json, updated_at)` — uma
linha por entidade, JSON espelhando o formato da entity, só com os campos mudados.
Validável contra o mesmo schema (núcleo + plugins); comporta o `{add, remove}` de
listas e override dentro de `extension_data`. O resolver monta a base composta e
aplica o patch. **Substitui o esboço EAV da doc 04.**

## Consequências

- Uma leitura por entidade traz todo o override; casa com o schema de
  [ADR-0001](0001-resolver-particionamento-por-origem.md).
- Sustenta [ADR-0003](0003-override-de-listas-por-operacoes.md),
  [ADR-0005](0005-composicao-uniforme-extension-data.md) e
  [ADR-0006](0006-presenca-da-chave-sinaliza-override.md).
