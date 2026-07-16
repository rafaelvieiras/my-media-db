package localmedia

// nfo.go monta o XML do .nfo (briefing 0062 §4) a partir de resolver.Entidade
// — o MESMO objeto que (s *Server) resolverEntity(ctx, id, lang) já produz
// (o handler reusa, nunca duplica a resolução). Mapeamento campo a campo
// exato conforme a tabela do briefing; ver MontarNFO. Puro: encoding/xml da
// stdlib, sem I/O (a escrita atômica em disco é EscreverArquivoAtomico, em
// naming.go).

import (
	"encoding/xml"
	"sort"

	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// InsumosArte sinaliza, para poster/fanart, se JÁ há uma versão materializada
// na pasta REAL da biblioteca (asset.local_path != null) — insumo que
// resolver.Entidade não carrega (ela só sabe se há asset no CACHE interno,
// via PosterURL/BackdropURL). Quando presente, o NFO OMITE o <thumb>/
// <fanart> de fallback: o arquivo local canônico (ex. poster.jpg) já é a
// fonte que o Kodi/Emby descobrem sozinhos pelo nome; a tag no NFO é só um
// ponteiro de URL para quando NÃO há arquivo local (briefing 0062 §4).
type InsumosArte struct {
	PosterLocal bool
	FanartLocal bool
}

// nfoActor é um <actor> do elenco.
type nfoActor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role"`
	Order int    `xml:"order"`
	Thumb string `xml:"thumb,omitempty"`
}

