package server

import (
	"testing"
)

// members_still_test covers the episode STILL: the raw URL of the 16:9 frame lives in
// the extension_data core.tv@1 (still_url) and comes out as `still_url` in the episode
// card (via /members and /episodes) and in the detail (ResolvedEntity) -- ONLY for an
// episode WITH a still. Absent for an episode without a still and for a non-episode.
// Served by the image proxy in the front end.

const nsStillE5 = `{"core.tv@1":{"numbering":{"official":{"season":1,"number":5}},` +
	`"still_url":"https://artworks.thetvdb.com/banners/episodes/72514/5.jpg"}}`

// TestStillNoCardDeEpisodio: /members brings still_url in the card of the episode WITH a
// still and nil in the one of the episode WITHOUT a still -- same annotation (one read)
// as the numbering.
func TestStillNoCardDeEpisodio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	semearEntity(t, d, "e5", "episode", "Ep com still")
	semearExtension(t, d, "e5", nsStillE5)
	semearEntity(t, d, "e6", "episode", "Ep sem still")
	semearExtension(t, d, "e6", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":6}}}}`)
	ligarFilhoOrdenado(t, d, "re5", "se1", "e5", "tv_episode", 5)
	ligarFilhoOrdenado(t, d, "re6", "se1", "e6", "tv_episode", 6)

	byID := map[string]entityCardDTO{}
	for _, c := range membrosDe(t, h, "se1") {
		byID[c.ID] = c
	}
	if byID["e5"].StillURL == nil {
		t.Fatalf("e5 sem still_url; quero a URL crua do provider")
	}
	if *byID["e5"].StillURL != "https://artworks.thetvdb.com/banners/episodes/72514/5.jpg" {
		t.Errorf("e5 still_url = %q, quero a URL crua do TheTVDB", *byID["e5"].StillURL)
	}
	if byID["e6"].StillURL != nil {
		t.Errorf("e6 (sem still) still_url = %v; quero nil", *byID["e6"].StillURL)
	}
}

// TestStillAusenteParaNaoEpisodio: SEASON cards (members of the series) never carry
// still_url -- only the episode card gets the annotation.
func TestStillAusenteParaNaoEpisodio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)

	for _, c := range membrosDe(t, h, "s1") {
		if c.StillURL != nil {
			t.Errorf("card %s (%s) com still_url %q; quero nil (nao-episodio)", c.ID, c.Type, *c.StillURL)
		}
	}
}

// TestStillNoEndpointEpisodes: the grouped episodes endpoint also exposes the
// still_url in the card -- the same annotation reused by the namespace read.
func TestStillNoEndpointEpisodes(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearExtension(t, d, "s1", `{"core.tv@1":{"default_season_type":"official"}}`)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	semearEntity(t, d, "e5", "episode", "Ep com still")
	semearExtension(t, d, "e5", nsStillE5)
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "re5", "se1", "e5", "tv_episode", 5)

	grupos := episodesDe(t, h, "s1", "official")
	var achou *string
	for _, g := range grupos.Groups {
		for _, c := range g.Episodes {
			if c.ID == "e5" {
				achou = c.StillURL
			}
		}
	}
	if achou == nil {
		t.Fatalf("e5 sem still_url no endpoint /episodes; quero a URL crua")
	}
}

// TestStillNoDetalheDoEpisodio: the detail (ResolvedEntity) of the episode WITH a still
// brings still_url; a non-episode (season) stays without still_url (nil).
func TestStillNoDetalheDoEpisodio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e5", "episode", "Ep com still")
	semearExtension(t, d, "e5", nsStillE5)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)

	det := detalheDe(t, h, "e5")
	if det.StillURL == nil {
		t.Fatalf("detalhe do episodio sem still_url; quero a URL crua")
	}
	if *det.StillURL != "https://artworks.thetvdb.com/banners/episodes/72514/5.jpg" {
		t.Errorf("detalhe still_url = %q, quero a URL crua do TheTVDB", *det.StillURL)
	}
	if temp := detalheDe(t, h, "se1"); temp.StillURL != nil {
		t.Errorf("detalhe de temporada com still_url %q; quero nil (nao-episodio)", *temp.StillURL)
	}
}
