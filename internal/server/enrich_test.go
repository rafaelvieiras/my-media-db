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
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
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

func (p *detProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (p *detProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
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

// TestImportMultiIdiomaResolvePorLang e o end-to-end do 0020 SEM REDE: liga o
// provider TMDB REAL a um httptest servindo o detalhe (com translations) e as
// listas de genero localizadas; importa e le a entity por ?lang=. Prova o criterio
// 1 (?lang=pt-BR serve pt-BR, ?lang=en serve en; generos traduzidos) e que
// original_language e gravado. Sem poster_path no detalhe -> sem download de imagem.
func TestImportMultiIdiomaResolvePorLang(t *testing.T) {
	const detalhe = `{
      "id": 900,
      "original_language": "en",
      "title": "The Matrix",
      "original_title": "The Matrix",
      "overview": "Neo learns the truth.",
      "release_date": "1999-03-31",
      "genres": [{"id": 878, "name": "Science Fiction"}],
      "translations": {"translations": [
        {"iso_3166_1": "US", "iso_639_1": "en", "data": {"title": "The Matrix", "overview": "Neo learns the truth."}},
        {"iso_3166_1": "BR", "iso_639_1": "pt", "data": {"title": "Matrix", "overview": "Neo descobre a verdade."}}
      ]}
    }`
	generos := map[string]string{
		"pt-BR": `{"genres":[{"id":878,"name":"Ficcao cientifica"}]}`,
		"en":    `{"genres":[{"id":878,"name":"Science Fiction"}]}`,
	}
	srv := servidorTMDBMultiIdioma(t, detalhe, generos)

	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	h, _ := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "900", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	id := decodeEntidade(t, rec).ID

	// ?lang=pt-BR serve o portugues (titulo/sinopse/generos traduzidos).
	assertEntidadePTBR(t, decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil)))
	// ?lang=en serve o ingles.
	assertEntidadeEN(t, decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/"+id+"?lang=en", nil)))
}

// TestImportEntityCreditosParidadeComExterno prova a PARIDADE (0029) entre o detalhe
// EXTERNO (GET /external/...) e a entity IMPORTADA (GET /entities/{id}): os mesmos
// créditos (elenco/equipe/produtoras), no MESMO shape, com profile_url/logo_url JÁ
// PROXYADOS nos dois lados (assetsDir configurado). Dividido em helpers (um assert
// por bloco) para manter a gocyclo baixa.
func TestImportEntityCreditosParidadeComExterno(t *testing.T) {
	srv := servidorTMDBRico(t, nil)
	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	h, _ := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	id := decodeEntidade(t, rec).ID

	ent := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil))
	ext := decodeDetalheExterno(t, do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil).Body.Bytes())

	assertParidadeCast(t, ent, ext)
	assertParidadeCrew(t, ent, ext)
	assertParidadeCompanies(t, ent, ext)
}

// assertParidadeCast confere o elenco: mesmo shape e profile_url proxyado nos dois
// lados (entity e externo).
func assertParidadeCast(t *testing.T, ent resolver.Entidade, ext detalheExternoDTO) {
	t.Helper()
	if len(ent.Cast) != 1 || len(ext.Cast) != 1 {
		t.Fatalf("cast: entity=%d externo=%d, quero 1 nos dois", len(ent.Cast), len(ext.Cast))
	}
	e, x := ent.Cast[0], ext.Cast[0]
	if e.SourceID != x.SourceID || e.Name != x.Name || e.Character != x.Character || e.Order != x.Order {
		t.Errorf("cast diverge: entity=%+v externo=%+v", e, x)
	}
	assertProfileURLParidadeEProxy(t, "cast", e.ProfileURL, x.ProfileURL)
}

// assertProfileURLParidadeEProxy confere que as URLs de foto/logo do lado entity e do
// lado externo são IGUAIS e que a do lado entity já é a rota do proxy local — extraído
// dos asserts de créditos só para manter a gocyclo baixa.
func assertProfileURLParidadeEProxy(t *testing.T, rotulo string, deEntity, deExterno *string) {
	t.Helper()
	if deEntity == nil || deExterno == nil || *deEntity != *deExterno {
		t.Errorf("%s: url diverge entre entity=%v e externo=%v", rotulo, deEntity, deExterno)
		return
	}
	if !strings.HasPrefix(*deEntity, rotaProxyImagem+"?src=") {
		t.Errorf("%s: entity url = %q, quero rota do proxy local", rotulo, *deEntity)
	}
}

// assertParidadeCrew confere a equipe: mesmo shape nos dois lados.
func assertParidadeCrew(t *testing.T, ent resolver.Entidade, ext detalheExternoDTO) {
	t.Helper()
	if len(ent.Crew) != 1 || len(ext.Crew) != 1 || ent.Crew[0].Job != ext.Crew[0].Job {
		t.Errorf("crew diverge: entity=%+v externo=%+v", ent.Crew, ext.Crew)
	}
}

// assertParidadeCompanies confere as produtoras: mesmo shape e logo_url proxyado.
func assertParidadeCompanies(t *testing.T, ent resolver.Entidade, ext detalheExternoDTO) {
	t.Helper()
	if len(ent.Companies) != 1 || len(ext.Companies) != 1 {
		t.Fatalf("companies: entity=%d externo=%d, quero 1 nos dois", len(ent.Companies), len(ext.Companies))
	}
	e, x := ent.Companies[0], ext.Companies[0]
	assertProfileURLParidadeEProxy(t, "company.logo_url", e.LogoURL, x.LogoURL)
}

// servidorTMDBMultiIdioma sobe um httptest que serve o detalhe do filme e as listas de
// genero localizadas por ?language=, como a API do TMDB.
func servidorTMDBMultiIdioma(t *testing.T, detalhe string, generos map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movie/900":
			_, _ = w.Write([]byte(detalhe))
		case "/genre/movie/list":
			_, _ = w.Write([]byte(generos[r.URL.Query().Get("language")]))
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// assertEntidadePTBR confere titulo/sinopse/generos traduzidos e a ancora original_language.
func assertEntidadePTBR(t *testing.T, pt resolver.Entidade) {
	t.Helper()
	eq(t, "pt.title", pt.Title, "Matrix")
	if pt.Description == nil || *pt.Description != "Neo descobre a verdade." {
		t.Errorf("pt.description = %v, quero 'Neo descobre a verdade.'", pt.Description)
	}
	if len(pt.Tags) != 1 || pt.Tags[0] != "Ficcao cientifica" {
		t.Errorf("pt.tags = %v, quero [Ficcao cientifica]", pt.Tags)
	}
	if pt.OriginalLanguage == nil || *pt.OriginalLanguage != "en" {
		t.Errorf("original_language = %v, quero 'en' (ancora gravada)", pt.OriginalLanguage)
	}
}

// assertEntidadeEN confere titulo e generos servidos em ingles.
func assertEntidadeEN(t *testing.T, en resolver.Entidade) {
	t.Helper()
	eq(t, "en.title", en.Title, "The Matrix")
	if len(en.Tags) != 1 || en.Tags[0] != "Science Fiction" {
		t.Errorf("en.tags = %v, quero [Science Fiction]", en.Tags)
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
