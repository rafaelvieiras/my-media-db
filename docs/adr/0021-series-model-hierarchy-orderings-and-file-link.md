# ADR-0021 — Series model: hierarchy, orderings as a projection, and N:M file↔episode link

- **Status:** Accepted
- **Date:** 2026-07-11
- **Agenda:** [TV1–TV5](../00-decision-agenda.md) · **Spec:** [04-data-model](../04-data-model.md), [07-enrichment-and-providers](../07-enrichment-and-providers.md)

## Context

Series are the jump in complexity beyond movies (a single item): a
**series → season → episode** hierarchy and the thorny problem of **multiple
season orderings** (aired / DVD / absolute / alternate). The two providers the MVP
uses expose this in different ways — TMDB through *episode groups*, TheTVDB through
*season types* — and one of them (TheTVDB) has content the other removed by policy
(old shorts aired on TV). Before any code, **empirical research** was done on both
APIs (Simpsons 71663/456, Looney Tunes 72514, One Piece 81797) and on media server
conventions (Jellyfin/Kodi/Plex/Sonarr).

The central tension the research resolved: the same episode appears with
**different** `(season, number)` depending on the order, so that pair **is not a
key**; and a streaming file often **packages several shorts** that were separate
episodes on TV, so the file↔episode link is not 1:1.

## Decision

1. **The episode is the canonical fine-grained entity.** One `entity type='episode'`
   per atomic episode of the provider, anchored on the **`episode.id`** (never on the
   number). The numbering of **all** orders lives in `extension_data`, namespace
   `core.tv@1` (`absolute_number` + `numbering: {official:{season,number}, dvd:{…}, …}`),
   mirroring TheTVDB's `episode.seasons[]`.

2. **Ordering (season type) is a projection of numbering, not structure.** Switching
   order **renumbers/regroups the same fine episodes — it never changes
   granularity**. Confirmed empirically through id sets: in Looney Tunes, DVD (413)
   is an exact subset of aired (1195), `dvd-only = 0` (it is **coverage**, not a
   merge); in One Piece the **union of the 4 orders = 1231 = the `official` total**,
   with **zero ids of their own** in the alternate orders. No provider has a concept
   of a "combined episode" (no `combined`/`parentEpisode`/`subEpisodes` field).

3. **A season is an entity only for the series' _default_ ordering** (configurable —
   it can be DVD if the owner's library is DVD). It exists to give a home to its own
   **artwork and override** and to navigation; it links to the series through
   `entity_relation(kind='tv_season', sort_index=number)`. **Alternate orderings do
   not materialize seasons** — they are numbering derived from the episode, grouped
   at read time by the server. (The uniform option — one season-entity per order,
   ~103 for Looney Tunes — was discarded for cost and because alternate orders rarely
   have artwork of their own.)

4. **The file↔episode link is N:M, anchored on the episode id.** New join table
   `media_file_episode(media_file_id, episode_id, ordinal)`. A streaming file that
   packages 3 shorts → **1 file linked to 3 episodes** (`ordinal` preserves the
   sequence); an episode split into parts → **1 episode linked to N files** (the
   direction already inherited from movies, [ADR-0008](0008-identity-and-matching-kodi-jellyfin.md)).
   The "combined" thing is a property of the **file/release**, not of the catalog.
   Anchoring on the **id** (not on the number) is normative: it immunizes against a
   re-scrape "shuffling" links.

5. **Two external sources go through the provider registry (executes R2).** It
   replaces the single provider and the hardcoded `prioridadeFontes=["tmdb"]` with a
   `name→Provider` registry and the global list `[tmdb, tvdb]`
   ([ADR-0002](0002-priority-between-external-sources.md)): per field, the first
   source with a value wins; TheTVDB supplies the orderings and exclusive artwork.
   Series **exclusive to one provider** (Looney Tunes has no TMDB or IMDB `remoteId`)
   use **TheTVDB's primary identity** — the schema accepts an item with no
   counterpart.

## Consequences

- **Changing the order re-projects the numbers without touching the files** — an
  advantage over Kodi/Jellyfin/Plex, which require renaming files when the order
  changes.
- Episode and season are **first-class entities** → they inherit the resolver, the
  per-language override, FTS search, ownership through `media_file` and artwork,
  **without touching the resolver engine** (fields through `extension_data`; the tree
  is composed on the server, as `montarMembros` already does for a collection). Cost:
  ~N entities per series (cheap).
- **`entity_relation` gains `sort_index`** (stable ordering of children; today it
  only orders by `id`). **New generated column `external_id_tvdb`** (dedup by TheTVDB
  id), mirroring `external_id_tmdb/_imdb`. `entity.type` does **not** change (no
  CHECK).
- **`extension_data core.tv@1`** demands the JSON Schema validation machinery
  (promised in [04](../04-data-model.md), still nonexistent) — or accepting
  unvalidated data at first and hardening later.
- Alternate orderings **without an entity**: navigating "DVD S23" is **grouping at
  read time** (server), not relational. Acceptable; promoting an alternate season to
  an entity (for its own override/artwork) stays as an **evolution** if the need
  arises.
- Multilanguage: episodes use the same per-language mechanism
  ([ADR-0019](0019-per-language-resolution-fallback-and-provenance.md)); TheTVDB uses
  the code `por` (3 letters, no pt-BR/pt-PT) and **requires fallback** (many episodes
  have no translation).
- Related decisions: [ADR-0001](0001-resolver-partition-by-origin.md)
  (generic engine), [ADR-0002](0002-priority-between-external-sources.md) (source
  priority), [ADR-0005](0005-uniform-composition-through-extension-data.md) (`extension_data`),
  [ADR-0008](0008-identity-and-matching-kodi-jellyfin.md) (Kodi/Jellyfin matching),
  [ADR-0011](0011-external-ids-authoritative-media-external-cache.md) (external
  identity).
