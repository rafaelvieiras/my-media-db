package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// Tests of AUTO-MATCH: the two endpoints (individual and
// batch) via httptest, covering the criteria. The pure scoring tests
// (former pontuarCandidato) moved to internal/matching/matching_test.go
// when the function was extracted there.

// ── setup ─────────────────────────────────────────────────────────────────

// automatchProvider is a fake provider (Nome="tmdb") with configurable
// Buscar/BuscarDetalhe/MapearDetalhe, to exercise the EXTERNAL branch of the
// auto-match.
type automatchProvider struct {
	resultados []provider.ResultadoExterno
	raw        json.RawMessage
	campos     map[string]any
	// termos stores each search term received, for the tests that look at
	// WHICH string was queried, not only the result.
	termos []string
}

func (p *automatchProvider) Nome() string { return "tmdb" }

func (p *automatchProvider) Buscar(_ context.Context, termo, _, _ string) ([]provider.ResultadoExterno, error) {
	p.termos = append(p.termos, termo)
	return p.resultados, nil
}

func (p *automatchProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return p.raw, nil
}

func (p *automatchProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return p.campos, nil
}

// setupAutoMatchServer starts a server with the automatchProvider (source "tmdb") and
// assetsDir, so the external branch (enrich) works end to end.
func setupAutoMatchServer(t *testing.T, resultados []provider.ResultadoExterno) (http.Handler, *db.DB) {
	t.Helper()
	prov := &automatchProvider{
		resultados: resultados,
		raw:        json.RawMessage(`{"title":"Blade Runner 2049"}`),
		campos:     map[string]any{"title": "Blade Runner 2049"},
	}
	return setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))
}

// strp returns the pointer of a string literal (for Year of ResultadoExterno).
func strp(s string) *string { return &s }

// servidorImagemArvoreSerie starts an httptest that responds to ANY path with a valid
// PNG: used as the ImageBase of internal/provider/fake.Provider (same series tree
// fixture of internal/enrich/enrich_arvore_test.go) in the tests that prove that
// Correct/Confirm candidate/auto-match/retroactive build season/episode via
// GET /members -- the fixture generates several poster/still URLs
// (series, each season, each episode), all under the same ImageBase.
func servidorImagemArvoreSerie(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngFake)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// semearEntityComAno creates an entity with release_date (so the local candidate
// enters the year adjustment of the score).
func semearEntityComAno(t *testing.T, d *db.DB, id, tipo, titulo, releaseDate string) {
	t.Helper()
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipo, Title: titulo, ReleaseDate: nsOf(releaseDate),
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

// ── POST /entities/{id}/auto-match ───────────────────────────────────────────

// TestAutoMatchLocalFundeSemDuplicar covers the criteria: a low-confidence
// item whose file matches (title+year) an ALREADY cataloged entity is
// MERGED into it (fundirEntities) — it never creates a duplicate.
func TestAutoMatchLocalFundeSemDuplicar(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-draft", "movie", "Draft Sem Nome")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMatch(t, rec)
	if !got.Resolved || got.Method != "local" {
		t.Fatalf("resultado = %+v, quero resolved:true method:local", got)
	}
	if got.Target == nil || got.Target.ID != "e-target" {
		t.Fatalf("target = %+v, quero id=e-target", got.Target)
	}
	if got.Score < LimiarAutoMatch {
		t.Errorf("score = %v, quero >= %v", got.Score, LimiarAutoMatch)
	}

	// e-draft (the loser) was deleted; the file moved to e-target.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-draft"); err == nil {
		t.Error("e-draft ainda existe após o auto-match local, quero fundida/apagada")
	}
	arquivo, err := escrita(t, d).ObterMediaFile(context.Background(), "f-draft")
	if err != nil {
		t.Fatalf("ObterMediaFile(f-draft): %v", err)
	}
	if arquivo.EntityID != "e-target" {
		t.Errorf("f-draft.entity_id = %q, quero e-target (religado pela fusão)", arquivo.EntityID)
	}
}