// nfoUniqueID é <uniqueid type="..." default="true">valor</uniqueid>.
type nfoUniqueID struct {
	Type    string `xml:"type,attr"`
	Default string `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

// nfoRating é <rating name="tmdb">valor</rating>.
type nfoRating struct {
	Name  string  `xml:"name,attr"`
	Value float64 `xml:",chardata"`
}

// nfoFanart embrulha o fallback de fundo: <fanart><thumb>url</thumb></fanart>
// (convenção Kodi).
type nfoFanart struct {
	Thumb string `xml:"thumb"`
}

// nfoResume é <resume><position>seg</position><total>seg</total></resume> —
// ponto de retomada de reprodução (convenção Kodi, NFO_files/Templates).
type nfoResume struct {
	Position float64 `xml:"position"`
	Total    float64 `xml:"total"`
}

// nfoDoc é o corpo comum de movie.nfo/tvshow.nfo/<episódio>.nfo — o XMLName
// muda por tipo (nomeRaizNFO), os campos são os mesmos (Kodi tolera
// elementos vazios/ausentes num tipo que não os usa). Status só é preenchido
// para type=tv (tvshow.nfo); os demais tipos deixam Status="" (omitido).
//
// UserRating/PlayCount/Watched/LastPlayed/DateAdded/Resume são os campos
// LOCAIS do Kodi/Emby/Jellyfin (NFO_files/Templates, jellyfin.org/docs
// .../metadata/nfo) -- avaliação pessoal, estado/contagem de assistido,
// última reprodução, data de adição à biblioteca e ponto de retomada.
// MontarNFO NUNCA os preenche a partir de resolver.Entidade (não fazem parte
// do nosso modelo de dados) -- eles só chegam aqui via CamposUsuarioNFO,
// extraídos do .nfo anterior por ExtrairCamposUsuario, para SOBREVIVER a
// uma regravação (ver gravarNFO/internal/server/localmedia_handlers.go).
type nfoDoc struct {
	XMLName       xml.Name
	Title         string        `xml:"title"`
	OriginalTitle string        `xml:"originaltitle,omitempty"`
	Plot          string        `xml:"plot,omitempty"`
	Tagline       string        `xml:"tagline,omitempty"`
	Genre         []string      `xml:"genre,omitempty"`
	MPAA          string        `xml:"mpaa,omitempty"`
	Premiered     string        `xml:"premiered,omitempty"`
	Rating        *nfoRating    `xml:"rating,omitempty"`
	UniqueIDs     []nfoUniqueID `xml:"uniqueid,omitempty"`
	Actors        []nfoActor    `xml:"actor,omitempty"`
	Directors     []string      `xml:"director,omitempty"`
	Credits       []string      `xml:"credits,omitempty"`
	Studios       []string      `xml:"studio,omitempty"`
	Status        string        `xml:"status,omitempty"`
	Thumb         string        `xml:"thumb,omitempty"`
	Fanart        *nfoFanart    `xml:"fanart,omitempty"`
	UserRating    *float64      `xml:"userrating,omitempty"`
	PlayCount     *int          `xml:"playcount,omitempty"`
	Watched       *bool         `xml:"watched,omitempty"`
	LastPlayed    string        `xml:"lastplayed,omitempty"`
	DateAdded     string        `xml:"dateadded,omitempty"`
	Resume        *nfoResume    `xml:"resume,omitempty"`
}

// CamposUsuarioNFO agrupa os campos LOCAIS do .nfo (ver nfoDoc) que só o
// usuário/Kodi/Emby/Jellyfin editam depois que o arquivo já existe --
// MontarNFO os copia tal como vieram, nunca os inventa nem os apaga.
type CamposUsuarioNFO struct {
	UserRating *float64
	PlayCount  *int
	Watched    *bool
	LastPlayed string
	DateAdded  string
	Resume     *struct{ Position, Total float64 }
}

// ExtrairCamposUsuario lê os campos de usuário de um .nfo já existente
// (bytes lidos do disco pelo chamador, se houver). Best-effort: arquivo
// ausente (bytes vazios) ou XML que não parseia devolve zero-value -- a
// gravação de um NFO nunca deve falhar por causa de um arquivo anterior
// corrompido ou editado à mão fora do padrão.
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
	}
	if doc.Resume != nil {
		out.Resume = &struct{ Position, Total float64 }{Position: doc.Resume.Position, Total: doc.Resume.Total}
	}
	return out
}

// Elementos raiz do XML por tipo (convenção Kodi/Emby).
const (
	raizMovie   = "movie"
	raizTVShow  = "tvshow"
	raizEpisode = "episodedetails"
)

// nomeRaizNFO decide o elemento raiz XML pelo type da entity: tv->tvshow,
// episode->episodedetails, os demais (movie e qualquer type futuro) ->movie
// (o mais genérico/seguro — só filme/episódio/série são cobertos pelo
// briefing 0062, ver NomeArquivoNFO/handler para as demais entities).
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

// ordemUniqueIDDefault é a prioridade de default quando tmdb está ausente
// (briefing 0062 §4: "o de type=tmdb recebe default=true quando presente,
// senão o primeiro na ordem tmdb, imdb, tvdb").
var ordemUniqueIDDefault = []string{"tmdb", "imdb", "tvdb"}

// MontarNFO monta o XML do .nfo a partir da Entidade JÁ RESOLVIDA
// (resolver.Entidade), dos insumos de arte (InsumosArte) e dos campos locais
// preservados de uma gravação anterior (CamposUsuarioNFO -- zero-value
// quando não há .nfo anterior, ver ExtrairCamposUsuario). Devolve os bytes
// prontos para gravar (cabeçalho XML + indentação + newline final).
func MontarNFO(e resolver.Entidade, arte InsumosArte, usuario CamposUsuarioNFO) ([]byte, error) {
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
	}
	if e.VoteAverage != nil {
		doc.Rating = &nfoRating{Name: "tmdb", Value: *e.VoteAverage}
	}
	if e.Type == TipoSerie {
		doc.Status = derefStr(e.AiringStatus)
	}
	if !arte.PosterLocal && e.PosterURL != nil {
		doc.Thumb = *e.PosterURL
	}
	if !arte.FanartLocal && e.BackdropURL != nil {
		doc.Fanart = &nfoFanart{Thumb: *e.BackdropURL}
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

// derefStr deref um *string ("" quando nil).
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// uniqueIDsDe converte o mapa ExternalIDs (source->id) numa lista ORDENADA
// (chave alfabética, determinismo) de <uniqueid>, marcando default="true" na
// chave escolhida por escolherDefaultUniqueID.
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

// escolherDefaultUniqueID: tmdb se presente; senão o primeiro em
// [tmdb,imdb,tvdb] que exista; senão "" (nenhum default marcado).
func escolherDefaultUniqueID(ids map[string]string) string {
	for _, k := range ordemUniqueIDDefault {
		if _, ok := ids[k]; ok {
			return k
		}
	}
	return ""
}

// actorsDe converte o elenco (richfields.Cast) em <actor>.
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

// diretoresDe filtra a equipe (richfields.Crew) pelos diretores
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

// creditosDe filtra a equipe pelos roteiristas (Department=="Writing") —
// mapeados para <credits> (briefing 0062 §4).
func creditosDe(crew []richfields.Crew) []string {
	var out []string
	for _, c := range crew {
		if c.Department == "Writing" {
			out = append(out, c.Name)
		}
	}
	return out
}

// nomesDeCompanies extrai os nomes das produtoras/estúdios para <studio>.
func nomesDeCompanies(companies []richfields.Company) []string {
	out := make([]string, 0, len(companies))
	for _, c := range companies {
		out = append(out, c.Name)
	}
	return out
}
