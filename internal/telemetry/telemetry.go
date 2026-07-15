// Package telemetry calcula o snapshot de saude operacional exposto por
// GET /api/v1/health/system (briefing 0055): tamanho do banco (PRAGMA),
// contagens de uso (entities/media_files/library_source/job_execution + as
// filas de curadoria do 0018/0053) e uptime/memoria/goroutines do processo.
// Espelha o mesmo espirito de interfaces pequenas do
// internal/job/libraryhealth: o Contador e injetado (curation.Servico
// satisfaz estruturalmente), permitindo fake em teste sem banco real.
//
// A parte de ARMAZENAMENTO por fonte (storage.sources da resposta) NAO mora
// aqui: listar as library_source e responsabilidade de internal/library, e
// este pacote evita importa-lo so para iterar fontes (acoplaria um pacote de
// baixo nivel a outro por conveniencia, ver notas do briefing 0055). O
// handler (internal/server/health_system.go) ja tem s.library.Listar(ctx) e
// monta a lista chamando UsoDisco/EstatisticasFilesystem por fonte -- mesmo
// espirito de composicao enxuta ja usado em libraryhealth.Handler.
package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"time"

	"golang.org/x/sys/unix"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Contador e o subconjunto de curation.Servico que este pacote consome: as
// contagens das filas de curadoria (briefing 0018/0053), reusando as MESMAS
// queries sqlc do GET /review e GET /orphans -- este pacote nao reimplementa
// nenhum predicado SQL. Mesmo padrao de interface pequena de
// internal/job/libraryhealth.Contador.
type Contador interface {
	ContarRevisao(ctx context.Context) (int, error)
	ContarOrfas(ctx context.Context) (int, error)
}

// Snapshot e o payload de banco+uso (SEM armazenamento por fonte -- ver doc
// do pacote).
type Snapshot struct {
	ComputedAt time.Time
	Database   DatabaseSize
	Usage      UsageCounts
}

// DatabaseSize e o tamanho LOGICO do arquivo .sqlite principal, via
// PRAGMA page_count/page_size -- decisao consciente de NAO somar os
// sidecars -wal/-shm (artefatos transitorios do WAL, ver notas do briefing
// 0055). SchemaVersion e o MESMO valor de checkDB().Schema, reexposto aqui
// por conveniencia.
type DatabaseSize struct {
	SizeBytes     int64
	PageCount     int64
	PageSize      int64
	SchemaVersion int64
}

// UsageCounts e o retrato operacional: contagens de catalogo + uptime +
// memoria/goroutines do processo. EntitiesByType so inclui os tipos que
// ocorrem (chave ausente = zero) -- o contrato nao fixa uma lista de tipos
// que ja e livre no schema (entity.type e TEXT sem CHECK).
type UsageCounts struct {
	UptimeSeconds      int64
	EntitiesTotal      int64
	EntitiesByType     map[string]int64
	MediaFilesTotal    int64
	MediaFilesPresent  int64
	MediaFilesMissing  int64
	LibrarySources     int64
	ReviewQueueSize    int
	OrphansCount       int
	JobExecutionsTotal int64
	Goroutines         int
	MemAllocBytes      uint64
}

// FilesystemStats e o espaco livre/total do filesystem que hospeda o Path de
// uma library_source, obtido via unix.Statfs (EstatisticasFilesystem).
type FilesystemStats struct {
	FreeBytes  uint64
	TotalBytes uint64
}

// Servico monta o snapshot de telemetria a partir do pool de banco e do
// Contador das filas de curadoria injetado pelo chamador.
type Servico struct {
	database   *db.DB
	contador   Contador
	iniciadoEm time.Time
}

// New cria o Servico a partir do pool de banco (mesmo padrao de
// library.New/curation.New), do Contador das filas de curadoria e do
// instante considerado "inicio do processo" para o calculo de uptime.
// iniciadoEm e INJETADO (em vez de capturado num init() global) para o teste
// poder controlar o valor sem precisar esperar de verdade -- o chamador real
// (server.montarServicos) passa time.Now() na construcao do Server,
// aproximacao aceitavel do boot (o Server e construido uma unica vez por
// processo).
func New(database *db.DB, contador Contador, iniciadoEm time.Time) *Servico {
	return &Servico{database: database, contador: contador, iniciadoEm: iniciadoEm}
}

// leitura devolve os Queries ligados ao pool de LEITURA (ADR-0010 -- toda
// consulta deste pacote e leitura, nunca precisa do escritor unico).
func (s *Servico) leitura() *sqlcgen.Queries { return sqlcgen.New(s.database.Read) }

// Snapshot monta o payload completo de banco+uso (sem armazenamento por
// fonte, ver doc do pacote/handler).
func (s *Servico) Snapshot(ctx context.Context) (Snapshot, error) {
	tamanho, err := s.TamanhoBanco(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("telemetry: tamanho do banco: %w", err)
	}
	uso, err := s.ContagensUso(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("telemetry: contagens de uso: %w", err)
	}
	return Snapshot{ComputedAt: time.Now().UTC(), Database: tamanho, Usage: uso}, nil
}