// TestAutoMatchAnoDivergenteNuncaAplica covers the criterion: a candidate with an
// identical title but a year diverging by MORE than 1 year is never applied, even
// without any external provider configured (it stays "none").
func TestAutoMatchAnoDivergenteNuncaAplica(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	// The file's year (2015) diverges from 1999 by more than 1 year.
	semearFile(t, d, "f-draft", "e-draft", "/lib/The Matrix (2015)/The Matrix (2015).mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMatch(t, rec)
	if got.Resolved || got.Method != "none" {
		t.Errorf("resultado = %+v, quero resolved:false method:none (ano divergente > 1)", got)
	}
	// e-draft remains intact in the queue.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-draft"); err != nil {
		t.Errorf("e-draft deveria continuar intacta: %v", err)
	}
}

// TestAutoMatchSemCandidatoForte covers the criterion: without a strong candidate in
// any source, the item stays intact and the endpoint responds 200 resolved:false.
func TestAutoMatchSemCandidatoForte(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntityComAno(t, d, "e-outro", "movie", "Completely Unrelated Title", "2005-01-01")
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMatch(t, rec)
	if got.Resolved || got.Method != "none" || got.Score != 0 {
		t.Errorf("resultado = %+v, quero resolved:false method:none score:0", got)
	}
}

// TestAutoMatchExterno covers the criterion through the EXTERNAL branch: without a
// local candidate, but with a strong external candidate, it applies through the
// "Correct" sequence (writes external_ids + re-enriches + raises confidence).
func TestAutoMatchExterno(t *testing.T) {
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "335984", Type: "movie", Title: "Blade Runner 2049", Year: strp("2017")},
	}
	h, d := setupAutoMatchServer(t, resultados)
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/Blade Runner 2049 (2017)/Blade Runner 2049 (2017).mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMatch(t, rec)
	if !got.Resolved || got.Method != "external" {
		t.Fatalf("resultado = %+v, quero resolved:true method:external", got)
	}
	if got.Target == nil || got.Target.ID != "e-draft" {
		t.Fatalf("target = %+v, quero id=e-draft (identidade aplicada NA MESMA entity)", got.Target)
	}

	// Identity written and confidence went up (it left the queue) — same contract as
	// "Correct".
	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-draft")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.ExternalIDTmdb.Valid || ent.ExternalIDTmdb.String != "335984" {
		t.Errorf("external_id_tmdb = %v, quero 335984", ent.ExternalIDTmdb)
	}
	if idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e-draft"] {
		t.Error("e-draft ainda na fila de revisão após auto-match externo")
	}
}

