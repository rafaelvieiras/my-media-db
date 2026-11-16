package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// This file implements the PER-LANGUAGE override EDITOR: the surface through which
// the owner WRITES the sparse patch that wins over everything.
//
//   PATCH  /entities/{id}/override?lang=<code>        — sparse merge of 2 levels
//   DELETE /entities/{id}/override/{campo}?lang=<code> — clear the override (per language)
//   DELETE /entities/{id}/override                     — reset (deletes the override)
//   GET    /entities/{id}/override                     — returns the RAW blob
//
// The endpoint does NOT invent semantics: it only merges the patch into the blob
// (preserving the raw RawMessage) and returns the RESOLVED entity — the resolver
// interprets the values at READ time. Which fields are editable/translatable comes from
// the resolver (resolver.CamposEditaveis — R1, single source). The writes go through the
// WRITE pool (ADR-0010).

// patchOverride handles PATCH /api/v1/entities/{id}/override?lang=<code>: it merges the
// body keys into override_json. Translatable → override[campo][lang] (deep-merge
// preserving the other languages; migrates a legacy scalar → {primary: legacy});
// neutral → override[campo] scalar (lang ignored). `null` in the language is a valid
// value (define empty). 404 entity does not exist; 400 lang outside the allowed ones; 422
// malformed patch (non-editorial key or invalid shape). 200 with the entity resolved in
// the lang.
func (s *Server) patchOverride(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	cfg, lang, ok := s.cfgELang(w, ctx, r.URL.Query().Get("lang"))
	if !ok {
		return
	}

	var patch map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "patch de override inválido")
		return
	}

	campos := resolver.CamposEditaveis()
	if !validarPatch(w, campos, patch) {
		return
	}

	blob, err := s.carregarBlobBruto(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}

	for campo, raw := range patch {
		novo, err := mesclarCampo(campos[campo], blob, campo, raw, lang, cfg.Primary)
		if err != nil {
			respondErroInterno(w)
			return
		}
		blob[campo] = novo
	}

	if err := s.gravarBlob(ctx, id, blob); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderOverride(w, ctx, id, lang)
}

// deleteOverrideCampo handles DELETE /api/v1/entities/{id}/override/{campo}?lang=:
// CLEAR override. Translatable WITH lang removes only override[campo][lang] (and the
// campo key if the map empties); translatable WITHOUT lang or a NEUTRAL field removes the
// whole campo key. Removing an absent key is idempotent (200). 422 if {campo} is not
// editorial; 400 if lang present outside the allowed ones; 404 entity does not exist. If the
// blob empties, the override row is removed.
func (s *Server) deleteOverrideCampo(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	campo := chi.URLParam(r, "campo")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	info, editorial := resolver.CamposEditaveis()[campo]
	if !editorial {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"campo nao editorial: "+campo)
		return
	}

	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	// The PRESENCE of ?lang= decides the scope (per language × whole field), so we read
	// the raw parameter; if present, we validate it against the allowed ones (400 when
	// outside).
	langRaw := r.URL.Query().Get("lang")
	if !s.validarLangOpcional(w, cfg, langRaw) {
		return
	}

	blob, err := s.carregarBlobBruto(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}

	if err := removerCampo(info, blob, campo, langRaw, cfg.Primary); err != nil {
		respondErroInterno(w)
		return
	}

	if err := s.gravarBlob(ctx, id, blob); err != nil {
		respondErroInterno(w)
		return
	}

	s.responderOverride(w, ctx, id, ouPrincipal(langRaw, cfg.Primary))
}

// deleteOverride handles DELETE /api/v1/entities/{id}/override: RESET — deletes the
// whole override. 404 entity does not exist; 200 with the resolved entity (at the base).
func (s *Server) deleteOverride(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	_, lang, ok := s.cfgELang(w, ctx, r.URL.Query().Get("lang"))
	if !ok {
		return
	}

	if err := sqlcgen.New(s.db.Write).ApagarMediaOverride(ctx, id); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderOverride(w, ctx, id, lang)
}

// getOverride handles GET /api/v1/entities/{id}/override: it returns the RAW BLOB stored
// ({} if none), so the UI can tell "overridden" from "inherited" per field and per
// language. 200 (entity exists); 404 if the entity does not exist.
func (s *Server) getOverride(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	ov, err := s.leitura().ObterMediaOverride(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, json.RawMessage("{}"))
			return
		}
		respondErroInterno(w)
		return
	}
	if !ov.OverrideJson.Valid || ov.OverrideJson.String == "" {
		writeJSON(w, http.StatusOK, json.RawMessage("{}"))
		return
	}
	// RAW blob, just as stored — without reinterpreting (the resolver interprets it when
	// reading the entity).
	writeJSON(w, http.StatusOK, json.RawMessage(ov.OverrideJson.String))
}

// ── Override editor helpers ──────────────────────────────────────────────────

