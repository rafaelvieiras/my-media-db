package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// pngFake is a binary with the real PNG SIGNATURE in the first 8 bytes, so that
// http.DetectContentType (in the service from the cache without extension) resolves
// image/png.
var pngFake = append([]byte("\x89PNG\r\n\x1a\n"), []byte("conteudo-de-teste-do-proxy")...)

// servidorImagem starts an httptest that responds to the artwork at /img (200 image/png)
// and counts the hits — to prove that the 2nd read comes from the CACHE, without hitting
// the source again.
func servidorImagem(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/img":
			if hits != nil {
				*hits++
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngFake)
		case "/erro":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/texto":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("nao sou imagem"))
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serverProxy builds a Server with a temporary assetsDir and the allowlist restricted to
// the httptest host (db nil: the proxy does not touch the database). It returns the Server
// and the cache dir.
func serverProxy(t *testing.T, hostPermitido string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	s := New("test", nil, WithAssetsDir(dir))
	s.hostsImagem = map[string]bool{hostPermitido: true}
	return s, dir
}

// pegarProxy performs GET /images?src=<url> straight on the handler and returns the
// recorder.
func pegarProxy(t *testing.T, s *Server, src string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, rotaProxyImagem+"?src="+url.QueryEscape(src), nil)
	rec := httptest.NewRecorder()
	s.getImageProxy(rec, req)
	return rec
}

func hostDe(t *testing.T, bruta string) string {
	t.Helper()
	u, err := url.Parse(bruta)
	if err != nil {
		t.Fatalf("parse %q: %v", bruta, err)
	}
	return u.Host
}

// TestProxyImagemBaixaCacheiaEServe: the 1st hit downloads and caches on disk; the 2nd
// serves from the cache (source already closed) — same binary, content-type sniffed as
// image/png.
func TestProxyImagemBaixaCacheiaEServe(t *testing.T) {
	var hits int
	src := servidorImagem(t, &hits)
	s, dir := serverProxy(t, hostDe(t, src.URL))

	rec := pegarProxy(t, s, src.URL+"/img")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), pngFake) {
		t.Errorf("corpo != binario da fonte")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, quero image/png", ct)
	}

	entradas, err := os.ReadDir(filepath.Join(dir, subdirProxyImagem))
	if err != nil {
		t.Fatalf("ler cache: %v", err)
	}
	if len(entradas) != 1 {
		t.Fatalf("arquivos no cache = %d, quero 1", len(entradas))
	}

	// 2nd read: close the source first; it has to come from the cache (no 2nd hit).
	src.Close()
	rec2 := pegarProxy(t, s, src.URL+"/img")
	if rec2.Code != http.StatusOK || !bytes.Equal(rec2.Body.Bytes(), pngFake) {
		t.Fatalf("cache: status = %d (quero 200 com o mesmo binario)", rec2.Code)
	}
	if hits != 1 {
		t.Errorf("idas a fonte = %d, quero 1 (cache)", hits)
	}
}

func TestProxyImagemHostNaoPermitido(t *testing.T) {
	s, _ := serverProxy(t, "image.tmdb.org") // allowlist does NOT include the host below
	rec := pegarProxy(t, s, "https://evil.example.com/x.png")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_query")
}

// TestProxyAllowlistTheTVDB: the DEFAULT allowlist accepts the TheTVDB artwork host
// (artworks.thetvdb.com) -- needed for the proxy to serve the episode still
// on demand. An arbitrary host remains blocked (anti-SSRF).
func TestProxyAllowlistTheTVDB(t *testing.T) {
	if !hostsImagemPadrao["artworks.thetvdb.com"] {
		t.Fatal("allowlist padrao nao inclui artworks.thetvdb.com")
	}
	s := New("test", nil)
	still := "https://artworks.thetvdb.com/banners/episodes/72514/5.jpg"
	if _, ok := s.urlImagemPermitida(still); !ok {
		t.Errorf("urlImagemPermitida rejeitou o still do TheTVDB: %s", still)
	}
	if _, ok := s.urlImagemPermitida("https://evil.example.com/x.png"); ok {
		t.Error("urlImagemPermitida aceitou host arbitrario; quero rejeitar")
	}
}

func TestProxyImagemSrcAusente(t *testing.T) {
	s, _ := serverProxy(t, "image.tmdb.org")
	req := httptest.NewRequest(http.MethodGet, rotaProxyImagem, nil)
	rec := httptest.NewRecorder()
	s.getImageProxy(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
}

func TestProxyImagemSemAssetsDir(t *testing.T) {
	s := New("test", nil) // without WithAssetsDir
	rec := pegarProxy(t, s, "https://image.tmdb.org/t/p/w780/bg.jpg")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "image_proxy_unavailable")
}

func TestProxyImagemFonteErro(t *testing.T) {
	src := servidorImagem(t, nil)
	s, _ := serverProxy(t, hostDe(t, src.URL))
	rec := pegarProxy(t, s, src.URL+"/erro")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

func TestProxyImagemContentTypeNaoImagem(t *testing.T) {
	src := servidorImagem(t, nil)
	s, _ := serverProxy(t, hostDe(t, src.URL))
	rec := pegarProxy(t, s, src.URL+"/texto")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502 (nao-imagem nao cacheia)", rec.Code)
	}
}

// TestUrlImagemServidaReescreveEDegrada covers the DTO decision: it rewrites to the proxy
// when there is an assetsDir + allowed host; it returns the direct URL on degradation;
// nil→nil.
func TestUrlImagemServidaReescreveEDegrada(t *testing.T) {
	direta := "https://image.tmdb.org/t/p/w780/bg.jpg"

	com := New("test", nil, WithAssetsDir(t.TempDir()))
	if got := com.urlImagemServida(&direta); got == nil ||
		*got != rotaProxyImagem+"?src="+url.QueryEscape(direta) {
		t.Errorf("com assetsDir+host permitido: got %v, quero rota de proxy", got)
	}

	sem := New("test", nil) // without assetsDir => degrades to the direct URL
	if got := sem.urlImagemServida(&direta); got == nil || *got != direta {
		t.Errorf("sem assetsDir: got %v, quero a URL direta", got)
	}

	foraDaLista := "https://cdn.exemplo.com/x.png"
	if got := com.urlImagemServida(&foraDaLista); got == nil || *got != foraDaLista {
		t.Errorf("host fora da allowlist: got %v, quero a URL direta", got)
	}

	if got := com.urlImagemServida(nil); got != nil {
		t.Errorf("nil: got %v, quero nil", got)
	}
}

// TestEscreverAtomicoNaoDeixaArquivoPrivado covers the file mode of the artwork cache: without
// the Chmod, the rename of the temporary file (0600) leaves the image unreadable to any process
// that is not the container uid.
func TestEscreverAtomicoNaoDeixaArquivoPrivado(t *testing.T) {
	destino := filepath.Join(t.TempDir(), "capa.jpg")
	if err := escreverAtomico(destino, []byte("bytes")); err != nil {
		t.Fatalf("escreverAtomico: %v", err)
	}
	info, err := os.Stat(destino)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if modo := info.Mode().Perm(); modo != 0o664 {
		t.Errorf("modo = %04o, quero 0664", modo)
	}
}
