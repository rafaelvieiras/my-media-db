-- +goose Up
-- Migration 0028 (typo door): the SUGGESTED MATCH.
--
-- The external sources are literal: a misspelled folder name makes the title search
-- return zero, and the item ends up as a review draft. A cheaper variant of the name (one
-- token dropped) often does find the right item -- but applying it automatically would be
-- guessing, and a wrong cataloged series is worse than a draft.
--
-- So the variant result is PERSISTED here, never applied: the item stays in review, and
-- the detail screen can show "did you mean this?" with the candidate poster and a
-- one-click correction (POST /entities/{id}/confirm-candidate, which already exists).
--
-- One row per entity (PK = entity_id): a new attempt REPLACES the previous suggestion,
-- because only the latest query round says anything about the current name on disk.
-- ON DELETE CASCADE: purging the entity purges the hint.
CREATE TABLE match_suggestion (
    entity_id  TEXT PRIMARY KEY REFERENCES entity(id) ON DELETE CASCADE,
    -- source and source_id of the CANDIDATE (not of the entity, which has none yet)
    source     TEXT NOT NULL,
    source_id  TEXT NOT NULL,
    title      TEXT NOT NULL,
    year       TEXT,
    poster_url TEXT,
    -- score [0,100] of the candidate against the name ON DISK (matching.Pontuar)
    score      INTEGER NOT NULL,
    -- the variant query that found it (observability: it explains the guess)
    query      TEXT NOT NULL,
    -- 1 when the owner dismissed the hint (DELETE /entities/{id}/match-suggestion). The row
    -- STAYS so the next screen open does not query the sources again: a dismissed hint must
    -- not cost anything, and it must not come back.
    dismissed  INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE match_suggestion;
