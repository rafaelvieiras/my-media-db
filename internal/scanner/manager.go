// Package scanner orchestrates the scan of a datasource: it walks the fs
// filtering video extensions, computes the partial content_hash, extracts the
// technical facts via ffprobe (internal/mediainfo), associates each file with an
// Entity through the Kodi/Jellyfin convention (internal/naming) and discovers
// the local artwork -- all in the background, non-destructive and honoring the
// `missing` lifecycle (ADR-0012). Writes serialize through the single
// connection, in short batches (ADR-0010); the parser and the hash are pure and
// parallelizable.
//
// Orchestration (ADR-0028): the scanner no longer fires its own goroutine nor
// keeps execution state in memory (running/filesSeen/filesTotal/startedAt/
// lastError) -- that lives in the unified queue (internal/execqueue). Start()
// only enqueues (ConfigurarFila); the scan work itself runs later, when the
// queue's sequential worker calls the Manager back as an execqueue.Runner
// (Executar) -- see ConfigurarFila/ConfigurarLibrary below. The only state that
// still lives in the Manager is StorageNaoVideo, which is specific to the scan
// domain and does not belong to the generic queue.
package scanner

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// carimboFormato is the format of the last_seen timestamp used by the lifecycle.
// Nanosecond precision makes each scan's stamp practically unique, which the
// `missing` marking uses as a sentinel ("seen in this scan?").
const carimboFormato = "2006-01-02T15:04:05.000000000Z"

// loteEscrita is the size of the short write batch (ADR-0010): every N files the
// transaction is committed, so that an interruption preserves what was already
// written.
const loteEscrita = 25

// Known triggers of Start -- they mirror execqueue.TriggerManual/Schedule/
// Watch, but are DEFINED HERE (not as an alias) so that packages that depend
// only on scanner (watch, libraryhealth) do not need to import
// internal/execqueue just to have the constant -- see mapTrigger.
const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
	TriggerWatch    = "watch"
)

// Orchestration errors; sentinels so the handler can map them to the error
// envelope.
var (
	// ErrScanEmAndamento: there is already a scan running (or enqueued) for
	// this datasource. -> 409. Mirrors execqueue.ErrJaEnfileirado on the
	// scanner's public surface (Start maps one to the other).
	ErrScanEmAndamento = errors.New("já existe um scan em andamento para este datasource")
	// ErrSourceOffline: the datasource is unreachable (it cannot be walked). -> 409
	ErrSourceOffline = errors.New("datasource inacessível (offline)")
	// ErrFilaNaoConfigurada: Start called before ConfigurarFila -- every
	// production path configures the queue at boot (cmd/mymediadb/main.go);
	// this should only show up in a test/incomplete use of the package.
	ErrFilaNaoConfigurada = errors.New("scanner: fila de execução não configurada (ConfigurarFila)")
)

// Prober is the mediainfo dependency the scanner consumes (ffprobe). Interface
// to allow injection in tests.
type Prober interface {
	Disponivel() bool
	Probe(ctx context.Context, path string) ([]byte, error)
}

// FingerprintCalculador is the acoustic fingerprint dependency (fpcalc/
// Chromaprint) that step 4.5 of the music matching consumes -- SAME SHAPE as
// Prober above (Disponivel + operation), implemented in production by
// *internal/fingerprint.Calculador and injectable for tests without the real
// fpcalc. Disponivel()=false or an error in CalcularFingerprint DEGRADE step
// 4.5 (it falls back to the draft, step 5), never take the scan down.
type FingerprintCalculador interface {
	Disponivel() bool
	CalcularFingerprint(ctx context.Context, path string) (fingerprint string, duracaoSegundos float64, err error)
}

// SerieImporter is the AUTO-IMPORT point that the series scanner (0034)
// consumes when the series of a folder is not in the catalog yet: it searches
// the provider by name, picks the exact and unique match and imports the tree
// (it reuses the Phase 1 enrich). Interface to inject without coupling the
// scanner to enrich and to test with a stub. Implemented by *enrich.Servico.
// preferredLang is the preferred language of the library_source that
// triggered the import (empty = no preference, it uses the global language
// resolved by `idiomas`).
type SerieImporter interface {
	ImportarSeriePorNome(ctx context.Context, nome string, langs []string, preferredLang string) (string, error)
	// ImportarSeriePorID imports the series that the FOLDER NAMES by id -- the
	// Jellyfin convention ("Series (2011) [tmdbid-39340]"). It is the answer to a
	// typo in the folder name: the title search is never asked, so a misspelled
	// name cannot make the import fail. fonte is "tmdb", "tvdb" or "imdb".
	ImportarSeriePorID(ctx context.Context, fonte, id string, langs []string) (string, error)
}

