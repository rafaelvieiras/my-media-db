// Package musicbrainz implements the BASE provider of the music vertical (ADR-0027 item
// 6): artist search, assembly of the artist→albums→tracks tree through the public
// MusicBrainz API (https://musicbrainz.org/ws/2/, without a key), and album cover art
// through the Cover Art Archive (https://coverartarchive.org/, same group of projects, a
// separate host). It follows the same pattern as internal/provider/themerrdb (the only
// other provider without a credential today): the Client implements the core
// provider.Provider with MINIMAL Buscar/BuscarDetalhe/MapearDetalhe (only enough for
// artist search/import), and the real capabilities are provider.MusicaProvider (tree) and
// provider.ArteProvider (cover through the CAA).
//
// MusicBrainz requires <= 1 request/s per IP and a User-Agent identifying the application
// -- without that it blocks for abuse
// (https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting). Every GET to musicbrainz.org
// goes through the limiter in ratelimit.go; the Cover Art Archive does not document an
// equivalent limit, so it is NOT limited here (it only gets the same User-Agent, as good
// practice).
package musicbrainz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// baseURLPadrao is the stable base of the MusicBrainz v2 API (JSON through ?fmt=json).
	baseURLPadrao = "https://musicbrainz.org/ws/2"
	// caaBaseURLPadrao is the base of the Cover Art Archive -- a host SEPARATE from
	// MusicBrainz (same group of projects, distinct infrastructure). It is not a parameter of
	// New() for simplicity (no other provider of the repo needed 2 bases so far); the tests
	// inject a test value directly into the `caaBaseURL` field of the Client (same package,
	// without a public API for that -- see musicbrainz_test.go).
	caaBaseURLPadrao = "https://coverartarchive.org"
	// timeoutPadrao bounds each request; an overrun becomes an error (enrich degrades).
	timeoutPadrao = 8 * time.Second
	// intervaloRateLimit is the minimum interval between two requests to musicbrainz.org
	// ("MusicBrainz requires <= 1 request/s").
	intervaloRateLimit = time.Second
	// nomeFonte is the Nome() of the source (also the source in entity/media_external).
	nomeFonte = "musicbrainz"
	// userAgent is the MANDATORY header in every call to MusicBrainz -- the API blocks for
	// abuse without a User-Agent identifying the application and a contact. The production
	// wiring (cmd/mymediadb/main.go, outside this package) could in the future replace the fixed
	// version with main.version; for now a stable value is enough (the requirement of the API is
	// to IDENTIFY the app consistently, not to version it strictly).
	userAgent = "MyMediaDB/dev (+https://github.com/rafaelvieiras/my-media-db)"
)

// Client is the MusicBrainz + Cover Art Archive provider. httpClient and baseURL are
// injectable for testing (point at an httptest.Server without the real network); caaBaseURL
// is only injectable internally (same package) -- see the comment of caaBaseURLPadrao.
type Client struct {
	httpClient *http.Client
	baseURL    string
	caaBaseURL string
	lim        *limitador
}

// New creates the client. A nil httpClient uses a default with timeout; an empty baseURL uses
// the public MusicBrainz base. Without a credential: it can always be registered
// (cmd/mymediadb/main.go, provedoresReais -- the same pattern as
// internal/provider/themerrdb.New).
func New(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeoutPadrao}
	}
	if baseURL == "" {
		baseURL = baseURLPadrao
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		caaBaseURL: caaBaseURLPadrao,
		lim:        novoLimitador(intervaloRateLimit),
	}
}

// Nome identifies the source.
func (c *Client) Nome() string { return nomeFonte }

// Buscar queries MusicBrainz by artist (core provider.Provider capability). It only answers to
// `tipo=="artista"` (or empty) -- other types return empty, without polluting the
// movie/series search. `lang` is ignored: the textual search of MusicBrainz does not filter by
// language.
func (c *Client) Buscar(ctx context.Context, query, tipo, lang string) ([]provider.ResultadoExterno, error) {
	_ = lang
	if tipo != "" && tipo != "artista" {
		return nil, nil
	}
	if query == "" {
		return nil, nil
	}

	params := url.Values{}
	params.Set("query", query)
	body, err := c.getMB(ctx, "/artist", params)
	if err != nil {
		return nil, err
	}

	var resp respBuscaArtista
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("musicbrainz: resposta de busca invalida: %w", err)
	}

	out := make([]provider.ResultadoExterno, 0, len(resp.Artists))
	for _, a := range resp.Artists {
		if a.ID == "" || a.Name == "" {
			continue
		}
		r := provider.ResultadoExterno{
			Source:   nomeFonte,
			SourceID: a.ID,
			Type:     "artista",
			Title:    a.Name,
			Saved:    false,
		}
		// Disambiguation (e.g. "English rock band") helps the owner choose between homonym
		// artists; we reuse Overview, the only free-text field of the generic shape without a
		// conflicting semantics.
		if a.Disambiguation != "" {
			d := a.Disambiguation
			r.Overview = &d
		}
		out = append(out, r)
	}
	return out, nil
}

// BuscarDetalhe pulls the RAW detail (core provider.Provider capability, used by the generic
// "Download Metadata" through internal/enrich and by importarItemBase). It dispatches by
// `tipo` to the correct endpoint -- /artist for artist, /release-group for album, /recording
// for track (the SAME ones that montarAlbumOuErro/BuscarAlbumDaFaixa already use) -- a real
// finding in the production validation: before that, it ALWAYS called /artist/{sourceID} no
// matter the type, and sourceID for album/track is the MBID of release-group/recording (not
// of artist), so it hit 404/502 on those two types. `langs` has no effect: MusicBrainz is not
// multi-language. For album/track, the body goes through remapParaNome before returning:
// release-group/recording expose the title in the "title" key (not "name"), so without that
// remap MapearDetalhe (below) would not find anything -- normalizing here avoids changing the
// signature of MapearDetalhe (which does not receive `tipo`), a change that would be
// BREAKING in every registered Provider (tmdb/tvdb/fanarttv/themerrdb), to solve a problem
// that belongs only to MusicBrainz.
func (c *Client) BuscarDetalhe(ctx context.Context, sourceID, tipo string, langs []string) (json.RawMessage, error) {
	_ = langs
	switch tipo {
	case "album":
		body, err := c.getMB(ctx, "/release-group/"+url.PathEscape(sourceID), url.Values{})
		if err != nil {
			return nil, err
		}
		return remapParaNome(body)
	case "faixa":
		body, err := c.getMB(ctx, "/recording/"+url.PathEscape(sourceID), url.Values{})
		if err != nil {
			return nil, err
		}
		return remapParaNome(body)
	default:
		body, err := c.getMB(ctx, "/artist/"+url.PathEscape(sourceID), url.Values{})
		if err != nil {
			return nil, err
		}
		return json.RawMessage(body), nil
	}
}

// remapParaNome rewrites the `"title"` of a raw release-group/recording to `"name"` -- the
// field that MapearDetalhe knows how to read (the same key that the artist shape already
// uses). See the comment of BuscarDetalhe for why this detour instead of changing
// MapearDetalhe.
func remapParaNome(body []byte) (json.RawMessage, error) {
	var t respTitulo
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, fmt.Errorf("musicbrainz: detalhe invalido: %w", err)
	}
	out, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: t.Title})
	if err != nil {
		return nil, fmt.Errorf("musicbrainz: remapear detalhe: %w", err)
	}
	return json.RawMessage(out), nil
}

