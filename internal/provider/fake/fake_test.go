package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Testes unitários do provider fake: garantem o contrato (Nome, busca, detalhe
// canônico, coleção, not-found) e mantêm o pacote coberto pela catraca unitária
// (ele é código de produção, ligado por MYMEDIADB_FAKE_PROVIDER).

func TestNomeEhTmdb(t *testing.T) {
	if got := New("").Nome(); got != "tmdb" {
		t.Fatalf("Nome() = %q, quero \"tmdb\" (dedup/interno-first)", got)
	}
}

func TestBuscarFiltraPorTituloETipo(t *testing.T) {
	p := New("http://img.local")
	res, err := p.Buscar(context.Background(), "matrix", "movie", "pt-BR")
	if err != nil {
		t.Fatalf("Buscar erro: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("resultados = %d, quero 2 (603 + 604)", len(res))
	}
	// Ordem estável por sourceID.
	if res[0].SourceID != "603" || res[1].SourceID != "604" {
		t.Errorf("ordem = [%s,%s], quero [603,604]", res[0].SourceID, res[1].SourceID)
	}
	if res[0].Year == nil || *res[0].Year != "1999" {
		t.Errorf("Year[0] = %v, quero 1999", res[0].Year)
	}
	if res[0].PosterURL == nil || *res[0].PosterURL != "http://img.local/matrix.png" {
		t.Errorf("PosterURL[0] = %v, quero http://img.local/matrix.png", res[0].PosterURL)
	}
	// Filtro que não casa nada.
	vazio, _ := p.Buscar(context.Background(), "inexistente", "", "")
	if len(vazio) != 0 {
		t.Errorf("busca sem match = %d, quero 0", len(vazio))
	}
}

func TestDetalheCanonicoMultiIdioma(t *testing.T) {
	p := New("")
	raw, err := p.BuscarDetalhe(context.Background(), "603", "movie", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("BuscarDetalhe erro: %v", err)
	}
	campos, err := p.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe erro: %v", err)
	}
	// Traduzíveis DEVEM ser map[string]any (o resolver exige esse tipo).
	title, ok := campos["title"].(map[string]any)
	if !ok {
		t.Fatalf("title nao e map[string]any: %T", campos["title"])
	}
	if title["pt-BR"] != "Matrix" || title["en"] != "The Matrix" {
		t.Errorf("title = %v, quero pt-BR=Matrix en=The Matrix", title)
	}
	if _, ok := campos["tags"].(map[string]any); !ok {
		t.Fatalf("tags nao e map[string]any: %T", campos["tags"])
	}
	if campos["original_language"] != "en" || campos["release_date"] != "1999-03-31" {
		t.Errorf("neutros errados: lang=%v date=%v", campos["original_language"], campos["release_date"])
	}
	// Sem ImageBase => sem poster_url.
	if _, tem := campos["poster_url"]; tem {
		t.Errorf("poster_url presente sem ImageBase: %v", campos["poster_url"])
	}
}

func TestBuscarDetalheNotFound(t *testing.T) {
	_, err := New("").BuscarDetalhe(context.Background(), "999", "movie", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("erro = %v, quero ErrNaoEncontrado", err)
	}
}

func TestBuscarColecaoComPartes(t *testing.T) {
	p := New("http://img.local")
	col, err := p.BuscarColecao(context.Background(), "2344", "pt-BR")
	if err != nil {
		t.Fatalf("BuscarColecao erro: %v", err)
	}
	if col.Name != "The Matrix Collection" || len(col.Parts) != 2 {
		t.Fatalf("colecao = %q com %d partes, quero 2", col.Name, len(col.Parts))
	}
	if col.Parts[0].SourceID != "603" || col.Parts[1].SourceID != "604" {
		t.Errorf("partes = [%s,%s], quero [603,604]", col.Parts[0].SourceID, col.Parts[1].SourceID)
	}
	if _, err := p.BuscarColecao(context.Background(), "0", ""); !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Errorf("colecao inexistente = %v, quero ErrNaoEncontrado", err)
	}
}
