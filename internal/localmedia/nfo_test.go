package localmedia

import (
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// strPtr/floatPtr sao helpers de teste para os ponteiros de resolver.Entidade.
func strPtr(s string) *string     { return &s }
func floatPtr(f float64) *float64 { return &f }

// fixtureEntidadeCompleta monta um resolver.Entidade "completo" (todos os
// campos do mapeamento do briefing 0062 SS4 preenchidos) para o teste de
// movie.nfo bater campo a campo.
func fixtureEntidadeCompleta() resolver.Entidade {
	return resolver.Entidade{
		ID:            "e1",
		Type:          "movie",
		Title:         "Matrix",
		OriginalTitle: strPtr("The Matrix"),
		Description:   strPtr("Um hacker descobre a verdade."),
		ReleaseDate:   strPtr("1999-03-31"),
		Tags:          []string{"Action", "Science Fiction"},
		ExternalIDs:   map[string]string{"tmdb": "603", "imdb": "tt0133093"},
		Tagline:       strPtr("Bem-vindo ao deserto do real."),
		Certification: strPtr("R"),
		VoteAverage:   floatPtr(8.2),
		PosterURL:     strPtr("/api/v1/entities/e1/assets/poster"),
		BackdropURL:   strPtr("https://image.tmdb.org/t/p/original/backdrop.jpg"),
		Cast: []richfields.Cast{
			{Name: "Keanu Reeves", Character: "Neo", Order: 0, ProfileURL: strPtr("https://image.tmdb.org/t/p/w185/keanu.jpg")},
			{Name: "Laurence Fishburne", Character: "Morpheus", Order: 1},
		},
		Crew: []richfields.Crew{
			{Name: "Lana Wachowski", Department: "Directing", Job: "Director"},
			{Name: "Lilly Wachowski", Department: "Directing", Job: "Director"},
			{Name: "Lana Wachowski", Department: "Writing", Job: "Writer"},
			{Name: "Joel Silver", Department: "Production", Job: "Producer"}, // nao entra em director nem credits
		},
		Companies: []richfields.Company{
			{Name: "Warner Bros. Pictures"},
			{Name: "Village Roadshow Pictures"},
		},
	}
}

// TestMontarNFOMovieCampoACampo: XML de movie.nfo bate campo a campo com a
// tabela de mapeamento do briefing 0062 SS4 (criterio de aceite SS5).
func TestMontarNFOMovieCampoACampo(t *testing.T) {
	e := fixtureEntidadeCompleta()
	out, err := MontarNFO(e, InsumosArte{}) // nada materializado localmente -> thumb/fanart de fallback aparecem
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)

	esperados := []string{
		"<movie>",
		"<title>Matrix</title>",
		"<originaltitle>The Matrix</originaltitle>",
		"<plot>Um hacker descobre a verdade.</plot>",
		"<tagline>Bem-vindo ao deserto do real.</tagline>",
		"<genre>Action</genre>",
		"<genre>Science Fiction</genre>",
		"<mpaa>R</mpaa>",
		"<premiered>1999-03-31</premiered>",
		`<rating name="tmdb">8.2</rating>`,
		`<uniqueid type="imdb">tt0133093</uniqueid>`,
		`<uniqueid type="tmdb" default="true">603</uniqueid>`,
		"<name>Keanu Reeves</name>",
		"<role>Neo</role>",
		"<order>0</order>",
		"<thumb>https://image.tmdb.org/t/p/w185/keanu.jpg</thumb>",
		"<name>Laurence Fishburne</name>",
		"<role>Morpheus</role>",
		"<director>Lana Wachowski</director>",
		"<director>Lilly Wachowski</director>",
		"<credits>Lana Wachowski</credits>",
		"<studio>Warner Bros. Pictures</studio>",
		"<studio>Village Roadshow Pictures</studio>",
		"<thumb>/api/v1/entities/e1/assets/poster</thumb>",
		"<fanart>",
		"<thumb>https://image.tmdb.org/t/p/original/backdrop.jpg</thumb>",
	}
	for _, esperado := range esperados {
		if !strings.Contains(xmlStr, esperado) {
			t.Errorf("XML nao contem %q\n--- XML completo ---\n%s", esperado, xmlStr)
		}
	}
	// Producer NAO deve aparecer nem como director nem como credits (so
	// Directing/Director e Writing entram).
	if strings.Contains(xmlStr, "Joel Silver") {
		t.Error("XML contem Joel Silver (Producer), mas so director/writer deveriam entrar")
	}
	// Filme NAO tem <status> (so tvshow.nfo).
	if strings.Contains(xmlStr, "<status>") {
		t.Error("movie.nfo contem <status>, mas status e exclusivo de tvshow.nfo")
	}
}

