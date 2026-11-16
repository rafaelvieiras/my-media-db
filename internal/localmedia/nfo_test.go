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

// fixtureEntidadeCompleta builds a "complete" resolver.Entidade (every field of
// the NFO mapping table, §4, filled in) so that the movie.nfo test can check it
// field by field.
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
			{Name: "Joel Silver", Department: "Production", Job: "Producer"}, // does not enter director nor credits
		},
		Companies: []richfields.Company{
			{Name: "Warner Bros. Pictures"},
			{Name: "Village Roadshow Pictures"},
		},
	}
}

// TestMontarNFOMovieCampoACampo: the movie.nfo XML matches the NFO mapping
// table field by field (acceptance criterion §5).
func TestMontarNFOMovieCampoACampo(t *testing.T) {
	e := fixtureEntidadeCompleta()
	out, err := MontarNFO(e, CamposUsuarioNFO{})
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
		"<ratings>",
		`<rating name="tmdb" max="10" default="true">`,
		"<value>8.2</value>",
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
	}
	for _, esperado := range esperados {
		if !strings.Contains(xmlStr, esperado) {
			t.Errorf("XML nao contem %q\n--- XML completo ---\n%s", esperado, xmlStr)
		}
	}
	// Producer must NOT appear as director or as credits (only
	// Directing/Director and Writing get in).
	if strings.Contains(xmlStr, "Joel Silver") {
		t.Error("XML contem Joel Silver (Producer), mas so director/writer deveriam entrar")
	}
	// A movie does NOT have <status> (only tvshow.nfo does).
	if strings.Contains(xmlStr, "<status>") {
		t.Error("movie.nfo contem <status>, mas status e exclusivo de tvshow.nfo")
	}
	// Poster/backdrop (movie level) are NEVER written from
	// e.PosterURL/e.BackdropURL (doc 09) -- the artwork materialized in the
	// real folder is already discovered by Kodi/Jellyfin through the naming
	// convention.
	if strings.Contains(xmlStr, "/api/v1/entities/e1/assets/poster") {
		t.Errorf("XML contem a URL do poster (e.PosterURL) -- <thumb> de nivel de filme nunca deveria ser escrito a partir do resolver:\n%s", xmlStr)
	}
	if strings.Contains(xmlStr, "<fanart>") {
		t.Errorf("XML contem <fanart> -- nunca deveria ser escrito a partir de e.BackdropURL:\n%s", xmlStr)
	}
}

