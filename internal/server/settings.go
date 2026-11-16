package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements the LANGUAGE config in app_settings:
//   GET  /api/v1/settings/languages  — returns {primary, fallback, allowed}
//   PUT  /api/v1/settings/languages  — validates and persists (upsert), invalidates the cache
// It also derives the fallback CHAIN consumed by the resolver (ADR-0019) and does the
// parse/validation of the ?lang= of the entities handlers. The read has a light in-memory cache
// (mutex, like the cacheExterno of the search), invalidated on the PUT.

// appSettingLanguages is the key of the language config in app_settings.
const appSettingLanguages = "languages"

// appSettingEnrichmentRetroativo is the key of the throttle config of the retroactive
// enrichment in app_settings.
const appSettingEnrichmentRetroativo = "enrichment_retroativo"

// appSettingAutoMaterializeArtwork is the key of the automatic artwork materialization toggle
// in app_settings.
const appSettingAutoMaterializeArtwork = "materializar_artes_automaticamente"

// appSettingYtdlp is the key of the customized path of the yt-dlp binary in app_settings
// (ADR-0025).
const appSettingYtdlp = "ytdlp"

// appSettingAutoDownloadTrailerTema is the key of the automatic trailer/theme song download
// toggle in app_settings (ADR-0026).
const appSettingAutoDownloadTrailerTema = "baixar_trailer_tema_automaticamente"

// retryDaysPadrao/retryDaysMinimo/retryDaysMaximo are the default (when never configured) and
// the valid range of the retry throttle: once a fixed 30-day constant in retroativo.go, now
// configurable by the owner, with a 15-day default.
const (
	retryDaysPadrao = 15
	retryDaysMinimo = 1
	retryDaysMaximo = 365
)

// languageSettings is the canonical language config (ADR-0018): primary language, fallback and
// the set of allowed ones. Serialized as the value_json of the `languages` key. It is also the
// PUT body and the GET response.
type languageSettings struct {
	Primary  string   `json:"primary"`
	Fallback string   `json:"fallback"`
	Allowed  []string `json:"allowed"`
}

// defaultLanguageSettings is the SAFE DEFAULT used when the `languages` row does not exist (or
// is corrupted): primary pt-BR, fallback en, allowed [pt-BR, en]. That way the resolution works
// end-to-end before any configuration.
func defaultLanguageSettings() languageSettings {
	return languageSettings{Primary: "pt-BR", Fallback: "en", Allowed: []string{"pt-BR", "en"}}
}

// retroactiveEnrichmentSettings is the throttle config of the retroactive enrichment: the
// minimum interval, in days, between two attempts of the SAME item without a provider.
// Serialized as the value_json of the `enrichment_retroativo` key. It is also the PUT body and
// the GET response.
type retroactiveEnrichmentSettings struct {
	RetryDays int `json:"retry_days"`
}

// defaultRetroactiveEnrichmentSettings is the SAFE DEFAULT used when the
// `enrichment_retroativo` row does not exist (or is corrupted): 15 days.
func defaultRetroactiveEnrichmentSettings() retroactiveEnrichmentSettings {
	return retroactiveEnrichmentSettings{RetryDays: retryDaysPadrao}
}

// autoMaterializeArtworkSettings is the automatic artwork materialization toggle: when Enabled,
// every successful auto-import of the scan (movie or series) already materializes the inventory
// artwork into the library's real folder right away, without waiting for the periodic
// midias_faltantes job nor a manual click. Serialized as the value_json of the
// `materializar_artes_automaticamente` key. It is also the PUT body and the GET response.
type autoMaterializeArtworkSettings struct {
	Enabled bool `json:"enabled"`
}

// defaultAutoMaterializeArtworkSettings is the SAFE DEFAULT used when the
// `materializar_artes_automaticamente` row does not exist (or is corrupted): off -- automatic
// materialization is opt-in (the owner turns it on when he wants).
func defaultAutoMaterializeArtworkSettings() autoMaterializeArtworkSettings {
	return autoMaterializeArtworkSettings{Enabled: false}
}

// autoDownloadTrailerTemaSettings is the automatic trailer/theme song download toggle
// (ADR-0026): when Enabled, both mechanisms share the SAME toggle (see ADR-0026 item 5) --
// (a) the periodic `trailer_tema_faltantes` job stays always registered/schedulable regardless
// of this toggle; (b) every successful auto-import of the scan (movie or series) fires, in a
// best-effort goroutine, an attempt to download trailer/theme for the new item, without waiting
// for the periodic job nor a manual click. Serialized as the value_json of the
// `baixar_trailer_tema_automaticamente` key. It is also the PUT body and the GET response.
type autoDownloadTrailerTemaSettings struct {
	Enabled bool `json:"enabled"`
}