// respTitulo is the minimal shape common to /release-group/{id} and /recording/{id}: both
// expose the title of the resource in the "title" key (core attribute, always present).
type respTitulo struct {
	Title string `json:"title"`
}

// MapearDetalhe extracts only the name (artist) or the already remapped title (album/track,
// see BuscarDetalhe) into the canonical shape -- enough for the generic import/enrich to give
// a title to the entity; the rest of the tree (albums/tracks) comes in through importarArtista
// (internal/enrich/musica.go, outside this package).
func (c *Client) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	var d struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("musicbrainz: detalhe invalido: %w", err)
	}
	out := map[string]any{}
	if d.Name != "" {
		out["title"] = d.Name
	}
	return out, nil
}

// BuscarArtistaArvore assembles the complete tree of the artist (capability
// provider.MusicaProvider): 1 call for the artist+release-groups, and per album, 1 call to
// choose the "official" release (release-group?inc=releases+genres, which already brings the
// genre with the highest count in a single shot) + 1 call for the tracks of that release
// (release?inc=recordings+artist-credits+isrcs). Cost: ~1+2N requests for N albums, at <= 1
// req/s (acceptable, it is an explicit/async operation of the owner).
func (c *Client) BuscarArtistaArvore(ctx context.Context, sourceID string, langs []string) (provider.ArtistaArvore, error) {
	_ = langs // MusicBrainz is not multi-language (see the interface docs); reserved

	body, err := c.getMB(ctx, "/artist/"+url.PathEscape(sourceID), url.Values{"inc": {"release-groups+annotation"}})
	if err != nil {
		return provider.ArtistaArvore{}, err
	}
	var art respArtistaArvore
	if err := json.Unmarshal(body, &art); err != nil {
		return provider.ArtistaArvore{}, fmt.Errorf("musicbrainz: artista invalido: %w", err)
	}

	arvore := provider.ArtistaArvore{
		Name: art.Name,
		MBID: sourceID,
		Bio:  art.Annotation,
	}
	for _, rg := range art.ReleaseGroups {
		if rg.ID == "" {
			continue
		}
		// ANY error (not only a 404) degrades THIS ALBUM to a shell without tracks, it never
		// aborts the whole tree -- a real finding in the production validation: the public
		// MusicBrainz server is unstable enough (503 in ~1/3 of the calls) that, in a tree of
		// dozens of albums (1+2N calls), even with retry (getMB) the chance of ONE specific call
		// exhausting the attempts is not negligible. An artist with a large discography is much
		// more useful partially imported (albums without tracks, fixable later through a
		// reimport) than failing entirely.
		album := c.montarAlbum(ctx, rg.ID, rg.Title, rg.FirstReleaseDate, provider.AlvoFaixaAlbum{})
		arvore.Albuns = append(arvore.Albuns, album)
	}
	return arvore, nil
}

// montarAlbum fetches the release-group (releases+genres, 1 call), chooses the release(s) to
// represent the album according to `alvoFaixa` (see buscarFaixasComAlvo), and fetches the
// tracks. It NEVER returns an error: a release-group without any release, a one-off 404, or
// the retry of getMB being exhausted (instability of the public server) all degrade to the
// same shell -- an album with name/MBID/year but without tracks -- instead of taking down the
// whole tree (see BuscarArtistaArvore). Tracks missing because of that are fixable in a later
// reimport (find-or-create by MBID, idempotent). `title`/`firstReleaseDate` are OPTIONAL: when
// empty (the caller does not have them beforehand, see BuscarAlbumDaFaixa), it uses the
// title/date that the release-group ITSELF returns (rg.Title/rg.FirstReleaseDate) -- the
// caller that already has them (BuscarArtistaArvore, from the browse of the release-groups of
// the artist) avoids that dependency on the 2nd field. `alvoFaixa` is the zero value in the
// paths that do not need it (BuscarArtistaArvore/BuscarAlbumPorMbid).
func (c *Client) montarAlbum(ctx context.Context, rgID, title, firstReleaseDate string, alvoFaixa provider.AlvoFaixaAlbum) provider.AlbumExterno {
	album, err := c.montarAlbumOuErro(ctx, rgID, title, firstReleaseDate, alvoFaixa)
	if err != nil {
		return provider.AlbumExterno{Name: title, MBID: rgID, Year: anoDe(firstReleaseDate)}
	}
	return album
}

// montarAlbumOuErro is the body of montarAlbum that PROPAGATES the error of the 1st call
// (fetch of the release-group) instead of degrading to the shell -- extracted for
// BuscarAlbumPorMbid, the only path of MusicaAlbumProvider that has no previous resolution
// confirming that the release-group exists, so it needs to know whether the failure was "not
// found" (→ ErrNaoEncontrado, 404) or something else. The failure of the TRACKS call
// (buscarFaixasComAlvo) keeps degrading silently (the same semantics as always): the
// release-group was already confirmed in this very call, so a one-off instability in the
// following round trip(s) is worth more as a partial album (without tracks, fixable in a
// reimport) than as an error to the caller.
func (c *Client) montarAlbumOuErro(ctx context.Context, rgID, title, firstReleaseDate string, alvoFaixa provider.AlvoFaixaAlbum) (provider.AlbumExterno, error) {
	body, err := c.getMB(ctx, "/release-group/"+url.PathEscape(rgID), url.Values{"inc": {"releases+genres"}})
	if err != nil {
		return provider.AlbumExterno{}, err
	}
	var rg respReleaseGroup
	if err := json.Unmarshal(body, &rg); err != nil {
		return provider.AlbumExterno{}, fmt.Errorf("musicbrainz: release-group invalido: %w", err)
	}

	album := provider.AlbumExterno{Name: title, MBID: rgID, Year: anoDe(firstReleaseDate)}
	if album.Name == "" {
		album.Name = rg.Title
	}
	if album.Year == "" {
		album.Year = anoDe(rg.FirstReleaseDate)
	}
	album.Genre = generoPrincipal(rg.Genres)

	faixas, err := c.buscarFaixasComAlvo(ctx, rg.Releases, alvoFaixa)
	if err != nil {
		return album, nil // no release answered (404/retry exhausted): degrades without tracks
	}
	album.Faixas = faixas
	return album, nil
}

// BuscarAlbumPorMbid is the provider.MusicaAlbumProvider capability (ADR-0029 item 6), the
// DIRECT path: the MBID already is the one of the release-group -- used by the external album
// screen (GET /external/musicbrainz/album/{mbid}) and by the single-track import (both start
// from the external_ids["mbid"] already stored in the internal album entity). Unlike
// montarAlbum (used only AFTER being resolved by another path), here the existence of the
// release-group is validated in this very call through montarAlbumOuErro -- a 404 becomes
// provider.ErrNaoEncontrado (errors.Is).
func (c *Client) BuscarAlbumPorMbid(ctx context.Context, albumMBID string, langs []string) (provider.AlbumExterno, error) {
	_ = langs
	if albumMBID == "" {
		return provider.AlbumExterno{}, fmt.Errorf("musicbrainz: mbid de album vazio")
	}
	return c.montarAlbumOuErro(ctx, albumMBID, "", "", provider.AlvoFaixaAlbum{})
}

