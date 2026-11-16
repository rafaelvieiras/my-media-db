package server

// Tests of the per-library post-scan (ADR-0031): at the end of the scan of ONE source, the
// server reconciles the drafts OF THAT source (without the day throttle of the periodic job)
// and retries the auto-match of its pending movies -- without touching the other libraries.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
)

// criarSourceTeste registers a real library_source (a real directory, the service validates
// that it exists) and returns the id.
func criarSourceTeste(t *testing.T, d *db.DB, path, contentType string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("criar diretório da fonte: %v", err)
	}
	src, err := library.New(d).Criar(context.Background(), path, "movie", contentType, nil, nil)
	if err != nil {
		t.Fatalf("criar library_source: %v", err)
	}
	return src.ID
}

// apontarMediaFileParaFonte writes library_source_id into the media_file (the semearFile helper
// does not know about sources -- the column is optional in the schema).
func apontarMediaFileParaFonte(t *testing.T, d *db.DB, mediaFileID, sourceID string) {
	t.Helper()
	if _, err := d.Write.ExecContext(context.Background(),
		`UPDATE media_file SET library_source_id = ? WHERE id = ?`, sourceID, mediaFileID); err != nil {
		t.Fatalf("apontar media_file %s para fonte: %v", mediaFileID, err)
	}
}

// TestPosScanReconciliaSomenteAFonteDoScan covers the central scoping: a draft track of the
// scanned source is reconciled; the one from ANOTHER source is not touched.
func TestPosScanReconciliaSomenteAFonteDoScan(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	pathAlvo := "/lib/musica/aha/01 - Track One.mp3"
	pathOutra := "/lib/outra/02 - Track One.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		pathAlvo:  {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
		pathOutra: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	})
	raiz := t.TempDir()
	srcA := criarSourceTeste(t, d, filepath.Join(raiz, "musica"), "music")
	srcB := criarSourceTeste(t, d, filepath.Join(raiz, "outra"), "music")

	semearFaixaDraftOrfa(t, d, "fx-a", "Track One", pathAlvo)
	semearFaixaDraftOrfa(t, d, "fx-b", "Track One", pathOutra)
	apontarMediaFileParaFonte(t, d, "mf-fx-a", srcA)
	apontarMediaFileParaFonte(t, d, "mf-fx-b", srcB)

	if err := s.ProcessarPosScan(context.Background(), srcA); err != nil {
		t.Fatalf("ProcessarPosScan: %v", err)
	}

	if got := mediaFileEntityID(t, d, pathAlvo); got == "fx-a" {
		t.Fatalf("faixa da fonte escaneada continuou no rascunho (%s): esperava reconciliação", got)
	}
	if got := mediaFileEntityID(t, d, pathOutra); got != "fx-b" {
		t.Fatalf("faixa de OUTRA fonte mudou de entity (%s), quero fx-b intocada", got)
	}
}

