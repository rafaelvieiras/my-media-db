package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// Tests of the CURATION QUEUES: review, orphans, confirm, correct and purge. They seed
// entities/media_files/override/asset straight through the write pool and exercise the routes
// via httptest. They cover the acceptance criteria.

// definirStatus changes the entity status straight in the database (CriarEntity is always born
// 'cataloged'; the review queue needs 'draft', the orphan one needs 'wanted', etc.).
func definirStatus(t *testing.T, d *db.DB, id, status string) {
	t.Helper()
	if _, err := d.Write.ExecContext(context.Background(),
		"UPDATE entity SET status = ? WHERE id = ?", status, id); err != nil {
		t.Fatalf("definirStatus(%s=%s): %v", id, status, err)
	}
}

// semearOverride writes a minimal override for the entity (it marks "owner investment" in the
// orphan predicate).
func semearOverride(t *testing.T, d *db.DB, entityID, blob string) {
	t.Helper()
	if _, err := escrita(t, d).UpsertMediaOverride(context.Background(), sqlcgen.UpsertMediaOverrideParams{
		EntityID: entityID, OverrideJson: nsOf(blob),
	}); err != nil {
		t.Fatalf("semearOverride(%s): %v", entityID, err)
	}
}

// semearAssetUser writes an origin='user' asset (the owner's artwork; investment in the orphan
// predicate).
func semearAssetUser(t *testing.T, d *db.DB, id, entityID, kind, path string) {
	t.Helper()
	if _, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID: id, EntityID: entityID, Kind: kind, Origin: "user", PathOrUri: path,
	}); err != nil {
		t.Fatalf("semearAssetUser(%s): %v", id, err)
	}
}

// decodeCards decodes the ARRAY-of-cards response of the /review and /orphans queues.
func decodeCards(t *testing.T, rec *httptest.ResponseRecorder) []entityCardDTO {
	t.Helper()
	var out []entityCardDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar []entityCardDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return out
}

// idsDe extracts the ids of a list of cards, for membership asserts.
func idsDe(cards []entityCardDTO) map[string]bool {
	m := make(map[string]bool, len(cards))
	for _, c := range cards {
		m[c.ID] = true
	}
	return m
}

// TestFilaRevisaoClassifica covers criterion §5.1 (classification): the queue lists
// draft + low confidence (present, 0<conf<70) and EXCLUDES high confidence, missing and
// the entity that has NO file at all -- a hole in the collection is not a decision.
func TestFilaRevisaoClassifica(t *testing.T) {
	h, d := setupEntitiesServer(t)

	// draft (unidentified) WITH a file — it enters by status.
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/draft.mkv", "missing", 0, "")
	// draft imported from the provider, WITHOUT any file — it does NOT enter: it is a hole
	// in the collection (422 episodes measured in production, all inside cataloged series).
	semearEntity(t, d, "e-draft-sem-arquivo", "episode", "Draft sem arquivo")
	definirStatus(t, d, "e-draft-sem-arquivo", "draft")
	// low confidence, present file — it enters through the confidence predicate.
	semearEntity(t, d, "e-baixa", "movie", "Baixa")
	semearFile(t, d, "f-baixa", "e-baixa", "/lib/baixa.mkv", "present", 60, "")
	// high confidence present — it does NOT enter (>= threshold 70).
	semearEntity(t, d, "e-alta", "movie", "Alta")
	semearFile(t, d, "f-alta", "e-alta", "/lib/alta.mkv", "present", 90, "")
	// low confidence but MISSING — it does NOT enter (state != present).
	semearEntity(t, d, "e-missing", "movie", "Missing")
	semearFile(t, d, "f-missing", "e-missing", "/lib/missing.mkv", "missing", 60, "")

	cards := decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil))
	got := idsDe(cards)
	if !got["e-draft"] {
		t.Error("fila de revisão não incluiu o draft")
	}
	if !got["e-baixa"] {
		t.Error("fila de revisão não incluiu a baixa confiança present")
	}
	if got["e-alta"] {
		t.Error("fila de revisão incluiu alta confiança (>= limiar)")
	}
	if got["e-missing"] {
		t.Error("fila de revisão incluiu baixa confiança MISSING")
	}
	if got["e-draft-sem-arquivo"] {
		t.Error("fila de revisão incluiu item sem arquivo (buraco de acervo, não decisão)")
	}
}

