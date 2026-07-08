# ADR-0006 — Presença da chave sinaliza override

- **Status:** Aceita
- **Data:** 2026-07-06
- **Agenda:** [R6](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

Num patch esparso é preciso distinguir "não fiz override neste campo" de "fiz
override para vazio" (apagar o valor externo). Sem isso, não há como o dono
silenciar um valor que a fonte insiste em trazer.

## Decisão

A **presença da chave** no patch é o sinal de autoridade: chave presente (mesmo com
`null`/vazio) → override ativo, vence a base, **inclusive para apagar**. Chave
ausente → herda da base. A UI distingue **"limpar override"** (remove a chave) de
**"definir vazio"** (chave com `null`). **Proveniência por campo não é exposta na
API** por ora.

## Consequências

- Semântica de deleção sem coluna extra; a UI consulta o override em separado.
- Expor proveniência fica como evolução (Fase 3).
- Depende do blob esparso ([ADR-0004](0004-override-como-blob-json-esparso.md)).
