package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/integration"
)

// fakeIntegrationDescritor é um integration.Descriptor mínimo para exercitar
// a API genérica sem depender do tmdb real (briefing 0095). Dois campos:
// "token" (secreto, com env var de fallback) e "modo" (texto, sem env var —
// só pode vir do banco), cobrindo os dois ramos de `valor` no JSON (nunca
// para secreto; presente para não-secreto quando há valor efetivo).
type fakeIntegrationDescritor struct{}

func (fakeIntegrationDescritor) Slug() string      { return "fake" }
func (fakeIntegrationDescritor) Nome() string      { return "Fake" }
func (fakeIntegrationDescritor) Descricao() string { return "Integração fake para teste." }
func (fakeIntegrationDescritor) Campos() []integration.Campo {
	return []integration.Campo{
		{Nome: "token", Label: "Token", Tipo: integration.TipoSegredo, EnvVar: "MYMEDIADB_TESTE_FAKE_TOKEN"},
		{Nome: "modo", Label: "Modo", Tipo: integration.TipoTexto},
	}
}
func (fakeIntegrationDescritor) Configurada(efetivos map[string]string) bool {
	return efetivos["token"] != ""
}

// setupIntegrationsServer sobe um Server com banco temporário migrado e o
// fakeIntegrationDescritor já registrado sob o slug "fake".
func setupIntegrationsServer(t *testing.T) (http.Handler, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "integrations.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	h, err := New("v-teste", database, WithIntegrations(fakeIntegrationDescritor{})).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database
}

// campoPorNome acha um item de `campos` (decodificado como []any de
// map[string]any) pelo `nome` — usado para inspecionar a presença/ausência
// EXATA da chave `valor` no JSON cru (integrationCampoDTO.Valor é omitempty).
func campoPorNome(t *testing.T, corpo map[string]any, nome string) map[string]any {
	t.Helper()
	campos, ok := corpo["campos"].([]any)
	if !ok {
		t.Fatalf("corpo.campos não é array: %v", corpo)
	}
	for _, c := range campos {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if m["nome"] == nome {
			return m
		}
	}
	t.Fatalf("campo %q não encontrado em %v", nome, campos)
	return nil
}

// bufferDeBytes é satisfeita por *bytes.Buffer (httptest.ResponseRecorder.Body).
type bufferDeBytes interface{ Bytes() []byte }

func decodeMap(t *testing.T, buf bufferDeBytes) map[string]any {
	t.Helper()
	var out map[string]any
	body := buf.Bytes()
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar JSON: %v (corpo=%s)", err, body)
	}
	return out
}

// TestListIntegrationsConfiguradaFalseSemNada cobre §5: nem banco nem env var
// têm valor ⇒ GET /integrations lista "fake" com configurada:false.
func TestListIntegrationsConfiguradaFalseSemNada(t *testing.T) {
	h, _ := setupIntegrationsServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/integrations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var body integrationsListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	if len(body.Integrations) != 1 {
		t.Fatalf("Integrations = %v, quero 1 item", body.Integrations)
	}
	item := body.Integrations[0]
	if item.Slug != "fake" || item.Configurada {
		t.Errorf("item = %+v, quero slug=fake configurada=false", item)
	}
}

// TestGetIntegrationEnvVarOrigemEnv cobre §5: setar a env var (sem tocar o
// banco) faz GET /integrations devolver configurada:true e GET
// /integrations/fake devolver o campo token com origem:"env", definido:true,
// SEM a chave "valor" (é secreto).
func TestGetIntegrationEnvVarOrigemEnv(t *testing.T) {
	t.Setenv("MYMEDIADB_TESTE_FAKE_TOKEN", "do-ambiente")
	h, _ := setupIntegrationsServer(t)

	recList := do(t, h, http.MethodGet, "/api/v1/integrations", nil)
	var list integrationsListDTO
	if err := json.Unmarshal(recList.Body.Bytes(), &list); err != nil {
		t.Fatalf("decodificar lista: %v", err)
	}
	if !list.Integrations[0].Configurada {
		t.Error("configurada deveria ser true com a env var setada")
	}

	rec := do(t, h, http.MethodGet, "/api/v1/integrations/fake", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	corpo := decodeMap(t, rec.Body)
	tokenCampo := campoPorNome(t, corpo, "token")
	if tokenCampo["origem"] != "env" {
		t.Errorf("origem = %v, quero env", tokenCampo["origem"])
	}
	if tokenCampo["definido"] != true {
		t.Errorf("definido = %v, quero true", tokenCampo["definido"])
	}
	if _, existe := tokenCampo["valor"]; existe {
		t.Errorf("campo secreto NUNCA deveria devolver \"valor\": %v", tokenCampo)
	}
}

// TestPutIntegrationGravaNoBancoComPrecedencia cobre §5: PUT grava no banco;
// GET subsequente devolve origem:"banco" (a env var segue setada, mas não é
// mais a fonte efetiva).
func TestPutIntegrationGravaNoBancoComPrecedencia(t *testing.T) {
	t.Setenv("MYMEDIADB_TESTE_FAKE_TOKEN", "do-ambiente")
	h, _ := setupIntegrationsServer(t)

	valor := "abc"
	rec := do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{"token": &valor}})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	corpoPut := decodeMap(t, rec.Body)
	if campoPorNome(t, corpoPut, "token")["origem"] != "banco" {
		t.Errorf("origem no eco do PUT = %v, quero banco", campoPorNome(t, corpoPut, "token")["origem"])
	}

	recGet := do(t, h, http.MethodGet, "/api/v1/integrations/fake", nil)
	corpoGet := decodeMap(t, recGet.Body)
	if got := campoPorNome(t, corpoGet, "token")["origem"]; got != "banco" {
		t.Errorf("GET após PUT: origem = %v, quero banco", got)
	}
}

