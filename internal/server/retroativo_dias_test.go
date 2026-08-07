package server

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Testes do THROTTLE CONFIGURÁVEL do enriquecimento retroativo (briefing
// 0085): override opcional por library_source (retroactive_enrichment_days)
// sobre o valor global (app_settings.enrichment_retroativo.retry_days), a
// regra de ambiguidade (MENOR valor entre bibliotecas, ao contrário do
// idioma que cai pro global) e o resolvedor diasReentativaRetroativa.

// semearLibrarySourceComRetroDays insere uma library_source com
// retroactive_enrichment_days (nil = sem override), bypassando
// library.Service.Criar -- mesmo espírito de semearLibrarySourceComIdioma
// (idioma_biblioteca_test.go).
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

// semearFilePresenteComFonteEConfianca cria um media_file 'present' com
// match_confidence=70 (confTituloAno do scanner -- o alvo típico do
// enriquecimento retroativo) vinculado a uma library_source específica.
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

// ── ListarElegiveisEnriquecimentoRetroativo: override por biblioteca ───────

// TestListarElegiveis_OverrideMenorAntecipaElegibilidade cobre o critério
// §5: item cujo ÚNICO media_file pertence a uma biblioteca com override de 3
// dias E last_external_match_attempt_at há 4 dias aparece como elegível,
// mesmo com o global bem maior (30) -- o override vence.
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

// TestListarElegiveis_SemOverrideRespeitaGlobal cobre o critério §5: item de
// biblioteca SEM override, com last_external_match_attempt_at há MENOS dias
// que o global configurado, NÃO aparece como elegível.
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

// TestListarElegiveis_AmbiguidadeUsaMenorValor cobre o critério §5: entity
// com media_file de DUAS bibliotecas com overrides DIFERENTES (3 e 10 dias)
// fica elegível a partir de 3 dias -- usa o MENOR (ao contrário do idioma,
// que em ambiguidade cai pro global; aqui usar o menor nunca causa dano, ver
// §6 do briefing 0085).
func TestListarElegiveis_AmbiguidadeUsaMenorValor(t *testing.T) {
	_, d := setupEntitiesServer(t)
	tres, dez := 3, 10
	semearLibrarySourceComRetroDays(t, d, "src-3d", "/lib/3d", &tres)
	semearLibrarySourceComRetroDays(t, d, "src-10d", "/lib/10d", &dez)

	semearEntity(t, d, "e-ambiguo", "movie", "Ambiguo")
	semearFilePresenteComFonteEConfianca(t, d, "f-ambiguo-1", "e-ambiguo", "/lib/3d/f1.mkv", "src-3d")
	semearFilePresenteComFonteEConfianca(t, d, "f-ambiguo-2", "e-ambiguo", "/lib/10d/f2.mkv", "src-10d")
	// Tentado há 5 dias: > 3 (elegível pelo menor) mas < 10 (NÃO elegível
	// pelo maior) -- prova que a query usa o MENOR, não o maior nem o global.
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

// ── diasReentativaRetroativa (o resolvedor Go) ──────────────────────────────

// TestDiasReentativaRetroativa_SemArquivoCaiPraGlobal cobre entity sem
// media_file algum: cai direto no global (default 15, sem PUT prévio).
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

// TestDiasReentativaRetroativa_OverrideUnicoVence cobre o caso central: um
// único media_file present, cuja library_source tem override -> usa esse
// valor, não o global.
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

// TestDiasReentativaRetroativa_AmbiguidadeUsaMenor cobre a regra de
// ambiguidade: DUAS bibliotecas com overrides DIFERENTES -> usa o MENOR
// (diferente de idiomaDeBusca, que cairia pro global).
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

// TestDiasReentativaRetroativa_SemOverrideCaiPraGlobal cobre uma fonte SEM
// override: usa o global configurado via PUT /settings/enrichment-retroativo
// (cache invalidado, sem reiniciar o servidor).
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

	// Muda o global via PUT para provar que o resolvedor lê a config atual
	// (cache invalidado), não um valor hardcoded.
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
