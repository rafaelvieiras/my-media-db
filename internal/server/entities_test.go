package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// setupEntitiesServer starts a Server with a migrated temporary database and returns the
// root handler AND the *db.DB, so the tests can seed data straight through the sqlc
// queries (via the write connection) and exercise the read routes.
func setupEntitiesServer(t *testing.T) (http.Handler, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "entities.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			// Close may already have been called by a specific test; ignore it.
			_ = err
		}
	})
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	h, err := New("v-teste", database).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database
}

// escrita returns the Queries on the WRITE pool, to seed the tests.
func escrita(t *testing.T, d *db.DB) *sqlcgen.Queries {
	t.Helper()
	return sqlcgen.New(d.Write)
}

// nsOf wraps a string in a valid sql.NullString.
func nsOf(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// semearEntity creates a minimal entity and returns its id.
func semearEntity(t *testing.T, d *db.DB, id, tipo, titulo string) string {
	t.Helper()
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:    id,
		Type:  tipo,
		Title: titulo,
	})
	if err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
	return id
}

// semearFile creates a media_file for the entity with the given state and confidence.
func semearFile(t *testing.T, d *db.DB, id, entityID, path, state string, conf float64, mediainfo string) {
	t.Helper()
	p := sqlcgen.CriarMediaFileParams{
		ID:              id,
		EntityID:        entityID,
		Path:            path,
		State:           state,
		MatchConfidence: sql.NullFloat64{Float64: conf, Valid: true},
	}
	if mediainfo != "" {
		p.MediainfoJson = nsOf(mediainfo)
	}
	if _, err := escrita(t, d).CriarMediaFile(context.Background(), p); err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

// semearAsset creates an asset (e.g. poster) for the entity.
func semearAsset(t *testing.T, d *db.DB, id, entityID, kind, path string) {
	t.Helper()
	_, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID:        id,
		EntityID:  entityID,
		Kind:      kind,
		Origin:    "local",
		PathOrUri: path,
	})
	if err != nil {
		t.Fatalf("CriarAsset(%s): %v", id, err)
	}
}

// semearSource creates a library_source pointing at path.
func semearSource(t *testing.T, d *db.DB, id, path string) {
	t.Helper()
	_, err := escrita(t, d).CriarLibrarySource(context.Background(), sqlcgen.CriarLibrarySourceParams{
		ID:   id,
		Path: path,
		Type: "movie",
	})
	if err != nil {
		t.Fatalf("CriarLibrarySource(%s): %v", id, err)
	}
}

// definirCreatedAt overwrites created_at straight in the database (CriarEntity is always
// born with the DEFAULT timestamp, with no dedicated parameter) -- used by the
// sort=-created_at tests to guarantee a deterministic order between entities created
// within the same millisecond window.
func definirCreatedAt(t *testing.T, d *db.DB, id, createdAt string) {
	t.Helper()
	if _, err := d.Write.ExecContext(context.Background(),
		"UPDATE entity SET created_at = ? WHERE id = ?", createdAt, id); err != nil {
		t.Fatalf("definirCreatedAt(%s=%s): %v", id, createdAt, err)
	}
}

// decodeList decodes the paged response of GET /api/v1/entities.
func decodeList(t *testing.T, body []byte) entityListDTO {
	t.Helper()
	var out entityListDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar entityListDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// eq compares comparable values; it keeps the test functions linear (low cyclomatic
// complexity) by moving each assertion out of the test body.
func eq[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, quero %v", name, got, want)
	}
}

// eqPtr compares a pointer with an expected value (nil fails).
func eqPtr[T comparable](t *testing.T, name string, got *T, want T) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, quero %v", name, want)
		return
	}
	eq(t, name, *got, want)
}