// TestMontarNFOTVShowIncluiStatus: tvshow.nfo includes <status> from
// AiringStatus; a movie (tested above) does not (§5 of the NFO mapping).
func TestMontarNFOTVShowIncluiStatus(t *testing.T) {
	e := resolver.Entidade{
		ID:           "s1",
		Type:         TipoSerie,
		Title:        "Show",
		AiringStatus: strPtr("Returning Series"),
	}
	out, err := MontarNFO(e, CamposUsuarioNFO{})
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

// TestMontarNFOUniqueIDFallbackImdb: without tmdb, the default falls to imdb
// (order tmdb, imdb, tvdb -- §5 of the NFO mapping).
func TestMontarNFOUniqueIDFallbackImdb(t *testing.T) {
	e := resolver.Entidade{
		ID:          "e2",
		Type:        "movie",
		Title:       "Sem TMDB",
		ExternalIDs: map[string]string{"imdb": "tt9999999", "tvdb": "12345"},
	}
	out, err := MontarNFO(e, CamposUsuarioNFO{})
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

// TestMontarNFOThumbFanartNuncaEscritosDoResolver proves the guarantee of the
// NFO design (doc 09): movie/series-level <thumb>/<fanart> are NEVER written by
// MontarNFO from e.PosterURL/e.BackdropURL -- regardless of what those fields
// contain (even when pointing to a "valid" absolute URL), since the InsumosArte
// parameter no longer exists. Whoever materializes the artwork in the
// real folder (and therefore decides whether poster/fanart "exist" for
// Kodi/Jellyfin) is gravarNFO, BEFORE calling MontarNFO -- this package is
// pure and knows nothing about the disk.
func TestMontarNFOThumbFanartNuncaEscritosDoResolver(t *testing.T) {
	e := fixtureEntidadeCompleta()
	// e.PosterURL/e.BackdropURL (fixtureEntidadeCompleta) are "valid" URLs
	// (one an internal route, the other absolute) -- even so they must not
	// leak into the XML.
	out, err := MontarNFO(e, CamposUsuarioNFO{})
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)
	// Only the ACTOR <thumb> (profile picture) may remain -- the top-level
	// <thumb> (poster) and the <fanart> must never appear.
	if strings.Contains(xmlStr, "/api/v1/entities/e1/assets/poster") {
		t.Errorf("XML contem a URL do poster (e.PosterURL) -- nunca deveria ser escrita:\n%s", xmlStr)
	}
	if strings.Contains(xmlStr, "<fanart>") {
		t.Errorf("XML contem <fanart> -- nunca deveria ser escrito a partir de e.BackdropURL:\n%s", xmlStr)
	}
}

// TestMontarNFOSemRating: a nil VoteAverage omits <rating> entirely.
func TestMontarNFOSemRating(t *testing.T) {
	e := resolver.Entidade{ID: "e3", Type: "movie", Title: "Sem nota"}
	out, err := MontarNFO(e, CamposUsuarioNFO{})
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	if strings.Contains(string(out), "<rating") {
		t.Errorf("XML contem <rating> mesmo com VoteAverage nil:\n%s", out)
	}
}

// TestExtrairCamposUsuarioDescartaRatingLegado proves the fix: an .nfo written
// BEFORE the migration to <ratings> (old format, <rating name="tmdb">N</rating>
// directly under <movie>) cannot stay preserved forever by the Extras catch-all
// just because the tag stopped being a NAMED field -- that would duplicate the
// score (the old one AND the new <ratings>) on every future rewrite.
// RatingLegacy claims the old tag and silently discards it.
func TestExtrairCamposUsuarioDescartaRatingLegado(t *testing.T) {
	anterior := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Sabrina (versao antiga)</title>
  <rating name="tmdb">6.21</rating>
</movie>`)

	usuario := ExtrairCamposUsuario(anterior)
	for _, ex := range usuario.Extras {
		if ex.XMLName.Local == "rating" {
			t.Errorf("Extras contem a tag <rating> legada -- deveria ter sido descartada, nao preservada: %+v", ex)
		}
	}

	e := fixtureEntidadeCompleta() // VoteAverage != nil
	out, err := MontarNFO(e, usuario)
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)

	if strings.Count(xmlStr, "<rating ") != 1 {
		t.Errorf("esperava exatamente 1 <rating> (dentro de <ratings>, formato novo), achei %d:\n%s", strings.Count(xmlStr, "<rating "), xmlStr)
	}
	if !strings.Contains(xmlStr, `<rating name="tmdb" max="10" default="true">`) {
		t.Errorf("XML nao contem o <rating> no formato novo (<ratings>):\n%s", xmlStr)
	}
}

// TestExtrairCamposUsuarioRoundTrip proves that the LOCAL fields (user rating,
// watched/count, last playback, date added and resume point -- Kodi/Emby/
// Jellyfin convention) survive a round-trip ExtrairCamposUsuario -> MontarNFO:
// that is exactly what gravarNFO (internal/server/localmedia_handlers.go) does
// before overwriting an existing .nfo, so that an edit made outside MyMediaDB
// is not erased.
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
	out, err := MontarNFO(e, usuario)
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
	// The title, however, MUST have been updated with the new value -- only
	// the local fields are preserved; the rest of the NFO always reflects the
	// current data of resolver.Entidade.
	if !strings.Contains(xmlStr, "<title>Matrix</title>") {
		t.Errorf("titulo nao foi atualizado para o valor atual da entity:\n%s", xmlStr)
	}
}

// TestExtrairCamposUsuarioPreservaTagDesconhecidaRepetida proves the generic
// catch-all (doc 09, §4): a repeated tag that nfoDoc NEVER modeled (<tag>)
// survives a round-trip ExtrairCamposUsuario -> MontarNFO, in the same order in
// which it appeared in the previous file -- without needing a new entry in the
// closed list of named fields.
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
	out, err := MontarNFO(e, usuario)
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)

	// Both tags must survive, in the same relative order (Sci-Fi before
	// Cult).
	idxSciFi := strings.Index(xmlStr, "<tag>Sci-Fi</tag>")
	idxCult := strings.Index(xmlStr, "<tag>Cult</tag>")
	if idxSciFi == -1 || idxCult == -1 {
		t.Fatalf("XML nao contem as duas <tag> desconhecidas:\n%s", xmlStr)
	}
	if idxSciFi > idxCult {
		t.Errorf("<tag>Sci-Fi</tag> deveria vir antes de <tag>Cult</tag>:\n%s", xmlStr)
	}
}

// TestExtrairCamposUsuarioPreservaBlocoAninhadoDesconhecido proves that a
// nested block that nfoDoc does not model (e.g. <fileinfo><streamdetails>...,
// which Kodi itself writes after identifying the codecs) survives INTACT --
// same inner structure -- through a rewrite (doc 09, §5).
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
	out, err := MontarNFO(e, usuario)
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)
	if !strings.Contains(xmlStr, blocoInterno) {
		t.Errorf("XML regravado nao contem o bloco aninhado intacto:\n--- esperado dentro de ---\n%s\n--- XML ---\n%s", blocoInterno, xmlStr)
	}
}

// TestMontarNFOCamposManagedNuncaVemDoArquivoAntigo proves that fields MANAGED
// by MyMediaDB (title, plot etc.) are NEVER inherited from the previous .nfo
// even with the generic catch-all active -- they match a NAMED field of nfoDoc
// (title/plot), so they never fall into Extras, and MontarNFO always fills them
// from resolver.Entidade (database = single source of truth, doc 09,
// §3/§5).
func TestMontarNFOCamposManagedNuncaVemDoArquivoAntigo(t *testing.T) {
	anterior := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Titulo Desatualizado</title>
  <plot>Sinopse desatualizada, escrita por outra ferramenta.</plot>
  <genre>Terror</genre>
  <tag>Sci-Fi</tag>
</movie>`)

	usuario := ExtrairCamposUsuario(anterior)

	// title/plot/genre are named fields of nfoDoc -- they must not appear in
	// Extras (only the truly unknown tag should).
	for _, e := range usuario.Extras {
		if e.XMLName.Local == "title" || e.XMLName.Local == "plot" || e.XMLName.Local == "genre" {
			t.Errorf("Extras contem elemento MANAGED %q -- deveria ter sido capturado pelo campo nomeado, nao pelo catch-all", e.XMLName.Local)
		}
	}

	e := fixtureEntidadeCompleta() // Title="Matrix", Description="Um hacker descobre a verdade.", Tags=[Action, Science Fiction]
	out, err := MontarNFO(e, usuario)
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
	// The truly unknown tag (not MANAGED), on the other hand, MUST survive --
	// that is the whole point of the catch-all.
	if !strings.Contains(xmlStr, "<tag>Sci-Fi</tag>") {
		t.Errorf("XML regravado deveria preservar a tag desconhecida <tag>Sci-Fi</tag>:\n%s", xmlStr)
	}
}

