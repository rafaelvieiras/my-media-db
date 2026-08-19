// Package musicbrainz implementa o provider BASE da vertical música (briefing 0106,
// ADR-0027 item 6): busca de artista, montagem da árvore artista→álbuns→faixas via a API
// pública do MusicBrainz (https://musicbrainz.org/ws/2/, sem chave), e arte de capa de
// álbum via o Cover Art Archive (https://coverartarchive.org/, mesmo grupo de projetos,
// host separado). Segue o mesmo padrão de internal/provider/themerrdb (o único outro
// provider sem credencial hoje): o Client implementa o core provider.Provider com
// Buscar/BuscarDetalhe/MapearDetalhe MÍNIMOS (só o suficiente para busca/import de
// artista), e as capacidades reais são provider.MusicaProvider (árvore) e
// provider.ArteProvider (capa via CAA).
//
// O MusicBrainz exige <= 1 requisição/s por IP e um User-Agent identificando a aplicação
// — sem isso bloqueia por abuso (https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting).
// Todo GET a musicbrainz.org passa pelo limitador em ratelimit.go; o Cover Art Archive não
// documenta limite equivalente, então NÃO é limitado aqui (só ganha o mesmo User-Agent,
// por boa prática).
package musicbrainz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// baseURLPadrao é a base estável da API v2 do MusicBrainz (JSON via ?fmt=json).
	baseURLPadrao = "https://musicbrainz.org/ws/2"
	// caaBaseURLPadrao é a base do Cover Art Archive — host SEPARADO do MusicBrainz
	// (mesmo grupo de projetos, infra distinta). Não é parâmetro de New() por
	// simplicidade (nenhum outro provider do repo precisou de 2 bases até hoje); os
	// testes injetam um valor de teste diretamente no campo `caaBaseURL` do Client
	// (mesmo pacote, sem API pública para isso — ver musicbrainz_test.go).
	caaBaseURLPadrao = "https://coverartarchive.org"
	// timeoutPadrao limita cada requisição; estouro vira erro (o enrich degrada).
	timeoutPadrao = 8 * time.Second
	// intervaloRateLimit é o intervalo mínimo entre duas requisições a musicbrainz.org
	// (briefing 0106 §3: "MusicBrainz exige <= 1 requisição/s").
	intervaloRateLimit = time.Second
	// nomeFonte é o Nome() da fonte (também o source em entity/media_external).
	nomeFonte = "musicbrainz"
	// userAgent é o header OBRIGATÓRIO em toda chamada ao MusicBrainz — a API bloqueia
	// por abuso sem um User-Agent identificando a aplicação e um contato. O wiring de
	// produção (cmd/mymediadb/main.go, fora deste briefing) poderia futuramente
	// substituir a versão fixa por main.version; por ora um valor estável é suficiente
	// (o requisito da API é IDENTIFICAR a app de forma consistente, não versionar à
	// risca) — ver nota no README do briefing 0106.
	userAgent = "MyMediaDB/dev (+https://github.com/rafaelvieiras/my-media-db)"
)

// Client é o provider MusicBrainz + Cover Art Archive. httpClient e baseURL são
// injetáveis para teste (apontar a um httptest.Server sem rede real); caaBaseURL só é
// injetável internamente (mesmo pacote) — ver comentário de caaBaseURLPadrao.
type Client struct {
	httpClient *http.Client
	baseURL    string
	caaBaseURL string
	lim        *limitador
}

// New cria o client. httpClient nil usa um default com timeout; baseURL vazio usa a base
// pública do MusicBrainz. Sem credencial: pode ser sempre registrado (cmd/mymediadb/main.go,
// provedoresReais — mesmo padrão de internal/provider/themerrdb.New).
func New(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeoutPadrao}
	}
	if baseURL == "" {
		baseURL = baseURLPadrao
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		caaBaseURL: caaBaseURLPadrao,
		lim:        novoLimitador(intervaloRateLimit),
	}
}

