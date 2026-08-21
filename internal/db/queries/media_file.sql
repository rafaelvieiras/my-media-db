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

-- name: AtualizarMediaFileConteudoMudou :one
-- O path ja tinha uma linha, mas o content_hash recem-calculado NAO bate com
-- ela (arquivo substituido/reencodado no lugar, ou mtime/size do provedor
-- (ex.: rclone/Drive) sofreram drift o bastante para invalidar o
-- short-circuit de ObterMediaFilePorPath -- ver mediaFileInalteradoPorPath em
-- internal/scanner/scan.go). Sem esta query, gravarArquivo cairia num INSERT
-- que bate no UNIQUE(path) da linha ja existente. Atualiza TODOS os campos
-- derivados do conteudo (hash/tamanho/mtime/mediainfo/edicao/parte/
-- confianca), preservando id/entity_id -- mesmo espirito de
-- AtualizarMediaFileVisto, mas o gatilho aqui e o PATH bater, nao o hash.
UPDATE media_file
SET content_hash = ?,
    size = ?,
    mtime = ?,
    edition = ?,
    part_no = ?,
    match_confidence = ?,
    mediainfo_json = ?,
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

-- name: ListarMediaFilePathsPorFonte :many
-- Recasco de path do datasource (briefing 0070): so as duas colunas que o
-- rename precisa (o resto da linha nao muda). O chamador troca o PREFIXO em
-- Go (strings.TrimPrefix) -- nunca SUBSTR/REPLACE no SQL, que divergiria em
-- paths com acento (SQLite conta codepoint, Go conta byte).
SELECT id, path
FROM media_file
WHERE library_source_id = ?;

-- name: AtualizarMediaFilePath :exec
-- Grava o path ja recascado (calculado em Go) de UM media_file. Nao toca o
-- resto da linha (content_hash/entity_id/state seguem intactos -- o arquivo
-- fisico e o MESMO, so mudou de onde e montado).
UPDATE media_file
SET path = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

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
-- Universo elegivel do job `midias_faltantes` (briefing 0064) PARA MOVIE/TV
-- DIRETO: toda entity que possui ao menos um media_file 'present' apontando
-- entity_id pra ela. Para SERIE, media_file.entity_id aponta pra SERIE (nunca
-- season/episodio, ver internal/scanner/serie.go gravarArquivoSerie) -- entao
-- esta query sozinha nao alcanca season/episodio (o vinculo deles e via a
-- juncao media_file_entity, kind='tv_episode'). Desde o briefing 0081, o
-- chamador (ListarEntitiesComMediaFilePresente, internal/server/midias_
-- faltantes.go) combina o resultado desta query com
-- ListarSeasonIDsComEpisodioPresente/ListarEpisodeIDsComMediaFilePresente
-- abaixo -- season continua sem NFO proprio (guarda em gravarNFOSeFaltante),
-- mas episodio passa a ser elegivel (NFO) e season tambem (poster de
-- temporada).
SELECT DISTINCT entity_id
FROM media_file
WHERE state = 'present'
ORDER BY entity_id;

-- name: ListarSeasonIDsComEpisodioPresente :many
-- Temporadas elegiveis para o job `midias_faltantes` (briefing 0081): toda
-- season com ao menos um episodio DESCENDENTE com media_file 'present'. O
-- vinculo serie->season->episodio e entity_relation (kind='tv_episode', ver
-- internal/enrich/serie.go relacaoEpisodio -- er.parent_id=season,
-- er.child_id=episodio); o vinculo episodio<->arquivo e a juncao generica
-- media_file_entity (mesmo kind, ADR-0021). Season continua sem NFO proprio
-- (guarda em gravarNFOSeFaltante) -- esta query so habilita o job a
-- materializar o POSTER de temporada (season0N-poster.jpg) na raiz da serie.
SELECT DISTINCT er.parent_id AS entity_id
FROM entity_relation er
WHERE er.kind = 'tv_episode'
  AND EXISTS (
      SELECT 1
      FROM media_file_entity mfe
      JOIN media_file mf ON mf.id = mfe.media_file_id
      WHERE mfe.entity_id = er.child_id
        AND mfe.kind = 'tv_episode'
        AND mf.state = 'present'
  )
ORDER BY entity_id;