// BuscarAlbumDaFaixa is the provider.MusicaAlbumProvider capability (ADR-0029 item 6), the
// PRECISE path: it resolves the release-group that contains the recording `recordingMBID` (1
// call, /recording/{id}?inc=releases+release-groups -- each release of the recording already
// comes with its release-group nested, without an extra call) and assembles THAT single album
// through montarAlbum (plus 1+N calls) -- never the whole discography. Among the releases of
// the recording, it prefers the one with status "Official" (the same criterion as
// escolherRelease); without any, the first one with a release-group present. It passes
// `provider.AlvoFaixaAlbum{RecordingMBID: recordingMBID}` to montarAlbum: the "Official"
// release of the release-group may NOT be the same one that contains this recording
// specifically (escolherReleaseGroupDoRecording only resolves the release-group, it does not
// guarantee that montarAlbum chooses the right release inside it) -- without the target, the
// PRECISE path had the same structural gap as the textual one.
func (c *Client) BuscarAlbumDaFaixa(ctx context.Context, recordingMBID string, langs []string) (provider.AlbumExterno, bool, error) {
	_ = langs
	if recordingMBID == "" {
		return provider.AlbumExterno{}, false, fmt.Errorf("musicbrainz: mbid de faixa vazio")
	}
	body, err := c.getMB(ctx, "/recording/"+url.PathEscape(recordingMBID), url.Values{"inc": {"releases+release-groups"}})
	if err != nil {
		return provider.AlbumExterno{}, false, err
	}
	var rec respRecordingReleases
	if err := json.Unmarshal(body, &rec); err != nil {
		return provider.AlbumExterno{}, false, fmt.Errorf("musicbrainz: recording invalido: %w", err)
	}
	rgID, ok := escolherReleaseGroupDoRecording(rec.Releases)
	if !ok {
		return provider.AlbumExterno{}, false, nil
	}
	return c.montarAlbum(ctx, rgID, "", "", provider.AlvoFaixaAlbum{RecordingMBID: recordingMBID}), true, nil
}

// escolherReleaseGroupDoRecording chooses, among the releases of the recording, the
// release-group of the FIRST one with status "Official" (the same criterion as
// escolherRelease); without any official one, the release-group of the first one that has
// one (some releases may come without a nested release-group -- incomplete data in the
// source). ok=false without ANY release with a usable release-group.
func escolherReleaseGroupDoRecording(releases []respReleaseComGrupo) (string, bool) {
	for _, r := range releases {
		if r.Status == "Official" && r.ReleaseGroup.ID != "" {
			return r.ReleaseGroup.ID, true
		}
	}
	for _, r := range releases {
		if r.ReleaseGroup.ID != "" {
			return r.ReleaseGroup.ID, true
		}
	}
	return "", false
}

// BuscarAlbumPorNome is the provider.MusicaAlbumProvider capability (ADR-0029 item 6), the
// TEXTUAL path: it lists the release-groups of the artist `artistaMBID` (1 cheap call --
// /artist/{id}?inc=release-groups, ids/titles only, the SAME 1st request that
// BuscarArtistaArvore already makes), chooses the one whose title is closest to `nomeAlbum`
// (Jaro-Winkler through internal/textsim, matching.LimiarAutoMatch threshold -- the same
// engine/number that the scanner uses for the textual artist match, see
// internal/scanner/musica.go melhorCandidatoTexto) and assembles ONLY that album through
// montarAlbum -- never the other listed release-groups. `alvoFaixa` is only forwarded to
// montarAlbum, without extra logic of its own here -- whoever decides WHICH releases to try
// inside the chosen release-group is buscarFaixasComAlvo.
func (c *Client) BuscarAlbumPorNome(ctx context.Context, artistaMBID, nomeAlbum string, alvoFaixa provider.AlvoFaixaAlbum, langs []string) (provider.AlbumExterno, bool, error) {
	_ = langs
	if artistaMBID == "" || nomeAlbum == "" {
		return provider.AlbumExterno{}, false, fmt.Errorf("musicbrainz: artista ou nome de album vazio")
	}
	body, err := c.getMB(ctx, "/artist/"+url.PathEscape(artistaMBID), url.Values{"inc": {"release-groups"}})
	if err != nil {
		return provider.AlbumExterno{}, false, err
	}
	var art respArtistaArvore
	if err := json.Unmarshal(body, &art); err != nil {
		return provider.AlbumExterno{}, false, fmt.Errorf("musicbrainz: artista invalido: %w", err)
	}
	rg, ok := melhorReleaseGroupPorTitulo(nomeAlbum, art.ReleaseGroups)
	if !ok {
		return provider.AlbumExterno{}, false, nil
	}
	return c.montarAlbum(ctx, rg.ID, rg.Title, rg.FirstReleaseDate, alvoFaixa), true, nil
}

// melhorReleaseGroupPorTitulo chooses, among `rgs`, the one whose title is closest to `alvo` --
// the same criterion as melhorCandidatoTexto of the scanner (Jaro-Winkler,
// matching.LimiarAutoMatch, WITHOUT a tie at the top): an ambiguous or too weak name never
// applies a "similar" match, it falls back to (zero,false).
func melhorReleaseGroupPorTitulo(alvo string, rgs []respReleaseGroupBreve) (respReleaseGroupBreve, bool) {
	melhorScore := -1.0
	var melhor respReleaseGroupBreve
	empate := false
	for _, rg := range rgs {
		if rg.ID == "" {
			continue
		}
		score := matching.SimilaridadeNormalizada(alvo, rg.Title)
		switch {
		case score > melhorScore:
			melhorScore, melhor, empate = score, rg, false
		case score == melhorScore:
			empate = true
		}
	}
	if empate || melhorScore < matching.LimiarAutoMatch {
		return respReleaseGroupBreve{}, false
	}
	return melhor, true
}

// escolherRelease chooses WHICH release represents the album for tracklist purposes, among the
// several pressings/editions that a release-group typically has (in the real world, dozens --
// reissues, countries, remasters). It prefers the first one with status "Official" (the
// convention of MusicBrainz itself for the "canonical" edition); without any official one, it
// falls back to the first of the list. ok=false only when the list is empty.
func escolherRelease(releases []respReleaseResumo) (string, bool) {
	for _, r := range releases {
		if r.Status == "Official" {
			return r.ID, true
		}
	}
	if len(releases) > 0 {
		return releases[0].ID, true
	}
	return "", false
}

// maxReleasesTentadosPorAlvo is the CEILING of releases tried by buscarFaixasComAlvo when
// there is an alvoFaixa to satisfy -- a real release-group may have dozens of releases
// (reissues, countries, remasters); without a ceiling, a track genuinely absent from the whole
// of MusicBrainz would generate dozens of HTTP calls per import/reconciliation attempt, at <= 1
// req/s (an already tight rate limit). Raised from 5 to 15 in round 2: the production
// validation found a real outlier of 13 releases (Bomfunk MC's "Freestyler") whose right
// release falls around the ~10th place in the Official-first order -- 15 covers that case with
// room to spare, the cost stays negligible (it only comes into play when the 1st attempt
// fails).
const maxReleasesTentadosPorAlvo = 15

