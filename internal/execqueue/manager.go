// Package execqueue é a fila de execução UNIFICADA para scan e jobs
// (briefing 0108, ADR-0028): substitui os dois sistemas paralelos que
// reimplementavam o mesmo padrão ("goroutine solta + mapa em memória +
// mutex") de forma independente e redundante. Uma única tabela persistida
// (`queue_item`) e um único worker global SEQUENCIAL (concorrência 1,
// decisão deliberada da ADR-0028 — casa com "local-first e leve",
// AGENTS.md item 4, e com o fato de o SQLite já ter um único escritor):
// no máximo um item `running` por vez, processado em FIFO por
// `enqueued_at`, nunca ocioso enquanto há item `queued`.
//
// Enfileirar (Enqueue) não executa — só grava a linha `queued` e devolve.
// Quem sabe RODAR um `kind` de item é um Runner, registrado via
// RegistrarRunner ANTES de Run começar (a fundação desta camada não
// implementa nenhum Runner de verdade — isso é responsabilidade de uma
// camada seguinte, que liga `internal/scanner` e `internal/job` aqui).
package execqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Kinds conhecidos de queue_item.kind — os únicos valores aceitos pelo CHECK
// da migration 0024. 'scan' referencia um library_source.id; 'job' referencia
// um job_definition.id.
const (
	KindScan = "scan"
	KindJob  = "job"
)

// Triggers conhecidos de queue_item.trigger — os únicos valores aceitos pelo
// CHECK da migration 0024. TriggerWatch é exclusivo de kind=scan (o watcher
// de filesystem nunca dispara job).
const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
	TriggerWatch    = "watch"
)

// Status possíveis de queue_item.status — ciclo de vida linear queued ->
// running -> success|error. Nunca volta para queued (reenfileirar é sempre
// um item NOVO).
const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusError   = "error"
)

// pollInterval é o intervalo que o worker dorme quando a fila está vazia
// (ProximoQueueItemPendente devolveu sql.ErrNoRows) antes de tentar de novo.
// Curto o bastante para não deixar a fila ociosa por muito tempo quando
// chega trabalho novo, sem ser polling agressivo. `var` (não `const`) para o
// teste poder encurtar sem esperar o valor de produção — mesmo padrão de
// internal/job.pollInterval; nenhum código fora de teste deve reatribuí-la.
var pollInterval = 500 * time.Millisecond

// mensagemProcessoReiniciado é a mensagem gravada em queue_item.message
// quando o boot do worker encontra um item 'running' órfão (o processo caiu
// no meio de uma execução anterior — ver recuperarOrfaos).
const mensagemProcessoReiniciado = "processo reiniciado durante a execução"

// Erros de orquestração; sentinelas para uma camada futura mapear ao
// envelope de erro HTTP padrão (mesmo espírito de
// scanner.ErrScanEmAndamento / job.ErrJobEmExecucao).
var (
	// ErrKindInvalido: kind fora de {scan, job}.
	ErrKindInvalido = errors.New("execqueue: kind inválido")
	// ErrTriggerInvalido: trigger fora de {manual, schedule, watch}.
	ErrTriggerInvalido = errors.New("execqueue: trigger inválido")
	// ErrRefIDVazio: refID vazio (Enqueue precisa saber A QUAL referência o
	// item pertence).
	ErrRefIDVazio = errors.New("execqueue: refID vazio")
	// ErrJaEnfileirado: já existe um item queued/running para a MESMA
	// referência (kind, ref_id) — dedup por referência, não por fila
	// inteira (ADR-0028). -> 409 numa camada futura.
	ErrJaEnfileirado = errors.New("execqueue: já existe um item enfileirado (ou em execução) para esta referência")
)

// ProgressoFunc é o callback que um Runner chama durante Executar para
// reportar progresso intermediário — current/total (granularidade decidida
// pelo próprio Runner: por arquivo processado no caso do scan) e uma
// mensagem curta opcional. Gravado a cada chamada (sem throttle — ver a nota
// na query AtualizarProgressoQueueItem).
type ProgressoFunc func(current, total int, message string)