func TestListEntitiesPosseEContagem(t *testing.T) {
	h, d := setupEntitiesServer(t)

	// A: movie with 2 files, one present -> owned, file_count=2.
	semearEntity(t, d, "a", "movie", "Alfa")
	semearFile(t, d, "a1", "a", "/lib/a1.mkv", "present", 0.9, "")
	semearFile(t, d, "a2", "a", "/lib/a2.mkv", "missing", 0.5, "")
	// B: movie with 1 missing file -> NOT owned, file_count=1.
	semearEntity(t, d, "b", "movie", "Beta")
	semearFile(t, d, "b1", "b", "/lib/b1.mkv", "missing", 0.3, "")
	// C: series with no files -> NOT owned, file_count=0.
	semearEntity(t, d, "c", "tv", "Gama")

	rec := do(t, h, http.MethodGet, "/api/v1/entities", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeList(t, rec.Body.Bytes())
	eq(t, "total", out.Total, 3)
	eq(t, "limit", out.Limit, limitPadrao)
	eq(t, "offset", out.Offset, 0)
	byID := map[string]entityCardDTO{}
	for _, it := range out.Items {
		byID[it.ID] = it
	}
	eq(t, "A.owned", byID["a"].Owned, true)
	eq(t, "A.file_count", byID["a"].FileCount, 2)
	eq(t, "B.owned", byID["b"].Owned, false)
	eq(t, "B.file_count", byID["b"].FileCount, 1)
	eq(t, "C.owned", byID["c"].Owned, false)
	eq(t, "C.file_count", byID["c"].FileCount, 0)
	// match_confidence = MAX of the confidences of A (0.9).
	eqPtr(t, "A.match_confidence", byID["a"].MatchConfidence, 0.9)
}

func TestListEntitiesFiltroType(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "a", "movie", "Alfa")
	semearEntity(t, d, "b", "movie", "Beta")
	semearEntity(t, d, "c", "tv", "Gama")

	rec := do(t, h, http.MethodGet, "/api/v1/entities?type=movie", nil)
	out := decodeList(t, rec.Body.Bytes())
	if out.Total != 2 || len(out.Items) != 2 {
		t.Fatalf("type=movie: total=%d items=%d, quero 2/2", out.Total, len(out.Items))
	}
	for _, it := range out.Items {
		if it.Type != "movie" {
			t.Errorf("item type = %q, quero movie", it.Type)
		}
	}
}

func TestListEntitiesFiltroOwned(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "a", "movie", "Alfa")
	semearFile(t, d, "a1", "a", "/lib/a1.mkv", "present", 1, "")
	semearEntity(t, d, "b", "movie", "Beta")
	semearFile(t, d, "b1", "b", "/lib/b1.mkv", "missing", 1, "")

	recTrue := do(t, h, http.MethodGet, "/api/v1/entities?owned=true", nil)
	outTrue := decodeList(t, recTrue.Body.Bytes())
	if outTrue.Total != 1 || outTrue.Items[0].ID != "a" {
		t.Fatalf("owned=true: total=%d, quero só A", outTrue.Total)
	}

	recFalse := do(t, h, http.MethodGet, "/api/v1/entities?owned=false", nil)
	outFalse := decodeList(t, recFalse.Body.Bytes())
	if outFalse.Total != 1 || outFalse.Items[0].ID != "b" {
		t.Fatalf("owned=false: total=%d, quero só B", outFalse.Total)
	}
}

