package server

// Tests of POST /api/v1/entities/{id}/refresh-files (Manage menu > "Verificar Atualizações de
// Arquivos"): a local asset imported into an ALREADY CATALOGED entity (a gap only covered by
// the 1st sweep, see internal/scanner/recheck_test.go for the low-level test of
// scanner.Manager) and LOCAL playback fields read from a `.nfo` already on disk, filled in ONLY
// when they are absent from the database (non-destructive).

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

// setupRefreshFilesServer starts a Server with a migrated temporary database (the same pattern
// as setupMidiasFaltantesServer) and returns the handler + the *Server (to seed local fields
// straight through the sqlc query reused by the handler).
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

// TestRefreshEntityFilesEntityInexistenteDevolve404 covers the existence guard ("404 if the
// entity does not exist").
func TestRefreshEntityFilesEntityInexistenteDevolve404(t *testing.T) {
	h, _ := setupRefreshFilesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/inexistente/refresh-files", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestRefreshEntityFilesImportaAssetLocalEExecutaRecheck covers acceptance criteria 1-2: a
// poster.jpg placed in the folder of an entity that ALREADY EXISTS in the catalog (never seen
// by the 1st sweep) is imported as an origin='local' asset in this call, and the entity's
// 'present' media_file has its re-hash forced (files_rechecked=1).
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

	// No local asset at all before the call.
	if cnt := contarLinhas(t, srv.db, "SELECT count(*) FROM asset WHERE entity_id='e1' AND kind='poster' AND origin='local'"); cnt != 0 {
		t.Fatalf("asset poster local pré-existente = %d, quero 0", cnt)
	}

	// The owner places the poster AFTERWARDS -- exactly the case that used to be broken
	// (descobrirPoster only runs in the 1st sweep, inside criarEntity).
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

	// The media_file had its content_hash recomputed (forced recheck).
	var hashDepois sql.NullString
	if err := srv.db.Read.QueryRowContext(ctx, "SELECT content_hash FROM media_file WHERE id='mf1'").Scan(&hashDepois); err != nil {
		t.Fatalf("ler content_hash: %v", err)
	}
	if !hashDepois.Valid || hashDepois.String == "" {
		t.Error("content_hash vazio após refresh-files, quero preenchido (recheck forçado)")
	}
}

// nfoComCamposLocais builds a minimal .nfo (Kodi convention) carrying the 5 LOCAL playback
// fields covered by the endpoint.
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

// TestRefreshEntityFilesPreencheCamposLocaisAusentes covers acceptance criterion 3: with a
// `.nfo` carrying playcount/watched/userrating/resume and the database still without a value in
// those fields, the endpoint fills in all of them.
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

// TestRefreshEntityFilesNaoSobrescreveCampoJaPreenchido covers the most important acceptance
// criterion (non-destructive, AGENTS.md principle 5): running the endpoint with a value ALREADY
// present in the database, and a `.nfo` carrying a DIFFERENT value for the SAME field, does not
// overwrite -- the already filled field keeps the previous value.
func TestRefreshEntityFilesNaoSobrescreveCampoJaPreenchido(t *testing.T) {
	h, srv := setupRefreshFilesServer(t)
	ctx := context.Background()

	semearEntity(t, srv.db, "e1", "movie", "Filme")

	// user_rating ALREADY filled in the database -- it is the local source of truth, it cannot
	// lose to the .nfo (principle 1/5, AGENTS.md).
	if err := sqlcgen.New(srv.db.Write).PreencherCamposLocaisReproducaoSeAusentes(ctx, sqlcgen.PreencherCamposLocaisReproducaoSeAusentesParams{
		UserRating: sql.NullFloat64{Float64: 9.9, Valid: true},
		ID:         "e1",
	}); err != nil {
		t.Fatalf("semear user_rating: %v", err)
	}

	// The .nfo carries a DIFFERENT value for user_rating, and a NEW value for play_count (that
	// one is still absent from the database -- it must be filled in).
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
	// play_count, that one absent before -- it must have been filled in.
	if !ent.PlayCount.Valid || ent.PlayCount.Int64 != 7 {
		t.Errorf("PlayCount = %+v, quero 7 (preenchido -- estava ausente)", ent.PlayCount)
	}
}

// contarLinhas is a local shortcut for ad-hoc counts through the read pool (the same spirit as
// the `contar` helper of internal/scanner/scanner_test.go, which is not reachable from here
// because it lives in another package).
func contarLinhas(t *testing.T, d *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("contarLinhas (%s): %v", query, err)
	}
	return n
}
