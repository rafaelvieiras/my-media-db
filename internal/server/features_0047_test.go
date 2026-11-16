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

// Tests of default-source robustness: without an explicit `source`, the source is
// resolved DYNAMICALLY by capability — it never assumes "tmdb". And the internal-first
// matches items from ANY source with a generated column (tvdb included), not only
// tmdb.

// provNaoTmdb is a test provider with a CONFIGURABLE Nome() (≠ "tmdb") and a fixed
// detail — it exercises the import path through a source that is not TMDB.
type provNaoTmdb struct{ nome string }

func (p *provNaoTmdb) Nome() string { return p.nome }

func (p *provNaoTmdb) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (p *provNaoTmdb) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{"title":"Item Externo"}`), nil
}

func (p *provNaoTmdb) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{"title": "Item Externo"}, nil
}

// provSerieNaoTmdb extends provNaoTmdb with the SerieProvider capability (an empty tree
// is enough for the capability type-assert — the content does not matter for the source
// selection).
type provSerieNaoTmdb struct{ provNaoTmdb }

func (p *provSerieNaoTmdb) BuscarSerieArvore(context.Context, string, []string) (provider.SerieArvore, error) {
	return provider.SerieArvore{}, nil
}

// provBuscaFonte returns FIXED search results under a configurable Nome(), so the
// internal-first matches the real source of the result (r.Source) against the
// library.
type provBuscaFonte struct {
	nome string
	res  []provider.ResultadoExterno
}

func (p *provBuscaFonte) Nome() string { return p.nome }

func (p *provBuscaFonte) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return p.res, nil
}

func (p *provBuscaFonte) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return nil, errors.New("não implementado")
}

func (p *provBuscaFonte) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// TestFonteDefaultResolveDinamicamente: fonteDefault picks the first provider CAPABLE of
// the type (by priority), without assuming "tmdb". A non-tmdb SerieProvider resolves
// 'movie' AND 'tv'; a core-only provider (without SerieProvider) resolves 'movie' but NOT
// 'tv'.
func TestFonteDefaultResolveDinamicamente(t *testing.T) {
	_, d := setupEntitiesServer(t)

	// Only one provider capable of series, called "tvdb" (not tmdb).
	sCapaz := New("v-teste", d, WithSearchProvider(&provSerieNaoTmdb{provNaoTmdb{nome: "tvdb"}}))
	if src, ok := sCapaz.fonteDefault("movie"); !ok || src != "tvdb" {
		t.Errorf("fonteDefault(movie) = (%q,%v), quero (tvdb,true)", src, ok)
	}
	if src, ok := sCapaz.fonteDefault("tv"); !ok || src != "tvdb" {
		t.Errorf("fonteDefault(tv) = (%q,%v), quero (tvdb,true)", src, ok)
	}

	// Only a core-only provider ("letterboxd"): it resolves movie, but not series.
	sCore := New("v-teste", d, WithSearchProvider(&provNaoTmdb{nome: "letterboxd"}))
	if src, ok := sCore.fonteDefault("movie"); !ok || src != "letterboxd" {
		t.Errorf("fonteDefault(movie) core = (%q,%v), quero (letterboxd,true)", src, ok)
	}
	if src, ok := sCore.fonteDefault("tv"); ok {
		t.Errorf("fonteDefault(tv) core = (%q,%v), quero (\"\",false) sem SerieProvider", src, ok)
	}

	// No provider at all: no source for any type.
	sVazio := New("v-teste", d)
	if src, ok := sVazio.fonteDefault("movie"); ok {
		t.Errorf("fonteDefault(movie) vazio = (%q,%v), quero (\"\",false)", src, ok)
	}
}

// TestImportSemSourceUsaFonteNaoTmdb: an environment with ONLY a non-tmdb provider
// ("tvdb") imports a movie WITHOUT an explicit `source` through the source actually
// registered — it does NOT fall into 502 for assuming "tmdb". The external identity
// written is that of the chosen source.
func TestImportSemSourceUsaFonteNaoTmdb(t *testing.T) {
	h, _ := setupSearchServer(t, WithSearchProvider(&provNaoTmdb{nome: "tvdb"}), WithAssetsDir(t.TempDir()))

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "500", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import sem source status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	eq(t, "external_ids.tvdb", ent.ExternalIDs["tvdb"], "500")
	if _, temTmdb := ent.ExternalIDs["tmdb"]; temTmdb {
		t.Errorf("external_ids não deveria ter tmdb (fonte foi tvdb): %v", ent.ExternalIDs)
	}
}

// TestImportTvSemFonteCapaz422: a series import WITHOUT `source` in an environment whose
// only provider is core-only (without SerieProvider) answers a SEMANTIC 422 ("nenhuma
// fonte disponível para tv"), NOT the generic 502 of unavailable source.
func TestImportTvSemFonteCapaz422(t *testing.T) {
	h, _ := setupSearchServer(t, WithSearchProvider(&provNaoTmdb{nome: "letterboxd"}), WithAssetsDir(t.TempDir()))

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "1", "type": "tv"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("import tv sem fonte capaz status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "no_source_for_type")
}

// provMusicaNaoTmdb extends provNaoTmdb with the music capabilities (MusicaProvider +
// MusicaAlbumProvider, ADR-0027) -- it exercises provedorCapaz("artista"/"album"/
// "faixa"), found while validating the "Baixar Metadados" button for artist/album in
// production: without those explicit capabilities in provedorCapaz, the `default: true`
// accepted ANY registered provider (including a "tmdb" with nothing to do with music) as
// capable of 'artista'/'album', and fonteDefault picked it by priority even without
// knowing how to serve the type.
type provMusicaNaoTmdb struct{ provNaoTmdb }

func (p *provMusicaNaoTmdb) BuscarArtistaArvore(context.Context, string, []string) (provider.ArtistaArvore, error) {
	return provider.ArtistaArvore{}, nil
}

func (p *provMusicaNaoTmdb) BuscarAlbumDaFaixa(context.Context, string, []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (p *provMusicaNaoTmdb) BuscarAlbumPorNome(context.Context, string, string, provider.AlvoFaixaAlbum, []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (p *provMusicaNaoTmdb) BuscarAlbumPorMbid(context.Context, string, []string) (provider.AlbumExterno, error) {
	return provider.AlbumExterno{}, nil
}

// TestFonteDefaultMusicaNaoEscolheProviderSemCapacidade: with a provider WITHOUT music
// registered BEFORE (higher priority) a provider OF music, fonteDefault('artista'/
// 'album'/'faixa') skips the first one and picks the music one -- it never assumes "the
// first registered one serves" for the music types (a regression of the bug reported
// above).
func TestFonteDefaultMusicaNaoEscolheProviderSemCapacidade(t *testing.T) {
	_, d := setupEntitiesServer(t)

	s := New("v-teste", d, WithSearchProvider(
		&provNaoTmdb{nome: "tmdb"},
		&provMusicaNaoTmdb{provNaoTmdb{nome: "musicbrainz"}},
	))

	for _, tipo := range []string{"artista", "album", "faixa"} {
		if src, ok := s.fonteDefault(tipo); !ok || src != "musicbrainz" {
			t.Errorf("fonteDefault(%s) = (%q,%v), quero (musicbrainz,true)", tipo, src, ok)
		}
	}
}

// TestFonteDefaultMusicaSemProviderCapaz422: an environment whose only registered
// provider does not model music (core-only, kind "tmdb") has no default source for
// 'artista'/'album'/'faixa' -- a semantic 422, never a 502/422 "sem source_id" for
// picking an incapable source.
func TestFonteDefaultMusicaSemProviderCapaz422(t *testing.T) {
	_, d := setupEntitiesServer(t)
	s := New("v-teste", d, WithSearchProvider(&provNaoTmdb{nome: "tmdb"}))

	for _, tipo := range []string{"artista", "album", "faixa"} {
		if src, ok := s.fonteDefault(tipo); ok {
			t.Errorf("fonteDefault(%s) = (%q,%v), quero (\"\",false) sem provider de música", tipo, src, ok)
		}
	}
}

// TestIdInternoItemFonteTvdb: the internal-first resolves an item imported from a
// NON-tmdb source (tvdb, through the generated column external_id_tvdb) — not only tmdb.
// A source without a column comes out nil.
func TestIdInternoItemFonteTvdb(t *testing.T) {
	_, d := setupEntitiesServer(t)
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              "ent-tvdb-1",
		Type:            "tv",
		Title:           "Serie Só-TVDB",
		ExternalIdsJson: nsOf(`{"tvdb":"9001"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity(tvdb): %v", err)
	}
	s := New("v-teste", d)
	ctx := context.Background()

	if got := s.idInternoItem(ctx, "tvdb", "9001"); got == nil || *got != "ent-tvdb-1" {
		t.Errorf("idInternoItem(tvdb,9001) = %v, quero ent-tvdb-1", got)
	}
	if got := s.idInternoItem(ctx, "tvdb", "404"); got != nil {
		t.Errorf("idInternoItem(tvdb,404) = %v, quero nil (inexistente)", got)
	}
	if got := s.idInternoItem(ctx, "letterboxd", "9001"); got != nil {
		t.Errorf("idInternoItem(letterboxd,9001) = %v, quero nil (fonte sem coluna gerada)", got)
	}
}

