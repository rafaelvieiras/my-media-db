// Tests of the datasource service. They live in the external library_test
// package and exercise the service against a real database and the real
// filesystem, so that the coverage of the library package functions is
// attributed to it.
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

// novoServico opens a temporary migrated database and returns the ready
// service.
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

// abrirDBMigrado opens a temporary migrated database and returns the raw *db.DB
// (for the tests that need to seed through the write connection, besides the
// service).
func abrirDBMigrado(t *testing.T) *db.DB {
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
	return d
}

// semearFonteComArquivo creates entity + media_file (linked to the source) +
// link through the write connection (the same path the scanner takes) and
// returns the entity and the file. The entity must exist first: the media_file
// references entity(id) NOT NULL.
func semearFonteComArquivo(t *testing.T, d *db.DB, src library.Source) (sqlcgen.Entity, sqlcgen.MediaFile) {
	t.Helper()
	ctx := context.Background()
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
	return ent, mf
}

func TestCriarDiretorioValido(t *testing.T) {
	s := novoServico(t)
	dir := t.TempDir()

	src, err := s.Criar(context.Background(), dir, "movie", "", nil, nil)
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

	src, err := s.Criar(context.Background(), naoLimpo, "tv", "", nil, nil)
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
			_, err := s.Criar(ctx, c.path, c.tipo, "", nil, nil)
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

	_, err := s.Criar(context.Background(), dir, "movie", "", nil, nil)
	if !errors.Is(err, library.ErrPathIlegivel) {
		t.Errorf("erro = %v, quero ErrPathIlegivel", err)
	}
}

