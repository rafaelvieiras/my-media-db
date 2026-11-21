# 13 — Episode orderings and release units

Same episodes, different numbers. That is the normal case, not the exception: a series
aired on TV gets a DVD release with another order, a streaming service re-cuts it, and a
box set bundles three aired episodes into one numbered unit. Three different problems hide
behind the word "ordering", and they need three different answers:

1. **Reorder** — same episodes, other `(season, number)`.
2. **Granularity of the file** — one file holds several aired episodes.
3. **Merge** — one numbered unit in the release contains N aired episodes.

Problem 1 is data from the providers. Problem 2 is already solved by the file↔episode link.
Problem 3 exists in **no provider**, so it can only come from the owner.

Related: [ADR-0021](adr/0021-series-model-hierarchy-orderings-and-file-link.md) (the model),
[ADR-0033](adr/0033-alternate-and-owner-orderings.md) (alternate and owner orderings),
[07](07-enrichment-and-providers.md) (providers), [09](09-nfo-and-export.md) (export).

**Status (19/nov/2026)** — all three problems have a home in the code:

| problem | where | shipped |
|---|---|---|
| 1. reorder by the provider | TMDB **episode groups** imported as alternate orderings (`internal/provider/tmdb/tmdb_grupos.go`); TheTVDB reads every `seasonType` when a key exists (`internal/provider/tvdb/tvdb_serie.go`) | v0.4.0 (TMDB groups) |
| 2. one file, several episodes | N links in `media_file_entity` with `ordinal` | before v0.4.0 |
| 3. one numbered unit merging N aired episodes (**release units**) | owner order: units of `(season, number)` holding 1..N episode ids, stored in the override layer (`media_override`), tried first by the matcher (`internal/tvext/ordem_dono.go`, `internal/tvmatch/ordem_dono.go`), edited in the series screen | v0.5.0 |
| 4. a file that only carries the **absolute** position | the series' orderings are flattened in `(season, number)` order and the Nth episode answers for absolute N, with the folder's season as the tie-break ([ADR-0045](adr/0045-an-absolute-number-is-resolved-through-the-series-ordering.md)) | v0.15.17 |

What is **not** done: TheTVDB stays off (no key in production, so every series in the
catalog is `official`-only in practice), and a file whose name carries no `(season, number)`
still cannot be placed — the parser has a labelled-episode rule (`Episódio 16 - …`) that is
not wired into `ParseEpisode`.

## What the providers offer (measured 2026-11-18)

### TMDB — episode groups

Two endpoints: `GET /tv/{id}/episode_groups` (list) and `GET /tv/episode_group/{id}` (items).

Measured on **2316** (*The Office*, US): **10 groups**.

| Group | Type | Episodes |
| --- | --- | --- |
| Alternate Order | 1 | 243 |
| Italian DVD | 2 | 186 |
| iTunes France | 2 | 201 |
| Blu-ray Order | 3 | 281 |

The payload has `groups[]` (sub-groups — volumes or seasons) with a 1-based `order` and an
`episodes[]` inside. **Inside a sub-group the `order` restarts at 0**, and each entry also
carries the underlying `id`, `season_number` and `episode_number`.

Limits, measured:

- **One entry = exactly one episode.** No field attaches two or more episodes to one entry:
  no `combined`, no `parent_episode`, no `sub_episodes`. Confirmed on the case that matters
  most here — *Beavis and Butt-Head*, group "The Mike Judge Collection" (type 3): **141
  entries for 141 distinct episodes**, even though that collection merged many two-minute
  shorts into longer units.
- No duplicates inside a group: 277 entries / 277 distinct episodes in the Blu-ray order of
  2316.
- Groups are **community data and often absent**: **4549** (*Comedians in Cars Getting
  Coffee*) returned **0 groups**. An order may simply not exist for a given series.

### TheTVDB — season types

`GET /series/{id}/episodes/{season-type}[/{lang}]`, and the values come from
`GET /seasons/types`. The API spec lists **eight** today:

```
default  official  dvd  absolute  alternate  regional  altdvd  alttwo
```

`EpisodeBaseRecord` carries `seasons[]` (the number **in each season type**),
`absoluteNumber`, and a placement trio — `airsBeforeSeason`, `airsBeforeEpisode`,
`airsAfterSeason` — which is how TheTVDB puts a special between two episodes.

Two facts worth knowing before betting on it:

