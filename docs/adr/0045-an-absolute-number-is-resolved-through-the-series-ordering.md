# ADR-0045 — An absolute number is resolved through the series' ordering

- **Status:** Accepted
- **Date:** 2026-11-22
- **Agenda:** issue #104. 76 files of one series (X-Men, TMDB 4574) named only by their
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

| ordering | split | files whose season disagrees with the folder |
|---|---|---|
| dvd-order | 13/13/25/15/10 | **1** |
| disney | 13/13/19/21/10 | 7 |
| production-order | 13/13/21/23/6 | 9 |
| official | 13/13/19/17/14 | 11 |

The folder split comes from a release, and it follows the DVD one.

## Decision

An absolute number that has no `absolute_number` from the provider is resolved through the
**series' orderings**, by flattening the ordering in `(season, number)` order and taking the
Nth episode as absolute N.

1. The provider's `absolute_number` still wins when it exists (ADR-0021 is untouched).
2. Among the orderings, the one that puts the absolute number **in the season the folder
   declares** wins: the folder is the anchor that an absolute number lacks.
3. When no ordering agrees with the folder, a fixed priority decides — release orderings first
   (`dvd-order`, `itunes-order`, `disney`, `production-order`), `official` last — and the
   disagreement is logged by the scanner, never silent.
4. An ordering whose flattened numbering has a **hole inside a season** (season 2 starting at
   episode 5) is discarded: counting positions over a broken grid would shift every number
   after the hole. Sending the file to review is better than a wrong absolute.
5. Season 0 (specials) is not counted in the absolute numbering.

## Consequences

- The 76 files of X-Men resolve to the real episodes with the DVD split; 75 of them agree with
  the folder, and the one that does not (`X-Men - 52`, folder `Temporada 03`, DVD season 4) is
  logged. Its NFO carries season 4 and Kodi/Jellyfin/tinyfin read the NFO, so it displays in
  the right place even though the file stays in the folder it was in.
- A series with no alternate ordering keeps today's behaviour: absolute without
  `absolute_number` → review.
- The resolution is a **pure function of the index** (`internal/tvmatch`), so it is testable
  without a database: `absolutoDaOrdem`, `resolverAbsoluto`, `CasarComPasta`.
- The season of the folder enters the matcher from three call sites (scanner, reconcile,
  enrich), all through `CasarComPasta`.
