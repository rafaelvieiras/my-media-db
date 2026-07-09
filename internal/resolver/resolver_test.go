package resolver

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// ov monta um patch de override a partir de pares chave→JSON cru, para os testes
// injetarem overlays diretamente no motor.
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

// entArquivo é uma Entity de núcleo mínima reutilizada pelos testes de composição.
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

// TestExtensionDataDefaultVazio garante que extension_data sai como objeto vazio
// (nunca nil/null) quando não há dado — espelho de tags.
func TestExtensionDataDefaultVazio(t *testing.T) {
	got := Resolver(Camadas{Entity: sqlcgen.Entity{ID: "y", Type: "tv", Title: "Vazio"}})
	if got.ExtensionData == nil {
		t.Fatal("extension_data nil, quero mapa vazio não-nil")
	}
	eq(t, "len(extension_data)", len(got.ExtensionData), 0)
}

// TestExternoSobrepoeArquivo cobre §5: a camada externa sobrepõe o arquivo campo a
// campo respeitando a base declarada (todos os editoriais têm base externo).
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
	// Campo sem valor externo herda o arquivo (release_date ausente em ambos → nil).
	if got.ReleaseDate != nil {
		t.Errorf("release_date = %v, quero nil (nem externo nem arquivo)", *got.ReleaseDate)
	}
}

// TestOverrideVenceExternoEArquivo cobre §5: override sobrepõe as duas bases.
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

// TestOverrideNullApaga cobre §5/R6: chave presente com null apaga a base; chave
// ausente herda.
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
	// title não está no override → herda (arquivo, pois sem externo de title).
	eq(t, "title", got.Title, "Título do Arquivo")
}

// TestMergeListaAddRemove cobre §5/R3: merge {add, remove} sobre a base externa,
// com ordem definida, remoção por valor, adição ao fim e dedup.
func TestMergeListaAddRemove(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entArquivo(),
		Externo: []CamadaExterna{{
			Fonte:  "tmdb",
			Campos: map[string]any{"tags": []any{"ação", "drama", "b"}},
		}},
		Override: ov(map[string]string{"tags": `{"add":["novo","ação"],"remove":["drama"]}`}),
	})
	// Base externa [ação,drama,b]; remove drama; add [novo,ação]; dedup ação.
	want := []string{"ação", "b", "novo"}
	if len(got.Tags) != len(want) {
		t.Fatalf("tags = %v, quero %v", got.Tags, want)
	}
	for i := range want {
		eq(t, "tags["+want[i]+"]", got.Tags[i], want[i])
	}
}

// TestListaOverrideNullApaga cobre R6 em campo-lista: null zera a lista.
func TestListaOverrideNullApaga(t *testing.T) {
	got := Resolver(Camadas{
		Entity:   entArquivo(),
		Override: ov(map[string]string{"tags": `null`}),
	})
	if len(got.Tags) != 0 {
		t.Errorf("tags = %v, quero vazio (apagado por null)", got.Tags)
	}
}

// TestListaOverrideSubstituicaoTotal cobre o atalho de array puro (substituição).
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

// TestPrioridadeEntreFontes cobre §5/R2: com duas fontes, vence a primeira da lista
// de prioridade que tiver valor.
func TestPrioridadeEntreFontes(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entArquivo(),
		Externo: []CamadaExterna{
			{Fonte: "tmdb", Campos: map[string]any{"title": "Alta Prioridade"}},
			{Fonte: "outra", Campos: map[string]any{"title": "Baixa Prioridade", "description": "Só a segunda tem."}},
		},
	})
	eq(t, "title", got.Title, "Alta Prioridade")
	// description só existe na segunda fonte → é a base externa dela.
	eqPtr(t, "description", got.Description, "Só a segunda tem.")
}

// TestExtensionDataComposicao cobre §5/R5: extension_data compõe por namespace e
// chave com a mesma precedência override ⟵ externo ⟵ arquivo.
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
	// artist: externo vence arquivo.
	eq(t, "mv.artist", mv["artist"], any("Externo"))
	// bpm: override (128) vence.
	if bpm, _ := mv["bpm"].(float64); bpm != 128 {
		t.Errorf("mv.bpm = %v, quero 128 (override)", mv["bpm"])
	}
	// label: override null apaga a chave do externo.
	if _, existe := mv["label"]; existe {
		t.Errorf("mv.label ainda presente, quero apagado por null: %v", mv["label"])
	}
	// namespace 'filme' herda do arquivo intacto.
	filme, _ := got.ExtensionData["filme"].(map[string]any)
	if filme == nil || filme["a"] == nil {
		t.Errorf("namespace filme perdido: %#v", got.ExtensionData)
	}
	// namespace novo do override entra inteiro.
	novo, _ := got.ExtensionData["novo"].(map[string]any)
	if novo == nil || novo["k"] != "v" {
		t.Errorf("namespace novo ausente: %#v", got.ExtensionData)
	}
}

// TestRegressaoZeroSoArquivo cobre §5: sem externo/override, a saída é a do 0010.
func TestRegressaoZeroSoArquivo(t *testing.T) {
	got := Resolver(Camadas{Entity: entArquivo()})
	eq(t, "title", got.Title, "Título do Arquivo")
	eqPtr(t, "description", got.Description, "Descrição do arquivo.")
	eq(t, "len(tags)", len(got.Tags), 2)
	eq(t, "tags[0]", got.Tags[0], "arquivo-a")
	eq(t, "len(extension_data)", len(got.ExtensionData), 0)
}
