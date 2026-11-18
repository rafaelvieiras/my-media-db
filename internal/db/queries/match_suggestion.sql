-- Queries for the SUGGESTED MATCH (migration 0028): the "did you mean this?" hint the
-- scanner persists when the literal title search finds nothing and a cheaper variant of
-- the name (one token dropped) does. It is NEVER applied automatically -- it only feeds
-- the warning on the item screen, whose action is the existing
-- POST /entities/{id}/confirm-candidate.
--
-- Comments in ASCII and explicit columns -- see the note in entity.sql.

-- name: UpsertMatchSuggestion :exec
-- One row per entity: a new attempt REPLACES the hint (the latest query round is the only
-- one that says anything about the name currently on disk).
INSERT INTO match_suggestion (entity_id, source, source_id, title, year, poster_url, score, query)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(entity_id) DO UPDATE SET
    source = excluded.source,
    source_id = excluded.source_id,
    title = excluded.title,
    year = excluded.year,
    poster_url = excluded.poster_url,
    score = excluded.score,
    query = excluded.query,
    created_at = datetime('now');

-- name: ObterMatchSuggestion :one
-- The hint of ONE entity (the detail screen). sql.ErrNoRows when there is none.
SELECT entity_id, source, source_id, title, year, poster_url, score, query, dismissed, created_at
FROM match_suggestion
WHERE entity_id = ?;

-- name: DeletarMatchSuggestion :exec
-- Idempotent removal: called whenever the entity gets a real identity (correct, confirm,
-- candidate applied, import) -- the hint described a state that no longer exists.
DELETE FROM match_suggestion WHERE entity_id = ?;

-- name: DescartarMatchSuggestion :exec
-- The owner ignored the hint. The row is KEPT with dismissed=1: it tells the next screen
-- open that the question was already answered, so the sources are not queried again (and
-- the warning does not come back).
UPDATE match_suggestion SET dismissed = 1 WHERE entity_id = ?;
