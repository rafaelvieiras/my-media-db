-- Queries for the DUPLICATE MERGE on external_id conflict:
-- when the owner writes an external id that already belongs to ANOTHER entity (of the
-- same type), instead of leaving two entities with the same identity, the system merges
-- the "loser" (the one that already had the id) into the "survivor" (the one being
-- edited) -- IF the loser has no owner investment (same predicate as the
-- orphan queue, see review.sql). Comments in ASCII and explicit columns --
-- see the note in entity.sql.
--
-- The sqlc SQL parser (sqlite engine) does not accept an alias on the target table of an
-- UPDATE nor a correlated subquery against it (ambiguous self-join) -- hence the
-- queries here are deliberately SIMPLE (update by PK/id, no subquery); the
-- decision "does an equivalent row already exist on the survivor?" is made in GO (the
-- caller lists the rows on both sides with ListarXPorEntity/ParaEntity and only
-- moves what does not collide), in the same spirit as the rest of the codebase (e.g.
-- batch purge iterates ids instead of bulk SQL).

-- name: EntityTemInvestimentoDono :one
-- Same "owner investment" predicate as the orphan queue (review.sql):
-- there is an override OR a custom asset (origin='user'). Decides whether an
-- external_id collision can be merged automatically (no investment) or requires
-- a conflict reported to the owner (with investment).
SELECT
    EXISTS (SELECT 1 FROM media_override o WHERE o.entity_id = ?)
    OR EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = ? AND a.origin = 'user')
    AS tem_investimento;

-- name: MoverMediaFileParaEntity :exec
-- media_file has no UNIQUE on entity_id (several files per work) -- bulk
-- UPDATE straight away, with no collision risk.
UPDATE media_file
SET entity_id = sqlc.arg('sobrevivente'), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE entity_id = sqlc.arg('perdedora');

-- name: ListarVinculosPorEntidade :many
-- media_file_entity links (series N:M, 0034) of ONE entity, so the
-- caller can compare (media_file_id, kind) of the loser against those of the
-- survivor before moving (avoids violating the composite PK).
SELECT media_file_id, entity_id, kind, ordinal
FROM media_file_entity
WHERE entity_id = ?
ORDER BY media_file_id, kind;

-- name: MoverMediaFileEntityLinha :exec
-- Moves ONE row (identified by the old PK) to the survivor entity.
-- Called only when Go has already confirmed the survivor does not have that
-- (media_file_id, kind) yet.
UPDATE media_file_entity
SET entity_id = sqlc.arg('sobrevivente')
WHERE media_file_id = sqlc.arg('media_file_id')
  AND entity_id = sqlc.arg('perdedora')
  AND kind = sqlc.arg('kind');

-- name: MoverMediaExternalLinha :exec
-- Moves ONE external cache row (by id) to the survivor entity. Called
-- only when the survivor has no cache for that source yet (avoids violating
-- UNIQUE(entity_id, source)).
UPDATE media_external
SET entity_id = sqlc.arg('sobrevivente')
WHERE id = sqlc.arg('id');

-- name: MoverAssetLinha :exec
-- Moves ONE asset (by id) to the survivor entity. Called only when the
-- survivor has no asset of that kind yet (avoids stacking a duplicate,
-- e.g. two posters).
UPDATE asset
SET entity_id = sqlc.arg('sobrevivente')
WHERE id = sqlc.arg('id');

-- name: MoverTodosCreditsParaEntity :exec
-- Moves ALL credits of the loser to the survivor -- called only when
-- Go has already confirmed the survivor has NONE yet (avoids mixing
-- two casts); the rest is left to ON DELETE CASCADE.
UPDATE credit
SET entity_id = sqlc.arg('sobrevivente')
WHERE entity_id = sqlc.arg('perdedora');

-- name: MoverEntityRelationParentLinha :exec
-- Moves ONE relation (by id) reassigning the PARENT side (e.g. collection->movie)
-- to the survivor. Called only when Go has already confirmed the survivor does
-- not have the same (parent,child,kind) yet (avoids violating the UNIQUE).
UPDATE entity_relation
SET parent_id = sqlc.arg('sobrevivente')
WHERE id = sqlc.arg('id');

-- name: MoverEntityRelationChildLinha :exec
-- Mirrors the one above, for the CHILD side (e.g. movie member of a collection).
UPDATE entity_relation
SET child_id = sqlc.arg('sobrevivente')
WHERE id = sqlc.arg('id');