// TestAutoMatchExternoSerieConstroiArvore covers series trees: the external
// auto-match over a type='tv' entity (aplicarCandidatoExterno -> s.enrich.Enriquecer)
// also starts synchronizing the season/episode tree, not only POST /import --
// GET /entities/{id}/members stops coming back empty after the auto-match resolves by
// itself. It uses the canned fixture of fake.Provider (same tree of internal/enrich/
// enrich_arvore_test.go: 2 seasons of 3 episodes).
func TestAutoMatchExternoSerieConstroiArvore(t *testing.T) {
	img := servidorImagemArvoreSerie(t)
	h, d := setupSearchServer(t, WithSearchProvider(fake.New(img.URL)), WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "e-draft", "tv", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/Springfield Tales (1989)/Springfield Tales (1989).mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMatch(t, rec)
	if !got.Resolved || got.Method != "external" {
		t.Fatalf("resultado = %+v, quero resolved:true method:external", got)
	}

	temporadas := membrosDe(t, h, "e-draft")
	if len(temporadas) != 2 {
		t.Fatalf("members(e-draft) = %d, quero 2 temporadas (auto-match externo sincronizou a arvore, 0067)", len(temporadas))
	}
	totalEpisodios := 0
	for _, temp := range temporadas {
		totalEpisodios += len(membrosDe(t, h, temp.ID))
	}
	if totalEpisodios != 6 {
		t.Errorf("episodios = %d, quero 6", totalEpisodios)
	}
}

// TestAutoMatchInexistente404 covers the 404 of the individual one for an absent
// entity.
func TestAutoMatchInexistente404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/auto-match", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestAutoMatchSemArquivoFicaNone covers the case without any media_file (nothing to
// derive title/year from): 200 resolved:false, without touching the entity.
func TestAutoMatchSemArquivoFicaNone(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e-vazia", "movie", "Sem Arquivo")
	definirStatus(t, d, "e-vazia", "draft")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-vazia/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMatch(t, rec)
	if got.Resolved {
		t.Errorf("resultado = %+v, quero resolved:false (sem arquivo, nada para derivar)", got)
	}
}

// ── POST /review/auto-match (batch) ───────────────────────────────────────────

// TestAutoMatchLoteTodosElegiveis covers the criterion: without `ids` in the body,
// it processes ALL the eligible ones of ListarRevisao and returns the aggregated
// summary.
func TestAutoMatchLoteTodosElegiveis(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Resolves via local: it matches e-target.
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-draft-ok", "movie", "Draft OK")
	definirStatus(t, d, "e-draft-ok", "draft")
	semearFile(t, d, "f-ok", "e-draft-ok", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 0, "")
	// Stays pending: nothing matches.
	semearEntity(t, d, "e-draft-pendente", "movie", "Draft Pendente")
	definirStatus(t, d, "e-draft-pendente", "draft")
	semearFile(t, d, "f-pendente", "e-draft-pendente", "/lib/Totally Unrelated Thing (2015)/x.mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/review/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got autoMatchBatchResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar autoMatchBatchResultDTO: %v", err)
	}
	if got.ResolvedCount != 1 {
		t.Errorf("resolved_count = %d, quero 1", got.ResolvedCount)
	}
	if got.PendingCount != 1 {
		t.Errorf("pending_count = %d, quero 1", got.PendingCount)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, quero 2", len(got.Items))
	}
}

