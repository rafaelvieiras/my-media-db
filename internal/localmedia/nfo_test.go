package localmedia

import (
	"reflect"
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

// TestExtrairCamposUsuarioPreservaTagDesconhecidaRepetida prova o catch-all
// genérico (briefing 0065): uma tag repetida que nfoDoc NUNCA modelou
// (<tag>) sobrevive a um round-trip ExtrairCamposUsuario -> MontarNFO, na
// mesma ordem em que apareceu no arquivo anterior -- sem precisar de uma
// nova entrada na lista fechada de campos nomeados.
func TestExtrairCamposUsuarioPreservaTagDesconhecidaRepetida(t *testing.T) {
	anterior := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Matrix (versao antiga)</title>
  <tag>Sci-Fi</tag>
  <tag>Cult</tag>
</movie>`)

	usuario := ExtrairCamposUsuario(anterior)
	if len(usuario.Extras) != 2 {
		t.Fatalf("Extras = %+v, quero 2 elementos <tag>", usuario.Extras)
	}
	for i, nome := range []string{"tag", "tag"} {
		if usuario.Extras[i].XMLName.Local != nome {
			t.Errorf("Extras[%d].XMLName.Local = %q, quero %q", i, usuario.Extras[i].XMLName.Local, nome)
		}
	}

	e := fixtureEntidadeCompleta()
	out, err := MontarNFO(e, InsumosArte{}, usuario)
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)

	// As duas tags devem sobreviver, na mesma ordem relativa (Sci-Fi antes
	// de Cult).
	idxSciFi := strings.Index(xmlStr, "<tag>Sci-Fi</tag>")
	idxCult := strings.Index(xmlStr, "<tag>Cult</tag>")
	if idxSciFi == -1 || idxCult == -1 {
		t.Fatalf("XML nao contem as duas <tag> desconhecidas:\n%s", xmlStr)
	}
	if idxSciFi > idxCult {
		t.Errorf("<tag>Sci-Fi</tag> deveria vir antes de <tag>Cult</tag>:\n%s", xmlStr)
	}
}

// TestExtrairCamposUsuarioPreservaBlocoAninhadoDesconhecido prova que um
// bloco aninhado que nfoDoc não modela (ex. <fileinfo><streamdetails>...,
// que o próprio Kodi escreve após identificar codecs) sobrevive INTACTO --
// mesma estrutura interna -- a uma regravação (briefing 0065 SS5).
func TestExtrairCamposUsuarioPreservaBlocoAninhadoDesconhecido(t *testing.T) {
	blocoInterno := "<streamdetails><video><codec>h264</codec><width>1920</width></video></streamdetails>"
	anterior := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Matrix (versao antiga)</title>
  <fileinfo>` + blocoInterno + `</fileinfo>
</movie>`)

	usuario := ExtrairCamposUsuario(anterior)
	if len(usuario.Extras) != 1 {
		t.Fatalf("Extras = %+v, quero 1 elemento <fileinfo>", usuario.Extras)
	}
	if usuario.Extras[0].XMLName.Local != "fileinfo" {
		t.Errorf("Extras[0].XMLName.Local = %q, quero \"fileinfo\"", usuario.Extras[0].XMLName.Local)
	}
	if usuario.Extras[0].Inner != blocoInterno {
		t.Errorf("Extras[0].Inner = %q, quero %q (conteudo interno intacto)", usuario.Extras[0].Inner, blocoInterno)
	}

	e := fixtureEntidadeCompleta()
	out, err := MontarNFO(e, InsumosArte{}, usuario)
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)
	if !strings.Contains(xmlStr, blocoInterno) {
		t.Errorf("XML regravado nao contem o bloco aninhado intacto:\n--- esperado dentro de ---\n%s\n--- XML ---\n%s", blocoInterno, xmlStr)
	}
}

