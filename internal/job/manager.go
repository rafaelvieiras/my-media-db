package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// pollInterval é o intervalo do ticker do agendador embutido (briefing 0052):
// a resolução mínima de cron é 1 minuto, 30s é suficiente pra nunca atrasar
// mais que isso. É `var` (não `const`) para o teste do agendador poder
// encurtar o intervalo sem esperar 30s de verdade — nenhum outro código deve
// reatribuí-la fora de teste.
var pollInterval = 30 * time.Second

// manterExecucoes é o cap de retenção do histórico por job (briefing 0052):
// constante Go, não exposta na API. A cada execução finalizada (item de
// queue_item, kind='job', desde o briefing 0108/ADR-0028), o excedente além
// das `manterExecucoes` mais recentes DAQUELE job é apagado (podarHistorico).
const manterExecucoes = 100

// carimboFormato é o layout ISO usado para next_run_at/last_run_at — mesmo
// formato de scan.go (time.RFC3339 em UTC), fixo-largura e lexicamente
// ordenável (a query do agendador compara next_run_at <= now como TEXTO).
const carimboFormato = time.RFC3339

// Erros de orquestração; sentinelas para o handler HTTP mapear ao envelope
// de erro padrão.
var (
	// ErrJobNaoEncontrado: o id não existe em job_definition. -> 404
	ErrJobNaoEncontrado = errors.New("job não encontrado")
	// ErrCronInvalido: schedule_cron não parseia como cron de 5 campos. -> 422
	ErrCronInvalido = errors.New("schedule_cron inválido")
	// ErrJobEmExecucao: já há uma execução em andamento para este job. -> 409
	ErrJobEmExecucao = errors.New("já existe uma execução em andamento para este job")
	// ErrJobDesconhecido: o job existe no banco (seed) mas não há Handler
	// registrado para ele no binário atual (ex.: downgrade). -> 503
	ErrJobDesconhecido = errors.New("nenhum handler registrado para este job")
)

// Manager orquestra a execução dos jobs: guarda o Registry de Handlers
// (exportado — WithJobHandlers registra direto nele) e roda o agendador
// embutido. Desde o briefing 0108/ADR-0028 (CAMADA 3), Manager NÃO dispara
// mais sua própria goroutine nem guarda estado de execução em memória
// (running/job_execution) — isso vive na fila unificada (internal/execqueue).
// Executar só enfileira (ConfigurarFila); o trabalho de fato roda depois,
// quando o worker sequencial da fila chama de volta o Manager como um
// execqueue.Runner (executarViaRunner) — ver ConfigurarFila abaixo. Mesmo
// espírito de internal/scanner.Manager para o kind "scan".
type Manager struct {
	db       *db.DB
	Registry Registry

	novoID func() string
	agora  func() time.Time

	// fila é a fila de execução unificada (briefing 0108): Executar enfileira
	// nela em vez de disparar uma goroutine própria. nil só em uso
	// incompleto do pacote (todo caminho de produção chama ConfigurarFila no
	// boot) -- Executar devolve ErrFilaNaoConfigurada nesse caso.
	fila *execqueue.Manager
}

// ErrFilaNaoConfigurada: Executar chamado antes de ConfigurarFila -- todo
// caminho de produção configura a fila no boot (cmd/mymediadb/main.go);
// isto só deveria aparecer em teste/uso incompleto do pacote.
var ErrFilaNaoConfigurada = errors.New("job: fila de execução não configurada (ConfigurarFila)")

// NewManager cria um Manager pronto para uso; o Registry começa vazio — os
// handlers entram via Registry.Registrar (server.WithJobHandlers) antes do
// primeiro Executar/RunScheduler.
func NewManager(database *db.DB) *Manager {
	return &Manager{
		db:     database,
		novoID: func() string { return uuid.NewString() },
		agora:  func() time.Time { return time.Now().UTC() },
	}
}

// ConfigurarFila injeta a fila de execução unificada (briefing 0108,
// ADR-0028) e registra o execqueue.Runner do kind "job" -- chamado pelo boot
// do processo (cmd/mymediadb/main.go). Sem isto, Executar devolve
// ErrFilaNaoConfigurada. Mesmo padrão de nome de
// scanner.Manager.ConfigurarFila (consistência de convenção no projeto); o
// Runner registrado é jobRunner{m} (ver o tipo, abaixo) -- não o próprio
// *Manager -- por causa do conflito de nome de método Executar (ver a nota
// em jobRunner).
func (m *Manager) ConfigurarFila(fila *execqueue.Manager) {
	m.fila = fila
	fila.RegistrarRunner(execqueue.KindJob, jobRunner{m})
}

