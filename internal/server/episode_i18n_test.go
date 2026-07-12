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

// Estes testes provam o critério central do 0041: com media_external per-idioma, o
// DETALHE do episódio (GET /entities/{id}?lang=) traduz pela cadeia do resolver, igual
// ao filme — sem tocar resolver.go. O provider é um tvdb.Client OFFLINE (MapearDetalhe
// é PURO: reconhece o envelope de episódio sem rede).

// tvdbOffline constrói um tvdb.Client sem rede (só o MapearDetalhe puro é exercitado).
func tvdbOffline() *tvdb.Client {
	return tvdb.New(tvdb.Credencial{APIKey: "x"}, http.DefaultClient, "http://127.0.0.1:0")
}

// semearEpisodioExterno cria um episódio (com título ESCALAR curado como base/fallback) e
// grava seu media_external per-idioma (fonte tvdb) via o envelope compartilhado.
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

// TestEpisodioDetalheTraduzPorLang: o detalhe do episódio serve título/sinopse no idioma
// pedido quando há tradução (?lang=pt-BR → pt-BR; ?lang=en → en) — o filme aplicado ao ep.
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

// TestEpisodioDetalheCaiNoFallback: idioma pedido SEM tradução cai no fallback da cadeia
// ([pt-BR, en]) — nunca vazio. Aqui só há 'en'; ?lang=pt-BR serve o inglês.
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

// TestEpisodioDetalheCaiNoCurado: sem NENHUM idioma da cadeia no externo (só 'es', fora
// de [pt-BR, en]) o detalhe cai no título ESCALAR curado da base — nunca vazio.
func TestEpisodioDetalheCaiNoCurado(t *testing.T) {
	h, d := setupSearchServer(t, WithSearchProvider(tvdbOffline()))
	semearEpisodioExterno(t, d, "ep3", "Titulo Curado",
		map[string]string{"es": "Solo Espanol"}, nil)

	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/ep3?lang=pt-BR", nil))
	eq(t, "titulo curado", pt.Title, "Titulo Curado")
}
