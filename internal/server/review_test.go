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
)

// Testes das FILAS DE CURADORIA (briefing 0018): revisão, órfãs, confirmar,
// corrigir e purga. Semeiam entities/media_files/override/asset direto pelo pool de
// escrita e exercitam as rotas via httptest. Cobrem os critérios de aceite §5.

// definirStatus muda o status da entity direto no banco (CriarEntity nasce sempre
// 'cataloged'; a fila de revisão precisa de 'draft', a de órfã de 'wanted' etc.).
func definirStatus(t *testing.T, d *db.DB, id, status string) {
	t.Helper()
	if _, err := d.Write.ExecContext(context.Background(),
		"UPDATE entity SET status = ? WHERE id = ?", status, id); err != nil {
		t.Fatalf("definirStatus(%s=%s): %v", id, status, err)
	}
}

// semearOverride grava um override mínimo para a entity (marca "investimento do
// dono" no predicado de órfã).
func semearOverride(t *testing.T, d *db.DB, entityID, blob string) {
	t.Helper()
	if _, err := escrita(t, d).UpsertMediaOverride(context.Background(), sqlcgen.UpsertMediaOverrideParams{
		EntityID: entityID, OverrideJson: nsOf(blob),
	}); err != nil {
		t.Fatalf("semearOverride(%s): %v", entityID, err)
	}
}

// semearAssetUser grava um asset origin='user' (arte do dono; investimento no
// predicado de órfã).
func semearAssetUser(t *testing.T, d *db.DB, id, entityID, kind, path string) {
	t.Helper()
	if _, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID: id, EntityID: entityID, Kind: kind, Origin: "user", PathOrUri: path,
	}); err != nil {
		t.Fatalf("semearAssetUser(%s): %v", id, err)
	}
}

// decodeCards decodifica a resposta em ARRAY de cards das filas /review e /orphans.
func decodeCards(t *testing.T, rec *httptest.ResponseRecorder) []entityCardDTO {
	t.Helper()
	var out []entityCardDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar []entityCardDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return out
}

// idsDe extrai os ids de uma lista de cards, para asserts de pertencimento.
func idsDe(cards []entityCardDTO) map[string]bool {
	m := make(map[string]bool, len(cards))
	for _, c := range cards {
		m[c.ID] = true
	}
	return m
}

// TestFilaRevisaoClassifica cobre o critério §5.1 (classificação): a fila lista
// draft + baixa confiança (present, 0<conf<70) e EXCLUI alta confiança e missing.
func TestFilaRevisaoClassifica(t *testing.T) {
	h, d := setupEntitiesServer(t)

	// draft (não-identificado) — entra por status.
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	// baixa confiança, arquivo present — entra pelo predicado de confiança.
	semearEntity(t, d, "e-baixa", "movie", "Baixa")
	semearFile(t, d, "f-baixa", "e-baixa", "/lib/baixa.mkv", "present", 60, "")
	// alta confiança present — NÃO entra (>= limiar 70).
	semearEntity(t, d, "e-alta", "movie", "Alta")
	semearFile(t, d, "f-alta", "e-alta", "/lib/alta.mkv", "present", 90, "")
	// baixa confiança mas MISSING — NÃO entra (state != present).
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
}

// TestFilaRevisaoVaziaLista200 cobre o contrato: 200 com [] quando não há nada.
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

