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

// These tests cover the wiring of the OPTIONAL capability provider.SimilarProvider at the two
// consumption points — the entity detail (GET /entities/{id}, via camadasDaFonte) and the
// external detail (GET /external/{source}/{type}/{id}, via getExternalDetail). The
// extraction/filter/ordering itself is already covered in internal/provider/tmdb; here we only
// prove that the two handlers CALL the capability when it is present, degrade silently when it
// is absent, and survive a Similares error without bringing the response down.

// fakeProviderSimilar is a minimal provider.Provider (no network) to exercise the
// SimilarProvider wiring: a fixed Nome() (to match media_external.source and prioridadeFontes),
// MapearDetalhe returns an empty map (the wiring writes "similar" into it by reference) and
// BuscarDetalhe returns an empty raw payload (the external detail needs it on a cache-miss).
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

// fakeProviderComSimilar wraps fakeProviderSimilar and IMPLEMENTS provider.SimilarProvider —
// sims/err control the fixed return of Similares.
type fakeProviderComSimilar struct {
	fakeProviderSimilar
	sims []provider.Similar
	err  error
}

func (f *fakeProviderComSimilar) Similares(json.RawMessage) ([]provider.Similar, error) {
	return f.sims, f.err
}

// simSims is the fixed list returned by the "with similar" fakes of these tests.
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

// TestEntitySimilaresComSimilarProvider: with a provider that implements SimilarProvider,
// GET /entities/{id} brings "similar" filled in from what Similares(raw) returned, converted to
// the expected canonical format.
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

// TestEntitySimilaresSemSimilarProvider: a provider that does NOT implement SimilarProvider
// (the "pure" fakeProviderSimilar) leaves "similar" absent — no error, no 500 — like the
// behavior when the source does not model the field.
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

// TestEntitySimilaresErroDegradaSemDerrubar: when Similares returns an error, the entity is
// still built normally (200), only without similars.
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

// TestExternalDetailSimilaresComSimilarProvider: the SAME wiring on the external detail
// (GET /external/{source}/{type}/{id}) — a provider with SimilarProvider fills in
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

// TestExternalDetailSimilaresSemSimilarProvider: without the capability, the external detail
// stays 200 with an empty "similar".
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

// TestExternalDetailSimilaresErroDegradaSemDerrubar: Similares in error does not bring the
// external detail down (it stays 200, only without similars).
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
