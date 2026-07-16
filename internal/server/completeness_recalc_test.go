package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Testes do CAMINHO EM LOTE da completude de catalogação (briefing 0058):
// RecalcularCompletudeCatalogacao grava o cache (entity_catalog_completeness)
// que a listagem/`/members` leem -- ver a nota longa em completeness.go sobre
// por que a listagem não calcula ao vivo.

// TestRecalcularCompletudeCatalogacaoGravaLinhaPorEntity cobre o critério
// central do briefing 0058: RecalcularCompletudeCatalogacao grava uma linha
// por entity NÃO-DRAFT em entity_catalog_completeness (pulando drafts), e a
// listagem (GET /api/v1/entities) passa a devolver catalog_completeness
// não-nulo para elas, lido do cache.
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
	// "collection" não tem entrada na config -- avaliada, sempre completa,
	// sem chamar o resolver (branch "tipo ausente" do recálculo em lote).
	semearEntity(t, d, "col1", "collection", "Coleção")

	s := New("v-teste", d)
	avaliados, incompletos, err := s.RecalcularCompletudeCatalogacao(context.Background())
	if err != nil {
		t.Fatalf("RecalcularCompletudeCatalogacao: %v", err)
	}
	// m1 (incompleto) + m2 (completo) + col1 (completo, tipo sem régua) = 3;
	// d1 (draft) é pulada.
	if avaliados != 3 {
		t.Errorf("avaliados = %d, quero 3 (draft não entra)", avaliados)
	}
	if incompletos != 1 {
		t.Errorf("incompletos = %d, quero 1 (só m1)", incompletos)
	}

	// A listagem lê do CACHE: catalog_completeness não-nulo para as
	// não-draft, refletindo o resultado gravado.
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

// TestAvaliarERecalcularUmItemEntityAusenteDevolveErro cobre o critério
// best-effort do loop: quando uma entity some entre a listagem e o
// processamento (purga concorrente -- mesmo cenário de
// autoMatchLote/EnriquecerRetroativo), avaliarERecalcularUmItem devolve um
// erro (não panica); é esse erro que RecalcularCompletudeCatalogacao apenas
// loga e segue para o próximo item, sem abortar o lote.
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

// TestRecalcularCompletudeCatalogacaoNaoAbortaComItemIsoladoRuim prova, no
// nível do loop, que UM item que falha (entity referenciada mas ausente do
// banco -- não pode acontecer via ListarEntities real, mas o loop trata o
// erro de UM item sem interromper os demais) não impede os OUTROS itens da
// mesma execução de serem avaliados e gravados: semeia vários itens válidos
// e confirma que TODOS aparecem no cache ao final, incluindo os que vêm
// DEPOIS de um item cujo tipo não está na config (branch sem resolver).
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
	avaliados, _, err := s.RecalcularCompletudeCatalogacao(context.Background())
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

// TestEntityCatalogCompletenessApagadaEmCascataAoPurgarEntity cobre o
// critério de aceite: a linha do cache some junto quando a entity é purgada
// (ON DELETE CASCADE da migration 0014) -- sem linha órfã.
func TestEntityCatalogCompletenessApagadaEmCascataAoPurgarEntity(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "m1", "movie", "Filme")

	s := New("v-teste", d)
	if _, _, err := s.RecalcularCompletudeCatalogacao(context.Background()); err != nil {
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
