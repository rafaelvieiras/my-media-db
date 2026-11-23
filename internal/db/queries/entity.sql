-- Minimal CRUD for the base entity.
--
-- IMPORTANT (sqlc + ASCII): the comments in this .sql file are ASCII (no
-- accents) ON PURPOSE. sqlc v1.31 slices the text of each query by BYTE
-- offset, but token positions come as RUNE offsets; any multi-byte
-- character (pt-BR accent) in a query file shifts the cut and CORRUPTS the
-- generated SQL (the trailing '?' disappears, 'AS x' becomes 'A', etc.). Migrations do NOT
-- suffer from this (they are parsed by structure, not stored as text), so there the
-- comments stay in accented pt-BR. Here, ASCII. General rule: columns
-- always EXPLICIT, never '*'/'RETURNING *' (the '*' expansion has the same
-- offset bug). The generated columns (external_id_tmdb/imdb/tvdb/mbid) derive from
-- external_ids_json and do NOT enter the INSERT/UPDATE, but they do appear in the
-- RETURNING/SELECT to mirror the model (migration 0007 added tvdb;
-- migration 0023 added mbid -- NOTE: 0023 had been left without
-- regenerating sqlc for a while, fixed along with this change).

-- name: CriarEntity :one
-- status does NOT enter the INSERT: it uses DEFAULT 'cataloged' (an auto-created item is
-- born cataloged; curation changes it later via AtualizarEntity). ADR-0009.
INSERT INTO entity (
    id, type, title, original_title, description, release_date,
    tags_json, external_ids_json, extension_data_json
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid;

-- name: ObterEntity :one
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE id = ?;

-- name: ObterTituloEntity :one
-- Just the title, by id: the midias_faltantes job narrates the entity it is on
-- (ADR-0040, issue #69) and the whole row would be waste for a label.
SELECT title FROM entity WHERE id = ?;

-- name: ObterEntityPorTmdb :one
-- Lookup by external tmdb id via generated column + index (doc 04, search without GIN).
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_tmdb = ?;

-- name: ObterEntityPorTvdb :one
-- Lookup by external tvdb id via generated column + index (migration 0007, ADR-0021).
-- Mirrors ObterEntityPorTmdb: series exclusive to TheTVDB (no tmdb/
-- imdb counterpart, e.g. Looney Tunes) dedupe through this path on import.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_tvdb = ?;

-- name: ObterEntityPorMbid :one
-- Lookup by external mbid (MusicBrainz) via generated column + index (ADR-0027
-- item 6). Mirrors ObterEntityPorTmdb: used by the music vertical import
-- for idempotent dedupe of artist/album/track by MBID.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_mbid = ?;

-- name: ObterEntityPorTmdbTipo :one
-- TYPE-SCOPED lookup by tmdb id. TMDB episode ids and series ids live in the SAME
-- numeric space (measured: a series and an episode of another series both answer to id
-- 13943), so an unscoped lookup lets an episode import
-- reuse the series entity -- exactly the corruption that turned a series into an
-- episode-shaped entity holding 314 files.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_tmdb = ? AND type = ?;

-- name: ObterEntityPorImdbTipo :one
-- TYPE-SCOPED lookup by imdb id, same reasoning as ObterEntityPorTmdbTipo: imdb
-- episodes and titles share the tt space.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_imdb = ? AND type = ?;

-- name: ObterEntityPorTvdbTipo :one
-- TYPE-SCOPED lookup by tvdb id, same reasoning as ObterEntityPorTmdbTipo.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_tvdb = ? AND type = ?;

-- name: ListarEntitiesPorTmdbExcetoId :many
-- Mirrors ObterEntityPorTmdb, but returns ALL entities with that id (except
-- alvo_id) instead of :one -- fixes the bug where already corrupted data
-- (two entities with the SAME external_id_tmdb, pre-existing before the
-- automatic merge) made :one return the target entity itself (with no guaranteed
-- ORDER BY) and mask the other duplicate. It excludes alvo_id in the SQL itself, so
-- the caller does not rely on "at most one result in total".
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_tmdb = sqlc.arg('tmdb_id') AND id != sqlc.arg('alvo_id');

-- name: ListarEntitiesPorImdbExcetoId :many
-- Mirrors ListarEntitiesPorTmdbExcetoId, for the imdb source.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_imdb = sqlc.arg('imdb_id') AND id != sqlc.arg('alvo_id');

-- name: ListarEntitiesPorTvdbExcetoId :many
-- Mirrors ListarEntitiesPorTmdbExcetoId, for the tvdb source.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_tvdb = sqlc.arg('tvdb_id') AND id != sqlc.arg('alvo_id');

-- name: ListarEntitiesPorMbidExcetoId :many
-- Mirrors ListarEntitiesPorTmdbExcetoId, for the mbid source (MusicBrainz) -- used by
-- acharColisaoExterna when auto-match/backfill applies a musicbrainz candidate to
-- an `artista` entity (music vertical, ADR-0027): without this query, collision
-- dedupe stayed limited to tmdb/imdb/tvdb and an artist already cataloged another
-- way (tree import, track scan) could get a SECOND entity for the same MBID.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_mbid = sqlc.arg('mbid_id') AND id != sqlc.arg('alvo_id');

-- name: ObterColecaoPorTmdb :one
-- Dedupe of the COLLECTION entity by tmdb id: reuses the generated column
-- external_id_tmdb, but RESTRICTED to type='collection'. The collection id
-- namespace differs from the movie one, yet the generated column is shared; the type filter
-- avoids matching a same-named movie of the same numeric id. Re-import stays idempotent.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE type = 'collection' AND external_id_tmdb = ?
LIMIT 1;

-- name: ListarCardsDeEpisodiosDaSerie :many
-- CARDS of ALL episodes of a series in ONE query, for GET /entities/{id}/episodes and
-- for the ordering annotation of the detail page (ADR-0021/0033).
--
-- Why it exists: ObterEntityCard is meant for small N and recomputes the transitive
-- closure of the relation tree ON EVERY CALL (the recursive CTE of `arvore`). A series
-- with 430 episodes therefore paid 430 closures plus 430 single-row reads, which took
-- MINUTES on a library served from a network share. The cards come whole, with the
-- extension_data the projection needs, so the server does not walk the episodes again.
--
-- Tree ownership (the recursive arm of ObterEntityCard) is deliberately ABSENT: an
-- episode has no descendants, so that arm could only ever match its own files, and the
-- junction arm below already covers them (media_file_entity is how an episode owns a
-- file, see ADR-0006). The other columns mirror ObterEntityCard.
SELECT
    e.id, e.type, e.title, e.status, e.release_date, e.extension_data_json,
    EXISTS (
        SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present'
        UNION ALL
        SELECT 1 FROM media_file_entity mfe JOIN media_file mf ON mf.id = mfe.media_file_id
            WHERE mfe.entity_id = e.id AND mf.state = 'present'
    ) AS owned,
    (SELECT COUNT(*) FROM (
        SELECT m.id FROM media_file m WHERE m.entity_id = e.id
        UNION ALL
        SELECT DISTINCT mfe.media_file_id FROM media_file_entity mfe WHERE mfe.entity_id = e.id
    )) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster,
    ecc.complete AS completeness_complete,
    ecc.missing_json AS completeness_missing_json
FROM entity e
JOIN entity_relation ep ON ep.child_id = e.id AND ep.kind = 'tv_episode'
JOIN entity_relation se ON se.child_id = ep.parent_id AND se.kind = 'tv_season'
LEFT JOIN entity_catalog_completeness ecc ON ecc.entity_id = e.id
WHERE se.parent_id = ?;

-- name: ObterEntityCard :one
-- CARD for ONE entity (same derived columns as ListarEntityCards) + release_date
-- for ordering. Used by the MEMBERS of a collection and by the episodes
-- of a season (0034): the child list comes from entity_relation and each child becomes
-- a card through here (small N). The derived columns mirror ListarEntityCards
-- (owned/file_count/match_confidence/has_poster), so the mapping reuses the cardDTO.
-- owned/file_count ALSO count the media_file_entity junction -- see the long note in
-- ListarEntityCards (the episode card owns a file only through the junction).
-- completeness_complete/completeness_missing_json come from the persisted CACHE
-- entity_catalog_completeness (recomputed in batch by the
-- saude_biblioteca job) via LEFT JOIN -- NULL when the item has not been
-- evaluated yet (new item, or draft).
-- Tree ownership (entity_relation): beyond the DIRECT file and the
-- media_file_entity junction (SIBLING ownership, see the long note in ListarEntityCards),
-- it also adds any DESCENDANT in the entity_relation tree (recursive CTE
-- parent_id->child_id) that has a 'present' media_file -- covers music
-- artist/album (ADR-0027/0029), whose only link to the file lives in the TRACK, two
-- levels below, without EVER using media_file_entity for a track. Generic to any
-- depth and to any relation kind (not hardcoded to music).
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT
    e.id, e.type, e.title, e.status, e.release_date,
    EXISTS (
        SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present'
        UNION ALL
        SELECT 1 FROM media_file_entity mfe JOIN media_file mf ON mf.id = mfe.media_file_id
            WHERE mfe.entity_id = e.id AND mf.state = 'present'
        UNION ALL
        SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
            WHERE t.ancestor_id = e.id
    ) AS owned,
    (SELECT COUNT(*) FROM (
        SELECT m.id FROM media_file m WHERE m.entity_id = e.id
        UNION ALL
        SELECT DISTINCT mfe.media_file_id FROM media_file_entity mfe WHERE mfe.entity_id = e.id
    )) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster,
    ecc.complete AS completeness_complete,
    ecc.missing_json AS completeness_missing_json
FROM entity e
LEFT JOIN entity_catalog_completeness ecc ON ecc.entity_id = e.id
WHERE e.id = ?;

-- name: ListarCardsDeMembros :many
-- CARDS of ALL children of one entity, for ONE relation kind, in ONE query -- the
-- batch that replaces the ObterEntityCard loop of montarMembros (GET
-- /entities/{id}/members, the season page). Why it exists: ObterEntityCard
-- materializes the transitive closure of the WHOLE relation tree on every call, so a
-- season with 296 episodes paid 296 closures -- 219 s on a library served from a
-- network share, measured (issue #89).
--
-- The recursion is ANCHORED AT THE CHILDREN of this parent (same fix as
-- ObterCaminhoDescendentePresente, issue #73): the tree arm only ever answers
-- "does this child have a descendant with a file?", which is what a season card needs
-- (its episodes) and what an episode card cannot use. Columns mirror ObterEntityCard
-- exactly -- same file_count rule (direct file + junction, NO tree), same completeness
-- cache, plus extension_data_json so the caller projects the episode numbering without
-- a second read per episode.
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT r.parent_id, r.child_id FROM entity_relation r
    WHERE r.parent_id = sqlc.arg('parent_id') AND r.kind = sqlc.arg('kind')
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT
    e.id, e.type, e.title, e.status, e.release_date, e.extension_data_json,
    EXISTS (
        SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present'
        UNION ALL
        SELECT 1 FROM media_file_entity mfe JOIN media_file mf ON mf.id = mfe.media_file_id
            WHERE mfe.entity_id = e.id AND mf.state = 'present'
        UNION ALL
        SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
            WHERE t.ancestor_id = e.id
    ) AS owned,
    (SELECT COUNT(*) FROM (
        SELECT m.id FROM media_file m WHERE m.entity_id = e.id
        UNION ALL
        SELECT DISTINCT mfe.media_file_id FROM media_file_entity mfe WHERE mfe.entity_id = e.id
    )) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster,
    ecc.complete AS completeness_complete,
    ecc.missing_json AS completeness_missing_json
FROM entity_relation r
JOIN entity e ON e.id = r.child_id
LEFT JOIN entity_catalog_completeness ecc ON ecc.entity_id = e.id
WHERE r.parent_id = sqlc.arg('parent_id') AND r.kind = sqlc.arg('kind')
ORDER BY r.sort_index IS NULL, r.sort_index, r.id;

-- name: ObterEntityPorImdb :one
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE external_id_imdb = ?;

-- name: ObterEntityPorTituloAno :one
-- Scanner dedupe without an external id: reuses the SAME entity for the
-- same (type, title, release_date). Uses `IS` (not `=`) to match a NULL
-- release_date when the name carries no year. Keeps the re-scan idempotent for works
-- without an id embedded in the name (does not duplicate the entity).
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE type = ? AND title = ? AND release_date IS ?
LIMIT 1;

-- name: ObterEntityPorTituloTipo :one
-- Series scanner lookup: the series already in the catalog by the
-- (type, title) pair, ignoring release_date. Used when the series folder carries no
-- embedded external id, to reuse the already imported series and avoid a new
-- round-trip to the provider on every scan. First by created_at (stable).
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
WHERE type = ? AND title = ?
ORDER BY created_at, id
LIMIT 1;

-- name: ListarEntities :many
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid
FROM entity
ORDER BY created_at DESC, id
LIMIT ? OFFSET ?;

-- name: ContarEntities :one
-- Total of entities in the table -- the denominator of the progress bar of the
-- completeness sweep (RecalcularCompletudeCatalogacao, internal/server). The sweep
-- walks ListarEntities (every type and every status, drafts included: it reports the
-- POSITION it is on, and skips the draft afterwards), so the count must be the whole
-- table, not the non-draft subset.
SELECT COUNT(*) AS total FROM entity;

-- name: ListarEntityCards :many
-- List of CARDS for the UI, ordered by title ASCENDING. A
-- single query with correlated-subselect aggregation -- avoids N+1 (no
-- per-item ownership/count query). Derived columns:
--   owned            DERIVED OWNERSHIP (ADR-0009): media_file present EXISTS
--                    DIRECTLY (media_file.entity_id = e.id) OR through the
--                    media_file_entity junction (see below).
--   file_count       total media_file of the entity: the DIRECT ones (present + missing)
--                    PLUS those linked through the media_file_entity junction.
--     match_confidence highest match confidence among the files (review badge).
--     has_poster       EXISTS of an asset kind='poster' (the UI decides the poster_url).
-- GENERIC ownership through the junction (0034): an episode file has
-- media_file.entity_id pointing at the SERIES (the work) and links to the EPISODE only
-- through media_file_entity. If the card ownership looked only at the direct entity_id,
-- the episode would show up as owned=false/file_count=0 while the detail and the
-- aggregated ownership (which already read the junction) show it owned -- inconsistent.
-- That is why owned/file_count ALSO count the junction. It is agnostic to kind
-- (any entity linked through media_file_entity counts; a future music 'track' likewise).
-- It does NOT duplicate: the direct link and the junction one point at DIFFERENT
-- entity ids (the series carries the file through entity_id; the episode carries it
-- through the junction) -- in a single card only one of the two paths matches e.id.
-- For the movie, nothing changes. COUNT(DISTINCT media_file_id) avoids double counting.
-- Only TOP-LEVEL WORKS come in: season/episode are internal nodes of the series
-- tree (0032), reached only through the parent tree (GET /members); they stay OUT of
-- the home/search via `type NOT IN ('season','episode')`. movie/tv/collection too.
-- Optional filters (NULL = no filter) via sqlc.narg: type and owned. The ordering
-- by title in DESC order lives in a sibling query (ListarEntityCardsDesc) because
-- sqlc does not bind parameters inside ORDER BY expressions -- dynamic direction
-- in a single statement is not supported; the handler chooses which query to run.
-- completeness_complete/completeness_missing_json come from the persisted CACHE
-- entity_catalog_completeness (recomputed in batch by the
-- saude_biblioteca job) via LEFT JOIN -- NULL when the item has not been
-- evaluated yet (new item, or draft). It is a simple JOIN (no
-- aggregation needed): the cache row already comes ready, 1:1 with the entity.
-- Tree ownership (entity_relation) added both to the `owned` column
-- and to the `owned` FILTER -- see the long note in the ASC version/ObterEntityCard.
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT
    e.id, e.type, e.title, e.status,
    EXISTS (
        SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present'
        UNION ALL
        SELECT 1 FROM media_file_entity mfe JOIN media_file mf ON mf.id = mfe.media_file_id
            WHERE mfe.entity_id = e.id AND mf.state = 'present'
        UNION ALL
        SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
            WHERE t.ancestor_id = e.id
    ) AS owned,
    (SELECT COUNT(*) FROM (
        SELECT m.id FROM media_file m WHERE m.entity_id = e.id
        UNION ALL
        SELECT DISTINCT mfe.media_file_id FROM media_file_entity mfe WHERE mfe.entity_id = e.id
    )) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster,
    ecc.complete AS completeness_complete,
    ecc.missing_json AS completeness_missing_json
FROM entity e
LEFT JOIN entity_catalog_completeness ecc ON ecc.entity_id = e.id
WHERE e.type NOT IN ('season', 'episode')
  AND (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR (
            EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present')
            OR EXISTS (SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
                WHERE t.ancestor_id = e.id)
          ) = sqlc.narg('owned'))
ORDER BY e.title COLLATE NOCASE ASC, e.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListarEntityCardsDesc :many
-- Same as ListarEntityCards, but orders by title DESCENDING (sort=-title). owned/
-- file_count add the media_file_entity junction the same way -- see the long note above.
-- Tree ownership (entity_relation) added both to the `owned` column
-- and to the `owned` FILTER -- see the long note in the ASC version/ObterEntityCard.
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT
    e.id, e.type, e.title, e.status,
    EXISTS (
        SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present'
        UNION ALL
        SELECT 1 FROM media_file_entity mfe JOIN media_file mf ON mf.id = mfe.media_file_id
            WHERE mfe.entity_id = e.id AND mf.state = 'present'
        UNION ALL
        SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
            WHERE t.ancestor_id = e.id
    ) AS owned,
    (SELECT COUNT(*) FROM (
        SELECT m.id FROM media_file m WHERE m.entity_id = e.id
        UNION ALL
        SELECT DISTINCT mfe.media_file_id FROM media_file_entity mfe WHERE mfe.entity_id = e.id
    )) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster,
    ecc.complete AS completeness_complete,
    ecc.missing_json AS completeness_missing_json
FROM entity e
LEFT JOIN entity_catalog_completeness ecc ON ecc.entity_id = e.id
WHERE e.type NOT IN ('season', 'episode')
  AND (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR (
            EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present')
            OR EXISTS (SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
                WHERE t.ancestor_id = e.id)
          ) = sqlc.narg('owned'))
ORDER BY e.title COLLATE NOCASE DESC, e.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListarEntityCardsRecentes :many
-- Same as ListarEntityCards, but orders by created_at DESCENDING
-- (sort=-created_at): "recently added" on the
-- home. owned/file_count add the media_file_entity junction the same way --
-- see the long note in ListarEntityCards. Only the DESC variant exists (most
-- recent first); there is no request for "oldest first". Same LIMIT/OFFSET
-- pagination shape as the title siblings -- the real consumer (home
-- carousel) always uses a small limit (e.g. 10), with no need for a
-- dedicated pagination shape.
-- Tree ownership (entity_relation) added both to the `owned` column
-- and to the `owned` FILTER -- see the long note in the ASC version/ObterEntityCard. It is
-- what makes the artist "recently added" carousel (owned=true) return
-- results even when the artist never had a DIRECT media_file nor one through the junction.
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT
    e.id, e.type, e.title, e.status,
    EXISTS (
        SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present'
        UNION ALL
        SELECT 1 FROM media_file_entity mfe JOIN media_file mf ON mf.id = mfe.media_file_id
            WHERE mfe.entity_id = e.id AND mf.state = 'present'
        UNION ALL
        SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
            WHERE t.ancestor_id = e.id
    ) AS owned,
    (SELECT COUNT(*) FROM (
        SELECT m.id FROM media_file m WHERE m.entity_id = e.id
        UNION ALL
        SELECT DISTINCT mfe.media_file_id FROM media_file_entity mfe WHERE mfe.entity_id = e.id
    )) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster,
    ecc.complete AS completeness_complete,
    ecc.missing_json AS completeness_missing_json
FROM entity e
LEFT JOIN entity_catalog_completeness ecc ON ecc.entity_id = e.id
WHERE e.type NOT IN ('season', 'episode')
  AND (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR (
            EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present')
            OR EXISTS (SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
                WHERE t.ancestor_id = e.id)
          ) = sqlc.narg('owned'))
ORDER BY e.created_at DESC, e.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListarTiposComContagem :many
-- DISTINCT entity types with at least 1 CATALOGED item (status !=
-- 'draft'), so the home (0109) and the navbar dropdown (0110) discover
-- which schemas to show without hardcoding a fixed list in the frontend -- covers
-- album/artista/faixa (0105-0107) along with movie/tv without requiring a change here for
-- each new vertical. Criterion = "status != 'draft'" (the SAME one of the review
-- queue in review.sql: a 'draft' item is just a search result not yet
-- committed to the catalog) -- DELIBERATELY distinct from "owned"
-- (ContarEntityCards/ListarEntityCards: PHYSICAL file ownership). An item
-- 'wanted'/'unreleased' cataloged but without a file on disk still counts --
-- consistent with AGENTS.md #2 ("the database is bigger than the library": it catalogs
-- what you have AND what you do not have yet). It excludes the SUB-NODES (internal nodes of
-- the series/music tree, see ListarEntityCards/0032) received via
-- sub_nos -- the CALLER passes the single list tiposSubNo (ADR-0030,
-- internal/server/entities.go), no longer a fixed literal here.
-- Ordered by count DESCENDING (most populated schema first) and type
-- ASCENDING as a stable tiebreaker.
SELECT type, COUNT(*) AS count
FROM entity
WHERE type NOT IN (sqlc.slice('sub_nos'))
  AND status != 'draft'
GROUP BY type
ORDER BY count DESC, type ASC;

-- name: ContarEntityCards :one
-- Total of items matching the filters (for the `total` field of the pagination). Same
-- WHERE clause as ListarEntityCards -- without LIMIT/OFFSET/ORDER BY. It includes the same
-- sub-node exclusion (season/episode) so the total matches the page (0032).
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT COUNT(*) AS total
FROM entity e
WHERE e.type NOT IN ('season', 'episode')
  AND (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR (
            EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present')
            OR EXISTS (SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
                WHERE t.ancestor_id = e.id)
          ) = sqlc.narg('owned'));

-- name: AtualizarEntity :one
UPDATE entity
SET type = ?,
    title = ?,
    original_title = ?,
    description = ?,
    release_date = ?,
    status = ?,
    tags_json = ?,
    external_ids_json = ?,
    extension_data_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb, last_external_match_attempt_at, nfo_path, nfo_written_at,
    watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds, external_id_mbid;

-- name: AtualizarEpisodioBase :exec
-- Rewrites the episode BASE on re-import: the find-or-create by
-- external id reuses the row, but before only create wrote these fields, so new still_url/
-- numbering (extension_data) and updated title/synopsis/date/ids from the provider
-- never reached an already imported episode. Here we reassert the source facts on the
-- base row. It does NOT touch status/tags/original_title nor media_override: the resolution
-- applies the override on top, so refreshing the base is safe (the override still wins).
UPDATE entity
SET title = ?,
    description = ?,
    release_date = ?,
    external_ids_json = ?,
    extension_data_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarTemporadaBase :exec
-- Rewrites the season BASE on re-import: title, date and the
-- extension_data core.tv@1 (ordering + season_number + year). Symmetric to
-- AtualizarEpisodioBase, without description (a season keeps no synopsis on the
-- row). It does NOT touch status/tags/original_title nor media_override -- the
-- resolution applies the override on top; refreshing the base is safe.
UPDATE entity
SET title = ?,
    release_date = ?,
    extension_data_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarExternalIds :exec
-- Merges the external identity (MD4): the app reads external_ids_json, joins
-- {source: source_id} and writes it here, without touching the rest of the entity. The
-- generated columns external_id_tmdb/imdb re-derive on their own from the new JSON.
-- Used by enrich/import -- media_external is a cache subordinated to it.
UPDATE entity
SET external_ids_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarTitulo :exec
-- Repairs the base title of an auto-created/imported item when it stayed as the
-- numeric fallback (source id) -- the regression of the "number instead of the name". Only
-- the title and updated_at change; it does not touch override/external (the owner's
-- authority still wins in the resolution). Called by enrich (self-healing) under a strict
-- condition (title == sourceID), never over an already readable title.
UPDATE entity
SET title = sqlc.arg('title'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg('id');

-- name: AtualizarExtensionData :exec
-- Writes the extension_data_json (namespace->object map) of an entity, without touching the
-- rest. Used by the series import: the series is created/enriched
-- as a common item (importarItem) and receives here the core.tv@1 namespace (season
-- types). Season/episode are already born with the extension_data in CriarEntity itself;
-- the series needs this UPDATE because it is born through the item path. Only this
-- column and updated_at change; it does not touch override/external.
UPDATE entity
SET extension_data_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarOriginalLanguage :exec
-- Writes the ANCHOR of the per-language fallback: the NEUTRAL code of the
-- original language of the work, obtained from the source detail on enrich/import.
-- Non-destructive: it is a fact from the source, not the owner's data; re-enriching only
-- reasserts it. Only this column and updated_at change.
UPDATE entity
SET original_language = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarReleaseDate :exec
-- Writes the neutral RELEASE DATE of the work, obtained from the source detail on enrich/
-- import (same handling as AtualizarOriginalLanguage: a fact of the source, not the
-- owner's data; re-enriching reasserts it). Denormalizes the base column for cheap
-- sorting/listing (e.g. ordering the members of a collection by year) without resolving
-- each item. Only this column and updated_at change; resolution still uses base=external.
UPDATE entity
SET release_date = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: ApagarRascunhoVazio :execrows
-- Purge of the REPLACED draft. The reconciliation re-links the
-- media_file to the definitive entity and the old draft entity was left behind without
-- a file and without a link -- it kept showing up in the review queue
-- forever (114 cases measured in production on Nov 8, 2026, group "no file").
-- The four conditions are the safety lock: only the draft that has no
-- file, has no link at all (neither as a child nor as a parent -- a draft
-- series with episodes linked stays) and has no owner override leaves. The draft
-- that the reconciliation replaces is created by the matcher ITSELF (title derived from the
-- file, without an external id) -- there is no owner data there, unlike
-- principle 5, which protects what the owner wrote. Idempotent: 0 rows when
-- nothing matches.
DELETE FROM entity
WHERE entity.id = sqlc.arg('id')
  AND entity.status = 'draft'
  AND NOT EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = entity.id)
  AND NOT EXISTS (SELECT 1 FROM entity_relation r WHERE r.child_id = entity.id)
  AND NOT EXISTS (SELECT 1 FROM entity_relation r WHERE r.parent_id = entity.id)
  AND NOT EXISTS (SELECT 1 FROM media_override o WHERE o.entity_id = entity.id);

-- name: ApagarRascunhosVazios :execrows
-- Batch version of the purge above, for the post-scan sweep:
-- it cleans the stock of empty drafts that the reconciliation left before this
-- fix. Same locks, without an id.
DELETE FROM entity
WHERE entity.status = 'draft'
  AND NOT EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = entity.id)
  AND NOT EXISTS (SELECT 1 FROM entity_relation r WHERE r.child_id = entity.id)
  AND NOT EXISTS (SELECT 1 FROM entity_relation r WHERE r.parent_id = entity.id)
  AND NOT EXISTS (SELECT 1 FROM media_override o WHERE o.entity_id = entity.id);

-- name: ApagarEntity :exec
-- Explicit purge: removes the entity and, via ON DELETE CASCADE, its media_file,
-- asset and entity_relation.
DELETE FROM entity
WHERE id = ?;

-- name: AtualizarEntityNfo :exec
-- Writes the path of the last exported .nfo + the write timestamp.
-- Called on every POST /entities/{id}/nfo, even when idempotent
-- in content (nfo_written_at always changes).
UPDATE entity
SET nfo_path = ?,
    nfo_written_at = ?
WHERE id = ?;

-- name: AtualizarEntityNfoPath :exec
-- Rebases ONLY the path of the already exported .nfo (editing the
-- datasource path): unlike AtualizarEntityNfo, it does NOT touch nfo_written_at --
-- this is not a new export, only the path prefix moved location on
-- the disk (the mount/HD changed, the content is the same).
UPDATE entity
SET nfo_path = ?
WHERE id = ?;

-- name: PreencherCamposLocaisReproducaoSeAusentes :exec
-- Fills the LOCAL playback fields ONLY where they are still
-- NULL (COALESCE) -- never overwrites an already present value (same
-- spirit of "the owner's override wins"/"non-destructive", AGENTS.md). Called
-- by the endpoint POST /entities/{id}/refresh-files after reading the .nfo on disk
-- (internal/localmedia.ExtrairCamposUsuario); parameters absent from the .nfo
-- come in as NULL/zero-value, the COALESCE takes care of the rest.
UPDATE entity SET
    watched = COALESCE(watched, ?),
    play_count = COALESCE(play_count, ?),
    user_rating = COALESCE(user_rating, ?),
    last_played_at = COALESCE(last_played_at, ?),
    resume_position_seconds = COALESCE(resume_position_seconds, ?),
    resume_total_seconds = COALESCE(resume_total_seconds, ?)
WHERE id = ?;

-- name: UpsertEntityCatalogCompleteness :exec
-- Writes (or replaces) the result of the CATALOG completeness evaluation
-- of ONE item -- called by the batch recalculation
-- (RecalcularCompletudeCatalogacao, saude_biblioteca job). A conflict on the PK
-- (entity_id) updates complete/missing_json and restamps evaluated_at --
-- never creates a second row for the same entity (1:1, same pattern as
-- UpsertAppSetting).
INSERT INTO entity_catalog_completeness (
    entity_id, complete, missing_json
) VALUES (
    sqlc.arg('entity_id'), sqlc.arg('complete'), sqlc.arg('missing_json')
)
ON CONFLICT (entity_id) DO UPDATE SET
    complete = excluded.complete,
    missing_json = excluded.missing_json,
    evaluated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: LimparNfoDaEntity :exec
-- Clears the exported-NFO memory of ONE entity. Called when the IDENTITY changes: the .nfo
-- still on disk describes the previous show (showtitle, season, episode of the OTHER tree),
-- and the completeness measure kept saying "complete" because the columns were filled -- so
-- no job ever rewrote it. Clearing makes the item incomplete again, which is exactly what
-- puts it back in the missing-media sweep. The tree below a series does not need it: a
-- changed identity DISCARDS the tree and imports a new one, whose rows are born without NFO.
UPDATE entity
SET nfo_path = NULL,
    nfo_written_at = NULL
WHERE id = sqlc.arg('id');
