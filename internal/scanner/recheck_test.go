package scanner

// Tests of the forced file recheck (Manage menu > "Verificar Atualizações de
// Arquivos"): RecheckEntityFiles forces the re-hash even without a change in
// mtime/size (it deliberately ignores the incremental scan short-circuit -- see
// scan_incremental_test.go for the OPPOSITE behavior, which stays intact);
// RefreshLocalAsset discovers/updates an asset origin='local' for an entity ALREADY
// CATALOGED (a gap that today is only covered in the first walk of the folder,
// inside criarEntity, match.go).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestRecheckEntityFilesForcaReHashMesmoSemMudanca covers the central criterion of
// the forced recheck: a path whose mtime/size MATCH the media_file already stored
// (the normal scan would skip the re-hash, see scan_incremental_test.go) has
// hashDoArquivoFn called again when it goes through RecheckEntityFiles.
func TestRecheckEntityFilesForcaReHashMesmoSemMudanca(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Duna (2021) [tmdbid-438631]")
	arq := filepath.Join(pasta, "Duna (2021) [tmdbid-438631] - 1080p.mkv")
	escrever(t, arq)

	chamadas := instrumentarHash(t)

	m := managerFake(t, d)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan inicial: %v", err)
	}
	if *chamadas != 1 {
		t.Fatalf("scan inicial: hashDoArquivoFn chamado %d vez(es), quero 1", *chamadas)
	}

	// No change to the file -- a normal scan would skip the re-hash (see
	// TestScanIncrementalPulaReHashSemMudanca). RecheckEntityFiles must IGNORE that
	// short-circuit on purpose.
	ctx := context.Background()
	n, err := m.RecheckEntityFiles(ctx, src, []string{arq})
	if err != nil {
		t.Fatalf("RecheckEntityFiles: %v", err)
	}
	if n != 1 {
		t.Errorf("rechecked = %d, quero 1", n)
	}
	if *chamadas != 2 {
		t.Errorf("hashDoArquivoFn total=%d, quero 2 (RecheckEntityFiles deveria forçar o re-hash)", *chamadas)
	}

	// media_file stays unique (it updates the existing row, it does not duplicate)
	// and present.
	if cnt := contar(t, d, "SELECT count(*) FROM media_file"); cnt != 1 {
		t.Errorf("media_file após recheck = %d, quero 1 (sem duplicar)", cnt)
	}
	if cnt := contar(t, d, "SELECT count(*) FROM media_file WHERE state='present'"); cnt != 1 {
		t.Errorf("media_file present após recheck = %d, quero 1", cnt)
	}
}

// TestRecheckEntityFilesPathDesconhecidoNaoFalha covers the defensive case: a path
// without a stored media_file (e.g. it disappeared between the caller's listing and
// the call) is ignored, without an error.
func TestRecheckEntityFilesPathDesconhecidoNaoFalha(t *testing.T) {
	d := abrirDB(t)
	m := managerFake(t, d)
	root := t.TempDir()
	src := criarSource(t, d, root)

	n, err := m.RecheckEntityFiles(context.Background(), src, []string{filepath.Join(root, "fantasma.mkv")})
	if err != nil {
		t.Fatalf("RecheckEntityFiles: %v", err)
	}
	if n != 0 {
		t.Errorf("rechecked = %d, quero 0 (path sem media_file conhecido)", n)
	}
}

// TestRefreshLocalAssetDescobrePosterEmEntityJaCatalogada covers the gap
// investigated for RefreshLocalAsset: descobrirPoster only runs inside criarEntity
// (match.go), that is, only in the first walk of the folder. A poster placed AFTER
// the entity already exists is never picked up by a normal re-scan --
// RefreshLocalAsset is the new, isolated path that covers that on demand.
func TestRefreshLocalAssetDescobrePosterEmEntityJaCatalogada(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Arrival (2016) [tmdbid-329865]")
	arq := filepath.Join(pasta, "Arrival (2016) [tmdbid-329865] - 1080p.mkv")
	escrever(t, arq)

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan inicial: %v", err)
	}
	// No poster.jpg yet -- the first walk registered no asset at all.
	if cnt := contar(t, d, "SELECT count(*) FROM asset WHERE kind='poster' AND origin='local'"); cnt != 0 {
		t.Fatalf("asset poster local após scan inicial = %d, quero 0 (sem poster.jpg na pasta)", cnt)
	}

	var entityID string
	if err := d.Read.QueryRowContext(context.Background(), "SELECT entity_id FROM media_file LIMIT 1").Scan(&entityID); err != nil {
		t.Fatalf("entity_id do media_file: %v", err)
	}

	// The owner places a poster.jpg in the folder AFTER the entity already exists.
	posterPath := filepath.Join(pasta, "poster.jpg")
	if err := os.WriteFile(posterPath, []byte("fake-poster"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	found, err := m.RefreshLocalAsset(ctx, entityID, []string{arq})
	if err != nil {
		t.Fatalf("RefreshLocalAsset: %v", err)
	}
	if !found {
		t.Fatal("found = false, quero true (poster.jpg presente na pasta)")
	}
	if cnt := contar(t, d, "SELECT count(*) FROM asset WHERE kind='poster' AND origin='local' AND entity_id = ?", entityID); cnt != 1 {
		t.Errorf("asset poster local = %d, quero 1", cnt)
	}

	// Running it again with no change at all: idempotent, it neither duplicates nor
	// reports found.
	found2, err := m.RefreshLocalAsset(ctx, entityID, []string{arq})
	if err != nil {
		t.Fatalf("RefreshLocalAsset (2a chamada): %v", err)
	}
	if found2 {
		t.Error("found (2a chamada, sem mudança) = true, quero false")
	}
	if cnt := contar(t, d, "SELECT count(*) FROM asset WHERE kind='poster' AND origin='local' AND entity_id = ?", entityID); cnt != 1 {
		t.Errorf("asset poster local após 2a chamada = %d, quero 1 (sem duplicar)", cnt)
	}

	// Swap the local poster (new file, folder.jpg convention) -- the NEXT call must
	// UPDATE the path_or_uri of the SAME row, not create a second one.
	if err := os.Remove(posterPath); err != nil {
		t.Fatal(err)
	}
	folderPath := filepath.Join(pasta, "folder.jpg")
	if err := os.WriteFile(folderPath, []byte("fake-folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	found3, err := m.RefreshLocalAsset(ctx, entityID, []string{arq})
	if err != nil {
		t.Fatalf("RefreshLocalAsset (3a chamada): %v", err)
	}
	if !found3 {
		t.Error("found (3a chamada, poster trocado) = false, quero true")
	}
	if cnt := contar(t, d, "SELECT count(*) FROM asset WHERE kind='poster' AND origin='local' AND entity_id = ?", entityID); cnt != 1 {
		t.Errorf("asset poster local após troca = %d, quero 1 (atualiza a linha, não duplica)", cnt)
	}
	if cnt := contar(t, d, "SELECT count(*) FROM asset WHERE entity_id = ? AND path_or_uri = ?", entityID, folderPath); cnt != 1 {
		t.Errorf("path_or_uri não atualizado para %q", folderPath)
	}
}
