// Package server assembles the application's HTTP router (chi): common
// middlewares, the versioned API (/api/v1/...) and the static server for the
// embedded SPA with route fallback.
package server

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rafaelvieiras/my-media-db/internal/audiotag"
	"github.com/rafaelvieiras/my-media-db/internal/curation"
	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/externaldetail"
	"github.com/rafaelvieiras/my-media-db/internal/fingerprint"
	"github.com/rafaelvieiras/my-media-db/internal/integration"
	"github.com/rafaelvieiras/my-media-db/internal/job"
	"github.com/rafaelvieiras/my-media-db/internal/job/libraryhealth"
	"github.com/rafaelvieiras/my-media-db/internal/job/midiasfaltantes"
	"github.com/rafaelvieiras/my-media-db/internal/job/trailertema"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
	"github.com/rafaelvieiras/my-media-db/internal/telemetry"
	"github.com/rafaelvieiras/my-media-db/internal/watch"
	"github.com/rafaelvieiras/my-media-db/internal/ytdlp"
)

// Server aggregates the dependencies needed to build the HTTP handler.
type Server struct {
	version string
	db      *db.DB
	// library is the datasource service (/api/v1/library). Nil when no database
	// is injected (e.g.: some route tests); the handlers degrade with 503.
	library *library.Service
	// scanner is the manager of scan jobs (/api/v1/library/{id}/scan). Nil when
	// no database is injected.
	scanner *scanner.Manager
	// reprocess keeps the progress of the owner-initiated reprocess per datasource
	// (reprocess.go). In-memory on purpose, like the scan progress: it is state of
	// the RUN, not of the catalog -- a restart loses the progress, not the data.
	reprocessMu sync.Mutex
	reprocess   map[string]*reprocessState
	// providers is the multi-provider REGISTRY of the external sources
	// (ADR-0002, R2): name→Provider. Empty = external search off: local search
	// keeps working and external_available:false. Each source (tmdb/tvdb) is
	// chosen BY the name of the datum (external layer, detail, collection); the
	// search queries ALL of them and merges. Fed via WithSearchProvider
	// (variadic/accumulates).
	providers provider.Registry
	// codecs is the REGISTRY of providers used ONLY for the PARSE capability
	// of an already cached payload (MapearDetalhe/Similares/TvdbCruzadoProvider)
	// -- never for live search (ADR-0024). Unlike `providers`, it is fed
	// INDEPENDENTLY of credentials (main.go always registers tmdb/tvdb/fanarttv
	// with an empty credential): an already saved media_external/external_detail
	// row remains interpretable even with NO key configured. See codecPara in
	// entities.go (fallback to `providers`, covers the MYMEDIADB_FAKE_PROVIDER of
	// the E2E tests).
	codecs provider.Registry
	// integrations is the REGISTRY of integration config (ADR-0023):
	// slug→integration.Descriptor (today only "tmdb"). Fed via WithIntegrations.
	// GET/PUT /integrations (internal/server/integrations.go) read this registry;
	// the EFFECTIVE value of each field (database→env var→absent) is computed by
	// integrationsResolver, below.
	integrations integration.Registry
	// integrationsResolver computes the effective value of each Campo of an
	// integration. Built in New whenever there is a database -- it does not
	// depend on options (same spirit as s.library/s.scanner) -- because it only
	// needs the *db.DB, and never stays nil once entitiesUnavailable has already
	// blocked earlier.
	integrationsResolver *integration.Resolver
	// cacheExterno keeps the external result per (query,type) for a short TTL,
	// to spare the source. It only exists when there is a provider.
	cacheExterno *cacheExterno
	// cacheTitulosAlt keeps, for a short TTL (ttlCacheExterno), the
	// alternative/translated titles of an external candidate per
	// (source,source_id) (melhorCandidatoExterno in automatch.go) -- it spares
	// the provider when the same candidate shows up again within the TTL. It only
	// exists when there is a provider (same condition as cacheExterno).
	cacheTitulosAlt *cacheTitulosAlternativos
	// assetsDir is the trusted directory where the image cache writes the
	// binaries. Injected via WithAssetsDir; it also takes part in the path guard
	// of the poster endpoint (besides the datasources).
	assetsDir string
	// enrich is the enrichment service (import/enrich). It only exists when
	// there is a provider AND assetsDir AND a database — otherwise the handlers
	// degrade with 503. Built in New after applying the options.
	enrich *enrich.Servico
	// externalDetail resolves the RICH EXTERNAL DETAIL with cache-first
	// (external_detail). It only exists when there is a database + provider (it
	// does not need assetsDir); without it the GET /external handler degrades
	// with 503. It shares the cache table with enrich (the state is the
	// database).
	externalDetail *externaldetail.Servico
	// settings is the lightweight (mutex) cache of the language config from
	// app_settings, invalidated on PUT /settings/languages. Always present.
	settings *settingsCache
	// catalogCompleteness is the lightweight (mutex) cache of the CATALOG
	// completeness config from app_settings, invalidated on PUT
	// /settings/catalog-completeness. Always present. Not to be confused with
	// OWNERSHIP completeness (web/src/lib/entity-badges.ts).
	catalogCompleteness *catalogCompletenessCache
	// hostsImagem is the allowlist of the image proxy (GET /images; anti-SSRF).
	// Default = hostsImagemPadrao; injectable in tests. httpImagem downloads the
	// image (short timeout). The proxy only operates with assetsDir configured.
	hostsImagem map[string]bool
	httpImagem  *http.Client
	// jobs is the manager of the job system: dynamic registration of job types
	// (Handler), persisted config/history and the embedded scheduler. Built
	// whenever there is a database (it does not depend on provider/assetsDir,
	// unlike enrich/externalDetail); the concrete handlers (e.g. backup_vacuum)
	// come in via WithJobHandlers.
	jobs *job.Manager
	// telemetry computes the operational health snapshot: database size + usage
	// portrait + process uptime/memory. It only exists when there is a database
	// (same pattern as s.jobs); without it GET /health/system degrades with 503 —
	// GET /health (basic) keeps working regardless (preserved contract, see
	// health.go).
	telemetry *telemetry.Servico
	// telemetryCache keeps, for a short TTL (ttlTelemetria), the ALREADY
	// SERIALIZED snapshot of GET /health/system, so it is not recomputed on every
	// reload/window focus of the health screen. SINGLETON cache (a single entry —
	// this endpoint has no variable parameter, unlike cacheExterno). Always
	// present (even without a database; it is only never populated in that case,
	// since the handler blocks earlier with s.telemetry == nil).
	telemetryCache *telemetryCache
	// libraryHealth is the SAME Handler of the saude_biblioteca job registered
	// by WithLibraryHealthJob — kept aside (besides already being in the Registry
	// of s.jobs) so health_system.go can read UltimoResultadoStorage
	// (managed/unmanaged of a NON-video file from the job's last run, without
	// recalculating it outside). Nil when WithLibraryHealthJob was not applied
	// (or without database/library/scanner) — the new storage fields degrade to 0
	// in that case, same defensive spirit as the rest of the Server.
	libraryHealth *libraryhealth.Handler
	// watch orchestrates the optional fsnotify watcher per library_source
	// (internal/watch): registered via WithWatch, triggered at boot
	// (IniciarWatchers) and HOT by the PATCH /library/{id} handler
	// (aplicarWatchToggle, library.go). Nil when WithWatch was not applied (or
	// without database/library/scanner) -- the watch_enabled toggle still
	// persists in the database in that case, only the in-memory effect does not
	// apply (same defensive guard as libraryHealth).
	watch *watch.Watcher
	// ytdlp manages the yt-dlp binary (ADR-0025): it resolves the usable path
	// (custom database→env → downloaded dynamically → absent) and exposes
	// Baixar/Status, reused by trailer/theme. Injected via WithYtdlp; nil when
	// not injected (e.g.: route tests) -- GET/PUT /settings/ytdlp degrade to
	// unavailable/"none" in that case, without panicking.
	ytdlp *ytdlp.Gerenciador
	// audioTagWriter rewrites the audio tag (ID3v2/Vorbis comment) with the
	// resolved metadata of a track (internal/audiotag — ffmpeg subprocess).
	// Always built (it does not depend on database/options, same pattern as
	// hostsImagem/httpImagem above): the handler (audio_tags.go) checks
	// Disponivel() and degrades with 503 when the ffmpeg binary is not in the
	// environment.
	audioTagWriter *audiotag.Writer
}

