// Package provider define a abstração de FONTE EXTERNA de metadados (briefing
// 0014). Um Provider sabe buscar títulos numa fonte (TMDB, TVDB, MusicBrainz…) e
// devolver resultados no tipo canônico ResultadoExterno — sem NUNCA persistir
// nada (a busca é local-first; só o import do 0015 escreve).
//
// A interface é o ponto de plugabilidade multi-vertical: trocar ou adicionar um
// provider não toca o handler de busca, que fala só com esta abstração.
package provider

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrNaoEncontrado é o erro SENTINELA que um provider devolve quando o item NÃO
// EXISTE na fonte (ex.: 404 do TMDB), distinto de uma falha de comunicação. O
// handler do detalhe externo (briefing 0022) o traduz em 404 (id inexistente),
// reservando o 502 para falhas reais da fonte (timeout/5xx). Compare com
// errors.Is(err, provider.ErrNaoEncontrado).
var ErrNaoEncontrado = errors.New("provider: item nao encontrado na fonte")

// Provider é uma fonte externa de metadados. Implementações são injetadas no
// server via option; nil significa "busca externa desligada" (degrada para só
// resultados locais, sem erro).
type Provider interface {
	// Nome identifica a fonte (ex.: "tmdb"). Usado em logs/diagnóstico.
	Nome() string
	// Buscar consulta a fonte por `query`, filtrando opcionalmente por `tipo`
	// (""|"movie"|"tv"). `lang` é o idioma de leitura ATIVO (0019/0021): a fonte
	// devolve títulos/sinopses nele quando houver (sem `lang`, cai no default da
	// fonte). É um único idioma (a busca mostra resultados diretos, sem a cadeia de
	// fallback do resolver). Erro/timeout é responsabilidade do chamador degradar —
	// o provider apenas o propaga.
	Buscar(ctx context.Context, query, tipo, lang string) ([]ResultadoExterno, error)
	// BuscarDetalhe puxa o DETALHE (payload da fonte) de um item pelo seu
	// `sourceID` e `tipo` ("movie"|"tv"), para o import/enrich (briefing 0015). Os
	// `langs` são os idiomas PERMITIDOS das settings (0019): a fonte multi-idioma
	// (0020) puxa TODAS as traduções permitidas numa só ida e devolve um PAYLOAD
	// AUMENTADO ({detail, tags_by_lang}) — ainda um cache re-componível (ADR-0011).
	// Sem `langs` degrada ao comportamento mono-idioma. O mapeamento canônico é de
	// MapearDetalhe (PURO, sem rede). Erro/timeout propaga.
	BuscarDetalhe(ctx context.Context, sourceID, tipo string, langs []string) (json.RawMessage, error)
	// MapearDetalhe traduz o payload de BuscarDetalhe para o mapa CANÔNICO com OS
	// NOSSOS nomes de campo, consumido pelo resolver (raw→canônico é do provider; o
	// resolver não conhece o TMDB). É PURO (roda em tempo de leitura, sem rede). Os
	// campos TRADUZÍVEIS (title, description, tags) saem como `map[idioma]valor`; os
	// NEUTROS (original_title, release_date, poster_url) como escalares. Expõe também
	// `original_language` (o enrich o grava em entity.original_language). Um payload
	// LEGADO (detalhe cru do 0015, sem envelope) degrada para escalares sob o idioma
	// principal. Só inclui as chaves que a fonte trouxe. SEM elenco/equipe (MD6).
	MapearDetalhe(raw json.RawMessage) (map[string]any, error)
}

// ColecaoProvider é uma CAPACIDADE OPCIONAL de um Provider: buscar uma coleção
// (belongs_to_collection do TMDB — um conjunto de filmes, ≠ playlist) com suas
// partes, para a PÁGINA de coleção (0024, follow-up do 0023). É opcional porque nem
// toda fonte tem coleções; o handler faz type-assert do Provider para esta interface
// e degrada com 503 quando a fonte não a implementa. Diferente do detalhe (0022),
// NÃO há cache re-componível multi-idioma aqui: a coleção é buscada no idioma ATIVO
// (mono-idioma pragmático); o multi-idioma pleno fica como follow-up.
type ColecaoProvider interface {
	// BuscarColecao puxa a coleção pelo seu `colecaoID` na fonte, localizada em
	// `lang` (idioma de leitura ativo; vazio cai no default da fonte). ErrNaoEncontrado
	// quando a coleção não existe (→ 404); demais erros propagam (→ 502).
	BuscarColecao(ctx context.Context, colecaoID, lang string) (Colecao, error)
}

