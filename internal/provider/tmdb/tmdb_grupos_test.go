package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// listaGruposTMDB is /tv/700/episode_groups: three groups, listed out of priority
// order on purpose (the DVD one comes last in the payload).
const listaGruposTMDB = `{
  "results": [
    {"id": "g-story", "name": "Story Arc", "type": 5, "group_count": 1, "episode_count": 3},
    {"id": "g-dvd", "name": "Blu-ray Order", "type": 3, "group_count": 2, "episode_count": 3},
    {"id": "g-digital", "name": "Netflix", "type": 4, "group_count": 1, "episode_count": 2}
  ]
}`

// grupoDVBTMDB is /tv/episode_group/g-dvd: two sub-groups (volumes). The episodes are
// REORDERED — the official S01E02 comes first in the volume — and the `order` of the
// items is 0-based and restarts in each sub-group.
const grupoDVBTMDB = `{
  "id": "g-dvd",
  "name": "Blu-ray Order",
  "type": 3,
  "group_count": 2,
  "episode_count": 3,
  "groups": [
    {"id": "sg1", "name": "Volume 1", "order": 1, "episodes": [
      {"id": 2002, "order": 0, "season_number": 1, "episode_number": 2, "name": "B"},
      {"id": 2001, "order": 1, "season_number": 1, "episode_number": 1, "name": "A"}
    ]},
    {"id": "sg2", "name": "Volume 2", "order": 2, "episodes": [
      {"id": 2003, "order": 0, "season_number": 1, "episode_number": 3, "name": "C"}
    ]}
  ]
}`

// grupoDigitalTMDB is /tv/episode_group/g-digital: one sub-group, items in the
// official order (the reorder is the seasonal grouping, not the sequence).
const grupoDigitalTMDB = `{
  "id": "g-digital",
  "name": "Netflix",
  "type": 4,
  "group_count": 1,
  "episode_count": 2,
  "groups": [
    {"id": "sg1", "name": "Season 1", "order": 1, "episodes": [
      {"id": 2001, "order": 0, "season_number": 1, "episode_number": 1, "name": "A"},
      {"id": 2002, "order": 1, "season_number": 1, "episode_number": 2, "name": "B"}
    ]}
  ]
}`

// grupoFantasmaTMDB is a group whose episodes are NOT in the tree (a season TMDB
// removed): it must not create an order out of nothing.
const grupoFantasmaTMDB = `{
  "id": "g-ghost",
  "name": "Ghost Order",
  "type": 3,
  "group_count": 1,
  "episode_count": 1,
  "groups": [
    {"id": "sg1", "name": "Season 9", "order": 1, "episodes": [
      {"id": 9999, "order": 0, "season_number": 9, "episode_number": 1, "name": "Z"}
    ]}
  ]
}`

