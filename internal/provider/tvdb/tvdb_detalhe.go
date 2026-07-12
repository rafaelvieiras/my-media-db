package tvdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo cobre o DETALHE canônico da SÉRIE (o análogo de BuscarDetalhe/
// MapearDetalhe do tmdb para o tipo "tv"): puxa /series/{id}/extended + as traduções
// permitidas (por/…, com fallback) num PAYLOAD AUMENTADO (envelope re-componível,
// ADR-0011) e o mapeia — PURO, sem rede — para o mapa canônico com nossos nomes de
// campo. remoteIds viram external_ids/external_links (cruzamento com TMDB/IMDB).

// respSerie envelopa a resposta de /series/{id}/extended ({status, data}).
type respSerie struct {
	Data serieData `json:"data"`
}

// serieData espelha o SeriesExtendedRecord (só os campos usados). seasonTypes[] e
// seasons[] alimentam a árvore (tvdb_serie.go); remoteIds o cruzamento de ids.
type serieData struct {
	ID                int64           `json:"id"`
	Name              string          `json:"name"`
	Slug              string          `json:"slug"`
	Image             string          `json:"image"`
	Overview          string          `json:"overview"`
	FirstAired        string          `json:"firstAired"`
	Year              string          `json:"year"`
	OriginalLanguage  string          `json:"originalLanguage"`
	AverageRuntime    int             `json:"averageRuntime"`
	DefaultSeasonType int             `json:"defaultSeasonType"`
	RemoteIds         []remoteID      `json:"remoteIds"`
	SeasonTypes       []seasonTypeRec `json:"seasonTypes"`
	Seasons           []seasonRec     `json:"seasons"`

	// Status/Genres do /series/{id}/extended: o FALLBACK do 0045 quando não há match
	// no TMDB. status.name é a SITUAÇÃO crua (ex.: "Continuing"|"Ended"); genres[] são
	// os gêneros (nome). O TVDB não tem tagline/keywords/similar — ausência graciosa.
	Status struct {
		Name string `json:"name"`
	} `json:"status"`
	Genres []generoSerie `json:"genres"`
}

// generoSerie é um gênero do /series/{id}/extended (só o nome nos interessa como tag).
type generoSerie struct {
	Name string `json:"name"`
}

// remoteID é um id do item em outra fonte (remoteIds[]): id + sourceName (o nome é
// mais estável que o `type` numérico para mapear tvdb/imdb/tmdb).
type remoteID struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	SourceName string `json:"sourceName"`
}

// seasonTypeRec é uma ordenação que a série usa (SeasonType): id + type (ex.: "official").
type seasonTypeRec struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// seasonRec é uma temporada do extended.seasons[] (SeasonBaseRecord + type embutido).
// number pode ser ano/0/índice — não assumir 1..N. type.type diz a ordenação.
type seasonRec struct {
	ID     int64  `json:"id"`
	Number int    `json:"number"`
	Year   string `json:"year"`
	Image  string `json:"image"`
	Name   string `json:"name"`
	Type   struct {
		Type string `json:"type"`
	} `json:"type"`
}

// traducaoSerie são os campos traduzíveis de uma tradução de série (name/overview).
type traducaoSerie struct {
	Name     string `json:"name"`
	Overview string `json:"overview"`
}

// envelopeSerie é o PAYLOAD AUMENTADO gravado em media_external: o detalhe cru do
// /series/{id}/extended (`data`) mais as traduções por idioma do app (derivadas no
// enrich, que faz rede — o read-time é puro). Detail vazio ⇒ payload LEGADO (mono-idioma).
type envelopeSerie struct {
	Detail      json.RawMessage          `json:"detail"`
	TransByLang map[string]traducaoSerie `json:"trans_by_lang"`
}

// BuscarDetalhe puxa o detalhe da SÉRIE já MULTI-IDIOMA (0020): /series/{id}/extended
// mais /series/{id}/translations/{code} para cada idioma permitido (com FALLBACK —
// idioma sem tradução é pulado, não quebra). Devolve o envelope re-componível
// {detail, trans_by_lang}. `tipo` deve ser "tv" (a fonte só cobre séries). Erro propaga.
func (c *Client) BuscarDetalhe(ctx context.Context, sourceID, tipo string, langs []string) (json.RawMessage, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("tvdb: sourceID vazio")
	}
	if tipo != "tv" {
		return nil, fmt.Errorf("tvdb: tipo invalido %q (a fonte cobre series: tv)", tipo)
	}
	detail, err := c.getBytes(ctx, "/series/"+url.PathEscape(sourceID)+"/extended", nil)
	if err != nil {
		return nil, err
	}
	var resp respSerie
	if err := json.Unmarshal(detail, &resp); err != nil {
		return nil, fmt.Errorf("tvdb: detalhe invalido: %w", err)
	}
	env := envelopeSerie{
		Detail:      json.RawMessage(detail),
		TransByLang: c.traducoesSerie(ctx, sourceID, langs),
	}
	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("tvdb: montar payload aumentado: %w", err)
	}
	return json.RawMessage(body), nil
}

