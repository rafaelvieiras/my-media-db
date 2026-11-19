# 0039 — A release name is not a title, and a folder tag is not part of one

Status: accepted (19/nov/2026)

## Context

A movie sat in the catalogue with the right title, the right year and **no external id**, and
the app did nothing about it: `101 Dálmatas O Filme` (1996), folder `101 Dálmatas - O Filme
(1996)`, file `101.Dálmatas.1996.WEB-DL.MAIOR.1080p.DualÁudio.AC3.H264-ACHEIOFILME.COM-trailer.mp4`.

Three independent defects, each one enough to stop the match on its own:

1. **The folder title carries a genre tag.** TMDb answers `/search/multi?query=101 Dálmatas O
   Filme` with **0 results**; `101 Dálmatas` returns the right movie (`11674`, 1996-11-27) as
   its **first** result. The folder style that appends "O Filme"/"A Série" produces a title no
   catalogue has.

2. **The release name parsed into garbage.** `naming.Parse` only recognized a year in
   parentheses, so the dot-separated file name produced the title
   `101 Dálmatas 1996 MAIOR DualÁudio H264-ACHEIOFILME COM-trailer` **with year 0** — a string
   no source answers, and without the year the candidate could not even be tie-broken.
   MEASURED on the production library: of the 67 movies still without an id, **46** derived a
   search string like that from the release name, **all of them with year 0**. The v0.9.1
   measurement said the same in other numbers ("about 0 reach the threshold by the release
   name"): the second attempt was effectively dead.

3. **A movie could never be told "did you mean this".** The hint of ADR-0035 was written for
   series and `podeSugerir` demanded `type == "tv"`, so the screen showed nothing at all — no
   candidate, no suggestion, no way out except knowing the id by heart.

The item had also already spent its retry: the retroactive job stamped it at 01:05 and the
throttle holds it out for 15 days.

## Decision

**A release name is not a title: it ends where its release section begins.** `naming.Parse`
now cuts the title at the first release token (a technical token, a resolution or codec glued
into a longer token — `BD1080p`, `H264-ACHEIOFILME` — or an audio marker: `DualÁudio`,
`Dublado`, `Legendado`), and reads the year from the tail of that name (walking the last three
fields, because noise often follows it: `…1997 Trial`, `…1993 rmz`), accepting the 2-digit form
older releases use (`Predador.86`).

Guards, because this parser names items:

| guard | why |
|---|---|
| the year must be plausible (`1900..2029`, fixed, not the clock) | `Blade.Runner.2049.2160p` keeps 2049 as the title |
| never consume the whole title | `2012.1080p` is the movie "2012", not a year with an empty name |
| the cut needs a release token AFTER the first field, or a `.`/`_` name | `Death Race 2000` is a title; `Dr. Strangelove` keeps its dot |
| ambiguous tokens (`web`, `dvd`, `divx`) never open a release section | `Charlotte's Web (2006)` keeps its "Web" — it was losing it before this ADR |

**The FOLDER is the authority, not the file.** The Kodi convention — the one this project
follows and every player reads — puts the work in the folder name; the entity title is only a
cleaned copy of it, and the copy loses structure the source can use (the subtitle after the
dash, the brackets). So the attempts of a movie start from the **raw folder name**, guarded
against the library root (there the folder is the library, and searching "Filmes" matches
anything):

| order | attempt | example |
|---|---|---|
| 1 | folder as the convention writes it | `101 Dálmatas O Filme` |
| 2 | folder up to the subtitle dash, or without the trailing genre tag | `101 Dálmatas` |
| 3 | the entity title, i.e. the stored copy of the folder | `101 Dálmatas O Filme` |
| 4 | the release name of the file, parsed | `101 Dálmatas` (year 1996) |

The release name is still searched, but **last**: it is the name of one file, not of the work.

**A trailing genre tag gets its own attempt, right after its literal.**
`naming.SemEnfeite` drops a trailing `O Filme` / `A Série` / `The Movie` and the variant is
appended after the literal ones (`comVariantesSemEnfeite`), so the literal title is still
searched first and the variant only costs a query when everything before it failed. Unlike the
typo of ADR-0035 this is cheap and deterministic, so it runs in the automatic path instead of
waiting for the owner to open the item.

**The "did you mean this?" hint covers movies.** `SugerirSerie` grew into `SugerirPorTipo`
(with `SugerirFilme` on top) and `podeSugerir` accepts `movie` as well — same ladder of cheaper
variants, same `LimiarSugestao`, same rule that it is never applied on its own. The source filter
is per type: a series needs the series capability (there is a tree to import), a movie only
needs a source that is not music-only.

## Measured effect

Replaying the derived search string of the **67** movies still without an id in production:

| | before | after |
|---|---|---|
| release name parsed into garbage | 46 | **2** |

The two survivors are genuinely broken names (an unbalanced bracket soup and a year at the
start of the title). The item that motivated this resolves by both routes: `101 Dálmatas
(1996)` reaches `tmdb:11674`, and the preview now offers the candidate.

## Consequences

- New items named release-style (a loose file at the library root, or a female folder with dots)
  get a **cleaner title and a year** instead of the whole tail, which raises the confidence from
  40 (title only) to 70 (title+year) — and 70 is the bottom of the review queue, so a
  title+year movie stops being invisible.
- `naming.Parse` is now the single place that knows what a release token is. `tokensTecnicos`
  keeps the meaning of "technical", `tokenDeRelease` decides what ENDS a title.
- The attempt ladder for a movie can reach four queries (title, release name, and each one
  without the filler). The walk stops at `LimiarAutoMatch`, so the extra attempts only cost a
  query when the item is genuinely hard.
- Out of scope, still open: the year veto discarding a real candidate (`Transformers 2`, folder
  2011 vs real 2009), and the parser does not glue a spaced-out title back together
  (`VingadePredador` stays glued).