// TestPutIntegrationNullRemoveOverride cobre §5: PUT com {"token":null}
// remove o override — GET volta a mostrar origem:"env" (a env var segue
// setada).
func TestPutIntegrationNullRemoveOverride(t *testing.T) {
	t.Setenv("MYMEDIADB_TESTE_FAKE_TOKEN", "do-ambiente")
	h, _ := setupIntegrationsServer(t)

	valor := "abc"
	do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{"token": &valor}})

	rec := do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{"token": nil}})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT (null) status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	corpo := decodeMap(t, rec.Body)
	if got := campoPorNome(t, corpo, "token")["origem"]; got != "env" {
		t.Errorf("origem após remover override = %v, quero env", got)
	}
}

// TestPutIntegrationNullSemEnvVoltaParaNenhum cobre a variante do critério
// acima sem env var setada: remover o override cai em origem:"nenhum".
func TestPutIntegrationNullSemEnvVoltaParaNenhum(t *testing.T) {
	h, _ := setupIntegrationsServer(t)

	valor := "abc"
	do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{"token": &valor}})

	rec := do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{"token": nil}})
	corpo := decodeMap(t, rec.Body)
	campo := campoPorNome(t, corpo, "token")
	if campo["origem"] != "nenhum" || campo["definido"] != false {
		t.Errorf("campo após remover sem env = %v, quero origem=nenhum definido=false", campo)
	}
}

// TestGetIntegrationInexistente404 cobre §5: GET /integrations/inexistente → 404.
func TestGetIntegrationInexistente404(t *testing.T) {
	h, _ := setupIntegrationsServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/integrations/inexistente", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestPutIntegrationInexistente404 cobre o mesmo 404 no PUT.
func TestPutIntegrationInexistente404(t *testing.T) {
	h, _ := setupIntegrationsServer(t)
	valor := "x"
	rec := do(t, h, http.MethodPut, "/api/v1/integrations/inexistente",
		map[string]any{"valores": map[string]*string{"token": &valor}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestPutIntegrationValoresVazio422 cobre §5: PUT {"valores":{}} → 422.
func TestPutIntegrationValoresVazio422(t *testing.T) {
	h, _ := setupIntegrationsServer(t)
	rec := do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPutIntegrationCampoDesconhecido422 cobre §5: PUT com uma chave que não
// é um Campo do Descriptor → 422.
func TestPutIntegrationCampoDesconhecido422(t *testing.T) {
	h, _ := setupIntegrationsServer(t)
	valor := "x"
	rec := do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{"campo_que_nao_existe": &valor}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestGetIntegrationCampoNaoSecretoMostraValor prova que um campo
// NÃO-secreto ("modo", sem env var) mostra "valor" no JSON quando há um
// valor efetivo gravado no banco — só o secreto é mascarado.
func TestGetIntegrationCampoNaoSecretoMostraValor(t *testing.T) {
	h, _ := setupIntegrationsServer(t)
	valor := "avancado"
	do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{"modo": &valor}})

	rec := do(t, h, http.MethodGet, "/api/v1/integrations/fake", nil)
	corpo := decodeMap(t, rec.Body)
	modoCampo := campoPorNome(t, corpo, "modo")
	if modoCampo["valor"] != "avancado" {
		t.Errorf("valor do campo não-secreto = %v, quero avancado", modoCampo["valor"])
	}
}

// TestPutIntegrationParcialNaoAfetaOutrosCampos prova que o PUT é PARCIAL:
// gravar "modo" não apaga um "token" já gravado antes.
func TestPutIntegrationParcialNaoAfetaOutrosCampos(t *testing.T) {
	h, _ := setupIntegrationsServer(t)
	tokenVal := "abc"
	do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{"token": &tokenVal}})

	modoVal := "avancado"
	rec := do(t, h, http.MethodPut, "/api/v1/integrations/fake",
		map[string]any{"valores": map[string]*string{"modo": &modoVal}})
	corpo := decodeMap(t, rec.Body)
	tokenCampo := campoPorNome(t, corpo, "token")
	if tokenCampo["origem"] != "banco" || tokenCampo["definido"] != true {
		t.Errorf("token deveria continuar gravado após PUT parcial de modo: %v", tokenCampo)
	}
}
