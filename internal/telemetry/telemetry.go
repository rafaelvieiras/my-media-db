// Package telemetry calculates the operational health snapshot exposed by
// GET /api/v1/health/system: database size (PRAGMA), usage counts
// (entities/media_files/library_source/job_execution plus the curation
// queues) and the process uptime/memory/goroutines. It mirrors the same
// small-interface spirit of internal/job/libraryhealth: the Contador is
// injected (curation.Servico satisfies it structurally), allowing a fake in
// tests without a real database.
//
// The STORAGE part per source (storage.sources of the response) does NOT live
// here: listing the library_source is the responsibility of internal/library,
// and this package avoids importing it just to iterate sources (it would
// couple a low-level package to another one for convenience). The handler
// (internal/server/health_system.go) already has s.library.Listar(ctx) and
// builds the list by calling UsoDisco/EstatisticasFilesystem per source -- the
// same lean-composition spirit already used in libraryhealth.Handler.
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

// Contador is the subset of curation.Servico that this package consumes: the
// counts of the curation queues, reusing the SAME sqlc queries as GET /review
// and GET /orphans -- this package does not reimplement any SQL predicate.
// Same small-interface pattern as internal/job/libraryhealth.Contador.
type Contador interface {
	ContarRevisao(ctx context.Context) (int, error)
	ContarOrfas(ctx context.Context) (int, error)
}

// Snapshot is the database+usage payload (WITHOUT per-source storage -- see
// the package doc).
type Snapshot struct {
	ComputedAt time.Time
	Database   DatabaseSize
	Usage      UsageCounts
}

// DatabaseSize is the LOGICAL size of the main .sqlite file, via
// PRAGMA page_count/page_size -- a conscious decision NOT to add up the
// -wal/-shm sidecars (transient WAL artifacts). SchemaVersion is the SAME
// value as checkDB().Schema, re-exposed here for convenience.
type DatabaseSize struct {
	SizeBytes     int64
	PageCount     int64
	PageSize      int64
	SchemaVersion int64
}

// UsageCounts is the operational picture: catalog counts + uptime +
// memory/goroutines of the process. EntitiesByType only includes the types
// that occur (absent key = zero) -- the contract does not fix a list of types,
// which is already free in the schema (entity.type is TEXT without CHECK).
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

// FilesystemStats is the free/total space of the filesystem that hosts the
// Path of a library_source, obtained via unix.Statfs
// (EstatisticasFilesystem).
type FilesystemStats struct {
	FreeBytes  uint64
	TotalBytes uint64
}

// Servico builds the telemetry snapshot from the database pool and from the
// curation-queue Contador injected by the caller.
type Servico struct {
	database   *db.DB
	contador   Contador
	iniciadoEm time.Time
}

// New creates the Servico from the database pool (same pattern as
// library.New/curation.New), from the curation-queue Contador and from the
// instant considered "process start" for the uptime calculation.
// iniciadoEm is INJECTED (instead of captured in a global init()) so the test
// can control the value without actually waiting -- the real caller
// (server.montarServicos) passes time.Now() when building the Server, an
// acceptable approximation of boot (the Server is built a single time per
// process).
func New(database *db.DB, contador Contador, iniciadoEm time.Time) *Servico {
	return &Servico{database: database, contador: contador, iniciadoEm: iniciadoEm}
}

// leitura returns the Queries bound to the READ pool (ADR-0010 -- every query
// in this package is a read, it never needs the single writer).
func (s *Servico) leitura() *sqlcgen.Queries { return sqlcgen.New(s.database.Read) }

// Snapshot builds the complete database+usage payload (without per-source
// storage, see the package/handler doc).
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

// TamanhoBanco runs both PRAGMAs through the READ connection (ADR-0010: it is
// a read, it does not need the single writer).
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

// ContagensUso runs the 4 sqlc aggregation queries (internal/db/queries/
// telemetria.sql) plus the curation-queue Contador (injected), and complements
// them with the process uptime/memory/goroutines
// (runtime.MemStats/NumGoroutine -- a rough thermometer to spot something
// clearly abnormal, without profiling).
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

// contarFilasDeCuradoria isolates the call to the injected Contador -- nil is
// treated as "no counter" (0/0), the same defensive spirit of
// libraryhealth.Handler.rodarAutoMatch for an optional collaborator.
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

// UsoDisco adds up media_file.size (state='present') of the source -- data
// ALREADY collected by the scanner in the last scan, WITHOUT walking the
// filesystem again (reuses the data instead of reprocessing the disk from
// scratch; the accepted staleness is the same one every read of media_file
// already has in the rest of the system).
func (s *Servico) UsoDisco(ctx context.Context, sourceID string) (int64, error) {
	total, err := s.leitura().SomarTamanhoMediaFilePorFonte(ctx, sql.NullString{String: sourceID, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("somar tamanho de media_file por fonte: %w", err)
	}
	return total, nil
}

// EstatisticasFilesystem calls unix.Statfs(path) and returns the free/total
// space of the filesystem that hosts path. The error (path missing/not
// mounted) is returned RAW so the caller decides the offline/error of the
// source -- this package does not decide by itself what is "offline" (that is
// a concept of internal/library), it only reports the error. Restricted to a
// Linux/Unix build (the project's deployment target).
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
