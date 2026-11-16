# ADR-0031 — Post-scan per library: the scan closes its own cycle

- **Status:** Accepted
- **Date:** 2026-11-08
- **Agenda:** [SCAN-POS](../00-decision-agenda.md) · **Spec:** [06-library-and-scan](../06-library-and-scan.md), [07-enrichment-and-providers](../07-enrichment-and-providers.md)

## Context

The scan is the only trigger for media entering the catalog, but it is **not** the
owner of the enrichment cycle. Today:

- the match pipeline runs **a single time per file**, inside the scan;
- whatever failed stays as a draft until the periodic `saude_biblioteca` job,
  which reconciles at most 15 items per type per run, with a 15-day throttle
  per item;
- the new-movie auto-match trigger was implemented **inside the batch write
  transaction** and always fails (`sql: no rows in result set`, measured in
  production on 2026-11-08) — and it would block the scan if it tried to write,
  because the write pool has a single connection (ADR-0010).

Observed result: a new item comes in raw (no metadata, no artwork, no
`external_ids`) and the owner has no way of knowing there is a pending retry. Principle
4 of `AGENTS.md` ("optional and pulled enrichment") remains respected — the
trigger is the **owner's action** of scanning the library, not a periodic
sweep of the catalog.

## Decision

1. **No enrichment inside the scan transaction.** Post-import triggers
   (auto-match, optional materialization) run **after the batch `Commit`**.
   Any read/write of the enrich inside the `BeginTx…Commit` is forbidden: the
   row does not exist for the read pool and the write blocks on the single
   connection.
2. **The scan has a final post-scan step**, per library, best-effort and
   capped: it reconciles the draft tracks and series **of that source** and retries
   the auto-match of the pending ones of that source, materializing the artwork of
   those that gained identity when the auto-download toggle is on.
3. **The post-scan is part of the queue's `scan` item** (ADR-0028), not a new item:
   same concurrency 1, same cancellable `ctx`, progress reported as the step's
   message.
4. **There is no day-based throttle in the post-scan.** It is the retry of the scan the owner
   just asked for; the 15-day throttle still applies only to the
   periodic retroactive enrichment. The cost is bounded by per-run caps.

## Consequences

- A new item now leaves the scan with identity and artwork (when the source responds),
  and the draft that would pass on the second attempt leaves the review queue in the same
  click.
- The scan becomes longer per library (network calls in the final step); the
  per-type cap keeps the worst case predictable, and the owner sees the step in the
  progress message.
- New rule for anyone touching the scanner: **no enrichment effect inside a
  transaction**. A regression test covers the case of the importer that writes.
- Left out (future evolution): a dedicated enrichment queue with priority,
  retry with its own backoff, and reconciliation of provisional episodes.
- Related: [ADR-0010](0010-sqlite-wal-concurrency-single-writer.md),
  [ADR-0028](0028-unified-execution-queue-scan-and-jobs.md),
  [ADR-0029](0029-local-first-music-import-per-owned-track.md).
