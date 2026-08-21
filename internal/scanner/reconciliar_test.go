package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// ── ReconciliarFaixaDraft ─────────────────────────────────────────────────

// criarFaixaDraftOrfa cria uma entity type='faixa' status='draft' e,
// opcionalmente, um media_file 'present' apontando pra ela -- simula o
// resultado do passo 5 do pipeline de música (musica.go, criarFaixaDraft) sem
// depender do scanner pra chegar nesse estado.
func criarFaixaDraftOrfa(t *testing.T, d *db.DB, id, titulo, path string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipoFaixa, Title: titulo,
	}); err != nil {
		t.Fatalf("criar faixa draft: %v", err)
	}
	if _, err := sqlcgen.New(d.Write).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		ID: id, Type: tipoFaixa, Title: titulo, Status: "draft",
	}); err != nil {
		t.Fatalf("rebaixar faixa draft: %v", err)
	}
	if path == "" {
		return
	}
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID: "mf-" + id, EntityID: id, Path: path, State: "present",
	}); err != nil {
		t.Fatalf("criar media_file da faixa draft: %v", err)
	}
}

// TestReconciliarFaixaDraftReligaComSucesso cobre o caso feliz (briefing
// 0120 §5): a faixa draft órfã tem um arquivo real no disco; o provider
// resolve por busca textual (mesmo caminho de matchTextualFaixa/musica.go) --
// o media_file some da faixa draft e passa a apontar pra faixa recém
// resolvida, com match_confidence de busca textual.
func TestReconciliarFaixaDraftReligaComSucesso(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	}, prov)

	draftID := "faixa-draft-orfa"
	criarFaixaDraftOrfa(t, d, draftID, "Track One", arq)

	linked, err := m.ReconciliarFaixaDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarFaixaDraft: %v", err)
	}
	if !linked {
		t.Fatalf("linked = false, quero true")
	}

	entityID, conf := mediaFileDe(t, d, arq)
	if entityID == draftID {
		t.Errorf("media_file ainda aponta pro draft %q, deveria estar repontado pra faixa resolvida", draftID)
	}
	if conf != confMusicaTextual {
		t.Errorf("match_confidence = %v, quero %v (busca textual)", conf, confMusicaTextual)
	}
	tipo, titulo, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || titulo != "Track One" || status != "cataloged" {
		t.Errorf("faixa resolvida = {%q %q %q}, quero {faixa Track One cataloged}", tipo, titulo, status)
	}
	// A faixa draft antiga é PRESERVADA (princípio 5, não-destrutivo), só
	// sem media_file (posse cai a zero).
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE id=?", draftID); n != 1 {
		t.Errorf("faixa draft antiga = %d, quero 1 (preservada, não apagada)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE entity_id=?", draftID); n != 0 {
		t.Errorf("media_file ainda apontando pro draft antigo = %d, quero 0", n)
	}
}

// TestReconciliarFaixaDraftSemMediaFileNaoLiga cobre a degradação: uma faixa
// draft sem NENHUM media_file (órfã na acepção mais rasa: a entity existe,
// mas nada no disco aponta pra ela) não pode ser reconciliada -- linked=false
// sem erro.
func TestReconciliarFaixaDraftSemMediaFileNaoLiga(t *testing.T) {
	d := abrirDB(t)
	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{}, prov)

	draftID := "faixa-draft-sem-arquivo"
	criarFaixaDraftOrfa(t, d, draftID, "Faixa Sem Arquivo", "")

	linked, err := m.ReconciliarFaixaDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarFaixaDraft: %v", err)
	}
	if linked {
		t.Errorf("linked = true, quero false (sem media_file nenhum)")
	}
}

// ── ReconciliarSerieDraft ─────────────────────────────────────────────────

// managerSerieAutoImportReconciliar monta um Manager só com ConfigurarSeries
// (sem depender de scanRun) -- mesmo provider fake de managerSerieAutoImport,
// mas sem exigir um scan de verdade para testar ReconciliarSerieDraft
// diretamente.
func managerSerieAutoImportReconciliar(t *testing.T, d *db.DB) *Manager {
	t.Helper()
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("PNG"))
	}))
	t.Cleanup(img.Close)
	svc := enrich.New(d, provider.NovaRegistry(fake.New(img.URL)), t.TempDir())
	m := managerFake(t, d)
	m.ConfigurarSeries(svc, func(context.Context) []string { return []string{"en"} })
	return m
}

// TestReconciliarSerieDraftReligaEpisodios cobre o caso feliz (briefing 0120
// §5): uma série draft com 1+ media_file é reimportada com sucesso
// (tentarImportarSerie/ImportarSeriePorNome contra o provider fake) e cada
// media_file é repontado + ganha o vínculo de episódio certo.
func TestReconciliarSerieDraftReligaEpisodios(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "Springfield Tales S01E01.mkv")
	escrever(t, arq)

	m := managerSerieAutoImportReconciliar(t, d)

	draftID := "serie-draft-orfa"
	criarSerieDraft(t, d, draftID, "Springfield Tales")
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID: "mf-serie-draft", EntityID: draftID, Path: arq, State: "present",
	}); err != nil {
		t.Fatalf("criar media_file da série draft: %v", err)
	}

	linked, err := m.ReconciliarSerieDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarSerieDraft: %v", err)
	}
	if !linked {
		t.Fatalf("linked = false, quero true")
	}

	if n := contarVinculos(t, d); n != 1 {
		t.Errorf("vínculos tv_episode = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=?", epIDPorTmdb(t, d, "101")); n != 1 {
		t.Errorf("vínculo para o ep 101 ausente")
	}
	entityID, conf := mediaFileDe(t, d, arq)
	if conf != confSerieEp {
		t.Errorf("match_confidence = %v, quero %v", conf, confSerieEp)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE id=? AND external_id_tmdb IS NOT NULL", entityID); n != 1 {
		t.Errorf("entity resolvida %q sem external_id_tmdb -- import real não persistiu identidade externa", entityID)
	}
}

// TestReconciliarSerieDraftSemImporterNaoLiga cobre a degradação explícita
// do briefing 0120: sem SerieImporter configurado (nil, ConfigurarSeries
// nunca chamado), ReconciliarSerieDraft nunca tenta nada -- linked=false sem
// erro (tentarImportarSerie já degrada assim, ver serie.go).
func TestReconciliarSerieDraftSemImporterNaoLiga(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "Some Show S01E01.mkv")
	escrever(t, arq)

	m := managerFake(t, d) // sem ConfigurarSeries -- serieImporter fica nil

	draftID := "serie-draft-sem-importer"
	criarSerieDraft(t, d, draftID, "Some Show")
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID: "mf-sem-importer", EntityID: draftID, Path: arq, State: "present",
	}); err != nil {
		t.Fatalf("criar media_file: %v", err)
	}

	linked, err := m.ReconciliarSerieDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarSerieDraft: %v", err)
	}
	if linked {
		t.Errorf("linked = true, quero false (sem SerieImporter configurado)")
	}
	// Nada foi repontado -- o media_file continua na série draft original.
	entityID := valorUnico(t, d, "SELECT entity_id FROM media_file WHERE path = ?", arq)
	if entityID != draftID {
		t.Errorf("media_file.entity_id mudou pra %q sem importer configurado", entityID)
	}
}
