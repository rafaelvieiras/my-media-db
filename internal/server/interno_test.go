package server

import (
	"context"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// Tests of the external→internal resolution: the helpers idInternoItem/
// idInternoColecao and the exposure of internal_id in the external DTOs (collection page
// and collection band of the external detail/entity).

// semearColecaoTmdb creates a type='collection' entity with the given tmdb external_id,
// so the dedup lookups find it (ObterColecaoPorTmdb).
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

// semearFilmeTmdb creates a movie entity with the given tmdb external_id
// (ObterEntityPorTmdb).
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

// semearSerieTmdb creates a type='tv' entity with the given tmdb external_id, so the
// interno-first of the series similar items matches by tmdb id (ObterEntityPorTmdb
// does not restrict by type — a series matches like a movie does).
func semearSerieTmdb(t *testing.T, d *db.DB, id, tmdbID, titulo string) {
	t.Helper()
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              id,
		Type:            "tv",
		Title:           titulo,
		ExternalIdsJson: nsOf(`{"tmdb":"` + tmdbID + `"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity(serie %s): %v", id, err)
	}
}

// TestSerieSimilarInternalId: the SIMILAR ITEMS of a SERIES gain internal_id
// when the similar item (tmdb id, coming from TMDB route C) is already in the library —
// the same interno-first post-process (anotarInternos) that already serves
// movie/collection matches a series, because idInternoItem resolves by tmdb id regardless
// of the type. A similar item that was not imported stays nil.
func TestSerieSimilarInternalId(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearSerieTmdb(t, d, "serie-1399", "1399", "A Guerra dos Tronos")
	s := New("v-teste", d)

	e := resolver.Entidade{
		Type: "tv",
		Similar: []richfields.Similar{
			{SourceID: "1399", Type: "tv", Title: "A Guerra dos Tronos"},
			{SourceID: "9999", Type: "tv", Title: "Serie Ausente"},
		},
	}
	s.anotarInternos(context.Background(), &e)

	if e.Similar[0].InternalID == nil || *e.Similar[0].InternalID != "serie-1399" {
		t.Errorf("similar[0].internal_id = %v, quero serie-1399", e.Similar[0].InternalID)
	}
	if e.Similar[1].InternalID != nil {
		t.Errorf("similar[1].internal_id = %v, quero nil (nao importada)", e.Similar[1].InternalID)
	}
}

// TestIdInternoResolveExterno: idInternoItem/idInternoColecao find the existing entity
// (by tmdb id), return nil for a nonexistent id and nil for a source != tmdb (only tmdb
// deduplicates today).
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
	// A movie (603) must NOT match as a collection (the query is restricted to
	// type='collection').
	if got := s.idInternoColecao(ctx, "tmdb", "603"); got != nil {
		t.Errorf("idInternoColecao(tmdb,603) = %v, quero nil (603 e filme, nao colecao)", got)
	}
}

// TestExternalCollectionInternalId: the external collection page annotates the internal_id
// of the collection and of the ALREADY imported parts; a part that was not imported stays
// with internal_id nil.
func TestExternalCollectionInternalId(t *testing.T) {
	h, d := setupColecaoServer(t)
	semearColecaoTmdb(t, d, "col-int", "2344", "Colecao Matrix")
	semearFilmeTmdb(t, d, "filme-603", "603", "Matrix") // part 603 imported; 604 not
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

// TestExternalCollectionInternalIdNulaSemImport: with nothing imported, all the
// internal_ids (collection and parts) come out nil — there is no centralization to
// do.
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

// TestExternalDetailBandInternalId: the collection band of the EXTERNAL detail (movie 603,
// which belongs to collection 2344) brings the internal_id when the collection is already
// an entity.
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
