// Package libraryhealth implementa o Handler do job `saude_biblioteca`
// (briefing 0053, follow-up explicito do 0052): a fila de revisao (/review) e
// a de orfas (/orphans) sao QUERY AO VIVO sobre entity/media_file (briefing
// 0018) -- so refletem a realidade do disco depois que ALGUEM roda um scan
// manual daquela library_source. Se um arquivo some do disco sem ninguem
// re-escanear, a entity continua 'present' para sempre. Este job fecha essa
// lacuna: dispara um re-scan de TODA fonte cadastrada, espera cada um
// terminar e resume o estado das filas de curadoria ao final -- prova de
// extensibilidade do sistema de jobs (0052 §5): so um novo Handler + uma
// linha de seed, sem tocar o schema/endpoints de job.
package libraryhealth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// nomeJob e o Handler.Nome() -- casa com o slug seedado em job_definition
// pela migration 0012.
const nomeJob = "saude_biblioteca"

// pollInterval e o intervalo entre checagens de scanner.Status enquanto o
// Handler espera um scan terminar. var (nao const) para o teste poder
// encurtar sem esperar de verdade -- mesmo padrao de job.pollInterval.
var pollInterval = 200 * time.Millisecond

// LibraryLister e o subconjunto de library.Service que o Handler consome:
// listar as fontes cadastradas. Interface para injetar um fake em teste sem
// depender de filesystem real.
type LibraryLister interface {
	Listar(ctx context.Context) ([]library.Source, error)
}

// ScanRunner e o subconjunto de scanner.Manager que o Handler consome:
// disparar um scan e acompanhar o progresso ate terminar. Interface para
// injetar um fake em teste -- scanner.Manager de verdade satisfaz isto
// estruturalmente, sem precisar declarar a interface la (evita import
// reverso).
type ScanRunner interface {
	Start(source library.Source) (string, error)
	Status(sourceID string) scanner.Status
}

// Contador e o subconjunto de curation.Servico que o Handler consome: as
// contagens das filas de curadoria (briefing 0018), reusando as MESMAS
// queries sqlc do GET /review e GET /orphans -- este pacote nao reimplementa
// nenhum predicado SQL.
type Contador interface {
	ContarRevisao(ctx context.Context) (int, error)
	ContarOrfas(ctx context.Context) (int, error)
}

// Matcher e o subconjunto de *server.Server que o Handler consome para rodar
// o auto-match em lote sobre os itens elegiveis da fila de Revisao (briefing
// 0054, follow-up do 0050/0053): resolve os de alta confianca sozinho, sem
// precisar de clique manual. Satisfeita estruturalmente por *server.Server
// (mesmo padrao de LibraryLister/ScanRunner/Contador) -- este pacote nunca
// importa internal/server (evita ciclo, ja que server.Handler e construido
// DE DENTRO do server, ver server.WithLibraryHealthJob).
type Matcher interface {
	AutoMatchElegiveis(ctx context.Context) (resolved, pending int, err error)
	// EnriquecerRetroativo varre o complemento de ListarRevisao restrito a
	// itens locais puros (sem external_ids, nunca verificados contra
	// provider nenhum) e tenta enriquecer um lote pequeno, throttled por
	// item (briefing 0056, follow-up do 0053/0054): aplica sozinho com alta
	// confiança, ou sinaliza para a fila de Revisão quando achou algo
	// plausível mas não confiável o bastante.
	EnriquecerRetroativo(ctx context.Context) (aplicados, sinalizados int, err error)
}