// TestListEntitiesOwnedViaArvore: the `owned` of a music artist/album would ALWAYS be
// false in a direct check (only the track links to the media_file, ADR-0027) -- that
// strict check left the artist "recently added" carousel always empty and the
// "Ownership" of /library/artists always zeroed, even with tracks really owned. It
// builds the artist->album->track tree through entity_relation (the same shape
// enrich.ImportarFaixaPossuida writes) with a present media_file only on the TRACK, and
// requires that both the artist and the album show up as owned=true (rollup through the
// tree), and that the `owned=true` filter brings them back. A second artist WITHOUT any
// owned track proves that the rollup is not always true (owned=false keeps working).
func TestListEntitiesOwnedViaArvore(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "art1", "artista", "Duran Duran")
	semearEntity(t, d, "alb1", "album", "Rio")
	semearEntity(t, d, "fx1", "faixa", "Hungry Like the Wolf")
	ligarFilhoOrdenado(t, d, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, d, "f1", "fx1", "/lib/musica/rio/01.flac", "present", 1, "")

	semearEntity(t, d, "art2", "artista", "Sem Posse")
	semearEntity(t, d, "alb2", "album", "Nada Aqui")
	ligarFilhoOrdenado(t, d, "r3", "art2", "alb2", "music_artista_principal", 0)

	rec := do(t, h, http.MethodGet, "/api/v1/entities?type=artista", nil)
	out := decodeList(t, rec.Body.Bytes())
	owned := map[string]bool{}
	for _, it := range out.Items {
		owned[it.ID] = it.Owned
	}
	if !owned["art1"] {
		t.Errorf("art1.owned = false, quero true (faixa possuida 2 niveis abaixo)")
	}
	if owned["art2"] {
		t.Errorf("art2.owned = true, quero false (nenhuma faixa possuida)")
	}

	recAlb := do(t, h, http.MethodGet, "/api/v1/entities?type=album", nil)
	outAlb := decodeList(t, recAlb.Body.Bytes())
	for _, it := range outAlb.Items {
		if it.ID == "alb1" && !it.Owned {
			t.Errorf("alb1.owned = false, quero true (faixa possuida 1 nivel abaixo)")
		}
		if it.ID == "alb2" && it.Owned {
			t.Errorf("alb2.owned = true, quero false")
		}
	}

	recFiltro := do(t, h, http.MethodGet, "/api/v1/entities?type=artista&owned=true", nil)
	outFiltro := decodeList(t, recFiltro.Body.Bytes())
	if outFiltro.Total != 1 || outFiltro.Items[0].ID != "art1" {
		t.Fatalf("type=artista&owned=true: total=%d, quero só art1", outFiltro.Total)
	}

	// sort=-created_at&owned=true is EXACTLY the query of the home "recently added"
	// carousel (RecentlyAddedRail) -- without the rollup through the tree, the artist
	// carousel stays always empty even with owned tracks.
	recRail := do(t, h, http.MethodGet, "/api/v1/entities?type=artista&owned=true&sort=-created_at&limit=10", nil)
	outRail := decodeList(t, recRail.Body.Bytes())
	if outRail.Total != 1 || outRail.Items[0].ID != "art1" {
		t.Fatalf("carrossel (sort=-created_at&owned=true): total=%d, quero só art1", outRail.Total)
	}
}

// TestGetEntityOwnedViaArvore covers the SAME bug of TestListEntitiesOwnedViaArvore, but
// on the SINGLE DETAIL (GET /api/v1/entities/{id}), which uses a computation path
// SEPARATE from the listings (resolver.Entidade.Owned via listarFilesDaEntity/temPosse --
// it only looks at media_file.entity_id DIRECTLY, never descends the entity_relation
// tree). Without completarOwnedViaArvore (entities.go), a music artist/album always
// showed owned=false on the single detail even after the listings were fixed.
func TestGetEntityOwnedViaArvore(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "art1", "artista", "Duran Duran")
	semearEntity(t, d, "alb1", "album", "Rio")
	semearEntity(t, d, "fx1", "faixa", "Hungry Like the Wolf")
	ligarFilhoOrdenado(t, d, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, d, "f1", "fx1", "/lib/musica/rio/01.flac", "present", 1, "")

	semearEntity(t, d, "art2", "artista", "Sem Posse")

	var alvo struct {
		Owned bool `json:"owned"`
	}

	recArt := do(t, h, http.MethodGet, "/api/v1/entities/art1", nil)
	if recArt.Code != http.StatusOK {
		t.Fatalf("GET /entities/art1 status = %d, quero 200", recArt.Code)
	}
	decodeInto(t, recArt, &alvo)
	if !alvo.Owned {
		t.Errorf("art1 (detalhe único) owned = false, quero true (faixa possuída 2 níveis abaixo)")
	}

	recAlb := do(t, h, http.MethodGet, "/api/v1/entities/alb1", nil)
	decodeInto(t, recAlb, &alvo)
	if !alvo.Owned {
		t.Errorf("alb1 (detalhe único) owned = false, quero true (faixa possuída 1 nível abaixo)")
	}

	recArt2 := do(t, h, http.MethodGet, "/api/v1/entities/art2", nil)
	decodeInto(t, recArt2, &alvo)
	if alvo.Owned {
		t.Errorf("art2 (detalhe único) owned = true, quero false (sem faixa nenhuma)")
	}
}

