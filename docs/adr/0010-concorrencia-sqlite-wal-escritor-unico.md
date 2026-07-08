# ADR-0010 — Concorrência SQLite: WAL + escritor único

- **Status:** Aceita
- **Data:** 2026-07-08
- **Agenda:** [MD3](../00-agenda-de-decisoes.md) · **Spec:** [03](../03-arquitetura.md)

## Contexto

A API precisa estar sempre no ar enquanto o scan escreve pesado, mas o SQLite
aceita **um escritor por vez**. Sem estratégia, o escritor bloqueia leitores e a UI
trava com `database is locked` durante o scan.

## Decisão

Receita em três camadas. **(1) WAL + pragmas**: `journal_mode=WAL`,
`busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON` — vários leitores + um
escritor sem bloqueio mútuo. **(2) Dois pools**: pool de N leitores + **uma**
conexão de escrita (o escritor único cai de um pool de escrita de tamanho 1).
**(3) Escritas curtas, dono na frente**: o scan comita em lotes curtos; escrita
interativa (override/import) tem prioridade sobre a do scan em massa.

## Consequências

- Leitura concorrente com o scan; sem `database is locked` na UI.
- Evolui para uma **fila/goroutine escritora** dedicada se a priorização precisar
  ser explícita (backpressure/ordenação), sem mudar o resto.
- `synchronous=NORMAL` é seguro porque os dados de scan são re-deriváveis.
