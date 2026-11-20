# 0044 — One NFO per file, one block per episode

Status: accepted (20/nov/2026)

## Context

The `midias_faltantes` job rewrote the same **689 NFOs** on every run while only ~314 files
changed on disk, and the file it rewrote was wrong. Measured on the production library
(issue #80):

| | |
|---|---|
| NFO paths shared by 2 episodes | 264 |
| NFO paths shared by 3 episodes | 58 |
| episode entities behind those 322 paths | 702 |
| NFO writes in one run | 689 |
| NFO files that actually changed | 314 |

A real file, `Johnny.Bravo.S03E04E05E06....mkv`, holds **three** episodes. The app correctly
created three episode entities (three distinct TMDb ids), but Kodi names the `.nfo` after the
**video**, so all three resolved to the same path, and each one wrote its own single-block
document over the others:

```
16:09:23Z  episode 6 wrote
16:10:16Z  episode 4 wrote
16:10:36Z  episode 5 wrote   ← last writer wins
```

What survived on disk was a mix nobody asked for — the title of episode 5 with
`<episode>4</episode>`, the number parsed out of the file name, which then travelled forever
through the "preserve unrecognized tags" catch-all. And because the content could never match
what the second and third episodes expected, `nfoConvergenteNoDisco` never converged: every run
rewrote all 380-odd blocks again.

## Decision

**The NFO is owned by the FILE, and its document has one `<episodedetails>` block per episode.**

1. `ObterMediaFileDoEpisodio` + `ListarEpisodiosDoMediaFile` give the writer the file and the
   whole group of episodes that live in it, with the numbering taken from the entity tree
   (series→season `sort_index` = season number, season→episode `sort_index` = episode number,
   [ADR-0021](0021-series-model-hierarchy-orderings-and-file-link.md)).
2. `localmedia.MontarNFOEpisodios` writes **one XML header and one block per episode, in file
   order**. This is Kodi's own format (its library export produces it; the concatenated roots are
   technically invalid XML and there is an open upstream issue about that, but it is what Kodi,
   Jellyfin and every NFO parser reads).
3. `<season>`/`<episode>` become **named fields** of the document, filled from the tree. They used
   to exist only as an accidental survivor of the Extras catch-all, which is how a wrong number
   became permanent. A block with no number in the tree (a special with no season/episode link)
   omits the tags instead of writing `0`.
4. `ExtrairBlocosDeEpisodio` parses every block of the existing file, so a rewrite preserves the
   watched/rating state of **each** episode, matched by `<season>`/`<episode>`; a legacy
   single-block file is matched to the first episode of the file (what the old code preserved).
5. The write stamps `nfo_path`/`nfo_written_at` on **every** episode of the file — the columns
   describe the file, not the entity — and removes a sibling's legacy `.nfo` when its destination
   moved.
6. **The write is retroactive.** A file whose `.nfo` was written by the old logic no longer
   converges (its content has a single block, and its `<episode>` comes from the file name), so
   the next run rewrites it as the document of the file. The first run after the upgrade fixes all
   322 shared paths; from then on they cost one convergence check and zero writes.

## Consequences

- The 322 shared paths leave the run for good: ~380 redundant writes per run (~4 min on the
  array) and the 689-writes-for-314-files anomaly disappear.
- The 12 thousand single-episode files keep the same shape (one header, one block); the only
  difference is the correct `<season>`/`<episode>` and the loss of any wrong value that had been
  preserved as an unknown tag.
- Kodi/Jellyfin now see every episode of a multi-episode file with its own title, plot and
  numbering, which they never did — one of the three always lost.

## Alternatives rejected

- **One owner per file** (only the lowest episode writes): cheapest, but the other episodes
  vanish from the NFO — exactly the state the owner has today for two of every three.
- **A separate `.nfo` per episode** (`S02E05.nfo` next to the combined video): Kodi only reads the
  `.nfo` named after the video, so the siblings would be invisible.