// FilmeImportador is the AUTO-MATCH point that the movie scanner consumes right
// after creating a NEW entity: it tries to match+enrich (metadata + artwork
// inventory) against the configured providers, reusing the SAME confidence
// threshold of the batch auto-match (LimiarAutoMatch, saude_biblioteca). With no
// strong enough candidate, the entity stays cataloged/draft, pending in the
// Review queue -- like today. Interface so as not to couple the scanner to the
// server package. Implemented by *Server.
type FilmeImportador interface {
	// TentarAutoMatchFilme tries to match+enrich the entity; matched=true only
	// when a candidate >= LimiarAutoMatch was actually applied.
	TentarAutoMatchFilme(ctx context.Context, entityID string) (matched bool, err error)
}

// Materializador is the OPTIONAL point that the scanner consumes after ANY
// successful auto-import (movie or series) so that, if the owner has turned on
// "materializar artes automaticamente" (app_settings), it materializes right
// away the artwork the enrich has just discovered -- without waiting for the
// periodic midias_faltantes job. Toggle off (default) = no-op.
// Implemented by *Server.
type Materializador interface {
	MaterializarSeAutoDownloadLigado(ctx context.Context, entityID string) error
}

// MusicaImportador is the IMPORT point of the music vertical that the scanner
// consumes. Importar (ADR-0027) is the generic method every vertical already
// uses (0015/0031/0106) -- today only used by the deliberate MANUAL import
// (search → import artist, dispatcher type="artista"), which brings the whole
// discography and REMAINS untouched. ImportarFaixaPossuida (ADR-0029) is the
// SCOPED import that the scanner's AUTOMATIC trigger has used since then: an
// owned track brings only the artist (identity) + the ALBUM of that track + the
// track itself, never the whole tree. Narrow interface (same pattern as
// SerieImporter/FilmeImportador) so as not to couple the scanner to the whole
// enrich package (only the FaixaAlvo type is exposed). Implemented by
// *enrich.Servico.
type MusicaImportador interface {
	Importar(ctx context.Context, source, sourceID, tipo string, langs []string) (id string, criada bool, err error)
	ImportarFaixaPossuida(ctx context.Context, source, artistaMBID string, alvo enrich.FaixaAlvo, langs []string) (faixaID string, criada bool, err error)
	// ImportarFaixaPorTituloDeAlbum is the COMPILATION/SOUNDTRACK path: it
	// resolves the album by title and uses the artist-credit the source
	// assigns to it. It degrades silently (ok=false) when the source does not
	// implement the capability -- see enrich.ImportarFaixaPorTituloDeAlbum.
	ImportarFaixaPorTituloDeAlbum(ctx context.Context, source string, alvo enrich.FaixaAlvo, langs []string) (faixaID string, criada bool, err error)
}

// LibraryGetter is the subset of library.Service the scanner consumes to
// RE-RESOLVE a library.Source from the id alone: the queue worker calls
// Manager.Executar well after the original Enqueue, with only the refID (string)
// in hand -- it needs to fetch the complete Source again (path, content_type) in
// order to scan. *library.Service satisfies this structurally (same signature
// used in internal/server/scan.go).
type LibraryGetter interface {
	Obter(ctx context.Context, id string) (library.Source, error)
}

// Status is the queryable snapshot of a scan (surface of the GET .../scan API).
type Status struct {
	// Existe is false when there has never been a scan for the datasource.
	Existe     bool
	Running    bool
	FilesSeen  int
	FilesTotal int
	StartedAt  time.Time
	LastError  string
	// CurrentItem/StepLabel/Elapsed carry the observability of the run to the
	// API (ADR-0040): the file being processed right now, the post-scan stage
	// label, and how long it has been running. Empty/zero when unknown.
	CurrentItem string
	StepLabel   string
	Elapsed     time.Duration
	Wait        time.Duration
}

