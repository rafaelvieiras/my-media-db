-- Queries for the CURATION QUEUES: review, orphans and the
-- confirm/purge writes. No new table -- the queues are QUERIES over entity/media_file/
-- media_override/asset (ADR-0012). Comments in ASCII and explicit columns -- see
-- the note in entity.sql.
--
-- Both lists return the SAME card shape as ListarEntityCards (id, type,
-- title, status, owned, file_count, match_confidence, has_poster): the handler
-- converts the row to the common card type and reuses the same DTO as the list.

-- name: ListarRevisao :many
-- REVIEW queue: entities with status='draft' (unidentified) OR that have a
-- media_file 'present' whose match_confidence is in (0, THRESHOLD) -- low confidence.
-- The THRESHOLD comes in as a parameter (sqlc.arg('limiar')): a single source in the
-- backend (it mirrors LIMIAR_BAIXA_CONFIANCA=70 in the front end). Orders by
-- confidence ASCENDING (worst first); drafts/without confidence go last
-- (match_confidence IS NULL = 1). Files of ONE entity: the ones pointing at it (work)
-- PLUS those linked through the media_file_entity junction (series episode -- the
-- file lives on the SERIES, the link with the episode is the junction). Same idiom as
-- ObterEntityCard/ListarEntityCards (0034): UNION ALL + COUNT(DISTINCT ...) on the
-- junction, with no double counting (the direct link and the junction one point at
-- different entities). Without this, EVERY episode came with file_count 0 and an empty
-- path, and fell into the "no file" group of the UI even with the file present --
-- 19 episodes measured in production. match_confidence still comes only from the
-- DIRECT file (parity with the card: it does not change the queue ordering).
SELECT
    e.id, e.type, e.title, e.status,
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
    -- path: path of ONE file of the entity ('present' comes before 'missing' in the
    -- ORDER BY DESC), used only to group the queue in the UI -- an annotation on the
    -- server, it does not change the queue predicate. Empty string for an entity
    -- with no file at all (imported item).
    CAST(COALESCE((SELECT p FROM (
        SELECT m.path AS p, m.state AS st FROM media_file m WHERE m.entity_id = e.id
        UNION ALL
        SELECT mf.path, mf.state FROM media_file_entity mfe JOIN media_file mf ON mf.id = mfe.media_file_id
            WHERE mfe.entity_id = e.id
    ) ORDER BY st DESC, p LIMIT 1), '') AS TEXT) AS path
FROM entity e
WHERE e.status = 'draft'
   OR EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id
          AND m.state = 'present'
          AND m.match_confidence > 0
          AND m.match_confidence < sqlc.arg('limiar')
   )
ORDER BY (match_confidence IS NULL), match_confidence ASC, e.title COLLATE NOCASE ASC, e.id;

-- name: ListarOrfas :many
-- ORPHAN queue: entities auto-created by the scanner (status='cataloged') that HAVE at
-- least one media_file, with NONE 'present' (all 'missing'), and NO owner
-- investment -- no row in media_override and no asset origin='user'. An entity only
-- imported (without media_file) is NOT an orphan. owned is always false here (no present).
SELECT
    e.id, e.type, e.title, e.status,
    EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') AS owned,
    (SELECT COUNT(*) FROM media_file m WHERE m.entity_id = e.id) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster
FROM entity e
WHERE e.status = 'cataloged'
  AND EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id)
  AND NOT EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present')
  AND NOT EXISTS (SELECT 1 FROM media_override o WHERE o.entity_id = e.id)
  AND NOT EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.origin = 'user')
ORDER BY e.title COLLATE NOCASE ASC, e.id;

