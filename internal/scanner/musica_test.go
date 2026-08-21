package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// ── fixtures/fakes de música ──────────────────────────────────────────────────
//
// Mesmos MBIDs sintéticos de internal/enrich/musica_test.go (UUIDs fictícios, não
// reais): 1 artista, 1 álbum com 2 faixas -- a 2ª com artist-credit DIVERGENTE
// (feat.) do artista do álbum, cobrindo o mesmo caso do ADR-0027 item 4 (já
// validado em 0106; aqui só provamos que o SCANNER dispara o import certo).

const (
	mbidArtistaFixture = "aaaaaaaa-0000-0000-0000-000000000001"
	mbidAlbumFixture   = "bbbbbbbb-0000-0000-0000-000000000001"
	mbidFaixa1Fixture  = "cccccccc-0000-0000-0000-000000000001"
	mbidFaixa2Fixture  = "cccccccc-0000-0000-0000-000000000002"
	mbidFeatFixture    = "dddddddd-0000-0000-0000-000000000001"
)

// fakeMusicaProvider implementa o core provider.Provider + provider.MusicaProvider
// (árvore, capacidade legada -- só usada pelo import MANUAL/dispatcher, não pelo
// scanner desde o briefing 0115) + provider.MusicaAlbumProvider (capacidade
// SELETIVA, briefing 0115 -- a que o scanner efetivamente usa) +
// provider.FaixaResolverProvider, sem rede real. Conta as chamadas (buscarN/
// arvoreN/resolverN/albumFaixaN/albumNomeN) para os testes provarem tanto a
// seletividade (nenhuma árvore inteira) quanto que um re-scan sem mudança NÃO
// rechama a fonte (briefing 0107 §5, incrementalidade).
type fakeMusicaProvider struct {
	buscarN, arvoreN, resolverN, albumFaixaN, albumNomeN int
}

func (p *fakeMusicaProvider) Nome() string { return sourceMusicBrainz }

// albumFixture é o ÚNICO álbum da discografia fake: "Fake Album", 2 faixas -- a 2ª
// (Track Two) com artist-credit DIVERGENTE (feat.) do artista principal, cobrindo o
// mesmo caso do ADR-0027 item 4 / ADR-0029 item 4. Reaproveitado por
// BuscarArtistaArvore (árvore, legado) e pelas capacidades seletivas novas
// (BuscarAlbumDaFaixa/BuscarAlbumPorNome) para não divergir os dois caminhos.
func albumFixture() provider.AlbumExterno {
	return provider.AlbumExterno{
		Name: "Fake Album",
		MBID: mbidAlbumFixture,
		Year: "1999",
		Faixas: []provider.FaixaExterna{
			{
				Title:              "Track One",
				MBID:               mbidFaixa1Fixture,
				TrackNumber:        1,
				DiscNumber:         1,
				DiscTotal:          1,
				ArtistaCreditoNome: "Fake Band",
				ArtistaCreditoMBID: mbidArtistaFixture,
			},
			{
				Title:              "Track Two",
				MBID:               mbidFaixa2Fixture,
				TrackNumber:        2,
				DiscNumber:         1,
				DiscTotal:          1,
				ArtistaCreditoNome: "Guest Artist",
				ArtistaCreditoMBID: mbidFeatFixture,
			},
		},
	}
}

// Buscar responde por NOME exato (normalizado) de artista: "Fake Band" -> 1
// candidato único (mbidArtistaFixture); "Artista Ambiguo" -> 2 candidatos
// empatados no topo (ambiguidade proposital); qualquer outra coisa -> vazio.
func (p *fakeMusicaProvider) Buscar(_ context.Context, query, tipo, _ string) ([]provider.ResultadoExterno, error) {
	p.buscarN++
	if tipo != "" && tipo != "artista" {
		return nil, nil
	}
	switch query {
	case "Fake Band":
		return []provider.ResultadoExterno{{Source: sourceMusicBrainz, SourceID: mbidArtistaFixture, Type: "artista", Title: "Fake Band"}}, nil
	case "Artista Ambiguo":
		return []provider.ResultadoExterno{
			{Source: sourceMusicBrainz, SourceID: "banda-x", Type: "artista", Title: "Artista Ambiguo"},
			{Source: sourceMusicBrainz, SourceID: "banda-y", Type: "artista", Title: "Artista Ambiguo"},
		}, nil
	default:
		return nil, nil
	}
}