// storageResultado is the result of the NON-video file classification of the
// LAST WalkDir pass of ONE source -- data specific to the scan domain (it does
// not belong to the generic execqueue), kept in memory in the Manager itself,
// DECOUPLED from the queue item lifecycle (it survives even if the history of
// the item itself has already been queried/rotated).
type storageResultado struct {
	managedBytes   int64
	unmanagedBytes int64
}

// Manager holds the scan dependencies and the small storage state in memory
// (StorageNaoVideo). The execution orchestration (queue, concurrency, progress,
// history) lives in internal/execqueue -- see ConfigurarFila. Database writes
// always use the write pool (db.Write) — the single writer of ADR-0010.
type Manager struct {
	db     *db.DB
	prober Prober
	novoID func() string
	agora  func() time.Time

	// fila is the unified execution queue: Start enqueues into it instead of
	// firing its own goroutine. nil only in an incomplete use of the package
	// (every production path calls ConfigurarFila at boot) -- Start returns
	// ErrFilaNaoConfigurada in that case.
	fila *execqueue.Manager
	// library resolves library.Source by id -- needed because Executar
	// (called by the queue worker) only receives the refID, not the whole
	// Source.
	library LibraryGetter

	// serieImporter and idiomas feed the SERIES path (0034): auto-import of the
	// tree by name and the allowed languages from the settings. Both nil when
	// enrich is off — without them, a series missing from the catalog falls into
	// the review queue (draft) instead of being imported. Configured via
	// ConfigurarSeries.
	serieImporter SerieImporter
	idiomas       func(ctx context.Context) []string

	// filmeImportador feeds the MOVIE path (0089): immediate auto-match fired
	// right after the scanner creates a new entity. nil when enrich is off --
	// without it, the movie stays cataloged/draft until a periodic job or a
	// manual action (behavior prior to 0089). Configured via ConfigurarFilmes.
	filmeImportador FilmeImportador

	// materializador is OPTIONAL (0089): when configured and the
	// "materializar_artes_automaticamente" toggle is on, it materializes the
	// newly discovered artwork right after a successful auto-import (movie or
	// series) of the scan. nil = no-op, same as the behavior prior to 0089.
	// Configured via ConfigurarMaterializacao.
	materializador Materializador

	// posScan is the OPTIONAL end-of-scan hook (ADR-0031): it reconciles the
	// drafts and retries the pendings OF THE LIBRARY that has just been walked.
	// nil = behavior prior to the hook (the scan ends at the lifecycle).
	// Configured via ConfigurarPosScan.
	posScan PosScan

	// musicaImportador/musicaProvider feed the MUSIC path (0107): the import by
	// MBID (idempotent, via MusicaImportador) and the MusicBrainz provider
	// itself (for the textual search in the fallback without an embedded MBID,
	// Provider.Buscar, and the optional capability
	// provider.FaixaResolverProvider -- reverse resolution track→artist). Both
	// nil = the music scanner runs in degraded mode (every track becomes a
	// draft, without trying to match) -- same nil-safe pattern of
	// serieImporter/filmeImportador. Configured via ConfigurarMusica.
	musicaImportador MusicaImportador
	musicaProvider   provider.Provider

	// fingerprintCalculador/fingerprintProvider feed the ACOUSTIC FINGERPRINT
	// step 4.5: the last fallback of the music pipeline, fired only after step
	// 4 (textual search) has already failed. fingerprintProvider is a DISTINCT
	// provider from musicaProvider (today: internal/provider/acoustid, never
	// MusicBrainz) -- the real capability (provider.FingerprintProvider) is
	// discovered by type-assert in matchPorFingerprint (musica.go), not here.
	// Both nil = step 4.5 degrades silently (same nil-safe pattern of
	// musicaImportador/musicaProvider) -- without fpcalc available OR without a
	// provider carrying the capability, the scan falls straight through to the
	// draft (step 5). Configured via ConfigurarFingerprint.
	fingerprintCalculador FingerprintCalculador
	fingerprintProvider   provider.Provider

	// muStorage protects storageNaoVideo -- small state in memory, PER SOURCE,
	// of the result of the managed/unmanaged classification (see
	// StorageNaoVideo). Its own mutex, separate from any execution
	// orchestration (which now lives in the execqueue).
	muStorage       sync.Mutex
	storageNaoVideo map[string]storageResultado
}

