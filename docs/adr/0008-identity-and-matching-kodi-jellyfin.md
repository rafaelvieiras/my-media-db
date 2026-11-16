# ADR-0008 — Identity and matching through the Kodi/Jellyfin convention

- **Status:** Accepted
- **Date:** 2026-07-07
- **Agenda:** [MD1](../00-decision-agenda.md) · **Spec:** [06](../06-library-and-scan.md), [04](../04-data-model.md)

## Context

One point was still open: how a file becomes/associates with an `Entity` and an
external id, and how to model a film with multiple files (editions/qualities).
Inventing a rule of our own would be reinventing the wheel of a problem already
solved by the ecosystem.

## Decision

Adopt the **Kodi/Jellyfin naming convention** (de-facto standard; Emby, Plex and
the \*arr stack in the same family). Consequences: **flat 1:N granularity** (1
work = 1 `Entity`; qualities/editions/parts = `media_file`), **edition = label on
the file**, **external id embedded in the name = deterministic key**, *stacking*
of parts. Every match carries a **confidence score**: high confidence matches on
its own; low confidence matches with the best guess **+ review alert** (*"mapeado
com 60% de certeza. Remover?"*); zero results becomes an unidentified `draft`.
Music follows the same principle through MusicBrainz + tags. Verticals outside
the standard (music video→IMVDB, comics→ComicInfo) bring their own identity
through `extension_data`, without touching the core.

## Consequences

- The scan is a **reader** of the standard, never imposes it (it renames nothing —
  non-destructive).
- The owner's correction is authoritative (it comes in through the override).
- Validates [ADR-0001](0001-resolver-partition-by-origin.md)/[ADR-0005](0005-uniform-composition-through-extension-data.md):
  verticals plug in through `extension_data`.
