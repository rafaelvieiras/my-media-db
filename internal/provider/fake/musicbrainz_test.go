package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Testes unitários do fake de música (Musicbrainz): garantem o contrato que o
// import de artista (0106) e a tela externa de álbum (0115 Parte 2) esperam —
// mesmo espírito de fake_test.go (código de produção, ligado por
// MYMEDIADB_FAKE_PROVIDER, mantido coberto pela catraca unitária).

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
	// De propósito: só 1 das 2 faixas oficiais do release — prova o caminho
	// de "faixa faltando" sem precisar de scan real.
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

// Os demais métodos (Buscar/BuscarDetalhe/MapearDetalhe/BuscarAlbumDaFaixa/
// BuscarAlbumPorNome) são stubs inertes — nenhuma jornada real os exercita
// (nem Camada 1 nem Camada 2 sobem o scanner nem a busca textual de música),
// mas ainda precisam do contrato certo (nunca panic, shape estável) por
// satisfazerem as interfaces provider.Provider/MusicaAlbumProvider. Um teste
// por método (em vez de um só combinado) mantém o gocyclo baixo (AGENTS.md).
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