func (p *fakeMusicaProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (p *fakeMusicaProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (p *fakeMusicaProvider) BuscarArtistaArvore(_ context.Context, sourceID string, _ []string) (provider.ArtistaArvore, error) {
	p.arvoreN++
	if sourceID != mbidArtistaFixture {
		return provider.ArtistaArvore{}, provider.ErrNaoEncontrado
	}
	return provider.ArtistaArvore{
		Name:   "Fake Band",
		MBID:   mbidArtistaFixture,
		Albuns: []provider.AlbumExterno{albumFixture()},
	}, nil
}

// ResolverArtistaDaFaixa só resolve mbidFaixa1Fixture -> mbidArtistaFixture
// (o caso "só o MBID de faixa está embutido, sem o de artista").
func (p *fakeMusicaProvider) ResolverArtistaDaFaixa(_ context.Context, recordingMBID string) (string, error) {
	p.resolverN++
	if recordingMBID != mbidFaixa1Fixture {
		return "", provider.ErrNaoEncontrado
	}
	return mbidArtistaFixture, nil
}

// BuscarAlbumDaFaixa é a capacidade SELETIVA (provider.MusicaAlbumProvider,
// briefing 0115) que o scanner efetivamente usa no caminho por MBID: monta SÓ o
// álbum que contém `recordingMBID`, nunca a árvore inteira.
func (p *fakeMusicaProvider) BuscarAlbumDaFaixa(_ context.Context, recordingMBID string, _ []string) (provider.AlbumExterno, bool, error) {
	p.albumFaixaN++
	alb := albumFixture()
	for _, fx := range alb.Faixas {
		if fx.MBID == recordingMBID {
			return alb, true, nil
		}
	}
	return provider.AlbumExterno{}, false, nil
}

// BuscarAlbumPorNome é a capacidade SELETIVA (provider.MusicaAlbumProvider,
// briefing 0115) que o scanner usa no caminho textual: monta SÓ o álbum
// `nomeAlbum` do artista `artistaMBID`, nunca a árvore inteira.
func (p *fakeMusicaProvider) BuscarAlbumPorNome(_ context.Context, artistaMBID, nomeAlbum string, _ provider.AlvoFaixaAlbum, _ []string) (provider.AlbumExterno, bool, error) {
	p.albumNomeN++
	alb := albumFixture()
	if artistaMBID != mbidArtistaFixture || nomeAlbum != alb.Name {
		return provider.AlbumExterno{}, false, nil
	}
	return alb, true, nil
}

// BuscarAlbumPorMbid é a capacidade SELETIVA (provider.MusicaAlbumProvider,
// briefing 0115 Parte 2) que o scanner NÃO chama (é a tela externa de álbum/
// import de faixa avulsa que usa) -- só precisa satisfazer a interface para o
// type-assert de MusicaAlbumProvider nos testes deste pacote continuar
// bem-sucedido.
func (p *fakeMusicaProvider) BuscarAlbumPorMbid(_ context.Context, albumMBID string, _ []string) (provider.AlbumExterno, error) {
	alb := albumFixture()
	if albumMBID != alb.MBID {
		return provider.AlbumExterno{}, provider.ErrNaoEncontrado
	}
	return alb, nil
}

// proberComTags é um Prober fake (0107): devolve um mediainfo_json cujo campo
// Tags vem do mapa `porPath` (chaveado pelo path do arquivo) -- permite
// exercitar o pipeline de matching sem depender de ffmpeg/ffprobe reais (mesmo
// espírito de proberVazio, mas com tags controláveis).
type proberComTags struct {
	porPath map[string]mediainfo.AudioTags
}

func (proberComTags) Disponivel() bool { return true }

func (p proberComTags) Probe(_ context.Context, path string) ([]byte, error) {
	info := mediainfo.Info{}
	if tags, ok := p.porPath[path]; ok {
		info.Tags = &tags
	}
	return json.Marshal(info)
}

// criarSourceMusica cadastra um datasource content_type='music' (0107; o campo
// já é aceito diretamente por library.Criar, ao contrário do content_type de
// série quando aquele teste foi escrito).
func criarSourceMusica(t *testing.T, d *db.DB, dir string) library.Source {
	t.Helper()
	src, err := library.New(d).Criar(context.Background(), dir, "movie", "music", nil, nil)
	if err != nil {
		t.Fatalf("criar source musica: %v", err)
	}
	return src
}

// managerMusica monta um Manager com o prober de tags dado e a vertical de
// música configurada de ponta a ponta: enrich.Servico real (find-or-create
// idempotente por MBID, 0106) sobre o fakeMusicaProvider (busca textual +
// árvore + resolução reversa).
func managerMusica(t *testing.T, d *db.DB, porPath map[string]mediainfo.AudioTags, prov *fakeMusicaProvider) *Manager {
	t.Helper()
	m := NewManagerComProber(d, proberComTags{porPath: porPath})
	m.agora = relogioCrescente()
	svc := enrich.New(d, provider.NovaRegistry(prov), t.TempDir())
	m.ConfigurarMusica(svc, prov)
	m.idiomas = func(context.Context) []string { return []string{"en"} }
	return m
}

// entityDe le type/title/status de uma entity pelo id (helper de asserção).
func entityDe(t *testing.T, d *db.DB, id string) (tipo, titulo, status string) {
	t.Helper()
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT type, title, status FROM entity WHERE id = ?", id).Scan(&tipo, &titulo, &status); err != nil {
		t.Fatalf("ler entity %s: %v", id, err)
	}
	return tipo, titulo, status
}

// mediaFileDe le entity_id/match_confidence do media_file de um path.
func mediaFileDe(t *testing.T, d *db.DB, path string) (entityID string, conf float64) {
	t.Helper()
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT entity_id, match_confidence FROM media_file WHERE path = ?", path).Scan(&entityID, &conf); err != nil {
		t.Fatalf("ler media_file %s: %v", path, err)
	}
	return entityID, conf
}

