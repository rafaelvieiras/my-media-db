package server

// Tests of ReprocessarArtesFaltantes: entities ALREADY identified (with external_ids) whose
// artwork inventory was never verified -- the same fakeArteProviderServer of
// materialize_missing_media_test.go, the same semearEntity/ligarFilhoOrdenado/semearFile
// helpers of retroativo_test.go.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// semearEntityComMbid creates an entity with external_ids_json={"mbid":mbid} -- the anchor that
// AtualizarInventarioArtes/idsExternosDe need to cross with a music ArteProvider (it mirrors
// semearEntityComTmdb, from materialize_missing_media_test.go, for the mbid source).
func semearEntityComMbid(t *testing.T, d *db.DB, id, tipo, titulo, mbid string) {
	t.Helper()
	extIDs, err := json.Marshal(map[string]string{"mbid": mbid})
	if err != nil {
		t.Fatalf("marshal external_ids: %v", err)
	}
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipo, Title: titulo, ExternalIdsJson: nsOf(string(extIDs)),
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

// setupReprocessarArtesServer starts a Server with the fakeArteProviderServer registered (which
// enables s.enrich), without an image allowlist (not exercised here -- the test looks only at
// the inventory envelope, not at the materialized binary).
func setupReprocessarArtesServer(t *testing.T, arte *fakeArteProviderServer) (*Server, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "reprocessar-artes.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return New("v-teste", database, WithAssetsDir(t.TempDir()), WithSearchProvider(arte)), database
}

// TestReprocessarArtesFaltantesArtistaJaIdentificado covers the central criterion: an artist that
// ALREADY has external_ids (mbid) but never had its artwork inventory verified (e.g. the
// ArteProvider was configured AFTER the import) gets the inventory on the next run of this
// method -- without needing the manual "Baixar Artes Faltantes" button per item.
func TestReprocessarArtesFaltantesArtistaJaIdentificado(t *testing.T) {
	arte := &fakeArteProviderServer{artes: []provider.Arte{
		{Kind: "artistthumb", URL: "https://assets.fanart.tv/thumb.jpg"},
	}}
	s, d := setupReprocessarArtesServer(t, arte)

	semearEntityComMbid(t, d, "art1", "artista", "a-ha", "mbid-1")
	semearEntity(t, d, "alb1", "album", "Hunting High and Low")
	semearEntity(t, d, "fx1", "faixa", "Take On Me")
	ligarFilhoOrdenado(t, d, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, d, "f1", "fx1", "/lib/musica/aha/01.flac", "present", 1, "")

	processados, err := s.ReprocessarArtesFaltantes(context.Background(), nil)
	if err != nil {
		t.Fatalf("ReprocessarArtesFaltantes: %v", err)
	}
	if processados != 1 {
		t.Fatalf("processados = %d, quero 1", processados)
	}
	if arte.chamadas != 1 {
		t.Errorf("BuscarArtes chamado %d vez(es), quero 1", arte.chamadas)
	}

	row, err := escrita(t, d).ObterMediaExternalPorFonte(context.Background(), sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: "art1", Source: "fanarttv",
	})
	if err != nil {
		t.Fatalf("inventario fanarttv ausente: %v", err)
	}
	if row.PayloadJson.String == "" {
		t.Error("payload do inventario fanarttv vazio")
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "art1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at deveria ter sido carimbado")
	}
}

// TestReprocessarArtesFaltantesRespeitaThrottle covers the throttle: an artist already verified
// recently (last_external_match_attempt_at stamped in this same test run, within the retryDays
// window) is NOT reprocessed again -- otherwise the job would hit the ArteProvider(s) on every
// run, without respecting retroactive_enrichment_days.
func TestReprocessarArtesFaltantesRespeitaThrottle(t *testing.T) {
	arte := &fakeArteProviderServer{artes: []provider.Arte{
		{Kind: "artistthumb", URL: "https://assets.fanart.tv/thumb.jpg"},
	}}
	s, d := setupReprocessarArtesServer(t, arte)

	semearEntityComMbid(t, d, "art1", "artista", "a-ha", "mbid-1")
	semearEntity(t, d, "alb1", "album", "Hunting High and Low")
	semearEntity(t, d, "fx1", "faixa", "Take On Me")
	ligarFilhoOrdenado(t, d, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, d, "f1", "fx1", "/lib/musica/aha/01.flac", "present", 1, "")

	if err := s.carimbarTentativaExterna(context.Background(), "art1"); err != nil {
		t.Fatalf("carimbarTentativaExterna: %v", err)
	}

	processados, err := s.ReprocessarArtesFaltantes(context.Background(), nil)
	if err != nil {
		t.Fatalf("ReprocessarArtesFaltantes: %v", err)
	}
	if processados != 0 {
		t.Errorf("processados = %d, quero 0 (dentro do throttle)", processados)
	}
	if arte.chamadas != 0 {
		t.Errorf("BuscarArtes chamado %d vez(es), quero 0 (dentro do throttle)", arte.chamadas)
	}
}

// TestReprocessarArtesFaltantesSemEnrichDegradaSemErro covers the s.enrich==nil guard (the same
// guard as EnriquecerRetroativo): without ANY external search provider registered, it degrades
// to (0, nil) without touching the database.
func TestReprocessarArtesFaltantesSemEnrichDegradaSemErro(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reprocessar-artes-sem-enrich.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database) // without WithSearchProvider: s.enrich stays nil

	processados, err := s.ReprocessarArtesFaltantes(context.Background(), nil)
	if err != nil {
		t.Fatalf("ReprocessarArtesFaltantes: %v", err)
	}
	if processados != 0 {
		t.Errorf("processados = %d, quero 0", processados)
	}
}