// buscarFaixasComAlvo fetches the tracks of the release-group represented by `releases`,
// trying 1 or more releases according to `alvoFaixa` (a real finding: Bomfunk MC's
// "Freestyler" is not in the "Official" release chosen by escolherRelease, but exists in
// another release of the SAME release-group). With `alvoFaixa` at the ZERO VALUE, it
// preserves the exact HISTORICAL behavior from before this feature: it chooses a single
// buscarFaixas -- ZERO cost/HTTP-call regression for whoever never needed a specific track
// (BuscarArtistaArvore/BuscarAlbumPorMbid). With `alvoFaixa` filled in: it tries the releases in
// priority order (ordenarReleasesPorPrioridade) until it finds one whose tracks satisfy the
// target (releaseContemFaixaAlvo) -- it stops at the first success -- or exhausts
// maxReleasesTentadosPorAlvo attempts, whichever comes first. If none matches within the
// ceiling, it returns the tracks of the LAST release that answered successfully (the same
// silent degradation as always -- whoever decides whether the right track was found is still
// escolherFaixaAlvo in internal/enrich/musica.go, this function only improves the CANDIDATES
// offered). The error is only propagated when ALL the attempts (of BOTH passes, see
// tentarReleases) failed on the network (no track obtained in any attempt) -- a one-off failure
// in the middle of the loop does not abort the attempt of the next release.
//
// A real finding in production: a SPECIFIC release that exhausts the network retry of getMB (3
// attempts, transient 503 "currently busy" from the public server) used to be discarded FOREVER
// in this function -- even if it was exactly the release with the target track. That is why,
// when the 1st pass does not match the target and at least one release failed on the NETWORK
// (not on not matching), there is a 2nd pass only over those -- each one gets a NEW and complete
// cycle of attempts (a 503 is transient instability: the same release may answer seconds
// later).
func (c *Client) buscarFaixasComAlvo(ctx context.Context, releases []respReleaseResumo, alvoFaixa provider.AlvoFaixaAlbum) ([]provider.FaixaExterna, error) {
	if alvoFaixa == (provider.AlvoFaixaAlbum{}) {
		releaseID, ok := escolherRelease(releases)
		if !ok {
			return nil, nil // release-group without any release: no tracks, no error
		}
		return c.buscarFaixas(ctx, releaseID)
	}

	ids := ordenarReleasesPorPrioridade(releases)
	if len(ids) > maxReleasesTentadosPorAlvo {
		ids = ids[:maxReleasesTentadosPorAlvo]
	}

	match, ultimaObtida, obtevesAlgumaVez, falhadas, ultimoErr := c.tentarReleases(ctx, ids, alvoFaixa)
	if match {
		return ultimaObtida, nil
	}
	if len(falhadas) > 0 {
		// 2nd pass: only over the releases that failed on the NETWORK in the 1st one -- it saves the
		// extra call when everything already answered and simply did not match.
		match2, ultimaObtida2, obtevesAlgumaVez2, _, ultimoErr2 := c.tentarReleases(ctx, falhadas, alvoFaixa)
		if match2 {
			return ultimaObtida2, nil
		}
		if obtevesAlgumaVez2 {
			ultimaObtida = ultimaObtida2 // the most recent one that succeeded, between the two passes
			obtevesAlgumaVez = true
		}
		ultimoErr = ultimoErr2
	}
	if !obtevesAlgumaVez {
		return nil, ultimoErr // ALL the attempts (of the two passes) failed on the network
	}
	return ultimaObtida, nil // none matched within the ceiling: it degrades with the LAST obtained one
}

// tentarReleases tries, in order, to fetch the tracks of each release in `ids`, stopping at the
// first success that satisfies `alvoFaixa` (releaseContemFaixaAlvo) -- the body of the loop
// extracted from buscarFaixasComAlvo to be reused by a 2nd pass (see there). Each failure of
// `buscarFaixas` (a NETWORK error, not "it answered but did not match") is logged with the
// releaseID and the error, and accumulated in `falhadas` -- before this, those releases were
// discarded forever and silently. It returns: `match` (it found a release that satisfies the
// target -- `ultimaObtida` already are its tracks), `ultimaObtida` (the tracks of the LAST
// release that answered successfully, for the degradation fallback of buscarFaixasComAlvo when
// nothing matches), `obtevesAlgumaVez` (true if ANY release of this pass answered successfully),
// `falhadas` (releaseIDs that failed on the network in this pass) and `ultimoErr` (the error of
// the last network failure of this pass, only relevant when `obtevesAlgumaVez` is false).
func (c *Client) tentarReleases(ctx context.Context, ids []string, alvoFaixa provider.AlvoFaixaAlbum) (match bool, ultimaObtida []provider.FaixaExterna, obtevesAlgumaVez bool, falhadas []string, ultimoErr error) {
	for _, releaseID := range ids {
		fx, err := c.buscarFaixas(ctx, releaseID)
		if err != nil {
			log.Printf("musicbrainz: busca de faixas da release %s falhou (retentando outra release do grupo): %v", releaseID, err)
			falhadas = append(falhadas, releaseID)
			ultimoErr = err
			continue
		}
		obtevesAlgumaVez = true
		ultimaObtida = fx
		if releaseContemFaixaAlvo(fx, alvoFaixa) {
			return true, fx, true, falhadas, nil
		}
	}
	return false, ultimaObtida, obtevesAlgumaVez, falhadas, ultimoErr
}