// CompletenessRecalculator e o subconjunto de *server.Server que o Handler
// consome para recalcular o CACHE de completude de catalogacao (briefing
// 0058, follow-up do 0053/0057): reavalia TODA entity nao-draft contra a
// regua configurada em /settings e grava entity_catalog_completeness, que a
// listagem/`/members` leem (a listagem NAO calcula ao vivo -- ver a nota
// longa em internal/server/completeness.go/completeness_recalc.go sobre por
// que). Interface pequena, mesmo padrao de Matcher/Contador: satisfeita
// estruturalmente por *server.Server, sem este pacote importar
// internal/server (evita ciclo, ja que server.Handler e construido DE DENTRO
// do server, ver server.WithLibraryHealthJob).
type CompletenessRecalculator interface {
	RecalcularCompletudeCatalogacao(ctx context.Context) (avaliados, incompletos int, err error)
}

// Handler e o Handler do job saude_biblioteca.
type Handler struct {
	library      LibraryLister
	scanner      ScanRunner
	contador     Contador
	matcher      Matcher
	completeness CompletenessRecalculator
}

// New cria o Handler a partir do servico de datasources, do gerenciador de
// scan, do contador das filas de curadoria, do Matcher de auto-match e do
// CompletenessRecalculator (briefing 0058) -- injetados pelo chamador
// (server.WithLibraryHealthJob), mesmo padrao de enrich.Servico/
// scanner.Manager serem construidos com suas dependencias na hora do
// registro. `completeness` e OPCIONAL: nil degrada para "nao recalcula"
// (mesmo padrao de matcher == nil em rodarAutoMatch/
// rodarEnriquecimentoRetroativo), util para testes que nao precisam
// exercitar este passo.
func New(lib LibraryLister, scn ScanRunner, cont Contador, matcher Matcher, completeness CompletenessRecalculator) *Handler {
	return &Handler{library: lib, scanner: scn, contador: cont, matcher: matcher, completeness: completeness}
}

// Nome devolve o slug estavel do job.
func (h *Handler) Nome() string { return nomeJob }

// Executar re-escaneia TODA library_source cadastrada (sequencialmente,
// aguardando cada scan terminar antes de seguir para o proximo -- nao ha
// config de paralelismo, ver notas do briefing 0053) e resume o resultado:
// quantas fontes escaneadas com sucesso, quantas falharam, quantos itens em
// revisao e quantas orfas ao final. cfg nao e usado (sem parametros hoje).
//
// Erro de UMA fonte nao aborta as outras (mesmo espirito best-effort do
// RunScheduler, briefing 0052); erro e devolvido apenas se TODAS as fontes
// falharem.
func (h *Handler) Executar(ctx context.Context, _ json.RawMessage) (string, error) {
	sources, err := h.library.Listar(ctx)
	if err != nil {
		return "", fmt.Errorf("listar fontes da biblioteca: %w", err)
	}

	escaneadas, falhas := h.escanearTodas(ctx, sources)
	if len(sources) > 0 && escaneadas == 0 {
		return "", fmt.Errorf("todas as %d fonte(s) falharam ao escanear: %s",
			len(sources), strings.Join(falhas, "; "))
	}

	revisao, err := h.contador.ContarRevisao(ctx)
	if err != nil {
		return montarMensagemSemContagem(len(sources), escaneadas, falhas, err), nil
	}
	orfas, err := h.contador.ContarOrfas(ctx)
	if err != nil {
		return montarMensagemSemContagem(len(sources), escaneadas, falhas, err), nil
	}

	resolvidos := h.rodarAutoMatch(ctx)
	if resolvidos > 0 {
		// Recontagem: o auto-match pode ter tirado itens da fila -- reflete o
		// estado FINAL, nao o de antes de rodar. Erro aqui degrada para a
		// contagem ja obtida acima (best-effort, nao esconde o resto do resumo).
		if recontado, err := h.contador.ContarRevisao(ctx); err == nil {
			revisao = recontado
		}
	}

	aplicados, sinalizados := h.rodarEnriquecimentoRetroativo(ctx)
	if sinalizados > 0 {
		// Recontagem de novo (briefing 0056): itens sinalizados por este
		// passo passam a aparecer na fila de Revisao -- o resumo final
		// precisa refletir o estado depois DESTE passo tambem, nao so do
		// auto-match acima.
		if recontado, err := h.contador.ContarRevisao(ctx); err == nil {
			revisao = recontado
		}
	}

	avaliados, incompletos := h.rodarRecalculoCompletude(ctx)

	return montarMensagem(len(sources), escaneadas, falhas, revisao, orfas, resolvidos, aplicados, sinalizados, avaliados, incompletos), nil
}

