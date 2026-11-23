package localmedia

// nfo.go builds the .nfo XML (see doc 09) from resolver.Entidade — the SAME
// object that (s *Server) resolverEntity(ctx, id, lang) already produces (the
// handler reuses it, never duplicating the resolution). The field-by-field
// mapping is exact according to the NFO mapping table; see MontarNFO. Pure:
// encoding/xml from the stdlib, no I/O (the atomic write to disk is
// EscreverArquivoAtomico, in naming.go).

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
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
	RatingLegacy *xmlElementoBruto `xml:"rating,omitempty"`
	UniqueIDs    []nfoUniqueID     `xml:"uniqueid,omitempty"`
	Actors       []nfoActor        `xml:"actor,omitempty"`
	Directors    []string          `xml:"director,omitempty"`
	Credits      []string          `xml:"credits,omitempty"`
	Studios      []string          `xml:"studio,omitempty"`
	Status       string            `xml:"status,omitempty"`
	Thumb        string            `xml:"thumb,omitempty"`
	Fanart       *nfoFanart        `xml:"fanart,omitempty"`
	UserRating   *float64          `xml:"userrating,omitempty"`
	PlayCount    *int              `xml:"playcount,omitempty"`
	Watched      *bool             `xml:"watched,omitempty"`
	LastPlayed   string            `xml:"lastplayed,omitempty"`
	DateAdded    string            `xml:"dateadded,omitempty"`
	Resume       *nfoResume        `xml:"resume,omitempty"`
	// Season/Episode are the episode NUMBERING inside the tree (ADR-0021): required by
	// Kodi/Jellyfin in an NFO that documents more than one episode in the same video file
	// (issue #80), absent for movie/tvshow. They are NAMED fields so that a value written
	// by an older version does NOT survive through the Extras catch-all (an episode NFO
	// used to keep the number parsed out of the FILE NAME forever, even when it disagreed
	// with the entity).
	Season  *int               `xml:"season,omitempty"`
	Episode *int               `xml:"episode,omitempty"`
	Extras  []xmlElementoBruto `xml:",any"`
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
	return camposDe(doc)
}

