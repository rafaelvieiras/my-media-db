package scanner

// Testes do briefing 0136 (matching de música): desempate de artista por
// EVIDÊNCIA (o álbum da tag existe em qual candidato?) e o caminho de
// COMPILAÇÃO/TRILHA (álbum resolvido pelo título, artista vindo do artist-credit
// da fonte). Fakes próprios deste arquivo para não mexer nas contagens do
// fakeMusicaProvider usado pelos testes de 0107/0115/0121.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	mbidArtistaTie    = "eeeeeeee-0000-0000-0000-000000000001"
	mbidArtistaTie2   = "eeeeeeee-0000-0000-0000-000000000002"
	mbidArtistaVarios = "89ad4ac3-39f7-470e-963a-56509c546377"
	mbidAlbumTie      = "ffffffff-0000-0000-0000-000000000001"
	mbidAlbumCompil   = "ffffffff-0000-0000-0000-000000000002"
)

// fakeProviderEmpateCompilacao cobre os dois casos do briefing 0136:
//   - "Tie Artist" devolve DOIS candidatos com o mesmo nome (ids diferentes) e o
//     álbum "Tie Album" só existe no segundo -- o pipeline tem que resolver por
//     evidência, não desistir no empate;
//   - "Various Artists" NÃO devolve artista; o álbum de trilha só resolve pela
//     capacidade nova MusicaAlbumPorTituloProvider.
type fakeProviderEmpateCompilacao struct {
	albumNomeN   int
	albumTituloN int
	possuidaN    int
}

func (p *fakeProviderEmpateCompilacao) Nome() string { return sourceMusicBrainz }

func (p *fakeProviderEmpateCompilacao) Buscar(_ context.Context, query, tipo, _ string) ([]provider.ResultadoExterno, error) {
	if tipo != "" && tipo != "artista" {
		return nil, nil
	}
	if query == "Tie Artist" {
		return []provider.ResultadoExterno{
			{Source: sourceMusicBrainz, SourceID: mbidArtistaTie, Type: "artista", Title: "Tie Artist"},
			{Source: sourceMusicBrainz, SourceID: mbidArtistaTie2, Type: "artista", Title: "Tie Artist"},
		}, nil
	}
	// "Various Artists" e qualquer outro nome: sem identidade de artista.
	return nil, nil
}

