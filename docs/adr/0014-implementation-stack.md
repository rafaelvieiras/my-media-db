# ADR-0014 — Implementation stack (backend + type contract)

- **Status:** Accepted
- **Date:** 2026-07-08
- **Agenda:** [doc 11, items 1–5](../11-open-decisions.md) · **Spec:** [03](../03-architecture.md), [05](../05-api.md), [06](../06-library-and-scan.md)

## Context

Doc 11 listed backend tool choices with annotated leanings, but without ratification.
All of them orbit the same pillar: **single binary, CGO-free, minimal memory, trivial
cross-compile (incl. ARM)**.

## Decision

Confirm the set (every leaning from doc 11):

- **HTTP router:** **chi** — minimalist, over `net/http`, composable middlewares
  (instead of echo, heavier/more opinionated).
- **SQLite driver:** **`modernc.org/sqlite`** — pure Go SQLite, **no CGO**
  (instead of `mattn/go-sqlite3`, which requires CGO and complicates cross-compile).
- **Data layer:** **sqlc** — typed SQL generating Go, no ORM (preserves fine control
  over FTS5, generated columns and JSON1).
- **Mediainfo:** **ffprobe** (FFmpeg), subprocess with stable JSON output
  (instead of `mediainfo-cli`, less ubiquitous).
- **Type contract:** **generated OpenAPI** from the API is the source of truth for the
  frontend's TS types (instead of hand-maintained types, which diverge).

## Consequences

- A truly portable static binary; simple build and distribution.
- The modernc/ffprobe performance trade-off is irrelevant in self-hosted use.
- Choices consistent with [ADR-0010](0010-sqlite-wal-concurrency-single-writer.md)
  (WAL/pools on the same driver) and with the subprocess architecture of doc 03.
- They are **Phase 1** decisions, reversible if an assumption changes.
