package tmdb

// This file implements the provider.SerieProvider capability (ADR-0021): the TREE
// of the series built FROM TMDB -- seasons + fine-grained episodes anchored on
// episode.id, each one with the numbering of the "official" order (the only one
// TMDB models: it does not expose season types like TheTVDB). This guarantees
// PROVIDER INDEPENDENCE: with only the TMDB key configured, series still import
// (TVDB stops being the mandatory source of the tree).
//
// PATH: /tv/{id} brings the LIST of seasons (seasons[]); /tv/{id}/season/{n}
// brings the episodes of that season. The i18n mirrors TVDB -- the base content
// comes out under the original language of the series and each requested language
// is fetched with ?language= and merged by episode.id (without N calls per
// episode). TMDB accepts our app codes (pt-BR/en) straight in ?language=, so there
// is no code table as in TVDB.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// ordemOficial is the only ordering (season type) that TMDB models: episodes are
	// numbered by the official (season_number, episode_number), without alternative
	// orders.
	ordemOficial = "official"
	// stillBaseURL is the base of the TMDB stills (episode frame), in a landscape
	// size (w300) -- the raw URL goes to extension_data and the image proxy fetches/
	// caches it on demand on the first view, never during the import.
	stillBaseURL = "https://image.tmdb.org/t/p/w300"
)

// BuscarSerieArvore satisfies provider.SerieProvider: it returns the tree of the
// series `sourceID` in the `langs` languages (with fallback). The DEFAULT order is the
// official one — it is the only one that becomes season entities; the alternative
// orders (episode groups, tmdb_grupos.go) come as extra `numbering` per episode, as
// ADR-0021 prescribes. ALL the seasons of /tv (including season 0/specials) become
// TemporadaExterna. ErrNaoEncontrado (404) and network errors propagate.
func (c *Client) BuscarSerieArvore(ctx context.Context, sourceID string, langs []string) (provider.SerieArvore, error) {
	if sourceID == "" {
		return provider.SerieArvore{}, fmt.Errorf("tmdb: sourceID vazio")
	}
	d, err := c.serieParaArvore(ctx, sourceID)
	if err != nil {
		return provider.SerieArvore{}, err
	}
	temporadas := make([]provider.TemporadaExterna, 0, len(d.Seasons))
	for _, resumo := range d.Seasons {
		temp, err := c.montarTemporadaTMDB(ctx, sourceID, resumo, d.OriginalLanguage, langs)
		if err != nil {
			return provider.SerieArvore{}, err
		}
		temporadas = append(temporadas, temp)
	}
	// Alternative orderings (TMDB episode groups) are merged into the numbering of
	// the episodes already built: the tree stays anchored on the official structure
	// and each group adds one more entry in `numbering` (tmdb_grupos.go).
	ordens := []string{ordemOficial}
	ordens = append(ordens, c.mesclarGruposTMDB(ctx, sourceID, temporadas)...)
	return provider.SerieArvore{
		DefaultSeasonType: ordemOficial,
		SeasonTypes:       ordens,
		Temporadas:        temporadas,
	}, nil
}

// respSerieArvoreTMDB mirrors /tv/{id} for the tree (only the fields used): the
// original language (anchor of the fallback) and the list of seasons (number, name,
// year, poster).
type respSerieArvoreTMDB struct {
	OriginalLanguage string                `json:"original_language"`
	Seasons          []temporadaResumoTMDB `json:"seasons"`
}

// temporadaResumoTMDB is a season as it comes in the seasons[] of /tv (a summary,
// without episodes) -- the episodes come from /tv/{id}/season/{n}.
type temporadaResumoTMDB struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	AirDate      string `json:"air_date"`
	PosterPath   string `json:"poster_path"`
}

// respTemporadaTMDB mirrors /tv/{id}/season/{n}: the localized name/year/poster of
// the season and the list of episodes (with the content in the requested language
// in ?language=).
type respTemporadaTMDB struct {
	Name       string         `json:"name"`
	AirDate    string         `json:"air_date"`
	PosterPath string         `json:"poster_path"`
	Episodes   []episodioTMDB `json:"episodes"`
}

