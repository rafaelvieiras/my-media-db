package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Tests of the PER-LANGUAGE OVERRIDE EDITOR. No network: the setup uses only a migrated
// database (no provider) — the external base stays empty, so the override composes over the
// FILE base. The settings fall back to the safe default (pt-BR primary, en fallback, [pt-BR,
// en] allowed), enabling the language mode in the resolver.

// patchOv sends an override PATCH with a raw JSON body (to control null/nested values).
func patchOv(t *testing.T, h http.Handler, id, lang, corpo string) *httptest.ResponseRecorder {
	t.Helper()
	alvo := "/api/v1/entities/" + id + "/override"
	if lang != "" {
		alvo += "?lang=" + lang
	}
	return do(t, h, http.MethodPatch, alvo, []byte(corpo))
}

// getBlob reads the RAW override blob as a field→raw map.
func getBlob(t *testing.T, h http.Handler, id string) map[string]json.RawMessage {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/override", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET override status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decodificar blob: %v (corpo=%s)", err, rec.Body.String())
	}
	return m
}

// Criterion §5: "Editing the title in pt-BR writes override.title['pt-BR'] and the resolver
// starts serving it only in pt-BR; reading in en is not affected."
func TestPatchTituloPorIdiomaIndependente(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base Title")

	rec := patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ptResp := decodeEntidade(t, rec)
	eq(t, "resposta.title", ptResp.Title, "Duna")
	eq(t, "resposta.lang", ptResp.Lang, "pt-BR")

	// The written blob is the per-language map.
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title", string(blob["title"]), `{"pt-BR":"Duna"}`)

	// pt-BR serves the override; en falls back to the FILE base (not affected).
	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title", pt.Title, "Duna")
	eq(t, "pt.resolved.title", pt.ResolvedLanguages["title"], "pt-BR")

	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	eq(t, "en.title", en.Title, "Base Title")
	eq(t, "en.resolved.title", en.ResolvedLanguages["title"], "")
}

// Criterion §5: "A 2-level merge preserves the other languages of the same field."
func TestPatchMergeDoisNiveisPreservaIdiomas(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	if rec := patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH pt status = %d", rec.Code)
	}
	if rec := patchOv(t, h, "e1", "en", `{"title":"Dune"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH en status = %d", rec.Code)
	}

	blob := getBlob(t, h, "e1")
	// json.Marshal sorts the keys; en before pt-BR.
	eq(t, "blob.title", string(blob["title"]), `{"en":"Dune","pt-BR":"Duna"}`)

	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title", pt.Title, "Duna")
	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	eq(t, "en.title", en.Title, "Dune")
}

// Criterion §5: "Clearing the override (DELETE …/{campo}?lang=) ≠ setting it empty (null in
// the language)." Clearing inherits the base again; null erases the base and interrupts the
// fallback.
func TestLimparDiferenteDeDefinirVazio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base Title")

	// SET EMPTY: null in the language. The pt-BR title becomes empty and interrupts the
	// fallback.
	if rec := patchOv(t, h, "e1", "pt-BR", `{"title":null}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH null status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title(null)", string(blob["title"]), `{"pt-BR":null}`)
	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title(vazio)", pt.Title, "") // erased, did NOT fall back to the base
	eq(t, "pt.resolved.title(vazio)", pt.ResolvedLanguages["title"], "pt-BR")

	// CLEAR: DELETE of the field in that language. It goes back to INHERITING the FILE base.
	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE campo status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	// The field map emptied → the field key is gone; the whole blob emptied → the row is
	// removed.
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob após limpar = %v, quero {}", b)
	}
	pt2 := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title(herdado)", pt2.Title, "Base Title")
	eq(t, "pt.resolved.title(herdado)", pt2.ResolvedLanguages["title"], "")
}

