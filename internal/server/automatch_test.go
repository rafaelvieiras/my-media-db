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
)

// Testes do AUTO-MATCH (briefing 0050): pontuação pura (sem banco), e os dois
// endpoints (individual e lote) via httptest, cobrindo os critérios §5.

// ── pontuarCandidato (pura, sem banco) ───────────────────────────────────────

func TestPontuarCandidato_AnosIguaisSomamBonus(t *testing.T) {
	score, descartado := pontuarCandidato("The Matrix", 1999, "The Matrix", 1999)
	if descartado {
		t.Fatal("não deveria descartar (anos iguais)")
	}
	if score != 100 { // 100 (identico) + 5, com cap em 100
		t.Errorf("score = %v, quero 100 (cap)", score)
	}
}

func TestPontuarCandidato_DiferencaDeUmAnoNaoAjusta(t *testing.T) {
	comAno, descartado := pontuarCandidato("The Matrix", 1999, "The Matrix", 2000)
	if descartado {
		t.Fatal("diferença de 1 ano não deveria descartar")
	}
	semAno, _ := pontuarCandidato("The Matrix", 0, "The Matrix", 0)
	if comAno != semAno {
		t.Errorf("diferença de 1 ano não deveria ajustar o score: comAno=%v semAno=%v", comAno, semAno)
	}
}

func TestPontuarCandidato_DiferencaDeMaisDeUmAnoDescarta(t *testing.T) {
	_, descartado := pontuarCandidato("The Matrix", 1999, "The Matrix", 2001)
	if !descartado {
		t.Error("diferença de 2 anos deveria descartar o candidato, mesmo com título idêntico")
	}
}

func TestPontuarCandidato_AnoAusenteNaoAjustaNemDescarta(t *testing.T) {
	score, descartado := pontuarCandidato("The Matrix", 1999, "The Matrix", 0)
	if descartado {
		t.Error("ano ausente no candidato não deveria descartar")
	}
	if score != 100 {
		t.Errorf("score = %v, quero 100 (título idêntico, sem bônus/penalidade de ano)", score)
	}
}

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