// TestAutoMatchLoteSubconjuntoIds covers the `{"ids":[...]}` body: it processes only the
// informed subset, ignoring the other eligible ones.
func TestAutoMatchLoteSubconjuntoIds(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-draft-ok", "movie", "Draft OK")
	definirStatus(t, d, "e-draft-ok", "draft")
	semearFile(t, d, "f-ok", "e-draft-ok", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 0, "")
	semearEntity(t, d, "e-draft-fora", "movie", "Fora Do Lote")
	definirStatus(t, d, "e-draft-fora", "draft")
	semearFile(t, d, "f-fora", "e-draft-fora", "/lib/The Matrix (1999)/The Matrix (1999) (2).mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/review/auto-match",
		map[string][]string{"ids": {"e-draft-ok"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got autoMatchBatchResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "e-draft-ok" {
		t.Fatalf("items = %+v, quero só e-draft-ok", got.Items)
	}
	// e-draft-fora was not processed (outside the subset) -- it remains in the queue.
	if !idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e-draft-fora"] {
		t.Error("e-draft-fora deveria continuar na fila (fora do subconjunto ids)")
	}
}

// TestAutoMatchElegiveis covers the method used by the health job: without an explicit
// `ids`, it resolves all the eligible ones of ListarRevisao and
// returns (resolved, pending) -- the same count that POST /review/auto-match
// would return in the body.
func TestAutoMatchElegiveis(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-draft-ok", "movie", "Draft OK")
	definirStatus(t, d, "e-draft-ok", "draft")
	semearFile(t, d, "f-ok", "e-draft-ok", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 0, "")
	semearEntity(t, d, "e-draft-pendente", "movie", "Draft Pendente")
	definirStatus(t, d, "e-draft-pendente", "draft")
	semearFile(t, d, "f-pendente", "e-draft-pendente", "/lib/Totally Unrelated Thing (2015)/x.mkv", "present", 0, "")

	s := New("v-teste", d)
	resolved, pending, err := s.AutoMatchElegiveis(context.Background(), execqueue.RegistroNulo())
	if err != nil {
		t.Fatalf("AutoMatchElegiveis: %v", err)
	}
	if resolved != 1 {
		t.Errorf("resolved = %d, quero 1", resolved)
	}
	if pending != 1 {
		t.Errorf("pending = %d, quero 1", pending)
	}
}

// TestAutoMatchSemBanco503 covers the entitiesUnavailable guard of the two
// endpoints when no database is injected.
func TestAutoMatchSemBanco503(t *testing.T) {
	h, err := New("v-teste", nil).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	casos := []struct{ metodo, alvo string }{
		{http.MethodPost, "/api/v1/entities/e1/auto-match"},
		{http.MethodPost, "/api/v1/review/auto-match"},
	}
	for _, c := range casos {
		rec := do(t, h, c.metodo, c.alvo, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s status = %d, quero 503", c.metodo, c.alvo, rec.Code)
		}
	}
}

// decodeAutoMatch decodes the response of the individual auto-match.
func decodeAutoMatch(t *testing.T, rec *httptest.ResponseRecorder) autoMatchResultDTO {
	t.Helper()
	var out autoMatchResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar autoMatchResultDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return out
}

// ── TentarAutoMatchFilme (adapter of scanner.FilmeImportador) ──

// TestTentarAutoMatchFilmeCandidatoForteAplica covers the immediate movie auto-match
// during the scan: a local candidate >= LimiarAutoMatch is actually applied
// (fundirEntities) and the adapter returns matched=true, without an error.
func TestTentarAutoMatchFilmeCandidatoForteAplica(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-draft", "movie", "Draft Sem Nome")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 0, "")

	s := New("v-teste", d)
	matched, err := s.TentarAutoMatchFilme(context.Background(), "e-draft")
	if err != nil {
		t.Fatalf("TentarAutoMatchFilme: %v", err)
	}
	if !matched {
		t.Fatal("matched = false, quero true (candidato local bate o limiar)")
	}
	// e-draft (the loser) was merged into e-target -- same guarantee as the HTTP
	// auto-match.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-draft"); err == nil {
		t.Error("e-draft ainda existe após o auto-match, quero fundida/apagada")
	}
}

// TestTentarAutoMatchFilmeSemCandidatoNaoFalha covers the silent degradation:
// without a strong enough candidate in any source, matched=false and err=nil --
// the entity remains intact, pending in the Review queue (same spirit as
// ErrSerieNaoEncontrada for series).
func TestTentarAutoMatchFilmeSemCandidatoNaoFalha(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearEntityComAno(t, d, "e-outro", "movie", "Completely Unrelated Title", "2005-01-01")
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 0, "")

	s := New("v-teste", d)
	matched, err := s.TentarAutoMatchFilme(context.Background(), "e-draft")
	if err != nil {
		t.Fatalf("TentarAutoMatchFilme: quero err=nil sem candidato forte, veio %v", err)
	}
	if matched {
		t.Error("matched = true, quero false (nenhum candidato bateu o limiar)")
	}
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-draft"); err != nil {
		t.Errorf("e-draft deveria continuar intacta: %v", err)
	}
}

// TestTentarAutoMatchFilmeErroDeInfraPropaga covers the other half of the
// semantics: a REAL database failure (closed read pool) propagates as an
// error, it never degrades silently to matched=false.
func TestTentarAutoMatchFilmeErroDeInfraPropaga(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")

	if err := d.Read.Close(); err != nil {
		t.Fatalf("fechar Read: %v", err)
	}

	s := New("v-teste", d)
	if _, err := s.TentarAutoMatchFilme(context.Background(), "e-draft"); err == nil {
		t.Fatal("TentarAutoMatchFilme com pool de leitura fechado: quero erro, veio nil")
	}
}