// rodarAutoMatch chama o Matcher (auto-match em lote sobre os elegiveis da
// fila de Revisao, briefing 0054) e devolve quantos itens foram resolvidos
// automaticamente. Best-effort: uma falha aqui NAO aborta o job -- so loga e
// devolve 0, para o resumo original de scan/contagem ainda sair.
func (h *Handler) rodarAutoMatch(ctx context.Context) int {
	if h.matcher == nil {
		return 0
	}
	resolved, _, err := h.matcher.AutoMatchElegiveis(ctx)
	if err != nil {
		log.Printf("saude_biblioteca: auto-match em lote falhou (best-effort, ignorado): %v", err)
		return 0
	}
	return resolved
}

// rodarEnriquecimentoRetroativo chama o Matcher (enriquecimento externo
// retroativo, briefing 0056) e devolve quantos itens foram aplicados
// sozinhos e quantos foram sinalizados para a fila de Revisao. Best-effort:
// uma falha aqui NAO aborta o job -- so loga e devolve (0, 0), para o resumo
// original ainda sair. Chamado DEPOIS de rodarAutoMatch (mesma ordem do
// briefing: re-scan -> auto-match em lote -> enriquecimento retroativo).
func (h *Handler) rodarEnriquecimentoRetroativo(ctx context.Context) (aplicados, sinalizados int) {
	if h.matcher == nil {
		return 0, 0
	}
	aplicados, sinalizados, err := h.matcher.EnriquecerRetroativo(ctx)
	if err != nil {
		log.Printf("saude_biblioteca: enriquecimento retroativo em lote falhou (best-effort, ignorado): %v", err)
		return 0, 0
	}
	return aplicados, sinalizados
}

// rodarRecalculoCompletude chama o CompletenessRecalculator (recalculo em
// lote do cache de completude de catalogacao, briefing 0058) e devolve
// quantos itens foram avaliados e quantos ficaram incompletos. Best-effort:
// uma falha aqui NAO aborta o job -- so loga e devolve (0, 0), para o resumo
// original ainda sair. Chamado por ULTIMO (depois do re-scan, auto-match e
// enriquecimento retroativo): a completude reflete o estado FINAL da
// execucao, apos os passos anteriores poderem ter mudado dados resolvidos.
func (h *Handler) rodarRecalculoCompletude(ctx context.Context) (avaliados, incompletos int) {
	if h.completeness == nil {
		return 0, 0
	}
	avaliados, incompletos, err := h.completeness.RecalcularCompletudeCatalogacao(ctx)
	if err != nil {
		log.Printf("saude_biblioteca: recalculo de completude de catalogacao falhou (best-effort, ignorado): %v", err)
		return 0, 0
	}
	return avaliados, incompletos
}

// escanearTodas dispara e aguarda o scan de cada fonte, sequencialmente.
// Devolve quantas terminaram com sucesso e as mensagens de falha das demais
// (fonte + motivo). Best-effort: uma falha nao interrompe as seguintes.
func (h *Handler) escanearTodas(ctx context.Context, sources []library.Source) (int, []string) {
	escaneadas := 0
	var falhas []string
	for _, src := range sources {
		if err := h.escanearEAguardar(ctx, src); err != nil {
			falhas = append(falhas, fmt.Sprintf("%s: %v", src.Path, err))
			continue
		}
		escaneadas++
	}
	return escaneadas, falhas
}

// escanearEAguardar dispara o scan da fonte e bloqueia ate ele terminar
// (sucesso ou erro), consultando scanner.Status em intervalos curtos.
func (h *Handler) escanearEAguardar(ctx context.Context, src library.Source) error {
	if _, err := h.scanner.Start(src); err != nil {
		return err
	}
	return h.aguardarConclusao(ctx, src.ID)
}