// Nome identifica a fonte.
func (c *Client) Nome() string { return nomeFonte }

// Buscar consulta o MusicBrainz por artista (capacidade core provider.Provider). Só
// responde a `tipo=="artista"` (ou vazio) — outros tipos devolvem vazio, sem poluir a
// busca de filme/série. `lang` é ignorado: a busca textual do MusicBrainz não filtra por
// idioma.
func (c *Client) Buscar(ctx context.Context, query, tipo, lang string) ([]provider.ResultadoExterno, error) {
	_ = lang
	if tipo != "" && tipo != "artista" {
		return nil, nil
	}
	if query == "" {
		return nil, nil
	}

	params := url.Values{}
	params.Set("query", query)
	body, err := c.getMB(ctx, "/artist", params)
	if err != nil {
		return nil, err
	}

	var resp respBuscaArtista
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("musicbrainz: resposta de busca invalida: %w", err)
	}

	out := make([]provider.ResultadoExterno, 0, len(resp.Artists))
	for _, a := range resp.Artists {
		if a.ID == "" || a.Name == "" {
			continue
		}
		r := provider.ResultadoExterno{
			Source:   nomeFonte,
			SourceID: a.ID,
			Type:     "artista",
			Title:    a.Name,
			Saved:    false,
		}
		// Disambiguation (ex.: "English rock band") ajuda o dono a escolher entre
		// artistas homônimos (briefing 0106 §6); reaproveitamos Overview, o único
		// campo de texto livre do shape genérico sem semântica conflitante.
		if a.Disambiguation != "" {
			d := a.Disambiguation
			r.Overview = &d
		}
		out = append(out, r)
	}
	return out, nil
}

// BuscarDetalhe puxa o detalhe CRU do artista (capacidade core provider.Provider,
// suficiente para o import genérico existente). `tipo`/`langs` não têm efeito aqui: o
// MusicBrainz não modela detalhe por tipo, e não é multi-idioma.
func (c *Client) BuscarDetalhe(ctx context.Context, sourceID, tipo string, langs []string) (json.RawMessage, error) {
	_, _ = tipo, langs
	body, err := c.getMB(ctx, "/artist/"+url.PathEscape(sourceID), url.Values{})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// MapearDetalhe extrai só o nome do artista para o shape canônico — o suficiente para o
// import genérico (0015) dar título à entity `artista`; o resto da árvore (álbuns/faixas)
// entra por importarArtista (internal/enrich/musica.go, fora deste pacote).
func (c *Client) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	var d struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("musicbrainz: detalhe invalido: %w", err)
	}
	out := map[string]any{}
	if d.Name != "" {
		out["title"] = d.Name
	}
	return out, nil
}

