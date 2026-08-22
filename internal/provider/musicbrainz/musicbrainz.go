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
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/textsim"
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

// BuscarDetalhe puxa o detalhe CRU (capacidade core provider.Provider, usada pelo
// "Baixar Metadados" genérico via internal/enrich e por importarItemBase). Despacha por
// `tipo` para o endpoint correto — /artist para artista, /release-group para album,
// /recording para faixa (os MESMOS que montarAlbumOuErro/BuscarAlbumDaFaixa já usam) —
// achado real na validação em produção de 20/08/2026: antes disso, SEMPRE chamava
// /artist/{sourceID} não importa o tipo, e sourceID pra album/faixa é o MBID de
// release-group/recording (não de artista), então batia em 404/502 nesses dois tipos.
// `langs` não tem efeito: o MusicBrainz não é multi-idioma. Para album/faixa, o corpo
// passa por remapParaNome antes de devolver: release-group/recording expõem o título na
// chave "title" (não "name"), então sem esse remap MapearDetalhe (abaixo) não encontraria
// nada — normalizar aqui evita mudar a assinatura de MapearDetalhe (que não recebe
// `tipo`), uma mudança que seria BREAKING em todo Provider registrado (tmdb/tvdb/
// fanarttv/themerrdb), para resolver um problema que é só do MusicBrainz.
func (c *Client) BuscarDetalhe(ctx context.Context, sourceID, tipo string, langs []string) (json.RawMessage, error) {
	_ = langs
	switch tipo {
	case "album":
		body, err := c.getMB(ctx, "/release-group/"+url.PathEscape(sourceID), url.Values{})
		if err != nil {
			return nil, err
		}
		return remapParaNome(body)
	case "faixa":
		body, err := c.getMB(ctx, "/recording/"+url.PathEscape(sourceID), url.Values{})
		if err != nil {
			return nil, err
		}
		return remapParaNome(body)
	default:
		body, err := c.getMB(ctx, "/artist/"+url.PathEscape(sourceID), url.Values{})
		if err != nil {
			return nil, err
		}
		return json.RawMessage(body), nil
	}
}

// remapParaNome reescreve o `"title"` de um release-group/recording cru para `"name"` —
// o campo que MapearDetalhe sabe ler (mesma chave que o shape de artista já usa). Ver
// comentário de BuscarDetalhe para o porquê deste desvio em vez de mudar MapearDetalhe.
func remapParaNome(body []byte) (json.RawMessage, error) {
	var t respTitulo
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, fmt.Errorf("musicbrainz: detalhe invalido: %w", err)
	}
	out, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: t.Title})
	if err != nil {
		return nil, fmt.Errorf("musicbrainz: remapear detalhe: %w", err)
	}
	return json.RawMessage(out), nil
}

// respTitulo é o shape mínimo comum a /release-group/{id} e /recording/{id}: ambos
// expõem o título do recurso na chave "title" (atributo core, sempre presente).
type respTitulo struct {
	Title string `json:"title"`
}

// MapearDetalhe extrai só o nome (artista) ou o título já remapeado (album/faixa, ver
// BuscarDetalhe) para o shape canônico — o suficiente para o import/enrich genérico
// (0015) dar título à entity; o resto da árvore (álbuns/faixas) entra por importarArtista
// (internal/enrich/musica.go, fora deste pacote).
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
		album := c.montarAlbum(ctx, rg.ID, rg.Title, rg.FirstReleaseDate, provider.AlvoFaixaAlbum{})
		arvore.Albuns = append(arvore.Albuns, album)
	}
	return arvore, nil
}

