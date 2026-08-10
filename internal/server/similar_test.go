package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Estes testes cobrem a CAMADA 2 do briefing 0091: a fiação da capacidade opcional
// provider.SimilarProvider nos dois pontos de consumo — o detalhe da entity (GET
// /entities/{id}, via camadasDaFonte) e o detalhe externo (GET
// /external/{source}/{type}/{id}, via getExternalDetail). A camada 1 (extração/
// filtro/ordenação em si) já é coberta em internal/provider/tmdb; aqui só provamos
// que os dois handlers CHAMAM a capacidade quando presente, degradam em silêncio
// quando ausente, e sobrevivem a um erro de Similares sem derrubar a resposta.

// fakeProviderSimilar é um provider.Provider mínimo (sem rede) para exercitar a
// fiação do SimilarProvider: Nome() fixo (para casar com media_external.source e
// prioridadeFontes), MapearDetalhe devolve um mapa vazio (a fiação escreve
// "similar" nele por referência) e BuscarDetalhe devolve um payload cru vazio (o
// detalhe externo precisa dele no cache-miss).
type fakeProviderSimilar struct {
	nome string
}

func (f *fakeProviderSimilar) Nome() string { return f.nome }

func (f *fakeProviderSimilar) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (f *fakeProviderSimilar) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (f *fakeProviderSimilar) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// fakeProviderComSimilar embrulha fakeProviderSimilar e IMPLEMENTA
// provider.SimilarProvider — sims/err controlam o retorno fixo de Similares.
type fakeProviderComSimilar struct {
	fakeProviderSimilar
	sims []provider.Similar
	err  error
}

func (f *fakeProviderComSimilar) Similares(json.RawMessage) ([]provider.Similar, error) {
	return f.sims, f.err
}

// simSims é a lista fixa devolvida pelos fakes "com similar" destes testes.
func simSims() []provider.Similar {
	return []provider.Similar{
		{
			SourceID:  "604",
			Type:      "movie",
			Title:     "The Matrix Reloaded",
			Year:      ptr("2003"),
			PosterURL: ptr("https://image.tmdb.org/t/p/w342/rel.jpg"),
		},
	}
}

// TestEntitySimilaresComSimilarProvider: com um provider que implementa
// SimilarProvider, GET /entities/{id} traz "similar" preenchido a partir do que
// Similares(raw) devolveu, convertido para o formato canônico esperado.
func TestEntitySimilaresComSimilarProvider(t *testing.T) {
	prov := &fakeProviderComSimilar{
		fakeProviderSimilar: fakeProviderSimilar{nome: "tmdb"},
		sims:                simSims(),
	}
	h, d := setupSearchServer(t, WithSearchProvider(prov))
	semearEntity(t, d, "x", "movie", "Matrix")
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: "me-x", EntityID: "x", Source: "tmdb",
		SourceID: nsOf("603"), PayloadJson: nsOf(`{}`),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal: %v", err)
	}

	ent := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/x", nil))
	if len(ent.Similar) != 1 {
		t.Fatalf("similar = %+v, quero 1 item", ent.Similar)
	}
	eq(t, "similar.source_id", ent.Similar[0].SourceID, "604")
	eq(t, "similar.type", ent.Similar[0].Type, "movie")
	eq(t, "similar.title", ent.Similar[0].Title, "The Matrix Reloaded")
	eqPtr(t, "similar.year", ent.Similar[0].Year, "2003")
}

// TestEntitySimilaresSemSimilarProvider: um provider que NÃO implementa
// SimilarProvider (o fakeProviderSimilar "puro") deixa "similar" ausente — sem
// erro, sem 500 — igual ao comportamento de hoje quando a fonte não modela o campo.
func TestEntitySimilaresSemSimilarProvider(t *testing.T) {
	prov := &fakeProviderSimilar{nome: "tmdb"}
	h, d := setupSearchServer(t, WithSearchProvider(prov))
	semearEntity(t, d, "x", "movie", "Matrix")
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: "me-x", EntityID: "x", Source: "tmdb",
		SourceID: nsOf("603"), PayloadJson: nsOf(`{}`),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/x", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	if len(ent.Similar) != 0 {
		t.Errorf("similar = %+v, quero vazio (provider sem a capacidade)", ent.Similar)
	}
}

// TestEntitySimilaresErroDegradaSemDerrubar: quando Similares devolve erro, a
// entity continua sendo montada normalmente (200), só sem similares.
func TestEntitySimilaresErroDegradaSemDerrubar(t *testing.T) {
	prov := &fakeProviderComSimilar{
		fakeProviderSimilar: fakeProviderSimilar{nome: "tmdb"},
		err:                 errors.New("payload ilegível"),
	}
	h, d := setupSearchServer(t, WithSearchProvider(prov))
	semearEntity(t, d, "x", "movie", "Matrix")
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: "me-x", EntityID: "x", Source: "tmdb",
		SourceID: nsOf("603"), PayloadJson: nsOf(`{}`),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/x", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 mesmo com Similares em erro (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	if len(ent.Similar) != 0 {
		t.Errorf("similar = %+v, quero vazio (Similares em erro degrada em silêncio)", ent.Similar)
	}
}

// TestExternalDetailSimilaresComSimilarProvider: o MESMO wiring no detalhe externo
// (GET /external/{source}/{type}/{id}) — provider com SimilarProvider preenche
// detalheExternoDTO.Similar.
func TestExternalDetailSimilaresComSimilarProvider(t *testing.T) {
	prov := &fakeProviderComSimilar{
		fakeProviderSimilar: fakeProviderSimilar{nome: "tmdb"},
		sims:                simSims(),
	}
	h, _ := setupSearchServer(t, WithSearchProvider(prov))

	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	d := decodeDetalheExterno(t, rec.Body.Bytes())
	if len(d.Similar) != 1 {
		t.Fatalf("similar = %+v, quero 1 item", d.Similar)
	}
	eq(t, "similar.source_id", d.Similar[0].SourceID, "604")
}

// TestExternalDetailSimilaresSemSimilarProvider: sem a capacidade, o detalhe
// externo segue 200 com "similar" vazio.
func TestExternalDetailSimilaresSemSimilarProvider(t *testing.T) {
	prov := &fakeProviderSimilar{nome: "tmdb"}
	h, _ := setupSearchServer(t, WithSearchProvider(prov))

	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	d := decodeDetalheExterno(t, rec.Body.Bytes())
	if len(d.Similar) != 0 {
		t.Errorf("similar = %+v, quero vazio (provider sem a capacidade)", d.Similar)
	}
}

// TestExternalDetailSimilaresErroDegradaSemDerrubar: Similares em erro não derruba
// o detalhe externo (continua 200, só sem similares).
func TestExternalDetailSimilaresErroDegradaSemDerrubar(t *testing.T) {
	prov := &fakeProviderComSimilar{
		fakeProviderSimilar: fakeProviderSimilar{nome: "tmdb"},
		err:                 errors.New("payload ilegível"),
	}
	h, _ := setupSearchServer(t, WithSearchProvider(prov))

	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 mesmo com Similares em erro (corpo=%s)", rec.Code, rec.Body.String())
	}
	d := decodeDetalheExterno(t, rec.Body.Bytes())
	if len(d.Similar) != 0 {
		t.Errorf("similar = %+v, quero vazio (Similares em erro degrada em silêncio)", d.Similar)
	}
}
