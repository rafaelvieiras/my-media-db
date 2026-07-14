-- Queries do FUNDIR DE DUPLICATA por conflito de external_id (briefing 0051):
-- quando o dono grava um id externo que ja pertence a OUTRA entity (do mesmo
-- tipo), em vez de deixar duas entities com a mesma identidade, o sistema funde
-- a "perdedora" (a que ja tinha o id) na "sobrevivente" (a que esta sendo
-- editada) -- SE a perdedora nao tiver investimento do dono (mesmo predicado da
-- fila de orfas, ver review.sql). Comentarios em ASCII e colunas explicitas --
-- ver a nota em entity.sql.
--
-- O parser SQL do sqlc (engine sqlite) nao aceita alias na tabela-alvo de um
-- UPDATE nem correlated subquery contra ela (self-join ambiguo) -- por isso as
-- queries daqui sao deliberadamente SIMPLES (update por PK/id, sem subquery); a
-- decisao "ja existe uma linha equivalente na sobrevivente?" e feita em GO (o
-- chamador lista as linhas dos dois lados com ListarXPorEntity/ParaEntity e so
-- move o que nao colide), no mesmo espirito do resto do codebase (ex.: purga em
-- lote itera ids em vez de SQL em massa).

-- name: EntityTemInvestimentoDono :one
-- Mesmo predicado de "investimento do dono" da fila de orfas (0018/review.sql):
-- existe override OU asset custom (origin='user'). Decide se uma colisao de
-- external_id pode ser fundida automaticamente (sem investimento) ou exige
-- conflito reportado ao dono (com investimento).
SELECT
    EXISTS (SELECT 1 FROM media_override o WHERE o.entity_id = ?)
    OR EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = ? AND a.origin = 'user')
    AS tem_investimento;

-- name: MoverMediaFileParaEntity :exec
-- media_file nao tem UNIQUE em entity_id (varios arquivos por obra) -- bulk
-- UPDATE direto, sem risco de colisao.
UPDATE media_file
SET entity_id = sqlc.arg('sobrevivente'), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE entity_id = sqlc.arg('perdedora');

-- name: ListarVinculosPorEntidade :many
-- Vinculos media_file_entity (N:M de serie, 0034) de UMA entidade, para o
-- chamador comparar (media_file_id, kind) da perdedora contra os da
-- sobrevivente antes de mover (evita violar a PK composta).
SELECT media_file_id, entity_id, kind, ordinal
FROM media_file_entity
WHERE entity_id = ?
ORDER BY media_file_id, kind;

-- name: MoverMediaFileEntityLinha :exec
-- Move UMA linha (identificada pela PK antiga) para a entity sobrevivente.
-- Chamado so quando o Go ja confirmou que a sobrevivente nao tem esse
-- (media_file_id, kind) ainda.
UPDATE media_file_entity
SET entity_id = sqlc.arg('sobrevivente')
WHERE media_file_id = sqlc.arg('media_file_id')
  AND entity_id = sqlc.arg('perdedora')
  AND kind = sqlc.arg('kind');

-- name: MoverMediaExternalLinha :exec
-- Move UMA linha de cache externo (por id) para a entity sobrevivente. Chamado
-- so quando a sobrevivente ainda nao tem cache daquela fonte (evita violar
-- UNIQUE(entity_id, source)).
UPDATE media_external
SET entity_id = sqlc.arg('sobrevivente')
WHERE id = sqlc.arg('id');

-- name: MoverAssetLinha :exec
-- Move UM asset (por id) para a entity sobrevivente. Chamado so quando a
-- sobrevivente ainda nao tem asset daquele kind (evita empilhar duplicata,
-- ex.: dois posteres).
UPDATE asset
SET entity_id = sqlc.arg('sobrevivente')
WHERE id = sqlc.arg('id');

-- name: MoverTodosCreditsParaEntity :exec
-- Move TODOS os creditos da perdedora para a sobrevivente -- chamado so quando
-- o Go ja confirmou que a sobrevivente ainda nao tem NENHUM (evita misturar
-- dois elencos); o resto fica para o ON DELETE CASCADE.
UPDATE credit
SET entity_id = sqlc.arg('sobrevivente')
WHERE entity_id = sqlc.arg('perdedora');

-- name: MoverEntityRelationParentLinha :exec
-- Move UMA relacao (por id) reatribuindo o lado PAI (ex.: colecao->filme) para a
-- sobrevivente. Chamado so quando o Go ja confirmou que a sobrevivente nao tem a
-- mesma (parent,child,kind) ainda (evita violar a UNIQUE).
UPDATE entity_relation
SET parent_id = sqlc.arg('sobrevivente')
WHERE id = sqlc.arg('id');

-- name: MoverEntityRelationChildLinha :exec
-- Espelha a de cima, para o lado FILHO (ex.: filme membro de colecao).
UPDATE entity_relation
SET child_id = sqlc.arg('sobrevivente')
WHERE id = sqlc.arg('id');
