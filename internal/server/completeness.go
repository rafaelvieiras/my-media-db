package server

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// Este arquivo implementa a AVALIAÇÃO DE COMPLETUDE DE CATALOGAÇÃO por item
// (briefing 0058): consome a régua configurada em catalog_completeness.go
// (briefing 0057) e decide, para UM item, o que falta preencher. Distinto de
// completude de POSSE (web/src/lib/entity-badges.ts, briefings 0036/0037) --
// ver a nota de catalog_completeness.go, repetida lá.
//
// avaliarCompletude é PURA (sem I/O) de propósito: o MESMO teste cobre os
// dois caminhos que a consomem -- ao vivo no detalhe
// (anotarCompletudeCatalogacao, entities.go) e em lote no job
// (RecalcularCompletudeCatalogacao, completeness_recalc.go). Só o modo de
// PREENCHER dadosCompletude muda entre os dois; a regra em si vive só aqui.

// statusDraft é o valor de entity.status de um item nunca casado (briefing
// 0050/0056): a fila de revisão e a query ListarRevisao já usam esta mesma
// string crua (internal/db/queries/review.sql); centralizado aqui só para os
// call-sites deste briefing (ehDraft) não espalharem o literal.
const statusDraft = "draft"

// dadosCompletude é o shape DESACOPLADO da origem dos dados, avaliado contra
// a régua de UM type (tipoCompletenessConfig, briefing 0057): preenchido
// tanto a partir de um resolver.Entidade já resolvido (caminho ao vivo do
// detalhe, via dadosCompletudeDeEntidade) quanto por uma leitura mais leve
// dentro do job em lote (que hoje reusa o MESMO resolverEntity -- ver
// completeness_recalc.go).
type dadosCompletude struct {
	Description    *string
	Runtime        *int
	Certification  *string
	Keywords       []string
	TemElenco      bool
	TemArquivo     bool
	ArtesPresentes map[string]bool
}

// resultadoCompletude é ALIAS de resolver.CatalogCompleteness (não um tipo
// novo): precisa ser o MESMO tipo que resolver.Entidade.CatalogCompleteness
// carrega, para anotarCompletudeCatalogacao (entities.go) preencher o campo
// do detalhe sem conversão — e para o card DTO (entityCardDTO,
// completeness_recalc.go) reusar a mesma forma ao ler/gravar o cache
// entity_catalog_completeness. As tags JSON (complete/missing) vivem na
// definição canônica em internal/resolver (é ela que o detalhe serializa
// direto por GET /entities/{id}).
type resultadoCompletude = resolver.CatalogCompleteness

// Chaves estáveis de `missing` (briefing 0058 §4): espelham 1:1 os bools de
// tipoCompletenessConfig; prefixoArtwork monta `artwork:<kind>` (um item por
// kind AUSENTE, não um "artwork" agregado -- o dono quer saber EXATAMENTE o
// que falta).
const (
	chaveDescription   = "description"
	chaveRuntime       = "runtime"
	chaveCertification = "certification"
	chaveKeywords      = "keywords"
	chaveCast          = "cast"
	chaveMediaFile     = "media_file"
	prefixoArtwork     = "artwork:"
)

// avaliarCompletude decide se UM item atende a régua `cfg` de completude de
// catalogação (briefing 0057/0058). PURA (sem I/O): testável isoladamente,
// sem banco. `achou=false` (o type da entity não tem entrada na config --
// 0057 §4: "tipo ausente = nada exigido") devolve {true, []} direto, sem
// olhar `dados`. Senão, testa cada bool configurado contra `dados` e cada
// kind de `cfg.Artwork` contra `dados.ArtesPresentes`, acumulando as chaves
// ausentes na ordem fixa description, runtime, certification, keywords,
// cast, media_file, artwork:<kind...> (a ordem de cfg.Artwork -- a que o
// dono configurou em /settings, não alfabética) -- ordem estável = tooltip/
// lista previsíveis, útil pra teste e pra não reordenar a UI a cada render.
func avaliarCompletude(cfg tipoCompletenessConfig, achou bool, dados dadosCompletude) resultadoCompletude {
	if !achou {
		return resultadoCompletude{Complete: true, Missing: []string{}}
	}
	faltando := make([]string, 0)
	if cfg.Description && campoTextoVazio(dados.Description) {
		faltando = append(faltando, chaveDescription)
	}
	if cfg.Runtime && dados.Runtime == nil {
		faltando = append(faltando, chaveRuntime)
	}
	if cfg.Certification && campoTextoVazio(dados.Certification) {
		faltando = append(faltando, chaveCertification)
	}
	if cfg.Keywords && len(dados.Keywords) == 0 {
		faltando = append(faltando, chaveKeywords)
	}
	if cfg.Cast && !dados.TemElenco {
		faltando = append(faltando, chaveCast)
	}
	if cfg.MediaFile && !dados.TemArquivo {
		faltando = append(faltando, chaveMediaFile)
	}
	for _, kind := range cfg.Artwork {
		if !dados.ArtesPresentes[kind] {
			faltando = append(faltando, prefixoArtwork+kind)
		}
	}
	return resultadoCompletude{Complete: len(faltando) == 0, Missing: faltando}
}

