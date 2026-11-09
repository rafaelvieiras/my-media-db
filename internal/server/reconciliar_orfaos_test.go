package server

// Testes de ReconciliarFilhosOrfaos (briefing 0120): faixas/séries que
// ficaram presas em rascunho (draft) porque o match falhou na hora do scan
// são retentadas contra a árvore de identidade já resolvida (ou que veio a
// se resolver depois). Reusa Manager.ReconciliarFaixaDraft/
// ReconciliarSerieDraft (já implementados/testados em internal/scanner) por
// trás de um *scanner.Manager real com Prober fake -- mesmo padrão de
// scan_test.go/TestPostScanEmAndamento409 (s.scanner substituído
// diretamente, re-ligando música/série manualmente porque a troca de
// instância perde a config que montarServicos fez na original).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// ── fakes ────────────────────────────────────────────────────────────────

// reconciliarProberFake serve tags de áudio pré-configuradas por path, sem
// tocar ffprobe/disco de verdade -- mesmo papel de proberComTags (scanner/
// musica_test.go), reimplementado aqui por ser um tipo de teste não
// exportado do outro pacote.
type reconciliarProberFake struct {
	porPath map[string]mediainfo.AudioTags
}

func (reconciliarProberFake) Disponivel() bool { return true }

func (p reconciliarProberFake) Probe(_ context.Context, path string) ([]byte, error) {
	info := mediainfo.Info{}
	if tags, ok := p.porPath[path]; ok {
		info.Tags = &tags
	}
	return json.Marshal(info)
}

// fakeMusicaProviderOrfaos implementa o core provider.Provider +
// provider.MusicaAlbumProvider (o mínimo que matchTextualFaixa/
// enrich.ImportarFaixaPossuida exigem para o caminho TEXTUAL), sob
// Nome()="musicbrainz" -- registra um único artista/álbum/faixa fake
// ("Fake Band"/"Fake Album"/"Track One") para provar o caminho feliz da
// reconciliação de faixa. chamadasBuscar conta as buscas de artista, para os
// testes provarem que o throttle de fato pulou a chamada ao provider.
type fakeMusicaProviderOrfaos struct {
	chamadasBuscar int
}

func (p *fakeMusicaProviderOrfaos) Nome() string { return "musicbrainz" }

func (p *fakeMusicaProviderOrfaos) Buscar(_ context.Context, query, tipo, _ string) ([]provider.ResultadoExterno, error) {
	p.chamadasBuscar++
	if tipo != "artista" || !strings.EqualFold(query, "Fake Band") {
		return nil, nil
	}
	return []provider.ResultadoExterno{{Source: "musicbrainz", SourceID: "mbid-artista-1", Type: "artista", Title: "Fake Band"}}, nil
}

