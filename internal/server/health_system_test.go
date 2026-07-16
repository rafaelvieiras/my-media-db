package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupSystemHealthServer sobe um Server com banco temporário migrado,
// devolvendo o *Server (não só o Handler) — os testes de cache/TTL precisam
// mexer em s.telemetryCache.agora, um campo não-exportado só acessível de
// dentro do pacote.
func setupSystemHealthServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "system-health.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return New("v-teste", database), database
}

// doSystemHealth chama o handler diretamente (mesmo padrão de doHealth em
// health_test.go) e devolve o recorder cru — os testes decidem se checam
// status 200 ou erro.
func doSystemHealth(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/system", nil)
	rec := httptest.NewRecorder()
	s.systemHealthHandler(rec, req)
	return rec
}

// semearLibrarySource insere uma library_source direto pelo sqlc (bypassa a
// validação de path de library.Service.Criar — permite testar path
// inacessível sem exercitar aquele fluxo).
func semearLibrarySource(t *testing.T, d *db.DB, id, path string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarLibrarySource(context.Background(), sqlcgen.CriarLibrarySourceParams{
		ID: id, Path: path, Type: "movie", ContentType: "movie",
	}); err != nil {
		t.Fatalf("CriarLibrarySource(%s): %v", id, err)
	}
}

// semearFileComFonte cria um media_file ligado a uma library_source_id (o
// semearFile de entities_test.go não cobre esse campo).
func semearFileComFonte(t *testing.T, d *db.DB, id, entityID, path, state string, size int64, sourceID string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID: id, EntityID: entityID, Path: path, State: state,
		Size:            sql.NullInt64{Int64: size, Valid: true},
		LibrarySourceID: sql.NullString{String: sourceID, Valid: true},
	}); err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

// TestSystemHealthSemBancoDevolve503 prova que sem banco injetado o endpoint
// novo degrada com 503 no envelope de erro padrão — e que isso NÃO afeta
// GET /api/v1/health (código independente, contrato do 0001/0006
// preservado).
func TestSystemHealthSemBancoDevolve503(t *testing.T) {
	s := New("v-teste", nil)

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "db_unavailable")

	// GET /health básico continua 200, mesmo sem banco.
	basico := doHealth(t, s)
	if basico.Status != "ok" {
		t.Fatalf("GET /health status = %q, quero \"ok\" mesmo sem banco", basico.Status)
	}
}

// TestSystemHealthShapeCompleto prova que, com banco e ao menos uma
// library_source, o endpoint devolve 200 com banco/uso/armazenamento
// preenchidos, e que database.size_bytes bate com uma leitura PRAGMA direta
// no mesmo instante (critérios de aceite do briefing 0055).
func TestSystemHealthShapeCompleto(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	dir := t.TempDir()
	semearLibrarySource(t, d, "src-1", dir)
	semearEntity(t, d, "e1", "movie", "Filme 1")
	semearFileComFonte(t, d, "f1", "e1", filepath.Join(dir, "f1.mkv"), "present", 12345, "src-1")

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar systemHealthResponse: %v", err)
	}

	var pageCount, pageSize int64
	if err := d.Read.QueryRowContext(context.Background(), "PRAGMA page_count").Scan(&pageCount); err != nil {
		t.Fatalf("PRAGMA page_count direto: %v", err)
	}
	if err := d.Read.QueryRowContext(context.Background(), "PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatalf("PRAGMA page_size direto: %v", err)
	}
	if out.Database.SizeBytes != pageCount*pageSize {
		t.Errorf("database.size_bytes = %d, quero %d (page_count*page_size)", out.Database.SizeBytes, pageCount*pageSize)
	}
	if out.Database.SchemaVersion != 14 {
		t.Errorf("database.schema_version = %d, quero 14", out.Database.SchemaVersion)
	}

	if len(out.Storage.Sources) != 1 {
		t.Fatalf("storage.sources = %d itens, quero 1", len(out.Storage.Sources))
	}
	src := out.Storage.Sources[0]
	if src.SourceID != "src-1" || src.Path != dir {
		t.Errorf("storage.sources[0] = %+v, quero source_id=src-1 path=%s", src, dir)
	}
	if src.UsedBytes != 12345 {
		t.Errorf("storage.sources[0].used_bytes = %d, quero 12345", src.UsedBytes)
	}
	if src.Offline {
		t.Error("storage.sources[0].offline = true, quero false (path existe)")
	}
	if src.Filesystem == nil || src.Filesystem.TotalBytes == 0 {
		t.Error("storage.sources[0].filesystem = nil/zero, quero preenchido (path real)")
	}
}

