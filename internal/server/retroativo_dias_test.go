package server

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Tests of the CONFIGURABLE THROTTLE of the retroactive enrichment: an optional override per
// library_source (retroactive_enrichment_days) on top of the global value
// (app_settings.enrichment_retroativo.retry_days), the ambiguity rule (the LOWEST value among
// libraries, unlike the language one, which falls back to the global) and the
// diasReentativaRetroativa resolver.

// semearLibrarySourceComRetroDays inserts a library_source with retroactive_enrichment_days
// (nil = no override), bypassing library.Service.Criar -- the same spirit as
// semearLibrarySourceComIdioma (idioma_biblioteca_test.go).
func semearLibrarySourceComRetroDays(t *testing.T, d *db.DB, id, path string, dias *int) {
	t.Helper()
	var ni sql.NullInt64
	if dias != nil {
		ni = sql.NullInt64{Int64: int64(*dias), Valid: true}
	}
	if _, err := sqlcgen.New(d.Write).CriarLibrarySource(context.Background(), sqlcgen.CriarLibrarySourceParams{
		ID: id, Path: path, Type: "movie", ContentType: "movie", RetroactiveEnrichmentDays: ni,
	}); err != nil {
		t.Fatalf("CriarLibrarySource(%s): %v", id, err)
	}
}

// semearFilePresenteComFonteEConfianca creates a 'present' media_file with
// match_confidence=70 (the scanner's confTituloAno -- the typical target of the retroactive
// enrichment) linked to a specific library_source.
func semearFilePresenteComFonteEConfianca(t *testing.T, d *db.DB, id, entityID, path, sourceID string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID:              id,
		EntityID:        entityID,
		Path:            path,
		State:           "present",
		MatchConfidence: sql.NullFloat64{Float64: 70, Valid: true},
		LibrarySourceID: sql.NullString{String: sourceID, Valid: true},
	}); err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

// ── ListarElegiveisEnriquecimentoRetroativo: per-library override ─────────

// TestListarElegiveis_OverrideMenorAntecipaElegibilidade covers criterion §5: an item whose
// ONLY media_file belongs to a library with a 3-day override AND
// last_external_match_attempt_at 4 days ago shows up as eligible, even with the global much
// larger (30) -- the override wins.
func TestListarElegiveis_OverrideMenorAntecipaElegibilidade(t *testing.T) {
	_, d := setupEntitiesServer(t)
	tres := 3
	semearLibrarySourceComRetroDays(t, d, "src-3d", "/lib/3d", &tres)

	semearEntity(t, d, "e-override", "movie", "Override")
	semearFilePresenteComFonteEConfianca(t, d, "f-override", "e-override", "/lib/3d/f.mkv", "src-3d")
	carimbarTentativa(t, d, "e-override", time.Now().Add(-4*24*time.Hour))

	rows, err := escrita(t, d).ListarElegiveisEnriquecimentoRetroativo(context.Background(),
		sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
			Limiar:     sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
			DiasGlobal: 30,
			Limite:     100,
		})
	if err != nil {
		t.Fatalf("ListarElegiveisEnriquecimentoRetroativo: %v", err)
	}
	if !idsDeRetroativo(rows)["e-override"] {
		t.Error("item com override de 3 dias tentado há 4 dias deveria ser elegível (override vence o global de 30)")
	}
}

// TestListarElegiveis_SemOverrideRespeitaGlobal covers criterion §5: an item from a library
// WITHOUT an override, with last_external_match_attempt_at FEWER days ago than the configured
// global, does NOT show up as eligible.
func TestListarElegiveis_SemOverrideRespeitaGlobal(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearLibrarySourceComRetroDays(t, d, "src-sem-override", "/lib/sem", nil)

	semearEntity(t, d, "e-sem-override", "movie", "SemOverride")
	semearFilePresenteComFonteEConfianca(t, d, "f-sem-override", "e-sem-override", "/lib/sem/f.mkv", "src-sem-override")
	carimbarTentativa(t, d, "e-sem-override", time.Now().Add(-4*24*time.Hour))

	rows, err := escrita(t, d).ListarElegiveisEnriquecimentoRetroativo(context.Background(),
		sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
			Limiar:     sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
			DiasGlobal: 30,
			Limite:     100,
		})
	if err != nil {
		t.Fatalf("ListarElegiveisEnriquecimentoRetroativo: %v", err)
	}
	if idsDeRetroativo(rows)["e-sem-override"] {
		t.Error("item sem override, tentado há 4 dias (< 30 dias global), NÃO deveria ser elegível")
	}
}