// defaultAutoDownloadTrailerTemaSettings is the SAFE DEFAULT used when the
// `baixar_trailer_tema_automaticamente` row does not exist (or is corrupted): off -- automatic
// download is opt-in (the owner turns it on when he wants).
func defaultAutoDownloadTrailerTemaSettings() autoDownloadTrailerTemaSettings {
	return autoDownloadTrailerTemaSettings{Enabled: false}
}

// ytdlpPathSettings is the customized path of the yt-dlp binary configured by the owner
// (ADR-0025): {"custom_binary_path": string|null}. Serialized as the value_json of the `ytdlp`
// key. It is the PUT body (the GET returns the richer shape ytdlpStatusResponse, below). It
// takes PRECEDENCE over the MYMEDIADB_YTDLP_PATH env var (the same spirit as ADR-0023) --
// applied at runtime through ytdlp.Gerenciador.SetCustomPath, without restarting the
// process.
type ytdlpPathSettings struct {
	CustomBinaryPath *string `json:"custom_binary_path"`
}

// defaultYtdlpPathSettings is the SAFE DEFAULT used when the `ytdlp` row does not exist (or is
// corrupted): no customized path -- the Gerenciador falls back to the env var (if any) or the
// automatic download.
func defaultYtdlpPathSettings() ytdlpPathSettings {
	return ytdlpPathSettings{CustomBinaryPath: nil}
}

// ytdlpStatusResponse is the body returned by GET/PUT /settings/ytdlp: the EFFECTIVE status of
// the Gerenciador (available/origin/detail, computed by ytdlp.Gerenciador.Status) plus the
// customized path currently stored in the database. `origem` does not distinguish whether a
// "custom" path came from the database or from the env var (the contract only exposes the
// custom_binary_path of the DATABASE -- the env var is never echoed by the API, the same spirit
// of never exposing a secret/boot config).
type ytdlpStatusResponse struct {
	Disponivel       bool    `json:"disponivel"`
	Origem           string  `json:"origem"`
	CustomBinaryPath *string `json:"custom_binary_path"`
	Detalhe          *string `json:"detalhe"`
}

// settingsCache keeps the already decoded language config and retroactive enrichment throttle
// config in memory, protected by the SAME mutex. `carregado`/`retroCarregado` distinguish "not
// read yet" from "read" (the value may be the default). Invalidated on the corresponding
// PUT.
type settingsCache struct {
	mu        sync.Mutex
	carregado bool
	langs     languageSettings

	retroCarregado bool
	retro          retroactiveEnrichmentSettings

	autoMaterializeCarregado bool
	autoMaterialize          autoMaterializeArtworkSettings

	autoDownloadTrailerTemaCarregado bool
	autoDownloadTrailerTema          autoDownloadTrailerTemaSettings

	ytdlpCarregado bool
	ytdlpPath      ytdlpPathSettings
}

// obterLanguageSettings returns the language config, from the cache when already loaded,
// otherwise reading app_settings through the READ connection. Missing row or invalid JSON ⇒
// safe default (never an error for that). Only a real database error is propagated.
func (s *Server) obterLanguageSettings(ctx context.Context) (languageSettings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.carregado {
		return s.settings.langs, nil
	}
	row, err := s.leitura().ObterAppSetting(ctx, appSettingLanguages)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.settings.langs = defaultLanguageSettings()
			s.settings.carregado = true
			return s.settings.langs, nil
		}
		return languageSettings{}, err
	}
	langs := defaultLanguageSettings()
	if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed languageSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil && parsed.valida() {
			langs = parsed
		}
	}
	s.settings.langs = langs
	s.settings.carregado = true
	return langs, nil
}

// obterRetroactiveEnrichmentSettings returns the throttle config of the retroactive enrichment,
// from the cache when already loaded, otherwise reading app_settings through the READ
// connection. Missing row or invalid JSON ⇒ safe default (never an error for that). Only a real
// database error is propagated. It mirrors obterLanguageSettings line by line.
func (s *Server) obterRetroactiveEnrichmentSettings(ctx context.Context) (retroactiveEnrichmentSettings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.retroCarregado {
		return s.settings.retro, nil
	}
	row, err := s.leitura().ObterAppSetting(ctx, appSettingEnrichmentRetroativo)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.settings.retro = defaultRetroactiveEnrichmentSettings()
			s.settings.retroCarregado = true
			return s.settings.retro, nil
		}
		return retroactiveEnrichmentSettings{}, err
	}
	retro := defaultRetroactiveEnrichmentSettings()
	if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed retroactiveEnrichmentSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil && parsed.valida() {
			retro = parsed
		}
	}
	s.settings.retro = retro
	s.settings.retroCarregado = true
	return retro, nil
}