// ── testes ───────────────────────────────────────────────────────────────────

// TestScanMusicaMbidFaixaFastPath cobre o passo 1 do pipeline (0107 §4): a
// faixa JÁ EXISTE no catálogo por MBID (ex.: outra faixa do mesmo álbum já
// importou a árvore, ou o arquivo já foi catalogado antes) -- lookup PURO, sem
// nenhuma chamada à fonte. Vínculo 1:1: media_file.entity_id aponta DIRETO
// pra ela, confiança 100.
func TestScanMusicaMbidFaixaFastPath(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "faixa.mp3")
	escrever(t, arq)

	// Pré-semeia a faixa por MBID (find-or-create já resolvido antes deste scan).
	faixaID := "faixa-preexistente"
	extIDs := fmt.Sprintf(`{"mbid":%q}`, mbidFaixa1Fixture)
	if _, err := sqlcgen.New(d.Write).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: faixaID, Type: tipoFaixa, Title: "Track One", ExternalIdsJson: nsOpt(extIDs),
	}); err != nil {
		t.Fatalf("pre-semear faixa: %v", err)
	}

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {MusicBrainzTrackID: mbidFaixa1Fixture},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	if entityID != faixaID {
		t.Errorf("media_file.entity_id = %q, quero %q (a faixa pré-existente)", entityID, faixaID)
	}
	if conf != confIDExterno {
		t.Errorf("match_confidence = %v, quero %v", conf, confIDExterno)
	}
	if prov.buscarN != 0 || prov.arvoreN != 0 || prov.resolverN != 0 {
		t.Errorf("provider chamado no fast-path por MBID: buscar=%d arvore=%d resolver=%d, quero 0/0/0", prov.buscarN, prov.arvoreN, prov.resolverN)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entities = %d, quero 1 (só a pré-semeada, nenhuma nova)", n)
	}
}

