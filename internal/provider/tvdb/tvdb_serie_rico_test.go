package tvdb

import (
	"encoding/json"
	"testing"
)

// serieExtendedComStatusGenres e um /series/{id}/extended com o status{name} e os
// genres[] que o 0045 passa a emitir no FALLBACK (usados quando nao ha match no TMDB).
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

// TestMapearSerieStatusGeneros (fallback do 0045): o MapearDetalhe da serie do TVDB
// emite airing_status (status.name CRU) e tags (generos ancorados no idioma original)
// a partir do /extended — hoje descartados. Tagline/keywords/similar o TVDB nao tem
// (ausencia graciosa: as chaves nem aparecem).
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
	// Ausencia graciosa: o TVDB nao da tagline/keywords/similar.
	exigirSemChaves(t, campos, "tagline", "keywords", "similar")
	// A identidade da arvore segue intacta (title do TVDB).
	if _, ok := campos["title"].(map[string]any); !ok {
		t.Errorf("title deveria seguir do TVDB, tive %T", campos["title"])
	}
}

// exigirSemChaves falha se o mapa carrega qualquer uma das chaves dadas.
func exigirSemChaves(t *testing.T, m map[string]any, chaves ...string) {
	t.Helper()
	for _, k := range chaves {
		if _, existe := m[k]; existe {
			t.Errorf("mapa nao deveria carregar %q", k)
		}
	}
}
