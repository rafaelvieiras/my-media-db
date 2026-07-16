-- CRUD de asset (poster/fanart/... referenciados por uma entity).
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: CriarAsset :one
INSERT INTO asset (
    id, entity_id, kind, origin, path_or_uri, meta_json
) VALUES (
    ?, ?, ?, ?, ?, ?
)
RETURNING id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal;

-- name: ObterAsset :one
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal
FROM asset
WHERE id = ?;

-- name: ListarAssetsPorEntity :many
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal
FROM asset
WHERE entity_id = ?
ORDER BY created_at, id;

-- name: ObterAssetPosterPorEntity :one
-- Poster local da entity (briefing 0010): o primeiro asset kind='poster'. Usado
-- pelo endpoint que serve o binario. Sem poster -> sql.ErrNoRows -> 404 na API.
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal
FROM asset
WHERE entity_id = ? AND kind = 'poster'
ORDER BY created_at, id
LIMIT 1;

-- name: ObterAssetPorEntityKindOrigin :one
-- Asset de UMA origem especifica (ex.: kind='poster', origin='external'), para o
-- dedup do cache de imagem (briefing 0015): reusa/atualiza o asset em vez de
-- duplicar. Filtrar por origin garante que o cache externo NUNCA case um asset
-- origin='user' (arte do dono, intocavel). Sem linha -> sql.ErrNoRows.
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal
FROM asset
WHERE entity_id = ? AND kind = ? AND origin = ?
ORDER BY created_at, id
LIMIT 1;

-- name: AtualizarAssetPath :exec
-- Atualiza o caminho/binario de um asset ja existente (dedup do cache externo:
-- re-baixar o poster reaproveita a mesma linha). Nao muda entity_id/kind/origin.
UPDATE asset
SET path_or_uri = ?,
    meta_json = ?
WHERE id = ?;

-- name: AtualizarAssetMeta :exec
-- Atualiza SO o meta_json de um asset (marca/desmarca a preferida do dono,
-- briefing 0017). Nao toca entity_id/kind/origin/path_or_uri. Usada ao (des)marcar
-- preferida: o merge preservando as demais chaves do meta_json e feito em Go.
UPDATE asset
SET meta_json = ?
WHERE id = ?;

-- name: ApagarAsset :exec
DELETE FROM asset
WHERE id = ?;

-- name: AtualizarAssetLocalPath :exec
-- Grava o caminho FISICO materializado na pasta real da biblioteca (briefing
-- 0062): coluna ortogonal a path_or_uri (cache interno/URL externa) -- so
-- preenchida quando a arte tambem foi copiada para dentro da pasta do item.
UPDATE asset
SET local_path = ?
WHERE id = ?;

-- name: MarcarAssetPrincipal :exec
-- Seta is_principal=1 no asset (fonte de verdade em coluna, briefing 0062).
-- O chamador ZERA o antigo principal do mesmo (entity,kind) numa query
-- separada (DesmarcarAssetsPrincipalPorKind) antes de chamar esta.
UPDATE asset
SET is_principal = 1
WHERE id = ?;

-- name: DesmarcarAssetsPrincipalPorKind :exec
-- Zera is_principal de TODOS os assets do (entity,kind) -- aplica a
-- invariante "no maximo um principal por kind" (briefing 0062) em codigo:
-- sempre chamada ANTES de MarcarAssetPrincipal, na mesma sequencia de
-- escrita (pool de escritor unico, ADR-0010 -- sem concorrencia entre as
-- duas chamadas).
UPDATE asset
SET is_principal = 0
WHERE entity_id = ? AND kind = ?;