// BuscarArtistaArvore monta a árvore completa do artista (capacidade
// provider.MusicaProvider): 1 chamada para o artista+release-groups, e por álbum, 1
// chamada para escolher a release "oficial" (release-group?inc=releases+genres, que já
// traz o gênero de maior contagem numa tacada só) + 1 chamada para as faixas dessa release
// (release?inc=recordings+artist-credits+isrcs). Custo: ~1+2N requisições para N álbuns, a
// <= 1 req/s (briefing 0106 §6: aceitável, é operação explícita/assíncrona do dono).
func (c *Client) BuscarArtistaArvore(ctx context.Context, sourceID string, langs []string) (provider.ArtistaArvore, error) {
	_ = langs // MusicBrainz nao e multi-idioma (ver doc da interface); reservado

	body, err := c.getMB(ctx, "/artist/"+url.PathEscape(sourceID), url.Values{"inc": {"release-groups+annotation"}})
	if err != nil {
		return provider.ArtistaArvore{}, err
	}
	var art respArtistaArvore
	if err := json.Unmarshal(body, &art); err != nil {
		return provider.ArtistaArvore{}, fmt.Errorf("musicbrainz: artista invalido: %w", err)
	}

	arvore := provider.ArtistaArvore{
		Name: art.Name,
		MBID: sourceID,
		Bio:  art.Annotation,
	}
	for _, rg := range art.ReleaseGroups {
		if rg.ID == "" {
			continue
		}
		// QUALQUER erro (não só 404) degrada este ÁLBUM pra um shell sem faixas, nunca
		// aborta a árvore inteira -- achado real na validação em produção: o servidor
		// público do MusicBrainz é instável o bastante (503 em ~1/3 das chamadas) que,
		// numa árvore de dezenas de álbuns (1+2N chamadas), mesmo com retry (getMB) a
		// chance de UMA chamada específica esgotar as tentativas não é desprezível. Um
		// artista com discografia grande é bem mais útil parcialmente importado (álbuns
		// sem faixa, corrigível depois via reimport) do que falhando por inteiro.
		album := c.montarAlbum(ctx, rg.ID, rg.Title, rg.FirstReleaseDate)
		arvore.Albuns = append(arvore.Albuns, album)
	}
	return arvore, nil
}

// montarAlbum busca o release-group (releases+genres, 1 chamada), escolhe a release
// "oficial" a representar o álbum, e busca as faixas dessa release (1 chamada). NUNCA
// devolve erro: release-group sem nenhuma release, 404 pontual, ou esgotamento do retry de
// getMB (instabilidade do servidor público) todos degradam pro mesmo shell -- álbum com
// nome/MBID/ano mas sem faixas -- em vez de derrubar a árvore inteira (ver
// BuscarArtistaArvore). Faixas ausentes por causa disso são corrigíveis num reimport
// posterior (find-or-create por MBID, idempotente).
func (c *Client) montarAlbum(ctx context.Context, rgID, title, firstReleaseDate string) provider.AlbumExterno {
	shell := provider.AlbumExterno{Name: title, MBID: rgID, Year: anoDe(firstReleaseDate)}

	body, err := c.getMB(ctx, "/release-group/"+url.PathEscape(rgID), url.Values{"inc": {"releases+genres"}})
	if err != nil {
		return shell
	}
	var rg respReleaseGroup
	if err := json.Unmarshal(body, &rg); err != nil {
		return shell
	}

	album := shell
	album.Genre = generoPrincipal(rg.Genres)

	releaseID, ok := escolherRelease(rg.Releases)
	if !ok {
		return album // release-group sem nenhuma release: album sem faixas
	}

	faixas, err := c.buscarFaixas(ctx, releaseID)
	if err != nil {
		return album // release indisponivel (404 ou retry esgotado): degrada sem faixas
	}
	album.Faixas = faixas
	return album
}

// escolherRelease escolhe QUAL release representa o álbum para efeito de tracklist,
// dentre as várias pressings/edições que um release-group tipicamente tem (no mundo real,
// dezenas — reedições, países, remasters). Prefere a primeira com status "Official"
// (convenção do próprio MusicBrainz para a edição "canônica"); sem nenhuma oficial, cai na
// primeira da lista. ok=false só quando a lista está vazia.
func escolherRelease(releases []respReleaseResumo) (string, bool) {
	for _, r := range releases {
		if r.Status == "Official" {
			return r.ID, true
		}
	}
	if len(releases) > 0 {
		return releases[0].ID, true
	}
	return "", false
}