// TestFilaRevisaoVaziaLista200 covers the contract: 200 with [] when there is nothing.
func TestFilaRevisaoVaziaLista200(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/review", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	if len(decodeCards(t, rec)) != 0 {
		t.Error("fila de revisão vazia deveria ser []")
	}
}

// TestConfirmarSobeConfianca covers criterion §5.1 (confirm): confirming raises the confidence
// and the item leaves the review queue. 404 for an unknown entity.
func TestConfirmarSobeConfianca(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Baixa")
	semearFile(t, d, "f1", "e1", "/lib/x.mkv", "present", 55, "")

	// Before: it is in the queue.
	if !idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e1"] {
		t.Fatal("pré-condição: e1 deveria estar na fila de revisão")
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/confirm", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}

	// After: it left the queue (the confidence rose to 100).
	if idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e1"] {
		t.Error("e1 ainda na fila de revisão após confirmar")
	}
	got, err := escrita(t, d).ObterMediaFile(context.Background(), "f1")
	if err != nil {
		t.Fatalf("ObterMediaFile: %v", err)
	}
	if !got.MatchConfidence.Valid || got.MatchConfidence.Float64 != 100 {
		t.Errorf("match_confidence = %v, quero 100", got.MatchConfidence)
	}
}

// statusDe reads the current entity status straight from the database (for promotion
// asserts).
func statusDe(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	ent, err := escrita(t, d).ObterEntity(context.Background(), id)
	if err != nil {
		t.Fatalf("ObterEntity(%s): %v", id, err)
	}
	return ent.Status
}

// TestConfirmarPromoveDraft covers the STATUS branch: confirming a draft (unidentified) must
// promote it to 'cataloged' and take it out of the queue — otherwise the confidence rises to
// 100 but the item stays stuck as a draft (a regression caught live).
func TestConfirmarPromoveDraft(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e-draft", "movie", "Sem Identidade")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/x.mkv", "present", 0, "")

	if !idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e-draft"] {
		t.Fatal("pré-condição: e-draft deveria estar na fila de revisão")
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/confirm", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if s := statusDe(t, d, "e-draft"); s != "cataloged" {
		t.Errorf("status = %q após confirmar, quero cataloged", s)
	}
	if idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e-draft"] {
		t.Error("e-draft ainda na fila de revisão após confirmar (draft não promovido)")
	}
}

func TestConfirmarInexistente404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/confirm", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestCorrigirGravaExternalIdsEReResolve covers criteria §5.1 (correct) and §5.4 (identity
// wins, reflected in the resolver): it corrects with the fake provider, writes external_ids,
// re-enriches (the external title composes), raises the confidence and re-resolves.
func TestCorrigirGravaExternalIdsEReResolve(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	// Entity wrongly matched by the scanner: low confidence, without an external id.
	semearEntity(t, d, "e1", "movie", "Palpite Errado")
	semearFile(t, d, "f1", "e1", "/lib/x.mkv", "present", 40, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/correct",
		map[string]string{"source_id": "555"})
	if rec.Code != http.StatusOK {
		t.Fatalf("correct status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	// Identity written (default source tmdb) and the external composition reflected.
	eq(t, "external_ids.tmdb", ent.ExternalIDs["tmdb"], "555")
	eq(t, "title(re-resolvido)", ent.Title, "Filme Externo")

	// Confidence rose → it leaves the review queue.
	if idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e1"] {
		t.Error("e1 ainda na fila de revisão após corrigir")
	}
	got, err := escrita(t, d).ObterMediaFile(context.Background(), "f1")
	if err != nil {
		t.Fatalf("ObterMediaFile: %v", err)
	}
	if !got.MatchConfidence.Valid || got.MatchConfidence.Float64 != 100 {
		t.Errorf("match_confidence = %v, quero 100", got.MatchConfidence)
	}
}

// TestCorrigirPromoveDraft covers the STATUS branch on correction: correcting a draft (the
// natural path to identify an unidentified item) writes the identity, re-enriches AND promotes
// it to 'cataloged', taking it out of the queue. A regression caught live.
func TestCorrigirPromoveDraft(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	semearEntity(t, d, "e-draft", "movie", "Sem Identidade")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/x.mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/correct",
		map[string]string{"source_id": "555"})
	if rec.Code != http.StatusOK {
		t.Fatalf("correct status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if s := statusDe(t, d, "e-draft"); s != "cataloged" {
		t.Errorf("status = %q após corrigir, quero cataloged", s)
	}
	if idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e-draft"] {
		t.Error("e-draft ainda na fila de revisão após corrigir (draft não promovido)")
	}
}

