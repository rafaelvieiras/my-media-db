// Package fanarttv implementa um provider DEDICADO A ARTE (briefing 0048): a fanart.tv
// é uma fonte exclusiva de arte de alta qualidade (logos, clearart, disc, character
// art) que o TMDB/TVDB não cobrem bem. Ela NÃO é fonte de busca: o Client implementa o
// core provider.Provider apenas com Buscar/BuscarDetalhe/MapearDetalhe NO-OP (para
// entrar no Registry e ser descoberto por type-assert), e a capacidade real é
// provider.ArteProvider (BuscarArtes). A credencial (chave pessoal) é lida no boot
// (main) e injetada aqui — NUNCA hardcoded, nunca logada.
package fanarttv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// baseURLPadrao é a base estável da API v3 da fanart.tv.
	baseURLPadrao = "https://webservice.fanart.tv/v3"
	// timeoutPadrao limita cada requisição; estouro vira erro (o enrich degrada).
	timeoutPadrao = 8 * time.Second
	// nomeFonte é o Nome() da fonte (também o source do inventário em media_external).
	nomeFonte = "fanarttv"
)

// kindsFilme mapeia cada tipo de arte de FILME da fanart.tv para o nosso kind (tabela do
// briefing 0048 §2). moviedisc carrega disc_type/disc no próprio item.
var kindsFilme = map[string]string{
	"movieposter":     "poster",
	"moviebackground": "fanart",
	"hdmovielogo":     "logo",
	"movielogo":       "logo",
	"hdmovieclearart": "clearart",
	"movieart":        "clearart",
	"moviedisc":       "disc",
	"moviebanner":     "banner",
	"moviethumb":      "thumb",
}

// kindsTV mapeia cada tipo de arte de SÉRIE da fanart.tv para o nosso kind. As artes de
// temporada (season*) carregam o campo `season` no item — o enrich as roteia à temporada.
var kindsTV = map[string]string{
	"tvposter":       "poster",
	"showbackground": "fanart",
	"hdtvlogo":       "logo",
	"clearlogo":      "logo",
	"hdclearart":     "clearart",
	"clearart":       "clearart",
	"tvbanner":       "banner",
	"tvthumb":        "thumb",
	"characterart":   "characterart",
	"seasonposter":   "poster",
	"seasonthumb":    "thumb",
	"seasonbanner":   "banner",
}

// kindsArtista mapeia cada tipo de arte de ARTISTA de música da fanart.tv para o nosso
// kind (briefing 0116, endpoint GET /music/{mbid} — mesmo shape achatado tipo->array que
// filme/série, então reusa mapearArtes sem struct nova). A fanart.tv também aninha
// capa/cdart de álbum dentro da MESMA resposta sob a chave "albums" (por release-group),
// mas essa chave não está na tabela abaixo, então mapearArtes a ignora — capa de álbum já
// é coberta pelo Cover Art Archive (MusicaBrainz), sem duplicar aqui.
var kindsArtista = map[string]string{
	"artistthumb":      "thumb",
	"artistbackground": "fanart",
	"hdmusiclogo":      "logo",
	"musiclogo":        "logo",
	"musicbanner":      "banner",
}

// Credencial é a credencial da fanart.tv: a chave PESSOAL (api_key), que autentica
// sozinha (validado, HTTP 200) e bypassa o limite da chave de projeto. Quem lê o env é
// o main; aqui já chega resolvida.
type Credencial struct {
	// APIKey é a chave pessoal (query api_key).
	APIKey string
}

// Configurada diz se há chave preenchida. Sem ela, o main nem cria o provider (arte da
// fanart.tv desligada — não é erro; regressão zero).
func (c Credencial) Configurada() bool { return c.APIKey != "" }

// Client é o provider fanart.tv. httpClient e baseURL são injetáveis para teste (apontar
// a um httptest.Server sem rede real).
type Client struct {
	cred       Credencial
	httpClient *http.Client
	baseURL    string
}

// New cria o client. httpClient nil usa um default com timeout; baseURL vazio usa a base
// pública da fanart.tv.
func New(cred Credencial, httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeoutPadrao}
	}
	if baseURL == "" {
		baseURL = baseURLPadrao
	}
	return &Client{cred: cred, httpClient: httpClient, baseURL: baseURL}
}

// Nome identifica a fonte.
func (c *Client) Nome() string { return nomeFonte }

// Buscar é NO-OP: a fanart.tv não é fonte de busca (nunca polui os resultados). Devolve
// vazio para satisfazer o core provider.Provider.
func (c *Client) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

