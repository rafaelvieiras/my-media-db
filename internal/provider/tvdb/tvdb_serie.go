package tvdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo implementa a capacidade provider.SerieProvider (ADR-0021): a ÁRVORE da
// série — temporadas (só da ordenação default) + episódios de grão fino ancorados no
// episode.id, cada um com a numeração de TODAS as ordens (Numbering).
//
// CAMINHO EFICIENTE (invariante 1): NÃO se busca cada episódio (N chamadas). Puxa-se
// a LISTA de episódios por ordem — /series/{id}/episodes/{ordem} (paginada 500/pág) —
// uma vez por ordem, e mescla-se por episode.id. Custo = 1 (extended) + Σ páginas por
// ordem + Σ páginas por idioma pedido. As numerações por ordem NÃO vêm no seasons[]
// da lista (vem vazio); por isso a fusão é por endpoint-de-ordem, não por episódio.

// BuscarSerieArvore cumpre provider.SerieProvider: devolve a árvore da série `sourceID`
// nos idiomas `langs` (com fallback). Só as temporadas da ordenação DEFAULT são
// materializadas; as demais ordens viram numeração nos episódios.
func (c *Client) BuscarSerieArvore(ctx context.Context, sourceID string, langs []string) (provider.SerieArvore, error) {
	if sourceID == "" {
		return provider.SerieArvore{}, fmt.Errorf("tvdb: sourceID vazio")
	}
	d, err := c.serieExtended(ctx, sourceID)
	if err != nil {
		return provider.SerieArvore{}, err
	}
	ordens := ordensDaSerie(d)
	ordemDef := ordemDefault(d, ordens)

	base, err := c.buscarEpisodios(ctx, sourceID, ordemDef, "")
	if err != nil {
		return provider.SerieArvore{}, err
	}
	episodios, indice := montarEpisodios(base, ordemDef, codigoApp(d.OriginalLanguage))

	for _, ord := range ordens {
		if ord == ordemDef {
			continue
		}
		if err := c.mesclarNumeracao(ctx, sourceID, ord, indice); err != nil {
			return provider.SerieArvore{}, err
		}
	}
	c.mesclarTraducoes(ctx, sourceID, ordemDef, langs, d.OriginalLanguage, indice)

	return provider.SerieArvore{
		DefaultSeasonType: ordemDef,
		SeasonTypes:       ordens,
		Temporadas:        montarTemporadas(d, ordemDef, episodios),
	}, nil
}

// serieExtended busca e parseia /series/{id}/extended (reusado pela árvore e pelo
// detalhe). ErrNaoEncontrado quando a série não existe.
func (c *Client) serieExtended(ctx context.Context, sourceID string) (serieData, error) {
	body, err := c.getBytes(ctx, "/series/"+url.PathEscape(sourceID)+"/extended", nil)
	if err != nil {
		return serieData{}, err
	}
	var resp respSerie
	if err := json.Unmarshal(body, &resp); err != nil {
		return serieData{}, fmt.Errorf("tvdb: extended invalido: %w", err)
	}
	return resp.Data, nil
}

// ordensDaSerie devolve as ordenações (season types) que a série usa, na ordem do
// extended (ex.: ["official","dvd","absolute"]).
func ordensDaSerie(d serieData) []string {
	out := make([]string, 0, len(d.SeasonTypes))
	for _, st := range d.SeasonTypes {
		if st.Type != "" {
			out = append(out, st.Type)
		}
	}
	return out
}

// ordemDefault resolve a ordenação default da série: casa defaultSeasonType (id) com o
// type em seasonTypes[]. Cai em "official" (ou na primeira ordem) se não achar.
func ordemDefault(d serieData, ordens []string) string {
	for _, st := range d.SeasonTypes {
		if st.ID == d.DefaultSeasonType && st.Type != "" {
			return st.Type
		}
	}
	if len(ordens) > 0 {
		return ordens[0]
	}
	return "official"
}

// episodeRec espelha o EpisodeBaseRecord da lista /series/{id}/episodes/{ordem} (só os
// campos usados). name/overview vêm no idioma pedido (null quando não há tradução).
type episodeRec struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Overview       string `json:"overview"`
	SeasonNumber   int    `json:"seasonNumber"`
	Number         int    `json:"number"`
	AbsoluteNumber int    `json:"absoluteNumber"`
	Aired          string `json:"aired"`
	Runtime        int    `json:"runtime"`
	Image          string `json:"image"`
}

// respEpisodios envelopa a resposta da lista de episódios ({data:{episodes[]}, links}).
type respEpisodios struct {
	Data struct {
		Episodes []episodeRec `json:"episodes"`
	} `json:"data"`
	Links struct {
		Next *string `json:"next"`
	} `json:"links"`
}

// buscarEpisodios puxa TODOS os episódios de uma ordem (paginando 500/pág via
// links.next). `lang` vazio traz o idioma base; um código TheTVDB traz a tradução
// daquela ordem (name/overview null quando falta). Erro de rede propaga.
func (c *Client) buscarEpisodios(ctx context.Context, sourceID, ordem, lang string) ([]episodeRec, error) {
	path := "/series/" + url.PathEscape(sourceID) + "/episodes/" + url.PathEscape(ordem)
	if lang != "" {
		path += "/" + url.PathEscape(lang)
	}
	var todos []episodeRec
	for page := 0; ; page++ {
		body, err := c.getBytes(ctx, path, url.Values{"page": {strconv.Itoa(page)}})
		if err != nil {
			return nil, err
		}
		var resp respEpisodios
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("tvdb: episodios invalidos: %w", err)
		}
		todos = append(todos, resp.Data.Episodes...)
		if resp.Links.Next == nil || len(resp.Data.Episodes) == 0 {
			return todos, nil
		}
	}
}