// campoTextoVazio diz se um campo textual traduzível (sinopse/classificação)
// conta como AUSENTE para a régua: ponteiro nil (nunca resolvido) OU string
// em branco (resolvido, mas vazio -- mesma UX de "nada pra mostrar" que o
// detalhe já trata como ausência).
func campoTextoVazio(s *string) bool {
	return s == nil || strings.TrimSpace(*s) == ""
}

// ehDraft diz se o status de uma entity é 'draft' (nunca casado, briefing
// 0050/0056): um item draft não tem metadados resolvidos pra avaliar
// completude de catalogação (briefing 0058 §3, explicitamente fora de
// escopo). Nomeado por clareza nos call-sites (entities.go/
// completeness_recalc.go).
func ehDraft(status string) bool {
	return status == statusDraft
}

// dadosCompletudeDeEntidade adapta o shape já resolvido do detalhe (briefing
// 0058) para dadosCompletude, sem nenhuma query extra: Description/Runtime/
// Certification/Keywords/Cast/Owned já vêm prontos de resolver.Entidade
// (resolverEntity já os monta de qualquer jeito, com ou sem este briefing).
// `assets` é a MESMA lista que resolverEntity já carregou
// (ListarAssetsPorEntity) para montar poster_url/backdrop_url/still_url --
// reusada aqui, nunca re-consultada (resolver.Entidade não guarda a lista
// crua de assets, só as URLs já resolvidas, por isso entra como parâmetro
// à parte em vez de um campo do shape).
func dadosCompletudeDeEntidade(e resolver.Entidade, assets []sqlcgen.Asset) dadosCompletude {
	return dadosCompletude{
		Description:    e.Description,
		Runtime:        e.Runtime,
		Certification:  e.Certification,
		Keywords:       e.Keywords,
		TemElenco:      len(e.Cast) > 0,
		TemArquivo:     e.Owned,
		ArtesPresentes: artesPresentesDeAssets(assets),
	}
}

// completudeDoCache decodifica as colunas completeness_complete/
// completeness_missing_json (LEFT JOIN entity_catalog_completeness) lidas
// pelas queries de card (ListarEntityCards/ListarEntityCardsDesc/
// ObterEntityCard, briefing 0058): nil quando a linha do cache não existe
// (NULL, item ainda não avaliado -- novo ou draft) ou o missing_json está
// corrompido (degrada para nil -- nunca quebra a listagem por um cache
// inválido).
func completudeDoCache(complete sql.NullInt64, missingJSON sql.NullString) *resultadoCompletude {
	if !complete.Valid || !missingJSON.Valid {
		return nil
	}
	var missing []string
	if err := json.Unmarshal([]byte(missingJSON.String), &missing); err != nil {
		return nil
	}
	if missing == nil {
		missing = []string{}
	}
	return &resultadoCompletude{Complete: complete.Int64 != 0, Missing: missing}
}

// artesPresentesDeAssets resume `assets` (ListarAssetsPorEntity) num mapa
// kind -> tem asset SERVÍVEL, pela MESMA precedência de escolherAssetServido
// (assets.go/briefing 0017): preferida -> user -> external -> base. Usado só
// para a avaliação de completude (briefing 0058); não confundir com a
// escolha de UM asset para servir (poster_url/backdrop_url), feita à parte
// em resolverEntity/artesLocais.
func artesPresentesDeAssets(assets []sqlcgen.Asset) map[string]bool {
	presentes := make(map[string]bool)
	for _, a := range assets {
		if presentes[a.Kind] {
			continue
		}
		if _, ok := escolherAssetServido(assets, a.Kind); ok {
			presentes[a.Kind] = true
		}
	}
	return presentes
}
