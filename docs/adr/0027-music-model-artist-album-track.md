# ADR-0027 — Music model: artist→album→track hierarchy via MusicBrainz

- **Status:** Accepted
- **Date:** 2026-08-19
- **Agenda:** [MU1–MU4](../00-decision-agenda.md) · **Spec:** [04-data-model](../04-data-model.md), [07-enrichment-and-providers](../07-enrichment-and-providers.md), [12-roadmap](../12-roadmap.md)

## Context

Music is the next planned vertical ([12-roadmap](../12-roadmap.md), Phase 5+) and the
real test of the promise "new vertical with no core migration"
([AGENTS.md](../../AGENTS.md) §3). Unlike series (rigid hierarchy with
multiple competing orderings, solved in [ADR-0021](0021-series-model-hierarchy-orderings-and-file-link.md)),
music has its own thorny point: the artist of a **track** may diverge
from the artist of the **album** (feat./collaboration), compilations/soundtracks use
"Various Artists" as a non-artistic grouper, and the identity of an album
depends on a set of fields — name + artist + total discs + label +
barcode — where varying any one of them **fragments the album** in real
players. This last point is not a hypothesis: it is an empirical finding documented in
the code of the owner's personal music organization scripts
(`~/ai-workspace/scripts/music-organizer`), which already solve this in production
through tag and folder conventions, cross-checked with the official
Jellyfin/Navidrome/Koel/Kodi documentation.

Research: the series model (ADR-0021) serves as a direct precedent — a canonical
fine-grained entity anchored on an external id, an intermediary entity only for
artwork/override, numbering in `extension_data`. The personal scripts served
as an empirical source of real conventions (folder structure, tags used,
compilation/multi-disc handling). **MusicBrainz + Cover Art Archive**
was chosen as the base provider — already listed in the vision of [doc 07](../07-enrichment-and-providers.md),
requiring no API key, a role equivalent to what TheTVDB plays today for
series.

## Decision

1. **The track is the canonical fine-grained entity.** `entity type='faixa'` per
   recording, anchored on the **recording MBID** (never on number/position).
   Numbering (`track_number`, `disc_number`, `disc_total`) lives in
   `extension_data`, namespace **`core.music@1`**, mirroring the episode numbering
   of `core.tv@1` ([ADR-0021](0021-series-model-hierarchy-orderings-and-file-link.md) item 1).

2. **Album is an entity only to give a home to artwork/override and grouping**,
   linked to the track through `entity_relation(kind='music_album', sort_index=track_number)`.
   Same reasoning as the season: it is not mandatory structure, it is where the owner
   edits cover/title/override of the album as a whole.

3. **Artist is a first-class entity** — it does **not** reuse `person`/`credit`
   (MD6): a band is not a person, it needs its own artwork/bio as a
   catalogable item, like movie/series. "Various Artists" (grouper of
   compilation/soundtrack) is a regular artist, with the **fixed canonical MBID**
   that MusicBrainz itself already uses (`89ad4ac3-39f7-470e-963a-56509c546377`) —
   no special case in the code/schema.

4. **Artist↔track/album attribution via `entity_relation`, with no new join
   table.** `kind='music_artista_principal'` (billing/album) vs.
   `kind='music_artista_feat'` (one-off collaboration on the track) distinguishes the
   album artist from the actual track artist when they diverge — the same generic
   pattern of [AGENTS.md](../../AGENTS.md) §3 ("never per-type columns or join
   tables"). `sort_index` gives the billing order.

5. **File↔track link is 1:1** (`media_file.entity_id` directly, the same
   pattern already used by movies) — **not** N:M like episodes. It covers the dominant
   real case (rip = 1 file = 1 track, which the owner's own personal scripts
   produce). Evolution to N:M (`media_file_faixa`, medley/DJ-mix/
   multi-quality) is left for when a real need appears, without breaking the
   model — the same open-evolution reasoning of [03-architecture](../03-architecture.md#concurrency-and-persistence)
   ("a single writer can become a queue").

6. **MusicBrainz + Cover Art Archive is the MVP base provider**, with no API
   key. A new optional tree capability (mold of `SerieProvider`) returns
   artist→albums→tracks. Primary identity by **MBID** — artist, album
   (`release`) and track (`recording`) share the same id scheme from the source.
   **ISRC** goes into `extension_data` as an auxiliary track identifier
   (matching/dedup in application code); it does **not** become a generated
   column in the MVP — evolution if a use case justifies a dedicated index.

7. **Genre lives at the album/artist level** in `extension_data` — not
   duplicated per track. It mirrors the convention observed in the personal scripts
   (grouping by album avoids reconciling genre per track coming from different
   sources).

## Consequences

- Confirms the promise of [AGENTS.md](../../AGENTS.md) §3: the whole vertical
  comes in with **zero new table**. Generic `entity`/`entity_relation` are already
  enough for artist/album/track and every attribution between them. The only schema
  touch: a generated column `external_id_mbid`
  (`json_extract(external_ids_json, '$.mbid')`) + index, mirroring
  `external_id_tmdb`/`_imdb`/`_tvdb`, for fast lookup by MusicBrainz id.
- `extension_data core.music@1` follows the **same real state** as
  `core.tv@1`: no formal per-JSON-Schema validation machinery (still
  nonexistent, confirmed in the code — `internal/tvext`); reading goes through a
  dedicated parser (`internal/musicext`, mold of `internal/tvext`), not
  declarative validation. Accepting that now is the same trade-off already taken
  by the series precedent.
- Artist/album/track inherit resolver, per-language override, FTS search, ownership
  through `media_file` and artwork **without touching the engine** — the same gain
  that ADR-0021 recorded for series.
- A single provider (MusicBrainz) already covers the whole vertical on its own
  (provider independence, [AGENTS.md](../../AGENTS.md) §6). Spotify/
  Deezer/Discogs/Last.fm — already used by the owner's personal scripts for
  identification and download, but outside the project's self-hosted/keyless
  spirit — remain as **future** cross-enrichment, never a base
  dependency.
- 1:1 file↔track is a deliberate scope bet, not a decision
  closed forever: if medley/DJ-mix becomes a real need, the migration
  to N:M follows the precedent already opened by `media_file_episode`
  ([ADR-0021](0021-series-model-hierarchy-orderings-and-file-link.md) item 4).
- Related decisions: [ADR-0021](0021-series-model-hierarchy-orderings-and-file-link.md)
  (series model, direct precedent), [ADR-0005](0005-uniform-composition-through-extension-data.md)
  (uniform composition of `extension_data`), [ADR-0011](0011-external-ids-authoritative-media-external-cache.md)
  (authoritative external identity), [AGENTS.md](../../AGENTS.md) §3 (universal
  and extensible without touching the core) and §6 (provider independence).
