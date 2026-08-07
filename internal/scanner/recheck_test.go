package scanner

// Testes do briefing 0088 (menu Gerenciar > "Verificar Atualizações de
// Arquivos"): RecheckEntityFiles força o re-hash mesmo sem mudança de
// mtime/size (ignora de propósito o short-circuit do scan incremental,
// briefing 0064 -- ver scan_incremental_test.go para o comportamento
// OPOSTO, que continua intacto); RefreshLocalAsset descobre/atualiza um
// asset origin='local' para uma entity JÁ CATALOGADA (gap que hoje só é
// coberto na 1ª varredura da pasta, dentro de criarEntity, match.go).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestRecheckEntityFilesForcaReHashMesmoSemMudanca cobre o critério central
// do briefing 0088: um path cujo mtime/size BATEM com o media_file já
// gravado (o scan normal pularia o re-hash, ver scan_incremental_test.go)
// tem hashDoArquivoFn chamado de novo quando passa por RecheckEntityFiles.
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

	// Nenhuma mudança no arquivo -- um scan normal pularia o re-hash (ver
	// TestScanIncrementalPulaReHashSemMudanca). RecheckEntityFiles deve
	// IGNORAR esse short-circuit de propósito.
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

	// media_file segue único (atualiza a linha existente, não duplica) e
	// present.
	if cnt := contar(t, d, "SELECT count(*) FROM media_file"); cnt != 1 {
		t.Errorf("media_file após recheck = %d, quero 1 (sem duplicar)", cnt)
	}
	if cnt := contar(t, d, "SELECT count(*) FROM media_file WHERE state='present'"); cnt != 1 {
		t.Errorf("media_file present após recheck = %d, quero 1", cnt)
	}
}

// TestRecheckEntityFilesPathDesconhecidoNaoFalha cobre o caso defensivo: um
// path sem media_file gravado (ex.: sumiu entre a listagem do chamador e a
// chamada) é ignorado, sem erro.
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

// TestRefreshLocalAssetDescobrePosterEmEntityJaCatalogada cobre o gap
// investigado no briefing 0088: descobrirPoster só roda dentro de
// criarEntity (match.go), ou seja, só na 1ª varredura da pasta. Um poster
// colocado DEPOIS que a entity já existe nunca é pego por um re-scan normal
// -- RefreshLocalAsset é o caminho novo e isolado que cobre isso sob
// demanda.
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
	// Sem poster.jpg ainda -- a 1ª varredura não registrou asset nenhum.
	if cnt := contar(t, d, "SELECT count(*) FROM asset WHERE kind='poster' AND origin='local'"); cnt != 0 {
		t.Fatalf("asset poster local após scan inicial = %d, quero 0 (sem poster.jpg na pasta)", cnt)
	}

	var entityID string
	if err := d.Read.QueryRowContext(context.Background(), "SELECT entity_id FROM media_file LIMIT 1").Scan(&entityID); err != nil {
		t.Fatalf("entity_id do media_file: %v", err)
	}

	// O dono coloca um poster.jpg na pasta DEPOIS que a entity já existe.
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

	// Rodar de novo sem mudança nenhuma: idempotente, não duplica nem
	// reporta found.
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

	// Trocar o poster local (novo arquivo, convenção folder.jpg) -- a
	// PRÓXIMA chamada deve ATUALIZAR o path_or_uri da MESMA linha, não
	// criar uma segunda.
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
