-- CRUD de media_file + a query de POSSE DERIVADA (ADR-0009) + as queries do
-- scanner (briefing 0009): dedup por content_hash, reencontro (rename/move) e
-- marcacao de missing por datasource.
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: CriarMediaFile :one
INSERT INTO media_file (
    id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, library_source_id
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at, library_source_id;

-- name: ObterMediaFile :one
SELECT id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at, library_source_id
FROM media_file
WHERE id = ?;

-- name: ObterMediaFilePorPath :one
-- Scan incremental (briefing 0064): busca o media_file ja gravado para este
-- MESMO path (UNIQUE, indexado) ANTES de re-hashear/re-extrair mediainfo --
-- se mtime+size baterem com o que ja esta aqui, o scanner reusa content_hash/
-- mediainfo_json em vez de reabrir o arquivo. sql.ErrNoRows para arquivo novo
-- (nunca visto neste path).
SELECT id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at, library_source_id
FROM media_file
WHERE path = ?;

-- name: ObterMediaFilePorContentHash :one
-- Dedup do scanner: acha o arquivo pelo hash de conteudo (estavel a rename/move).
-- content_hash NAO e UNIQUE (colisao real e astronomica com o hash parcial), por
-- isso LIMIT 1. Se achar, o scanner atualiza o path em vez de criar outra linha.
SELECT id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at, library_source_id
FROM media_file
WHERE content_hash = ?
LIMIT 1;

-- name: ListarMediaFilesPorEntity :many
SELECT id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at, library_source_id
FROM media_file
WHERE entity_id = ?
ORDER BY created_at, id;

-- name: AtualizarMediaFileState :one
-- Transicao de ciclo de vida: marca present/missing e carimba last_seen.
UPDATE media_file
SET state = ?,
    last_seen = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at, library_source_id;

-- name: AtualizarMediaFileVisto :one
-- Reencontro num scan: o arquivo foi achado pelo content_hash. Atualiza o path
-- (cobre rename/move -- idempotente, nao cria outra linha), o size/mtime, volta
-- para present e carimba last_seen com o timestamp do scan (usado depois para
-- decidir quem virou missing). Fixa tambem a fonte de origem.
UPDATE media_file
SET path = ?,
    size = ?,
    mtime = ?,
    state = 'present',
    last_seen = ?,
    library_source_id = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at, library_source_id;

-- name: MarcarMediaFilesNaoVistosMissing :execrows
-- Fecho do ciclo de vida (ADR-0012): ao concluir um scan COMPLETO com a fonte
-- acessivel, todo media_file 'present' daquela fonte cujo last_seen NAO e o
-- carimbo deste scan (ou seja, nao foi revisto) passa a 'missing'. A entity e
-- preservada; a posse cai. Escopo por library_source_id garante que um datasource
-- offline (que nem chega aqui) nao afete arquivos de outras fontes.
UPDATE media_file
SET state = 'missing',
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE library_source_id = ?
  AND state = 'present'
  AND last_seen IS NOT ?;

-- name: ApagarMediaFile :exec
DELETE FROM media_file
WHERE id = ?;

-- name: ApagarMediaFilesPorFonte :exec
-- Apaga todos os arquivos de uma fonte (usada ao remover o datasource, 0035).
-- O cascade de media_file_entity (migration 0009, ON DELETE CASCADE em
-- media_file_id) limpa os vinculos junto; a entity do catalogo e preservada.
DELETE FROM media_file
WHERE library_source_id = ?;

-- name: ListarMediaFilesPresentes :many
-- Todos os media_file 'present' (de QUALQUER entity) -- usada por
-- EhPastaExclusiva (internal/localmedia, briefing 0062) para decidir se uma
-- pasta tem 1 unico video (nome de arte bare) ou varios (nome prefixado).
-- Biblioteca self-hosted: full scan tolerado (mesmo espirito de
-- montarPosseAgregada, internal/server/entities.go).
SELECT id, entity_id, path, state
FROM media_file
WHERE state = 'present';

-- name: ListarEntityIDsComMediaFilePresente :many
-- Universo elegivel do job `midias_faltantes` (briefing 0064): toda entity
-- que possui ao menos um media_file 'present'. Para SERIE, media_file.entity_id
-- aponta pra SERIE (nunca season/episodio, ver internal/scanner/serie.go
-- gravarArquivoSerie) -- entao season/episodio nunca aparecem aqui, o que casa
-- com a regra de writeEntityNfo (season nao tem NFO proprio, internal/server/
-- localmedia_handlers.go).
SELECT DISTINCT entity_id
FROM media_file
WHERE state = 'present'
ORDER BY entity_id;

-- name: EntityTemPosse :one
-- POSSE DERIVADA (ADR-0009): verdadeiro se, e so se, existe ao menos um
-- media_file 'present' para a entity. Posse NAO e coluna (uma coluna gerada nao
-- enxerga outra tabela) -- e computada aqui.
SELECT EXISTS (
    SELECT 1 FROM media_file
    WHERE entity_id = ? AND state = 'present'
) AS tem_posse;
