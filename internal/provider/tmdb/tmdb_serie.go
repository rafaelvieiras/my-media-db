package tmdb

// Este arquivo implementa a capacidade provider.SerieProvider (ADR-0021 / briefing
// 0046): a ÁRVORE da série montada a PARTIR DO TMDB — temporadas + episódios de grão
// fino ancorados no episode.id, cada um com a numeração da ordem "official" (a única
// que o TMDB modela: ele nao expõe season types como o TheTVDB). Isso garante a
// INDEPENDÊNCIA DE PROVIDER: com só a chave do TMDB cadastrada, séries ainda importam
// (o TVDB deixa de ser a fonte obrigatória da árvore).
//
// CAMINHO: /tv/{id} traz a LISTA de temporadas (seasons[]); /tv/{id}/season/{n} traz os
// episódios daquela temporada. A i18n espelha o TVDB — o conteúdo base sai sob o idioma
// original da série e cada idioma pedido é buscado com ?language= e mesclado por
// episode.id (sem N chamadas por episódio). O TMDB aceita nossos códigos de app
// (pt-BR/en) direto no ?language=, então não há tabela de códigos como no TVDB.

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
	// ordemOficial é a única ordenação (season type) que o TMDB modela: os episódios
	// numeram por (season_number, episode_number) oficial, sem ordens alternativas.
	ordemOficial = "official"
	// stillBaseURL é a base dos stills (frame do episódio) do TMDB, num tamanho de
	// paisagem (w300) — a URL crua vai ao extension_data e o proxy de imagem (0042) a
	// busca/cacheia sob demanda na 1ª visualização, nunca no import.
	stillBaseURL = "https://image.tmdb.org/t/p/w300"
)

// BuscarSerieArvore cumpre provider.SerieProvider: devolve a árvore da série `sourceID`
// nos idiomas `langs` (com fallback). O TMDB só tem a ordem "official", então
// DefaultSeasonType/SeasonTypes refletem apenas ela; TODAS as temporadas do /tv (inclui
// a 0/especiais) viram TemporadaExterna. ErrNaoEncontrado (404) e erros de rede propagam.
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
	return provider.SerieArvore{
		DefaultSeasonType: ordemOficial,
		SeasonTypes:       []string{ordemOficial},
		Temporadas:        temporadas,
	}, nil
}

// respSerieArvoreTMDB espelha o /tv/{id} para a árvore (só os campos usados): o idioma
// original (âncora do fallback) e a lista de temporadas (número, nome, ano, pôster).
type respSerieArvoreTMDB struct {
	OriginalLanguage string                `json:"original_language"`
	Seasons          []temporadaResumoTMDB `json:"seasons"`
}

// temporadaResumoTMDB é uma temporada como vem no seasons[] do /tv (resumo, sem
// episódios) — os episódios vêm do /tv/{id}/season/{n}.
type temporadaResumoTMDB struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	AirDate      string `json:"air_date"`
	PosterPath   string `json:"poster_path"`
}

// respTemporadaTMDB espelha o /tv/{id}/season/{n}: o nome/ano/pôster localizados da
// temporada e a lista de episódios (com o conteúdo no idioma pedido em ?language=).
type respTemporadaTMDB struct {
	Name       string         `json:"name"`
	AirDate    string         `json:"air_date"`
	PosterPath string         `json:"poster_path"`
	Episodes   []episodioTMDB `json:"episodes"`
}

// episodioTMDB é um episódio cru do /season/{n}. name/overview vêm no idioma de
// ?language= (vazios quando não há tradução naquele idioma).
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

// serieParaArvore puxa o /tv/{id} e devolve a lista de temporadas + idioma original.
// ErrNaoEncontrado (via getBytes) propaga para o handler responder 404.
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

// montarTemporadaTMDB monta uma TemporadaExterna: puxa a temporada no idioma ORIGINAL
// (a base/âncora), constrói os episódios e mescla as traduções dos idiomas pedidos por
// episode.id. Erro na temporada-base propaga (a árvore é incompleta sem ela); traduções
// que falhem degradam (o idioma fica ausente no mapa).
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

// buscarTemporadaTMDB puxa o /tv/{id}/season/{n} localizado em `lang` (vazio cai no
// default da fonte). Erro de rede/parse propaga.
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

// montarEpisodiosTMDB cria os EpisodioExterno da temporada, preservando a ordem da
// fonte, e devolve o índice id→*episódio (para mesclar as traduções). A numeração
// "official" já entra; o conteúdo base vai sob o idioma original da série.
//
// O índice guarda ponteiros PARA DENTRO do slice `episodios`: isso só é seguro porque a
// capacidade é reservada em len(eps) e os appends nunca a estouram (sem realocação — os
// ponteiros permanecem válidos). Manter esse invariante ao mexer aqui.
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

// mesclarTemporadaTraduzida busca a temporada em cada idioma pedido (≠ original) e mescla
// name/overview por episode.id, além do NOME da temporada. Best-effort: idioma que falhe
// é pulado (fallback = ausente no mapa). Idiomas repetidos e o original são pulados.
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

// aplicarTraducaoTMDB grava name/overview não-vazios de cada episódio traduzido sob as
// chaves de idioma do app (combinado + base).
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

// anoTemporada extrai o ano (4 dígitos) do air_date da temporada (do resumo ou do
// detalhe). "" quando a fonte não informou data.
func anoTemporada(resumo temporadaResumoTMDB, base respTemporadaTMDB) string {
	data := primeiroNaoVazio(resumo.AirDate, base.AirDate)
	if len(data) < 4 {
		return ""
	}
	return data[:4]
}

// codigosAppTMDB devolve as chaves de app sob as quais gravar uma tradução de `lang`:
// o próprio código (ex.: "pt-BR") e o base (ex.: "pt"), para o resolver casar tanto o
// combinado quanto o base — mesma convenção do resto do provider.
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

// imagemDireta monta a URL direta de uma imagem do TMDB (base + path), ou "" quando não
// há path.
func imagemDireta(baseURL, path string) string {
	if path == "" {
		return ""
	}
	return baseURL + path
}

// setSeAusenteStr grava valor não-vazio em chave não-vazia só se ainda não existe.
func setSeAusenteStr(m map[string]string, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}