// setupAutoMatchServerComBusca is setupAutoMatchServer returning the fake provider,
// for the tests that look at the QUERIED TERM, not only the
// returned candidate.
func setupAutoMatchServerComBusca(t *testing.T, resultados []provider.ResultadoExterno) (http.Handler, *db.DB, *automatchProvider) {
	t.Helper()
	prov := &automatchProvider{
		resultados: resultados,
		raw:        json.RawMessage(`{"title":"Gárgulas"}`),
		campos:     map[string]any{"title": "Gárgulas"},
	}
	h, d := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))
	return h, d, prov
}

// ── series title comes from the folder, not from the episode file ────────

// TestAutoMatchDeSerieConsultaAPastaDaSerie covers the central criterion: in a series,
// the term taken to the provider is the name of the series FOLDER
// ("Os Gárgulas (1994)" -> "Os Gárgulas"), never the episode file's
// ("Os gárgulas - S03E08 - Genesis Undone"), which gave similarity 58 against
// "Gárgulas" and returned no candidate at all.
func TestAutoMatchDeSerieConsultaAPastaDaSerie(t *testing.T) {
	ano := "1994"
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "1719", Type: "tv", Title: "Gárgulas", Year: &ano},
	}
	h, d, prov := setupAutoMatchServerComBusca(t, resultados)
	semearSource(t, d, "src-series", "/lib/Series Infantis")
	semearEntity(t, d, "e-serie", "tv", "Os Gárgulas")
	definirStatus(t, d, "e-serie", "draft")
	semearFileComFonte(t, d, "f-serie", "e-serie",
		"/lib/Series Infantis/Os Gárgulas (1994)/Season 03/Os gárgulas - S03E08 - Genesis Undone.mkv",
		"present", 0, "src-series")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e-serie/match-preview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if len(prov.termos) == 0 {
		t.Fatal("o provider não foi consultado: a série não deveria cair em found:false")
	}
	if prov.termos[0] != "Os Gárgulas" {
		t.Errorf("termo consultado = %q, quero %q (nome da pasta da série)", prov.termos[0], "Os Gárgulas")
	}
	got := decodeMatchPreview(t, rec)
	if !got.Found || got.Method != "external" {
		t.Fatalf("preview = %+v, quero found:true method:external", got)
	}
	if got.Target == nil || got.Target.SourceID == nil || *got.Target.SourceID != "1719" {
		t.Errorf("target = %+v, quero source_id 1719", got.Target)
	}
	if !got.MeetsAutoThreshold {
		t.Errorf("meets_auto_threshold = false (score %v), quero true: título da pasta + ano casando", got.Score)
	}
}

// TestAutoMatchDeSerieNaRaizDaFonteUsaTituloDaEntity covers the guard: a file loose at
// the source root cannot become a query with the source name ("Series") -- the entity
// has a readable title and that is what goes to the search.
func TestAutoMatchDeSerieNaRaizDaFonteUsaTituloDaEntity(t *testing.T) {
	ano := "2007"
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "4607", Type: "tv", Title: "Chuck", Year: &ano},
	}
	h, d, prov := setupAutoMatchServerComBusca(t, resultados)
	semearSource(t, d, "src-series", "/lib/Series")
	semearEntity(t, d, "e-chuck", "tv", "Chuck")
	definirStatus(t, d, "e-chuck", "draft")
	semearFileComFonte(t, d, "f-chuck", "e-chuck",
		"/lib/Series/Chuck - S01E01.mkv", "present", 0, "src-series")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e-chuck/match-preview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if len(prov.termos) == 0 {
		t.Fatal("o provider não foi consultado")
	}
	if prov.termos[0] != "Chuck" {
		t.Errorf("termo consultado = %q, quero %q (título da entity, nunca o nome da fonte)", prov.termos[0], "Chuck")
	}
}

