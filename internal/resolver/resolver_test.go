package resolver

import (
	"database/sql"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// eq/eqPtr mantêm os testes lineares (baixa complexidade ciclomática).
func eq[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, quero %v", name, got, want)
	}
}

func eqPtr[T comparable](t *testing.T, name string, got *T, want T) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, quero %v", name, want)
		return
	}
	eq(t, name, *got, want)
}

// TestResolverCamadaArquivoCompleta cobre a composição da camada arquivo/núcleo:
// campos diretos, tags/external_ids parseados, posse derivada e poster.
func TestResolverCamadaArquivoCompleta(t *testing.T) {
	poster := "/api/v1/entities/x/assets/poster"
	got := Resolver(Camadas{
		Entity: sqlcgen.Entity{
			ID:              "x",
			Type:            "movie",
			Title:           "Matrix",
			OriginalTitle:   ns("The Matrix"),
			Description:     ns("Neo acorda."),
			ReleaseDate:     ns("1999-03-31"),
			Status:          "cataloged",
			TagsJson:        ns(`["ação","ficção"]`),
			ExternalIdsJson: ns(`{"tmdb":"603","imdb":"tt0133093"}`),
		},
		Files: []sqlcgen.MediaFile{
			{ID: "f1", State: "missing"},
			{ID: "f2", State: "present"},
		},
		PosterURL: &poster,
	})

	eq(t, "id", got.ID, "x")
	eq(t, "title", got.Title, "Matrix")
	eq(t, "status", got.Status, "cataloged")
	eqPtr(t, "original_title", got.OriginalTitle, "The Matrix")
	eq(t, "owned", got.Owned, true)
	eq(t, "file_count", got.FileCount, 2)
	eq(t, "len(tags)", len(got.Tags), 2)
	eq(t, "tags[0]", got.Tags[0], "ação")
	eq(t, "external_ids.tmdb", got.ExternalIDs["tmdb"], "603")
	eqPtr(t, "poster_url", got.PosterURL, poster)
}

// TestResolverVazioNaoNula garante nulos/omitidos coerentes: sem arquivos não há
// posse; tags/external_ids saem como coleções vazias (nunca nil); os *string
// ausentes ficam nil.
func TestResolverVazioNaoNula(t *testing.T) {
	got := Resolver(Camadas{
		Entity: sqlcgen.Entity{ID: "y", Type: "tv", Title: "Vazio", Status: "draft"},
	})
	eq(t, "owned", got.Owned, false)
	eq(t, "file_count", got.FileCount, 0)
	nilStr := (*string)(nil)
	if got.Tags == nil {
		t.Error("tags nil, quero fatia vazia não-nil")
	}
	eq(t, "len(tags)", len(got.Tags), 0)
	if got.ExternalIDs == nil {
		t.Error("external_ids nil, quero mapa vazio não-nil")
	}
	eq(t, "len(external_ids)", len(got.ExternalIDs), 0)
	eq(t, "original_title", got.OriginalTitle, nilStr)
	eq(t, "description", got.Description, nilStr)
	eq(t, "release_date", got.ReleaseDate, nilStr)
	eq(t, "poster_url", got.PosterURL, nilStr)
}

// TestResolverJSONInvalidoDegrada garante que tags/external_ids malformados não
// derrubam a composição (viram coleção vazia).
func TestResolverJSONInvalidoDegrada(t *testing.T) {
	got := Resolver(Camadas{
		Entity: sqlcgen.Entity{
			ID: "z", Type: "movie", Title: "Ruído",
			TagsJson:        ns("{não é array}"),
			ExternalIdsJson: ns("isto não é json"),
		},
	})
	if len(got.Tags) != 0 {
		t.Errorf("tags = %v, quero vazio ao degradar", got.Tags)
	}
	if len(got.ExternalIDs) != 0 {
		t.Errorf("external_ids = %v, quero vazio ao degradar", got.ExternalIDs)
	}
}
