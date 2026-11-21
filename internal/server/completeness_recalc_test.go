package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Tests of the BATCH PATH of the catalog completeness:
// RecalcularCompletudeCatalogacao writes the cache (entity_catalog_completeness)
// that the listing/`/members` read -- see the long note in completeness.go about
// why the listing does not compute it live.

// TestRecalcularCompletudeCatalogacaoGravaLinhaPorEntity covers the central criterion:
// RecalcularCompletudeCatalogacao writes one row per NON-DRAFT entity in
// entity_catalog_completeness (skipping drafts), and the listing
// (GET /api/v1/entities) starts returning a non-null catalog_completeness
// for them, read from the cache.
func TestRecalcularCompletudeCatalogacaoGravaLinhaPorEntity(t *testing.T) {
	h, d := setupEntitiesServer(t)
	corpo := catalogCompletenessSettings{"movie": {Description: true}}
	if rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo); rec.Code != http.StatusOK {
		t.Fatalf("PUT settings status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}

	semearEntity(t, d, "m1", "movie", "Sem sinopse")
	semearEntity(t, d, "m2", "movie", "Com sinopse")
	if _, err := escrita(t, d).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		ID: "m2", Type: "movie", Title: "Com sinopse", Status: "cataloged",
		Description: nsOf("Uma sinopse"),
	}); err != nil {
		t.Fatalf("AtualizarEntity m2: %v", err)
	}
	semearEntity(t, d, "d1", "movie", "Draft")
	definirStatus(t, d, "d1", "draft")
	// "collection" has no entry in the config -- evaluated, always complete,
	// without calling the resolver (the "absent type" branch of the batch recalculation).
	semearEntity(t, d, "col1", "collection", "Coleção")

	s := New("v-teste", d)
	avaliados, incompletos, err := s.RecalcularCompletudeCatalogacao(context.Background(), nil)
	if err != nil {
		t.Fatalf("RecalcularCompletudeCatalogacao: %v", err)
	}
	// m1 (incomplete) + m2 (complete) + col1 (complete, type without ruler) = 3;
	// d1 (draft) is skipped.
	if avaliados != 3 {
		t.Errorf("avaliados = %d, quero 3 (draft não entra)", avaliados)
	}
	if incompletos != 1 {
		t.Errorf("incompletos = %d, quero 1 (só m1)", incompletos)
	}

	// The listing reads from the CACHE: non-null catalog_completeness for the
	// non-draft ones, reflecting the written result.
	rec := do(t, h, http.MethodGet, "/api/v1/entities", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /entities status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	list := decodeList(t, rec.Body.Bytes())
	porID := map[string]entityCardDTO{}
	for _, item := range list.Items {
		porID[item.ID] = item
	}

	m1 := porID["m1"]
	if m1.CatalogCompleteness == nil {
		t.Fatal("m1.catalog_completeness = nil, quero cache preenchido")
	}
	if m1.CatalogCompleteness.Complete {
		t.Error("m1.complete = true, quero false (sem sinopse)")
	}

	m2 := porID["m2"]
	if m2.CatalogCompleteness == nil || !m2.CatalogCompleteness.Complete {
		t.Errorf("m2.catalog_completeness = %+v, quero {true, []}", m2.CatalogCompleteness)
	}

	col1 := porID["col1"]
	if col1.CatalogCompleteness == nil || !col1.CatalogCompleteness.Complete {
		t.Errorf("col1.catalog_completeness = %+v, quero {true, []} (tipo sem régua)", col1.CatalogCompleteness)
	}
}

// TestAvaliarERecalcularUmItemEntityAusenteDevolveErro covers the best-effort
// criterion of the loop: when an entity disappears between the listing and the
// processing (concurrent purge -- same scenario as
// autoMatchLote/EnriquecerRetroativo), avaliarERecalcularUmItem returns an
// error (it does not panic); it is this error that RecalcularCompletudeCatalogacao only
// logs and moves on to the next item, without aborting the batch.
func TestAvaliarERecalcularUmItemEntityAusenteDevolveErro(t *testing.T) {
	_, d := setupEntitiesServer(t)
	s := New("v-teste", d)
	cfg := catalogCompletenessSettings{"movie": {Description: true}}
	w := sqlcgen.New(d.Write)

	fantasma := sqlcgen.Entity{ID: "fantasma", Type: "movie", Status: "cataloged"}
	if _, err := s.avaliarERecalcularUmItem(context.Background(), w, cfg, fantasma); err == nil {
		t.Fatal("esperava erro para entity que não existe mais, obteve nil")
	}
}

// TestRecalcularCompletudeCatalogacaoNaoAbortaComItemIsoladoRuim proves, at
// the loop level, that ONE failing item (entity referenced but absent from
// the database -- it cannot happen via the real ListarEntities, but the loop handles the
// error of ONE item without interrupting the others) does not prevent the OTHER items of
// the same run from being evaluated and written: it seeds several valid items
// and confirms that ALL of them appear in the cache at the end, including the ones that
// come AFTER an item whose type is not in the config (branch without resolver).
func TestRecalcularCompletudeCatalogacaoAvaliaTodosOsItensValidos(t *testing.T) {
	h, d := setupEntitiesServer(t)
	corpo := catalogCompletenessSettings{"movie": {Description: true}}
	if rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo); rec.Code != http.StatusOK {
		t.Fatalf("PUT settings status = %d", rec.Code)
	}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		semearEntity(t, d, id, "movie", "Filme "+id)
	}

	s := New("v-teste", d)
	avaliados, _, err := s.RecalcularCompletudeCatalogacao(context.Background(), nil)
	if err != nil {
		t.Fatalf("RecalcularCompletudeCatalogacao: %v", err)
	}
	if avaliados != 5 {
		t.Fatalf("avaliados = %d, quero 5 (todos processados, nenhum abortou o lote)", avaliados)
	}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		var n int
		if err := d.Read.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM entity_catalog_completeness WHERE entity_id = ?", id).Scan(&n); err != nil {
			t.Fatalf("count(%s): %v", id, err)
		}
		if n != 1 {
			t.Errorf("entity_catalog_completeness[%s] count = %d, quero 1", id, n)
		}
	}
}

// TestEntityCatalogCompletenessApagadaEmCascataAoPurgarEntity covers the
// acceptance criterion: the cache row goes away together when the entity is purged
// (ON DELETE CASCADE of migration 0014) -- no orphan row.
func TestEntityCatalogCompletenessApagadaEmCascataAoPurgarEntity(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "m1", "movie", "Filme")

	s := New("v-teste", d)
	if _, _, err := s.RecalcularCompletudeCatalogacao(context.Background(), nil); err != nil {
		t.Fatalf("RecalcularCompletudeCatalogacao: %v", err)
	}

	var antes int
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM entity_catalog_completeness WHERE entity_id = ?", "m1").Scan(&antes); err != nil {
		t.Fatalf("count antes: %v", err)
	}
	if antes != 1 {
		t.Fatalf("linha de completude não foi gravada antes da purga (count=%d)", antes)
	}

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/m1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}

	var depois int
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM entity_catalog_completeness WHERE entity_id = ?", "m1").Scan(&depois); err != nil {
		t.Fatalf("count depois: %v", err)
	}
	if depois != 0 {
		t.Errorf("entity_catalog_completeness ainda tem linha para m1 após purga (count=%d), quero 0 (ON DELETE CASCADE)", depois)
	}
}
