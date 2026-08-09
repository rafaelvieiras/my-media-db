package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// Testes do AUTO-MATCH (briefing 0050): os dois endpoints (individual e
// lote) via httptest, cobrindo os critérios §5. Os testes de pontuação pura
// (antiga pontuarCandidato) migraram para internal/matching/matching_test.go
// quando a função foi extraída para lá (briefing 0054).

// ── setup ─────────────────────────────────────────────────────────────────

// automatchProvider é um provider fake (Nome="tmdb") com Buscar/BuscarDetalhe/
// MapearDetalhe configuráveis, para exercitar o ramo EXTERNO do auto-match.
type automatchProvider struct {
	resultados []provider.ResultadoExterno
	raw        json.RawMessage
	campos     map[string]any
}

func (p *automatchProvider) Nome() string { return "tmdb" }

func (p *automatchProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return p.resultados, nil
}

func (p *automatchProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return p.raw, nil
}

func (p *automatchProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return p.campos, nil
}

// setupAutoMatchServer sobe um server com o automatchProvider (fonte "tmdb") e
// assetsDir, para o ramo externo (enrich) funcionar de ponta a ponta.
func setupAutoMatchServer(t *testing.T, resultados []provider.ResultadoExterno) (http.Handler, *db.DB) {
	t.Helper()
	prov := &automatchProvider{
		resultados: resultados,
		raw:        json.RawMessage(`{"title":"Blade Runner 2049"}`),
		campos:     map[string]any{"title": "Blade Runner 2049"},
	}
	return setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))
}

// strp devolve o ponteiro de uma string literal (para Year de ResultadoExterno).
func strp(s string) *string { return &s }

// servidorImagemArvoreSerie sobe um httptest que responde QUALQUER caminho com um PNG
// valido: usado como ImageBase do internal/provider/fake.Provider (mesma fixture de
// arvore de serie de internal/enrich/enrich_arvore_test.go, briefing 0067) nos testes
// que provam que Corrigir/Confirmar candidato/auto-match/retroativo constroem
// temporada/episodio via GET /members -- a fixture gera varias URLs de poster/still
// (serie, cada temporada, cada episodio), todas sob o mesmo ImageBase.
func servidorImagemArvoreSerie(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngFake)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// semearEntityComAno cria uma entity com release_date (para o candidato local
// entrar no ajuste de ano do score).
func semearEntityComAno(t *testing.T, d *db.DB, id, tipo, titulo, releaseDate string) {
	t.Helper()
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipo, Title: titulo, ReleaseDate: nsOf(releaseDate),
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

// ── POST /entities/{id}/auto-match ───────────────────────────────────────────

// TestAutoMatchLocalFundeSemDuplicar cobre os critérios §5.1/§5.2: um item de
// baixa confiança cujo arquivo casa (título+ano) com uma entity JÁ catalogada é
// FUNDIDO nela (fundirEntities) — nunca cria duplicata.
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

	// e-draft (perdedora) foi apagada; o arquivo migrou para e-target.
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

// TestAutoMatchAnoDivergenteNuncaAplica cobre o critério §5.3: candidato com
// título idêntico mas ano divergindo em MAIS de 1 ano nunca é aplicado, mesmo
// sem nenhum provider externo configurado (fica "none").
func TestAutoMatchAnoDivergenteNuncaAplica(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	// Ano do arquivo (2015) diverge de 1999 em mais de 1 ano.
	semearFile(t, d, "f-draft", "e-draft", "/lib/The Matrix (2015)/The Matrix (2015).mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMatch(t, rec)
	if got.Resolved || got.Method != "none" {
		t.Errorf("resultado = %+v, quero resolved:false method:none (ano divergente > 1)", got)
	}
	// e-draft continua intacta na fila.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-draft"); err != nil {
		t.Errorf("e-draft deveria continuar intacta: %v", err)
	}
}

// TestAutoMatchSemCandidatoForte cobre o critério §5.4: sem candidato forte em
// nenhuma fonte, o item fica intacto e o endpoint responde 200 resolved:false.
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

// TestAutoMatchExterno cobre o critério §5.1 pelo ramo EXTERNO: sem candidato
// local, mas com um candidato externo forte, aplica pela sequência do
// "Corrigir" (grava external_ids + re-enriquece + sobe confiança).
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

	// Identidade gravada e confiança subiu (saiu da fila) — mesmo contrato do
	// "Corrigir" (0018).
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

// TestAutoMatchExternoSerieConstroiArvore cobre o briefing 0067: o auto-match
// externo sobre uma entity type='tv' (aplicarCandidatoExterno -> s.enrich.Enriquecer)
// passa a sincronizar a arvore de temporada/episodio tambem, nao so POST /import --
// GET /entities/{id}/members deixa de vir vazio apos o auto-match resolver sozinho.
// Usa o fixture canned de fake.Provider (mesma arvore de internal/enrich/
// enrich_arvore_test.go: 2 temporadas de 3 episodios).
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

// TestAutoMatchInexistente404 cobre o 404 do individual para entity ausente.
func TestAutoMatchInexistente404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/auto-match", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestAutoMatchSemArquivoFicaNone cobre o caso sem media_file algum (nada para
// derivar título/ano): 200 resolved:false, sem tocar a entity.
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

// ── POST /review/auto-match (lote) ───────────────────────────────────────────

// TestAutoMatchLoteTodosElegiveis cobre o critério §5.5: sem `ids` no corpo,
// processa TODOS os elegíveis de ListarRevisao e devolve o resumo agregado.
func TestAutoMatchLoteTodosElegiveis(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Resolve via local: casa com e-target.
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-draft-ok", "movie", "Draft OK")
	definirStatus(t, d, "e-draft-ok", "draft")
	semearFile(t, d, "f-ok", "e-draft-ok", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 0, "")
	// Fica pendente: nada bate.
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

// TestAutoMatchLoteSubconjuntoIds cobre o corpo `{"ids":[...]}`: processa só o
// subconjunto informado, ignorando os demais elegíveis.
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
	// e-draft-fora não foi processada (fora do subconjunto) -- continua na fila.
	if !idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e-draft-fora"] {
		t.Error("e-draft-fora deveria continuar na fila (fora do subconjunto ids)")
	}
}