func TestCriarDuplicado(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := s.Criar(ctx, dir, "movie", "", nil, nil); err != nil {
		t.Fatalf("primeiro Criar: %v", err)
	}
	_, err := s.Criar(ctx, dir, "movie", "", nil, nil)
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
	removido, err := s.Criar(ctx, dirRemovido, "movie", "", nil, nil)
	if err != nil {
		t.Fatalf("Criar removido: %v", err)
	}
	if _, err := s.Criar(ctx, t.TempDir(), "tv", "", nil, nil); err != nil {
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

// TestContentType covers the content_type field (ADR-0021): empty normalizes to
// "movie" (compatibility), and "series" persists and comes back on read.
func TestContentType(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()

	// Omitted => "movie".
	filme, err := s.Criar(ctx, t.TempDir(), "movie", "", nil, nil)
	if err != nil {
		t.Fatalf("Criar filme: %v", err)
	}
	if filme.ContentType != "movie" {
		t.Errorf("content_type (omitido) = %q, quero movie", filme.ContentType)
	}

	// "series" persists and comes back in Obter.
	serie, err := s.Criar(ctx, t.TempDir(), "tv", "series", nil, nil)
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

	src, err := s.Criar(ctx, dir, "movie", "", nil, nil)
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}
	if err := s.Apagar(ctx, src.ID); err != nil {
		t.Fatalf("Apagar: %v", err)
	}
	if _, err := s.Obter(ctx, src.ID); !errors.Is(err, library.ErrNaoEncontrado) {
		t.Errorf("após Apagar, Obter erro = %v, quero ErrNaoEncontrado", err)
	}
	// Deleting a nonexistent one -> ErrNaoEncontrado.
	if err := s.Apagar(ctx, "nada"); !errors.Is(err, library.ErrNaoEncontrado) {
		t.Errorf("Apagar inexistente erro = %v, quero ErrNaoEncontrado", err)
	}
}

// TestAtualizarPathRecascaCascata covers the core of the datasource repoint (see
// doc 06): changing the path of a source cascades the re-prefixing to
// media_file.path (directly and through media_file_entity), asset.local_path and
// entity.nfo_path, preserving id/content_hash/links -- with no re-scan. It
// covers both ways of reaching an entity: DIRECT (media_file.entity_id, the
// movie case) and through a JOIN (media_file_entity, the episode case, whose
// file points at the SERIES). One of the sub-paths uses an accent (pt-BR) in the
// folder name to validate that TrimPrefix in Go (byte-safe) does not diverge
// from SQLite (which counts codepoints).
func TestAtualizarPathRecascaCascata(t *testing.T) {
	ctx := context.Background()
	d := abrirDBMigrado(t)
	s := library.New(d)
	q := sqlcgen.New(d.Write)

	dirAntigo := t.TempDir()
	src, err := s.Criar(ctx, dirAntigo, "tv", "series", nil, nil)
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}

	// Case 1 (through a JOIN): media_file points at the SERIES; the EPISODE is
	// only reached through media_file_entity (ADR-0021). Accented subfolder on
	// purpose.
	serie, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "serie-1", Type: "tv", Title: "Série"})
	if err != nil {
		t.Fatalf("CriarEntity serie: %v", err)
	}
	episodio, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "ep-1", Type: "episode", Title: "Episódio 1"})
	if err != nil {
		t.Fatalf("CriarEntity episodio: %v", err)
	}
	pathMfEp := filepath.Join(dirAntigo, "Temporada 1 - Édição", "S01E01.mkv")
	mfEp, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID:              "mf-ep-1",
		EntityID:        serie.ID,
		Path:            pathMfEp,
		State:           "present",
		LibrarySourceID: sql.NullString{String: src.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("CriarMediaFile episodio: %v", err)
	}
	if err := q.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: mfEp.ID, EntityID: episodio.ID, Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}
	assetEp, err := q.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID: "asset-ep-1", EntityID: episodio.ID, Kind: "still", Origin: "user", PathOrUri: "/cache/ep1.jpg",
	})
	if err != nil {
		t.Fatalf("CriarAsset episodio: %v", err)
	}
	localPathAntigoEp := filepath.Join(dirAntigo, "Temporada 1 - Édição", "ep1-still.jpg")
	if err := q.AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: sql.NullString{String: localPathAntigoEp, Valid: true}, ID: assetEp.ID,
	}); err != nil {
		t.Fatalf("AtualizarAssetLocalPath episodio: %v", err)
	}

	// nfo_path of the SERIES (reached DIRECTLY through media_file.entity_id).
	nfoPathAntigoSerie := filepath.Join(dirAntigo, "tvshow.nfo")
	if err := q.AtualizarEntityNfo(ctx, sqlcgen.AtualizarEntityNfoParams{
		NfoPath:      sql.NullString{String: nfoPathAntigoSerie, Valid: true},
		NfoWrittenAt: sql.NullString{String: "2026-01-01T00:00:00.000Z", Valid: true},
		ID:           serie.ID,
	}); err != nil {
		t.Fatalf("AtualizarEntityNfo serie: %v", err)
	}

	// Case 2 (DIRECT): movie, media_file.entity_id points straight at the
	// entity (no join), in the SAME source.
	filme, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "filme-1", Type: "movie", Title: "Filme"})
	if err != nil {
		t.Fatalf("CriarEntity filme: %v", err)
	}
	pathMfFilme := filepath.Join(dirAntigo, "filme.mkv")
	mfFilme, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-filme-1", EntityID: filme.ID, Path: pathMfFilme,
		ContentHash:     sql.NullString{String: "hash-abc", Valid: true},
		State:           "present",
		LibrarySourceID: sql.NullString{String: src.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("CriarMediaFile filme: %v", err)
	}
	assetFilme, err := q.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID: "asset-filme-1", EntityID: filme.ID, Kind: "poster", Origin: "user", PathOrUri: "/cache/poster.jpg",
	})
	if err != nil {
		t.Fatalf("CriarAsset filme: %v", err)
	}
	localPathAntigoFilme := filepath.Join(dirAntigo, "poster.jpg")
	if err := q.AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: sql.NullString{String: localPathAntigoFilme, Valid: true}, ID: assetFilme.ID,
	}); err != nil {
		t.Fatalf("AtualizarAssetLocalPath filme: %v", err)
	}
	// asset WITHOUT local_path (artwork never materialized on disk): the
	// cascade must ignore it (without an error), without trying to re-prefix a
	// null value.
	assetSemLocal, err := q.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID: "asset-filme-sem-local", EntityID: filme.ID, Kind: "fanart", Origin: "external", PathOrUri: "/cache/fanart.jpg",
	})
	if err != nil {
		t.Fatalf("CriarAsset sem local_path: %v", err)
	}

	// Case 3 (DEFENSIVE, §6 of the repoint design): media_file with the
	// library_source_id of the source, but a path that does NOT start with the
	// old prefix (a pathological situation that should not occur in practice).
	// The cascade must skip this row without touching it and without breaking
	// the rest of the transaction.
	pathEstranho := filepath.Join(t.TempDir(), "fora-do-prefixo.mkv")
	mfEstranho, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-estranho-1", EntityID: filme.ID, Path: pathEstranho,
		State:           "present",
		LibrarySourceID: sql.NullString{String: src.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("CriarMediaFile estranho: %v", err)
	}

	novoDir := t.TempDir()
	atualizado, err := s.AtualizarPath(ctx, src.ID, novoDir)
	if err != nil {
		t.Fatalf("AtualizarPath: %v", err)
	}
	if atualizado.ID != src.ID {
		t.Errorf("id da fonte mudou: %q, quero %q", atualizado.ID, src.ID)
	}
	if atualizado.Path != novoDir {
		t.Errorf("path = %q, quero %q", atualizado.Path, novoDir)
	}
	if atualizado.State != "online" {
		t.Errorf("state = %q, quero online (novo dir existe)", atualizado.State)
	}

	r := sqlcgen.New(d.Read)

	// media_file of the episode: SAME id, SAME implicit content_hash (not set
	// here, but the rest of the row preserved), path re-prefixed.
	gotMfEp, err := r.ObterMediaFile(ctx, mfEp.ID)
	if err != nil {
		t.Fatalf("ObterMediaFile episodio: %v", err)
	}
	queroMfEp := filepath.Join(novoDir, "Temporada 1 - Édição", "S01E01.mkv")
	if gotMfEp.Path != queroMfEp {
		t.Errorf("media_file(episodio).path = %q, quero %q", gotMfEp.Path, queroMfEp)
	}
	if gotMfEp.EntityID != serie.ID {
		t.Errorf("media_file(episodio).entity_id mudou: %q, quero %q", gotMfEp.EntityID, serie.ID)
	}

	// media_file of the movie: id/content_hash preserved, path re-prefixed.
	gotMfFilme, err := r.ObterMediaFile(ctx, mfFilme.ID)
	if err != nil {
		t.Fatalf("ObterMediaFile filme: %v", err)
	}
	queroMfFilme := filepath.Join(novoDir, "filme.mkv")
	if gotMfFilme.Path != queroMfFilme {
		t.Errorf("media_file(filme).path = %q, quero %q", gotMfFilme.Path, queroMfFilme)
	}
	if !gotMfFilme.ContentHash.Valid || gotMfFilme.ContentHash.String != "hash-abc" {
		t.Errorf("content_hash do filme não preservado: %+v", gotMfFilme.ContentHash)
	}

	// asset.local_path of the episode (entity reached through a JOIN) re-prefixed.
	gotAssetEp, err := r.ObterAsset(ctx, assetEp.ID)
	if err != nil {
		t.Fatalf("ObterAsset episodio: %v", err)
	}
	queroAssetEp := filepath.Join(novoDir, "Temporada 1 - Édição", "ep1-still.jpg")
	if !gotAssetEp.LocalPath.Valid || gotAssetEp.LocalPath.String != queroAssetEp {
		t.Errorf("asset(episodio).local_path = %+v, quero %q", gotAssetEp.LocalPath, queroAssetEp)
	}

	// asset.local_path of the movie (entity reached DIRECTLY) re-prefixed.
	gotAssetFilme, err := r.ObterAsset(ctx, assetFilme.ID)
	if err != nil {
		t.Fatalf("ObterAsset filme: %v", err)
	}
	queroAssetFilme := filepath.Join(novoDir, "poster.jpg")
	if !gotAssetFilme.LocalPath.Valid || gotAssetFilme.LocalPath.String != queroAssetFilme {
		t.Errorf("asset(filme).local_path = %+v, quero %q", gotAssetFilme.LocalPath, queroAssetFilme)
	}

	// entity.nfo_path of the series re-prefixed; nfo_written_at PRESERVED (this
	// is not a new export, only the prefix moved).
	gotSerie, err := r.ObterEntity(ctx, serie.ID)
	if err != nil {
		t.Fatalf("ObterEntity serie: %v", err)
	}
	queroNfoSerie := filepath.Join(novoDir, "tvshow.nfo")
	if !gotSerie.NfoPath.Valid || gotSerie.NfoPath.String != queroNfoSerie {
		t.Errorf("entity(serie).nfo_path = %+v, quero %q", gotSerie.NfoPath, queroNfoSerie)
	}
	if !gotSerie.NfoWrittenAt.Valid || gotSerie.NfoWrittenAt.String != "2026-01-01T00:00:00.000Z" {
		t.Errorf("entity(serie).nfo_written_at mudou: %+v, quero preservado", gotSerie.NfoWrittenAt)
	}

	// asset without local_path: the cascade neither breaks nor invents a value.
	gotAssetSemLocal, err := r.ObterAsset(ctx, assetSemLocal.ID)
	if err != nil {
		t.Fatalf("ObterAsset sem local_path: %v", err)
	}
	if gotAssetSemLocal.LocalPath.Valid {
		t.Errorf("asset sem local_path ganhou um valor: %+v", gotAssetSemLocal.LocalPath)
	}

	// "foreign" media_file (path outside the old prefix, the defensive case of
	// §6): it stays UNTOUCHED -- the cascade does not corrupt a path that does
	// not match.
	gotMfEstranho, err := r.ObterMediaFile(ctx, mfEstranho.ID)
	if err != nil {
		t.Fatalf("ObterMediaFile estranho: %v", err)
	}
	if gotMfEstranho.Path != pathEstranho {
		t.Errorf("media_file estranho foi tocado: path = %q, quero %q intacto", gotMfEstranho.Path, pathEstranho)
	}

	// library_source.path persisted (read back from scratch).
	relido, err := s.Obter(ctx, src.ID)
	if err != nil {
		t.Fatalf("Obter apos AtualizarPath: %v", err)
	}
	if relido.Path != novoDir {
		t.Errorf("path relido = %q, quero %q", relido.Path, novoDir)
	}
}

