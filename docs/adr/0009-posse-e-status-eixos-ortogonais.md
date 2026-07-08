# ADR-0009 — Posse e `status` como eixos ortogonais

- **Status:** Aceita
- **Data:** 2026-07-07
- **Agenda:** [MD2](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

Na doc 04, um único `status` conflava "tenho o arquivo" (fato) com "o que pretendo"
(intenção) — o valor `owned` era posse disfarçada de intenção.

## Decisão

**Dois eixos ortogonais.** **Posse = fato derivado** (existe `media_file` presente?),
não editável, não mente. **`status` = intenção do dono**, um estado por vez:
`cataloged` (default), `wanted`, `unreleased`, `draft`. O `owned` é removido. Os
eixos combinam livremente (ex.: `wanted` + posse=sim = "tenho cópia, quero upgrade").

## Consequências

- Sustenta o "banco maior que a biblioteca" sem gambiarra.
- Posse pode virar coluna gerada para filtro rápido.
- As transições quando um arquivo aparece/some ficam em
  [ADR-0012](0012-ciclo-de-vida-nao-destrutivo.md).