// aguardarConclusao faz polling de scanner.Status ate Running=false --
// Start() sempre cria um job NOVO (com lastError zerado) no map do
// scanner.Manager antes de devolver, entao o LastError lido aqui pertence
// SEMPRE a esta execucao (nunca a uma anterior). Sai antes do tempo se ctx
// for cancelado (nao ha timeout proprio -- ver nota do briefing 0053 sobre
// esta limitacao, compartilhada com todo o sistema de jobs: Manager.executar
// roda o Handler em context.Background(), sem prazo).
func (h *Handler) aguardarConclusao(ctx context.Context, sourceID string) error {
	for {
		status := h.scanner.Status(sourceID)
		if !status.Existe || !status.Running {
			if status.LastError != "" {
				return errors.New(status.LastError)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// montarMensagem compoe o resumo final de sucesso: quantas fontes
// escaneadas (com o detalhe das falhas parciais, se houver) + as contagens
// atuais das filas de curadoria. resolvidos (briefing 0054) e o resultado do
// auto-match em lote; 0 omite o parenteses para nao poluir a mensagem em
// operacao normal, quando nao havia nada a resolver. aplicados/sinalizados
// (briefing 0056) sao o resultado do enriquecimento retroativo; a clausula
// extra e omitida quando os dois sao zero (mesmo espirito da omissao de
// resolvidos==0 acima). avaliados/incompletos (briefing 0058) sao o
// resultado do recalculo de completude de catalogacao; a clausula extra e
// omitida quando nao houve NENHUM item avaliado (h.completeness == nil ou a
// biblioteca vazia) -- com pelo menos um avaliado, a clausula aparece mesmo
// com incompletos==0 (0 incompletos e uma informacao positiva relevante,
// diferente de aplicados/sinalizados==0 que so significa "nada a fazer").
func montarMensagem(total, escaneadas int, falhas []string, revisao, orfas, resolvidos, aplicados, sinalizados, avaliados, incompletos int) string {
	base := fmt.Sprintf("%s; %d em revisão, %d órfã(s)", resumoScans(total, escaneadas, falhas), revisao, orfas)
	if resolvidos > 0 {
		base = fmt.Sprintf("%s; %d em revisão (%d resolvidos automaticamente), %d órfã(s)",
			resumoScans(total, escaneadas, falhas), revisao, resolvidos, orfas)
	}
	if aplicados > 0 || sinalizados > 0 {
		base = fmt.Sprintf("%s; enriquecimento retroativo: %d aplicado(s), %d sinalizado(s) para revisão",
			base, aplicados, sinalizados)
	}
	if avaliados > 0 {
		base = fmt.Sprintf("%s; %d itens avaliados, %d incompletos em catalogação", base, avaliados, incompletos)
	}
	return base
}

// montarMensagemSemContagem e usada quando os scans terminaram mas a
// contagem das filas falhou -- ainda assim reflete o que rodou de fato (o
// scan em si nao falhou por causa disto).
func montarMensagemSemContagem(total, escaneadas int, falhas []string, contErr error) string {
	return fmt.Sprintf("%s; erro ao contar filas de curadoria: %v", resumoScans(total, escaneadas, falhas), contErr)
}

// resumoScans descreve quantas fontes foram escaneadas com sucesso.
func resumoScans(total, escaneadas int, falhas []string) string {
	switch {
	case total == 0:
		return "nenhuma fonte de biblioteca cadastrada"
	case len(falhas) == 0:
		return fmt.Sprintf("%d fonte(s) escaneada(s) com sucesso", escaneadas)
	default:
		return fmt.Sprintf("%d de %d fonte(s) escaneada(s) com sucesso (%d falharam: %s)",
			escaneadas, total, len(falhas), strings.Join(falhas, "; "))
	}
}