// servidorComGrupos answers the tree of series 700 (one season, three episodes) plus
// the episode groups. `falha` makes one group endpoint fail, to exercise the degrade.
func servidorComGrupos(t *testing.T, falha string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tv/700":
			_, _ = w.Write([]byte(`{"original_language":"en","seasons":[{"season_number":1,"name":"Season 1"}]}`))
		case "/tv/700/season/1":
			_, _ = w.Write([]byte(`{"name":"Season 1","episodes":[
				{"id":2001,"season_number":1,"episode_number":1,"name":"A"},
				{"id":2002,"season_number":1,"episode_number":2,"name":"B"},
				{"id":2003,"season_number":1,"episode_number":3,"name":"C"}]}`))
		case "/tv/700/episode_groups":
			_, _ = w.Write([]byte(listaGruposTMDB))
		case "/tv/episode_group/g-dvd":
			if falha == "g-dvd" {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(grupoDVBTMDB))
		case "/tv/episode_group/g-digital":
			_, _ = w.Write([]byte(grupoDigitalTMDB))
		case "/tv/episode_group/g-story":
			_, _ = w.Write([]byte(grupoFantasmaTMDB))
		default:
			http.Error(w, "path inesperado: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// epPorID finds the external episode by its tmdb id.
func epPorID(t *testing.T, a provider.SerieArvore, id string) provider.EpisodioExterno {
	t.Helper()
	for _, temp := range a.Temporadas {
		for _, ep := range temp.Episodios {
			if ep.SourceID == id {
				return ep
			}
		}
	}
	t.Fatalf("episodio %s ausente da arvore", id)
	return provider.EpisodioExterno{}
}

// TestArvoreComEpisodeGroups: the groups of the series become ALTERNATIVE orderings in
// the numbering, numbered by (sub-group order, position+1), while the official order
// stays untouched and the default stays official.
func TestArvoreComEpisodeGroups(t *testing.T) {
	srv := servidorComGrupos(t, "")
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	arvore, err := c.BuscarSerieArvore(context.Background(), "700", []string{"en"})
	if err != nil {
		t.Fatalf("BuscarSerieArvore: %v", err)
	}
	if arvore.DefaultSeasonType != "official" {
		t.Errorf("default = %q, quero official (os grupos sao alternativos)", arvore.DefaultSeasonType)
	}
	// DVD first (type 3), then digital (type 4, 2 eps) — and NOT the ghost group.
	if len(arvore.SeasonTypes) != 3 || arvore.SeasonTypes[0] != "official" {
		t.Fatalf("ordens = %v, quero official + 2 grupos", arvore.SeasonTypes)
	}
	if arvore.SeasonTypes[1] != "blu-ray-order" || arvore.SeasonTypes[2] != "netflix" {
		t.Errorf("ordens = %v, quero [official blu-ray-order netflix]", arvore.SeasonTypes)
	}

	// Official numbering intact, keys from the group names.
	ep2 := epPorID(t, arvore, "2002")
	if n := ep2.Numbering["official"]; n.Season != 1 || n.Number != 2 {
		t.Errorf("official de 2002 = %v, quero {1,2}", n)
	}
	// 2002 is the FIRST item of volume 1 in the DVD order.
	if n := ep2.Numbering["blu-ray-order"]; n.Season != 1 || n.Number != 1 {
		t.Errorf("dvd de 2002 = %v, quero {1,1} (primeiro do volume 1)", n)
	}
	// 2001 is the second item of volume 1.
	if n := epPorID(t, arvore, "2001").Numbering["blu-ray-order"]; n.Season != 1 || n.Number != 2 {
		t.Errorf("dvd de 2001 = %v, quero {1,2}", n)
	}
	// 2003 opens volume 2 (season 2 of the DVD order).
	if n := epPorID(t, arvore, "2003").Numbering["blu-ray-order"]; n.Season != 2 || n.Number != 1 {
		t.Errorf("dvd de 2003 = %v, quero {2,1}", n)
	}
	// The digital group keeps the sequence and puts everything in season 1.
	if n := epPorID(t, arvore, "2002").Numbering["netflix"]; n.Season != 1 || n.Number != 2 {
		t.Errorf("netflix de 2002 = %v, quero {1,2}", n)
	}
	// Neither an order from a group with no episode of the tree...
	for _, ordem := range arvore.SeasonTypes {
		if ordem == "ghost-order" {
			t.Error("grupo sem episodio na arvore nao deveria virar ordem")
		}
	}
}

// TestArvoreGrupoQuebradoDegrada: a group that fails (500) does not cost the other
// orders nor the tree.
func TestArvoreGrupoQuebradoDegrada(t *testing.T) {
	srv := servidorComGrupos(t, "g-dvd")
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	arvore, err := c.BuscarSerieArvore(context.Background(), "700", []string{"en"})
	if err != nil {
		t.Fatalf("BuscarSerieArvore: %v", err)
	}
	if len(arvore.SeasonTypes) != 2 || arvore.SeasonTypes[1] != "netflix" {
		t.Errorf("ordens = %v, quero [official netflix] (o DVD falhou)", arvore.SeasonTypes)
	}
	if _, ok := epPorID(t, arvore, "2002").Numbering["blu-ray-order"]; ok {
		t.Error("ordem do grupo que falhou nao deveria existir")
	}
}

// TestArvoreSemGrupos: a series without groups (404 in the list) keeps exactly the
// official order — the pre-existing behaviour is preserved.
func TestArvoreSemGrupos(t *testing.T) {
	srv := servidorArvoreTMDB(t) // the old fixture: no episode_groups endpoint at all
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	arvore, err := c.BuscarSerieArvore(context.Background(), "456", []string{"en"})
	if err != nil {
		t.Fatalf("BuscarSerieArvore: %v", err)
	}
	if len(arvore.SeasonTypes) != 1 || arvore.SeasonTypes[0] != "official" {
		t.Errorf("ordens = %v, quero [official]", arvore.SeasonTypes)
	}
}

// TestChaveDeOrdem: the key comes from the name (readable in the UI), degrades to the
// type when the name is empty and never collides with an order already in use.
func TestChaveDeOrdem(t *testing.T) {
	casos := []struct {
		nome   string
		tipo   int
		usados map[string]bool
		quero  string
	}{
		{"Blu-ray Order", 3, map[string]bool{}, "blu-ray-order"},
		{"Temporada Estendida (PT)", 1, map[string]bool{}, "temporada-estendida-pt"},
		{"Ítens Especiais", 7, map[string]bool{}, "itens-especiais"},
		{"", 4, map[string]bool{}, "group-4"},
		{"DVD", 3, map[string]bool{"dvd": true}, "dvd-2"},
		{"DVD", 3, map[string]bool{"dvd": true, "dvd-2": true}, "dvd-3"},
	}
	for _, caso := range casos {
		got := chaveDeOrdem(grupoResumoTMDB{Name: caso.nome, Type: caso.tipo}, caso.usados)
		if got != caso.quero {
			t.Errorf("chaveDeOrdem(%q) = %q, quero %q", caso.nome, got, caso.quero)
		}
	}
}

// TestOrdenarGruposRespeitaPreferencia: the cap keeps the most useful types first
// (DVD before digital before story arc), and the biggest group wins inside a type.
func TestOrdenarGruposRespeitaPreferencia(t *testing.T) {
	entrada := []grupoResumoTMDB{
		{ID: "a", Name: "Story", Type: 5, EpisodeCount: 99},
		{ID: "b", Name: "Igual", Type: 3, EpisodeCount: 10},
		{ID: "c", Name: "Digital", Type: 4, EpisodeCount: 50},
		{ID: "d", Name: "DVD grande", Type: 3, EpisodeCount: 200},
	}
	got := ordenarGrupos(entrada)
	ordem := []string{got[0].ID, got[1].ID, got[2].ID, got[3].ID}
	quero := []string{"d", "b", "c", "a"}
	if strings.Join(ordem, ",") != strings.Join(quero, ",") {
		t.Errorf("ordem = %v, quero %v", ordem, quero)
	}
}