// TestCorrigirSerieConstroiArvore covers the series tree: "Corrigir" on a type='tv' entity
// without a tree (aplicarIdentidadeExterna -> s.enrich.Enriquecer) builds season/episode as
// child entities, exactly as POST /import already did -- GET /entities/{id}/members stops
// coming out empty. It uses the canned fake.Provider fixture (the same tree as
// internal/enrich/enrich_arvore_test.go: 2 seasons of 3 episodes).
func TestCorrigirSerieConstroiArvore(t *testing.T) {
	img := servidorImagemArvoreSerie(t)
	h, d := setupSearchServer(t, WithSearchProvider(fake.New(img.URL)), WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "s1", "tv", "Serie Sem Arvore")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/s1/correct",
		map[string]string{"source_id": fake.SerieFixtureID})
	if rec.Code != http.StatusOK {
		t.Fatalf("correct status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	eq(t, "external_ids.tmdb", ent.ExternalIDs["tmdb"], fake.SerieFixtureID)

	temporadas := membrosDe(t, h, "s1")
	if len(temporadas) != 2 {
		t.Fatalf("members(s1) = %d, quero 2 temporadas (Corrigir sincronizou a arvore, 0067)", len(temporadas))
	}
	totalEpisodios := 0
	for _, temp := range temporadas {
		totalEpisodios += len(membrosDe(t, h, temp.ID))
	}
	if totalEpisodios != 6 {
		t.Errorf("episodios = %d, quero 6", totalEpisodios)
	}
}

func TestCorrigirSemSourceID422(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/correct", map[string]string{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

func TestCorrigirInexistente404(t *testing.T) {
	h, _, _, _, _ := setupEnrichServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/correct",
		map[string]string{"source_id": "1"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestCorrigirEnrichDesligado503 covers the requirement of an enabled enrich service: without a
// provider/assetsDir, correct answers 503 (like enrich/import).
func TestCorrigirEnrichDesligado503(t *testing.T) {
	h, d := setupEntitiesServer(t) // without WithSearchProvider/WithAssetsDir => enrich nil
	semearEntity(t, d, "e1", "movie", "X")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/correct",
		map[string]string{"source_id": "1"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "external_source_unavailable")
}

// TestFilaOrfas covers criterion §5.2: it lists only cataloged + with file(s) + ALL missing +
// without owner investment; it EXCLUDES an item with an override or a user asset (the key
// test), with ownership, with an intention status (wanted) and imported-only.
func TestFilaOrfas(t *testing.T) {
	h, d := setupEntitiesServer(t)

	// Canonical orphan: cataloged, 1 missing file, no investment.
	semearEntity(t, d, "orfa", "movie", "Orfa")
	semearFile(t, d, "f-orfa", "orfa", "/lib/orfa.mkv", "missing", 80, "")

	// It has ownership (present file) — it is NOT an orphan.
	semearEntity(t, d, "com-posse", "movie", "ComPosse")
	semearFile(t, d, "f-posse", "com-posse", "/lib/posse.mkv", "present", 80, "")

	// wanted (owner intention), missing file — it is NOT an orphan.
	semearEntity(t, d, "wanted", "movie", "Wanted")
	semearFile(t, d, "f-wanted", "wanted", "/lib/wanted.mkv", "missing", 80, "")
	definirStatus(t, d, "wanted", "wanted")

	// Imported only (no media_file) — it is NOT an orphan.
	semearEntity(t, d, "so-import", "movie", "SoImport")

	// Orphan candidate, BUT with an owner override — it is NOT an orphan (investment).
	semearEntity(t, d, "com-override", "movie", "ComOverride")
	semearFile(t, d, "f-ov", "com-override", "/lib/ov.mkv", "missing", 80, "")
	semearOverride(t, d, "com-override", `{"title":{"pt-BR":"Meu Titulo"}}`)

	// Orphan candidate, BUT with an origin='user' asset — it is NOT an orphan (investment).
	semearEntity(t, d, "com-asset", "movie", "ComAsset")
	semearFile(t, d, "f-as", "com-asset", "/lib/as.mkv", "missing", 80, "")
	semearAssetUser(t, d, "a-user", "com-asset", "poster", "/assets/com-asset/user-a.png")

	got := idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/orphans", nil)))
	if !got["orfa"] {
		t.Error("fila de órfãs não incluiu a órfã canônica")
	}
	for _, id := range []string{"com-posse", "wanted", "so-import", "com-override", "com-asset"} {
		if got[id] {
			t.Errorf("fila de órfãs incluiu %q, que NÃO é órfã", id)
		}
	}
}

// TestPurgaIndividualRemoveDerivados covers criterion §5.3: DELETE removes the entity and its
// derivatives (media_file/override/asset through CASCADE), without affecting other entities.
func TestPurgaIndividualRemoveDerivados(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Alvo")
	semearFile(t, d, "f1", "e1", "/lib/x.mkv", "missing", 80, "")
	semearOverride(t, d, "e1", `{"title":{"pt-BR":"T"}}`)
	semearAssetUser(t, d, "a1", "e1", "poster", "/assets/e1/user-a.png")
	// A neighbor that must SURVIVE.
	semearEntity(t, d, "e2", "movie", "Vizinha")
	semearFile(t, d, "f2", "e2", "/lib/y.mkv", "present", 80, "")

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var body purgaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.Purged {
		t.Fatalf("corpo = %s, quero {purged:true}", rec.Body.String())
	}

	// Entity and derivatives are gone (CASCADE).
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e1"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("ObterEntity(e1) err = %v, quero ErrNoRows", err)
	}
	if _, err := escrita(t, d).ObterMediaFile(context.Background(), "f1"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("media_file f1 sobreviveu, quero CASCADE (err=%v)", err)
	}
	if _, err := escrita(t, d).ObterMediaOverride(context.Background(), "e1"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("media_override e1 sobreviveu, quero CASCADE (err=%v)", err)
	}
	if _, err := escrita(t, d).ObterAsset(context.Background(), "a1"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("asset a1 sobreviveu, quero CASCADE (err=%v)", err)
	}
	// Neighbor intact.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e2"); err != nil {
		t.Errorf("vizinha e2 foi afetada: %v", err)
	}
}

func TestPurgaIndividualInexistente404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodDelete, "/api/v1/entities/naoexiste", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestPurgaLote covers criterion §5.3 (batch): it purges the ids of the body, idempotent for an
// absent id, and does not affect the other entities.
func TestPurgaLote(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Um")
	semearEntity(t, d, "e2", "movie", "Dois")
	semearEntity(t, d, "e3", "movie", "Tres")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/purge",
		map[string][]string{"ids": {"e1", "e3", "ausente"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("purge lote status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var body purgaLoteDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decodificar purgaLoteDTO: %v", err)
	}
	if body.Removed != 2 {
		t.Errorf("removed = %d, quero 2 (e1, e3; 'ausente' idempotente)", body.Removed)
	}
	// e1/e3 are gone, e2 survived.
	for _, id := range []string{"e1", "e3"} {
		if _, err := escrita(t, d).ObterEntity(context.Background(), id); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("%s sobreviveu à purga em lote (err=%v)", id, err)
		}
	}
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e2"); err != nil {
		t.Errorf("e2 foi afetada pela purga em lote: %v", err)
	}
}

func TestPurgaLoteCorpoInvalido422(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/purge", []byte("{nao-json"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestCorrigirCorpoMalformado422 covers the invalid JSON body branch of correct
// (decodeOpcional): 422 invalid_body.
func TestCorrigirCorpoMalformado422(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/correct", []byte("{nao-json"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPurgaLimpaBinariosDeAssets covers the best-effort cleanup of the entity binary directory
// ({assetsDir}/{id}) in the individual purge — the os.RemoveAll path with assetsDir configured
// (the enrich server has one).
func TestPurgaLimpaBinariosDeAssets(t *testing.T) {
	h, d, _, _, assetsDir := setupEnrichServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	// Creates the entity binary directory with a file inside.
	dir := filepath.Join(assetsDir, "e1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "user-a.png"), []byte("bin"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("diretório de binários %s ainda existe (err=%v), quero limpo", dir, err)
	}
}

// provFalha is a provider whose BuscarDetalhe ALWAYS fails (Nome tmdb, it matches the
// find-or-create), to exercise the 502 of correct when the source does not deliver.
type provFalha struct{}

func (*provFalha) Nome() string { return "tmdb" }

func (*provFalha) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (*provFalha) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return nil, errors.New("fonte fora do ar")
}

func (*provFalha) MapearDetalhe(json.RawMessage) (map[string]any, error) { return nil, nil }

// TestCorrigirFonteFalha502 covers the source failure branch of correct (the source does not
// deliver the detail): 502 external_source_error.
func TestCorrigirFonteFalha502(t *testing.T) {
	h, d := setupSearchServer(t, WithSearchProvider(&provFalha{}), WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "e1", "movie", "X")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/correct",
		map[string]string{"source_id": "9"})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "external_source_error")
}

// TestCuradoriaSemBanco503 covers the entitiesUnavailable guard of all the curation routes when
// no database is injected (db nil) — they degrade with 503.
func TestCuradoriaSemBanco503(t *testing.T) {
	h, err := New("v-teste", nil).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	casos := []struct {
		metodo, alvo string
	}{
		{http.MethodGet, "/api/v1/review"},
		{http.MethodGet, "/api/v1/orphans"},
		{http.MethodPost, "/api/v1/entities/e1/confirm"},
		{http.MethodPost, "/api/v1/entities/e1/correct"},
		{http.MethodDelete, "/api/v1/entities/e1"},
		{http.MethodPost, "/api/v1/entities/purge"},
	}
	for _, c := range casos {
		rec := do(t, h, c.metodo, c.alvo, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s status = %d, quero 503", c.metodo, c.alvo, rec.Code)
		}
	}
}

// TestReviewEpisodioContaArquivoDaJuncao covers the production bug measured with the review
// group work: 19 draft episodes showed up in the queue as "sem arquivo" (file_count 0, empty
// path) even with the file present, because the episode file points at the SERIES and the link
// to the episode is the media_file_entity join. The review queue now counts/groups through it,
// like the card does.
func TestReviewEpisodioContaArquivoDaJuncao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 2")
	semearEntity(t, d, "ep1", "episode", "Ep provisorio")
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 2)
	ligarFilhoOrdenado(t, d, "r-ep1", "se1", "ep1", "tv_episode", 6)
	// Draft (a provisional entity is born as draft) with the file on the SERIES and the join on
	// the episode.
	semearFile(t, d, "mf1", "s1", "/lib/serie/Season 02/ep s02e06.mkv", "present", 0.9, "")
	if _, err := escrita(t, d).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		ID: "ep1", Type: "episode", Title: "Ep provisorio", Status: "draft",
	}); err != nil {
		t.Fatalf("AtualizarEntity: %v", err)
	}
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "ep1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/review", nil)
	cards := decodeCards(t, rec)
	var ep1 *entityCardDTO
	for i := range cards {
		if cards[i].ID == "ep1" {
			ep1 = &cards[i]
		}
	}
	if ep1 == nil {
		t.Fatalf("ep1 fora da fila de revisao: %+v", cards)
	}
	eq(t, "ep1 owned", ep1.Owned, true)
	eq(t, "ep1 file_count", ep1.FileCount, 1)
	// The group comes from the path: with the path coming from the join, the episode stops
	// falling into the "no file" kind and starts grouping by the series folder.
	if ep1.ReviewGroup == nil || ep1.ReviewGroup.Kind != grupoRevisaoSerie {
		t.Fatalf("review_group = %+v, quero agrupado pela pasta da serie", ep1.ReviewGroup)
	}
}