// obterAutoMaterializeArtworkSettings returns the automatic artwork materialization toggle, from
// the cache when already loaded, otherwise reading app_settings through the READ connection.
// Missing row or invalid JSON ⇒ safe default (off, never an error for that). Only a real
// database error is propagated. It mirrors
// obterLanguageSettings/obterRetroactiveEnrichmentSettings line by line.
func (s *Server) obterAutoMaterializeArtworkSettings(ctx context.Context) (autoMaterializeArtworkSettings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.autoMaterializeCarregado {
		return s.settings.autoMaterialize, nil
	}
	row, err := s.leitura().ObterAppSetting(ctx, appSettingAutoMaterializeArtwork)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.settings.autoMaterialize = defaultAutoMaterializeArtworkSettings()
			s.settings.autoMaterializeCarregado = true
			return s.settings.autoMaterialize, nil
		}
		return autoMaterializeArtworkSettings{}, err
	}
	cfg := defaultAutoMaterializeArtworkSettings()
	if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed autoMaterializeArtworkSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil {
			cfg = parsed
		}
	}
	s.settings.autoMaterialize = cfg
	s.settings.autoMaterializeCarregado = true
	return cfg, nil
}

// obterAutoDownloadTrailerTemaSettings returns the automatic trailer/theme song download toggle,
// from the cache when already loaded, otherwise reading app_settings through the READ
// connection. Missing row or invalid JSON ⇒ safe default (off, never an error for that). Only a
// real database error is propagated. It mirrors obterAutoMaterializeArtworkSettings line by
// line.
func (s *Server) obterAutoDownloadTrailerTemaSettings(ctx context.Context) (autoDownloadTrailerTemaSettings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.autoDownloadTrailerTemaCarregado {
		return s.settings.autoDownloadTrailerTema, nil
	}
	row, err := s.leitura().ObterAppSetting(ctx, appSettingAutoDownloadTrailerTema)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.settings.autoDownloadTrailerTema = defaultAutoDownloadTrailerTemaSettings()
			s.settings.autoDownloadTrailerTemaCarregado = true
			return s.settings.autoDownloadTrailerTema, nil
		}
		return autoDownloadTrailerTemaSettings{}, err
	}
	cfg := defaultAutoDownloadTrailerTemaSettings()
	if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed autoDownloadTrailerTemaSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil {
			cfg = parsed
		}
	}
	s.settings.autoDownloadTrailerTema = cfg
	s.settings.autoDownloadTrailerTemaCarregado = true
	return cfg, nil
}

// obterYtdlpPathSettings returns the customized yt-dlp path stored in the database, from the
// cache when already loaded, otherwise reading app_settings through the READ connection. Missing
// row or invalid JSON ⇒ safe default (no customized path). Only a real database error is
// propagated. On (re)load it synchronizes the value into ytdlp.Gerenciador
// (s.ytdlp.SetCustomPath) -- that is how the persisted override starts to apply, both at boot
// (called once in montarServicos) and after a PUT (invalidarSettings zeroes the cache, forcing
// this re-read on the next call). Without s.ytdlp injected, it only returns the config (without
// synchronizing anything) -- a defensive scenario of route test without a Gerenciador.
func (s *Server) obterYtdlpPathSettings(ctx context.Context) (ytdlpPathSettings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.ytdlpCarregado {
		return s.settings.ytdlpPath, nil
	}
	cfg := defaultYtdlpPathSettings()
	row, err := s.leitura().ObterAppSetting(ctx, appSettingYtdlp)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return ytdlpPathSettings{}, err
		}
	} else if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed ytdlpPathSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil {
			cfg = parsed
		}
	}
	s.settings.ytdlpPath = cfg
	s.settings.ytdlpCarregado = true
	if s.ytdlp != nil {
		if cfg.CustomBinaryPath != nil {
			s.ytdlp.SetCustomPath(*cfg.CustomBinaryPath)
		} else {
			s.ytdlp.SetCustomPath("")
		}
	}
	return cfg, nil
}

