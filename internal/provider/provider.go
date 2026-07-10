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
