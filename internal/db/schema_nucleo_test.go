package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupNucleo abre um banco temporário, aplica as migrations (schema 4 após o
// 0004) e devolve o DB junto de um *Queries no pool de ESCRITA.
func setupNucleo(t *testing.T) (*DB, *sqlcgen.Queries) {
	t.Helper()
	d := openTest(t)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return d, sqlcgen.New(d.Write)
}

// ns embrulha uma string num sql.NullString válido (não-nulo).
func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// check falha o teste se err for não-nulo. Concentra o `if err != nil` num
// helper para manter as funções de teste simples (baixa complexidade).
func check(t *testing.T, err error, ctx string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", ctx, err)
	}
}

// criarEntity insere uma entity mínima e devolve a linha criada. `externalIDs` é
// o JSON cru de external_ids (pode ser "" para nulo).
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

// assertTabelasExistem confere que cada nome está no sqlite_master.
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

// populaGrafoMinimo cria entity + media_file + asset + uma segunda entity ligada
// por entity_relation, para exercitar as FKs.
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

// TestMigrationNucleoCriaTabelas prova que o 0002 cria todas as tabelas do
// escopo, que o foreign_key_check fica limpo com dados, e que o Down as remove.
func TestMigrationNucleoCriaTabelas(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()

	if v := mustSchemaVersion(t, d); v != 14 {
		t.Fatalf("versão de schema = %d, quero 14", v)
	}
	assertTabelasExistem(t, d,
		"entity", "media_file", "library_source", "asset", "entity_relation", "entity_fts",
		"media_external", "media_override", "app_settings", "external_detail")

	populaGrafoMinimo(t, q)

	// Integridade referencial limpa após popular o grafo.
	rows, err := d.Read.QueryContext(ctx, "PRAGMA foreign_key_check")
	check(t, err, "foreign_key_check")
	temViolacao := rows.Next()
	_ = rows.Close()
	if temViolacao {
		t.Error("foreign_key_check retornou violações, quero nenhuma")
	}

	// Down reverte, um por um, o 0014 (entity_catalog_completeness, briefing
	// 0058), o 0013 (entity.last_external_match_attempt_at, briefing 0056), o
	// 0012 (seed do job saude_biblioteca), o 0011
	// (job_definition/job_execution), o 0010 (person/company/credit), o 0009
	// (juncao media_file_entity), o 0008 (library_source.content_type), o
	// 0007 (sort_index + external_id_tvdb), o 0006 (external_detail), o 0005
	// (settings + coluna original_language), o 0004 (camadas
	// externo/override), o 0003 (coluna library_source_id) e depois o 0002
	// (schema do núcleo, volta ao 0001). Cada passo deve deixar a versão no
	// valor esperado.
	for _, esperada := range []int64{13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1} {
		downEChecaVersao(t, d, esperada)
	}
	if _, err := d.Read.QueryContext(ctx, "SELECT 1 FROM entity"); err == nil {
		t.Error("tabela entity ainda existe após o down")
	}
}

// downEChecaVersao aplica um MigrateDown e confere que a versão de schema resultante
// bate com a esperada (falha o teste caso contrário).
func downEChecaVersao(t *testing.T, d *DB, esperada int64) {
	t.Helper()
	check(t, d.MigrateDown(context.Background()), "MigrateDown")
	if v := mustSchemaVersion(t, d); v != esperada {
		t.Fatalf("versão após down = %d, quero %d", v, esperada)
	}
}

// TestStatusDefaultECheck prova o default 'cataloged' e o CHECK do conjunto.
func TestStatusDefaultECheck(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()

	e := criarEntity(t, q, "ent-1", "Sem status", "")
	if e.Status != "cataloged" {
		t.Errorf("status default = %q, quero \"cataloged\"", e.Status)
	}

	// Um status fora do conjunto deve ser rejeitado pelo CHECK.
	_, err := d.Write.ExecContext(ctx,
		"INSERT INTO entity (id, type, title, status) VALUES ('x', 'movie', 'T', 'invalido')")
	if err == nil {
		t.Error("INSERT com status inválido deveria falhar (CHECK)")
	}
}