// ordenarReleasesPorPrioridade sorts the IDs of `releases` by priority -- the SAME criterion as
// escolherRelease (status=="Official" first, in the original order among themselves; then the
// others, also in the original order among themselves), but returning the WHOLE LIST instead of
// stopping at the first one -- used by buscarFaixasComAlvo to try multiple releases of the same
// release-group when there is an alvoFaixa to satisfy.
func ordenarReleasesPorPrioridade(releases []respReleaseResumo) []string {
	ids := make([]string, 0, len(releases))
	for _, r := range releases {
		if r.Status == "Official" {
			ids = append(ids, r.ID)
		}
	}
	for _, r := range releases {
		if r.Status != "Official" {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// releaseContemFaixaAlvo reports whether `faixas` (the ones of ONE already fetched release)
// contain the track that `alvo` describes. SELF-CONTAINED in this package -- it does NOT reuse
// escolherFaixaAlvo/melhorFaixaPorTitulo from internal/enrich because of an import cycle (the
// provider cannot depend on enrich, which already depends on provider); it is a MINIMAL
// functional copy, the same formula/threshold, not a new shared abstraction. The same priority of
// AlvoFaixaAlbum: exact MBID > title by similarity (matching.SimilaridadeNormalizada, best score
// among the tracks, matching.LimiarAutoMatch threshold) > exact track_number. Without any
// applicable criterion (all fields of `alvo` empty/zero), it returns false -- it never applies a
// "similar one" for nothing.
func releaseContemFaixaAlvo(faixas []provider.FaixaExterna, alvo provider.AlvoFaixaAlbum) bool {
	if alvo.RecordingMBID != "" {
		return releaseContemRecordingMBID(faixas, alvo.RecordingMBID)
	}
	if alvo.FaixaTitulo != "" {
		return releaseContemFaixaPorTitulo(faixas, alvo)
	}
	if alvo.FaixaNumero > 0 {
		return releaseContemFaixaPorNumero(faixas, alvo.FaixaNumero)
	}
	return false
}

// releaseContemRecordingMBID is the highest-priority criterion of releaseContemFaixaAlvo: the
// exact recording MBID.
func releaseContemRecordingMBID(faixas []provider.FaixaExterna, recordingMBID string) bool {
	for _, fx := range faixas {
		if fx.MBID == recordingMBID {
			return true
		}
	}
	return false
}

// desempatePorDiscoEFaixa returns the SINGLE track of the list whose position (disc, track)
// matches the one of the file. ok=false when the position is unknown on one of the sides or when
// none/more than one track matches -- the caller then decides by another signal. Disc 0 of the
// target counts as "unknown" and matches disc 1 of the source (a file without a disc tag in a
// single-disc release).
func desempatePorDiscoEFaixa(faixas []provider.FaixaExterna, disco, numero int) (provider.FaixaExterna, bool) {
	// 1st attempt: the EXACT position (disc, track). Without a disc tag, disc 1 is the safe
	// default (a single-disc release also reports 1).
	if numero > 0 {
		alvoDisco := disco
		if alvoDisco <= 0 {
			alvoDisco = 1
		}
		var porPosicao []provider.FaixaExterna
		for _, fx := range faixas {
			if fx.TrackNumber == numero && discoDe(fx) == alvoDisco {
				porPosicao = append(porPosicao, fx)
			}
		}
		if len(porPosicao) == 1 {
			return porPosicao[0], true
		}
	}
	// 2nd attempt: only the DISC, and only when the FILE declared which one it is. The numbering
	// of the file comes from tags/folder and may not match the order of the release, but the disc
	// usually does match -- and it is what separates the track of disc 1 from the SAME track on
	// disc 2 of a reissue.
	if disco <= 0 {
		return provider.FaixaExterna{}, false
	}
	var porDisco []provider.FaixaExterna
	for _, fx := range faixas {
		if discoDe(fx) == disco {
			porDisco = append(porDisco, fx)
		}
	}
	if len(porDisco) == 1 {
		return porDisco[0], true
	}
	return provider.FaixaExterna{}, false
}

// discoDe is the disc of the track in the source, with 0 treated as disc 1 (a file/single-disc
// release).
func discoDe(fx provider.FaixaExterna) int {
	if fx.DiscNumber <= 0 {
		return 1
	}
	return fx.DiscNumber
}

// releaseContemFaixaPorTitulo chooses the track with the BEST title similarity score against
// alvo.FaixaTitulo; it accepts when the score reaches matching.LimiarAutoMatch AND, in addition,
// when the duration gate confirms it (or is skipped because the duration is unknown on one of
// the sides, via duracoesCompativeis).
//
// Real releases may have 2+ tracks with the SAME title (e.g.: a remix/edit that kept the "clean"
// title in MusicBrainz) -- all of them tie on the highest score. In that case the duration
// becomes a TIEBREAK criterion (not only confirmation): among the tied ones, it only accepts
// when exactly 1 has a known duration AND inside the tolerance; 0 or 2+ plausible candidates
// keeps it ambiguous and rejects (it never chooses arbitrarily by iteration order).
func releaseContemFaixaPorTitulo(faixas []provider.FaixaExterna, alvo provider.AlvoFaixaAlbum) bool {
	melhorScore := -1.0
	var empatados []provider.FaixaExterna
	for _, fx := range faixas {
		score := matching.SimilaridadeNormalizada(alvo.FaixaTitulo, fx.Title)
		switch {
		case score > melhorScore:
			melhorScore = score
			empatados = []provider.FaixaExterna{fx}
		case score == melhorScore:
			empatados = append(empatados, fx)
		}
	}
	if melhorScore < matching.LimiarAutoMatch {
		return false
	}
	if len(empatados) == 1 {
		return duracoesCompativeis(alvo.DuracaoMs, empatados[0].DuracaoMs)
	}
	// 2+ tied at the top. The (disc, track) of the FILE breaks the tie first: in a reissue the
	// SAME track appears on disc 1 and on disc 2, and the position is what tells which of the two
	// the file is (the real case "The Hurting (Deluxe Edition)"). Only then does the duration come
	// in.
	if unico, ok := desempatePorDiscoEFaixa(empatados, alvo.FaixaDisco, alvo.FaixaNumero); ok {
		return duracoesCompativeis(alvo.DuracaoMs, unico.DuracaoMs)
	}
	// Without a usable position: the duration breaks the tie, it does not confirm. Without a
	// duration of the target there is no signal to decide -- it rejects (the same safe behavior as
	// when there is no duration).
	if alvo.DuracaoMs <= 0 {
		return false
	}
	compativeis := 0
	for _, fx := range empatados {
		if fx.DuracaoMs > 0 && duracoesCompativeis(alvo.DuracaoMs, fx.DuracaoMs) {
			compativeis++
		}
	}
	return compativeis == 1
}

// releaseContemFaixaPorNumero is the lowest-priority criterion of releaseContemFaixaAlvo: exact
// track_number.
func releaseContemFaixaPorNumero(faixas []provider.FaixaExterna, faixaNumero int) bool {
	for _, fx := range faixas {
		if fx.TrackNumber == faixaNumero {
			return true
		}
	}
	return false
}

// duracoesCompativeis applies the duration gate of round 2: an additional CONFIRMATION to the
// title match, it never makes the match more lenient. It only applies when BOTH sides report a
// known duration (> 0 on both) -- either one at 0 (unknown) skips the gate and returns true,
// preserving the behavior from before this change (no regression for files/data without a
// reliable duration).
func duracoesCompativeis(duracaoAlvoMs, duracaoCandidatoMs int) bool {
	if duracaoAlvoMs <= 0 || duracaoCandidatoMs <= 0 {
		return true
	}
	diff := duracaoAlvoMs - duracaoCandidatoMs
	if diff < 0 {
		diff = -diff
	}
	return diff <= int(matching.ToleranciaDuracaoSegundos*1000)
}

// duracaoMsDaFaixa chooses the duration (ms) to use for a track: it prioritizes the `length` of
// the TRACK inside the release; without it (<= 0), it falls back to the `length` of the nested
// RECORDING; without either (or negative), 0 (unknown).
func duracaoMsDaFaixa(trackLength, recordingLength int) int {
	duracaoMs := trackLength
	if duracaoMs <= 0 {
		duracaoMs = recordingLength
	}
	if duracaoMs < 0 {
		duracaoMs = 0
	}
	return duracaoMs
}

// buscarFaixas fetches the chosen release with its tracks (recordings), the artist-credit of each
// one (to detect a divergence from the album artist, ADR-0027 item 4) and the ISRCs.
func (c *Client) buscarFaixas(ctx context.Context, releaseID string) ([]provider.FaixaExterna, error) {
	body, err := c.getMB(ctx, "/release/"+url.PathEscape(releaseID), url.Values{"inc": {"recordings+artist-credits+isrcs"}})
	if err != nil {
		return nil, err
	}
	var rel respRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("musicbrainz: release invalido: %w", err)
	}

	discTotal := len(rel.Media)
	var faixas []provider.FaixaExterna
	for _, media := range rel.Media {
		for _, tr := range media.Tracks {
			credito := tr.Recording.ArtistCredit
			if len(credito) == 0 {
				credito = tr.ArtistCredit // defensive: the source rarely omits it on the recording
			}
			var creditoNome, creditoMBID string
			if len(credito) > 0 {
				creditoNome = credito[0].Artist.Name
				creditoMBID = credito[0].Artist.ID
			}
			var isrc string
			if len(tr.Recording.ISRCs) > 0 {
				isrc = tr.Recording.ISRCs[0]
			}
			titulo := tr.Recording.Title
			if titulo == "" {
				titulo = tr.Title
			}
			duracaoMs := duracaoMsDaFaixa(tr.Length, tr.Recording.Length)
			faixas = append(faixas, provider.FaixaExterna{
				Title:              titulo,
				MBID:               tr.Recording.ID,
				TrackNumber:        tr.Position,
				DiscNumber:         media.Position,
				DiscTotal:          discTotal,
				ISRC:               isrc,
				ArtistaCreditoNome: creditoNome,
				ArtistaCreditoMBID: creditoMBID,
				DuracaoMs:          duracaoMs,
			})
		}
	}
	return faixas, nil
}

// anoDe extracts the first 4 digits of a "YYYY-MM-DD"/"YYYY-MM"/"YYYY" date from
// MusicBrainz. It returns "" for dates that are too short or absent.
func anoDe(data string) string {
	if len(data) < 4 {
		return ""
	}
	return data[:4]
}

// generoPrincipal chooses the genre with the HIGHEST count (vote) from the list that
// MusicBrainz returns (ADR-0027 item 7: a single genre per album, not per track). On a tie: it
// keeps the order in which the source listed them (sort.SliceStable). An empty list returns
// "".
func generoPrincipal(generos []generoMB) string {
	if len(generos) == 0 {
		return ""
	}
	ordenados := make([]generoMB, len(generos))
	copy(ordenados, generos)
	sort.SliceStable(ordenados, func(i, j int) bool { return ordenados[i].Count > ordenados[j].Count })
	return ordenados[0].Name
}

// BuscarArtes lists the album cover through the Cover Art Archive (capability
// provider.ArteProvider). It only answers to `tipo=="album"` (other types are the territory of
// fanart.tv/TMDB). It uses ids.Mbid, which is the RELEASE-GROUP MBID (the same one stored in the
// `album` entity -- see the comment of ExternalIDs.Mbid in internal/provider/provider.go),
// against the /release-group/{mbid} endpoint of the CAA -- which the official documentation
// confirms returns the SAME JSON shape as /release/{mbid}, aggregating the art of any release of
// the group, without requiring the caller to choose a specific release. Without an id or without
// a match (404): (nil, nil), gracefully. A real error propagates.
func (c *Client) BuscarArtes(ctx context.Context, ids provider.ExternalIDs, tipo string, langs []string) ([]provider.Arte, error) {
	_ = langs
	if tipo != "album" || ids.Mbid == "" {
		return nil, nil
	}
	body, ok, err := c.getCAA(ctx, "/release-group/"+url.PathEscape(ids.Mbid))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var resp respCoverArt
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("musicbrainz: resposta do cover art archive invalida: %w", err)
	}

	var out []provider.Arte
	for _, img := range resp.Images {
		if img.Image == "" {
			continue
		}
		if img.Front {
			out = append(out, provider.Arte{Kind: "poster", URL: img.Image})
		}
		if img.Back {
			out = append(out, provider.Arte{Kind: "back", URL: img.Image})
		}
	}
	return out, nil
}

// ResolverArtistaDaFaixa resolves the artist MBID from the RECORDING MBID (capability
// provider.FaixaResolverProvider): it covers the path in which the file only has the track MBID
// embedded by the tagger (without the artist MBID), common enough to justify this 2nd round trip
// to the source. It uses the same rate limiter and User-Agent of getMB (1 call, without the
// extra cost of N albums as in BuscarArtistaArvore). ErrNaoEncontrado when the recording does not
// exist; a recording that exists but without ANY artist-credit (incomplete data in the source,
// extremely rare) is a generic error -- there is no artist at all to return.
func (c *Client) ResolverArtistaDaFaixa(ctx context.Context, recordingMBID string) (string, error) {
	body, err := c.getMB(ctx, "/recording/"+url.PathEscape(recordingMBID), url.Values{"inc": {"artist-credits"}})
	if err != nil {
		return "", err
	}
	var rec struct {
		ArtistCredit []creditoMB `json:"artist-credit"`
	}
	if err := json.Unmarshal(body, &rec); err != nil {
		return "", fmt.Errorf("musicbrainz: recording invalido: %w", err)
	}
	if len(rec.ArtistCredit) == 0 || rec.ArtistCredit[0].Artist.ID == "" {
		return "", fmt.Errorf("musicbrainz: recording %s sem artist-credit", recordingMBID)
	}
	return rec.ArtistCredit[0].Artist.ID, nil
}

// tentativasMB is the MAXIMUM number of attempts of getMB per call -- the public MusicBrainz
// server returns 503 "currently busy" with a real frequency observed in production (~1 in 3
// calls in the validation of this vertical), and a whole artist tree makes 1+2N sequential
// requests (N albums): without retry, the probability of the WHOLE sequence working drops close
// to zero for an artist with a large discography.
const tentativasMB = 3

// backoffMB is the delay between attempts on top of a 502/503/504 -- multiplied by the attempt
// number (the 1st retry waits backoffMB, the 2nd waits 2×backoffMB).
const backoffMB = 500 * time.Millisecond

// getMB performs a RATE-LIMITED GET (1 req/s) to musicbrainz.org with the mandatory User-Agent
// and `fmt=json` always present. A 404 becomes the sentinel error provider.ErrNaoEncontrado
// (errors.Is), without retry (it is not transient). 502/503/504 (documented instability of the
// public server) try again up to tentativasMB times, with a growing backoff; any other status !=
// 200 is a generic error without retry. I/O errors propagate without retry (an unstable local
// network is not the problem this retry solves).
func (c *Client) getMB(ctx context.Context, path string, params url.Values) ([]byte, error) {
	var ultimoErr error
	for tentativa := 1; tentativa <= tentativasMB; tentativa++ {
		if tentativa > 1 {
			c.lim.dormirFn(backoffMB * time.Duration(tentativa-1))
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		body, status, err := c.tentarGetMB(ctx, path, params)
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			return nil, fmt.Errorf("musicbrainz: %s: %w", path, provider.ErrNaoEncontrado)
		}
		if status == http.StatusOK {
			return body, nil
		}
		if !transitorioMB(status) {
			return nil, fmt.Errorf("musicbrainz: status inesperado %d em %s", status, path)
		}
		ultimoErr = fmt.Errorf("musicbrainz: status inesperado %d em %s", status, path)
	}
	return nil, fmt.Errorf("musicbrainz: %s: esgotou %d tentativas: %w", path, tentativasMB, ultimoErr)
}

// transitorioMB reports whether `status` is the kind of instability of the public MusicBrainz
// server that is worth retrying (502/503/504) -- never a 4xx, which is an error of the request
// itself and does not go away on a second attempt.
func transitorioMB(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// tentarGetMB makes ONE attempt of the GET (without retry) and returns the raw status for the
// caller to decide -- separate from getMB only to isolate the retry loop from the assembly of
// the request.
func (c *Client) tentarGetMB(ctx context.Context, path string, params url.Values) ([]byte, int, error) {
	c.lim.esperar()

	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, 0, err
	}
	q := params
	if q == nil {
		q = url.Values{}
	}
	q.Set("fmt", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// An error body does not interest us (the message is already generic enough in the two
		// statuses handled by getMB), but it must be drained for the keep-alive of the connection.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, resp.StatusCode, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("musicbrainz: ler %s: %w", path, err)
	}
	return body, resp.StatusCode, nil
}

// getCAA performs a GET to the Cover Art Archive -- WITHOUT rate limiting (the source does not
// document a limit; see the package comment), but with the same User-Agent as good practice.
// ok=false on a 404 (release/release-group without art, or a nonexistent mbid -- the CAA itself
// does not distinguish the two cases; it is not an error). A status != 200/404 tries again
// (retry, see tentarGetCAA) -- a real finding in the production validation: the redirect of the
// CAA to archive.org (dnNNNNNN.ca.archive.org, a distributed CDN) gave "context deadline
// exceeded" in a good part of the calls of a 25-album import, without a pattern of HTTP status
// (it is a network timeout, not a 5xx) -- different from the "busy" 503 of MusicBrainz in
// getMB.
func (c *Client) getCAA(ctx context.Context, path string) ([]byte, bool, error) {
	var ultimoErr error
	for tentativa := 1; tentativa <= tentativasMB; tentativa++ {
		if tentativa > 1 {
			c.lim.dormirFn(backoffMB * time.Duration(tentativa-1))
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
		}
		body, ok, err := c.tentarGetCAA(ctx, path)
		if err == nil {
			return body, ok, nil
		}
		ultimoErr = err
	}
	return nil, false, fmt.Errorf("musicbrainz: %s: esgotou %d tentativas (cover art archive): %w", path, tentativasMB, ultimoErr)
}

// tentarGetCAA makes ONE attempt of the GET to the Cover Art Archive (without retry).
func (c *Client) tentarGetCAA(ctx context.Context, path string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.caaBaseURL+path, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, false, fmt.Errorf("musicbrainz: status inesperado %d em %s (cover art archive)", resp.StatusCode, path)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("musicbrainz: ler %s (cover art archive): %w", path, err)
	}
	return body, true, nil
}