func (p *fakeMusicaProviderOrfaos) BuscarDetalhe(_ context.Context, sourceID, _ string, _ []string) (json.RawMessage, error) {
	if sourceID != "mbid-artista-1" {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(`{"title":"Fake Band"}`), nil
}

func (p *fakeMusicaProviderOrfaos) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *fakeMusicaProviderOrfaos) BuscarAlbumDaFaixa(_ context.Context, _ string, _ []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (p *fakeMusicaProviderOrfaos) BuscarAlbumPorNome(_ context.Context, artistaMBID, nomeAlbum string, _ provider.AlvoFaixaAlbum, _ []string) (provider.AlbumExterno, bool, error) {
	if artistaMBID != "mbid-artista-1" || !strings.EqualFold(nomeAlbum, "Fake Album") {
		return provider.AlbumExterno{}, false, nil
	}
	return provider.AlbumExterno{
		Name: "Fake Album",
		MBID: "mbid-album-1",
		Faixas: []provider.FaixaExterna{
			{Title: "Track One", MBID: "mbid-faixa-1", TrackNumber: 1},
		},
	}, true, nil
}

func (p *fakeMusicaProviderOrfaos) BuscarAlbumPorMbid(_ context.Context, _ string, _ []string) (provider.AlbumExterno, error) {
	return provider.AlbumExterno{}, provider.ErrNaoEncontrado
}

// setupReconciliarOrfaosServer sobe um Server com o musica/série fake
// registrados (liga s.enrich) e substitui s.scanner por um com Prober
// controlável -- re-ligando ConfigurarMusica/ConfigurarSeries manualmente
// (a troca de instância descarta a config que montarServicos fez na
// original, mesmo padrão de scan_test.go).
func setupReconciliarOrfaosServer(t *testing.T, musica *fakeMusicaProviderOrfaos, tags map[string]mediainfo.AudioTags) (*Server, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "reconciliar-orfaos.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	s := New("v-teste", database, WithSearchProvider(musica, fake.New("")), WithAssetsDir(t.TempDir()))
	s.scanner = scanner.NewManagerComProber(database, reconciliarProberFake{porPath: tags})
	s.scanner.ConfigurarMusica(s.enrich, musica)
	s.scanner.ConfigurarSeries(s.enrich, s.idiomasScanner)
	return s, database
}

// rebaixarParaDraft marca uma entity já criada (semearEntity) como
// status='draft' -- CriarEntity não aceita status na criação (default
// 'cataloged'), então precisa de um AtualizarEntity extra, mesmo padrão de
// criarFaixaDraftOrfa/criarSerieDraft (internal/scanner/reconciliar_test.go/
// serie_test.go).
func rebaixarParaDraft(t *testing.T, d *db.DB, id, tipo, titulo string) {
	t.Helper()
	if _, err := escrita(t, d).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		ID: id, Type: tipo, Title: titulo, Status: "draft",
	}); err != nil {
		t.Fatalf("rebaixar %s para draft: %v", id, err)
	}
}

// semearFaixaDraftOrfa cria uma entity type=faixa status=draft com um
// media_file 'present' apontando pra ela -- simula o resultado do passo 5 do
// pipeline de música (criarFaixaDraft) sem depender de um scan de verdade.
func semearFaixaDraftOrfa(t *testing.T, d *db.DB, id, titulo, path string) {
	t.Helper()
	semearEntity(t, d, id, tipoEntFaixa, titulo)
	rebaixarParaDraft(t, d, id, tipoEntFaixa, titulo)
	semearFile(t, d, "mf-"+id, id, path, "present", 0.4, "")
}

// semearSerieDraftComArquivo cria uma entity type=tv status=draft com um
// media_file 'present' apontando direto pra ela (gravarArquivoSerie sempre
// grava assim, mesmo em revisão).
func semearSerieDraftComArquivo(t *testing.T, d *db.DB, id, titulo, path string) {
	t.Helper()
	semearEntity(t, d, id, tipoEntSerie, titulo)
	rebaixarParaDraft(t, d, id, tipoEntSerie, titulo)
	semearFile(t, d, "mf-"+id, id, path, "present", 0.4, "")
}

// mediaFileEntityID lê o entity_id do media_file de um path (helper de
// asserção local -- valorUnico/contar de internal/scanner não são
// exportados/reusáveis aqui).
func mediaFileEntityID(t *testing.T, d *db.DB, path string) string {
	t.Helper()
	var id string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT entity_id FROM media_file WHERE path = ?", path).Scan(&id); err != nil {
		t.Fatalf("ler media_file %q: %v", path, err)
	}
	return id
}

// contarMediaFilePorEntity conta quantos media_file apontam pra uma entity.
func contarMediaFilePorEntity(t *testing.T, d *db.DB, entityID string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT count(*) FROM media_file WHERE entity_id = ?", entityID).Scan(&n); err != nil {
		t.Fatalf("contar media_file de %q: %v", entityID, err)
	}
	return n
}

