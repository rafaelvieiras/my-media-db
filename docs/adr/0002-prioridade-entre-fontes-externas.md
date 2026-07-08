# ADR-0002 — Prioridade entre fontes externas (lista global)

- **Status:** Aceita
- **Data:** 2026-07-06
- **Agenda:** [R2](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

Com mais de uma fonte externa (ex.: TMDB + TVDB) trazendo valor para o mesmo campo,
era preciso um critério de desempate na camada externa, antes do override.

## Decisão

Usar uma **lista de prioridade global e configurável** de fontes (ex.:
`[tmdb, tvdb, musicbrainz]`). Por campo, vence a **primeira fonte da lista que tiver
valor**; o resultado forma a base externa, sobre a qual o override entra. **Default
do MVP:** `[tmdb]`.

## Consequências

- Comportamento previsível e configurável pelo dono, sem regra por campo no MVP.
- Prioridade por campo e "fonte primária por entidade" ficam como evolução pós-MVP,
  sem quebrar o modelo.
- Independe do override, que sempre vence ([ADR-0001](0001-resolver-particionamento-por-origem.md)).
