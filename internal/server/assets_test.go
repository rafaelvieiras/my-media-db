package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Testes da ARTE CUSTOM (briefing 0017): upload origin='user', validacao de mime
// (magic bytes) e tamanho, precedencia de servico (helper puro + endpoint de
// poster), preferencia (uma por kind), remocao (recusa external), regressao
// nao-destrutiva do re-enrich e a guarda de path-traversal.

// setupAssetsServer sobe um Server com banco migrado e um assetsDir temporario
// (WithAssetsDir), devolvendo tambem o dir para as assercoes de disco.
func setupAssetsServer(t *testing.T) (http.Handler, *db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	h, d := setupSearchServer(t, WithAssetsDir(dir))
	return h, d, dir
}

// pngSig e a assinatura magic-byte de um PNG; jpgSig de um JPEG; webpBytes monta um
// contêiner RIFF/WEBP minimo — o que http.DetectContentType fareja.
var (
	pngSig = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	jpgSig = []byte{0xff, 0xd8, 0xff, 0xe0}
)

// pngPayload devolve n bytes comecando pela assinatura PNG (magic bytes validos).
func pngPayload(n int) []byte { return pngPayloadMark(n, 0) }

// pngPayloadMark e um PNG valido com um byte marcador (indice 10) para diferenciar
// conteudos servidos sem quebrar a assinatura.
func pngPayloadMark(n int, marker byte) []byte {
	b := make([]byte, n)
	copy(b, pngSig)
	if n > 10 {
		b[10] = marker
	}
	return b
}

// jpgPayload devolve um JPEG valido (assinatura magic-byte).
func jpgPayload(n int) []byte {
	b := make([]byte, n)
	copy(b, jpgSig)
	return b
}

// webpPayload monta "RIFF????WEBPVP..." — o que o sniff de http reconhece.
func webpPayload(n int) []byte {
	b := make([]byte, n)
	copy(b, []byte("RIFF"))
	copy(b[8:], []byte("WEBPVP"))
	return b
}

// postAsset envia um multipart/form-data (campo `file` + `kind` opcional) para o
// upload e devolve o recorder.
func postAsset(t *testing.T, h http.Handler, entityID, kind string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if kind != "" {
		if err := mw.WriteField("kind", kind); err != nil {
			t.Fatalf("WriteField kind: %v", err)
		}
	}
	fw, err := mw.CreateFormFile("file", "art.bin")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("escrever conteudo: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart Close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entities/"+entityID+"/assets", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// postSemArquivo envia um multipart SEM o campo `file` (so `kind`) — corpo malformado.
func postSemArquivo(t *testing.T, h http.Handler, entityID string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("kind", "poster")
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entities/"+entityID+"/assets", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeAsset(t *testing.T, rec *httptest.ResponseRecorder) assetDTO {
	t.Helper()
	var a assetDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode assetDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return a
}

func decodeAssets(t *testing.T, rec *httptest.ResponseRecorder) []assetDTO {
	t.Helper()
	var a []assetDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode []assetDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return a
}

// acharAsset devolve o assetDTO de id na lista (t.Fatalf se ausente).
func acharAsset(t *testing.T, lista []assetDTO, id string) assetDTO {
	t.Helper()
	for _, a := range lista {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("asset %q ausente na lista", id)
	return assetDTO{}
}

// ── Upload: cria asset user + valida mime e tamanho (criterio 5.1) ───────────

// TestUploadCriaAssetUserComBinario cobre o criterio 5.1: upload cria asset
// origin=user com o binario em disco (layout {assetsDir}/{entityID}/user-{id}.ext).
func TestUploadCriaAssetUserComBinario(t *testing.T) {
	h, d, dir := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	rec := postAsset(t, h, "e1", "poster", pngPayload(64))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	a := decodeAsset(t, rec)
	eq(t, "origin", a.Origin, "user")
	eq(t, "kind", a.Kind, "poster")
	eq(t, "preferred", a.Preferred, false)
	eq(t, "url", a.URL, "/api/v1/entities/e1/assets/"+a.ID+"/file")

	// O binario mora sob {assetsDir}/e1/user-{id}.png.
	bin := filepath.Join(dir, "e1", "user-"+a.ID+".png")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("binario ausente em %s: %v", bin, err)
	}
	// Linha origin='user' persistida.
	got, err := escrita(t, d).ObterAsset(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("ObterAsset: %v", err)
	}
	eq(t, "db.origin", got.Origin, "user")
}

// TestUploadDetectaMimePorMagicBytes cobre o criterio 5.1: mime validado pelos
// magic bytes — jpeg/png/webp aceitos (201), text/plain rejeitado (415), mesmo com
// o multipart declarando outra coisa.
func TestUploadDetectaMimePorMagicBytes(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	casos := []struct {
		nome    string
		content []byte
		quero   int
	}{
		{"png", pngPayload(64), http.StatusCreated},
		{"jpeg", jpgPayload(64), http.StatusCreated},
		{"webp", webpPayload(64), http.StatusCreated},
		{"texto", []byte("isto e texto puro, definitivamente nao uma imagem"), http.StatusUnsupportedMediaType},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rec := postAsset(t, h, "e1", "poster", c.content)
			if rec.Code != c.quero {
				t.Fatalf("%s: status = %d, quero %d (corpo=%s)", c.nome, rec.Code, c.quero, rec.Body.String())
			}
			if c.quero == http.StatusUnsupportedMediaType {
				checkErrorShape(t, rec, "unsupported_media_type")
			}
		})
	}
}

