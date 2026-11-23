// Package libraryhealth implements the Handler of the `saude_biblioteca` job
// (an explicit follow-up of the job system foundation): the review queue
// (/review) and the orphans queue (/orphans) are LIVE QUERIES over
// entity/media_file -- they only reflect the reality of the disk after SOMEONE
// runs a manual scan of that library_source. If a file disappears from the disk
// without anyone re-scanning, the entity stays 'present' forever. This job
// closes that gap: it triggers a re-scan of EVERY registered source, waits for
// each one to finish and summarizes the state of the curation queues at the end
// -- a proof of extensibility of the job system: just a new Handler plus a seed
// row, without touching the job schema/endpoints.
package libraryhealth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/job"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// nomeJob is the Handler.Nome() -- it matches the slug seeded in job_definition
// by migration 0012.
const nomeJob = "saude_biblioteca"

// LibraryLister is the subset of library.Service that the Handler consumes:
// listing the registered sources. An interface so a fake can be injected in a
// test without depending on a real filesystem.
type LibraryLister interface {
	Listar(ctx context.Context) ([]library.Source, error)
}

// ScanRunner is the subset of scanner.Manager that the Handler consumes: run
// the scan pipeline of ONE source, SYNCHRONOUSLY AND DIRECTLY, without going
// through the unified queue (internal/execqueue).
//
// Since ADR-0028 (LAYER 3): job.Manager also runs on the SAME sequential worker
// (concurrency 1) as the scanner -- if this Handler (which is already executing
// INSIDE that worker, called by job.Manager.executarViaRunner) enqueued a scan
// and waited for it to finish (the old pattern, Start()+polling Status()), the
// worker would deadlock against itself: the single worker would be busy running
// THIS job, so it would never get to pick up the scan item the job itself had
// just enqueued (a real finding while validating this layer -- the
// storage_snapshot_test.go/TestWithLibraryHealthJobWiring tests hung until the
// test timeout). Executar (the SAME method scanner.Manager exposes as
// execqueue.Runner) runs the scan pipeline INLINE, on the same goroutine as
// this Handler, without touching the queue -- exactly as the worker would do,
// only called directly instead of scheduled.
type ScanRunner interface {
	Executar(ctx context.Context, refID, trigger string, registro execqueue.Registro) (mensagem string, err error)
	// StorageNaoVideo returns the bytes of NON-video files classified as
	// managed/unmanaged on the last full sweep of the source -- ok=false if
	// there has never been a scan. Calculated INSIDE the same WalkDir pass that
	// Executar already triggers (scanner.coletarArquivos); this package does not
	// sweep the disk again, it only reads the in-memory result.
	StorageNaoVideo(sourceID string) (managedBytes, unmanagedBytes int64, ok bool)
}

// Contador is the subset of curation.Servico that the Handler consumes: the
// counts of the curation queues, reusing the SAME sqlc queries as GET /review
// and GET /orphans -- this package does not reimplement any SQL predicate.
type Contador interface {
	ContarRevisao(ctx context.Context) (int, error)
	ContarOrfas(ctx context.Context) (int, error)
}

