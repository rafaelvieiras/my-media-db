# ADR-0013 — Override de relações (elenco/equipe) por operações

- **Status:** Aceita
- **Data:** 2026-07-08
- **Agenda:** [MD6](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

O [ADR-0003](0003-override-de-listas-por-operacoes.md) cobriu listas de valores
simples, mas adiou elenco/equipe por serem **relações** (tabelas `person`/`credit`),
bem mais complexas.

## Decisão

**Generalizar o merge por operações para créditos estruturados.** O override guarda
**add/remove/edit** (papel, personagem, `ord` de billing); a base externa (`credit`
do payload) segue viva e evolui em re-scrape. A **chave do crédito** é `person_id` +
**papel/departamento** (a mesma pessoa em dois papéis = dois créditos), com o
`credit_id` do provider como desempate. Pessoa adicionada pelo dono vira `person`
**local** (`external_ids` vazio, ok por [ADR-0011](0011-external-ids-autoritativo-media-external-cache.md)).

## Consequências

- O resolver compõe **crédito a crédito**, igual a qualquer campo
  ([ADR-0005](0005-composicao-uniforme-extension-data.md)); as ops vivem no
  `media_override` ([ADR-0004](0004-override-como-blob-json-esparso.md)).
- Fecha a espec do resolver para relações.
