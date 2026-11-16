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

// Tests of the CUSTOM ARTWORK: upload origin='user', mime validation
// (magic bytes) and size, serving precedence (pure helper + poster
// endpoint), preference (one per kind), removal (refuses external), non-destructive
// regression of re-enrich and the path-traversal guard.

// setupAssetsServer starts a Server with a migrated database and a temporary assetsDir
// (WithAssetsDir), also returning the dir for the disk assertions.
func setupAssetsServer(t *testing.T) (http.Handler, *db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	h, d := setupSearchServer(t, WithAssetsDir(dir))
	return h, d, dir
}

// pngSig is the magic-byte signature of a PNG; jpgSig of a JPEG; webpBytes builds a
// minimal RIFF/WEBP container — what http.DetectContentType sniffs.
var (
	pngSig = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	jpgSig = []byte{0xff, 0xd8, 0xff, 0xe0}
)

// pngPayload returns n bytes starting with the PNG signature (valid magic bytes).
func pngPayload(n int) []byte { return pngPayloadMark(n, 0) }

// pngPayloadMark is a valid PNG with a marker byte (index 10) to differentiate
// served contents without breaking the signature.
func pngPayloadMark(n int, marker byte) []byte {
	b := make([]byte, n)
	copy(b, pngSig)
	if n > 10 {
		b[10] = marker
	}
	return b
}

// jpgPayload returns a valid JPEG (magic-byte signature).
func jpgPayload(n int) []byte {
	b := make([]byte, n)
	copy(b, jpgSig)
	return b
}

// webpPayload builds "RIFF????WEBPVP..." — what the http sniff recognizes.
func webpPayload(n int) []byte {
	b := make([]byte, n)
	copy(b, []byte("RIFF"))
	copy(b[8:], []byte("WEBPVP"))
	return b
}

// postAsset sends a multipart/form-data (field `file` + optional `kind`) to the
// upload and returns the recorder.
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

// postSemArquivo sends a multipart WITHOUT the `file` field (only `kind`) — malformed
// body.
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

// acharAsset returns the assetDTO of id in the list (t.Fatalf if absent).
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

// ── Upload: creates a user asset + validates mime and size (criterion 5.1) ───────────

// TestUploadCriaAssetUserComBinario covers the criterion 5.1: the upload creates an asset
// origin=user with the binary on disk (layout {assetsDir}/{entityID}/user-{id}.ext).
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

	// The binary lives under {assetsDir}/e1/user-{id}.png.
	bin := filepath.Join(dir, "e1", "user-"+a.ID+".png")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("binario ausente em %s: %v", bin, err)
	}
	// origin='user' row persisted.
	got, err := escrita(t, d).ObterAsset(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("ObterAsset: %v", err)
	}
	eq(t, "db.origin", got.Origin, "user")
}

// TestUploadDetectaMimePorMagicBytes covers the criterion 5.1: mime validated by the
// magic bytes — jpeg/png/webp accepted (201), text/plain rejected (415), even with
// the multipart declaring something else.
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

// TestUploadRejeitaGrandeDemais covers the criterion 5.1: size validated — a file
// above 20 MiB responds 413.
func TestUploadRejeitaGrandeDemais(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	rec := postAsset(t, h, "e1", "poster", pngPayload(tamanhoMaxAsset+1))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, quero 413 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "payload_too_large")
}