// Runner é o contrato que QUALQUER kind de item implementa para ser
// executado pelo worker — quem registra um Runner para "scan" e "job" é uma
// camada SEGUINTE (esta fundação só define a interface e o registro).
// Executar deve ser bloqueante (o worker é sequencial por design: só chama o
// próximo item depois que este retornar) e deve respeitar ctx.Done() para
// permitir cancelamento no shutdown do processo.
type Runner interface {
	Executar(ctx context.Context, refID string, trigger string, progresso ProgressoFunc) (mensagemFinal string, err error)
}

// Manager guarda os Runners registrados e orquestra o worker sequencial.
// Mesmo padrão de acesso a banco de internal/job.Manager/internal/scanner.Manager:
// leitura pelo pool de leitura (db.Read), escrita sempre pelo pool de
// escrita — o escritor único da ADR-0010.
type Manager struct {
	db     *db.DB
	novoID func() string
	agora  func() time.Time

	// enqueueMu serializa o guard-then-insert de Enqueue DENTRO do processo:
	// a checagem de dedup (ObterQueueItemAtivo) e o INSERT não formam uma
	// transação de banco, então sem este mutex duas chamadas concorrentes a
	// Enqueue para a MESMA referência poderiam intercalar entre a checagem e
	// o insert de cada uma e criar dois itens ativos. Único escritor do
	// processo (ADR-0010) + este mutex tornam o guard efetivamente atômico.
	enqueueMu sync.Mutex

	runnersMu sync.Mutex
	runners   map[string]Runner
}

// NewManager cria um Manager pronto para uso; nenhum Runner registrado ainda
// (RegistrarRunner antes de chamar Run).
func NewManager(database *db.DB) *Manager {
	return &Manager{
		db:      database,
		novoID:  func() string { return uuid.NewString() },
		agora:   func() time.Time { return time.Now().UTC() },
		runners: make(map[string]Runner),
	}
}

// RegistrarRunner associa um Runner a um kind ("scan" ou "job", tipicamente
// as constantes KindScan/KindJob). Chamar de novo com o mesmo kind substitui
// o Runner anterior (comportamento previsível, mesmo espírito de
// job.Registry.Registrar). Deve ser chamado ANTES de Run — o worker resolve
// o Runner de um item no momento em que o processa, então registrar depois
// do boot só vale para itens ainda não processados.
func (m *Manager) RegistrarRunner(kind string, r Runner) {
	m.runnersMu.Lock()
	defer m.runnersMu.Unlock()
	if m.runners == nil {
		m.runners = make(map[string]Runner)
	}
	m.runners[kind] = r
}

func (m *Manager) runner(kind string) (Runner, bool) {
	m.runnersMu.Lock()
	defer m.runnersMu.Unlock()
	r, ok := m.runners[kind]
	return r, ok
}

// ── Enfileiramento ───────────────────────────────────────────────────────────

// Enqueue grava um item novo em status 'queued' e devolve o id. Não executa
// nada — quem consome a fila é o worker (Run), rodando em background.
// ErrKindInvalido/ErrTriggerInvalido/ErrRefIDVazio em input inválido;
// ErrJaEnfileirado se já existe um item queued/running para a MESMA
// referência (kind, ref_id) — dedup por referência, não por fila inteira.
//
// Quem chama Enqueue ainda PRECISA ter registrado (via RegistrarRunner) um
// Runner para o `kind` antes do worker chegar a processar este item — sem
// isso, o item é finalizado como 'error' automaticamente (ver
// processarProximo), o worker não trava.
func (m *Manager) Enqueue(ctx context.Context, kind, refID, trigger string) (string, error) {
	if err := validarKind(kind); err != nil {
		return "", err
	}
	if err := validarTrigger(trigger); err != nil {
		return "", err
	}
	if refID == "" {
		return "", ErrRefIDVazio
	}

	m.enqueueMu.Lock()
	defer m.enqueueMu.Unlock()

	_, err := m.leitura().ObterQueueItemAtivo(ctx, sqlcgen.ObterQueueItemAtivoParams{Kind: kind, RefID: refID})
	if err == nil {
		return "", ErrJaEnfileirado
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("execqueue: checar dedup: %w", err)
	}

	id := m.novoID()
	if _, err := m.escrita().CriarQueueItem(ctx, sqlcgen.CriarQueueItemParams{
		ID: id, Kind: kind, RefID: refID, Trigger: trigger,
	}); err != nil {
		return "", fmt.Errorf("execqueue: criar item: %w", err)
	}
	return id, nil
}