// TestPosScanIgnoraFilmeComIdentidadeExterna covers the movie retry filter: only the movie that
// does NOT have an external identity (tmdb) yet enters the post-scan auto-match -- no
// re-enriching of what was already matched.
func TestPosScanIgnoraFilmeComIdentidadeExterna(t *testing.T) {
	s, d := setupReconciliarOrfaosServer(t, &fakeMusicaProviderOrfaos{}, nil)
	src := criarSourceTeste(t, d, filepath.Join(t.TempDir(), "filmes"), "movie")

	semearEntity(t, d, "mv-pendente", "movie", "Enquanto Voce Dormia")
	semearFile(t, d, "mf-mv-pendente", "mv-pendente", filepath.Join("/lib/filmes", "Enquanto Voce Dormia (1995)", "filme.mkv"), "present", 0, "")
	apontarMediaFileParaFonte(t, d, "mf-mv-pendente", src)

	semearEntity(t, d, "mv-ok", "movie", "Duna")
	semearFile(t, d, "mf-mv-ok", "mv-ok", filepath.Join("/lib/filmes", "Duna (2021)", "filme.mkv"), "present", 100, "")
	apontarMediaFileParaFonte(t, d, "mf-mv-ok", src)
	if _, err := d.Write.ExecContext(context.Background(),
		`UPDATE entity SET external_ids_json = '{"tmdb":"438631"}' WHERE id = 'mv-ok'`); err != nil {
		t.Fatalf("gravar external_ids: %v", err)
	}

	rows, err := s.leitura().ListarFilmesPendentesPorSource(context.Background(), sqlcgen.ListarFilmesPendentesPorSourceParams{
		SourceID: sql.NullString{String: src, Valid: true},
		Limite:   50,
	})
	if err != nil {
		t.Fatalf("ListarFilmesPendentesPorSource: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "mv-pendente" {
		t.Fatalf("pendentes = %+v, quero só mv-pendente", rows)
	}
}

// TestPosScanSemFonteNaoVarreNada ensures the cheap exit: without a sourceID (or without a
// scanner) the post-scan is a no-op, never an error.
func TestPosScanSemFonteNaoVarreNada(t *testing.T) {
	s, _ := setupReconciliarOrfaosServer(t, &fakeMusicaProviderOrfaos{}, nil)
	if err := s.ProcessarPosScan(context.Background(), ""); err != nil {
		t.Fatalf("ProcessarPosScan(\"\"): %v", err)
	}
	vazio := &Server{}
	if err := vazio.ProcessarPosScan(context.Background(), "qualquer"); err != nil {
		t.Fatalf("ProcessarPosScan sem scanner: %v", err)
	}
}

// marcarDraft puts the entity in the 'draft' status (CriarEntity uses the schema default,
// which is cataloged).
func marcarDraft(t *testing.T, d *db.DB, entityID string) {
	t.Helper()
	if _, err := d.Write.ExecContext(context.Background(),
		`UPDATE entity SET status = 'draft' WHERE id = ?`, entityID); err != nil {
		t.Fatalf("marcar draft %q: %v", entityID, err)
	}
}

// TestPosScanPurgaRascunhoVazio covers the empty-shell purge: a draft WITHOUT a file and
// without a link (the shell the reconciliation left behind) leaves the database at the end of
// the post-scan. That is what kept the review queue from ever shrinking, even with the
// reconciliation resolving tracks -- 114 shells measured in production.
func TestPosScanPurgaRascunhoVazio(t *testing.T) {
	s, d := setupReconciliarOrfaosServer(t, &fakeMusicaProviderOrfaos{}, nil)
	src := criarSourceTeste(t, d, filepath.Join(t.TempDir(), "musica"), "music")

	// Shell: a draft without media_file, without a link, without an override.
	semearEntity(t, d, "fx-casco", "faixa", "Crawling")
	marcarDraft(t, d, "fx-casco")

	if err := s.ProcessarPosScan(context.Background(), src); err != nil {
		t.Fatalf("ProcessarPosScan: %v", err)
	}
	if n := contarLinhas(t, d, "SELECT count(*) FROM entity WHERE id = 'fx-casco'"); n != 0 {
		t.Fatalf("casco ainda na base = %d, quero 0 (purgado)", n)
	}
}

// TestPosScanNaoPurgaRascunhoComVinculo ensures the purge lock: a draft that still has a file
// or a linked child is NOT deleted (a draft series with episodes, a movie that is really still
// in the review queue).
func TestPosScanNaoPurgaRascunhoComVinculo(t *testing.T) {
	s, d := setupReconciliarOrfaosServer(t, &fakeMusicaProviderOrfaos{}, nil)
	src := criarSourceTeste(t, d, filepath.Join(t.TempDir(), "musica"), "music")

	// A draft really in the review queue: it has a file.
	semearFaixaDraftOrfa(t, d, "fx-na-fila", "Track One", "/lib/musica/aha/01 - Track One.mp3")

	// Draft series with a linked episode (child), without a file of its own.
	semearEntity(t, d, "tv-casco", "tv", "Serie Desconhecida")
	marcarDraft(t, d, "tv-casco")
	semearEntity(t, d, "ep-filho", "episode", "Episodio Um")
	if _, err := d.Write.ExecContext(context.Background(),
		`INSERT INTO entity_relation (id, parent_id, child_id, kind)
		 VALUES ('rel-casco', 'tv-casco', 'ep-filho', 'episode')`); err != nil {
		t.Fatalf("criar relação: %v", err)
	}

	if err := s.ProcessarPosScan(context.Background(), src); err != nil {
		t.Fatalf("ProcessarPosScan: %v", err)
	}
	if n := contarLinhas(t, d, "SELECT count(*) FROM entity WHERE id = 'fx-na-fila'"); n != 1 {
		t.Error("rascunho com arquivo foi purgado -- a trava do media_file falhou")
	}
	if n := contarLinhas(t, d, "SELECT count(*) FROM entity WHERE id = 'tv-casco'"); n != 1 {
		t.Error("rascunho com vínculo de filho foi purgado -- a trava de relação falhou")
	}
}

// TestPosScanPulaItemTentadoRecentemente covers the post-scan throttle: an item stamped a
// moment ago does NOT come back to the post-scan queue (without this floor, every scan retried
// the same 200 tracks and spent 45+ min of external calls).
func TestPosScanPulaItemTentadoRecentemente(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/musica/aha/01 - Track One.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		path: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	})
	src := criarSourceTeste(t, d, filepath.Join(t.TempDir(), "musica"), "music")

	semearFaixaDraftOrfa(t, d, "fx-recente", "Track One", path)
	apontarMediaFileParaFonte(t, d, "mf-fx-recente", src)
	if _, err := d.Write.ExecContext(context.Background(),
		`UPDATE entity SET last_external_match_attempt_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = 'fx-recente'`); err != nil {
		t.Fatalf("carimbar tentativa: %v", err)
	}

	if err := s.ProcessarPosScan(context.Background(), src); err != nil {
		t.Fatalf("ProcessarPosScan: %v", err)
	}
	if got := mediaFileEntityID(t, d, path); got != "fx-recente" {
		t.Fatalf("faixa tentada agora foi reconciliada de novo (%s): o piso de horas não valeu", got)
	}
}
