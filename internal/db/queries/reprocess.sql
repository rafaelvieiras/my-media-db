-- name: ContarRascunhosVazios :one
SELECT COUNT(*) AS total
FROM entity
WHERE entity.status = 'draft'
  AND NOT EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = entity.id)
  AND NOT EXISTS (SELECT 1 FROM entity_relation r WHERE r.child_id = entity.id)
  AND NOT EXISTS (SELECT 1 FROM entity_relation r WHERE r.parent_id = entity.id)
  AND NOT EXISTS (SELECT 1 FROM media_override o WHERE o.entity_id = entity.id);