// TestAtualizarPathNoOp covers the no-op: a new path EQUAL to the current one
// does not touch the database and returns 200 with the source as it is.
func TestAtualizarPathNoOp(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()
	dir := t.TempDir()

	src, err := s.Criar(ctx, dir, "movie", "", nil, nil)
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}
	atualizado, err := s.AtualizarPath(ctx, src.ID, dir)
	if err != nil {
		t.Fatalf("AtualizarPath (no-op): %v", err)
	}
	if atualizado.Path != dir {
		t.Errorf("path = %q, quero %q (no-op)", atualizado.Path, dir)
	}
	if atualizado.ID != src.ID {
		t.Errorf("id = %q, quero %q", atualizado.ID, src.ID)
	}
}

// TestAtualizarPathDuplicado covers the collision with ANOTHER source already
// registered.
func TestAtualizarPathDuplicado(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()
	dirA := t.TempDir()
	dirB := t.TempDir()

	srcA, err := s.Criar(ctx, dirA, "movie", "", nil, nil)
	if err != nil {
		t.Fatalf("Criar A: %v", err)
	}
	if _, err := s.Criar(ctx, dirB, "movie", "", nil, nil); err != nil {
		t.Fatalf("Criar B: %v", err)
	}

	_, err = s.AtualizarPath(ctx, srcA.ID, dirB)
	if !errors.Is(err, library.ErrPathDuplicado) {
		t.Errorf("erro = %v, quero ErrPathDuplicado", err)
	}
	// Source A must not have been touched.
	relido, err := s.Obter(ctx, srcA.ID)
	if err != nil {
		t.Fatalf("Obter A: %v", err)
	}
	if relido.Path != dirA {
		t.Errorf("path de A mudou para %q apos 409, quero %q intacto", relido.Path, dirA)
	}
}