// TestListarElegiveis_AmbiguidadeUsaMenorValor covers criterion §5: an entity with media_file
// rows from TWO libraries with DIFFERENT overrides (3 and 10 days) becomes eligible from 3 days
// on -- it uses the LOWEST one (unlike the language, which falls back to the global on
// ambiguity; here using the lowest never causes harm).
func TestListarElegiveis_AmbiguidadeUsaMenorValor(t *testing.T) {
	_, d := setupEntitiesServer(t)
	tres, dez := 3, 10
	semearLibrarySourceComRetroDays(t, d, "src-3d", "/lib/3d", &tres)
	semearLibrarySourceComRetroDays(t, d, "src-10d", "/lib/10d", &dez)

	semearEntity(t, d, "e-ambiguo", "movie", "Ambiguo")
	semearFilePresenteComFonteEConfianca(t, d, "f-ambiguo-1", "e-ambiguo", "/lib/3d/f1.mkv", "src-3d")
	semearFilePresenteComFonteEConfianca(t, d, "f-ambiguo-2", "e-ambiguo", "/lib/10d/f2.mkv", "src-10d")
	// Attempted 5 days ago: > 3 (eligible through the lowest) but < 10 (NOT eligible through
	// the highest) -- it proves that the query uses the LOWEST, neither the highest nor the
	// global.
	carimbarTentativa(t, d, "e-ambiguo", time.Now().Add(-5*24*time.Hour))

	rows, err := escrita(t, d).ListarElegiveisEnriquecimentoRetroativo(context.Background(),
		sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
			Limiar:     sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
			DiasGlobal: 30,
			Limite:     100,
		})
	if err != nil {
		t.Fatalf("ListarElegiveisEnriquecimentoRetroativo: %v", err)
	}
	if !idsDeRetroativo(rows)["e-ambiguo"] {
		t.Error("entity com overrides ambíguos (3 e 10 dias), tentada há 5 dias, deveria ser elegível (usa o MENOR: 3)")
	}
}

// ── diasReentativaRetroativa (the Go resolver) ─────────────────────────────

// TestDiasReentativaRetroativa_SemArquivoCaiPraGlobal covers an entity without any
// media_file: it falls straight back to the global (default 15, with no prior PUT).
func TestDiasReentativaRetroativa_SemArquivoCaiPraGlobal(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Sem Arquivo")

	s := New("v-teste", d)
	dias, err := s.diasReentativaRetroativa(context.Background(), "e1")
	if err != nil {
		t.Fatalf("diasReentativaRetroativa: %v", err)
	}
	if dias != retryDaysPadrao {
		t.Errorf("dias = %d, quero %d (global default)", dias, retryDaysPadrao)
	}
}

// TestDiasReentativaRetroativa_OverrideUnicoVence covers the central case: a single present
// media_file whose library_source has an override -> it uses that value, not the global.
func TestDiasReentativaRetroativa_OverrideUnicoVence(t *testing.T) {
	_, d := setupEntitiesServer(t)
	cinco := 5
	semearLibrarySourceComRetroDays(t, d, "src-5d", "/lib/5d", &cinco)
	semearEntity(t, d, "e1", "movie", "Filme")
	semearFilePresenteComFonteEConfianca(t, d, "f1", "e1", "/lib/5d/f1.mkv", "src-5d")

	s := New("v-teste", d)
	dias, err := s.diasReentativaRetroativa(context.Background(), "e1")
	if err != nil {
		t.Fatalf("diasReentativaRetroativa: %v", err)
	}
	if dias != 5 {
		t.Errorf("dias = %d, quero 5 (override da biblioteca)", dias)
	}
}

// TestDiasReentativaRetroativa_AmbiguidadeUsaMenor covers the ambiguity rule: TWO libraries
// with DIFFERENT overrides -> it uses the LOWEST one (unlike idiomaDeBusca, which would fall
// back to the global).
func TestDiasReentativaRetroativa_AmbiguidadeUsaMenor(t *testing.T) {
	_, d := setupEntitiesServer(t)
	tres, dez := 3, 10
	semearLibrarySourceComRetroDays(t, d, "src-3d", "/lib/3d", &tres)
	semearLibrarySourceComRetroDays(t, d, "src-10d", "/lib/10d", &dez)
	semearEntity(t, d, "e1", "movie", "Filme")
	semearFilePresenteComFonteEConfianca(t, d, "f1", "e1", "/lib/3d/f1.mkv", "src-3d")
	semearFilePresenteComFonteEConfianca(t, d, "f2", "e1", "/lib/10d/f2.mkv", "src-10d")

	s := New("v-teste", d)
	dias, err := s.diasReentativaRetroativa(context.Background(), "e1")
	if err != nil {
		t.Fatalf("diasReentativaRetroativa: %v", err)
	}
	if dias != 3 {
		t.Errorf("dias = %d, quero 3 (o MENOR entre os overrides ambíguos)", dias)
	}
}

// TestDiasReentativaRetroativa_SemOverrideCaiPraGlobal covers a source WITHOUT an override: it
// uses the global configured through PUT /settings/enrichment-retroativo (cache invalidated,
// without restarting the server).
func TestDiasReentativaRetroativa_SemOverrideCaiPraGlobal(t *testing.T) {
	dbPath := t.TempDir() + "/retro-dias.sqlite"
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database)
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	semearLibrarySourceComRetroDays(t, database, "src-sem", "/lib/sem", nil)
	semearEntity(t, database, "e1", "movie", "Filme")
	semearFilePresenteComFonteEConfianca(t, database, "f1", "e1", "/lib/sem/f1.mkv", "src-sem")

	// Changes the global through PUT to prove that the resolver reads the current config (cache
	// invalidated), not a hardcoded value.
	rec := do(t, h, "PUT", "/api/v1/settings/enrichment-retroativo", retroactiveEnrichmentSettings{RetryDays: 20})
	if rec.Code != 200 {
		t.Fatalf("PUT settings: status = %d, corpo=%s", rec.Code, rec.Body.String())
	}

	dias, err := s.diasReentativaRetroativa(context.Background(), "e1")
	if err != nil {
		t.Fatalf("diasReentativaRetroativa: %v", err)
	}
	if dias != 20 {
		t.Errorf("dias = %d, quero 20 (global configurado via PUT)", dias)
	}
}
