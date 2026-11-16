package historico

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupDB brings up a migrated temporary database (same pattern as
// internal/enrich/enrich_test.go / internal/server/entities_test.go).
func setupDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "historico.sqlite")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}

// criarEntity seeds a minimal entity.
func criarEntity(t *testing.T, q *sqlcgen.Queries, id, tipo, titulo string) {
	t.Helper()
	if _, err := q.CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipo, Title: titulo,
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

// criarOrfao seeds an orphan event (entity_id NULL) with the given raw ids.
func criarOrfao(t *testing.T, q *sqlcgen.Queries, tmdb, imdb, tvdb string) sqlcgen.HistoricoEvento {
	t.Helper()
	ev, err := q.CriarHistoricoEventoOrfao(context.Background(), sqlcgen.CriarHistoricoEventoOrfaoParams{
		ID:         uuid.NewString(),
		Kind:       "assistir",
		Status:     "started",
		OccurredAt: "2026-01-01T10:00:00Z",
		Source:     "jellyfin-webhook",
		RawTmdbID:  ns(tmdb),
		RawImdbID:  ns(imdb),
		RawTvdbID:  ns(tvdb),
	})
	if err != nil {
		t.Fatalf("CriarHistoricoEventoOrfao: %v", err)
	}
	return ev
}

func entityIDDoEvento(t *testing.T, q *sqlcgen.Queries, eventoID string) sql.NullString {
	t.Helper()
	row, err := q.BuscarHistoricoEventoPorID(context.Background(), eventoID)
	if err != nil {
		t.Fatalf("BuscarHistoricoEventoPorID: %v", err)
	}
	return row.EntityID
}

// TestVincularOrfaosPorTmdb covers the main case: an orphan event whose
// raw_tmdb_id matches the id of the entity is linked (entity_id filled in).
func TestVincularOrfaosPorTmdb(t *testing.T) {
	d := setupDB(t)
	q := sqlcgen.New(d.Write)
	criarEntity(t, q, "ent-1", "movie", "Filme 1")
	orfao := criarOrfao(t, q, "603", "", "")

	total, err := VincularOrfaos(context.Background(), q, "ent-1", map[string]string{"tmdb": "603"})
	if err != nil {
		t.Fatalf("VincularOrfaos: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, quero 1", total)
	}
	got := entityIDDoEvento(t, q, orfao.ID)
	if !got.Valid || got.String != "ent-1" {
		t.Errorf("entity_id do evento = %+v, quero ent-1", got)
	}
}

// TestVincularOrfaosSemBater covers the negative case: no id matches, nothing
// is touched (entity_id stays NULL).
func TestVincularOrfaosSemBater(t *testing.T) {
	d := setupDB(t)
	q := sqlcgen.New(d.Write)
	criarEntity(t, q, "ent-1", "movie", "Filme 1")
	orfao := criarOrfao(t, q, "999", "", "")

	total, err := VincularOrfaos(context.Background(), q, "ent-1", map[string]string{"tmdb": "603"})
	if err != nil {
		t.Fatalf("VincularOrfaos: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, quero 0", total)
	}
	got := entityIDDoEvento(t, q, orfao.ID)
	if got.Valid {
		t.Errorf("entity_id do evento = %+v, quero NULL (nada deveria bater)", got)
	}
}

// TestVincularOrfaosDuasFontesMesmaEntity covers the acceptance criterion "it
// is not the first to arrive, the only one to win": two orphan events with
// DIFFERENT sources (tmdb and imdb) matching the SAME entity are both linked in
// a single call.
func TestVincularOrfaosDuasFontesMesmaEntity(t *testing.T) {
	d := setupDB(t)
	q := sqlcgen.New(d.Write)
	criarEntity(t, q, "ent-1", "movie", "Filme 1")
	orfaoTmdb := criarOrfao(t, q, "603", "", "")
	orfaoImdb := criarOrfao(t, q, "", "tt0133093", "")

	total, err := VincularOrfaos(context.Background(), q, "ent-1", map[string]string{
		"tmdb": "603",
		"imdb": "tt0133093",
	})
	if err != nil {
		t.Fatalf("VincularOrfaos: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, quero 2", total)
	}
	if got := entityIDDoEvento(t, q, orfaoTmdb.ID); !got.Valid || got.String != "ent-1" {
		t.Errorf("orfao tmdb entity_id = %+v, quero ent-1", got)
	}
	if got := entityIDDoEvento(t, q, orfaoImdb.ID); !got.Valid || got.String != "ent-1" {
		t.Errorf("orfao imdb entity_id = %+v, quero ent-1", got)
	}
}

// TestVincularOrfaosIgnoraFonteDesconhecida covers the decision that keys
// outside tmdb/imdb/tvdb are ignored (no error, no query called for them) --
// the backfill only covers the same 3 sources as the webhook matching.
func TestVincularOrfaosIgnoraFonteDesconhecida(t *testing.T) {
	d := setupDB(t)
	q := sqlcgen.New(d.Write)
	criarEntity(t, q, "ent-1", "movie", "Filme 1")

	total, err := VincularOrfaos(context.Background(), q, "ent-1", map[string]string{"fanarttv": "abc"})
	if err != nil {
		t.Fatalf("VincularOrfaos: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, quero 0", total)
	}
}
