# ADR-0007 — Fatos técnicos são overridáveis

- **Status:** Aceita
- **Data:** 2026-07-06
- **Agenda:** [R7](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

Restava decidir se fatos técnicos extraídos do arquivo (mediainfo: codec, HDR,
faixas) são intocáveis ou se o dono pode corrigi-los — ex.: um HDR falso-positivo
do `ffprobe`.

## Decisão

**Sim**, são overridáveis pelo mesmo mecanismo universal, sem regra especial. O
dado detectado permanece em `media_file` (não-destrutivo); o override entra por
cima na composição. Uso de exceção, não de rotina.

## Consequências

- Nenhum campo fica "blindado"; o princípio de autoridade do dono não tem exceção.
- O detectado original segue preservado para re-derivação.
- Reforça a universalidade do override ([ADR-0001](0001-resolver-particionamento-por-origem.md)).