func validarKind(kind string) error {
	switch kind {
	case KindScan, KindJob:
		return nil
	default:
		return ErrKindInvalido
	}
}

func validarTrigger(trigger string) error {
	switch trigger {
	case TriggerManual, TriggerSchedule, TriggerWatch:
		return nil
	default:
		return ErrTriggerInvalido
	}
}

// ── Consulta ─────────────────────────────────────────────────────────────────

// QueueItemStatus é a fotografia consultável do estado mais recente de UMA
// referência (kind, ref_id) — o item ATIVO (queued/running) se houver, senão
// o último finalizado (success/error). Pensada para alimentar tanto
// GET /library/{id}/scan quanto GET /jobs/{id}/executions sem quebrar o
// contrato JSON atual desses endpoints (camada futura). Os campos de
// timestamp saem como o texto já gravado no banco (mesmo formato ISO que o
// resto do server já repassa direto pra JSON, sem re-parse) — vazio quando
// NULL (ex.: StartedAt vazio enquanto o item ainda está 'queued').
type QueueItemStatus struct {
	// Existe é falso quando nunca houve item de fila para esta referência.
	Existe bool

	ID      string
	Kind    string
	RefID   string
	Trigger string
	// Status: queued | running | success | error.
	Status          string
	ProgressCurrent int
	ProgressTotal   int
	Message         string
	EnqueuedAt      string
	StartedAt       string
	FinishedAt      string
}

// Status devolve o estado mais recente da referência (kind, ref_id): o item
// ATIVO (queued/running) se houver, senão o ÚLTIMO item finalizado —
// QueueItemStatus{Existe: false} quando nunca houve item nenhum. kind
// inválido devolve ErrKindInvalido.
func (m *Manager) Status(ctx context.Context, kind, refID string) (*QueueItemStatus, error) {
	if err := validarKind(kind); err != nil {
		return nil, err
	}

	item, err := m.leitura().ObterQueueItemAtivoPorKindRef(ctx, sqlcgen.ObterQueueItemAtivoPorKindRefParams{
		Kind: kind, RefID: refID,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("execqueue: obter item ativo: %w", err)
		}
		item, err = m.leitura().ObterUltimoQueueItemPorKindRef(ctx, sqlcgen.ObterUltimoQueueItemPorKindRefParams{
			Kind: kind, RefID: refID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &QueueItemStatus{Existe: false}, nil
			}
			return nil, fmt.Errorf("execqueue: obter ultimo item: %w", err)
		}
	}
	return queueItemToStatus(item), nil
}

func queueItemToStatus(item sqlcgen.QueueItem) *QueueItemStatus {
	return &QueueItemStatus{
		Existe:          true,
		ID:              item.ID,
		Kind:            item.Kind,
		RefID:           item.RefID,
		Trigger:         item.Trigger,
		Status:          item.Status,
		ProgressCurrent: int(item.ProgressCurrent),
		ProgressTotal:   int(item.ProgressTotal),
		Message:         nullString(item.Message),
		EnqueuedAt:      item.EnqueuedAt,
		StartedAt:       nullString(item.StartedAt),
		FinishedAt:      nullString(item.FinishedAt),
	}
}

