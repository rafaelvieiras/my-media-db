// Command mymediadb: entrypoint of the single HTTP server that serves the API and
// the SPA embedded in the same origin. It reads the port from env/flag, starts the
// server and performs a graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/fingerprint"
	"github.com/rafaelvieiras/my-media-db/internal/integration"
	"github.com/rafaelvieiras/my-media-db/internal/job/backup"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/acoustid"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fanarttv"
	"github.com/rafaelvieiras/my-media-db/internal/provider/musicbrainz"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tvdb"
	"github.com/rafaelvieiras/my-media-db/internal/server"
	"github.com/rafaelvieiras/my-media-db/internal/ytdlp"
)

// defaultDBPath is the default database path in development: inside tmp/,
// ignored by git. In production, set MYMEDIADB_DB_PATH.
const defaultDBPath = "tmp/mymediadb.sqlite"

// version is the binary version. Defaults to "dev"; overridden at build time
// with -ldflags "-X main.version=<v>".
var version = "dev"

// shutdownTimeout caps how long the server waits to drain in-flight connections
// during shutdown.
const shutdownTimeout = 10 * time.Second

// migrateTimeout caps how long the migrations may take at boot.
const migrateTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatalf("mymediadb: erro fatal: %v", err)
	}
}

func run() error {
	addr := ":" + resolvePort()

	// Opens the database and applies the migrations BEFORE accepting traffic. A
	// failure to open or to migrate aborts the boot (fail-fast — ADR-0017).
	database, err := openAndMigrate()
	if err != nil {
		return err
	}
	defer func() {
		if cerr := database.Close(); cerr != nil {
			log.Printf("mymediadb: erro ao fechar o banco: %v", cerr)
		}
	}()

	// Unified execution queue for scan and jobs (ADR-0028, the final LAYER 3):
	// ONE single shared *execqueue.Manager, over the SAME *db.DB the other
	// managers already receive. Built BEFORE server.New
	// (server.WithExecQueue, below, needs it to wire the scanner) and BEFORE
	// srv.Jobs().ConfigurarFila (wires the job) -- both wirings (which register
	// the Runners through RegistrarRunner) must happen before `go
	// fila.Run(ctx)` starts processing items, further below, otherwise an item
	// that arrived too early would hit the "no runner registered" path
	// (execqueue already handles that without blocking the worker, but it is
	// wasteful and confuses the log).
	fila := execqueue.NewManager(database)

	opts, err := opcoesServer(database)
	if err != nil {
		return err
	}
	opts = append(opts, server.WithExecQueue(fila))
	srv := server.New(version, database, opts...)
	// srv.Jobs() is never nil here — database (above) is always non-nil when
	// the boot reaches this point, and opcoesServer always includes
	// WithJobHandlers (backup_vacuum), which builds the jobs Manager.
	srv.Jobs().ConfigurarFila(fila)

	handler, err := srv.Handler()
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Termination signal (SIGINT/SIGTERM) — the SAME ctx stops the queue
	// worker (fila.Run, below), the jobs scheduler (RunScheduler) and the
	// HTTP shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Sequential worker of the unified queue (ADR-0028): consumes scan and job
	// in FIFO order, concurrency 1. It starts BEFORE the RunScheduler below
	// (which only ENQUEUES through job.Manager.Executar from this layer on --
	// the one that actually runs the Handler is this worker) -- both Runners
	// (scan and job) were already registered above (WithExecQueue /
	// ConfigurarFila), so there is no race between "worker starts processing"
	// and "runner is registered".
	go fila.Run(ctx)

	// Embedded jobs scheduler: it fires BEFORE ListenAndServe, so the HTTP boot
	// does not delay the first tick. It exits on its own when ctx is canceled
	// (the same signal as the HTTP shutdown).
	go srv.Jobs().RunScheduler(ctx)

	// Optional per-source filesystem watch: registers the fsnotify watcher of
	// each library_source with watch_enabled=1 BEFORE ListenAndServe (the same
	// spirit as RunScheduler above — it does not delay the HTTP boot;
	// best-effort, it never aborts because of ONE source).
	// FecharWatchers on the graceful shutdown, below.
	srv.IniciarWatchers(context.Background())
	defer srv.FecharWatchers()

	// Channel to propagate a ListenAndServe error to the main flow.
	serverErr := make(chan error, 1)

	go func() {
		log.Printf("mymediadb: iniciando (versão=%s) escutando em %s", version, addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		stop() // restores the default signal behavior
		log.Printf("mymediadb: sinal recebido, encerrando (graceful shutdown, timeout=%s)", shutdownTimeout)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		log.Printf("mymediadb: encerrado com sucesso")
		return nil
	}
}

// openAndMigrate opens the database at the configured path and applies the pending
// migrations. On success, it logs the schema version. An error aborts the boot.
//
// The order matters and is the whole point of an unattended host (a NAS upgrading the
// image on a schedule): read the plan, REFUSE a database newer than this binary, copy
// the database, and only then migrate. The copy is what makes an upgrade reversible.
func openAndMigrate() (*db.DB, error) {
	database, err := db.Open(db.Config{Path: resolveDBPath()})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	atual, alvo, err := database.PlanoDeMigracao(ctx)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	// Downgrade: the owner rolled the image back after an automatic upgrade. Refusing to
	// start is the only safe answer -- code from before the migration does not know the
	// columns it would now read and write.
	if err := db.VerificarDowngrade(atual, alvo); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := copiarAntesDeMigrar(database, ctx, atual, alvo); err != nil {
		_ = database.Close()
		return nil, err
	}

	if err := database.Migrate(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	if v, verr := database.SchemaVersion(ctx); verr == nil {
		log.Printf("mymediadb: banco aberto e migrado (schema=%d)", v)
	}
	return database, nil
}

// copiarAntesDeMigrar writes the pre-migration copy and logs where it went. Skipping is
// allowed through MYMEDIADB_SKIP_PRE_MIGRATION_BACKUP=1 (a disk with no room, or a
// throwaway instance), but it is LOUD: the migration is then irreversible.
func copiarAntesDeMigrar(database *db.DB, ctx context.Context, atual, alvo int64) error {
	if atual == 0 || atual >= alvo {
		return nil
	}
	if os.Getenv("MYMEDIADB_SKIP_PRE_MIGRATION_BACKUP") == "1" {
		log.Printf("mymediadb: AVISO banco sera migrado de %d para %d SEM copia de seguranca (MYMEDIADB_SKIP_PRE_MIGRATION_BACKUP=1)", atual, alvo)
		return nil
	}
	dir, err := resolveBackupDir()
	if err != nil {
		return err
	}
	manter := 0
	if v := os.Getenv("MYMEDIADB_PRE_MIGRATION_BACKUPS"); v != "" {
		if n, convErr := strconv.Atoi(v); convErr == nil {
			manter = n
		}
	}
	caminho, err := database.BackupAntesDeMigrar(ctx, dir, atual, alvo, manter)
	if err != nil {
		// No copy, no migration: an upgrade nobody is watching must be reversible.
		return err
	}
	if caminho != "" {
		log.Printf("mymediadb: copia do banco antes da migracao (%d -> %d) em %q", atual, alvo, caminho)
	}
	return nil
}

// opcoesServer assembles the server options: the external search/enrich, the
// assets directory, the seed jobs backup_vacuum, saude_biblioteca and
// midias_faltantes + filesystem watch. It only errors while creating the
// directories (assets/backups).
func opcoesServer(database *db.DB) ([]server.Option, error) {
	opts := opcoesBusca(database)

	// Foundation of integrations configurable through the UI/database
	// (ADR-0023): registers the TMDB field schema in the Registry consumed by
	// GET/PUT /api/v1/integrations. Only TMDB in this delivery -- TVDB/
	// fanart.tv keep going through the env var directly (out of scope, see the
	// ADR). AcoustID: registers its field schema (client key) in the same
	// Registry -- the Integrations UI inherits the entry "for free" through the
	// generic dynamic form, with no dedicated screen code.
	opts = append(opts, server.WithIntegrations(tmdb.NovoDescriptorIntegracao(), acoustid.NovoDescriptorIntegracao()))

	// CODEC registry (ADR-0024): always populated, regardless of credential, it
	// only enables the capability of INTERPRETING an already cached payload
	// (never a live search), so that cataloged data survives the absence or
	// revocation of a key. OUTSIDE fake mode (E2E): there the search registry
	// (fake.New, in opcoesBusca) already answers for "tmdb" on its own --
	// registering the real tmdb codec here as well would make codecPara
	// prioritize it over the fake and break the parsing of the fixtures.
	if os.Getenv("MYMEDIADB_FAKE_PROVIDER") != "1" {
		opts = append(opts, server.WithCodecRegistry(provedoresConhecidos()...))
	}

	dir, err := resolveAssetsDir()
	if err != nil {
		return nil, err
	}
	log.Printf("mymediadb: diretório de assets em %q", dir)
	opts = append(opts, server.WithAssetsDir(dir))

	backupDir, err := resolveBackupDir()
	if err != nil {
		return nil, err
	}
	log.Printf("mymediadb: diretório de backups em %q (job backup_vacuum)", backupDir)
	opts = append(opts, server.WithJobHandlers(backup.New(database.Write, backupDir)))

	// Manager for the yt-dlp binary (ADR-0025): foundation for the trailer and
	// the theme music. Data directory following the same pattern as
	// resolveAssetsDir/resolveBackupDir; the admin's custom path (env
	// MYMEDIADB_YTDLP_PATH) is resolved HERE and passed in ready-made -- the
	// internal/ytdlp package never reads os.Getenv by itself (it stays
	// testable). The override through the database (app_settings, Integrations
	// UI) takes precedence and is synced afterwards, at Server boot
	// (montarServicos).
	ytdlpDir, err := resolveYtdlpDir()
	if err != nil {
		return nil, err
	}
	log.Printf("mymediadb: diretório do yt-dlp em %q", ytdlpDir)
	opts = append(opts, server.WithYtdlp(ytdlp.Novo(ytdlpDir, os.Getenv("MYMEDIADB_YTDLP_PATH"))))

	// saude_biblioteca job: registered through a DEDICATED option (not
	// WithJobHandlers) because the Handler needs scanner.Manager and
	// library.Service, internal to the Server — see the comment on
	// WithLibraryHealthJob in internal/server/server.go.
	opts = append(opts, server.WithLibraryHealthJob())

	// midias_faltantes job + optional per-source filesystem watch: the same
	// reason for a DEDICATED option as saude_biblioteca above — both need
	// dependencies internal to the Server. The effective boot of the watch
	// (listing the sources with watch_enabled=1 and registering each one)
	// happens in srv.IniciarWatchers, called from run() after server.New (see
	// the comment there).
	opts = append(opts, server.WithMidiasFaltantesJob(), server.WithIdentidadesJob(), server.WithWatch())

	// trailer_tema_faltantes job (ADR-0026): the same reason for a DEDICATED
	// option -- the Handler needs the *Server itself as
	// EntityLister/TrailerTemaBaixador.
	opts = append(opts, server.WithTrailerTemaJob())

	return opts, nil
}

// opcoesBusca assembles the option for the external sources: it REGISTERS each
// configured provider in the multi-provider registry. TMDB and TheTVDB can COEXIST (the
// search queries both and merges; the rest picks the source by name).
//
// - TMDB: MYMEDIADB_TMDB_TOKEN (Read Access Token v4, preferred) or MYMEDIADB_TMDB_API_KEY (v3).
// - TheTVDB: MYMEDIADB_TTDB_API_KEY (project apikey, v4 login).
//
// With NO credential at all, it registers no provider (external search off — not an error;
// enrich/import are off too). It NEVER logs the credential value, only on/off.
//
// MYMEDIADB_FAKE_PROVIDER=1 takes PRECEDENCE and injects the deterministic fixtures
// provider, EXCLUSIVELY: the binary runs the whole flow with no real source and no
// secret, for the E2E tests (Layer 2) and manual inspection. The images come from
// MYMEDIADB_FAKE_IMAGE_BASE (a fixtures server); empty => no poster.
func opcoesBusca(database *db.DB) []server.Option {
	if os.Getenv("MYMEDIADB_FAKE_PROVIDER") == "1" {
		log.Printf("mymediadb: PROVIDER FAKE de fixtures habilitado (E2E; sem fonte real)")
		// The music fake runs alongside the movie/series fake under
		// Nome()="musicbrainz" — the same fixed data source that
		// internal/e2e uses in Layer 1, now also reachable from the real
		// binary that Layer 2 (Playwright) drives.
		return []server.Option{server.WithSearchProvider(
			fake.New(os.Getenv("MYMEDIADB_FAKE_IMAGE_BASE")),
			fake.NewMusicbrainz(),
		)}
	}
	provs := provedoresReais(context.Background(), integration.NovoResolver(database))
	if len(provs) == 0 {
		log.Printf("mymediadb: busca externa desabilitada (sem credencial)")
		return nil
	}
	return []server.Option{server.WithSearchProvider(provs...)}
}

// provedoresReais builds the real providers whose credentials are configured
// (TMDB and/or TheTVDB), in the default priority order. Empty when none exists.
// TMDB resolves the credential through integration.Resolver (ADR-0023): the
// database (integration_config) takes PRECEDENCE over the env var -- the same
// result as before when the database is empty (it falls back to the env var),
// but already reading through the new layer. TVDB/fanart.tv did NOT migrate in
// this delivery (out of scope) -- they keep going through os.Getenv directly.
func provedoresReais(ctx context.Context, resolver *integration.Resolver) []provider.Provider {
	var provs []provider.Provider

	descTmdb := tmdb.NovoDescriptorIntegracao()
	efetivosTmdb := resolver.Valores(ctx, descTmdb.Slug(), descTmdb.Campos())
	credTmdb := tmdb.Credencial{
		Token:  efetivosTmdb["token"],
		APIKey: efetivosTmdb["api_key"],
	}
	if credTmdb.Configurada() {
		log.Printf("mymediadb: busca externa TMDB habilitada")
		provs = append(provs, tmdb.New(credTmdb, nil, ""))
	}

	credTvdb := tvdb.Credencial{APIKey: os.Getenv("MYMEDIADB_TTDB_API_KEY")}
	if credTvdb.Configurada() {
		log.Printf("mymediadb: busca externa TheTVDB habilitada")
		provs = append(provs, tvdb.New(credTvdb, nil, ""))
	}

	// fanart.tv: provider DEDICATED TO ARTWORK (never a search source). It enters
	// the registry by capability (ArteProvider) when the PERSONAL key is in the
	// environment; without it, it is silently omitted (zero regression). The key only
	// comes from the env, never hardcoded.
	credFanart := fanarttv.Credencial{APIKey: os.Getenv("MYMEDIADB_FANARTTV_API_KEY")}
	if credFanart.Configurada() {
		log.Printf("mymediadb: enriquecimento de arte fanart.tv habilitado")
		provs = append(provs, fanarttv.New(credFanart, nil, ""))
	}

	// MusicBrainz + Cover Art Archive (ADR-0027 item 6): BASE provider of the
	// music vertical (search/import of artist->album->track + album cover), with NO
	// API key -- always available, the same role TheTVDB has for series. Registered
	// UNCONDITIONALLY (no credential guard): the music research found that ThemerrDB
	// (the same "no key" pattern, ADR-0025) had ended up implemented but never
	// built/registered here -- do not repeat that gap.
	log.Printf("mymediadb: provider MusicBrainz (musica) habilitado")
	provs = append(provs, musicbrainz.New(nil, ""))

	// AcoustID: provider of the FingerprintProvider capability ONLY (acoustic
	// fingerprint through Chromaprint/fpcalc), the last fallback of the music
	// matching (step 4.5). It only enters the registry when BOTH dependencies are
	// present: the configured client key (database taking precedence over the env
	// var, the same resolver as always) AND the fpcalc binary available in the
	// container (fingerprint.Disponivel(), checked here at BOOT, never at build
	// time). If either one is missing, it DEGRADES (logs a warning, does not
	// register the capability) -- it never takes the boot down; the music scan
	// falls straight through to step 5 (draft) as before.
	descAcoustid := acoustid.NovoDescriptorIntegracao()
	efetivosAcoustid := resolver.Valores(ctx, descAcoustid.Slug(), descAcoustid.Campos())
	credAcoustid := acoustid.Credencial{ClientKey: efetivosAcoustid["client_key"]}
	switch {
	case !credAcoustid.Configurada():
		log.Printf("mymediadb: fingerprint acustico (AcoustID) desabilitado: client key nao configurada")
	case !fingerprint.NewCalculador().Disponivel():
		log.Printf("mymediadb: fingerprint acustico (AcoustID) desabilitado: fpcalc nao encontrado no PATH")
	default:
		log.Printf("mymediadb: fingerprint acustico (AcoustID) habilitado")
		provs = append(provs, acoustid.New(credAcoustid, nil, ""))
	}

	return provs
}

// provedoresConhecidos builds ALL the providers the binary knows how to
// interpret (tmdb/tvdb/fanarttv), with an EMPTY credential, INDEPENDENT of any
// configured key -- used only for the PARSE capability of an already cached
// payload (MapearDetalhe/Similares/TvdbCruzadoProvider), never for a live
// search. Registering them always guarantees that already cataloged data
// survives the absence or revocation of a credential (ADR-0024).
func provedoresConhecidos() []provider.Provider {
	return []provider.Provider{
		tmdb.New(tmdb.Credencial{}, nil, ""),
		tvdb.New(tvdb.Credencial{}, nil, ""),
		fanarttv.New(fanarttv.Credencial{}, nil, ""),
	}
}

// resolveAssetsDir determines the image cache directory: the env
// MYMEDIADB_ASSETS_DIR takes priority; when absent, it uses dirname(DB)/assets.
// It creates the directory if missing (the cache writes binaries there; the poster
// path guard accepts it as a trusted root).
func resolveAssetsDir() (string, error) {
	dir := os.Getenv("MYMEDIADB_ASSETS_DIR")
	if dir == "" {
		dir = filepath.Join(filepath.Dir(resolveDBPath()), "assets")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// resolveBackupDir determines the directory of the backup_vacuum job snapshots:
// the env MYMEDIADB_BACKUP_DIR takes priority; when absent, it uses
// dirname(DB)/backups — the same convention as resolveAssetsDir. It creates the
// directory if missing.
func resolveBackupDir() (string, error) {
	dir := os.Getenv("MYMEDIADB_BACKUP_DIR")
	if dir == "" {
		dir = filepath.Join(filepath.Dir(resolveDBPath()), "backups")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// resolveYtdlpDir determines the data directory of the yt-dlp binary downloaded
// dynamically (ADR-0025): the env MYMEDIADB_YTDLP_DIR takes priority; when
// absent, it uses dirname(DB)/bin -- the same convention as
// resolveAssetsDir/resolveBackupDir, reusing the volume that already exists
// today (no new volume to provision). It creates the directory if missing.
func resolveYtdlpDir() (string, error) {
	dir := os.Getenv("MYMEDIADB_YTDLP_DIR")
	if dir == "" {
		dir = filepath.Join(filepath.Dir(resolveDBPath()), "bin")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// resolvePort determines the listening port: the env PORT takes priority; when
// absent, it uses the default 8080.
func resolvePort() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}

// resolveDBPath determines the SQLite file path: the env MYMEDIADB_DB_PATH
// takes priority; when absent, it uses the development default in tmp/.
func resolveDBPath() string {
	if p := os.Getenv("MYMEDIADB_DB_PATH"); p != "" {
		return p
	}
	return defaultDBPath
}