// Colecao é uma coleção canônica: os metadados do conjunto (nome/sinopse/artes) e
// suas PARTES, cada uma um ResultadoExterno (o mesmo shape de um item de busca —
// reuso proposital, a parte é um filme navegável ao detalhe externo). Os ponteiros
// são opcionais (nil quando a fonte não trouxe o campo).
type Colecao struct {
	// Source é o nome da fonte (ex.: "tmdb").
	Source string
	// SourceID é o id da coleção NA FONTE (como string).
	SourceID string
	// Name é o nome exibível da coleção.
	Name string
	// Overview é a sinopse da coleção, quando houver.
	Overview *string
	// PosterURL/BackdropURL são as artes DIRETAS da coleção na fonte, quando houver
	// (o handler pode reescrevê-las para o proxy local — 0024).
	PosterURL   *string
	BackdropURL *string
	// Parts são os itens da coleção, em ordem da fonte.
	Parts []ResultadoExterno
}

// TituloAlternativoProvider é uma CAPACIDADE OPCIONAL de um Provider (briefing
// 0084, análoga a ColecaoProvider): trazer os TÍTULOS ALTERNATIVOS/TRADUZIDOS
// que a fonte conhece de um item, para a pontuação de candidato
// (matching.PontuarComAlternativos) comparar contra o LEQUE de títulos que a
// fonte já tem, não só o título no idioma consultado. É opcional porque nem
// toda fonte expõe traduções de título de forma barata; o finder de candidato
// (melhorCandidatoExterno, internal/server/automatch.go) faz type-assert do
// Provider para esta interface e degrada para o score de título único quando
// ausente -- NUNCA bloqueia a pontuação.
type TituloAlternativoProvider interface {
	// BuscarTitulosAlternativos lista os títulos alternativos/traduzidos que a
	// fonte conhece do item `sourceID` (tipo "movie"|"tv"). A lista pode conter
	// duplicatas ou strings vazias -- o chamador não exige dedupe perfeito.
	// Erro/timeout propaga; o chamador degrada (best-effort, loga e segue sem
	// alternativos para aquele candidato).
	BuscarTitulosAlternativos(ctx context.Context, tipo, sourceID string) ([]string, error)
}

// SerieProvider é uma CAPACIDADE OPCIONAL de um Provider (análoga a ColecaoProvider,
// briefing 0031 / ADR-0021): trazer a HIERARQUIA de uma série — temporadas +
// episódios + as ordenações (season types) que ela usa. É opcional porque nem toda
// fonte modela séries com ordens (o TMDB não expõe season types); o handler faz
// type-assert do Provider para esta interface e DEGRADA (503) quando a fonte não a
// implementa. Como o detalhe (0022), os idiomas são resolvidos com FALLBACK e os
// campos traduzíveis saem por idioma; os neutros como escalares.
type SerieProvider interface {
	// BuscarSerieArvore puxa a árvore da série `sourceID` na fonte, localizada nos
	// idiomas PERMITIDOS `langs` (0019; vazio cai no idioma da fonte). Devolve SÓ as
	// temporadas da ordenação DEFAULT (TV2 — enxuto); as demais ordens existem apenas
	// como numeração nos episódios (Numbering). ErrNaoEncontrado quando a série não
	// existe (→ 404); demais erros propagam (→ 502).
	BuscarSerieArvore(ctx context.Context, sourceID string, langs []string) (SerieArvore, error)
}

