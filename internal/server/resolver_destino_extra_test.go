package server

// The extras rule of the DESTINATION (issue #96): the trailer of a movie is a
// media_file of the movie, and it used to win BOTH decisions of resolverDestino —
// it is often the OLDER file (created_at), so it named the exported .nfo and the
// artwork (`<trailer>.nfo`, `<trailer>-poster.jpg`), and it made the folder look
// "not exclusive", so the artwork was never born bare.
//
// What the media servers read: Kodi/Emby/Jellyfin/tinyfin look for `<video>.nfo`,
// `movie.nfo`, `poster.jpg`, `folder.jpg` — never `<something>-trailer-poster.jpg`.
// Measured on the owner's library: 1.154 movies had exactly that shape and showed
// no poster at all.

import (
	"context"
	"testing"
)

// TestResolverDestinoIgnoraTrailerAoNomear is the regression: the OLDER file of the
// folder is the trailer, and it must still not be the one that names the export.
func TestResolverDestinoIgnoraTrailerAoNomear(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	const id = "e-rush2"
	semearEntity(t, d, id, "movie", "A Hora do Rush 2")
	// The trailer is inserted FIRST, so it is the older row (created_at asc) — the
	// exact shape found in production (2 ms of difference was enough).
	semearFile(t, d, "f-trailer", id, "/lib/A Hora do Rush 2 (2001)/A.Hora.do.Rush.2.2001.BluRay.720p-trailer.mp4", "present", 0, "")
	semearFile(t, d, "f-filme", id, "/lib/A Hora do Rush 2 (2001)/Rush Hour 2 (2001) WEBDL-1080p.mkv", "present", 0, "")

	ent, err := srv.leitura().ObterEntity(context.Background(), id)
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	dir, nomeBase, exclusiva, ok, err := srv.resolverDestino(context.Background(), ent)
	if err != nil {
		t.Fatalf("resolverDestino: %v", err)
	}
	if !ok {
		t.Fatal("resolverDestino devolveu ok=false, queria destino")
	}
	if dir != "/lib/A Hora do Rush 2 (2001)" {
		t.Fatalf("dir = %q", dir)
	}
	if nomeBase != "Rush Hour 2 (2001) WEBDL-1080p" {
		t.Fatalf("nomeBaseVideo = %q, queria o nome do FILME (o trailer nao pode nomear a exportacao)", nomeBase)
	}
	if !exclusiva {
		t.Fatal("pastaExclusiva = false: filme + trailer ainda e a pasta do filme (arte nasce poster.jpg/fanart.jpg)")
	}
}

// TestResolverDestinoUsaTrailerQuandoEhSoOQueExiste keeps the fallback honest: an
// entity whose only present file is extra material still gets a destination, so
// nothing that used to be written disappears.
func TestResolverDestinoUsaTrailerQuandoEhSoOQueExiste(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	const id = "e-so-trailer"
	semearEntity(t, d, id, "movie", "Só Trailer")
	semearFile(t, d, "f-so-trailer", id, "/lib/So Trailer (2020)/So Trailer (2020)-trailer.mp4", "present", 0, "")

	ent, err := srv.leitura().ObterEntity(context.Background(), id)
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	_, nomeBase, _, ok, err := srv.resolverDestino(context.Background(), ent)
	if err != nil {
		t.Fatalf("resolverDestino: %v", err)
	}
	if !ok {
		t.Fatal("ok=false: o fallback para o extra nao pode perder o destino")
	}
	if nomeBase != "So Trailer (2020)-trailer" {
		t.Fatalf("nomeBaseVideo = %q, queria o unico arquivo existente", nomeBase)
	}
}

// TestPastaExclusivaNoDiscoIgnoraExtra locks the SQL-backed form of the rule (it
// reads the names of the folder through the indexed range scan).
func TestPastaExclusivaNoDiscoIgnoraExtra(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	const id = "e-pasta"
	semearEntity(t, d, id, "movie", "Filme Com Trailer")
	semearFile(t, d, "f-a", id, "/lib/Filme (2001)/Filme (2001).mkv", "present", 0, "")
	semearFile(t, d, "f-b", id, "/lib/Filme (2001)/Filme (2001)-trailer.mp4", "present", 0, "")
	semearFile(t, d, "f-c", id, "/lib/Filme (2001)/Filme (2001)-sample.mkv", "present", 0, "")

	exclusiva, err := srv.pastaExclusivaNoDisco(context.Background(), "/lib/Filme (2001)")
	if err != nil {
		t.Fatalf("pastaExclusivaNoDisco: %v", err)
	}
	if !exclusiva {
		t.Fatal("exclusiva = false, queria true: trailer e sample nao contam como segundo video do item")
	}

	// Two REAL videos in the same folder stay ambiguous (two editions) — the rule
	// must not be loosened beyond extra material.
	semearFile(t, d, "f-d", id, "/lib/Filme (2001)/Filme (2001) - 4K.mkv", "present", 0, "")
	exclusiva, err = srv.pastaExclusivaNoDisco(context.Background(), "/lib/Filme (2001)")
	if err != nil {
		t.Fatalf("pastaExclusivaNoDisco (2): %v", err)
	}
	if exclusiva {
		t.Fatal("exclusiva = true com dois videos reais: a pasta deixa de ser exclusiva")
	}
}