// buscarFaixas busca a release escolhida com as faixas (recordings), o artist-credit de
// cada uma (para detectar divergência do artista do álbum, ADR-0027 item 4) e os ISRCs.
func (c *Client) buscarFaixas(ctx context.Context, releaseID string) ([]provider.FaixaExterna, error) {
	body, err := c.getMB(ctx, "/release/"+url.PathEscape(releaseID), url.Values{"inc": {"recordings+artist-credits+isrcs"}})
	if err != nil {
		return nil, err
	}
	var rel respRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("musicbrainz: release invalido: %w", err)
	}

	discTotal := len(rel.Media)
	var faixas []provider.FaixaExterna
	for _, media := range rel.Media {
		for _, tr := range media.Tracks {
			credito := tr.Recording.ArtistCredit
			if len(credito) == 0 {
				credito = tr.ArtistCredit // defensivo: fonte raramente omite no recording
			}
			var creditoNome, creditoMBID string
			if len(credito) > 0 {
				creditoNome = credito[0].Artist.Name
				creditoMBID = credito[0].Artist.ID
			}
			var isrc string
			if len(tr.Recording.ISRCs) > 0 {
				isrc = tr.Recording.ISRCs[0]
			}
			titulo := tr.Recording.Title
			if titulo == "" {
				titulo = tr.Title
			}
			faixas = append(faixas, provider.FaixaExterna{
				Title:              titulo,
				MBID:               tr.Recording.ID,
				TrackNumber:        tr.Position,
				DiscNumber:         media.Position,
				DiscTotal:          discTotal,
				ISRC:               isrc,
				ArtistaCreditoNome: creditoNome,
				ArtistaCreditoMBID: creditoMBID,
			})
		}
	}
	return faixas, nil
}

// anoDe extrai os 4 primeiros dígitos de uma data "YYYY-MM-DD"/"YYYY-MM"/"YYYY" do
// MusicBrainz. Devolve "" para datas curtas demais ou ausentes.
func anoDe(data string) string {
	if len(data) < 4 {
		return ""
	}
	return data[:4]
}

// generoPrincipal escolhe o gênero de MAIOR contagem (voto) da lista que o MusicBrainz
// devolve (ADR-0027 item 7: um único gênero por álbum, não por faixa). Empate: mantém a
// ordem em que a fonte listou (sort.SliceStable). Lista vazia devolve "".
func generoPrincipal(generos []generoMB) string {
	if len(generos) == 0 {
		return ""
	}
	ordenados := make([]generoMB, len(generos))
	copy(ordenados, generos)
	sort.SliceStable(ordenados, func(i, j int) bool { return ordenados[i].Count > ordenados[j].Count })
	return ordenados[0].Name
}

// BuscarArtes lista a capa do álbum via Cover Art Archive (capacidade
// provider.ArteProvider). Só responde a `tipo=="album"` (outros tipos são território de
// fanart.tv/TMDB). Usa ids.Mbid, que é o MBID de RELEASE-GROUP (o mesmo gravado na entity
// `album` — ver comentário de ExternalIDs.Mbid em internal/provider/provider.go), contra o
// endpoint /release-group/{mbid} do CAA — que a documentação oficial confirma devolver o
// MESMO shape JSON de /release/{mbid}, agregando a arte de qualquer release do grupo, sem
// exigir que o chamador escolha uma release específica. Sem id ou sem match (404): (nil,
// nil), graça. Erro real propaga.
func (c *Client) BuscarArtes(ctx context.Context, ids provider.ExternalIDs, tipo string, langs []string) ([]provider.Arte, error) {
	_ = langs
	if tipo != "album" || ids.Mbid == "" {
		return nil, nil
	}
	body, ok, err := c.getCAA(ctx, "/release-group/"+url.PathEscape(ids.Mbid))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var resp respCoverArt
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("musicbrainz: resposta do cover art archive invalida: %w", err)
	}

	var out []provider.Arte
	for _, img := range resp.Images {
		if img.Image == "" {
			continue
		}
		if img.Front {
			out = append(out, provider.Arte{Kind: "poster", URL: img.Image})
		}
		if img.Back {
			out = append(out, provider.Arte{Kind: "back", URL: img.Image})
		}
	}
	return out, nil
}