// TestSystemHealthEntitiesByType prova que usage.entities.by_type reflete as
// contagens reais por type de um fixture misto (movie/season/episode/
// collection) — critério de aceite do briefing 0055.
func TestSystemHealthEntitiesByType(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	semearEntity(t, d, "m1", "movie", "Filme 1")
	semearEntity(t, d, "m2", "movie", "Filme 2")
	semearEntity(t, d, "s1", "season", "Temporada 1")
	semearEntity(t, d, "ep1", "episode", "Episódio 1")
	semearEntity(t, d, "c1", "collection", "Coleção 1")

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	want := map[string]int64{"movie": 2, "season": 1, "episode": 1, "collection": 1}
	for tipo, total := range want {
		if out.Usage.Entities.ByType[tipo] != total {
			t.Errorf("entities.by_type[%s] = %d, quero %d", tipo, out.Usage.Entities.ByType[tipo], total)
		}
	}
	if len(out.Usage.Entities.ByType) != len(want) {
		t.Errorf("entities.by_type tem %d chaves, quero %d (so tipos que ocorrem)", len(out.Usage.Entities.ByType), len(want))
	}
	if out.Usage.Entities.Total != 5 {
		t.Errorf("entities.total = %d, quero 5", out.Usage.Entities.Total)
	}
}

// TestSystemHealthFilaBateComReviewEOrphans prova que
// usage.review_queue_size/orphans_count batem EXATAMENTE com o tamanho que
// GET /review e GET /orphans devolveriam para o MESMO fixture (mesma
// garantia que o 0053 já prova para internal/curation).
func TestSystemHealthFilaBateComReviewEOrphans(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	// e-draft: não identificado -> entra em revisão.
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")

	// e-baixa: present com confiança 40 (< 70) -> entra em revisão.
	semearEntity(t, d, "e-baixa", "movie", "Baixa Confianca")
	semearFile(t, d, "f-baixa", "e-baixa", "/x/baixa.mkv", "present", 40, "")

	// e-alta: present com confiança 95 -> não entra em revisão nem órfãs.
	semearEntity(t, d, "e-alta", "movie", "Alta Confianca")
	semearFile(t, d, "f-alta", "e-alta", "/x/alta.mkv", "present", 95, "")

	// e-orfa: todos os arquivos missing, sem investimento -> órfã.
	semearEntity(t, d, "e-orfa", "movie", "Orfa")
	semearFile(t, d, "f-orfa", "e-orfa", "/x/orfa.mkv", "missing", 0, "")

	// Chama /review e /orphans de verdade para comparar com o mesmo fixture.
	recReview := httptest.NewRecorder()
	s.listReview(recReview, httptest.NewRequest(http.MethodGet, "/api/v1/review", nil))
	var review []entityCardDTO
	if err := json.Unmarshal(recReview.Body.Bytes(), &review); err != nil {
		t.Fatalf("decodificar /review: %v", err)
	}

	recOrphans := httptest.NewRecorder()
	s.listOrphans(recOrphans, httptest.NewRequest(http.MethodGet, "/api/v1/orphans", nil))
	var orphans []entityCardDTO
	if err := json.Unmarshal(recOrphans.Body.Bytes(), &orphans); err != nil {
		t.Fatalf("decodificar /orphans: %v", err)
	}

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	if out.Usage.ReviewQueueSize != len(review) {
		t.Errorf("usage.review_queue_size = %d, quero %d (== GET /review)", out.Usage.ReviewQueueSize, len(review))
	}
	if out.Usage.OrphansCount != len(orphans) {
		t.Errorf("usage.orphans_count = %d, quero %d (== GET /orphans)", out.Usage.OrphansCount, len(orphans))
	}
	// Trava os números concretos também, não só a igualdade cruzada.
	if len(review) != 2 {
		t.Errorf("len(review) = %d, quero 2 (draft + baixa confiança)", len(review))
	}
	if len(orphans) != 1 {
		t.Errorf("len(orphans) = %d, quero 1", len(orphans))
	}
}

// TestSystemHealthUsedBytesSemWalkDir prova que storage.sources[].used_bytes
// bate com a soma de media_file.size da fonte com state='present' — o
// critério "sem WalkDir" é estrutural (UsoDisco só roda SUM sqlc, não há
// nenhuma chamada a filepath.WalkDir no code path deste endpoint, ver
// internal/telemetry/telemetry.go); aqui provamos o número batendo mesmo com
// arquivo missing e de outra fonte misturados.
func TestSystemHealthUsedBytesSemWalkDir(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	dirA := t.TempDir()
	dirB := t.TempDir()
	semearLibrarySource(t, d, "src-a", dirA)
	semearLibrarySource(t, d, "src-b", dirB)

	semearEntity(t, d, "ea1", "movie", "A1")
	semearEntity(t, d, "ea2", "movie", "A2")
	semearEntity(t, d, "eb1", "movie", "B1")
	semearFileComFonte(t, d, "fa1", "ea1", filepath.Join(dirA, "a1.mkv"), "present", 1000, "src-a")
	semearFileComFonte(t, d, "fa2", "ea2", filepath.Join(dirA, "a2.mkv"), "missing", 5000, "src-a")
	semearFileComFonte(t, d, "fb1", "eb1", filepath.Join(dirB, "b1.mkv"), "present", 2000, "src-b")

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	got := map[string]int64{}
	for _, src := range out.Storage.Sources {
		got[src.SourceID] = src.UsedBytes
	}
	if got["src-a"] != 1000 {
		t.Errorf("used_bytes[src-a] = %d, quero 1000 (so o present)", got["src-a"])
	}
	if got["src-b"] != 2000 {
		t.Errorf("used_bytes[src-b] = %d, quero 2000", got["src-b"])
	}
}

