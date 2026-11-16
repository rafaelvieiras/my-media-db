package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// This file tests GET /external/{source}/album/{id} (external_album.go) and the
// standalone track import (POST /import type=faixa, enrich.go). It reuses the mold of
// external_colecao_test.go (fake source without network) plus the pattern of direct
// seeding of entities/entity_relation already used by
// members_test.go/collections_test.go.

const (
	albumMbidFixture  = "rg-1"
	faixa1MbidFixture = "track-mbid-1" // already cataloged (internal_id filled)
	faixa2MbidFixture = "track-mbid-2" // missing (internal_id absent)
)

// albumExternoFixture is the only album that fakeAlbumProvider knows: 2 tracks, the
// 1st already cataloged internally in the tests that seed the track, the 2nd always
// "missing".
func albumExternoFixture() provider.AlbumExterno {
	return provider.AlbumExterno{
		Name:  "Fake Album",
		MBID:  albumMbidFixture,
		Year:  "1999",
		Genre: "rock",
		Faixas: []provider.FaixaExterna{
			{Title: "Track One", MBID: faixa1MbidFixture, TrackNumber: 1, DiscNumber: 1},
			{Title: "Track Two", MBID: faixa2MbidFixture, TrackNumber: 2, DiscNumber: 1},
		},
	}
}

// fakeAlbumProvider implements the core provider.Provider (minimum) + the
// provider.MusicaAlbumProvider capability, without a real network, under the name
// "musicbrainz" — it only knows albumExternoFixture (mbid "rg-1"); any other mbid is
// provider.ErrNaoEncontrado.
type fakeAlbumProvider struct{}

func (fakeAlbumProvider) Nome() string { return "musicbrainz" }

