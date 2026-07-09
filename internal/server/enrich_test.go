package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// detProvider é um provider fake para os testes de enrich/import no server:
// Nome()="tmdb" (casa o find-or-create e a composição externa), BuscarDetalhe
// devolve um raw fixo e MapearDetalhe campos canônicos fixos (com poster_url num
// httptest de imagem).
type detProvider struct {
	raw    json.RawMessage
	campos map[string]any
}

func (p *detProvider) Nome() string { return "tmdb" }

func (p *detProvider) Buscar(context.Context, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (p *detProvider) BuscarDetalhe(context.Context, string, string) (json.RawMessage, error) {
	return p.raw, nil
}

func (p *detProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return p.campos, nil
}

// setupEnrichServer sobe um Server com banco migrado, um provider fake (tmdb) e um
// assetsDir temporário, e um httptest servindo o binário do pôster. Devolve tudo o
// necessário para os testes de enrich/import.
func setupEnrichServer(t *testing.T) (http.Handler, *db.DB, *detProvider, []byte, string) {
	t.Helper()
	imgBytes := []byte("POSTER-BYTES")
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imgBytes)
	}))
	t.Cleanup(img.Close)

	prov := &detProvider{
		raw: json.RawMessage(`{"title":"Filme Externo","overview":"sinopse externa"}`),
		campos: map[string]any{
			"title":       "Filme Externo",
			"description": "sinopse externa",
			"poster_url":  img.URL + "/p.png",
		},
	}
	assetsDir := t.TempDir()
	h, d := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(assetsDir))
	return h, d, prov, imgBytes, assetsDir
}

// decodeEntidade decodifica a entity resolvida do corpo.
func decodeEntidade(t *testing.T, rec *httptest.ResponseRecorder) resolver.Entidade {
	t.Helper()
	var e resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decodificar Entidade: %v (corpo=%s)", err, rec.Body.String())
	}
	return e
}

func TestImportCriaEReusaSemDuplicar(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source": "tmdb", "source_id": "777", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import #1 status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	// Resposta resolvida reflete o detalhe externo (título e sinopse compostos).
	eq(t, "title", ent.Title, "Filme Externo")
	if ent.Description == nil || *ent.Description != "sinopse externa" {
		t.Errorf("description = %v, quero 'sinopse externa' (composição externa)", ent.Description)
	}
	eq(t, "external_ids.tmdb", ent.ExternalIDs["tmdb"], "777")
	id1 := ent.ID

	// Segundo import mesmo id externo: reusa (200), não duplica.
	rec2 := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source": "tmdb", "source_id": "777", "type": "movie"})
	if rec2.Code != http.StatusOK {
		t.Fatalf("import #2 status = %d, quero 200 (reuso)", rec2.Code)
	}
	ent2 := decodeEntidade(t, rec2)
	if ent2.ID != id1 {
		t.Errorf("id do reuso = %q, quero %q", ent2.ID, id1)
	}
	todas, _ := escrita(t, d).ListarEntities(context.Background(), sqlcgen.ListarEntitiesParams{Limit: 100, Offset: 0})
	if len(todas) != 1 {
		t.Errorf("total de entities = %d, quero 1 (dedup por external_id)", len(todas))
	}
}

func TestEnrichEntityInexistente404(t *testing.T) {
	h, _, _, _, _ := setupEnrichServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/enrich", map[string]string{"source_id": "1"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

func TestEnrichSemSourceID422(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	semearEntity(t, d, "e1", "movie", "Sem Id Externo") // sem external_ids
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/enrich", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

func TestImportValidacoes(t *testing.T) {
	h, _, _, _, _ := setupEnrichServer(t)

	// Corpo JSON malformado -> 400.
	rec := do(t, h, http.MethodPost, "/api/v1/import", []byte("{nao-json"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("corpo inválido: status = %d, quero 400", rec.Code)
	}
	// type fora de movie|tv -> 422.
	rec = do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "1", "type": "person"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("type inválido: status = %d, quero 422", rec.Code)
	}
	// source_id vazio -> 422.
	rec = do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"type": "movie"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("source_id vazio: status = %d, quero 422", rec.Code)
	}
}

func TestEnrichPosterServidoDoAssetsDir(t *testing.T) {
	h, _, _, imgBytes, assetsDir := setupEnrichServer(t)

	// Importa: cria a entity, cacheia o pôster externo sob o assetsDir.
	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "777", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)

	// O pôster externo é servível pela guarda (path sob o assetsDir confiável).
	poster := do(t, h, http.MethodGet, "/api/v1/entities/"+ent.ID+"/assets/poster", nil)
	if poster.Code != http.StatusOK {
		t.Fatalf("GET poster status = %d, quero 200 (corpo=%s)", poster.Code, poster.Body.String())
	}
	if poster.Body.String() != string(imgBytes) {
		t.Errorf("binário servido diverge do cacheado")
	}
	// Sanidade: o arquivo mora mesmo sob o assetsDir.
	if _, err := filepath.Rel(assetsDir, filepath.Join(assetsDir, ent.ID)); err != nil {
		t.Fatalf("layout de assets inesperado: %v", err)
	}
}

func TestPosterForaDosDirsConfiaveis403(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	// Asset apontando para fora de qualquer dir confiável (nem source nem assetsDir).
	if _, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID: "a1", EntityID: "e1", Kind: "poster", Origin: "external", PathOrUri: "/etc/segredo.png",
	}); err != nil {
		t.Fatalf("CriarAsset: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/assets/poster", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quero 403 (path fora dos dirs confiáveis)", rec.Code)
	}
	checkErrorShape(t, rec, "forbidden")
}
