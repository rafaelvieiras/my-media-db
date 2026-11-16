package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements the CATALOG COMPLETENESS config in app_settings:
// GET/PUT /api/v1/settings/catalog-completeness. It mirrors exactly the structure of
// settings.go (language config): a light in-memory cache (mutex + `carregado` flag), a
// non-destructive GET (factory default when the key does not exist, writing nothing)
// and a PUT as TOTAL replacement (not a partial PATCH).
//
// Critical distinction: this is "CATALOG completeness" (filling the metadata/artwork
// of ONE item) — DIFFERENT from "OWNERSHIP completeness" (how many
// episodes/seasons the owner HAS on disk, `web/src/lib/entity-badges.ts`). Do not
// confuse the two concepts.

// appSettingCatalogCompleteness is the key of the catalog completeness config in
// app_settings.
const appSettingCatalogCompleteness = "catalog_completeness"

// tipoCompletenessConfig is the set of desirable fields/artwork for ONE entity.type.
// All the fields are required in the PUT — the form always sends the complete object
// (total replacement).
type tipoCompletenessConfig struct {
	Description   bool     `json:"description"`
	Runtime       bool     `json:"runtime"`
	Certification bool     `json:"certification"`
	Keywords      bool     `json:"keywords"`
	Cast          bool     `json:"cast"`
	MediaFile     bool     `json:"media_file"`
	Artwork       []string `json:"artwork"`
}

// catalogCompletenessSettings is the canonical config, top-level key = entity.type
// (OPEN — any type string is accepted, not only movie/tv; entity.type is free TEXT). A
// type missing from the map means "nothing required" for that type. Serialized as the
// value_json of the `catalog_completeness` key; it is also the PUT body and the GET
// response.
type catalogCompletenessSettings map[string]tipoCompletenessConfig

// defaultCatalogCompletenessSettings is the deliberately minimal FACTORY DEFAULT:
// synopsis + media file + poster flagged for `movie` and `tv`, the rest unchecked. An
// "everything checked" default would retroactively punish collections already
// cataloged without the owner asking for anything.
func defaultCatalogCompletenessSettings() catalogCompletenessSettings {
	minimo := tipoCompletenessConfig{
		Description: true,
		MediaFile:   true,
		Artwork:     []string{kindPoster},
	}
	return catalogCompletenessSettings{
		"movie": minimo,
		"tv":    minimo,
	}
}

// catalogCompletenessCache keeps the already decoded config in memory, protected by a
// mutex — the same pattern as settingsCache. `carregado` distinguishes "not read yet"
// from "read" (the value may be the default). Invalidated on the PUT.
type catalogCompletenessCache struct {
	mu        sync.Mutex
	carregado bool
	cfg       catalogCompletenessSettings
}

// obterCatalogCompletenessSettings returns the catalog completeness config, from the
// cache when already loaded, otherwise reading app_settings through the READ
// connection. Missing row or invalid JSON ⇒ factory default (never an error for that).
// Only a real database error is propagated. Exportable from the server package so other
// handlers/packages can consume it without an I/O hit per request.
func (s *Server) obterCatalogCompletenessSettings(ctx context.Context) (catalogCompletenessSettings, error) {
	s.catalogCompleteness.mu.Lock()
	defer s.catalogCompleteness.mu.Unlock()
	if s.catalogCompleteness.carregado {
		return s.catalogCompleteness.cfg, nil
	}
	row, err := s.leitura().ObterAppSetting(ctx, appSettingCatalogCompleteness)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.catalogCompleteness.cfg = defaultCatalogCompletenessSettings()
			s.catalogCompleteness.carregado = true
			return s.catalogCompleteness.cfg, nil
		}
		return nil, err
	}
	cfg := defaultCatalogCompletenessSettings()
	if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed catalogCompletenessSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil && parsed.valida() == nil {
			cfg = parsed
		}
	}
	s.catalogCompleteness.cfg = cfg
	s.catalogCompleteness.carregado = true
	return cfg, nil
}

// invalidarCatalogCompleteness zeroes the cache (called after the PUT writes).
func (s *Server) invalidarCatalogCompleteness() {
	s.catalogCompleteness.mu.Lock()
	s.catalogCompleteness.carregado = false
	s.catalogCompleteness.mu.Unlock()
}

// valida tells whether the config is internally consistent: every type key is
// non-empty and every artwork item belongs to the kindsValidos registry
// (internal/server/assets.go). Types with an empty object ({}) are accepted (they amount
// to "nothing required"). Returns nil when valid, or a descriptive error (for the 422
// message) otherwise.
func (c catalogCompletenessSettings) valida() error {
	for tipo, cfg := range c {
		if tipo == "" {
			return errors.New("chave de tipo vazia não é permitida")
		}
		for _, kind := range cfg.Artwork {
			if !kindsValidos[kind] {
				return errors.New("kind de arte desconhecido \"" + kind +
					"\" (use " + kindsValidosListaOrdenada() + ")")
			}
		}
	}
	return nil
}

// kindsValidosListaOrdenada returns the kinds of the kindsValidos registry
// (internal/server/assets.go) in a sorted, comma-separated list, to compose the
// validation error message.
func kindsValidosListaOrdenada() string {
	kinds := make([]string, 0, len(kindsValidos))
	for k := range kindsValidos {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return strings.Join(kinds, ", ")
}

// getCatalogCompletenessSettings handles GET /api/v1/settings/catalog-completeness:
// returns the current config (or the factory default when absent, writing nothing).
func (s *Server) getCatalogCompletenessSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cfg, err := s.obterCatalogCompletenessSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putCatalogCompletenessSettings handles PUT /api/v1/settings/catalog-completeness:
// validates (non-empty type keys, artwork ⊆ kindsValidos) and upserts, invalidating the
// cache. TOTAL replacement (not a partial PATCH — the same pattern as the languages
// PUT). Malformed body ⇒ 400; invalid ⇒ 422; success ⇒ 200 with the written config
// (echo).
func (s *Server) putCatalogCompletenessSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body catalogCompletenessSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if err := body.valida(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"config de completude de catalogação inválida: "+err.Error())
		return
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingCatalogCompleteness,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarCatalogCompleteness()
	writeJSON(w, http.StatusOK, body)
}
