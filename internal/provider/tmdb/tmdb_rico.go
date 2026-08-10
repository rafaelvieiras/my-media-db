package tmdb

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo concentra o DETALHE RICO do TMDB (briefing 0022): as structs dos
// blocos vindos do append_to_response ampliado e o mapeamento desses blocos para o
// mapa CANÔNICO (nossos nomes de campo). É PURO (sem rede) — roda dentro de
// MapearDetalhe, em tempo de leitura. Os campos TRADUZÍVEIS (tagline, keywords) saem
// como `map[idioma]valor`; os NEUTROS (runtime, certification_by_region, backdrop_url,
// videos, collection, similar, vote_average, external_links) como escalares/listas.
// SEM elenco/equipe (MD6) — decisão do dono, fora desta versão.

// colecaoTMDB é o belongs_to_collection do detalhe de filme (a coleção a que ele
// pertence, ≠ playlist). Só filmes têm; séries vêm sem.
type colecaoTMDB struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	PosterPath string `json:"poster_path"`
}

// videosTMDB envelopa a lista de vídeos (trailer/clip/teaser) do append `videos`.
type videosTMDB struct {
	Results []videoTMDB `json:"results"`
}

// videoTMDB é um vídeo da fonte: nome, chave no provedor (YT/Vimeo), site, tipo
// (Trailer|Clip|Teaser…) e se é oficial. iso_639_1 é o idioma do vídeo (informativo).
type videoTMDB struct {
	Name     string `json:"name"`
	Key      string `json:"key"`
	Site     string `json:"site"`
	Type     string `json:"type"`
	Official bool   `json:"official"`
	Iso639   string `json:"iso_639_1"`
}

// keywordsTMDB envelopa as palavras-chave do append `keywords`. O TMDB usa `keywords`
// para filme e `results` para série — ambos são listas {id,name}. As keywords NÃO são
// localizadas pelo TMDB (vêm num idioma só), então o canônico as ancora sob o idioma
// original do item (o resolver do detalhe cai nelas pela cadeia/último recurso).
type keywordsTMDB struct {
	Keywords []generoTMDB `json:"keywords"` // filme
	Results  []generoTMDB `json:"results"`  // série
}

// itens devolve a lista de keywords, seja de filme (`keywords`) ou série (`results`).
func (k keywordsTMDB) itens() []generoTMDB {
	if len(k.Keywords) > 0 {
		return k.Keywords
	}
	return k.Results
}

// listaPaisTMDB é o append `release_dates` (classificação indicativa de FILME): uma
// lista por país, cada uma com as datas/certificações daquele país.
type listaPaisTMDB struct {
	Results []paisReleaseDatesTMDB `json:"results"`
}

// paisReleaseDatesTMDB são as datas de lançamento de um país e suas certificações.
type paisReleaseDatesTMDB struct {
	Iso3166      string                 `json:"iso_3166_1"`
	ReleaseDates []certificacaoDataTMDB `json:"release_dates"`
}

// certificacaoDataTMDB é uma entrada de release_date com a certificação (pode vir
// vazia em algumas entradas — a primeira não-vazia do país é a que usamos).
type certificacaoDataTMDB struct {
	Certification string `json:"certification"`
}

// contentRatingsTMDB é o append `content_ratings` (classificação indicativa de SÉRIE):
// uma classificação por país.
type contentRatingsTMDB struct {
	Results []paisContentRatingTMDB `json:"results"`
}

// paisContentRatingTMDB é a classificação de série de um país.
type paisContentRatingTMDB struct {
	Iso3166 string `json:"iso_3166_1"`
	Rating  string `json:"rating"`
}

// externalIdsTMDB é o append `external_ids`: imdb_id (link para a página externa) e
// tvdb_id (0087 — o cross-id que o fanart.tv exige para TV; o TMDB já devolve isso de
// graça para /tv/{id}, vazio/inofensivo para /movie/{id}).
type externalIdsTMDB struct {
	ImdbID string         `json:"imdb_id"`
	TvdbID idFlexivelTMDB `json:"tvdb_id"`
}

