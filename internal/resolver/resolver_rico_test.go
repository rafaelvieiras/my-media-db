package resolver

import (
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// These tests cover the RICH FIELDS (parity with the external detail) that the
// resolver exposes from the canonical external layer: translatable ones
// (tagline/keywords) composed per language, and neutral/structured ones (runtime,
// certification by region, vote_average, backdrop, videos, collection, similar,
// external_links) extracted straight from the canonical via richfields.

// entRica is a core entity with a TMDB external id (for the TMDB link) and an
// original language anchoring the fallback chain.
func entRica() sqlcgen.Entity {
	return sqlcgen.Entity{
		ID:               "x",
		Type:             "movie",
		Title:            "Titulo Arquivo",
		Status:           "cataloged",
		ExternalIdsJson:  ns(`{"tmdb":"603","imdb":"tt0133093"}`),
		OriginalLanguage: ns("en"),
	}
}

// camadaRica returns a canonical external layer with the rich fields in the SAME
// format as MapearDetalhe (translatable ones as map[idioma]valor; neutral ones as
// scalar/list/map; structures as []any of map[string]any).
func camadaRica() []CamadaExterna {
	return []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{
		"tagline":                 map[string]any{"pt-BR": "Bem-vindo ao mundo real.", "en": "Welcome to the real world."},
		"keywords":                map[string]any{"en": []any{"simulated reality", "artificial intelligence"}},
		"runtime":                 136,
		"vote_average":            8.2,
		"backdrop_url":            "https://img/backdrop.jpg",
		"certification_by_region": map[string]any{"BR": "14", "US": "R"},
		"videos": []any{
			map[string]any{"name": "Trailer", "key": "abc", "site": "YouTube", "type": "Trailer", "official": true, "url": "https://youtu.be/abc"},
		},
		"collection": map[string]any{"source_id": "2344", "name": "Colecao Matrix", "poster_url": "https://img/col.jpg"},
		"similar": []any{
			map[string]any{"source_id": "604", "type": "movie", "title": "Matrix Reloaded", "year": "2003", "poster_url": "https://img/604.jpg"},
		},
		"external_links": map[string]any{"imdb": "https://www.imdb.com/title/tt0133093"},
		"cast": []any{
			map[string]any{"source_id": "6384", "name": "Keanu Reeves", "character": "Neo", "order": 0, "profile_path": "/keanu.jpg"},
		},
		"crew": []any{
			map[string]any{"source_id": "9339", "name": "Lana Wachowski", "department": "Directing", "job": "Director"},
		},
		"companies": []any{
			map[string]any{"source_id": "174", "name": "Warner Bros.", "logo_path": "/wb.png"},
		},
	}}}
}

func TestResolverCamposRicosTraduziveis(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
	})

	// tagline resolves in the requested language (pt-BR); keywords fall back to en (it
	// only exists there, anchored in the original_language of the chain).
	eqPtr(t, "tagline", got.Tagline, "Bem-vindo ao mundo real.")
	eq(t, "tagline prov", got.ResolvedLanguages["tagline"], "pt-BR")
	if len(got.Keywords) != 2 || got.Keywords[0] != "simulated reality" {
		t.Errorf("keywords = %v, quero [simulated reality, artificial intelligence]", got.Keywords)
	}
	eq(t, "keywords prov", got.ResolvedLanguages["keywords"], "en")
}

func TestResolverCamposRicosNeutros(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
	})

	eqPtr(t, "runtime", got.Runtime, 136)
	eqPtr(t, "vote_average", got.VoteAverage, 8.2)
	eqPtr(t, "backdrop_url", got.BackdropURL, "https://img/backdrop.jpg")
	// Certification by the region of the active language (pt-BR → BR).
	eqPtr(t, "certification", got.Certification, "14")

	if len(got.Videos) != 1 || got.Videos[0].Name != "Trailer" || got.Videos[0].URL == nil {
		t.Fatalf("videos = %+v, quero 1 trailer com url", got.Videos)
	}
	if got.Collection == nil || got.Collection.Name != "Colecao Matrix" {
		t.Fatalf("collection = %+v, quero Colecao Matrix", got.Collection)
	}
	if len(got.Similar) != 1 || got.Similar[0].SourceID != "604" {
		t.Fatalf("similar = %+v, quero 1 similar 604", got.Similar)
	}
	// external_links: imdb from the canonical + tmdb built from
	// external_ids[tmdb]+type.
	eq(t, "link imdb", got.ExternalLinks["imdb"], "https://www.imdb.com/title/tt0133093")
	eq(t, "link tmdb", got.ExternalLinks["tmdb"], "https://www.themoviedb.org/movie/603")
}