// Matcher is the subset of *server.Server that the Handler consumes to run the
// batch auto-match over the eligible items of the review queue (a follow-up of
// the auto-match foundation): it resolves the high-confidence ones on its own,
// without needing a manual click. Structurally satisfied by *server.Server (the
// same pattern as LibraryLister/ScanRunner/Contador) -- this package never
// imports internal/server (it avoids a cycle, since server.Handler is built
// FROM INSIDE the server, see server.WithLibraryHealthJob).
type Matcher interface {
	// Both steps receive the queue registrar of the run (ADR-0040) so every
	// entity they process leaves a row in the trail: "which ones succeeded and
	// which ones failed" is answered per item, not only as a total.
	AutoMatchElegiveis(ctx context.Context, registro execqueue.Registro) (resolved, pending int, err error)
	// EnriquecerRetroativo sweeps the complement of ListarRevisao restricted to
	// pure local items (no external_ids, never checked against any provider) and
	// tries to enrich a small batch, throttled per item (a follow-up of the
	// auto-match): it applies on its own with high confidence, or flags it for
	// the review queue when it found something plausible but not trustworthy
	// enough.
	EnriquecerRetroativo(ctx context.Context, registro execqueue.Registro) (aplicados, sinalizados int, err error)
	// ReprocessarArtesFaltantes sweeps entities ALREADY identified (with
	// external_ids) whose art inventory (fanart.tv etc.) was never verified --
	// and tries again, in a small batch, throttled per item (a complement of the
	// retroactive enrichment): it closes the gap of an entity that gained its
	// identity BEFORE an ArteProvider existed/was configured in the system.
	ReprocessarArtesFaltantes(ctx context.Context, registro execqueue.Registro) (processados int, err error)
	// DescobrirArteLocalMusica sweeps music artist/album with an owned track
	// looking for a local poster (poster.jpg/folder.jpg) in the folder derived
	// from the track files -- the same mechanism movie/series already had (a
	// complement of the art reprocessing): the image the owner already has in the
	// folder becomes a valid art source, alongside MusicBrainz/fanart.tv.
	DescobrirArteLocalMusica(ctx context.Context, registro execqueue.Registro) (encontrados int, err error)
	// ReconciliarFilhosOrfaos tries to re-link an orphan DRAFT track/episode
	// (with a file present, but never linked to a parent tree) against the
	// identity/tree already resolved -- or one that came to be resolved later
	// (it closes the gap found while investigating the local music art discovery
	// in production: a match that failed once during the scan stays stuck forever
	// without this).
	ReconciliarFilhosOrfaos(ctx context.Context, registro execqueue.Registro) (reconciliados int, err error)
}

// CompletenessRecalculator is the subset of *server.Server that the Handler
// consumes to recalculate the catalog completeness CACHE (a follow-up of the
// retroactive enrichment work): it re-evaluates EVERY non-draft entity against
// the ruler configured in /settings and writes entity_catalog_completeness,
// which the listing/`/members` read (the listing does NOT calculate it live --
// see the long note in internal/server/completeness.go/completeness_recalc.go
// about why). A small interface, same pattern as Matcher/Contador: structurally
// satisfied by *server.Server, without this package importing internal/server
// (it avoids a cycle, since server.Handler is built FROM INSIDE the server, see
// server.WithLibraryHealthJob).
type CompletenessRecalculator interface {
	RecalcularCompletudeCatalogacao(ctx context.Context, registro execqueue.Registro) (avaliados, incompletos int, err error)
}

// resultadoStorage is the managed/unmanaged aggregate of NON-video files from
// the LAST execution of the job that ran at least one source -- it mirrors the
// same pattern of "response calculated on demand, with no long-lived cache in
// the database" used by the disk usage reading (Statfs): it stays only in
// memory, recalculated on every run of the saude_biblioteca job.
type resultadoStorage struct {
	managedBytes   int64
	unmanagedBytes int64
	computadoEm    time.Time
}

// Handler is the Handler of the saude_biblioteca job.
type Handler struct {
	library      LibraryLister
	scanner      ScanRunner
	contador     Contador
	matcher      Matcher
	completeness CompletenessRecalculator

	muStorage sync.Mutex
	storage   resultadoStorage
	storageOK bool
}

// New creates the Handler from the datasources service, the scan manager, the
// curation queue counter, the auto-match Matcher and the
// CompletenessRecalculator -- injected by the caller
// (server.WithLibraryHealthJob), the same pattern as enrich.Servico/
// scanner.Manager being built with their dependencies at registration time.
// `completeness` is OPTIONAL: nil degrades to "do not recalculate" (same
// pattern as matcher == nil in rodarAutoMatch/rodarEnriquecimentoRetroativo),
// useful for tests that do not need to exercise this step.
func New(lib LibraryLister, scn ScanRunner, cont Contador, matcher Matcher, completeness CompletenessRecalculator) *Handler {
	return &Handler{library: lib, scanner: scn, contador: cont, matcher: matcher, completeness: completeness}
}

// Nome returns the stable slug of the job.
func (h *Handler) Nome() string { return nomeJob }

// Executar re-scans EVERY registered library_source (sequentially, waiting for
// each scan to finish before moving on to the next -- there is no parallelism
// config) and summarizes the result: how many sources scanned successfully, how
// many failed, how many items in review and how many orphans at the end. cfg is
// not used (no parameters today).
//
// An error on ONE source does not abort the others (the same best-effort spirit
// as RunScheduler); an error is only returned if ALL sources fail.
func (h *Handler) Executar(ctx context.Context, cfg json.RawMessage) (string, error) {
	// Without a queue behind it (tests, direct call) the narration is discarded;
	// the pipeline is literally the same method.
	return h.executar(ctx, cfg, job.NovoProgresso(nil))
}