// TestAtualizarPathErrosDeValidacao covers the same 422/400 errors of Criar,
// now through AtualizarPath.
func TestAtualizarPathErrosDeValidacao(t *testing.T) {
	s := novoServico(t)
	ctx := context.Background()
	src, err := s.Criar(ctx, t.TempDir(), "movie", "", nil, nil)
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}

	outroDir := t.TempDir()
	arquivo := filepath.Join(outroDir, "f.txt")
	if err := os.WriteFile(arquivo, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	casos := []struct {
		nome    string
		path    string
		querErr error
	}{
		{"path vazio", "", library.ErrPathVazio},
		{"path inexistente", filepath.Join(outroDir, "nada"), library.ErrPathInexistente},
		{"nao diretorio", arquivo, library.ErrNaoDiretorio},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := s.AtualizarPath(ctx, src.ID, c.path)
			if !errors.Is(err, c.querErr) {
				t.Errorf("erro = %v, quero %v", err, c.querErr)
			}
		})
	}
}

// TestAtualizarPathSemPermissao cobre o 422 path_unreadable.
func TestAtualizarPathSemPermissao(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root: chmod 0000 não impede leitura; caso não aplicável")
	}
	s := novoServico(t)
	ctx := context.Background()
	src, err := s.Criar(ctx, t.TempDir(), "movie", "", nil, nil)
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}

	trancado := filepath.Join(t.TempDir(), "trancado")
	if err := os.Mkdir(trancado, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(trancado, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(trancado, 0o755) })

	_, err = s.AtualizarPath(ctx, src.ID, trancado)
	if !errors.Is(err, library.ErrPathIlegivel) {
		t.Errorf("erro = %v, quero ErrPathIlegivel", err)
	}
}