// SerieRicoProvider é uma CAPACIDADE OPCIONAL de um Provider (briefing 0045, rota C):
// ENRIQUECER o NÓ SÉRIE com os campos ricos que a fonte da árvore (TVDB) não dá —
// slogan, gêneros, classificação, palavras-chave, similares e SITUAÇÃO. É separada de
// SerieProvider (a árvore) de propósito: quem tem a árvore (TVDB) não tem os ricos, e
// quem tem os ricos (TMDB) não modela a árvore com ordens. O enrich faz o type-assert
// do provider "tmdb" para esta interface e DEGRADA em silêncio quando ausente (série
// só-TVDB fica com os campos vazios, nunca erro).
type SerieRicoProvider interface {
	// BuscarSerieDetalheRico cruza o `tvdbID` da série para o id da fonte rica e
	// devolve o canônico RICO já mapeado (SÓ as chaves ricas — nunca title/overview/
	// original_language, para não clobbar a identidade da árvore TVDB na composição de
	// camadas). ok=false sem match (graça, sem erro); erro real (rede) propaga para o
	// enrich logar e seguir só-TVDB. `langs` são os idiomas permitidos (0020): tagline e
	// gêneros saem por idioma.
	BuscarSerieDetalheRico(ctx context.Context, tvdbID string, langs []string) (map[string]any, bool, error)
}

// ChaveEnvelopeSerieRico é a chave do envelope de media_external que carrega o canônico
// RICO de série JÁ MAPEADO (rota C do 0045). O enrich grava o payload
// {ChaveEnvelopeSerieRico: <canônico rico>} sob a fonte "tmdb" do nó série, e o
// MapearDetalhe do provider TMDB o reconhece e devolve o mapa como está — assim os
// campos ricos entram na composição de camadas (montarExterno) reusando a maquinaria do
// filme, SEM clobbar título/idioma da árvore TVDB (o envelope não os carrega).
const ChaveEnvelopeSerieRico = "tmdb_serie_rico"

// TvdbCruzadoProvider é uma CAPACIDADE OPCIONAL de um Provider (briefing 0087): extrair
// o cross-id TVDB de um payload de detalhe de SÉRIE já buscado (sem ida extra à rede) —
// fecha a lacuna que deixava toda série cuja árvore vem só do TMDB (sem TVDB configurado,
// o cenário do briefing 0046, independência de provider) sem id utilizável para o
// ArteProvider fanart.tv, que na TV exige tvdb_id exclusivamente (fanarttv.rotaETabela,
// case "tv" — sem fallback para tmdb/imdb, ao contrário do case "movie"). Só o client
// TMDB a implementa hoje (o append `external_ids` que o TMDB já busca no detalhe rico traz
// `tvdb_id` de graça, mas o mapeamento canônico só extraía o `imdb_id` até aqui). O
// enrich faz type-assert do provider da FONTE-BASE da árvore para esta interface e
// degrada em silêncio quando ausente (fonte-base != TMDB) ou sem match no payload.
type TvdbCruzadoProvider interface {
	// TvdbIDDoDetalheSerie extrai o tvdb_id de um payload de detalhe de série JÁ
	// buscado (o mesmo raw devolvido por BuscarDetalhe/PayloadFresco) — PURO, sem rede.
	// ok=false quando o payload não traz um tvdb_id utilizável.
	TvdbIDDoDetalheSerie(raw json.RawMessage) (string, bool)
}

// Similar é um item similar/recomendado canônico (briefing 0091), já filtrado
// (sem adulto, sem volume de voto irrelevante) e ORDENADO por afinidade com o
// item de origem — o consumidor não reprocessa nota/gênero/adulto, só exibe.
type Similar struct {
	// SourceID é o id do item NA FONTE (ex.: id numérico do TMDB, como string).
	SourceID string
	// Type é o nosso tipo canônico ("movie"|"tv"), herdado do item de origem.
	Type string
	// Title é o título exibível do item.
	Title string
	// Year são os 4 primeiros dígitos da data de lançamento/estreia, quando houver.
	Year *string
	// PosterURL é a URL DIRETA do pôster do item na fonte, quando houver.
	PosterURL *string
}

// SimilarProvider é uma CAPACIDADE OPCIONAL de um Provider (briefing 0091):
// extrai os itens SIMILARES/recomendados a partir do payload de detalhe JÁ
// buscado por BuscarDetalhe (sem ida extra à rede — mesmo padrão de
// TvdbCruzadoProvider), já filtrados por qualidade (sem adulto, sem volume de
// voto irrelevante) e ORDENADOS por afinidade real com o item de origem. É
// opcional porque nem toda fonte modela recomendação (o TVDB não tem); o
// chamador (entities.go/external_detail.go) faz type-assert do Provider para
// esta interface e degrada para lista vazia (sem erro, sem 500) quando ausente.
type SimilarProvider interface {
	// Similares extrai os itens similares/recomendados do payload `raw` (o
	// mesmo devolvido por BuscarDetalhe) — PURO, sem rede. Erro real (payload
	// ilegível) propaga; o chamador loga e segue sem similares daquela camada.
	Similares(raw json.RawMessage) ([]Similar, error)
}