// ExecutarComProgresso implements job.HandlerComProgresso (ADR-0040): the same
// pipeline as Executar, narrating the 7 steps to the queue item so the owner can
// see where the run is, which source/item it is on and how many items already
// failed.
func (h *Handler) ExecutarComProgresso(ctx context.Context, cfg json.RawMessage, p *job.Progresso) (string, error) {
	return h.executar(ctx, cfg, p)
}

func (h *Handler) executar(ctx context.Context, _ json.RawMessage, p *job.Progresso) (string, error) {
	sources, err := h.library.Listar(ctx)
	if err != nil {
		return "", fmt.Errorf("listar fontes da biblioteca: %w", err)
	}

	p.Etapa(1, passosSaude, "escanear as fontes")
	p.Contagem(0, len(sources))
	escaneadas, falhas := h.escanearTodas(ctx, sources, p)
	h.atualizarStorage(sources)
	if len(sources) > 0 && escaneadas == 0 {
		return "", fmt.Errorf("todas as %s falharam ao escanear: %s",
			job.Plural(len(sources), "fonte", "fontes"), strings.Join(falhas, "; "))
	}

	p.Etapa(2, passosSaude, "contar as filas de curadoria")
	revisao, err := h.contador.ContarRevisao(ctx)
	if err != nil {
		return montarMensagemSemContagem(len(sources), escaneadas, falhas, err), nil
	}
	orfas, err := h.contador.ContarOrfas(ctx)
	if err != nil {
		return montarMensagemSemContagem(len(sources), escaneadas, falhas, err), nil
	}

	p.Etapa(3, passosSaude, "auto-match em lote")
	resolvidos := h.rodarAutoMatch(ctx, p.Registro())
	if resolvidos > 0 {
		// Recount: the auto-match may have removed items from the queue -- reflect
		// the FINAL state, not the one from before it ran. An error here degrades
		// to the count already obtained above (best-effort, it does not hide the
		// rest of the summary).
		if recontado, err := h.contador.ContarRevisao(ctx); err == nil {
			revisao = recontado
		}
	}

	p.Etapa(4, passosSaude, "enriquecimento retroativo")
	aplicados, sinalizados := h.rodarEnriquecimentoRetroativo(ctx, p.Registro())
	if sinalizados > 0 {
		// Recount again: items flagged by this step start appearing in the review
		// queue -- the final summary must also reflect the state after THIS step,
		// not only after the auto-match above.
		if recontado, err := h.contador.ContarRevisao(ctx); err == nil {
			revisao = recontado
		}
	}

	p.Etapa(5, passosSaude, "reprocessar artes")
	artesReprocessadas := h.rodarReprocessamentoArtes(ctx, p)
	p.Etapa(6, passosSaude, "descobrir arte local de música")
	arteLocalMusica := h.rodarDescobertaArteLocalMusica(ctx, p)

	p.Etapa(7, passosSaude, "reconciliar filhos órfãos")
	reconciliados := h.rodarReconciliacaoFilhosOrfaos(ctx, p)
	if reconciliados > 0 {
		// Recount (same spirit as the resolvidos/sinalizados above): reconciling
		// removes draft tracks/series from the review queue, so the final summary
		// must reflect the FINAL state, not the one from before this step.
		if recontado, err := h.contador.ContarRevisao(ctx); err == nil {
			revisao = recontado
		}
	}

	p.Etapa(8, passosSaude, "recalcular completude do catálogo")
	avaliados, incompletos := h.rodarRecalculoCompletude(ctx, p)

	return montarMensagem(len(sources), escaneadas, falhas, revisao, orfas, resolvidos, aplicados, sinalizados, artesReprocessadas, arteLocalMusica, reconciliados, avaliados, incompletos), nil
}

