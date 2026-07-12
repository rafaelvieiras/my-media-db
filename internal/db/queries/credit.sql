-- Acesso a tabela credit (briefing 0029, migration 0010): a aresta obra<->pessoa/
-- empresa, cache re-componivel do payload externo. O re-enriquecimento apaga os
-- creditos de (entity_id, source) e os repopula (idempotente, sem duplicar). O dado do
-- dono nunca vive aqui. Comentarios em ASCII e colunas explicitas -- ver a nota em
-- entity.sql (o sqlc v1.31 corrompe SQL de query com acento).

-- name: CriarCredito :exec
-- Insere um credito ligado a entity. person_id (cast/crew) OU company_id (company) e
-- preenchido, o outro fica NULL. ordering carrega a ordem de bilhagem do elenco;
-- character/department/job/episode_count sao os atributos da aresta.
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
-- Apaga todos os creditos de uma entity vindos de UMA fonte, antes de repopular no
-- re-enriquecimento. Por fonte (nao total) para nao apagar creditos de outra source
-- que enriqueceu a mesma obra (ex.: serie com arvore TVDB + elenco TMDB).
DELETE FROM credit
WHERE entity_id = ? AND source = ?;

-- name: ListarCreditosDaEntity :many
-- Todos os creditos de uma obra, para o detalhe. Ordena por kind, ordem de bilhagem e
-- id (estavel).
SELECT id, entity_id, person_id, company_id, kind,
    department, job, character, ordering, episode_count,
    source, source_id
FROM credit
WHERE entity_id = ?
ORDER BY kind, ordering, id;

-- name: ListarFilmografiaDaPessoa :many
-- Filmografia da pessoa (briefing 0029 Fase D, interno-first): as obras do catalogo
-- que a creditam, elenco OU equipe (credito de produtora nao tem person_id, sai
-- naturalmente do WHERE). Junta credit com entity para trazer o papel (kind/
-- character/job/department/ordering) daquele credito naquela obra; o handler busca
-- titulo/poster/ano de cada entity via ObterEntityCard (mesmo card da lista, sem
-- reinventar resolucao). Ordena por tipo, ordem de bilhagem e id (estavel).
SELECT
    e.id AS entity_id, e.type AS entity_type,
    c.kind, c.character, c.job, c.department, c.ordering
FROM credit c
JOIN entity e ON e.id = c.entity_id
WHERE c.person_id = ?
ORDER BY e.type, c.ordering, e.id;
