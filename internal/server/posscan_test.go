package server

// Testes do pós-scan por biblioteca (briefing 0135, ADR-0031): ao fim do scan
// de UMA fonte, o server reconcilía os rascunhos DAQUELA fonte (sem o throttle
// de dias do job periódico) e retenta o auto-match dos filmes pendentes dela --
// sem tocar nas outras bibliotecas.

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

// criarSourceTeste cadastra um library_source real (diretório de verdade, o
// serviço valida que existe) e devolve o id.
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

// apontarMediaFileParaFonte grava library_source_id no media_file (o helper
// semearFile não conhece fonte -- a coluna é opcional no schema).
func apontarMediaFileParaFonte(t *testing.T, d *db.DB, mediaFileID, sourceID string) {
	t.Helper()
	if _, err := d.Write.ExecContext(context.Background(),
		`UPDATE media_file SET library_source_id = ? WHERE id = ?`, sourceID, mediaFileID); err != nil {
		t.Fatalf("apontar media_file %s para fonte: %v", mediaFileID, err)
	}
}

// TestPosScanReconciliaSomenteAFonteDoScan cobre o recorte central: faixa em
// rascunho da fonte escaneada é reconciliada; a de OUTRA fonte não é tocada.
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

// TestPosScanIgnoraFilmeComIdentidadeExterna cobre o filtro do retry de filme:
// só entra no auto-match do pós-scan o filme que ainda NÃO tem identidade
// externa (tmdb) -- nada de re-enriquecer o que já foi casado.
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

// TestPosScanSemFonteNaoVarreNada garante a saída barata: sem sourceID (ou sem
// scanner) o pós-scan é no-op, nunca um erro.
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
