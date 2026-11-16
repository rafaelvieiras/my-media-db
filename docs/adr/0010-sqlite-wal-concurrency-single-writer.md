# ADR-0010 — SQLite concurrency: WAL + single writer

- **Status:** Accepted
- **Date:** 2026-07-08
- **Agenda:** [MD3](../00-decision-agenda.md) · **Spec:** [03](../03-architecture.md)

## Context

The API must stay up at all times while the scan writes heavily, but SQLite accepts
**one writer at a time**. Without a strategy, the writer blocks readers and the UI
hangs with `database is locked` during the scan.

## Decision

A three-layer recipe. **(1) WAL + pragmas**: `journal_mode=WAL`, `busy_timeout=5000`,
`synchronous=NORMAL`, `foreign_keys=ON` — several readers + one writer with no mutual
blocking. **(2) Two pools**: a pool of N readers + **one** write connection (the single
writer falls out of a write pool of size 1). **(3) Short writes, owner first**: the scan
commits in short batches; interactive writes (override/import) take priority over bulk
scan writes.

## Consequences

- Concurrent reads during the scan; no `database is locked` in the UI.
- Evolves into a dedicated **writer queue/goroutine** if prioritization needs to be
  explicit (backpressure/ordering), without changing the rest.
- `synchronous=NORMAL` is safe because scan data is re-derivable.
