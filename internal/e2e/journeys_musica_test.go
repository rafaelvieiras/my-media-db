package e2e

// MUSIC journey: it imports an artist through the manual search (the whole
// discography, ADR-0027) and completes the album with the track that was left out,
// through the external "missing tracks" screen (ADR-0029). It uses
// fake.Musicbrainz (internal/provider/fake/musicbrainz.go) — the SAME fake the real
// binary uses when MYMEDIADB_FAKE_PROVIDER=1 (cmd/mymediadb), so the fixture is not
// duplicated between Layer 1 (here) and Layer 2 (Playwright). The E2E harness of
// this layer does not start the scanner, so the path under test is the direct HTTP
// import (manual search → import artist; external screen → import a standalone
// track), not the automatic scan trigger.
//
// The per-step assertions live in their own helpers (importarArtistaEAlbum/
// assertMembrosAlbum/assertFaixaFaltando/importarFaixaAvulsa) to keep the gocyclo
// of the test function low (AGENTS.md) — each helper covers 1 verification branch,
// the main function only chains the journey.

import (
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
	"github.com/rafaelvieiras/my-media-db/internal/server"
)

func TestJornadaMusicaImportArtistaEFaixaFaltando(t *testing.T) {
	c := novoHarnessComOpcoes(t, server.WithSearchProvider(fake.NewMusicbrainz()))

	// 1) Imports the whole artist (manual search → POST /import type=artista):
	// it creates the artist + the only album + the track the fake tree brings.
	_, albumID := importarArtistaEAlbum(t, c)
	assertMembrosAlbum(t, c, albumID, 1) // only the track the tree brought

	// 2) The external album screen lists the OFFICIAL tracks of the release
	// (GET /external/musicbrainz/album/{mbid}): track 1 (already imported by the
	// tree) comes out with internal_id filled; track 2 (outside the tree) comes out
	// missing — the same internal-first contract that collection/series already use.
	faixa2SourceID := assertFaixaFaltando(t, c, albumID)

	// 3) Imports the missing standalone track, linking it to the already internal
	// PARENT album (POST /import type=faixa + parent_id).
	faixa2 := importarFaixaAvulsa(t, c, faixa2SourceID, albumID, http.StatusCreated)
	if got := str(t, faixa2, "title"); got != "Faixa Dois" {
		t.Fatalf("title da faixa avulsa = %q, quero %q", got, "Faixa Dois")
	}
	assertMembrosAlbum(t, c, albumID, 2)

	// Reimporting the same standalone track is IDEMPOTENT (find-or-create by MBID,
	// the same movie/collection pattern) — it does not duplicate the link.
	faixa2Denovo := importarFaixaAvulsa(t, c, faixa2SourceID, albumID, http.StatusOK)
	if str(t, faixa2Denovo, "id") != str(t, faixa2, "id") {
		t.Fatalf("reimport da faixa avulsa criou outra entity")
	}
	assertMembrosAlbum(t, c, albumID, 2)
}

// importarArtistaEAlbum imports the fake artist (the whole discography) and returns
// (artistaID, albumID) — the album is the only member of the newly created artist.
func importarArtistaEAlbum(t *testing.T, c *cliente) (artistaID, albumID string) {
	t.Helper()
	artista := c.obj("POST", "/api/v1/import", map[string]any{
		"source":    "musicbrainz",
		"source_id": fake.MusicbrainzArtistaID,
		"type":      "artista",
	}, http.StatusCreated)
	artistaID = str(t, artista, "id")
	if got := str(t, artista, "title"); got != "Artista Fake" {
		t.Fatalf("title do artista = %q, quero %q", got, "Artista Fake")
	}
	membros := objs(t, c.arr("GET", "/api/v1/entities/"+artistaID+"/members", nil, http.StatusOK))
	if len(membros) != 1 {
		t.Fatalf("membros (álbuns) do artista = %d, quero 1", len(membros))
	}
	return artistaID, str(t, membros[0], "id")
}

// assertMembrosAlbum checks the track (members) count of the album.
func assertMembrosAlbum(t *testing.T, c *cliente, albumID string, quero int) {
	t.Helper()
	membros := c.arr("GET", "/api/v1/entities/"+albumID+"/members", nil, http.StatusOK)
	if len(membros) != quero {
		t.Fatalf("membros (faixas) do álbum = %d, quero %d", len(membros), quero)
	}
}

// assertFaixaFaltando checks the contract of the external album screen (2 official
// tracks, the 1st already internal and the 2nd missing) and returns the MusicBrainz
// source_id of the missing track, so the caller can import it next.
func assertFaixaFaltando(t *testing.T, c *cliente, albumID string) string {
	t.Helper()
	albumExt := c.obj("GET", "/api/v1/external/musicbrainz/album/"+fake.MusicbrainzAlbumID, nil, http.StatusOK)
	if got := str(t, albumExt, "internal_id"); got != albumID {
		t.Fatalf("internal_id do álbum externo = %q, quero %q", got, albumID)
	}
	tracksRaw, ok := albumExt["tracks"].([]any)
	if !ok || len(tracksRaw) != 2 {
		t.Fatalf("tracks do álbum externo = %v, quero 2 (release oficial completo)", albumExt["tracks"])
	}
	tracks := objs(t, tracksRaw)
	if _, ok := tracks[0]["internal_id"].(string); !ok {
		t.Fatalf("track[0].internal_id ausente/vazio (deveria ser a faixa já importada): %v", tracks[0])
	}
	if tracks[1]["internal_id"] != nil {
		t.Fatalf("track[1].internal_id = %v, quero nil (faixa ainda não importada)", tracks[1]["internal_id"])
	}
	faixa2SourceID := str(t, tracks[1], "source_id")
	if faixa2SourceID != fake.MusicbrainzFaixa2ID {
		t.Fatalf("track[1].source_id = %q, quero %q", faixa2SourceID, fake.MusicbrainzFaixa2ID)
	}
	return faixa2SourceID
}

// importarFaixaAvulsa fires POST /import type=faixa, linking to the already
// internal parent album, and requires the expected HTTP status (201 the first
// time, 200 on the idempotent reimport).
func importarFaixaAvulsa(t *testing.T, c *cliente, sourceID, albumID string, statusEsperado int) map[string]any {
	t.Helper()
	return c.obj("POST", "/api/v1/import", map[string]any{
		"source":    "musicbrainz",
		"source_id": sourceID,
		"type":      "faixa",
		"parent_id": albumID,
	}, statusEsperado)
}
