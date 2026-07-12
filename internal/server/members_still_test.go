package server

import (
	"testing"
)

// members_still_test cobre o STILL do episodio (0042): a URL crua do frame 16:9 vive no
// extension_data core.tv@1 (still_url) e sai como `still_url` no card de episodio (via
// /members e /episodes) e no detalhe (ResolvedEntity) -- SO para episodio COM still.
// Ausente para episodio sem still e para nao-episodio. Servido pelo image proxy no front.

const nsStillE5 = `{"core.tv@1":{"numbering":{"official":{"season":1,"number":5}},` +
	`"still_url":"https://artworks.thetvdb.com/banners/episodes/72514/5.jpg"}}`

// TestStillNoCardDeEpisodio: /members traz still_url no card do episodio COM still e nil
// no do episodio SEM still -- mesma anotacao (uma leitura) do numbering.
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

// TestStillAusenteParaNaoEpisodio: cards de TEMPORADA (membros da serie) nunca carregam
// still_url -- so o card de episodio recebe a anotacao.
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

// TestStillNoEndpointEpisodes (0040): o endpoint de episodios agrupados tambem expoe o
// still_url no card -- a mesma anotacao reusada pela leitura do namespace.
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

// TestStillNoDetalheDoEpisodio: o detalhe (ResolvedEntity) do episodio COM still traz
// still_url; um nao-episodio (temporada) fica sem still_url (nil).
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