// ConfigurarFila injects the unified execution queue (ADR-0028) and registers
// the Manager itself as the execqueue.Runner of kind "scan" -- called by the
// process boot (cmd/mymediadb/main.go). Without this, Start returns
// ErrFilaNaoConfigurada.
func (m *Manager) ConfigurarFila(fila *execqueue.Manager) {
	m.fila = fila
	fila.RegistrarRunner(execqueue.KindScan, m)
}

// ConfigurarLibrary injects the resolver of library.Source by id -- called by
// the process boot. Without this, Executar (called by the queue worker) fails
// when trying to re-resolve the Source from the refID.
func (m *Manager) ConfigurarLibrary(lister LibraryGetter) {
	m.library = lister
}

// ConfigurarSeries injects the series auto-import (0034): the importer by name and
// the source of the allowed languages. Called by the server after assembling enrich;
// without it the series scanner runs in degraded mode (it does not import, it only
// matches what already exists and sends the rest to review).
func (m *Manager) ConfigurarSeries(imp SerieImporter, idiomas func(ctx context.Context) []string) {
	m.serieImporter = imp
	m.idiomas = idiomas
}

// ConfigurarFilmes injects the immediate movie auto-match (0089): called by the
// server after assembling enrich; without it the movie scanner runs in degraded
// mode (it does not try an auto-match in the scan, it only creates the
// cataloged/draft entity as before).
func (m *Manager) ConfigurarFilmes(imp FilmeImportador) {
	m.filmeImportador = imp
}

// ConfigurarMaterializacao injects the optional automatic materialization
// (0089), consumed after any successful auto-import of the scan (movie or
// series). Without calling this, the scan never materializes artwork
// automatically, even with the settings toggle on.
func (m *Manager) ConfigurarMaterializacao(mat Materializador) {
	m.materializador = mat
}

// ConfigurarMusica injects the music vertical (0107): the importer by MBID
// (idempotent find-or-create of the whole tree, via
// *enrich.Servico.Importar) and the MusicBrainz provider itself (textual search
// + reverse resolution track→artist, when the provider implements
// provider.FaixaResolverProvider -- see internal/scanner/musica.go). Without
// calling this, the music scanner runs in degraded mode: every audio file
// becomes a draft `faixa` entity, without trying to match against the source
// (same pattern as ConfigurarSeries/ConfigurarFilmes).
func (m *Manager) ConfigurarMusica(imp MusicaImportador, prov provider.Provider) {
	m.musicaImportador = imp
	m.musicaProvider = prov
}

// ConfigurarFingerprint injects the acoustic fingerprint step 4.5: the
// fingerprint calculator (fpcalc, internal/fingerprint) and the provider able to
// query it (today only internal/provider/acoustid, via the optional capability
// provider.FingerprintProvider -- the type-assert happens in
// matchPorFingerprint, not here). Without calling this, step 4.5 degrades to a
// no-op and every file without a textual match falls straight into the draft
// (step 5) -- the behavior prior to the fingerprint step.
func (m *Manager) ConfigurarFingerprint(calc FingerprintCalculador, prov provider.Provider) {
	m.fingerprintCalculador = calc
	m.fingerprintProvider = prov
}

// NewManager creates a Manager with the real ffprobe from PATH.
func NewManager(database *db.DB) *Manager {
	return NewManagerComProber(database, mediainfo.NewProber())
}

// NewManagerComProber creates a Manager with an explicit Prober (tests).
func NewManagerComProber(database *db.DB, prober Prober) *Manager {
	return &Manager{
		db:              database,
		prober:          prober,
		novoID:          func() string { return uuid.NewString() },
		agora:           func() time.Time { return time.Now().UTC() },
		storageNaoVideo: make(map[string]storageResultado),
	}
}