// Criterion §5: "tags are edited through {add, remove} inside the language."
func TestPatchTagsPorIdioma(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// FILE base with one tag ("Guerra") for the remove to bite on.
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: "e1", Type: "movie", Title: "Base", TagsJson: nsOf(`["Guerra","Drama"]`),
	}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	if rec := patchOv(t, h, "e1", "pt-BR", `{"tags":{"add":["Classico"],"remove":["Guerra"]}}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH tags status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.tags", string(blob["tags"]), `{"pt-BR":{"add":["Classico"],"remove":["Guerra"]}}`)

	// pt-BR: FILE base [Guerra, Drama] - Guerra + Classico = [Drama, Classico].
	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	if len(pt.Tags) != 2 || pt.Tags[0] != "Drama" || pt.Tags[1] != "Classico" {
		t.Errorf("pt.tags = %v, quero [Drama Classico]", pt.Tags)
	}
	// en: without an override, the FILE base stays alive.
	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	if len(en.Tags) != 2 || en.Tags[0] != "Guerra" || en.Tags[1] != "Drama" {
		t.Errorf("en.tags = %v, quero [Guerra Drama]", en.Tags)
	}
}

// Criterion §5: "A neutral field writes a single scalar (no language) and applies in any
// lang."
func TestPatchCampoNeutroEscalar(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	// The lang is IGNORED for the neutral field.
	if rec := patchOv(t, h, "e1", "pt-BR", `{"original_title":"Dune"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH neutro status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.original_title", string(blob["original_title"]), `"Dune"`)

	// It applies in any language.
	for _, lang := range []string{"pt-BR", "en"} {
		e := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang="+lang, nil))
		if e.OriginalTitle == nil || *e.OriginalTitle != "Dune" {
			t.Errorf("original_title(%s) = %v, quero Dune", lang, e.OriginalTitle)
		}
	}

	// A second PATCH in another language overwrites the scalar (it does not become a map).
	if rec := patchOv(t, h, "e1", "en", `{"original_title":"Dune2"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH neutro #2 status = %d", rec.Code)
	}
	eq(t, "blob.original_title#2", string(getBlob(t, h, "e1")["original_title"]), `"Dune2"`)
}

// Criterion §5: "Migration of a legacy scalar override → {principal:…}." When merging a new
// language onto a legacy override (without a language key), it migrates to the primary
// one.
func TestPatchMigraLegadoParaPrincipal(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")
	// Seeds a LEGACY override directly (title as a scalar, without a language key).
	if _, err := escrita(t, d).UpsertMediaOverride(context.Background(), sqlcgen.UpsertMediaOverrideParams{
		EntityID: "e1", OverrideJson: nsOf(`{"title":"Legado"}`),
	}); err != nil {
		t.Fatalf("seed legado: %v", err)
	}

	// A PATCH in en must migrate the legacy value to the primary one (pt-BR) and add en.
	if rec := patchOv(t, h, "e1", "en", `{"title":"English"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH migra status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title(migrado)", string(blob["title"]), `{"en":"English","pt-BR":"Legado"}`)

	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title(migrado)", pt.Title, "Legado")
	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	eq(t, "en.title(migrado)", en.Title, "English")
}

// Criterion §5: "A malformed patch / non-editorial key / non-allowed lang are rejected
// (422/400)."
func TestPatchValidacoes(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	casos := []struct {
		nome   string
		lang   string
		corpo  string
		status int
		code   string
	}{
		{"chave nao editorial", "pt-BR", `{"status":"cataloged"}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"escalar numero", "pt-BR", `{"title":123}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"tags forma invalida", "pt-BR", `{"tags":{"foo":["x"]}}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"extension_data array", "pt-BR", `{"extension_data":[1,2]}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"nota local string", "pt-BR", `{"local_rating":"9"}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"nota local fora da faixa", "pt-BR", `{"local_rating":11}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"corpo malformado", "pt-BR", `{nao-json`, http.StatusUnprocessableEntity, "invalid_body"},
		{"lang nao permitido", "xx", `{"title":"y"}`, http.StatusBadRequest, "invalid_query"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rec := patchOv(t, h, "e1", c.lang, c.corpo)
			if rec.Code != c.status {
				t.Fatalf("status = %d, quero %d (corpo=%s)", rec.Code, c.status, rec.Body.String())
			}
			checkErrorShape(t, rec, c.code)
		})
	}
	// An invalid key among valid ones writes NOTHING (validation before the merge).
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob após rejeições = %v, quero {} (nada gravado)", b)
	}
}

// TestPatchCreditsValido covers a credits PATCH with the valid operations of the
// openapi.yaml example (OverridePatch): reorder+edit on the cast, remove+add on the crew. 200,
// it writes the `credits` key into the blob, and the `add` shows up in the resolved result —
// the field is NEUTRAL (the lang is ignored).
func TestPatchCreditsValido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	corpo := `{"credits":{
		"cast": {"reorder": ["6193"], "edit": {"6193": {"character": "Paul Atreides"}}},
		"crew": {"remove": ["1892|Producer"], "add": [
			{"source_id": "custom-1", "name": "Fulano de Tal", "department": "Direcao", "job": "Assistente de Direcao"}
		]}
	}}`
	rec := patchOv(t, h, "e1", "pt-BR", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH credits status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if _, ok := getBlob(t, h, "e1")["credits"]; !ok {
		t.Fatalf("blob deveria ter a chave credits: %v", getBlob(t, h, "e1"))
	}

	// With no provider/external base in this test, only the crew `add` shows up; the field is
	// NEUTRAL (it applies in any lang, without consulting the language chain).
	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	if len(en.Crew) != 1 || en.Crew[0].SourceID != "custom-1" || en.Crew[0].Job != "Assistente de Direcao" {
		t.Fatalf("crew = %+v, quero 1 credito custom-1 (o add, ja que a base externa esta vazia)", en.Crew)
	}
}

// TestPatchCreditsFormaInvalida covers the REJECTED shapes (422) of the credits patch: an
// operation with the wrong type (reorder/edit), add without the required fields, an unknown
// sub-block kind, and credits/sub-block that is not an object.
func TestPatchCreditsFormaInvalida(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	casos := []struct {
		nome  string
		corpo string
	}{
		{"reorder nao-array", `{"credits":{"cast":{"reorder":"6193"}}}`},
		{"edit nao-objeto", `{"credits":{"cast":{"edit":["x"]}}}`},
		{"add sem source_id", `{"credits":{"crew":{"add":[{"name":"Fulano"}]}}}`},
		{"add sem name", `{"credits":{"crew":{"add":[{"source_id":"c1"}]}}}`},
		{"kind de sub-bloco desconhecido", `{"credits":{"director":{"remove":["1"]}}}`},
		{"credits nao-objeto", `{"credits":"foo"}`},
		{"sub-bloco nao-objeto", `{"credits":{"cast":"foo"}}`},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rec := patchOv(t, h, "e1", "pt-BR", c.corpo)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
			}
			checkErrorShape(t, rec, "invalid_body")
		})
	}
	// None of the rejections writes anything (validation before the merge).
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob após rejeições = %v, quero {} (nada gravado)", b)
	}
}

