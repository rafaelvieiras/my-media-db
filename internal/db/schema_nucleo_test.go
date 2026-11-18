package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupNucleo opens a temporary database, applies the migrations (schema 4 after
// 0004) and returns the DB together with a *Queries on the WRITE pool.
func setupNucleo(t *testing.T) (*DB, *sqlcgen.Queries) {
	t.Helper()
	d := openTest(t)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return d, sqlcgen.New(d.Write)
}

// ns wraps a string in a valid (non-null) sql.NullString.
func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// check fails the test if err is non-nil. It concentrates the `if err != nil` in a
// helper to keep the test functions simple (low complexity).
func check(t *testing.T, err error, ctx string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", ctx, err)
	}
}

// criarEntity inserts a minimal entity and returns the created row. `externalIDs` is
// the raw external_ids JSON (may be "" for null).
func criarEntity(t *testing.T, q *sqlcgen.Queries, id, title, externalIDs string) sqlcgen.Entity {
	t.Helper()
	ext := sql.NullString{}
	if externalIDs != "" {
		ext = ns(externalIDs)
	}
	e, err := q.CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              id,
		Type:            "movie",
		Title:           title,
		ExternalIdsJson: ext,
	})
	check(t, err, "CriarEntity")
	return e
}

// assertTabelasExistem checks that each name is in sqlite_master.
func assertTabelasExistem(t *testing.T, d *DB, tabelas ...string) {
	t.Helper()
	for _, tab := range tabelas {
		var nome string
		err := d.Read.QueryRowContext(context.Background(),
			"SELECT name FROM sqlite_master WHERE name = ?", tab).Scan(&nome)
		if err != nil {
			t.Errorf("tabela %q ausente: %v", tab, err)
		}
	}
}

// populaGrafoMinimo creates entity + media_file + asset + a second entity linked
// through entity_relation, to exercise the FKs.
func populaGrafoMinimo(t *testing.T, q *sqlcgen.Queries) {
	t.Helper()
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "Matrix", `{"tmdb":"603","imdb":"tt0133093"}`)
	_, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-1", EntityID: e.ID, Path: "/data/matrix.mkv", State: "present",
	})
	check(t, err, "CriarMediaFile")
	_, err = q.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID: "as-1", EntityID: e.ID, Kind: "poster", Origin: "local", PathOrUri: "/data/p.jpg",
	})
	check(t, err, "CriarAsset")
	e2 := criarEntity(t, q, "ent-2", "Matrix Reloaded", "")
	_, err = q.CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID: "rel-1", ParentID: e.ID, ChildID: e2.ID, Kind: "collection",
	})
	check(t, err, "CriarEntityRelation")
}

// TestMigrationNucleoCriaTabelas proves that 0002 creates every table of the
// scope, that foreign_key_check stays clean with data, and that Down removes them.
func TestMigrationNucleoCriaTabelas(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()

	alvo, err := versaoEmbarcada()
	if err != nil {
		t.Fatalf("versaoEmbarcada: %v", err)
	}
	if v := mustSchemaVersion(t, d); v != alvo {
		t.Fatalf("versão de schema = %d, quero %d (última migration embarcada)", v, alvo)
	}
	assertTabelasExistem(t, d,
		"entity", "media_file", "library_source", "asset", "entity_relation", "entity_fts",
		"media_external", "media_override", "app_settings", "external_detail", "historico_evento")

	populaGrafoMinimo(t, q)

	// Clean referential integrity after populating the graph.
	rows, err := d.Read.QueryContext(ctx, "PRAGMA foreign_key_check")
	check(t, err, "foreign_key_check")
	temViolacao := rows.Next()
	_ = rows.Close()
	if temViolacao {
		t.Error("foreign_key_check retornou violações, quero nenhuma")
	}

	// Down reverts, one by one, 0027 (default cron of the midias_faltantes job),
	// 0026 (integration_config, foundation of the integrations configurable
	// through the UI, ADR-0023), 0025 (recreates job_execution -- schema only,
	// no data; LAYER 3 of the same ADR: internal/job/manager.go now
	// enqueues through execqueue, so job_execution becomes a redundant source
	// of truth and is dropped), 0024 (queue_item, unified execution queue,
	// ADR-0028 -- job_execution is not touched in this layer), 0023
	// (entity.external_id_mbid, foundation of the music vertical), 0022 (seed
	// of the trailer_tema_faltantes job), 0021 (historico_evento.entity_id
	// nullable + raw_* for orphan events), 0020 (historico_evento), 0019
	// (local playback fields on entity), 0018
	// (library_source.retroactive_enrichment_days), 0017
	// (library_source.preferred_language), 0016 (library_source.watch_enabled
	// + seed of the midias_faltantes job), 0015 (asset.local_path/
	// is_principal + entity.nfo_path/nfo_written_at), 0014
	// (entity_catalog_completeness), 0013
	// (entity.last_external_match_attempt_at), 0012 (seed of the
	// saude_biblioteca job), 0011 (job_definition/job_execution), 0010
	// (person/company/credit), 0009 (media_file_entity junction), 0008
	// (library_source.content_type), 0007 (sort_index + external_id_tvdb),
	// 0006 (external_detail), 0005 (settings + original_language column), 0004
	// (external/override layers), 0003 (library_source_id column) and then
	// 0002 (core schema, back to 0001). Each step must leave the version at
	// the expected value.
	for esperada := alvo - 1; esperada >= 1; esperada-- {
		downEChecaVersao(t, d, esperada)
	}
	if _, err := d.Read.QueryContext(ctx, "SELECT 1 FROM entity"); err == nil {
		t.Error("tabela entity ainda existe após o down")
	}
}