// ArteProvider é uma CAPACIDADE OPCIONAL de um Provider (briefing 0048): buscar o
// INVENTÁRIO de artes de um item por seu id externo — pôster, fanart, logo, clearart,
// disc, banner, thumb, characterart e artes de temporada. É a capacidade da fanart.tv
// (fonte DEDICADA a arte que o TMDB/TVDB não cobrem bem). É opcional porque nem toda
// fonte tem arte extra; o enrich faz type-assert do Provider para esta interface e
// DEGRADA em silêncio quando ausente (sem a chave configurada, regressão zero). O
// resultado é um INVENTÁRIO (lista de URLs + metadados), NUNCA binários baixados — o
// download real é lazy (só a arte selecionada, via cachearArte).
type ArteProvider interface {
	// BuscarArtes lista as artes do item pelos ids externos que ele tiver (`ids`),
	// filtrando por `tipo` ("movie"|"tv"). `langs` são os idiomas permitidos (0020),
	// para a escolha da arte servida por padrão; o inventário PRESERVA o `lang` de todas.
	// Sem id utilizável para a fonte, ou sem match, devolve (nil, nil) — graça. Erro real
	// (rede/status) propaga para o enrich logar e seguir.
	BuscarArtes(ctx context.Context, ids ExternalIDs, tipo string, langs []string) ([]Arte, error)
}

// TemaMusicalProvider é uma CAPACIDADE OPCIONAL (briefing 0103): busca um
// candidato de música-tema (link de vídeo, nunca binário) pelos ids
// externos que o item tiver. É opcional porque nem toda fonte modela tema
// musical (só ThemerrDB hoje); o chamador faz type-assert e degrada em
// silêncio (sem candidato) quando ausente ou sem match.
type TemaMusicalProvider interface {
	// BuscarTemaMusical procura um tema pelos ids externos (`ids`), filtrando
	// por `tipo` ("movie"|"tv"). Sem id utilizável ou sem match:
	// (TemaMusical{}, false, nil) — graça, não erro. Erro real (rede/status)
	// propaga.
	BuscarTemaMusical(ctx context.Context, ids ExternalIDs, tipo string) (TemaMusical, bool, error)
}

// TemaMusical é um candidato de tema musical (ThemerrDB): a URL do vídeo na
// fonte (YouTube) — NUNCA o binário; download real é lazy via yt-dlp (0101).
type TemaMusical struct {
	URL string
}

// ChaveEnvelopeTemaMusical é a chave do envelope de media_external que carrega
// o CANDIDATO de tema musical de uma fonte (ThemerrDB, briefing 0103) — mesma
// técnica de ChaveEnvelopeInventarioArtes, mas mais simples (um candidato só,
// não um mapa por kind). O enrich grava {ChaveEnvelopeTemaMusical: <url>} sob
// a fonte "themerrdb" quando há match; sem match, grava um envelope vazio (a
// consulta já foi feita, sem re-bater na API a cada GET). É uma camada de
// candidato PURA: NUNCA entra na composição de camadas do resolver.
const ChaveEnvelopeTemaMusical = "themerrdb_tema"

// PessoaProvider é uma CAPACIDADE OPCIONAL de um Provider (briefing 0029 Fase D,
// segunda seção): trazer a FILMOGRAFIA de uma PESSOA na fonte — os trabalhos dela
// (cast/crew) que AINDA NÃO estão no catálogo, para a página da pessoa mostrar "ainda
// não importados" ao lado da filmografia já importada (que vem só do banco, sem
// depender de provider). É opcional porque nem toda fonte modela pessoa; o handler faz
// type-assert do Provider para esta interface e DEGRADA para `[]` (sem erro) quando a
// fonte não a implementa.
type PessoaProvider interface {
	// BuscarFilmografia puxa os trabalhos da pessoa `sourceID` na fonte, localizados em
	// `lang` (idioma de leitura ativo; vazio cai no default da fonte). A implementação
	// decide teto/ordenação/dedup (cada fonte tem sua própria noção de "filmografia
	// grande demais"). ErrNaoEncontrado quando a pessoa não existe na fonte (o handler
	// degrada do mesmo jeito de um erro real: loga e devolve `[]`, a página não cai).
	// (nil, nil) é graça (pessoa existe, sem créditos).
	BuscarFilmografia(ctx context.Context, sourceID, lang string) ([]CreditoFilmografia, error)
}