func TestListEntitiesPaginacaoEOrdenacao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Titles out of order, to prove the ordering by title.
	semearEntity(t, d, "id-c", "movie", "Charlie")
	semearEntity(t, d, "id-a", "movie", "Alfa")
	semearEntity(t, d, "id-b", "movie", "Bravo")

	// Ascending, page 1 (limit=2): Alfa, Bravo.
	rec := do(t, h, http.MethodGet, "/api/v1/entities?sort=title&limit=2&offset=0", nil)
	out := decodeList(t, rec.Body.Bytes())
	if out.Total != 3 || len(out.Items) != 2 {
		t.Fatalf("asc pg1: total=%d items=%d, quero 3/2", out.Total, len(out.Items))
	}
	if out.Items[0].Title != "Alfa" || out.Items[1].Title != "Bravo" {
		t.Errorf("asc pg1 = [%s,%s], quero [Alfa,Bravo]", out.Items[0].Title, out.Items[1].Title)
	}
	// Ascending, page 2 (offset=2): Charlie.
	rec2 := do(t, h, http.MethodGet, "/api/v1/entities?sort=title&limit=2&offset=2", nil)
	out2 := decodeList(t, rec2.Body.Bytes())
	if len(out2.Items) != 1 || out2.Items[0].Title != "Charlie" {
		t.Fatalf("asc pg2 = %+v, quero [Charlie]", out2.Items)
	}
	// Descending: Charlie, Bravo, Alfa.
	recD := do(t, h, http.MethodGet, "/api/v1/entities?sort=-title", nil)
	outD := decodeList(t, recD.Body.Bytes())
	if len(outD.Items) != 3 || outD.Items[0].Title != "Charlie" || outD.Items[2].Title != "Alfa" {
		t.Fatalf("desc = %+v, quero [Charlie,Bravo,Alfa]", outD.Items)
	}
}

func TestListEntitiesQueryInvalida(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	for _, qs := range []string{"?limit=abc", "?offset=-1", "?owned=talvez", "?sort=ano"} {
		rec := do(t, h, http.MethodGet, "/api/v1/entities"+qs, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, quero 400", qs, rec.Code)
		}
		checkErrorShape(t, rec, "invalid_query")
	}
}

// TestListEntitiesSortCreatedAtDesc covers sort=-created_at: the home uses this order for
// the "recently added" carousel. The CREATION order at seeding is deliberately different
// from the created_at order forced below, to prove that the ordering is by created_at and
// not by insertion order/id.
func TestListEntitiesSortCreatedAtDesc(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "id-x", "movie", "Xeque")
	definirCreatedAt(t, d, "id-x", "2026-01-01T00:00:00.000Z")
	semearEntity(t, d, "id-y", "movie", "Yankee")
	definirCreatedAt(t, d, "id-y", "2026-03-01T00:00:00.000Z")
	semearEntity(t, d, "id-z", "movie", "Zulu")
	definirCreatedAt(t, d, "id-z", "2026-02-01T00:00:00.000Z")

	rec := do(t, h, http.MethodGet, "/api/v1/entities?sort=-created_at", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeList(t, rec.Body.Bytes())
	if len(out.Items) != 3 {
		t.Fatalf("items = %d, quero 3", len(out.Items))
	}
	gotIDs := []string{out.Items[0].ID, out.Items[1].ID, out.Items[2].ID}
	wantIDs := []string{"id-y", "id-z", "id-x"} // mar > feb > jan
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("ordem = %v, quero %v", gotIDs, wantIDs)
		}
	}

	// Small limit (the carousel's real use): only the 2 most recent ones.
	recLim := do(t, h, http.MethodGet, "/api/v1/entities?sort=-created_at&limit=2", nil)
	outLim := decodeList(t, recLim.Body.Bytes())
	if len(outLim.Items) != 2 || outLim.Items[0].ID != "id-y" || outLim.Items[1].ID != "id-z" {
		t.Fatalf("limit=2 = %+v, quero [id-y,id-z]", outLim.Items)
	}
}