// exigirEntity loads the entity (read pool) requiring that it EXISTS: 404 in the
// standard envelope when absent, 500 on a real database error. Common guard of the
// override handlers (the override row has an FK to entity — writing to a
// nonexistent entity would violate integrity).
func (s *Server) exigirEntity(w http.ResponseWriter, ctx context.Context, id string) (sqlcgen.Entity, bool) {
	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return sqlcgen.Entity{}, false
		}
		respondErroInterno(w)
		return sqlcgen.Entity{}, false
	}
	return ent, true
}

// cfgELang loads the settings and resolves the ?lang= with the default semantics (absent
// = primary; outside the allowed ones → 400). It already writes the error (500/400) and
// returns ok=false when it fails. Common guard of PATCH/DELETE-reset.
func (s *Server) cfgELang(w http.ResponseWriter, ctx context.Context, langParam string) (languageSettings, string, bool) {
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return languageSettings{}, "", false
	}
	lang, ok := s.parseLang(w, cfg, langParam)
	if !ok {
		return languageSettings{}, "", false
	}
	return cfg, lang, true
}

// validarLangOpcional validates a ?lang= whose PRESENCE is meaningful (DELETE of a
// field): absent = ok (whole-field scope); present validates against the allowed ones
// (400 when outside, already written).
func (s *Server) validarLangOpcional(w http.ResponseWriter, cfg languageSettings, langRaw string) bool {
	if langRaw == "" {
		return true
	}
	_, ok := s.parseLang(w, cfg, langRaw)
	return ok
}

// ouPrincipal returns langRaw when non-empty, otherwise the primary — the language in which
// the resolved response is served when the DELETE was not scoped by language.
func ouPrincipal(langRaw, principal string) string {
	if langRaw == "" {
		return principal
	}
	return langRaw
}

// carregarBlobBruto reads the current override_json as the sparse RAW map (always
// non-nil, mutable) that the editor merge manipulates. It reuses carregarOverride
// (absent/malformed → empty map).
func (s *Server) carregarBlobBruto(ctx context.Context, id string) (map[string]json.RawMessage, error) {
	m, err := s.carregarOverride(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return map[string]json.RawMessage{}, nil
	}
	return m, nil
}

// gravarBlob persists the override blob through the WRITE pool. An empty blob ({}) ⇒
// it removes the row (ApagarMediaOverride) instead of storing an innocuous patch — the
// resolver goes back to inheriting the base.
func (s *Server) gravarBlob(ctx context.Context, id string, blob map[string]json.RawMessage) error {
	esc := sqlcgen.New(s.db.Write)
	if len(blob) == 0 {
		return esc.ApagarMediaOverride(ctx, id)
	}
	b, err := json.Marshal(blob)
	if err != nil {
		return err
	}
	_, err = esc.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID:     id,
		OverrideJson: sql.NullString{String: string(b), Valid: true},
	})
	return err
}

// mesclarCampo returns the new RAW value of a field after the patch merge: for a
// TRANSLATABLE one, it merges `raw` into `blob[campo][lang]` (deep-merge of 2 levels, migrating a
// legacy scalar → {primary:…}); for a NEUTRAL one, it returns `raw` directly (lang
// ignored). It preserves the raw RawMessages — it does not reinterpret the values.
func mesclarCampo(info resolver.CampoInfo, blob map[string]json.RawMessage, campo string, raw json.RawMessage, lang, principal string) (json.RawMessage, error) {
	if !info.Traduzivel {
		return raw, nil
	}
	mapa := map[string]json.RawMessage{}
	if existente, ok := blob[campo]; ok {
		mapa = resolver.MapaIdiomaDoOverride(campo, existente, principal)
	}
	mapa[lang] = raw
	return json.Marshal(mapa)
}

// removerCampo applies the CLEAR removal to the blob (in place): translatable WITH
// lang removes only `blob[campo][langRaw]` (and the campo key if the map empties);
// translatable WITHOUT lang or a NEUTRAL field removes the whole campo key. Removing what
// does not exist is a no-op (idempotent).
func removerCampo(info resolver.CampoInfo, blob map[string]json.RawMessage, campo, langRaw, principal string) error {
	if !info.Traduzivel || langRaw == "" {
		delete(blob, campo)
		return nil
	}
	existente, ok := blob[campo]
	if !ok {
		return nil
	}
	mapa := resolver.MapaIdiomaDoOverride(campo, existente, principal)
	delete(mapa, langRaw)
	if len(mapa) == 0 {
		delete(blob, campo)
		return nil
	}
	reser, err := json.Marshal(mapa)
	if err != nil {
		return err
	}
	blob[campo] = reser
	return nil
}

// responderOverride resolves the entity in the given lang and responds 200 (the UI
// reflects the resolved value right away). 404 if the entity vanished in the middle; 500 on
// a database error.
func (s *Server) responderOverride(w http.ResponseWriter, ctx context.Context, id, lang string) {
	resolvida, encontrada, err := s.resolverEntity(ctx, id, lang, true)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !encontrada {
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, resolvida)
}