// downEChecaVersao applies a MigrateDown and checks that the resulting schema version
// matches the expected one (it fails the test otherwise).
func downEChecaVersao(t *testing.T, d *DB, esperada int64) {
	t.Helper()
	check(t, d.MigrateDown(context.Background()), "MigrateDown")
	if v := mustSchemaVersion(t, d); v != esperada {
		t.Fatalf("versão após down = %d, quero %d", v, esperada)
	}
}

// TestStatusDefaultECheck proves the 'cataloged' default and the CHECK of the set.
func TestStatusDefaultECheck(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()

	e := criarEntity(t, q, "ent-1", "Sem status", "")
	if e.Status != "cataloged" {
		t.Errorf("status default = %q, quero \"cataloged\"", e.Status)
	}

	// A status outside the set must be rejected by the CHECK.
	_, err := d.Write.ExecContext(ctx,
		"INSERT INTO entity (id, type, title, status) VALUES ('x', 'movie', 'T', 'invalido')")
	if err == nil {
		t.Error("INSERT com status inválido deveria falhar (CHECK)")
	}
}

// TestMediaFileConstraints proves the state CHECK, the path UNIQUE and the
// content_hash index.
func TestMediaFileConstraints(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "T", "")

	// a state outside {present,missing} is rejected.
	if _, err := d.Write.ExecContext(ctx,
		"INSERT INTO media_file (id, entity_id, path, state) VALUES ('m1', ?, '/a', 'bogus')",
		e.ID); err == nil {
		t.Error("INSERT com state inválido deveria falhar (CHECK)")
	}

	// path is UNIQUE.
	_, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "m2", EntityID: e.ID, Path: "/dup.mkv", State: "present",
	})
	check(t, err, "CriarMediaFile 1")
	if _, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "m3", EntityID: e.ID, Path: "/dup.mkv", State: "present",
	}); err == nil {
		t.Error("segundo media_file com o mesmo path deveria falhar (UNIQUE)")
	}

	// content_hash is indexed: the plan of a search by hash uses the index.
	detalhe := explicaPlano(t, d, "SELECT id FROM media_file WHERE content_hash = ?", "abc")
	if !strings.Contains(detalhe, "idx_media_file_content_hash") {
		t.Errorf("plano por content_hash = %q, quero mencionar idx_media_file_content_hash", detalhe)
	}
}

// TestLookupTmdbUsaIndice proves criterion 4: the lookup by external tmdb id (via
// generated column) uses the index, not a table scan.
func TestLookupTmdbUsaIndice(t *testing.T) {
	d, q := setupNucleo(t)
	criarEntity(t, q, "ent-1", "Matrix", `{"tmdb":"603"}`)

	// The generated column exposes the value of $.tmdb and the lookup through the generated query finds it.
	got, err := q.ObterEntityPorTmdb(context.Background(), ns("603"))
	check(t, err, "ObterEntityPorTmdb")
	if got.ID != "ent-1" {
		t.Errorf("id encontrado = %q, quero ent-1", got.ID)
	}

	detalhe := explicaPlano(t, d, "SELECT id FROM entity WHERE external_id_tmdb = ?", "603")
	if !strings.Contains(detalhe, "idx_entity_external_id_tmdb") {
		t.Errorf("plano = %q, quero mencionar idx_entity_external_id_tmdb (SEARCH, não SCAN)", detalhe)
	}
}

// TestFTSSincroniza proves criterion 5: insert/update/delete of an entity keep
// entity_fts in sync (search via `entity_fts MATCH`, done by hand because sqlc
// does not model MATCH over the name of the virtual table).
func TestFTSSincroniza(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()

	e := criarEntity(t, q, "ent-1", "Matrix", "")
	if got := buscaFTS(t, d, "Matrix"); got != "ent-1" {
		t.Errorf("busca após insert = %q, quero ent-1", got)
	}

	// After the title UPDATE, the old search finds nothing and the new one finds it.
	_, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID: e.ID, Type: "movie", Title: "Inception", Status: "cataloged",
	})
	check(t, err, "AtualizarEntity")
	if got := buscaFTS(t, d, "Matrix"); got != "" {
		t.Errorf("busca por título antigo após update = %q, quero vazio", got)
	}
	if got := buscaFTS(t, d, "Inception"); got != "ent-1" {
		t.Errorf("busca por título novo após update = %q, quero ent-1", got)
	}

	// After the DELETE, nothing is found.
	check(t, q.ApagarEntity(ctx, e.ID), "ApagarEntity")
	if got := buscaFTS(t, d, "Inception"); got != "" {
		t.Errorf("busca após delete = %q, quero vazio", got)
	}
}