// ResolverArtistaDaFaixa resolve o MBID do artista a partir do MBID de RECORDING
// (capacidade provider.FaixaResolverProvider, briefing 0107): cobre o caminho em que
// o arquivo só tem o MBID de faixa embutido pelo tagueador (sem o MBID de artista),
// comum o bastante para justificar esta 2ª ida à fonte. Usa o mesmo rate-limiter e
// User-Agent de getMB (1 chamada, sem custo extra de N álbuns como
// BuscarArtistaArvore). ErrNaoEncontrado quando o recording não existe; um recording
// existente mas sem NENHUM artist-credit (dado incompleto na fonte, raríssimo) é um
// erro genérico -- não há artista nenhum para devolver.
func (c *Client) ResolverArtistaDaFaixa(ctx context.Context, recordingMBID string) (string, error) {
	body, err := c.getMB(ctx, "/recording/"+url.PathEscape(recordingMBID), url.Values{"inc": {"artist-credits"}})
	if err != nil {
		return "", err
	}
	var rec struct {
		ArtistCredit []creditoMB `json:"artist-credit"`
	}
	if err := json.Unmarshal(body, &rec); err != nil {
		return "", fmt.Errorf("musicbrainz: recording invalido: %w", err)
	}
	if len(rec.ArtistCredit) == 0 || rec.ArtistCredit[0].Artist.ID == "" {
		return "", fmt.Errorf("musicbrainz: recording %s sem artist-credit", recordingMBID)
	}
	return rec.ArtistCredit[0].Artist.ID, nil
}

// tentativasMB é o número MÁXIMO de tentativas de getMB por chamada -- o servidor
// público do MusicBrainz devolve 503 "currently busy" com frequência real observada
// em produção (~1 em 3 chamadas na validação deste briefing), e uma árvore de artista
// inteira faz 1+2N requisições sequenciais (N álbuns): sem retry, a probabilidade de
// TODA a sequência dar certo cai pra perto de zero num artista com discografia grande.
const tentativasMB = 3

// backoffMB é o atraso entre tentativas em cima de um 502/503/504 -- multiplicado pelo
// número da tentativa (1ª retry espera backoffMB, 2ª espera 2×backoffMB).
const backoffMB = 500 * time.Millisecond

// getMB faz um GET RATE-LIMITADO (1 req/s) a musicbrainz.org com o User-Agent obrigatório
// e `fmt=json` sempre presente. 404 vira erro-sentinela provider.ErrNaoEncontrado
// (errors.Is), sem retry (não é transitório). 502/503/504 (instabilidade documentada do
// servidor público) tenta de novo até tentativasMB vezes, com backoff crescente; outro
// status != 200 é erro genérico sem retry. I/O propaga sem retry (rede local instável não
// é o problema que este retry resolve).
func (c *Client) getMB(ctx context.Context, path string, params url.Values) ([]byte, error) {
	var ultimoErr error
	for tentativa := 1; tentativa <= tentativasMB; tentativa++ {
		if tentativa > 1 {
			c.lim.dormirFn(backoffMB * time.Duration(tentativa-1))
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		body, status, err := c.tentarGetMB(ctx, path, params)
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			return nil, fmt.Errorf("musicbrainz: %s: %w", path, provider.ErrNaoEncontrado)
		}
		if status == http.StatusOK {
			return body, nil
		}
		if !transitorioMB(status) {
			return nil, fmt.Errorf("musicbrainz: status inesperado %d em %s", status, path)
		}
		ultimoErr = fmt.Errorf("musicbrainz: status inesperado %d em %s", status, path)
	}
	return nil, fmt.Errorf("musicbrainz: %s: esgotou %d tentativas: %w", path, tentativasMB, ultimoErr)
}