// TestResolverCamposRicosCreditos covers the CREDITS: the PURE resolver extracts
// them via richfields with the DIRECT (non-proxied) URL — the server proxies after
// resolution (outside the pure resolver, the same boundary as
// Ancestors/Ownership).
func TestResolverCamposRicosCreditos(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
	})

	if len(got.Cast) != 1 || got.Cast[0].SourceID != "6384" || got.Cast[0].Character != "Neo" {
		t.Fatalf("cast = %+v, quero 1 credito (6384/Neo)", got.Cast)
	}
	eqPtr(t, "cast[0].ProfileURL", got.Cast[0].ProfileURL, "https://image.tmdb.org/t/p/w185/keanu.jpg")
	if len(got.Crew) != 1 || got.Crew[0].SourceID != "9339" || got.Crew[0].Job != "Director" {
		t.Fatalf("crew = %+v, quero 1 credito (9339/Director)", got.Crew)
	}
	if len(got.Companies) != 1 || got.Companies[0].Name != "Warner Bros." {
		t.Fatalf("companies = %+v, quero 1 produtora (Warner Bros.)", got.Companies)
	}
	eqPtr(t, "companies[0].LogoURL", got.Companies[0].LogoURL, "https://image.tmdb.org/t/p/w185/wb.png")
}

// TestResolverBackdropLocalVenceCanonico: when the caller passes BackdropURL
// (background cached locally), the resolver serves it instead of the source's direct
// URL; without it, it falls back to the `backdrop_url` of the external canonical.
func TestResolverBackdropLocalVenceCanonico(t *testing.T) {
	local := "/api/v1/entities/x/assets/fanart"
	comLocal := Resolver(Camadas{
		Entity:      entRica(),
		Cadeia:      []string{"pt-BR"},
		LangPedido:  "pt-BR",
		Externo:     camadaRica(),
		BackdropURL: &local,
	})
	eqPtr(t, "backdrop local", comLocal.BackdropURL, local)

	semLocal := Resolver(Camadas{
		Entity:     entRica(),
		Cadeia:     []string{"pt-BR"},
		LangPedido: "pt-BR",
		Externo:    camadaRica(),
	})
	eqPtr(t, "backdrop canonico", semLocal.BackdropURL, "https://img/backdrop.jpg")
}

// TestResolverCertificacaoFallbackUS: a language with no region (en) falls back to
// the US region.
func TestResolverCertificacaoFallbackUS(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"en"},
		LangPedido:      "en",
		IdiomaPrincipal: "en",
		Externo:         camadaRica(),
	})
	eqPtr(t, "certification US", got.Certification, "R")
}

// TestResolverSemExternoCamposRicosVazios: with no external layer, the rich fields
// come out empty/nil and the lists as `[]` (never null) — the front end does not
// break.
func TestResolverSemExternoCamposRicosVazios(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
	})
	if got.Tagline != nil || got.Runtime != nil || got.Certification != nil || got.Collection != nil {
		t.Errorf("esperava campos ricos nil sem externo, veio tagline=%v runtime=%v cert=%v col=%v",
			got.Tagline, got.Runtime, got.Certification, got.Collection)
	}
	if got.Videos == nil || got.Similar == nil || got.Keywords == nil {
		t.Errorf("listas ricas devem ser [] (nao nil): videos=%v similar=%v keywords=%v",
			got.Videos, got.Similar, got.Keywords)
	}
}