// TestScanMusicaMbidFaixaEArtistaImportaEscopado cobre o passo 2 (ADR-0029,
// briefing 0115): MBID de faixa SEM match ainda, mas a tag TAMBÉM tem o MBID
// do artista -- dispara o import ESCOPADO (artista + ALBUM DAQUELA FAIXA +
// a própria faixa), NUNCA a árvore inteira. A faixa 2 do mesmo álbum (Track
// Two, feat.) NÃO é tocada só por estar no mesmo release: nem ela nem o
// artista feat (Guest Artist) viram entity.
func TestScanMusicaMbidFaixaEArtistaImportaEscopado(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "track-one.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {MusicBrainzTrackID: mbidFaixa1Fixture, MusicBrainzArtistID: mbidArtistaFixture},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || titulo != "Track One" {
		t.Errorf("faixa = {type:%q title:%q}, quero {faixa, Track One}", tipo, titulo)
	}
	if status != "cataloged" {
		t.Errorf("status = %q, quero cataloged (import via MBID, não draft)", status)
	}
	if conf != confIDExterno {
		t.Errorf("match_confidence = %v, quero %v", conf, confIDExterno)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (import escopado, nunca a arvore inteira)", prov.arvoreN)
	}
	if prov.albumFaixaN != 1 {
		t.Errorf("BuscarAlbumDaFaixa chamado %d vez(es), quero 1", prov.albumFaixaN)
	}
	// ADR-0029 item 1/3: só o artista + O ÁLBUM DAQUELA FAIXA + a própria faixa
	// entram no catálogo -- nenhum outro release, nenhuma outra faixa do MESMO
	// álbum (Track Two) e nenhum artista feat são auto-criados.
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='artista'"); n != 1 {
		t.Errorf("artistas = %d, quero 1 (só o principal; feat NÃO auto-cria entity)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='album'"); n != 1 {
		t.Errorf("albuns = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='faixa'"); n != 1 {
		t.Errorf("faixas = %d, quero 1 (só a faixa casada, não a arvore/album inteiro)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity_relation WHERE kind = ?", musicext.KindArtistaFeat); n != 0 {
		t.Errorf("relacoes music_artista_feat = %d, quero 0 (import escopado nunca auto-cria feat)", n)
	}
}

// TestScanMusicaResolucaoReversaFaixaSemArtista cobre o passo 3: só o MBID de
// faixa está embutido (sem o de artista) -- a resolução reversa
// (FaixaResolverProvider) acha o artista, e o resto segue igual ao passo 2.
func TestScanMusicaResolucaoReversaFaixaSemArtista(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "track-one.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {MusicBrainzTrackID: mbidFaixa1Fixture}, // SEM MusicBrainzArtistID
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, _ := entityDe(t, d, entityID)
	if tipo != tipoFaixa || titulo != "Track One" {
		t.Errorf("faixa = {type:%q title:%q}, quero {faixa, Track One}", tipo, titulo)
	}
	if conf != confIDExterno {
		t.Errorf("match_confidence = %v, quero %v", conf, confIDExterno)
	}
	if prov.resolverN != 1 {
		t.Errorf("ResolverArtistaDaFaixa chamado %d vez(es), quero 1", prov.resolverN)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (import escopado, nunca a arvore inteira)", prov.arvoreN)
	}
	if prov.albumFaixaN != 1 {
		t.Errorf("BuscarAlbumDaFaixa chamado %d vez(es), quero 1", prov.albumFaixaN)
	}
}

// TestScanMusicaBuscaTextual cobre o passo 4 (caso DOMINANTE nos arquivos
// reais do dono): sem NENHUM MBID embutido, casamento por nome do artista
// (album_artist) + import ESCOPADO pelo caminho TEXTUAL (álbum por nome via
// BuscarAlbumPorNome, faixa por título dentro dele) -- ADR-0029/briefing 0115,
// nunca a árvore inteira.
func TestScanMusicaBuscaTextual(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, _ := entityDe(t, d, entityID)
	if tipo != tipoFaixa || titulo != "Track One" {
		t.Errorf("faixa = {type:%q title:%q}, quero {faixa, Track One}", tipo, titulo)
	}
	if conf != confMusicaTextual {
		t.Errorf("match_confidence = %v, quero %v (busca textual)", conf, confMusicaTextual)
	}
	if prov.buscarN != 1 {
		t.Errorf("Buscar chamado %d vez(es), quero 1", prov.buscarN)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (import escopado, nunca a arvore inteira)", prov.arvoreN)
	}
	if prov.albumNomeN != 1 {
		t.Errorf("BuscarAlbumPorNome chamado %d vez(es), quero 1", prov.albumNomeN)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='faixa'"); n != 1 {
		t.Errorf("faixas = %d, quero 1 (só a faixa casada, não o album inteiro)", n)
	}
}

// TestScanMusicaBuscaTextualAmbiguaViraDraft cobre a ambiguidade (>1
// candidato empatado no topo): NUNCA aplica um match "parecido" -- cai para o
// draft (passo 5).
func TestScanMusicaBuscaTextualAmbiguaViraDraft(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "faixa-ambigua.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Artista Ambiguo", Album: "Algum Album", Title: "Alguma Faixa"},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || status != "draft" {
		t.Errorf("faixa = {type:%q status:%q}, quero {faixa, draft}", tipo, status)
	}
	if titulo != "Alguma Faixa" {
		t.Errorf("titulo do draft = %q, quero %q (da tag title)", titulo, "Alguma Faixa")
	}
	if conf != confDraft {
		t.Errorf("match_confidence = %v, quero %v", conf, confDraft)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (ambiguo nunca importa)", prov.arvoreN)
	}
}

