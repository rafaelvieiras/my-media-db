# ADR-0001 — Resolver: campos particionados por origem

- **Status:** Aceita
- **Data:** 2026-07-06
- **Agenda:** [R1](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

O resolver compõe três camadas (`override ⟵ externo ⟵ arquivo`). Faltava definir
se as camadas competem indiscriminadamente por todo campo ou se o espaço de campos
é particionado por origem — e onde essa regra mora.

## Decisão

Particionar por origem, sendo a origem **metadado do campo**, não hard-code. O
núcleo declara a origem no schema do núcleo; extensões/plugins declaram no JSON
Schema do próprio namespace. O resolver é um **motor genérico** que não conhece
campo por nome. A anotação declara só as **fontes-base** (`arquivo`/`externo`/
`nenhuma`); o **override é universal** (nunca declarado). Campos de sistema (`id`,
`created_at`, `content_hash`) ficam fora do resolver.

## Consequências

- Adicionar uma vertical/plugin não toca o núcleo nem o resolver.
- O resolver fica pequeno e estável; a complexidade vai para os schemas.
- Base para [ADR-0004](0004-override-como-blob-json-esparso.md) (blob validável) e
  [ADR-0005](0005-composicao-uniforme-extension-data.md) (composição uniforme).
