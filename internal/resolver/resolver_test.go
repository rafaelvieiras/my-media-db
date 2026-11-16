package resolver

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// ov builds an override patch from key→raw JSON pairs, so the tests can inject
// overlays straight into the engine.
func ov(pares map[string]string) map[string]json.RawMessage {
	if pares == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(pares))
	for k, v := range pares {
		out[k] = json.RawMessage(v)
	}
	return out
}

func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// eq/eqPtr keep the tests linear (low cyclomatic complexity).
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

// TestResolverCamadaArquivoCompleta covers the composition of the file/core layer:
// direct fields, parsed tags/external_ids, derived ownership and poster.
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

// TestResolverVazioNaoNula guarantees coherent nulls/omissions: with no files there
// is no ownership; tags/external_ids come out as empty collections (never nil); the
// absent *string stay nil.
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

// TestResolverJSONInvalidoDegrada guarantees that malformed tags/external_ids do not
// bring the composition down (they become an empty collection).
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

// entArquivo is a minimal core Entity reused by the composition tests.
func entArquivo() sqlcgen.Entity {
	return sqlcgen.Entity{
		ID:          "x",
		Type:        "movie",
		Title:       "Título do Arquivo",
		Description: ns("Descrição do arquivo."),
		Status:      "cataloged",
		TagsJson:    ns(`["arquivo-a","arquivo-b"]`),
	}
}

// TestExtensionDataDefaultVazio guarantees that extension_data comes out as an empty
// object (never nil/null) when there is no data — the mirror of tags.
func TestExtensionDataDefaultVazio(t *testing.T) {
	got := Resolver(Camadas{Entity: sqlcgen.Entity{ID: "y", Type: "tv", Title: "Vazio"}})
	if got.ExtensionData == nil {
		t.Fatal("extension_data nil, quero mapa vazio não-nil")
	}
	eq(t, "len(extension_data)", len(got.ExtensionData), 0)
}

// TestExternoSobrepoeArquivo covers §5: the external layer overrides the file field
// by field respecting the declared base (all editorial fields have an external
// base).
func TestExternoSobrepoeArquivo(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entArquivo(),
		Externo: []CamadaExterna{{
			Fonte: "tmdb",
			Campos: map[string]any{
				"title":       "Título Externo",
				"description": "Sinopse do TMDB.",
			},
		}},
	})
	eq(t, "title", got.Title, "Título Externo")
	eqPtr(t, "description", got.Description, "Sinopse do TMDB.")
	// A field with no external value inherits the file (release_date absent in both →
	// nil).
	if got.ReleaseDate != nil {
		t.Errorf("release_date = %v, quero nil (nem externo nem arquivo)", *got.ReleaseDate)
	}
}

// TestOverrideVenceExternoEArquivo covers §5: the override overrides both bases.
func TestOverrideVenceExternoEArquivo(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entArquivo(),
		Externo: []CamadaExterna{{
			Fonte:  "tmdb",
			Campos: map[string]any{"title": "Título Externo"},
		}},
		Override: ov(map[string]string{"title": `"Título do Dono"`}),
	})
	eq(t, "title", got.Title, "Título do Dono")
}

// TestOverrideNullApaga covers §5/R6: a present key with null erases the base; an
// absent key inherits.
func TestOverrideNullApaga(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entArquivo(),
		Externo: []CamadaExterna{{
			Fonte:  "tmdb",
			Campos: map[string]any{"description": "Sinopse do TMDB."},
		}},
		Override: ov(map[string]string{"description": `null`}),
	})
	if got.Description != nil {
		t.Errorf("description = %v, quero nil (apagado por override null)", *got.Description)
	}
	// title is not in the override → inherits (file, since there is no external for
	// title).
	eq(t, "title", got.Title, "Título do Arquivo")
}

// TestMergeListaAddRemove covers §5/R3: {add, remove} merge over the external base,
// with a defined order, removal by value, addition at the end and dedup.
func TestMergeListaAddRemove(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entArquivo(),
		Externo: []CamadaExterna{{
			Fonte:  "tmdb",
			Campos: map[string]any{"tags": []any{"ação", "drama", "b"}},
		}},
		Override: ov(map[string]string{"tags": `{"add":["novo","ação"],"remove":["drama"]}`}),
	})
	// External base [ação,drama,b]; remove drama; add [novo,ação]; dedup ação.
	want := []string{"ação", "b", "novo"}
	if len(got.Tags) != len(want) {
		t.Fatalf("tags = %v, quero %v", got.Tags, want)
	}
	for i := range want {
		eq(t, "tags["+want[i]+"]", got.Tags[i], want[i])
	}
}

