# 0042 — A music album is resolved once, and a hopeless file is left alone

Status: accepted (20/nov/2026)

## Context

The queue made the cost of the music scan visible ([ADR-0040](0040-queue-observability.md)): a
scan of a 3 150-file source took **5 h 50 min** and one `saude_biblioteca` run **10 h 28 min**.
Both were interrupted, never finished.

Measured on the production library, the cost is not disk, not ffprobe and not the database:

| | |
|---|---|
| tracks matched by MBID (confidence 100) | 721 |
| tracks matched by textual search (confidence 70) | 2 087 |
| tracks in draft (confidence 0) | 342 |

Every track of the textual path costs **2–4 MusicBrainz calls** (one artist search plus one scoped
import per tied candidate, `maxCandidatosArtista = 3`) and every call is serialized by the
**1 req/s** limit of the provider. The 2 087 tracks alone are ≈ 6 200 requests ≈ **1 h 45 of pure
rate-limit wait**. Worse, that price was paid **per track** even though the expensive object is the
**album**: the first file of a release already fetched the whole tracklist
(`provider.AlbumExterno.Faixas`) to pick one track from it, then threw it away — and the next
fourteen files of the same album asked MusicBrainz the same question again.

Two smaller defects around it:

- **A hopeless file was retried forever.** The incremental short-circuit already skips files with
  unchanged `mtime`/`size`, but a file that ended in draft and then changed (retag, re-download,
  a source whose mtime drifts) went through the whole pipeline again — network plus `fpcalc`
  reading the entire file, for a track the source does not have.
- **A music file could keep the wrong link after changing.** `AtualizarMediaFileConteudoMudou`
  deliberately preserves `entity_id`, which is right for movies (identity comes from the folder)
  and wrong for music, where the link is derived from the **content** (the tags). A draft file that
  finally resolved another track stayed pointing at the old draft.

## Decision

**1. The album is resolved once per scan.** `enrich` exposes an `AlbumImportado` (the album entity,
the artist MBID and the fetched tracklist) and the scan keeps it in `albumCache`, keyed by
`album artist + album` normalized. The first file of an album pays the network;
`ImportarFaixaDesteAlbum` serves every other file of it with **zero requests** — it picks the track
from the in-memory tracklist with the same selection the first file used (`escolherFaixaAlvo`) and
find-or-creates it locally. Same shape as the existing `serieCache`, which already resolved a
series folder once per scan.

**2. A draft attempted inside the window is not attempted again.** A file whose stored track is a
`draft` with `last_external_match_attempt_at` inside **7 days** keeps its draft with no request at
all. Every draft attempt is stamped (the same column the retroactive sweep uses). Unchanged files
never reach this check: the incremental short-circuit above already covers them.

**3. A music file follows its content.** When the freshly resolved track differs from the stored
one, `gravarArquivoMusica` repoints `entity_id` (`AtualizarMediaFileEntityID`) instead of keeping
the stale link.

**4. A failure is remembered too, and only when it is a fact.** Measured after the first
three landed: the folder that ends in draft paid **3 artist candidates x 2-3 requests per FILE**,
because only success was cached. The scan now stores, in the same per-scan cache:

- **"this artist has no album with that name"** (`enrich.ErrAlbumDoArtistaAusente`) — the source
  answered; the next file of the folder does not ask that candidate again;
- **the resolved album, even when the track was not in it** (`enrich.ErrFaixaForaDoAlbum` carries
  the album back) — so the next file is answered by the tracklist in memory, locally;
- and **nothing at all on a network error**. A transient failure is not a fact of the
  (artist, album) pair, so it must stay unanswered and be retried by the next file. That
  distinction is the whole point of the two sentinel errors: without it, a bad minute of
  MusicBrainz would turn a whole folder into drafts.

**Not adopted: caching the acoustic fingerprint.** It looked like the obvious second win and it is
not: an unchanged file never reaches `fpcalc` (the short-circuit), so the cache would only pay for
files whose content **changed** — where recomputing the fingerprint is the correct behaviour
anyway. A migration to save that is not justified by the measurement.

## Consequences

- **A music scan of an already-known library drops from hours to minutes.** The measured target is
  the number of requests per album (1–2) instead of per track (2–4): ≈ 8× fewer calls on the
  textual path, and no call at all for the 14 tracks of an album after the first.
- **Confidence is unchanged.** A track resolved from the album cache gets the same
  `confMusicaTextual` (70) and the same `escolherFaixaAlvo` decision the first file got — it is the
  same source data, read once.
- **The negative cache is per scan, and safe because a fact of an album cannot change between
  two files probed seconds apart.** It is not persisted for the same reason the positive one is
  not: after a restart the first file of a folder pays the attempt again, which is one attempt per
  folder and not one per file.
- **The cache is per scan, not persisted.** A scan that resolves no file of an album does not
  remember anything; the next scan pays the first track again. That is deliberate: an in-memory map
  cannot go stale, and one fetch per album per scan is already the win.
- **The window is a constant, not a setting.** A week for a hopeless file; if it turns out to be
  too long or too short for the owner's library, it becomes an integration setting.
- **The relink fixes a silent hole**: after a retag, a file now moves to the track it actually
  resolves, instead of staying on whatever it pointed at before.

Agenda: Q3 (music cost) — see [docs/00](../00-decision-agenda.md).
