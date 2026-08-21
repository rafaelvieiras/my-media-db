package server

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa a RECONCILIAÇÃO DE FILHOS ÓRFÃOS (briefing 0120),
// complemento do enriquecimento retroativo (0056/0116/0117/0118): uma
// faixa/série que ficou presa em rascunho (draft) porque o match textual/de
// série falhou na hora do scan (ou a árvore pai ainda não existia naquele
// momento) nunca é retentada pelo scan em si -- o pipeline de match roda uma
// ÚNICA vez, por arquivo, no momento do scan. Este passo tenta de novo, em
// lote pequeno throttled por item, reusando Manager.ReconciliarFaixaDraft/
// Manager.ReconciliarSerieDraft (internal/scanner/reconciliar.go, já
// implementados).

// limiteReconciliacaoOrfaosPorExecucao é o teto de itens verificados POR TIPO
// (faixa e série, separadamente) por execução do job -- mesmo raciocínio/
// valor de limiteReprocessamentoArtesPorExecucao/
// limiteEnriquecimentoRetroativoPorExecucao: throttling de custo/rate-limit
// numa biblioteca com backlog grande na primeira vez que este briefing roda.
const limiteReconciliacaoOrfaosPorExecucao = 15

// ReconciliarFilhosOrfaos varre faixas draft órfãs (sem entity_relation como
// filha de nenhum álbum) e séries draft com arquivo, e tenta religá-las
// contra a árvore de identidade já resolvida (ou que veio a se resolver
// depois). Satisfaz libraryhealth.Matcher (chamado pelo job saude_biblioteca,
// depois de DescobrirArteLocalMusica). err é SÓ erro real de banco na
// LISTAGEM; falha num item individual é absorvida (best-effort, loga e
// segue). reconciliados só conta SUCESSO (linked==true) -- diferente de
// ReprocessarArtesFaltantes/EnriquecerRetroativo, que contam toda tentativa:
// a mensagem final deste passo precisa dizer quantos itens de fato saíram da
// fila de revisão, não quantos foram só verificados.
func (s *Server) ReconciliarFilhosOrfaos(ctx context.Context) (reconciliados int, err error) {
	// Sem scanner, ReconciliarFaixaDraft/ReconciliarSerieDraft nem existiriam
	// para chamar -- mesma guarda de DescobrirArteLocalMusica.
	if s.scanner == nil {
		return 0, nil
	}

	cfgRetry, err := s.obterRetroactiveEnrichmentSettings(ctx)
	if err != nil {
		return 0, err
	}
	langCfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return 0, err
	}

	faixas, err := s.leitura().ListarFaixasDraftOrfas(ctx, sqlcgen.ListarFaixasDraftOrfasParams{
		DiasGlobal: int64(cfgRetry.RetryDays),
		Limite:     limiteReconciliacaoOrfaosPorExecucao,
	})
	if err != nil {
		return 0, err
	}
	for _, row := range faixas {
		if s.reconciliarECarimbar(ctx, row.ID, langCfg.Allowed, "faixa", s.scanner.ReconciliarFaixaDraft) {
			reconciliados++
		}
	}

	series, err := s.leitura().ListarSeriesDraftComArquivo(ctx, sqlcgen.ListarSeriesDraftComArquivoParams{
		DiasGlobal: int64(cfgRetry.RetryDays),
		Limite:     limiteReconciliacaoOrfaosPorExecucao,
	})
	if err != nil {
		return reconciliados, err
	}
	for _, row := range series {
		if s.reconciliarECarimbar(ctx, row.ID, langCfg.Allowed, "série", s.scanner.ReconciliarSerieDraft) {
			reconciliados++
		}
	}

	return reconciliados, nil
}

