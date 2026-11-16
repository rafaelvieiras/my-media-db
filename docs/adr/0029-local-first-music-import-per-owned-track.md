# ADR-0029 — Local-first music import: only what the owned track brings

- **Status:** Accepted
- **Date:** 2026-08-19
- **Agenda:** [MU1–MU4](../00-decision-agenda.md) · **Spec:** [ADR-0027](0027-music-model-artist-album-track.md), [AGENTS.md](../../AGENTS.md) §4/§6

## Context

ADR-0027 decided, as an accepted trade-off, that matching **one**
local track against MusicBrainz triggers the import of the artist's **entire
discography** (every album, every track, as `cataloged` entities without
ownership). In production that produced a severe distortion: 411 real audio
files on disk (confirmed via `find`) produced **478 artists, 236 albums and
2478 tracks** in the database — only 98 tracks actually owned (`owned=true`).
The owner compared against **LMS** (`epoupon/lms`, a self-hosted Subsonic server
already running in production over the same library, `/userdata/gdrive/Musicas`) and
confirmed the market pattern: LMS derives artist/album/track **straight from the
ID3 tags of the files that exist**, without going off to fetch the whole external
catalog — the result, in LMS's own database (`lms.db`), is **153 artists, 72
albums, 411 tracks**, 1:1 with the physical files.

The current behavior also strains [AGENTS.md](../../AGENTS.md) §4 itself:
"External sources are **optional** and **pulled** enrichment (pull, never
automatic sync)". Importing the whole tree from one matched track
during a passive scan is, in practice, an unsolicited automatic sync —
not an explicit pull by the owner.

## Decision

1. **Artist/album materialization is local-first.** An `entity
   type=artista`/`type=album` is only created when the scanner actually **matches a real
   file** against a track. No full-tree import is triggered from
   automatic matching.
2. **The main artist** of an owned track comes from the artist credit resolved
   by the external source for that specific track (fallback: the local tag
   `AlbumArtist`/`Artist` when the source does not resolve it) — never from the enumeration of
   the artist's whole discography.
3. **An album imports only itself.** When identifying the owned track, the
   corresponding release (album) is imported — not the other albums of the same artist.
   The tracks of the release that the owner does **not** have on disk do not become
   `cataloged` entities automatically and silently: they stay exposed as
   "missing" on the album screen (the same external-item-not-yet-imported surface
   used by movie/collection/series), and the owner decides
   individually to mark them as **wanted** (`status='wanted'`, ADR-0009) —
   never in bulk and never without explicit action.
4. **A secondary/feat artist is not auto-imported.** When the track's artist credit
   diverges from the album artist (collaboration/feat), the system **does not**
   create an entity + `entity_relation(kind=music_artista_feat)` automatically. The
   information stays available (metadata) for the owner to decide to import it
   explicitly — the default behavior is "do not create".
5. **Manual artist import remains available, intact.** `POST
   /api/v1/import` with `type=artista` from a deliberate search
   still brings the whole tree (the same behavior as the original music model
   decision) — that is an **explicit pull** by the owner, not the automatic scan
   trigger. That is: the `MusicaProvider` full-tree capability is not
   removed, it just stops being invoked implicitly by file matching.
6. `MusicaProvider` (MusicBrainz) gains a selective capability — fetching **one**
   specific release/album by MBID without enumerating the artist's discography —
   to serve the new scanner flow (the client already has the internal `montarAlbum`
   doing exactly that; it just needs to be exposed as a granular public capability).

## Consequences

- Fixes the real scale distortion found in production without giving up
  enrichment: the owner can still import a whole discography when he wants,
  through a manual search — it just stops happening behind his back from a single
  file.
- Aligns the behavior with the §4 principle of AGENTS.md (explicit pull, never
  automatic sync) — the original music model decision already cited that principle,
  but the implementation of the automatic trigger contradicted it in practice; this ADR is
  the course correction, not a reversal of the data model (ADR-0027 remains
  fully valid: `entity`/`entity_relation`, kinds, `extension_data
  core.music@1` — none of that changes).
- Reuses the "database larger than the library" pattern (missing-track item
  ↔ episode/unowned item) instead of inventing a new "wanted" mechanism
  for music.
- **Out of scope** (found, not resolved here): series/collection have the
  same "imports the whole tree" pattern (`importarSerie` brings all
  seasons/episodes at once) — an analogous tension, but not touched by this
  ADR; a future decision if the owner wants to extend the same rationale.
- It requires a production data cleanup (removing artist/album/track
  cataloged without ownership that resulted from the previous behavior) — an
  operational action taken **after** deploying the new behavior, so as not to
  reintroduce the problem in a subsequent scan.
- Related decisions: [ADR-0027](0027-music-model-artist-album-track.md)
  (data model, unchanged), [ADR-0009](0009-ownership-and-status-orthogonal-axes.md)
  (orthogonal ownership/status, the basis of the "wanted track"), [AGENTS.md](../../AGENTS.md)
  §4 (optional and pulled enrichment) and §6 (provider independence).
