# Contributing to MyMediaDB

Thanks for your interest. MyMediaDB is free software under the [MIT license](LICENSE).
This guide explains **how the project works**. Read it before you open a large
pull request: the process puts decisions and documentation before code.

## How the project thinks

Five principles apply to every contribution:

1. **The owner is the authority.** The local database is the source of truth.
   External data is a suggestion. Every edit you make is an override, and an
   override always wins.
2. **The catalog is larger than the library.** It holds what you own and what you
   plan to own, through the `status` field.
3. **Universal and extensible without touching the core.** Verticals and
   integrations live in namespaced `extension_data`. No per-type columns.
4. **Local-first and light.** One binary, one image, low memory. External sources
   are optional enrichment.
5. **Non-destructive.** Nothing deletes your data on its own. A purge is always
   explicit.

A pull request that breaks one of these principles will probably be rejected.
Open a discussion first.

## Decisions first, code later

- The [decision agenda](docs/00-decision-agenda.md) is the live backlog. An item
  starts open, gets discussed, and ends documented.
- A closed decision becomes an [ADR](docs/adr/) and moves into the spec docs.
- Before you propose a structural change (schema, resolver, API), check whether it
  touches a closed decision. If it does, propose a new ADR that supersedes the
  old one. Do not edit the spec directly.

## Contribution flow

1. **Open an issue** that describes the problem or idea before you write code for
   anything large.
2. **Fork the repository and create a branch** from `master`, for example
   `feat/incremental-scan`.
3. **Follow the stack and the style** of the code around you. See
   [docs/03](docs/03-architecture.md).
4. **Write in English** in code, comments, docs, and commit messages.
5. **Use Conventional Commits**: `type: summary` in the imperative, subject of 100
   characters or fewer. The release pipeline reads these messages to pick the next
   version number, so `feat:` and `fix:` matter.
6. **Open the pull request** and reference the issue.

## Development with hot reload

The dev bench starts the front end and the back end side by side, each with live
reload. You need Docker and Compose v2.

```bash
docker compose -f compose.dev.yaml up --build
```

Two services come up:

| Service | Port | What runs | What reloads |
| --- | --- | --- | --- |
| **web** | http://localhost:5173 | Vite dev server | Edit a `.tsx` or `.css` file and HMR swaps the module without a page reload |
| **api** | http://localhost:8080 | Go binary under [air](https://github.com/air-verse/air) | Edit a `.go` file and the binary rebuilds and restarts in seconds |

Open **http://localhost:5173**. The front end calls a relative `/api` path and the
Vite proxy forwards it to the `api` service, which is the same contract as
production, with no CORS. The proxy target comes from
`VITE_PROXY_TARGET=http://api:8080`, set in the compose file. Outside Docker,
`npm run dev` uses the default `http://localhost:8080`.

`Ctrl+C`, or `docker compose -f compose.dev.yaml down`, stops both cleanly. `air`
and `Dockerfile.dev` are dev-only dependencies and never enter the production
image.

> **File watching:** if saving a file does not trigger HMR or a rebuild, which is
> common on bind mounts of some filesystems, turn on polling: set
> `server.watch.usePolling = true` in `web/vite.config.ts`.

## Quality gate and git hooks

The project has one **quality ratchet** for Go and TypeScript. A single
`baseline.json` at the root only lets the metrics improve. It runs on `pre-push`
and in CI. See [ADR-0016](docs/adr/0016-unified-quality-gate-ratchet.md).

**Install the hooks once** with [lefthook](https://github.com/evilmartians/lefthook):

```bash
lefthook install
# lefthook via: go install github.com/evilmartians/lefthook@latest
#          or:  npm install -g lefthook   (or npx lefthook install)
```

That activates three hooks:

- **`pre-commit`** formats staged files (`gofmt -w`, `prettier --write`) and
  stages them again.
- **`pre-push`** runs the ratchet and stops the push on a blocking regression.
- **`commit-msg`** checks the Conventional Commits format.

**Tools for `pre-push`.** The ratchet needs these tools on your `PATH`. These
three are optional: when one is missing, its metric is skipped with a warning
instead of failing the gate (CI installs all three and verifies them, so the PR
always measures them):

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
# make sure "$(go env GOPATH)/bin" is on your PATH
```

`govulncheck` has zero tolerance. Keep the Go toolchain current.

**Web tools.** The front end has ESLint and vitest with v8 coverage. The ratchet
reads line coverage (blocking) and ESLint issue count (warning). Install the web
dependencies first — without them those collectors cannot run, which is a
**blocking** "not measured", not a skip:

```bash
cd web && npm install
npm run lint
npm run test:coverage     # writes web/coverage/coverage-summary.json
```

**Measured, skipped, or not measured.** Three different things
(see [ADR-0036](docs/adr/0036-gate-unknown-is-not-fine.md)):

| result | meaning | blocks? |
| --- | --- | --- |
| a number | the collector measured it | only if it got worse than `baseline.json` |
| *pulada* | skipped on purpose: the side was not touched by the diff, `--fast`, or the metric belongs to another workflow | no |
| *não medido* | the collector ran and could not produce a result (suite does not compile, tool missing, report not written) | **yes** |

When a metric shows *não medido*, the full output of the failing command is in
`reports/<metric>-failure.log`.

**Commands** (Makefile at the root):

| Command | What it does |
| --- | --- |
| `make fmt` | Formats Go and the web code |
| `make gate-fast` | Ratchet filtered by your diff, without the slow collectors |
| `make gate-local` | Full ratchet with human output, before a commit |
| `make gate` | Ratchet in CI mode (Markdown) |

The collectors of a side run **in parallel** (vitest, `tsc`, ESLint, jscpd and
`npm audit` are independent; so are the two Go suites): a side costs its slowest
collector, not the sum. In CI the two sides run in separate jobs and a third job
merges their partial reports — `--side=go|web --out=<file>` measures one side,
`--report=<a>,<b>` merges them and prints the single table.

Do not run `make baseline`. Only the maintainer advances the baseline, and only
after a genuine improvement.

## Security

**Secrets never go in the code.** API keys and credentials live in the database
through the Integrations screen (see
[ADR-0023](docs/adr/0023-integration-config-through-the-ui-wins-over-env.md)), or
in an environment variable as a bootstrap fallback. Never commit them and never
hardcode them. If you find a secret in the history, report it privately through
[SECURITY.md](SECURITY.md) so it can be rotated.

## License of your contribution

By contributing, you agree to license your work under the MIT license of the
project.