-- name: AtualizarConfidencePorEntity :exec
-- Confirm: the owner says the guess is right. Raises the confidence
-- of ALL media_file of the entity to the given value (100 = owner authority),
-- making the item leave the review queue. Does not touch override/artwork.
UPDATE media_file
SET match_confidence = sqlc.arg('match_confidence'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE entity_id = sqlc.arg('entity_id');

-- name: ListarElegiveisEnriquecimentoRetroativo :many
-- Complement of the WHERE of ListarRevisao, restricted to TOP-LEVEL types (ADR-0030):
-- entities WITHOUT any external_id (external_ids_json IS NULL) that are NOT
-- in the Review queue (status != 'draft', without a low-confidence media_file)
-- AND that have at least one 'present' media_file -- directly (movie/tv/any
-- type with a file of its own) OR through the entity_relation tree (music:
-- the artist never links media_file directly, ADR-0027 -- only the track does; same
-- WITH RECURSIVE pattern already used by ListarEntitiesMusicaComFaixaPossuida,
-- media_file.sql). The type NOT IN below is the LITERAL list of the same values
-- as tiposSubNo (ADR-0030, internal/server/entities.go) --
-- sqlc.slice('sub_nos') was tried and discarded: sqlc, in the sqlite
-- dialect, does not combine sqlc.slice with other sqlc.arg in the SAME
-- statement -- the dynamic expansion of the slice's `?` misaligns the numbering of
-- the named placeholders (?N) of the other args, causing a bind error at
-- runtime ("missing named argument"/"datatype mismatch"), even with each arg
-- referenced a single time. TestListaSubNosSincronizadaComQuery
-- (retroativo_test.go) locks the literal list against tiposSubNo -- if a new type
-- enters it, the test fails until this query is updated by hand.
-- Throttle: never attempted (NULL) has priority, then the one attempted longest ago
-- first; sqlc.arg('dias_global') is the GLOBAL interval in days,
-- used as a fallback when no 'present' media_file of the entity
-- comes from a library with an override (the override subquery only looks at
-- DIRECT media_file -- for an artist this always falls back to the global value,
-- see the "Out of scope" section of ADR-0027); sqlc.arg('limite') prunes the batch
-- per execution.
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT e.id, e.type, e.title, e.release_date, e.last_external_match_attempt_at
FROM entity e
WHERE e.external_ids_json IS NULL
  AND e.type NOT IN ('season', 'episode', 'album', 'faixa')
  AND e.status != 'draft'
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
        UNION
        SELECT 1 FROM arvore t
        JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
        WHERE t.ancestor_id = e.id
      )
  AND NOT EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id
          AND m.state = 'present'
          AND m.match_confidence > 0
          AND m.match_confidence < sqlc.arg('limiar')
      )
  AND (
        e.last_external_match_attempt_at IS NULL
        OR e.last_external_match_attempt_at < COALESCE(
            (SELECT datetime('now', '-' || MIN(COALESCE(ls.retroactive_enrichment_days, CAST(sqlc.arg('dias_global') AS INTEGER))) || ' days')
             FROM media_file mf
             JOIN library_source ls ON ls.id = mf.library_source_id
             WHERE mf.entity_id = e.id AND mf.state = 'present'),
            datetime('now', '-' || CAST(sqlc.arg('dias_global') AS INTEGER) || ' days')
        )
      )
ORDER BY
    (e.last_external_match_attempt_at IS NULL) DESC,
    e.last_external_match_attempt_at ASC,
    e.title COLLATE NOCASE ASC,
    e.id
LIMIT sqlc.arg('limite');

-- name: ListarElegiveisReprocessamentoArtes :many
-- Complement of ListarElegiveisEnriquecimentoRetroativo: instead of entities
-- WITHOUT identity, here they are the ALREADY IDENTIFIED ones (external_ids_json
-- NOT NULL) whose art inventory (fanart.tv etc.) was never
-- (re)verified -- closes the gap of an entity that gained its identity BEFORE
-- an ArteProvider existed/was configured in the system (or whose art search at
-- the time found nothing and was never reprocessed): without this step it stays without
-- extra art forever, only reachable through the manual "Download Missing Artwork"
-- button per item. Reuses last_external_match_attempt_at --
-- FREE for these entities, since the sibling query (above) only looks at entities with
-- external_ids_json IS NULL -- except that here the timestamp means "the last time the
-- art inventory was verified", not "the last match attempt". Same
-- throttle/days (retroactive_enrichment_days) and same literal list of top-level
-- types (sub_nos EXCLUDED, ADR-0030 -- TestListaSubNosSincronizadaComQuery
-- locks BOTH queries against tiposSubNo) and ownership through the entity_relation tree
-- (music: the artist never links media_file directly, ADR-0027) that the sibling query
-- already uses.
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT e.id, e.type, e.title
FROM entity e
WHERE e.external_ids_json IS NOT NULL
  AND e.type NOT IN ('season', 'episode', 'album', 'faixa')
  AND e.status != 'draft'
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
        UNION
        SELECT 1 FROM arvore t
        JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
        WHERE t.ancestor_id = e.id
      )
  AND (
        e.last_external_match_attempt_at IS NULL
        OR e.last_external_match_attempt_at < COALESCE(
            (SELECT datetime('now', '-' || MIN(COALESCE(ls.retroactive_enrichment_days, CAST(sqlc.arg('dias_global') AS INTEGER))) || ' days')
             FROM media_file mf
             JOIN library_source ls ON ls.id = mf.library_source_id
             WHERE mf.entity_id = e.id AND mf.state = 'present'),
            datetime('now', '-' || CAST(sqlc.arg('dias_global') AS INTEGER) || ' days')
        )
      )
