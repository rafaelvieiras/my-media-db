package scanner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
)

// ── test infrastructure ──────────────────────────────────────────────────────

// abrirDB opens a temporary database that has been migrated.
func abrirDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "scan.sqlite")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return d
}

// relogioCrescente returns an injectable clock that advances 1s on every call, to
// give distinct and deterministic scan timestamps.
func relogioCrescente() func() time.Time {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	var n int64
	return func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Second)
	}
}

// managerFake creates a Manager with a prober that does NOT run ffprobe (empty
// mediainfo), enough for the detection/dedup/lifecycle tests.
func managerFake(t *testing.T, d *db.DB) *Manager {
	t.Helper()
	m := NewManagerComProber(d, proberVazio{})
	m.agora = relogioCrescente()
	return m
}

// proberVazio simulates an unavailable ffprobe: mediainfo stays empty.
type proberVazio struct{}

func (proberVazio) Disponivel() bool                              { return false }
func (proberVazio) Probe(context.Context, string) ([]byte, error) { return nil, nil }

// criarSource registers a datasource pointing to dir (through the real service).
func criarSource(t *testing.T, d *db.DB, dir string) library.Source {
	t.Helper()
	src, err := library.New(d).Criar(context.Background(), dir, "movie", "", nil, nil)
	if err != nil {
		t.Fatalf("criar source: %v", err)
	}
	return src
}

// escrever creates a file with unique content (the path itself) so that the
// content_hash values differ between distinct files.
func escrever(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("conteudo-"+path), 0o644); err != nil {
		t.Fatal(err)
	}
}

// libraryGetterFake is a fake LibraryGetter that ALWAYS returns the same
// library.Source received at construction, regardless of the id passed to Obter --
// enough for the tests of this package, which call Manager.Executar directly (queue
// bypass) with the library.Source already in hand and only want to run the scan
// pipeline deterministically.
type libraryGetterFake struct {
	source library.Source
}

func (f libraryGetterFake) Obter(context.Context, string) (library.Source, error) {
	return f.source, nil
}

// progressoNoOp is the progress callback used by the tests of this package that
// call Manager.Executar directly and do not care about the intermediate reporting
// (see TestExecutarReportaProgressoPorArquivo, which DOES care and uses its own
// callback).
func progressoNoOp(int, int, string) {}

// scanSync runs the scan pipeline synchronously, bypassing the queue (execqueue):
// it calls Manager.Executar directly -- which resolves `src` through a fake
// LibraryGetter that always returns the very `src` received -- for deterministic
// assertions, without having to spin up a real execqueue.Manager nor a worker in its
// own goroutine.
func scanSync(t *testing.T, m *Manager, src library.Source) error {
	t.Helper()
	m.ConfigurarLibrary(libraryGetterFake{source: src})
	_, err := m.Executar(context.Background(), src.ID, TriggerManual, progressoNoOp)
	return err
}

// ── assertion helpers over the database ──────────────────────────────────────

func contar(t *testing.T, d *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("contar (%s): %v", query, err)
	}
	return n
}

// ── tests ────────────────────────────────────────────────────────────────────