// TestListaOverrideNullApaga covers R6 on a list field: null zeroes the list.
func TestListaOverrideNullApaga(t *testing.T) {
	got := Resolver(Camadas{
		Entity:   entArquivo(),
		Override: ov(map[string]string{"tags": `null`}),
	})
	if len(got.Tags) != 0 {
		t.Errorf("tags = %v, quero vazio (apagado por null)", got.Tags)
	}
}

// TestListaOverrideSubstituicaoTotal covers the plain-array shortcut (replacement).
func TestListaOverrideSubstituicaoTotal(t *testing.T) {
	got := Resolver(Camadas{
		Entity:   entArquivo(),
		Override: ov(map[string]string{"tags": `["só","isto","isto"]`}),
	})
	want := []string{"só", "isto"} // dedup
	if len(got.Tags) != len(want) || got.Tags[0] != "só" || got.Tags[1] != "isto" {
		t.Errorf("tags = %v, quero %v", got.Tags, want)
	}
}

// TestPrioridadeEntreFontes covers §5/R2: with two sources, the first one in the
// priority list that has a value wins.
func TestPrioridadeEntreFontes(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entArquivo(),
		Externo: []CamadaExterna{
			{Fonte: "tmdb", Campos: map[string]any{"title": "Alta Prioridade"}},
			{Fonte: "outra", Campos: map[string]any{"title": "Baixa Prioridade", "description": "Só a segunda tem."}},
		},
	})
	eq(t, "title", got.Title, "Alta Prioridade")
	// description only exists in the second source → it is its external base.
	eqPtr(t, "description", got.Description, "Só a segunda tem.")
}

// TestExtensionDataComposicao covers §5/R5: extension_data composes by namespace and
// key with the same override ⟵ externo ⟵ arquivo precedence.
func TestExtensionDataComposicao(t *testing.T) {
	ent := entArquivo()
	ent.ExtensionDataJson = ns(`{"music_video":{"bpm":120,"artist":"Arquivo"},"filme":{"a":1}}`)
	got := Resolver(Camadas{
		Entity: ent,
		Externo: []CamadaExterna{{
			Fonte: "tmdb",
			Campos: map[string]any{"extension_data": map[string]any{
				"music_video": map[string]any{"artist": "Externo", "label": "Gravadora"},
			}},
		}},
		Override: ov(map[string]string{
			"extension_data": `{"music_video":{"bpm":128,"label":null},"novo":{"k":"v"}}`,
		}),
	})
	mv, ok := got.ExtensionData["music_video"].(map[string]any)
	if !ok {
		t.Fatalf("music_video ausente/errado: %#v", got.ExtensionData)
	}
	// artist: the external wins over the file.
	eq(t, "mv.artist", mv["artist"], any("Externo"))
	// bpm: the override (128) wins.
	if bpm, _ := mv["bpm"].(float64); bpm != 128 {
		t.Errorf("mv.bpm = %v, quero 128 (override)", mv["bpm"])
	}
	// label: the null override erases the external's key.
	if _, existe := mv["label"]; existe {
		t.Errorf("mv.label ainda presente, quero apagado por null: %v", mv["label"])
	}
	// the 'filme' namespace inherits from the file intact.
	filme, _ := got.ExtensionData["filme"].(map[string]any)
	if filme == nil || filme["a"] == nil {
		t.Errorf("namespace filme perdido: %#v", got.ExtensionData)
	}
	// a new namespace from the override enters whole.
	novo, _ := got.ExtensionData["novo"].(map[string]any)
	if novo == nil || novo["k"] != "v" {
		t.Errorf("namespace novo ausente: %#v", got.ExtensionData)
	}
}

// TestRegressaoZeroSoArquivo covers §5: with no external/override, the output is the
// pre-enrichment one.
func TestRegressaoZeroSoArquivo(t *testing.T) {
	got := Resolver(Camadas{Entity: entArquivo()})
	eq(t, "title", got.Title, "Título do Arquivo")
	eqPtr(t, "description", got.Description, "Descrição do arquivo.")
	eq(t, "len(tags)", len(got.Tags), 2)
	eq(t, "tags[0]", got.Tags[0], "arquivo-a")
	eq(t, "len(extension_data)", len(got.ExtensionData), 0)
}