// montarAlbum busca o release-group (releases+genres, 1 chamada), escolhe a(s)
// release(s) a representar o álbum conforme `alvoFaixa` (ver buscarFaixasComAlvo), e
// busca as faixas. NUNCA devolve erro: release-group sem nenhuma release, 404
// pontual, ou esgotamento do retry de getMB (instabilidade do servidor público) todos
// degradam pro mesmo shell -- álbum com nome/MBID/ano mas sem faixas -- em vez de
// derrubar a árvore inteira (ver BuscarArtistaArvore). Faixas ausentes por causa disso
// são corrigíveis num reimport posterior (find-or-create por MBID, idempotente).
// `title`/`firstReleaseDate` são OPCIONAIS (briefing 0115): quando vazios (chamador
// não os tem de antemão, ver BuscarAlbumDaFaixa), usa o título/data que o PRÓPRIO
// release-group devolve (rg.Title/rg.FirstReleaseDate) -- o chamador que já os tem
// (BuscarArtistaArvore, a partir do browse de release-groups do artista) evita essa
// dependência do 2º campo. `alvoFaixa` (briefing 0121) é zero value nos caminhos que
// não precisam dela (BuscarArtistaArvore/BuscarAlbumPorMbid).
func (c *Client) montarAlbum(ctx context.Context, rgID, title, firstReleaseDate string, alvoFaixa provider.AlvoFaixaAlbum) provider.AlbumExterno {
	album, err := c.montarAlbumOuErro(ctx, rgID, title, firstReleaseDate, alvoFaixa)
	if err != nil {
		return provider.AlbumExterno{Name: title, MBID: rgID, Year: anoDe(firstReleaseDate)}
	}
	return album
}

// montarAlbumOuErro é o corpo de montarAlbum que PROPAGA o erro da 1ª chamada
// (busca do release-group) em vez de degradar para o shell -- extraído para
// BuscarAlbumPorMbid (briefing 0115 Parte 2), o único caminho de
// MusicaAlbumProvider que não tem uma resolução prévia confirmando que o
// release-group existe, então precisa saber se a falha foi "não encontrado"
// (→ ErrNaoEncontrado, 404) ou outra coisa. A falha da chamada de FAIXAS
// (buscarFaixasComAlvo) continua degradando em silêncio (mesma semântica de sempre):
// o release-group já foi confirmado nesta mesma chamada, então uma
// instabilidade pontual na(s) ida(s) seguinte(s) vale mais como álbum parcial (sem
// faixas, corrigível num reimport) do que como erro pro chamador.
func (c *Client) montarAlbumOuErro(ctx context.Context, rgID, title, firstReleaseDate string, alvoFaixa provider.AlvoFaixaAlbum) (provider.AlbumExterno, error) {
	body, err := c.getMB(ctx, "/release-group/"+url.PathEscape(rgID), url.Values{"inc": {"releases+genres"}})
	if err != nil {
		return provider.AlbumExterno{}, err
	}
	var rg respReleaseGroup
	if err := json.Unmarshal(body, &rg); err != nil {
		return provider.AlbumExterno{}, fmt.Errorf("musicbrainz: release-group invalido: %w", err)
	}

	album := provider.AlbumExterno{Name: title, MBID: rgID, Year: anoDe(firstReleaseDate)}
	if album.Name == "" {
		album.Name = rg.Title
	}
	if album.Year == "" {
		album.Year = anoDe(rg.FirstReleaseDate)
	}
	album.Genre = generoPrincipal(rg.Genres)

	faixas, err := c.buscarFaixasComAlvo(ctx, rg.Releases, alvoFaixa)
	if err != nil {
		return album, nil // nenhuma release respondeu (404/retry esgotado): degrada sem faixas
	}
	album.Faixas = faixas
	return album, nil
}

