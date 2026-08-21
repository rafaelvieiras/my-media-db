package server

// Testes de ReprocessarArtesFaltantes (briefing 0118): entities JÁ
// identificadas (com external_ids) cujo inventário de arte nunca foi
// verificado -- mesmo fakeArteProviderServer de
// materialize_missing_media_test.go, mesmos helpers semearEntity/
// ligarFilhoOrdenado/semearFile de retroativo_test.go.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// semearEntityComMbid cria uma entity com external_ids_json={"mbid":mbid} --
// a âncora que AtualizarInventarioArtes/idsExternosDe precisa para cruzar com
// um ArteProvider de música (espelha semearEntityComTmdb, de
// materialize_missing_media_test.go, para a fonte mbid).
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

// setupReprocessarArtesServer sobe um Server com o fakeArteProviderServer
// registrado (liga s.enrich), sem allowlist de imagem (não exercitada aqui --
// o teste olha só o envelope de inventário, não o binário materializado).
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

// TestReprocessarArtesFaltantesArtistaJaIdentificado cobre o critério central
// do briefing 0118: um artista que JÁ tem external_ids (mbid) mas nunca teve
// o inventário de arte verificado (ex.: o ArteProvider foi configurado DEPOIS
// do import) ganha o inventário na próxima passada deste método -- sem
// precisar do botão manual "Baixar Artes Faltantes" por item.
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

	processados, err := s.ReprocessarArtesFaltantes(context.Background())
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

// TestReprocessarArtesFaltantesRespeitaThrottle cobre o throttle: um artista
// já verificado recentemente (last_external_match_attempt_at carimbado nesta
// mesma execução de teste, dentro da janela de retryDays) NÃO é reprocessado
// de novo -- senão o job bateria no(s) ArteProvider(s) a cada execução, sem
// respeitar retroactive_enrichment_days.
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

	processados, err := s.ReprocessarArtesFaltantes(context.Background())
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

// TestReprocessarArtesFaltantesSemEnrichDegradaSemErro cobre a guarda
// s.enrich==nil (mesma guarda de EnriquecerRetroativo): sem NENHUM provider
// de busca externa registrado, degrada para (0, nil) sem tocar o banco.
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
	s := New("v-teste", database) // sem WithSearchProvider: s.enrich fica nil

	processados, err := s.ReprocessarArtesFaltantes(context.Background())
	if err != nil {
		t.Fatalf("ReprocessarArtesFaltantes: %v", err)
	}
	if processados != 0 {
		t.Errorf("processados = %d, quero 0", processados)
	}
}
