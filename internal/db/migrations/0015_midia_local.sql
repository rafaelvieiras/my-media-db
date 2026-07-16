-- +goose Up
-- Materializacao fisica de arte/NFO na pasta REAL da biblioteca (briefing
-- 0062): local_path e o caminho absoluto do arquivo de arte gravado no disco
-- da biblioteca (ortogonal a path_or_uri, que continua sendo o cache interno
-- em assetsDir ou a URL externa -- NAO muda de significado); is_principal
-- formaliza em COLUNA o "principal" do (entity, kind) -- fonte de verdade
-- NOVA para o codigo deste briefing em diante (meta_json.preferred continua
-- existindo por compat, so o codigo antigo o le). nfo_path/nfo_written_at
-- guardam onde e quando a entity teve seu .nfo exportado por ultimo (NULL =
-- nunca gravado).
ALTER TABLE asset ADD COLUMN local_path TEXT;
ALTER TABLE asset ADD COLUMN is_principal INTEGER NOT NULL DEFAULT 0;

ALTER TABLE entity ADD COLUMN nfo_path TEXT;
ALTER TABLE entity ADD COLUMN nfo_written_at TEXT;

-- Backfill: preserva o estado atual de "preferida" (meta_json.preferred,
-- gravado como o literal JSON `true` por comPreferred/internal/server/
-- assets.go) como is_principal=1 -- sem perda ao trocar de fonte de verdade.
-- json_extract devolve o inteiro SQL 1 para o JSON `true` (e NULL para linha
-- sem meta_json ou sem a chave), entao a comparacao abaixo so casa quem
-- estava mesmo marcado preferido.
UPDATE asset
SET is_principal = 1
WHERE json_extract(meta_json, '$.preferred') = 1;

-- +goose Down
-- Reverte 0015. Nenhum indice/coluna gerada referencia as colunas novas; DROP
-- COLUMN direto funciona (mesmo padrao das migrations 0003/0005/0007/0008/0013).
ALTER TABLE asset DROP COLUMN local_path;
ALTER TABLE asset DROP COLUMN is_principal;
ALTER TABLE entity DROP COLUMN nfo_path;
ALTER TABLE entity DROP COLUMN nfo_written_at;
