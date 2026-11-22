# 07 — Enrichment and providers

> **Status of this doc:** scoped stub. Locked decisions are listed; the deeper
> design detail lives in the [ADRs](adr/).

## Principles

- **Pull > Sync.** Nothing syncs on its own. The owner **pulls** from the external
  source when they want to ([scenario F](01-vision-and-use-cases.md)).
- **Local-first.** Searching queries the database first; only then the external
  source, whose results show up marked as **"não salvos"** until an explicit
  import.
- **Explicit import.** The owner decides what to persist. What is pulled becomes
  the `media_external` layer (base), **without touching** the override.
- **Local cache.** Pulled payloads and images are cached locally (see
  [08](08-artwork-and-assets.md)) so as not to depend on the source afterwards.

## Provider abstraction

A **provider** encapsulates an external source: search, detail fetch, mapping to
the `Entity`/`extension_data`. Providers are pluggable and declared by
**capability**, not by a fixed name.

- **Capabilities** are **optional** interfaces discovered by type assertion, on
  top of a minimal core (`Nome/Buscar/BuscarDetalhe/MapearDetalhe`):
  `SerieProvider` (season/episode tree), `SerieRicoProvider` (cross enrichment of
  the series node), `ColecaoProvider`, and future ones (`ArteProvider` for
  fanart.tv, trailer, theme). A provider that does not implement a capability is
  degraded in silence.
- **Provider independence** (principle, see [AGENTS.md](../AGENTS.md) §6): the
  media survives **any** registered provider. TMDB only → it searches and imports
  **both** movie **and** series through TMDB; TVDB only → the same through TVDB;
  both → they cross and enrich. A field one source does not model stays empty, it
  **never** blocks the vertical. Implementation state and gaps: out of scope here
  — this doc settles only the principle.
- **N-provider registry** with order = priority; multiple providers coexist and
  compose in the resolver (`override ⟵ externo ⟵ arquivo`, external layers
  ordered by priority).
- **Vision (multi-vertical):** TVDB, IMVDB (music videos), MusicBrainz (music),
  ScreenScraper (games), fanart.tv (art) and others — each one as a provider.

## The search term

What the app sends to the provider is not the raw title of the item: it is a list of
**attempts**, tried in order, stopping at the first one that reaches the auto-match
threshold ([ADR-0037](adr/0037-search-term-from-the-catalog-too.md)):

| type                             | attempts, in order                                                                                                                                                                                                                                                               |
| -------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| movie                            | the **folder name** (the convention writes the work there: as-is, then up to the subtitle dash, then without a trailing genre tag), then the **catalog title**, then the **release name** of the file and its trimmed form ([ADR-0039](adr/0039-release-name-is-not-a-title.md)) |
| series                           | the **series folder** (`tituloAnoDaPastaDaSerie`), then the entity title when the file is loose at the library root -- the name is sent WITHOUT the year, and the year is scored, never filtered ([ADR-0046](adr/0046-series-identity-is-title-year-and-shape.md))               |
| artist                           | the entity title, without a year (an artist has no file of its own, [ADR-0027](adr/0027-music-model-artist-album-track.md))                                                                                                                                                      |
| any other type with its own file | the release name of the file                                                                                                                                                                                                                                                     |

The two movie attempts are not redundant. The scan gives a movie two very different
strings — the folder (`Alligator 2 A Mutação`, `Transformers 2 - A Vingança dos
Derrotados (2011)`) and the file (`Alligator2.91.BD1080p.MemoriadaTV.Maior.mkv`) — and the
provider only recognizes the first. Measured on a production library: of 25 random movies
with no external id, **21 reach the threshold** when searched by the catalog title and
about **0** when searched by the release name. The release name is still tried second,
because a draft named `Draft` in the database has nothing better than its file.

An entity with **no file at all** has no attempts: manual drafts and orphans are never
searched on their own.

Attempts whose titles are the same once normalized (`Alligator 2: A Mutação` vs
`Alligator 2 A Mutação`) collapse into one — the provider is not asked twice for the same
text.

The **folder is the authority**: it is where the Kodi convention puts the work, and the entity
title is only a cleaned copy of it — so the raw folder name is searched before the stored copy,
and the release name of one file comes last.

What the attempts are made of matters as much as how many there are. `naming.Parse` treats a
**release name** as a title that ends where the release section begins: the title stops at the
first technical token (`WEB-DL`, `1080p`, a glued `BD1080p`, `DualÁudio`) and the year is read
from the tail of that section, including the 2-digit form older releases use (`Predador.86`).
Without that cut the search string was the whole tail with year 0 — measured, that was the case
for **46 of 67** unmatched movies in a production library. The trailing genre tag of the folder
style gets its own attempt (`101 Dálmatas O Filme` → `101 Dálmatas`), tried **last** so the
literal title always goes first.

### How the series identity is decided