- `altdvd` and `alttwo` were **broken for about two years** (issue
  [thetvdb/v4-api#340](https://github.com/thetvdb/v4-api/issues/340) — the endpoint returned
  `0 episodes`) and were **fixed on 2026-11-10**. They exist, but they are new again.
- **Still no merge.** An episode is one record; nothing attaches N episodes to one numbered
  unit. `airsBefore*` moves an episode between two others, it does not fuse them.

### Conclusion for both

Reorder is provider data — rich in TheTVDB, partial in TMDB. **Merge is impossible in both.**
This is the second time we measure it: ADR-0021 reached the same conclusion on *Looney
Tunes* and *One Piece* (no `combined`/`parentEpisode`/`subEpisodes` in either API). It is not
a gap we can wait out.

## What this project already has

- `core.tv@1` keeps `numbering: {order: {season, number}}` and `absolute_number` **per
  episode**; a season entity exists only for the **default** order, and the other orders are
  a projection computed at read time (ADR-0021).
- **N:M file↔episode** through `media_file_entity(media_file_id, entity_id, ordinal)`. A file
  named `Show S01E01-E03.mkv` produces **one file and three links**, with `ordinal` 0/1/2
  (`TestScanSerieRangeTresVinculos`). Problem 2 is done.
- The TheTVDB adapter is **complete**: it reads `seasonTypes`, pulls every ordering and merges
  them into `Numbering` (`internal/provider/tvdb/tvdb_serie.go`), with pagination and
  per-language titles.
- The TMDB adapter consumes **episode groups** (v0.4.0): each group becomes an entry in
  `numbering`, keyed by the group NAME, capped at 8 groups, best-effort
  (`internal/provider/tmdb/tmdb_grupos.go`). The default ordering stays `official`.
- Production today has **no TheTVDB key** (`MYMEDIADB_TTDB_API_KEY` is not set), so in practice
  every series in the catalog is `official`-only. That is why the numbering of a DVD box set
  never matches.

## The gap, precisely

| Problem | Provider | This project today |
| --- | --- | --- |
| Reorder (aired / DVD / absolute / regional) | TMDB: only if a group exists. TheTVDB: yes, all types | Model ready, **TheTVDB off, TMDB groups unused** |
| File with several episodes | n/a | **Solved**: N links with `ordinal` |
| One numbered unit merging N episodes | **nobody** | **No place to store it** |

`numbering` is per episode, 1:1. There is no object whose identity is "unit 5 of season 5 of
*my* order, containing aired episodes 13, 14 and 15". So the owner has no way to say it, and
the scanner has no way to honour it.

## Proposal — the owner order, as one more ordering

### Shape

```jsonc
// core.tv@1
"owner_order": {
  "name": "HBO 2004",              // label the owner gives
  "based_on": "official",          // which order it reworks, for comparability
  "units": [
    { "season": 5, "number": 5, "title": "Blackout", "episodes": ["<id>", "<id>", "<id>"] },
    { "season": 5, "number": 6,                    "episodes": ["<id>"] }
  ],
  "provenance": "owner"
}
```

Rules that follow the existing design:

- **Anchored on episode ids**, never on numbers — a re-scrape cannot shuffle it (ADR-0021,
  rule 4).
- `episodes` is 1..N and **ordered**, mirroring the `ordinal` of the file link.
- It is **a projection like any other order**: choosing it renumbers what the UI shows and
  what the export writes. It never renames a file and never duplicates an episode.
- It lives in `extension_data` (ADR-0005) — no migration, no new table, and it can be promoted
  to a table later if it earns its own artwork or relations.
- Provenance is explicit, like the resolver: an owner order never pretends to be provider data.

### How it plugs into the scan

Resolution order for a file: **owner order → default order → absolute**. A file that matches
an owner unit with three episodes gets the three links it would get from `S01E01-E03` today —
the mechanism already exists, only the source of the `(season, number)` changes.

### Owner control in the UI

A panel on the series screen:

- lists the provider orders **read-only** (from the cached payload) so the owner can see what
  the release changed;
- "Create my own order" → pick a starting order, then reorder, and **merge selected episodes
  into one unit** (or split a unit back);
- preview of the resulting `SxxEyy` per file, before saving, so a mistake is visible.

Export then writes the owner numbering (`S05E05 - Blackout`, with the aired episodes recorded
as episodes 13–15), which is exactly what Jellyfin/Plex/Kodi cannot do for the owner today.

### Cost and risk

- No migration, no schema change: the work is the editor, the resolution order and the export.
- Risk: a unit with three episodes makes one file answer to three episodes in lists and
  counters. Decide once whether the review screen shows the **unit** or the **episodes** (my
  proposal: unit in the series view, episodes in the item view).
- Risk: changing an owner order after files are linked must not orphan links. Same rule as the
  rest of the system: the links stay anchored on episode ids, so a renumber only changes the
  projection.

## What I recommend, in order

1. **Add the TheTVDB key** (config only). The adapter is done; this unlocks `dvd`, `absolute`,
   `alternate`, `regional`, `altdvd`, `alttwo` for essentially every series, with zero code.
   Cheapest real win, and it answers "TheTVDB is more flexible" for the majority of the catalog.
2. **Consume TMDB episode groups** (one request per group, cached). Fills the gap for series
   where TheTVDB has no entry, and lets the owner *see* the orders before creating one.
3. **Owner order** — the only thing that solves the merge case. Extension data first, editor
   last, per series.

Steps 1 and 2 cover "the order is different". Only step 3 covers "three episodes became one",
because no provider can express it.
