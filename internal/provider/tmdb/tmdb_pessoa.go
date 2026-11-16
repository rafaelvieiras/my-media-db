package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// This file implements the PERSON filmography CAPABILITY of the provider
// (provider.PessoaProvider, second section "not yet imported"): GET
// {base}/person/{id}?append_to_response=combined_credits in a single round trip
// (TMDB already merges movie+series in combined_credits) and the mapping of the
// credits (cast+crew) to the canonical CreditoFilmografia. The handler crosses the
// result against the catalog (internal-first) and discards what was already
// imported; it is up to us only to deliver a LEAN set: dedup by work
// (Type+SourceID, cast preferred), sorted by descending year (without a year last)
// and cut at a ceiling -- real filmographies of prolific people add up to hundreds
// of credits, and this is a PREVIEW, not a complete catalog.

const (
	// posterFilmografiaBaseURL is the poster base in the external filmography items --
	// smaller (w185) than the card poster (w342, imagemBaseURL) because it is
	// potentially a long list; the server still proxies each URL like the others.
	posterFilmografiaBaseURL = "https://image.tmdb.org/t/p/w185"
	// tetoFilmografiaExterna is the ceiling of credits returned by BuscarFilmografia,
	// already after dedup and sorting.
	tetoFilmografiaExterna = 40

	// kindCastExterno/kindCrewExterno are the values of the role in the external
	// filmography (the same pair kind "cast"/"crew" of the internal filmography, see
	// server/person.go).
	kindCastExterno = "cast"
	kindCrewExterno = "crew"
)

// pessoaDetalheTMDB mirrors GET /person/{id}?append_to_response=combined_credits
// (only the fields used). The `id` of the person itself does not interest us here
// (we already have it through sourceID); only the combined credits.
type pessoaDetalheTMDB struct {
	CombinedCredits combinedCreditsTMDB `json:"combined_credits"`
}

// combinedCreditsTMDB is the combined_credits block: cast and crew already merging
// movie+series (media_type discriminates). The SAME work may appear in both lists
// when the person acted AND was also part of the crew.
type combinedCreditsTMDB struct {
	Cast []creditoCombinadoTMDB `json:"cast"`
	Crew []creditoCombinadoTMDB `json:"crew"`
}