// rodarAutoMatch calls the Matcher (batch auto-match over the eligible items of
// the review queue) and returns how many items were resolved automatically.
// Best-effort: a failure here does NOT abort the job -- it only logs and returns
// 0, so the original scan/count summary still comes out.
func (h *Handler) rodarAutoMatch(ctx context.Context, registro execqueue.Registro) int {
	if h.matcher == nil {
		return 0
	}
	resolved, _, err := h.matcher.AutoMatchElegiveis(ctx, registro)
	if err != nil {
		log.Printf("saude_biblioteca: auto-match em lote falhou (best-effort, ignorado): %v", err)
		return 0
	}
	return resolved
}

// rodarEnriquecimentoRetroativo calls the Matcher (retroactive external
// enrichment) and returns how many items were applied on their own and how many
// were flagged for the review queue. Best-effort: a failure here does NOT abort
// the job -- it only logs and returns (0, 0), so the original summary still
// comes out. Called AFTER rodarAutoMatch (same order as the pipeline design:
// re-scan -> batch auto-match -> retroactive enrichment).
func (h *Handler) rodarEnriquecimentoRetroativo(ctx context.Context, registro execqueue.Registro) (aplicados, sinalizados int) {
	if h.matcher == nil {
		return 0, 0
	}
	aplicados, sinalizados, err := h.matcher.EnriquecerRetroativo(ctx, registro)
	if err != nil {
		log.Printf("saude_biblioteca: enriquecimento retroativo em lote falhou (best-effort, ignorado): %v", err)
		return 0, 0
	}
	return aplicados, sinalizados
}

// rodarReprocessamentoArtes calls the Matcher (reprocessing of missing art) and
// returns how many items were verified again. Best-effort: a failure here does
// NOT abort the job -- it only logs and returns 0, so the original summary still
// comes out. Called AFTER rodarEnriquecimentoRetroativo (same family of
// best-effort enrichment steps) and BEFORE the completeness recalculation.
func (h *Handler) rodarReprocessamentoArtes(ctx context.Context, p *job.Progresso) int {
	if h.matcher == nil {
		return 0
	}
	processados, err := h.matcher.ReprocessarArtesFaltantes(ctx, p.Registro())
	if err != nil {
		log.Printf("saude_biblioteca: reprocessamento de artes em lote falhou (best-effort, ignorado): %v", err)
		return 0
	}
	return processados
}

// rodarDescobertaArteLocalMusica calls the Matcher (local music art discovery)
// and returns how many entities gained/updated a local poster. Best-effort: a
// failure here does NOT abort the job -- it only logs and returns 0. Called
// right after rodarReprocessamentoArtes (same family of art steps) and BEFORE
// the completeness recalculation.
func (h *Handler) rodarDescobertaArteLocalMusica(ctx context.Context, p *job.Progresso) int {
	if h.matcher == nil {
		return 0
	}
	encontrados, err := h.matcher.DescobrirArteLocalMusica(ctx, p.Registro())
	if err != nil {
		log.Printf("saude_biblioteca: descoberta de arte local de musica falhou (best-effort, ignorado): %v", err)
		return 0
	}
	return encontrados
}

// rodarReconciliacaoFilhosOrfaos calls the Matcher (reconciliation of orphan
// children -- draft track/episode with a file but never linked to a parent
// tree) and returns how many items were actually re-linked. Best-effort: a
// failure here does NOT abort the job -- it only logs and returns 0. Called
// right after rodarDescobertaArteLocalMusica and BEFORE the completeness
// recalculation.
func (h *Handler) rodarReconciliacaoFilhosOrfaos(ctx context.Context, p *job.Progresso) int {
	if h.matcher == nil {
		return 0
	}
	reconciliados, err := h.matcher.ReconciliarFilhosOrfaos(ctx, p.Registro())
	if err != nil {
		log.Printf("saude_biblioteca: reconciliacao de filhos orfaos falhou (best-effort, ignorado): %v", err)
		return 0
	}
	return reconciliados
}

// rodarRecalculoCompletude calls the CompletenessRecalculator (batch
// recalculation of the catalog completeness cache) and returns how many items
// were evaluated and how many stayed incomplete. Best-effort: a failure here
// does NOT abort the job -- it only logs and returns (0, 0), so the original
// summary still comes out. Called LAST (after the re-scan, auto-match and
// retroactive enrichment): completeness reflects the FINAL state of the
// execution, after the previous steps may have changed resolved data.
func (h *Handler) rodarRecalculoCompletude(ctx context.Context, p *job.Progresso) (avaliados, incompletos int) {
	if h.completeness == nil {
		return 0, 0
	}
	avaliados, incompletos, err := h.completeness.RecalcularCompletudeCatalogacao(ctx, p.Registro())
	if err != nil {
		log.Printf("saude_biblioteca: recalculo de completude de catalogacao falhou (best-effort, ignorado): %v", err)
		return 0, 0
	}
	return avaliados, incompletos
}