// TestPatchNotaLocalDefinirELimpar covers the owner's LOCAL RATING (ADR-0012): setting the
// rating writes the neutral number (no language) and the resolved entity exposes it in
// local_rating; CLEARING (DELETE without lang) removes the key and the rating goes back to null
// (non-destructive).
func TestPatchNotaLocalDefinirELimpar(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	// Sets the rating (accepts 1 decimal place; the lang is IGNORED — a neutral field).
	rec := patchOv(t, h, "e1", "pt-BR", `{"local_rating":8.5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH nota status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeEntidade(t, rec)
	if got.LocalRating == nil || *got.LocalRating != 8.5 {
		t.Fatalf("local_rating = %v, quero 8.5", got.LocalRating)
	}
	// Stored as a direct numeric scalar (no per-language map).
	eq(t, "blob.local_rating", string(getBlob(t, h, "e1")["local_rating"]), `8.5`)

	// CLEAR (DELETE without lang): removes the key; the rating goes back to null.
	del := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/local_rating", nil)
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE nota status = %d, quero 200 (corpo=%s)", del.Code, del.Body.String())
	}
	limpa := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	if limpa.LocalRating != nil {
		t.Errorf("local_rating apos limpar = %v, quero null", limpa.LocalRating)
	}
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob apos limpar = %v, quero {}", b)
	}
}

// Criterion §5: "GET returns the raw blob." And {} when there is no override.
func TestGetBlobCru(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	// No override → {}.
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob inicial = %v, quero {}", b)
	}
	// After patches on distinct fields → the raw blob with both keys.
	_ = patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	_ = patchOv(t, h, "e1", "pt-BR", `{"original_title":"Dune"}`)
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title", string(blob["title"]), `{"pt-BR":"Duna"}`)
	eq(t, "blob.original_title", string(blob["original_title"]), `"Dune"`)
}

// Criterion §5 / §4: "DELETE reset erases the whole override."
func TestDeleteReset(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base Title")
	_ = patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	_ = patchOv(t, h, "e1", "en", `{"title":"Dune"}`)

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE reset status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob após reset = %v, quero {}", b)
	}
	// It resolves back from the base.
	e := decodeEntidade(t, rec)
	eq(t, "reset.title", e.Title, "Base Title")
}

// DELETE of a translatable field WITHOUT lang removes the whole field key (all languages).
func TestDeleteCampoSemLangRemoveTudo(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base Title")
	_ = patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	_ = patchOv(t, h, "e1", "en", `{"title":"Dune"}`)

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE sem lang status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob = %v, quero {} (campo inteiro removido)", b)
	}
}

// DELETE of a specific language preserves the other languages of the field.
func TestDeleteCampoPorIdiomaPreservaOutros(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")
	_ = patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	_ = patchOv(t, h, "e1", "en", `{"title":"Dune"}`)

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title?lang=en", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE en status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title(sem en)", string(blob["title"]), `{"pt-BR":"Duna"}`)
}

// DELETE of an absent key is idempotent (200), and a non-editorial {campo} → 422.
func TestDeleteCampoIdempotenteEValidacao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	// Absent: idempotent.
	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE ausente status = %d, quero 200", rec.Code)
	}
	// Non-editorial field → 422.
	rec = do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/status", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("DELETE status(nao editorial) = %d, quero 422", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_body")
	// lang present but outside the allowed ones → 400.
	rec = do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title?lang=xx", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE lang invalido = %d, quero 400", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_query")
}

// Unknown entity → 404 on the override PATCH and GET.
func TestOverrideEntityInexistente404(t *testing.T) {
	h, _ := setupEntitiesServer(t)

	rec := patchOv(t, h, "naoexiste", "pt-BR", `{"title":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH inexistente = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")

	rec = do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/override", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET inexistente = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}
