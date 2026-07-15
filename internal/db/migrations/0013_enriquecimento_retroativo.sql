-- +goose Up
-- Migration 0013 (briefing 0056): coluna de throttle do enriquecimento
-- externo retroativo do job saude_biblioteca. Sem ela, cada execucao do job
-- bateria de novo no provider externo para TODO item elegivel, mesmo que a
-- tentativa anterior (minutos ou horas atras) nao tenha achado nada --
-- caro e sujeito a rate-limit sem necessidade, numa biblioteca que muda
-- pouco de um dia para o outro. NULL = nunca tentado (prioridade maxima na
-- fila do proximo lote); preenchida sempre que o item e efetivamente
-- verificado contra o provider (aplicado, sinalizado ou sem candidato) --
-- nao e tocada quando o passo inteiro e pulado por falta de provider
-- configurado (nesse caso nao houve tentativa real nenhuma).
ALTER TABLE entity ADD COLUMN last_external_match_attempt_at TEXT;

-- +goose Down
-- Reverte 0013. Nenhum indice/coluna gerada referencia a coluna nova, DROP
-- COLUMN direto funciona (mesmo padrao das migrations 0003/0005/0007/0008).
ALTER TABLE entity DROP COLUMN last_external_match_attempt_at;