// montarYtdlpStatusResponse builds the body of GET/PUT /settings/ytdlp: the effective status
// (through s.ytdlp.Status) plus the customized path stored in the database. Without s.ytdlp
// injected, it degrades to unavailable/"none" instead of querying an inexistent Gerenciador (a
// defensive scenario, it should not happen in production -- main.go always injects it through
// WithYtdlp).
func (s *Server) montarYtdlpStatusResponse(ctx context.Context, pathCfg ytdlpPathSettings) ytdlpStatusResponse {
	if s.ytdlp == nil {
		detalhe := "yt-dlp nao configurado neste servidor"
		return ytdlpStatusResponse{
			Disponivel:       false,
			Origem:           "nenhum",
			CustomBinaryPath: pathCfg.CustomBinaryPath,
			Detalhe:          &detalhe,
		}
	}
	disponivel, origem, detalhe := s.ytdlp.Status(ctx)
	resp := ytdlpStatusResponse{
		Disponivel:       disponivel,
		Origem:           origem,
		CustomBinaryPath: pathCfg.CustomBinaryPath,
	}
	if detalhe != "" {
		resp.Detalhe = &detalhe
	}
	return resp
}

// invalidarSettings zeroes the cache (called after the PUT writes).
func (s *Server) invalidarSettings() {
	s.settings.mu.Lock()
	s.settings.carregado = false
	s.settings.retroCarregado = false
	s.settings.autoMaterializeCarregado = false
	s.settings.autoDownloadTrailerTemaCarregado = false
	s.settings.ytdlpCarregado = false
	s.settings.mu.Unlock()
}

// valida tells whether the config is internally consistent (ADR-0018): primary and fallback
// non-empty and ∈ allowed; at least one allowed, all of them non-empty. It is the PUT rule and
// the guard against a corrupted value_json in the database.
func (l languageSettings) valida() bool {
	if l.Primary == "" || l.Fallback == "" || len(l.Allowed) == 0 {
		return false
	}
	for _, a := range l.Allowed {
		if a == "" {
			return false
		}
	}
	return l.permite(l.Primary) && l.permite(l.Fallback)
}

// valida tells whether the throttle config is internally consistent: retry_days within
// [retryDaysMinimo, retryDaysMaximo]. It is the PUT rule and the guard against a corrupted
// value_json in the database.
func (r retroactiveEnrichmentSettings) valida() bool {
	return r.RetryDays >= retryDaysMinimo && r.RetryDays <= retryDaysMaximo
}

// permite tells whether the code is in the allowed set.
func (l languageSettings) permite(codigo string) bool {
	for _, a := range l.Allowed {
		if a == codigo {
			return true
		}
	}
	return false
}

// cadeiaDeIdiomas derives the fallback chain for a requested `lang` (ADR-0019):
// `[lang, fallback, original_language?]` DEDUPLICATED, ending implicitly at the neutral base
// (the file — the resolver falls back to it when the chain runs out). Empty entries are
// discarded.
func (l languageSettings) cadeiaDeIdiomas(lang string, originalLanguage sql.NullString) []string {
	bruta := []string{lang, l.Fallback}
	if originalLanguage.Valid && originalLanguage.String != "" {
		bruta = append(bruta, originalLanguage.String)
	}
	vista := make(map[string]struct{}, len(bruta))
	cadeia := make([]string, 0, len(bruta))
	for _, c := range bruta {
		if c == "" {
			continue
		}
		if _, ok := vista[c]; ok {
			continue
		}
		vista[c] = struct{}{}
		cadeia = append(cadeia, c)
	}
	return cadeia
}

// parseLang reads the ?lang= validating it against the allowed ones. Absent ⇒ primary (default).
// Present but outside the allowed ones ⇒ 400 invalid_query (ok=false).
func (s *Server) parseLang(w http.ResponseWriter, cfg languageSettings, lang string) (string, bool) {
	if lang == "" {
		return cfg.Primary, true
	}
	if !cfg.permite(lang) {
		writeError(w, http.StatusBadRequest, "invalid_query",
			"lang fora dos idiomas permitidos")
		return "", false
	}
	return lang, true
}

// getLanguageSettings handles GET /api/v1/settings/languages: it returns the current config (or
// the safe default when absent).
func (s *Server) getLanguageSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cfg, err := s.obterLanguageSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putLanguageSettings handles PUT /api/v1/settings/languages: it validates (primary and fallback
// ⊆ allowed, non-empty codes) and upserts, invalidating the cache. Malformed body ⇒ 400; invalid
// ⇒ 422; success ⇒ 200 with the written config.
func (s *Server) putLanguageSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body languageSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if !body.valida() {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"config de idioma inválida: principal e fallback devem estar em allowed, e os códigos não podem ser vazios")
		return
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingLanguages,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarSettings()
	writeJSON(w, http.StatusOK, body)
}

