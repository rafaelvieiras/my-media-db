# ADR-0012 — Ciclo de vida não-destrutivo

- **Status:** Aceita
- **Data:** 2026-07-08
- **Agenda:** [MD5](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md), [06](../06-biblioteca-e-scan.md)

## Contexto

Quando um arquivo some do disco, o item deveria virar `wanted`? órfão? O risco é um
desmonte de HD virar "sumiço" de milhares de itens, ou uma intenção do dono ser
reescrita por um evento de filesystem.

## Decisão

**Evento de arquivo mexe só na posse; nunca no `status` nem no trabalho do dono.**
Arquivo some (datasource acessível) → `media_file` vira **`missing`** (guarda
`last_seen`), entity preservada, posse→não, `status` **intacto**; reversível se
reaparecer. **Datasource inteiro offline** (HD desmontado) → nada vira `missing`, o
datasource é sinalizado offline. **Órfã** = entity auto-criada, arquivo sumiu,
**zero investimento do dono** → fila de limpeza, **nunca apagada sozinha**.
**Purga** é a única operação destrutiva, sempre explícita.

## Consequências

- O disco ausente nunca é confundido com deleção.
- Coerente com [ADR-0009](0009-posse-e-status-eixos-ortogonais.md): o sistema não
  reescreve intenção.
- Purga remove entity + `media_external`/`media_override`/assets.
