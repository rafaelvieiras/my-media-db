# 0043 — Music artwork lands in the folder, under the library's own names

Status: accepted (20/nov/2026)

## Context

The `midias_faltantes` job printed the same summary on every run: **301 artes materializadas**,
and the count came back identical the next day. The number was not progress — it was a loop.

Measured on the production library after a full run (issue #79):

| | |
|---|---|
| artworks reported as materialized | 301 |
| images that reached the library | **0** |
| albums with an unmaterialized poster that DO own a present track | 300 |
| artists in the same situation | 47 |

The cause is the destination. `resolverDestino` → `localmedia.DiretorioDestino` only accepts a
`media_file` whose `entity_id` **is the entity being materialized**, and a music file points at
the **track** ([ADR-0027](0027-music-vertical.md)): an album or an artist never has one. With no
folder, `materializarAsset` answered `errSemDestino`, and `materializarAssetOuCache` turned that
into `true` — "materialized" — because the binary was already in the internal cache and the
card's `has_poster` only needs the row. The cover/picture was therefore downloaded again on
every run (301 network fetches), written to a new cache file with a new UUID, and thrown away.

That decision was documented and deliberate at the time ("materializing music cover/picture in
the real folder is out of scope here"). It stopped being acceptable when the job's own summary
became the way we measure it: a run that reports 301 artworks and writes none is worse than a
run that reports zero.

## Decision

**Music entities get a real destination, and its names are the ones the library already uses.**

1. `pastaDeMusica` resolves the folder of an album (the directory of a descendant track) and of
   an artist (its parent) — the same rule the local-artwork discovery has always used, now shared
   with the write side. It answers "no destination" (never an error) when the entity has no owned
   track, when the folder is not on disk right now, or when the artist's would-be folder is a
   library root: going up one level from a track that sits directly in the root would drop the
   artist picture in the root of the library, which is worse than not writing it.
2. `resolverDestino` returns that folder for `album`/`artist`, with `pastaExclusiva = true` and no
   video base name — so the artwork name never takes the `<video>-poster` shape.
3. `NomeArquivoArteMusica` writes the **bare** names, and they are the library's own words:

   | kind (aggregator vocabulary) | file |
   | --- | --- |
   | `poster` | `folder.jpg` |
   | `fanart` | `backdrop.jpg` |
   | `banner` | `banner.jpg` |
   | `logo` / `clearlogo` | `logo.png` |
   | `disc` / `discart` | `cdart.png` |
   | `thumb` | `thumb.jpg` |

   Writing `poster.jpg` next to an existing `folder.jpg` would duplicate the same image under two
   names instead of converging with what the owner has (190 `folder.jpg` + 196 `cdart.png` +
   43 `backdrop.jpg` + 42 `logo.png` + 38 `banner.jpg` in `Music/`).
4. **The destination is resolved BEFORE the download, and "no destination" is not success.** An
   entity without a folder is a no-op: no fetch, no cache file, no `artes_baixadas` entry.
5. An artist's `thumb` is not materialized: it *is* the picture the alias already wrote as the
   poster, and writing both would put the same image in the folder twice.
6. Music has **no NFO** in this change. `MontarNFO` covers movie/tv/episode only; the folder is
   now resolvable, so the guards that keep the job from writing a nameless `.nfo` next to the
   tracks are explicit (`gravarNFOSeFaltante`, `writeEntityNfo`).

## Consequences

- The steady state of a music entity is the existing reconfirm path: the winner is local and
  already in the library, the canonical name is the one on disk, nothing is downloaded and nothing
  is counted. `301 artes` per run becomes `0`.
- The first run after the upgrade writes ~347 files into `Music/` (one per album/artist with a
  cached cover) — that is the backlog the job was pretending to do all along.
- The cache stops growing by ~301 orphan binaries per run.
- A music folder that the app cannot reach (mount offline) is a clean no-op, not a false success.

## Alternatives rejected

- **Keep the cache-only success.** It is what produced a number nobody could trust; the queue is
  the way the owner reads a run ([ADR-0040](0040-queue-observability.md)), so its counters must
  mean files.
- **Write `poster.jpg` in music folders.** Correct by the video convention, wrong for this
  library: the folders already hold `folder.jpg`, and the scanner's `nomesPoster` accepts both, so
  the only effect would be a second copy of every cover.
- **Create the missing folder.** `MkdirAll` in the owner's library hides a real problem (a renamed
  or unmounted source) behind a folder the app invented.
