package acoustid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// novoClientDeTeste monta um Client apontando para um servidor fake
// (httptest.Server), SEM nenhum sleep real do rate limiter (intervalo=0) --
// mesmo padrão de internal/provider/musicbrainz/musicbrainz_test.go
// novoClientDeTeste. O comportamento do rate limiter em si é coberto
// isoladamente por ratelimit_test.go.
func novoClientDeTeste(baseURL, clientKey string) *Client {
	return &Client{
		cred:       Credencial{ClientKey: clientKey},
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    baseURL,
		lim:        novoLimitador(0),
	}
}

func TestIdentificarPorFingerprint_Sucesso(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client") != "chave-de-teste" {
			t.Errorf("client = %q, esperava chave-de-teste", q.Get("client"))
		}
		if q.Get("fingerprint") != "AQAAT0mUaEkS" {
			t.Errorf("fingerprint = %q inesperado", q.Get("fingerprint"))
		}
		if q.Get("duration") != "245" {
			t.Errorf("duration = %q, esperava 245", q.Get("duration"))
		}
		if q.Get("meta") != "recordings" {
			t.Errorf("meta = %q, esperava recordings", q.Get("meta"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"results": [
				{"id": "res-baixo", "score": 0.4, "recordings": [{"id": "mbid-baixo"}]},
				{"id": "res-alto", "score": 0.92, "recordings": [{"id": "mbid-alto"}]},
				{"id": "res-sem-recording", "score": 0.99}
			]
		}`))
	}))
	defer srv.Close()

	c := novoClientDeTeste(srv.URL, "chave-de-teste")
	cand, ok, err := c.IdentificarPorFingerprint(context.Background(), "AQAAT0mUaEkS", 245.4)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !ok {
		t.Fatal("esperava ok=true")
	}
	if cand.RecordingMBID != "mbid-alto" {
		t.Errorf("RecordingMBID = %q, esperava mbid-alto (maior score, com recording)", cand.RecordingMBID)
	}
	if cand.Confianca != 0.92 {
		t.Errorf("Confianca = %v, esperava 0.92", cand.Confianca)
	}
}

func TestIdentificarPorFingerprint_SemResultado(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status": "ok", "results": []}`))
	}))
	defer srv.Close()

	c := novoClientDeTeste(srv.URL, "chave-de-teste")
	cand, ok, err := c.IdentificarPorFingerprint(context.Background(), "fp-desconhecido", 10)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if ok {
		t.Fatalf("esperava ok=false, achou %+v", cand)
	}
}

func TestIdentificarPorFingerprint_ResultadosSoComRecordingVazio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"results": [{"id": "res-1", "score": 0.95}]
		}`))
	}))
	defer srv.Close()

	c := novoClientDeTeste(srv.URL, "chave-de-teste")
	_, ok, err := c.IdentificarPorFingerprint(context.Background(), "fp-x", 10)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if ok {
		t.Fatal("esperava ok=false quando nenhum resultado tem recording")
	}
}

func TestIdentificarPorFingerprint_ErroDaAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status": "error", "error": {"code": 4, "message": "invalid API key"}}`))
	}))
	defer srv.Close()

	c := novoClientDeTeste(srv.URL, "chave-invalida")
	_, _, err := c.IdentificarPorFingerprint(context.Background(), "fp-x", 10)
	if err == nil {
		t.Fatal("esperava erro quando a API devolve status=error")
	}
}

func TestIdentificarPorFingerprint_StatusHTTPInesperado(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := novoClientDeTeste(srv.URL, "chave-de-teste")
	_, _, err := c.IdentificarPorFingerprint(context.Background(), "fp-x", 10)
	if err == nil {
		t.Fatal("esperava erro em status HTTP != 200")
	}
}

func TestIdentificarPorFingerprint_ClientKeyAusenteDegradaSemRede(t *testing.T) {
	chamou := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamou = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := novoClientDeTeste(srv.URL, "") // sem client key configurada
	cand, ok, err := c.IdentificarPorFingerprint(context.Background(), "fp-qualquer", 10)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if ok {
		t.Fatalf("esperava ok=false sem client key, achou %+v", cand)
	}
	if chamou {
		t.Fatal("nao deveria ter batido na rede sem client key configurada")
	}
}

