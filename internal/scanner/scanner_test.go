package scanner

import (
	"context"
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

// ── infraestrutura de teste ──────────────────────────────────────────────────

// abrirDB abre um banco temporário migrado.
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

// relogioCrescente devolve um clock injetável que avança 1s a cada chamada, para
// dar timestamps de scan distintos e determinísticos.
func relogioCrescente() func() time.Time {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	var n int64
	return func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Second)
	}
}

// managerFake cria um Manager com um prober que NÃO roda ffprobe (mediainfo
// vazio), suficiente para os testes de detecção/dedup/ciclo de vida.
func managerFake(t *testing.T, d *db.DB) *Manager {
	t.Helper()
	m := NewManagerComProber(d, proberVazio{})
	m.agora = relogioCrescente()
	return m
}

// proberVazio simula ffprobe indisponível: mediainfo fica vazio.
type proberVazio struct{}

func (proberVazio) Disponivel() bool                              { return false }
func (proberVazio) Probe(context.Context, string) ([]byte, error) { return nil, nil }

// criarSource cadastra um datasource apontando para dir (via o serviço real).
func criarSource(t *testing.T, d *db.DB, dir string) library.Source {
	t.Helper()
	src, err := library.New(d).Criar(context.Background(), dir, "movie", "")
	if err != nil {
		t.Fatalf("criar source: %v", err)
	}
	return src
}

// escrever cria um arquivo com conteúdo único (o próprio caminho) para que os
// content_hash difiram entre arquivos distintos.
func escrever(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("conteudo-"+path), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scanSync roda o pipeline de scan de forma síncrona (sem a goroutine), para
// asserções determinísticas.
func scanSync(t *testing.T, m *Manager, src library.Source) error {
	t.Helper()
	j := &job{id: "job-teste", running: true, startedAt: m.agora()}
	return m.executarScan(context.Background(), src, j)
}

// ── helpers de asserção sobre o banco ────────────────────────────────────────

func contar(t *testing.T, d *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("contar (%s): %v", query, err)
	}
	return n
}

// ── testes ───────────────────────────────────────────────────────────────────

// TestScanDetectaEntidadesEMediaFile cobre o critério central: a pasta vira UMA
// entity; as versões (qualidades) viram media_file da MESMA entity; o id embutido
// dá external_ids.tmdb + confiança 100; e o poster local vira asset.
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
	// library_source carimbado online + last_scan.
	if n := contar(t, d, "SELECT count(*) FROM library_source WHERE state='online' AND last_scan IS NOT NULL"); n != 1 {
		t.Errorf("library_source não carimbado: %d", n)
	}
}

// TestReScanIdempotente prova que reescanear não duplica nada e que renomear um
// arquivo (mesmo conteúdo) atualiza o path em vez de criar outra linha.
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

	// Renomeia (mesmo conteúdo). O dedup por hash deve atualizar o path.
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

// TestConteudoTrocadoNoMesmoPathNaoDuplica prova que, quando o content_hash
// recalculado diverge do que estava gravado para o MESMO path (arquivo
// substituído/reencodado no lugar, ou mtime/size do provedor sofreram drift o
// bastante para invalidar o short-circuit incremental), o scanner ATUALIZA a
// linha existente em vez de tentar um INSERT que bateria no UNIQUE(path).
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

	// Substitui o conteúdo NO MESMO path (tamanho diferente, então o
	// short-circuit de size/mtime não reusa o hash antigo -- força recálculo).
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

// TestMatchTituloAnoVsID contrasta o id embutido (confiança 100) com o match por
// título+ano (score < 100), e prova que o re-scan reusa a mesma entity sem id.
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
	// re-scan não duplica a entity sem id (dedup por título+ano).
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entity duplicada no re-scan sem id: %d", n)
	}
}

// TestDraftSemMatch prova que um arquivo cujo nome não rende título (só um token
// técnico) vira entity draft com confiança 0.
func TestDraftSemMatch(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "2160p.mkv")) // solto na raiz, sem título útil

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

// TestMissingArquivoRemovido prova o ciclo de vida: com a fonte acessível, um
// arquivo que sumiu vira missing; a entity é preservada; a posse cai.
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

	// Remove o arquivo B (e sua pasta) e reescaneia com a fonte ainda acessível.
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
	// A entity de B é preservada (nenhuma entity apagada).
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 2 {
		t.Errorf("entities = %d, quero 2 (nenhuma apagada)", n)
	}
	// Posse de B caiu (não há media_file present para ela).
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

// TestDatasourceOfflineNaoMarcaMissing prova que, com a fonte inacessível, o scan
// é suspenso, o datasource vira offline e NENHUM arquivo é marcado missing.
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

	// Simula desmonte: remove a raiz inteira.
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

// TestScanParcialNaoMarcaMissing prova que quando uma SUBPASTA fica ilegível
// no meio da varredura (não a raiz -- isso já é coberto por
// TestDatasourceOfflineNaoMarcaMissing), o scan é tratado como PARCIAL:
// nenhum media_file é marcado missing, mesmo que ele não tenha sido revisto
// nesta rodada. Simula uma falha transitória de mount de rede (FUSE/rclone)
// que derruba a listagem de uma subpasta sem derrubar a raiz.
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

	// Simula a subpasta B ficando ilegível (ex.: FUSE/rclone instável) sem
	// remover nada -- a raiz continua acessível.
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

// TestStartOfflineRetornaErro prova que Start recusa uma fonte inacessível com
// ErrSourceOffline (sem disparar goroutine).
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
	if _, err := m.Start(src); err != ErrSourceOffline {
		t.Errorf("Start offline err = %v, quero ErrSourceOffline", err)
	}
}

// TestStackingEdicoes prova que partes (cd1/cd2) e edições ficam na MESMA entity,
// cada uma como um media_file com part_no/edition próprios.
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

// TestMediainfoPreenchidoPeloFfprobe roda o ffprobe DE VERDADE (skip se ausente):
// prova que o mediainfo_json é preenchido a partir de um clipe sintético.
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
	// deve mencionar o codec do vídeo.
	if want := "h264"; !strings.Contains(mi, want) {
		t.Errorf("mediainfo_json não menciona %q: %s", want, mi)
	}
}