// JobConfigPatch é o patch PARCIAL de PATCH .../jobs/{id}: Enabled nil =
// inalterado; ScheduleCronSet=false = inalterado; ScheduleCronSet=true com
// ScheduleCron nil = LIMPA o agendamento (job vira só-manual); com
// ScheduleCron não-nil = novo valor (validado como cron de 5 campos).
type JobConfigPatch struct {
	Enabled         *bool
	ScheduleCronSet bool
	ScheduleCron    *string
}

// ── Leitura/config ───────────────────────────────────────────────────────────

// Listar devolve todos os job_definition (nunca vazio após a migration 0011 —
// sempre tem o seed backup_vacuum), ordem estável.
func (m *Manager) Listar(ctx context.Context) ([]sqlcgen.JobDefinition, error) {
	return m.leitura().ListarJobDefinitions(ctx)
}

// Obter devolve um job_definition por id. ErrJobNaoEncontrado quando o id não
// existe.
func (m *Manager) Obter(ctx context.Context, id string) (sqlcgen.JobDefinition, error) {
	def, err := m.leitura().ObterJobDefinition(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlcgen.JobDefinition{}, ErrJobNaoEncontrado
		}
		return sqlcgen.JobDefinition{}, err
	}
	return def, nil
}

// AtualizarConfig aplica o patch parcial de enabled/schedule_cron, recalcula
// next_run_at (via ProximaExecucao) e persiste — tudo em UMA linha final (a
// query sqlc sempre grava as 3 colunas; o merge acontece aqui, em Go).
// ErrJobNaoEncontrado se o id não existe; ErrCronInvalido se schedule_cron não
// parseia (o estado anterior NÃO é alterado nesse caso).
func (m *Manager) AtualizarConfig(ctx context.Context, id string, patch JobConfigPatch) (sqlcgen.JobDefinition, error) {
	atual, err := m.Obter(ctx, id)
	if err != nil {
		return sqlcgen.JobDefinition{}, err
	}

	enabled := atual.Enabled
	if patch.Enabled != nil {
		enabled = boolParaInt(*patch.Enabled)
	}

	scheduleCron := atual.ScheduleCron
	if patch.ScheduleCronSet {
		if patch.ScheduleCron == nil {
			scheduleCron = sql.NullString{}
		} else {
			scheduleCron = sql.NullString{String: *patch.ScheduleCron, Valid: true}
		}
	}
	if scheduleCron.Valid {
		if err := ValidarCron(scheduleCron.String); err != nil {
			return sqlcgen.JobDefinition{}, ErrCronInvalido
		}
	}

	nextRunAt := formatarProxima(ProximaExecucao(enabled == 1, scheduleCron.String, m.agora()))

	return m.escrita().AtualizarJobConfig(ctx, sqlcgen.AtualizarJobConfigParams{
		Enabled:      enabled,
		ScheduleCron: scheduleCron,
		NextRunAt:    nextRunAt,
		ID:           id,
	})
}

