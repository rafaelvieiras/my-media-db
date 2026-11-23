# ADR-0046 — The identity of a series is decided by title in any language, the folder's year, and the shape of the tree

- Status: accepted (23/nov/2026)
- Supersedes part of ADR-0036 (the exact-title rule of the series import)
- Related: ADR-0021 (series tree), ADR-0038 (match alerts), ADR-0045 (absolute numbers)

## Context

The series auto-import (`ImportarSeriePorNome`) searched the provider by the folder's name and
accepted the result whose title was **exactly** equal (case-insensitive). The folder's year was
parsed by the scanner and never used, and nothing compared the size of the candidate with the
size of the folder.

That combination produced confident wrong identities in production. All of them carry
`match_confidence` 90–100 and a `tree_mismatch` alert that says "the entry is another show with
the same name" — which was literally true:

| the folder says        | entry accepted | what the provider says of that entry        | files stranded |
| ---------------------- | -------------- | ------------------------------------------- | -------------- |
| a series, no year      | tmdb 81197     | the same title **2018**, 3 seasons / 22 eps | 44             |
| a series marked (1990) | tmdb 93693     | the same title **2019**, 3 / 13             | 65             |
| a series marked (1983) | tmdb 51817     | **2012**                                    | 36             |
| a series, no year      | tmdb 280224    | the same title **2009**, 1 / 3              | 23             |
| a series, no year      | tmdb 39261     | a translated title, **2011**, 1 / 16        | 35             |
| a series, no year      | tmdb 14383     | **0 seasons / 0 episodes**                  | 86             |
| a series, no year      | tmdb 5565      | **0 / 0**                                   | 26             |

Three measured causes, each independent:

1. **The year never reached the decision.** `derivarSerie` parses `(1990)` out of the folder name
   and the import function did not take it.
2. **The exact-title rule preferred the wrong entry.** TMDb answers `Clube do Terror` with
   `2913 "O Clube do Terror" (1992)` and `93693 "Clube do Terror" (2019)`; `EqualFold` rejects the
   1992 show because of the article and accepts the reboot.
3. **Nothing compared sizes.** An entry with 3 episodes was accepted for a folder with 26 files,
   and an entry with **zero** episodes was accepted at all — `ErrArvoreVazia` only guarded the
   scan path.

The provider also knows the titles a show has in other languages
(`/tv/{id}?append_to_response=translations`), and the local folder very often uses the original
title ("Cold Case" for the pt-BR "Arquivo Morto"). The movie auto-match already scored the original
and the alternative titles; the series path did not.

## Decision

The identity of a series is decided by three pieces of evidence, in this order of weight:

1. **The name, in any language the source knows.** The score compares the folder's name against
   the candidate's translated title, its **original** title (free in the search payload) and, for
   the few closest candidates only, its **alternative/translated** titles (one extra round trip,
   capped at 3 candidates). Comparison tolerates a leading article ("O Clube do Terror" vs
   "Clube do Terror"), because an article is not part of a name — measured: that difference alone
   costs 17 points of Jaro-Winkler and pushed the correct show 9 points below the cut.
2. **The year the folder states**, with a **2-year slack** for series (movies keep 1). The slack is
   not cosmetic: a folder marked `(1990)` was measured against the 1992 show, so a 1-year gate
   would discard the only
   correct candidate. A candidate outside the slack is **discarded**, which is what keeps a remake
   of the same name from winning.
3. **The shape of the tree**, checked BEFORE anything is written: the provider's tree must be able
   to host at least **half** the folder's video files. Half, not "any", because a folder
   legitimately holds more files than episodes (specials, a part 1/part 2 split, unrecognized
   extras). An entry with **zero** episodes is always refused.

Two candidates within **5 points** of each other are a tie, and a tie goes to review — never a
coin flip. The name found nothing above the cut (`LimiarAutoMatch`, 92) is also review.

## Consequences

- The wrong identity becomes a **review item with evidence**, not a cataloged show whose files can
  never be found. The failure mode of the whole feature changes from "confidently wrong" to
  "visibly undecided".
- The year is a SCORE input, never a hard filter: a folder year that the source disagrees with by
  a couple of years must not lose the identity. (A hard filter is what Jellyfin/TMDb does with
  `first_air_date_year`; it fails _safe_ but it does fail, and it does not use the size evidence we
  have for free.)
- The shape gate costs no extra request: it reads the tree that the import was going to fetch
  anyway, and refuses before persisting.
- `ErrSerieIdentidadeIncompativel` is a new, distinct outcome. The scanner logs it with its own
  message and sends the folder to review; it is not "not found" and not "ambiguous".
- The article tolerance changes the MOVIE score too (same scorer). That is deliberate: an article
  is not part of a name for a film either, and the year veto is untouched.
- Rejected alternative: renumbering the files to fit the entry. The folder is the authority for
  what the collection is (see #111, closed): a season a client reads from the folder cannot be
  overridden by the provider's numbering.