// TestConfirmarSobeConfianca cobre o critério §5.1 (confirmar): confirmar sobe a
// confiança e o item some da fila de revisão. 404 para entity inexistente.
func TestConfirmarSobeConfianca(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Baixa")
	semearFile(t, d, "f1", "e1", "/lib/x.mkv", "present", 55, "")

	// Antes: está na fila.
	if !idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e1"] {
		t.Fatal("pré-condição: e1 deveria estar na fila de revisão")
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/confirm", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}

	// Depois: sumiu da fila (confiança subiu para 100).
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

// statusDe le o status atual da entity direto do banco (para asserts de promocao).
func statusDe(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	ent, err := escrita(t, d).ObterEntity(context.Background(), id)
	if err != nil {
		t.Fatalf("ObterEntity(%s): %v", id, err)
	}
	return ent.Status
}

// TestConfirmarPromoveDraft cobre o ramo do STATUS: confirmar um draft
// (nao-identificado) tem de promove-lo a 'cataloged' e tira-lo da fila — senao a
// confianca sobe a 100 mas o item fica preso como draft (regressao pega ao vivo).
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

// TestCorrigirGravaExternalIdsEReResolve cobre os critérios §5.1 (corrigir) e §5.4
// (identidade vence, reflete no resolver): corrige com o provider fake, grava
// external_ids, re-enriquece (título externo compõe), sobe a confiança e re-resolve.
func TestCorrigirGravaExternalIdsEReResolve(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	// Entity casada errado pelo scanner: baixa confiança, sem id externo.
	semearEntity(t, d, "e1", "movie", "Palpite Errado")
	semearFile(t, d, "f1", "e1", "/lib/x.mkv", "present", 40, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/correct",
		map[string]string{"source_id": "555"})
	if rec.Code != http.StatusOK {
		t.Fatalf("correct status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	// Identidade gravada (source default tmdb) e composição externa refletida.
	eq(t, "external_ids.tmdb", ent.ExternalIDs["tmdb"], "555")
	eq(t, "title(re-resolvido)", ent.Title, "Filme Externo")

	// Confiança subiu → some da fila de revisão.
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

// TestCorrigirPromoveDraft cobre o ramo do STATUS na correcao: corrigir um draft
// (o caminho natural de identificar um nao-identificado) grava a identidade,
// re-enriquece E promove a 'cataloged', tirando-o da fila. Regressao pega ao vivo.
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

// TestCorrigirEnrichDesligado503 cobre a exigência do serviço de enrich ligado: sem
// provider/assetsDir, corrigir responde 503 (como enrich/import).
func TestCorrigirEnrichDesligado503(t *testing.T) {
	h, d := setupEntitiesServer(t) // sem WithSearchProvider/WithAssetsDir => enrich nil
	semearEntity(t, d, "e1", "movie", "X")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/correct",
		map[string]string{"source_id": "1"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "external_source_unavailable")
}

// TestFilaOrfas cobre o critério §5.2: lista só cataloged + com arquivo(s) + TODOS
// missing + sem investimento do dono; EXCLUI item com override ou asset user (o
// teste-chave), com posse, com status de intenção (wanted) e só-importado.
func TestFilaOrfas(t *testing.T) {
	h, d := setupEntitiesServer(t)

	// Órfã canônica: cataloged, 1 arquivo missing, sem investimento.
	semearEntity(t, d, "orfa", "movie", "Orfa")
	semearFile(t, d, "f-orfa", "orfa", "/lib/orfa.mkv", "missing", 80, "")

	// Tem posse (arquivo present) — NÃO é órfã.
	semearEntity(t, d, "com-posse", "movie", "ComPosse")
	semearFile(t, d, "f-posse", "com-posse", "/lib/posse.mkv", "present", 80, "")

	// wanted (intenção do dono), arquivo missing — NÃO é órfã.
	semearEntity(t, d, "wanted", "movie", "Wanted")
	semearFile(t, d, "f-wanted", "wanted", "/lib/wanted.mkv", "missing", 80, "")
	definirStatus(t, d, "wanted", "wanted")

	// Só importada (sem media_file) — NÃO é órfã.
	semearEntity(t, d, "so-import", "movie", "SoImport")

	// Candidata a órfã, MAS com override do dono — NÃO é órfã (investimento).
	semearEntity(t, d, "com-override", "movie", "ComOverride")
	semearFile(t, d, "f-ov", "com-override", "/lib/ov.mkv", "missing", 80, "")
	semearOverride(t, d, "com-override", `{"title":{"pt-BR":"Meu Titulo"}}`)

	// Candidata a órfã, MAS com asset origin='user' — NÃO é órfã (investimento).
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

// TestPurgaIndividualRemoveDerivados cobre o critério §5.3: DELETE remove a entity e
// os derivados (media_file/override/asset via CASCADE), sem afetar outras entities.
func TestPurgaIndividualRemoveDerivados(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Alvo")
	semearFile(t, d, "f1", "e1", "/lib/x.mkv", "missing", 80, "")
	semearOverride(t, d, "e1", `{"title":{"pt-BR":"T"}}`)
	semearAssetUser(t, d, "a1", "e1", "poster", "/assets/e1/user-a.png")
	// Vizinha que deve SOBREVIVER.
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

	// Entity e derivados sumiram (CASCADE).
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
	// Vizinha intacta.
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

// TestPurgaLote cobre o critério §5.3 (lote): purga os ids do corpo, idempotente
// por id ausente, e não afeta as demais entities.
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
	// e1/e3 sumiram, e2 sobreviveu.
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

// TestCorrigirCorpoMalformado422 cobre o branch de corpo JSON inválido do correct
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

// TestPurgaLimpaBinariosDeAssets cobre a limpeza best-effort do diretório de
// binários da entity ({assetsDir}/{id}) na purga individual — o caminho do
// os.RemoveAll com assetsDir configurado (o enrich server tem um).
func TestPurgaLimpaBinariosDeAssets(t *testing.T) {
	h, d, _, _, assetsDir := setupEnrichServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	// Cria o diretório de binários da entity com um arquivo dentro.
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

// provFalha e um provider cujo BuscarDetalhe SEMPRE falha (Nome tmdb, casa o
// find-or-create), para exercitar o 502 do correct quando a fonte não entrega.
type provFalha struct{}

func (*provFalha) Nome() string { return "tmdb" }

func (*provFalha) Buscar(context.Context, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (*provFalha) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return nil, errors.New("fonte fora do ar")
}

func (*provFalha) MapearDetalhe(json.RawMessage) (map[string]any, error) { return nil, nil }

// TestCorrigirFonteFalha502 cobre o branch de falha da fonte no correct (a fonte
// não entrega o detalhe): 502 external_source_error.
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

// TestCuradoriaSemBanco503 cobre a guarda entitiesUnavailable de todas as rotas de
// curadoria quando não há banco injetado (db nil) — degradam com 503.
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
