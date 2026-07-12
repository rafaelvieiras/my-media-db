// Testes do serviço de datasources. Ficam no pacote externo library_test e
// exercitam o serviço contra um banco real e o sistema de arquivos real, para
// que a cobertura das funções do pacote library seja atribuída a ele próprio.
package library_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// novoServico abre um banco temporário migrado e devolve o serviço pronto.
func novoServico(t *testing.T) *library.Service {
	t.Helper()
	d, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "lib.sqlite")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return library.New(d)
}

func TestCriarDiretorioValido(t *testing.T) {
	s := novoServico(t)
	dir := t.TempDir()

	src, err := s.Criar(context.Background(), dir, "movie", "")
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}
	if src.ID == "" {
		t.Error("id vazio")
	}
	if src.Path != dir {
		t.Errorf("path = %q, quero %q (normalizado)", src.Path, dir)
	}
	if src.State != "online" {
		t.Errorf("state = %q, quero online", src.State)
	}
	if src.LastScan != nil {
		t.Errorf("last_scan = %v, quero nil", *src.LastScan)
	}
}

func TestCriarNormalizaPathNaoLimpo(t *testing.T) {
	s := novoServico(t)
	dir := t.TempDir()
	naoLimpo := filepath.Join(dir, "..", filepath.Base(dir))

	src, err := s.Criar(context.Background(), naoLimpo, "tv", "")
	if err != nil {
		t.Fatalf("Criar path não-limpo: %v", err)
	}
	if src.Path != dir {
		t.Errorf("path = %q, quero %q normalizado", src.Path, dir)
	}
}

func TestCriarErrosDeValidacao(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()
	dir := t.TempDir()

	arquivo := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(arquivo, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	casos := []struct {
		nome    string
		path    string
		tipo    string
		querErr error
	}{
		{"tipo invalido", dir, "musica", library.ErrTipoInvalido},
		{"path vazio", "", "movie", library.ErrPathVazio},
		{"path inexistente", filepath.Join(dir, "nada"), "movie", library.ErrPathInexistente},
		{"nao diretorio", arquivo, "movie", library.ErrNaoDiretorio},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := s.Criar(ctx, c.path, c.tipo, "")
			if !errors.Is(err, c.querErr) {
				t.Errorf("erro = %v, quero %v", err, c.querErr)
			}
		})
	}
}

func TestCriarSemPermissao(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root: chmod 0000 não impede leitura; caso não aplicável")
	}
	s := novoServico(t)
	dir := filepath.Join(t.TempDir(), "trancado")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := s.Criar(context.Background(), dir, "movie", "")
	if !errors.Is(err, library.ErrPathIlegivel) {
		t.Errorf("erro = %v, quero ErrPathIlegivel", err)
	}
}

func TestCriarDuplicado(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := s.Criar(ctx, dir, "movie", ""); err != nil {
		t.Fatalf("primeiro Criar: %v", err)
	}
	_, err := s.Criar(ctx, dir, "movie", "")
	if !errors.Is(err, library.ErrPathDuplicado) {
		t.Errorf("erro = %v, quero ErrPathDuplicado", err)
	}
}