// TestUploadRejeitaGrandeDemais cobre o criterio 5.1: tamanho validado — um arquivo
// acima de 20 MiB responde 413.
func TestUploadRejeitaGrandeDemais(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	rec := postAsset(t, h, "e1", "poster", pngPayload(tamanhoMaxAsset+1))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, quero 413 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "payload_too_large")
}

// TestUploadCorpoMalformado cobre o 422: sem o campo `file` no multipart.
func TestUploadCorpoMalformado(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	rec := postSemArquivo(t, h, "e1")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestUploadEntityInexistente404 cobre o 404 do upload.
func TestUploadEntityInexistente404(t *testing.T) {
	h, _, _ := setupAssetsServer(t)
	rec := postAsset(t, h, "naoexiste", "poster", pngPayload(64))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestUploadKindInvalido cobre o 422 de kind fora de poster|fanart.
func TestUploadKindInvalido(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	rec := postAsset(t, h, "e1", "banner", pngPayload(64))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// ── Precedencia: helper puro (criterio 5.2) ──────────────────────────────────

// mkAsset monta um sqlcgen.Asset em memoria (para o teste do helper puro).
func mkAsset(id, kind, origin string, preferred bool) sqlcgen.Asset {
	a := sqlcgen.Asset{ID: id, EntityID: "e", Kind: kind, Origin: origin}
	if preferred {
		a.MetaJson = sql.NullString{String: `{"preferred":true}`, Valid: true}
	}
	return a
}

// TestEscolherAssetServido cobre o criterio 5.2 no helper PURO: preferida vence user
// vence external vence base; sem arte cai na base ('local' do scanner); nada quando
// vazio; o kind filtra. `quer` vazio significa ok=false.
func TestEscolherAssetServido(t *testing.T) {
	casos := []struct {
		nome   string
		assets []sqlcgen.Asset
		quer   string
	}{
		{"external quando so ha external", []sqlcgen.Asset{
			mkAsset("ext", "poster", "external", false),
		}, "ext"},
		{"user vence external", []sqlcgen.Asset{
			mkAsset("ext", "poster", "external", false),
			mkAsset("usr", "poster", "user", false),
		}, "usr"},
		{"preferida vence user", []sqlcgen.Asset{
			mkAsset("ext", "poster", "external", false),
			mkAsset("u1", "poster", "user", false),
			mkAsset("u2", "poster", "user", true),
		}, "u2"},
		{"user mais recente desempata", []sqlcgen.Asset{
			mkAsset("u1", "poster", "user", false),
			mkAsset("u2", "poster", "user", false),
		}, "u2"},
		{"external vence base local", []sqlcgen.Asset{
			mkAsset("loc", "poster", "local", false),
			mkAsset("ext", "poster", "external", false),
		}, "ext"},
		{"base local quando nao ha user/external", []sqlcgen.Asset{
			mkAsset("loc", "poster", "local", false),
		}, "loc"},
		{"kind filtra", []sqlcgen.Asset{
			mkAsset("f", "fanart", "user", false),
		}, ""},
		{"nada quando vazio", nil, ""},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, ok := escolherAssetServido(c.assets, "poster")
			if c.quer == "" {
				if ok {
					t.Fatalf("ok=true, quero nenhum")
				}
				return
			}
			if !ok {
				t.Fatalf("ok=false, quero %q", c.quer)
			}
			eq(t, "asset servido", got.ID, c.quer)
		})
	}
}

// TestPosterServeArteDoDono cobre o criterio 5.2 ponta a ponta: com arte external e
// arte do dono, GET /assets/poster serve a do dono.
func TestPosterServeArteDoDono(t *testing.T) {
	h, d, dir := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	// Arte external cacheada em disco (sob o assetsDir confiavel).
	extContent := pngPayloadMark(64, 0xAA)
	extPath := filepath.Join(dir, "e1", "poster.png")
	if err := os.MkdirAll(filepath.Dir(extPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(extPath, extContent, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID: "ext1", EntityID: "e1", Kind: "poster", Origin: "external", PathOrUri: extPath,
	}); err != nil {
		t.Fatalf("CriarAsset external: %v", err)
	}

	// Arte do dono (upload).
	userContent := pngPayloadMark(64, 0xBB)
	if rec := postAsset(t, h, "e1", "poster", userContent); rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}

	poster := do(t, h, http.MethodGet, "/api/v1/entities/e1/assets/poster", nil)
	if poster.Code != http.StatusOK {
		t.Fatalf("GET poster status = %d, quero 200 (corpo=%s)", poster.Code, poster.Body.String())
	}
	if !bytes.Equal(poster.Body.Bytes(), userContent) {
		t.Errorf("poster servido nao e a arte do dono (vence a external)")
	}
}

// ── Preferencia: uma por kind, limpa as outras (criterio 5.2) ────────────────

// TestPreferLimpaOutrasDoMesmoKind cobre o criterio 5.2: marcar preferida seta a
// flag no alvo e LIMPA a dos outros do MESMO kind, sem tocar outro kind.
func TestPreferLimpaOutrasDoMesmoKind(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	a1 := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	a2 := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	af := decodeAsset(t, postAsset(t, h, "e1", "fanart", pngPayload(64)))

	// Prefere a1.
	p := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/"+a1.ID+"/prefer", nil)
	if p.Code != http.StatusOK {
		t.Fatalf("prefer a1 status = %d, quero 200 (corpo=%s)", p.Code, p.Body.String())
	}
	if pa := decodeAsset(t, p); !pa.Preferred {
		t.Errorf("resposta do prefer: preferred=false, quero true")
	}
	lista := decodeAssets(t, do(t, h, http.MethodGet, "/api/v1/entities/e1/assets", nil))
	eq(t, "a1.preferred", acharAsset(t, lista, a1.ID).Preferred, true)
	eq(t, "a2.preferred", acharAsset(t, lista, a2.ID).Preferred, false)

	// Prefere a2: a1 e limpa, a2 marcada (uma por kind).
	if p2 := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/"+a2.ID+"/prefer", nil); p2.Code != http.StatusOK {
		t.Fatalf("prefer a2 status = %d, quero 200", p2.Code)
	}
	lista2 := decodeAssets(t, do(t, h, http.MethodGet, "/api/v1/entities/e1/assets", nil))
	eq(t, "a1.preferred(depois)", acharAsset(t, lista2, a1.ID).Preferred, false)
	eq(t, "a2.preferred(depois)", acharAsset(t, lista2, a2.ID).Preferred, true)

	// Preferir o fanart nao afeta a preferida de poster (uma por kind).
	if pf := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/"+af.ID+"/prefer", nil); pf.Code != http.StatusOK {
		t.Fatalf("prefer fanart status = %d, quero 200", pf.Code)
	}
	lista3 := decodeAssets(t, do(t, h, http.MethodGet, "/api/v1/entities/e1/assets", nil))
	eq(t, "af.preferred", acharAsset(t, lista3, af.ID).Preferred, true)
	eq(t, "a2.preferred(kind poster intacto)", acharAsset(t, lista3, a2.ID).Preferred, true)
}