// TestAutoMatchDeFilmeConsultaTituloDoCatalogo is the regression of the bug that left half
// of a production movie library unmatched. Auto-match used to derive the movie query ONLY
// from the FILE name, throwing away the catalog title (which comes from the FOLDER, the
// part the owner curated). With a release name like "Alligator2.91.BD1080p.MemoriadaTV.Maior"
// the provider returned ZERO candidates, while the catalog title "Alligator 2 A Mutação"
// returned the right movie as the first result.
//
// The catalog title is tried FIRST and, when it reaches LimiarAutoMatch, the release name is
// never sent -- the assertion on len(termos) is the short-circuit, not decoration.
func TestAutoMatchDeFilmeConsultaTituloDoCatalogo(t *testing.T) {
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "29680", Type: "movie", Title: "Alligator 2: The Mutation", Year: strp("1991"),
			TituloOriginal: "Alligator 2: The Mutation"},
	}
	h, d, prov := setupAutoMatchServerComBusca(t, resultados)
	semearEntityComAno(t, d, "e-aligator", "movie", "Alligator 2 A Mutação", "1991-01-01")
	definirStatus(t, d, "e-aligator", "draft")
	semearFile(t, d, "f-aligator", "e-aligator",
		"/lib/Filmes/Alligator 2 - A Mutação (1991)/Alligator2.91.BD1080p.MemoriadaTV.Maior.mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-aligator/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if len(prov.termos) == 0 {
		t.Fatal("o provider não foi consultado")
	}
	if prov.termos[0] != "Alligator 2 A Mutação" {
		t.Errorf("primeiro termo consultado = %q, quero %q (título do catálogo, não o nome do release)",
			prov.termos[0], "Alligator 2 A Mutação")
	}
	if len(prov.termos) != 1 {
		t.Errorf("termos consultados = %v, quero só o título do catálogo (o primeiro já passou do limiar)", prov.termos)
	}

	got := decodeAutoMatch(t, rec)
	if !got.Resolved || got.Method != "external" {
		t.Fatalf("resultado = %+v, quero resolved:true method:external", got)
	}
	if got.Score < LimiarAutoMatch {
		t.Errorf("score = %v, quero >= %v", got.Score, LimiarAutoMatch)
	}
	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-aligator")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.ExternalIDTmdb.Valid || ent.ExternalIDTmdb.String != "29680" {
		t.Errorf("external_id_tmdb = %v, quero 29680", ent.ExternalIDTmdb)
	}
}

// TestAutoMatchDeFilmeAindaTentaNomeDoArquivo covers the other half of the contract: the
// folder comes first, the catalog title second, but the file name is STILL tried when nothing
// before it reaches the threshold -- the folder and the entity title here are useless, and the
// release name resolves the item exactly as before this change.
func TestAutoMatchDeFilmeAindaTentaNomeDoArquivo(t *testing.T) {
	ano := "2021"
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "438631", Type: "movie", Title: "Dune", Year: &ano},
	}
	h, d, prov := setupAutoMatchServerComBusca(t, resultados)
	semearSource(t, d, "src-filmes", "/lib/Filmes")
	semearEntity(t, d, "e-dune", "movie", "Draft")
	definirStatus(t, d, "e-dune", "draft")
	// Pasta sem nada aproveitável ("zzz"): só o nome do arquivo serve.
	semearFileComFonte(t, d, "f-dune", "e-dune",
		"/lib/Filmes/zzz/Dune (2021).mkv", "present", 0, "src-filmes")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e-dune/match-preview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if len(prov.termos) != 3 {
		t.Fatalf("termos consultados = %v, quero 3 (pasta, título do catálogo e, por último, nome do arquivo)", prov.termos)
	}
	if prov.termos[0] != "zzz" || prov.termos[1] != "Draft" || prov.termos[2] != "Dune" {
		t.Errorf("termos consultados = %v, quero [zzz Draft Dune] (ordem: pasta, catálogo, arquivo)", prov.termos)
	}
	got := decodeMatchPreview(t, rec)
	if !got.Found || got.Method != "external" {
		t.Fatalf("preview = %+v, quero found:true method:external", got)
	}
	if !got.MeetsAutoThreshold {
		t.Errorf("meets_auto_threshold = false (score %v), quero true: o nome do arquivo casa", got.Score)
	}
}