// Option optionally configures the Server at construction, preserving the base
// signature New(version, database). See WithSearchProvider.
type Option func(*Server)

// WithSearchProvider REGISTERS one or more external sources (e.g.: TMDB,
// TheTVDB) in the multi-provider registry and turns on the external cache.
// Variadic and ACCUMULATIVE: each provider enters under its Nome() (multiple
// calls add up); nil is ignored. With no provider the registry stays empty
// (external search off). The name preserves compatibility with the singular
// callers (main and tests pass 1 provider).
func WithSearchProvider(provs ...provider.Provider) Option {
	return func(s *Server) {
		registrou := false
		for _, p := range provs {
			if p == nil {
				continue
			}
			s.providers.Registrar(p)
			registrou = true
		}
		if registrou && s.cacheExterno == nil {
			s.cacheExterno = novoCacheExterno(ttlCacheExterno)
		}
		if registrou && s.cacheTitulosAlt == nil {
			s.cacheTitulosAlt = novoCacheTitulosAlternativos(ttlCacheExterno)
		}
	}
}

// WithCodecRegistry REGISTERS one or more providers ONLY for the parse
// capability of an already cached payload
// (MapearDetalhe/Similares/TvdbCruzadoProvider) -- never for live search.
// Independent of WithSearchProvider: a provider may be here without being there
// (no credential) and vice versa (fake mode of the E2E tests). See ADR-0024.
func WithCodecRegistry(provs ...provider.Provider) Option {
	return func(s *Server) {
		for _, p := range provs {
			s.codecs.Registrar(p)
		}
	}
}

// WithIntegrations REGISTERS one or more integration.Descriptor in the
// integrations registry (ADR-0023) -- today only tmdb.NovoDescriptorIntegracao().
// Variadic and ACCUMULATIVE (same pattern as WithCodecRegistry): each Descriptor
// enters under its Slug(); nil is ignored. Without this option, the registry
// stays empty (GET /integrations lists nothing; GET/PUT /integrations/{slug}
// degrade to 404 for any slug).
func WithIntegrations(descs ...integration.Descriptor) Option {
	return func(s *Server) {
		for _, d := range descs {
			s.integrations.Registrar(d)
		}
	}
}

// WithAssetsDir injects the trusted assets directory (image cache). main
// resolves it (env MYMEDIADB_ASSETS_DIR or dirname(DB)/assets) and creates the
// directory before injecting.
func WithAssetsDir(dir string) Option {
	return func(s *Server) {
		s.assetsDir = dir
	}
}

// WithYtdlp injects the Manager of the yt-dlp binary (ADR-0025). main builds it
// with resolveYtdlpDir() + the already resolved MYMEDIADB_YTDLP_PATH env var,
// same spirit as WithAssetsDir/WithSearchProvider. Without this option, s.ytdlp
// stays nil and GET/PUT /settings/ytdlp degrade to unavailable/"none"
// (defensive scenario, it should not happen in production).
func WithYtdlp(g *ytdlp.Gerenciador) Option {
	return func(s *Server) {
		s.ytdlp = g
	}
}