func (p *fakeProviderEmpateCompilacao) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (p *fakeProviderEmpateCompilacao) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (p *fakeProviderEmpateCompilacao) BuscarAlbumDaFaixa(context.Context, string, []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

// BuscarAlbumPorNome: só o SEGUNDO "Tie Artist" tem "Tie Album" -- é essa a
// evidência que desempata.
func (p *fakeProviderEmpateCompilacao) BuscarAlbumPorNome(_ context.Context, artistaMBID, nomeAlbum string, _ provider.AlvoFaixaAlbum, _ []string) (provider.AlbumExterno, bool, error) {
	p.albumNomeN++
	if artistaMBID != mbidArtistaTie2 || nomeAlbum != "Tie Album" {
		return provider.AlbumExterno{}, false, nil
	}
	return provider.AlbumExterno{
		Name: "Tie Album", MBID: mbidAlbumTie, Year: "2001",
		Faixas: []provider.FaixaExterna{
			{Title: "Tie Track", MBID: "rec-tie-1", TrackNumber: 1},
		},
	}, true, nil
}

func (p *fakeProviderEmpateCompilacao) BuscarAlbumPorMbid(context.Context, string, []string) (provider.AlbumExterno, error) {
	return provider.AlbumExterno{}, provider.ErrNaoEncontrado
}

// BuscarAlbumPorTitulo é a capacidade nova (briefing 0136): trilha compilada,
// sem artista local -- o artist-credit vem da fonte.
func (p *fakeProviderEmpateCompilacao) BuscarAlbumPorTitulo(_ context.Context, nomeAlbum string, _ provider.AlvoFaixaAlbum, _ []string) (provider.AlbumComArtista, bool, error) {
	p.albumTituloN++
	if nomeAlbum != "Trilha do Filme" {
		return provider.AlbumComArtista{}, false, nil
	}
	return provider.AlbumComArtista{
		Album: provider.AlbumExterno{
			Name: "Trilha do Filme", MBID: mbidAlbumCompil, Year: "2010",
			Faixas: []provider.FaixaExterna{
				{Title: "Tema Principal", MBID: "rec-comp-1", TrackNumber: 4},
			},
		},
		ArtistaMBID: mbidArtistaVarios,
		ArtistaNome: "Various Artists",
	}, true, nil
}

// BuscarAlbumPorTituloNaoUsado garante que o fake satisfaça a interface de
// faixa possuída usada pelo caminho por artista (não é chamado nos testes de
// compilação, mas o type-assert de MusicaAlbumProvider exige o método).
func (p *fakeProviderEmpateCompilacao) ImportarFaixaPossuida(context.Context, string, string, enrich.FaixaAlvo, []string) (string, bool, error) {
	p.possuidaN++
	return "", false, nil
}

// managerEmpateCompilacao monta o Manager com a vertical de música real
// (enrich.Servico) sobre o fake acima.
func managerEmpateCompilacao(t *testing.T, d *db.DB, tags map[string]mediainfo.AudioTags, prov *fakeProviderEmpateCompilacao) *Manager {
	t.Helper()
	m := NewManagerComProber(d, proberComTags{porPath: tags})
	m.agora = relogioCrescente()
	svc := enrich.New(d, provider.NovaRegistry(prov), t.TempDir())
	m.ConfigurarMusica(svc, prov)
	m.idiomas = func(context.Context) []string { return []string{"en"} }
	return m
}

// TestScanMusicaEmpateDeArtistaResolvePorEvidencia cobre o caso "Linkin Park"
// medido em produção: dois artistas com o MESMO nome (score 100 nos dois). Antes
// do 0136 isso virava rascunho; agora o pipeline testa os empatados e aplica o
// que contém o álbum da tag.
func TestScanMusicaEmpateDeArtistaResolvePorEvidencia(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Tie Track.mp3")
	escrever(t, arq)

	prov := &fakeProviderEmpateCompilacao{}
	m := managerEmpateCompilacao(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Tie Artist", Album: "Tie Album", Title: "Tie Track", TrackNumber: 1},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, _, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || status == "draft" {
		t.Fatalf("faixa = {type:%q status:%q}, quero faixa resolvida (fora do rascunho)", tipo, status)
	}
	if conf != confMusicaTextual {
		t.Errorf("match_confidence = %v, quero %v", conf, confMusicaTextual)
	}
	// Tentou os DOIS empatados (o primeiro não tem o álbum, o segundo tem).
	if prov.albumNomeN < 2 {
		t.Errorf("BuscarAlbumPorNome chamado %d vez(es), quero >= 2 (os dois empatados)", prov.albumNomeN)
	}
}

// TestScanMusicaCompilacaoResolvePorTituloDoAlbum cobre o caso "Various Artists"
// (34 faixas reais): a busca de artista não devolve nada e o álbum só existe pelo
// título -- o artista principal vem do artist-credit da fonte.
func TestScanMusicaCompilacaoResolvePorTituloDoAlbum(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "04 - Tema Principal.mp3")
	escrever(t, arq)

	prov := &fakeProviderEmpateCompilacao{}
	m := managerEmpateCompilacao(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Various Artists", Artist: "Banda da Trilha", Album: "Trilha do Filme", Title: "Tema Principal", TrackNumber: 4},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	_, _, status := entityDe(t, d, entityID)
	if status == "draft" {
		t.Fatal("faixa continuou em rascunho: o caminho de compilação não foi usado")
	}
	if conf != confMusicaTextual {
		t.Errorf("match_confidence = %v, quero %v", conf, confMusicaTextual)
	}
	if prov.albumTituloN != 1 {
		t.Errorf("BuscarAlbumPorTitulo chamado %d vez(es), quero 1", prov.albumTituloN)
	}

	// O artista da árvore é o do artist-credit da FONTE (Various Artists), não a
	// tag local "Banda da Trilhada" do arquivo.
	var artistas []string
	rows, err := d.Read.QueryContext(context.Background(),
		`SELECT e.title FROM entity e WHERE e.type = 'artista'`)
	if err != nil {
		t.Fatalf("listar artistas: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var titulo string
		if err := rows.Scan(&titulo); err != nil {
			t.Fatalf("scan artista: %v", err)
		}
		artistas = append(artistas, titulo)
	}
	if len(artistas) != 1 || artistas[0] == "" {
		t.Fatalf("artistas = %v, quero exatamente 1 (o do artist-credit da fonte)", artistas)
	}
}
