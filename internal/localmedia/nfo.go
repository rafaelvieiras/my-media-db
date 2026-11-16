package localmedia

// nfo.go builds the .nfo XML (see doc 09) from resolver.Entidade — the SAME
// object that (s *Server) resolverEntity(ctx, id, lang) already produces (the
// handler reuses it, never duplicating the resolution). The field-by-field
// mapping is exact according to the NFO mapping table; see MontarNFO. Pure:
// encoding/xml from the stdlib, no I/O (the atomic write to disk is
// EscreverArquivoAtomico, in naming.go).

import (
	"encoding/xml"
	"sort"

	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// nfoActor is a cast <actor>.
type nfoActor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role"`
	Order int    `xml:"order"`
	Thumb string `xml:"thumb,omitempty"`
}

// nfoUniqueID is <uniqueid type="..." default="true">value</uniqueid>.
type nfoUniqueID struct {
	Type    string `xml:"type,attr"`
	Default string `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

// nfoRatings is the <ratings> wrapper (current Kodi) around one or more
// <rating>.
type nfoRatings struct {
	Ratings []nfoRating `xml:"rating"`
}

// nfoRating is <rating name="tmdb" max="10" default="true"><value>X</value></rating>.
type nfoRating struct {
	Name    string  `xml:"name,attr"`
	Max     int     `xml:"max,attr"`
	Default bool    `xml:"default,attr"`
	Value   float64 `xml:"value"`
}

// nfoFanart wraps the backdrop fallback: <fanart><thumb>url</thumb></fanart>
// (Kodi convention).
type nfoFanart struct {
	Thumb string `xml:"thumb"`
}

// nfoResume is <resume><position>sec</position><total>sec</total></resume> —
// playback resume point (Kodi convention, NFO_files/Templates).
type nfoResume struct {
	Position float64 `xml:"position"`
	Total    float64 `xml:"total"`
}

// xmlElementoBruto captures a top-level XML element that nfoDoc does not know
// -- name, attributes and inner content exactly as they were in the previous
// file, to be re-embedded without interpretation (doc 09, §4).
type xmlElementoBruto struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Inner   string     `xml:",innerxml"`
}

// nfoDoc is the body shared by movie.nfo/tvshow.nfo/<episode>.nfo — the
// XMLName changes per type (nomeRaizNFO), the fields are the same (Kodi
// tolerates empty/absent elements of a type that does not use them). Status is
// only filled for type=tv (tvshow.nfo); the other types leave Status=""
// (omitted).
//
// UserRating/PlayCount/Watched/LastPlayed/DateAdded/Resume are the LOCAL
// fields of Kodi/Emby/Jellyfin (NFO_files/Templates, jellyfin.org/docs
// .../metadata/nfo) -- personal rating, watched state/count, last playback,
// date added to the library and resume point. MontarNFO NEVER fills them from
// resolver.Entidade (they are not part of our data model) -- they only get
// here through CamposUsuarioNFO, extracted from the previous .nfo by
// ExtrairCamposUsuario, so that they SURVIVE a rewrite (see
// gravarNFO/internal/server/localmedia_handlers.go).
//
// Extras is the generic catch-all (doc 09, §4): any top-level element of the
// previous .nfo that NONE of the fields above models (e.g. <tag>, <set>,
// <fileinfo>) lands here through xml:",any" and survives the rewrite exactly
// as it was, without MyMediaDB needing to know the tag name in advance. It is
// a superset of the six named fields -- they stay explicit above (more
// readable, existing tests) and never duplicate into Extras because they
// already matched a named field before the catch-all comes into play.
type nfoDoc struct {
	XMLName       xml.Name
	Title         string      `xml:"title"`
	OriginalTitle string      `xml:"originaltitle,omitempty"`
	Plot          string      `xml:"plot,omitempty"`
	Tagline       string      `xml:"tagline,omitempty"`
	Genre         []string    `xml:"genre,omitempty"`
	MPAA          string      `xml:"mpaa,omitempty"`
	Premiered     string      `xml:"premiered,omitempty"`
	Ratings       *nfoRatings `xml:"ratings,omitempty"`
	// RatingLegacy recognizes the TOP-LEVEL <rating> of the format used
	// before the move to <ratings> (e.g. <rating name="tmdb">6.21</rating>,
	// directly under <movie>/<tvshow> -- not to be confused with the
	// <rating> NESTED inside <ratings> above, which is another level and
	// does not match this field). It is never read nor re-emitted by
	// MontarNFO -- it exists only to "claim" the tag name and prevent it
	// from falling into the Extras catch-all (xml:",any", doc 09, §4):
	// without this, a .nfo written before that format change would have the
	// old tag preserved forever alongside the new <ratings>, duplicating the
	// score on every rewrite.
	RatingLegacy *xmlElementoBruto  `xml:"rating,omitempty"`
	UniqueIDs    []nfoUniqueID      `xml:"uniqueid,omitempty"`
	Actors       []nfoActor         `xml:"actor,omitempty"`
	Directors    []string           `xml:"director,omitempty"`
	Credits      []string           `xml:"credits,omitempty"`
	Studios      []string           `xml:"studio,omitempty"`
	Status       string             `xml:"status,omitempty"`
	Thumb        string             `xml:"thumb,omitempty"`
	Fanart       *nfoFanart         `xml:"fanart,omitempty"`
	UserRating   *float64           `xml:"userrating,omitempty"`
	PlayCount    *int               `xml:"playcount,omitempty"`
	Watched      *bool              `xml:"watched,omitempty"`
	LastPlayed   string             `xml:"lastplayed,omitempty"`
	DateAdded    string             `xml:"dateadded,omitempty"`
	Resume       *nfoResume         `xml:"resume,omitempty"`
	Extras       []xmlElementoBruto `xml:",any"`
}

// CamposUsuarioNFO groups the LOCAL fields of the .nfo (see nfoDoc) that only
// the user/Kodi/Emby/Jellyfin edit after the file already exists -- MontarNFO
// copies them as they came, never inventing nor erasing them. Extras is the
// generic catch-all (see the comment in nfoDoc).
type CamposUsuarioNFO struct {
	UserRating *float64
	PlayCount  *int
	Watched    *bool
	LastPlayed string
	DateAdded  string
	Resume     *struct{ Position, Total float64 }
	Extras     []xmlElementoBruto
}

// ExtrairCamposUsuario reads the user fields of an already existing .nfo
// (bytes read from disk by the caller, if any). Best-effort: a missing file
// (empty bytes) or XML that does not parse returns the zero-value -- writing
// an NFO must never fail because of a previous file that is corrupted or was
// hand-edited out of the pattern.
func ExtrairCamposUsuario(existente []byte) CamposUsuarioNFO {
	if len(existente) == 0 {
		return CamposUsuarioNFO{}
	}
	var doc nfoDoc
	if err := xml.Unmarshal(existente, &doc); err != nil {
		return CamposUsuarioNFO{}
	}
	out := CamposUsuarioNFO{
		UserRating: doc.UserRating,
		PlayCount:  doc.PlayCount,
		Watched:    doc.Watched,
		LastPlayed: doc.LastPlayed,
		DateAdded:  doc.DateAdded,
		Extras:     doc.Extras,
	}
	if doc.Resume != nil {
		out.Resume = &struct{ Position, Total float64 }{Position: doc.Resume.Position, Total: doc.Resume.Total}
	}
	return out
}

// Root elements of the XML per type (Kodi/Emby convention).
const (
	raizMovie   = "movie"
	raizTVShow  = "tvshow"
	raizEpisode = "episodedetails"
)

// nomeRaizNFO decides the XML root element by the entity type: tv->tvshow,
// episode->episodedetails, the others (movie and any future type) ->movie (the
// most generic/safe one — only movie/episode/series are covered by the NFO
// mapping of doc 09; see NomeArquivoNFO/handler for the other entities).
func nomeRaizNFO(entityType string) string {
	switch entityType {
	case TipoSerie:
		return raizTVShow
	case TipoEpisodio:
		return raizEpisode
	default:
		return raizMovie
	}
}

// ordemUniqueIDDefault is the default priority when tmdb is absent (see doc 09,
// §4: "the type=tmdb one receives default=true when present, otherwise the
// first in the order tmdb, imdb, tvdb").
var ordemUniqueIDDefault = []string{"tmdb", "imdb", "tvdb"}

// MontarNFO builds the .nfo XML from the ALREADY RESOLVED Entity
// (resolver.Entidade) and the local fields preserved from a previous write
// (CamposUsuarioNFO -- zero-value when there is no previous .nfo, see
// ExtrairCamposUsuario). It returns the bytes ready to be written (XML header
// + indentation + trailing newline).
//
// Poster/backdrop (Thumb/Fanart of nfoDoc) are NEVER filled from
// e.PosterURL/e.BackdropURL (see doc 09): whoever calls MontarNFO (gravarNFO)
// already materializes the physical file in the real folder BEFORE building
// the XML, and Kodi/Jellyfin discover that artwork on their own through the
// file naming convention (poster.jpg/fanart.jpg) -- with no need for any
// <thumb>/<fanart>. The fields keep existing in nfoDoc only to recognize those
// elements as NAMED (so they do not fall into Extras) in case a previous .nfo
// has them.
func MontarNFO(e resolver.Entidade, usuario CamposUsuarioNFO) ([]byte, error) {
	doc := nfoDoc{
		XMLName:       xml.Name{Local: nomeRaizNFO(e.Type)},
		Title:         e.Title,
		OriginalTitle: derefStr(e.OriginalTitle),
		Plot:          derefStr(e.Description),
		Tagline:       derefStr(e.Tagline),
		Genre:         e.Tags,
		MPAA:          derefStr(e.Certification),
		Premiered:     derefStr(e.ReleaseDate),
		UniqueIDs:     uniqueIDsDe(e.ExternalIDs),
		Actors:        actorsDe(e.Cast),
		Directors:     diretoresDe(e.Crew),
		Credits:       creditosDe(e.Crew),
		Studios:       nomesDeCompanies(e.Companies),
		UserRating:    usuario.UserRating,
		PlayCount:     usuario.PlayCount,
		Watched:       usuario.Watched,
		LastPlayed:    usuario.LastPlayed,
		DateAdded:     usuario.DateAdded,
		Extras:        usuario.Extras,
	}
	if e.VoteAverage != nil {
		doc.Ratings = &nfoRatings{Ratings: []nfoRating{{Name: "tmdb", Max: 10, Default: true, Value: *e.VoteAverage}}}
	}
	if e.Type == TipoSerie {
		doc.Status = derefStr(e.AiringStatus)
	}
	if usuario.Resume != nil {
		doc.Resume = &nfoResume{Position: usuario.Resume.Position, Total: usuario.Resume.Total}
	}

	corpo, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	saida := make([]byte, 0, len(xml.Header)+len(corpo)+1)
	saida = append(saida, []byte(xml.Header)...)
	saida = append(saida, corpo...)
	saida = append(saida, '\n')
	return saida, nil
}

// derefStr dereferences a *string ("" when nil).
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// uniqueIDsDe converts the ExternalIDs map (source->id) into a SORTED list
// (alphabetic key, determinism) of <uniqueid>, marking default="true" on the
// key chosen by escolherDefaultUniqueID.
func uniqueIDsDe(ids map[string]string) []nfoUniqueID {
	if len(ids) == 0 {
		return nil
	}
	chaves := make([]string, 0, len(ids))
	for k := range ids {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	defaultChave := escolherDefaultUniqueID(ids)
	out := make([]nfoUniqueID, 0, len(ids))
	for _, k := range chaves {
		u := nfoUniqueID{Type: k, Value: ids[k]}
		if k == defaultChave {
			u.Default = "true"
		}
		out = append(out, u)
	}
	return out
}

// escolherDefaultUniqueID: tmdb if present; otherwise the first of
// [tmdb,imdb,tvdb] that exists; otherwise "" (no default marked).
func escolherDefaultUniqueID(ids map[string]string) string {
	for _, k := range ordemUniqueIDDefault {
		if _, ok := ids[k]; ok {
			return k
		}
	}
	return ""
}

// actorsDe converts the cast (richfields.Cast) into <actor>.
func actorsDe(cast []richfields.Cast) []nfoActor {
	out := make([]nfoActor, 0, len(cast))
	for _, c := range cast {
		out = append(out, nfoActor{
			Name:  c.Name,
			Role:  c.Character,
			Order: c.Order,
			Thumb: derefStr(c.ProfileURL),
		})
	}
	return out
}

// diretoresDe filters the crew (richfields.Crew) by directors
// (Department=="Directing" && Job=="Director").
func diretoresDe(crew []richfields.Crew) []string {
	var out []string
	for _, c := range crew {
		if c.Department == "Directing" && c.Job == "Director" {
			out = append(out, c.Name)
		}
	}
	return out
}

// creditosDe filters the crew by writers (Department=="Writing") — mapped to
// <credits> (see doc 09, §4).
func creditosDe(crew []richfields.Crew) []string {
	var out []string
	for _, c := range crew {
		if c.Department == "Writing" {
			out = append(out, c.Name)
		}
	}
	return out
}

// nomesDeCompanies extracts the names of the production companies/studios for
// <studio>.
func nomesDeCompanies(companies []richfields.Company) []string {
	out := make([]string, 0, len(companies))
	for _, c := range companies {
		out = append(out, c.Name)
	}
	return out
}
