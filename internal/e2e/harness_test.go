// Package e2e hosts the BLACK-BOX harness of Layer 1: it starts the REAL
// Server (httptest.NewServer) with the fake fixtures provider, a temporary database
// migrated from scratch and a local image server, and exercises whole API JOURNEYS
// over HTTP — the same ones that used to be manual smoke tests through curl.
// Deterministic, with no network, no secret, and it enters the ratchet
// (e2e_coverage_lines metric via -coverpkg).
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
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// pngFixture is a minimal valid 1x1 PNG, served for ANY image path: the enrich
// downloads and caches the poster without touching a CDN. Fixed bytes = determinism.
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

// cliente is the HTTP driver of the harness: it talks to the real Server through its public URL.
// `prov` is the SAME *fake.Provider injected into the Server: the harness keeps being
// black-box through the API, but some effects (e.g. which language reached the
// provider in a search) do not show up in the HTTP body of any endpoint -- that is the
// only place to confirm them without instrumenting the real network. See
// fake.Provider.IdiomasBuscados.
type cliente struct {
	t    *testing.T
	base string
	hc   *http.Client
	prov *fake.Provider
}

// novoHarness starts everything a journey needs and returns an HTTP client. It closes
// the servers and the database in the test Cleanup.
func novoHarness(t *testing.T) *cliente {
	t.Helper()
	return novoHarnessComOpcoes(t)
}

// novoHarnessComOpcoes is the base of novoHarness: it assembles real
// database+image+Server, accepting `extra` ADDITIONAL server.Option on top of the
// always present WithSearchProvider/WithAssetsDir pair -- used by
// novoHarnessComYtdlp (journeys_trailer_tema_test.go, ADR-0025) to inject a
// ytdlp.Gerenciador without duplicating the whole harness boot.
func novoHarnessComOpcoes(t *testing.T, extra ...server.Option) *cliente {
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

	prov := fake.New(img.URL)
	// The unified execution queue (ADR-0028) is required by the scanner ever
	// since it stopped firing its own goroutine -- Start() only enqueues; with
	// no worker running, POST .../scan never leaves the 'queued' state. In
	// production the one that starts this is cmd/mymediadb/main.go (the next
	// layer, outside the scope of the scanner itself); here the E2E harness
	// starts and runs its OWN test execqueue.Manager, exactly as the real boot
	// would do.
	fila := execqueue.NewManager(database)
	filaCtx, filaCancel := context.WithCancel(context.Background())
	filaDone := make(chan struct{})
	go func() {
		fila.Run(filaCtx)
		close(filaDone)
	}()
	t.Cleanup(func() {
		filaCancel()
		<-filaDone
	})

	opts := append([]server.Option{
		server.WithSearchProvider(prov),
		server.WithAssetsDir(t.TempDir()),
		server.WithExecQueue(fila),
	}, extra...)
	srv := server.New("e2e", database, opts...)
	// Wires the jobs system (internal/job) to the SAME unified queue, mirroring
	// srv.Jobs().ConfigurarFila(fila) from cmd/mymediadb/main.go -- without this,
	// POST /jobs/{id}/run enqueues but NO worker ever runs the Handler (no
	// journey used jobs until journeys_reconciliar_test.go became the first
	// one). Nil when no job option (e.g. WithLibraryHealthJob) was applied --
	// srv.Jobs() returns nil in that case.
	if jobs := srv.Jobs(); jobs != nil {
		jobs.ConfigurarFila(fila)
	}
	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("montar handler: %v", err)
	}
	pub := httptest.NewServer(handler)
	t.Cleanup(pub.Close)

	return &cliente{t: t, base: pub.URL, hc: pub.Client(), prov: prov}
}

// bruto sends a request and returns the status + raw body. body nil => no body.
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

// obj sends the request, requires the expected status and decodes the body into an object.
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

// arr is like obj, but the body is an array (e.g. GET members).
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

// --- assertion accessors over the decoded JSON (map[string]any) ---

func str(t *testing.T, m map[string]any, chave string) string {
	t.Helper()
	v, ok := m[chave].(string)
	if !ok {
		t.Fatalf("campo %q ausente ou nao-string em %v", chave, m)
	}
	return v
}

// strs returns a []string from an array-of-strings field (tags etc.).
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

// objs converts an already decoded JSON array ([]any, e.g. returned by
// cliente.arr or by an array field inside an object) to []map[string]any,
// failing loudly if any element is not an object — it avoids repeating the
// type-assert in every journey that iterates over members/tracks/etc.
func objs(t *testing.T, xs []any) []map[string]any {
	t.Helper()
	out := make([]map[string]any, len(xs))
	for i, x := range xs {
		m, ok := x.(map[string]any)
		if !ok {
			t.Fatalf("elemento[%d] nao e objeto: %v", i, x)
		}
		out[i] = m
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
