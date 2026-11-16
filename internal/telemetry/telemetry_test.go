package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupBanco brings up a migrated test database, isolated per test -- same
// pattern as internal/curation/curation_test.go.
func setupBanco(t *testing.T) *db.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "telemetry.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}

func semearEntity(t *testing.T, d *db.DB, id, tipo, titulo string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipo, Title: titulo,
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

func semearFile(t *testing.T, d *db.DB, id, entityID, path, state string, size int64, sourceID string) {
	t.Helper()
	params := sqlcgen.CriarMediaFileParams{
		ID: id, EntityID: entityID, Path: path, State: state,
		Size: sql.NullInt64{Int64: size, Valid: true},
	}
	if sourceID != "" {
		params.LibrarySourceID = sql.NullString{String: sourceID, Valid: true}
	}
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), params); err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

func semearLibrarySource(t *testing.T, d *db.DB, id, path string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarLibrarySource(context.Background(), sqlcgen.CriarLibrarySourceParams{
		ID: id, Path: path, Type: "movie", ContentType: "movie",
	}); err != nil {
		t.Fatalf("CriarLibrarySource(%s): %v", id, err)
	}
}

// semearJobExecucao inserts a job history item directly into queue_item
// (kind='job') -- since ADR-0028 (LAYER 3), ContarJobExecutions counts
// queue_item, no longer job_execution (dropped by migration 0025).
func semearJobExecucao(t *testing.T, d *db.DB, id, jobID string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarQueueItem(context.Background(), sqlcgen.CriarQueueItemParams{
		ID: id, Kind: "job", RefID: jobID, Trigger: "manual",
	}); err != nil {
		t.Fatalf("CriarQueueItem(%s): %v", id, err)
	}
}

// contadorFake is a fake of Contador injectable in tests, without depending on
// internal/curation (avoids an import cycle and allows controlling the numbers
// and the error returned).
type contadorFake struct {
	revisao    int
	orfas      int
	errRevisao error
	errOrfas   error
}

func (c *contadorFake) ContarRevisao(ctx context.Context) (int, error) {
	if c.errRevisao != nil {
		return 0, c.errRevisao
	}
	return c.revisao, nil
}

func (c *contadorFake) ContarOrfas(ctx context.Context) (int, error) {
	if c.errOrfas != nil {
		return 0, c.errOrfas
	}
	return c.orfas, nil
}

// TestTamanhoBancoBateComPragma proves that DatabaseSize.SizeBytes == page_count
// * page_size read directly by PRAGMA.
func TestTamanhoBancoBateComPragma(t *testing.T) {
	d := setupBanco(t)
	s := New(d, nil, time.Now())
	ctx := context.Background()

	tamanho, err := s.TamanhoBanco(ctx)
	if err != nil {
		t.Fatalf("TamanhoBanco: %v", err)
	}

	var pageCount, pageSize int64
	if err := d.Read.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		t.Fatalf("PRAGMA page_count direto: %v", err)
	}
	if err := d.Read.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatalf("PRAGMA page_size direto: %v", err)
	}

	if tamanho.PageCount != pageCount || tamanho.PageSize != pageSize {
		t.Fatalf("PageCount/PageSize = %d/%d, quero %d/%d", tamanho.PageCount, tamanho.PageSize, pageCount, pageSize)
	}
	if tamanho.SizeBytes != pageCount*pageSize {
		t.Fatalf("SizeBytes = %d, quero %d", tamanho.SizeBytes, pageCount*pageSize)
	}
	if tamanho.SchemaVersion <= 0 {
		t.Fatalf("SchemaVersion = %d, quero > 0 (migrations ja aplicadas)", tamanho.SchemaVersion)
	}
}