// TestResolverSemExternoCreditosVazios: with no external layer, the credits come out
// as `[]` (never null) — the front end does not break. Isolated from
// TestResolverSemExternoCamposRicosVazios just to keep gocyclo low.
func TestResolverSemExternoCreditosVazios(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
	})
	if got.Cast == nil || got.Crew == nil || got.Companies == nil {
		t.Errorf("creditos devem ser [] (nao nil): cast=%v crew=%v companies=%v",
			got.Cast, got.Crew, got.Companies)
	}
}

// TestResolverAiringStatusSerie: the series AIRING situation is exposed VERBATIM from
// the external canonical (neutral, read-only); absent → nil. It is distinct from the
// CATALOG Status (owned/cataloged) — it does not contaminate it.
func TestResolverAiringStatusSerie(t *testing.T) {
	ent := entRica()
	ent.Type = "tv"
	ent.Status = "cataloged" // CATALOG status, distinct from the airing situation

	comStatus := Resolver(Camadas{
		Entity:          ent,
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo: []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{
			"airing_status": "Ended",
		}}},
	})
	eqPtr(t, "airing_status", comStatus.AiringStatus, "Ended")
	eq(t, "status catalogo intacto", comStatus.Status, "cataloged")

	// Rich canonical WITHOUT airing_status → nil (TVDB-only series with no match /
	// movie).
	semStatus := Resolver(Camadas{
		Entity:          ent,
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
	})
	if semStatus.AiringStatus != nil {
		t.Errorf("airing_status = %v, quero nil (ausente)", *semStatus.AiringStatus)
	}
}

// TestResolverRatingsPorFonte: each external layer that models vote_average enters
// Ratings, in the ORDER of the layers (already by priority, R2) — distinct from
// VoteAverage, which is only the merged value of the highest priority source.
func TestResolverRatingsPorFonte(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo: []CamadaExterna{
			{Fonte: "tmdb", Campos: map[string]any{"vote_average": 7.5}},
			{Fonte: "outrafonte", Campos: map[string]any{"vote_average": 8.0}},
		},
	})
	if len(got.Ratings) != 2 {
		t.Fatalf("ratings = %+v, quero 2 entradas", got.Ratings)
	}
	if got.Ratings[0].Source != "tmdb" || got.Ratings[0].Value != 7.5 {
		t.Errorf("ratings[0] = %+v, quero {tmdb 7.5}", got.Ratings[0])
	}
	if got.Ratings[1].Source != "outrafonte" || got.Ratings[1].Value != 8.0 {
		t.Errorf("ratings[1] = %+v, quero {outrafonte 8.0}", got.Ratings[1])
	}
}

// TestResolverRatingsPorFonteVazio: no layer models vote_average → Ratings comes out
// `[]RatingFonte{}` (never nil), and the JSON serializes as `[]` (never null).
func TestResolverRatingsPorFonteVazio(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo: []CamadaExterna{
			{Fonte: "tmdb", Campos: map[string]any{"runtime": 120}},
		},
	})
	if got.Ratings == nil || len(got.Ratings) != 0 {
		t.Fatalf("ratings = %v, quero [] (nao nil)", got.Ratings)
	}
	b, err := json.Marshal(got.Ratings)
	if err != nil {
		t.Fatalf("marshal ratings: %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("json ratings = %s, quero []", b)
	}
}

// TestCamposEditaveisExcluiRicos: the read-only rich fields (tagline/keywords/
// airing_status) do NOT appear on the editing surface through override, although
// they are resolved.
func TestCamposEditaveisExcluiRicos(t *testing.T) {
	editaveis := CamposEditaveis()
	if _, ok := editaveis["tagline"]; ok {
		t.Error("tagline não deveria ser editável (soLeitura)")
	}
	if _, ok := editaveis["keywords"]; ok {
		t.Error("keywords não deveria ser editável (soLeitura)")
	}
	if _, ok := editaveis["airing_status"]; ok {
		t.Error("airing_status não deveria ser editável (soLeitura)")
	}
	// Sanity: the usual editorial ones are still there.
	if _, ok := editaveis["title"]; !ok {
		t.Error("title deveria seguir editável")
	}
}