// TestMelhorCandidatoExternoUsaTituloOriginal is the regression of a WRONG match that
// reached the catalog marked as "catalogado" -- no review, no human ever saw it: the
// folder "White Collar" (2009, the US series) was matched to TMDB 2154 "White Collar
// Blue" (2002, Australian) with score 94.1.
//
// Two causes, and this test covers the scoring half: the search answers in the reading
// language, so the RIGHT show comes back titled "Crimes do Colarinho Branco" (its
// original_name being "White Collar"), scores ~0 against the folder name, and never
// reaches the top-3 that gets rescued by alternative titles; meanwhile a longer title
// that merely STARTS with the query wins on raw similarity.
func TestMelhorCandidatoExternoUsaTituloOriginal(t *testing.T) {
	t.Parallel()
	s := &Server{}
	candidatos := []provider.ResultadoExterno{
		{
			Source: "tmdb", SourceID: "2154", Type: "tv",
			Title: "White Collar Blue", TituloOriginal: "White Collar Blue", Year: strp("2002"),
		},
		{
			Source: "tmdb", SourceID: "21510", Type: "tv",
			Title: "Crimes do Colarinho Branco", TituloOriginal: "White Collar", Year: strp("2009"),
		},
	}

	melhor, score, _, achou := s.melhorCandidatoExterno(context.Background(), candidatos, "White Collar", 0)
	if !achou {
		t.Fatal("nenhum candidato sobreviveu ao descarte")
	}
	if melhor.SourceID != "21510" {
		t.Fatalf("escolheu %q (%s), queria 21510 -- o titulo original tem de pesar igual ao traduzido", melhor.SourceID, melhor.Title)
	}
	if score < LimiarAutoMatch {
		t.Fatalf("score %.1f abaixo do limiar %.1f: o show certo tem de passar pelo auto-match", score, float64(LimiarAutoMatch))
	}
}

// TestVariantesSemEnfeite: the folder style that appends a genre tag to the title gets one
// more attempt, and it goes LAST -- the literal title is always tried first. MEASURED on the
// case that motivated it: "101 Dálmatas O Filme" returns zero results in TMDb, while
// "101 Dálmatas" returns the right movie as its first result.
func TestVariantesSemEnfeite(t *testing.T) {
	entrada := []consultaTituloAno{
		{Titulo: "101 Dálmatas O Filme", Ano: 1996},
		{Titulo: "101 Dálmatas", Ano: 1996}, // já sem enfeite: não duplica
	}
	got := comVariantesSemEnfeite(entrada)
	if len(got) != 3 {
		t.Fatalf("variantes = %+v, quero 3 (a literal, a literal sem enfeite e a que já vinha limpa)", got)
	}
	if got[0].Titulo != "101 Dálmatas O Filme" {
		t.Errorf("primeira variante = %q, quero a literal (o dono procura pelo nome que está na pasta)", got[0].Titulo)
	}
	if got[1].Titulo != "101 Dálmatas" || got[1].Ano != 1996 {
		t.Errorf("segunda variante = %+v, quero \"101 Dálmatas\" mantendo o ano", got[1])
	}
	if got[2].Titulo != "101 Dálmatas" {
		t.Errorf("terceira variante = %q, quero a original limpa", got[2].Titulo)
	}
	// A deduplicação roda depois e derruba a repetição: o provedor é consultado uma vez só.
	if dedup := dedupeConsultas(got); len(dedup) != 2 {
		t.Errorf("depois do dedupe = %+v, quero 2 consultas distintas", dedup)
	}
}