ORDER BY
    (e.last_external_match_attempt_at IS NULL) DESC,
    e.last_external_match_attempt_at ASC,
    e.title COLLATE NOCASE ASC,
    e.id
LIMIT sqlc.arg('limite');

-- name: AtualizarTentativaMatchExterno :exec
-- Records that the item WAS verified in this execution (applied, flagged
-- or with no candidate) -- the throttle: the next batch only comes back to
-- consider this id after sqlc.arg('desde') (above) has passed.
UPDATE entity
SET last_external_match_attempt_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg('id');

-- name: PromoverDeDraft :exec
-- Curation: a draft (unidentified) that the owner CONFIRMS or
-- CORRECTS stops being a draft -- it becomes 'cataloged' and leaves the review queue
-- through the status branch (otherwise the confidence rises to 100 but the item stays
-- stuck as a draft). Guarded by status='draft': idempotent no-op in any other state (it
-- does not demote 'wanted'/'unreleased' nor touch an already cataloged item).
UPDATE entity
SET status = 'cataloged',
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg('id') AND status = 'draft';

-- name: ListarFaixasDraftOrfas :many
-- Complement of ListarElegiveisReprocessamentoArtes: tracks
-- type='faixa' status='draft' with a present media_file (entity_id points
-- directly, ADR-0027 is 1:1) WITHOUT entity_relation as a child of any album (NOT
-- EXISTS child_id) -- unlike the other eligibility queries in this
-- file, here faixa/tv are NOT excluded as "sub_nos": they are exactly the
-- universe this query covers. Same throttle by
-- library_source.retroactive_enrichment_days with a fallback to dias_global,
-- same logic as ListarElegiveisReprocessamentoArtes.
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT e.id, e.type
FROM entity e
WHERE e.type = 'faixa'
  AND e.status = 'draft'
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
      )
  AND NOT EXISTS (
        SELECT 1 FROM entity_relation er WHERE er.child_id = e.id
      )
  AND (
        e.last_external_match_attempt_at IS NULL
        OR e.last_external_match_attempt_at < COALESCE(
            (SELECT datetime('now', '-' || MIN(COALESCE(ls.retroactive_enrichment_days, CAST(sqlc.arg('dias_global') AS INTEGER))) || ' days')
             FROM media_file mf
             JOIN library_source ls ON ls.id = mf.library_source_id
             WHERE mf.entity_id = e.id AND mf.state = 'present'),
            datetime('now', '-' || CAST(sqlc.arg('dias_global') AS INTEGER) || ' days')
        )
      )
ORDER BY
    (e.last_external_match_attempt_at IS NULL) DESC,
    e.last_external_match_attempt_at ASC,
    e.id
LIMIT sqlc.arg('limite');

-- name: ListarSeriesDraftComArquivo :many
-- Complement of ListarFaixasDraftOrfas: series type='tv'
-- status='draft' with a present media_file pointing directly (entity_id =
-- serie.id, gravarArquivoSerie always writes it that way, even under review). Same
-- throttle as the sibling query above.
SELECT e.id, e.type
FROM entity e
WHERE e.type = 'tv'
  AND e.status = 'draft'
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
      )
  AND (
        e.last_external_match_attempt_at IS NULL
        OR e.last_external_match_attempt_at < COALESCE(
            (SELECT datetime('now', '-' || MIN(COALESCE(ls.retroactive_enrichment_days, CAST(sqlc.arg('dias_global') AS INTEGER))) || ' days')
             FROM media_file mf
             JOIN library_source ls ON ls.id = mf.library_source_id
             WHERE mf.entity_id = e.id AND mf.state = 'present'),
            datetime('now', '-' || CAST(sqlc.arg('dias_global') AS INTEGER) || ' days')
        )
      )
