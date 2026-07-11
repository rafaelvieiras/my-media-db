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
