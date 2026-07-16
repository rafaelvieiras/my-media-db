-- +goose Up
-- Migration 0016 (briefing 0064): fecha o ciclo do 0062 em escala -- watch
-- opcional de filesystem por fonte + o job periodico `midias_faltantes` que
-- materializa na pasta real o que ja esta resolvido no banco mas ainda falta
-- no disco. Nao mexe no scan incremental (mtime/size, internal/scanner) --
-- media_file.size/mtime ja existem desde a migration 0002, este briefing so
-- passa a LER esses valores antes de re-hashear (mudanca só em Go).

-- watch_enabled: liga/desliga o watcher fsnotify desta fonte (0 = default).
-- Estado do watcher em si (mapa source_id->*fsnotify.Watcher) fica só em
-- memoria, reconstruido no boot a partir desta coluna -- nenhuma tabela nova
-- para isso (briefing 0064 §4).
ALTER TABLE library_source ADD COLUMN watch_enabled INTEGER NOT NULL DEFAULT 0
    CHECK (watch_enabled IN (0, 1));

-- Seed do job `midias_faltantes` (mesmo padrao do saude_biblioteca, migration
-- 0012): agendado para as 4h da manha, MESMO horario do saude_biblioteca --
-- aceito pelo briefing 0064 §4 (cron default `0 4 * * *`) porque o perfil de
-- I/O e diferente (rede/download, nao varredura de disco em lote) e os dois
-- jobs rodam em goroutines independentes do agendador (0052). Desabilitado
-- por padrao, mesmo espirito de backup_vacuum/saude_biblioteca: acao
-- automatica que baixa/escreve no disco nao deveria comecar sozinha sem o
-- dono revisar o agendamento primeiro.
INSERT INTO job_definition (id, nome, descricao, enabled, schedule_cron, config_json)
VALUES (
    'midias_faltantes',
    'Baixar mídias faltantes (arte + NFO na pasta real)',
    'Materializa na pasta real da biblioteca a arte já resolvida e o .nfo de toda entity com arquivo presente, sem varrer o disco ao vivo.',
    0,
    '0 4 * * *',
    '{}'
);

-- +goose Down
DELETE FROM job_definition WHERE id = 'midias_faltantes';
ALTER TABLE library_source DROP COLUMN watch_enabled;