// idFlexivelTMDB (0090) aceita `tvdb_id` vindo como STRING ou NÚMERO JSON — o TMDB é
// inconsistente entre títulos/endpoints nesse campo especificamente. Antes desta
// tolerância, um único título com tvdb_id numérico fazia o Unmarshal do detalhe
// INTEIRO falhar, derrubando título/sinopse/elenco/tagline junto (mapearEnvelope
// aborta tudo em qualquer erro de subcampo).
type idFlexivelTMDB string

func (id *idFlexivelTMDB) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*id = idFlexivelTMDB(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*id = idFlexivelTMDB(n.String())
	return nil
}

// recommendationsTMDB envelopa os itens recomendados do append `recommendations`
// (0091 — trocado de `similar`, ver tmdb.go).
type recommendationsTMDB struct {
	Results []itemRecommendationTMDB `json:"results"`
}

// itemRecommendationTMDB é um item recomendado (mesmo media_type do pai).
// title/release_date são de filme; name/first_air_date de série — o pai define o
// tipo (o item herda-o). Os campos de QUALIDADE (genre_ids/vote_average/vote_count/
// adult) foram acrescentados no briefing 0091: o payload de `recommendations` já os
// trazia, mas até então eram descartados no unmarshal — servem só para o filtro/
// ordenação de similaresFiltrados, nunca são expostos no canônico (provider.Similar).
type itemRecommendationTMDB struct {
	ID           int64   `json:"id"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	PosterPath   string  `json:"poster_path"`
	GenreIds     []int64 `json:"genre_ids"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	Adult        bool    `json:"adult"`
}

// enriquecerCanonicoRico acrescenta ao mapa canônico os campos do DETALHE RICO
// (0022), na convenção do ADR-0018 (traduzíveis como map por idioma, neutros como
// escalares/listas). Só grava chaves com valor — o consumidor distingue "campo
// ausente" de "campo vazio". Não mexe nas chaves já postas por mapearEnvelope.
func enriquecerCanonicoRico(out map[string]any, d detalheTMDB) {
	enriquecerTraduziveisRico(out, d)
	enriquecerNeutrosRico(out, d)
	enriquecerCreditos(out, d)
}

// enriquecerTraduziveisRico grava os campos ricos TRADUZÍVEIS (map por idioma):
// tagline e keywords.
func enriquecerTraduziveisRico(out map[string]any, d detalheTMDB) {
	if taglines := taglinesPorIdioma(d); len(taglines) > 0 {
		out["tagline"] = taglines
	}
	if kws := keywordsPorIdioma(d); len(kws) > 0 {
		out["keywords"] = kws
	}
}

// enriquecerNeutrosRico grava os campos ricos NEUTROS (escalares/listas): duração,
// classificação por região, backdrop, nota, vídeos, coleção e links. Os SIMILARES
// (0091) NÃO são mais preenchidos aqui — essa responsabilidade migrou inteiramente
// para o método Similares (provider.SimilarProvider), chamado explicitamente pelo
// servidor (entities.go/external_detail.go) fora de MapearDetalhe, para o filtro/
// ordenação por afinidade ser um ponto de extensão de verdade (ver provider.go).
func enriquecerNeutrosRico(out map[string]any, d detalheTMDB) {
	if r := runtimeDe(d); r > 0 {
		out["runtime"] = r
	}
	if cert := certificacoesPorRegiao(d); len(cert) > 0 {
		out["certification_by_region"] = cert
	}
	if d.BackdropPath != "" {
		out["backdrop_url"] = backdropBaseURL + d.BackdropPath
	}
	if d.VoteAverage > 0 {
		out["vote_average"] = d.VoteAverage
	}
	if vids := videosCanonicos(d.Videos); len(vids) > 0 {
		out["videos"] = vids
	}
	if col := colecaoCanonica(d.BelongsToCollection); col != nil {
		out["collection"] = col
	}
	if links := linksExternos(d); len(links) > 0 {
		out["external_links"] = links
	}
}