// TestAtualizarPathIdInexistente cobre o 404.
func TestAtualizarPathIdInexistente(t *testing.T) {
	s := novoServico(t)
	_, err := s.AtualizarPath(context.Background(), "nao-existe", t.TempDir())
	if !errors.Is(err, library.ErrNaoEncontrado) {
		t.Errorf("erro = %v, quero ErrNaoEncontrado", err)
	}
}

// TestApagarComArquivos covers the fix: deleting a source that already scanned
// files must work (it used to return 500 because of the FK without ON DELETE).
// It verifies that, in the same transaction, the media_file of the source and
// their media_file_entity links disappear (cascade of migration 0009), while the
// catalog entity REMAINS.
func TestApagarComArquivos(t *testing.T) {
	ctx := context.Background()
	d := abrirDBMigrado(t)
	s := library.New(d)

	// Series source with one scanned episode.
	src, err := s.Criar(ctx, t.TempDir(), "tv", "series", nil, nil)
	if err != nil {
		t.Fatalf("Criar: %v", err)
	}

	ent, mf := semearFonteComArquivo(t, d, src)

	// Deletes the source: it must not blow up even with linked files.
	if err := s.Apagar(ctx, src.ID); err != nil {
		t.Fatalf("Apagar com arquivos: %v", err)
	}

	// The source is gone.
	if _, err := s.Obter(ctx, src.ID); !errors.Is(err, library.ErrNaoEncontrado) {
		t.Errorf("após Apagar, Obter erro = %v, quero ErrNaoEncontrado", err)
	}
	// The media_file is gone.
	r := sqlcgen.New(d.Read)
	if _, err := r.ObterMediaFile(ctx, mf.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("media_file ainda existe (erro = %v), quero sql.ErrNoRows", err)
	}
	// The media_file_entity link cascaded.
	vinculos, err := r.ListarVinculosPorArquivo(ctx, mf.ID)
	if err != nil {
		t.Fatalf("ListarVinculosPorArquivo: %v", err)
	}
	if len(vinculos) != 0 {
		t.Errorf("vínculos = %d, quero 0 (cascade)", len(vinculos))
	}
	// The catalog entity REMAINS (entities are not purged -- API decision, §6).
	if _, err := r.ObterEntity(ctx, ent.ID); err != nil {
		t.Errorf("entity deveria permanecer, mas ObterEntity = %v", err)
	}
}
