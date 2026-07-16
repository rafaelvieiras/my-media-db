package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Testes de POST /entities/{id}/download-missing-artwork (briefing 0059):
// baixa a melhor arte disponível para os kinds `artwork:<kind>` que aparecem
// em catalog_completeness.missing (recalculado ao vivo). Sem rede real: o
// download aponta a um httptest allowlistado, mesmo padrão de
// artwork_select_test.go.

// setupDownloadMissingServer sobe um Server com assetsDir temporário e a
// allowlist do proxy restrita ao host informado, devolvendo o handler e o
// *db.DB para os testes semearem entity/config/inventário.
func setupDownloadMissingServer(t *testing.T, hostPermitido string) (http.Handler, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "download-missing.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database, WithAssetsDir(dir))
	s.hostsImagem = map[string]bool{hostPermitido: true}
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database
}

// configurarCompletudeArtwork faz o PUT de uma config de completude que só
// exige os kinds de arte dados (movie), sem os demais campos -- para isolar
// o teste do download de arte de qualquer outro campo faltando.
func configurarCompletudeArtwork(t *testing.T, h http.Handler, kinds ...string) {
	t.Helper()
	corpo := catalogCompletenessSettings{"movie": {Artwork: kinds}}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT catalog-completeness status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// semearInventarioFanart grava o envelope de inventário fanart.tv (mesmo
// formato de artwork_test.go) para a entity, com os kinds/candidatas dadas.
func semearInventarioFanart(t *testing.T, d *db.DB, entityID string, inv map[string][]provider.ArteInventario) {
	t.Helper()
	envelope, err := json.Marshal(map[string]any{provider.ChaveEnvelopeInventarioArtes: inv})
	if err != nil {
		t.Fatalf("marshal inventario: %v", err)
	}
	semearMediaExternal(t, d, "me-fanart-"+entityID, entityID, "fanarttv", string(envelope))
}

// decodeDownloadMissingResult decodifica a resposta do endpoint.
func decodeDownloadMissingResult(t *testing.T, body []byte) downloadMissingArtworkResultDTO {
	t.Helper()
	var out downloadMissingArtworkResultDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar downloadMissingArtworkResultDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestDownloadMissingArtworkBaixaMultiplosKinds cobre o critério §5: com
// inventário fanart.tv disponível para os DOIS kinds faltando (poster,
// fanart), baixa os dois e materializa dois assets origin=external novos.
func TestDownloadMissingArtworkBaixaMultiplosKinds(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster, kindFanart)
	semearEntity(t, d, "e1", "movie", "Duna")
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		kindPoster: {{URL: srv.URL + "/img", Likes: 5}},
		kindFanart: {{URL: srv.URL + "/img", Likes: 5}},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 2 || !contains(resultado.Downloaded, kindPoster) || !contains(resultado.Downloaded, kindFanart) {
		t.Errorf("downloaded = %v, quero [poster fanart] (em qualquer ordem)", resultado.Downloaded)
	}
	if len(resultado.Unavailable) != 0 {
		t.Errorf("unavailable = %v, quero vazio", resultado.Unavailable)
	}

	assets, err := escrita(t, d).ListarAssetsPorEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("assets criados = %d, quero 2 (poster + fanart materializados)", len(assets))
	}
	for _, a := range assets {
		if a.Origin != originExternal {
			t.Errorf("asset %s origin = %q, quero external", a.Kind, a.Origin)
		}
	}
}

// TestDownloadMissingArtworkKindSemOpcao cobre o critério §5: um kind
// faltando SEM nenhuma opção nos providers cai em `unavailable`, sem erro; o
// outro kind (com opção) ainda baixa normalmente.
func TestDownloadMissingArtworkKindSemOpcao(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster, kindClearart)
	semearEntity(t, d, "e1", "movie", "Filme Obscuro")
	// Só poster tem candidata no inventário -- clearart não aparece em lugar
	// nenhum (nem inventário, nem camada externa).
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		kindPoster: {{URL: srv.URL + "/img", Likes: 1}},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 1 || resultado.Downloaded[0] != kindPoster {
		t.Errorf("downloaded = %v, quero [poster]", resultado.Downloaded)
	}
	if len(resultado.Unavailable) != 1 || resultado.Unavailable[0] != kindClearart {
		t.Errorf("unavailable = %v, quero [clearart]", resultado.Unavailable)
	}
}