// reconciliarECarimbar chama `tentar` (Manager.ReconciliarFaixaDraft ou
// Manager.ReconciliarSerieDraft) para UM item, loga falha best-effort e
// carimba a tentativa SEMPRE (mesmo padrão de ReprocessarArtesFaltantes/
// EnriquecerRetroativo: o item foi verificado nesta execução, mesmo sem ter
// religado -- senão o próximo lote tentaria de novo no dia seguinte, sem
// respeitar o throttle). `rotulo` é só para a mensagem de log ("faixa"/
// "série").
func (s *Server) reconciliarECarimbar(ctx context.Context, id string, langs []string, rotulo string, tentar func(context.Context, string, []string) (bool, error)) bool {
	linked, err := tentar(ctx, id, langs)
	if err != nil {
		log.Printf("saude_biblioteca: reconciliação de %s órfã falhou para %s (best-effort, ignorado): %v", rotulo, id, err)
	}
	if werr := s.carimbarTentativaExterna(ctx, id); werr != nil {
		log.Printf("saude_biblioteca: gravar tentativa de reconciliação falhou para %s: %v", id, werr)
	}
	return linked
}

// forcarReconciliacaoResultDTO é a resposta de POST
// /entities/{id}/force-reconcile (briefing 0122, openapi.yaml
// ForceReconcileResult) -- único campo, `linked`, espelhando o retorno de
// ReconciliarFaixaDraft/ReconciliarSerieDraft.
type forcarReconciliacaoResultDTO struct {
	Linked bool `json:"linked"`
}

// forcarReconciliacao trata POST /api/v1/entities/{id}/force-reconcile
// (briefing 0122): dispara a MESMA reconciliação pai-filho do job em lote
// (ReconciliarFilhosOrfaos, 0120) para UMA entity, mas IGNORANDO
// completamente o throttle de last_external_match_attempt_at -- válvula de
// escape manual para quando o dono quer feedback imediato (ex.: acabou de
// corrigir uma tag do arquivo, ou sabe que uma correção no backend acabou de
// ser publicada) sem esperar o próximo ciclo do saude_biblioteca. Dispatch
// por ent.Type: "faixa" -> ReconciliarFaixaDraft, "tv" ->
// ReconciliarSerieDraft; qualquer outro tipo é no-op (200, linked=false, SEM
// carimbar -- não houve tentativa real de nada, mesmo critério de "tipo não
// aplicável" do §4 do briefing). Nos dois casos aplicáveis, carimba a
// tentativa SEMPRE ao final (mesmo em erro de tentar), mesma semântica de
// enrichMissing (retroativo.go): coordena com o PRÓXIMO lote do job para ele
// não competir pelo item que o dono acabou de resolver manualmente. 404
// entity inexistente; 500 só em erro real de banco/infra (na reconciliação
// OU no carimbo) -- nunca por "não achou candidato" (linked=false é
// resultado válido, não erro).
func (s *Server) forcarReconciliacao(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	// Sem scanner configurado, ReconciliarFaixaDraft/ReconciliarSerieDraft nem
	// existiriam para chamar -- mesma guarda de ReconciliarFilhosOrfaos:
	// degrada (200, linked=false) em vez de 503/500.
	if s.scanner == nil {
		writeJSON(w, http.StatusOK, forcarReconciliacaoResultDTO{Linked: false})
		return
	}

	langCfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}

	var tentar func(context.Context, string, []string) (bool, error)
	switch ent.Type {
	case tipoEntFaixa:
		tentar = s.scanner.ReconciliarFaixaDraft
	case tipoEntSerie:
		tentar = s.scanner.ReconciliarSerieDraft
	default:
		// Tipo não aplicável (ex.: movie): no-op, sem carimbar -- não houve
		// tentativa real de reconciliação pai-filho nenhuma.
		writeJSON(w, http.StatusOK, forcarReconciliacaoResultDTO{Linked: false})
		return
	}

	linked, tentarErr := tentar(ctx, ent.ID, langCfg.Allowed)
	// Carimba SEMPRE, mesmo se tentarErr != nil -- o item foi de fato
	// verificado nesta chamada (mesma semântica de enrichMissing/
	// EnriquecerRetroativo). Diferente do job em lote (best-effort, loga e
	// segue), aqui um erro real de banco em QUALQUER um dos dois passos vira
	// 500 -- é uma chamada HTTP individual, não um lote.
	carimbarErr := s.carimbarTentativaExterna(ctx, ent.ID)
	if tentarErr != nil || carimbarErr != nil {
		respondErroInterno(w)
		return
	}

	writeJSON(w, http.StatusOK, forcarReconciliacaoResultDTO{Linked: linked})
}