// TestScanDetectaEntidadesEMediaFile covers the central criterion: the folder
// becomes ONE entity; the versions (qualities) become media_file of the SAME entity;
// the embedded id gives external_ids.tmdb + confidence 100; and the local poster
// becomes an asset.
func TestScanDetectaEntidadesEMediaFile(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Blade Runner 2049 (2017) [tmdbid-335984]")
	escrever(t, filepath.Join(pasta, "Blade Runner 2049 (2017) [tmdbid-335984] - 2160p.mkv"))
	escrever(t, filepath.Join(pasta, "Blade Runner 2049 (2017) [tmdbid-335984] - 1080p.mkv"))
	escrever(t, filepath.Join(pasta, "poster.jpg"))

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entities = %d, quero 1 (uma obra)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 2 {
		t.Errorf("media_file = %d, quero 2 (duas qualidades)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE external_id_tmdb = '335984'"); n != 1 {
		t.Errorf("entity com tmdb 335984 = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE match_confidence = 100"); n != 2 {
		t.Errorf("media_file com confiança 100 = %d, quero 2", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM asset WHERE kind='poster' AND origin='local'"); n != 1 {
		t.Errorf("asset poster local = %d, quero 1", n)
	}
	// library_source stamped online + last_scan.
	if n := contar(t, d, "SELECT count(*) FROM library_source WHERE state='online' AND last_scan IS NOT NULL"); n != 1 {
		t.Errorf("library_source não carimbado: %d", n)
	}
}

// TestReScanIdempotente proves that re-scanning duplicates nothing and that renaming
// a file (same content) updates the path instead of creating another row.
func TestReScanIdempotente(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Inception (2010) [tmdbid-27205]")
	arq := filepath.Join(pasta, "Inception (2010) [tmdbid-27205] - 1080p.mkv")
	escrever(t, arq)

	m := managerFake(t, d)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entities após re-scan = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file após re-scan = %d, quero 1", n)
	}

	// It renames it (same content). The dedup by hash must update the path.
	novo := filepath.Join(pasta, "Inception (2010) [tmdbid-27205] - 2160p.mkv")
	if err := os.Rename(arq, novo); err != nil {
		t.Fatal(err)
	}
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 3: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file após rename = %d, quero 1 (path atualizado, não duplicado)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE path = ?", novo); n != 1 {
		t.Errorf("path não foi atualizado para o novo nome")
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE state='present'"); n != 1 {
		t.Errorf("arquivo renomeado deveria seguir present, present=%d", n)
	}
}

// TestConteudoTrocadoNoMesmoPathNaoDuplica proves that, when the recomputed
// content_hash diverges from what was stored for the SAME path (a file
// replaced/reencoded in place, or mtime/size of the provider drifted enough to
// invalidate the incremental short-circuit), the scanner UPDATES the existing row
// instead of trying an INSERT that would hit the UNIQUE(path).
func TestConteudoTrocadoNoMesmoPathNaoDuplica(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "Filme (2000)", "Filme (2000).mkv")
	escrever(t, arq)

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	var hashAntigo string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT content_hash FROM media_file").Scan(&hashAntigo); err != nil {
		t.Fatalf("ler hash: %v", err)
	}

	// It replaces the content AT THE SAME path (different size, so the size/mtime
	// short-circuit does not reuse the old hash -- it forces a recalculation).
	if err := os.WriteFile(arq, []byte("conteudo-totalmente-diferente-e-maior-"+arq), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2 (conteúdo trocado): %v", err)
	}

	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file após troca de conteúdo = %d, quero 1 (atualizado, não duplicado)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entity após troca de conteúdo = %d, quero 1", n)
	}
	var hashNovo, state string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT content_hash, state FROM media_file WHERE path = ?", arq).Scan(&hashNovo, &state); err != nil {
		t.Fatalf("ler hash novo: %v", err)
	}
	if hashNovo == hashAntigo {
		t.Errorf("content_hash não foi atualizado após troca de conteúdo")
	}
	if state != "present" {
		t.Errorf("state após troca de conteúdo = %q, quero present", state)
	}
}

// TestMatchTituloAnoVsID contrasts the embedded id (confidence 100) with the match by
// title+year (score < 100), and proves that the re-scan reuses the same entity without
// an id.
func TestMatchTituloAnoVsID(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	semID := filepath.Join(root, "Some Movie (2005)")
	escrever(t, filepath.Join(semID, "Some Movie (2005).mkv"))

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	var conf float64
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT match_confidence FROM media_file").Scan(&conf); err != nil {
		t.Fatalf("ler confiança: %v", err)
	}
	if conf >= 100 || conf <= 0 {
		t.Errorf("confiança sem id = %v, quero 0 < c < 100", conf)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE external_id_tmdb IS NULL"); n != 1 {
		t.Errorf("entity sem id externo esperada")
	}
	// the re-scan does not duplicate the entity without an id (dedup by title+year).
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entity duplicada no re-scan sem id: %d", n)
	}
}

