package scanner

// MUSIC matching tests: artist tie-breaking by EVIDENCE (in which candidate does the
// album of the tag exist?) and the COMPILATION/SOUNDTRACK path (album resolved by
// title, artist coming from the artist-credit of the source). Fakes of this file own,
// so as not to disturb the counts of the fakeMusicaProvider used by the other music
// tests.

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

// fakeProviderEmpateCompilacao covers the two cases of the music matching:
//   - "Tie Artist" returns TWO candidates with the same name (different ids) and the
//     album "Tie Album" only exists in the second one -- the pipeline has to resolve
//     by evidence, not give up on the tie;
//   - "Various Artists" does NOT return an artist; the soundtrack album only resolves
//     through the new capability MusicaAlbumPorTituloProvider.
type fakeProviderEmpateCompilacao struct {
	albumNomeN   int
	albumTituloN int
	possuidaN    int
	// ultimoAlvoTitulo keeps the target received by BuscarAlbumPorTitulo, so the test
	// can check what the scanner SENDS on the path by title (album artist and track
	// disc).
	ultimoAlvoTitulo provider.AlvoFaixaAlbum
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
	// "Various Artists" and any other name: no artist identity.
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

// BuscarAlbumPorNome: only the SECOND "Tie Artist" has "Tie Album" -- that is the
// evidence that breaks the tie.
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

// BuscarAlbumPorTitulo is the new capability: a compiled soundtrack, without a local
// artist -- the artist-credit comes from the source.
func (p *fakeProviderEmpateCompilacao) BuscarAlbumPorTitulo(_ context.Context, nomeAlbum string, alvo provider.AlvoFaixaAlbum, _ []string) (provider.AlbumComArtista, bool, error) {
	p.albumTituloN++
	p.ultimoAlvoTitulo = alvo
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

// ImportarFaixaPossuida guarantees that the fake satisfies the owned-track interface
// used by the path by artist (it is not called in the compilation tests, but the
// type-assert of MusicaAlbumProvider requires the method).
func (p *fakeProviderEmpateCompilacao) ImportarFaixaPossuida(context.Context, string, string, enrich.FaixaAlvo, []string) (string, bool, error) {
	p.possuidaN++
	return "", false, nil
}

// managerEmpateCompilacao assembles the Manager with the real music vertical
// (enrich.Servico) on top of the fake above.
func managerEmpateCompilacao(t *testing.T, d *db.DB, tags map[string]mediainfo.AudioTags, prov *fakeProviderEmpateCompilacao) *Manager {
	t.Helper()
	m := NewManagerComProber(d, proberComTags{porPath: tags})
	m.agora = relogioCrescente()
	svc := enrich.New(d, provider.NovaRegistry(prov), t.TempDir())
	m.ConfigurarMusica(svc, prov)
	m.idiomas = func(context.Context) []string { return []string{"en"} }
	return m
}

// TestScanMusicaEmpateDeArtistaResolvePorEvidencia covers the "Linkin Park" case
// measured in production: two artists with the SAME name (score 100 on both). This used
// to become a draft; now the pipeline tests the tied candidates and applies the one
// that contains the album of the tag.
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
	// It tried BOTH tied candidates (the first one does not have the album, the second
	// one does).
	if prov.albumNomeN < 2 {
		t.Errorf("BuscarAlbumPorNome chamado %d vez(es), quero >= 2 (os dois empatados)", prov.albumNomeN)
	}
}

// TestScanMusicaCompilacaoResolvePorTituloDoAlbum covers the "Various Artists" case
// (34 real tracks): the artist search returns nothing and the album only exists by
// title -- the main artist comes from the artist-credit of the source.
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

	// The artist of the tree is the one from the artist-credit of the SOURCE (Various
	// Artists), not the local tag of the file.
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

// TestScanMusicaPassaArtistaEDiscoAoTituloDoAlbum covers the plumbing of the path by
// album TITLE: it now receives the album artist (so the search can be scoped) and the
// disc of the track (to break ties between reissues). Without that, the search returned
// homonyms of other artists and the track from disc 2 fell into the draft.
func TestScanMusicaPassaArtistaEDiscoAoTituloDoAlbum(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "02 - Tema Principal.mp3")
	escrever(t, arq)

	prov := &fakeProviderEmpateCompilacao{}
	// AlbumArtist is a REAL artist (not a compilation) whose album "Trilha do Filme"
	// does not exist through the path by artist -- it forces step 4b.
	m := managerEmpateCompilacao(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Tie Artist", Artist: "Tie Artist", Album: "Trilha do Filme", Title: "Tema Principal", TrackNumber: 2, DiscNumber: 2},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if prov.albumTituloN != 1 {
		t.Fatalf("BuscarAlbumPorTitulo chamado %d vez(es), quero 1 (passo 4b)", prov.albumTituloN)
	}
	if prov.ultimoAlvoTitulo.ArtistaNome != "Tie Artist" {
		t.Errorf("ArtistaNome = %q, quero %q (album_artist da tag)", prov.ultimoAlvoTitulo.ArtistaNome, "Tie Artist")
	}
	if prov.ultimoAlvoTitulo.FaixaDisco != 2 {
		t.Errorf("FaixaDisco = %d, quero 2 (disc_number da tag)", prov.ultimoAlvoTitulo.FaixaDisco)
	}
}
