package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// sourceDTO is the serialization form of a source in the API:
// { id, path, type, state, last_scan }. last_scan is null while the scanner
// has not run.
type sourceDTO struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Type string `json:"type"`
	// ContentType is the nature of the library: "movie" | "series". The scanner
	// branches the match flow by this value. Default "movie".
	ContentType string  `json:"content_type"`
	State       string  `json:"state"`
	LastScan    *string `json:"last_scan"`
	// WatchEnabled turns the fsnotify watcher of this source on/off.
	// Default false; editable only via PATCH /api/v1/library/{id}.
	WatchEnabled bool `json:"watch_enabled"`
	// PreferredLanguage is the preferred search/display language of this
	// library; nil = no preference, it uses the global language from
	// app_settings.
	PreferredLanguage *string `json:"preferred_language"`
	// RetroactiveEnrichmentDays is the OPTIONAL override of the retry interval
	// of this library's retroactive enrichment; nil = no override, it uses the
	// global value from app_settings.enrichment_retroativo.
	RetroactiveEnrichmentDays *int `json:"retroactive_enrichment_days"`
}

// patchSourceRequest is the body accepted by PATCH /api/v1/library/{id}
// (PatchSource in the contract): a PARTIAL PATCH -- an absent field stays unchanged.
// Path and watch_enabled may come together or each on its own; when both come in the
// same body, the path is applied FIRST (cascading re-root) and watch_enabled
// afterwards. PreferredLanguage uses jsonNullable (a VALUE field, not a pointer --
// see the type) to distinguish the three forms: a field ABSENT from the JSON leaves it
// unchanged; an explicit `null` clears the preference; a string defines it.
type patchSourceRequest struct {
	WatchEnabled              *bool           `json:"watch_enabled"`
	Path                      *string         `json:"path"`
	PreferredLanguage         jsonNullable    `json:"preferred_language"`
	RetroactiveEnrichmentDays jsonNullableInt `json:"retroactive_enrichment_days"`
}

// jsonNullable distinguishes the THREE forms of an optional-and-nullable JSON field
// (preferred_language of the patch): absent / explicit `null` / value. THE STRUCT FIELD
// MUST BE A VALUE (not `*jsonNullable`): the encoding/json package, upon finding the
// literal `null`, zeroes a field whose DECLARED TYPE is already a pointer directly --
// without even calling UnmarshalJSON (see encoding/json/decode.go, indirect(): the
// early-break `decodingNull && v.CanSet()` fires before checking the Unmarshaler
// interface when the field itself is already a pointer). With a VALUE field, the decoder
// takes its ADDRESS to satisfy Unmarshaler -- on that path CanSet() of the temporary
// address is false, the early-break does not fire, and UnmarshalJSON IS called even for
// `null`. A field absent from the JSON never invokes UnmarshalJSON (the decoder does not
// touch the key) -- which is why Present is born false (zero value) and only becomes
// true inside the method.
type jsonNullable struct {
	Present bool
	Value   *string
}

func (n *jsonNullable) UnmarshalJSON(data []byte) error {
	n.Present = true
	if string(data) == "null" {
		n.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	n.Value = &s
	return nil
}

// jsonNullableInt is the equivalent of jsonNullable for an optional-and-nullable INTEGER
// field (retroactive_enrichment_days of the patch): absent / explicit `null` / value.
// SAME technique (a value field, not a pointer) and SAME reason -- see the comment of
// jsonNullable above.
type jsonNullableInt struct {
	Present bool
	Value   *int
}

func (n *jsonNullableInt) UnmarshalJSON(data []byte) error {
	n.Present = true
	if string(data) == "null" {
		n.Value = nil
		return nil
	}
	var v int
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	n.Value = &v
	return nil
}

// listDTO is the response of GET /api/v1/library.
type listDTO struct {
	Items []sourceDTO `json:"items"`
}

// createRequest is the body accepted by POST /api/v1/library.
type createRequest struct {
	Path string `json:"path"`
	Type string `json:"type"`
	// ContentType is optional: "movie" | "series". Omitted => "movie" (compat).
	ContentType string `json:"content_type"`
	// PreferredLanguage is optional: preferred search/display language
	// of this library. Omitted/empty => no preference (uses the global language).
	PreferredLanguage string `json:"preferred_language"`
	// RetroactiveEnrichmentDays is optional: override of the retry interval of
	// this library's retroactive enrichment. Omitted/nil
	// => no override (uses the global value). A pointer because 0 is not a valid
	// value (the minimum interval is 1) -- distinguishing "absent" from "0" avoids
	// silently accepting an invalid value as if it were "no override".
	RetroactiveEnrichmentDays *int `json:"retroactive_enrichment_days"`
}

// browseEntryDTO is the serialization form of a folder browser entry
// (BrowseEntry schema of openapi.yaml).
type browseEntryDTO struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Readable bool   `json:"readable"`
}

