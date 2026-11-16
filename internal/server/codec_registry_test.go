package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
)

// These tests cover ADR-0024: the separation between the LIVE SEARCH capability
// (requires a credential, `providers` registry) and the ability to PARSE a payload
// already cached (`codecs` registry, always available regardless of credential). An
// item already cataloged (media_external/external_detail already written) must stay
// rich on the screen even without ANY live search provider registered -- only
// WithCodecRegistry, which main.go does unconditionally through
// provedoresConhecidos().

// TestEntityRicaComCodecRegistrySemBuscaAoVivo (acceptance criterion (a)): an entity
// with media_external already cached keeps bringing rich fields (here, "similar",
// already wired through camadasDaFonte/codecPara) when the source is only registered in
// WithCodecRegistry -- NO live search provider (WithSearchProvider) is present.
func TestEntityRicaComCodecRegistrySemBuscaAoVivo(t *testing.T) {
	prov := &fakeProviderComSimilar{
		fakeProviderSimilar: fakeProviderSimilar{nome: "tmdb"},
		sims:                simSims(),
	}
	// Codec only -- proves that reading does NOT depend on live search.
	h, d := setupSearchServer(t, WithCodecRegistry(prov))
	semearEntity(t, d, "x", "movie", "Matrix")
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: "me-x", EntityID: "x", Source: "tmdb",
		SourceID: nsOf("603"), PayloadJson: nsOf(`{}`),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal: %v", err)
	}

	ent := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/x", nil))
	if len(ent.Similar) != 1 {
		t.Fatalf("similar = %+v, quero 1 item (codec deveria resolver mesmo sem busca ao vivo)", ent.Similar)
	}
	eq(t, "similar.source_id", ent.Similar[0].SourceID, "604")
}

// TestExternalDetailStaleSemBuscaAoVivo (acceptance criterion (b)): with an
// external_detail cache already written and EXPIRED (fetched_at beyond the TTL), and the
// source only registered in WithCodecRegistry (no WithSearchProvider), GET
// /external/{source}/{type}/{id} serves the STALE payload (200) instead of 503/404 --
// data already visited does not disappear for lack of a credential.
func TestExternalDetailStaleSemBuscaAoVivo(t *testing.T) {
	prov := tmdb.New(tmdb.Credencial{}, nil, "") // no credential: parse only (codec)
	h, d := setupSearchServer(t, WithCodecRegistry(prov))

	// The payload persisted by the service is always the AUGMENTED ENVELOPE that
	// BuscarDetalhe builds ({detail,tags_by_lang}), not the raw legacy JSON -- it matches
	// the format MapearDetalhe really expects to resolve per language.
	envelope := `{"detail":` + filmeRicoJSON + `,"tags_by_lang":{}}`
	if err := sqlcgen.New(d.Write).UpsertExternalDetail(context.Background(), sqlcgen.UpsertExternalDetailParams{
		Source: "tmdb", SourceID: "603", Type: "movie",
		PayloadJson: nsOf(envelope), LangsJson: nsOf(`["pt-BR","en"]`),
	}); err != nil {
		t.Fatalf("UpsertExternalDetail: %v", err)
	}
	// Forces the timestamp far into the past -- beyond the 24h TTLPadrao.
	if _, err := d.Write.ExecContext(context.Background(),
		`UPDATE external_detail SET fetched_at = '2000-01-01T00:00:00.000Z' WHERE source='tmdb' AND source_id='603'`); err != nil {
		t.Fatalf("forcar staleness: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (stale servido) (corpo=%s)", rec.Code, rec.Body.String())
	}
	d2 := decodeDetalheExterno(t, rec.Body.Bytes())
	eq(t, "title", d2.Title, "Matrix")
}

// TestExternalDetail503FonteTotalmenteDesconhecida ensures that a source with NO
// registration at all (neither codecs nor providers) still answers 503 -- the behavior
// does not regress for sources the binary cannot even interpret.
func TestExternalDetail503FonteTotalmenteDesconhecida(t *testing.T) {
	h, _ := setupSearchServer(t) // neither codec nor live search
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "external_source_unavailable")
}
