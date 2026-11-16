package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Unit tests of the music fake (Musicbrainz): they guarantee the contract that
// the artist import and the external album screen expect -- the same spirit as
// fake_test.go (production code, wired in by MYMEDIADB_FAKE_PROVIDER, kept
// covered by the unit ratchet).

func TestMusicbrainzNome(t *testing.T) {
	if got := NewMusicbrainz().Nome(); got != "musicbrainz" {
		t.Fatalf("Nome() = %q, quero \"musicbrainz\"", got)
	}
}

func TestMusicbrainzBuscarArtistaArvoreSoTrazAPrimeiraFaixa(t *testing.T) {
	arvore, err := NewMusicbrainz().BuscarArtistaArvore(context.Background(), MusicbrainzArtistaID, nil)
	if err != nil {
		t.Fatalf("BuscarArtistaArvore erro: %v", err)
	}
	if arvore.MBID != MusicbrainzArtistaID || arvore.Name != "Artista Fake" {
		t.Fatalf("artista = %+v, quero mbid=%s nome=Artista Fake", arvore, MusicbrainzArtistaID)
	}
	if len(arvore.Albuns) != 1 {
		t.Fatalf("albuns = %d, quero 1", len(arvore.Albuns))
	}
	alb := arvore.Albuns[0]
	if alb.MBID != MusicbrainzAlbumID {
		t.Errorf("album.MBID = %q, quero %q", alb.MBID, MusicbrainzAlbumID)
	}
	// On purpose: only 1 of the 2 official tracks of the release -- it proves the
	// "missing track" path without needing a real scan.
	if len(alb.Faixas) != 1 || alb.Faixas[0].MBID != MusicbrainzFaixa1ID {
		t.Fatalf("faixas da arvore = %+v, quero só [%s]", alb.Faixas, MusicbrainzFaixa1ID)
	}
}

func TestMusicbrainzBuscarAlbumPorMbidTrazAsDuasFaixasOficiais(t *testing.T) {
	alb, err := NewMusicbrainz().BuscarAlbumPorMbid(context.Background(), MusicbrainzAlbumID, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorMbid erro: %v", err)
	}
	if alb.Name != "Album Fake" || alb.Year != "2000" || alb.Genre != "rock" {
		t.Errorf("album = %+v, quero nome/ano/genero fixos", alb)
	}
	if len(alb.Faixas) != 2 {
		t.Fatalf("faixas = %d, quero 2 (release oficial completo)", len(alb.Faixas))
	}
	if alb.Faixas[0].MBID != MusicbrainzFaixa1ID || alb.Faixas[1].MBID != MusicbrainzFaixa2ID {
		t.Errorf("mbids das faixas = [%s,%s], quero [%s,%s]",
			alb.Faixas[0].MBID, alb.Faixas[1].MBID, MusicbrainzFaixa1ID, MusicbrainzFaixa2ID)
	}
}

func TestMusicbrainzBuscarAlbumPorMbidNaoEncontrado(t *testing.T) {
	_, err := NewMusicbrainz().BuscarAlbumPorMbid(context.Background(), "mbid-inexistente", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("erro = %v, quero ErrNaoEncontrado", err)
	}
}

// The remaining methods (Buscar/BuscarDetalhe/MapearDetalhe/BuscarAlbumDaFaixa/
// BuscarAlbumPorNome) are inert stubs -- no real journey exercises them (neither
// Layer 1 nor Layer 2 brings up the scanner nor the textual music search), but
// they still need the right contract (never panic, stable shape) because they
// satisfy the provider.Provider/MusicaAlbumProvider interfaces. One test per
// method (instead of a single combined one) keeps gocyclo low (AGENTS.md).
func TestMusicbrainzBuscarStub(t *testing.T) {
	res, err := NewMusicbrainz().Buscar(context.Background(), "qualquer", "", "")
	if err != nil || res != nil {
		t.Errorf("Buscar = (%v, %v), quero (nil, nil)", res, err)
	}
}

func TestMusicbrainzBuscarDetalheEMapearDetalheStub(t *testing.T) {
	m := NewMusicbrainz()
	raw, err := m.BuscarDetalhe(context.Background(), "x", "artista", nil)
	if err != nil || string(raw) != "{}" {
		t.Errorf("BuscarDetalhe = (%s, %v), quero ({}, nil)", raw, err)
	}
	campos, err := m.MapearDetalhe(raw)
	if err != nil || len(campos) != 0 {
		t.Errorf("MapearDetalhe = (%v, %v), quero (mapa vazio, nil)", campos, err)
	}
}

func TestMusicbrainzBuscarAlbumDaFaixaStub(t *testing.T) {
	_, ok, err := NewMusicbrainz().BuscarAlbumDaFaixa(context.Background(), "recording-x", nil)
	if ok || err != nil {
		t.Errorf("BuscarAlbumDaFaixa ok=%v err=%v, quero (false, nil)", ok, err)
	}
}

func TestMusicbrainzBuscarAlbumPorNomeStub(t *testing.T) {
	_, ok, err := NewMusicbrainz().BuscarAlbumPorNome(context.Background(), MusicbrainzArtistaID, "qualquer", provider.AlvoFaixaAlbum{}, nil)
	if ok || err != nil {
		t.Errorf("BuscarAlbumPorNome ok=%v err=%v, quero (false, nil)", ok, err)
	}
}