// TestListEntityTypes covers GET /entities/types: distinct TOP-LEVEL types in the
// catalog (status != 'draft') with the right count -- with no list hardcoded in the
// frontend. A 'wanted' item counts (cataloged, even without a file); a 'draft' item does
// NOT count. album/track are SUB-NODES (tiposSubNo, ADR-0030) and NEVER show up, even
// when cataloged -- a regression of the finding in production (neither of the two old
// hardcoded exclusions covered music).
func TestListEntityTypes(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "m1", "movie", "M1")
	semearEntity(t, d, "m2", "movie", "M2")
	semearEntity(t, d, "s1", "tv", "S1")
	semearEntity(t, d, "ar1", "artista", "AR1")
	semearEntity(t, d, "al1", "album", "AL1")
	semearEntity(t, d, "al2", "album", "AL2")
	semearEntity(t, d, "al3", "album", "AL3")
	semearEntity(t, d, "fx1", "faixa", "FX1")
	semearEntity(t, d, "w1", "tv", "W1 wanted")
	definirStatus(t, d, "w1", "wanted")
	semearEntity(t, d, "dr1", "artista", "DR1 draft")
	definirStatus(t, d, "dr1", "draft")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/types", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out []entityTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar []entityTypeDTO: %v (corpo=%s)", err, rec.Body.Bytes())
	}
	byType := map[string]int64{}
	for _, tt := range out {
		byType[tt.Type] = tt.Count
	}
	eq(t, "movie count", byType["movie"], int64(2))
	eq(t, "tv count", byType["tv"], int64(2)) // s1 (cataloged) + w1 (wanted)
	eq(t, "artista count", byType["artista"], int64(1))
	if _, ok := byType["album"]; ok {
		t.Errorf("album (sub-nó) não deveria aparecer, byType=%+v", byType)
	}
	if _, ok := byType["faixa"]; ok {
		t.Errorf("faixa (sub-nó) não deveria aparecer, byType=%+v", byType)
	}
}

// TestListEntityTypesNaoColideComGetPorId ensures that the static route
// /entities/types is not captured by the /entities/{id} wildcard: without a literal
// entity with id "types" in the database, the types handler answers 200 with the list --
// if the collision existed, it would fall into getEntity and return 404 "entidade não
// encontrada".
func TestListEntityTypesNaoColideComGetPorId(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "m1", "movie", "M1")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/types", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s) -- rota /types caiu no wildcard /{id}?", rec.Code, rec.Body.String())
	}
	var out []entityTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar []entityTypeDTO: %v (corpo=%s)", err, rec.Body.Bytes())
	}
	if len(out) != 1 || out[0].Type != "movie" || out[0].Count != 1 {
		t.Fatalf("out = %+v, quero [{movie 1}]", out)
	}

	// A literal id "types" never existed -- a regular GET /entities/{id} for any id
	// other than "types" still answers 404, proving that the static route did not
	// "steal" the wildcard for other ids.
	rec404 := do(t, h, http.MethodGet, "/api/v1/entities/inexistente", nil)
	if rec404.Code != http.StatusNotFound {
		t.Errorf("GET /entities/inexistente: status = %d, quero 404", rec404.Code)
	}
}