// TestDraftSemMatch proves that a file whose name yields no title (only a technical
// token) becomes a draft entity with confidence 0.
func TestDraftSemMatch(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "2160p.mkv")) // loose at the root, no useful title

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE status='draft'"); n != 1 {
		t.Errorf("entity draft = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE match_confidence = 0"); n != 1 {
		t.Errorf("media_file confiança 0 = %d, quero 1", n)
	}
}

// TestMissingArquivoRemovido proves the lifecycle: with the source reachable, a file
// that disappeared becomes missing; the entity is preserved; ownership drops.
func TestMissingArquivoRemovido(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pastaA := filepath.Join(root, "Filme A (2001)")
	pastaB := filepath.Join(root, "Filme B (2002)")
	escrever(t, filepath.Join(pastaA, "Filme A (2001).mkv"))
	arqB := filepath.Join(pastaB, "Filme B (2002).mkv")
	escrever(t, arqB)

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}

	// It removes file B (and its folder) and re-scans with the source still
	// reachable.
	if err := os.RemoveAll(pastaB); err != nil {
		t.Fatal(err)
	}
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE state='missing'"); n != 1 {
		t.Errorf("media_file missing = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE state='present'"); n != 1 {
		t.Errorf("media_file present = %d, quero 1 (o A)", n)
	}
	// The entity of B is preserved (no entity deleted).
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 2 {
		t.Errorf("entities = %d, quero 2 (nenhuma apagada)", n)
	}
	// Ownership of B dropped (there is no present media_file for it).
	var posseB int
	if err := d.Read.QueryRowContext(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM media_file mf JOIN entity e ON e.id=mf.entity_id
		 WHERE e.title='Filme B' AND mf.state='present')`).Scan(&posseB); err != nil {
		t.Fatalf("posse B: %v", err)
	}
	if posseB != 0 {
		t.Errorf("posse de B = %d, quero 0 (arquivo missing não conta)", posseB)
	}
}

// TestDatasourceOfflineNaoMarcaMissing proves that, with the source unreachable, the
// scan is suspended, the datasource becomes offline and NO file is marked missing.
func TestDatasourceOfflineNaoMarcaMissing(t *testing.T) {
	d := abrirDB(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "fonte")
	escrever(t, filepath.Join(root, "Filme (2000)", "Filme (2000).mkv"))

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}

	// It simulates an unmount: it removes the whole root.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := scanSync(t, m, src); err == nil {
		t.Fatal("scan com fonte removida deveria retornar erro (offline)")
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE state='missing'"); n != 0 {
		t.Errorf("missing com fonte offline = %d, quero 0", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM library_source WHERE state='offline'"); n != 1 {
		t.Errorf("library_source offline = %d, quero 1", n)
	}
}

// TestScanParcialNaoMarcaMissing proves that when a SUBFOLDER becomes unreadable in
// the middle of the walk (not the root -- that is already covered by
// TestDatasourceOfflineNaoMarcaMissing), the scan is treated as PARTIAL: no
// media_file is marked missing, even if it was not reviewed in this round. It
// simulates a transient network mount failure (FUSE/rclone) that takes down the
// listing of a subfolder without taking down the root.
func TestScanParcialNaoMarcaMissing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("rodando como root: chmod 0000 não impede leitura; caso não aplicável")
	}
	d := abrirDB(t)
	root := t.TempDir()
	pastaA := filepath.Join(root, "Filme A (2001)")
	pastaB := filepath.Join(root, "Filme B (2002)")
	escrever(t, filepath.Join(pastaA, "Filme A (2001).mkv"))
	escrever(t, filepath.Join(pastaB, "Filme B (2002).mkv"))

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}

	// It simulates subfolder B becoming unreadable (e.g. unstable FUSE/rclone)
	// without removing anything -- the root stays reachable.
	if err := os.Chmod(pastaB, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pastaB, 0o755) })

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2 (parcial): %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE state='missing'"); n != 0 {
		t.Errorf("missing após scan parcial = %d, quero 0 (B só ficou ilegível, não foi removido)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE state='present'"); n != 2 {
		t.Errorf("present após scan parcial = %d, quero 2 (A revisto + B preservado do scan anterior)", n)
	}
}

// TestStartOfflineRetornaErro proves that Start refuses an unreachable source with
// ErrSourceOffline (without firing a goroutine).
func TestStartOfflineRetornaErro(t *testing.T) {
	d := abrirDB(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "fonte")
	escrever(t, filepath.Join(root, "Filme (2000)", "Filme (2000).mkv"))
	src := criarSource(t, d, root)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	m := managerFake(t, d)
	if _, err := m.Start(src, TriggerManual); err != ErrSourceOffline {
		t.Errorf("Start offline err = %v, quero ErrSourceOffline", err)
	}
}

// TestStartSemFilaConfiguradaDevolveErro proves that Start refuses with a clear error
// (not a panic) when ConfigurarFila was never called -- every production path
// configures the queue at boot (cmd/mymediadb/main.go); this should only happen in an
// incomplete use of the package.
func TestStartSemFilaConfiguradaDevolveErro(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Filme (2000)", "Filme (2000).mkv"))
	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if _, err := m.Start(src, TriggerManual); !errors.Is(err, ErrFilaNaoConfigurada) {
		t.Errorf("Start sem fila err = %v, quero ErrFilaNaoConfigurada", err)
	}
}

// TestStackingEdicoes proves that parts (cd1/cd2) and editions stay in the SAME
// entity, each one as a media_file with its own part_no/edition.
func TestStackingEdicoes(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Kill Bill (2003) [tmdbid-24]")
	escrever(t, filepath.Join(pasta, "Kill Bill (2003) [tmdbid-24] - cd1.mkv"))
	escrever(t, filepath.Join(pasta, "Kill Bill (2003) [tmdbid-24] - cd2.mkv"))
	escrever(t, filepath.Join(pasta, "Kill Bill (2003) [tmdbid-24] - Directors Cut.mkv"))

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entities = %d, quero 1 (mesma obra)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 3 {
		t.Errorf("media_file = %d, quero 3", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE part_no IS NOT NULL"); n != 2 {
		t.Errorf("media_file com part_no = %d, quero 2 (cd1/cd2)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE edition IS NOT NULL"); n != 1 {
		t.Errorf("media_file com edition = %d, quero 1", n)
	}
}

// TestMediainfoPreenchidoPeloFfprobe runs the REAL ffprobe (skipped if absent):
// it proves that mediainfo_json is filled from a synthetic clip.
func TestMediainfoPreenchidoPeloFfprobe(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg ausente")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe ausente")
	}
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Clipe (2020) [tmdbid-1]")
	if err := os.MkdirAll(pasta, 0o755); err != nil {
		t.Fatal(err)
	}
	clipe := filepath.Join(pasta, "Clipe (2020) [tmdbid-1] - 1080p.mkv")
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-f", "lavfi",
		"-i", "testsrc=d=1:s=64x64", "-c:v", "libx264", "-t", "1", clipe)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg não gerou o clipe: %v", err)
	}

	m := NewManagerComProber(d, mediainfo.NewProber())
	m.agora = relogioCrescente()
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	var mi string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT mediainfo_json FROM media_file").Scan(&mi); err != nil {
		t.Fatalf("ler mediainfo: %v", err)
	}
	if mi == "" {
		t.Fatal("mediainfo_json vazio; ffprobe deveria ter preenchido")
	}
	// it must mention the video codec.
	if want := "h264"; !strings.Contains(mi, want) {
		t.Errorf("mediainfo_json não menciona %q: %s", want, mi)
	}
}
