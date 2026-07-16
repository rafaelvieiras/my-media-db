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
	out, err := MontarNFO(e, InsumosArte{}, CamposUsuarioNFO{}) // nada materializado localmente -> thumb/fanart de fallback aparecem
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
	out, err := MontarNFO(e, InsumosArte{}, CamposUsuarioNFO{})
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
	out, err := MontarNFO(e, InsumosArte{}, CamposUsuarioNFO{})
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
	out, err := MontarNFO(e, InsumosArte{PosterLocal: true, FanartLocal: true}, CamposUsuarioNFO{})
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
	out, err := MontarNFO(e, InsumosArte{}, CamposUsuarioNFO{})
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	if strings.Contains(string(out), "<rating") {
		t.Errorf("XML contem <rating> mesmo com VoteAverage nil:\n%s", out)
	}
}

// TestExtrairCamposUsuarioRoundTrip prova que os campos LOCAIS (avaliacao do
// usuario, assistido/contagem, ultima reproducao, data de adicao e ponto de
// retomada -- convencao Kodi/Emby/Jellyfin) sobrevivem a um round-trip
// ExtrairCamposUsuario -> MontarNFO: e exatamente o que gravarNFO
// (internal/server/localmedia_handlers.go) faz antes de sobrescrever um .nfo
// ja existente, para nao apagar edicao feita fora do MyMediaDB.
func TestExtrairCamposUsuarioRoundTrip(t *testing.T) {
	anterior := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Matrix (versao antiga, sera substituida)</title>
  <userrating>9.5</userrating>
  <playcount>3</playcount>
  <watched>true</watched>
  <lastplayed>2026-06-01 20:00:00</lastplayed>
  <dateadded>2024-01-15 10:00:00</dateadded>
  <resume>
    <position>1200.5</position>
    <total>7260</total>
  </resume>
</movie>`)

	usuario := ExtrairCamposUsuario(anterior)
	if usuario.UserRating == nil || *usuario.UserRating != 9.5 {
		t.Errorf("UserRating = %v, quero 9.5", usuario.UserRating)
	}
	if usuario.PlayCount == nil || *usuario.PlayCount != 3 {
		t.Errorf("PlayCount = %v, quero 3", usuario.PlayCount)
	}
	if usuario.Watched == nil || !*usuario.Watched {
		t.Errorf("Watched = %v, quero true", usuario.Watched)
	}
	if usuario.LastPlayed != "2026-06-01 20:00:00" {
		t.Errorf("LastPlayed = %q", usuario.LastPlayed)
	}
	if usuario.DateAdded != "2024-01-15 10:00:00" {
		t.Errorf("DateAdded = %q", usuario.DateAdded)
	}
	if usuario.Resume == nil || usuario.Resume.Position != 1200.5 || usuario.Resume.Total != 7260 {
		t.Errorf("Resume = %+v, quero {1200.5 7260}", usuario.Resume)
	}

	e := fixtureEntidadeCompleta()
	out, err := MontarNFO(e, InsumosArte{}, usuario)
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)
	for _, esperado := range []string{
		"<userrating>9.5</userrating>",
		"<playcount>3</playcount>",
		"<watched>true</watched>",
		"<lastplayed>2026-06-01 20:00:00</lastplayed>",
		"<dateadded>2024-01-15 10:00:00</dateadded>",
		"<position>1200.5</position>",
		"<total>7260</total>",
	} {
		if !strings.Contains(xmlStr, esperado) {
			t.Errorf("XML regravado nao contem %q (campo local do usuario deveria ter sido preservado):\n%s", esperado, xmlStr)
		}
	}
	// O titulo, porem, DEVE ter sido atualizado com o valor novo -- so os
	// campos locais sao preservados, o resto do NFO sempre reflete o dado
	// atual do resolver.Entidade.
	if !strings.Contains(xmlStr, "<title>Matrix</title>") {
		t.Errorf("titulo nao foi atualizado para o valor atual da entity:\n%s", xmlStr)
	}
}

// TestExtrairCamposUsuarioArquivoAusenteOuInvalido cobre os dois casos
// degradados (best-effort): sem arquivo anterior (bytes vazio) e XML
// corrompido -- ambos devolvem zero-value, nunca erro (a gravacao do NFO
// nao pode falhar por causa disso).
func TestExtrairCamposUsuarioArquivoAusenteOuInvalido(t *testing.T) {
	if usuario := ExtrairCamposUsuario(nil); usuario != (CamposUsuarioNFO{}) {
		t.Errorf("bytes nil = %+v, quero zero-value", usuario)
	}
	if usuario := ExtrairCamposUsuario([]byte("isso nao e xml valido <<<")); usuario != (CamposUsuarioNFO{}) {
		t.Errorf("XML invalido = %+v, quero zero-value", usuario)
	}
}