// respBuscaArtista is the response of GET /artist?query=...&fmt=json (only the fields used).
type respBuscaArtista struct {
	Artists []struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Disambiguation string `json:"disambiguation"`
	} `json:"artists"`
}

// respArtistaArvore is the response of GET /artist/{id}?inc=release-groups+annotation&fmt=json
// (BuscarArtistaArvore) OR GET /artist/{id}?inc=release-groups&fmt=json (BuscarAlbumPorNome --
// annotation is not requested on that path, the field stays zero-value, no problem).
type respArtistaArvore struct {
	Name          string                  `json:"name"`
	Annotation    string                  `json:"annotation"`
	ReleaseGroups []respReleaseGroupBreve `json:"release-groups"`
}

// respReleaseGroupBreve is a SUMMARIZED release-group, as listed inside the artist tree
// (inc=release-groups) -- ids/titles, without tracks (BuscarArtistaArvore iterates and assembles
// each one through montarAlbum; BuscarAlbumPorNome chooses ONE by title and assembles only that
// one, see melhorReleaseGroupPorTitulo).
type respReleaseGroupBreve struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	FirstReleaseDate string `json:"first-release-date"`
}

// respReleaseComGrupo is a release of the recording (inc=releases+release-groups), with the
// release-group NESTED -- used only by BuscarAlbumDaFaixa/escolherReleaseGroupDoRecording to
// resolve the album of a recording without a 2nd round trip per release.
type respReleaseComGrupo struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ReleaseGroup struct {
		ID string `json:"id"`
	} `json:"release-group"`
}