// CreditoFilmografia é um trabalho canônico da filmografia EXTERNA de uma pessoa (ainda
// não importado): a identidade do item (source/source_id/type) mais o papel da pessoa
// nele (kind/character/job — o par que não se aplica ao kind fica nil). Espelha
// ResultadoExterno na identidade do item, mas carrega também o papel (que
// ResultadoExterno não tem, por não ser sobre pessoa).
type CreditoFilmografia struct {
	// Source é o nome da fonte (ex.: "tmdb").
	Source string
	// SourceID é o id do item (obra) NA FONTE.
	SourceID string
	// Type é o nosso tipo canônico do item ("movie"|"tv").
	Type string
	// Title é o título exibível do item.
	Title string
	// Year são os 4 primeiros dígitos da data de lançamento/estreia, quando houver.
	Year *string
	// PosterURL é a URL DIRETA do pôster do item na fonte, quando houver (o handler
	// proxya, como os demais campos de imagem externos).
	PosterURL *string
	// Kind é o papel da pessoa NESTE item: "cast" (atuação) ou "crew" (equipe).
	Kind string
	// Character é o personagem, só quando Kind="cast" (nil em Kind="crew").
	Character *string
	// Job é o(s) cargo(s) de equipe, só quando Kind="crew" (nil em Kind="cast"); pode
	// juntar vários cargos distintos da MESMA obra (a fonte pode creditar mais de um).
	Job *string
}

// ExternalIDs carrega os ids externos que um item possui (o que a identidade MD4 tiver).
// A fanart.tv usa Tmdb/Imdb para filme e Tvdb para série; cada ArteProvider escolhe o
// que sabe consumir e degrada quando o id de que precisa está vazio.
type ExternalIDs struct {
	Tmdb string
	Imdb string
	Tvdb string
}

// Arte é uma arte canônica do inventário de um ArteProvider (briefing 0048). Kind é o
// nosso kind normalizado (poster/fanart/logo/clearart/disc/banner/thumb/characterart);
// URL é a arte DIRETA na fonte (servida via proxy de imagem). Lang é o idioma (2 letras
// ou ""=textless). Likes é a popularidade (para ordenar). DiscType/Disc só valem para
// disc; Season (nº, "all" ou "0") só para arte de temporada — roteia a arte à entity da
// temporada correspondente.
type Arte struct {
	Kind     string
	URL      string
	Lang     string
	Likes    int
	DiscType string
	Disc     int
	Season   string
}

// ChaveEnvelopeInventarioArtes é a chave do envelope de media_external que carrega o
// INVENTÁRIO de artes de uma fonte de arte (fanart.tv). O enrich grava
// {ChaveEnvelopeInventarioArtes: {kind: [ArteInventario...]}} sob a fonte da arte
// (source="fanarttv"), SEM baixar binários; o endpoint /artwork o lê para agrupar as
// artes disponíveis por kind. É uma camada de inventário PURA: o MapearDetalhe da fonte
// de arte é no-op (não polui a composição de camadas do resolver).
const ChaveEnvelopeInventarioArtes = "fanarttv_inventory"

// ArteInventario é uma arte serializada no envelope de inventário (media_external). É o
// espelho persistível de Arte SEM o Kind (que vira a chave do mapa por kind no envelope).
type ArteInventario struct {
	URL      string `json:"url"`
	Lang     string `json:"lang,omitempty"`
	Likes    int    `json:"likes,omitempty"`
	DiscType string `json:"disc_type,omitempty"`
	Disc     int    `json:"disc,omitempty"`
	Season   string `json:"season,omitempty"`
}