// episodioTMDB is a raw episode of /season/{n}. name/overview come in the language
// of ?language= (empty when there is no translation in that language).
type episodioTMDB struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Overview      string `json:"overview"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
	AirDate       string `json:"air_date"`
	StillPath     string `json:"still_path"`
	Runtime       int    `json:"runtime"`
}

// serieParaArvore pulls /tv/{id} and returns the list of seasons + original
// language. ErrNaoEncontrado (via getBytes) propagates so the handler answers 404.
func (c *Client) serieParaArvore(ctx context.Context, sourceID string) (respSerieArvoreTMDB, error) {
	body, err := c.getBytes(ctx, "/tv/"+url.PathEscape(sourceID), nil)
	if err != nil {
		return respSerieArvoreTMDB{}, err
	}
	var d respSerieArvoreTMDB
	if err := json.Unmarshal(body, &d); err != nil {
		return respSerieArvoreTMDB{}, fmt.Errorf("tmdb: serie invalida: %w", err)
	}
	return d, nil
}

// montarTemporadaTMDB builds a TemporadaExterna: it pulls the season in the
// ORIGINAL language (the base/anchor), builds the episodes and merges the
// translations of the requested languages by episode.id. An error in the base
// season propagates (the tree is incomplete without it); translations that fail
// degrade (the language stays absent from the map).
func (c *Client) montarTemporadaTMDB(ctx context.Context, serieID string, resumo temporadaResumoTMDB, origLang string, langs []string) (provider.TemporadaExterna, error) {
	base, err := c.buscarTemporadaTMDB(ctx, serieID, resumo.SeasonNumber, origLang)
	if err != nil {
		return provider.TemporadaExterna{}, err
	}
	episodios, indice := montarEpisodiosTMDB(base.Episodes, origLang)
	nomes := map[string]string{}
	setSeAusenteStr(nomes, origLang, base.Name)
	c.mesclarTemporadaTraduzida(ctx, serieID, resumo.SeasonNumber, langs, origLang, indice, nomes)
	return provider.TemporadaExterna{
		Ordering:     ordemOficial,
		SeasonNumber: resumo.SeasonNumber,
		Name:         nomes,
		Year:         anoTemporada(resumo, base),
		PosterURL:    imagemDireta(imagemBaseURL, primeiroNaoVazio(resumo.PosterPath, base.PosterPath)),
		Episodios:    episodios,
	}, nil
}

// buscarTemporadaTMDB pulls /tv/{id}/season/{n} localized in `lang` (empty falls
// back to the default of the source). Network/parse errors propagate.
func (c *Client) buscarTemporadaTMDB(ctx context.Context, serieID string, n int, lang string) (respTemporadaTMDB, error) {
	var params url.Values
	if lang != "" {
		params = url.Values{"language": {lang}}
	}
	body, err := c.getBytes(ctx, "/tv/"+url.PathEscape(serieID)+"/season/"+strconv.Itoa(n), params)
	if err != nil {
		return respTemporadaTMDB{}, err
	}
	var sd respTemporadaTMDB
	if err := json.Unmarshal(body, &sd); err != nil {
		return respTemporadaTMDB{}, fmt.Errorf("tmdb: temporada invalida: %w", err)
	}
	return sd, nil
}

// montarEpisodiosTMDB creates the EpisodioExterno of the season, preserving the
// order of the source, and returns the id→*episode index (to merge the
// translations). The "official" numbering already comes in; the base content goes
// under the original language of the series.
//
// The index stores pointers INTO the `episodios` slice: that is only safe because
// the capacity is reserved at len(eps) and the appends never exceed it (no
// reallocation -- the pointers stay valid). Keep that invariant when touching
// this code.
func montarEpisodiosTMDB(eps []episodioTMDB, origLang string) ([]provider.EpisodioExterno, map[int64]*provider.EpisodioExterno) {
	episodios := make([]provider.EpisodioExterno, 0, len(eps))
	indice := make(map[int64]*provider.EpisodioExterno, len(eps))
	for _, e := range eps {
		ep := provider.EpisodioExterno{
			SourceID:    itoa(e.ID),
			ExternalIDs: map[string]string{"tmdb": itoa(e.ID)},
			Title:       map[string]string{},
			Overview:    map[string]string{},
			Numbering:   map[string]provider.Numero{ordemOficial: {Season: e.SeasonNumber, Number: e.EpisodeNumber}},
			AirDate:     e.AirDate,
			StillURL:    imagemDireta(stillBaseURL, e.StillPath),
		}
		if e.Runtime != 0 {
			rt := e.Runtime
			ep.Runtime = &rt
		}
		setSeAusenteStr(ep.Title, origLang, e.Name)
		setSeAusenteStr(ep.Overview, origLang, e.Overview)
		episodios = append(episodios, ep)
		indice[e.ID] = &episodios[len(episodios)-1]
	}
	return episodios, indice
}

// mesclarTemporadaTraduzida fetches the season in each requested language (≠ the
// original) and merges name/overview by episode.id, besides the NAME of the season.
// Best-effort: a language that fails is skipped (fallback = absent from the map).
// Repeated languages and the original one are skipped.
func (c *Client) mesclarTemporadaTraduzida(ctx context.Context, serieID string, n int, langs []string, origLang string, indice map[int64]*provider.EpisodioExterno, nomes map[string]string) {
	vistos := map[string]bool{origLang: true}
	for _, lang := range langs {
		if lang == "" || vistos[lang] {
			continue
		}
		vistos[lang] = true
		sd, err := c.buscarTemporadaTMDB(ctx, serieID, n, lang)
		if err != nil {
			continue
		}
		chaves := codigosAppTMDB(lang)
		for _, chave := range chaves {
			setSeAusenteStr(nomes, chave, sd.Name)
		}
		aplicarTraducaoTMDB(sd.Episodes, chaves, indice)
	}
}

// aplicarTraducaoTMDB writes the non-empty name/overview of each translated episode
// under the app language keys (combined + base).
func aplicarTraducaoTMDB(eps []episodioTMDB, chaves []string, indice map[int64]*provider.EpisodioExterno) {
	for _, e := range eps {
		ep, ok := indice[e.ID]
		if !ok {
			continue
		}
		for _, chave := range chaves {
			setSeAusenteStr(ep.Title, chave, e.Name)
			setSeAusenteStr(ep.Overview, chave, e.Overview)
		}
	}
}

// anoTemporada extracts the year (4 digits) from the air_date of the season (from
// the summary or from the detail). "" when the source did not provide a date.
func anoTemporada(resumo temporadaResumoTMDB, base respTemporadaTMDB) string {
	data := primeiroNaoVazio(resumo.AirDate, base.AirDate)
	if len(data) < 4 {
		return ""
	}
	return data[:4]
}

// codigosAppTMDB returns the app keys under which to write a translation of `lang`:
// the code itself (e.g. "pt-BR") and the base one (e.g. "pt"), so the resolver
// matches both the combined and the base -- the same convention as the rest of the
// provider.
func codigosAppTMDB(lang string) []string {
	if lang == "" {
		return nil
	}
	base := strings.SplitN(lang, "-", 2)[0]
	if base == lang || base == "" {
		return []string{lang}
	}
	return []string{lang, base}
}

// imagemDireta builds the direct URL of a TMDB image (base + path), or "" when
// there is no path.
func imagemDireta(baseURL, path string) string {
	if path == "" {
		return ""
	}
	return baseURL + path
}

// setSeAusenteStr writes a non-empty value under a non-empty key only if it does not exist yet.
func setSeAusenteStr(m map[string]string, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}
