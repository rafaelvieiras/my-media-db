package server

import (
	"context"
	"log"
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

// storageSnapshotDTO é uma linha por library_source cadastrada + os quatro
// buckets do briefing 0063 (banco fica em Database, acima; os outros três
// aqui): mídia gerenciada (vídeo indexado + NFO/arte de nome canônico do
// 0062), cache interno da app (assetsDir) e não identificado (resto das
// pastas rastreadas).
type storageSnapshotDTO struct {
	Sources           []sourceStorageDTO `json:"sources"`
	ManagedMediaBytes int64              `json:"managed_media_bytes"`
	AppCacheBytes     int64              `json:"app_cache_bytes"`
	UnmanagedBytes    int64              `json:"unmanaged_bytes"`
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
		Storage: s.montarStorageSnapshot(ctx),
	}, nil
}

// montarStorageSnapshot monta o objeto storage completo (briefing 0055 +
// 0063): a lista por fonte (com o total de vídeo indexado somado ao longo do
// caminho, sem SUM extra) + os três buckets novos do 0063 — mídia gerenciada
// (vídeo + NÃO-vídeo canônico do 0062, este último lido do resultado em
// memória do job saude_biblioteca), cache interno da app (assetsDir) e não
// identificado.
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

// montarStorageSources itera as fontes cadastradas (s.library.Listar) e
// monta uma linha por fonte, acumulando o total de vídeo indexado (used_bytes
// somado) para montarStorageSnapshot compor managed_media_bytes sem rodar
// outra query de agregação. Sem s.library (sem banco — já barrado antes) ou
// falha ao listar, devolve lista vazia + 0 em vez de abortar o endpoint
// inteiro (o resto do snapshot já foi calculado com sucesso).
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

// storageNaoVideoAcumulado lê o agregado managed/unmanaged de arquivo
// NÃO-vídeo (briefing 0063) da última execução do job saude_biblioteca
// (s.libraryHealth.UltimoResultadoStorage) — nunca recalcula por fora, só lê
// o que o job já apurou na MESMA varredura que ele faz. Sem s.libraryHealth
// (WithLibraryHealthJob não aplicada) ou antes da primeira execução do job,
// degrada para (0, 0).
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

// appCacheBytes soma o tamanho de assetsDir (telemetry.TamanhoDiretorio,
// briefing 0063) — best-effort: erro de leitura (raro; não é "diretório
// inexistente", que TamanhoDiretorio já trata como 0 sem erro) degrada para
// 0 em vez de abortar o endpoint inteiro. Sem assetsDir configurado
// (WithAssetsDir não aplicada), devolve 0 direto.
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
