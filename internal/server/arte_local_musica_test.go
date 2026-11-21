package server

// Tests of DescobrirArteLocalMusica: a music artist/album
// with an owned track gains an asset with origin='local' when the derived folder (album =
// dir of the track file; artist = one level above) has poster.jpg/folder.jpg --
// the same mechanism already used by movie/series (scanner.RefreshLocalAsset), except
// that the folder is DERIVED from the tree instead of coming straight from the entity's
// own media_file (artist/album never have a direct media_file, ADR-0027).

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

func TestDescobrirArteLocalMusicaAlbumEArtista(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "arte-local-musica.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database)
	ctx := context.Background()

	libDir := t.TempDir()
	artistaDir := filepath.Join(libDir, "Bomfunk MC's")
	albumDir := filepath.Join(artistaDir, "Freestyler (2000)")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artistaDir, "folder.jpg"), []byte("ARTISTA"), 0o644); err != nil {
		t.Fatalf("WriteFile folder.jpg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(albumDir, "poster.jpg"), []byte("ALBUM"), 0o644); err != nil {
		t.Fatalf("WriteFile poster.jpg: %v", err)
	}
	trackPath := filepath.Join(albumDir, "05 - Freestyler.mp3")
	if err := os.WriteFile(trackPath, []byte("fake mp3"), 0o644); err != nil {
		t.Fatalf("WriteFile track: %v", err)
	}

	semearEntity(t, database, "art1", "artista", "Bomfunk MC's")
	semearEntity(t, database, "alb1", "album", "Freestyler")
	semearEntity(t, database, "fx1", "faixa", "Freestyler")
	ligarFilhoOrdenado(t, database, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, database, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, database, "f1", "fx1", trackPath, "present", 100, "")

	encontrados, err := s.DescobrirArteLocalMusica(ctx, nil)
	if err != nil {
		t.Fatalf("DescobrirArteLocalMusica: %v", err)
	}
	if encontrados != 2 {
		t.Fatalf("encontrados = %d, quero 2 (album + artista)", encontrados)
	}

	r := sqlcgen.New(database.Read)
	albAsset, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: "alb1", Kind: "poster", Origin: "local",
	})
	if err != nil {
		t.Fatalf("asset local do album ausente: %v", err)
	}
	if albAsset.PathOrUri != filepath.Join(albumDir, "poster.jpg") {
		t.Errorf("path do asset do album = %q, quero %q", albAsset.PathOrUri, filepath.Join(albumDir, "poster.jpg"))
	}

	artAsset, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: "art1", Kind: "poster", Origin: "local",
	})
	if err != nil {
		t.Fatalf("asset local do artista ausente: %v", err)
	}
	if artAsset.PathOrUri != filepath.Join(artistaDir, "folder.jpg") {
		t.Errorf("path do asset do artista = %q, quero %q", artAsset.PathOrUri, filepath.Join(artistaDir, "folder.jpg"))
	}
}

// TestDescobrirArteLocalMusicaSemPasta covers the case without any poster.jpg/folder.jpg
// in the folders: nothing is created, without an error.
func TestDescobrirArteLocalMusicaSemPasta(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "arte-local-musica-vazia.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database)
	ctx := context.Background()

	libDir := t.TempDir()
	albumDir := filepath.Join(libDir, "Artista Sem Foto", "Album (2020)")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	trackPath := filepath.Join(albumDir, "01 - Faixa.mp3")
	if err := os.WriteFile(trackPath, []byte("fake mp3"), 0o644); err != nil {
		t.Fatalf("WriteFile track: %v", err)
	}

	semearEntity(t, database, "art2", "artista", "Artista Sem Foto")
	semearEntity(t, database, "alb2", "album", "Album")
	semearEntity(t, database, "fx2", "faixa", "Faixa")
	ligarFilhoOrdenado(t, database, "r3", "art2", "alb2", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, database, "r4", "alb2", "fx2", "music_album", 1)
	semearFile(t, database, "f2", "fx2", trackPath, "present", 100, "")

	encontrados, err := s.DescobrirArteLocalMusica(ctx, nil)
	if err != nil {
		t.Fatalf("DescobrirArteLocalMusica: %v", err)
	}
	if encontrados != 0 {
		t.Errorf("encontrados = %d, quero 0 (nenhum poster.jpg/folder.jpg nas pastas)", encontrados)
	}
}
