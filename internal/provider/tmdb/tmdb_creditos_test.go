package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuscarDetalheAppendCreditos guarantees that append_to_response requests the credit
// blocks (credits of the movie + aggregate_credits of the series) in a single round trip.
func TestBuscarDetalheAppendCreditos(t *testing.T) {
	var req http.Request
	srv := servidorRico(t, &req)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)
	if _, err := c.BuscarDetalhe(context.Background(), "603", "movie", []string{"pt-BR"}); err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	ap := req.URL.Query().Get("append_to_response")
	for _, bloco := range []string{"credits", "aggregate_credits"} {
		if !strings.Contains(ap, bloco) {
			t.Errorf("append_to_response %q nao contem %q", ap, bloco)
		}
	}
}

// TestMapearCreditosFilme covers the MOVIE canonical via BuscarDetalhe+MapearDetalhe: the
// cast comes sorted by billing, the crew with department/job and the production companies.
func TestMapearCreditosFilme(t *testing.T) {
	srv := servidorCreditosFilme(t)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)
	raw, err := c.BuscarDetalhe(context.Background(), "603", "movie", []string{"en"})
	if err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	campos, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}

	cast := listaDeMapaTest(t, campos, "cast")
	if len(cast) != 2 {
		t.Fatalf("cast tem %d itens, quero 2", len(cast))
	}
	// Sorted by `order`: Keanu (0) before Laurence (1), despite the inverted raw order.
	eqStr(t, cast[0], "name", "Keanu Reeves")
	eqStr(t, cast[0], "character", "Neo")
	eqStr(t, cast[0], "source_id", "6384")
	eqStr(t, cast[0], "profile_path", "/keanu.jpg")
	eqStr(t, cast[1], "name", "Laurence Fishburne")

	crew := listaDeMapaTest(t, campos, "crew")
	if len(crew) != 1 {
		t.Fatalf("crew tem %d itens, quero 1", len(crew))
	}
	eqStr(t, crew[0], "name", "Lana Wachowski")
	eqStr(t, crew[0], "department", "Directing")
	eqStr(t, crew[0], "job", "Director")

	comps := listaDeMapaTest(t, campos, "companies")
	if len(comps) != 1 {
		t.Fatalf("companies tem %d itens, quero 1", len(comps))
	}
	eqStr(t, comps[0], "name", "Warner Bros.")
	eqStr(t, comps[0], "logo_path", "/wb.png")
}

// TestMapearCreditosSerieNormalizaRoles covers the AGGREGATED canonical of a series:
// multiple `roles` of an actor become ONE `character` joined by " / " with
// total_episode_count; and multiple `jobs` of a crew member choose the one with the
// highest episode_count.
func TestMapearCreditosSerieNormalizaRoles(t *testing.T) {
	d := detalheTMDB{AggregateCredits: aggregateCreditsTMDB{
		Cast: []aggCastTMDB{{
			ID: 1, Name: "Seth MacFarlane", Order: 0, TotalEpisodeCount: 400,
			Roles: []papelTMDB{
				{Character: "Peter", EpisodeCount: 400},
				{Character: "Brian", EpisodeCount: 390},
				{Character: "Stewie (voz)", EpisodeCount: 380},
			},
		}},
		Crew: []aggCrewTMDB{{
			ID: 2, Name: "Alex Borstein", Department: "Writing",
			Jobs: []jobTMDB{
				{Job: "Staff Writer", EpisodeCount: 10},
				{Job: "Executive Producer", EpisodeCount: 200},
			},
		}},
	}}
	out := map[string]any{}
	enriquecerCreditos(out, d)

	cast := out["cast"].([]any)
	if len(cast) != 1 {
		t.Fatalf("cast tem %d, quero 1", len(cast))
	}
	m := cast[0].(map[string]any)
	if got := m["character"].(string); got != "Peter / Brian / Stewie (voz)" {
		t.Errorf("character = %q, quero 'Peter / Brian / Stewie (voz)'", got)
	}
	if got := m["episode_count"].(int); got != 400 {
		t.Errorf("episode_count = %d, quero 400 (total_episode_count)", got)
	}

	crew := out["crew"].([]any)[0].(map[string]any)
	if got := crew["job"].(string); got != "Executive Producer" {
		t.Errorf("job = %q, quero 'Executive Producer' (maior episode_count)", got)
	}
	if got := crew["episode_count"].(int); got != 200 {
		t.Errorf("episode_count da equipe = %d, quero 200", got)
	}
}

