# ADR-0033 — Where alternative and merged episode orderings come from

- **Status:** Accepted
- **Date:** 2026-11-18
- **Agenda:** the owner asked for the DVD-vs-TV numbering problem and for a way to say
  "these three aired episodes are one entry in my release". Research:
  [13](../13-episode-orderings-and-release-units.md).
- **Builds on:** [ADR-0021](0021-series-model-hierarchy-orderings-and-file-link.md) (the
  series model), [ADR-0005](0005-uniform-composition-through-extension-data.md)
  (`extension_data`), [ADR-0002](0002-priority-between-external-sources.md) (source priority).

## Context

ADR-0021 decided that the episode is the atomic entity, that `(season, number)` is **not** a
key, and that orderings are a **projection** of the `numbering` stored per episode. It also
measured that **no provider models a merge**: neither TMDB nor TheTVDB has a field to attach
two or more episodes to one numbered unit.

Two things were left undone and now became the priority:

1. The TMDB adapter imported **only** the `official` order. The alternative orders exist in
   TMDB as **episode groups** and were never consumed. Production has no TheTVDB key, so the
   whole catalog is `official`-only and a DVD box set never matches.
2. The merge case ("HBO re-cut three aired episodes into one entry") had **nowhere to live**:
   `numbering` is per episode, 1:1.

Decision from the owner: implement both, and **do not** add the TheTVDB key now.

## Decision

### 1. TMDB episode groups become alternative orderings

The tree keeps the **official** order as the default — it stays the only one that becomes
season entities — and each imported group adds **one more entry in `numbering`** of the
episodes it contains. The mapping is direct:

| TMDB episode group | our `numbering[<chave>]` |
| --- | --- |
| sub-group (`groups[].order`, 1-based) | `season` |
| position inside the sub-group (`episodes[].order`, **0-based**, restarts per sub-group) | `number + 1` |
| group `name` | the order **key**, slugged (`Blu-ray Order` → `blu-ray-order`) |

Rules that come from the measurements:

- **The key comes from the name, never from the type.** The `type` (1 original air date,
  2 absolute, 3 DVD, 4 digital, 5 story arc, 6 production, 7 TV) is picked by whoever created
  the group — *The Office*'s "Alternate Order" is filed as type 1. The type is used **only** to
  decide which groups win the import cap.
- **Cap of `maxGruposTMDB` = 8** groups per series, ordered by type preference (DVD, absolute,
  original air date, TV, digital, production, story arc) and then by size. Cost is one request
  for the list plus one per group, once per import.
- **Best-effort**: a series without groups (404 or empty list), a group that fails, or a group
  whose episodes are all absent from the tree degrades to "that order does not exist". A group
  that matches **no** episode never creates an order.
- Nothing else changes: the read-time projection, the ordering selector and the matcher already
  work off `numbering`/`season_types`, so they pick the new orders up without changes.

### 2. The owner order, as one more ordering

The owner declares their own numbering, and it is stored like any other order:

```jsonc
// core.tv@1
"owner_order": {
  "name": "HBO 2004",
  "based_on": "official",
  "units": [
    { "season": 5, "number": 5, "title": "Blackout", "episodes": ["<id>", "<id>", "<id>"] },
    { "season": 5, "number": 6,                    "episodes": ["<id>"] }
  ]
}
```

- **Anchored on episode ids**, never on numbers (ADR-0021 rule 4) — a re-scrape cannot shuffle
  it.
- `episodes` has 1..N entries and the **order inside a unit is meaningful**, the same idea as
  `media_file_entity.ordinal`.
- It is a **projection**: choosing it renumbers what the UI shows and what the export writes. It
  never renames a file and never duplicates an episode.
- Stored in `extension_data` (ADR-0005) — no migration. It becomes a table only if it earns its
  own artwork or relations.
- It is written **only** by the owner (an explicit endpoint), never by an import: a re-enrich
  must not overwrite it.

### 3. The TheTVDB key is deferred, not rejected

The adapter is complete and would bring `dvd`, `absolute`, `alternate`, `regional`, `altdvd`
and `alttwo` for free. It stays one config line away, and when it arrives the two sources will
coexist: TheTVDB order keys (`dvd`, `absolute`…) and TMDB group keys (`blu-ray-order`…) are
disjoint by construction, because TMDB keys always come from a name slug.

## Consequences

- A DVD/streaming reorder now **matches local files without renaming them** — the matcher
  already tries every ordering present in the `numbering`, and only accepts when the orderings
  agree on the same episode sequence.
- Series whose community never made a group stay `official`-only: the fix is the owner order
  (or, later, the TheTVDB key).
- **A merge now has a home.** One owner unit with three episodes means one file linking three
  episodes with ordinals 0/1/2, which the model already supports; the unit's `(season, number)`
  is what the scan, the display and the export use.
- Cost of the import: up to 9 extra TMDB requests per series, once, and up to a few hundred
  extra `numbering` entries per episode in `extension_data` (the same order of magnitude TheTVDB
  would bring).
- Known gap, to be handled with the editor: the ordering selector shows the **key**
  (`blu-ray-order`). A human label per order (`order_labels` in `extension_data`) needs the
  label to travel provider → server → UI, which is a cross-cutting change; until then the UI
  prettifies the key.