// taglinesPorIdioma monta o mapa idioma→slogan a partir das translations do detalhe,
// chaveando cada idioma pelo código COMBINADO (pt-BR) e pelo BASE (pt) — mesma
// convenção dos títulos (ver traduzidosPorIdioma). O slogan do topo do detalhe vem no
// idioma da requisição e também aparece nas translations, então não precisa âncora.
func taglinesPorIdioma(d detalheTMDB) map[string]any {
	out := map[string]any{}
	for _, tr := range d.Translations.Translations {
		combinado := combinarIdioma(tr.Iso639, tr.Iso3166)
		if combinado == "" || tr.Data.Tagline == "" {
			continue
		}
		porSeAusenteAny(out, combinado, tr.Data.Tagline)
		porSeAusenteAny(out, tr.Iso639, tr.Data.Tagline)
	}
	return out
}

// keywordsPorIdioma devolve as palavras-chave sob o idioma ORIGINAL do item (o TMDB
// não as localiza). Fica na dimensão idioma×camada (campo traduzível, decisão da
// revisão), mas populado num idioma só; o resolver do detalhe cai nele pela cadeia ou
// pelo último recurso. Sem idioma original, não há como ancorar — devolve vazio.
func keywordsPorIdioma(d detalheTMDB) map[string]any {
	nomes := nomesDosGeneros(d.Keywords.itens())
	if len(nomes) == 0 || d.OriginalLanguage == "" {
		return nil
	}
	return map[string]any{d.OriginalLanguage: nomes}
}

// runtimeDe devolve a duração em minutos: `runtime` para filme; o primeiro
// `episode_run_time` para série. 0 quando a fonte não informou.
func runtimeDe(d detalheTMDB) int {
	if d.Runtime > 0 {
		return d.Runtime
	}
	for _, r := range d.EpisodeRunTime {
		if r > 0 {
			return r
		}
	}
	return 0
}

// certificacoesPorRegiao monta o mapa REGIÃO→classificação (neutro; o handler escolhe
// a região do idioma ativo com fallback US). Vem de release_dates (filme) ou
// content_ratings (série). Para filme, a primeira certificação não-vazia do país vence.
func certificacoesPorRegiao(d detalheTMDB) map[string]any {
	out := map[string]any{}
	for _, pais := range d.ReleaseDates.Results {
		if pais.Iso3166 == "" {
			continue
		}
		for _, rd := range pais.ReleaseDates {
			if rd.Certification != "" {
				out[pais.Iso3166] = rd.Certification
				break
			}
		}
	}
	for _, pais := range d.ContentRatings.Results {
		if pais.Iso3166 != "" && pais.Rating != "" {
			if _, existe := out[pais.Iso3166]; !existe {
				out[pais.Iso3166] = pais.Rating
			}
		}
	}
	return out
}

// videosCanonicos converte os vídeos da fonte na lista canônica (mapas), montando a
// URL do provedor quando conhecido (YouTube/Vimeo). Descarta vídeos sem chave. A
// ordenação "locais em destaque" NÃO se aplica ao externo (não há arquivo local) — a
// ordem é a da fonte; a UI (0023) decide a apresentação.
func videosCanonicos(v videosTMDB) []any {
	out := make([]any, 0, len(v.Results))
	for _, vid := range v.Results {
		if vid.Key == "" {
			continue
		}
		item := map[string]any{
			"name":     vid.Name,
			"key":      vid.Key,
			"site":     vid.Site,
			"type":     vid.Type,
			"official": vid.Official,
		}
		if u := urlDoVideo(vid.Site, vid.Key); u != "" {
			item["url"] = u
		}
		out = append(out, item)
	}
	return out
}

