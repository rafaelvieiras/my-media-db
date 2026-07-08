# ADR-0003 — Override de listas por merge de operações

- **Status:** Aceita
- **Data:** 2026-07-06
- **Agenda:** [R3](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

Override em campos-lista (`tags`, `genres`) poderia substituir a lista inteira ou
fazer merge. Substituir em bloco descartaria a evolução da base externa a cada
re-scrape.

## Decisão

**Merge por operações**, não substituição. O override guarda um conjunto de
**adições** e um de **remoções por valor**; a base externa segue viva por baixo. A
ordem exibida segue a externa, com remoções aplicadas e adições ao final; dedup na
composição. Vale **só para listas de valores simples** (`tags`, `genres`).

## Consequências

- Um re-scrape que traga um gênero novo o inclui sem desfazer as edições do dono.
- Elenco/equipe (relações) **não** são cobertos aqui — generalizados em
  [ADR-0013](0013-override-de-relacoes-por-operacoes.md).
- As operações vivem no blob de override ([ADR-0004](0004-override-como-blob-json-esparso.md)).
