-- Queries da juncao N:M GENERICA arquivo<->entidade (briefing 0034, migration
-- 0009). Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql (o
-- sqlc v1.31 corrompe SQL de query com acento). O vinculo e ancorado no entity_id
-- (ADR-0021): renumerar a serie nao mexe em nenhuma linha daqui. O `kind`
-- discrimina o papel do vinculo (ex.: 'tv_episode'), deixando a tabela reusavel
-- por outros tipos de midia sem migration nova.

-- name: CriarVinculoEntidade :exec
-- Upsert idempotente por PK (media_file_id, entity_id, kind): re-scan do mesmo
-- arquivo so atualiza o ordinal, nunca duplica o vinculo. Mesmo padrao de
-- ON CONFLICT ... DO UPDATE SET usado em app_settings/media_external.
INSERT INTO media_file_entity (
    media_file_id, entity_id, kind, ordinal
) VALUES (
    ?, ?, ?, ?
)
ON CONFLICT (media_file_id, entity_id, kind) DO UPDATE SET
    ordinal = excluded.ordinal;

-- name: ListarVinculosPorArquivo :many
-- As entidades vinculadas a UM arquivo, na ordem local (ordinal) -- cobre o range
-- (E01-E03 vira 3 linhas com ordinal 0/1/2 sob o mesmo media_file). Sem filtro de
-- kind: o detalhe do arquivo mostra todos os papeis.
SELECT media_file_id, entity_id, kind, ordinal
FROM media_file_entity
WHERE media_file_id = ?
ORDER BY ordinal, entity_id;

-- name: ListarArquivosDaEntidade :many
-- Os arquivos de UMA entidade num dado papel (detalhe do episodio: mediainfo,
-- caminho, estado). Filtra por entity_id + kind e faz join com media_file para
-- trazer a linha do arquivo. Ordena por ordinal e depois pela criacao, estavel.
SELECT mf.id, mf.entity_id, mf.path, mf.content_hash, mf.size, mf.mtime,
    mf.edition, mf.part_no, mf.match_confidence, mf.mediainfo_json, mf.state,
    mf.last_seen, mf.created_at, mf.updated_at, mf.library_source_id, mfe.ordinal
FROM media_file_entity mfe
JOIN media_file mf ON mf.id = mfe.media_file_id
WHERE mfe.entity_id = ? AND mfe.kind = ?
ORDER BY mfe.ordinal, mf.created_at, mf.id;

-- name: RemoverVinculosDoArquivo :exec
-- Re-scan limpo TOTAL: apaga todos os vinculos de um arquivo, de qualquer kind.
-- Use com cuidado: se o arquivo tem papeis de tipos diferentes, prefira a
-- variante por kind abaixo para nao apagar vinculo de outro plugin.
DELETE FROM media_file_entity
WHERE media_file_id = ?;

-- name: RemoverVinculosDoArquivoPorKind :exec
-- Re-scan limpo POR KIND: apaga so os vinculos de um kind daquele arquivo, antes
-- de recria-los. Preferida no re-scan de um tipo especifico (ex.: o scanner de TV
-- so mexe nos 'tv_episode', sem tocar vinculos de outros tipos).
DELETE FROM media_file_entity
WHERE media_file_id = ? AND kind = ?;

-- name: ListarEntityIDsPorFonte :many
-- Entities alcancadas pelos media_file de UMA fonte (briefing 0070, recasco de
-- path do datasource): direto (media_file.entity_id -- caso do filme, ou da
-- SERIE no caso de TV) UNION via a juncao generica (media_file_entity.entity_id
-- -- caso do EPISODIO, ligado ao arquivo so pela juncao, nunca pelo entity_id
-- direto). Usada para saber em quais entities checar/recascar
-- asset.local_path e entity.nfo_path apos trocar library_source.path. O mesmo
-- parametro (library_source_id) aparece nas duas metades da UNION.
SELECT DISTINCT mf1.entity_id
FROM media_file mf1
WHERE mf1.library_source_id = sqlc.arg('library_source_id')
UNION
SELECT DISTINCT mfe.entity_id
FROM media_file_entity mfe
JOIN media_file mf2 ON mf2.id = mfe.media_file_id
WHERE mf2.library_source_id = sqlc.arg('library_source_id');

-- name: EntidadeTemPosse :one
-- POSSE DERIVADA da entidade num papel (analogo ao EntityTemPosse de filme):
-- verdadeiro se, e so se, existe ao menos um arquivo 'present' vinculado aquela
-- entity naquele kind.
SELECT EXISTS (
    SELECT 1
    FROM media_file_entity mfe
    JOIN media_file mf ON mf.id = mfe.media_file_id
    WHERE mfe.entity_id = ? AND mfe.kind = ? AND mf.state = 'present'
) AS tem_posse;

-- name: ContarEntidadesComPosse :one
-- Posse agregada BASICA: quantos FILHOS DIRETOS de parent_id (via entity_relation)
-- tem, no kind dado, ao menos um arquivo 'present' vinculado. Serve direto para a
-- TEMPORADA (cujos filhos diretos sao episodios: "possui 12 de 13"). Para a SERIE
-- (dois niveis: serie->temporada->episodio) isto conta zero, porque os filhos
-- diretos sao temporadas, sem vinculo proprio -- a agregacao recursiva por serie
-- fica pro Passo 3 (leitura), que refina a posse agregada. NAO recebe o total
-- aqui (o total = numero de filhos, calculado por quem chama a partir das
-- relacoes).
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
