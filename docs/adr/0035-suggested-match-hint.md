# ADR-0035 — Suggested match: a hint the owner accepts, never an automatic fix

## Context

The external sources are literal. TMDb `/search/multi` does not spell-correct, so a folder
named `2 Broken Girls` (the series is `2 Broke Girls`) returns **zero** results, the import
fails, and the item parks as a draft. Measured against the real API on 18/nov/2026:

| query | results |
|---|---|
| `2 Broken Girls` | 0 |
| `Broken Girls` | 0 |
| `2 Broken` | 3 (none of them the series) |
| `2 Brok` | 5 — **finds it** |
| `2 Broken Gir` | 0 |

Two conclusions came out of that table.

1. The mature tools do not invest in string similarity; they give the user a deterministic
   door. tinyMediaManager (v4.1.4+) accepts `TMDB:39340` / `TVDB:248741` / `IMDB:tt…` in the
   search box; Jellyfin accepts a provider id in *Identify* and in the folder name
   (`Series (year) [tvdbid-12345]`). ADR work for those doors landed in #43 (search) and #44
   (folder name).
2. Dropping a token — the obvious "cheaper variant" — **does not solve the motivating case**.
   What works is cutting the string until it becomes a *prefix* of the real title, because
   TMDb matches word prefixes.

## Decision

The fallback exists, but it can only ever produce a **suggestion**:

1. **Never applied.** The scanner does not use it and does not import from it. Applying it is
   a human click, through the endpoint that already exists
   (`POST /entities/{id}/confirm-candidate`, `method=external`, which ignores
   `LimiarAutoMatch` by design).
2. **Computed on first open, not during a scan.** Four variant queries per name (drop last
   token, drop first token, two proportional character cuts) is affordable for one item the
   owner is looking at, and is not affordable multiplied by the size of a review queue — that
   burst already earned a 429 from TMDb once.
3. **Persisted, one row per entity** (`match_suggestion`, migration 0028), so the next open is
   a single row read. The row carries the candidate (source/source_id/title/year/poster), the
   score against the name **on disk**, and the variant that found it.
4. **Dismissal is sticky.** `DELETE /entities/{id}/match-suggestion` sets `dismissed = 1` and
   keeps the row: opening the item again neither re-queries the sources nor brings the warning
   back. A hint with no way out becomes permanent noise.
5. **The cut is calibrated below the auto-match threshold.** `matching.LimiarSugestao = 80`
   against `LimiarAutoMatch = 92`, because the two answer different questions: auto-match asks
   "is this certainly the item?", the suggestion asks "is this worth showing a human?". A
   dropped token scores 86-89 with this repo's own scorer, a real typo 95-100, so a cut at 92
   would reject exactly the variants the fallback generates.
6. **Only for an unresolved series.** No external id and `type = tv`. A resolved item needs no
   hint, and the fallback was designed and measured for the series vertical only.

## Consequences

- The item screen (hero, under the synopsis) shows the candidate poster, title, year, source
  and the query that found it, with two actions: *Fix match* and *Not this one*.
- The item stays in the review queue exactly as before. The hint is an addition, not a new
  queue: a scan with hundreds of drafts does zero extra work.
- An item whose name is wrong in a way that no prefix cut reaches (a transposition, a wrong
  first letter) still gets no hint. That is the intended failure mode: the owner uses the id
  door (#43) or writes the id in the folder name (#44).
- The score is stored, not recomputed: it says how plausible the guess was at the time, which
  is the number the screen shows and the one that explains a later "why did it suggest this?".