// TestScanMusicaSemTagsViraDraftComNomeDoArquivo cobre o passo 5 raso: sem
// NENHUMA tag legível (ffprobe indisponível/tag vazia), a faixa vira draft
// com o NOME DO ARQUIVO como título (mesma filosofia de filme/série -- visível,
// não perdido).
func TestScanMusicaSemTagsViraDraftComNomeDoArquivo(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "sem-tags-nenhuma.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{}, prov) // sem entrada = sem tags
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || status != "draft" {
		t.Errorf("faixa = {type:%q status:%q}, quero {faixa, draft}", tipo, status)
	}
	if titulo != "sem-tags-nenhuma" {
		t.Errorf("titulo do draft = %q, quero %q (nome do arquivo)", titulo, "sem-tags-nenhuma")
	}
	if conf != confDraft {
		t.Errorf("match_confidence = %v, quero %v", conf, confDraft)
	}
}

// TestScanMusicaReescaneioNaoRechamaImportarNemDuplica cobre a
// incrementalidade (briefing 0107 §5): reescanear o MESMO arquivo (mtime/size
// inalterados) reusa o entity_id já gravado sem rechamar Importar/Buscar/
// ResolverArtistaDaFaixa, e sem duplicar entity nem media_file.
func TestScanMusicaReescaneioNaoRechamaImportarNemDuplica(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "track-one.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {MusicBrainzTrackID: mbidFaixa1Fixture, MusicBrainzArtistID: mbidArtistaFixture},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	entityID1, _ := mediaFileDe(t, d, arq)
	albumFaixaNApos1 := prov.albumFaixaN

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2 (re-scan sem mudanca): %v", err)
	}
	entityID2, conf2 := mediaFileDe(t, d, arq)

	if entityID2 != entityID1 {
		t.Errorf("entity_id mudou no re-scan: %q -> %q", entityID1, entityID2)
	}
	if conf2 != confIDExterno {
		t.Errorf("match_confidence apos re-scan = %v, quero %v", conf2, confIDExterno)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (import escopado, nunca a arvore inteira)", prov.arvoreN)
	}
	if prov.albumFaixaN != albumFaixaNApos1 {
		t.Errorf("BuscarAlbumDaFaixa chamado de novo no re-scan sem mudanca: antes=%d depois=%d", albumFaixaNApos1, prov.albumFaixaN)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='faixa'"); n != 1 {
		t.Errorf("faixas apos re-scan = %d, quero 1 (nao duplicou, import escopado so traz 1 faixa)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file apos re-scan = %d, quero 1 (nao duplicou)", n)
	}
	// Vinculo 1:1: nunca a juncao N:M de serie.
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity"); n != 0 {
		t.Errorf("media_file_entity = %d, quero 0 (musica e 1:1, media_file.entity_id direto)", n)
	}
}
