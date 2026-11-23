# 0048 — The review queue requires a file

Status: accepted (23/nov/2026)

## Context

`GET /api/v1/review` listed every `draft` entity. A series that matched the provider brings its
whole catalogue — seasons, episodes, specials — and the episodes nobody has are `draft` too, so
they entered the queue. Measured on the production library: **422 items, all of them draft
episodes inside seasons of already cataloged series** (one show alone had 292 distinct specials).
They carried a group of their own, "Sem arquivo", and 75 % of a queue of 1692 items that the
owner could not act on.

## Decision

**The queue holds what the owner has.** An entity with no `media_file` at all is a hole in the
collection, not a decision, and it leaves the queue. Two consequences:

1. The `sem_arquivo` group disappears from the API, the catalog and the screen — a kind that
   cannot occur is a lie in the contract.
2. A **provisional episode** is not a card either: the identity that is missing belongs to the
   SERIES. Listing them would put 832 episodes of 16 series in the queue instead of 16 items.
   The episode stays in the series tree, marked provisional.

What is missing stays visible where it means something: the completeness of the series and the
season ("has 87/312 episodes"), and the season screen, where the episodes outside the library sit
behind a switch, muted, one line each.

## Consequences

The production queue went from **1692 to 422 items** (402 tracks, 16 series, 4 movies) — every
one of them something the owner can identify, correct or purge.

The file-existence clause is written as `EXISTS (… UNION ALL … JOIN media_file …)`. The same
EXISTS **without** that join is a SQLite trap measured here: it answers FALSE for an entity whose
file is direct on it, which hid 402 tracks from the queue.
