-- +goose Up
-- Migration 0019 (briefing 0088): campos LOCAIS de reproducao em `entity` --
-- assistido, contagem de reproducoes, avaliacao do usuario, ultima
-- reproducao e posicao/total de retomada. Nao sao campo de catalogo
-- (multi-idioma/multi-fonte, resolver de 3 camadas): sao estado local de
-- reproducao, sem equivalente em nenhum provider externo -- mesmo
-- tratamento dado a local_path/nfo_path (briefing 0062), direto na tabela.
-- Todas nullable, sem default: ausencia = "nunca lido do .nfo, nunca setado
-- por ninguem". So sao preenchidas quando ausentes (COALESCE, nunca
-- sobrescreve -- principios 1 "override do dono vence" e 5
-- "nao-destrutivo" do AGENTS.md), a partir da leitura pontual do .nfo ja em
-- disco (internal/localmedia.ExtrairCamposUsuario) disparada pelo endpoint
-- POST /entities/{id}/refresh-files.

-- watched: 0/1 (equivalente ao <watched> do .nfo Kodi/Emby/Jellyfin).
ALTER TABLE entity ADD COLUMN watched INTEGER CHECK (watched IN (0, 1));

-- play_count: quantas vezes o item foi reproduzido (<playcount> do .nfo).
ALTER TABLE entity ADD COLUMN play_count INTEGER;

-- user_rating: nota do usuario, separada de qualquer rating de provider
-- (<userrating> do .nfo).
ALTER TABLE entity ADD COLUMN user_rating REAL;

-- last_played_at: timestamp ISO da ultima reproducao (<lastplayed> do .nfo).
ALTER TABLE entity ADD COLUMN last_played_at TEXT;

-- resume_position_seconds / resume_total_seconds: retomada de reproducao em
-- andamento (<resume><position>/<total></resume> do .nfo).
ALTER TABLE entity ADD COLUMN resume_position_seconds REAL;
ALTER TABLE entity ADD COLUMN resume_total_seconds REAL;

-- +goose Down
ALTER TABLE entity DROP COLUMN resume_total_seconds;
ALTER TABLE entity DROP COLUMN resume_position_seconds;
ALTER TABLE entity DROP COLUMN last_played_at;
ALTER TABLE entity DROP COLUMN user_rating;
ALTER TABLE entity DROP COLUMN play_count;
ALTER TABLE entity DROP COLUMN watched;
