package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tvdb"
)

// These tests prove the central criterion: with per-language media_external, the
// EPISODE detail (GET /entities/{id}?lang=) translates through the resolver chain, just
// like the movie — without touching resolver.go. The provider is an OFFLINE tvdb.Client
// (MapearDetalhe is PURE: it recognizes the episode envelope without network).

// tvdbOffline builds a tvdb.Client without network (only the pure MapearDetalhe is
// exercised).
func tvdbOffline() *tvdb.Client {
	return tvdb.New(tvdb.Credencial{APIKey: "x"}, http.DefaultClient, "http://127.0.0.1:0")
}

// semearEpisodioExterno creates an episode (with a curated SCALAR title as
// base/fallback) and writes its per-language media_external (source tvdb) via the shared
// envelope.
func semearEpisodioExterno(t *testing.T, d *db.DB, id, curado string, title, overview map[string]string) {
	t.Helper()
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: "episode", Title: curado,
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
	payload, err := provider.MontarEnvelopeEpisodio(provider.EpisodioExterno{
		SourceID: id, Title: title, Overview: overview,
	}, "")
	if err != nil {
		t.Fatalf("MontarEnvelopeEpisodio: %v", err)
	}
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: "me-" + id, EntityID: id, Source: "tvdb",
		SourceID: nsOf(id), PayloadJson: nsOf(string(payload)),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal(%s): %v", id, err)
	}
}

// TestEpisodioDetalheTraduzPorLang: the episode detail serves title/synopsis in the
// requested language when there is a translation (?lang=pt-BR → pt-BR; ?lang=en → en) —
// the movie behavior applied to the episode.
func TestEpisodioDetalheTraduzPorLang(t *testing.T) {
	h, d := setupSearchServer(t, WithSearchProvider(tvdbOffline()))
	semearEpisodioExterno(t, d, "ep1", "Curado 1",
		map[string]string{"en": "Episode One", "pt-BR": "Episodio Um"},
		map[string]string{"en": "The first", "pt-BR": "O primeiro"})

	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/ep1?lang=pt-BR", nil))
	eq(t, "titulo pt-BR", pt.Title, "Episodio Um")
	if pt.Description == nil || *pt.Description != "O primeiro" {
		t.Errorf("sinopse pt-BR = %v, quero 'O primeiro'", pt.Description)
	}

	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/ep1?lang=en", nil))
	eq(t, "titulo en", en.Title, "Episode One")
	if en.Description == nil || *en.Description != "The first" {
		t.Errorf("sinopse en = %v, quero 'The first'", en.Description)
	}
}

// TestEpisodioDetalheCaiNoFallback: a requested language WITHOUT a translation falls back
// to the chain ([pt-BR, en]) — never empty. Here there is only 'en'; ?lang=pt-BR serves
// english.
func TestEpisodioDetalheCaiNoFallback(t *testing.T) {
	h, d := setupSearchServer(t, WithSearchProvider(tvdbOffline()))
	semearEpisodioExterno(t, d, "ep2", "Curado 2",
		map[string]string{"en": "Only English"},
		map[string]string{"en": "Only overview"})

	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/ep2?lang=pt-BR", nil))
	eq(t, "titulo fallback en", pt.Title, "Only English")
	if pt.Description == nil || *pt.Description != "Only overview" {
		t.Errorf("sinopse fallback = %v, quero 'Only overview'", pt.Description)
	}
}

// TestEpisodioDetalheCaiNoCurado: without ANY language of the chain in the external one
// (only 'es', outside [pt-BR, en]) the detail falls back to the curated SCALAR title of
// the base — never empty.
func TestEpisodioDetalheCaiNoCurado(t *testing.T) {
	h, d := setupSearchServer(t, WithSearchProvider(tvdbOffline()))
	semearEpisodioExterno(t, d, "ep3", "Titulo Curado",
		map[string]string{"es": "Solo Espanol"}, nil)

	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/ep3?lang=pt-BR", nil))
	eq(t, "titulo curado", pt.Title, "Titulo Curado")
}
