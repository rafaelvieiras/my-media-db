package db

import (
	"context"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// contaLinhas counts the rows of a table filtering by a column = value.
// It concentrates the SELECT count(*) in a helper to keep the tests simple.
func contaLinhas(t *testing.T, d *DB, query string, args ...any) int {
	t.Helper()
	var n int
	err := d.Read.QueryRowContext(context.Background(), query, args...).Scan(&n)
	check(t, err, "count")
	return n
}

// TestMediaExternalIndiceLookup proves that the lookup by (source, source_id) uses the
// idx_media_external_source index (SEARCH, not SCAN) -- the path for reconciling
// identity through the cached external id.
func TestMediaExternalIndiceLookup(t *testing.T) {
	d, _ := setupNucleo(t)
	detalhe := explicaPlano(t, d,
		"SELECT id FROM media_external WHERE source = ? AND source_id = ?", "tmdb", "603")
	if !strings.Contains(detalhe, "idx_media_external_source") {
		t.Errorf("plano por (source, source_id) = %q, quero mencionar idx_media_external_source", detalhe)
	}
}

// TestMediaExternalUpsertUnique proves the UNIQUE(entity_id, source): a second
// upsert with the same (entity, source) UPDATES the row (payload + source_id), it does not
// duplicate it; a different source creates a second row.
func TestMediaExternalUpsertUnique(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "Matrix", "")

	primeira, err := q.UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID: "ext-1", EntityID: e.ID, Source: "tmdb",
		SourceID: ns("603"), PayloadJson: ns(`{"a":1}`),
	})
	check(t, err, "UpsertMediaExternal 1")

	// Same (entity, source), different id: the ON CONFLICT updates the
	// existing row (id preserved) instead of inserting another one.
	segunda, err := q.UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID: "ext-OUTRO", EntityID: e.ID, Source: "tmdb",
		SourceID: ns("603-v2"), PayloadJson: ns(`{"a":2}`),
	})
	check(t, err, "UpsertMediaExternal 2 (conflito)")
	if segunda.ID != primeira.ID {
		t.Errorf("upsert trocou o id (%q -> %q), quero preservar %q", primeira.ID, segunda.ID, primeira.ID)
	}
	if segunda.PayloadJson.String != `{"a":2}` || segunda.SourceID.String != "603-v2" {
		t.Errorf("upsert nao atualizou payload/source_id: %+v", segunda)
	}
	if n := contaLinhas(t, d, "SELECT count(*) FROM media_external WHERE entity_id = ?", e.ID); n != 1 {
		t.Errorf("linhas de media_external para a entity = %d, quero 1 (upsert nao duplica)", n)
	}

	// Different source: creates a second row.
	_, err = q.UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID: "ext-2", EntityID: e.ID, Source: "imdb", SourceID: ns("tt0133093"),
	})
	check(t, err, "UpsertMediaExternal outra fonte")
	if n := contaLinhas(t, d, "SELECT count(*) FROM media_external WHERE entity_id = ?", e.ID); n != 2 {
		t.Errorf("linhas apos segunda fonte = %d, quero 2", n)
	}
}

// TestMediaOverride1a1 proves that media_override is 1:1 (PK = entity_id): a second
// upsert on the same entity REPLACES the override, it does not create a second row.
func TestMediaOverride1a1(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "Matrix", "")

	_, err := q.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID: e.ID, OverrideJson: ns(`{"title":"A"}`),
	})
	check(t, err, "UpsertMediaOverride 1")
	segunda, err := q.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID: e.ID, OverrideJson: ns(`{"title":"B"}`),
	})
	check(t, err, "UpsertMediaOverride 2")
	if segunda.OverrideJson.String != `{"title":"B"}` {
		t.Errorf("override apos upsert = %q, quero {\"title\":\"B\"}", segunda.OverrideJson.String)
	}
	if n := contaLinhas(t, d, "SELECT count(*) FROM media_override WHERE entity_id = ?", e.ID); n != 1 {
		t.Errorf("linhas de media_override = %d, quero 1 (1:1)", n)
	}
}

// TestCamadasCascata proves the ON DELETE CASCADE FKs: purging the entity removes the
// media_external and media_override rows of that entity.
func TestCamadasCascata(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "Matrix", "")

	_, err := q.UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID: "ext-1", EntityID: e.ID, Source: "tmdb", PayloadJson: ns(`{"a":1}`),
	})
	check(t, err, "UpsertMediaExternal")
	_, err = q.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID: e.ID, OverrideJson: ns(`{"title":"X"}`),
	})
	check(t, err, "UpsertMediaOverride")

	check(t, q.ApagarEntity(ctx, e.ID), "ApagarEntity")

	if n := contaLinhas(t, d, "SELECT count(*) FROM media_external WHERE entity_id = ?", e.ID); n != 0 {
		t.Errorf("media_external orfaos apos purga = %d, quero 0 (cascata)", n)
	}
	if n := contaLinhas(t, d, "SELECT count(*) FROM media_override WHERE entity_id = ?", e.ID); n != 0 {
		t.Errorf("media_override orfaos apos purga = %d, quero 0 (cascata)", n)
	}
}