// respRecordingReleases is the response of GET /recording/{id}?inc=releases+release-groups&fmt=json
// (only the field used by BuscarAlbumDaFaixa).
type respRecordingReleases struct {
	Releases []respReleaseComGrupo `json:"releases"`
}

// generoMB is a genre with its count (vote), as MusicBrainz returns it in `inc=genres`.
type generoMB struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// respReleaseResumo is a summarized release, as listed inside a release-group (inc=releases) --
// just enough for escolherRelease.
type respReleaseResumo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// respReleaseGroup is the response of GET /release-group/{id}?inc=releases+genres&fmt=json.
// Title/FirstReleaseDate are CORE attributes of the release-group (the source always returns
// them, even without requesting them through inc) -- used by montarAlbum as a fallback when the
// caller does not have them beforehand (BuscarAlbumDaFaixa does not browse the artist
// first).
type respReleaseGroup struct {
	Title            string              `json:"title"`
	FirstReleaseDate string              `json:"first-release-date"`
	Genres           []generoMB          `json:"genres"`
	Releases         []respReleaseResumo `json:"releases"`
}

// creditoMB is an artist-credit entry (MusicBrainz models it as a list, to join several artists
// with joinphrase; we use only the first one -- the main credited artist).
type creditoMB struct {
	Artist struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

// respRelease is the response of GET /release/{id}?inc=recordings+artist-credits+isrcs&fmt=json.
type respRelease struct {
	Media []struct {
		Position int `json:"position"`
		Tracks   []struct {
			Position     int         `json:"position"`
			Title        string      `json:"title"`
			Length       int         `json:"length"`
			ArtistCredit []creditoMB `json:"artist-credit"`
			Recording    struct {
				ID           string      `json:"id"`
				Title        string      `json:"title"`
				Length       int         `json:"length"`
				ISRCs        []string    `json:"isrcs"`
				ArtistCredit []creditoMB `json:"artist-credit"`
			} `json:"recording"`
		} `json:"tracks"`
	} `json:"media"`
}

// respCoverArt is the response of GET /release-group/{id} (or /release/{id}) on the Cover Art
// Archive: only the fields used (front/back/image) -- thumbnails/approved/comment/edit/id are
// ignored (the full-size URL in `image` is what the other ArteProvider of the repo also
// expose).
type respCoverArt struct {
	Images []struct {
		Front bool   `json:"front"`
		Back  bool   `json:"back"`
		Image string `json:"image"`
	} `json:"images"`
}

// ── Album search by TITLE (compilations) ──────────────────────────────────────

// maxReleaseGroupsPorTitulo is the ceiling of release-groups assembled per attempt in
// BuscarAlbumPorTitulo: the textual search returns many homonyms, and each one costs 1 request
// (rate limit of 1 req/s) + the tracklist search. Only the best placed by title similarity get
// in.
const maxReleaseGroupsPorTitulo = 3

// BuscarAlbumPorTitulo implements provider.MusicaAlbumPorTituloProvider: it resolves ONE album
// by its TITLE, without starting from an artist -- the path needed by compilations/soundtracks,
// where `album_artist` is "Various Artists" and the artist search of the normal pipeline does not
// return a unique identity.
//
// It makes ONE release-group search (`/release-group?query=releasegroup:"..."`), sorts the
// candidates by matching.SimilaridadeNormalizada (the same engine/threshold as the rest of the
// music matching) and assembles up to maxReleaseGroupsPorTitulo of them, accepting the first one
// whose tracklist contains the target track (releaseContemFaixaAlvo). It also returns the
// artist-credit of the winning release-group -- it is from that credit that the main artist comes
// (ADR-0029 item 2) when the compilation is imported.
func (c *Client) BuscarAlbumPorTitulo(ctx context.Context, nomeAlbum string, alvo provider.AlvoFaixaAlbum, langs []string) (provider.AlbumComArtista, bool, error) {
	_ = langs
	if nomeAlbum == "" {
		return provider.AlbumComArtista{}, false, fmt.Errorf("musicbrainz: nome de album vazio")
	}
	var ultimoErr error
	for _, consulta := range c.consultasDeBuscaDoAlbum(nomeAlbum, alvo.ArtistaNome) {
		if err := ctx.Err(); err != nil {
			return provider.AlbumComArtista{}, false, err
		}
		rgs, err := c.buscarReleaseGroups(ctx, consulta)
		if err != nil {
			ultimoErr = err
			continue
		}
		candidatos := ordenarCandidatosDeAlbum(rgs, nomeAlbum)
		if len(candidatos) == 0 {
			continue
		}
		ac, achou, err := c.tentarCandidatosDeAlbum(ctx, candidatos, alvo)
		if err != nil {
			return provider.AlbumComArtista{}, false, err
		}
		if achou {
			return ac, true, nil
		}
	}
	return provider.AlbumComArtista{}, false, ultimoErr
}

// tentarCandidatosDeAlbum tries up to maxReleaseGroupsPorTitulo candidates (the ones with the
// highest title similarity, above the threshold) and returns the first one whose release contains
// the target track.
func (c *Client) tentarCandidatosDeAlbum(ctx context.Context, candidatos []rgBuscado, alvo provider.AlvoFaixaAlbum) (provider.AlbumComArtista, bool, error) {
	tentados := 0
	for _, cand := range candidatos {
		if tentados >= maxReleaseGroupsPorTitulo {
			break
		}
		if err := ctx.Err(); err != nil {
			return provider.AlbumComArtista{}, false, err
		}
		tentados++
		album := c.montarAlbum(ctx, cand.rg.ID, cand.rg.Title, cand.rg.FirstReleaseDate, alvo)
		if !releaseContemFaixaAlvo(album.Faixas, alvo) {
			continue
		}
		out := provider.AlbumComArtista{Album: album}
		if artista, ok := artistaPrincipalDoReleaseGroup(cand.rg); ok {
			out.ArtistaMBID = artista.MBID
			out.ArtistaNome = artista.Nome
		}
		return out, true, nil
	}
	return provider.AlbumComArtista{}, false, nil
}

// ordenarCandidatosDeAlbum computes the similarity score of each returned release-group against
// the queried name and sorts from best to worst.
func ordenarCandidatosDeAlbum(rgs []respReleaseGroupBusca, nomeAlbum string) []rgBuscado {
	candidatos := make([]rgBuscado, 0, len(rgs))
	for _, rg := range rgs {
		if rg.ID == "" || rg.Title == "" {
			continue
		}
		candidatos = append(candidatos, rgBuscado{
			rg:    rg,
			score: matching.SimilaridadeNormalizada(nomeAlbum, rg.Title),
		})
	}
	sort.SliceStable(candidatos, func(i, j int) bool { return candidatos[i].score > candidatos[j].score })
	return candidatos
}

// consultasDeBuscaDoAlbum builds, in ORDER of preference, the textual searches of the path by
// TITLE. The first two variants compare with the NORMALIZED name because the tag carries an
// edition suffix that the canonical title of the source does not have; the last one uses the RAW
// name, because there are editions that are a release-group of their OWN in the source ("Hybrid
// Theory (Bonus Edition)", "Notorious (Deluxe Edition)") -- in those cases the normalized phrase
// never returns the right release-group, and it is this variant that finds it.
//
// The artist of the album scopes the normalized variants: without it, the search for a common
// title returns homonyms of other artists and the right album stays out of the top (a real case:
// "Rio (Collector's Edition)" brought ten release-groups called "Collector's Edition"). A
// compilation is not scoped (the generic credit filters nothing). The second artist variant uses
// only the FIRST name when the tag brings more than one separated by "/" ("Flume/Chet Faker" is
// not a credit of the source, which uses "&"/"and").
func (c *Client) consultasDeBuscaDoAlbum(nomeAlbum, artista string) []string {
	normalizado := matching.NormalizarNome(nomeAlbum)
	consultas := make([]string, 0, 4)
	if !artistaDeCompilacao(artista) {
		for _, alt := range alternativasDeArtista(artista) {
			consultas = append(consultas, fraseReleaseGroup(normalizado)+` AND artistname:`+fraseArtista(alt))
		}
	}
	consultas = append(consultas, fraseReleaseGroup(normalizado))
	if normalizado != nomeAlbum {
		consultas = append(consultas, fraseReleaseGroup(nomeAlbum))
	}
	return consultas
}

// alternativasDeArtista returns the name of the artist of the album as it came and, when the tag
// joins more than one artist with "/", only the first one -- the credit of the source uses
// "&"/"and"/", ", never the slash (a real case "Flume/Chet Faker").
func alternativasDeArtista(artista string) []string {
	artista = strings.TrimSpace(artista)
	if artista == "" {
		return nil
	}
	out := []string{artista}
	if i := strings.IndexAny(artista, "/"); i > 0 {
		if primeiro := strings.TrimSpace(artista[:i]); primeiro != "" && primeiro != artista {
			out = append(out, primeiro)
		}
	}
	return out
}

// buscarReleaseGroups executes ONE textual search of release-group.
func (c *Client) buscarReleaseGroups(ctx context.Context, query string) ([]respReleaseGroupBusca, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", "10")
	body, err := c.getMB(ctx, "/release-group", params)
	if err != nil {
		return nil, err
	}
	var resp respBuscaReleaseGroup
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("musicbrainz: busca de release-group invalida: %w", err)
	}
	return resp.ReleaseGroups, nil
}