// montarEpisodios cria os EpisodioExterno da ordenação default (a âncora), preservando
// a ordem da fonte, e devolve também o índice id→*episódio (para as fusões seguintes).
// A numeração da ordem default já entra em Numbering; o conteúdo base vai sob o idioma
// original da série.
func montarEpisodios(base []episodeRec, ordemDef, origLang string) ([]*provider.EpisodioExterno, map[int64]*provider.EpisodioExterno) {
	episodios := make([]*provider.EpisodioExterno, 0, len(base))
	indice := make(map[int64]*provider.EpisodioExterno, len(base))
	for _, r := range base {
		ep := &provider.EpisodioExterno{
			SourceID:    strconv.FormatInt(r.ID, 10),
			ExternalIDs: map[string]string{"tvdb": strconv.FormatInt(r.ID, 10)},
			Title:       map[string]string{},
			Overview:    map[string]string{},
			Numbering:   map[string]provider.Numero{ordemDef: {Season: r.SeasonNumber, Number: r.Number}},
			AirDate:     r.Aired,
			StillURL:    r.Image,
		}
		if r.AbsoluteNumber != 0 {
			abs := r.AbsoluteNumber
			ep.AbsoluteNumber = &abs
		}
		if r.Runtime != 0 {
			rt := r.Runtime
			ep.Runtime = &rt
		}
		setSeAusente(ep.Title, origLang, r.Name)
		setSeAusente(ep.Overview, origLang, r.Overview)
		episodios = append(episodios, ep)
		indice[r.ID] = ep
	}
	return episodios, indice
}

// mesclarNumeracao busca os episódios de uma ordem ALTERNATIVA e injeta a numeração
// daquela ordem no MESMO episódio (por id). Episódios fora da ordem default são
// ignorados (as ordens são subconjunto do superconjunto default — ADR-0021).
func (c *Client) mesclarNumeracao(ctx context.Context, sourceID, ordem string, indice map[int64]*provider.EpisodioExterno) error {
	recs, err := c.buscarEpisodios(ctx, sourceID, ordem, "")
	if err != nil {
		return err
	}
	for _, r := range recs {
		if ep, ok := indice[r.ID]; ok {
			ep.Numbering[ordem] = provider.Numero{Season: r.SeasonNumber, Number: r.Number}
		}
	}
	return nil
}

// mesclarTraducoes sobrepõe título/sinopse por idioma pedido, usando a LISTA traduzida
// da ordem default (/episodes/{ordem}/{code}) — sem N chamadas por episódio. É
// best-effort: o idioma original é pulado (já é a base) e um idioma que falhe degrada
// (fallback: o episódio fica sem aquele idioma no mapa).
func (c *Client) mesclarTraducoes(ctx context.Context, sourceID, ordemDef string, langs []string, origTvdb string, indice map[int64]*provider.EpisodioExterno) {
	vistos := map[string]bool{origTvdb: true}
	for _, lang := range langs {
		code := codigoTVDB(lang)
		if code == "" || vistos[code] {
			continue
		}
		vistos[code] = true
		recs, err := c.buscarEpisodios(ctx, sourceID, ordemDef, code)
		if err != nil {
			continue
		}
		aplicarTraducao(recs, codigosApp(lang), indice)
	}
}

// aplicarTraducao grava name/overview não-vazios de cada episódio traduzido sob as
// chaves de idioma do app (combinado + base).
func aplicarTraducao(recs []episodeRec, chaves []string, indice map[int64]*provider.EpisodioExterno) {
	for _, r := range recs {
		ep, ok := indice[r.ID]
		if !ok {
			continue
		}
		for _, chave := range chaves {
			setSeAusente(ep.Title, chave, r.Name)
			setSeAusente(ep.Overview, chave, r.Overview)
		}
	}
}

// montarTemporadas cria as TemporadaExterna SÓ da ordenação default (invariante 2), a
// partir do extended.seasons[] (número, ano, pôster), anexando os episódios agrupados
// pelo seu número de temporada NA ordem default. Uma temporada presente nos episódios
// mas ausente do seasons[] (raro) é sintetizada para não perder episódios.
func montarTemporadas(d serieData, ordemDef string, episodios []*provider.EpisodioExterno) []provider.TemporadaExterna {
	porTemp := map[int][]provider.EpisodioExterno{}
	for _, ep := range episodios {
		n := ep.Numbering[ordemDef].Season
		porTemp[n] = append(porTemp[n], *ep)
	}
	temps := []provider.TemporadaExterna{}
	vistos := map[int]bool{}
	for _, s := range d.Seasons {
		if s.Type.Type != ordemDef || vistos[s.Number] {
			continue
		}
		vistos[s.Number] = true
		temps = append(temps, provider.TemporadaExterna{
			Ordering:     ordemDef,
			SeasonNumber: s.Number,
			Year:         s.Year,
			PosterURL:    s.Image,
			Episodios:    porTemp[s.Number],
		})
	}
	for n, eps := range porTemp {
		if !vistos[n] {
			temps = append(temps, provider.TemporadaExterna{Ordering: ordemDef, SeasonNumber: n, Episodios: eps})
		}
	}
	sort.Slice(temps, func(i, j int) bool { return temps[i].SeasonNumber < temps[j].SeasonNumber })
	return temps
}

// setSeAusente grava valor não-vazio em chave não-vazia só se ainda não existe.
func setSeAusente(m map[string]string, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}