// TestFTSIndexaTags proves that the tags enter the FTS (the `tags` column receives the
// raw tags_json; the tokenizer splits on the JSON punctuation and indexes the values).
func TestFTSIndexaTags(t *testing.T) {
	d, q := setupNucleo(t)
	_, err := q.CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: "ent-1", Type: "movie", Title: "Sem título útil", TagsJson: ns(`["cyberpunk","noir"]`),
	})
	check(t, err, "CriarEntity")
	if got := buscaFTS(t, d, "cyberpunk"); got != "ent-1" {
		t.Errorf("busca por tag = %q, quero ent-1", got)
	}
}

// TestPosseDerivada proves criterion 6: ownership is true only when there is at least
// one 'present' media_file.
func TestPosseDerivada(t *testing.T) {
	_, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "T", "")

	if posse(t, q, e.ID) {
		t.Error("posse sem media_file = true, quero false")
	}

	// A 'missing' file does not confer ownership.
	mf, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-1", EntityID: e.ID, Path: "/a.mkv", State: "missing",
	})
	check(t, err, "CriarMediaFile missing")
	if posse(t, q, e.ID) {
		t.Error("posse só com media_file missing = true, quero false")
	}

	// Marking the file as 'present' confers ownership.
	_, err = q.AtualizarMediaFileState(ctx, sqlcgen.AtualizarMediaFileStateParams{
		ID: mf.ID, State: "present", LastSeen: ns("2026-07-09T00:00:00Z"),
	})
	check(t, err, "AtualizarMediaFileState")
	if !posse(t, q, e.ID) {
		t.Error("posse com media_file present = false, quero true")
	}
}

// TestApagarEntityCascata proves that purging the entity removes the children (FK
// ON DELETE CASCADE).
func TestApagarEntityCascata(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "T", "")
	_, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-1", EntityID: e.ID, Path: "/a.mkv", State: "present",
	})
	check(t, err, "CriarMediaFile")
	check(t, q.ApagarEntity(ctx, e.ID), "ApagarEntity")

	var n int
	err = d.Read.QueryRowContext(ctx,
		"SELECT count(*) FROM media_file WHERE entity_id = ?", e.ID).Scan(&n)
	check(t, err, "count media_file")
	if n != 0 {
		t.Errorf("media_file órfãos após purga = %d, quero 0 (cascata)", n)
	}
}

// TestLibrarySourceState proves the 'online' default and the CHECK of the set.
func TestLibrarySourceState(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	ls, err := q.CriarLibrarySource(ctx, sqlcgen.CriarLibrarySourceParams{
		ID: "ls-1", Path: "/mnt/filmes", Type: "movies",
	})
	check(t, err, "CriarLibrarySource")
	if ls.State != "online" {
		t.Errorf("state default = %q, quero \"online\"", ls.State)
	}
	if _, err := d.Write.ExecContext(ctx,
		"INSERT INTO library_source (id, path, type, state) VALUES ('x', '/y', 'movies', 'bogus')"); err == nil {
		t.Error("INSERT com state inválido deveria falhar (CHECK)")
	}
}

// (The complete CRUD exercise of each generated query lives in
// internal/db/sqlcgen/crud_test.go, so that the coverage is attributed to the
// sqlcgen package itself in the go test profile.)

// ── read helpers ───────────────────────────────────────────────────────────

// explicaPlano runs EXPLAIN QUERY PLAN and concatenates the `detail` column of all
// rows, for assertions about index usage.
func explicaPlano(t *testing.T, d *DB, query string, args ...any) string {
	t.Helper()
	rows, err := d.Read.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	check(t, err, "EXPLAIN QUERY PLAN")
	defer func() { _ = rows.Close() }()
	var partes []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		check(t, rows.Scan(&id, &parent, &notused, &detail), "scan do plano")
		partes = append(partes, detail)
	}
	return strings.Join(partes, " | ")
}

// buscaFTS searches entity_fts for the term and returns the entity_id of the first hit
// (or "" if none). Done by hand with database/sql because sqlc does not model
// MATCH over the name of the virtual table.
func buscaFTS(t *testing.T, d *DB, termo string) string {
	t.Helper()
	var id string
	err := d.Read.QueryRowContext(context.Background(),
		"SELECT entity_id FROM entity_fts WHERE entity_fts MATCH ? LIMIT 1", termo).Scan(&id)
	if err == sql.ErrNoRows {
		return ""
	}
	check(t, err, "busca FTS")
	return id
}

// posse calls the generated derived ownership query.
func posse(t *testing.T, q *sqlcgen.Queries, entityID string) bool {
	t.Helper()
	tem, err := q.EntityTemPosse(context.Background(), entityID)
	check(t, err, "EntityTemPosse")
	return tem
}