// fraseReleaseGroup builds the title clause as a PHRASE (in quotes): without the quotes Lucene
// treats each word as an independent term and an edition suffix ("Deluxe Edition") ends up
// weighing more than the name of the album.
func fraseReleaseGroup(nome string) string {
	return `releasegroup:"` + colapsarEspacos(escaparLucene(nome)) + `"`
}

// fraseArtista builds the artist clause as a phrase.
func fraseArtista(nome string) string {
	return `"` + colapsarEspacos(escaparLucene(nome)) + `"`
}

// colapsarEspacos joins repeated spaces: escaparLucene replaces each punctuation mark with a
// space, and a Lucene phrase with a double space stops matching (measured:
// releasegroup:"Rule The World  The Greatest Hits" returns 1, but with artistname together it
// returns 0).
func colapsarEspacos(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// artistaDeCompilacao reports whether the name is the generic compilation label -- in that case
// the album search by title does NOT scope by artist, because the credit of the source is exactly
// the generic one and the title is the only available signal.
func artistaDeCompilacao(nome string) bool {
	cru := strings.ToLower(strings.TrimSpace(nome))
	switch cru {
	case "various artists", "various", "va", "varios artistas", "vários artistas",
		"artistas variados", "compilation", "compilacao", "compilação",
		"разные исполнители", "исполнители разные":
		return true
	}
	norm := matching.NormalizarNome(cru)
	return norm == "various artists" || norm == "varios artistas" || norm == "various"
}

// rgBuscado is a release-group from the textual search with the score already computed against
// the queried name.
type rgBuscado struct {
	rg    respReleaseGroupBusca
	score float64
}

// artistaPrincipalDoReleaseGroup extracts the FIRST artist-credit of the release-group (the
// "artist" of the album in the source) -- in MusicBrainz, a compilation has the single
// artist-credit "Various Artists". ok=false when the search did not bring an artist-credit
// (incomplete data in the source).
func artistaPrincipalDoReleaseGroup(rg respReleaseGroupBusca) (provider.ArtistaExterno, bool) {
	for _, credito := range rg.ArtistCredit {
		if credito.Artist.ID != "" {
			nome := credito.Artist.Name
			if nome == "" {
				nome = credito.Name
			}
			return provider.ArtistaExterno{MBID: credito.Artist.ID, Nome: nome}, true
		}
	}
	return provider.ArtistaExterno{}, false
}

// escaparLucene neutralizes the characters that the search syntax of MusicBrainz (Lucene) treats
// as an operator, preventing a real title ("Right Here, Right Now", "Muse - Live") from becoming
// a malformed query. Only the subset that appears in real titles is handled; quotes and backslash
// are removed, the rest becomes a space.
func escaparLucene(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteRune(' ')
		case '+', '-', '&', '|', '!', '(', ')', '{', '}', '[', ']', '^', '~', '*', '?', ':':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// respBuscaReleaseGroup is the response of GET /release-group?query=...&fmt=json (textual search
// of release-group, used only by BuscarAlbumPorTitulo).
type respBuscaReleaseGroup struct {
	ReleaseGroups []respReleaseGroupBusca `json:"release-groups"`
}

// respReleaseGroupBusca is a release-group from the SEARCH (not from the detail): it brings the
// score of the source itself, the release date and the artist-credit with the id/name of the
// artist -- it is from that credit that the main artist of a compilation comes.
type respReleaseGroupBusca struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	FirstReleaseDate string `json:"first-release-date"`
	Score            int    `json:"score"`
	ArtistCredit     []struct {
		Name   string `json:"name"`
		Artist struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"artist-credit"`
}