func (fakeAlbumProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (fakeAlbumProvider) BuscarDetalhe(_ context.Context, sourceID, _ string, _ []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (fakeAlbumProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (fakeAlbumProvider) BuscarAlbumPorMbid(_ context.Context, albumMBID string, _ []string) (provider.AlbumExterno, error) {
	if albumMBID != albumMbidFixture {
		return provider.AlbumExterno{}, provider.ErrNaoEncontrado
	}
	return albumExternoFixture(), nil
}

// BuscarAlbumDaFaixa/BuscarAlbumPorNome are not exercised by these tests
// (only the DIRECT path by mbid, BuscarAlbumPorMbid, is used by the external album
// screen and by the standalone track import) -- stubs only to satisfy the complete
// provider.MusicaAlbumProvider interface (the type-assert fails at
// runtime if ANY method is missing).
func (fakeAlbumProvider) BuscarAlbumDaFaixa(context.Context, string, []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (fakeAlbumProvider) BuscarAlbumPorNome(context.Context, string, string, provider.AlvoFaixaAlbum, []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

// setupAlbumServer starts a Server with the fakeAlbumProvider registered and a temporary
// assetsDir (needed for s.enrich != nil, used by the standalone track
// import tests).
func setupAlbumServer(t *testing.T) (http.Handler, *db.DB) {
	t.Helper()
	return setupSearchServer(t, WithSearchProvider(fakeAlbumProvider{}), WithAssetsDir(t.TempDir()))
}

func decodeAlbumExterno(t *testing.T, body []byte) albumExternoDTO {
	t.Helper()
	var out albumExternoDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar albumExternoDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestExternalAlbumInternalIdDasJaCatalogadas: seeds the internal album entity
// (mbid "rg-1") and the internal track entity of the 1st track (mbid "track-mbid-1"),
// linked by entity_relation kind=music_album — the external screen must mark
// internal_id on the album and on that track, and leave the 2nd track (never cataloged)
// without internal_id.
func TestExternalAlbumInternalIdDasJaCatalogadas(t *testing.T) {
	h, d := setupAlbumServer(t)
	ctx := context.Background()

	albumExtIDs, _ := json.Marshal(map[string]string{"mbid": albumMbidFixture})
	if _, err := escrita(t, d).CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "album-1", Type: "album", Title: "Fake Album",
		ExternalIdsJson: nsOf(string(albumExtIDs)),
	}); err != nil {
		t.Fatalf("CriarEntity album: %v", err)
	}
	faixaExtIDs, _ := json.Marshal(map[string]string{"mbid": faixa1MbidFixture})
	if _, err := escrita(t, d).CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "faixa-1", Type: "faixa", Title: "Track One",
		ExternalIdsJson: nsOf(string(faixaExtIDs)),
	}); err != nil {
		t.Fatalf("CriarEntity faixa: %v", err)
	}
	if _, err := escrita(t, d).CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID: "rel-1", ParentID: "album-1", ChildID: "faixa-1", Kind: musicext.KindAlbum,
	}); err != nil {
		t.Fatalf("CriarEntityRelation: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/external/musicbrainz/album/"+albumMbidFixture+"?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeAlbumExterno(t, rec.Body.Bytes())

	eq(t, "source", out.Source, "musicbrainz")
	eq(t, "source_id", out.SourceID, albumMbidFixture)
	eq(t, "name", out.Name, "Fake Album")
	eqPtr(t, "internal_id", out.InternalID, "album-1")

	if len(out.Tracks) != 2 {
		t.Fatalf("tracks = %d, quero 2", len(out.Tracks))
	}
	eqPtr(t, "tracks[0].internal_id", out.Tracks[0].InternalID, "faixa-1")
	if out.Tracks[1].InternalID != nil {
		t.Errorf("tracks[1].internal_id = %v, quero nil (faixa faltando)", *out.Tracks[1].InternalID)
	}
	eq(t, "tracks[1].source_id", out.Tracks[1].SourceID, faixa2MbidFixture)
}

func TestExternalAlbum404(t *testing.T) {
	h, _ := setupAlbumServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/external/musicbrainz/album/mbid-inexistente?lang=pt-BR", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

func TestExternalAlbum503SemProvider(t *testing.T) {
	h, _ := setupSearchServer(t) // no provider => no album capability
	rec := do(t, h, http.MethodGet, "/api/v1/external/musicbrainz/album/"+albumMbidFixture, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "external_source_unavailable")
}

// TestImportFaixaAvulsaLigaAoAlbumPai covers the import of a MISSING track:
// POST /import type=faixa with parent_id=internal album already cataloged creates the
// track and links it by entity_relation(kind=music_album, sort_index=track_number).
func TestImportFaixaAvulsaLigaAoAlbumPai(t *testing.T) {
	h, d := setupAlbumServer(t)
	ctx := context.Background()

	albumExtIDs, _ := json.Marshal(map[string]string{"mbid": albumMbidFixture})
	if _, err := escrita(t, d).CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "album-1", Type: "album", Title: "Fake Album",
		ExternalIdsJson: nsOf(string(albumExtIDs)),
	}); err != nil {
		t.Fatalf("CriarEntity album: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{
		"source": "musicbrainz", "source_id": faixa2MbidFixture, "type": "faixa", "parent_id": "album-1",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	eq(t, "external_ids.mbid", ent.ExternalIDs["mbid"], faixa2MbidFixture)

	rels, err := escrita(t, d).ListarRelacoesFilhas(ctx, "album-1")
	if err != nil {
		t.Fatalf("ListarRelacoesFilhas: %v", err)
	}
	achou := false
	for _, r := range rels {
		if r.Kind == musicext.KindAlbum && r.ChildID == ent.ID {
			achou = true
			if !r.SortIndex.Valid || r.SortIndex.Int64 != 2 {
				t.Errorf("sort_index = %v, quero 2 (track_number)", r.SortIndex)
			}
		}
	}
	if !achou {
		t.Errorf("nenhuma entity_relation kind=music_album ligando album-1 -> %s", ent.ID)
	}

	// Reimport (same source_id/parent_id): it reuses, it does not duplicate the relation.
	rec2 := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{
		"source": "musicbrainz", "source_id": faixa2MbidFixture, "type": "faixa", "parent_id": "album-1",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("reimport status = %d, quero 200", rec2.Code)
	}
	rels2, err := escrita(t, d).ListarRelacoesFilhas(ctx, "album-1")
	if err != nil {
		t.Fatalf("ListarRelacoesFilhas: %v", err)
	}
	if len(rels2) != len(rels) {
		t.Errorf("reimport duplicou entity_relation: antes %d, depois %d", len(rels), len(rels2))
	}
}

// TestImportFaixaSemParentID422 covers the input validation: type=faixa without
// parent_id never tries to import (there is no way to link the track to an album).
func TestImportFaixaSemParentID422(t *testing.T) {
	h, _ := setupAlbumServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{
		"source": "musicbrainz", "source_id": faixa2MbidFixture, "type": "faixa",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestImportFaixaAlbumPaiSemMbid422Like covers the parent album without an external mbid
// (data impossible under normal conditions, but the import must not produce an orphan
// track): the source fails to resolve and the handler responds 502 (same "external source
// failed" contract as the other imports).
func TestImportFaixaAlbumPaiSemMbid(t *testing.T) {
	h, d := setupAlbumServer(t)
	ctx := context.Background()
	if _, err := escrita(t, d).CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "album-sem-mbid", Type: "album", Title: "Album Sem Mbid",
	}); err != nil {
		t.Fatalf("CriarEntity album: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{
		"source": "musicbrainz", "source_id": faixa2MbidFixture, "type": "faixa", "parent_id": "album-sem-mbid",
	})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502 (corpo=%s)", rec.Code, rec.Body.String())
	}
}
