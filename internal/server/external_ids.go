package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/historico"
)

// This file implements the EDITING of the entity's external identity:
// PUT /api/v1/entities/{id}/external-ids writes the `external_ids` map (source -> id at the
// source). It is the AUTHORITATIVE identity (MD4): the generated columns external_id_tmdb/imdb
// re-derive from the written JSON, so pinning the source id here prepares a
// future synchronization (the owner fixes the id when the source changes/deletes/merges an item).
//
// It does NOT re-enrich: it only pins the identity. To pull the detail through the new identity,
// the owner calls POST /entities/{id}/enrich afterwards. Written through the WRITE pool (ADR-0010),
// via the already existing AtualizarExternalIds query (also used by enrich).

// updateExternalIdsBody is the body of PUT /entities/{id}/external-ids: the COMPLETE
// map of identities (source -> id). It replaces the whole map (PUT semantics); a pair
// with an empty value ("") REMOVES that source (it does not write an empty id).
type updateExternalIdsBody struct {
	ExternalIDs map[string]string `json:"external_ids"`
}

// updateExternalIds handles PUT /api/v1/entities/{id}/external-ids. 503 without a database; 404
// entity does not exist; 400 invalid JSON body; 422 when any KEY (source) is empty.
// Pairs with an empty value are discarded (removal). 200 with the RESOLVED entity — the UI
// reflects the new identity right away.
func (s *Server) updateExternalIds(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}

	var body updateExternalIdsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}

	ids, ok := normalizarExternalIds(w, body.ExternalIDs)
	if !ok {
		return
	}
	// Safety net against a duplicate: every new id that already
	// belongs to ANOTHER entity merges that other one into this one (no investment from the
	// owner) or aborts with 409 (with investment) -- before writing anything.
	for source, sourceID := range ids {
		if _, ok := s.resolverColisaoExterna(w, ctx, id, ent.Type, source, sourceID); !ok {
			return
		}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if err := sqlcgen.New(s.db.Write).AtualizarExternalIds(ctx, sqlcgen.AtualizarExternalIdsParams{
		ExternalIdsJson: nsDe(string(b)),
		ID:              id,
	}); err != nil {
		respondErroInterno(w)
		return
	}
	// Retroactive backfill: the identity has already been written
	// successfully -- linking orphan events is a bonus, it never reverts/blocks
	// the response already formed. An error is only logged.
	if _, err := historico.VincularOrfaos(ctx, sqlcgen.New(s.db.Write), id, ids); err != nil {
		log.Printf("external-ids: backfill de eventos orfaos para %s falhou (segue): %v", id, err)
	}
	s.responderResolvida(w, ctx, id, http.StatusOK)
}

// normalizarExternalIds validates and trims the map: it rejects an empty key (422) and discards
// pairs with an empty value (removal), trimming spaces from key and value. It returns the
// clean map and ok=true; on error it writes the 422 and returns ok=false. A nil map becomes `{}`
// (clears every identity — that is the PUT semantics of the complete map).
func normalizarExternalIds(w http.ResponseWriter, in map[string]string) (map[string]string, bool) {
	out := make(map[string]string, len(in))
	for k, v := range in {
		chave := strings.TrimSpace(k)
		valor := strings.TrimSpace(v)
		if chave == "" {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body", "source (chave) não pode ser vazio")
			return nil, false
		}
		if valor == "" {
			continue // empty value = remove that source
		}
		out[chave] = valor
	}
	return out, true
}
