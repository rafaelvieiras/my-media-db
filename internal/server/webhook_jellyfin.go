package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/webhook"
)

// This file implements the RECEIVER of the Jellyfin playback webhook (ADR-0022, which depends
// on the consumption history foundation): it normalizes each play/pause/stop notification from
// the `jellyfin-plugin-webhook` plugin into a `historico_evento`, matching the item by the
// external ids the payload already carries. The tolerant parser
// (NotificationType/ItemType/Provider_*/ticks) lives in internal/webhook (no database/HTTP);
// here there is only the HTTP wiring + the match by external id + the token config through
// app_settings.

// appSettingWebhookJellyfinToken is the key of the Jellyfin webhook token config in
// app_settings, value {"token": "..."}.
const appSettingWebhookJellyfinToken = "webhook_jellyfin_token"

// tamanhoTokenBytes is the size (in bytes, before hex-encode) of the generated token -- 32 bytes
// = 64 hex chars, the same spirit as an API key.
const tamanhoTokenBytes = 32

// jellyfinWebhookTokenSettings is the token config, serialized as the value_json of the
// webhook_jellyfin_token key. It is also the body of POST/GET .../webhooks/jellyfin/token.
type jellyfinWebhookTokenSettings struct {
	Token string `json:"token"`
}

// obterJellyfinWebhookToken reads the current token from app_settings through the READ
// connection. sql.ErrNoRows (no token generated yet) is propagated as it came -- the GET/the
// webhook handler decide what to do (404/401).
func (s *Server) obterJellyfinWebhookToken(ctx context.Context) (string, error) {
	row, err := s.leitura().ObterAppSetting(ctx, appSettingWebhookJellyfinToken)
	if err != nil {
		return "", err
	}
	if !row.ValueJson.Valid || row.ValueJson.String == "" {
		return "", sql.ErrNoRows
	}
	var cfg jellyfinWebhookTokenSettings
	if err := json.Unmarshal([]byte(row.ValueJson.String), &cfg); err != nil || cfg.Token == "" {
		return "", sql.ErrNoRows
	}
	return cfg.Token, nil
}