// traducoesSerie busca a tradução da série em cada idioma permitido (dedup por código
// TheTVDB), pulando os ausentes (fallback). Chaveia pelo código do app pedido.
func (c *Client) traducoesSerie(ctx context.Context, sourceID string, langs []string) map[string]traducaoSerie {
	out := map[string]traducaoSerie{}
	vistos := map[string]bool{}
	for _, lang := range langs {
		code := codigoTVDB(lang)
		if code == "" || vistos[code] {
			continue
		}
		vistos[code] = true
		tr, ok := c.traducaoSerie(ctx, sourceID, code)
		if ok {
			out[lang] = tr
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// traducaoSerie busca UMA tradução de série; ok=false quando ausente (404 → fallback)
// ou vazia. Erros de rede também degradam para ok=false (a tradução é best-effort).
func (c *Client) traducaoSerie(ctx context.Context, sourceID, code string) (traducaoSerie, bool) {
	body, err := c.getBytes(ctx, "/series/"+url.PathEscape(sourceID)+"/translations/"+url.PathEscape(code), nil)
	if err != nil {
		return traducaoSerie{}, false
	}
	var resp struct {
		Data traducaoSerie `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return traducaoSerie{}, false
	}
	if resp.Data.Name == "" && resp.Data.Overview == "" {
		return traducaoSerie{}, false
	}
	return resp.Data, true
}

// MapearDetalhe traduz o payload do TheTVDB para o mapa CANÔNICO (nossos nomes). É PURO
// (sem rede): detecta o ENVELOPE (0020) vs o LEGADO (extended cru) e mapeia conforme. Só
// inclui as chaves que a fonte trouxe. SEM elenco/equipe (MD6).
func (c *Client) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	// EPISODIO (0041): payload com discriminador entity_kind — title/description por
	// idioma. Vem ANTES do ramo de serie (a mesma fonte "tvdb" serve os dois shapes);
	// um payload sem o discriminador cai adiante, sem quebrar o mapeamento de serie.
	if campos, ok := provider.MapearEpisodio(raw); ok {
		return campos, nil
	}
	var env envelopeSerie
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Detail) > 0 {
		return mapearEnvelopeSerie(env)
	}
	return mapearDetailBruto(raw, nil)
}

// mapearEnvelopeSerie mapeia o payload aumentado: title/description por idioma (do
// detalhe + traduções) e os neutros/escalares.
func mapearEnvelopeSerie(env envelopeSerie) (map[string]any, error) {
	return mapearDetailBruto(env.Detail, env.TransByLang)
}

// mapearDetailBruto mapeia o /series/{id}/extended cru (`data` ou envelope-response) e
// as traduções por idioma no mapa canônico. `trans` nil degrada ao mono-idioma.
func mapearDetailBruto(detail json.RawMessage, trans map[string]traducaoSerie) (map[string]any, error) {
	var resp respSerie
	if err := json.Unmarshal(detail, &resp); err != nil {
		return nil, fmt.Errorf("tvdb: detalhe do envelope invalido: %w", err)
	}
	d := resp.Data
	out := map[string]any{}
	titulos, descricoes := traduzidosSerie(d, trans)
	if len(titulos) > 0 {
		out["title"] = titulos
	}
	if len(descricoes) > 0 {
		out["description"] = descricoes
	}
	enriquecerNeutrosSerie(out, d)
	return out, nil
}

// traduzidosSerie monta os mapas idioma→título e idioma→descrição: ancora o idioma
// ORIGINAL com o name/overview do topo (garantidamente no idioma original) e sobrepõe
// cada tradução permitida, chaveada pelo código do app (combinado + base).
func traduzidosSerie(d serieData, trans map[string]traducaoSerie) (titulos, descricoes map[string]any) {
	titulos = map[string]any{}
	descricoes = map[string]any{}
	if orig := codigoApp(d.OriginalLanguage); orig != "" {
		porSeAusente(titulos, orig, d.Name)
		porSeAusente(descricoes, orig, d.Overview)
	}
	for lang, tr := range trans {
		for _, chave := range codigosApp(lang) {
			porSeAusente(titulos, chave, tr.Name)
			porSeAusente(descricoes, chave, tr.Overview)
		}
	}
	return titulos, descricoes
}

// enriquecerNeutrosSerie grava os campos NEUTROS/escalares do detalhe (title original,
// idioma original, data, pôster, duração) e os ids externos (external_ids/links).
func enriquecerNeutrosSerie(out map[string]any, d serieData) {
	if d.Name != "" {
		out["original_title"] = d.Name
	}
	if orig := codigoApp(d.OriginalLanguage); orig != "" {
		out["original_language"] = orig
	}
	if d.FirstAired != "" {
		out["release_date"] = d.FirstAired
	}
	if d.Image != "" {
		out["poster_url"] = d.Image
	}
	if d.AverageRuntime > 0 {
		out["runtime"] = d.AverageRuntime
	}
	if ids := idsExternos(d); len(ids) > 0 {
		out["external_ids"] = ids
	}
	if links := linksExternos(d); len(links) > 0 {
		out["external_links"] = links
	}
	enriquecerFallbackRico(out, d)
}

// enriquecerFallbackRico emite os campos ricos que o TVDB tem e o 0045 antes descartava,
// usados como FALLBACK quando não há match no TMDB para o nó série: a SITUAÇÃO crua
// (airing_status, escalar neutro) e os gêneros do /extended como tags ancoradas no idioma
// ORIGINAL (o TVDB não os localiza) — espelha keywordsPorIdioma do TMDB, mesma dimensão
// idioma×camada que tags usa. Tagline/keywords/similar o TVDB não tem (ausência graciosa).
func enriquecerFallbackRico(out map[string]any, d serieData) {
	if d.Status.Name != "" {
		out["airing_status"] = d.Status.Name
	}
	if orig := codigoApp(d.OriginalLanguage); orig != "" {
		if nomes := nomesGeneros(d.Genres); len(nomes) > 0 {
			out["tags"] = map[string]any{orig: nomes}
		}
	}
}

// nomesGeneros extrai os nomes dos gêneros do /extended como lista de tags (descarta
// os vazios). Ordem estável = ordem da fonte.
func nomesGeneros(gs []generoSerie) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
}

// idsExternos monta o mapa fonte→id a partir dos remoteIds (sempre inclui tvdb=id da
// série). O mapeamento é por sourceName (mais estável que o `type` numérico). Séries
// exclusivas (Looney Tunes) só terão tvdb — sem tmdb/imdb (ADR-0021).
func idsExternos(d serieData) map[string]any {
	out := map[string]any{"tvdb": strconv.FormatInt(d.ID, 10)}
	for _, r := range d.RemoteIds {
		if chave := chaveFonte(r.SourceName); chave != "" && r.ID != "" {
			if _, existe := out[chave]; !existe {
				out[chave] = r.ID
			}
		}
	}
	return out
}

// chaveFonte normaliza o sourceName do remoteId para a nossa chave canônica. Só as
// fontes que nos interessam para cruzar identidade; as demais são ignoradas.
func chaveFonte(sourceName string) string {
	switch sourceName {
	case "TheMovieDB.com":
		return "tmdb"
	case "IMDB":
		return "imdb"
	case "Wikidata":
		return "wikidata"
	case "TV Maze":
		return "tvmaze"
	default:
		return ""
	}
}

// linksExternos monta os links para páginas externas a partir dos ids cruzados (hoje
// imdb e tmdb). Reusa idsExternos para não reparsear os remoteIds.
func linksExternos(d serieData) map[string]any {
	ids := idsExternos(d)
	out := map[string]any{}
	if imdb, ok := ids["imdb"].(string); ok && imdb != "" {
		out["imdb"] = "https://www.imdb.com/title/" + imdb
	}
	if tmdb, ok := ids["tmdb"].(string); ok && tmdb != "" {
		out["tmdb"] = "https://www.themoviedb.org/tv/" + tmdb
	}
	return out
}

// porSeAusente grava valor não-vazio em chave não-vazia só se a chave ainda não existe
// (a primeira ocorrência por idioma vence).
func porSeAusente(m map[string]any, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}