// TestContagensUsoReflete proves that EntitiesByType/MediaFiles reflect exactly
// the seeded fixture (movie/season/episode/collection mixed, present+missing)
// and that revisao/orfas come from the injected Contador -- without
// duplicating the predicate.
func TestContagensUsoReflete(t *testing.T) {
	d := setupBanco(t)
	ctx := context.Background()

	semearEntity(t, d, "e-movie-1", "movie", "Filme 1")
	semearEntity(t, d, "e-movie-2", "movie", "Filme 2")
	semearEntity(t, d, "e-season-1", "season", "Temporada 1")
	semearEntity(t, d, "e-episode-1", "episode", "Episodio 1")
	semearEntity(t, d, "e-collection-1", "collection", "Colecao 1")

	semearFile(t, d, "f1", "e-movie-1", "/x/f1.mkv", "present", 100, "")
	semearFile(t, d, "f2", "e-movie-2", "/x/f2.mkv", "missing", 200, "")
	semearFile(t, d, "f3", "e-episode-1", "/x/f3.mkv", "present", 300, "")

	fake := &contadorFake{revisao: 5, orfas: 2}
	s := New(d, fake, time.Now().Add(-90*time.Second))

	uso, err := s.ContagensUso(ctx)
	if err != nil {
		t.Fatalf("ContagensUso: %v", err)
	}

	if uso.EntitiesTotal != 5 {
		t.Fatalf("EntitiesTotal = %d, quero 5", uso.EntitiesTotal)
	}
	wantByType := map[string]int64{"movie": 2, "season": 1, "episode": 1, "collection": 1}
	for tipo, total := range wantByType {
		if uso.EntitiesByType[tipo] != total {
			t.Fatalf("EntitiesByType[%s] = %d, quero %d", tipo, uso.EntitiesByType[tipo], total)
		}
	}
	if len(uso.EntitiesByType) != len(wantByType) {
		t.Fatalf("EntitiesByType tem %d chaves, quero %d (so tipos que ocorrem)", len(uso.EntitiesByType), len(wantByType))
	}

	if uso.MediaFilesTotal != 3 || uso.MediaFilesPresent != 2 || uso.MediaFilesMissing != 1 {
		t.Fatalf("MediaFiles = total:%d present:%d missing:%d, quero 3/2/1",
			uso.MediaFilesTotal, uso.MediaFilesPresent, uso.MediaFilesMissing)
	}

	if uso.ReviewQueueSize != 5 || uso.OrphansCount != 2 {
		t.Fatalf("ReviewQueueSize/OrphansCount = %d/%d, quero 5/2 (do Contador injetado)",
			uso.ReviewQueueSize, uso.OrphansCount)
	}

	if uso.UptimeSeconds < 90 {
		t.Fatalf("UptimeSeconds = %d, quero >= 90 (iniciadoEm 90s atras)", uso.UptimeSeconds)
	}

	if uso.Goroutines <= 0 {
		t.Fatalf("Goroutines = %d, quero > 0", uso.Goroutines)
	}
}

// TestContagensUsoContadorNil proves that a nil Contador degrades to 0/0
// instead of panicking (optional collaborator, the same defensive spirit of
// libraryhealth.Handler.rodarAutoMatch).
func TestContagensUsoContadorNil(t *testing.T) {
	d := setupBanco(t)
	s := New(d, nil, time.Now())

	uso, err := s.ContagensUso(context.Background())
	if err != nil {
		t.Fatalf("ContagensUso: %v", err)
	}
	if uso.ReviewQueueSize != 0 || uso.OrphansCount != 0 {
		t.Fatalf("ReviewQueueSize/OrphansCount = %d/%d, quero 0/0 (sem Contador)", uso.ReviewQueueSize, uso.OrphansCount)
	}
}

// TestContagensUsoErroDoContadorPropaga proves that a failure of the injected
// Contador propagates as an error (the handler decides to degrade to 503, this
// package simply does not hide the problem).
func TestContagensUsoErroDoContadorPropaga(t *testing.T) {
	d := setupBanco(t)
	fake := &contadorFake{errRevisao: errors.New("falha simulada")}
	s := New(d, fake, time.Now())

	if _, err := s.ContagensUso(context.Background()); err == nil {
		t.Fatal("ContagensUso deveria propagar o erro do Contador")
	}
}

