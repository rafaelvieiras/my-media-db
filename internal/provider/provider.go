// Package provider define a abstração de FONTE EXTERNA de metadados (briefing
// 0014). Um Provider sabe buscar títulos numa fonte (TMDB, TVDB, MusicBrainz…) e
// devolver resultados no tipo canônico ResultadoExterno — sem NUNCA persistir
// nada (a busca é local-first; só o import do 0015 escreve).
//
// A interface é o ponto de plugabilidade multi-vertical: trocar ou adicionar um
// provider não toca o handler de busca, que fala só com esta abstração.
package provider

import "context"

// Provider é uma fonte externa de metadados. Implementações são injetadas no
// server via option; nil significa "busca externa desligada" (degrada para só
// resultados locais, sem erro).
type Provider interface {
	// Nome identifica a fonte (ex.: "tmdb"). Usado em logs/diagnóstico.
	Nome() string
	// Buscar consulta a fonte por `query`, filtrando opcionalmente por `tipo`
	// (""|"movie"|"tv"). Erro/timeout é responsabilidade do chamador degradar —
	// o provider apenas o propaga.
	Buscar(ctx context.Context, query, tipo string) ([]ResultadoExterno, error)
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