// Start enqueues the scan of a datasource (it no longer fires its own
// goroutine) and returns the id of the queue item (202 in the API).
// ErrSourceOffline if the datasource is unreachable -- in that case the offline
// state is PERSISTED and nothing is marked missing (ADR-0012); that check
// remains SYNCHRONOUS (a cheap stat, it does not need the queue).
// ErrScanEmAndamento if there is already a queued/running item for this source
// (execqueue dedup, mapped to the usual public sentinel error).
// ErrFilaNaoConfigurada if ConfigurarFila was never called.
func (m *Manager) Start(source library.Source, trigger string) (string, error) {
	if !acessivel(source.Path) {
		m.persistirEstado(context.Background(), source, "offline")
		return "", ErrSourceOffline
	}
	if m.fila == nil {
		return "", ErrFilaNaoConfigurada
	}

	id, err := m.fila.Enqueue(context.Background(), execqueue.KindScan, source.ID, mapTrigger(trigger))
	if err != nil {
		if errors.Is(err, execqueue.ErrJaEnfileirado) {
			return "", ErrScanEmAndamento
		}
		return "", err
	}
	return id, nil
}

// mapTrigger translates a scanner.TriggerX into the equivalent
// execqueue.TriggerX -- the string values are EQUAL today, but they are mapped
// explicitly (not by alias/shared const) so that scanner.TriggerX remains
// defined in this package without depending on the internal representation of
// the execqueue. A trigger outside the known set is passed through as is --
// Enqueue returns ErrTriggerInvalido, which surfaces as a normal Start error.
func mapTrigger(trigger string) string {
	switch trigger {
	case TriggerManual:
		return execqueue.TriggerManual
	case TriggerSchedule:
		return execqueue.TriggerSchedule
	case TriggerWatch:
		return execqueue.TriggerWatch
	default:
		return trigger
	}
}

// Executar implements execqueue.Runner: it is called by the queue's sequential
// worker (well after the original Enqueue of Start), with only the refID
// (library_source.id) in hand -- it re-resolves the complete Source via
// LibraryGetter and runs the usual scan pipeline, synchronous (the worker IS
// the background goroutine already; the scanner no longer needs its own). A
// panic inside the pipeline is recovered by the execqueue ITSELF
// (executarComRecover) -- we do not duplicate recover here.
func (m *Manager) Executar(ctx context.Context, refID string, _ string, progresso execqueue.Registro) (string, error) {
	if m.library == nil {
		return "", errors.New("scanner: library getter não configurado (ConfigurarLibrary)")
	}
	source, err := m.library.Obter(ctx, refID)
	if err != nil {
		return "", err
	}
	if err := m.executarScan(ctx, source, progresso); err != nil {
		return "", err
	}
	return "", nil
}

// Status returns the snapshot of the datasource scan (Existe=false if there has
// never been a queue item for this source), translating
// execqueue.QueueItemStatus into the usual public shape -- see
// internal/server/scan.go, which keeps consuming this with no change to the
// JSON contract.
func (m *Manager) Status(sourceID string) Status {
	if m.fila == nil {
		return Status{}
	}
	qis, err := m.fila.Status(context.Background(), execqueue.KindScan, sourceID)
	if err != nil || !qis.Existe {
		return Status{}
	}

	st := Status{
		Existe: true,
		// Running=true both for 'queued' and 'running': a freshly enqueued
		// item may not have been picked up by the worker yet (something else
		// is running), but the caller (libraryhealth.aguardarConclusao) must
		// not see Running=false prematurely just because of that -- it
		// preserves the observable behavior from before (Start() created the
		// synchronous job already with running=true).
		Running:    qis.Status == execqueue.StatusQueued || qis.Status == execqueue.StatusRunning,
		FilesSeen:  qis.ProgressCurrent,
		FilesTotal: qis.ProgressTotal,
	}

	// started_at: empty while the item is still 'queued' (it never really
	// started running). The old job always had startedAt set from CREATION --
	// to preserve that guarantee (Existe=true never had a zero started_at
	// before), it falls back to enqueued_at when started_at does not exist
	// yet.
	ts := qis.StartedAt
	if ts == "" {
		ts = qis.EnqueuedAt
	}
	if t, perr := time.Parse(time.RFC3339, ts); perr == nil {
		st.StartedAt = t
	}

	st.CurrentItem = qis.CurrentItem
	st.StepLabel = qis.StepLabel
	if qis.ElapsedMS != nil {
		st.Elapsed = time.Duration(*qis.ElapsedMS) * time.Millisecond
	}
	if qis.WaitMS != nil {
		st.Wait = time.Duration(*qis.WaitMS) * time.Millisecond
	}

	// LastError only on error -- it does not leak qis.Message from a
	// successful completion (the Message field also carries the final success
	// message, which here is always "" from the Runner, but we check the
	// status defensively anyway).
	if qis.Status == execqueue.StatusError {
		st.LastError = qis.Message
	}
	return st
}