// TestVariantesDaPastaDoFilme: per the Kodi convention the FOLDER is the authoritative name of
// the work, so the search starts from it -- with the folder as written and, when the convention
// put a subtitle after the dash, with the title alone. The release name of the file comes LAST,
// and nothing is derived from the library root (its name is the library, not a title).
func TestVariantesDaPastaDoFilme(t *testing.T) {
	database := setupAutoMatchDB(t)
	ctx := context.Background()
	semearEntityComAno(t, database, "filme-1", "movie", "101 Dálmatas O Filme", "1996-01-01")
	semearFile(t, database, "arq-1", "filme-1",
		"/media/filmes/101 Dálmatas - O Filme (1996)/101.Dálmatas.1996.WEB-DL.MAIOR.1080p.mkv",
		"present", 70, "")

	s := New("v-teste", database)
	ent, err := s.leitura().ObterEntity(ctx, "filme-1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	got, err := s.variantesTituloAno(ctx, ent)
	if err != nil {
		t.Fatalf("variantesTituloAno: %v", err)
	}
	titulos := make([]string, len(got))
	for i, c := range got {
		titulos[i] = c.Titulo
	}
	if len(got) == 0 || got[0].Titulo != "101 Dálmatas O Filme" {
		t.Fatalf("variantes = %v, quero a pasta primeiro", titulos)
	}
	// A segunda é a pasta SEM o subtítulo -- a tentativa que o TMDb responde.
	if got[1].Titulo != "101 Dálmatas" || got[1].Ano != 1996 {
		t.Errorf("segunda variante = %+v, quero \"101 Dálmatas\" com o ano da pasta", got[1])
	}
	// O nome do release do arquivo NÃO pode vir antes da pasta.
	for i, c := range got {
		if strings.HasPrefix(c.Titulo, "101 Dálmatas 1996") || strings.Contains(c.Titulo, "WEB") {
			if i < 2 {
				t.Errorf("variante do arquivo na posição %d (%q): a pasta vale mais", i, c.Titulo)
			}
		}
	}
}

// TestVariantesDaPastaNaoUsaRaizDaBiblioteca: a file loose at the root of the library must not
// be searched by the folder name -- there the folder is the library itself ("Filmes"), which
// would match anything.
func TestVariantesDaPastaNaoUsaRaizDaBiblioteca(t *testing.T) {
	database := setupAutoMatchDB(t)
	ctx := context.Background()
	semearEntityComAno(t, database, "filme-1", "movie", "Toy Story", "1995-01-01")
	semearFile(t, database, "arq-1", "filme-1", "/media/filmes/Toy.Story.1995.1080p.mkv", "present", 70, "")
	// A biblioteca é /media/filmes, e o arquivo está solto nela.
	if _, err := escrita(t, database).CriarLibrarySource(ctx, sqlcgen.CriarLibrarySourceParams{
		ID: "src-1", Path: "/media/filmes", ContentType: "movie",
	}); err != nil {
		t.Fatalf("CriarLibrarySource: %v", err)
	}
	if _, err := database.Write.ExecContext(ctx,
		`UPDATE media_file SET library_source_id = 'src-1' WHERE id = 'arq-1'`); err != nil {
		t.Fatalf("library_source_id: %v", err)
	}

	s := New("v-teste", database)
	ent, err := s.leitura().ObterEntity(ctx, "filme-1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	got, err := s.variantesTituloAno(ctx, ent)
	if err != nil {
		t.Fatalf("variantesTituloAno: %v", err)
	}
	for _, c := range got {
		if strings.EqualFold(c.Titulo, "filmes") {
			t.Fatalf("variante %q veio da raiz da biblioteca", c.Titulo)
		}
	}
}

// setupAutoMatchDB opens a migrated database for the pure variant tests (no HTTP).
func setupAutoMatchDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "variantes.sqlite")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}