// atualizarStorage adds up, for EVERY registered source (not only the ones
// scanned successfully in THIS run), the managed/unmanaged buckets of NON-video
// files that scanner.Manager keeps in memory, and stores the aggregate for
// internal/server/health_system.go to read via UltimoResultadoStorage. A source
// offline in this run simply does NOT update its bucket in the scanner.Manager
// (Executar fails before touching the storage state) -- so the value added up
// here automatically reflects the "keeps the last calculated value" policy,
// without any extra offline logic in this package (same policy as the disk
// usage/Statfs reading). Best-effort: it never fails the job.
func (h *Handler) atualizarStorage(sources []library.Source) {
	var managed, unmanaged int64
	for _, src := range sources {
		m, u, ok := h.scanner.StorageNaoVideo(src.ID)
		if !ok {
			continue
		}
		managed += m
		unmanaged += u
	}
	h.muStorage.Lock()
	h.storage = resultadoStorage{managedBytes: managed, unmanagedBytes: unmanaged, computadoEm: time.Now().UTC()}
	h.storageOK = true
	h.muStorage.Unlock()
}

// UltimoResultadoStorage returns the managed/unmanaged aggregate of NON-video
// files from the LAST execution of the saude_biblioteca job -- read by
// internal/server/health_system.go to compose
// storage.managed_media_bytes (added to the indexed video)/
// storage.unmanaged_bytes. ok=false before the first execution (freshly started
// process, the job has not run even once yet).
func (h *Handler) UltimoResultadoStorage() (managedBytes, unmanagedBytes int64, computadoEm time.Time, ok bool) {
	h.muStorage.Lock()
	defer h.muStorage.Unlock()
	return h.storage.managedBytes, h.storage.unmanagedBytes, h.storage.computadoEm, h.storageOK
}

// escanearTodas triggers and waits for the scan of each source, sequentially.
// It returns how many finished successfully and the failure messages of the
// others (source + reason). Best-effort: a failure does not interrupt the
// following ones.
func (h *Handler) escanearTodas(ctx context.Context, sources []library.Source, p *job.Progresso) (int, []string) {
	escaneadas := 0
	var falhas []string
	for i, src := range sources {
		p.Item(src.Path)
		p.Contagem(i, len(sources))
		if err := h.escanearEAguardar(ctx, src, p); err != nil {
			falhas = append(falhas, fmt.Sprintf("%s: %v", src.Path, err))
			p.Falhou(src.Path, err)
			continue
		}
		p.OkDetalhe(src.Path, "varredura concluída")
		escaneadas++
	}
	p.Contagem(len(sources), len(sources))
	return escaneadas, falhas
}

// escanearEAguardar runs the scan of the source SYNCHRONOUSLY AND DIRECTLY (via
// ScanRunner.Executar, which bypasses the unified queue -- see the long comment
// on ScanRunner for why: enqueueing+waiting here would deadlock against the very
// worker that is executing this job). There is no more polling: Executar only
// returns when the scan has actually finished.
func (h *Handler) escanearEAguardar(ctx context.Context, src library.Source, p *job.Progresso) error {
	// TriggerSchedule is the reasonable approximation: job.Handler.Executar does
	// not receive today whether it was triggered manually or by schedule
	// (job.Handler{Nome, Executar} does not carry that datum, and changing that
	// interface is out of scope for this layer) -- "schedule" is always true in
	// the sense of being a SYSTEMIC trigger, never literally "the owner clicked
	// to scan THIS source".
	// The SAME registro the job is narrating through (ADR-0040): the file-level
	// progress of this scan is what the owner sees on the job item, not a
	// separate queue item (the scan runs inline, see the ScanRunner comment).
	_, err := h.scanner.Executar(ctx, src.ID, scanner.TriggerSchedule, p.Registro())
	return err
}

// passosSaude is how many steps the job announces (Etapa index ranges over it).
const passosSaude = 8

