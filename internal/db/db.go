// Package db is the persistence foundation of MyMediaDB on top of SQLite. It
// encapsulates the concurrency recipe ratified in ADR-0010 (WAL + pragmas, a
// read pool with N connections and a single writer in a pool of size 1) and the
// application of the embedded migrations via goose (ADR-0017), all with the
// CGO-free modernc.org/sqlite driver. Upcoming work only needs to add tables and
// queries -- the plumbing lives here.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go SQLite, no CGO)

	"github.com/rafaelvieiras/my-media-db/internal/db/migrations"
)

// driverName is the name under which modernc.org/sqlite registers itself in
// database/sql. Not to be confused with "sqlite3" (that is the goose dialect).
const driverName = "sqlite"

// migrationsDir is the directory name inside the migrations embed.FS. goose
// expects the path relative to the root of the embedded FS.
const migrationsDir = "."

// defaultMaxReadConns is the default size of the read pool when Config does not
// specify a value. A handful of concurrent readers is enough for self-hosted
// use; writes are always serialized in a pool of size 1.
const defaultMaxReadConns = 4

// pragmas are the ADR-0010 PRAGMAs applied to EVERY new connection. The modernc
// driver runs each `_pragma=...` of the DSN on every connection opened by the
// pool -- that is why they hold for the whole pool, not a single time.
//
//	journal_mode=WAL   -> several readers + one writer without mutual blocking
//	busy_timeout=5000  -> waits up to 5s for a lock instead of failing at once
//	synchronous=NORMAL -> safe under WAL; scan data is re-derivable
//	foreign_keys=ON    -> referential integrity active (the SQLite default is OFF)
var pragmas = []string{
	"_pragma=journal_mode(WAL)",
	"_pragma=busy_timeout(5000)",
	"_pragma=synchronous(NORMAL)",
	"_pragma=foreign_keys(ON)",
}

// Config parameterizes the opening of the database.
type Config struct {
	// Path is the path of the .sqlite file. Required. The parent directory is
	// created if it does not exist.
	Path string
	// MaxReadConns is the maximum number of connections in the read pool. If <= 0,
	// defaultMaxReadConns is used.
	MaxReadConns int
}

// DB aggregates the two pools over the SAME file: Read for concurrent reads and
// Write for the single writer (SetMaxOpenConns(1)). Keeping the pools apart is
// what guarantees, together with WAL, that a write in progress does not cause
// `database is locked` on the reads (ADR-0010).
type DB struct {
	// Read is the read pool (N connections).
	Read *sql.DB
	// Write is the write pool (exactly 1 connection = single writer).
	Write *sql.DB
}

// gooseOnce guarantees that the global goose setup (embedded FS + dialect) is
// done a single time per process, since the goose library API is based on global
// state.
var gooseOnce sync.Once
var gooseErr error

// setupGoose configures goose to read the embedded migrations with the SQLite
// dialect. Idempotent and safe for concurrent use.
func setupGoose() error {
	gooseOnce.Do(func() {
		goose.SetBaseFS(migrations.FS)
		gooseErr = goose.SetDialect("sqlite3")
	})
	return gooseErr
}

// dsn builds the modernc DSN for a file path, appending the ADR-0010 PRAGMAs as
// `_pragma` parameters -- applied to every new connection of the pool.
func dsn(path string) string {
	return path + "?" + strings.Join(pragmas, "&")
}

// Open opens both pools (read and write) over the cfg.Path file, applying the
// concurrency PRAGMAs to every connection. It creates the parent directory if
// needed. It does not run migrations -- call Migrate next.
func Open(cfg Config) (*DB, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("db: Config.Path vazio")
	}
	if dir := filepath.Dir(cfg.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("db: não foi possível criar o diretório %q: %w", dir, err)
		}
	}

	maxRead := cfg.MaxReadConns
	if maxRead <= 0 {
		maxRead = defaultMaxReadConns
	}

	connStr := dsn(cfg.Path)

	read, err := sql.Open(driverName, connStr)
	if err != nil {
		return nil, fmt.Errorf("db: abrir pool de leitura: %w", err)
	}
	read.SetMaxOpenConns(maxRead)

	write, err := sql.Open(driverName, connStr)
	if err != nil {
		_ = read.Close()
		return nil, fmt.Errorf("db: abrir pool de escrita: %w", err)
	}
	// Single writer: at most one write connection, serializing the writes at the
	// application level (ADR-0010).
	write.SetMaxOpenConns(1)

	// Materialize at least one connection of each pool to fail early (and so that
	// the PRAGMAs -- especially journal_mode=WAL, which is persisted in the
	// file -- already hold from the start).
	if err := write.Ping(); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, fmt.Errorf("db: ping do pool de escrita: %w", err)
	}
	if err := read.Ping(); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, fmt.Errorf("db: ping do pool de leitura: %w", err)
	}

	return &DB{Read: read, Write: write}, nil
}

// Migrate applies every pending migration through the WRITE connection (the
// single writer). Fail-fast: any error is propagated to abort the boot.
func (d *DB) Migrate(ctx context.Context) error {
	if err := setupGoose(); err != nil {
		return fmt.Errorf("db: configurar goose: %w", err)
	}
	if err := goose.UpContext(ctx, d.Write, migrationsDir); err != nil {
		return fmt.Errorf("db: aplicar migrations: %w", err)
	}
	return nil
}

// MigrateDown reverts the last applied migration (development use; in
// production the policy is forward only -- ADR-0017).
func (d *DB) MigrateDown(ctx context.Context) error {
	if err := setupGoose(); err != nil {
		return fmt.Errorf("db: configurar goose: %w", err)
	}
	if err := goose.DownContext(ctx, d.Write, migrationsDir); err != nil {
		return fmt.Errorf("db: reverter migration: %w", err)
	}
	return nil
}

// SchemaVersion returns the current schema version according to goose (the
// highest migration number already applied). Useful for health and diagnostics.
func (d *DB) SchemaVersion(ctx context.Context) (int64, error) {
	if err := setupGoose(); err != nil {
		return 0, fmt.Errorf("db: configurar goose: %w", err)
	}
	v, err := goose.GetDBVersionContext(ctx, d.Write)
	if err != nil {
		return 0, fmt.Errorf("db: obter versão de schema: %w", err)
	}
	return v, nil
}

// Ping verifies the connectivity of the read pool.
func (d *DB) Ping(ctx context.Context) error {
	return d.Read.PingContext(ctx)
}

// Close closes both pools. It returns the first error found, but tries to close
// both anyway.
func (d *DB) Close() error {
	var errs []error
	if d.Read != nil {
		if err := d.Read.Close(); err != nil {
			errs = append(errs, fmt.Errorf("fechar pool de leitura: %w", err))
		}
	}
	if d.Write != nil {
		if err := d.Write.Close(); err != nil {
			errs = append(errs, fmt.Errorf("fechar pool de escrita: %w", err))
		}
	}
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("db: %s", strings.Join(msgs, "; "))
	}
	return nil
}