// gerarTokenAleatorio generates a hex token of tamanhoTokenBytes bytes through crypto/rand
// (never math/rand -- an authentication token).
func gerarTokenAleatorio() (string, error) {
	b := make([]byte, tamanhoTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// regenerateJellyfinWebhookToken handles POST /api/v1/webhooks/jellyfin/token: it generates a
// new token and REPLACES the previous one (upsert in app_settings) -- subsequent calls to the
// webhook with the old token stop working. 200 with the new token; 500 on a real error
// (generation or write).
func (s *Server) regenerateJellyfinWebhookToken(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	token, err := gerarTokenAleatorio()
	if err != nil {
		respondErroInterno(w)
		return
	}
	cfg := jellyfinWebhookTokenSettings{Token: token}
	valor, err := json.Marshal(cfg)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingWebhookJellyfinToken,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// getJellyfinWebhookToken handles GET /api/v1/webhooks/jellyfin/token: it returns the current
// token. 404 if no token was generated yet (the owner must call POST first).
func (s *Server) getJellyfinWebhookToken(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	token, err := s.obterJellyfinWebhookToken(r.Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found",
				"nenhum token de webhook do Jellyfin foi gerado ainda")
			return
		}
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, jellyfinWebhookTokenSettings{Token: token})
}

// receiveJellyfinWebhook handles POST /api/v1/webhooks/jellyfin?token=: it authenticates by
// token (CONSTANT-TIME comparison), parses the payload best-effort, matches the item by external
// id and persists a historico_evento. ALWAYS 204 when the token matches -- including an
// unrecognizable payload / no match / unsupported ItemType (so Jellyfin does not retry); 401 when
// the token is missing or does not match. Never a 500 because of the payload BODY (parse/match
// degrade to "discarded and logged").
func (s *Server) receiveJellyfinWebhook(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if !s.autenticarJellyfinWebhook(w, r) {
		return
	}
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("webhook jellyfin: falha ao ler o corpo (descartado): %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ev, err := webhook.ParsePayload(body)
	if err != nil {
		log.Printf("webhook jellyfin: payload nao e JSON valido (descartado): %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !ev.ItemTypeSuportado() {
		log.Printf("webhook jellyfin: ItemType %q nao suportado (descartado)", ev.ItemType)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	status := webhook.MapStatus(ev.NotificationType, ev.ProgressPercent)
	if status == "" {
		log.Printf("webhook jellyfin: NotificationType %q desconhecido (descartado)", ev.NotificationType)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	entityID, ok := s.casarEntityJellyfin(ctx, ev)
	if !ok {
		// No match: it NO LONGER discards -- it writes an ORPHAN event (entity_id NULL) with the
		// raw IDs/title/year of the payload, to allow a retroactive backfill when an entity
		// gains that external id (future import, match correction/confirmation). A write error
		// only logs -- the same "never fail for Jellyfin" spirit as the matching path right
		// below.
		if _, err := sqlcgen.New(s.db.Write).CriarHistoricoEventoOrfao(ctx, sqlcgen.CriarHistoricoEventoOrfaoParams{
			ID:              uuid.NewString(),
			Kind:            webhook.Kind,
			Status:          status,
			ProgressPercent: f64ToNull(ev.ProgressPercent),
			PositionSeconds: f64ToNull(ev.PositionSeconds),
			OccurredAt:      time.Now().UTC().Format(time.RFC3339),
			Source:          webhook.Source,
			PayloadJson:     sql.NullString{String: string(body), Valid: true},
			RawTmdbID:       nsDe(ev.TmdbID),
			RawImdbID:       nsDe(ev.ImdbID),
			RawTvdbID:       nsDe(ev.TvdbID),
			RawTitle:        nsDe(ev.Title),
			RawYear:         nsDe(ev.Year),
		}); err != nil {
			log.Printf("webhook jellyfin: falha ao gravar evento orfao (tmdb=%q imdb=%q tvdb=%q): %v",
				ev.TmdbID, ev.ImdbID, ev.TvdbID, err)
		} else {
			log.Printf("webhook jellyfin: nenhuma entity casou (tmdb=%q imdb=%q tvdb=%q) -- gravado como orfao",
				ev.TmdbID, ev.ImdbID, ev.TvdbID)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	_, err = sqlcgen.New(s.db.Write).CriarHistoricoEvento(ctx, sqlcgen.CriarHistoricoEventoParams{
		ID:              uuid.NewString(),
		EntityID:        nsDe(entityID),
		Kind:            webhook.Kind,
		Status:          status,
		ProgressPercent: f64ToNull(ev.ProgressPercent),
		PositionSeconds: f64ToNull(ev.PositionSeconds),
		OccurredAt:      time.Now().UTC().Format(time.RFC3339),
		Source:          webhook.Source,
		PayloadJson:     sql.NullString{String: string(body), Valid: true},
	})
	if err != nil {
		// Real database error on write: it logs but still answers 204 -- Jellyfin must not retry
		// because of an internal failure of ours (the owner investigates through the log; a retry
		// would not help).
		log.Printf("webhook jellyfin: falha ao gravar historico_evento (entity=%s): %v", entityID, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// autenticarJellyfinWebhook validates the query `?token=` against the one configured in
// app_settings, already writing the error response (401/500) when it fails. Extracted from the
// main handler only to keep the cyclomatic complexity low (the same pattern as exigirEntity) —
// `false` means "already answered, stop here".
func (s *Server) autenticarJellyfinWebhook(w http.ResponseWriter, r *http.Request) bool {
	tokenEsperado, err := s.obterJellyfinWebhookToken(r.Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "unauthorized",
				"nenhum token de webhook do Jellyfin foi gerado ainda")
			return false
		}
		respondErroInterno(w)
		return false
	}
	if !tokenValido(r.URL.Query().Get("token"), tokenEsperado) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "token invalido ou ausente")
		return false
	}
	return true
}

// tokenValido compares the received token with the expected one in CONSTANT TIME
// (crypto/subtle -- it avoids a timing attack, even being a low-risk endpoint). "" never
// matches (it avoids comparing two empty values).
func tokenValido(recebido, esperado string) bool {
	if recebido == "" || esperado == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(recebido), []byte(esperado)) == 1
}

// casarEntityJellyfin tries to match the Jellyfin item with a cataloged entity through the
// external ids present in the payload, in the order tmdb -> imdb -> tvdb, stopping at the first
// one that matches. It returns (id, false) with no id present or none matching -- it never
// creates a new entity from a webhook.
func (s *Server) casarEntityJellyfin(ctx context.Context, ev webhook.Evento) (string, bool) {
	if ev.TmdbID != "" {
		if ent, err := s.leitura().ObterEntityPorTmdb(ctx, sql.NullString{String: ev.TmdbID, Valid: true}); err == nil {
			return ent.ID, true
		}
	}
	if ev.ImdbID != "" {
		if ent, err := s.leitura().ObterEntityPorImdb(ctx, sql.NullString{String: ev.ImdbID, Valid: true}); err == nil {
			return ent.ID, true
		}
	}
	if ev.TvdbID != "" {
		if ent, err := s.leitura().ObterEntityPorTvdb(ctx, sql.NullString{String: ev.TvdbID, Valid: true}); err == nil {
			return ent.ID, true
		}
	}
	return "", false
}
