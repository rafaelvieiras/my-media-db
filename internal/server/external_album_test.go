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

// Este arquivo testa GET /external/{source}/album/{id} (external_album.go) e o
// import de faixa avulsa (POST /import type=faixa, enrich.go) — briefing 0115
// Parte 2. Reusa o molde de external_colecao_test.go (fonte fake sem rede) mais o
// padrão de semeadura direta de entities/entity_relation já usado por
// members_test.go/collections_test.go.

const (
	albumMbidFixture  = "rg-1"
	faixa1MbidFixture = "track-mbid-1" // já catalogada (internal_id preenchido)
	faixa2MbidFixture = "track-mbid-2" // faltando (internal_id ausente)
)

// albumExternoFixture é o único álbum que fakeAlbumProvider conhece: 2 faixas, a
// 1ª já catalogada internamente nos testes que semeiam a faixa, a 2ª sempre
// "faltando".
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

// fakeAlbumProvider implementa o core provider.Provider (mínimo) + a capacidade
// provider.MusicaAlbumProvider, sem rede real, sob o nome "musicbrainz" — só
// conhece albumExternoFixture (mbid "rg-1"); qualquer outro mbid é
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

// BuscarAlbumDaFaixa/BuscarAlbumPorNome não são exercitados por estes testes
// (só o caminho DIRETO por mbid, BuscarAlbumPorMbid, é usado pela tela externa
// de álbum e pelo import de faixa avulsa) -- stubs só para satisfazer a
// interface provider.MusicaAlbumProvider completa (o type-assert falha em
// runtime se faltar QUALQUER método).
func (fakeAlbumProvider) BuscarAlbumDaFaixa(context.Context, string, []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (fakeAlbumProvider) BuscarAlbumPorNome(context.Context, string, string, []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

// setupAlbumServer sobe um Server com o fakeAlbumProvider registrado e um
// assetsDir temporário (necessário para s.enrich != nil, usado pelos testes de
// import de faixa avulsa).
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

// TestExternalAlbumInternalIdDasJaCatalogadas: semeia a entity álbum interna
// (mbid "rg-1") e a entity faixa interna da 1ª faixa (mbid "track-mbid-1"),
// ligada por entity_relation kind=music_album — a tela externa deve marcar
// internal_id no álbum e nessa faixa, e deixar a 2ª faixa (nunca catalogada) sem
// internal_id.
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
	h, _ := setupSearchServer(t) // sem provider => sem capacidade de album
	rec := do(t, h, http.MethodGet, "/api/v1/external/musicbrainz/album/"+albumMbidFixture, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "external_source_unavailable")
}

// TestImportFaixaAvulsaLigaAoAlbumPai cobre o import de uma faixa FALTANTE
// (briefing 0115 Parte 2): POST /import type=faixa com parent_id=album interno
// já catalogado cria a faixa e liga por entity_relation(kind=music_album,
// sort_index=track_number).
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

	// Reimport (mesmo source_id/parent_id): reusa, não duplica a relação.
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

// TestImportFaixaSemParentID422 cobre a validação de entrada: type=faixa sem
// parent_id nunca tenta importar (não há como ligar a faixa a um álbum).
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

// TestImportFaixaAlbumPaiSemMbid422Like cobre o álbum pai sem mbid externo (dado
// impossível em condições normais, mas o import não deve produzir uma faixa
// órfã): a fonte falha ao resolver e o handler responde 502 (mesmo contrato de
// "fonte externa falhou" dos demais imports).
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
