-- name: ContarEpisodiosDraftDaFonteDireto :one
-- Episodes in draft hanging DIRECTLY from a show with a file in the source
-- (episode -> show). Indexed joins only: the tempting single join with
-- "m.entity_id = p.id OR m.entity_id = r2.parent_id" degenerates into a cartesian
-- product (64 s measured on a 15k-file catalog). The season hop lives in the twin
-- file, and the caller sums the two.
SELECT COUNT(DISTINCT e.id) AS total
FROM entity e
JOIN entity_relation r ON r.child_id = e.id
JOIN media_file m ON m.entity_id = r.parent_id
WHERE e.type = 'episode'
  AND e.status = 'draft'
  AND m.library_source_id = sqlc.arg('source_id')
  AND m.state = 'present';