// TamanhoBanco roda os dois PRAGMA pela conexao de LEITURA (ADR-0010: e uma
// leitura, nao precisa do escritor unico).
func (s *Servico) TamanhoBanco(ctx context.Context) (DatabaseSize, error) {
	var pageCount, pageSize int64
	if err := s.database.Read.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return DatabaseSize{}, fmt.Errorf("PRAGMA page_count: %w", err)
	}
	if err := s.database.Read.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return DatabaseSize{}, fmt.Errorf("PRAGMA page_size: %w", err)
	}
	schema, err := s.database.SchemaVersion(ctx)
	if err != nil {
		return DatabaseSize{}, fmt.Errorf("versao de schema: %w", err)
	}
	return DatabaseSize{
		SizeBytes:     pageCount * pageSize,
		PageCount:     pageCount,
		PageSize:      pageSize,
		SchemaVersion: schema,
	}, nil
}

// ContagensUso roda as 4 queries sqlc de agregacao (internal/db/queries/
// telemetria.sql) + o Contador das filas de curadoria (injetado), e
// complementa com uptime/memoria/goroutines do processo
// (runtime.MemStats/NumGoroutine -- termometro grosso para detectar algo
// claramente anormal, sem profiling, ver notas do briefing 0055).
func (s *Servico) ContagensUso(ctx context.Context) (UsageCounts, error) {
	q := s.leitura()

	porTipo, err := q.ContarEntitiesPorTipo(ctx)
	if err != nil {
		return UsageCounts{}, fmt.Errorf("contar entities por tipo: %w", err)
	}
	byType := make(map[string]int64, len(porTipo))
	var entitiesTotal int64
	for _, row := range porTipo {
		byType[row.Type] = row.Total
		entitiesTotal += row.Total
	}

	porState, err := q.ContarMediaFilesPorState(ctx)
	if err != nil {
		return UsageCounts{}, fmt.Errorf("contar media_files por state: %w", err)
	}
	var present, missing int64
	for _, row := range porState {
		switch row.State {
		case "present":
			present = row.Total
		case "missing":
			missing = row.Total
		}
	}

	sources, err := q.ContarLibrarySources(ctx)
	if err != nil {
		return UsageCounts{}, fmt.Errorf("contar library_source: %w", err)
	}
	jobExecutions, err := q.ContarJobExecutions(ctx)
	if err != nil {
		return UsageCounts{}, fmt.Errorf("contar job_execution: %w", err)
	}

	revisao, orfas, err := s.contarFilasDeCuradoria(ctx)
	if err != nil {
		return UsageCounts{}, err
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return UsageCounts{
		UptimeSeconds:      s.UptimeSegundos(),
		EntitiesTotal:      entitiesTotal,
		EntitiesByType:     byType,
		MediaFilesTotal:    present + missing,
		MediaFilesPresent:  present,
		MediaFilesMissing:  missing,
		LibrarySources:     sources,
		ReviewQueueSize:    revisao,
		OrphansCount:       orfas,
		JobExecutionsTotal: jobExecutions,
		Goroutines:         runtime.NumGoroutine(),
		MemAllocBytes:      mem.Alloc,
	}, nil
}

// contarFilasDeCuradoria isola a chamada ao Contador injetado -- nil e
// tratado como "sem contador" (0/0), o mesmo espirito defensivo de
// libraryhealth.Handler.rodarAutoMatch para um colaborador opcional.
func (s *Servico) contarFilasDeCuradoria(ctx context.Context) (revisao, orfas int, err error) {
	if s.contador == nil {
		return 0, 0, nil
	}
	revisao, err = s.contador.ContarRevisao(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("contar fila de revisao: %w", err)
	}
	orfas, err = s.contador.ContarOrfas(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("contar orfas: %w", err)
	}
	return revisao, orfas, nil
}

// UsoDisco soma media_file.size (state='present') da fonte -- dado JA
// coletado pelo scanner no ultimo scan, SEM varrer o filesystem de novo
// (briefing 0055: reaproveita o dado em vez de reprocessar o disco do zero;
// a defasagem aceita e a mesma que toda leitura de media_file ja tem no
// resto do sistema).
func (s *Servico) UsoDisco(ctx context.Context, sourceID string) (int64, error) {
	total, err := s.leitura().SomarTamanhoMediaFilePorFonte(ctx, sql.NullString{String: sourceID, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("somar tamanho de media_file por fonte: %w", err)
	}
	return total, nil
}

// EstatisticasFilesystem chama unix.Statfs(path) e devolve o espaco
// livre/total do filesystem que hospeda path. O erro (path inexistente/nao
// montado) e devolvido CRU para o chamador decidir o offline/error da fonte
// -- este pacote nao decide sozinho o que e "offline" (isso e conceito de
// internal/library), so reporta o erro. Restrito a build Linux/Unix (alvo de
// deploy do projeto, ver notas do briefing 0055).
func EstatisticasFilesystem(path string) (FilesystemStats, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return FilesystemStats{}, fmt.Errorf("estatisticas de filesystem indisponiveis: %w", err)
	}
	return FilesystemStats{
		FreeBytes:  uint64(stat.Bavail) * uint64(stat.Bsize),
		TotalBytes: uint64(stat.Blocks) * uint64(stat.Bsize),
	}, nil
}