// BuscarAlbumPorMbid é a capacidade provider.MusicaAlbumProvider (ADR-0029 item 6,
// briefing 0115 Parte 2), caminho DIRETO: o MBID já é o do release-group -- usado
// pela tela externa de álbum (GET /external/musicbrainz/album/{mbid}) e pelo import
// de faixa avulsa (ambos partem do external_ids["mbid"] já gravado na entity álbum
// interna). Ao contrário de montarAlbum (usado só DEPOIS de resolvido por outro
// caminho), aqui a existência do release-group é validada nesta própria chamada via
// montarAlbumOuErro -- 404 vira provider.ErrNaoEncontrado (errors.Is).
func (c *Client) BuscarAlbumPorMbid(ctx context.Context, albumMBID string, langs []string) (provider.AlbumExterno, error) {
	_ = langs
	if albumMBID == "" {
		return provider.AlbumExterno{}, fmt.Errorf("musicbrainz: mbid de album vazio")
	}
	return c.montarAlbumOuErro(ctx, albumMBID, "", "", provider.AlvoFaixaAlbum{})
}

// BuscarAlbumDaFaixa é a capacidade provider.MusicaAlbumProvider (ADR-0029 item 6,
// briefing 0115), caminho PRECISO: resolve o release-group que contém o recording
// `recordingMBID` (1 chamada, /recording/{id}?inc=releases+release-groups -- cada
// release do recording já vem com seu release-group aninhado, sem chamada extra) e
// monta ESSE único álbum via montarAlbum (mais 1+N chamadas) -- nunca a discografia
// inteira. Entre as releases do recording, prefere a de status "Official" (mesmo
// critério de escolherRelease); sem nenhuma, a primeira com release-group presente.
// Passa `provider.AlvoFaixaAlbum{RecordingMBID: recordingMBID}` a montarAlbum
// (briefing 0121): a release "Official" do release-group pode NÃO ser a mesma que
// contém este recording especificamente (escolherReleaseGroupDoRecording só resolve
// o release-group, não garante que montarAlbum escolha a release certa dentro dele)
// -- sem o alvo, o caminho PRECISO tinha o mesmo gap estrutural do textual.
func (c *Client) BuscarAlbumDaFaixa(ctx context.Context, recordingMBID string, langs []string) (provider.AlbumExterno, bool, error) {
	_ = langs
	if recordingMBID == "" {
		return provider.AlbumExterno{}, false, fmt.Errorf("musicbrainz: mbid de faixa vazio")
	}
	body, err := c.getMB(ctx, "/recording/"+url.PathEscape(recordingMBID), url.Values{"inc": {"releases+release-groups"}})
	if err != nil {
		return provider.AlbumExterno{}, false, err
	}
	var rec respRecordingReleases
	if err := json.Unmarshal(body, &rec); err != nil {
		return provider.AlbumExterno{}, false, fmt.Errorf("musicbrainz: recording invalido: %w", err)
	}
	rgID, ok := escolherReleaseGroupDoRecording(rec.Releases)
	if !ok {
		return provider.AlbumExterno{}, false, nil
	}
	return c.montarAlbum(ctx, rgID, "", "", provider.AlvoFaixaAlbum{RecordingMBID: recordingMBID}), true, nil
}

// escolherReleaseGroupDoRecording escolhe, entre as releases do recording, o
// release-group da PRIMEIRA de status "Official" (mesmo critério de
// escolherRelease); sem nenhuma oficial, o release-group da primeira que tiver um
// (algumas releases podem vir sem release-group aninhado -- dado incompleto na
// fonte). ok=false sem NENHUMA release com release-group utilizável.
func escolherReleaseGroupDoRecording(releases []respReleaseComGrupo) (string, bool) {
	for _, r := range releases {
		if r.Status == "Official" && r.ReleaseGroup.ID != "" {
			return r.ReleaseGroup.ID, true
		}
	}
	for _, r := range releases {
		if r.ReleaseGroup.ID != "" {
			return r.ReleaseGroup.ID, true
		}
	}
	return "", false
}