// TestUsoDiscoSomaSoPresenteDaFonte proves that UsoDisco adds up ONLY
// media_file.size with state='present' filtered by library_source_id,
// ignoring files of another source or missing.
func TestUsoDiscoSomaSoPresenteDaFonte(t *testing.T) {
	d := setupBanco(t)
	ctx := context.Background()
	s := New(d, nil, time.Now())

	semearLibrarySource(t, d, "src-a", "/mnt/a")
	semearLibrarySource(t, d, "src-b", "/mnt/b")

	semearEntity(t, d, "e1", "movie", "Filme A1")
	semearEntity(t, d, "e2", "movie", "Filme A2")
	semearEntity(t, d, "e3", "movie", "Filme B1")

	semearFile(t, d, "f1", "e1", "/mnt/a/f1.mkv", "present", 1000, "src-a")
	semearFile(t, d, "f2", "e2", "/mnt/a/f2.mkv", "missing", 5000, "src-a") // missing: not added up
	semearFile(t, d, "f3", "e3", "/mnt/b/f3.mkv", "present", 2000, "src-b") // another source: not added up

	total, err := s.UsoDisco(ctx, "src-a")
	if err != nil {
		t.Fatalf("UsoDisco: %v", err)
	}
	if total != 1000 {
		t.Fatalf("UsoDisco(src-a) = %d, quero 1000", total)
	}
}

// TestUsoDiscoFonteSemArquivo proves that a source without any media_file
// returns 0 (COALESCE), not an error/NULL.
func TestUsoDiscoFonteSemArquivo(t *testing.T) {
	d := setupBanco(t)
	s := New(d, nil, time.Now())

	total, err := s.UsoDisco(context.Background(), "fonte-inexistente")
	if err != nil {
		t.Fatalf("UsoDisco: %v", err)
	}
	if total != 0 {
		t.Fatalf("UsoDisco = %d, quero 0", total)
	}
}

// TestEstatisticasFilesystemPathInexistenteDevolveErro proves that an
// inaccessible path returns an error (no panic, no invented value) -- the
// caller (handler) uses it to mark offline:true/filesystem:null.
func TestEstatisticasFilesystemPathInexistenteDevolveErro(t *testing.T) {
	if _, err := EstatisticasFilesystem("/caminho/que/definitivamente/nao/existe/xyz123"); err == nil {
		t.Fatal("EstatisticasFilesystem deveria devolver erro para path inexistente")
	}
}

// TestEstatisticasFilesystemPathValido proves the happy path: a path that
// exists (t.TempDir()) returns free/total > 0.
func TestEstatisticasFilesystemPathValido(t *testing.T) {
	fs, err := EstatisticasFilesystem(t.TempDir())
	if err != nil {
		t.Fatalf("EstatisticasFilesystem: %v", err)
	}
	if fs.TotalBytes == 0 {
		t.Fatal("TotalBytes = 0, quero > 0 para um filesystem real")
	}
}

// TestSnapshotComputedAtAvanca proves that two successive calls to Snapshot
// (no cache -- caching is the responsibility of internal/server) produce
// distinct ComputedAt values (each call computes on the spot).
func TestSnapshotComputedAtAvanca(t *testing.T) {
	d := setupBanco(t)
	s := New(d, &contadorFake{}, time.Now())
	ctx := context.Background()

	snap1, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot 1: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	snap2, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot 2: %v", err)
	}
	if !snap2.ComputedAt.After(snap1.ComputedAt) {
		t.Fatalf("ComputedAt nao avancou entre chamadas: %v -> %v", snap1.ComputedAt, snap2.ComputedAt)
	}
}

// TestContarJobExecutions proves that the count of queue_item (kind='job')
// matches the number of inserted rows (beyond the job_definition seeds, which
// do not count here -- only the execution history).
func TestContarJobExecutions(t *testing.T) {
	d := setupBanco(t)
	s := New(d, nil, time.Now())
	ctx := context.Background()

	semearJobExecucao(t, d, "exec-1", "backup_vacuum")
	semearJobExecucao(t, d, "exec-2", "backup_vacuum")
	semearJobExecucao(t, d, "exec-3", "saude_biblioteca")

	uso, err := s.ContagensUso(ctx)
	if err != nil {
		t.Fatalf("ContagensUso: %v", err)
	}
	if uso.JobExecutionsTotal != 3 {
		t.Fatalf("JobExecutionsTotal = %d, quero 3", uso.JobExecutionsTotal)
	}
}
