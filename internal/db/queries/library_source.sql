-- CRUD for library_source (scan roots).
-- Comments in ASCII and explicit columns -- see the note in entity.sql.

-- name: CriarLibrarySource :one
-- state and last_scan stay out of the INSERT: a new source is born 'online'
-- (DEFAULT) and without last_scan; the scan fills it later via AtualizarLibrarySource.
-- content_type goes in explicitly (0034): 'movie' | 'series' -- the scanner branches
-- on this value; the service normalizes empty to 'movie' (compat). watch_enabled
-- stays out of the INSERT too: it is born 0 (DEFAULT) -- the owner turns it on
-- later via PATCH. preferred_language (0084) goes in explicitly, NULLABLE -- the
-- service passes NULL when the owner did not pick a preferred language at registration.
-- retroactive_enrichment_days (0085) goes in explicitly, NULLABLE -- the service
-- passes NULL when the owner did not set a retry override at registration.
INSERT INTO library_source (
    id, path, type, content_type, preferred_language, retroactive_enrichment_days
) VALUES (
    ?, ?, ?, ?, ?, ?
)
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days;

-- name: ObterLibrarySource :one
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days
FROM library_source
WHERE id = ?;

-- name: ObterLibrarySourcePorPath :one
-- Used by the POST handler to detect a duplicate path (already normalized) before
-- the INSERT and answer 409 with a clear message instead of blowing up on the UNIQUE.
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days
FROM library_source
WHERE path = ?;

-- name: ListarLibrarySources :many
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days
FROM library_source
ORDER BY created_at, id;

-- name: ListarLibrarySourcesComWatchHabilitado :many
-- Used at server BOOT to register the fsnotify watcher
-- for each source with watch_enabled=1 -- avoids listing/filtering in Go over ALL
-- the sources just to find the few with watch turned on.
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days
FROM library_source
WHERE watch_enabled = 1
ORDER BY created_at, id;

-- name: AtualizarLibrarySource :one
-- Updates the scan state: timestamp of the last scan and online/offline.
UPDATE library_source
SET type = ?,
    last_scan = ?,
    state = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days;

-- name: AtualizarLibrarySourceWatch :one
-- PATCH /api/v1/library/{id}: turns watch_enabled on/off. The
-- handler calls the fsnotify watcher register/unregister routine RIGHT
-- AFTER this UPDATE (applied hot, without restart -- see internal/watch).
UPDATE library_source
SET watch_enabled = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days;

-- name: AtualizarLibrarySourcePath :one
-- PATCH /api/v1/library/{id}: repoints the datasource to a
-- new path (e.g. the disk/mount moved). The CALLER rebases in
-- cascade media_file.path/asset.local_path/entity.nfo_path IN THE SAME
-- transaction, right after this UPDATE (same pattern as Apagar). It does not touch
-- `state`: it is always re-evaluated on read (avaliarState), never read from this
-- column.
UPDATE library_source
SET path = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days;

-- name: AtualizarLibrarySourcePreferredLanguage :one
-- PATCH /api/v1/library/{id}: sets or clears the preferred
-- search/display language of the source. An explicit NULL clears it (back to the
-- global language). The HANDLER (internal/server/library.go) has already validated
-- the code against app_settings.languages.allowed before calling this query.
UPDATE library_source
SET preferred_language = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days;

-- name: AtualizarLibrarySourceRetroactiveEnrichmentDays :one
-- PATCH /api/v1/library/{id}: sets or clears the retry
-- interval override for the source's retroactive enrichment. An explicit
-- NULL clears it (back to the global value). The HANDLER
-- (internal/server/library.go) has already validated the interval (1-365) before
-- calling this query.
UPDATE library_source
SET retroactive_enrichment_days = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled, preferred_language, retroactive_enrichment_days;

-- name: ApagarLibrarySource :exec
DELETE FROM library_source
WHERE id = ?;
