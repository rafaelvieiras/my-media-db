# ADR-0022 — Consumption history: generic event table + `TrackerProvider` capability

- **Status:** Accepted
- **Date:** 2026-08-10
- **Agenda:** [H1–H5](../00-decision-agenda.md) · **Spec:** [04-data-model](../04-data-model.md), [07-enrichment-and-providers](../07-enrichment-and-providers.md)

## Context

The owner asked for a way to track **consumption** (watched/listened/read/played)
that is agnostic to both media type and originating client, citing the
"scrobbling" ecosystem (Trakt.tv, Last.fm, ListenBrainz, Open Scrobbler) and
vertical-specific trackers (scrob.app, sofa.watch, Librarium, GamingGaiden,
RetroAchievements, among others). Market research (5 tracks: movie/series, music,
books, games, plus a survey of the current schema) found a convergent pattern
across **every** self-hosted/relevant competitor, regardless of vertical:

1. **Identity through an already-known external ID**, never a catalog schema of
   its own — Trakt/Simkl use `ids.{tmdb,imdb,tvdb}`; ListenBrainz uses MBID;
   BookWyrm cascades OpenLibrary→Inventaire/Wikidata; RetroAchievements uses a ROM
   hash; Playnite crosses AppID/GOG-ID/IGDB-ID.
2. **History is a sequence of events, not a single mutable state** —
   StoryGraph/BookWyrm store "progress updates" over time; Hardcover treats a
   re-read as N separate reads; Trakt/Simkl scrobble `start/pause/stop` with
   `progress` 0–100 and only close "watched" when `stop` happens with
   `progress ≥ 80%`; RetroAchievements stores `unlock{user, timestamp, mode}` per
   achievement.
3. **N heterogeneous clients → 1 canonical schema, via plugin/capability** —
   Playnite/SuccessStory aggregates Steam+GOG+Epic+RetroAchievements+manual into a
   single "achievement" schema; Maloja and Koito speak Last.fm **and**
   ListenBrainz at the same time to inherit clients for free; scrob.app
   imports/exports from Trakt/Simkl/MDBList simultaneously.

The only precedent today in the project (migration 0019) is the opposite of that:
six loose columns in `entity` (`watched`, `play_count`, `user_rating`,
`last_played_at`, `resume_position_seconds`, `resume_total_seconds`), filled
**passively** by a one-off re-read of the `.nfo` already on disk, with no history
(1 value, not N events), no real-time entry point (webhook) and no write API at
all. It was a justified exception (mirror the `.nfo`, with no equivalent in an
external provider) — it is not the pattern to extend into a real tracking system.

## Decision

1. **History is a generic event table, not per-vertical columns.**
   New child table of `entity`, following the same mold as `asset`/`media_file`
   (`id TEXT PRIMARY KEY`, `entity_id TEXT NOT NULL REFERENCES entity(id) ON
   DELETE CASCADE`, `created_at`), **append-only**:
   `historico_evento(id, entity_id, kind, status, progress_percent,
   position_seconds, occurred_at, source, payload_json, created_at)`.
   `kind` (`'assistir'|'ouvir'|'ler'|'jogar'|…`), `status`
   (`'started'|'paused'|'stopped'|'completed'|'dropped'|…`) and `source`
   (`'local'|'manual'|'jellyfin-webhook'|'plex-webhook'|'emby-webhook'|…`) are
   **free TEXT, with no closed CHECK** — principle 3 of `AGENTS.md` (never a
   column/table per type). `payload_json` stores extras namespaced by vertical
   (episode, page, track) without requiring a new migration when a future vertical
   needs a specific field.

2. **A "milestone"/achievement is CATALOG + event — it never becomes an event on
   its own.** Review of the initial decision (which treated an achievement only as
   `kind='conquista'` inside `historico_evento`): the definition of an achievement
   (title, description, icon, points) is **catalog** data — it must exist and be
   navigable even BEFORE the owner unlocks it (that is how RetroAchievements and
   Steam expose it: the whole list of possible achievements for a game, with the
   ones not yet obtained visible). An event does not model that. Decision:
   - The definition becomes an **`entity` of its own** (`type='conquista'`), linked
     to the parent item (game, album, whatever) through the **generic
     `entity_relation` that already exists** (`kind='conquista'`, `parent_id`=parent
     item, `child_id`=achievement) — the SAME mechanism that already links
     series→season→episode and collection→movie, with no new table. Title/description
     go through the normal resolver (`override ⟵ external`); the icon is a regular
     `asset` (same poster/fanart pipeline, with no new mandatory `kind` beyond the
     already free one).
   - The **unlock** (when the owner reached that) is what becomes a
     `historico_evento` — with `entity_id` pointing at the **achievement** (not at
     the parent item), `kind='conquista'`, `status='unlocked'`. Reuses the schema of
     item 1 with no change whatsoever.
   - Unlock-condition data specific to the source (e.g. the memory-address formula
     RetroAchievements uses to know something happened) is **opaque to the core** —
     stored namespaced in the achievement's own `extension_data`, never interpreted
     by MyMediaDB; only whoever knows that source's format (an external bridge
     running next to the emulator, for instance) interprets it.
   - Importing the achievement catalog from an external source is the job of a new
     optional capability `MarcoProvider` (same mold as `SimilarProvider`: given the
     parent item's external id, it returns the list of achievements to import as
     child entities). **Outside this delivery** — designed, with no concrete
     provider.
   - No new table in any of the points above: `entity`, `entity_relation`, `asset`,
     `extension_data` and `historico_evento` already suffice — it reinforces item 1
     instead of contradicting it.

