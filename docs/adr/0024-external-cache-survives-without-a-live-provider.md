# ADR-0024 — External cache survives without a live provider (pure parse ≠ live fetch)

- **Status:** Accepted
- **Date:** 2026-08-12
- **Agenda:** new item PR1 (see [00](../00-decision-agenda.md)) · **Spec:** [03](../03-architecture.md), [04](../04-data-model.md), [07](../07-enrichment-and-providers.md)

## Context

In the production migration of 12/08/2026, the new environment came up with the 4
provider credentials (TMDB/fanart.tv/TVDB) empty. Observed result: **every
already-cataloged item lost its synopsis, cast, crew, tags and
similar/suggestions** — not only for new items (expected, without a key you cannot
search), but for items that **already had that payload cached in
`media_external`/`external_detail` for months**, copied intact in the migration
(confirmed by an identical hash of the `.sqlite` file).

An investigation of the code (not speculation) found the exact cause:

- `cmd/mymediadb/main.go` only builds and registers a `provider.Provider`
  (`tmdb.New`, `tvdb.New`, `fanarttv.New`) when the matching credential is not
  empty. Without a credential, the provider **never enters `provider.Registry`** —
  it is not a partial degradation, it is the total absence of the object.
- `internal/server/entities.go:montarExterno` returns `nil` for the whole thing
  when `s.providers.Vazio()`; and `camadasDaFonte` returns `nil` **per source** when
  `s.providers.Get(fonte)` fails — **even if `media_external` has rows for that
  source with a present and valid `payload_json`**. The reason `Get` failed does not
  matter to the code: key never configured, key revoked, provider discontinued — all
  collapse into the same "no provider = no data", including for data that is
  already stored locally.
- The raw→canonical mapping (`prov.MapearDetalhe`, `prov.(SimilarProvider).Similares`,
  the extractors in `internal/richfields`) is a **pure function**: it takes the
  already saved `payload_json` and returns structured fields, **with no network call
  and no credential**. Today that function is only reachable through the same
  `provider.Registry` that also holds the live search capability
  (`BuscarDetalhe`, which does need a credential) — the two capabilities,
  semantically independent, share a single binary gate.
- A second spot with the same defect, found in the same investigation:
  `internal/externaldetail/externaldetail.go:PayloadFresco` — when the cache
  (`external_detail`) expires (`TTLPadrao = 24h`) and the re-fetch from the provider
  fails (network down, key revoked, rate limit), the function returns the **raw
  error** and throws away the old payload still sitting in the same table row,
  instead of serving the stale-but-present one and merely signaling that it could
  not refresh.

This contradicts two principles already established in the project: `AGENTS.md` §6
("provider independence: the media survives ANY registered provider") and
[ADR-0011](0011-external-ids-authoritative-media-external-cache.md) (`media_external`
is already declared a "payload cache" — implicitly durable, not conditioned on the
current network session). And it is the opposite of the project's founding goal: a
cataloged item must stay **readable forever in 100% offline mode**, even if the
provider that enriched it one day ceases to exist, changes its API, or the owner
decides not to keep that key anymore.

## Decision

**Separate the two capabilities that are fused today in `provider.Registry`:**

1. **Live search capability** (`BuscarDetalhe`/`BuscarSerieArvore`/etc.) — it still
   requires a credential. Without it, the provider does not enter the search
   registry; current behavior preserved (you cannot search for what was never
   searched, and that is fine).
2. **Read/parse capability for an already cached payload** (`MapearDetalhe`,
   `Similares`, the `internal/richfields` extractors, `PessoaProvider` over a saved
   payload) — **it becomes available for ANY `source` the binary knows how to
   interpret, regardless of the credential configured right now**. A "parser-only"
   provider is always registered at birth (`tmdb`, `tvdb`, `fanarttv` are codecs the
   binary knows, not secrets) — only live search depends on a key.
3. `montarExterno`/`camadasDaFonte` (`internal/server/entities.go`) start consulting
   the parse capability (always present), no longer the live search registry — a row
   of `media_external` with a valid payload **always** composes the resolver's
   external layer, key configured or not.
4. `externaldetail.PayloadFresco` (`internal/externaldetail/externaldetail.go`):
   when the re-fetch fails (TTL expired + provider error), **serve the existing stale
   payload** instead of propagating the error — explicit best-effort, the same spirit
   of `doCacheFresco` ("cache is an optimization, not a barrier") applied to the error
   path as well, not only to the happy path. The caller may optionally know that the
   data is stale (an extra field in the response, outside the scope of this ADR to
   fix the exact format), but it **never loses the data because of provider
   unavailability**.
5. Direct operational consequence: `s.providers.Vazio()` stops being a synonym for
   "no external data at all" — it now means only "no new search possible". An
   instance with NO credential configured at all, restored from a backup of an
   instance that had credentials in the past, must render 100% of what was already
   cataloged.

## Consequences

- A cataloged item survives the retirement of the provider that enriched it (TMDB
  discontinuing the API, key expiring, owner not renewing) — a central condition of
  the project's digital survivalist principle, not just a UX detail.
- Restoring a backup in a new environment (like the migration that exposed this) never
  again loses cast/synopsis/similar for lack of reconfiguring credentials on the first
  boot — the Integrations UI ([ADR-0023](0023-integration-config-through-the-ui-wins-over-env.md))
  remains the only way to enable NEW search, but it stops being a prerequisite for
  reading what already exists.
- Cost: `provider.Registry` (or a sibling registry) must explicitly distinguish the
  two capabilities by type/interface, instead of a single `name→Provider` map.
  "Parser-only" providers (without a credential) must keep failing clearly in
  `BuscarDetalhe` (do not silence the absence of search — only the absence of READING
  what already exists is the bug).
- Outside the scope of this ADR: deciding the exact format of "signaling stale data"
  in the API (item 4) — that stays as an implementation detail of whatever
  materializes this decision.
- Sibling decision: [ADR-0011](0011-external-ids-authoritative-media-external-cache.md)
  (what `media_external` is) and [ADR-0023](0023-integration-config-through-the-ui-wins-over-env.md)
  (how the credential reaches the provider) remain valid unchanged — this ADR only
  fixes WHO CAN READ the cache that the other two already describe.
