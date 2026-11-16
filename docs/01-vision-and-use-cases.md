# 01 — Vision and use cases

The MyMediaDB documentation is driven by **normative scenarios**: real situations
that any implementation must satisfy. If a design decision conflicts with a
scenario here, the decision is wrong, or the scenario must be revised on purpose.

The scenarios below define the **what**. The decisions about *how* live in the
other docs.

---

## Scenario A — A local title that differs from the official one

**Context:** a 1980s film aired on TV under one name, came out on VHS under
another, and sits on TMDB under the international title. The owner wants the name
that makes sense to him.

Requirements:

- **A1.** The owner imports the film from TMDB and then **edits the title**,
  without losing the original value.
- **A2.** The edit is an **override layer**: the external value stays recorded for
  reference and re-scraping, but the value the API serves is the owner's.
- **A3.** Pulling the item from TMDB again **never overwrites** the override. It
  only refreshes the external layer underneath.

## Scenario B — Your own artwork, no third-party approval

**Context:** the owner has a better poster, or artwork that fanart.tv rejected.

Requirements:

- **B1.** The owner **uploads their own artwork** and marks it as preferred for
  that item, with no external moderation.
- **B2.** External artwork and the owner's artwork coexist. The API serves the one
  the owner picked by default.
- **B3.** No custom artwork is lost when the item is re-enriched from external
  sources.

## Scenario C — Your own taxonomy (the animated short case)

**Context:** the owner wants classic animated shorts treated as **episodes of a
series** that he defines, which the TMDB philosophy refuses. Or he wants to group
films in a collection that does not exist officially.

Requirements:

- **C1.** The owner can **create entities and relations that exist in no external
  source**: a home-made series, a private collection.
- **C2.** Items without an external counterpart are first-class citizens. They are
  not "incomplete", they are **his**.
- **C3.** The structure (series → season → episode, collection → movies) is
  defined by the owner, whatever the external source says.

## Scenario D — The catalog is larger than the library

**Context:** the owner wants to record films and series he does not own yet, to
plan, to follow releases, to keep a wish list, next to the ones he owns.

Requirements:

- **D1.** An entity exists **with or without an associated file**. Having a file
  is an attribute, not a precondition.
- **D2.** The **`status`** field holds the state (cataloged, wanted, draft,
  unreleased) without inventing parallel tables.
- **D3.** The owner can list and filter "what I own", "what I want", and "drafts".

## Scenario E — Read the local library and extract file metadata

**Context:** the owner points at folders on the server. The system finds the files
and extracts the technical facts from them.

Requirements:

- **E1.** The owner registers one or more **library sources** (folders). The scan
  is **non-destructive**: it never renames or moves anything without an explicit
  order.
- **E2.** For each video file the system extracts **mediainfo** (resolution,
  codecs, HDR, audio and subtitle streams, real duration) and attaches it to the
  entity.
- **E3.** A file stays identifiable after a rename, through the content hash, so
  no duplicate record appears.

## Scenario F — Enrichment on demand (pull beats sync)

**Context:** the owner wants to complete an item with external data, when he
wants it.

Requirements:

- **F1.** Search is **local-first**: the database first, then the external source,
  with results clearly marked as **not saved**.
- **F2.** Import is **explicit**. The owner decides what to persist.
- **F3.** Nothing syncs on its own. Anything pulled is **cached locally**,
  including images through the proxy cache, so the source is not needed again.

## Scenario G — Consumed by Jellyfin, Kodi, Plex

**Context:** the owner wants his media server to read the MyMediaDB catalog instead
of calling TMDB directly.

Requirements:

- **G1.** MyMediaDB exposes a **TMDB-compatible API** under its own namespace, so
  a client that already speaks TMDB can be pointed at the server.
- **G2.** Responses reflect the owner's catalog **with the overrides applied**:
  his title, his artwork.
- **G3.** Features outside the TMDB scope are served by a separate **native API**,
  which never leaks into the compatibility layer.

## Scenario H — Multiple users with permissions

**Context:** the owner wants to give other people access, at different levels.

Requirements:

- **H1.** The owner creates **users** and issues **API tokens**.
- **H2.** Permissions are graded: read-only, read and write, admin.
- **H3.** **The owner sets the limit.** There is no quota imposed by the system.

## Scenario I — Media beyond movies and series (vision)

**Context:** the server holds media that **no catalog covers well**, and the owner
wants to catalog it with the same rigor. This is the universal scope of the
project.

Requirements:

- **I1.** The **core does not assume** that only movies and series exist. A new
  vertical **never requires a core schema change**.
- **I2.** Fields specific to a vertical live in isolated, validated **extension
  data** (see [04](04-data-model.md)).
- **I3.** In the MVP the mechanism exists in the model, but only movies and series
  ship. Extra verticals and third-party **plugins** come later (see
  [11](11-open-decisions.md)).

---

## Non-scenarios (left out on purpose)

- **Streaming, playback, and transcode.** That is the job of the media server.
- **Compulsory file reorganization.** The truth is the database, not the folder.
- **A shared public catalog or global moderation.** The data belongs to the owner.
