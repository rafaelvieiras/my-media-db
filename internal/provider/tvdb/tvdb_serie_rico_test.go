package tvdb

import (
	"encoding/json"
	"testing"
)

// serieExtendedComStatusGenres is a /series/{id}/extended with the status{name} and the
// genres[] that are now emitted in the FALLBACK (used when there is no match in TMDB).
const serieExtendedComStatusGenres = `{
  "status": "success",
  "data": {
    "id": 71663,
    "name": "The Simpsons",
    "originalLanguage": "eng",
    "firstAired": "1989-12-17",
    "image": "https://artworks.thetvdb.com/x.jpg",
    "status": {"id": 1, "name": "Continuing"},
    "genres": [{"id": 16, "name": "Animation"}, {"id": 35, "name": "Comedy"}]
  }
}`

// TestMapearSerieStatusGeneros (fallback): the MapearDetalhe of the TVDB series emits
// airing_status (RAW status.name) and tags (genres anchored in the original language) from
// /extended -- previously discarded. Tagline/keywords/similar do not exist in TVDB
// (graceful absence: the keys do not even appear).
func TestMapearSerieStatusGeneros(t *testing.T) {
	env := envelopeSerie{Detail: json.RawMessage(serieExtendedComStatusGenres)}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	c := &Client{}
	campos, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}

	if campos["airing_status"] != "Continuing" {
		t.Errorf("airing_status = %v, quero Continuing (cru)", campos["airing_status"])
	}
	tags, _ := campos["tags"].(map[string]any)
	nomes, _ := tags["en"].([]string)
	if len(nomes) != 2 || nomes[0] != "Animation" || nomes[1] != "Comedy" {
		t.Errorf("tags[en] = %v, quero [Animation Comedy] sob o idioma original", tags["en"])
	}
	// Graceful absence: TVDB does not provide tagline/keywords/similar.
	exigirSemChaves(t, campos, "tagline", "keywords", "similar")
	// The identity of the tree stays intact (title from TVDB).
	if _, ok := campos["title"].(map[string]any); !ok {
		t.Errorf("title deveria seguir do TVDB, tive %T", campos["title"])
	}
}

// exigirSemChaves fails if the map carries any of the given keys.
func exigirSemChaves(t *testing.T, m map[string]any, chaves ...string) {
	t.Helper()
	for _, k := range chaves {
		if _, existe := m[k]; existe {
			t.Errorf("mapa nao deveria carregar %q", k)
		}
	}
}
