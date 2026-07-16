package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa o CAMINHO EM LOTE da completude de catalogação
// (briefing 0058): RecalcularCompletudeCatalogacao é chamado pelo job
// `saude_biblioteca` (internal/job/libraryhealth, briefing 0053) ao final de
// cada execução, e recalcula o CACHE persistido (entity_catalog_completeness)
// que a listagem/`/members` leem — ver a nota longa em completeness.go/
// briefing 0058 §6 sobre por que a listagem NÃO calcula ao vivo por item
// (o detalhe calcula, este arquivo não).

// paginaRecalculoCompletude é o tamanho de página usado ao varrer TODAS as
// entities (ListarEntities/LIMIT+OFFSET): grande o bastante para poucas
// idas ao banco, pequeno o bastante para não seguar uma página gigante em
// memória de uma vez. É um passo de paginação INTERNA do job, por isso não
// reusa limitMaximo (entities.go) — aquele é um teto de resposta HTTP, este
// é só o passo do varredor.
const paginaRecalculoCompletude = 200

// RecalcularCompletudeCatalogacao varre TODAS as entities NÃO-DRAFT em
// páginas (mesmo padrão de paginação interna do pacote) e grava o resultado
// da avaliação de completude de catalogação (briefing 0058) em
// entity_catalog_completeness. Satisfaz libraryhealth.CompletenessRecalculator
// (injetada em server.WithLibraryHealthJob) para o job `saude_biblioteca`
// (0053) chamar este passo ao final de cada execução.
//
// Erro de UM item (ex.: sql.ErrNoRows numa entity purgada entre a listagem e
// o processamento) NÃO aborta os demais — best-effort, mesmo espírito de
// escanearTodas/autoMatchLote (0053/0054): só loga e segue para o próximo.
// Só um erro de LISTAGEM (a própria página falhar) interrompe a varredura —
// aí não há como saber o que mais existe para avaliar.
func (s *Server) RecalcularCompletudeCatalogacao(ctx context.Context) (avaliados, incompletos int, err error) {
	cfg, err := s.obterCatalogCompletenessSettings(ctx)
	if err != nil {
		return 0, 0, err
	}
	w := sqlcgen.New(s.db.Write)

	var offset int64
	for {
		ents, err := s.leitura().ListarEntities(ctx, sqlcgen.ListarEntitiesParams{
			Limit: paginaRecalculoCompletude, Offset: offset,
		})
		if err != nil {
			return avaliados, incompletos, err
		}
		if len(ents) == 0 {
			break
		}
		for _, ent := range ents {
			if ehDraft(ent.Status) {
				continue
			}
			resultado, itemErr := s.avaliarERecalcularUmItem(ctx, w, cfg, ent)
			if itemErr != nil {
				log.Printf("saude_biblioteca: recalculo de completude de catalogacao falhou para %s (best-effort, ignorado): %v", ent.ID, itemErr)
				continue
			}
			avaliados++
			if !resultado.Complete {
				incompletos++
			}
		}
		if len(ents) < paginaRecalculoCompletude {
			break
		}
		offset += paginaRecalculoCompletude
	}
	return avaliados, incompletos, nil
}

// avaliarERecalcularUmItem avalia UMA entity não-draft contra a régua `cfg` e
// grava o resultado (UPSERT) em entity_catalog_completeness.
//
// Quando o type de `ent` não está em `cfg`, grava {true,[]} DIRETO, sem
// chamar o resolver — evita o custo de resolução (files/assets/override/
// externo/ancestrais/posse) para tipos sem régua nenhuma (briefing 0058 §4).
// Senão, chama s.resolverEntity com idioma vazio (base neutra — completude
// depende de PRESENÇA do campo, não de qual idioma o preencheu) e reaproveita
// resolvida.CatalogCompleteness, que resolverEntity JÁ calcula internamente
// (anotarCompletudeCatalogacao, entities.go, mesmo caminho do detalhe) — não
// duplica a extração de rich fields nem a leitura de assets aqui.
func (s *Server) avaliarERecalcularUmItem(ctx context.Context, w *sqlcgen.Queries, cfg catalogCompletenessSettings, ent sqlcgen.Entity) (resultadoCompletude, error) {
	var resultado resultadoCompletude
	if _, achou := cfg[ent.Type]; !achou {
		resultado = resultadoCompletude{Complete: true, Missing: []string{}}
	} else {
		resolvida, ok, err := s.resolverEntity(ctx, ent.ID, "", true)
		if err != nil {
			return resultadoCompletude{}, err
		}
		if !ok {
			return resultadoCompletude{}, sql.ErrNoRows
		}
		if resolvida.CatalogCompleteness == nil {
			// Não deveria acontecer para um item já filtrado como não-draft
			// (anotarCompletudeCatalogacao só deixa nil para draft) — degrada
			// com um erro explícito em vez de gravar um zero-value enganoso.
			return resultadoCompletude{}, errors.New("resolverEntity não calculou catalog_completeness para item não-draft")
		}
		resultado = *resolvida.CatalogCompleteness
	}

	missingJSON, err := json.Marshal(resultado.Missing)
	if err != nil {
		return resultadoCompletude{}, err
	}
	var complete int64
	if resultado.Complete {
		complete = 1
	}
	if err := w.UpsertEntityCatalogCompleteness(ctx, sqlcgen.UpsertEntityCatalogCompletenessParams{
		EntityID:    ent.ID,
		Complete:    complete,
		MissingJson: string(missingJSON),
	}); err != nil {
		return resultadoCompletude{}, err
	}
	return resultado, nil
}
