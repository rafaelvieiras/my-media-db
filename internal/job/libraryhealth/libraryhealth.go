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

// Handler e o Handler do job saude_biblioteca.
type Handler struct {
	library  LibraryLister
	scanner  ScanRunner
	contador Contador
}

// New cria o Handler a partir do servico de datasources, do gerenciador de
// scan e do contador das filas de curadoria -- injetados pelo chamador
// (server.WithLibraryHealthJob), mesmo padrao de enrich.Servico/
// scanner.Manager serem construidos com suas dependencias na hora do
// registro.
func New(lib LibraryLister, scn ScanRunner, cont Contador) *Handler {
	return &Handler{library: lib, scanner: scn, contador: cont}
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

	return montarMensagem(len(sources), escaneadas, falhas, revisao, orfas), nil
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
// atuais das filas de curadoria.
func montarMensagem(total, escaneadas int, falhas []string, revisao, orfas int) string {
	return fmt.Sprintf("%s; %d em revisão, %d órfã(s)",
		resumoScans(total, escaneadas, falhas), revisao, orfas)
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