// TestMediaFileConstraints prova o CHECK de state, o UNIQUE de path e o índice
// de content_hash.
func TestMediaFileConstraints(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "T", "")

	// state fora de {present,missing} é rejeitado.
	if _, err := d.Write.ExecContext(ctx,
		"INSERT INTO media_file (id, entity_id, path, state) VALUES ('m1', ?, '/a', 'bogus')",
		e.ID); err == nil {
		t.Error("INSERT com state inválido deveria falhar (CHECK)")
	}

	// path é UNIQUE.
	_, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "m2", EntityID: e.ID, Path: "/dup.mkv", State: "present",
	})
	check(t, err, "CriarMediaFile 1")
	if _, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "m3", EntityID: e.ID, Path: "/dup.mkv", State: "present",
	}); err == nil {
		t.Error("segundo media_file com o mesmo path deveria falhar (UNIQUE)")
	}

	// content_hash é indexado: o plano de uma busca por hash usa o índice.
	detalhe := explicaPlano(t, d, "SELECT id FROM media_file WHERE content_hash = ?", "abc")
	if !strings.Contains(detalhe, "idx_media_file_content_hash") {
		t.Errorf("plano por content_hash = %q, quero mencionar idx_media_file_content_hash", detalhe)
	}
}

// TestLookupTmdbUsaIndice prova o critério 4: o lookup por id externo tmdb (via
// coluna gerada) usa o índice, não uma varredura de tabela.
func TestLookupTmdbUsaIndice(t *testing.T) {
	d, q := setupNucleo(t)
	criarEntity(t, q, "ent-1", "Matrix", `{"tmdb":"603"}`)

	// A coluna gerada expõe o valor de $.tmdb e o lookup pela query gerada acha.
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

// TestFTSSincroniza prova o critério 5: insert/update/delete de entity mantêm o
// entity_fts em sincronia (busca via `entity_fts MATCH`, feita à mão pois o sqlc
// não modela o MATCH sobre o nome da tabela virtual).
func TestFTSSincroniza(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()

	e := criarEntity(t, q, "ent-1", "Matrix", "")
	if got := buscaFTS(t, d, "Matrix"); got != "ent-1" {
		t.Errorf("busca após insert = %q, quero ent-1", got)
	}

	// Após o UPDATE do título, a busca antiga não acha e a nova acha.
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

	// Após o DELETE, nada é encontrado.
	check(t, q.ApagarEntity(ctx, e.ID), "ApagarEntity")
	if got := buscaFTS(t, d, "Inception"); got != "" {
		t.Errorf("busca após delete = %q, quero vazio", got)
	}
}

// TestFTSIndexaTags prova que as tags entram na FTS (a coluna `tags` recebe o
// tags_json cru; o tokenizer quebra nos sinais do JSON e indexa os valores).
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

// TestPosseDerivada prova o critério 6: posse é verdadeira só quando há ao menos
// um media_file 'present'.
func TestPosseDerivada(t *testing.T) {
	_, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "T", "")

	if posse(t, q, e.ID) {
		t.Error("posse sem media_file = true, quero false")
	}

	// Um arquivo 'missing' não confere posse.
	mf, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-1", EntityID: e.ID, Path: "/a.mkv", State: "missing",
	})
	check(t, err, "CriarMediaFile missing")
	if posse(t, q, e.ID) {
		t.Error("posse só com media_file missing = true, quero false")
	}

	// Marcar o arquivo como 'present' confere posse.
	_, err = q.AtualizarMediaFileState(ctx, sqlcgen.AtualizarMediaFileStateParams{
		ID: mf.ID, State: "present", LastSeen: ns("2026-07-09T00:00:00Z"),
	})
	check(t, err, "AtualizarMediaFileState")
	if !posse(t, q, e.ID) {
		t.Error("posse com media_file present = false, quero true")
	}
}

// TestApagarEntityCascata prova que a purga da entity remove os filhos (FK
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

// TestLibrarySourceState prova o default 'online' e o CHECK do conjunto.
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

// (O exercício completo de CRUD de cada query gerada vive em
// internal/db/sqlcgen/crud_test.go, para que a cobertura seja atribuída ao
// próprio pacote sqlcgen no perfil do go test.)

// ── helpers de leitura ──────────────────────────────────────────────────────

// explicaPlano roda EXPLAIN QUERY PLAN e concatena a coluna `detail` de todas as
// linhas, para asserção sobre uso de índice.
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

// buscaFTS busca na entity_fts pelo termo e devolve o entity_id do primeiro hit
// (ou "" se nenhum). Feita à mão com database/sql porque o sqlc não modela o
// MATCH sobre o nome da tabela virtual.
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

// posse chama a query gerada de posse derivada.
func posse(t *testing.T, q *sqlcgen.Queries, entityID string) bool {
	t.Helper()
	tem, err := q.EntityTemPosse(context.Background(), entityID)
	check(t, err, "EntityTemPosse")
	return tem
}
