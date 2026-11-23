# Architecture Decision Records (ADRs)

Every **foundational** decision already closed becomes an **ADR** — a short,
immutable and dated file. It is the memory of *why* the project is the way it is: it
replaces the flat list of "open decisions" with a traceable record, one file per decision.

## How it works (lightweight format)

- One ADR per decision, numbered in sequence: `NNNN-slug.md`.
- It follows the [template](template.md): **Context · Decision · Consequences**, plus
  status, date and the **agenda ID** ([`docs/00`](../00-decision-agenda.md)).
- **An ADR is immutable.** A decision is not rewritten: when a new one supersedes it,
  a **new ADR** is created that marks it as `Superseded by ADR-XXXX`, and vice-versa.
- Flow: a decision is born in the [agenda](../00-decision-agenda.md) (`open` →
  `documented`), becomes an ADR, and is promoted to the spec docs (`03`, `04`, …).

## Index

| ADR | Decision | Agenda | Status |
|---|---|---|---|
| [0001](0001-resolver-partition-by-origin.md) | Resolver: fields partitioned by origin | R1 | Accepted |
| [0002](0002-priority-between-external-sources.md) | Priority between external sources (global list) | R2 | Accepted |
| [0003](0003-list-override-by-operations.md) | List override by merging operations | R3 | Accepted |
| [0004](0004-override-as-sparse-json-blob.md) | Override as a sparse JSON blob | R4 | Accepted |
| [0005](0005-uniform-composition-through-extension-data.md) | Uniform composition (includes `extension_data`) | R5 | Accepted |
| [0006](0006-key-presence-signals-override.md) | Key presence signals override | R6 | Accepted |
| [0007](0007-technical-facts-are-overridable.md) | Technical facts are overridable | R7 | Accepted |
| [0008](0008-identity-and-matching-kodi-jellyfin.md) | Identity and matching by the Kodi/Jellyfin convention | MD1 | Accepted |
| [0009](0009-ownership-and-status-orthogonal-axes.md) | Ownership and `status` as orthogonal axes | MD2 | Accepted |
| [0010](0010-sqlite-wal-concurrency-single-writer.md) | SQLite concurrency: WAL + single writer | MD3 | Accepted |
| [0011](0011-external-ids-authoritative-media-external-cache.md) | `external_ids` authoritative, `media_external` cache | MD4 | Accepted |
| [0012](0012-non-destructive-lifecycle.md) | Non-destructive lifecycle | MD5 | Accepted |
| [0013](0013-relation-override-by-operations.md) | Relation override (cast/crew) by operations | MD6 | Accepted |
| [0014](0014-implementation-stack.md) | Implementation stack (chi, modernc, sqlc, ffprobe, OpenAPI) | doc 11 (1–5) | Accepted |
| [0015](0015-tmdb-api-passthrough.md) | `/api/tmdb`: passthrough with cache, never 404 | doc 11 (6) | Accepted |
| [0016](0016-unified-quality-gate-ratchet.md) | Quality gate: unified ratchet Go + TS | QG1 | Accepted |
| [0017](0017-embedded-goose-migrations.md) | SQLite migrations: embedded goose | Phase 2 | Accepted |
| [0018](0018-multilanguage-model-language-as-a-layer-dimension.md) | Multi-language model: language as a layer dimension | L1 | Accepted |
| [0019](0019-per-language-resolution-fallback-and-provenance.md) | Per-language resolution: fallback chain + provenance | L2 | Accepted |
| [0020](0020-translation-of-genres-and-keywords-tags.md) | Tag translation (localized genres + keywords) | L3 | Accepted |
| [0021](0021-series-model-hierarchy-orderings-and-file-link.md) | Series: hierarchy, orderings as a projection, N:M file↔episode link | TV1–TV5 | Accepted |
| [0022](0022-consumption-history-generic-events-and-tracker-provider.md) | Consumption history: generic events + `TrackerProvider` capability | H1–H5 | Accepted |
| [0023](0023-integration-config-through-the-ui-wins-over-env.md) | Integrations configurable through the UI: database takes precedence over environment variable | I1–I4 | Accepted |
| [0024](0024-external-cache-survives-without-a-live-provider.md) | External cache survives without a live provider (pure parse ≠ live search) | PR1 | Accepted |
| [0025](0025-trailer-and-theme-through-ytdlp-and-themerrdb.md) | Trailer and theme music: yt-dlp accepted (dynamic binary + custom path) + ThemerrDB | TR1–TR2 | Accepted |
| [0026](0026-trailer-and-theme-job-and-automatic-scan-trigger.md) | Trailer/theme: periodic job + automatic trigger on scan (opt-in) | TR3 | Accepted |
| [0027](0027-music-model-artist-album-track.md) | Music model: artist→album→track hierarchy via MusicBrainz | MU1–MU4 | Accepted |
| [0028](0028-unified-execution-queue-scan-and-jobs.md) | Unified execution queue for scan and jobs | — | Accepted |
| [0029](0029-local-first-music-import-per-owned-track.md) | Local-first music import: only what the owned track brings | MU1–MU4 | Accepted |
| [0030](0030-generic-top-types-library-and-search.md) | Top-level types: generic library, search and navigation driven by schema | see [12-roadmap](../12-roadmap.md) | Accepted |
| [0031](0031-post-scan-per-library.md) | Post-scan per library: the scan closes its own cycle (and no enrichment inside the transaction) | SCAN-POS | Accepted |
| [0032](0032-ui-i18n-dependency-free-catalogs.md) | UI internationalization: dependency-free typed catalogs, English as the source | — | Accepted |
| [0033](0033-alternate-and-owner-orderings.md) | Alternate and owner orderings: provider groups, plus a numbering the owner declares | EP-ORD | Accepted |
| [0034](0034-reversible-unattended-upgrades.md) | An unattended upgrade has to be reversible: pre-migration copy, downgrade guard, PUID/PGID | — | Accepted |
| [0035](0035-suggested-match-hint.md) | A cheaper variant of a misspelled name is a suggestion the owner accepts, never an automatic fix | — | Accepted |
| [0036](0036-gate-unknown-is-not-fine.md) | A metric that could not be measured blocks the gate | QG1 | Accepted |
| [0037](0037-search-term-from-the-catalog-too.md) | A movie is searched by its catalog title, not only by the release name | MD1 | Accepted |
| [0038](0038-match-alerts-beyond-the-typo.md) | A match that succeeds can still be useless: warn when the tree is empty or does not fit | — | Accepted |
| [0039](0039-release-name-is-not-a-title.md) | A release name is not a title: cut at the release section, drop a trailing genre tag, and give movies the "did you mean this?" hint | — | Accepted |
| [0040](0040-queue-observability.md) | The queue is observable: per-item progress, a bounded trail, a queue view and a derived duration | Q1 | Accepted |
| [0041](0041-parallelism-is-intra-run-and-per-provider.md) | Parallelism lives inside a run and per provider (behind a process-wide limiter), never at the queue level | Q2 | Accepted |
| [0042](0042-a-music-album-is-resolved-once.md) | A music album is resolved once per scan, a draft attempted inside the window is left alone, and a music file follows its content | Q3 | Accepted |
| [0043](0043-music-artwork-lands-in-the-folder.md) | Music artwork lands in the folder under the library's own names (folder.jpg, cdart.png…), and a download without a destination is not success | MW1 | Accepted |
| [0044](0044-one-nfo-per-file.md) | One NFO per video file, one `<episodedetails>` block per episode, numbering from the tree | NF1 | Accepted |
| [0045](0045-an-absolute-number-is-resolved-through-the-series-ordering.md) | An absolute number with no `absolute_number` from the provider is resolved through the series' orderings, with the folder's season as the anchor | #105 | Accepted |
| [0046](0046-series-identity-is-title-year-and-shape.md) | A series is identified by title in any language, the folder's year and the shape of the tree | #112 | Accepted |
| [0047](0047-a-folder-is-a-shape.md) | A folder is a shape: an unmatched series still gets its provisional tree, a specials folder is season 0, and a later identity adopts the provisional episode | #136 | Accepted |
| [0048](0048-review-requires-a-file.md) | The review queue requires a file: a hole in the collection is not a decision, and a provisional episode is the series' card | #136 | Accepted |