func TestIdentificarPorFingerprint_FingerprintVazio(t *testing.T) {
	c := novoClientDeTeste("http://example.invalid", "chave-de-teste")
	_, _, err := c.IdentificarPorFingerprint(context.Background(), "", 10)
	if err == nil {
		t.Fatal("esperava erro com fingerprint vazio")
	}
}

func TestIdentificarPorFingerprint_RateLimitRespeitado(t *testing.T) {
	var chamadas int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status": "ok", "results": []}`))
	}))
	defer srv.Close()

	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	c := &Client{
		cred:       Credencial{ClientKey: "chave-de-teste"},
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    srv.URL,
		lim: &limitador{
			intervalo: intervaloRateLimit,
			agora:     rel.now,
			dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
		},
	}

	for i := 0; i < 3; i++ {
		if _, _, err := c.IdentificarPorFingerprint(context.Background(), "fp-x", 10); err != nil {
			t.Fatalf("chamada %d: erro inesperado: %v", i, err)
		}
	}

	if chamadas != 3 {
		t.Fatalf("chamadas HTTP = %d, esperava 3", chamadas)
	}
	if len(dorm) != 2 {
		t.Fatalf("esperava 2 esperas do rate limiter (so a 1a chamada eh gratis), houve %d", len(dorm))
	}
	for i, d := range dorm {
		if d != intervaloRateLimit {
			t.Fatalf("espera[%d] = %v, esperava %v (limite de 3 req/s)", i, d, intervaloRateLimit)
		}
	}
}

func TestNew_DefaultsQuandoVazio(t *testing.T) {
	c := New(Credencial{ClientKey: "x"}, nil, "")
	if c.httpClient == nil {
		t.Error("httpClient nil deveria virar um default")
	}
	if c.baseURL != baseURLPadrao {
		t.Errorf("baseURL = %q, esperava %q", c.baseURL, baseURLPadrao)
	}
}

func TestNew_BaseURLCustomizada(t *testing.T) {
	custom := "http://example.invalid/lookup"
	c := New(Credencial{}, nil, custom)
	if c.baseURL != custom {
		t.Errorf("baseURL = %q, esperava %q", c.baseURL, custom)
	}
}

func TestNome(t *testing.T) {
	c := New(Credencial{}, nil, "")
	if c.Nome() != "acoustid" {
		t.Errorf("Nome() = %q, esperava acoustid", c.Nome())
	}
}

func TestCoreProviderStubs_NaoQuebram(t *testing.T) {
	c := New(Credencial{}, nil, "")
	if res, err := c.Buscar(context.Background(), "qualquer", "", ""); err != nil || res != nil {
		t.Errorf("Buscar deveria ser NO-OP silencioso: res=%v err=%v", res, err)
	}
	if raw, err := c.BuscarDetalhe(context.Background(), "id", "", nil); err != nil || string(raw) != "{}" {
		t.Errorf("BuscarDetalhe deveria devolver {} sem erro: raw=%s err=%v", raw, err)
	}
	if m, err := c.MapearDetalhe([]byte(`{}`)); err != nil || len(m) != 0 {
		t.Errorf("MapearDetalhe deveria devolver mapa vazio sem erro: m=%v err=%v", m, err)
	}
}

// Confirma que a URL montada de fato usa a base configurada (não a
// constante padrão), mesmo espírito de outros pacotes que injetam baseURL.
func TestGetLookup_UsaBaseURLInjetada(t *testing.T) {
	var recebida *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recebida = r.URL
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()

	c := novoClientDeTeste(srv.URL, "chave-de-teste")
	if _, _, err := c.IdentificarPorFingerprint(context.Background(), "fp", 1); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if recebida == nil {
		t.Fatal("servidor de teste nao recebeu nenhuma requisicao")
	}
}

// Garante que o Client satisfaz mesmo a capacidade provider.FingerprintProvider
// (contrato exato do briefing 0126) -- redundante em runtime com a asserção
// de pacote em acoustid.go, mas mantido aqui como documentação executável
// local ao teste.
func TestIdentificarPorFingerprint_TipoDeRetorno(t *testing.T) {
	var _ provider.FingerprintProvider = (*Client)(nil)
}
