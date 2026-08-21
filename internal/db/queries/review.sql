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
-- Complemento do WHERE de ListarRevisao, restrito a tipos DE TOPO (ADR-0030):
-- entities SEM nenhum external_id (external_ids_json IS NULL) que NAO estao
-- na fila de Revisao (status != 'draft', sem media_file de baixa confianca)
-- E que tem ao menos um media_file 'present' -- direto (movie/tv/qualquer
-- tipo com arquivo proprio) OU via arvore de entity_relation (musica:
-- artista nunca liga media_file direto, ADR-0027 -- so a faixa liga; mesmo
-- padrao WITH RECURSIVE ja usado por ListarEntitiesMusicaComFaixaPossuida,
-- media_file.sql). O type NOT IN abaixo e a lista LITERAL dos mesmos valores
-- de tiposSubNo (ADR-0030/briefing 0112, internal/server/entities.go) --
-- sqlc.slice('sub_nos') foi tentado (briefing 0116) e descartado: sqlc, no
-- dialeto sqlite, nao combina sqlc.slice com outros sqlc.arg no MESMO
-- statement -- a expansao dinamica de `?` do slice desalinha a numeracao dos
-- placeholders nomeados (?N) dos demais args, causando erro de bind em
-- runtime ("missing named argument"/"datatype mismatch"), mesmo com cada arg
-- referenciado uma unica vez. TestListaSubNosSincronizadaComQuery
-- (retroativo_test.go) trava a lista literal contra tiposSubNo -- se um tipo
-- novo entrar ali, o teste falha ate esta query ser atualizada a mao.
-- Throttle: nunca tentado (NULL) tem prioridade, depois o mais antigo
-- tentado primeiro; sqlc.arg('dias_global') e o intervalo GLOBAL em dias
-- (briefing 0085), usado como fallback quando nenhum media_file 'present' da
-- entity vem de uma biblioteca com override (a subquery de override so olha
-- media_file DIRETO -- para artista isso sempre cai no fallback global, ver
-- briefing 0116 secao 3 "Fora"); sqlc.arg('limite') poda o lote por execucao.
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
-- Complemento de ListarElegiveisEnriquecimentoRetroativo (briefing 0118): em vez
-- de entities SEM identidade, aqui sao as JA IDENTIFICADAS (external_ids_json
-- NOT NULL) cujo inventario de arte (fanart.tv etc, briefing 0048) nunca foi
-- (re)verificado -- fecha o gap de uma entity que ganhou a identidade ANTES de
-- um ArteProvider existir/ser configurado no sistema (ou cuja busca de arte na
-- epoca nao achou nada e nunca foi reprocessada): sem este passo ela fica sem
-- arte extra para sempre, so alcancavel pelo botao manual "Baixar Artes
-- Faltantes" por item (briefing 0088). Reusa last_external_match_attempt_at --
-- LIVRE para estas entities, ja que a query irma (acima) so olha entities com
-- external_ids_json IS NULL -- so que aqui o carimbo significa "ultima vez que
-- o inventario de arte foi verificado", nao "ultima tentativa de match". Mesmo
-- throttle/dias (retroactive_enrichment_days) e mesma lista literal de tipos
-- de topo (sub_nos EXCLUIDOS, ADR-0030/0112 -- TestListaSubNosSincronizadaComQuery
-- trava as DUAS queries contra tiposSubNo) e posse via arvore de entity_relation
-- (musica: artista nunca liga media_file direto, ADR-0027) que a query irma ja
-- usa.
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

-- name: ListarFaixasDraftOrfas :many
-- Complemento de ListarElegiveisReprocessamentoArtes (briefing 0120): faixas
-- type='faixa' status='draft' com media_file present (entity_id aponta
-- direto, ADR-0027 e 1:1) SEM entity_relation como filha de nenhum album (NOT
-- EXISTS child_id) -- diferente das outras queries de elegibilidade deste
-- arquivo, aqui faixa/tv NAO sao excluidos como "sub_nos": sao exatamente o
-- universo que este briefing cobre. Mesmo throttle por
-- library_source.retroactive_enrichment_days com fallback pro dias_global,
-- mesma logica de ListarElegiveisReprocessamentoArtes.
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
-- Complemento de ListarFaixasDraftOrfas (briefing 0120): series type='tv'
-- status='draft' com media_file present apontando direto (entity_id =
-- serie.id, gravarArquivoSerie sempre grava assim, mesmo em revisao). Mesmo
-- throttle da query irma acima.
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