// browseResultDTO is the response of GET /api/v1/library/browse (schema
// BrowseResult of openapi.yaml).
type browseResultDTO struct {
	Path      string           `json:"path"`
	Parent    *string          `json:"parent"`
	Entries   []browseEntryDTO `json:"entries"`
	Truncated bool             `json:"truncated"`
}

// toBrowseDTO converts the domain BrowseResult into the serializable form.
func toBrowseDTO(r library.BrowseResult) browseResultDTO {
	entries := make([]browseEntryDTO, 0, len(r.Entries))
	for _, e := range r.Entries {
		entries = append(entries, browseEntryDTO{Name: e.Name, Path: e.Path, Readable: e.Readable})
	}
	return browseResultDTO{
		Path:      r.Path,
		Parent:    r.Parent,
		Entries:   entries,
		Truncated: r.Truncated,
	}
}

// toDTO converts the domain Source into the serializable form.
func toDTO(s library.Source) sourceDTO {
	return sourceDTO{
		ID:                        s.ID,
		Path:                      s.Path,
		Type:                      s.Type,
		ContentType:               s.ContentType,
		State:                     s.State,
		LastScan:                  s.LastScan,
		WatchEnabled:              s.WatchEnabled,
		PreferredLanguage:         s.PreferredLanguage,
		RetroactiveEnrichmentDays: s.RetroactiveEnrichmentDays,
	}
}

// mapLibraryError translates a domain error from the library package into the pair
// (HTTP status, envelope code). Unmapped errors become 500 internal_error.
func mapLibraryError(err error) (int, string) {
	switch {
	case errors.Is(err, library.ErrPathVazio):
		return http.StatusBadRequest, "invalid_body"
	case errors.Is(err, library.ErrTipoInvalido):
		return http.StatusUnprocessableEntity, "invalid_type"
	case errors.Is(err, library.ErrPathInexistente):
		return http.StatusUnprocessableEntity, "invalid_path"
	case errors.Is(err, library.ErrNaoDiretorio):
		return http.StatusUnprocessableEntity, "not_a_directory"
	case errors.Is(err, library.ErrPathIlegivel):
		return http.StatusUnprocessableEntity, "path_unreadable"
	case errors.Is(err, library.ErrPathDuplicado):
		return http.StatusConflict, "duplicate_path"
	case errors.Is(err, library.ErrNaoEncontrado):
		return http.StatusNotFound, "not_found"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

// respondLibraryError writes the domain error in the standard shape. For 500 it
// hides the internal detail (generic message), preserving the cause in the log via
// middleware.
func respondLibraryError(w http.ResponseWriter, err error) {
	status, code := mapLibraryError(err)
	msg := err.Error()
	if status == http.StatusInternalServerError {
		msg = "erro interno ao processar a requisição"
	}
	writeError(w, status, code, msg)
}

// libraryUnavailable responds when the service was not initialized (no database
// injected). It should not occur in production; it protects route test scenarios.
func (s *Server) libraryUnavailable(w http.ResponseWriter) bool {
	if s.library == nil {
		writeError(w, http.StatusServiceUnavailable, "db_unavailable",
			"persistência indisponível")
		return true
	}
	return false
}

// createLibrary handles POST /api/v1/library.
func (s *Server) createLibrary(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	var req createRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body",
			"corpo da requisição inválido: "+err.Error())
		return
	}

	ctx := r.Context()
	preferredLanguage := preferredLanguagePtr(req.PreferredLanguage)
	if !s.validarPreferredLanguageOuErro(ctx, w, preferredLanguage) {
		return
	}
	if !validarRetroactiveEnrichmentDaysOuErro(w, req.RetroactiveEnrichmentDays) {
		return
	}

	src, err := s.library.Criar(ctx, req.Path, req.Type, req.ContentType, preferredLanguage, req.RetroactiveEnrichmentDays)
	if err != nil {
		respondLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDTO(src))
}

