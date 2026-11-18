# ADR-0036 — A metric that could not be measured blocks the gate

- **Status:** Accepted
- **Date:** 2026-11-18
- **Agenda:** [QG1](../00-decision-agenda.md)

## Context

The gate ([ADR-0016](0016-unified-quality-gate-ratchet.md)) compares each metric with
`baseline.json` and blocks a regression. Every collector could fail to produce a number —
`go test` does not compile, vitest crashes, `tsc` cannot start, the JSON report is not
written — and the script handled all of that the same way: it warned, wrote
`reports/<key>-failure.log` and returned `null`, which is also what a *deliberate* skip
returns (side not touched by the diff, `--fast`, metric that belongs to another workflow).

`compareMetrics` treats `null` as non-blocking. So a broken unit suite showed up as
"pulada" and the gate **passed**. Nothing else in CI runs the unit suite: the E2E job
builds the binary and runs the journeys, it does not run `go test ./...`. A commit whose
tests do not even compile could reach `master` with a green ratchet, as long as the
metrics that *were* measured did not get worse.

That is the difference between a gate and a progress bar: "I could not look" was being
reported as "I looked and it is fine".

## Decision

**Three outcomes, never confused.**

| outcome | written as | meaning | verdict |
|---|---|---|---|
| a number | `75.8` | measured | compared with the baseline |
| `null` | `null` | skipped **on purpose** — side untouched, `--fast`, metric of another workflow, optional tool absent locally | not a verdict |
| `'failed'` | `FALHOU` | it ran and could not produce a number | **blocking** |

1. Every "could not measure" path returns `falhou(key, stdout, stderr, motivo)`: it writes
   the full output to `reports/<key>-failure.log` and returns `FALHOU`.
2. `compareMetrics` puts `FALHOU` in the blocking list with a **distinct cause** —
   *"não foi possível medir"* instead of *"piorou vs baseline"* — because the fix is
   different. The metric's own policy still decides: a warning-level metric (gocyclo)
   stays a warning.
3. In the split CI mode, a key **absent** from a partial report is `FALHOU`, not `null`:
   a real skip arrives as an explicit `null`, so absence means the job never measured it.
4. `--generate-baseline` never writes `FALHOU` into `baseline.json` — it warns and keeps
   the previous value, because a failed measurement says nothing about the reference.
5. A missing **optional** tool (`golangci-lint`, `govulncheck`, `gocyclo`) still skips:
   the metric is optional by design and dev machines do not carry the tools. CI installs
   them and now **verifies they are on the PATH** in its own step, so where the verdict
   matters that path cannot be taken.

## Consequences

- A test suite that does not compile fails the gate, with the compiler output in the log
  and in the PR comment — instead of a green table with a "pulada" cell.
- The pre-push loop can now fail for reasons that are not a quality regression but a
  broken environment (`tsc` missing, `node_modules` half-installed). That is the point:
  it fails loudly, names the metric and the log, and does not pretend.
- `null` keeps exactly the meaning it had; nothing that was legitimately skipped becomes
  red.
- The failure log per metric already existed; the change is only in what the verdict does
  with it.