The name is compared against **every title the source knows**: the translated title, the
**original** title (free in the search payload) and, for the three closest candidates only, the
alternative/translated titles (`/tv/{id}?append_to_response=translations`). A leading article is
ignored on both sides — "Clube do Terror" and "O Clube do Terror" are the same name.

The year the folder states is a **score** input with a **2-year slack**, never a filter: the
measured `Clube do Terror (1990)` is the 1992 show, so a 1-year gate would discard the only
correct candidate. A candidate outside the slack is discarded, which is what keeps a remake out.

Before anything is written, the **shape** of the tree is checked: the provider's tree must be able
to host at least **half** the folder's video files, and an entry with zero episodes is always
refused. The shape gate reads the tree the import was going to fetch anyway, so it costs no extra
request. Two candidates within 5 points of each other are a tie and go to review.

## When the match is useless

A match can succeed at the source and still be worthless locally. Three reasons reach the
owner as the same warning on the item screen
([ADR-0035](adr/0035-suggested-match-hint.md), [ADR-0038](adr/0038-match-alerts-beyond-the-typo.md)),
and none of them is ever applied automatically:

| `reason`        | what it means                                                             | how it is found                                                                                                                                 |
| --------------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`          | the name on disk finds nothing; a cheaper variant of it finds a candidate | on the first open of a series or a movie with no identity (the cheaper variants cost up to four queries, so a scan never fires them in a burst) |
| `empty_tree`    | the matched entry **has no episodes** in the source                       | when the tree is fetched: zero episodes across all seasons                                                                                      |
| `tree_mismatch` | the tree exists but **does not fit the files**                            | post-scan census: at least 5 draft episodes **holding a file**, and at least as many as cataloged episodes holding one                          |

The two tree signals come from the same component, `internal/enrich/alertas.go`, and share
the row with the typo hint (migration 0029 adds `reason` and the file counts). The
difference is what the row points at: `name` and `empty_tree` carry a **real candidate**
(another item of the same name — measured, the phantom `Aladdin` #14383 with 0 episodes has
the real `Aladdin: A Série Animada` #2745 next to it), while `tree_mismatch` initially
carries the **current identity**, because that is the thing under suspicion. The API reflects
that in `can_apply`, so the screen never offers to reapply what the item already is.

### The job that fills `tree_mismatch` with a real candidate

An alert that only says "this is wrong" is a chore; the owner asked why no job fixes the
items by itself, and the answer was that none did — the retroactive enrichment only fills
items **without** an identity, the post-scan only signals, and the media sweep only fills
what is missing. The `sugerir_identidades` job (migration 0031, off by default) closes the
gap without touching a single identity:

1. it takes the **same census** the post-scan uses, so the job and the alert cannot disagree
   about who is suspect;
2. re-scores each series with the evidence above — the name of the FOLDER (derived from one
   file of the tree, because a series has no `media_file` of its own), the year the folder
   states, and how many present files the tree holds;
3. only accepts a candidate that is **not** the identity the item already has, that reaches
   `LimiarSugestao` (80 — a suggestion is a question the owner answers, not a decision the
   app takes) and whose tree can host at least **half** those files;
4. rewrites the hint with that candidate, keeping the `reason` and the file counts the census
   had already measured. `can_apply` then comes out true by itself, because the candidate
   differs from the current identity.

Nothing is applied: the owner still clicks once per series. That keeps ADR-0038 intact and
turns 14 alerts that said "this is wrong" into 14 alerts that say which identity to use —
`Clube do Terror` → `O Clube do Terror (1992), score 100, 91 episodes for 78 files`.

**The identity is kept when the tree is empty on purpose.** Revoking it would make the next
scan try the same name, pick the same broken entry and revoke it again — a loop with a tree
fetch per scan and a series that never leaves draft. The warning is the way out, and it is
dismissible.

Measured on a production library (19/nov/2026), after enabling the retroactive enrichment:
1355 items in review, of which 1351 were episode drafts. The census behind the warning
selected **10 series**, all genuine (a folder of another show, a source that numbers seasons
differently); loosening the cut to "half as many provisional as cataloged" would pull in
series whose extra files are legitimate specials — 430 matched episodes and 290 specials.

## Credential configuration

**API key** management — never hardcoded in the code (alert in
[11](11-open-decisions.md); the IMVDB incident in the old repo). Since
[ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md), each
provider's credential is configurable through a dedicated UI
(`/dashboard/integrations`): a value written to the database takes precedence over
the bootstrap environment variable, through the new `internal/integration` package
(`Descriptor`+`Registry`, capability-optional, the same mold as
`provider.Registry`).

## Open decisions (see [11](11-open-decisions.md))

- Rate-limit/cache strategy per provider.
- ~~Universal scraper (combining sources) vs explicit choice of the source~~ →
  **closed**: single base source per item + the remaining providers enrich by
  capability (provider independence).