ORDER BY
    (e.last_external_match_attempt_at IS NULL) DESC,
    e.last_external_match_attempt_at ASC,
    e.id
LIMIT sqlc.arg('limite');

-- == Post-scan per library (ADR-0031) ==
-- The three queries below are the PER-library_source versions of the
-- reconciliation queries above. The catalog day throttle
-- (retroactive_enrichment_days/dias_global) does NOT apply -- the post-scan is the retry
-- of the scan the owner has just asked for --, but there is a floor of its own, in
-- hours: without it, every scan retried the SAME 200 tracks that
-- had just failed (measured: 45+ min of MusicBrainz per scan, without resolving
-- anything new). The order also became the one of those NEVER attempted
-- first, so the queue moves forward on every scan even with the item/hour ceiling.
-- The timestamp is written in ISO with 'T' and 'Z' (strftime '%Y-%m-%dT%H:%M:%fZ') and
-- the cutoff is datetime() with a space; comparing the two raw strings is wrong because
-- of the 'T' (0x54 > 0x20), so the column goes wrapped in datetime() -- which
-- normalizes both to the same format.

-- name: ListarFaixasDraftOrfasPorSource :many
-- Tracks type='faixa' status='draft' with a present media_file FROM the given SOURCE
-- and without entity_relation as a child of an album (same universe as
-- ListarFaixasDraftOrfas). Post-scan throttle of its own: it skips those already
-- attempted in the last sqlc.arg('horas_minimas') hours; the order
-- is "never attempted first" so the queue moves forward on every scan.
SELECT e.id, e.type
FROM entity e
WHERE e.type = 'faixa'
  AND e.status = 'draft'
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
          AND m.library_source_id = sqlc.arg('source_id')
      )
  AND NOT EXISTS (
        SELECT 1 FROM entity_relation er WHERE er.child_id = e.id
      )
  AND (
        e.last_external_match_attempt_at IS NULL
        OR datetime(e.last_external_match_attempt_at) < datetime('now', '-' || CAST(sqlc.arg('horas_minimas') AS INTEGER) || ' hours')
      )
ORDER BY (e.last_external_match_attempt_at IS NULL) DESC, e.last_external_match_attempt_at ASC, e.id
LIMIT sqlc.arg('limite');

-- name: ListarSeriesDraftPorSource :many
-- Series type='tv' status='draft' with a present media_file FROM the given SOURCE
-- (same universe as ListarSeriesDraftComArquivo, with the hour throttle and the
-- post-scan order).
SELECT e.id, e.type
FROM entity e
WHERE e.type = 'tv'
  AND e.status = 'draft'
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
          AND m.library_source_id = sqlc.arg('source_id')
      )
  AND (
        e.last_external_match_attempt_at IS NULL
        OR datetime(e.last_external_match_attempt_at) < datetime('now', '-' || CAST(sqlc.arg('horas_minimas') AS INTEGER) || ' hours')
      )
ORDER BY (e.last_external_match_attempt_at IS NULL) DESC, e.last_external_match_attempt_at ASC, e.id
LIMIT sqlc.arg('limite');

-- name: ListarFilmesPendentesPorSource :many
-- Movies (type='movie') FROM the given SOURCE that have no external identity yet
-- and whose best match stayed below the low-confidence threshold -- the universe that
-- the post-scan auto-match retries (real case: an item that came in raw because
-- of the 0089 bug, Nov 8, 2026).
SELECT e.id, e.type
FROM entity e
WHERE e.type = 'movie'
  AND (e.external_ids_json IS NULL OR e.external_ids_json NOT LIKE '%tmdb%')
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
          AND m.library_source_id = sqlc.arg('source_id')
      )
ORDER BY e.id
LIMIT sqlc.arg('limite');