func TestGetEntityResolvido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Entity with titles, tags and external_ids, one present file and one poster.
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              "x",
		Type:            "movie",
		Title:           "Matrix",
		OriginalTitle:   nsOf("The Matrix"),
		Description:     nsOf("Neo acorda."),
		ReleaseDate:     nsOf("1999-03-31"),
		TagsJson:        nsOf(`["ação","ficção"]`),
		ExternalIdsJson: nsOf(`{"tmdb":"603","imdb":"tt0133093"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	semearFile(t, d, "xf", "x", "/lib/matrix.mkv", "present", 1, "")
	semearAsset(t, d, "xa", "x", "poster", "/lib/matrix.jpg")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/x", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar Entidade: %v (corpo=%s)", err, rec.Body.String())
	}
	eq(t, "id", ent.ID, "x")
	eq(t, "title", ent.Title, "Matrix")
	eqPtr(t, "original_title", ent.OriginalTitle, "The Matrix")
	eq(t, "owned", ent.Owned, true)
	eq(t, "file_count", ent.FileCount, 1)
	eq(t, "len(tags)", len(ent.Tags), 2)
	eq(t, "external_ids.tmdb", ent.ExternalIDs["tmdb"], "603")
	eq(t, "external_ids.imdb", ent.ExternalIDs["imdb"], "tt0133093")
	eqPtr(t, "poster_url", ent.PosterURL, "/api/v1/entities/x/assets/poster")
}

// TestGetEntityOverrideComposto covers the end-to-end wiring of the override in the
// handler: it writes a row into media_override (through the UPSERT) and checks that
// GET /{id} reflects the composed value — the overridden title — and that extension_data
// shows up as a composed object.
func TestGetEntityOverrideComposto(t *testing.T) {
	h, d := setupEntitiesServer(t)
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:                "x",
		Type:              "movie",
		Title:             "Título Original",
		TagsJson:          nsOf(`["a","b"]`),
		ExtensionDataJson: nsOf(`{"music_video":{"bpm":120}}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	// Owner's patch: overrides the title, removes the tag "a" and adjusts the bpm.
	_, err = escrita(t, d).UpsertMediaOverride(context.Background(), sqlcgen.UpsertMediaOverrideParams{
		EntityID:     "x",
		OverrideJson: nsOf(`{"title":"Título do Dono","tags":{"remove":["a"]},"extension_data":{"music_video":{"bpm":128}}}`),
	})
	if err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/x", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar Entidade: %v (corpo=%s)", err, rec.Body.String())
	}
	eq(t, "title", ent.Title, "Título do Dono")
	// tags: base [a,b], removing a → [b].
	if len(ent.Tags) != 1 || ent.Tags[0] != "b" {
		t.Errorf("tags = %v, quero [b]", ent.Tags)
	}
	mv, ok := ent.ExtensionData["music_video"].(map[string]any)
	if !ok {
		t.Fatalf("extension_data.music_video ausente: %#v", ent.ExtensionData)
	}
	if bpm, _ := mv["bpm"].(float64); bpm != 128 {
		t.Errorf("bpm = %v, quero 128 (override)", mv["bpm"])
	}
}

// TestGetEntitySemOverrideExtensionDataVazio ensures that, without an override, the
// resolved shape brings extension_data as an object (never null).
func TestGetEntitySemOverrideExtensionDataVazio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Filme")
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	ed, ok := raw["extension_data"]
	if !ok || string(ed) != "{}" {
		t.Errorf("extension_data = %s, quero {} (presente e não-null)", ed)
	}
}

func TestGetEntityNaoEncontrado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/nao-existe", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

func TestGetEntityFiles(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Filme")
	mi := `{"general":{"format":"matroska","duration_seconds":120.5,"size_bytes":1024,"bitrate":800},` +
		`"video":[{"codec":"hevc","width":3840,"height":2160,"hdr":"HDR10"}],` +
		`"audio":[{"codec":"eac3","channels":6,"language":"por"}]}`
	// File with edition/part_no and mediainfo.
	_, err := escrita(t, d).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID:            "f1",
		EntityID:      "e",
		Path:          "/lib/filme.mkv",
		State:         "present",
		Edition:       nsOf("Director's Cut"),
		PartNo:        sql.NullInt64{Int64: 1, Valid: true},
		Size:          sql.NullInt64{Int64: 1024, Valid: true},
		MediainfoJson: nsOf(mi),
	})
	if err != nil {
		t.Fatalf("CriarMediaFile: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/files", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out filesListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar filesListDTO: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d, quero 1", len(out.Items))
	}
	f := out.Items[0]
	eqPtr(t, "edition", f.Edition, "Director's Cut")
	eqPtr(t, "part_no", f.PartNo, int64(1))
	eqPtr(t, "size", f.Size, int64(1024))
	// Mediainfo forwarded: it must carry the technical facts (codec/resolution/HDR).
	var parsed map[string]any
	if err := json.Unmarshal(f.Mediainfo, &parsed); err != nil {
		t.Fatalf("mediainfo não é JSON válido: %v (%s)", err, f.Mediainfo)
	}
	if parsed["video"] == nil {
		t.Errorf("mediainfo sem faixa de vídeo: %s", f.Mediainfo)
	}
}

// obterResolvida GETs /entities/{id} and decodes the ResolvedEntity (a local helper for
// the series tree read tests).
func obterResolvida(t *testing.T, h http.Handler, id string) resolver.Entidade {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, quero 200 (corpo=%s)", id, rec.Code, rec.Body.String())
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar Entidade(%s): %v", id, err)
	}
	return ent
}