// TestAutoMatchElegiveis cobre o método usado pelo job de saúde (briefing
// 0054): sem `ids` explícito, resolve todos os elegíveis de ListarRevisao e
// devolve (resolved, pending) -- mesma contagem que POST /review/auto-match
// devolveria no corpo.
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
	resolved, pending, err := s.AutoMatchElegiveis(context.Background())
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

// TestAutoMatchSemBanco503 cobre a guarda entitiesUnavailable dos dois
// endpoints quando não há banco injetado.
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

// decodeAutoMatch decodifica a resposta do auto-match individual.
func decodeAutoMatch(t *testing.T, rec *httptest.ResponseRecorder) autoMatchResultDTO {
	t.Helper()
	var out autoMatchResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar autoMatchResultDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return out
}

// ── TentarAutoMatchFilme (briefing 0089, adaptador de scanner.FilmeImportador) ──

// TestTentarAutoMatchFilmeCandidatoForteAplica cobre o auto-match imediato de
// filme no scan: candidato local >= LimiarAutoMatch é de fato aplicado
// (fundirEntities) e o adaptador devolve matched=true, sem erro.
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
	// e-draft (perdedora) foi fundida em e-target -- mesma garantia do auto-match HTTP.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-draft"); err == nil {
		t.Error("e-draft ainda existe após o auto-match, quero fundida/apagada")
	}
}

// TestTentarAutoMatchFilmeSemCandidatoNaoFalha cobre a degradação em silêncio:
// sem candidato forte o bastante em nenhuma fonte, matched=false e err=nil --
// a entity permanece intacta, pendente na fila de Revisão (mesmo espírito de
// ErrSerieNaoEncontrada para série).
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

// TestTentarAutoMatchFilmeErroDeInfraPropaga cobre a outra metade da
// semântica: uma falha REAL de banco (pool de leitura fechado) propaga como
// erro, nunca degrada silenciosamente para matched=false.
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