// BuscarAlbumPorNome é a capacidade provider.MusicaAlbumProvider (ADR-0029 item 6,
// briefing 0115), caminho TEXTUAL: lista os release-groups do artista `artistaMBID`
// (1 chamada barata -- /artist/{id}?inc=release-groups, só ids/títulos, o MESMO
// 1º request que BuscarArtistaArvore já faz), escolhe o de título mais próximo de
// `nomeAlbum` (Jaro-Winkler via internal/textsim, limiar matching.LimiarAutoMatch --
// mesmo motor/número que o scanner usa para o match textual de artista, ver
// internal/scanner/musica.go melhorCandidatoTexto) e monta SÓ esse álbum via
// montarAlbum -- nunca os demais release-groups listados. `alvoFaixa` (briefing
// 0121) é só repassado a montarAlbum, sem lógica extra própria aqui -- quem decide
// QUAIS releases tentar dentro do release-group escolhido é buscarFaixasComAlvo.
func (c *Client) BuscarAlbumPorNome(ctx context.Context, artistaMBID, nomeAlbum string, alvoFaixa provider.AlvoFaixaAlbum, langs []string) (provider.AlbumExterno, bool, error) {
	_ = langs
	if artistaMBID == "" || nomeAlbum == "" {
		return provider.AlbumExterno{}, false, fmt.Errorf("musicbrainz: artista ou nome de album vazio")
	}
	body, err := c.getMB(ctx, "/artist/"+url.PathEscape(artistaMBID), url.Values{"inc": {"release-groups"}})
	if err != nil {
		return provider.AlbumExterno{}, false, err
	}
	var art respArtistaArvore
	if err := json.Unmarshal(body, &art); err != nil {
		return provider.AlbumExterno{}, false, fmt.Errorf("musicbrainz: artista invalido: %w", err)
	}
	rg, ok := melhorReleaseGroupPorTitulo(nomeAlbum, art.ReleaseGroups)
	if !ok {
		return provider.AlbumExterno{}, false, nil
	}
	return c.montarAlbum(ctx, rg.ID, rg.Title, rg.FirstReleaseDate, alvoFaixa), true, nil
}