// TestMontarNFOCamposManagedNuncaVemDoArquivoAntigo prova que campos
// MANAGED pelo MyMediaDB (title, plot etc.) NUNCA são herdados do .nfo
// anterior mesmo com o catch-all genérico ativo -- eles batem com um campo
// NOMEADO de nfoDoc (title/plot), então nunca caem em Extras, e MontarNFO
// sempre os preenche a partir de resolver.Entidade (banco = unica fonte de
// verdade, briefing 0065 SS3/SS5).
func TestMontarNFOCamposManagedNuncaVemDoArquivoAntigo(t *testing.T) {
	anterior := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Titulo Desatualizado</title>
  <plot>Sinopse desatualizada, escrita por outra ferramenta.</plot>
  <genre>Terror</genre>
  <tag>Sci-Fi</tag>
</movie>`)

	usuario := ExtrairCamposUsuario(anterior)

	// title/plot/genre sao campos nomeados de nfoDoc -- nao devem aparecer em
	// Extras (so a tag verdadeiramente desconhecida deve).
	for _, e := range usuario.Extras {
		if e.XMLName.Local == "title" || e.XMLName.Local == "plot" || e.XMLName.Local == "genre" {
			t.Errorf("Extras contem elemento MANAGED %q -- deveria ter sido capturado pelo campo nomeado, nao pelo catch-all", e.XMLName.Local)
		}
	}

	e := fixtureEntidadeCompleta() // Title="Matrix", Description="Um hacker descobre a verdade.", Tags=[Action, Science Fiction]
	out, err := MontarNFO(e, InsumosArte{}, usuario)
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)

	if strings.Contains(xmlStr, "Titulo Desatualizado") {
		t.Errorf("XML regravado herdou o title do arquivo antigo:\n%s", xmlStr)
	}
	if strings.Contains(xmlStr, "Sinopse desatualizada") {
		t.Errorf("XML regravado herdou o plot do arquivo antigo:\n%s", xmlStr)
	}
	if strings.Contains(xmlStr, "<genre>Terror</genre>") {
		t.Errorf("XML regravado herdou o genre do arquivo antigo:\n%s", xmlStr)
	}
	if !strings.Contains(xmlStr, "<title>Matrix</title>") {
		t.Errorf("XML regravado nao reflete o title atual do banco:\n%s", xmlStr)
	}
	if !strings.Contains(xmlStr, "<plot>Um hacker descobre a verdade.</plot>") {
		t.Errorf("XML regravado nao reflete o plot atual do banco:\n%s", xmlStr)
	}
	// A tag verdadeiramente desconhecida (nao MANAGED), por outro lado, DEVE
	// sobreviver -- e o proprio objetivo deste briefing.
	if !strings.Contains(xmlStr, "<tag>Sci-Fi</tag>") {
		t.Errorf("XML regravado deveria preservar a tag desconhecida <tag>Sci-Fi</tag>:\n%s", xmlStr)
	}
}

// TestMontarNFOSemElementosDesconhecidosNaoGeraExtrasFantasma prova que um
// .nfo sem nenhum elemento desconhecido (caso comum, CamposUsuarioNFO.Extras
// nil) continua gerando o mesmo XML de antes -- sem nenhum elemento
// "<extras>" ou wrapper fantasma aparecendo. Isso e uma garantia estrutural
// do encoding/xml (um campo ",any" com slice nil nao emite nada ao
// marshalizar, e o nome do campo Go "Extras" nunca vira uma tag XML, so os
// elementos individuais dentro dele importariam), mas o teste documenta essa
// garantia explicitamente como criterio de aceite (briefing 0065 SS5).
func TestMontarNFOSemElementosDesconhecidosNaoGeraExtrasFantasma(t *testing.T) {
	e := fixtureEntidadeCompleta()
	out, err := MontarNFO(e, InsumosArte{}, CamposUsuarioNFO{})
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)
	if strings.Contains(xmlStr, "<extras") || strings.Contains(xmlStr, "</extras>") {
		t.Errorf("XML contem elemento <extras> fantasma mesmo sem Extras preenchido:\n%s", xmlStr)
	}
}

// TestExtrairCamposUsuarioArquivoAusenteOuInvalido cobre os dois casos
// degradados (best-effort): sem arquivo anterior (bytes vazio) e XML
// corrompido -- ambos devolvem zero-value, nunca erro (a gravacao do NFO
// nao pode falhar por causa disso).
//
// Usa reflect.DeepEqual (nao == / !=) porque CamposUsuarioNFO ganhou o campo
// Extras []xmlElementoBruto (briefing 0065) -- um slice torna o struct
// nao-comparavel por igualdade nativa.
func TestExtrairCamposUsuarioArquivoAusenteOuInvalido(t *testing.T) {
	if usuario := ExtrairCamposUsuario(nil); !reflect.DeepEqual(usuario, CamposUsuarioNFO{}) {
		t.Errorf("bytes nil = %+v, quero zero-value", usuario)
	}
	if usuario := ExtrairCamposUsuario([]byte("isso nao e xml valido <<<")); !reflect.DeepEqual(usuario, CamposUsuarioNFO{}) {
		t.Errorf("XML invalido = %+v, quero zero-value", usuario)
	}
}
