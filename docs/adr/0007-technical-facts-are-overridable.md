# ADR-0007 — Technical facts are overridable

- **Status:** Accepted
- **Date:** 2026-07-06
- **Agenda:** [R7](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

It remained to decide whether technical facts extracted from the file
(mediainfo: codec, HDR, tracks) are untouchable or whether the owner can correct
them — e.g. a false-positive HDR from `ffprobe`.

## Decision

**Yes**, they are overridable through the same universal mechanism, with no
special rule. The detected data stays in `media_file` (non-destructive); the
override comes on top at composition time. Use as an exception, not as routine.

## Consequences

- No field is "armoured"; the owner's authority principle has no exception.
- The original detected data stays preserved for re-derivation.
- Reinforces the universality of the override
  ([ADR-0001](0001-resolver-partition-by-origin.md)).
