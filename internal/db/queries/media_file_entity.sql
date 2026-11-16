-- Queries for the GENERIC N:M junction file<->entity (migration
-- 0009). Comments in ASCII and explicit columns -- see the note in entity.sql
-- (sqlc v1.31 corrupts query SQL with an accent). The link is anchored on the
-- entity_id (ADR-0021): renumbering the series touches no row here. The `kind`
-- discriminates the role of the link (e.g. 'tv_episode'), leaving the table
-- reusable by other media types without a new migration.

-- name: CriarVinculoEntidade :exec
-- Idempotent upsert by PK (media_file_id, entity_id, kind): re-scan of the same
-- file only updates the ordinal, never duplicates the link. Same pattern of
-- ON CONFLICT ... DO UPDATE SET used in app_settings/media_external.
INSERT INTO media_file_entity (
    media_file_id, entity_id, kind, ordinal
) VALUES (
    ?, ?, ?, ?
)
ON CONFLICT (media_file_id, entity_id, kind) DO UPDATE SET
    ordinal = excluded.ordinal;

-- name: ListarVinculosPorArquivo :many
-- The entities linked to ONE file, in local order (ordinal) -- covers the range
-- (E01-E03 becomes 3 rows with ordinal 0/1/2 under the same media_file). No kind
-- filter: the file detail shows every role.
SELECT media_file_id, entity_id, kind, ordinal
FROM media_file_entity
WHERE media_file_id = ?
ORDER BY ordinal, entity_id;

-- name: ListarArquivosDaEntidade :many
-- The files of ONE entity in a given role (episode detail: mediainfo,
-- path, state). Filters by entity_id + kind and joins with media_file to
-- bring the file row. Orders by ordinal and then by creation, stable.
SELECT mf.id, mf.entity_id, mf.path, mf.content_hash, mf.size, mf.mtime,
    mf.edition, mf.part_no, mf.match_confidence, mf.mediainfo_json, mf.state,
    mf.last_seen, mf.created_at, mf.updated_at, mf.library_source_id, mfe.ordinal
FROM media_file_entity mfe
JOIN media_file mf ON mf.id = mfe.media_file_id
WHERE mfe.entity_id = ? AND mfe.kind = ?
ORDER BY mfe.ordinal, mf.created_at, mf.id;

-- name: RemoverVinculosDoArquivo :exec
-- FULL clean re-scan: deletes every link of a file, of any kind.
-- Use with care: if the file has roles of different types, prefer the
-- per-kind variant below so a link from another plugin is not deleted.
DELETE FROM media_file_entity
WHERE media_file_id = ?;

-- name: RemoverVinculosDoArquivoPorKind :exec
-- PER-KIND clean re-scan: deletes only the links of one kind for that file,
-- before recreating them. Preferred on the re-scan of a specific type (e.g. the
-- TV scanner only touches the 'tv_episode' ones, without touching other kinds).
DELETE FROM media_file_entity
WHERE media_file_id = ? AND kind = ?;

-- name: ListarEntityIDsPorFonte :many
-- Entities reached by the media_file of ONE source (datasource path
-- rebase): directly (media_file.entity_id -- the movie case, or the
-- SERIES in the TV case) UNION through the generic junction
-- (media_file_entity.entity_id -- the EPISODE case, linked to the file only
-- through the junction, never through entity_id directly). Used to know which
-- entities to check/rebase asset.local_path and entity.nfo_path after swapping
-- library_source.path. The same parameter (library_source_id) appears in both
-- halves of the UNION.
SELECT DISTINCT mf1.entity_id
FROM media_file mf1
WHERE mf1.library_source_id = sqlc.arg('library_source_id')
UNION
SELECT DISTINCT mfe.entity_id
FROM media_file_entity mfe
JOIN media_file mf2 ON mf2.id = mfe.media_file_id
WHERE mf2.library_source_id = sqlc.arg('library_source_id');

-- name: EntidadeTemPosse :one
-- DERIVED OWNERSHIP of the entity in a role (analogous to EntityTemPosse for a movie):
-- true if, and only if, there is at least one 'present' file linked to that
-- entity in that kind.
SELECT EXISTS (
    SELECT 1
    FROM media_file_entity mfe
    JOIN media_file mf ON mf.id = mfe.media_file_id
    WHERE mfe.entity_id = ? AND mfe.kind = ? AND mf.state = 'present'
) AS tem_posse;

-- name: ContarEntidadesComPosse :one
-- BASIC aggregate ownership: how many DIRECT CHILDREN of parent_id (via
-- entity_relation) have, in the given kind, at least one 'present' file linked.
-- Serves the SEASON directly (whose direct children are episodes: "owns 12 of 13").
-- For the SERIES (two levels: series->season->episode) this counts zero, because
-- the direct children are seasons, without a link of their own -- the recursive
-- aggregation per series is left for Step 3 (reading), which refines the aggregate
-- ownership. It does NOT receive the total here (the total = number of children,
-- computed by the caller from the relations).
SELECT COUNT(DISTINCT er.child_id) AS possuidos
FROM entity_relation er
WHERE er.parent_id = ?
  AND EXISTS (
      SELECT 1
      FROM media_file_entity mfe
      JOIN media_file mf ON mf.id = mfe.media_file_id
      WHERE mfe.entity_id = er.child_id
        AND mfe.kind = ?
        AND mf.state = 'present'
  );