// BuscarDetalhe é NO-OP: a fanart.tv não modela o detalhe de um item (só arte). Devolve
// um payload vazio para satisfazer o core.
func (c *Client) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// MapearDetalhe é NO-OP: devolve um mapa vazio — a camada de inventário da fanart.tv em
// media_external NÃO deve poluir a composição de camadas do resolver (só o /artwork a lê).
func (c *Client) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// BuscarArtes lista as artes do item na fanart.tv (capacidade provider.ArteProvider).
// Filme: /movies/{id} (id TMDB preferido, senão IMDB). Série: /tv/{id} (id TheTVDB).
// Artista de música: /music/{id} (id MBID, briefing 0116) — foto/fundo/logo do artista;
// capa de álbum fica a cargo do Cover Art Archive (MusicBrainz), não desta fonte. Sem id
// utilizável ou sem match (404) devolve (nil, nil) — graça. Erro real propaga. `langs`
// não é usado na chamada (a fanart.tv devolve TODOS os idiomas de uma vez); o lang de cada
// arte é preservado no inventário.
func (c *Client) BuscarArtes(ctx context.Context, ids provider.ExternalIDs, tipo string, langs []string) ([]provider.Arte, error) {
	_ = langs
	path, tabela, ok := c.rotaETabela(ids, tipo)
	if !ok {
		return nil, nil // sem id utilizável para esta fonte
	}
	body, ok, err := c.getArtes(ctx, path)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil // 404: item sem arte na fanart.tv (graça)
	}
	return mapearArtes(body, tabela)
}

// rotaETabela escolhe o path e a tabela de kinds pelo tipo e pelos ids disponíveis. Filme
// prefere o id TMDB (senão o IMDB); série usa o id TheTVDB. ok=false quando falta o id.
func (c *Client) rotaETabela(ids provider.ExternalIDs, tipo string) (string, map[string]string, bool) {
	switch tipo {
	case "movie":
		id := ids.Tmdb
		if id == "" {
			id = ids.Imdb
		}
		if id == "" {
			return "", nil, false
		}
		return "/movies/" + url.PathEscape(id), kindsFilme, true
	case "tv":
		if ids.Tvdb == "" {
			return "", nil, false
		}
		return "/tv/" + url.PathEscape(ids.Tvdb), kindsTV, true
	case "artista":
		if ids.Mbid == "" {
			return "", nil, false
		}
		return "/music/" + url.PathEscape(ids.Mbid), kindsArtista, true
	default:
		return "", nil, false
	}
}

// getArtes faz o GET autenticado (api_key) a `path` e devolve o corpo. ok=false num 404
// (item sem arte — não é erro). Status != 200/404 e I/O propagam como erro.
func (c *Client) getArtes(ctx context.Context, path string) ([]byte, bool, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, false, err
	}
	q := u.Query()
	q.Set("api_key", c.cred.APIKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("fanarttv: status inesperado %d em %s", resp.StatusCode, path)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("fanarttv: ler %s: %w", path, err)
	}
	return body, true, nil
}

// imagemFanart é uma imagem crua da fanart.tv. Os números vêm como STRING no JSON
// (likes/disc/season) — convertidos com atoiSeguro. `season` pode ser "all"/"0"/"1"…
type imagemFanart struct {
	URL      string `json:"url"`
	Lang     string `json:"lang"`
	Likes    string `json:"likes"`
	Disc     string `json:"disc"`
	DiscType string `json:"disc_type"`
	Season   string `json:"season"`
}

// mapearArtes decodifica a resposta (objeto com uma array por tipo) e mapeia cada tipo
// CONHECIDO da tabela para []provider.Arte. Tipos fora da tabela (name/tmdb_id/…) e
// imagens sem URL são ignorados. Um tipo com array malformada é pulado (degrada aquele
// tipo, não a busca inteira).
func mapearArtes(body []byte, tabela map[string]string) ([]provider.Arte, error) {
	var bruto map[string]json.RawMessage
	if err := json.Unmarshal(body, &bruto); err != nil {
		return nil, fmt.Errorf("fanarttv: resposta invalida: %w", err)
	}
	var out []provider.Arte
	for chave, kind := range tabela {
		raw, ok := bruto[chave]
		if !ok || len(raw) == 0 {
			continue
		}
		var imgs []imagemFanart
		if err := json.Unmarshal(raw, &imgs); err != nil {
			continue // array malformada deste tipo: degrada o tipo
		}
		for _, img := range imgs {
			if img.URL == "" {
				continue
			}
			out = append(out, arteDe(kind, img))
		}
	}
	return out, nil
}

// arteDe converte uma imagem crua no nosso Arte canônico, normalizando o kind e
// convertendo os números-string (likes/disc). DiscType/Season vêm intactos (vazios para
// tipos que não os têm).
func arteDe(kind string, img imagemFanart) provider.Arte {
	return provider.Arte{
		Kind:     kind,
		URL:      img.URL,
		Lang:     img.Lang,
		Likes:    atoiSeguro(img.Likes),
		DiscType: img.DiscType,
		Disc:     atoiSeguro(img.Disc),
		Season:   img.Season,
	}
}

// atoiSeguro converte uma string numérica em int, devolvendo 0 quando vazia/inválida
// (a fanart.tv serializa likes/disc como string).
func atoiSeguro(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
