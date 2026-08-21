package server

import (
	"context"
	"log"

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
