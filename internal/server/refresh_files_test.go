package server

// Testes de POST /api/v1/entities/{id}/refresh-files (briefing 0088, menu
// Gerenciar > "Verificar Atualizações de Arquivos"): asset local importado
// numa entity JÁ CATALOGADA (gap hoje só coberto na 1ª varredura, ver
// internal/scanner/recheck_test.go para o teste de baixo nível do
// scanner.Manager) e campos LOCAIS de reprodução lidos de um `.nfo` já em
// disco, preenchidos SÓ quando ausentes no banco (não-destrutivo).

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupRefreshFilesServer sobe um Server com banco temporário migrado
// (mesmo padrão de setupMidiasFaltantesServer) e devolve o handler + o *Server
// (para semear campos locais direto pela query sqlc reusada pelo handler).
func setupRefreshFilesServer(t *testing.T) (http.Handler, *Server) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "refresh-files.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	srv := New("v-teste", database, WithAssetsDir(t.TempDir()))
	h, err := srv.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, srv
}

// TestRefreshEntityFilesEntityInexistenteDevolve404 cobre a guarda de
// existência (briefing 0088 §4: "404 se entity não existe").
func TestRefreshEntityFilesEntityInexistenteDevolve404(t *testing.T) {
	h, _ := setupRefreshFilesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/inexistente/refresh-files", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestRefreshEntityFilesImportaAssetLocalEExecutaRecheck cobre os critérios
// de aceite 1-2 do briefing 0088: um poster.jpg colocado na pasta de uma
// entity QUE JÁ EXISTE no catálogo (nunca visto pela 1ª varredura) é
// importado como asset origin='local' nesta chamada, e o media_file
// 'present' da entity tem o re-hash forçado (files_rechecked=1).
func TestRefreshEntityFilesImportaAssetLocalEExecutaRecheck(t *testing.T) {
	h, srv := setupRefreshFilesServer(t)
	ctx := context.Background()

	semearEntity(t, srv.db, "e1", "movie", "Filme")

	pasta := t.TempDir()
	semearSource(t, srv.db, "src1", pasta)
	video := filepath.Join(pasta, "Filme.2020.mkv")
	if err := os.WriteFile(video, []byte("conteudo-video"), 0o644); err != nil {
		t.Fatal(err)
	}
	semearFileComFonte(t, srv.db, "mf1", "e1", video, "present", int64(len("conteudo-video")), "src1")

	// Sem asset local nenhum antes da chamada.
	if cnt := contarLinhas(t, srv.db, "SELECT count(*) FROM asset WHERE entity_id='e1' AND kind='poster' AND origin='local'"); cnt != 0 {
		t.Fatalf("asset poster local pré-existente = %d, quero 0", cnt)
	}

	// O dono coloca o poster DEPOIS -- exatamente o caso hoje quebrado
	// (descobrirPoster só roda na 1ª varredura, dentro de criarEntity).
	poster := filepath.Join(pasta, "poster.jpg")
	if err := os.WriteFile(poster, []byte("fake-poster"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/refresh-files", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var dto refreshFilesResultDTO
	decodeInto(t, rec, &dto)

	if dto.FilesRechecked != 1 {
		t.Errorf("files_rechecked = %d, quero 1", dto.FilesRechecked)
	}
	if dto.AssetsFound != 1 {
		t.Errorf("assets_found = %d, quero 1", dto.AssetsFound)
	}

	if cnt := contarLinhas(t, srv.db, "SELECT count(*) FROM asset WHERE entity_id='e1' AND kind='poster' AND origin='local' AND path_or_uri=?", poster); cnt != 1 {
		t.Errorf("asset poster local apontando para %q = %d, quero 1", poster, cnt)
	}

	// O media_file teve o content_hash recalculado (recheck forçado).
	var hashDepois sql.NullString
	if err := srv.db.Read.QueryRowContext(ctx, "SELECT content_hash FROM media_file WHERE id='mf1'").Scan(&hashDepois); err != nil {
		t.Fatalf("ler content_hash: %v", err)
	}
	if !hashDepois.Valid || hashDepois.String == "" {
		t.Error("content_hash vazio após refresh-files, quero preenchido (recheck forçado)")
	}
}

// nfoComCamposLocais monta um .nfo mínimo (convenção Kodi) trazendo os 5
// campos LOCAIS de reprodução cobertos pelo briefing 0088.
func nfoComCamposLocais(userRating string, playCount string, watched string, lastPlayed string, resumePos, resumeTotal string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Filme</title>
  <userrating>` + userRating + `</userrating>
  <playcount>` + playCount + `</playcount>
  <watched>` + watched + `</watched>
  <lastplayed>` + lastPlayed + `</lastplayed>
  <resume>
    <position>` + resumePos + `</position>
    <total>` + resumeTotal + `</total>
  </resume>
</movie>`
}

// TestRefreshEntityFilesPreencheCamposLocaisAusentes cobre o critério de
// aceite 3: com um `.nfo` trazendo playcount/watched/userrating/resume e o
// banco ainda sem valor nesses campos, o endpoint preenche todos.
func TestRefreshEntityFilesPreencheCamposLocaisAusentes(t *testing.T) {
	h, srv := setupRefreshFilesServer(t)
	ctx := context.Background()

	semearEntity(t, srv.db, "e1", "movie", "Filme")
	nfoPath := filepath.Join(t.TempDir(), "Filme.nfo")
	if err := os.WriteFile(nfoPath, []byte(nfoComCamposLocais("8.5", "3", "true", "2026-01-01 10:00:00", "120.5", "5400")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sqlcgen.New(srv.db.Write).AtualizarEntityNfo(ctx, sqlcgen.AtualizarEntityNfoParams{
		NfoPath:      sql.NullString{String: nfoPath, Valid: true},
		NfoWrittenAt: sql.NullString{String: "2026-01-01T00:00:00.000Z", Valid: true},
		ID:           "e1",
	}); err != nil {
		t.Fatalf("AtualizarEntityNfo: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/refresh-files", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var dto refreshFilesResultDTO
	decodeInto(t, rec, &dto)

	esperados := []string{"watched", "play_count", "user_rating", "last_played_at", "resume_position_seconds", "resume_total_seconds"}
	if len(dto.PlaybackFieldsFilled) != len(esperados) {
		t.Fatalf("playback_fields_filled = %v, quero %v", dto.PlaybackFieldsFilled, esperados)
	}
	presentes := map[string]bool{}
	for _, f := range dto.PlaybackFieldsFilled {
		presentes[f] = true
	}
	for _, esperado := range esperados {
		if !presentes[esperado] {
			t.Errorf("playback_fields_filled não contém %q: %v", esperado, dto.PlaybackFieldsFilled)
		}
	}

	ent, err := sqlcgen.New(srv.db.Read).ObterEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.Watched.Valid || ent.Watched.Int64 != 1 {
		t.Errorf("Watched = %+v, quero 1 (true)", ent.Watched)
	}
	if !ent.PlayCount.Valid || ent.PlayCount.Int64 != 3 {
		t.Errorf("PlayCount = %+v, quero 3", ent.PlayCount)
	}
	if !ent.UserRating.Valid || ent.UserRating.Float64 != 8.5 {
		t.Errorf("UserRating = %+v, quero 8.5", ent.UserRating)
	}
	if !ent.LastPlayedAt.Valid || ent.LastPlayedAt.String != "2026-01-01 10:00:00" {
		t.Errorf("LastPlayedAt = %+v, quero \"2026-01-01 10:00:00\"", ent.LastPlayedAt)
	}
	if !ent.ResumePositionSeconds.Valid || ent.ResumePositionSeconds.Float64 != 120.5 {
		t.Errorf("ResumePositionSeconds = %+v, quero 120.5", ent.ResumePositionSeconds)
	}
	if !ent.ResumeTotalSeconds.Valid || ent.ResumeTotalSeconds.Float64 != 5400 {
		t.Errorf("ResumeTotalSeconds = %+v, quero 5400", ent.ResumeTotalSeconds)
	}
}

// TestRefreshEntityFilesNaoSobrescreveCampoJaPreenchido cobre o critério de
// aceite mais importante (não-destrutivo, AGENTS.md princípio 5): rodar o
// endpoint com um valor JÁ presente no banco, e um `.nfo` trazendo um valor
// DIFERENTE para o MESMO campo, não sobrescreve -- o campo já preenchido
// continua com o valor anterior.
func TestRefreshEntityFilesNaoSobrescreveCampoJaPreenchido(t *testing.T) {
	h, srv := setupRefreshFilesServer(t)
	ctx := context.Background()

	semearEntity(t, srv.db, "e1", "movie", "Filme")

	// user_rating JÁ preenchido no banco -- é a fonte de verdade local,
	// não pode perder para o .nfo (princípio 1/5, AGENTS.md).
	if err := sqlcgen.New(srv.db.Write).PreencherCamposLocaisReproducaoSeAusentes(ctx, sqlcgen.PreencherCamposLocaisReproducaoSeAusentesParams{
		UserRating: sql.NullFloat64{Float64: 9.9, Valid: true},
		ID:         "e1",
	}); err != nil {
		t.Fatalf("semear user_rating: %v", err)
	}

	// .nfo traz um valor DIFERENTE para user_rating, e um valor NOVO para
	// play_count (esse sim ainda ausente no banco -- deve ser preenchido).
	nfoPath := filepath.Join(t.TempDir(), "Filme.nfo")
	if err := os.WriteFile(nfoPath, []byte(nfoComCamposLocais("2.0", "7", "false", "", "0", "0")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sqlcgen.New(srv.db.Write).AtualizarEntityNfo(ctx, sqlcgen.AtualizarEntityNfoParams{
		NfoPath:      sql.NullString{String: nfoPath, Valid: true},
		NfoWrittenAt: sql.NullString{String: "2026-01-01T00:00:00.000Z", Valid: true},
		ID:           "e1",
	}); err != nil {
		t.Fatalf("AtualizarEntityNfo: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/refresh-files", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var dto refreshFilesResultDTO
	decodeInto(t, rec, &dto)

	for _, f := range dto.PlaybackFieldsFilled {
		if f == "user_rating" {
			t.Errorf("playback_fields_filled contém user_rating, mas ele já estava preenchido -- não deveria ser reportado como novo: %v", dto.PlaybackFieldsFilled)
		}
	}

	ent, err := sqlcgen.New(srv.db.Read).ObterEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.UserRating.Valid || ent.UserRating.Float64 != 9.9 {
		t.Errorf("UserRating = %+v, quero preservado em 9.9 (não sobrescrito pelo .nfo=2.0)", ent.UserRating)
	}
	// play_count, esse sim, ausente antes -- deve ter sido preenchido.
	if !ent.PlayCount.Valid || ent.PlayCount.Int64 != 7 {
		t.Errorf("PlayCount = %+v, quero 7 (preenchido -- estava ausente)", ent.PlayCount)
	}
}

// contarLinhas é um atalho local para contagens ad-hoc pelo pool de leitura
// (mesmo espírito do helper `contar` de internal/scanner/scanner_test.go,
// que não é acessível daqui por ser de outro pacote).
func contarLinhas(t *testing.T, d *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("contarLinhas (%s): %v", query, err)
	}
	return n
}