// preferredLanguagePtr converts the plain string field (JSON, without distinguishing
// "absent" x "empty") into the domain pointer: an empty string becomes nil ("no
// preference"), same convention as empty content_type => default in library.Criar.
func preferredLanguagePtr(lang string) *string {
	if lang == "" {
		return nil
	}
	return &lang
}

// validarPreferredLanguageOuErro validates a non-empty preferred_language
// against app_settings.languages.allowed, writing 422
// invalid_body when the code is outside the list. A nil/empty lang never
// validates anything (no preference is always valid). ok=false when the response
// has already been written (validation error or real database error) -- the caller
// must return immediately in that case.
func (s *Server) validarPreferredLanguageOuErro(ctx context.Context, w http.ResponseWriter, lang *string) bool {
	if lang == nil || *lang == "" {
		return true
	}
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return false
	}
	if !cfg.permite(*lang) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"preferred_language fora dos idiomas permitidos")
		return false
	}
	return true
}

// validarRetroactiveEnrichmentDaysOuErro validates a non-nil override of
// retroactive_enrichment_days against the range [retryDaysMinimo,
// retryDaysMaximo], writing 422 invalid_body when outside
// the range. A nil dias never validates anything (no override is always valid).
// ok=false when the response has already been written -- the caller must return
// immediately in that case.
func validarRetroactiveEnrichmentDaysOuErro(w http.ResponseWriter, dias *int) bool {
	if dias == nil {
		return true
	}
	if *dias < retryDaysMinimo || *dias > retryDaysMaximo {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"retroactive_enrichment_days fora do intervalo permitido (1-365)")
		return false
	}
	return true
}

// listLibrary handles GET /api/v1/library.
func (s *Server) listLibrary(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	sources, err := s.library.Listar(r.Context())
	if err != nil {
		respondLibraryError(w, err)
		return
	}
	items := make([]sourceDTO, 0, len(sources))
	for _, src := range sources {
		items = append(items, toDTO(src))
	}
	writeJSON(w, http.StatusOK, listDTO{Items: items})
}

// browseLibraryPath handles GET /api/v1/library/browse:
// folder browser of the filesystem that the API process sees, used
// by the datasource create/edit form to choose a path by clicking
// instead of typing it from memory. An absent/empty path uses "/" as the root;
// errors reuse mapLibraryError (422 invalid_path/not_a_directory/
// path_unreadable), the same codes as createLibrary/patchLibrary.
func (s *Server) browseLibraryPath(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	rawPath := r.URL.Query().Get("path")
	result, err := s.library.Browse(r.Context(), rawPath)
	if err != nil {
		respondLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toBrowseDTO(result))
}

// getLibrary handles GET /api/v1/library/{id}.
func (s *Server) getLibrary(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	src, err := s.library.Obter(r.Context(), id)
	if err != nil {
		respondLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(src))
}