// creditoCombinadoTMDB is a raw item of combined_credits.{cast,crew}. title/
// release_date are movie ones; name/first_air_date series ones (same pattern as
// itemMulti); character only makes sense in cast, job/department only in crew.
type creditoCombinadoTMDB struct {
	ID           int64  `json:"id"`
	MediaType    string `json:"media_type"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	Character    string `json:"character"`
	Job          string `json:"job"`
}

// BuscarFilmografia pulls the person `sourceID` with combined_credits in `lang` and
// maps it to the canonical (dedup+sorting+cut in montarFilmografiaExterna, PURE). A
// 404 from TMDB becomes provider.ErrNaoEncontrado (via getBytes); a real error
// propagates. Satisfies provider.PessoaProvider.
func (c *Client) BuscarFilmografia(ctx context.Context, sourceID, lang string) ([]provider.CreditoFilmografia, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("tmdb: sourceID vazio")
	}
	params := url.Values{"append_to_response": {"combined_credits"}}
	if lang != "" {
		params.Set("language", lang)
	}
	body, err := c.getBytes(ctx, "/person/"+url.PathEscape(sourceID), params)
	if err != nil {
		return nil, err
	}
	var d pessoaDetalheTMDB
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("tmdb: filmografia invalida: %w", err)
	}
	return montarFilmografiaExterna(d.CombinedCredits), nil
}

// montarFilmografiaExterna deduplicates the combined credits by work
// (Type+SourceID) -- cast ALWAYS wins over crew (the more prominent role); when
// there are only crew credit(s) for the work, the distinct jobs are joined
// (" · ", without duplicating). The result is sorted by descending year (without a
// year last) and cut at tetoFilmografiaExterna. It is PURE (no network) -- separate
// from BuscarFilmografia to keep the I/O function simple and this mapping testable
// without httptest.
func montarFilmografiaExterna(cc combinedCreditsTMDB) []provider.CreditoFilmografia {
	porObra := make(map[string]*provider.CreditoFilmografia, len(cc.Cast)+len(cc.Crew))
	ordem := make([]string, 0, len(cc.Cast)+len(cc.Crew))

	for _, it := range cc.Cast {
		chave := chaveObraExterna(it)
		if chave == "" || porObra[chave] != nil {
			continue // without a usable type, or the work already has a cast credit (the 1st occurrence wins)
		}
		credito := mapearCreditoCombinado(it, kindCastExterno)
		porObra[chave] = &credito
		ordem = append(ordem, chave)
	}
	for _, it := range cc.Crew {
		chave := chaveObraExterna(it)
		if chave == "" {
			continue
		}
		if existente, ok := porObra[chave]; ok {
			if existente.Kind == kindCastExterno {
				continue // cast already won this work
			}
			existente.Job = juntarCargoExterno(existente.Job, it.Job)
			continue
		}
		credito := mapearCreditoCombinado(it, kindCrewExterno)
		porObra[chave] = &credito
		ordem = append(ordem, chave)
	}

	out := make([]provider.CreditoFilmografia, 0, len(ordem))
	for _, chave := range ordem {
		out = append(out, *porObra[chave])
	}
	ordenarFilmografiaExterna(out)
	if len(out) > tetoFilmografiaExterna {
		out = out[:tetoFilmografiaExterna]
	}
	return out
}

// chaveObraExterna builds the dedup key "type|source_id" of a combined credit, or
// "" when the media_type is not movie/tv (the credit is discarded).
func chaveObraExterna(it creditoCombinadoTMDB) string {
	if it.MediaType != "movie" && it.MediaType != "tv" {
		return ""
	}
	return it.MediaType + "|" + itoa(it.ID)
}

// mapearCreditoCombinado converts a raw item to the canonical, with character
// filled in only in cast and job only in crew (the pair that does not apply to the
// kind stays nil).
func mapearCreditoCombinado(it creditoCombinadoTMDB, kind string) provider.CreditoFilmografia {
	credito := provider.CreditoFilmografia{
		Source:    "tmdb",
		SourceID:  itoa(it.ID),
		Type:      it.MediaType,
		Title:     primeiroNaoVazio(it.Title, it.Name),
		Year:      anoDe(primeiroNaoVazio(it.ReleaseDate, it.FirstAirDate)),
		PosterURL: posterFilmografiaURL(it.PosterPath),
		Kind:      kind,
	}
	if kind == kindCastExterno {
		credito.Character = ptrNaoVazio(it.Character)
	} else {
		credito.Job = ptrNaoVazio(it.Job)
	}
	return credito
}

// juntarCargoExterno appends `novo` to the " · "-separated list of `atual`, without
// duplicating (the same person may repeat the job in distinct raw credits of the
// work) and without changing anything when `novo` is empty. Mirrors juntarCargo of
// server/person.go (a different package, same semantics -- it avoids importing
// server here).
func juntarCargoExterno(atual *string, novo string) *string {
	if novo == "" {
		return atual
	}
	if atual == nil || *atual == "" {
		v := novo
		return &v
	}
	for _, cargo := range strings.Split(*atual, " · ") {
		if cargo == novo {
			return atual
		}
	}
	junto := *atual + " · " + novo
	return &junto
}

// ordenarFilmografiaExterna sorts by descending Year; credits without a year go to
// the end. Stable: works of the same year keep the order of arrival (cast before
// crew, each block in the order of the source) -- deterministic for the tests.
func ordenarFilmografiaExterna(creditos []provider.CreditoFilmografia) {
	sort.SliceStable(creditos, func(i, j int) bool {
		yi, yj := creditos[i].Year, creditos[j].Year
		switch {
		case yi == nil && yj == nil:
			return false
		case yi == nil:
			return false // i without a year: it does not come before j
		case yj == nil:
			return true // j without a year: i (with a year) comes first
		default:
			return *yi > *yj
		}
	})
}

// posterFilmografiaURL builds the direct poster URL (w185 size) of an item of the
// external filmography, or nil without poster_path.
func posterFilmografiaURL(posterPath string) *string {
	if posterPath == "" {
		return nil
	}
	u := posterFilmografiaBaseURL + posterPath
	return &u
}
