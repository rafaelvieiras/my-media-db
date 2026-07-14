package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// pollInterval é o intervalo do ticker do agendador embutido (briefing 0052):
// a resolução mínima de cron é 1 minuto, 30s é suficiente pra nunca atrasar
// mais que isso. É `var` (não `const`) para o teste do agendador poder
// encurtar o intervalo sem esperar 30s de verdade — nenhum outro código deve
// reatribuí-la fora de teste.
var pollInterval = 30 * time.Second

// manterExecucoes é o cap de retenção do histórico por job (briefing 0052):
// constante Go, não exposta na API. A cada INSERT em job_execution, o
// excedente além das `manterExecucoes` mais recentes DAQUELE job é apagado.
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
// (exportado — WithJobHandlers registra direto nele), o estado em memória de
// execuções em andamento (por job_id) e roda o agendador embutido. Mesmo
// padrão de scanner.Manager (estado em memória + mutex; escritas sempre pelo
// pool de escrita — ADR-0010).
type Manager struct {
	db       *db.DB
	Registry Registry

	novoID func() string
	agora  func() time.Time

	mu      sync.Mutex
	running map[string]bool
}

// NewManager cria um Manager pronto para uso; o Registry começa vazio — os
// handlers entram via Registry.Registrar (server.WithJobHandlers) antes do
// primeiro Executar/RunScheduler.
func NewManager(database *db.DB) *Manager {
	return &Manager{
		db:      database,
		novoID:  func() string { return uuid.NewString() },
		agora:   func() time.Time { return time.Now().UTC() },
		running: make(map[string]bool),
	}
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

// Historico devolve a página de job_execution do job `id`, mais recente
// primeiro. `before` nil = primeira página; não-nil = pagina para trás
// (started_at estritamente menor que o cursor). ErrJobNaoEncontrado se o id
// não existe.
func (m *Manager) Historico(ctx context.Context, id string, limit int, before *string) ([]sqlcgen.JobExecution, error) {
	if _, err := m.Obter(ctx, id); err != nil {
		return nil, err
	}
	var beforeArg any
	if before != nil {
		beforeArg = *before
	}
	return m.leitura().ListarJobExecucoes(ctx, sqlcgen.ListarJobExecucoesParams{
		JobID:  id,
		Before: beforeArg,
		Limit:  int64(limit),
	})
}

// Rodando devolve um snapshot (cópia defensiva) do estado em memória: quais
// job_id estão executando agora — usado por GET /jobs para compor o campo
// `running` de cada item (efêmero por design, o banco não guarda isso).
func (m *Manager) Rodando() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]bool, len(m.running))
	for k, v := range m.running {
		if v {
			out[k] = true
		}
	}
	return out
}

// ── Execução ─────────────────────────────────────────────────────────────────

// Executar dispara uma execução do job `jobID` em background (goroutine):
// cria a linha job_execution (status=running) SINCRONAMENTE (o chamador só
// recebe o id depois que ela existe), então roda o Handler e, ao final,
// atualiza a linha e o job_definition (last_*/next_run_at) e poda o histórico.
// trigger é "manual" (POST .../run) ou "schedule" (agendador).
//
// ErrJobNaoEncontrado se o id não existe em job_definition; ErrJobDesconhecido
// se não há Handler registrado (job seedado, binário sem o handler);
// ErrJobEmExecucao se já roda uma execução deste job.
func (m *Manager) Executar(ctx context.Context, jobID, trigger string) (string, error) {
	if _, err := m.Obter(ctx, jobID); err != nil {
		return "", err
	}
	handler, ok := m.Registry.Get(jobID)
	if !ok {
		return "", ErrJobDesconhecido
	}

	m.mu.Lock()
	if m.running[jobID] {
		m.mu.Unlock()
		return "", ErrJobEmExecucao
	}
	m.running[jobID] = true
	m.mu.Unlock()

	execID := m.novoID()
	if _, err := m.escrita().CriarJobExecucao(ctx, sqlcgen.CriarJobExecucaoParams{
		ID: execID, JobID: jobID, Trigger: trigger,
	}); err != nil {
		m.mu.Lock()
		delete(m.running, jobID)
		m.mu.Unlock()
		return "", err
	}

	go m.executar(handler, jobID, execID)
	return execID, nil
}

// executar roda o Handler e finaliza a execução — SEMPRE em contexto próprio
// (context.Background()), independente do request HTTP que disparou: o
// trabalho (ex.: o VACUUM INTO do backup) não deve abortar só porque a
// requisição terminou. Recupera de pânico para nunca deixar uma execução
// "running" presa (mesmo padrão de scanner.Manager.run).
func (m *Manager) executar(handler Handler, jobID, execID string) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("job: pânico na execução de %q: %v", jobID, rec)
			m.finalizar(jobID, execID, fmt.Errorf("pânico durante a execução: %v", rec), "")
		}
	}()

	ctx := context.Background()
	def, err := m.leitura().ObterJobDefinition(ctx, jobID)
	if err != nil {
		m.finalizar(jobID, execID, fmt.Errorf("erro ao carregar a configuração do job: %w", err), "")
		return
	}
	var cfg json.RawMessage
	if def.ConfigJson.Valid {
		cfg = json.RawMessage(def.ConfigJson.String)
	}

	mensagem, err := handler.Executar(ctx, cfg)
	m.finalizar(jobID, execID, err, mensagem)
}

// finalizar fecha a job_execution, espelha o resultado em job_definition
// (recalculando next_run_at), poda o histórico e libera o job do mapa
// `running` — nesta ordem, sempre (best-effort: falha de log em qualquer
// etapa não impede as seguintes).
func (m *Manager) finalizar(jobID, execID string, err error, mensagemSucesso string) {
	status := "success"
	msg := mensagemSucesso
	if err != nil {
		status = "error"
		msg = err.Error()
	}

	ctx := context.Background()
	if _, ferr := m.escrita().FinalizarJobExecucao(ctx, sqlcgen.FinalizarJobExecucaoParams{
		Status:  status,
		Message: sql.NullString{String: msg, Valid: msg != ""},
		ID:      execID,
	}); ferr != nil {
		log.Printf("job: erro ao finalizar job_execution %q: %v", execID, ferr)
	}

	m.atualizarDefinicaoAoFim(ctx, jobID, status, msg)
	m.podarHistorico(ctx, jobID)

	m.mu.Lock()
	delete(m.running, jobID)
	m.mu.Unlock()
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

// podarHistorico apaga o excedente de job_execution além de manterExecucoes
// para o job — best-effort, chamada após cada execução finalizar (ver a nota
// em internal/db/queries/job.sql sobre a limitação do sqlc para esta poda em
// UMA query DELETE...NOT IN).
func (m *Manager) podarHistorico(ctx context.Context, jobID string) {
	ids, err := m.leitura().ListarJobExecucaoIdsExcedentes(ctx, sqlcgen.ListarJobExecucaoIdsExcedentesParams{
		JobID: jobID, Offset: manterExecucoes,
	})
	if err != nil {
		log.Printf("job: poda de histórico: erro ao listar excedentes de %q: %v", jobID, err)
		return
	}
	w := m.escrita()
	for _, id := range ids {
		if err := w.ApagarJobExecucao(ctx, id); err != nil {
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