// TestMontarNFOSemElementosDesconhecidosNaoGeraExtrasFantasma proves that an
// .nfo with no unknown element (the common case, CamposUsuarioNFO.Extras nil)
// keeps generating the same XML as before -- with no "<extras>" element or
// phantom wrapper showing up. This is a structural guarantee of encoding/xml (a
// ",any" field with a nil slice emits nothing when marshaling, and the Go field
// name "Extras" never becomes an XML tag, only the individual elements inside
// it would matter), but the test documents that guarantee explicitly as an
// acceptance criterion (doc 09, §5).
func TestMontarNFOSemElementosDesconhecidosNaoGeraExtrasFantasma(t *testing.T) {
	e := fixtureEntidadeCompleta()
	out, err := MontarNFO(e, CamposUsuarioNFO{})
	if err != nil {
		t.Fatalf("MontarNFO: %v", err)
	}
	xmlStr := string(out)
	if strings.Contains(xmlStr, "<extras") || strings.Contains(xmlStr, "</extras>") {
		t.Errorf("XML contem elemento <extras> fantasma mesmo sem Extras preenchido:\n%s", xmlStr)
	}
}

// TestExtrairCamposUsuarioArquivoAusenteOuInvalido covers the two degraded
// cases (best-effort): no previous file (empty bytes) and corrupted XML --
// both return the zero-value, never an error (writing the NFO must not fail
// because of that).
//
// It uses reflect.DeepEqual (not == / !=) because CamposUsuarioNFO gained the
// field Extras []xmlElementoBruto (doc 09, §4) -- a slice makes the struct
// non-comparable through native equality.
func TestExtrairCamposUsuarioArquivoAusenteOuInvalido(t *testing.T) {
	if usuario := ExtrairCamposUsuario(nil); !reflect.DeepEqual(usuario, CamposUsuarioNFO{}) {
		t.Errorf("bytes nil = %+v, quero zero-value", usuario)
	}
	if usuario := ExtrairCamposUsuario([]byte("isso nao e xml valido <<<")); !reflect.DeepEqual(usuario, CamposUsuarioNFO{}) {
		t.Errorf("XML invalido = %+v, quero zero-value", usuario)
	}
}
