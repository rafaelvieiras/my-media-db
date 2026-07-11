package tvdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// lerFixture lê um JSON de testdata/ (fixtures reais RECORTADAS do TheTVDB, sem token
// nem chave). Falha o teste se o arquivo não existir.
func lerFixture(t *testing.T, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", nome))
	if err != nil {
		t.Fatalf("fixture %s: %v", nome, err)
	}
	return b
}

// servidorTVDB sobe um httptest.Server que fala o dialeto v4: POST /login devolve um
// token fake e cada GET (com Bearer) é roteado a uma fixture. Um path sem fixture vira
// 404 (→ ErrNaoEncontrado). Rotas de tradução ausentes (episódio sem `por`) também 404.
func servidorTVDB(t *testing.T) *Client {
	t.Helper()
	rotas := map[string]string{
		"/series/72514/extended":              "series_looney_extended.json",
		"/series/71663/extended":              "series_simpsons_extended.json",
		"/series/71663/translations/por":      "series_simpsons_trans_por.json",
		"/series/72514/episodes/official":     "episodes_looney_official.json",
		"/series/72514/episodes/dvd":          "episodes_looney_dvd.json",
		"/series/72514/episodes/absolute":     "episodes_looney_absolute.json",
		"/series/72514/episodes/official/por": "episodes_looney_official_por.json",
		"/search":                             "search_series.json",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/login" {
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"faketoken"}}`))
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "sem auth", http.StatusUnauthorized)
			return
		}
		if nome, ok := rotas[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(lerFixture(t, nome))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return New(Credencial{APIKey: "chave-fake"}, srv.Client(), srv.URL)
}

func TestNome(t *testing.T) {
	if n := (&Client{}).Nome(); n != "tvdb" {
		t.Fatalf("Nome = %q, quero tvdb", n)
	}
}

func TestCredencialConfigurada(t *testing.T) {
	if (Credencial{}).Configurada() {
		t.Error("credencial vazia nao deveria estar configurada")
	}
	if !(Credencial{APIKey: "x"}).Configurada() {
		t.Error("credencial com apikey deveria estar configurada")
	}
}

// TestBuscarSeries valida a busca: type=series → ResultadoExterno com Type="tv",
// person descartado, id/ano/pôster mapeados.
func TestBuscarSeries(t *testing.T) {
	c := servidorTVDB(t)
	res, err := c.Buscar(context.Background(), "simpsons", "tv", "pt-BR")
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if len(res) != 2 { // person descartado
		t.Fatalf("len(res) = %d, quero 2", len(res))
	}
	r0 := res[0]
	if r0.Source != "tvdb" || r0.SourceID != "71663" || r0.Type != "tv" {
		t.Errorf("res[0] = %+v, quero tvdb/71663/tv", r0)
	}
	if r0.Title != "The Simpsons" {
		t.Errorf("title = %q", r0.Title)
	}
	if r0.Year == nil || *r0.Year != "1989" {
		t.Errorf("year = %v, quero 1989", r0.Year)
	}
	if r0.PosterURL == nil {
		t.Error("poster nil")
	}
}

// TestBuscarFilmeVazio garante que a fonte (só séries no MVP) não devolve nada para
// tipo=movie, sem erro.
func TestBuscarFilmeVazio(t *testing.T) {
	c := servidorTVDB(t)
	res, err := c.Buscar(context.Background(), "matrix", "movie", "")
	if err != nil || res != nil {
		t.Fatalf("Buscar movie = (%v, %v), quero (nil, nil)", res, err)
	}
}

// TestDetalheSerieMultiIdioma exercita BuscarDetalhe+MapearDetalhe do Simpsons: título/
// descrição por idioma (original en + tradução por/pt-BR) e os ids externos.
func TestDetalheSerieMultiIdioma(t *testing.T) {
	c := servidorTVDB(t)
	raw, err := c.BuscarDetalhe(context.Background(), "71663", "tv", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	canon, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}
	titulos := mapaStr(t, canon, "title")
	if titulos["en"] != "The Simpsons" {
		t.Errorf("title[en] = %q, quero The Simpsons", titulos["en"])
	}
	if titulos["pt-BR"] != "Os Simpsons" || titulos["pt"] != "Os Simpsons" {
		t.Errorf("title pt = %q/%q, quero Os Simpsons", titulos["pt-BR"], titulos["pt"])
	}
	if canon["original_language"] != "en" {
		t.Errorf("original_language = %v, quero en", canon["original_language"])
	}
	if canon["release_date"] != "1989-12-17" {
		t.Errorf("release_date = %v", canon["release_date"])
	}
}

// TestDetalheIDsExternos valida remoteIds→external_ids: Simpsons COM tmdb 456/imdb;
// Looney SEM tmdb/imdb (só tvdb).
func TestDetalheIDsExternos(t *testing.T) {
	c := servidorTVDB(t)
	simpsons := idsDoDetalhe(t, c, "71663")
	if simpsons["tmdb"] != "456" || simpsons["imdb"] != "tt0096697" {
		t.Errorf("simpsons ids = %v, quero tmdb 456 + imdb tt0096697", simpsons)
	}
	if simpsons["tvdb"] != "71663" {
		t.Errorf("simpsons tvdb = %v, quero 71663", simpsons["tvdb"])
	}
	looney := idsDoDetalhe(t, c, "72514")
	if _, temTmdb := looney["tmdb"]; temTmdb {
		t.Errorf("looney NAO deveria ter tmdb: %v", looney)
	}
	if _, temImdb := looney["imdb"]; temImdb {
		t.Errorf("looney NAO deveria ter imdb: %v", looney)
	}
	if looney["tvdb"] != "72514" {
		t.Errorf("looney tvdb = %v, quero 72514", looney["tvdb"])
	}
}

// TestDetalheNaoEncontrado garante o sentinela ErrNaoEncontrado para série inexistente.
func TestDetalheNaoEncontrado(t *testing.T) {
	c := servidorTVDB(t)
	_, err := c.BuscarDetalhe(context.Background(), "000", "tv", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("erro = %v, quero ErrNaoEncontrado", err)
	}
}

// TestCumpreInterfaces garante em compile-time que *Client satisfaz Provider e
// SerieProvider (o handler faz type-assert por SerieProvider).
func TestCumpreInterfaces(t *testing.T) {
	var _ provider.Provider = (*Client)(nil)
	var _ provider.SerieProvider = (*Client)(nil)
}

// idsDoDetalhe roda BuscarDetalhe+MapearDetalhe e devolve o mapa external_ids.
func idsDoDetalhe(t *testing.T, c *Client, id string) map[string]string {
	t.Helper()
	raw, err := c.BuscarDetalhe(context.Background(), id, "tv", nil)
	if err != nil {
		t.Fatalf("BuscarDetalhe %s: %v", id, err)
	}
	canon, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe %s: %v", id, err)
	}
	return mapaStr(t, canon, "external_ids")
}

// mapaStr extrai canon[chave] como map[string]string (os valores são strings no
// canônico). Falha se ausente ou de tipo errado.
func mapaStr(t *testing.T, canon map[string]any, chave string) map[string]string {
	t.Helper()
	bruto, ok := canon[chave].(map[string]any)
	if !ok {
		t.Fatalf("canon[%q] ausente ou tipo errado: %T", chave, canon[chave])
	}
	out := map[string]string{}
	for k, v := range bruto {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