// deleteLibrary handles DELETE /api/v1/library/{id}.
func (s *Server) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.library.Apagar(r.Context(), id); err != nil {
		respondLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// patchLibrary handles PATCH /api/v1/library/{id}: a PARTIAL
// PATCH -- it accepts path and/or watch_enabled, each on its own or both
// together. A field absent from the body (nil pointer) stays unchanged. When both
// come together, path is applied FIRST (cascading re-root of
// media_file/asset/entity to the new prefix) and watch_enabled afterwards, in the
// SAME final response. Changing watch_enabled applies HOT, without a server restart
// (acceptance criterion): false->true calls the SAME registration routine as the
// boot for that source (s.watch.RegistrarFonte);
// true->false calls Close() on the source's watcher and removes its entry from the
// in-memory map (s.watch.DesregistrarFonte). 200 with the updated source; 404
// source does not exist; 400/422/409 according to the domain error (empty/
// invalid/duplicate path); 422 malformed body.
func (s *Server) patchLibrary(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")

	var req patchSourceRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"corpo da requisição inválido: "+err.Error())
		return
	}

	ctx := r.Context()
	if req.PreferredLanguage.Present && !s.validarPreferredLanguageOuErro(ctx, w, req.PreferredLanguage.Value) {
		return
	}
	if req.RetroactiveEnrichmentDays.Present && !validarRetroactiveEnrichmentDaysOuErro(w, req.RetroactiveEnrichmentDays.Value) {
		return
	}

	src, err := s.aplicarPatchLibrary(ctx, id, req)
	if err != nil {
		respondLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(src))
}

// aplicarPatchLibrary applies the partial patch (path and/or watch_enabled and/or
// preferred_language and/or retroactive_enrichment_days) and returns the final
// source -- extracted from patchLibrary to keep the cyclomatic complexity
// low. An empty body (no field present) only returns the current state, without
// touching anything. path is applied FIRST (cascading re-root via
// AtualizarPath); preferred_language and retroactive_enrichment_days afterwards;
// watch_enabled last, applied HOT by the fsnotify watcher
// (aplicarWatchToggle).
func (s *Server) aplicarPatchLibrary(ctx context.Context, id string, req patchSourceRequest) (library.Source, error) {
	if req.Path == nil && req.WatchEnabled == nil && !req.PreferredLanguage.Present && !req.RetroactiveEnrichmentDays.Present {
		return s.library.Obter(ctx, id)
	}

	src, err := s.aplicarPatchPath(ctx, id, req.Path)
	if err != nil {
		return src, err
	}

	if req.PreferredLanguage.Present {
		src, err = s.library.AtualizarPreferredLanguage(ctx, id, req.PreferredLanguage.Value)
		if err != nil {
			return library.Source{}, err
		}
	}

	if req.RetroactiveEnrichmentDays.Present {
		src, err = s.library.AtualizarRetroactiveEnrichmentDays(ctx, id, req.RetroactiveEnrichmentDays.Value)
		if err != nil {
			return library.Source{}, err
		}
	}

	if req.WatchEnabled == nil {
		return src, nil
	}

	src, err = s.library.AtualizarWatch(ctx, id, *req.WatchEnabled)
	if err != nil {
		return library.Source{}, err
	}
	s.aplicarWatchToggle(src, *req.WatchEnabled)
	return src, nil
}

// aplicarPatchPath re-points the source when path was
// provided; otherwise, it only re-reads the current state (no path change
// requested -- the body brought only watch_enabled).
func (s *Server) aplicarPatchPath(ctx context.Context, id string, path *string) (library.Source, error) {
	if path == nil {
		return s.library.Obter(ctx, id)
	}
	return s.library.AtualizarPath(ctx, id, *path)
}

// aplicarWatchToggle triggers the in-memory fsnotify watcher (internal/watch)
// to reflect the watch_enabled change that was JUST persisted --
// without this, the toggle would only take effect after a restart (which the
// documentation explicitly forbids). s.watch is nil when the server has no
// database/library/scanner (same defensive guard as WithLibraryHealthJob);
// in that case it degrades silently (the column was already persisted, only the
// in-memory effect does not apply -- a scenario of route tests only, without a real
// database).
func (s *Server) aplicarWatchToggle(src library.Source, enabled bool) {
	if s.watch == nil {
		return
	}
	if enabled {
		if err := s.watch.RegistrarFonte(src); err != nil {
			log.Printf("library: falha ao ativar o watch da fonte %q (best-effort, fonte segue dependendo do polling periodico): %v", src.ID, err)
		}
		return
	}
	s.watch.DesregistrarFonte(src.ID)
}