// TestSerieLeituraPosseEArquivosDeEpisodio covers the READ side of the series tree:
//   - the series file has media_file.entity_id = SERIES, and the per-episode link lives
//     in media_file_entity (kind='tv_episode'); the EPISODE's detail/files come from the
//     join, not from media_file.entity_id;
//   - a season exposes aggregate ownership (owned episodes / total of the direct
//     children);
//   - a series sums across seasons (the direct query would count zero);
//   - a movie has no ownership (null).
func TestSerieLeituraPosseEArquivosDeEpisodio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "ep1", "episode", "Ep 1")
	semearEntity(t, d, "ep2", "episode", "Ep 2")
	semearEntity(t, d, "ep3", "episode", "Ep 3")
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-ep1", "se1", "ep1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-ep2", "se1", "ep2", "tv_episode", 2)
	ligarFilhoOrdenado(t, d, "r-ep3", "se1", "ep3", "tv_episode", 3)
	// Series file: entity_id = SERIES (the work), linked to episode 1 through the join.
	semearFile(t, d, "mf1", "s1", "/lib/serie/s01e01.mkv", "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "ep1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	// Episode 1: /files brings the file through the join, even with entity_id pointing at
	// the series.
	rec := do(t, h, http.MethodGet, "/api/v1/entities/ep1/files", nil)
	var files filesListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("decodificar filesListDTO: %v", err)
	}
	if len(files.Items) != 1 || files.Items[0].Path != "/lib/serie/s01e01.mkv" {
		t.Fatalf("arquivos do ep1 = %+v, quero 1 arquivo s01e01", files.Items)
	}
	ep1 := obterResolvida(t, h, "ep1")
	eq(t, "ep1 owned", ep1.Owned, true)
	eq(t, "ep1 file_count", ep1.FileCount, 1)
	if ep1.Ownership != nil {
		t.Errorf("ep1 ownership = %+v, quero nil (episodio nao agrega)", ep1.Ownership)
	}

	// Episode 2: no linked file.
	ep2 := obterResolvida(t, h, "ep2")
	eq(t, "ep2 owned", ep2.Owned, false)
	eq(t, "ep2 file_count", ep2.FileCount, 0)

	// Season: 1 of 3 episodes owned.
	se1 := obterResolvida(t, h, "se1")
	if se1.Ownership == nil {
		t.Fatalf("se1 ownership = nil, quero {1,3}")
	}
	eq(t, "se1 owned_count", se1.Ownership.OwnedCount, 1)
	eq(t, "se1 total_count", se1.Ownership.TotalCount, 3)

	// Series: sums across seasons (the direct query would count zero) -> 1 of 3.
	s1 := obterResolvida(t, h, "s1")
	if s1.Ownership == nil {
		t.Fatalf("s1 ownership = nil, quero {1,3}")
	}
	eq(t, "s1 owned_count", s1.Ownership.OwnedCount, 1)
	eq(t, "s1 total_count", s1.Ownership.TotalCount, 3)

	// Movie: no ownership aggregation (ownership null).
	semearEntity(t, d, "m1", "movie", "Filme")
	m1 := obterResolvida(t, h, "m1")
	if m1.Ownership != nil {
		t.Errorf("m1 ownership = %+v, quero nil (filme)", m1.Ownership)
	}
}

func TestGetEntityFilesEntityNaoEncontrada(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/nada/files", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

func TestGetPosterServeComContentType(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Real datasource on disk; poster inside it.
	root := t.TempDir()
	posterPath := filepath.Join(root, "poster.png")
	// PNG signature + arbitrary bytes (the content-type comes from the extension).
	conteudo := append([]byte("\x89PNG\r\n\x1a\n"), []byte("dados-do-poster")...)
	if err := os.WriteFile(posterPath, conteudo, 0o644); err != nil {
		t.Fatalf("WriteFile poster: %v", err)
	}
	semearSource(t, d, "src", root)
	semearEntity(t, d, "e", "movie", "Filme")
	semearAsset(t, d, "pa", "e", "poster", posterPath)

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/assets/poster", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, quero image/png", ct)
	}
	if !bytesEqual(rec.Body.Bytes(), conteudo) {
		t.Errorf("corpo do poster diverge do arquivo (len=%d vs %d)", rec.Body.Len(), len(conteudo))
	}
}