// WithJobHandlers REGISTERS one or more job types in the Registry of the jobs
// Manager. Variadic and ACCUMULATIVE (same pattern as WithSearchProvider): each
// handler enters under its Nome() (multiple calls add up); nil is ignored. The
// Manager itself is built in montarServicos (whenever there is a database); this
// option only needs it to already exist when applied — New applies the options
// after assembling the mandatory services, so the Registry may stay empty until
// then (Registrar on the zero-value works).
func WithJobHandlers(handlers ...job.Handler) Option {
	return func(s *Server) {
		if s.jobs == nil {
			s.jobs = job.NewManager(s.db)
		}
		for _, h := range handlers {
			s.jobs.Registry.Registrar(h)
		}
	}
}

// WithLibraryHealthJob REGISTERS the `saude_biblioteca` job in the Registry of
// the jobs Manager. Unlike WithJobHandlers (which receives a READY Handler,
// built in main.go with main-only dependencies — e.g. backup.New(db, dir)), the
// Handler of this job NEEDS the scanner.Manager and the library.Service, which
// are internal to the Server (private, built in New BEFORE the options loop —
// the same prerequisite WithJobHandlers already assumes for s.jobs to exist).
// That is why the Handler construction happens HERE, at the moment the option
// runs (with s.library/s.scanner already assembled), instead of exposing those
// services so main.go can rebuild them outside (which would duplicate the
// wiring). main.go only has to pass server.WithLibraryHealthJob() along with the
// other options. Null (registering nothing) if there is no database — same guard
// as WithJobHandlers/montarServicos for the other optional services.
func WithLibraryHealthJob() Option {
	return func(s *Server) {
		if s.db == nil || s.library == nil || s.scanner == nil {
			return
		}
		if s.jobs == nil {
			s.jobs = job.NewManager(s.db)
		}
		// s satisfies both libraryhealth.Matcher (auto-match/retroactive
		// enrichment) and libraryhealth.CompletenessRecalculator (recalculation of
		// the catalog completeness cache) -- both parameters receive the SAME
		// *Server.
		h := libraryhealth.New(s.library, s.scanner, curation.New(s.db), s, s)
		s.libraryHealth = h
		s.jobs.Registry.Registrar(h)
	}
}

// WithMidiasFaltantesJob REGISTERS the `midias_faltantes` job in the Registry of
// the jobs Manager. Same reason as WithLibraryHealthJob being a DEDICATED option
// (not WithJobHandlers): the Handler needs the *Server itself as
// EntityLister/Materializador (internal/job/midiasfaltantes), which only exists
// in here. Null (registering nothing) if there is no database -- same guard as
// the others.
func WithMidiasFaltantesJob() Option {
	return func(s *Server) {
		if s.db == nil {
			return
		}
		if s.jobs == nil {
			s.jobs = job.NewManager(s.db)
		}
		// s satisfies midiasfaltantes.EntityLister,
		// midiasfaltantes.Materializador and midiasfaltantes.ContadorMissing (see
		// internal/server/midias_faltantes.go).
		h := midiasfaltantes.New(s, s, s)
		s.jobs.Registry.Registrar(h)
	}
}

// WithTrailerTemaJob REGISTERS the `trailer_tema_faltantes` job in the Registry
// of the jobs Manager. Same reason as WithMidiasFaltantesJob being a DEDICATED
// option (not WithJobHandlers): the Handler needs the *Server itself as
// trailertema.EntityLister/TrailerTemaBaixador (see
// internal/server/localmedia_handlers.go and midias_faltantes.go), which only
// exists in here. Null (registering nothing) if there is no database -- same
// guard as the others.
func WithTrailerTemaJob() Option {
	return func(s *Server) {
		if s.db == nil {
			return
		}
		if s.jobs == nil {
			s.jobs = job.NewManager(s.db)
		}
		// s satisfies both trailertema.EntityLister
		// (ListarEntitiesComMediaFilePresente, midias_faltantes.go) and
		// trailertema.TrailerTemaBaixador
		// (YtdlpDisponivel/BaixarTrailerEntity/BaixarTemaEntity,
		// localmedia_handlers.go).
		h := trailertema.New(s, s)
		s.jobs.Registry.Registrar(h)
	}
}

// WithWatch REGISTERS the orchestrator of the optional fsnotify watch per source
// (internal/watch). It needs the scanner.Manager (internal to the Server, already
// built before the options loop) to trigger the TARGETED scan of a source when
// the debounce expires. Null (s.watch stays nil) if there is no
// database/library/scanner -- same guard as WithLibraryHealthJob; the caller
// (main.go) must still call IniciarWatchers AFTER srv := server.New(...) to
// register the sources that are already born with watch_enabled=1 (it is not
// done in here so as not to do disk I/O during the construction of the
// Server).
func WithWatch() Option {
	return func(s *Server) {
		if s.db == nil || s.library == nil || s.scanner == nil {
			return
		}
		s.watch = watch.New(s.scanner, watch.DebounceDefault)
	}
}

// WithExecQueue connects the Server's internal scanner to the unified
// execution queue (ADR-0028, internal/execqueue): it calls
// scanner.Manager.ConfigurarLibrary/ConfigurarFila with the already assembled
// dependencies (s.library, the `fila` received). It does not create the queue nor
// start the worker (fila.Run) -- that is the responsibility of whoever BUILDS the
// *execqueue.Manager and passes it here (production: cmd/mymediadb/main.go, in a
// following layer that will also migrate internal/job to the SAME queue; tests:
// each harness starts and runs its own test execqueue.Manager). Without this
// option (or with a nil queue), Start() of the scanner returns
// scanner.ErrFilaNaoConfigurada -- no scan path works until someone applies it.
// Null (no-op) if there is no database/library/scanner -- same guard as
// WithWatch/WithLibraryHealthJob.
func WithExecQueue(fila *execqueue.Manager) Option {
	return func(s *Server) {
		if s.db == nil || s.library == nil || s.scanner == nil || fila == nil {
			return
		}
		s.scanner.ConfigurarLibrary(s.library)
		s.scanner.ConfigurarFila(fila)
	}
}