// TestReconciliarFilhosOrfaosFaixaReligaComSucesso cobre o critério central
// do briefing 0120 para música: uma faixa draft órfã com arquivo presente,
// quando o artista/álbum resolvem via busca textual, é religada (some da
// revisão, media_file passa a apontar pra faixa resolvida) e a tentativa
// fica carimbada.
func TestReconciliarFilhosOrfaosFaixaReligaComSucesso(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/musica/aha/01 - Track One.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		path: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	})

	semearFaixaDraftOrfa(t, d, "fx-draft", "Track One", path)

	reconciliados, err := s.ReconciliarFilhosOrfaos(context.Background())
	if err != nil {
		t.Fatalf("ReconciliarFilhosOrfaos: %v", err)
	}
	if reconciliados != 1 {
		t.Fatalf("reconciliados = %d, quero 1", reconciliados)
	}

	// Briefing 0138: o draft que perdeu o arquivo é PURGADO (ele ficava para
	// sempre na fila de revisão como "sem arquivo"). O carimbo de tentativa vive
	// nele, então some junto -- o que importa é o vínculo novo.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "fx-draft"); err == nil {
		t.Error("faixa draft substituída deveria ter sido purgada")
	}

	entityID := mediaFileEntityID(t, d, path)
	if entityID == "fx-draft" {
		t.Error("media_file ainda aponta pro draft, deveria estar repontado pra faixa resolvida")
	}
	if n := contarMediaFilePorEntity(t, d, "fx-draft"); n != 0 {
		t.Errorf("media_file ainda apontando pro draft antigo = %d, quero 0", n)
	}
}

// TestReconciliarFilhosOrfaosSerieSemMatchAindaCarimba cobre o critério de
// throttle mesmo em falha: uma série draft com arquivo cujo título não casa
// com nenhuma fixture do provider (ImportarSeriePorNome não encontra
// candidato) não é religada (linked=false, sem erro) -- mas a tentativa é
// carimbada mesmo assim, para o job não martelar o mesmo item todo ciclo.
func TestReconciliarFilhosOrfaosSerieSemMatchAindaCarimba(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/series/desconhecida/Desconhecida S01E01.mkv"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{})

	semearSerieDraftComArquivo(t, d, "serie-draft", "Série Sem Fixture Nenhuma", path)

	reconciliados, err := s.ReconciliarFilhosOrfaos(context.Background())
	if err != nil {
		t.Fatalf("ReconciliarFilhosOrfaos: %v", err)
	}
	if reconciliados != 0 {
		t.Errorf("reconciliados = %d, quero 0 (sem candidato na fonte)", reconciliados)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "serie-draft")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at deveria ter sido carimbado mesmo sem religar")
	}
	entityID := mediaFileEntityID(t, d, path)
	if entityID != "serie-draft" {
		t.Errorf("media_file.entity_id mudou pra %q sem match nenhum", entityID)
	}
}

// TestReconciliarFilhosOrfaosRespeitaThrottle cobre o throttle: um item já
// verificado recentemente (last_external_match_attempt_at carimbado nesta
// mesma execução de teste, dentro da janela de retryDays) não aparece na
// lista elegível -- o provider de música nem chega a ser chamado de novo.
func TestReconciliarFilhosOrfaosRespeitaThrottle(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/musica/aha/01 - Track One.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		path: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	})

	semearFaixaDraftOrfa(t, d, "fx-draft", "Track One", path)
	if err := s.carimbarTentativaExterna(context.Background(), "fx-draft"); err != nil {
		t.Fatalf("carimbarTentativaExterna: %v", err)
	}

	reconciliados, err := s.ReconciliarFilhosOrfaos(context.Background())
	if err != nil {
		t.Fatalf("ReconciliarFilhosOrfaos: %v", err)
	}
	if reconciliados != 0 {
		t.Errorf("reconciliados = %d, quero 0 (dentro do throttle)", reconciliados)
	}
	if musica.chamadasBuscar != 0 {
		t.Errorf("provider de música chamado %d vez(es), quero 0 (dentro do throttle)", musica.chamadasBuscar)
	}
}

// TestReconciliarFilhosOrfaosSemScannerDegradaSemErro cobre a guarda
// s.scanner==nil: sem scanner, os métodos de reconciliação nem existiriam
// para chamar -- degrada para (0, nil) sem tocar o banco.
func TestReconciliarFilhosOrfaosSemScannerDegradaSemErro(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reconciliar-orfaos-sem-scanner.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database)
	s.scanner = nil

	reconciliados, err := s.ReconciliarFilhosOrfaos(context.Background())
	if err != nil {
		t.Fatalf("ReconciliarFilhosOrfaos: %v", err)
	}
	if reconciliados != 0 {
		t.Errorf("reconciliados = %d, quero 0", reconciliados)
	}
}
