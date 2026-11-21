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
--
-- `dismissed` is deliberately NOT reset: a hint the owner already answered must not come
-- back as a fresh question on the next scan.
INSERT INTO match_suggestion (entity_id, source, source_id, title, year, poster_url, score, query, reason, files_unmatched, files_total)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(entity_id) DO UPDATE SET
    source = excluded.source,
    source_id = excluded.source_id,
    title = excluded.title,
    year = excluded.year,
    poster_url = excluded.poster_url,
    score = excluded.score,
    query = excluded.query,
    reason = excluded.reason,
    files_unmatched = excluded.files_unmatched,
    files_total = excluded.files_total,
    created_at = datetime('now');

-- name: ObterMatchSuggestion :one
-- The hint of ONE entity (the detail screen). sql.ErrNoRows when there is none.
SELECT entity_id, source, source_id, title, year, poster_url, score, query, dismissed, created_at,
    reason, files_unmatched, files_total
FROM match_suggestion
WHERE entity_id = ?;

-- name: DeletarMatchSuggestion :exec
-- Idempotent removal: called whenever the entity gets a real identity (correct, confirm,
-- candidate applied, import) -- the hint described a state that no longer exists.
DELETE FROM match_suggestion WHERE entity_id = ?;

-- name: ListarSeriesComArvoreIncompativel :many
-- THE CENSUS BEHIND THE 'tree_mismatch' ALERT: series that DO have an identity but whose
-- FILES did not fit the tree -- at least 5 draft episodes HOLDING a present file, and at
-- least as many of them as cataloged episodes holding a file.
--
-- The unit is the FILE, not the episode. The first version counted episodes by status and
-- therefore fired on every series where the owner has a fraction of the show's episodes: a
-- library with 21 files of a 42-episode show has 21 cataloged episodes (each holding a file)
-- and 21 draft episodes (the ones the owner does not have), which satisfied
-- "provisional >= cataloged" and produced an alert on a perfectly healthy series
-- (measured 22/nov/2026: Toonsylvania, 0 draft episodes holding a file). A draft episode that
-- holds NOTHING is not evidence against the match -- it is the provider's episode that the
-- library does not have.
--
-- Why the cut (measured 19/nov/2026): with `>= 5` alone, 10 series qualified and all 10 were
-- real problems (a folder of another show, a source that numbers seasons differently);
-- loosening to "provisional >= cataloged/2" pulls in series whose extra files are legitimate
-- specials (430 matched episodes plus 290 specials), which would teach the owner to ignore the
-- warning. A false negative here costs nothing -- the series keeps accumulating drafts in the
-- review queue -- a false positive costs trust.
--
-- Series whose episodes hang DIRECTLY on the series (no season) are invisible here on
-- purpose: the review queue walks the same shape (series -> season -> episode).
-- Only series WITHOUT a row in match_suggestion are listed: an alert already on screen (or
-- already dismissed) must not be rewritten by every scan.
SELECT tv.id AS entity_id, tv.title AS title,
    CAST(SUM(CASE WHEN ep.status = 'draft' AND EXISTS (
        SELECT 1 FROM media_file_entity f
        JOIN media_file m ON m.id = f.media_file_id AND m.state = 'present'
        WHERE f.entity_id = ep.id) THEN 1 ELSE 0 END) AS INTEGER) AS provisional,
    CAST(SUM(CASE WHEN ep.status = 'cataloged' AND EXISTS (
        SELECT 1 FROM media_file_entity f
        JOIN media_file m ON m.id = f.media_file_id AND m.state = 'present'
        WHERE f.entity_id = ep.id) THEN 1 ELSE 0 END) AS INTEGER) AS cataloged
FROM entity tv
JOIN entity_relation xs ON xs.parent_id = tv.id AND xs.kind = 'tv_season'
JOIN entity_relation re ON re.parent_id = xs.child_id AND re.kind = 'tv_episode'
JOIN entity ep ON ep.id = re.child_id
WHERE tv.type = 'tv'
  AND tv.external_ids_json IS NOT NULL
  AND tv.external_ids_json NOT IN ('', '{}')
  AND NOT EXISTS (SELECT 1 FROM match_suggestion ms WHERE ms.entity_id = tv.id)
GROUP BY tv.id
HAVING SUM(CASE WHEN ep.status = 'draft' AND EXISTS (
            SELECT 1 FROM media_file_entity f
            JOIN media_file m ON m.id = f.media_file_id AND m.state = 'present'
            WHERE f.entity_id = ep.id) THEN 1 ELSE 0 END) >= 5
   AND SUM(CASE WHEN ep.status = 'draft' AND EXISTS (
            SELECT 1 FROM media_file_entity f
            JOIN media_file m ON m.id = f.media_file_id AND m.state = 'present'
            WHERE f.entity_id = ep.id) THEN 1 ELSE 0 END)
       >= SUM(CASE WHEN ep.status = 'cataloged' AND EXISTS (
            SELECT 1 FROM media_file_entity f
            JOIN media_file m ON m.id = f.media_file_id AND m.state = 'present'
            WHERE f.entity_id = ep.id) THEN 1 ELSE 0 END)
ORDER BY provisional DESC;

-- name: DeletarAlertasArvoreIncompativelObsoletos :execrows
-- A 'tree_mismatch' hint that no longer describes reality must not stay on screen. The census
-- above only WRITES, and it skips every series that already has a row -- so without this, an
-- alert whose cause was repaired (files re-placed, draft shells cleaned) would live forever and
-- teach the owner to ignore the warning.
--
-- Same measurement as the census, without its NOT EXISTS filter (this one needs the series that
-- QUALIFY, whoever they are), and only rows the owner did not answer: a dismissed alert is the
-- owner's decision and is never resurrected or removed here.
DELETE FROM match_suggestion
WHERE reason = 'tree_mismatch'
  AND dismissed = 0
  AND entity_id NOT IN (
    SELECT tv.id
    FROM entity tv
    JOIN entity_relation xs ON xs.parent_id = tv.id AND xs.kind = 'tv_season'
    JOIN entity_relation re ON re.parent_id = xs.child_id AND re.kind = 'tv_episode'
    JOIN entity ep ON ep.id = re.child_id
    GROUP BY tv.id
    HAVING SUM(CASE WHEN ep.status = 'draft' AND EXISTS (
                SELECT 1 FROM media_file_entity f
                JOIN media_file m ON m.id = f.media_file_id AND m.state = 'present'
                WHERE f.entity_id = ep.id) THEN 1 ELSE 0 END) >= 5
       AND SUM(CASE WHEN ep.status = 'draft' AND EXISTS (
                SELECT 1 FROM media_file_entity f
                JOIN media_file m ON m.id = f.media_file_id AND m.state = 'present'
                WHERE f.entity_id = ep.id) THEN 1 ELSE 0 END)
           >= SUM(CASE WHEN ep.status = 'cataloged' AND EXISTS (
                SELECT 1 FROM media_file_entity f
                JOIN media_file m ON m.id = f.media_file_id AND m.state = 'present'
                WHERE f.entity_id = ep.id) THEN 1 ELSE 0 END)
  );


-- name: DescartarMatchSuggestion :exec
-- The owner ignored the hint. The row is KEPT with dismissed=1: it tells the next screen
-- open that the question was already answered, so the sources are not queried again (and
-- the warning does not come back).
UPDATE match_suggestion SET dismissed = 1 WHERE entity_id = ?;