// IniciarWatchers lists EVERY library_source with watch_enabled=1 and registers
// the fsnotify watcher of each one (same routine used by the PATCH /library/{id}
// handler when it turns the toggle on hot) -- called by main BEFORE
// ListenAndServe, same spirit as RunScheduler not delaying the HTTP boot. A
// failure to register ONE source is logged and NEVER aborts the boot nor the
// others. Silent no-op if WithWatch was not applied or there is no
// library.Service (no database).
func (s *Server) IniciarWatchers(ctx context.Context) {
	if s.watch == nil || s.library == nil {
		return
	}
	sources, err := s.library.Listar(ctx)
	if err != nil {
		log.Printf("watch: falha ao listar fontes no boot (nenhum watcher registrado): %v", err)
		return
	}
	for _, src := range sources {
		if !src.WatchEnabled {
			continue
		}
		if err := s.watch.RegistrarFonte(src); err != nil {
			log.Printf("watch: falha ao registrar a fonte %q no boot (best-effort, fonte segue dependendo do polling periodico): %v", src.ID, err)
		}
	}
}

// FecharWatchers closes every registered fsnotify watcher -- called by main on
// the graceful shutdown of the server (same instant as httpServer.Shutdown).
// No-op if WithWatch was not applied.
func (s *Server) FecharWatchers() {
	if s.watch == nil {
		return
	}
	s.watch.Close()
}

// Jobs returns the jobs manager (nil when no database is injected) — used by
// main to trigger the embedded scheduler (RunScheduler) before
// ListenAndServe.
func (s *Server) Jobs() *job.Manager {
	return s.jobs
}

// New creates a Server with the binary version and the database pools (passed
// in by main). The db may be nil in scenarios without persistence (e.g.: some
// route tests); health degrades gracefully in that case. Options are optional
// extensions (e.g.: WithSearchProvider) — the New(version, database) form
// remains valid.
func New(version string, database *db.DB, opts ...Option) *Server {
	s := &Server{version: version, db: database, settings: &settingsCache{},
		catalogCompleteness: &catalogCompletenessCache{},
		hostsImagem:         hostsImagemPadrao, httpImagem: clienteImagemPadrao(),
		telemetryCache: novoTelemetryCache(ttlTelemetria),
		audioTagWriter: audiotag.NewWriter()}
	if database != nil {
		s.library = library.New(database)
		s.scanner = scanner.NewManager(database)
		s.integrationsResolver = integration.NovoResolver(database)
	}
	for _, opt := range opts {
		opt(s)
	}
	s.montarServicos(database)
	return s
}

// montarServicos builds the OPTIONAL services (enrich, rich external detail and
// the scanner's series auto-import) after the options are applied. Each one
// requires its own set of dependencies (database/provider/assetsDir); without
// them it stays nil and the corresponding handler degrades with 503. Extracted
// from New to keep the construction readable (one responsibility per method).
func (s *Server) montarServicos(database *db.DB) {
	// Enrichment requires a database + external source + assets directory.
	// Without any of them it stays nil and the /enrich and /import handlers
	// degrade with 503 (external search off => there is nothing to import).
	if database != nil && !s.providers.Vazio() && s.assetsDir != "" {
		s.enrich = enrich.New(database, s.providers, s.assetsDir)
	}
	// The rich external detail only needs a database -- it no longer depends on
	// a non-empty `providers`: the service now exists whenever there is a
	// database, and the degradation for a missing credential happens PER SOURCE
	// inside PayloadFresco (it serves stale/any cache instead of turning the
	// whole service off). `s.codecs` comes in so the parse capability of the
	// cached payload survives without a key.
	if database != nil {
		s.externalDetail = externaldetail.New(database, s.providers, 0)
	}
	// Turns on the AUTO-IMPORT of series in the scanner: the importer by name
	// (enrich) and the allowed languages from the settings. Only when enrich
	// exists; without it the series scanner degrades (it matches what is already
	// in the catalog, the rest goes to review).
	if s.scanner != nil && s.enrich != nil {
		s.scanner.ConfigurarSeries(s.enrich, s.idiomasScanner)
	}
	// Turns on the immediate AUTO-MATCH of movies during the scan: the new
	// entity just created by the scanner tries to match+enrich on its own, reusing
	// tentarAutoMatch (same threshold as the saude_biblioteca job). Unlike
	// ConfigurarSeries, it does not depend on s.enrich != nil -- the local-first
	// of auto-match works without any external provider, and the external step
	// already degrades silently (autoMatchNone) when s.enrich is nil.
	if s.scanner != nil {
		s.scanner.ConfigurarFilmes(s)
	}
	// Turns on music matching in the scanner (ADR-0027): the import by MBID
	// (idempotent, via s.enrich) and the MusicBrainz provider (textual search +
	// reverse track->artist resolution). Only when both exist -- without enrich or
	// without the registered provider, the scan of a library_source with
	// content_type=music degrades to draft (file cataloged without hierarchy,
	// fixable later).
	if s.scanner != nil && s.enrich != nil {
		if mb, ok := s.providers.Get("musicbrainz"); ok {
			s.scanner.ConfigurarMusica(s.enrich, mb)
		}
	}
	// Turns on step 4.5 of ACOUSTIC FINGERPRINT in the music scanner: the
	// calculator via fpcalc (internal/fingerprint) and the provider able to query
	// AcoustID (optional provider.FingerprintProvider capability -- the real
	// type-assert happens inside the scanner). Only when the "acoustid" provider
	// was registered (cmd/mymediadb/main.go only registers it with the client key
	// configured AND fpcalc available at boot) -- without it, step 4.5 degrades
	// silently and the scan falls straight through to draft (step 5), as before.
	if s.scanner != nil {
		if fp, ok := s.providers.Get("acoustid"); ok {
			s.scanner.ConfigurarFingerprint(fingerprint.NewCalculador(), fp)
		}
	}
	// Turns on the OPTIONAL automatic materialization: consumed after ANY
	// successful auto-import of the scan (movie or series). A disabled toggle
	// (default) makes the adapter degrade to a no-op -- it is safe to configure
	// whenever the scanner exists.
	if s.scanner != nil {
		s.scanner.ConfigurarMaterializacao(s)
	}
	// Turns on the POST-SCAN per library (ADR-0031): at the end of a source's
	// scan, the server itself reconciles the drafts and retries the pending items
	// OF THAT source, materializing the artwork of whoever gained an identity
	// when the toggle above is on. nil-safe: without this the scan ends as
	// before.
	if s.scanner != nil {
		s.scanner.ConfigurarPosScan(s)
	}
	// The jobs Manager only needs a database -- unlike
	// enrich/externalDetail, it does not depend on a provider nor assetsDir (the
	// seed job backup_vacuum uses no external source). WithJobHandlers may have
	// already built it (to register the handlers before this point); here we only
	// cover the case where no option was applied (the GET /jobs listing still has
	// to work, even with no Handler registered).
	if database != nil && s.jobs == nil {
		s.jobs = job.NewManager(database)
	}
	// System telemetry only needs a database — it does not depend on
	// provider/assetsDir (unlike enrich/externalDetail). iniciadoEm=time.Now()
	// here approximates the "process start" (the reported uptime): the Server is
	// built a single time at boot, so this is the best instant available without
	// additional plumbing in main.go.
	if database != nil && s.telemetry == nil {
		s.telemetry = telemetry.New(database, curation.New(database), time.Now())
	}
	// Synchronizes the custom yt-dlp path persisted in app_settings already at
	// boot: without this, an override written in a previous run would only take
	// effect after the 1st read via GET /settings/ytdlp, leaving the
	// trailer/theme code calling ytdlp.Gerenciador directly without the override
	// until then. Best-effort (same spirit as everything that is already a
	// lazy-cache in this file): an error here does not abort the boot, it only
	// postpones the synchronization to the next read via HTTP.
	if database != nil && s.ytdlp != nil {
		_, _ = s.obterYtdlpPathSettings(context.Background())
	}
}

