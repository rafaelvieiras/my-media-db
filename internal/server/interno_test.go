package server

import (
	"context"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Testes da resolução externo→interno (briefing 0026): os helpers idInternoItem/
// idInternoColecao e a exposição do internal_id nos DTOs externos (página da coleção
// e banda de coleção do detalhe externo/entity).

// semearColecaoTmdb cria uma entity type='collection' com o external_id tmdb dado,
// para os lookups de dedup a acharem (ObterColecaoPorTmdb).
func semearColecaoTmdb(t *testing.T, d *db.DB, id, tmdbID, titulo string) {
	t.Helper()
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              id,
		Type:            "collection",
		Title:           titulo,
		ExternalIdsJson: nsOf(`{"tmdb":"` + tmdbID + `"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity(colecao %s): %v", id, err)
	}
}

// semearFilmeTmdb cria uma entity movie com o external_id tmdb dado (ObterEntityPorTmdb).
func semearFilmeTmdb(t *testing.T, d *db.DB, id, tmdbID, titulo string) {
	t.Helper()
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              id,
		Type:            "movie",
		Title:           titulo,
		ExternalIdsJson: nsOf(`{"tmdb":"` + tmdbID + `"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity(filme %s): %v", id, err)
	}
}

// TestIdInternoResolveExterno: idInternoItem/idInternoColecao acham a entity existente
// (por id tmdb), devolvem nil para id inexistente e nil para fonte != tmdb (só o tmdb
// deduplica hoje).
func TestIdInternoResolveExterno(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearFilmeTmdb(t, d, "ent-603", "603", "Matrix")
	semearColecaoTmdb(t, d, "col-2344", "2344", "Colecao Matrix")
	s := New("v-teste", d)
	ctx := context.Background()

	if got := s.idInternoItem(ctx, "tmdb", "603"); got == nil || *got != "ent-603" {
		t.Errorf("idInternoItem(tmdb,603) = %v, quero ent-603", got)
	}
	if got := s.idInternoItem(ctx, "tmdb", "999"); got != nil {
		t.Errorf("idInternoItem(tmdb,999) = %v, quero nil (inexistente)", got)
	}
	if got := s.idInternoItem(ctx, "letterboxd", "603"); got != nil {
		t.Errorf("idInternoItem(letterboxd,603) = %v, quero nil (fonte sem dedup)", got)
	}
	if got := s.idInternoColecao(ctx, "tmdb", "2344"); got == nil || *got != "col-2344" {
		t.Errorf("idInternoColecao(tmdb,2344) = %v, quero col-2344", got)
	}
	if got := s.idInternoColecao(ctx, "tmdb", "999"); got != nil {
		t.Errorf("idInternoColecao(tmdb,999) = %v, quero nil", got)
	}
	// Um filme (603) NÃO deve casar como coleção (a query é restrita a type='collection').
	if got := s.idInternoColecao(ctx, "tmdb", "603"); got != nil {
		t.Errorf("idInternoColecao(tmdb,603) = %v, quero nil (603 e filme, nao colecao)", got)
	}
}

// TestExternalCollectionInternalId: a página da coleção externa anota o internal_id da
// coleção e das partes JÁ importadas; parte não importada fica com internal_id nil.
func TestExternalCollectionInternalId(t *testing.T) {
	h, d := setupColecaoServer(t)
	semearColecaoTmdb(t, d, "col-int", "2344", "Colecao Matrix")
	semearFilmeTmdb(t, d, "filme-603", "603", "Matrix") // parte 603 importada; 604 nao
	dto := obterColecao(t, h)

	if dto.InternalID == nil || *dto.InternalID != "col-int" {
		t.Errorf("colecao.internal_id = %v, quero col-int", dto.InternalID)
	}
	byID := map[string]parteColecaoDTO{}
	for _, p := range dto.Parts {
		byID[p.SourceID] = p
	}
	if p := byID["603"]; p.InternalID == nil || *p.InternalID != "filme-603" {
		t.Errorf("parte 603 internal_id = %v, quero filme-603", p.InternalID)
	}
	if p := byID["604"]; p.InternalID != nil {
		t.Errorf("parte 604 internal_id = %v, quero nil (nao importada)", p.InternalID)
	}
}

// TestExternalCollectionInternalIdNulaSemImport: sem nada importado, todos os
// internal_id (coleção e partes) saem nil — não há centralização a fazer.
func TestExternalCollectionInternalIdNulaSemImport(t *testing.T) {
	dto := obterColecao(t, mustFirst(setupColecaoServer(t)))
	if dto.InternalID != nil {
		t.Errorf("colecao.internal_id = %v, quero nil", dto.InternalID)
	}
	for _, p := range dto.Parts {
		if p.InternalID != nil {
			t.Errorf("parte %s internal_id = %v, quero nil", p.SourceID, p.InternalID)
		}
	}
}

// TestExternalDetailBandInternalId: a banda de coleção do detalhe EXTERNO (movie 603,
// que pertence à coleção 2344) traz o internal_id quando a coleção já é entity.
func TestExternalDetailBandInternalId(t *testing.T) {
	h, d := setupExternalServer(t, nil)
	semearColecaoTmdb(t, d, "col-band", "2344", "Colecao Matrix")
	dto := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")
	if dto.Collection == nil {
		t.Fatal("collection nil")
	}
	if dto.Collection.InternalID == nil || *dto.Collection.InternalID != "col-band" {
		t.Errorf("collection.internal_id = %v, quero col-band", dto.Collection.InternalID)
	}
}
