-- Access to the credit table (migration 0010): the work<->person/
-- company edge, re-composable cache of the external payload. Re-enrichment deletes the
-- credits of (entity_id, source) and repopulates them (idempotent, no duplication). The
-- owner's data never lives here. Comments in ASCII and explicit columns -- see the
-- note in entity.sql (sqlc v1.31 corrupts query SQL with an accent).

-- name: CriarCredito :exec
-- Inserts a credit linked to the entity. person_id (cast/crew) OR company_id (company)
-- is filled in, the other stays NULL. ordering carries the cast billing order;
-- character/department/job/episode_count are the attributes of the edge.
INSERT INTO credit (
    id, entity_id, person_id, company_id, kind,
    department, job, character, ordering, episode_count,
    source, source_id
) VALUES (
    ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?
);

-- name: ApagarCreditosDaEntityFonte :exec
-- Deletes all credits of an entity coming from ONE source, before repopulating on
-- re-enrichment. Per source (not total) so it does not delete credits from another
-- source that enriched the same work (e.g. series with a TVDB tree + TMDB cast).
DELETE FROM credit
WHERE entity_id = ? AND source = ?;

-- name: ListarCreditosDaEntity :many
-- All credits of a work, for the detail. Orders by kind, billing order and
-- id (stable).
SELECT id, entity_id, person_id, company_id, kind,
    department, job, character, ordering, episode_count,
    source, source_id
FROM credit
WHERE entity_id = ?
ORDER BY kind, ordering, id;

-- name: ListarFilmografiaDaPessoa :many
-- Person filmography (internal-first): the works in the catalog
-- that credit them, cast OR crew (a production company credit has no person_id, it
-- drops out of the WHERE naturally). Joins credit with entity to bring the role (kind/
-- character/job/department/ordering) of that credit in that work; the handler fetches
-- title/poster/year of each entity via ObterEntityCard (same card as the list,
-- without reinventing resolution). Orders by type, billing order and id (stable).
SELECT
    e.id AS entity_id, e.type AS entity_type,
    c.kind, c.character, c.job, c.department, c.ordering
FROM credit c
JOIN entity e ON e.id = c.entity_id
WHERE c.person_id = ?
ORDER BY e.type, c.ordering, e.id;
