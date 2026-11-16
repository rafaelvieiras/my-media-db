# ADR-0017 — SQLite migrations: embedded goose

- **Status:** Accepted
- **Date:** 2026-07-09
- **Agenda:** Phase 2 (persistence foundation)

## Context

[ADR-0014](0014-implementation-stack.md) fixed the driver (`modernc.org/sqlite`,
CGO-free) and the data layer (**sqlc**), but sqlc **generates typed queries — it
does not version the schema**. Phase 2 introduces the first real schema and, from
there on, every table evolution needs **versioned, deterministic migrations that
can be applied from the single binary** (no external step, no CGO, without
breaking the *single container* principle).

## Decision

Adopt **[goose](https://github.com/pressly/goose) as an embedded library**, not as
an external binary.

1. **Migrations in `.sql`**, numbered, versioned in the repo (e.g.
   `internal/db/migrations/0001_schema_inicial.sql`), with
   `-- +goose Up` / `-- +goose Down` blocks.
2. **Embedded via `//go:embed`** into an `embed.FS` and applied **at binary
   startup** through goose's library API (`goose.Up`), using the same
   `database/sql` handle opened over `modernc.org/sqlite` — no separate process.
3. **SQLite dialect** configured explicitly (`goose.SetDialect("sqlite3")`).
4. Migrations run **before** the server accepts traffic; a migration failure
   **aborts boot** (fail-fast).

## Consequences

- Schema versioned and reproducible; the binary migrates itself on startup —
  consistent with "one container, zero-config" ([03](../03-architecture.md)).
- One more Go dependency, but a mature one, CGO-free and compatible with the
  driver already chosen; it coexists with **sqlc** (goose versions the DDL; sqlc
  generates the access code).
- `Down` available for development; in production the policy is **forward only**
  (rollback is a manual exception).
- Alternatives discarded: **golang-migrate** (heavier, historically at odds with
  the CGO-free sqlite driver) and a **homegrown runner** (zero-dep, but it
  reinvents versioning/locking that goose already solves). Related decisions:
  [ADR-0014](0014-implementation-stack.md) (stack), [ADR-0010](0010-sqlite-wal-concurrency-single-writer.md)
  (WAL + single writer — migrations run through the write connection).
