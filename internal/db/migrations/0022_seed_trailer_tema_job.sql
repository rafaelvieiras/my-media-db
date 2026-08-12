-- +goose Up
-- Migration 0022 (briefing 0104, ADR-0026): job periodico
-- `trailer_tema_faltantes` -- varre a biblioteca e baixa trailer/tema
-- musical faltante (mesmo molde do job `midias_faltantes`, migration 0016).

-- Seed do job `trailer_tema_faltantes`: agendado para 30 minutos depois do
-- `midias_faltantes` (4h -> 4h30), so para nao concorrer pelo mesmo I/O de
-- rede/disco na mesma janela -- nao ha dependencia funcional entre os dois.
-- Desabilitado por padrao, mesmo espirito de midias_faltantes/
-- backup_vacuum/saude_biblioteca: acao automatica que baixa/escreve no
-- disco nao deveria comecar sozinha sem o dono revisar o agendamento
-- primeiro.
INSERT INTO job_definition (id, nome, descricao, enabled, schedule_cron, config_json)
VALUES (
    'trailer_tema_faltantes',
    'Baixar trailer e tema musical faltante',
    'Varre a biblioteca e baixa, via yt-dlp, o trailer (TMDB/TVDB) e o tema musical (ThemerrDB) de toda entity com candidato disponivel e ainda sem o arquivo local.',
    0,
    '30 4 * * *',
    '{}'
);

-- +goose Down
DELETE FROM job_definition WHERE id = 'trailer_tema_faltantes';
