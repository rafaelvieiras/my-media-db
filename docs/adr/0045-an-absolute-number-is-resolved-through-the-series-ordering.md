# ADR-0045 — An absolute number is resolved through the series' ordering

- **Status:** Accepted
- **Date:** 2026-11-22
- **Agenda:** issue #105. 76 files of one series (X-Men, TMDB 4574) named only by their
  absolute position (`X-Men - 14 - Até que a Morte nos Separe 1.mkv`) had no
  `<episode>` in their NFO, and each one sat as a DRAFT episode next to the real one,
  which had no file.
- **Builds on:** [ADR-0021](0021-series-model-hierarchy-orderings-and-file-link.md) (the series
  model, the absolute number), [ADR-0033](0033-alternate-and-owner-orderings.md) (alternate and
  owner orderings), [ADR-0044](0044-one-nfo-per-file.md) (the NFO unit).

## Context

The parser has an absolute (anime) tier since ADR-0021: a name that carries
`Show - 013` states the position of the episode in the series, not in a season. The matcher
resolves it against `absolute_number` in the episode's extension data.

The provider fills `absolute_number` only for the series that declare it. TMDB leaves it
empty for X-Men, so the matcher had nothing to match: `Casar` returned false, the scanner
created a provisional episode from the file name, and the real episode stayed fileless.

What the provider DID give for that series is four alternate orderings (DVD, iTunes,
Disney+, production) — already imported as `numbering` per episode since v0.4.0 — plus the
official one. The library's folder split is 13/13/26/14/10 and the orderings are:

| ordering         | split          | files whose season disagrees with the folder |
| ---------------- | -------------- | -------------------------------------------- |
| dvd-order        | 13/13/25/15/10 | **1**                                        |
| disney           | 13/13/19/21/10 | 7                                            |
| production-order | 13/13/21/23/6  | 9                                            |
| official         | 13/13/19/17/14 | 11                                           |

The folder split comes from a release, and it follows the DVD one.

## Decision

An absolute number that has no `absolute_number` from the provider is resolved through the
**series' orderings**, by flattening the ordering in `(season, number)` order and taking the
Nth episode as absolute N.

1. The provider's `absolute_number` still wins when it exists (ADR-0021 is untouched).
2. Among the orderings, the one whose episode at that position carries the **title the file
   states** wins. This is measured, not assumed: in a 76-file series the files follow the
   `disney` order (74 of 76 names match exactly), while `dvd-order` fits the folder COUNTS
   better and points at the wrong episodes — with it a file stating one episode title landed
   on another episode. A title vote only counts when it is strict
   (one candidate better than every other, and equal notes only agree when they point at the
   same episode).
3. When the title does not decide, the ordering that puts the number **in the season the
   folder declares** wins: the folder is the anchor that an absolute number lacks.
4. When nothing agrees with the folder, a fixed priority decides — release orderings first
   (`dvd-order`, `itunes-order`, `disney`, `production-order`), `official` last — and the
   disagreement is logged by the scanner, never silent.
5. **A file that states a title (two or more words) matching no candidate at all goes to
   review.** Falling back to the priority there is how two files ended up on ONE episode
   ("Um Mutante dos Diabos" took the episode of "O Valor de um Homem 1"). A name with no
   episode title (`X-Men - 02.mkv`) does not fall under this rule: it has nothing to compare,
   and the folder still anchors it.
6. An ordering whose flattened numbering has a **hole inside a season** (season 2 starting at
   episode 5) is discarded: counting positions over a broken grid would shift every number
   after the hole. Sending the file to review is better than a wrong absolute.
7. Season 0 (specials) is not counted in the absolute numbering.

## Consequences

- Measured on a series of 76 files (dumps of the numbering and of the file names): the
  resolution puts **72** files on the episode whose title matches the file and leaves **4** for
  review — no wrong link and no episode with two files. The four are one name that exists in
  no provider list, one title the release translated differently and two files **swapped** in
  the release against every ordering (`60`/`61`) — a per-number resolution cannot see a
  permutation.
- The season of the NFO follows the series TREE (the provider's official seasons), not the
  folder: `X-Men - 52` in `Temporada 03` carries season 4. Kodi/Jellyfin/tinyfin read the NFO,
  so it displays in the right place while the file stays where it was.
- A series with no alternate ordering keeps today's behaviour: absolute without
  `absolute_number` → review.
- The resolution is a **pure function of the index** (`internal/tvmatch`), so it is testable
  without a database: `absolutoDaOrdem`, `resolverAbsoluto`, `CasarComPasta`.
- The season of the folder enters the matcher from three call sites (scanner, reconcile,
  enrich), all through `CasarComPasta`.