// TestUploadCorpoMalformado covers the 422: without the `file` field in the
// multipart.
func TestUploadCorpoMalformado(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	rec := postSemArquivo(t, h, "e1")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestUploadEntityInexistente404 covers the 404 of the upload.
func TestUploadEntityInexistente404(t *testing.T) {
	h, _, _ := setupAssetsServer(t)
	rec := postAsset(t, h, "naoexiste", "poster", pngPayload(64))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestUploadKindInvalido covers the 422 of a kind OUTSIDE the expanded valid set:
// banner/logo/disc/... started being accepted, so it uses a nonexistent kind ('cdart').
func TestUploadKindInvalido(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	rec := postAsset(t, h, "e1", "cdart", pngPayload(64))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestUploadAceitaKindsExpandidos: the fanart.tv kinds are accepted in the upload.
func TestUploadAceitaKindsExpandidos(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	for _, kind := range []string{"logo", "clearart", "disc", "banner", "thumb", "characterart"} {
		rec := postAsset(t, h, "e1", kind, pngPayload(64))
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload kind=%s status = %d, quero 201 (corpo=%s)", kind, rec.Code, rec.Body.String())
		}
	}
}

// ── Precedence: pure helper (criterion 5.2) ──────────────────────────────────

// mkAsset builds an in-memory sqlcgen.Asset (for the pure helper test).
func mkAsset(id, kind, origin string, preferred bool) sqlcgen.Asset {
	a := sqlcgen.Asset{ID: id, EntityID: "e", Kind: kind, Origin: origin}
	if preferred {
		a.MetaJson = sql.NullString{String: `{"preferred":true}`, Valid: true}
	}
	return a
}

// TestEscolherAssetServido covers the criterion 5.2 (plus the local-artwork decision) in the
// PURE helper: preferred beats user beats local (file found by the scanner in the folder)
// beats external beats base; with none of the four it falls back to base
// (unknown/future provenance); nothing when empty; the kind filters. An empty `quer`
// means ok=false.
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
		{"local (pasta) vence external", []sqlcgen.Asset{
			mkAsset("loc", "poster", "local", false),
			mkAsset("ext", "poster", "external", false),
		}, "loc"},
		{"user vence local", []sqlcgen.Asset{
			mkAsset("loc", "poster", "local", false),
			mkAsset("usr", "poster", "user", false),
		}, "usr"},
		{"preferida vence local", []sqlcgen.Asset{
			mkAsset("loc", "poster", "local", false),
			mkAsset("u1", "poster", "user", true),
		}, "u1"},
		{"local sozinho quando nao ha user/external", []sqlcgen.Asset{
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

// TestPosterServeArteDoDono covers the criterion 5.2 end to end: with external artwork and
// the owner's artwork, GET /assets/poster serves the owner's.
func TestPosterServeArteDoDono(t *testing.T) {
	h, d, dir := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")

	// External artwork cached on disk (under the trusted assetsDir).
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

	// Owner's artwork (upload).
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

// ── Preference: one per kind, clears the others (criterion 5.2) ──────────────

// TestPreferLimpaOutrasDoMesmoKind covers the criterion 5.2: marking as preferred sets the
// flag on the target and CLEARS it on the others of the SAME kind, without touching another
// kind. Since a later delivery, PUT /prefer also materializes/rotates physically — that is
// why the entity needs a present media_file (destination folder), otherwise 404.
func TestPreferLimpaOutrasDoMesmoKind(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	semearFile(t, d, "mf1", "e1", filepath.Join(t.TempDir(), "video.mkv"), "present", 1.0, "")

	a1 := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	a2 := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	af := decodeAsset(t, postAsset(t, h, "e1", "fanart", pngPayload(64)))

	// Prefers a1.
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

	// Prefers a2: a1 is cleared, a2 is marked (one per kind).
	if p2 := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/"+a2.ID+"/prefer", nil); p2.Code != http.StatusOK {
		t.Fatalf("prefer a2 status = %d, quero 200", p2.Code)
	}
	lista2 := decodeAssets(t, do(t, h, http.MethodGet, "/api/v1/entities/e1/assets", nil))
	eq(t, "a1.preferred(depois)", acharAsset(t, lista2, a1.ID).Preferred, false)
	eq(t, "a2.preferred(depois)", acharAsset(t, lista2, a2.ID).Preferred, true)

	// Preferring the fanart does not affect the preferred poster (one per kind).
	if pf := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/"+af.ID+"/prefer", nil); pf.Code != http.StatusOK {
		t.Fatalf("prefer fanart status = %d, quero 200", pf.Code)
	}
	lista3 := decodeAssets(t, do(t, h, http.MethodGet, "/api/v1/entities/e1/assets", nil))
	eq(t, "af.preferred", acharAsset(t, lista3, af.ID).Preferred, true)
	eq(t, "a2.preferred(kind poster intacto)", acharAsset(t, lista3, a2.ID).Preferred, true)
}

// TestPreferAssetInexistente404 covers the 404 of prefer.
func TestPreferAssetInexistente404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	rec := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/naoexiste/prefer", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// ── Removal: removes user, refuses external (criterion 5.4) ─────────────────

// TestDeleteRemoveUserRecusaExternal covers the criterion 5.4: DELETE removes the owner's
// artwork (row + binary) and refuses to delete external artwork (422).
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

	// External artwork: DELETE refuses (422), the row remains.
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

// ── Non-destructive regression: re-enrich does not remove user artwork (criterion 5.3) ──

// TestReEnrichNaoRemoveArteUser covers the criterion 5.3: after uploading the owner's
// artwork, re-enriching (which re-caches the external artwork by origin dedup) NEVER removes
// the origin='user' artwork, and the poster keeps serving the owner's.
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

	// The poster starts serving the owner's artwork (it beats the cached external one).
	p1 := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/assets/poster", nil)
	if !bytes.Equal(p1.Body.Bytes(), userContent) {
		t.Fatalf("poster nao serve a arte do dono apos upload")
	}

	// Re-enriches: uses external_ids[tmdb]=777; re-caches only the external one.
	if en := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/enrich", nil); en.Code != http.StatusOK {
		t.Fatalf("re-enrich status = %d, quero 200 (corpo=%s)", en.Code, en.Body.String())
	}

	// The owner's artwork keeps existing and being served.
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

// ── Gallery + path-traversal guard (criterion 5.5) ──────────────────────────

// TestListAssetsVazioE404 covers the gallery: [] when there is no asset; 404 for a
// nonexistent entity.
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

// TestGetAssetFileForaDasRaizes403 covers the criterion 5.5: the binary service keeps
// the path-traversal guard — an asset pointing outside the trusted roots
// responds 403.
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

// TestGetAssetFileServeBinario covers the happy /file: it serves the entity's asset
// binary; 404 when the assetId belongs to another entity.
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
	// assetId of e1 requested under e2 -> 404 (it does not leak across ids).
	cross := do(t, h, http.MethodGet, "/api/v1/entities/e2/assets/"+a.ID+"/file", nil)
	if cross.Code != http.StatusNotFound {
		t.Errorf("id cruzado: status = %d, quero 404", cross.Code)
	}
}