// montarMensagem composes the final success summary: how many sources scanned
// (with the detail of partial failures, if any) + the current counts of the
// curation queues. resolvidos is the result of the batch auto-match; 0 omits the
// parentheses so the message is not polluted in normal operation, when there was
// nothing to resolve. aplicados/sinalizados are the result of the retroactive
// enrichment; the extra clause is omitted when both are zero (same spirit as
// omitting resolvidos==0 above). artesReprocessadas is how many entities ALREADY
// identified had their art inventory re-verified; the clause is omitted when
// zero (same spirit). arteLocalMusica is how many artist/album entities
// gained/updated a local poster; the clause is omitted when zero. reconciliados
// is how many orphan draft tracks/episodes were re-linked back to the parent
// tree (artist/album, series); the clause is omitted when zero (same spirit).
// avaliados/incompletos are the result of the catalog completeness
// recalculation; the extra clause is omitted when NO item was evaluated
// (h.completeness == nil or an empty library) -- with at least one evaluated,
// the clause appears even with incompletos==0 (0 incompletos is relevant
// positive information, unlike aplicados/sinalizados==0 which only means
// "nothing to do").
func montarMensagem(total, escaneadas int, falhas []string, revisao, orfas, resolvidos, aplicados, sinalizados, artesReprocessadas, arteLocalMusica, reconciliados, avaliados, incompletos int) string {
	base := fmt.Sprintf("%s; %d em revisão, %s", resumoScans(total, escaneadas, falhas), revisao, pluralOrfas(orfas))
	if resolvidos > 0 {
		base = fmt.Sprintf("%s; %d em revisão (%s), %s",
			resumoScans(total, escaneadas, falhas), revisao,
			job.Plural(resolvidos, "resolvido automaticamente", "resolvidos automaticamente"),
			pluralOrfas(orfas))
	}
	if aplicados > 0 || sinalizados > 0 {
		base = fmt.Sprintf("%s; enriquecimento retroativo: %s, %s",
			base,
			job.Plural(aplicados, "aplicado", "aplicados"),
			job.Plural(sinalizados, "sinalizado para revisão", "sinalizados para revisão"))
	}
	if artesReprocessadas > 0 {
		base = fmt.Sprintf("%s; %s com inventário de arte reprocessado", base,
			job.Plural(artesReprocessadas, "entity", "entities"))
	}
	if arteLocalMusica > 0 {
		base = fmt.Sprintf("%s; %s de música com pôster local encontrado", base,
			job.Plural(arteLocalMusica, "entity", "entities"))
	}
	if reconciliados > 0 {
		base = fmt.Sprintf("%s; %s", base,
			job.Plural(reconciliados, "faixa/episódio religado à árvore", "faixas/episódios religados à árvore"))
	}
	if avaliados > 0 {
		base = fmt.Sprintf("%s; %s, %s em catalogação", base,
			job.Plural(avaliados, "item avaliado", "itens avaliados"),
			job.Plural(incompletos, "incompleto", "incompletos"))
	}
	return base
}

// pluralOrfas writes the review-queue count the way a person does -- "2 órfãs",
// "1 órfã", "0 órfãs" -- instead of the old programmer placeholder "2 órfã(s)"
// (the final message is shown verbatim on the queue screen, contract #34).
func pluralOrfas(n int) string { return job.Plural(n, "órfã", "órfãs") }

// montarMensagemSemContagem is used when the scans finished but the count of the
// queues failed -- it still reflects what actually ran (the scan itself did not
// fail because of this).
func montarMensagemSemContagem(total, escaneadas int, falhas []string, contErr error) string {
	return fmt.Sprintf("%s; erro ao contar filas de curadoria: %v", resumoScans(total, escaneadas, falhas), contErr)
}

// resumoScans describes how many sources were scanned successfully.
func resumoScans(total, escaneadas int, falhas []string) string {
	switch {
	case total == 0:
		return "nenhuma fonte de biblioteca cadastrada"
	case len(falhas) == 0:
		return job.Plural(escaneadas, "fonte escaneada com sucesso", "fontes escaneadas com sucesso")
	default:
		return fmt.Sprintf("%d de %s com sucesso (%s: %s)",
			escaneadas,
			job.Plural(total, "fonte escaneada", "fontes escaneadas"),
			job.Plural(len(falhas), "falhou", "falharam"),
			strings.Join(falhas, "; "))
	}
}