// idiomasScanner returns the allowed languages from the settings (nil on
// error), used as the language source of the scanner's series auto-import.
func (s *Server) idiomasScanner(ctx context.Context) []string {
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return nil
	}
	return cfg.Allowed
}

// Handler builds and returns the root http.Handler of the application.
func (s *Server) Handler() (http.Handler, error) {
	r := chi.NewRouter()

	// Base middlewares: request log and panic recovery.
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Versioned API. Routes under /api that do not match return 404 in JSON,
	// never index.html — so API errors are not masked as a page.
	r.Route("/api", func(api chi.Router) {
		api.NotFound(apiNotFound)
		api.MethodNotAllowed(apiNotFound)

		api.Route("/v1", func(v1 chi.Router) {
			v1.Get("/health", s.healthHandler)

			// System telemetry: database size + operational portrait + disk
			// usage per source, in a SNAPSHOT on demand (60s cache). Kept apart
			// from /health on purpose — this one is more expensive (N Statfs
			// syscalls + SQL aggregations) and cannot weigh on the Docker
			// HEALTHCHECK (which hits /health every 10s).
			v1.Get("/health/system", s.systemHealthHandler)

			// Local-first search: FTS5 over the catalog + optional passthrough
			// to the external source ("unsaved" results).
			v1.Get("/search", s.search)

			// Explicit import: find-or-create by external id, then enrich. It
			// writes external, never override.
			v1.Post("/import", s.importar)

			// Manual LOCAL collection creation (Phase B): the owner assembles a
			// collection from scratch (with no external source) and receives the
			// resolved `type='collection'` entity. Members come in and out later
			// through the /entities/{id}/members routes below.
			v1.Post("/collections", s.createCollection)

			// RICH external detail before importing: it pulls the complete item
			// from the source, PERSISTS it on the 1st call (external_detail cache)
			// and serves it from the cache on the following ones, resolved in the
			// active language. View != catalog (it never creates an entity).
			v1.Get("/external/{source}/{type}/{id}", s.getExternalDetail)

			// External collection as its own PAGE: the set's name/artwork + the
			// parts (movies) navigable to the external detail. The static segment
			// "collection" takes precedence over the {type} of the route above in
			// chi. Via the optional provider.ColecaoProvider capability; sources
			// without collections degrade with 503.
			v1.Get("/external/{source}/collection/{id}", s.getExternalCollection)

			// External album as its own PAGE (ADR-0029): official name/tracks of
			// the release (via provider.MusicaAlbumProvider, BuscarAlbumPorMbid) --
			// the "missing tracks" screen of the already internal album, same
			// pattern as the collection above ("album" also takes precedence over
			// the generic {type}). Sources without the selective album capability
			// degrade with 503.
			v1.Get("/external/{source}/album/{id}", s.getExternalAlbum)

			// READ-THROUGH proxy of images: it downloads and caches on disk the
			// image of an allowed host (anti-SSRF allowlist) so the EXTERNAL detail
			// screen (pre-import) does not depend on the source's CDN at runtime —
			// like the cached poster/fanart of the entity. 503 without assetsDir;
			// the resolution lives in image_proxy.go.
			v1.Get("/images", s.getImageProxy)

			// CURATION queues: review (draft or low confidence) and orphans
			// (auto-created, all missing, no investment from the owner). Read-only;
			// the same card shape as the list. The actions
			// (confirm/correct/purge) live under /entities.
			v1.Get("/review", s.listReview)
			v1.Get("/orphans", s.listOrphans)

			// BATCH AUTO-MATCH: it tries to resolve on its own every eligible
			// item of the queue (or the subset of `ids` in the body), reusing the
			// SAME logic as the individual one in /entities/{id}/auto-match below.
			// Always 200 with the aggregated summary.
			v1.Post("/review/auto-match", s.autoMatchReview)

			// PERSON page (interno-first): the person deduplicated in the catalog
			// + the filmography (already imported works that credit them).
			// Destination of the cast/crew card when the credit carries
			// `person_id`. Read-only; 404 when the person does not exist in the
			// catalog.
			v1.Get("/person/{id}", s.getPerson)

			// Editorial FORM descriptor: static metadata that tells the front end
			// HOW to render each editable field, derived from the single source
			// resolver.CamposEditaveis() + the presentation registry. Read-only, no
			// database; cacheable (immutable between deploys).
			v1.Get("/schema/entity-fields", s.getEntityFieldsSchema)

			// Application settings. Today only the language config
			// (primary/fallback/allowed), the basis of per-language resolution.
			v1.Route("/settings", func(cfg chi.Router) {
				cfg.Get("/languages", s.getLanguageSettings)
				cfg.Put("/languages", s.putLanguageSettings)

				// CATALOG completeness per type: which fields/artwork the owner
				// considers desirable for an item to be well cataloged. Different
				// from OWNERSHIP completeness (front end).
				cfg.Get("/catalog-completeness", s.getCatalogCompletenessSettings)
				cfg.Put("/catalog-completeness", s.putCatalogCompletenessSettings)

				// Throttle of retroactive enrichment: GLOBAL retry interval, in
				// days, between two attempts on the SAME item without a provider
				// (default 15 days). The optional per-library override lives in
				// library_source (see library.go).
				cfg.Get("/enrichment-retroativo", s.getEnrichmentRetroativoSettings)
				cfg.Put("/enrichment-retroativo", s.putEnrichmentRetroativoSettings)

				// Toggle of automatic artwork materialization: off by default --
				// when on, every successful auto-import of the scan (movie or
				// series) already materializes the inventory artwork in the real
				// folder, without waiting for the periodic midias_faltantes job nor
				// a manual click.
				cfg.Get("/auto-materialize-artwork", s.getAutoMaterializeArtworkSettings)
				cfg.Put("/auto-materialize-artwork", s.putAutoMaterializeArtworkSettings)

				// Toggle of automatic trailer/theme download: off by default --
				// when on, shared by the periodic trailer_tema_faltantes job AND by
				// the automatic trigger in the scan (every successful auto-import
				// tries to download trailer/theme in the background, without
				// blocking the scan response).
				cfg.Get("/auto-download-trailer-tema", s.getAutoDownloadTrailerTemaSettings)
				cfg.Put("/auto-download-trailer-tema", s.putAutoDownloadTrailerTemaSettings)

				// Custom yt-dlp binary path: effective status
				// (available/origin/detail) + the owner's override, with
				// database > env var MYMEDIADB_YTDLP_PATH precedence, applied at
				// runtime without a restart. Foundation for trailer and theme.
				cfg.Get("/ytdlp", s.getYtdlpSettings)
				cfg.Put("/ytdlp", s.putYtdlpSettings)
			})

			// Integrations configurable through the UI: generic registry of
			// provider config (today only TMDB), with database > env var
			// precedence computed by integrationsResolver. No screen of its own in
			// this delivery -- these endpoints are consumed elsewhere. GET lists
			// (with `configurada`)/details (schema + effective value per field,
			// never the raw value of a secret field); PUT writes PARTIALLY (only
			// the fields present in the body change, `null` removes the key and
			// goes back to inheriting the env var).
			v1.Route("/integrations", func(ig chi.Router) {
				ig.Get("/", s.listIntegrations)
				ig.Get("/{slug}", s.getIntegration)
				ig.Put("/{slug}", s.putIntegration)
			})

			// Library datasources. Writes (POST/PATCH/DELETE) through the write
			// connection, reads (GET) through the pool — encapsulated in the
			// internal/library service (ADR-0010). PATCH edits watch_enabled and
			// applies the fsnotify watcher HOT (aplicarWatchToggle, library.go),
			// without a restart.
			v1.Route("/library", func(lib chi.Router) {
				lib.Post("/", s.createLibrary)
				lib.Get("/", s.listLibrary)

				// Folder browser: STATIC route, it must be declared before it
				// would in theory conflict with the "/{id}" wildcard below — in
				// practice chi's radix tree already prefers the static route anyway,
				// but the order here makes the intent explicit and is covered by an
				// integration test.
				lib.Get("/browse", s.browseLibraryPath)

				lib.Get("/{id}", s.getLibrary)
				lib.Patch("/{id}", s.patchLibrary)
				lib.Delete("/{id}", s.deleteLibrary)

				// Background scan per datasource: it triggers (202) and polls the
				// progress.
				lib.Post("/{id}/scan", s.startScan)
				lib.Get("/{id}/scan", s.getScanStatus)

				// Owner-initiated reprocess of ONE library: reconcile the drafts
				// with no budget/throttle, purge the empty drafts and, in the full
				// mode, re-scan first (which re-parses every name). dry_run is the
				// default and answers what would change before writing anything.
				lib.Post("/{id}/reprocess", s.startReprocess)
				lib.Get("/{id}/reprocess", s.getReprocess)
			})

			// READ surface of the catalog. GETs only, all through the read pool
			// (ADR-0010): paginated list, resolved detail, files+mediainfo and the
			// poster binary.
			v1.Route("/entities", func(ent chi.Router) {
				ent.Get("/", s.listEntities)

				// Manual LOCAL ITEM creation: the owner catalogs a movie/series
				// (type=movie|tv) WITHOUT an external source and receives the
				// resolved entity. It combines with PUT .../external-ids below
				// (pin the source id later, to synchronize). A manual collection has
				// /collections.
				ent.Post("/", s.createItem)

				// Types with cataloged data: STATIC route, it must be declared
				// before the "/{id}" wildcard below — in practice chi's radix tree
				// already prefers the static route anyway (same pattern as
				// /library/browse above), but the order here makes the intent
				// explicit and is covered by an integration test.
				ent.Get("/types", s.listEntityTypes)

				ent.Get("/{id}", s.getEntity)
				ent.Get("/{id}/files", s.getEntityFiles)

				// Members of a COLLECTION: the movies linked by
				// entity_relation(kind='collection'), as cards (same shape as the
				// list), ordered by year. `[]` for a non-collection. The POST links
				// an item and the DELETE unlinks it (Phase B, manual editing of the
				// members): both validate that the parent is a collection and return
				// the updated list.
				ent.Get("/{id}/members", s.getEntityMembers)
				ent.Post("/{id}/members", s.addMember)
				ent.Delete("/{id}/members/{childId}", s.removeMember)

				// Episodes of the series GROUPED by ordering: the ordering
				// selector (official/dvd/absolute) reprojects the series at READ
				// time from the `numbering` — season headers of the order do NOT
				// become an entity (ADR-0021), only the episode is navigable. A
				// missing or invalid `?ordering=` falls back to the series default;
				// `absolute` is a special order.
				ent.Get("/{id}/episodes", s.getEntityEpisodes)
				ent.Get("/{id}/assets/poster", s.getEntityPoster)
				ent.Get("/{id}/assets/fanart", s.getEntityFanart)
				ent.Get("/{id}/assets/still", s.getEntityStill)

				// AVAILABLE artwork grouped by kind: it merges the local assets +
				// the poster/background of the external layers (TMDB) + the lazy
				// fanart.tv inventory, with already proxied URLs and
				// `available:{kind:count}` for the badges. Read-only; consumed by
				// the gallery/badges.
				ent.Get("/{id}/artwork", s.getEntityArtwork)

				// ACTION "download only the selected one": it receives the url of
				// ONE artwork from the inventory (host validated against the proxy
				// allowlist — anti-SSRF), downloads the binary and materializes it
				// as an asset with origin='external' of the kind (idempotent by
				// url+kind), optionally marking it as preferred.
				ent.Post("/{id}/artwork/select", s.selectArtwork)

				// Custom artwork of the owner: upload (origin='user'), gallery,
				// service of an asset binary, preference (flag in meta_json) and
				// removal. The serving precedence (preferred → user → external →
				// 404) is applied in /assets/poster above. Writes through the write
				// pool (ADR-0010).
				ent.Post("/{id}/assets", s.uploadAsset)
				ent.Get("/{id}/assets", s.listAssets)
				ent.Get("/{id}/assets/{assetId}/file", s.getAssetFile)
				ent.Put("/{id}/assets/{assetId}/prefer", s.preferAsset)
				ent.Delete("/{id}/assets/{assetId}", s.deleteAsset)

				// Physical materialization in the REAL folder of the library: it
				// copies an artwork already cached internally to the library disk,
				// under the canonical Kodi/Emby name (internal/localmedia). It does
				// NOT touch is_principal (that is the PUT /prefer above, extended in
				// the same delivery). "Save information (NFO)" (group of quick
				// actions) writes/rewrites the entity's .nfo in the real folder —
				// one-way export (database → disk), idempotent.
				ent.Post("/{id}/assets/{assetId}/materialize", s.materializeAsset)
				ent.Post("/{id}/nfo", s.writeEntityNfo)

				// Local theme: LOCAL-ONLY foundation, without a provider — it
				// detects a theme.{mp3,ogg,flac} placed by hand by the owner in the
				// SAME root folder that materialize/nfo use and serves the binary.
				// 404 without a destination folder/file; it never materializes an
				// asset.
				ent.Get("/{id}/theme", s.getEntityTheme)
				// HEAD of the same handler: the front end checks existence (a
				// single check, without downloading the whole binary) before
				// rendering the player — http.ServeContent already honors HEAD (no
				// body).
				ent.Head("/{id}/theme", s.getEntityTheme)

				// Trailer materialized on disk: GET detects/serves the already
				// downloaded binary (same destination resolution as the theme
				// above); POST downloads via yt-dlp the first YouTube trailer listed
				// in `videos` and writes it under the canonical Kodi/Jellyfin name.
				// No new `asset` table — same precedent as theme.mp3.
				ent.Get("/{id}/trailer", s.getEntityTrailer)
				ent.Post("/{id}/trailer/download", s.postEntityTrailerDownload)

				// Automatic theme via ThemerrDB: GET exposes whether there is a
				// cached candidate (without leaking the YouTube URL) and whether a
				// local theme.* already exists; POST downloads the candidate via
				// yt-dlp to theme.mp3 — the SAME path that GET /{id}/theme above
				// already serves. No `force`: the theme is singular per folder.
				ent.Get("/{id}/theme/candidato", s.getEntityThemeCandidato)
				ent.Post("/{id}/theme/download", s.postEntityThemeDownload)

				// Enrichment of an existing entity: it pulls the detail from the
				// provider and writes the external base + cached poster.
				ent.Post("/{id}/enrich", s.enrichEntity)

				// Phase 3 curation: confirming raises the confidence to 100;
				// correcting writes the correct identity + re-enriches; DELETE
				// purges the entity (CASCADE + binary cleanup); /purge purges in
				// batch. Purging is the only destructive door (ADR-0012).
				ent.Post("/{id}/confirm", s.confirmEntity)
				ent.Post("/{id}/correct", s.correctEntity)
				ent.Delete("/{id}", s.deleteEntity)
				ent.Post("/purge", s.purgeBatch)

				// Individual AUTO-MATCH: it tries to resolve on its own an item of
				// the queue — local-first (fundirEntities) or external (same sequence
				// as "Correct"), only when the score hits LimiarAutoMatch. Always
				// 200 (never 5xx for "no candidate found"); 404 if it does not
				// exist.
				ent.Post("/{id}/auto-match", s.autoMatchEntity)

				// Candidate preview + supervised application: the preview reuses
				// the SAME search/scoring as the auto-match above, but WITHOUT the
				// LimiarAutoMatch cutoff (dry-run, it never applies anything);
				// confirm-candidate applies the shown candidate even below the
				// threshold -- an informed human decision, a third endpoint, not a
				// rework of /confirm nor /correct.
				ent.Get("/{id}/match-preview", s.matchPreview)
				ent.Post("/{id}/confirm-candidate", s.confirmCandidate)

				// Quick actions ON DEMAND for ONE item open in the detail:
				// enrich-missing triggers the SAME orchestration of
				// tentarEnriquecerOuSinalizar, outside the job's batch/throttle;
				// download-missing-artwork downloads the best available artwork for
				// the kinds missing in the catalog completeness, via the same
				// aggregator/download.
				ent.Post("/{id}/enrich-missing", s.enrichMissing)
				ent.Post("/{id}/download-missing-artwork", s.downloadMissingArtwork)

				// Force the reconciliation of ONE track/series stuck in draft: it
				// triggers the SAME logic as the batch job
				// (ReconciliarFilhosOrfaos), but ignoring the
				// last_external_match_attempt_at throttle -- a manual escape hatch,
				// same ON DEMAND pattern as enrich-missing above.
				ent.Post("/{id}/force-reconcile", s.forcarReconciliacao)

				// "Check for File Updates": an ALWAYS visible action of the Manage
				// menu (it does not depend on catalog_completeness) — it forces a
				// re-hash/mediainfo of the entity's files even without an
				// mtime/size change, rechecks the folder for a new/updated local
				// asset and fills LOCAL playback fields from an `.nfo` already on
				// disk (only what is still empty).
				ent.Post("/{id}/refresh-files", s.refreshEntityFiles)

				// "Download Missing Artwork": an ALWAYS visible action of the
				// Manage menu (it does not depend on catalog_completeness) — it
				// forces a new query to the registered ArteProvider(s) (today:
				// fanart.tv) to update the entity's artwork inventory and then
				// materializes whatever is missing in the real folder (same effect
				// as the midias_faltantes job, on demand and restricted to this
				// entity).
				ent.Post("/{id}/materialize-missing-media", s.materializeMissingMediaEntity)

				// "Write metadata to the file": it rewrites the audio tag
				// ITSELF (ID3v2/Vorbis comment) with the RESOLVED metadata of the
				// track (or of each owned track, in cascade, for an album) -- an
				// alternative to the music NFO (which the project decided NOT to
				// do): LMS/Navidrome/gonic/Supysonic and the
				// Subsonic/OpenSubsonic ecosystem read straight from the tag. No
				// body; 422 for type=='artista' (no coherent cascade of "work").
				ent.Post("/{id}/audio-tags/write", s.writeAudioTags)

				// Editing of the external IDENTITY: it writes the external_ids map
				// (source -> id at the source). The generated columns re-derive;
				// pinning the id prepares a future synchronization. It does NOT
				// re-enrich (the owner calls /enrich afterwards). Written through
				// the write pool (ADR-0010).
				ent.Put("/{id}/external-ids", s.updateExternalIds)

				// PER-LANGUAGE override editor: the owner writes the sparse patch
				// that wins over everything. PATCH merges per language (?lang=), the
				// DELETEs clean (field/language or reset), GET returns the raw blob.
				// Writes through the write pool (ADR-0010).
				ent.Patch("/{id}/override", s.patchOverride)
				ent.Get("/{id}/override", s.getOverride)
				ent.Delete("/{id}/override", s.deleteOverride)
				ent.Delete("/{id}/override/{campo}", s.deleteOverrideCampo)

				// Consumption history (ADR-0022): generic tracking events
				// (watch/listen/play/etc.), vertical-agnostic. POST records an
				// event; GET lists the history (optional ?kind= filter) in
				// occurred_at DESC order; GET .../estado derives the summary per
				// kind (last event of each); DELETE removes a single event (owner
				// correction, not a mass purge).
				ent.Post("/{id}/historico", s.createHistoricoEvento)
				ent.Get("/{id}/historico", s.listHistoricoEventos)
				ent.Get("/{id}/historico/estado", s.getHistoricoEstado)
				ent.Delete("/{id}/historico/{eventoId}", s.deleteHistoricoEvento)
			})

			// JOB system: job types registered in code (Handler/Registry), dynamic
			// configuration (enabled/schedule) and generic history in the database,
			// with an embedded scheduler (goroutine + ticker, no OS cron). It lists
			// and edits the config, triggers a manual run and lists the paginated
			// history — same standard error envelope as the rest of the API.
			v1.Route("/jobs", func(jobs chi.Router) {
				jobs.Get("/", s.listJobs)
				jobs.Patch("/{id}", s.patchJob)
				jobs.Post("/{id}/run", s.runJob)
				jobs.Get("/{id}/executions", s.listJobExecutions)
			})

			// Jellyfin webhook (ADR-0022, depends on the consumption history
			// above): it receives the playback notification from the
			// `jellyfin-plugin-webhook` plugin (authenticated by ?token=,
			// constant-time comparison) and writes a historico_evento when the item
			// matches by external id. The token endpoints (generate/read) are
			// internal, without a token of their own -- it is the owner's panel
			// operating.
			v1.Route("/webhooks/jellyfin", func(wh chi.Router) {
				wh.Post("/", s.receiveJellyfinWebhook)
				wh.Post("/token", s.regenerateJellyfinWebhookToken)
				wh.Get("/token", s.getJellyfinWebhookToken)
			})
			// Orphan history events: GLOBAL route, outside /entities/{id} -- by
			// definition an orphan does not have an entity yet.
			v1.Route("/historico", func(h chi.Router) {
				h.Get("/orfaos", s.listHistoricoOrfaos)
			})
		})
	})

	// SPA: every remaining route serves embedded static files, with a fallback
	// to index.html (deep links of the client-side routing).
	spa, err := newSPAHandler()
	if err != nil {
		return nil, err
	}
	r.Handle("/*", spa)

	return r, nil
}

// apiNotFound responds 404 in JSON for any unresolved /api route, already in
// the standard error shape ({error:{code,message}}) used by all /api/v1 errors.
// (It used to respond {"error":"not found"}, an old/inconsistent shape, now
// retired.)
func apiNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "recurso não encontrado")
}
