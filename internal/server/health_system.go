package server

import (
	"context"
	"log"
	"net/http"

	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/telemetry"
)

// This file implements GET /api/v1/health/system: the telemetry
// snapshot (database size + operational portrait + disk usage per source),
// SEPARATE from GET /api/v1/health (health.go) on purpose -- the Docker
// HEALTHCHECK hits /health every 10s and cannot pay the cost of N syscalls
// Statfs + SQL aggregations on every hit (see the notes in health.go). Cached
// for a short TTL (telemetryCache, telemetry_cache.go).

// systemHealthResponse is the body of GET /api/v1/health/system.
type systemHealthResponse struct {
	ComputedAt string             `json:"computed_at"`
	Database   databaseHealthDTO  `json:"database"`
	Usage      usageCountsDTO     `json:"usage"`
	Storage    storageSnapshotDTO `json:"storage"`
}

// databaseHealthDTO re-exposes the logical size of the .sqlite file (PRAGMA) +
// schema_version (the SAME value as checkDB().Schema).
type databaseHealthDTO struct {
	SizeBytes     int64 `json:"size_bytes"`
	PageCount     int64 `json:"page_count"`
	PageSize      int64 `json:"page_size"`
	SchemaVersion int64 `json:"schema_version"`
}

// usageCountsDTO is the operational portrait: catalog counts + uptime +
// process memory/goroutines.
type usageCountsDTO struct {
	UptimeSeconds      int64              `json:"uptime_seconds"`
	Entities           entitiesUsageDTO   `json:"entities"`
	MediaFiles         mediaFilesUsageDTO `json:"media_files"`
	LibrarySources     int64              `json:"library_sources"`
	ReviewQueueSize    int                `json:"review_queue_size"`
	OrphansCount       int                `json:"orphans_count"`
	JobExecutionsTotal int64              `json:"job_executions_total"`
	Goroutines         int                `json:"goroutines"`
	MemAllocBytes      uint64             `json:"mem_alloc_bytes"`
}

// entitiesUsageDTO.ByType only includes the types that occur (absent key =
// zero, not an explicit 0) — the contract does not fix a list of types that is
// already free in the schema (entity.type is TEXT without CHECK).
type entitiesUsageDTO struct {
	Total  int64            `json:"total"`
	ByType map[string]int64 `json:"by_type"`
}

type mediaFilesUsageDTO struct {
	Total   int64 `json:"total"`
	Present int64 `json:"present"`
	Missing int64 `json:"missing"`
}

// storageSnapshotDTO is one row per registered library_source + the four
// buckets (the database lives in Database, above; the other three here):
// managed media (indexed video + NFO/artwork with the canonical name),
// internal app cache (assetsDir) and unidentified (the rest of the tracked
// folders).
type storageSnapshotDTO struct {
	Sources           []sourceStorageDTO `json:"sources"`
	ManagedMediaBytes int64              `json:"managed_media_bytes"`
	AppCacheBytes     int64              `json:"app_cache_bytes"`
	UnmanagedBytes    int64              `json:"unmanaged_bytes"`
}

// sourceStorageDTO mirrors the semantics of avaliarState already used in
// library.Listar: offline/error when the Path does not answer Statfs.
// Filesystem is null (nil pointer) in that case — no invented number.
type sourceStorageDTO struct {
	SourceID   string              `json:"source_id"`
	Path       string              `json:"path"`
	UsedBytes  int64               `json:"used_bytes"`
	Filesystem *filesystemStatsDTO `json:"filesystem"`
	Offline    bool                `json:"offline"`
	Error      string              `json:"error,omitempty"`
}

type filesystemStatsDTO struct {
	FreeBytes  uint64 `json:"free_bytes"`
	TotalBytes uint64 `json:"total_bytes"`
}

// systemHealthHandler handles GET /api/v1/health/system: it assembles the telemetry
// snapshot (database+usage+storage), applying the TTL cache (see
// telemetry_cache.go) before recalculating. Without a database (s.telemetry nil) ->
// 503 with the standard error envelope; an error in the calculation (e.g. PRAGMA failed) ->
// 503 as well. It NEVER affects GET /api/v1/health (independent handlers/code paths,
// see health.go).
func (s *Server) systemHealthHandler(w http.ResponseWriter, r *http.Request) {
	if s.telemetry == nil {
		writeError(w, http.StatusServiceUnavailable, "db_unavailable",
			"persistência indisponível")
		return
	}

	if cached, ok := s.telemetryCache.obter(); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	resp, err := s.montarSystemHealth(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "telemetry_unavailable",
			"não foi possível calcular a telemetria do sistema")
		return
	}

	s.telemetryCache.guardar(resp)
	writeJSON(w, http.StatusOK, resp)
}

