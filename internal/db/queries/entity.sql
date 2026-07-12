-- CRUD minimo da entity base.
--
-- IMPORTANTE (sqlc + ASCII): os comentarios deste arquivo .sql sao ASCII (sem
-- acentos) DE PROPOSITO. O sqlc v1.31 fatia o texto de cada query por offset de
-- BYTE, mas as posicoes dos tokens vem em offset de RUNE; qualquer caractere
-- multi-byte (acento pt-BR) num arquivo de query desloca o corte e CORROMPE o
-- SQL gerado (some o '?' final, troca 'AS x' por 'A', etc.). As migrations NAO
-- sofrem disso (sao parseadas por estrutura, nao guardadas como texto), entao la
-- os comentarios seguem em pt-BR acentuado. Aqui, ASCII. Regra geral: colunas
-- sempre EXPLICITAS, nunca '*'/'RETURNING *' (a expansao de '*' tem o mesmo bug
-- de offset). As colunas geradas (external_id_tmdb/imdb/tvdb) derivam de
-- external_ids_json e NAO entram no INSERT/UPDATE, mas aparecem no
-- RETURNING/SELECT para espelhar o modelo (migration 0007 somou tvdb).

-- name: CriarEntity :one
-- status NAO entra no INSERT: usa o DEFAULT 'cataloged' (item auto-criado nasce
-- catalogado; a curadoria muda depois via AtualizarEntity). ADR-0009.
INSERT INTO entity (
    id, type, title, original_title, description, release_date,
    tags_json, external_ids_json, extension_data_json
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb;

-- name: ObterEntity :one
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb
FROM entity
WHERE id = ?;

-- name: ObterEntityPorTmdb :one
-- Lookup por id externo tmdb via coluna gerada + indice (doc 04, busca sem GIN).
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb
FROM entity
WHERE external_id_tmdb = ?;

-- name: ObterEntityPorTvdb :one
-- Lookup por id externo tvdb via coluna gerada + indice (migration 0007, ADR-0021).
-- Espelha ObterEntityPorTmdb: series exclusivas do TheTVDB (sem contraparte tmdb/
-- imdb, ex.: Looney Tunes) deduplicam por este caminho no import.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb
FROM entity
WHERE external_id_tvdb = ?;

-- name: ObterColecaoPorTmdb :one
-- Dedup da entity COLECAO por id tmdb (briefing 0025): reusa a coluna gerada
-- external_id_tmdb, mas RESTRITA a type='collection'. O namespace de id de colecao
-- e distinto do de filme, porem a coluna gerada e compartilhada; o filtro por type
-- evita casar um filme homonimo de mesmo id numerico. Mantem o re-import idempotente.
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb
FROM entity
WHERE type = 'collection' AND external_id_tmdb = ?
LIMIT 1;

-- name: ObterEntityCard :one
-- CARD de UMA entity (mesmas colunas derivadas de ListarEntityCards) + release_date
-- para ordenar. Usado pelos MEMBROS de uma colecao (briefing 0025) e pelos episodios
-- de uma temporada (0034): a lista de filhos vem de entity_relation e cada filho vira
-- card por aqui (N pequeno). As colunas derivadas espelham ListarEntityCards
-- (owned/file_count/match_confidence/has_poster), entao o mapeamento reusa o cardDTO.
-- owned/file_count contam TAMBEM a juncao media_file_entity -- ver a nota longa em
-- ListarEntityCards (o card do episodio possui arquivo so pela juncao).
SELECT
    e.id, e.type, e.title, e.status, e.release_date,
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
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster
FROM entity e
WHERE e.id = ?;

-- name: ObterEntityPorImdb :one
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb
FROM entity
WHERE external_id_imdb = ?;

-- name: ObterEntityPorTituloAno :one
-- Dedup do scanner sem id externo (briefing 0009): reusa a MESMA entity para o
-- mesmo (type, title, release_date). Usa `IS` (nao `=`) para casar release_date
-- NULO quando o nome nao traz ano. Mantem o re-scan idempotente para obras sem
-- id embutido no nome (nao duplica a entity).
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb
FROM entity
WHERE type = ? AND title = ? AND release_date IS ?
LIMIT 1;

-- name: ObterEntityPorTituloTipo :one
-- Lookup do scanner de series (briefing 0034): a serie ja no catalogo pelo par
-- (type, title), ignorando release_date. Usada quando a pasta da serie nao traz id
-- externo embutido, para reusar a serie ja importada e evitar um novo round-trip ao
-- provider a cada scan. Primeira por created_at (estavel).
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb
FROM entity
WHERE type = ? AND title = ?
ORDER BY created_at, id
LIMIT 1;

-- name: ListarEntities :many
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb
FROM entity
ORDER BY created_at DESC, id
LIMIT ? OFFSET ?;

-- name: ListarEntityCards :many
-- Lista de CARDS para a UI (briefing 0010), ordenada por titulo ASCENDENTE. Uma
-- unica query com agregacao por subselect correlacionado -- evita o N+1 (nada de
-- uma query de posse/contagem por item). Colunas derivadas:
--   owned            POSSE DERIVADA (ADR-0009): EXISTS media_file present
--                    DIRETO (media_file.entity_id = e.id) OU via a juncao
--                    media_file_entity (ver abaixo).
--   file_count       total de media_file da entity: os DIRETOS (present + missing)
--                    MAIS os ligados pela juncao media_file_entity.
--   match_confidence maior confianca de match entre os arquivos (badge de revisao).
--   has_poster       EXISTS de um asset kind='poster' (a UI decide o poster_url).
-- Posse GENERICA pela juncao (0034): um arquivo de episodio tem media_file.entity_id
-- apontando a SERIE (a obra) e liga ao EPISODIO so por media_file_entity. Se a posse
-- do card olhasse so o entity_id direto, o episodio apareceria owned=false/file_count=0
-- enquanto o detalhe e a posse agregada (que ja leem a juncao) mostram possuido --
-- inconsistente. Por isso owned/file_count somam TAMBEM a juncao. E agnostico ao kind
-- (qualquer entity ligada por media_file_entity conta; futuro 'track' de musica idem).
-- NAO duplica: o vinculo direto e o da juncao apontam entities e.id DIFERENTES (a
-- serie leva o arquivo pelo entity_id; o episodio leva pela juncao) -- num mesmo card
-- so um dos dois caminhos casa e.id. Para o filme, sem linhas na juncao, o comportamento
-- fica inalterado. COUNT(DISTINCT media_file_id) na juncao evita contar duas vezes um
-- arquivo ligado a mesma entity em kinds diferentes.
-- So OBRAS DE TOPO entram: season/episode sao nos internos da arvore de series
-- (0032), alcancados so pela arvore do pai (GET /members); ficam FORA da home e
-- da busca via `type NOT IN ('season','episode')`. movie/tv/collection seguem.
-- Filtros opcionais (NULL = sem filtro) via sqlc.narg: type e owned. A ordenacao
-- por titulo em ordem DESC vive numa query irma (ListarEntityCardsDesc) porque o
-- sqlc nao liga parametros dentro de expressoes de ORDER BY -- direcao dinamica
-- num unico statement nao e suportada; o handler escolhe qual query rodar.
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
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster
FROM entity e
WHERE e.type NOT IN ('season', 'episode')
  AND (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') = sqlc.narg('owned'))
ORDER BY e.title COLLATE NOCASE ASC, e.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListarEntityCardsDesc :many
-- Igual a ListarEntityCards, mas ordena por titulo DESCENDENTE (sort=-title). owned/
-- file_count somam a juncao media_file_entity do mesmo jeito -- ver a nota longa acima.
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
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster
FROM entity e
WHERE e.type NOT IN ('season', 'episode')
  AND (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') = sqlc.narg('owned'))
ORDER BY e.title COLLATE NOCASE DESC, e.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ContarEntityCards :one
-- Total de itens que casam os filtros (para o campo `total` da paginacao). Mesma
-- clausula WHERE da ListarEntityCards -- sem LIMIT/OFFSET/ORDER BY. Inclui a mesma
-- exclusao de sub-nos (season/episode) para o total bater com a pagina (0032).
SELECT COUNT(*) AS total
FROM entity e
WHERE e.type NOT IN ('season', 'episode')
  AND (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') = sqlc.narg('owned'));

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
    created_at, updated_at, external_id_tmdb, external_id_imdb, original_language, external_id_tvdb;

-- name: AtualizarEpisodioBase :exec
-- Reescreve a BASE do episodio no re-import (briefing 0043): o find-or-create por id
-- externo reusa a linha, mas antes so o create gravava estes campos, entao still_url/
-- numbering novos (extension_data) e titulo/sinopse/data/ids atualizados do provider
-- nunca chegavam a um episodio ja importado. Aqui reafirmamos os fatos da fonte na
-- linha-base. NAO toca status/tags/original_title nem media_override: a resolucao
-- aplica override por cima, entao refrescar a base e seguro (o override segue vencendo).
UPDATE entity
SET title = ?,
    description = ?,
    release_date = ?,
    external_ids_json = ?,
    extension_data_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarTemporadaBase :exec
-- Reescreve a BASE da temporada no re-import (briefing 0043): titulo, data e o
-- extension_data core.tv@1 (ordering + season_number + year). Simetrica a
-- AtualizarEpisodioBase, sem description (temporada nao guarda sinopse na linha). NAO
-- toca status/tags/original_title nem media_override -- a resolucao aplica override por
-- cima; refrescar a base e seguro.
UPDATE entity
SET title = ?,
    release_date = ?,
    extension_data_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarExternalIds :exec
-- Mescla a identidade externa (MD4): a app le external_ids_json, junta
-- {source: source_id} e grava aqui, sem tocar o resto da entity. As colunas
-- geradas external_id_tmdb/imdb re-derivam sozinhas do novo JSON. Usado pelo
-- enrich/import (briefing 0015) -- media_external e cache subordinado a isto.
UPDATE entity
SET external_ids_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarTitulo :exec
-- Repara o titulo base de um item auto-criado/importado quando ele ficou como o
-- fallback numerico (id da fonte) -- a regressao do "numero no lugar do nome". So o
-- titulo e o updated_at mudam; nao toca override/externo (a autoridade do dono
-- segue vencendo na resolucao). Chamada pelo enrich (auto-cura) sob condicao
-- estrita (titulo == sourceID), nunca sobre um titulo ja legivel.
UPDATE entity
SET title = sqlc.arg('title'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg('id');

-- name: AtualizarExtensionData :exec
-- Grava o extension_data_json (mapa namespace->objeto) de uma entity, sem tocar o
-- resto. Usado pelo import de series (briefing 0031): a serie e criada/enriquecida
-- como um item comum (importarItem) e recebe aqui o namespace core.tv@1 (season
-- types). Temporada/episodio ja nascem com o extension_data no proprio CriarEntity;
-- a serie precisa deste UPDATE porque nasce pelo caminho de item. So esta coluna e o
-- updated_at mudam; nao toca override/externo.
UPDATE entity
SET extension_data_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarOriginalLanguage :exec
-- Grava a ANCORA do fallback por idioma (briefing 0020): o codigo NEUTRO do idioma
-- original da obra, obtido do detalhe da fonte no enrich/import. Nao-destrutivo: e
-- um fato da fonte, nao dado do dono; re-enriquecer apenas o reafirma. So esta
-- coluna e o updated_at mudam.
UPDATE entity
SET original_language = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: AtualizarReleaseDate :exec
-- Grava a DATA DE LANCAMENTO neutra da obra, obtida do detalhe da fonte no enrich/
-- import (mesmo tratamento de AtualizarOriginalLanguage: fato da fonte, nao dado do
-- dono; re-enriquecer reafirma). Denormaliza a coluna base para ordenacao/listagem
-- baratas (ex.: ordenar os membros de uma colecao por ano) sem precisar resolver cada
-- item. So esta coluna e o updated_at mudam; a resolucao segue com base=externo.
UPDATE entity
SET release_date = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: ApagarEntity :exec
-- Purga explicita: remove a entity e, por ON DELETE CASCADE, seus media_file,
-- asset e entity_relation.
DELETE FROM entity
WHERE id = ?;