// urlDoVideo monta a URL pública do vídeo pelo site do provedor; "" quando o site é
// desconhecido (a UI pode só exibir o nome/thumb nesse caso).
func urlDoVideo(site, key string) string {
	switch site {
	case "YouTube":
		return "https://www.youtube.com/watch?v=" + key
	case "Vimeo":
		return "https://vimeo.com/" + key
	default:
		return ""
	}
}

// colecaoCanonica converte belongs_to_collection na coleção canônica (id como
// source_id para navegar no 0023, nome e pôster). nil quando o item não pertence a
// coleção (ou é série).
func colecaoCanonica(c *colecaoTMDB) map[string]any {
	if c == nil || c.ID == 0 {
		return nil
	}
	out := map[string]any{
		"source_id": itoa(c.ID),
		"name":      c.Name,
	}
	if c.PosterPath != "" {
		out["poster_url"] = imagemBaseURL + c.PosterPath
	}
	return out
}

// votoMinimoSimilar é o piso de vote_count abaixo do qual um item recomendado é
// descartado (briefing 0091): volume de voto é um proxy melhor de "recomendação de
// um título real, conhecido, não ruído" do que a nota média isolada — um título com
// 2 votos e nota 8 não é "melhor" que um com 500 votos e nota 6.5. Não há piso de
// nota: mesmo um título de nota mediana é uma recomendação válida se for do gênero
// certo e amplamente assistido.
const votoMinimoSimilar = 20

// similaresFiltrados filtra e ordena os itens de `d.Recommendations.Results` por
// qualidade e por afinidade com o item de ORIGEM (briefing 0091):
//  1. descarta item adulto (`Adult == true`) — nunca conteúdo adulto na lista;
//  2. descarta item com `VoteCount` abaixo de votoMinimoSimilar (ruído estatístico);
//  3. ordena o que sobra por (a) sobreposição de gênero com o item de origem, DESC —
//     usando os IDs numéricos de `d.Genres` (o item de origem, MESMO payload), nunca
//     nomes de tag traduzidos/resolvidos (evita mismatch de idioma/taxonomia entre
//     fontes); (b) `VoteAverage` DESC como desempate.
//
// Não trunca a lista além do filtro — o que sobra é devolvido inteiro (SliceStable
// preserva a ordem original da fonte em empate total).
func similaresFiltrados(d detalheTMDB) []itemRecommendationTMDB {
	generosOrigem := make(map[int64]struct{}, len(d.Genres))
	for _, g := range d.Genres {
		generosOrigem[g.ID] = struct{}{}
	}
	overlapDe := func(it itemRecommendationTMDB) int {
		n := 0
		for _, gid := range it.GenreIds {
			if _, ok := generosOrigem[gid]; ok {
				n++
			}
		}
		return n
	}

	itens := make([]itemRecommendationTMDB, 0, len(d.Recommendations.Results))
	for _, it := range d.Recommendations.Results {
		if it.Adult {
			continue
		}
		if it.VoteCount < votoMinimoSimilar {
			continue
		}
		itens = append(itens, it)
	}

	sort.SliceStable(itens, func(i, j int) bool {
		oi, oj := overlapDe(itens[i]), overlapDe(itens[j])
		if oi != oj {
			return oi > oj
		}
		return itens[i].VoteAverage > itens[j].VoteAverage
	})
	return itens
}