func TestGetEntityFanartServe(t *testing.T) {
	h, d := setupEntitiesServer(t)
	root := t.TempDir()
	fanartArq := filepath.Join(root, "fanart.png")
	conteudo := append([]byte("\x89PNG\r\n\x1a\n"), []byte("dados-do-fundo")...)
	if err := os.WriteFile(fanartArq, conteudo, 0o644); err != nil {
		t.Fatalf("WriteFile fanart: %v", err)
	}
	semearSource(t, d, "src", root)
	semearEntity(t, d, "e", "movie", "Filme")
	semearAsset(t, d, "fa", "e", "fanart", fanartArq)

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/assets/fanart", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if !bytesEqual(rec.Body.Bytes(), conteudo) {
		t.Errorf("corpo do fundo diverge do arquivo (len=%d)", rec.Body.Len())
	}
}

func TestGetEntityFanartAusente404(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "SemFundo")
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/assets/fanart", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestResolveBackdropLocalQuandoHaFanart: with a fanart asset, GET /{id} serves
// backdrop_url as the LOCAL route (not the source's direct URL).
func TestResolveBackdropLocalQuandoHaFanart(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "ComFundo")
	semearAsset(t, d, "fa", "e", "fanart", "/lib/fundo.jpg")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar Entidade: %v", err)
	}
	eqPtr(t, "backdrop_url", ent.BackdropURL, "/api/v1/entities/e/assets/fanart")
}

func TestGetPosterBloqueiaPathTraversal(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Registered datasource.
	root := t.TempDir()
	semearSource(t, d, "src", root)

	// (1) File OUTSIDE any datasource (another, unregistered tempdir).
	fora := t.TempDir()
	arqFora := filepath.Join(fora, "segredo.png")
	if err := os.WriteFile(arqFora, []byte("secreto"), 0o644); err != nil {
		t.Fatalf("WriteFile fora: %v", err)
	}
	semearEntity(t, d, "e1", "movie", "Fora")
	semearAsset(t, d, "a1", "e1", "poster", arqFora)
	rec1 := do(t, h, http.MethodGet, "/api/v1/entities/e1/assets/poster", nil)
	if rec1.Code != http.StatusForbidden {
		t.Fatalf("fora do datasource: status = %d, quero 403 (corpo=%s)", rec1.Code, rec1.Body.String())
	}
	checkErrorShape(t, rec1, "forbidden")

	// (2) Path with '..' escaping the datasource: root/../<something>/etc-fake.
	escapa := filepath.Join(root, "..", "escapa.png")
	semearEntity(t, d, "e2", "movie", "Escapa")
	semearAsset(t, d, "a2", "e2", "poster", escapa)
	rec2 := do(t, h, http.MethodGet, "/api/v1/entities/e2/assets/poster", nil)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("path traversal '..': status = %d, quero 403", rec2.Code)
	}
	checkErrorShape(t, rec2, "forbidden")
}

func TestGetPosterAusente404(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "SemArte")
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/assets/poster", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestLeiturasNaoUsamConexaoDeEscrita proves that ALL the read routes use only the read
// pool: after seeding, we close the WRITE pool and the reads keep answering 200. If any
// handler touched d.Write, it would break here.
func TestLeiturasNaoUsamConexaoDeEscrita(t *testing.T) {
	h, d := setupEntitiesServer(t)
	root := t.TempDir()
	posterPath := filepath.Join(root, "p.png")
	if err := os.WriteFile(posterPath, []byte("\x89PNG\r\n\x1a\nx"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	semearSource(t, d, "src", root)
	semearEntity(t, d, "e", "movie", "Filme")
	semearFile(t, d, "f", "e", "/lib/f.mkv", "present", 1, "")
	semearAsset(t, d, "pa", "e", "poster", posterPath)

	// Closes ONLY the write pool; the read one stays alive.
	if err := d.Write.Close(); err != nil {
		t.Fatalf("fechar Write: %v", err)
	}

	casos := []string{
		"/api/v1/entities",
		"/api/v1/entities/e",
		"/api/v1/entities/e/files",
		"/api/v1/entities/e/assets/poster",
	}
	for _, alvo := range casos {
		rec := do(t, h, http.MethodGet, alvo, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s após fechar Write: status = %d, quero 200 (corpo=%s)", alvo, rec.Code, rec.Body.String())
		}
	}
}

// bytesEqual compares two byte slices (avoids an extra dependency).
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
