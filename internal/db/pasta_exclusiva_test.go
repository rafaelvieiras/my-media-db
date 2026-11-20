package db_test

// The SQL count and the Go slice rule must answer the SAME question. Issue #73 replaced
// "load every present media_file into memory and call EhPastaExclusiva" with one indexed
// count in resolverDestino; a silent divergence would rename artwork and NFO files inside
// the owner's library, so the two forms are checked against the same data here.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
)

// arquivoDeTeste is one media_file row: the path, its state, and whether it belongs to
// the entity under test.
type arquivoDeTeste struct {
	path  string
	state string
}

func TestContarMediaFilesPresentesNaPastaBateComEhPastaExclusiva(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "pasta.sqlite")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// The whole library of this test, spread over the cases that matter.
	arquivos := []arquivoDeTeste{
		{"/lib/um-filme/Um Filme.mkv", "present"},      // folder with a single video
		{"/lib/dois-videos/A.mkv", "present"},          // folder with two videos
		{"/lib/dois-videos/B.mkv", "present"},          //
		{"/lib/serie/Serie/S01/E01.mkv", "present"},    // series root: no video directly inside
		{"/lib/serie/Serie/S01/E02.mkv", "present"},    //
		{"/lib/com-missing/Presente.mkv", "present"},   // a 'missing' row must not count
		{"/lib/com-missing/Sumiu.mkv", "missing"},      //
		{"/lib/prefixo/ab/Z.mkv", "present"},           // sibling that shares the prefix
		{"/lib/prefixo/aninhado/C.mkv", "present"},     // deeper file
		{"/lib/prefixo/aninhado/D.mkv", "present"},     //
		{"/lib/prefixo/aninhado/D2.mkv", "present"},    // two present videos in a subfolder
		{"/lib/vazio-por-dentro/sub/E.mkv", "present"}, // only descendants
		{"/lib/um-com-fanart/Filme.mkv", "present"},    // art files are not media_file rows
	}

	if _, err := d.Write.ExecContext(ctx, "INSERT INTO entity (id, type, title) VALUES ('e1', 'movie', 'Um Filme')"); err != nil {
		t.Fatalf("inserir entity: %v", err)
	}
	for i, a := range arquivos {
		id := "mf" + string(rune('a'+i))
		if _, err := d.Write.ExecContext(ctx,
			"INSERT INTO media_file (id, entity_id, path, state) VALUES (?, 'e1', ?, ?)",
			id, a.path, a.state); err != nil {
			t.Fatalf("inserir media_file %q: %v", a.path, err)
		}
	}

	// The list the OLD code built: every present media_file of the library.
	linhas, err := d.Read.QueryContext(ctx, "SELECT path, state FROM media_file WHERE state = 'present'")
	if err != nil {
		t.Fatalf("listar presentes: %v", err)
	}
	defer func() { _ = linhas.Close() }()
	var todos []localmedia.MediaFile
	for linhas.Next() {
		var p, st string
		if err := linhas.Scan(&p, &st); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		todos = append(todos, localmedia.MediaFile{Path: p, State: st})
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("iterar presentes: %v", err)
	}

	q := sqlcgen.New(d.Read)
	pastas := []string{
		"/lib/um-filme",
		"/lib/dois-videos",
		"/lib/serie/Serie",
		"/lib/serie/Serie/S01",
		"/lib/com-missing",
		"/lib/prefixo",
		"/lib/prefixo/aninhado",
		"/lib/prefixo/a", // prefix of "ab": must see nothing
		"/lib/vazio-por-dentro",
		"/lib/um-com-fanart",
		"/lib/nao-existe",
	}
	for _, pasta := range pastas {
		t.Run(pasta, func(t *testing.T) {
			n, err := q.ContarMediaFilesPresentesNaPasta(ctx, sqlcgen.ContarMediaFilesPresentesNaPastaParams{
				Prefixo:    pasta + "/",
				PrefixoFim: pasta + "0",
			})
			if err != nil {
				t.Fatalf("ContarMediaFilesPresentesNaPasta: %v", err)
			}
			querSQL := localmedia.PastaExclusivaPorContagem(int(n))
			querGo := localmedia.EhPastaExclusiva(pasta, todos)
			if querSQL != querGo {
				t.Fatalf("divergência em %q: SQL disse %v (contagem %d), EhPastaExclusiva disse %v",
					pasta, querSQL, n, querGo)
			}
		})
	}
}

// TestContarMediaFilesPresentesNaPastaParaNoSegundoAcerto locks the LIMIT 2: the answer is
// a boolean, so a folder with a thousand files must cost the same as one with two.
func TestContarMediaFilesPresentesNaPastaParaNoSegundoAcerto(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "limite.sqlite")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := d.Write.ExecContext(ctx, "INSERT INTO entity (id, type, title) VALUES ('e1', 'movie', 'Cheio')"); err != nil {
		t.Fatalf("inserir entity: %v", err)
	}
	for i := 0; i < 50; i++ {
		if _, err := d.Write.ExecContext(ctx,
			"INSERT INTO media_file (id, entity_id, path, state) VALUES (?, 'e1', ?, 'present')",
			"mf"+string(rune('a'+i)), "/lib/cheio/parte"+string(rune('a'+i))+".mkv"); err != nil {
			t.Fatalf("inserir media_file %d: %v", i, err)
		}
	}

	q := sqlcgen.New(d.Read)
	n, err := q.ContarMediaFilesPresentesNaPasta(ctx, sqlcgen.ContarMediaFilesPresentesNaPastaParams{
		Prefixo:    "/lib/cheio/",
		PrefixoFim: "/lib/cheio0",
	})
	if err != nil {
		t.Fatalf("ContarMediaFilesPresentesNaPasta: %v", err)
	}
	if n != 2 {
		t.Fatalf("contagem = %d, quero 2 (o LIMIT 2 para no segundo acerto, não conta os 50)", n)
	}
}

// TestContarMediaFilesPresentesNaPastaPastaVazia: a folder with no present file of its own
// (a series root, or a folder whose only videos are 'missing') answers zero, which is what
// makes the caller fall back to the bare artwork name.
func TestContarMediaFilesPresentesNaPastaPastaVazia(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "vazia.sqlite")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := d.Write.ExecContext(ctx, "INSERT INTO entity (id, type, title) VALUES ('e1', 'tv', 'Serie')"); err != nil {
		t.Fatalf("inserir entity: %v", err)
	}
	if _, err := d.Write.ExecContext(ctx,
		"INSERT INTO media_file (id, entity_id, path, state) VALUES ('mf1', 'e1', '/lib/serie/S01/E01.mkv', 'missing')"); err != nil {
		t.Fatalf("inserir media_file: %v", err)
	}

	q := sqlcgen.New(d.Read)
	n, err := q.ContarMediaFilesPresentesNaPasta(ctx, sqlcgen.ContarMediaFilesPresentesNaPastaParams{
		Prefixo:    "/lib/serie/",
		PrefixoFim: "/lib/serie0",
	})
	if err != nil {
		t.Fatalf("ContarMediaFilesPresentesNaPasta: %v", err)
	}
	if n != 0 {
		t.Fatalf("contagem = %d, quero 0 (missing não conta)", n)
	}
	if localmedia.PastaExclusivaPorContagem(int(n)) {
		t.Fatal("PastaExclusivaPorContagem(0) = true, quero false")
	}
}