// TestMontarNFOTVShowIncluiStatus: tvshow.nfo inclui <status> a partir de
// AiringStatus; filme (testado acima) nao inclui (briefing 0062 SS5).
func TestMontarNFOTVShowIncluiStatus(t *testing.T) {
	e := resolver.Entidade{
		ID:           "s1",
		Type:         TipoSerie,
		Title:        "Show",
		AiringStatus: strPtr("Returning Series"),
	}
	out, err := MontarNFO(e, InsumosArte{})
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)
	if !strings.Contains(xmlStr, "<tvshow>") {
		t.Error("XML nao usa raiz <tvshow> para type=tv")
	}
	if !strings.Contains(xmlStr, "<status>Returning Series</status>") {
		t.Errorf("XML nao contem <status>Returning Series</status>:\n%s", xmlStr)
	}
}

// TestMontarNFOUniqueIDFallbackImdb: sem tmdb, o default cai para imdb (ordem
// tmdb, imdb, tvdb -- briefing 0062 SS5).
func TestMontarNFOUniqueIDFallbackImdb(t *testing.T) {
	e := resolver.Entidade{
		ID:          "e2",
		Type:        "movie",
		Title:       "Sem TMDB",
		ExternalIDs: map[string]string{"imdb": "tt9999999", "tvdb": "12345"},
	}
	out, err := MontarNFO(e, InsumosArte{})
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)
	if !strings.Contains(xmlStr, `<uniqueid type="imdb" default="true">tt9999999</uniqueid>`) {
		t.Errorf("uniqueid imdb sem default=true (deveria ser o fallback):\n%s", xmlStr)
	}
	if strings.Contains(xmlStr, `type="tvdb" default="true"`) {
		t.Error("tvdb nao deveria ser o default quando imdb esta presente")
	}
}

// TestMontarNFOThumbFanartOmitidosQuandoLocal: quando InsumosArte sinaliza
// poster/fanart JA materializados na pasta real, o NFO OMITE as tags de
// fallback (o arquivo local ja e a fonte -- briefing 0062 SS4).
func TestMontarNFOThumbFanartOmitidosQuandoLocal(t *testing.T) {
	e := fixtureEntidadeCompleta()
	out, err := MontarNFO(e, InsumosArte{PosterLocal: true, FanartLocal: true})
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)
	// So o <thumb> do ATOR (foto de perfil) pode sobrar -- o <thumb> de
	// nivel superior (fallback do poster) e o <fanart> devem sumir.
	if strings.Contains(xmlStr, "/api/v1/entities/e1/assets/poster") {
		t.Errorf("XML contem a URL do poster mesmo com PosterLocal=true:\n%s", xmlStr)
	}
	if strings.Contains(xmlStr, "<fanart>") {
		t.Errorf("XML contem <fanart> mesmo com FanartLocal=true:\n%s", xmlStr)
	}
}

// TestMontarNFOSemRating: VoteAverage nil omite <rating> por completo.
func TestMontarNFOSemRating(t *testing.T) {
	e := resolver.Entidade{ID: "e3", Type: "movie", Title: "Sem nota"}
	out, err := MontarNFO(e, InsumosArte{})
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	if strings.Contains(string(out), "<rating") {
		t.Errorf("XML contem <rating> mesmo com VoteAverage nil:\n%s", out)
	}
}