func TestListarEObterComStateReavaliado(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()

	dirRemovido := filepath.Join(t.TempDir(), "d")
	if err := os.Mkdir(dirRemovido, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	removido, err := s.Criar(ctx, dirRemovido, "movie", "")
	if err != nil {
		t.Fatalf("Criar removido: %v", err)
	}
	if _, err := s.Criar(ctx, t.TempDir(), "tv", ""); err != nil {
		t.Fatalf("Criar vivo: %v", err)
	}

	if err := os.Remove(dirRemovido); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	lista, err := s.Listar(ctx)
	if err != nil {
		t.Fatalf("Listar: %v", err)
	}
	if len(lista) != 2 {
		t.Fatalf("lista = %d, quero 2", len(lista))
	}

	// Obter reavalia o state: o dir removido volta offline.
	got, err := s.Obter(ctx, removido.ID)
	if err != nil {
		t.Fatalf("Obter: %v", err)
	}
	if got.State != "offline" {
		t.Errorf("state = %q, quero offline (dir removido)", got.State)
	}
}

func TestObterInexistente(t *testing.T) {
	s := novoServico(t)
	_, err := s.Obter(context.Background(), "nao-existe")
	if !errors.Is(err, library.ErrNaoEncontrado) {
		t.Errorf("erro = %v, quero ErrNaoEncontrado", err)
	}
}

// TestContentType cobre o campo content_type (0034): vazio normaliza para "movie"
// (compat), e "series" persiste e volta na leitura.
func TestContentType(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()

	// Omitido => "movie".
	filme, err := s.Criar(ctx, t.TempDir(), "movie", "")
	if err != nil {
		t.Fatalf("Criar filme: %v", err)
	}
	if filme.ContentType != "movie" {
		t.Errorf("content_type (omitido) = %q, quero movie", filme.ContentType)
	}

	// "series" persiste e volta no Obter.
	serie, err := s.Criar(ctx, t.TempDir(), "tv", "series")
	if err != nil {
		t.Fatalf("Criar serie: %v", err)
	}
	if serie.ContentType != "series" {
		t.Errorf("content_type = %q, quero series", serie.ContentType)
	}
	lido, err := s.Obter(ctx, serie.ID)
	if err != nil {
		t.Fatalf("Obter: %v", err)
	}
	if lido.ContentType != "series" {
		t.Errorf("content_type relido = %q, quero series", lido.ContentType)
	}
}

func TestApagar(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()
	dir := t.TempDir()

	src, err := s.Criar(ctx, dir, "movie", "")
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}
	if err := s.Apagar(ctx, src.ID); err != nil {
		t.Fatalf("Apagar: %v", err)
	}
	if _, err := s.Obter(ctx, src.ID); !errors.Is(err, library.ErrNaoEncontrado) {
		t.Errorf("após Apagar, Obter erro = %v, quero ErrNaoEncontrado", err)
	}
	// Apagar inexistente -> ErrNaoEncontrado.
	if err := s.Apagar(ctx, "nada"); !errors.Is(err, library.ErrNaoEncontrado) {
		t.Errorf("Apagar inexistente erro = %v, quero ErrNaoEncontrado", err)
	}
}

// TestApagarComArquivos cobre o fix 0035: apagar uma fonte que já escaneou
// arquivos deve funcionar (antes dava 500 pela FK sem ON DELETE). Verifica que,
// na mesma transação, os media_file da fonte e seus vínculos media_file_entity
// somem (cascade da migration 0009), enquanto a entity do catálogo PERMANECE.
func TestApagarComArquivos(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "lib.sqlite")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := library.New(d)

	// Fonte de séries com um episódio escaneado.
	src, err := s.Criar(ctx, t.TempDir(), "tv", "series")
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}

	// Semeia entity + media_file (vinculado à fonte) + vínculo pela conexão de
	// escrita (mesmo caminho do scanner). Entity precisa existir antes: o
	// media_file referencia entity(id) NOT NULL.
	q := sqlcgen.New(d.Write)
	ent, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:    "ent-ep-1",
		Type:  "tv_episode",
		Title: "Episódio 1",
	})
	if err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	mf, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID:              "mf-1",
		EntityID:        ent.ID,
		Path:            filepath.Join(src.Path, "S01E01.mkv"),
		State:           "present",
		LibrarySourceID: sql.NullString{String: src.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("CriarMediaFile: %v", err)
	}
	if err := q.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: mf.ID,
		EntityID:    ent.ID,
		Kind:        "tv_episode",
		Ordinal:     0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	// Apaga a fonte: não pode explodir mesmo com arquivos vinculados.
	if err := s.Apagar(ctx, src.ID); err != nil {
		t.Fatalf("Apagar com arquivos: %v", err)
	}

	// Fonte sumiu.
	if _, err := s.Obter(ctx, src.ID); !errors.Is(err, library.ErrNaoEncontrado) {
		t.Errorf("após Apagar, Obter erro = %v, quero ErrNaoEncontrado", err)
	}
	// media_file sumiu.
	r := sqlcgen.New(d.Read)
	if _, err := r.ObterMediaFile(ctx, mf.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("media_file ainda existe (erro = %v), quero sql.ErrNoRows", err)
	}
	// Vínculo media_file_entity cascateou.
	vinculos, err := r.ListarVinculosPorArquivo(ctx, mf.ID)
	if err != nil {
		t.Fatalf("ListarVinculosPorArquivo: %v", err)
	}
	if len(vinculos) != 0 {
		t.Errorf("vínculos = %d, quero 0 (cascade)", len(vinculos))
	}
	// Entity do catálogo PERMANECE (não purgamos entities -- 0008 §6).
	if _, err := r.ObterEntity(ctx, ent.ID); err != nil {
		t.Errorf("entity deveria permanecer, mas ObterEntity = %v", err)
	}
}
