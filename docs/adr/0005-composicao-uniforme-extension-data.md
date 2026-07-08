# ADR-0005 — Composição uniforme (inclui `extension_data`)

- **Status:** Aceita
- **Data:** 2026-07-06
- **Agenda:** [R5](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

Faltava definir se os campos de `extension_data` (verticais) passam pelo resolver
como os do núcleo, ou se teriam tratamento próprio.

## Decisão

**Sim, composição uniforme.** Núcleo e extensão passam pelo mesmo resolver:
`override ⟵ externo ⟵ arquivo` vale campo a campo, **inclusive dentro dos
namespaces de `extension_data`**. O patch de override espelha a entity inteira,
extensão incluída.

## Consequências

- Uma vertical nova ganha override/enriquecimento "de graça", sem código no resolver.
- Decorre de [ADR-0001](0001-resolver-particionamento-por-origem.md) (motor genérico)
  e [ADR-0004](0004-override-como-blob-json-esparso.md) (blob espelha a entity).