// TestElencoTop50 proves the ceiling: a cast of 60 is cut at the first 50 BY billing
// (order), regardless of the raw order.
func TestElencoTop50(t *testing.T) {
	cast := make([]castMembroTMDB, 0, 60)
	// Inserts in DECREASING `order` (59..0) to force the sorting to act.
	for i := 59; i >= 0; i-- {
		cast = append(cast, castMembroTMDB{ID: int64(i), Name: "Ator", Order: i})
	}
	d := detalheTMDB{Credits: creditsTMDB{Cast: cast}}
	out := map[string]any{}
	enriquecerCreditos(out, d)

	lista := out["cast"].([]any)
	if len(lista) != 50 {
		t.Fatalf("cast cortado tem %d, quero 50", len(lista))
	}
	// The first one must be order=0 (lowest billing) and the last one order=49.
	if o := lista[0].(map[string]any)["order"].(int); o != 0 {
		t.Errorf("primeiro order = %d, quero 0", o)
	}
	if o := lista[49].(map[string]any)["order"].(int); o != 49 {
		t.Errorf("ultimo order = %d, quero 49 (top 50 por bilhagem)", o)
	}
}

// TestCreditosAusentesNaoGravam guarantees that without cast/crew/companies the keys do
// not appear in the canonical (the consumer distinguishes absent from empty).
func TestCreditosAusentesNaoGravam(t *testing.T) {
	out := map[string]any{}
	enriquecerCreditos(out, detalheTMDB{})
	for _, k := range []string{"cast", "crew", "companies"} {
		if _, existe := out[k]; existe {
			t.Errorf("chave %q nao deveria existir sem dados", k)
		}
	}
}

// --- payload and test helpers ---

const detalheCreditosFilme = `{
  "id": 603,
  "original_language": "en",
  "title": "The Matrix",
  "credits": {
    "cast": [
      {"id": 2975, "name": "Laurence Fishburne", "character": "Morpheus", "order": 1, "profile_path": "/lf.jpg"},
      {"id": 6384, "name": "Keanu Reeves", "character": "Neo", "order": 0, "profile_path": "/keanu.jpg"},
      {"id": 999, "name": "", "character": "Extra", "order": 2}
    ],
    "crew": [
      {"id": 9339, "name": "Lana Wachowski", "department": "Directing", "job": "Director", "profile_path": "/lw.jpg"}
    ]
  },
  "production_companies": [
    {"id": 174, "name": "Warner Bros.", "logo_path": "/wb.png"},
    {"id": 0, "name": ""}
  ]
}`

// servidorCreditosFilme answers /movie/603 with the movie credits payload.
func servidorCreditosFilme(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/movie/603" {
			_, _ = w.Write([]byte(detalheCreditosFilme))
			return
		}
		http.Error(w, "path inesperado", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// listaDeMapaTest extracts campos[key] as []map[string]any (test helper).
func listaDeMapaTest(t *testing.T, campos map[string]any, chave string) []map[string]any {
	t.Helper()
	lista, ok := campos[chave].([]any)
	if !ok {
		t.Fatalf("%s nao e []any: %T", chave, campos[chave])
	}
	out := make([]map[string]any, 0, len(lista))
	for _, it := range lista {
		m, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("item de %s nao e map: %T", chave, it)
		}
		out = append(out, m)
	}
	return out
}

// eqStr checks m[key] == want (string) in a dynamic canonical map.
func eqStr(t *testing.T, m map[string]any, chave, want string) {
	t.Helper()
	if got, _ := m[chave].(string); got != want {
		t.Errorf("[%s] = %q, quero %q", chave, got, want)
	}
}