// Historico devolve a página de queue_item (kind='job') do job `id`, mais
// recente primeiro por enqueued_at. `before` nil = primeira página; não-nil =
// pagina para trás (enqueued_at estritamente menor que o cursor).
// ErrJobNaoEncontrado se o id não existe.
//
// Desde o briefing 0108/ADR-0028 (CAMADA 3): o histórico vive em queue_item,
// não mais em job_execution. A query ListarQueueItemsPorKindRef (camada 1)
// não tem suporte NATIVO a cursor (`WHERE enqueued_at < ?`) como
// ListarJobExecucoes tinha via sqlc.narg('before') -- por isso o filtro do
// cursor é aplicado AQUI, em Go, sobre a listagem completa (LIMIT -1, sem
// teto): o cap de retenção (podarHistorico, manterExecucoes=100) garante que
// isso nunca itera mais que 100 linhas por job, então não é hot-path de
// performance.
func (m *Manager) Historico(ctx context.Context, id string, limit int, before *string) ([]sqlcgen.QueueItem, error) {
	if _, err := m.Obter(ctx, id); err != nil {
		return nil, err
	}
	itens, err := m.leitura().ListarQueueItemsPorKindRef(ctx, sqlcgen.ListarQueueItemsPorKindRefParams{
		Kind:  execqueue.KindJob,
		RefID: id,
		Limit: -1,
	})
	if err != nil {
		return nil, err
	}
	out := make([]sqlcgen.QueueItem, 0, limit)
	for _, item := range itens {
		if before != nil && item.EnqueuedAt >= *before {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Rodando devolve, para cada job_id com um item ATIVO (queued OU running) na
// fila unificada, running=true — usado por GET /jobs para compor o campo
// `running` de cada item. Desde o briefing 0108/ADR-0028 (CAMADA 3), a fonte
// da verdade de "rodando" é o execqueue, não mais um mapa em memória do
// próprio Manager.
//
// Decisão: para CADA job_definition, consulta execqueue.Manager.Status
// (camada 1, já pública) em vez de tentar uma query só de "running" -- a
// lista de jobs nunca é grande (hoje 4 jobs seedados), então N chamadas
// pequenas não compensam o SQL extra de uma query dedicada
// (ListarQueueItemsRunningPorKind). Trata queued+running como "rodando"
// (não SÓ running de fato) -- MESMA leitura de scanner.Manager.Status (ver o
// comentário lá: "Running=true tanto para 'queued' quanto 'running'"),
// necessária para o padrão de polling usado nos testes/handlers
// (aguardarJobEstado/aguardarRodando): um item recém-enfileirado (ainda
// atrás de outro na fila global de concorrência 1) não pode parecer "já
// terminou" só porque o worker ainda não pegou ele.
func (m *Manager) Rodando() map[string]bool {
	out := make(map[string]bool)
	if m.fila == nil {
		return out
	}
	ctx := context.Background()
	defs, err := m.leitura().ListarJobDefinitions(ctx)
	if err != nil {
		log.Printf("job: Rodando: erro ao listar job_definition: %v", err)
		return out
	}
	for _, def := range defs {
		st, err := m.fila.Status(ctx, execqueue.KindJob, def.ID)
		if err != nil {
			log.Printf("job: Rodando: erro ao consultar status de %q: %v", def.ID, err)
			continue
		}
		if st.Existe && (st.Status == execqueue.StatusQueued || st.Status == execqueue.StatusRunning) {
			out[def.ID] = true
		}
	}
	return out
}

// ── Execução ─────────────────────────────────────────────────────────────────

// Executar valida SINCRONAMENTE (job existe? Handler registrado?) e então
// ENFILEIRA a execução do job `jobID` na fila unificada (briefing 0108,
// ADR-0028) — não roda mais nada em goroutine própria (isso passou a ser
// responsabilidade do worker de internal/execqueue, que chama de volta este
// Manager via executarViaRunner, o método que satisfaz execqueue.Runner).
// trigger é "manual" (POST .../run) ou "schedule" (agendador).
//
// ErrJobNaoEncontrado se o id não existe em job_definition; ErrJobDesconhecido
// se não há Handler registrado (job seedado, binário sem o handler);
// ErrJobEmExecucao se já há um item queued/running para este job na fila
// (execqueue.ErrJaEnfileirado mapeado ao erro sentinela público de sempre —
// o handler HTTP, respondJobError, já trata isso como 409, sem mudança);
// ErrFilaNaoConfigurada se ConfigurarFila nunca foi chamado.
func (m *Manager) Executar(ctx context.Context, jobID, trigger string) (string, error) {
	if _, err := m.Obter(ctx, jobID); err != nil {
		return "", err
	}
	if _, ok := m.Registry.Get(jobID); !ok {
		return "", ErrJobDesconhecido
	}
	if m.fila == nil {
		return "", ErrFilaNaoConfigurada
	}

	id, err := m.fila.Enqueue(ctx, execqueue.KindJob, jobID, trigger)
	if err != nil {
		if errors.Is(err, execqueue.ErrJaEnfileirado) {
			return "", ErrJobEmExecucao
		}
		return "", err
	}
	return id, nil
}

// executarViaRunner implementa execqueue.Runner: é chamado pelo worker
// sequencial da fila (bem depois do Enqueue original de Executar), com só o
// refID (job_definition.id) e o trigger em mãos. job.Handler.Executar não
// tem noção de progresso — progresso nunca é reportado aqui (fica 0/0,
// aceitável: job_execution nunca teve progresso antes deste briefing
// também). Pânico dentro do Handler é recuperado pelo PRÓPRIO execqueue
// (executarComRecover) -- não duplicamos recover aqui (mesmo espírito de
// scanner.Manager.Executar).
//
// Antes de devolver ao execqueue, faz o que é ESPECÍFICO DO DOMÍNIO job (não
// é responsabilidade do execqueue genérico): espelha o resultado em
// job_definition (atualizarDefinicaoAoFim, recalcula next_run_at) e poda o
// histórico (podarHistorico) — SEMPRE, tanto em sucesso quanto erro. O
// execqueue já cuida de IniciarQueueItem/FinalizarQueueItem ao redor desta
// chamada; este método só cuida do que é dele.
func (m *Manager) executarViaRunner(ctx context.Context, refID, _ string, _ execqueue.ProgressoFunc) (string, error) {
	def, err := m.leitura().ObterJobDefinition(ctx, refID)
	if err != nil {
		err = fmt.Errorf("erro ao carregar a configuração do job: %w", err)
		m.atualizarDefinicaoAoFim(ctx, refID, "error", err.Error())
		m.podarHistorico(ctx, refID)
		return "", err
	}
	var cfg json.RawMessage
	if def.ConfigJson.Valid {
		cfg = json.RawMessage(def.ConfigJson.String)
	}

	handler, ok := m.Registry.Get(refID)
	if !ok {
		// Caso raro: o Handler sumiu entre o Enqueue e o worker chegar a
		// processar o item (downgrade do binário no meio do caminho).
		err := fmt.Errorf("nenhum handler registrado para o job %q", refID)
		m.atualizarDefinicaoAoFim(ctx, refID, "error", err.Error())
		m.podarHistorico(ctx, refID)
		return "", err
	}

	mensagem, execErr := handler.Executar(ctx, cfg)

	status := "success"
	msg := mensagem
	if execErr != nil {
		status = "error"
		msg = execErr.Error()
	}
	m.atualizarDefinicaoAoFim(ctx, refID, status, msg)
	m.podarHistorico(ctx, refID)

	return mensagem, execErr
}

// jobRunner é um adaptador ESTREITO (não expõe estado próprio, só embrulha
// *Manager) para satisfazer execqueue.Runner: a interface exige um método
// chamado literalmente `Executar(ctx, refID, trigger string, progresso
// ProgressoFunc) (string, error)` -- só *Manager* já expõe um `Executar`
// público com OUTRA assinatura (a API síncrona de enfileiramento acima, `
// Executar(ctx, jobID, trigger string) (string, error)`), e Go não permite
// dois métodos com o mesmo nome no mesmo tipo (não é sobrecarga por
// assinatura). scanner.Manager não precisou deste adaptador porque sua API
// síncrona se chama Start, não Executar -- job.Manager já nasceu com esse
// nome (briefing 0052, anterior a este). RegistrarRunner recebe um jobRunner
// (ver ConfigurarFila), que só repassa para executarViaRunner.
type jobRunner struct{ m *Manager }

func (r jobRunner) Executar(ctx context.Context, refID, trigger string, progresso execqueue.ProgressoFunc) (string, error) {
	return r.m.executarViaRunner(ctx, refID, trigger, progresso)
}

// atualizarDefinicaoAoFim espelha o resultado em job_definition e recalcula
// next_run_at — SEMPRE (independente do trigger): cron.Next(agora) aponta
// para o próximo horário FUTURO, então uma execução manual nunca "pula" o
// próximo agendado.
func (m *Manager) atualizarDefinicaoAoFim(ctx context.Context, jobID, status, msg string) {
	def, err := m.leitura().ObterJobDefinition(ctx, jobID)
	if err != nil {
		log.Printf("job: erro ao carregar job_definition %q para atualizar ao fim: %v", jobID, err)
		return
	}
	agora := m.agora()
	nextRunAt := formatarProxima(ProximaExecucao(def.Enabled == 1, def.ScheduleCron.String, agora))

	if _, err := m.escrita().FinalizarJobDefinicao(ctx, sqlcgen.FinalizarJobDefinicaoParams{
		LastRunAt:   sql.NullString{String: agora.Format(carimboFormato), Valid: true},
		LastStatus:  sql.NullString{String: status, Valid: true},
		LastMessage: sql.NullString{String: msg, Valid: msg != ""},
		NextRunAt:   nextRunAt,
		ID:          jobID,
	}); err != nil {
		log.Printf("job: erro ao atualizar job_definition %q ao fim da execução: %v", jobID, err)
	}
}

// podarHistorico apaga o excedente de queue_item (kind='job', ref_id=jobID)
// além de manterExecucoes para o job — best-effort, chamada após cada
// execução finalizar. Desde o briefing 0108/ADR-0028 (CAMADA 3): antes
// operava em job_execution (ListarJobExecucaoIdsExcedentes/ApagarJobExecucao,
// job.sql); agora usa o equivalente de internal/execqueue
// (ListarQueueItemsExcedentesPorRef/ApagarQueueItem, queue_item.sql, camada
// 1) -- mesmo padrão de duas queries simples (limitação do sqlc para poda em
// UMA query DELETE...NOT IN, documentada em queue_item.sql).
func (m *Manager) podarHistorico(ctx context.Context, jobID string) {
	ids, err := m.leitura().ListarQueueItemsExcedentesPorRef(ctx, sqlcgen.ListarQueueItemsExcedentesPorRefParams{
		Kind: execqueue.KindJob, RefID: jobID, Offset: manterExecucoes,
	})
	if err != nil {
		log.Printf("job: poda de histórico: erro ao listar excedentes de %q: %v", jobID, err)
		return
	}
	w := m.escrita()
	for _, id := range ids {
		if err := w.ApagarQueueItem(ctx, id); err != nil {
			log.Printf("job: poda de histórico: erro ao apagar execução %q: %v", id, err)
		}
	}
}

// ── Agendador embutido ───────────────────────────────────────────────────────

// RunScheduler roda o loop do agendador embutido: a cada pollInterval,
// dispara (trigger="schedule") os jobs habilitados cujo next_run_at já
// passou — best-effort (erro de um não impede os outros; ErrJobEmExecucao é
// esperado e silencioso). Sai quando ctx é cancelado — o MESMO ctx do
// signal.NotifyContext do main, para parar limpo junto do resto no
// SIGINT/SIGTERM.
func (m *Manager) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

// tick consulta os jobs agendáveis vencidos e dispara cada um; chamado a cada
// tick do ticker de RunScheduler (extraído para o teste poder chamar direto,
// sem esperar o ticker).
func (m *Manager) tick(ctx context.Context) {
	now := m.agora().Format(carimboFormato)
	defs, err := m.leitura().ListarJobDefinitionsAgendaveis(ctx, sql.NullString{String: now, Valid: true})
	if err != nil {
		log.Printf("job: agendador: erro ao listar jobs agendáveis: %v", err)
		return
	}
	for _, def := range defs {
		if _, err := m.Executar(ctx, def.ID, "schedule"); err != nil && !errors.Is(err, ErrJobEmExecucao) {
			log.Printf("job: agendador: erro ao disparar %q: %v", def.ID, err)
		}
	}
}

// ── Cron: validação e cálculo do próximo horário ─────────────────────────────

// ValidarCron valida uma expressão cron de 5 campos; vazia é válida (= sem
// agendamento, job só-manual). Usada por AtualizarConfig para rejeitar (422)
// uma expressão malformada ANTES de gravar.
func ValidarCron(expr string) error {
	if expr == "" {
		return nil
	}
	_, err := cron.ParseStandard(expr)
	return err
}

// ProximaExecucao calcula o próximo horário (UTC) a partir de `agora` quando
// o job está enabled E tem schedule_cron não-vazio; nil nos demais casos (job
// só-manual, desabilitado, ou cron inválido — chame ValidarCron antes se a
// origem não for confiável). Único parser usado: cron.ParseStandard(...)
// .Next(t) — o loop de disparo é sempre o nosso RunScheduler, nunca o
// scheduler embutido da lib robfig/cron.
func ProximaExecucao(enabled bool, scheduleCron string, agora time.Time) *time.Time {
	if !enabled || scheduleCron == "" {
		return nil
	}
	sched, err := cron.ParseStandard(scheduleCron)
	if err != nil {
		return nil
	}
	next := sched.Next(agora)
	return &next
}

// formatarProxima converte o *time.Time de ProximaExecucao no sql.NullString
// gravável em next_run_at (NULL quando nil).
func formatarProxima(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(carimboFormato), Valid: true}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (m *Manager) leitura() *sqlcgen.Queries { return sqlcgen.New(m.db.Read) }
func (m *Manager) escrita() *sqlcgen.Queries { return sqlcgen.New(m.db.Write) }

func boolParaInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
