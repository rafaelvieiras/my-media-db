# 0038 — A match can succeed and still be useless: tell the owner

Status: accepted (19/nov/2026)

## Context

ADR-0035 added one warning to the item screen: a name that finds nothing, but whose
cheaper variant finds a candidate. It is a hint, never an identity, and the reason it
exists is that a wrong cataloged series is worse than a draft.

Production showed two more ways for a match to be accepted and be worthless, and neither
one is a naming problem:

1. **The entry has no episodes.** TMDb carries phantom entries: full duplicates of a real
   series with no seasons, no episodes, an empty overview and an origin country that means
   nothing. One measured series matched `tmdb 14383` (0 seasons, 0 episodes, origin `IO`,
   homepage of an Indian site) instead of `tmdb 2745` (the same name in its translated form,
   1994). The name comparison was exact and the folder had no year, so nothing vetoed it.
   The tree imported empty, and all 86 files — each with a clean `S01E01` in its name —
   became provisional drafts. Same shape for another series (`tmdb 5565`): the entry is
   correct, TMDb simply never catalogued its episodes.

2. **The tree exists but does not fit the files.** `Batman` matched "O Batman" (2004, 13
   episodes per season) while the folder holds another Batman series (`BatmanOld_S01E14…`);
   `Pica-Pau` matched a source that numbers its seasons `S2` and `S1940` against folders
   named `Temporada 01..07`. In both, the matched part looks fine and the rest accumulates
   as provisional episodes — which is why nobody noticed: only **2 of 106** affected series
   had a completely empty tree.

Both were invisible: the series stays `cataloged`, the review queue fills with episodes the
owner has no reason to suspect, and nothing in the UI compares "files that fit" against
"files that did not".

## Decision

The suggested match gains two reasons, sharing the row and the warning of ADR-0035
(migration 0029 adds `reason`, `files_unmatched`, `files_total`):

| reason          | trigger                                                                                                             | the row points at                                                                                        |
| --------------- | ------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `empty_tree`    | the tree fetched for the current match has **zero episodes**                                                        | the best **sibling** of the same name, when one reaches `LimiarSugestao`; otherwise the current identity |
| `tree_mismatch` | post-scan census: **≥ 5** draft episodes **holding a file** **and** that count **≥** cataloged episodes holding one | the **current identity** (the thing under suspicion)                                                     |

The API exposes `can_apply` so the screen hides the apply action when the candidate is the
identity the item already has. The other reasons reuse the same two actions — apply through
`POST /confirm-candidate`, or dismiss — plus a line pointing at "Fix match", which already
exists in the item menu.

The identity is **not revoked** when the tree is empty. Revoking it was the first design and
it loops: the next scan searches the same name, picks the same broken entry, imports an
empty tree and revokes again — one tree fetch per scan, forever, with the series never
settling. Keeping the identity and telling the owner is strictly better: nothing gets worse
than it already is, and the fix is one click away.

## Why the census lives in the post-scan

The scanner knows how many files it placed, but not whether a series is _mostly_ unmatched:
that ratio needs the whole tree, which the database already has when the scan ends. Reading
it in the post-scan also covers series scanned by an earlier run, so an existing library
gets the warning without waiting for another full scan.

The cut was measured against a production library rather than guessed:

| rule                                | series selected | comment                                                |
| ----------------------------------- | --------------- | ------------------------------------------------------ |
| `prov ≥ 5` and `prov ≥ cataloged`   | **10**          | all 10 genuine (e.g. 97 vs 43, 86 vs 0, 81 vs 78)      |
| `prov ≥ 5` and `prov ≥ cataloged/2` | 11+             | one of them enters on legitimate specials (290 vs 430) |

A false negative costs nothing — the series keeps dropping drafts into the review queue,
where it already was. A false positive costs trust in the warning, which is the only thing
that makes the owner act.

## Consequences

- The warning now covers three situations with one shape. A row with an unknown reason
  (older client) falls back to the name hint.
- Series with an identity and existing drafts get an alert **retroactively** (the census is
  read from the database, not from the scan progress): the 10 measured series got theirs on
  the next post-scan.
- `dismissed` is never reset by an upsert, so a dismissed alert does not come back on the
  next scan — and the census skips series that already have a row, so a dismissed alert
  costs no work at all.
- The tree signal requires the tree to be fetched. A series whose identity came from an id
  and never went through enrichment gets the alert on the first enrichment (auto-match,
  retroactive job, "Fix match").
- Out of scope: detecting the same situation for albums/artists, and choosing a size for
  the person photo (`h632` vs `w185`, issue #53).

## Amendment (22/nov/2026): the unit is the FILE, and a stale hint leaves the screen

The census counted episodes **by status**, so a series where the owner has a fraction of the
show tripped it: 21 files of a 42-episode show means 21 cataloged episodes (each holding a file)
and 21 draft episodes (the ones the library does not have), which satisfies "provisional ≥
cataloged". Measured in production: a series with 0 draft episodes holding a file, wearing the
warning. A draft episode that holds nothing is the provider's episode, not evidence against the
match.

The census now counts **draft episodes holding a present file**, and the percentage the screen
shows is a file count. In the same run, a `tree_mismatch` hint whose symptom disappeared is
**removed** (unless the owner dismissed it — a dismissal is a decision, not a symptom), because
the census only wrote and skipped any series that already had a row.

Measured after the change: 10 alerts → 9, all 9 with real stranded files (97, 86, 81, 65, 44,
35, 26, 23, 11).

## Amendment (21/nov/2026) — a hint may carry the candidate for a WRONG identity

`tree_mismatch` used to store the **current identity**, which is the thing under suspicion: the
alert could say "this is wrong" and nothing else. The `sugerir_identidades` job now re-scores
those series with the rules of [ADR-0046](0046-series-identity-is-title-year-and-shape.md) and,
when a different identity reaches the suggestion threshold **and** its tree can host at least half
the folder's files, rewrites the row with that candidate.

The decision this ADR records is unchanged: **nothing is applied automatically**. The owner asked
for the middle ground explicitly (21/nov/2026) — the job must not change an identity behind his
back, but an alert that cannot say what to do instead is a chore. `can_apply` follows from the
rule already in place ("a row whose candidate is the identity the entity already has is not
applicable"), so no new field was needed.