-- name: ListarEpisodeIDsComMediaFilePresente :many
-- Episodios elegiveis para o job `midias_faltantes` (briefing 0081): todo
-- episodio com media_file 'present' vinculado via a juncao generica
-- media_file_entity (kind='tv_episode') -- media_file.entity_id aponta pra
-- SERIE, nunca pro episodio (ver ListarEntityIDsComMediaFilePresente acima),
-- entao esta query cobre o caminho que a outra nao alcanca. Habilita o job a
-- gravar o NFO do episodio (kind ja suportado por nomeRaizNFO/raizEpisode,
-- so nunca era alcancado); arte de episodio (still) continua FORA
-- (kindsMidiasFaltantes nao ganha still -- lazy/manual, briefing 0044).
SELECT DISTINCT mfe.entity_id
FROM media_file_entity mfe
JOIN media_file mf ON mf.id = mfe.media_file_id
WHERE mfe.kind = 'tv_episode' AND mf.state = 'present'
ORDER BY mfe.entity_id;

-- name: ListarEntitiesMusicaComFaixaPossuida :many
-- Universo elegivel do job `midias_faltantes` PARA MUSICA (briefing 0116): toda entity
-- artista/album com ao menos um descendente (faixa, via artista->album->faixa ou
-- album->faixa direto) com media_file 'present'. media_file.entity_id de musica aponta
-- SEMPRE pra faixa (ADR-0027), nunca pro album/artista -- ListarEntityIDsComMediaFilePresente
-- acima nunca alcanca artista/album, entao precisa da MESMA arvore recursiva de
-- entity_relation ja usada pelo campo `owned` das listagens (entity.sql, WITH RECURSIVE
-- arvore) e por EntityTemPosseNaArvore acima. Sem isso, capa de album/foto de artista
-- nunca eram materializadas pelo job/toggle de auto-download (so cobriam movie/tv).
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT DISTINCT e.id AS entity_id
FROM entity e
JOIN arvore t ON t.ancestor_id = e.id
JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
WHERE e.type IN ('artista', 'album')
ORDER BY entity_id;

-- name: ObterCaminhoDescendentePresente :one
-- Devolve o path de UM media_file 'present' qualquer, descendente (via a MESMA
-- arvore de entity_relation) da entity `entity_id` -- usado pelo briefing 0119
-- (descoberta de arte LOCAL de artista/album de musica: a pasta canonica de um
-- album e o diretorio do arquivo da faixa; a do artista, um nivel acima). Artista/
-- album NUNCA tem media_file DIRETO (ADR-0027, so a faixa liga), entao so a arvore
-- alcanca. sql.ErrNoRows quando a entity nao tem nenhum descendente possuido (nao
-- deveria acontecer para quem passou por ListarEntitiesMusicaComFaixaPossuida, mas
-- o chamador trata como "nada a fazer", nao erro).
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT m.path
FROM entity e
JOIN arvore t ON t.ancestor_id = e.id
JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
WHERE e.id = sqlc.arg('entity_id')
LIMIT 1;

-- name: EntityTemPosse :one
-- POSSE DERIVADA (ADR-0009): verdadeiro se, e so se, existe ao menos um
-- media_file 'present' para a entity. Posse NAO e coluna (uma coluna gerada nao
-- enxerga outra tabela) -- e computada aqui.
SELECT EXISTS (
    SELECT 1 FROM media_file
    WHERE entity_id = ? AND state = 'present'
) AS tem_posse;

-- name: EntityTemPosseNaArvore :one
-- POSSE DERIVADA na ARVORE de descendentes (briefing 0116): verdadeiro se existe
-- media_file 'present' em QUALQUER descendente desta entity via entity_relation, a
-- qualquer profundidade -- cobre artista->album->faixa (ADR-0027: media_file.entity_id
-- so aponta pra faixa, nunca pro album/artista). Mesma CTE de fecho transitivo ja usada
-- pelas listagens (entity.sql, ObterEntityCard/ListarEntityCards*), aqui isolada porque
-- cada query sqlc compila a propria WITH RECURSIVE. Usada pelo detalhe unico
-- (resolverEntity, internal/server/entities.go) para completar o Owned do resolver
-- quando EntityTemPosse (arquivo DIRETO) e false -- sem isso, artista/album de musica
-- sempre mostravam posse=false no detalhe mesmo com faixa possuida (as listagens ja
-- foram corrigidas antes; o detalhe usava um caminho separado, ListarMediaFilesPorEntity,
-- que nunca desce a arvore).
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT EXISTS (
    SELECT 1 FROM arvore t
    JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
    WHERE t.ancestor_id = e.id
) AS tem_posse
FROM entity e
WHERE e.id = ?;