// TestPreferAssetInexistente404 cobre o 404 do prefer.
func TestPreferAssetInexistente404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	rec := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/naoexiste/prefer", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// ── Remocao: remove user, recusa external (criterio 5.4) ─────────────────────

// TestDeleteRemoveUserRecusaExternal cobre o criterio 5.4: DELETE apaga arte do dono
// (linha + binario) e recusa apagar arte external (422).
func TestDeleteRemoveUserRecusaExternal(t *testing.T) {
	h, d, dir := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	ctx := context.Background()

	a := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	bin := filepath.Join(dir, "e1", "user-"+a.ID+".png")

	del := do(t, h, http.MethodDelete, "/api/v1/entities/e1/assets/"+a.ID, nil)
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE user status = %d, quero 200 (corpo=%s)", del.Code, del.Body.String())
	}
	if _, err := escrita(t, d).ObterAsset(ctx, a.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("linha do asset ainda existe apos DELETE: err=%v", err)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Errorf("binario ainda existe apos DELETE: %v", err)
	}

	// Arte external: DELETE recusa (422), a linha permanece.
	if _, err := escrita(t, d).CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID: "ext1", EntityID: "e1", Kind: "poster", Origin: "external", PathOrUri: filepath.Join(dir, "e1", "poster.png"),
	}); err != nil {
		t.Fatalf("CriarAsset external: %v", err)
	}
	delExt := do(t, h, http.MethodDelete, "/api/v1/entities/e1/assets/ext1", nil)
	if delExt.Code != http.StatusUnprocessableEntity {
		t.Fatalf("DELETE external status = %d, quero 422 (corpo=%s)", delExt.Code, delExt.Body.String())
	}
	checkErrorShape(t, delExt, "asset_nao_removivel")
	if _, err := escrita(t, d).ObterAsset(ctx, "ext1"); err != nil {
		t.Errorf("arte external foi apagada indevidamente: err=%v", err)
	}
}