func nullString(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// ── Worker ───────────────────────────────────────────────────────────────────

// Run é o loop do worker sequencial: enquanto ctx não é cancelado, busca o
// próximo item 'queued' mais antigo e processa (bloqueante — concorrência 1
// por design, ADR-0028); quando a fila está vazia, dorme pollInterval antes
// de tentar de novo. Antes de entrar no loop, recupera qualquer item
// 'running' órfão (processo caiu no meio de uma execução anterior).
//
// Chamar Run é responsabilidade do boot do processo (cmd/mymediadb/main.go
// numa camada futura, via `go execQueue.Run(ctx)`) — bloqueia até ctx ser
// cancelado, então deve rodar em goroutine própria.
func (m *Manager) Run(ctx context.Context) {
	m.recuperarOrfaos(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		processou, err := m.processarProximo(ctx)
		if err != nil {
			log.Printf("execqueue: erro ao processar a fila: %v", err)
		}
		if processou {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// recuperarOrfaos fecha, no BOOT do worker, qualquer item que já esteja
// 'running' no banco: sinal de que o processo caiu no meio de uma execução
// anterior (o normal seria nunca haver 'running' antes do worker começar a
// processar, já que só este worker transiciona queued->running). Best-effort
// — erro de um item não impede a recuperação dos demais.
func (m *Manager) recuperarOrfaos(ctx context.Context) {
	itens, err := m.leitura().ListarQueueItemsRunning(ctx)
	if err != nil {
		log.Printf("execqueue: erro ao listar itens orfaos no boot: %v", err)
		return
	}
	for _, item := range itens {
		m.finalizar(ctx, item.ID, StatusError, mensagemProcessoReiniciado)
	}
}

// processarProximo busca o próximo item 'queued' e o processa até o fim
// (bloqueante). devolve processou=true se havia um item (processado com
// sucesso, com erro do Runner, ou finalizado imediatamente por falta de
// Runner) — o chamador (Run) só dorme quando processou=false (fila vazia).
func (m *Manager) processarProximo(ctx context.Context) (processou bool, err error) {
	pendente, err := m.leitura().ProximoQueueItemPendente(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("buscar proximo item: %w", err)
	}

	item, err := m.escrita().IniciarQueueItem(ctx, pendente.ID)
	if err != nil {
		return false, fmt.Errorf("iniciar item %q: %w", pendente.ID, err)
	}

	runner, ok := m.runner(item.Kind)
	if !ok {
		m.finalizar(ctx, item.ID, StatusError,
			fmt.Sprintf("nenhum runner registrado para o kind %q", item.Kind))
		return true, nil
	}

	progresso := func(current, total int, message string) {
		if perr := m.escrita().AtualizarProgressoQueueItem(ctx, sqlcgen.AtualizarProgressoQueueItemParams{
			ProgressCurrent: int64(current),
			ProgressTotal:   int64(total),
			Message:         sql.NullString{String: message, Valid: message != ""},
			ID:              item.ID,
		}); perr != nil {
			log.Printf("execqueue: erro ao gravar progresso de %q: %v", item.ID, perr)
		}
	}

	mensagem, execErr := m.executarComRecover(ctx, runner, item, progresso)
	if execErr != nil {
		m.finalizar(ctx, item.ID, StatusError, execErr.Error())
	} else {
		m.finalizar(ctx, item.ID, StatusSuccess, mensagem)
	}
	return true, nil
}

// executarComRecover chama Runner.Executar recuperando de pânico — nunca
// deixa um item 'running' preso para sempre por causa de um Runner que
// entrou em pânico (mesmo padrão de scanner.Manager.run / job.Manager.executar).
func (m *Manager) executarComRecover(ctx context.Context, runner Runner, item sqlcgen.QueueItem, progresso ProgressoFunc) (mensagem string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("pânico durante a execução: %v", rec)
		}
	}()
	return runner.Executar(ctx, item.RefID, item.Trigger, progresso)
}

// finalizar transiciona um item para success/error — best-effort (erro só
// vai pro log; não há mais nada sensato a fazer aqui, o item fica 'running'
// preso só em caso de falha de banco, que já teria quebrado a execução
// inteira antes disso).
func (m *Manager) finalizar(ctx context.Context, id, status, message string) {
	if _, err := m.escrita().FinalizarQueueItem(ctx, sqlcgen.FinalizarQueueItemParams{
		Status:  status,
		Message: sql.NullString{String: message, Valid: message != ""},
		ID:      id,
	}); err != nil {
		log.Printf("execqueue: erro ao finalizar item %q: %v", id, err)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (m *Manager) leitura() *sqlcgen.Queries { return sqlcgen.New(m.db.Read) }
func (m *Manager) escrita() *sqlcgen.Queries { return sqlcgen.New(m.db.Write) }
