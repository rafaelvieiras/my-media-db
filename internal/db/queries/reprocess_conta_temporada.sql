-- name: ContarEpisodiosDraftDaFonteViaTemporada :one
-- Same count for the episode -> season -> show shape (the common one in a library
-- organized by season folders).
SELECT COUNT(DISTINCT e.id) AS total
FROM entity e
JOIN entity_relation r ON r.child_id = e.id
JOIN entity_relation r2 ON r2.child_id = r.parent_id
JOIN media_file m ON m.entity_id = r2.parent_id
WHERE e.type = 'episode'
  AND e.status = 'draft'
  AND m.library_source_id = sqlc.arg('source_id')
  AND m.state = 'present';
