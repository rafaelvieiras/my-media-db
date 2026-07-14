-- +goose Up
-- Migration 0012 (briefing 0053): job SAUDE DA BIBLIOTECA, follow-up
-- explicito previsto na fundacao de jobs (0052 §7). Nao altera schema --
-- job_definition/job_execution ja sao genericos o bastante (0052 §5, criterio
-- de aceite "adicionar um NOVO tipo de job nao exige alterar o schema") -- so
-- semeia a linha do novo Handler `saude_biblioteca`
-- (internal/job/libraryhealth), que re-escaneia toda library_source
-- cadastrada e resume a fila de revisao/orfas (briefing 0018) ao final.
--
-- Agendado para as 4h da manha, uma hora DEPOIS do backup (0 3 * * *, 0011)
-- -- evita concorrencia de I/O pesado (VACUUM INTO + varredura de disco) no
-- mesmo instante; o backup tende a ser mais rapido, entao 1h de folga e
-- confortavel para bibliotecas de porte domestico.
--
-- Desabilitado por padrao, mesmo espirito do backup_vacuum: uma acao
-- automatica que gera I/O em disco (ler toda a arvore de arquivos de cada
-- fonte) nao deveria começar sozinha sem o dono revisar o agendamento
-- primeiro, especialmente num Pi com cartao SD.
--
-- config_json = '{}': o job nao tem parametro hoje (nao ha "keep" ou
-- equivalente) -- o objeto vazio so documenta a convencao (config_json
-- namespaced por tipo, ADR-0005) para quando/se aparecer um parametro real
-- (ex.: paralelismo entre fontes).
INSERT INTO job_definition (id, nome, descricao, enabled, schedule_cron, config_json)
VALUES (
    'saude_biblioteca',
    'Saúde da biblioteca (re-scan + revisão/órfãs)',
    'Re-escaneia todas as fontes cadastradas e resume quantos itens novos caíram na fila de revisão e quantos ficaram órfãos.',
    0,
    '0 4 * * *',
    '{}'
);

-- +goose Down
DELETE FROM job_definition WHERE id = 'saude_biblioteca';