// SerieArvore é a hierarquia canônica de uma série: a ordenação default, a lista de
// ordenações que a série usa, e as temporadas SÓ da ordenação default. Cada episódio
// (grão fino, ancorado no id da fonte) carrega a numeração de TODAS as ordens.
type SerieArvore struct {
	// DefaultSeasonType é a ordenação default da série (ex.: "official"); é a única
	// materializada em Temporadas.
	DefaultSeasonType string
	// SeasonTypes são as ordenações que a série usa (ex.: ["official","dvd","absolute"]).
	SeasonTypes []string
	// Temporadas são as temporadas SÓ da ordenação default (ADR-0021: as alternativas
	// não viram temporada — só numeração nos episódios).
	Temporadas []TemporadaExterna
}

// TemporadaExterna é uma temporada canônica da ordenação default, com seus episódios.
type TemporadaExterna struct {
	// Ordering é a ordenação a que a temporada pertence (sempre a default aqui).
	Ordering string
	// SeasonNumber é o número da temporada NAQUELA ordem (pode ser ano civil, 0 para
	// specials, índice de box-set — não assumir 1..N; ADR-0021).
	SeasonNumber int
	// Name é o nome exibível por idioma (pode ser vazio: temporadas raramente têm nome).
	Name map[string]string
	// Year é o ano da temporada, quando a fonte informou.
	Year string
	// PosterURL é a arte DIRETA da temporada na fonte, quando houver.
	PosterURL string
	// Episodios são os episódios desta temporada (na ordenação default), em ordem.
	Episodios []EpisodioExterno
}

// EpisodioExterno é um episódio canônico de grão fino, ancorado no id da fonte. A
// numeração de TODAS as ordens vive em Numbering; a identidade NUNCA é o (season,
// number), que muda por ordem (ADR-0021).
type EpisodioExterno struct {
	// SourceID é o episode.id na fonte — a ÂNCORA canônica (nunca o número).
	SourceID string
	// ExternalIDs são os ids do episódio em outras fontes ({tvdb, imdb, tmdb} quando
	// houver). Do tree só vem {tvdb} (o cruzamento por episódio exige o extended, N
	// chamadas — deferido); o cruzamento forte é no nível da SÉRIE.
	ExternalIDs map[string]string
	// Title é o título por idioma, COM FALLBACK (o idioma sem tradução simplesmente não
	// aparece no mapa — o resolver cai na cadeia). Pode conter só o idioma original.
	Title map[string]string
	// Overview é a sinopse por idioma (mesma semântica de Title).
	Overview map[string]string
	// AbsoluteNumber é a numeração linear canônica (estável entre ordens), quando houver.
	AbsoluteNumber *int
	// Numbering traz a numeração do MESMO episódio em TODAS as ordens que a série usa
	// (ex.: {"official":{1933,16}, "dvd":{23,5}, "absolute":{1,373}}).
	Numbering map[string]Numero
	// AirDate é a data de exibição (YYYY-MM-DD), quando houver.
	AirDate string
	// Runtime é a duração em minutos, quando a fonte informou.
	Runtime *int
	// StillURL é o frame/screencap do episódio, quando houver.
	StillURL string
}

// Numero é o par (temporada, número) de um episódio NUMA ordenação. Não é chave: o
// mesmo episódio tem Numero diferente por ordem (ADR-0021).
type Numero struct {
	Season int
	Number int
}

// ResultadoExterno é o resultado canônico de uma busca externa, independente da
// fonte. Os ponteiros são opcionais (nil quando a fonte não trouxe o campo).
// Saved é SEMPRE false na busca: um resultado externo nunca é uma entidade do
// catálogo até o import explícito (0015).
type ResultadoExterno struct {
	// Source é o nome da fonte (ex.: "tmdb").
	Source string
	// SourceID é o id do item NA FONTE (ex.: id numérico do TMDB, como string).
	SourceID string
	// Type é o nosso tipo canônico ("movie"|"tv").
	Type string
	// Title é o título exibível do item.
	Title string
	// Year são os 4 primeiros dígitos da data de lançamento, quando houver.
	Year *string
	// Overview é a sinopse, quando houver.
	Overview *string
	// PosterURL é a URL DIRETA da arte na fonte, quando houver (o proxy/cache de
	// imagem é do 0015).
	PosterURL *string
	// Saved indica se o item já existe no catálogo. Na busca é sempre false.
	Saved bool
}
