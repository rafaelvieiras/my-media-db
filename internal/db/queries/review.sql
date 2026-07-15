-- Queries das FILAS DE CURADORIA (briefing 0018): revisao, orfas e as escritas de
-- confirmar/purgar. Sem tabela nova -- as filas sao QUERY sobre entity/media_file/
-- media_override/asset (ADR-0012). Comentarios em ASCII e colunas explicitas -- ver
-- a nota em entity.sql.
--
-- As duas listas devolvem o MESMO shape de card que ListarEntityCards (id, type,
-- title, status, owned, file_count, match_confidence, has_poster): o handler
-- converte a linha para o tipo de card comum e reusa o mesmo DTO da lista.

-- name: ListarRevisao :many
-- Fila de REVISAO: entities status='draft' (nao-identificado) OU que tem um
-- media_file 'present' cuja match_confidence esta em (0, LIMIAR) -- baixa confianca.
-- O LIMIAR entra como parametro (sqlc.arg('limiar')): fonte unica no backend
-- (espelha o LIMIAR_BAIXA_CONFIANCA=70 do front). Ordena por confianca ASCENDENTE
-- (pior primeiro); drafts/sem confianca vao ao fim (match_confidence IS NULL = 1).
SELECT
    e.id, e.type, e.title, e.status,
    EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') AS owned,
    (SELECT COUNT(*) FROM media_file m WHERE m.entity_id = e.id) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster
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
-- Fila de ORFAS: entities auto-criadas pelo scanner (status='cataloged') que TEM ao
-- menos um media_file, com NENHUM 'present' (todos 'missing'), e SEM investimento do
-- dono -- sem linha em media_override e sem asset origin='user'. Uma entity SO
-- importada (sem media_file) NAO e orfa. owned e sempre falso aqui (nenhum present).
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
-- Confirmar (briefing 0018): o dono diz que o palpite esta certo. Sobe a confianca
-- de TODOS os media_file da entity para o valor dado (100 = autoridade do dono),
-- fazendo o item sair da fila de revisao. Nao toca override/arte.
UPDATE media_file
SET match_confidence = sqlc.arg('match_confidence'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE entity_id = sqlc.arg('entity_id');

-- name: ListarElegiveisEnriquecimentoRetroativo :many
-- Complemento EXATO (De Morgan) do WHERE de ListarRevisao: entities SEM
-- NENHUM external_id (external_ids_json IS NULL) que NAO estao na fila de
-- Revisao -- ou seja, status != 'draft' E sem nenhum media_file de baixa
-- confianca (0 < match_confidence < limiar). Na pratica, hoje isso e quase
-- sempre o caso confianca=70 (confTituloAno do scanner, titulo+ano
-- parseados do arquivo, sem id embutido) -- NUNCA verificado contra
-- provider nenhum, apesar de "parecer" catalogado. Restrito a type
-- movie/tv (nivel obra/serie-topo, mesmo escopo do briefing 0050) e a
-- entities com pelo menos um media_file 'present' (entity so-importada sem
-- arquivo nao e alvo do scanner, fora de escopo aqui). Throttle: nunca
-- tentado (NULL) tem prioridade, depois o mais antigo tentado primeiro;
-- sqlc.arg('desde') e o corte de "tentado ha pouco tempo, pula por agora";
-- sqlc.arg('limite') poda o lote por execucao (custo/rate-limit do
-- provider externo, briefing 0056).
SELECT e.id, e.type, e.title, e.release_date, e.last_external_match_attempt_at
FROM entity e
WHERE e.external_ids_json IS NULL
  AND e.type IN ('movie', 'tv')
  AND e.status != 'draft'
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
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
        OR e.last_external_match_attempt_at < sqlc.arg('desde')
      )
ORDER BY
    (e.last_external_match_attempt_at IS NULL) DESC,
    e.last_external_match_attempt_at ASC,
    e.title COLLATE NOCASE ASC,
    e.id
LIMIT sqlc.arg('limite');

-- name: AtualizarTentativaMatchExterno :exec
-- Registra que o item FOI verificado nesta execucao (aplicado, sinalizado
-- ou sem candidato) -- throttle do briefing 0056: o proximo lote so volta a
-- considerar este id depois de sqlc.arg('desde') (acima) ter passado.
UPDATE entity
SET last_external_match_attempt_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg('id');

-- name: PromoverDeDraft :exec
-- Curadoria (briefing 0018): um draft (nao-identificado) que o dono CONFIRMA ou
-- CORRIGE deixa de ser rascunho -- vira 'cataloged' e sai da fila de revisao pelo
-- ramo do status (senao a confianca sobe a 100 mas o item fica preso como draft).
-- Guardada por status='draft': no-op idempotente em qualquer outro estado (nao
-- rebaixa 'wanted'/'unreleased' nem mexe em item ja catalogado).
UPDATE entity
SET status = 'cataloged',
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg('id') AND status = 'draft';