// setStorageNaoVideo records the result of the managed/unmanaged classification
// of NON-video files from this WalkDir pass, under muStorage.
func (m *Manager) setStorageNaoVideo(sourceID string, managedBytes, unmanagedBytes int64) {
	m.muStorage.Lock()
	m.storageNaoVideo[sourceID] = storageResultado{managedBytes: managedBytes, unmanagedBytes: unmanagedBytes}
	m.muStorage.Unlock()
}

// StorageNaoVideo returns the bytes of NON-video files classified as managed
// (the name matches a canonical naming pattern) and unmanaged (the rest) in the
// LAST full walk of this source -- without walking the disk again (the value was
// already computed INSIDE executarScan, in the SAME WalkDir pass that
// coletarArquivos already did). ok=false when there has never been a scan for
// this source -- the caller (libraryhealth.Handler) treats that as "no data
// yet", not as zero.
func (m *Manager) StorageNaoVideo(sourceID string) (managedBytes, unmanagedBytes int64, ok bool) {
	m.muStorage.Lock()
	defer m.muStorage.Unlock()
	res, existe := m.storageNaoVideo[sourceID]
	if !existe {
		return 0, 0, false
	}
	return res.managedBytes, res.unmanagedBytes, true
}

// acessivel tells whether a path exists, is a directory and is readable (a cheap
// stat).
func acessivel(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// ── Post-scan per library (see ADR-0031) ────────────────────────────────────

// PosScan is the OPTIONAL hook called at the END of the scan of a library, after
// the commit of every batch and the closing of the lifecycle (missing +
// last_scan): the library itself reconciles whatever stayed as a draft and
// retries the auto-match of ITS pendings, without waiting for the cron of the
// saude_biblioteca job. Implemented by *Server (internal/server/posscan.go),
// which reuses ReconciliarFaixaDraft/ReconciliarSerieDraft/TentarAutoMatchFilme
// -- no new match logic here. Best-effort: an error is logged and swallowed by
// the scanner.
type PosScan interface {
	ProcessarPosScan(ctx context.Context, sourceID string) error
}

// Caps of the post-scan: how many items of each kind ONE scan execution tries to
// reconcile/retry. Cost/rate-limit throttling -- a large scan cannot turn into a
// cascade of external calls. They are NOT the days throttle of the retroactive
// enrichment (that one stays in the periodic job): here it is the retry of the
// scan the owner has just requested.
//
// Measured in production (Nov 8, 2026; see ADR-0031): without an hour floor,
// EVERY scan retried the same 200 tracks that had just failed -- 45+ min of
// MusicBrainz per scan and no new item resolved. The floor below is the
// post-scan one (not the days one of the periodic job) and the query order is
// "never attempted first", so the queue moves forward on every scan.
const (
	// CapReconciliacaoFaixas is the ceiling of draft tracks reconciled per scan.
	CapReconciliacaoFaixas = 200
	// CapReconciliacaoSeries is the ceiling of draft series reconciled per scan.
	CapReconciliacaoSeries = 50
	// CapAutoMatchPendentes is the ceiling of pending movies/episodes retried per scan.
	CapAutoMatchPendentes = 50
	// PosScanHorasMinimas is the hour floor before the post-scan retries the SAME
	// item. 12h: a library scanned in the morning and again at night keeps moving
	// the queue forward, without repeating what has just failed.
	PosScanHorasMinimas = 12
)

// ConfigurarPosScan injects the post-scan per library. Without it the scan ends
// as before the hook (no reconciliation at the end); the movie auto-match
// trigger (0089) and the optional materialization keep working, because they do
// not depend on this hook.
func (m *Manager) ConfigurarPosScan(p PosScan) {
	m.posScan = p
}