// melhorReleaseGroupPorTitulo escolhe, entre `rgs`, o de título mais próximo de
// `alvo` -- mesmo critério de melhorCandidatoTexto do scanner (Jaro-Winkler,
// matching.LimiarAutoMatch, SEM empate no topo): um nome ambíguo ou fraco demais
// nunca aplica um match "parecido", cai para (zero,false).
func melhorReleaseGroupPorTitulo(alvo string, rgs []respReleaseGroupBreve) (respReleaseGroupBreve, bool) {
	melhorScore := -1.0
	var melhor respReleaseGroupBreve
	empate := false
	for _, rg := range rgs {
		if rg.ID == "" {
			continue
		}
		score := textsim.Similaridade(alvo, rg.Title) * 100
		switch {
		case score > melhorScore:
			melhorScore, melhor, empate = score, rg, false
		case score == melhorScore:
			empate = true
		}
	}
	if empate || melhorScore < matching.LimiarAutoMatch {
		return respReleaseGroupBreve{}, false
	}
	return melhor, true
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

// maxReleasesTentadosPorAlvo é o TETO de releases tentadas por buscarFaixasComAlvo
// quando há um alvoFaixa a satisfazer (briefing 0121) -- um release-group real pode
// ter dezenas de releases (reedições, países, remasters); sem teto, uma faixa
// genuinamente ausente do MusicBrainz inteiro geraria dezenas de chamadas HTTP por
// tentativa de import/reconciliação, a <= 1 req/s (rate limit já apertado). Subido de
// 5 para 15 na rodada 2 (briefing 0121 §8): validação em produção achou um outlier
// real de 13 releases (Bomfunk MC's "Freestyler") cuja release certa cai em ~10º
// lugar na ordem Official-primeiro -- 15 cobre esse caso com folga, custo continua
// desprezível (só entra em jogo quando a 1ª tentativa falha).
const maxReleasesTentadosPorAlvo = 15

// buscarFaixasComAlvo busca as faixas do release-group representado por `releases`,
// tentando 1 ou mais releases conforme `alvoFaixa` (briefing 0121, achado real:
// Bomfunk MC's "Freestyler" não está na release "Official" escolhida por
// escolherRelease, mas existe noutra release do MESMO release-group). Com
// `alvoFaixa` no ZERO VALUE, preserva o comportamento HISTÓRICO exato de antes deste
// briefing: escolhe 1 única release (escolherRelease) e faz 1 única chamada a
// buscarFaixas -- ZERO regressão de custo/nº de chamadas HTTP para quem nunca
// precisou de uma faixa específica (BuscarArtistaArvore/BuscarAlbumPorMbid). Com
// `alvoFaixa` preenchido: tenta as releases em ordem de prioridade
// (ordenarReleasesPorPrioridade) até achar uma cujas faixas satisfaçam o alvo
// (releaseContemFaixaAlvo) -- para no primeiro sucesso -- ou esgotar
// maxReleasesTentadosPorAlvo tentativas, o que vier primeiro. Sem nenhuma bater
// dentro do teto, devolve as faixas da ÚLTIMA release que respondeu com sucesso
// (mesma degradação silenciosa de sempre -- quem decide se a faixa certa foi
// encontrada continua sendo escolherFaixaAlvo em internal/enrich/musica.go, esta
// função só melhora as CANDIDATAS oferecidas). O erro só é propagado quando TODAS as
// tentativas (das DUAS passadas, ver tentarReleases) falharam de rede (nenhuma faixa
// obtida em nenhuma tentativa) -- uma falha pontual no meio do laço não aborta a
// tentativa da próxima release.
//
// Achado real em produção (briefing 0123): uma release ESPECÍFICA que esgota o retry
// de rede de getMB (3 tentativas, 503 "currently busy" transitório do servidor
// público) era descartada PARA SEMPRE nesta função -- mesmo que fosse justo a release
// com a faixa-alvo. Por isso, quando a 1ª passada não bate o alvo e ao menos uma
// release falhou de REDE (não por não bater o match), há uma 2ª passada só sobre
// essas -- cada uma ganha um ciclo NOVO e completo de tentativas (503 é
// instabilidade transitória: a mesma release pode responder segundos depois).
func (c *Client) buscarFaixasComAlvo(ctx context.Context, releases []respReleaseResumo, alvoFaixa provider.AlvoFaixaAlbum) ([]provider.FaixaExterna, error) {
	if alvoFaixa == (provider.AlvoFaixaAlbum{}) {
		releaseID, ok := escolherRelease(releases)
		if !ok {
			return nil, nil // release-group sem nenhuma release: sem faixas, sem erro
		}
		return c.buscarFaixas(ctx, releaseID)
	}

	ids := ordenarReleasesPorPrioridade(releases)
	if len(ids) > maxReleasesTentadosPorAlvo {
		ids = ids[:maxReleasesTentadosPorAlvo]
	}

	match, ultimaObtida, obtevesAlgumaVez, falhadas, ultimoErr := c.tentarReleases(ctx, ids, alvoFaixa)
	if match {
		return ultimaObtida, nil
	}
	if len(falhadas) > 0 {
		// 2a passada: só sobre as releases que falharam de REDE na 1a -- economiza a
		// chamada extra quando tudo já respondeu e só não bateu por matching mesmo.
		match2, ultimaObtida2, obtevesAlgumaVez2, _, ultimoErr2 := c.tentarReleases(ctx, falhadas, alvoFaixa)
		if match2 {
			return ultimaObtida2, nil
		}
		if obtevesAlgumaVez2 {
			ultimaObtida = ultimaObtida2 // a mais recente sucedida, entre as duas passadas
			obtevesAlgumaVez = true
		}
		ultimoErr = ultimoErr2
	}
	if !obtevesAlgumaVez {
		return nil, ultimoErr // TODAS as tentativas (das duas passadas) falharam de rede
	}
	return ultimaObtida, nil // nenhuma bateu dentro do teto: degrada com a ULTIMA obtida
}

// tentarReleases tenta, em ordem, buscar as faixas de cada release em `ids`, parando no
// primeiro sucesso que satisfaça `alvoFaixa` (releaseContemFaixaAlvo) -- corpo do loop
// extraído de buscarFaixasComAlvo (briefing 0123) para ser reusado por uma 2ª passada
// (ver ali). Cada falha de `buscarFaixas` (erro de REDE, não "respondeu mas não bateu o
// match") é logada com o releaseID e o erro, e acumulada em `falhadas` -- antes deste
// briefing essas releases eram descartadas para sempre e em silêncio. Devolve: `match`
// (achou uma release que satisfaz o alvo -- `ultimaObtida` já são as faixas dela),
// `ultimaObtida` (as faixas da ÚLTIMA release que respondeu com sucesso, para o
// fallback de degradação de buscarFaixasComAlvo quando nada bate), `obtevesAlgumaVez`
// (true se QUALQUER release desta passada respondeu com sucesso), `falhadas`
// (releaseIDs que falharam de rede nesta passada) e `ultimoErr` (o erro da última
// falha de rede desta passada, só relevante quando `obtevesAlgumaVez` for false).
func (c *Client) tentarReleases(ctx context.Context, ids []string, alvoFaixa provider.AlvoFaixaAlbum) (match bool, ultimaObtida []provider.FaixaExterna, obtevesAlgumaVez bool, falhadas []string, ultimoErr error) {
	for _, releaseID := range ids {
		fx, err := c.buscarFaixas(ctx, releaseID)
		if err != nil {
			log.Printf("musicbrainz: busca de faixas da release %s falhou (retentando outra release do grupo): %v", releaseID, err)
			falhadas = append(falhadas, releaseID)
			ultimoErr = err
			continue
		}
		obtevesAlgumaVez = true
		ultimaObtida = fx
		if releaseContemFaixaAlvo(fx, alvoFaixa) {
			return true, fx, true, falhadas, nil
		}
	}
	return false, ultimaObtida, obtevesAlgumaVez, falhadas, ultimoErr
}

// ordenarReleasesPorPrioridade ordena os IDs de `releases` por prioridade -- MESMO
// critério de escolherRelease (status=="Official" primeiro, na ordem original entre
// si; depois as demais, também na ordem original entre si), mas devolvendo a LISTA
// INTEIRA em vez de parar na primeira -- usada por buscarFaixasComAlvo para tentar
// múltiplas releases do mesmo release-group quando há um alvoFaixa a satisfazer.
func ordenarReleasesPorPrioridade(releases []respReleaseResumo) []string {
	ids := make([]string, 0, len(releases))
	for _, r := range releases {
		if r.Status == "Official" {
			ids = append(ids, r.ID)
		}
	}
	for _, r := range releases {
		if r.Status != "Official" {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// releaseContemFaixaAlvo diz se `faixas` (as de UMA release já buscada) contêm a
// faixa que `alvo` descreve. AUTOCONTIDO neste pacote (briefing 0121) -- NÃO reusa
// escolherFaixaAlvo/melhorFaixaPorTitulo de internal/enrich por import cycle
// (provider não pode depender de enrich, que já depende de provider); é uma cópia
// funcional MÍNIMA, mesma fórmula/limiar, não uma abstração compartilhada nova.
// Mesma prioridade de AlvoFaixaAlbum: MBID exato > título por similaridade
// (textsim.Similaridade, melhor score entre as faixas, limiar
// matching.LimiarAutoMatch) > track_number exato. Sem nenhum critério aplicável
// (todos os campos de `alvo` vazios/zero), devolve false -- nunca aplica um
// "parecido" à toa.
func releaseContemFaixaAlvo(faixas []provider.FaixaExterna, alvo provider.AlvoFaixaAlbum) bool {
	if alvo.RecordingMBID != "" {
		return releaseContemRecordingMBID(faixas, alvo.RecordingMBID)
	}
	if alvo.FaixaTitulo != "" {
		return releaseContemFaixaPorTitulo(faixas, alvo)
	}
	if alvo.FaixaNumero > 0 {
		return releaseContemFaixaPorNumero(faixas, alvo.FaixaNumero)
	}
	return false
}

// releaseContemRecordingMBID é o critério de maior prioridade de
// releaseContemFaixaAlvo: MBID exato de recording.
func releaseContemRecordingMBID(faixas []provider.FaixaExterna, recordingMBID string) bool {
	for _, fx := range faixas {
		if fx.MBID == recordingMBID {
			return true
		}
	}
	return false
}

// releaseContemFaixaPorTitulo escolhe a faixa de MELHOR score de similaridade de
// título contra alvo.FaixaTitulo; aceita quando o score bate matching.LimiarAutoMatch
// E, adicionalmente, quando o gate de duração (briefing 0121 §8) confirma (ou é
// pulado por duração desconhecida de um dos lados, via duracoesCompativeis).
func releaseContemFaixaPorTitulo(faixas []provider.FaixaExterna, alvo provider.AlvoFaixaAlbum) bool {
	melhorScore := -1.0
	var melhorFx provider.FaixaExterna
	for _, fx := range faixas {
		score := textsim.Similaridade(alvo.FaixaTitulo, fx.Title) * 100
		if score > melhorScore {
			melhorScore = score
			melhorFx = fx
		}
	}
	if melhorScore < matching.LimiarAutoMatch {
		return false
	}
	return duracoesCompativeis(alvo.DuracaoMs, melhorFx.DuracaoMs)
}

// releaseContemFaixaPorNumero é o critério de menor prioridade de
// releaseContemFaixaAlvo: track_number exato.
func releaseContemFaixaPorNumero(faixas []provider.FaixaExterna, faixaNumero int) bool {
	for _, fx := range faixas {
		if fx.TrackNumber == faixaNumero {
			return true
		}
	}
	return false
}

// duracoesCompativeis aplica o gate de duração da rodada 2 (briefing 0121 §8):
// CONFIRMAÇÃO adicional ao match de título, nunca torna o match mais permissivo.
// Só aplica quando AMBOS os lados informam duração conhecida (> 0 nos dois) --
// qualquer um dos dois em 0 (desconhecida) pula o gate e devolve true, preservando o
// comportamento de antes desta mudança (sem regressão para arquivos/dados sem
// duração confiável).
func duracoesCompativeis(duracaoAlvoMs, duracaoCandidatoMs int) bool {
	if duracaoAlvoMs <= 0 || duracaoCandidatoMs <= 0 {
		return true
	}
	diff := duracaoAlvoMs - duracaoCandidatoMs
	if diff < 0 {
		diff = -diff
	}
	return diff <= int(matching.ToleranciaDuracaoSegundos*1000)
}

// duracaoMsDaFaixa escolhe a duração (ms) a usar para uma faixa (briefing 0121 §8):
// prioriza o `length` do TRACK dentro da release; sem esse (<= 0), cai para o
// `length` do RECORDING aninhado; sem nenhum dos dois (ou negativo), 0 (desconhecida).
func duracaoMsDaFaixa(trackLength, recordingLength int) int {
	duracaoMs := trackLength
	if duracaoMs <= 0 {
		duracaoMs = recordingLength
	}
	if duracaoMs < 0 {
		duracaoMs = 0
	}
	return duracaoMs
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
			duracaoMs := duracaoMsDaFaixa(tr.Length, tr.Recording.Length)
			faixas = append(faixas, provider.FaixaExterna{
				Title:              titulo,
				MBID:               tr.Recording.ID,
				TrackNumber:        tr.Position,
				DiscNumber:         media.Position,
				DiscTotal:          discTotal,
				ISRC:               isrc,
				ArtistaCreditoNome: creditoNome,
				ArtistaCreditoMBID: creditoMBID,
				DuracaoMs:          duracaoMs,
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
// distingue os dois casos; não é erro). Status != 200/404 tenta de novo (retry, ver
// tentarGetCAA) -- achado real na validação em produção: o redirect do CAA pra
// archive.org (dnNNNNNN.ca.archive.org, CDN distribuída) deu "context deadline exceeded"
// em boa parte das chamadas de uma importação de 25 álbuns, sem padrão de status HTTP (é
// timeout de rede, não um 5xx) -- diferente do 503 "busy" do MusicBrainz em getMB.
func (c *Client) getCAA(ctx context.Context, path string) ([]byte, bool, error) {
	var ultimoErr error
	for tentativa := 1; tentativa <= tentativasMB; tentativa++ {
		if tentativa > 1 {
			c.lim.dormirFn(backoffMB * time.Duration(tentativa-1))
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
		}
		body, ok, err := c.tentarGetCAA(ctx, path)
		if err == nil {
			return body, ok, nil
		}
		ultimoErr = err
	}
	return nil, false, fmt.Errorf("musicbrainz: %s: esgotou %d tentativas (cover art archive): %w", path, tentativasMB, ultimoErr)
}

// tentarGetCAA faz UMA tentativa de GET ao Cover Art Archive (sem retry).
func (c *Client) tentarGetCAA(ctx context.Context, path string) ([]byte, bool, error) {
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
		_, _ = io.Copy(io.Discard, resp.Body)
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

// respArtistaArvore é a resposta de GET /artist/{id}?inc=release-groups+annotation&fmt=json
// (BuscarArtistaArvore) OU GET /artist/{id}?inc=release-groups&fmt=json (BuscarAlbumPorNome
// -- annotation não é pedido nesse caminho, o campo fica zero-value, sem problema).
type respArtistaArvore struct {
	Name          string                  `json:"name"`
	Annotation    string                  `json:"annotation"`
	ReleaseGroups []respReleaseGroupBreve `json:"release-groups"`
}

// respReleaseGroupBreve é um release-group RESUMIDO, como listado dentro da árvore do
// artista (inc=release-groups) -- ids/títulos, sem faixas (BuscarArtistaArvore itera e
// monta cada um via montarAlbum; BuscarAlbumPorNome escolhe UM por título e só monta
// esse, ver melhorReleaseGroupPorTitulo).
type respReleaseGroupBreve struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	FirstReleaseDate string `json:"first-release-date"`
}

// respReleaseComGrupo é uma release do recording (inc=releases+release-groups), com o
// release-group ANINHADO -- usada só por BuscarAlbumDaFaixa/escolherReleaseGroupDoRecording
// para resolver o álbum de um recording sem uma 2ª ida por release.
type respReleaseComGrupo struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ReleaseGroup struct {
		ID string `json:"id"`
	} `json:"release-group"`
}

// respRecordingReleases é a resposta de GET /recording/{id}?inc=releases+release-groups&fmt=json
// (só o campo usado por BuscarAlbumDaFaixa).
type respRecordingReleases struct {
	Releases []respReleaseComGrupo `json:"releases"`
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
// Title/FirstReleaseDate são atributos CORE do release-group (a fonte sempre os devolve,
// mesmo sem pedir via inc) -- usados por montarAlbum como fallback quando o chamador não
// os tem de antemão (briefing 0115, BuscarAlbumDaFaixa não faz o browse do artista
// antes).
type respReleaseGroup struct {
	Title            string              `json:"title"`
	FirstReleaseDate string              `json:"first-release-date"`
	Genres           []generoMB          `json:"genres"`
	Releases         []respReleaseResumo `json:"releases"`
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
			Length       int         `json:"length"`
			ArtistCredit []creditoMB `json:"artist-credit"`
			Recording    struct {
				ID           string      `json:"id"`
				Title        string      `json:"title"`
				Length       int         `json:"length"`
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
