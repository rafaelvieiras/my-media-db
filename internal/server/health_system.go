package server

import (
	"context"
	"net/http"

	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/telemetry"
)

// Este arquivo implementa GET /api/v1/health/system (briefing 0055): o
// snapshot de telemetria (tamanho do banco + retrato operacional + uso de
// disco por fonte), SEPARADO de GET /api/v1/health (health.go) de proposito
// -- o HEALTHCHECK do Docker bate em /health a cada 10s e nao pode pagar o
// custo de N syscalls Statfs + agregacoes SQL a cada batida (ver notas do
// briefing). Cacheado por TTL curto (telemetryCache, telemetry_cache.go).

// systemHealthResponse e o corpo de GET /api/v1/health/system.
type systemHealthResponse struct {
	ComputedAt string             `json:"computed_at"`
	Database   databaseHealthDTO  `json:"database"`
	Usage      usageCountsDTO     `json:"usage"`
	Storage    storageSnapshotDTO `json:"storage"`
}

// databaseHealthDTO reexpõe o tamanho lógico do arquivo .sqlite (PRAGMA) +
// schema_version (o MESMO valor de checkDB().Schema).
type databaseHealthDTO struct {
	SizeBytes     int64 `json:"size_bytes"`
	PageCount     int64 `json:"page_count"`
	PageSize      int64 `json:"page_size"`
	SchemaVersion int64 `json:"schema_version"`
}

// usageCountsDTO é o retrato operacional: contagens de catálogo + uptime +
// memória/goroutines do processo.
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

// entitiesUsageDTO.ByType só inclui os tipos que ocorrem (chave ausente =
// zero, não 0 explícito) — o contrato não fixa uma lista de tipos que já é
// livre no schema (entity.type é TEXT sem CHECK).
type entitiesUsageDTO struct {
	Total  int64            `json:"total"`
	ByType map[string]int64 `json:"by_type"`
}

type mediaFilesUsageDTO struct {
	Total   int64 `json:"total"`
	Present int64 `json:"present"`
	Missing int64 `json:"missing"`
}

// storageSnapshotDTO é uma linha por library_source cadastrada.
type storageSnapshotDTO struct {
	Sources []sourceStorageDTO `json:"sources"`
}

// sourceStorageDTO espelha a semântica de avaliarState já usada em
// library.Listar: offline/error quando o Path não responde a Statfs.
// Filesystem é null (ponteiro nil) nesse caso — sem número inventado.
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

// systemHealthHandler trata GET /api/v1/health/system: monta o snapshot de
// telemetria (banco+uso+armazenamento), aplicando o cache TTL (ver
// telemetry_cache.go) antes de recalcular. Sem banco (s.telemetry nil) ->
// 503 com o envelope de erro padrão; erro no cálculo (ex. PRAGMA falhou) ->
// 503 também. NUNCA deriva GET /api/v1/health (handlers/code paths
// independentes, ver health.go).
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

// montarSystemHealth monta o corpo completo: banco+uso vêm de
// telemetry.Servico.Snapshot; armazenamento por fonte é montado AQUI (não em
// internal/telemetry, que não conhece internal/library — ver doc do
// pacote), iterando s.library.Listar.
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
		Storage: storageSnapshotDTO{Sources: s.montarStorageSources(ctx)},
	}, nil
}

// montarStorageSources itera as fontes cadastradas (s.library.Listar) e
// monta uma linha por fonte. Sem s.library (sem banco — já barrado antes) ou
// falha ao listar, devolve lista vazia em vez de abortar o endpoint inteiro
// (o resto do snapshot já foi calculado com sucesso).
func (s *Server) montarStorageSources(ctx context.Context) []sourceStorageDTO {
	if s.library == nil {
		return []sourceStorageDTO{}
	}
	sources, err := s.library.Listar(ctx)
	if err != nil {
		return []sourceStorageDTO{}
	}
	out := make([]sourceStorageDTO, 0, len(sources))
	for _, src := range sources {
		out = append(out, s.montarSourceStorage(ctx, src))
	}
	return out
}

// montarSourceStorage monta a linha de UMA fonte: soma de media_file.size
// (UsoDisco) + estatísticas de filesystem (Statfs). Erro em UMA fonte não
// aborta as demais (best-effort, mesma filosofia do job saude_biblioteca,
// 0053): uma fonte offline vira offline:true/filesystem:null/error, as
// outras continuam normais.
func (s *Server) montarSourceStorage(ctx context.Context, src library.Source) sourceStorageDTO {
	// UsoDisco é uma leitura agregada sobre o MESMO banco já confirmado no ar
	// (o Snapshot já rodou com sucesso); uma falha aqui é rara e degrada para 0
	// em vez de abortar a linha da fonte inteira.
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