// getAutoMaterializeArtworkSettings handles GET /api/v1/settings/auto-materialize-artwork: it
// returns the current toggle (or the safe default {enabled:false} when absent). It mirrors
// getLanguageSettings/getEnrichmentRetroativoSettings.
func (s *Server) getAutoMaterializeArtworkSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cfg, err := s.obterAutoMaterializeArtworkSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putAutoMaterializeArtworkSettings handles PUT /api/v1/settings/auto-materialize-artwork: it
// upserts the toggle, invalidating the cache. Malformed body ⇒ 400 invalid_body; success ⇒ 200
// with the written value. It mirrors putLanguageSettings/putEnrichmentRetroativoSettings (with
// no semantic validation step: a bool has no invalid state beyond "it did not decode").
func (s *Server) putAutoMaterializeArtworkSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body autoMaterializeArtworkSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingAutoMaterializeArtwork,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarSettings()
	writeJSON(w, http.StatusOK, body)
}

// getAutoDownloadTrailerTemaSettings handles GET /api/v1/settings/auto-download-trailer-tema: it
// returns the current toggle (or the safe default {enabled:false} when absent). It mirrors
// getAutoMaterializeArtworkSettings.
func (s *Server) getAutoDownloadTrailerTemaSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cfg, err := s.obterAutoDownloadTrailerTemaSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putAutoDownloadTrailerTemaSettings handles PUT /api/v1/settings/auto-download-trailer-tema: it
// upserts the toggle, invalidating the cache. Malformed body ⇒ 400 invalid_body; success ⇒ 200
// with the written value. It mirrors putAutoMaterializeArtworkSettings (with no semantic
// validation step: a bool has no invalid state beyond "it did not decode").
func (s *Server) putAutoDownloadTrailerTemaSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body autoDownloadTrailerTemaSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingAutoDownloadTrailerTema,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarSettings()
	writeJSON(w, http.StatusOK, body)
}

// getEnrichmentRetroativoSettings handles GET /api/v1/settings/enrichment-retroativo: it returns
// the current config (or the safe default {retry_days:15} when absent). It mirrors
// getLanguageSettings.
func (s *Server) getEnrichmentRetroativoSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cfg, err := s.obterRetroactiveEnrichmentSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putEnrichmentRetroativoSettings handles PUT /api/v1/settings/enrichment-retroativo: it validates
// (retry_days ∈ [1, 365]) and upserts, invalidating the cache. Malformed body ⇒ 400; invalid ⇒
// 422; success ⇒ 200 with the written config. It mirrors putLanguageSettings.
func (s *Server) putEnrichmentRetroativoSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body retroactiveEnrichmentSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if !body.valida() {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"config de enriquecimento retroativo inválida: retry_days deve estar entre 1 e 365")
		return
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingEnrichmentRetroativo,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarSettings()
	writeJSON(w, http.StatusOK, body)
}

// getYtdlpSettings handles GET /api/v1/settings/ytdlp (ADR-0025): it returns the EFFECTIVE
// status of the yt-dlp binary (the customized path from the database wins over the
// MYMEDIADB_YTDLP_PATH env var, which wins over the dynamically downloaded binary) plus the
// customized path stored in the database. Never a 500 for lack of yt-dlp -- unavailability is a
// normal STATE of the body (disponivel=false + detail), not an HTTP error.
func (s *Server) getYtdlpSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	pathCfg, err := s.obterYtdlpPathSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, s.montarYtdlpStatusResponse(r.Context(), pathCfg))
}

// putYtdlpSettings handles PUT /api/v1/settings/ytdlp (ADR-0025): it writes (or clears, when
// null/empty string) the customized path in the database and synchronizes the
// ytdlp.Gerenciador (SetCustomPath) IMMEDIATELY -- a runtime effect, without restarting the
// process (unlike the TMDB integration: here no provider credential is involved at boot, only a
// binary path). Malformed body ⇒ 400 invalid_body; success ⇒ 200 with the effective status
// recomputed (the same shape as the GET).
func (s *Server) putYtdlpSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body ytdlpPathSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	// empty string == null: both clear the override (they go back to inheriting the env
	// var/automatic download).
	if body.CustomBinaryPath != nil && *body.CustomBinaryPath == "" {
		body.CustomBinaryPath = nil
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingYtdlp,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarSettings()
	if s.ytdlp != nil {
		if body.CustomBinaryPath != nil {
			s.ytdlp.SetCustomPath(*body.CustomBinaryPath)
		} else {
			s.ytdlp.SetCustomPath("")
		}
	}
	writeJSON(w, http.StatusOK, s.montarYtdlpStatusResponse(r.Context(), body))
}
