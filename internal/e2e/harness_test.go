// Package e2e hospeda o harness BLACK-BOX da Camada 1 (briefing 0028): sobe o
// Server REAL (httptest.NewServer) com o provider fake de fixtures, banco temporário
// migrado do zero e um servidor de imagem local, e exercita JORNADAS de API inteiras
// por HTTP — as mesmas que antes eram smoke tests manuais via curl. Determinístico,
// sem rede, sem segredo, e entra na catraca (métrica e2e_coverage_lines via -coverpkg).
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// pngFixture é um PNG 1x1 mínimo válido, servido para QUALQUER caminho de imagem: o
// enrich baixa e cacheia o pôster sem tocar CDN. Bytes fixos = determinismo.
var pngFixture = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

// cliente é o driver HTTP do harness: fala com o Server real por sua URL pública.
type cliente struct {
	t    *testing.T
	base string
	hc   *http.Client
}

// novoHarness sobe tudo o que uma jornada precisa e devolve um cliente HTTP. Fecha
// os servidores e o banco no Cleanup do teste.
func novoHarness(t *testing.T) *cliente {
	t.Helper()

	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngFixture)
	}))
	t.Cleanup(img.Close)

	database, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "e2e.sqlite")})
	if err != nil {
		t.Fatalf("abrir banco: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrar banco: %v", err)
	}

	srv := server.New("e2e", database,
		server.WithSearchProvider(fake.New(img.URL)),
		server.WithAssetsDir(t.TempDir()),
	)
	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("montar handler: %v", err)
	}
	pub := httptest.NewServer(handler)
	t.Cleanup(pub.Close)

	return &cliente{t: t, base: pub.URL, hc: pub.Client()}
}

// bruto envia uma requisição e devolve status + corpo cru. body nil => sem corpo.
func (c *cliente) bruto(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal do corpo: %v", err)
		}
		r = bytes.NewReader(b)
	}
	reqst, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		c.t.Fatalf("montar request %s %s: %v", method, path, err)
	}
	if body != nil {
		reqst.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(reqst)
	if err != nil {
		c.t.Fatalf("request %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("ler corpo %s %s: %v", method, path, err)
	}
	return resp.StatusCode, raw
}

// obj envia a requisição, exige o status esperado e decodifica o corpo num objeto.
func (c *cliente) obj(method, path string, body any, statusEsperado int) map[string]any {
	c.t.Helper()
	status, raw := c.bruto(method, path, body)
	if status != statusEsperado {
		c.t.Fatalf("%s %s: status = %d, quero %d (corpo=%s)", method, path, status, statusEsperado, raw)
	}
	var m map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			c.t.Fatalf("%s %s: corpo nao e objeto: %v (corpo=%s)", method, path, err, raw)
		}
	}
	return m
}

// arr é como obj, mas o corpo é um array (ex.: GET members).
func (c *cliente) arr(method, path string, body any, statusEsperado int) []any {
	c.t.Helper()
	status, raw := c.bruto(method, path, body)
	if status != statusEsperado {
		c.t.Fatalf("%s %s: status = %d, quero %d (corpo=%s)", method, path, status, statusEsperado, raw)
	}
	var a []any
	if err := json.Unmarshal(raw, &a); err != nil {
		c.t.Fatalf("%s %s: corpo nao e array: %v (corpo=%s)", method, path, err, raw)
	}
	return a
}

// --- acessores de asserção sobre o JSON decodificado (map[string]any) ---

func str(t *testing.T, m map[string]any, chave string) string {
	t.Helper()
	v, ok := m[chave].(string)
	if !ok {
		t.Fatalf("campo %q ausente ou nao-string em %v", chave, m)
	}
	return v
}

// strs devolve um []string de um campo array de strings (tags etc.).
func strs(t *testing.T, m map[string]any, chave string) []string {
	t.Helper()
	bruto, ok := m[chave].([]any)
	if !ok {
		t.Fatalf("campo %q ausente ou nao-array em %v", chave, m)
	}
	out := make([]string, 0, len(bruto))
	for _, e := range bruto {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contem(xs []string, alvo string) bool {
	for _, x := range xs {
		if x == alvo {
			return true
		}
	}
	return false
}