// TestDownloadMissingArtworkItemCompletoNaoTocaNada cobre o critério §5: item
// já completo (todos os kinds exigidos já têm asset local) devolve as duas
// listas vazias, sem baixar nada.
func TestDownloadMissingArtworkItemCompletoNaoTocaNada(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster)
	semearEntity(t, d, "e1", "movie", "Ja Completo")
	semearAsset(t, d, "a1", "e1", kindPoster, "/x/poster.jpg")
	// Inventário existe (candidata disponível), mas não deveria ser tocado --
	// o item já está completo antes de chegar no loop de download.
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		kindPoster: {{URL: srv.URL + "/img", Likes: 1}},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 0 || len(resultado.Unavailable) != 0 {
		t.Errorf("resultado = %+v, quero as duas listas vazias (item já completo)", resultado)
	}
	assets, err := escrita(t, d).ListarAssetsPorEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	if len(assets) != 1 {
		t.Errorf("assets = %d, quero 1 (o pré-existente, nada novo baixado)", len(assets))
	}
}

// TestDownloadMissingArtworkDraftNaoTocaNada cobre o critério §5: item draft
// (sem catalog_completeness avaliado -- 0058 §3) devolve as duas listas
// vazias, sem tocar nada.
func TestDownloadMissingArtworkDraftNaoTocaNada(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster)
	semearEntity(t, d, "e1", "movie", "Rascunho")
	definirStatus(t, d, "e1", "draft")
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		kindPoster: {{URL: srv.URL + "/img", Likes: 1}},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 0 || len(resultado.Unavailable) != 0 {
		t.Errorf("resultado = %+v, quero as duas listas vazias (draft, sem completude avaliada)", resultado)
	}
}

// TestDownloadMissingArtworkFalhaTentaProxima cobre o critério §5: a
// candidata de MAIOR likes falha ao baixar (500 do httptest) -- o kind ainda
// assim baixa via a PRÓXIMA candidata, best-effort, antes de cair em
// unavailable.
func TestDownloadMissingArtworkFalhaTentaProxima(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster)
	semearEntity(t, d, "e1", "movie", "Com Candidata Ruim")
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		// A candidata de likes MAIOR (10) falha (500); a de likes menor (5)
		// funciona -- prova que a ordem tenta a de likes maior primeiro E
		// que uma falha não desiste do kind.
		kindPoster: {
			{URL: srv.URL + "/erro", Likes: 10},
			{URL: srv.URL + "/img", Likes: 5},
		},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 1 || resultado.Downloaded[0] != kindPoster {
		t.Errorf("downloaded = %v, quero [poster] (a 2a candidata deveria ter salvado o kind)", resultado.Downloaded)
	}
	if len(resultado.Unavailable) != 0 {
		t.Errorf("unavailable = %v, quero vazio", resultado.Unavailable)
	}
	assets, err := escrita(t, d).ListarAssetsPorEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	if len(assets) != 1 {
		t.Errorf("assets criados = %d, quero exatamente 1 (só a candidata que funcionou)", len(assets))
	}
}

// TestDownloadMissingArtworkEntidadeInexistente: 404 para entity inexistente.
func TestDownloadMissingArtworkEntidadeInexistente(t *testing.T) {
	h, _ := setupDownloadMissingServer(t, "example.com")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/download-missing-artwork", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestDownloadMissingArtworkSemAssetsDir: 503 quando o server não tem
// assetsDir configurado (mesma guarda de uploadAsset/selectArtwork).
func TestDownloadMissingArtworkSemAssetsDir(t *testing.T) {
	h, d := setupEntitiesServer(t) // sem WithAssetsDir
	semearEntity(t, d, "e1", "movie", "Sem AssetsDir")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// contains diz se `slice` contém `alvo` (helper simples para asserções de
// ordem-livre nas listas downloaded/unavailable).
func contains(slice []string, alvo string) bool {
	for _, v := range slice {
		if v == alvo {
			return true
		}
	}
	return false
}