// TestCorrigirDescartaIdentidadeAntigaDasOutrasFontes is the regression of a real
// corruption found right after correcting a wrong match: the series wrongly matched to
// "White Collar Blue" (2002) carried `tvdb: 82010` -- the id of the OTHER show -- and
// correcting it to the right TMDB id kept that id alongside. The NFO export would point
// Jellyfin at a different series.
func TestCorrigirDescartaIdentidadeAntigaDasOutrasFontes(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	semearEntity(t, d, "e-errada", "movie", "White Collar")
	semearFile(t, d, "f-errada", "e-errada", "/lib/x.mkv", "present", 40, "")
	// The wrong identity: the id of the OTHER source was written by the earlier match.
	if err := escrita(t, d).AtualizarExternalIds(context.Background(), sqlcgen.AtualizarExternalIdsParams{
		ExternalIdsJson: nsOf(`{"tmdb":"2154","tvdb":"82010"}`),
		ID:              "e-errada",
	}); err != nil {
		t.Fatalf("AtualizarExternalIds: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-errada/correct",
		map[string]string{"source_id": "21510"})
	if rec.Code != http.StatusOK {
		t.Fatalf("correct status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	eq(t, "external_ids.tmdb", ent.ExternalIDs["tmdb"], "21510")
	if v, sobrou := ent.ExternalIDs["tvdb"]; sobrou {
		t.Fatalf("tvdb = %q sobreviveu: o id da identidade ERRADA nao pode ficar", v)
	}
}

// TestCorrigirDescartaArvoreDaIdentidadeAntiga is the regression of the duplicated
// episodes found right after correcting a series: the OLD tree (seasons/episodes of the
// previous identity) stayed linked to the same seasons, so season 1 came out holding
// the 22 episodes of the wrong show AND the 14 real ones -- two cards per number.
//
// Correcting now discards the tree before importing the new one. The files survive: they
// hang on the series (`media_file.entity_id`), not on the episode.
func TestCorrigirDescartaArvoreDaIdentidadeAntiga(t *testing.T) {
	img := servidorImagemArvoreSerie(t)
	h, d := setupSearchServer(t, WithSearchProvider(fake.New(img.URL)), WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "s-antiga", "tv", "Serie Trocada")
	ctx := context.Background()

	// The tree of the PREVIOUS identity: one season with two episodes that the new tree
	// does not know.
	semearEntity(t, d, "temp-old", "season", "Temporada 1")
	semearEntity(t, d, "ep-old-1", "episode", "Episódio 1")
	semearEntity(t, d, "ep-old-2", "episode", "Episódio 2")
	ligar := func(parentID, childID, kind string) {
		if _, err := escrita(t, d).CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
			ID: parentID + ">" + childID, ParentID: parentID, ChildID: childID, Kind: kind,
		}); err != nil {
			t.Fatalf("CriarEntityRelation(%s>%s): %v", parentID, childID, err)
		}
	}
	ligar("s-antiga", "temp-old", "season")
	ligar("temp-old", "ep-old-1", "episode")
	ligar("temp-old", "ep-old-2", "episode")
	semearFile(t, d, "f-old", "s-antiga", "/lib/velha/e10.mkv", "present", 100, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/s-antiga/correct",
		map[string]string{"source_id": fake.SerieFixtureID})
	if rec.Code != http.StatusOK {
		t.Fatalf("correct status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}

	temp := membrosDe(t, h, "s-antiga")
	if len(temp) != 2 {
		t.Fatalf("temporadas = %d, quero 2 (arvore da identidade NOVA)", len(temp))
	}
	for _, season := range temp {
		if season.ID == "temp-old" {
			t.Fatalf("a temporada da identidade ANTIGA sobreviveu (%s)", season.ID)
		}
		for _, ep := range membrosDe(t, h, season.ID) {
			if ep.ID == "ep-old-1" || ep.ID == "ep-old-2" {
				t.Fatalf("episodio da identidade antiga sobreviveu na temporada %s: %s", season.Title, ep.Title)
			}
		}
	}
}

// TestCorrigirInvalidaODerivadoDaIdentidadeAnterior: correcting the identity must invalidate
// what the PREVIOUS one produced, or the wrong show keeps passing for the new one. Measured in
// production on a 1990 series (23/nov/2026): the tree and the links came out right
// while the folder kept the 2019 poster.jpg/fanart.jpg, the app kept serving the 2019 artwork
// and the tree kept NFOs naming the 2019 episodes -- with the completeness saying "complete",
// so no job ever rewrote them.
//
// The owner's own upload (origin='user') is NOT derived data and must survive.
func TestCorrigirInvalidaODerivadoDaIdentidadeAnterior(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	ctx := context.Background()
	semearEntity(t, d, "e1", "movie", "Palpite Errado")
	semearFile(t, d, "f1", "e1", "/lib/x.mkv", "present", 40, "")

	// Derived from the WRONG identity: a downloaded poster, an artwork inventory, an exported
	// NFO and the cached base payload.
	semearAssetOrigin(t, d, "a-externo", "e1", kindPoster, "external", "/data/assets/e1/poster.jpg", "")
	semearAssetOrigin(t, d, "a-dono", "e1", "fanart", "user", "/data/assets/e1/meu.jpg", "")
	semearMediaExternal(t, d, "me-antigo", "e1", "fanarttv", `{"artes":{}}`)
	semearMediaExternal(t, d, "me-base", "e1", "tmdb", `{"name":"Show Errado"}`)
	if err := escrita(t, d).AtualizarEntityNfo(ctx, sqlcgen.AtualizarEntityNfoParams{
		NfoPath:      nsOf("/lib/e1/tvshow.nfo"),
		NfoWrittenAt: nsOf("2026-11-20T00:04:00Z"),
		ID:           "e1",
	}); err != nil {
		t.Fatalf("AtualizarEntityNfo: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/correct", map[string]string{"source_id": "555"})
	if rec.Code != http.StatusOK {
		t.Fatalf("correct status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}

	assets, err := escrita(t, d).ListarAssetsPorEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	for _, a := range assets {
		if a.ID == "a-externo" {
			t.Error("o poster baixado da identidade ANTERIOR sobreviveu à correção")
		}
		if a.ID == "a-dono" && a.Origin != "user" {
			t.Errorf("o upload do dono virou %q, quero 'user'", a.Origin)
		}
	}
	if !temAsset(assets, "a-dono") {
		t.Error("o upload do dono foi apagado junto com o que a fonte baixou")
	}

	externos, err := escrita(t, d).ListarMediaExternalPorEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ListarMediaExternalPorEntity: %v", err)
	}
	for _, me := range externos {
		if me.ID == "me-antigo" || me.ID == "me-base" {
			t.Errorf("o payload em cache %q da identidade anterior sobreviveu", me.ID)
		}
	}

	ent, err := escrita(t, d).ObterEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.NfoPath.Valid || ent.NfoWrittenAt.Valid {
		t.Errorf("nfo_path/nfo_written_at = %v/%v, quero vazio: o NFO do outro item não vale mais",
			ent.NfoPath, ent.NfoWrittenAt)
	}
}

// temAsset tells whether `alvos` contains the asset id.
func temAsset(assets []sqlcgen.Asset, id string) bool {
	for _, a := range assets {
		if a.ID == id {
			return true
		}
	}
	return false
}