// TestSearchInternoFirstFonteTvdb: a search result from the tvdb source, whose item is
// already in the library, gets internal_id ("na biblioteca") — the internal-first is no
// longer tmdb-only.
func TestSearchInternoFirstFonteTvdb(t *testing.T) {
	prov := &provBuscaFonte{
		nome: "tvdb",
		res: []provider.ResultadoExterno{
			{Source: "tvdb", SourceID: "9001", Type: "tv", Title: "Serie Só-TVDB"},
			{Source: "tvdb", SourceID: "8888", Type: "tv", Title: "Serie Ausente"},
		},
	}
	h, d := setupSearchServer(t, WithSearchProvider(prov))
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              "ent-tvdb-1",
		Type:            "tv",
		Title:           "Serie Só-TVDB",
		ExternalIdsJson: nsOf(`{"tvdb":"9001"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity(tvdb): %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/search?q=serie", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeSearch(t, rec.Body.Bytes())
	byID := map[string]externalResultDTO{}
	for _, e := range out.External {
		byID[e.SourceID] = e
	}
	if e := byID["9001"]; e.InternalID == nil || *e.InternalID != "ent-tvdb-1" {
		t.Errorf("resultado tvdb 9001 internal_id = %v, quero ent-tvdb-1", e.InternalID)
	}
	if e := byID["8888"]; e.InternalID != nil {
		t.Errorf("resultado tvdb 8888 internal_id = %v, quero nil (não importado)", e.InternalID)
	}
}