// transitorioMB diz se `status` é o tipo de instabilidade do servidor público do
// MusicBrainz que vale a pena tentar de novo (502/503/504) -- nunca 4xx, que é erro do
// pedido em si e não some numa segunda tentativa.
func transitorioMB(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// tentarGetMB faz UMA tentativa de GET (sem retry) e devolve o status bruto pro chamador
// decidir -- separado de getMB só para isolar o loop de retry da montagem do request.
func (c *Client) tentarGetMB(ctx context.Context, path string, params url.Values) ([]byte, int, error) {
	c.lim.esperar()

	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, 0, err
	}
	q := params
	if q == nil {
		q = url.Values{}
	}
	q.Set("fmt", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Corpo de erro não nos interessa (a mensagem já é genérica o bastante nos dois
		// status tratados por getMB), mas precisa ser drenado para o keep-alive da conexão.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, resp.StatusCode, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("musicbrainz: ler %s: %w", path, err)
	}
	return body, resp.StatusCode, nil
}

// getCAA faz um GET ao Cover Art Archive — SEM rate-limit (a fonte não documenta um
// limite; ver comentário do package), mas com o mesmo User-Agent por boa prática. ok=false
// num 404 (release/release-group sem arte, ou mbid inexistente — a própria CAA não
// distingue os dois casos; não é erro). Status != 200/404 e I/O propagam como erro.
func (c *Client) getCAA(ctx context.Context, path string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.caaBaseURL+path, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("musicbrainz: status inesperado %d em %s (cover art archive)", resp.StatusCode, path)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("musicbrainz: ler %s (cover art archive): %w", path, err)
	}
	return body, true, nil
}

// respBuscaArtista é a resposta de GET /artist?query=...&fmt=json (só os campos usados).
type respBuscaArtista struct {
	Artists []struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Disambiguation string `json:"disambiguation"`
	} `json:"artists"`
}

// respArtistaArvore é a resposta de GET /artist/{id}?inc=release-groups+annotation&fmt=json.
type respArtistaArvore struct {
	Name          string `json:"name"`
	Annotation    string `json:"annotation"`
	ReleaseGroups []struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		FirstReleaseDate string `json:"first-release-date"`
	} `json:"release-groups"`
}

// generoMB é um gênero com sua contagem (voto), como o MusicBrainz devolve em `inc=genres`.
type generoMB struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// respReleaseResumo é uma release resumida, como listada dentro de um release-group
// (inc=releases) — só o suficiente para escolherRelease.
type respReleaseResumo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// respReleaseGroup é a resposta de GET /release-group/{id}?inc=releases+genres&fmt=json.
type respReleaseGroup struct {
	Genres   []generoMB          `json:"genres"`
	Releases []respReleaseResumo `json:"releases"`
}

// creditoMB é uma entrada de artist-credit (a MusicBrainz modela como lista, para juntar
// vários artistas com joinphrase; usamos só o primeiro — o artista principal creditado).
type creditoMB struct {
	Artist struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

// respRelease é a resposta de GET /release/{id}?inc=recordings+artist-credits+isrcs&fmt=json.
type respRelease struct {
	Media []struct {
		Position int `json:"position"`
		Tracks   []struct {
			Position     int         `json:"position"`
			Title        string      `json:"title"`
			ArtistCredit []creditoMB `json:"artist-credit"`
			Recording    struct {
				ID           string      `json:"id"`
				Title        string      `json:"title"`
				ISRCs        []string    `json:"isrcs"`
				ArtistCredit []creditoMB `json:"artist-credit"`
			} `json:"recording"`
		} `json:"tracks"`
	} `json:"media"`
}

// respCoverArt é a resposta de GET /release-group/{id} (ou /release/{id}) no Cover Art
// Archive: só os campos usados (front/back/image) — thumbnails/approved/comment/edit/id
// ignorados (a URL de tamanho completo em `image` é o que os demais ArteProvider do repo
// também expõem).
type respCoverArt struct {
	Images []struct {
		Front bool   `json:"front"`
		Back  bool   `json:"back"`
		Image string `json:"image"`
	} `json:"images"`
}
