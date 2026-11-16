# ADR-0016 — Quality gate: unified ratchet for Go + TS

- **Status:** Accepted
- **Date:** 2026-07-08
- **Agenda:** [QG1](../00-decision-agenda.md)

## Context

MyMediaDB is a **polyglot** monorepo: Go backend + TypeScript frontend. Before writing
the first line, the owner wanted a **quality gate** that validates code quality and
coverage on every delivery, running both **locally before push** and **in the PR**.

There is [boatman](https://github.com/rafaelvieiras/boatman) (the owner's own project):
a zero-dependency CLI that installs a quality **ratchet** in JS/TS projects — it writes
a `baseline.json`, re-compares on every PR and **blocks regression** (nothing ever
gets silently worse; improvements advance the baseline). Problem: boatman's metric
collectors are **hard-wired to JS tools** (vitest/jest coverage, ESLint, jscpd,
npm audit, Stryker) and **cannot see Go** — the backend would be left with unmeasured
coverage.

## Decision

**One unified ratchet, one `baseline.json`, covering Go + TS** (Option A among unified /
two separate gates / extend boatman upstream).

1. **Node runner, starting from boatman.** Run `boatman init` to generate the JS half
   (standalone `quality-gate.mjs` script + workflow + baseline) and **extend the
   generated script** to also read the reports of the Go toolchain. The gate is a
   **dev/CI** tool — it does not go into the production binary, so it does not break the
   CGO-free *single binary* principle ([ADR-0014](0014-implementation-stack.md)).
2. **Go collectors added** to the same baseline:
   - Lint → **golangci-lint** (meta-linter).
   - Formatting → **gofmt/gofumpt** (check mode).
   - Coverage → **`go test -coverprofile`** → `go tool cover`.
   - Vulnerabilities → **govulncheck**.
   - Complexity → **gocyclo/gocognit** (non-blocking).
3. **Local hooks with lefthook** (not husky). Single Go binary, polyglot: it runs the
   Go task and the JS task side by side, without requiring Node just for the hooks to
   exist — a better fit for a Go+TS repo than husky (Node-only), which boatman
   generates by default.
4. **Two runs:** local `pre-push` (early feedback) + PR workflow (locks the merge and
   comments the report).

Ratchet rules preserved from boatman: coverage/duplication/critical vulns **block**;
lint/complexity/mutation **warn**; improvements require consciously advancing the
baseline.

## Consequences

- A single honest "green/red" for the whole project (Go included) — no false "all
  green" with the backend unmeasured.
- Reuses boatman where it is strong; the Go half is our own code in the repo (the
  generated script is standalone and "yours", like shadcn components).
- **Coupling avoided:** MyMediaDB's progress does not wait for boatman to become
  polyglot. Promoting the Go collectors upstream to boatman remains a **follow-up**
  (Option C), not a prerequisite.
- Cost: maintaining a local extension of the generated script as boatman evolves; and
  requiring Node in the CI/dev environment to run the ratchet (already present because
  of the frontend).
- The baseline is only meaningful when there is code to measure: bootstrap with an
  initial baseline as soon as the scaffold appears (backend skeleton and frontend
  scaffold with API integration).