// TestSystemHealthFonteOfflineNaoAbortaAsDemais prova que uma fonte com Path
// inacessível aparece com offline:true/filesystem:null (com o campo error
// preenchido), SEM abortar as demais fontes nem devolver 5xx para o
// endpoint inteiro — critério de aceite do briefing 0055.
func TestSystemHealthFonteOfflineNaoAbortaAsDemais(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	dirOK := t.TempDir()
	semearLibrarySource(t, d, "src-ok", dirOK)
	semearLibrarySource(t, d, "src-offline", "/caminho/que/definitivamente/nao/existe/xyz123")

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 mesmo com uma fonte offline (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	if len(out.Storage.Sources) != 2 {
		t.Fatalf("storage.sources = %d itens, quero 2 (as duas fontes, nenhuma abortada)", len(out.Storage.Sources))
	}
	byID := map[string]sourceStorageDTO{}
	for _, src := range out.Storage.Sources {
		byID[src.SourceID] = src
	}

	ok := byID["src-ok"]
	if ok.Offline || ok.Filesystem == nil {
		t.Errorf("src-ok = %+v, quero offline=false com filesystem preenchido", ok)
	}

	off := byID["src-offline"]
	if !off.Offline {
		t.Errorf("src-offline.offline = false, quero true")
	}
	if off.Filesystem != nil {
		t.Errorf("src-offline.filesystem = %+v, quero nil", off.Filesystem)
	}
	if off.Error == "" {
		t.Error("src-offline.error vazio, quero mensagem de erro")
	}
}

// TestSystemHealthCacheDentroDoTTLNaoRecalcula prova que duas chamadas
// dentro do TTL devolvem o MESMO computed_at (segundo request não
// recalculou), e que uma chamada após o TTL expirar devolve um computed_at
// novo — critério de aceite do briefing 0055.
func TestSystemHealthCacheDentroDoTTLNaoRecalcula(t *testing.T) {
	s, d := setupSystemHealthServer(t)
	semearLibrarySource(t, d, "src-1", t.TempDir())

	// Relógio controlado: o teste avança o tempo manualmente, sem sleep real.
	agora := time.Now()
	s.telemetryCache.agora = func() time.Time { return agora }

	rec1 := doSystemHealth(t, s)
	var out1 systemHealthResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &out1); err != nil {
		t.Fatalf("decodificar 1a chamada: %v", err)
	}

	// Ainda dentro do TTL (avança bem menos que ttlTelemetria).
	agora = agora.Add(1 * time.Second)
	rec2 := doSystemHealth(t, s)
	var out2 systemHealthResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &out2); err != nil {
		t.Fatalf("decodificar 2a chamada: %v", err)
	}
	if out1.ComputedAt != out2.ComputedAt {
		t.Errorf("computed_at mudou dentro do TTL: %q -> %q, queria igual (cache hit)", out1.ComputedAt, out2.ComputedAt)
	}

	// Expira o TTL: nova chamada deve recalcular. ComputedAt vem do RELOGIO
	// REAL (telemetry.Servico.Snapshot usa time.Now(), independente do agora
	// mockado do cache, que só controla a expiracao) e é truncado a
	// milissegundos na serializacao -- um sleep curto real evita a rara
	// coincidencia de duas chamadas caindo no mesmo milissegundo.
	time.Sleep(2 * time.Millisecond)
	agora = agora.Add(ttlTelemetria + time.Second)
	rec3 := doSystemHealth(t, s)
	var out3 systemHealthResponse
	if err := json.Unmarshal(rec3.Body.Bytes(), &out3); err != nil {
		t.Fatalf("decodificar 3a chamada: %v", err)
	}
	if out3.ComputedAt == out1.ComputedAt {
		t.Error("computed_at não mudou após o TTL expirar, queria um novo cálculo")
	}
}

// TestSystemHealthNaoAlteraHealthBasico é uma prova estrutural complementar
// ao 503 acima: com banco no ar, GET /health continua no shape antigo
// (status/version/db) mesmo depois de bater em /health/system — os dois
// handlers não compartilham estado que vaze contrato.
func TestSystemHealthNaoAlteraHealthBasico(t *testing.T) {
	s, d := setupSystemHealthServer(t)
	semearLibrarySource(t, d, "src-1", t.TempDir())

	_ = doSystemHealth(t, s)

	basico := doHealth(t, s)
	if basico.Status != "ok" || basico.Version != "v-teste" || !basico.DB.OK || basico.DB.Schema != 14 {
		t.Errorf("GET /health mudou de shape/valor apos /health/system: %+v", basico)
	}
}
