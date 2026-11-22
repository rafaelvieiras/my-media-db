-- O job `sugerir_identidades`: para cada serie que o pos-scan JA sinalizou como "a arvore nao
-- abriga os arquivos", repontua a identidade com as mesmas provas do auto-import (nome em
-- qualquer idioma, ano da pasta, tamanho da pasta contra a arvore) e reescreve o alerta com o
-- candidato encontrado -- para o dono aplicar com um clique.
--
-- NUNCA troca identidade sozinho (decisao do dono, 21/nov/2026): o alerta continua sendo um
-- alerta (ADR-0038), so que agora com nome, ano, score e "quantos arquivos cabem" na arvore do
-- candidato.
--
-- Desabilitado por padrao, como os outros jobs que escrevem estado: o dono revisa o agendamento
-- antes. Roda uma hora depois do `midias_faltantes` de proposito -- a materializacao pode
-- resolver a causa do alerta (NFO/arte) antes de o job gastar requisicoes na fonte.

-- +goose Up
INSERT INTO job_definition (id, nome, descricao, enabled, schedule_cron, config_json)
VALUES (
    'sugerir_identidades',
    'Sugerir identidade para séries com árvore incompatível',
    'Repontua as séries sinalizadas com as regras atuais (título em qualquer idioma, ano da pasta, tamanho) e reescreve o alerta com o candidato que abriga os arquivos. Não troca nada sozinho.',
    0,
    '0 5 * * *',
    '{}'
);

-- +goose Down
DELETE FROM job_definition WHERE id = 'sugerir_identidades';