3. **Identity for verticals based on file hashes (e.g. ROM) reuses
   `media_file.content_hash`.** No new column: the scanner already computes a
   content hash per file (today used to detect a changed file); for a
   RetroAchievements-style vertical, that same hash is the game's "identity
   document". A fan-translation/hack ROM (hash different from the official one) is
   modeled as **one more `media_file` of the same `entity`** — the already
   normative pattern "1 work = N files/editions"
   ([ADR-0008](0008-identity-and-matching-kodi-jellyfin.md)/MD1), with no new
   concept. A hash no provider recognizes degrades gracefully (the item stays
   cataloged, just without automatic matching for that source — principle 6); the
   fix is the same one already used for movie/series matching (MD1: "the owner's
   correction is authoritative" — the owner links the file/hash to the right item by
   hand). If a patch's memory offset requires an unlock condition different from the
   official one just for that hash, the owner overrides **only that field** through
   the **already existing and universal** override mechanism (`media_override`,
   [R6](../00-decision-agenda.md) — key presence = active override) — it is not a
   new mechanism, it is the project's universal override applied to one more field.

4. **"Current state" is a projection derived from the history, not a second mutable
   source of truth.** What `watched`/`play_count` try to represent in `entity` today
   becomes computable from the last event per `(entity_id, kind)` — it avoids two
   diverging sources of truth.

5. **Migration 0019 is not migrated or unified now.** It remains the one-off mirror
   of the `.nfo` (passive read, best-effort, an already documented exception). Known
   and accepted tension: the two mechanisms coexist with no automatic
   synchronization between them in this delivery — unifying them stays as an
   evolution, only if practice shows a real need (e.g. a job that derives
   `watched`/`play_count` from the history without exporting anything back to the
   `.nfo`).

6. **Ingestion (input) and synchronization (output) are distinct mechanisms:**
   - **Input through webhook adapters**, with a registry of their own (it is not a
     `provider.Provider` capability — it does not fetch/map a catalog, it only
     receives and normalizes a player payload). Each media server
     (Jellyfin/Plex/Emby) gets an adapter that translates its proprietary payload
     into a `historico_evento`. First delivery: Jellyfin only (the simplest and
     publicly documented webhook; Plex requires Plex Pass).
   - **Output/synchronization through a new optional capability `TrackerProvider`**
     in `internal/provider/provider.go`, following EXACTLY the mold of
     `SimilarProvider`/`ArteProvider` (small interface, type-assert by the caller, it
     degrades silently when absent). It allows importing/exporting history to/from an
     external service (Trakt as the first candidate) when an item is already matched
     by `external_ids`. **Outside this delivery** — the capability is designed, with
     no concrete implementation yet (no provider implements it in this phase).

7. **Interoperability vocabulary aligned with what has already converged in the
   market**, to minimize friction for a future `TrackerProvider`: identity through
   `ids.{tmdb,imdb,tvdb}` (already `entity.external_ids_json`),
   `progress_percent` 0–100, and the same **80%** threshold that Trakt and Simkl use
   to close an event as `completed`.

8. **Slicing the epic:** generic schema + minimal API first; the real-time player
   webhook afterwards, covering only movies/series in the first concrete
   integration; the state/history UI in the item detail last. A concrete
   `TrackerProvider` (Trakt) and the music/book/game verticals reuse the SAME schema
   in future deliveries, with no new migration.

## Consequences

- You gain real history (N events per item, progress over time, multiple sessions)
  without touching migration 0019 or the `.nfo` export.
- Any future CONTINUOUS CONSUMPTION vertical (music/book/game) comes in using the
  SAME table — a new `kind`, zero migration — consistent with principle 3
  (universal and extensible without touching the core).
- "Milestone"/achievement (navigable catalog + unlock) also does not ask for a new
  table, even though it is structurally richer than a simple event —
  `entity`/`entity_relation`/`asset` already cover the definition, `historico_evento`
  already covers the unlock. Proof that the generic model holds up to a more complex
  case (RetroAchievements/Steam) without having to grow.
- Identity through file hash (ROM) reuses `media_file.content_hash` already computed
  by the scanner — zero new columns; hash variants (fan-translation) and condition
  fix per hash reuse already existing mechanisms (MD1 manual match correction + R6
  universal override), with no new game-specific mechanism.
- The Jellyfin webhook is the lowest effort with the greatest leverage: it works
  without the owner needing an account on any external service.
- `TrackerProvider` is ready as a real extension point (not decorative) from now on,
  even without a concrete implementation — the day Trakt/ListenBrainz are
  implemented, they plug in without changing the caller (the same pattern already
  paid for by `ColecaoProvider`/`ArteProvider`/`SimilarProvider`).
- Accepted cost: two "playback state" mechanisms coexist for now (migration 0019
  passive through NFO + `historico_evento` active through webhook/API) — there is no
  synchronization between them today; revisit if that causes practical confusion
  visible to the owner.
- Related decisions: [ADR-0011](0011-external-ids-authoritative-media-external-cache.md)
  (authoritative external identity, the base for `TrackerProvider`),
  [ADR-0008](0008-identity-and-matching-kodi-jellyfin.md) (1 work = N
  files/editions; match correction is the owner's authority — the basis for handling
  ROM/fan-translation hashes), [ADR-0005](0005-uniform-composition-through-extension-data.md)
  (namespaced `extension_data`, the basis for the opaque unlock-condition data),
  [principles 3 and 6 of `AGENTS.md`](../../AGENTS.md)
  (extensibility without touching the core; provider independence + optional
  capability).