// similaresCanonicos converte os itens JÁ FILTRADOS/ORDENADOS (similaresFiltrados)
// na lista canônica (provider.Similar), herdando o TIPO do pai (filme/série) —
// usado pelo 0023 para navegar ao detalhe externo do similar. Descarta itens sem
// título (mesma regra de sempre).
func similaresCanonicos(d detalheTMDB) []provider.Similar {
	tipo := tipoInferido(d)
	filtrados := similaresFiltrados(d)
	out := make([]provider.Similar, 0, len(filtrados))
	for _, it := range filtrados {
		titulo := primeiroNaoVazio(it.Title, it.Name)
		if titulo == "" {
			continue
		}
		item := provider.Similar{
			SourceID: itoa(it.ID),
			Type:     tipo,
			Title:    titulo,
		}
		if ano := anoDe(primeiroNaoVazio(it.ReleaseDate, it.FirstAirDate)); ano != nil {
			item.Year = ano
		}
		if it.PosterPath != "" {
			u := imagemBaseURL + it.PosterPath
			item.PosterURL = &u
		}
		out = append(out, item)
	}
	return out
}

// Similares implementa provider.SimilarProvider (briefing 0091): extrai os itens
// similares/recomendados de um payload de detalhe JÁ buscado por BuscarDetalhe —
// PURO, sem ida extra à rede (mesmo padrão de TvdbIDDoDetalheSerie). Aceita tanto o
// envelope AUMENTADO {detail,tags_by_lang} (0020) quanto o payload LEGADO sem
// envelope (0015), via detalheBrutoDoRaw (mesma detecção de MapearDetalhe,
// extraída em tmdb.go para os dois métodos reusarem sem duplicar o parsing).
func (c *Client) Similares(raw json.RawMessage) ([]provider.Similar, error) {
	var d detalheTMDB
	if err := json.Unmarshal(detalheBrutoDoRaw(raw), &d); err != nil {
		return nil, fmt.Errorf("tmdb: detalhe invalido para similares: %w", err)
	}
	return similaresCanonicos(d), nil
}

// linksExternos monta o mapa de links para páginas externas. Hoje só o imdb (a partir
// do imdb_id); o link do próprio TMDB o handler monta (precisa do tipo+id da rota).
func linksExternos(d detalheTMDB) map[string]any {
	out := map[string]any{}
	if d.ExternalIds.ImdbID != "" {
		out["imdb"] = "https://www.imdb.com/title/" + d.ExternalIds.ImdbID
	}
	return out
}

// tipoInferido deduz o media_type do detalhe: filme tem `title`; série tem `name` (o
// detalhe de série não traz `title`). Default movie (o caso mais comum).
func tipoInferido(d detalheTMDB) string {
	if d.Title == "" && d.Name != "" {
		return "tv"
	}
	return "movie"
}

// porSeAusenteAny grava valor não-vazio em chave não-vazia só se a chave ainda não
// existe (primeira tradução por idioma vence — como porSeAusente, para map[string]any).
func porSeAusenteAny(m map[string]any, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}

// itoa formata um id inteiro do TMDB como string (source_id canônico).
func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}

// TvdbIDDoDetalheSerie extrai o tvdb_id do append external_ids de um detalhe de série JÁ
// BUSCADO (o mesmo raw devolvido por BuscarDetalhe/PayloadFresco) — SEM ida extra à
// rede. Satisfaz provider.TvdbCruzadoProvider (0087): fecha a lacuna que deixava toda
// série cuja árvore vem só do TMDB (sem TVDB configurado, briefing 0046) sem id
// utilizável para o fanart.tv, que na TV exige tvdb_id exclusivamente (rotaETabela, case
// "tv"). Aceita tanto o envelope AUMENTADO {detail,tags_by_lang} (0020) quanto o detalhe
// cru sem envelope (payload legado do 0015) — se não há `detail` no envelope, tenta o raw
// direto. ok=false sem tvdb_id ou payload ilegível.
func (c *Client) TvdbIDDoDetalheSerie(raw json.RawMessage) (string, bool) {
	var d struct {
		ExternalIds externalIdsTMDB `json:"external_ids"`
	}
	if err := json.Unmarshal(detalheBrutoDoRaw(raw), &d); err != nil {
		return "", false
	}
	return string(d.ExternalIds.TvdbID), d.ExternalIds.TvdbID != ""
}
