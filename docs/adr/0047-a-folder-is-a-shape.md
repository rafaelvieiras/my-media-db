# 0047 — A folder is a shape: an unmatched series still gets its tree

Status: accepted (23/nov/2026)

## Context

A series that did not match any provider stopped being a series. `factsSerie` returned as soon
as the identity was missing, so the files were written **flat on the series**: no season, no
episode, nothing to open. Measured on the production library:

| | |
|---|---|
| draft series holding files with no season | 16 |
| files hanging directly on those series | 785 |
| files in a folder the scanner did not read as a season | 269, in 37 names |
| `Specials`-style folders that became a series of their own | 2 |
| episodes the provider publishes and the library does not have, in the review queue | 422 |
| duplicate episodes created by one broken dedupe | 213 |

The screen showed it: `/entities/<id>` of such a series rendered a lone card, and the review
queue grouped its files by the **season folder** — so the section read "Temporada 01", as if the
season were an item waiting for a series of its own. The owner read it that way and he was right.

## Decision

**The folders and the file names are evidence, and they are enough to draw the shape.**

1. A series without identity gets a **provisional tree**: the season comes from the folder, the
   episode from `SxxEyy` (or from the number alone inside a season folder), and the file is
   linked to the episode it names. The series stays `draft` — the identity is still missing; the
   tree is not.
2. A **specials folder is season 0**: `Specials`, `Especiais`, `Temporada Specials`,
   `Temporada Especiais`, `Season Extras`, `Extras`, `Temporada 00`. Inside it the extra-word
   filter does not apply: the folder exists for the commercials, bumpers and interviews that the
   providers do not carry, and those are the items to match by hand.
3. The **provisional episode is adopted** when identity arrives later, by number: same entity,
   provider data written, promoted out of the draft. Never a second episode next to it.
4. The dedupe key of a provisional episode is the **decoded** file name. Comparing the raw
   `extension_data` missed every name with `&`, `<` or `>` (Go writes `\u0026`), which created a
   duplicate per re-scan.

## Consequences

- `Manager.RepararArvoreProvisoria` repairs the data that predates this ADR, once, idempotently.
  Rehearsed on a copy of the production database: 34 seasons and 832 episodes built, 177
  duplicates removed, 0 files left without a link.
- A season folder that is a **typo** (`Temporda 01`) still becomes the series name: the folder
  is the authority and the app will not guess. The same holds for a release sub-folder inside a
  season folder. Both stay as they are, visible in the review queue.
- The tree is not a numbering: it never renumbers a file. `S01E02E03` still links two episodes.
