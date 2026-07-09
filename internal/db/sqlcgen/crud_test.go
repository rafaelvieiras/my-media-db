// Testes de fumaça do acesso gerado pelo sqlc. Ficam NO pacote sqlcgen (como
// pacote de teste externo sqlcgen_test) de propósito: assim a cobertura das
// funções geradas é atribuída ao PRÓPRIO pacote sqlcgen no perfil do `go test`
// (a atribuição cruzada entre pacotes não vale sem -coverpkg). Aqui exercitamos
// cada query ao menos uma vez sobre um banco real, com o schema aplicado pelas
// migrations via o pacote db.
package sqlcgen_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setup abre um banco temporário migrado (schema 3) e devolve os Queries no pool
// de escrita.
func setup(t *testing.T) *sqlcgen.Queries {
	t.Helper()
	d, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "sqlcgen.sqlite")})
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
	return sqlcgen.New(d.Write)
}

func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// check falha o teste se err for não-nulo. Mantém as funções de teste simples.
func check(t *testing.T, err error, ctx string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", ctx, err)
	}
}

// TestCRUDEntity cobre o ciclo completo da entity, incluindo lookups por id
// externo (colunas geradas) e a query de posse derivada.
func TestCRUDEntity(t *testing.T) {
	q := setup(t)
	ctx := context.Background()

	e, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "ent-1", Type: "movie", Title: "Matrix",
		OriginalTitle:   ns("The Matrix"),
		Description:     ns("Neo descobre a verdade."),
		ReleaseDate:     ns("1999-03-31"),
		TagsJson:        ns(`["scifi"]`),
		ExternalIdsJson: ns(`{"tmdb":"603","imdb":"tt0133093"}`),
	})
	check(t, err, "CriarEntity")
	if e.Status != "cataloged" {
		t.Errorf("status default = %q, quero cataloged", e.Status)
	}

	_, err = q.ObterEntity(ctx, e.ID)
	check(t, err, "ObterEntity")
	got, err := q.ObterEntityPorTmdb(ctx, ns("603"))
	check(t, err, "ObterEntityPorTmdb")
	if got.ID != e.ID {
		t.Errorf("ObterEntityPorTmdb id = %q, quero %q", got.ID, e.ID)
	}
	_, err = q.ObterEntityPorImdb(ctx, ns("tt0133093"))
	check(t, err, "ObterEntityPorImdb")

	lista, err := q.ListarEntities(ctx, sqlcgen.ListarEntitiesParams{Limit: 10, Offset: 0})
	check(t, err, "ListarEntities")
	if len(lista) != 1 {
		t.Errorf("ListarEntities = %d, quero 1", len(lista))
	}

	upd, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID: e.ID, Type: "movie", Title: "Matrix (upgrade)", Status: "wanted",
	})
	check(t, err, "AtualizarEntity")
	if upd.Status != "wanted" || upd.Title != "Matrix (upgrade)" {
		t.Errorf("update não refletiu: %+v", upd)
	}

	// posse: falsa sem arquivo, verdadeira com present.
	if tem, _ := q.EntityTemPosse(ctx, e.ID); tem {
		t.Error("posse sem arquivo = true, quero false")
	}
	_, err = q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-1", EntityID: e.ID, Path: "/m.mkv", State: "present",
	})
	check(t, err, "CriarMediaFile")
	if tem, _ := q.EntityTemPosse(ctx, e.ID); !tem {
		t.Error("posse com present = false, quero true")
	}

	check(t, q.ApagarEntity(ctx, e.ID), "ApagarEntity")
}

// TestCRUDMediaFile cobre criar/obter/listar/atualizar-estado/apagar.
func TestCRUDMediaFile(t *testing.T) {
	q := setup(t)
	ctx := context.Background()
	e, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e", Type: "movie", Title: "T"})
	check(t, err, "CriarEntity")

	mf, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-1", EntityID: e.ID, Path: "/a.mkv", State: "present",
		ContentHash:     ns("deadbeef"),
		Size:            sql.NullInt64{Int64: 123, Valid: true},
		Mtime:           ns("2026-07-09T00:00:00Z"),
		Edition:         ns("Director's Cut"),
		PartNo:          sql.NullInt64{Int64: 1, Valid: true},
		MatchConfidence: sql.NullFloat64{Float64: 0.9, Valid: true},
		MediainfoJson:   ns(`{"codec":"h264"}`),
		LastSeen:        ns("2026-07-09T00:00:00Z"),
	})
	check(t, err, "CriarMediaFile")

	_, err = q.ObterMediaFile(ctx, mf.ID)
	check(t, err, "ObterMediaFile")
	fs, err := q.ListarMediaFilesPorEntity(ctx, e.ID)
	check(t, err, "ListarMediaFilesPorEntity")
	if len(fs) != 1 {
		t.Errorf("ListarMediaFilesPorEntity = %d, quero 1", len(fs))
	}
	upd, err := q.AtualizarMediaFileState(ctx, sqlcgen.AtualizarMediaFileStateParams{
		ID: mf.ID, State: "missing", LastSeen: ns("2026-07-10T00:00:00Z"),
	})
	check(t, err, "AtualizarMediaFileState")
	if upd.State != "missing" {
		t.Errorf("state após update = %q, quero missing", upd.State)
	}
	check(t, q.ApagarMediaFile(ctx, mf.ID), "ApagarMediaFile")
}

