-- +goose Up
-- Migration 0027 (briefing 0128, Caso A): o job `midias_faltantes` e o job
-- `saude_biblioteca` nasceram com o MESMO cron default (`0 4 * * *`, migration
-- 0016) e contendem pelo mesmo mount FUSE remoto (rclone/gdrive) -- em
-- producao, terminaram com so 9s de intervalo entre si. Se um media_file
-- estava temporariamente fora do estado 'present' no instante em que
-- midias_faltantes consultou o universo elegivel, a entity ficava fora da
-- lista processada naquele dia, sem nenhum sinal visivel. Escalona o
-- default para 1h depois -- tempo generoso frente ao observado em producao
-- (segundos a poucos minutos para o rescan completo). So altera instalacoes
-- que NUNCA customizaram o cron via PATCH /jobs/{id} (WHERE cobre o valor
-- default exato semeado pela 0016) -- valor customizado pelo dono nunca e
-- sobrescrito.
UPDATE job_definition
SET schedule_cron = '0 5 * * *'
WHERE id = 'midias_faltantes' AND schedule_cron = '0 4 * * *';

-- +goose Down
UPDATE job_definition
SET schedule_cron = '0 4 * * *'
WHERE id = 'midias_faltantes' AND schedule_cron = '0 5 * * *';