// montarSystemHealth assembles the complete body: database+usage come from
// telemetry.Servico.Snapshot; the storage per source is assembled HERE (not in
// internal/telemetry, which does not know internal/library — see the package
// doc), iterating s.library.Listar.
func (s *Server) montarSystemHealth(ctx context.Context) (systemHealthResponse, error) {
	snap, err := s.telemetry.Snapshot(ctx)
	if err != nil {
		return systemHealthResponse{}, err
	}

	byType := snap.Usage.EntitiesByType
	if byType == nil {
		byType = map[string]int64{}
	}

	return systemHealthResponse{
		ComputedAt: snap.ComputedAt.Format("2006-01-02T15:04:05.000Z"),
		Database: databaseHealthDTO{
			SizeBytes:     snap.Database.SizeBytes,
			PageCount:     snap.Database.PageCount,
			PageSize:      snap.Database.PageSize,
			SchemaVersion: snap.Database.SchemaVersion,
		},
		Usage: usageCountsDTO{
			UptimeSeconds:      snap.Usage.UptimeSeconds,
			Entities:           entitiesUsageDTO{Total: snap.Usage.EntitiesTotal, ByType: byType},
			MediaFiles:         mediaFilesUsageDTO{Total: snap.Usage.MediaFilesTotal, Present: snap.Usage.MediaFilesPresent, Missing: snap.Usage.MediaFilesMissing},
			LibrarySources:     snap.Usage.LibrarySources,
			ReviewQueueSize:    snap.Usage.ReviewQueueSize,
			OrphansCount:       snap.Usage.OrphansCount,
			JobExecutionsTotal: snap.Usage.JobExecutionsTotal,
			Goroutines:         snap.Usage.Goroutines,
			MemAllocBytes:      snap.Usage.MemAllocBytes,
		},
		Storage: s.montarStorageSnapshot(ctx),
	}, nil
}

// montarStorageSnapshot assembles the complete storage object: the list per
// source (with the total indexed video added along the way, without an extra SUM) + the
// three new buckets — managed media (video + canonical NON-video, this last one
// read from the in-memory result of the saude_biblioteca job), internal app cache
// (assetsDir) and unidentified.
func (s *Server) montarStorageSnapshot(ctx context.Context) storageSnapshotDTO {
	sources, managedVideoBytes := s.montarStorageSources(ctx)
	managedNaoVideo, unmanaged := s.storageNaoVideoAcumulado()
	return storageSnapshotDTO{
		Sources:           sources,
		ManagedMediaBytes: managedVideoBytes + managedNaoVideo,
		AppCacheBytes:     s.appCacheBytes(),
		UnmanagedBytes:    unmanaged,
	}
}

// montarStorageSources iterates the registered sources (s.library.Listar) and
// builds one row per source, accumulating the total indexed video (used_bytes
// added up) so montarStorageSnapshot can compose managed_media_bytes without running
// another aggregation query. Without s.library (no database — already blocked earlier) or
// a listing failure, it returns an empty list + 0 instead of aborting the whole
// endpoint (the rest of the snapshot was already calculated successfully).
func (s *Server) montarStorageSources(ctx context.Context) ([]sourceStorageDTO, int64) {
	if s.library == nil {
		return []sourceStorageDTO{}, 0
	}
	sources, err := s.library.Listar(ctx)
	if err != nil {
		return []sourceStorageDTO{}, 0
	}
	out := make([]sourceStorageDTO, 0, len(sources))
	var totalVideo int64
	for _, src := range sources {
		row := s.montarSourceStorage(ctx, src)
		totalVideo += row.UsedBytes
		out = append(out, row)
	}
	return out, totalVideo
}

// storageNaoVideoAcumulado reads the aggregated managed/unmanaged of a NON-video
// file from the last run of the saude_biblioteca job
// (s.libraryHealth.UltimoResultadoStorage) — it never recalculates outside, it only reads
// what the job already computed in the SAME sweep it performs. Without s.libraryHealth
// (WithLibraryHealthJob not applied) or before the job's first run,
// it degrades to (0, 0).
func (s *Server) storageNaoVideoAcumulado() (managedBytes, unmanagedBytes int64) {
	if s.libraryHealth == nil {
		return 0, 0
	}
	managedBytes, unmanagedBytes, _, ok := s.libraryHealth.UltimoResultadoStorage()
	if !ok {
		return 0, 0
	}
	return managedBytes, unmanagedBytes
}

// appCacheBytes adds up the size of assetsDir (telemetry.TamanhoDiretorio) —
// best-effort: a read error (rare; it is not "directory does not exist", which
// TamanhoDiretorio already treats as 0 without an error) degrades to
// 0 instead of aborting the whole endpoint. Without assetsDir configured
// (WithAssetsDir not applied), it returns 0 straight away.
func (s *Server) appCacheBytes() int64 {
	if s.assetsDir == "" {
		return 0
	}
	total, err := telemetry.TamanhoDiretorio(s.assetsDir)
	if err != nil {
		log.Printf("health_system: erro ao calcular o tamanho de assetsDir (%q): %v", s.assetsDir, err)
		return 0
	}
	return total
}

// montarSourceStorage assembles the row of ONE source: sum of media_file.size
// (UsoDisco) + filesystem statistics (Statfs). An error in ONE source does not
// abort the others (best-effort, same philosophy as the saude_biblioteca job):
// an offline source becomes offline:true/filesystem:null/error, the
// others stay normal.
func (s *Server) montarSourceStorage(ctx context.Context, src library.Source) sourceStorageDTO {
	// UsoDisco is an aggregated read over the SAME database already confirmed up
	// (the Snapshot already ran successfully); a failure here is rare and degrades to 0
	// instead of aborting the whole source row.
	used, _ := s.telemetry.UsoDisco(ctx, src.ID)

	fs, err := telemetry.EstatisticasFilesystem(src.Path)
	if err != nil {
		return sourceStorageDTO{
			SourceID:  src.ID,
			Path:      src.Path,
			UsedBytes: used,
			Offline:   true,
			Error:     err.Error(),
		}
	}
	return sourceStorageDTO{
		SourceID:   src.ID,
		Path:       src.Path,
		UsedBytes:  used,
		Filesystem: &filesystemStatsDTO{FreeBytes: fs.FreeBytes, TotalBytes: fs.TotalBytes},
		Offline:    false,
	}
}