// ── Regressao nao-destrutiva: re-enrich nao remove arte user (criterio 5.3) ──

// TestReEnrichNaoRemoveArteUser cobre o criterio 5.3: apos subir arte do dono,
// re-enriquecer (que re-cacheia a arte external por dedup de origin) NUNCA remove a
// arte origin='user', e o poster segue servindo a do dono.
func TestReEnrichNaoRemoveArteUser(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	ctx := context.Background()

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "777", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	id := decodeEntidade(t, rec).ID

	userContent := pngPayloadMark(64, 0xCC)
	ua := decodeAsset(t, postAsset(t, h, id, "poster", userContent))

	// O poster passa a servir a arte do dono (vence a external cacheada).
	p1 := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/assets/poster", nil)
	if !bytes.Equal(p1.Body.Bytes(), userContent) {
		t.Fatalf("poster nao serve a arte do dono apos upload")
	}

	// Re-enriquece: usa external_ids[tmdb]=777; re-cacheia so a external.
	if en := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/enrich", nil); en.Code != http.StatusOK {
		t.Fatalf("re-enrich status = %d, quero 200 (corpo=%s)", en.Code, en.Body.String())
	}

	// A arte do dono continua existindo e sendo servida.
	if _, err := escrita(t, d).ObterAsset(ctx, ua.ID); err != nil {
		t.Errorf("arte do dono removida pelo re-enrich: err=%v", err)
	}
	assets, err := escrita(t, d).ListarAssetsPorEntity(ctx, id)
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	var temUser bool
	for _, a := range assets {
		if a.Origin == "user" {
			temUser = true
		}
	}
	if !temUser {
		t.Errorf("nenhum asset origin=user apos re-enrich (regressao)")
	}
	p2 := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/assets/poster", nil)
	if !bytes.Equal(p2.Body.Bytes(), userContent) {
		t.Errorf("poster deixou de servir a arte do dono apos re-enrich")
	}
}

// ── Galeria + guarda de path-traversal (criterio 5.5) ────────────────────────

// TestListAssetsVazioE404 cobre a galeria: [] quando nao ha assets; 404 para entity
// inexistente.
func TestListAssetsVazioE404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/assets", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	if lista := decodeAssets(t, rec); len(lista) != 0 {
		t.Errorf("lista = %v, quero [] (sem assets)", lista)
	}
	if r404 := do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/assets", nil); r404.Code != http.StatusNotFound {
		t.Errorf("entity inexistente: status = %d, quero 404", r404.Code)
	}
}

// TestGetAssetFileForaDasRaizes403 cobre o criterio 5.5: o servico do binario mantem
// a guarda de path-traversal — um asset apontando para fora das raizes confiaveis
// responde 403.
func TestGetAssetFileForaDasRaizes403(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	if _, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID: "a1", EntityID: "e1", Kind: "poster", Origin: "user", PathOrUri: "/etc/segredo.png",
	}); err != nil {
		t.Fatalf("CriarAsset: %v", err)
	}
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/assets/a1/file", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quero 403 (path fora das raizes)", rec.Code)
	}
	checkErrorShape(t, rec, "forbidden")
}

// TestGetAssetFileServeBinario cobre o /file feliz: serve o binario do asset da
// entity; 404 quando o assetId e de outra entity.
func TestGetAssetFileServeBinario(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	semearEntity(t, d, "e2", "movie", "Y")

	content := pngPayloadMark(64, 0xDD)
	a := decodeAsset(t, postAsset(t, h, "e1", "poster", content))

	ok := do(t, h, http.MethodGet, "/api/v1/entities/e1/assets/"+a.ID+"/file", nil)
	if ok.Code != http.StatusOK {
		t.Fatalf("GET file status = %d, quero 200 (corpo=%s)", ok.Code, ok.Body.String())
	}
	if !bytes.Equal(ok.Body.Bytes(), content) {
		t.Errorf("binario servido diverge do enviado")
	}
	// assetId de e1 pedido sob e2 -> 404 (nao vaza por id cruzado).
	cross := do(t, h, http.MethodGet, "/api/v1/entities/e2/assets/"+a.ID+"/file", nil)
	if cross.Code != http.StatusNotFound {
		t.Errorf("id cruzado: status = %d, quero 404", cross.Code)
	}
}