// camposDe extracts the local/user fields of one already parsed nfoDoc.
func camposDe(doc nfoDoc) CamposUsuarioNFO {
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

// BlocoUsuarioNFO is one parsed block of an existing EPISODE .nfo: the numbering it declares
// (0 when the tags are absent) and the local fields it carries.
type BlocoUsuarioNFO struct {
	Temporada int
	Episodio  int
	Campos    CamposUsuarioNFO
}

// ExtrairBlocosDeEpisodio reads EVERY <episodedetails> block of an existing .nfo -- a
// multi-episode file has several, concatenated after one XML header (see
// MontarNFOEpisodios). It exists so a rewrite preserves the watched/rating state of EACH
// episode of the file instead of only the first one: the caller matches the blocks by
// (Temporada, Episodio). Best-effort, like ExtrairCamposUsuario: a corrupted file yields
// whatever was readable before the defect, never an error -- writing an NFO must never fail
// because of the previous one.
func ExtrairBlocosDeEpisodio(existente []byte) []BlocoUsuarioNFO {
	if len(existente) == 0 {
		return nil
	}
	dec := xml.NewDecoder(bytes.NewReader(existente))
	var out []BlocoUsuarioNFO
	for {
		var doc nfoDoc
		if err := dec.Decode(&doc); err != nil {
			break // io.EOF, or the file stopped being valid XML: keep what was read
		}
		if doc.XMLName.Local != raizEpisode {
			continue
		}
		bloco := BlocoUsuarioNFO{Campos: camposDe(doc)}
		if doc.Season != nil {
			bloco.Temporada = *doc.Season
		}
		if doc.Episode != nil {
			bloco.Episodio = *doc.Episode
		}
		out = append(out, bloco)
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
	corpo, err := xml.MarshalIndent(docDe(e, usuario), "", "  ")
	if err != nil {
		return nil, err
	}
	saida := make([]byte, 0, len(xml.Header)+len(corpo)+1)
	saida = append(saida, []byte(xml.Header)...)
	saida = append(saida, corpo...)
	saida = append(saida, '\n')
	return saida, nil
}

// BlocoEpisodioNFO is one <episodedetails> block of the .nfo of a video FILE: the resolved
// episode entity, its numbering and the local fields preserved from the previous file.
// Temporada/Episodio are 0 when the tree has no number for the episode (a special without a
// season/episode link) -- the tags are then omitted, never written as 0.
type BlocoEpisodioNFO struct {
	Entidade  resolver.Entidade
	Temporada int
	Episodio  int
	Usuario   CamposUsuarioNFO
}

// MontarNFOEpisodios builds the .nfo of a video file that holds one or more episodes
// (issue #80). A multi-episode file (S02E01-E02-E03) is ONE file with ONE .nfo -- Kodi
// derives the episode numbers from the FILE NAME, so the document has to carry one
// <episodedetails> block per episode, in file order (Kodi's own library export writes it
// exactly like this; the concatenated roots are technically invalid XML and there is an open
// upstream issue about it, but it is what Kodi, Jellyfin and every NFO parser out there
// read).
//
// With a single block the output is byte-identical to MontarNFO (plus the numbering), so the
// 12 thousand single-episode files keep their shape.
func MontarNFOEpisodios(blocos []BlocoEpisodioNFO) ([]byte, error) {
	if len(blocos) == 0 {
		return nil, errors.New("localmedia: nenhum episodio para montar o NFO")
	}
	saida := make([]byte, 0, 1024*len(blocos))
	saida = append(saida, []byte(xml.Header)...)
	for _, b := range blocos {
		doc := docDe(b.Entidade, b.Usuario)
		if b.Temporada > 0 {
			temporada := b.Temporada
			doc.Season = &temporada
		}
		if b.Episodio > 0 {
			episodio := b.Episodio
			doc.Episode = &episodio
		}
		corpo, err := xml.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, err
		}
		saida = append(saida, corpo...)
		saida = append(saida, '\n')
	}
	return saida, nil
}

// docDe assembles the nfoDoc of ONE entity: the mapping table of doc 09 plus the local fields
// preserved from a previous write. Shared by MontarNFO (one document) and
// MontarNFOEpisodios (one block per episode).
func docDe(e resolver.Entidade, usuario CamposUsuarioNFO) nfoDoc {
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
	return doc
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

// CanonicalizarNFOEpisodios re-emits the blocks of an EXISTING episode .nfo in the same
// canonical layout MontarNFOEpisodios produces, WITHOUT changing any value. It exists because
// the same document can be written in more than one layout: an .nfo written before the numbered
// fields existed keeps <season>/<episode> among the unknown tags, as they arrived from whatever
// manager wrote the file, in the middle of other tags -- semantically identical, byte-different.
// Comparing the canonical form (instead of the raw bytes) against the document we would write is
// what keeps the first run after the upgrade from rewriting the ~12 thousand single-episode
// files, while a file whose NUMBERING or block count really differs still does not converge.
//
// A file that is not valid XML, or that has no <episodedetails> block, returns an error: the
// caller rewrites from scratch.
func CanonicalizarNFOEpisodios(existente []byte) ([]byte, error) {
	if len(existente) == 0 {
		return nil, errors.New("localmedia: NFO vazio")
	}
	dec := xml.NewDecoder(bytes.NewReader(existente))
	saida := make([]byte, 0, len(existente))
	blocos := 0
	for {
		var doc nfoDoc
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if doc.XMLName.Local != raizEpisode {
			continue
		}
		if blocos == 0 {
			saida = append(saida, []byte(xml.Header)...)
		}
		corpo, err := xml.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, err
		}
		saida = append(saida, corpo...)
		saida = append(saida, '\n')
		blocos++
	}
	if blocos == 0 {
		return nil, errors.New("localmedia: NFO sem episodedetails")
	}
	return saida, nil
}