// validarPatch rejects (422) any non-editorial key or one with an invalid shape
// for the field kind. It returns false having already written the error.
func validarPatch(w http.ResponseWriter, campos map[string]resolver.CampoInfo, patch map[string]json.RawMessage) bool {
	for campo, raw := range patch {
		info, editorial := campos[campo]
		if !editorial {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body",
				"campo nao editorial: "+campo)
			return false
		}
		if !formaValida(info, raw) {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body",
				"forma invalida para o campo "+campo)
			return false
		}
	}
	return true
}

// formaValida validates the SHAPE of the patch value by the field class: scalar
// accepts string|null; list accepts an array of strings, {add?,remove?} or null;
// extension accepts an object or null.
func formaValida(info resolver.CampoInfo, raw json.RawMessage) bool {
	switch info.Kind {
	case resolver.KindLista:
		return formaLista(raw)
	case resolver.KindExtensao:
		return ehNullRaw(raw) || ehObjeto(raw)
	case resolver.KindNumero:
		return formaNumero(raw)
	case resolver.KindCreditos:
		return formaCreditos(raw)
	default: // KindEscalar
		return ehNullRaw(raw) || ehString(raw)
	}
}

// formaCreditos validates the shape of the credits patch: `null`
// (deletes everything) or an object whose keys ⊆ {cast,crew,companies}; each present
// sub-block is `null` or an object whose keys ⊆ {reorder,remove,edit,add}, with
// `reorder`/`remove` as an array of strings, `edit` as a shallow object
// string→(string→string), and `add` as an array of objects requiring `source_id`+
// `name` string (the other keys, if present, also strings). Any shape
// outside that is 422.
func formaCreditos(raw json.RawMessage) bool {
	if ehNullRaw(raw) {
		return true
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return false
	}
	for kind, rawBloco := range top {
		if kind != "cast" && kind != "crew" && kind != "companies" {
			return false
		}
		if !formaBlocoCredito(rawBloco) {
			return false
		}
	}
	return true
}

// formaBlocoCredito validates ONE sub-block (cast/crew/companies) of the credits
// patch: `null` or an object with keys ⊆ {reorder,remove,edit,add}.
func formaBlocoCredito(raw json.RawMessage) bool {
	if ehNullRaw(raw) {
		return true
	}
	var ops map[string]json.RawMessage
	if json.Unmarshal(raw, &ops) != nil {
		return false
	}
	for op, rawOp := range ops {
		switch op {
		case "reorder", "remove":
			var arr []string
			if json.Unmarshal(rawOp, &arr) != nil {
				return false
			}
		case "edit":
			if !formaEditCredito(rawOp) {
				return false
			}
		case "add":
			if !formaAddCredito(rawOp) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// formaEditCredito validates `edit`: a SHALLOW object key→(field→value), with the
// field values always strings (character/job/department/name are NEUTRAL,
// without a per-language sub-map at this stage).
func formaEditCredito(raw json.RawMessage) bool {
	var m map[string]map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	for _, campos := range m {
		for _, v := range campos {
			var s string
			if json.Unmarshal(v, &s) != nil {
				return false
			}
		}
	}
	return true
}

// formaAddCredito validates `add`: an array of objects requiring `source_id`+`name`
// as strings (chosen by the OWNER — the resolver is pure, it does not generate an id); the
// other keys, if present, must also be strings.
func formaAddCredito(raw json.RawMessage) bool {
	var arr []map[string]json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return false
	}
	for _, item := range arr {
		if !presenteComoString(item, "source_id") || !presenteComoString(item, "name") {
			return false
		}
		for _, v := range item {
			var s string
			if json.Unmarshal(v, &s) != nil {
				return false
			}
		}
	}
	return true
}

// presenteComoString says whether `chave` is present in the object and is a string.
func presenteComoString(item map[string]json.RawMessage, chave string) bool {
	raw, ok := item[chave]
	if !ok {
		return false
	}
	var s string
	return json.Unmarshal(raw, &s) == nil
}

// formaNumero accepts `null` (set/clear the rating) or a number on the 0–10 scale (the
// local rating matches the vote_average of the source). Out of range or not a number:
// invalid.
func formaNumero(raw json.RawMessage) bool {
	if ehNullRaw(raw) {
		return true
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		return false
	}
	return n >= 0 && n <= 10
}

// formaLista accepts `null`, an array of strings (replacement), or an object of
// operations `{add?,remove?}` whose values are arrays of strings.
func formaLista(raw json.RawMessage) bool {
	if ehNullRaw(raw) {
		return true
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		return true
	}
	var ops map[string]json.RawMessage
	if json.Unmarshal(raw, &ops) != nil {
		return false
	}
	for k, v := range ops {
		if k != "add" && k != "remove" {
			return false
		}
		var a []string
		if json.Unmarshal(v, &a) != nil {
			return false
		}
	}
	return true
}

// ehNullRaw says whether the raw JSON is the literal `null` (a valid value = define
// empty).
func ehNullRaw(raw json.RawMessage) bool {
	var v any
	return json.Unmarshal(raw, &v) == nil && v == nil
}

// ehString says whether the raw JSON is a string.
func ehString(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) == nil
}

// ehObjeto says whether the raw JSON is an object (not an array, not a scalar, not
// null).
func ehObjeto(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && m != nil
}