// TestCRUDAssetRelation cobre asset e entity_relation.
func TestCRUDAssetRelation(t *testing.T) {
	q := setup(t)
	ctx := context.Background()
	e1, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "collection", Title: "Trilogia"})
	check(t, err, "CriarEntity e1")
	e2, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e2", Type: "movie", Title: "Filme"})
	check(t, err, "CriarEntity e2")

	as, err := q.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID: "as-1", EntityID: e2.ID, Kind: "poster", Origin: "local",
		PathOrUri: "/p.jpg", MetaJson: ns(`{"w":600}`),
	})
	check(t, err, "CriarAsset")
	_, err = q.ObterAsset(ctx, as.ID)
	check(t, err, "ObterAsset")
	assets, err := q.ListarAssetsPorEntity(ctx, e2.ID)
	check(t, err, "ListarAssetsPorEntity")
	if len(assets) != 1 {
		t.Errorf("ListarAssetsPorEntity = %d, quero 1", len(assets))
	}
	check(t, q.ApagarAsset(ctx, as.ID), "ApagarAsset")

	rel, err := q.CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID: "rel-1", ParentID: e1.ID, ChildID: e2.ID, Kind: "collection",
	})
	check(t, err, "CriarEntityRelation")
	_, err = q.ObterEntityRelation(ctx, rel.ID)
	check(t, err, "ObterEntityRelation")
	filhas, err := q.ListarRelacoesFilhas(ctx, e1.ID)
	check(t, err, "ListarRelacoesFilhas")
	if len(filhas) != 1 {
		t.Errorf("ListarRelacoesFilhas = %d, quero 1", len(filhas))
	}
	pais, err := q.ListarRelacoesPais(ctx, e2.ID)
	check(t, err, "ListarRelacoesPais")
	if len(pais) != 1 {
		t.Errorf("ListarRelacoesPais = %d, quero 1", len(pais))
	}
	check(t, q.ApagarEntityRelation(ctx, rel.ID), "ApagarEntityRelation")
}

// TestCRUDLibrarySourceEMeta cobre library_source e a query-semente ValorMeta.
func TestCRUDLibrarySourceEMeta(t *testing.T) {
	q := setup(t)
	ctx := context.Background()

	ls, err := q.CriarLibrarySource(ctx, sqlcgen.CriarLibrarySourceParams{
		ID: "ls-1", Path: "/mnt/filmes", Type: "movies",
	})
	check(t, err, "CriarLibrarySource")
	if ls.State != "online" {
		t.Errorf("state default = %q, quero online", ls.State)
	}
	_, err = q.ObterLibrarySource(ctx, ls.ID)
	check(t, err, "ObterLibrarySource")
	fontes, err := q.ListarLibrarySources(ctx)
	check(t, err, "ListarLibrarySources")
	if len(fontes) != 1 {
		t.Errorf("ListarLibrarySources = %d, quero 1", len(fontes))
	}
	upd, err := q.AtualizarLibrarySource(ctx, sqlcgen.AtualizarLibrarySourceParams{
		ID: ls.ID, Type: "movies", State: "offline", LastScan: ns("2026-07-09T00:00:00Z"),
	})
	check(t, err, "AtualizarLibrarySource")
	if upd.State != "offline" {
		t.Errorf("state após update = %q, quero offline", upd.State)
	}
	check(t, q.ApagarLibrarySource(ctx, ls.ID), "ApagarLibrarySource")

	// query-semente do 0006.
	v, err := q.ValorMeta(ctx, "fundacao")
	check(t, err, "ValorMeta")
	if v != "0006" {
		t.Errorf("ValorMeta(fundacao) = %q, quero 0006", v)
	}
}
