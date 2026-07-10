package tmdb

import "strconv"

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

// externalIdsTMDB é o append `external_ids`: hoje só o imdb_id nos interessa (link
// para a página externa). Outras fontes entram aqui conforme o roadmap.
type externalIdsTMDB struct {
	ImdbID string `json:"imdb_id"`
}

// similarTMDB envelopa os itens similares do append `similar`.
type similarTMDB struct {
	Results []itemSimilarTMDB `json:"results"`
}

// itemSimilarTMDB é um item similar (mesmo media_type do pai). title/release_date são
// de filme; name/first_air_date de série — o pai define o tipo (o similar herda-o).
type itemSimilarTMDB struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	PosterPath   string `json:"poster_path"`
}

// enriquecerCanonicoRico acrescenta ao mapa canônico os campos do DETALHE RICO
// (0022), na convenção do ADR-0018 (traduzíveis como map por idioma, neutros como
// escalares/listas). Só grava chaves com valor — o consumidor distingue "campo
// ausente" de "campo vazio". Não mexe nas chaves já postas por mapearEnvelope.
func enriquecerCanonicoRico(out map[string]any, d detalheTMDB) {
	enriquecerTraduziveisRico(out, d)
	enriquecerNeutrosRico(out, d)
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
// classificação por região, backdrop, nota, vídeos, coleção, similares e links.
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
	if sim := similaresCanonicos(d); len(sim) > 0 {
		out["similar"] = sim
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

// similaresCanonicos converte os similares na lista canônica, herdando o TIPO do pai
// (filme/série) — usado pelo 0023 para navegar ao detalhe externo do similar. Descarta
// itens sem título.
func similaresCanonicos(d detalheTMDB) []any {
	tipo := tipoInferido(d)
	out := make([]any, 0, len(d.Similar.Results))
	for _, it := range d.Similar.Results {
		titulo := primeiroNaoVazio(it.Title, it.Name)
		if titulo == "" {
			continue
		}
		item := map[string]any{
			"source_id": itoa(it.ID),
			"type":      tipo,
			"title":     titulo,
		}
		if ano := anoDe(primeiroNaoVazio(it.ReleaseDate, it.FirstAirDate)); ano != nil {
			item["year"] = *ano
		}
		if it.PosterPath != "" {
			item["poster_url"] = imagemBaseURL + it.PosterPath
		}
		out = append(out, item)
	}
	return out
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
