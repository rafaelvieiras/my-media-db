package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements the FOUNDATION of the consumption history (ADR-0022): the 4 REST
// endpoints over `historico_evento` (record, list, derived state, delete). It follows the
// same validation/404/response-shape pattern already used in
// assets.go/creditos_interno.go — no new pattern.
// `kind`/`status`/`source` are free TEXT (principle 3 of AGENTS.md): the validation here
// is only "non-empty", never a closed enum. The derivation of the "current state"
// (GET .../historico/estado) groups IN GO over ListarUltimoHistoricoEventoPorKind (an
// authorized trade-off — low volume per entity, so advanced SQL is not worth it).

// historicoSourcePadrao is the value of `source` when the POST body informs none
// (default "manual" — the Jellyfin webhook uses "jellyfin-webhook").
const historicoSourcePadrao = "manual"

// createHistoricoEventoRequest is the body of POST /entities/{id}/historico.
type createHistoricoEventoRequest struct {
	Kind            string           `json:"kind"`
	Status          string           `json:"status"`
	ProgressPercent *float64         `json:"progress_percent"`
	PositionSeconds *float64         `json:"position_seconds"`
	OccurredAt      string           `json:"occurred_at"`
	Source          string           `json:"source"`
	Payload         *json.RawMessage `json:"payload"`
}

// historicoEventoDTO is the public shape of an event (POST 201 and the items of
// GET .../historico).
type historicoEventoDTO struct {
	ID              string           `json:"id"`
	Kind            string           `json:"kind"`
	Status          string           `json:"status"`
	ProgressPercent *float64         `json:"progress_percent"`
	PositionSeconds *float64         `json:"position_seconds"`
	OccurredAt      string           `json:"occurred_at"`
	Source          string           `json:"source"`
	Payload         *json.RawMessage `json:"payload"`
	CreatedAt       string           `json:"created_at"`
}

// historicoEventoListDTO is the response of GET /entities/{id}/historico.
type historicoEventoListDTO struct {
	Eventos []historicoEventoDTO `json:"eventos"`
}

// historicoEstadoItemDTO is the projection of the most recent event of ONE kind
// (GET .../historico/estado) — no id/payload, only the summary used by the UI.
type historicoEstadoItemDTO struct {
	Status          string   `json:"status"`
	ProgressPercent *float64 `json:"progress_percent"`
	PositionSeconds *float64 `json:"position_seconds"`
	OccurredAt      string   `json:"occurred_at"`
	Source          string   `json:"source"`
}

// historicoEstadoDTO is the response of GET .../historico/estado: one item per distinct
// kind already seen in the entity's history.
type historicoEstadoDTO struct {
	Estado map[string]historicoEstadoItemDTO `json:"estado"`
}

// historicoEventoToDTO converts the database row into the public shape, decoding
// payload_json (when present) back into an object.
func historicoEventoToDTO(e sqlcgen.HistoricoEvento) historicoEventoDTO {
	dto := historicoEventoDTO{
		ID:              e.ID,
		Kind:            e.Kind,
		Status:          e.Status,
		ProgressPercent: nf64Ptr(e.ProgressPercent),
		PositionSeconds: nf64Ptr(e.PositionSeconds),
		OccurredAt:      e.OccurredAt,
		Source:          e.Source,
		CreatedAt:       e.CreatedAt,
	}
	if e.PayloadJson.Valid && e.PayloadJson.String != "" {
		raw := json.RawMessage(e.PayloadJson.String)
		dto.Payload = &raw
	}
	return dto
}

// The *ParaEvento helpers below exist because sqlc only reuses the
// sqlcgen.HistoricoEvento model (SELECT *) when the query selects ALL the columns of the
// table; with the raw_tmdb_id/raw_imdb_id/raw_tvdb_id/raw_title/raw_year columns, the
// queries that do not select them (CriarHistoricoEvento, ListarHistoricoEventoPorEntity,
// ListarUltimoHistoricoEventoPorKind, BuscarHistoricoEventoPorID) each got their own Row
// type -- structurally equal to HistoricoEvento minus the raw_*/EntityID fields never used
// by these handlers. It converts to the common shape instead of duplicating
// historicoEventoToDTO/derivarHistoricoEstado per Row type.

func criarRowParaEvento(r sqlcgen.CriarHistoricoEventoRow) sqlcgen.HistoricoEvento {
	return sqlcgen.HistoricoEvento{
		ID: r.ID, EntityID: r.EntityID, Kind: r.Kind, Status: r.Status,
		ProgressPercent: r.ProgressPercent, PositionSeconds: r.PositionSeconds,
		OccurredAt: r.OccurredAt, Source: r.Source, PayloadJson: r.PayloadJson,
		CreatedAt: r.CreatedAt,
	}
}

func listarPorEntityRowParaEvento(r sqlcgen.ListarHistoricoEventoPorEntityRow) sqlcgen.HistoricoEvento {
	return sqlcgen.HistoricoEvento{
		ID: r.ID, EntityID: r.EntityID, Kind: r.Kind, Status: r.Status,
		ProgressPercent: r.ProgressPercent, PositionSeconds: r.PositionSeconds,
		OccurredAt: r.OccurredAt, Source: r.Source, PayloadJson: r.PayloadJson,
		CreatedAt: r.CreatedAt,
	}
}

func ultimoPorKindRowParaEvento(r sqlcgen.ListarUltimoHistoricoEventoPorKindRow) sqlcgen.HistoricoEvento {
	return sqlcgen.HistoricoEvento{
		ID: r.ID, EntityID: r.EntityID, Kind: r.Kind, Status: r.Status,
		ProgressPercent: r.ProgressPercent, PositionSeconds: r.PositionSeconds,
		OccurredAt: r.OccurredAt, Source: r.Source, PayloadJson: r.PayloadJson,
		CreatedAt: r.CreatedAt,
	}
}

func buscarPorIDRowParaEvento(r sqlcgen.BuscarHistoricoEventoPorIDRow) sqlcgen.HistoricoEvento {
	return sqlcgen.HistoricoEvento{
		ID: r.ID, EntityID: r.EntityID, Kind: r.Kind, Status: r.Status,
		ProgressPercent: r.ProgressPercent, PositionSeconds: r.PositionSeconds,
		OccurredAt: r.OccurredAt, Source: r.Source, PayloadJson: r.PayloadJson,
		CreatedAt: r.CreatedAt,
	}
}

// nf64Ptr converts sql.NullFloat64 into *float64 (nil when NULL).
func nf64Ptr(nf sql.NullFloat64) *float64 {
	if !nf.Valid {
		return nil
	}
	v := nf.Float64
	return &v
}

// createHistoricoEvento handles POST /api/v1/entities/{id}/historico: it creates a
// generic historico_evento. `source` absent -> "manual"; `occurred_at` absent -> the
// server timestamp at call time. 201 with the created event; 404 unknown entity; 422
// empty kind/status or progress_percent outside [0,100] or a malformed occurred_at.
func (s *Server) createHistoricoEvento(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	var body createHistoricoEventoRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	occurredAt, source, erroCode, erroMsg, ok := validarCreateHistoricoEvento(body)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, erroCode, erroMsg)
		return
	}

	var payloadJSON sql.NullString
	if body.Payload != nil {
		payloadJSON = sql.NullString{String: string(*body.Payload), Valid: true}
	}

	evento, err := sqlcgen.New(s.db.Write).CriarHistoricoEvento(ctx, sqlcgen.CriarHistoricoEventoParams{
		ID:              uuid.NewString(),
		EntityID:        nsDe(id),
		Kind:            body.Kind,
		Status:          body.Status,
		ProgressPercent: f64ToNull(body.ProgressPercent),
		PositionSeconds: f64ToNull(body.PositionSeconds),
		OccurredAt:      occurredAt,
		Source:          source,
		PayloadJson:     payloadJSON,
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusCreated, historicoEventoToDTO(criarRowParaEvento(evento)))
}

// validarCreateHistoricoEvento validates the body of POST .../historico and returns
// `occurred_at`/`source` already normalized (default applied). Extracted from the handler
// only to keep the cyclomatic complexity low (the same pattern as
// exigirEntity/obterAssetDaEntity) — `ok=false` carries the code/message ready for the
// caller's `writeError`.
func validarCreateHistoricoEvento(body createHistoricoEventoRequest) (occurredAt, source, code, msg string, ok bool) {
	if body.Kind == "" || body.Status == "" {
		return "", "", "invalid_body", "kind e status sao obrigatorios e nao podem ser vazios", false
	}
	if body.ProgressPercent != nil && (*body.ProgressPercent < 0 || *body.ProgressPercent > 100) {
		return "", "", "invalid_body", "progress_percent deve estar entre 0 e 100", false
	}

	occurredAt = time.Now().UTC().Format(time.RFC3339)
	if body.OccurredAt != "" {
		t, err := time.Parse(time.RFC3339, body.OccurredAt)
		if err != nil {
			return "", "", "invalid_body", "occurred_at deve estar no formato RFC3339", false
		}
		occurredAt = t.UTC().Format(time.RFC3339)
	}

	source = body.Source
	if source == "" {
		source = historicoSourcePadrao
	}
	return occurredAt, source, "", "", true
}

// f64ToNull converts *float64 into sql.NullFloat64 (NULL when nil).
func f64ToNull(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

// listHistoricoEventos handles GET /api/v1/entities/{id}/historico: it lists the
// entity's events in occurred_at DESC order, with an optional ?kind= filter.
// 200 ({"eventos":[]} when there is none); 404 unknown entity.
func (s *Server) listHistoricoEventos(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	var kindFiltro any
	if k := r.URL.Query().Get("kind"); k != "" {
		kindFiltro = k
	}
	eventos, err := s.leitura().ListarHistoricoEventoPorEntity(ctx, sqlcgen.ListarHistoricoEventoPorEntityParams{
		EntityID: nsDe(id),
		Kind:     kindFiltro,
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	out := make([]historicoEventoDTO, 0, len(eventos))
	for _, e := range eventos {
		out = append(out, historicoEventoToDTO(listarPorEntityRowParaEvento(e)))
	}
	writeJSON(w, http.StatusOK, historicoEventoListDTO{Eventos: out})
}

// getHistoricoEstado handles GET /api/v1/entities/{id}/historico/estado: the derived
// summary — one item per distinct kind, with the most recent occurred_at event of that
// kind. The grouping happens HERE IN GO (low volume per entity, so advanced SQL is not
// worth it) over ListarUltimoHistoricoEventoPorKind, which already comes ordered
// occurred_at DESC, id DESC — the first event of each kind seen in the sweep is the most
// recent one of that kind, even with kinds interleaved out of insertion order. {} when
// there is no history; 404 unknown entity.
func (s *Server) getHistoricoEstado(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	eventos, err := s.leitura().ListarUltimoHistoricoEventoPorKind(ctx, nsDe(id))
	if err != nil {
		respondErroInterno(w)
		return
	}
	convertidos := make([]sqlcgen.HistoricoEvento, 0, len(eventos))
	for _, e := range eventos {
		convertidos = append(convertidos, ultimoPorKindRowParaEvento(e))
	}
	writeJSON(w, http.StatusOK, historicoEstadoDTO{Estado: derivarHistoricoEstado(convertidos)})
}

// derivarHistoricoEstado groups events (already ordered occurred_at DESC, id DESC) by
// kind, keeping only the FIRST seen of each kind (the most recent one). PURE helper,
// tested in isolation.
func derivarHistoricoEstado(eventos []sqlcgen.HistoricoEvento) map[string]historicoEstadoItemDTO {
	estado := make(map[string]historicoEstadoItemDTO)
	for _, e := range eventos {
		if _, ja := estado[e.Kind]; ja {
			continue
		}
		estado[e.Kind] = historicoEstadoItemDTO{
			Status:          e.Status,
			ProgressPercent: nf64Ptr(e.ProgressPercent),
			PositionSeconds: nf64Ptr(e.PositionSeconds),
			OccurredAt:      e.OccurredAt,
			Source:          e.Source,
		}
	}
	return estado
}

// deleteHistoricoEvento handles DELETE /api/v1/entities/{id}/historico/{eventoId}: it
// removes a single event (the owner's correction, principle 5 of AGENTS.md preserved --
// it is not a mass purge). 204 on success; 404 unknown entity/event or an event from
// another entity.
func (s *Server) deleteHistoricoEvento(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	eventoID := chi.URLParam(r, "eventoId")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	evento, ok := s.obterHistoricoEventoDaEntity(w, ctx, id, eventoID)
	if !ok {
		return
	}
	if err := sqlcgen.New(s.db.Write).ApagarHistoricoEvento(ctx, evento.ID); err != nil {
		respondErroInterno(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// obterHistoricoEventoDaEntity loads an event requiring that it EXISTS and belongs to
// `id` (404 when absent or from another entity). The common guard of the DELETE (it
// mirrors obterAssetDaEntity in assets.go).
func (s *Server) obterHistoricoEventoDaEntity(w http.ResponseWriter, ctx context.Context, id, eventoID string) (sqlcgen.HistoricoEvento, bool) {
	row, err := s.leitura().BuscarHistoricoEventoPorID(ctx, eventoID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "evento de historico nao encontrado")
			return sqlcgen.HistoricoEvento{}, false
		}
		respondErroInterno(w)
		return sqlcgen.HistoricoEvento{}, false
	}
	evento := buscarPorIDRowParaEvento(row)
	if !evento.EntityID.Valid || evento.EntityID.String != id {
		writeError(w, http.StatusNotFound, "not_found", "evento de historico nao encontrado")
		return sqlcgen.HistoricoEvento{}, false
	}
	return evento, true
}

// ── Orphan events ───────────────────────────────────────────────────────────

// historicoRawExternalIdsDTO is the `raw_external_ids` of historicoEventoOrfaoDTO -- only
// the keys present in the event show up in the JSON (omitempty), mirroring "Absent = not
// informed by the event" from openapi.yaml.
type historicoRawExternalIdsDTO struct {
	Tmdb string `json:"tmdb,omitempty"`
	Imdb string `json:"imdb,omitempty"`
	Tvdb string `json:"tvdb,omitempty"`
}

// historicoEventoOrfaoDTO is the shape of an item of GET /historico/orfaos -- without
// payload/created_at (they are not part of the HistoricoEventoOrfao schema of
// openapi.yaml), with the raw_* provenance fields instead of entity_id (which is always
// NULL for an orphan, by definition).
type historicoEventoOrfaoDTO struct {
	ID              string                     `json:"id"`
	Kind            string                     `json:"kind"`
	Status          string                     `json:"status"`
	ProgressPercent *float64                   `json:"progress_percent"`
	PositionSeconds *float64                   `json:"position_seconds"`
	OccurredAt      string                     `json:"occurred_at"`
	Source          string                     `json:"source"`
	RawTitle        *string                    `json:"raw_title"`
	RawYear         *string                    `json:"raw_year"`
	RawExternalIDs  historicoRawExternalIdsDTO `json:"raw_external_ids"`
}

// historicoEventoOrfaoListDTO is the response of GET /historico/orfaos -- the same
// pagination envelope as GET /entities (items/total/limit/offset).
type historicoEventoOrfaoListDTO struct {
	Items  []historicoEventoOrfaoDTO `json:"items"`
	Total  int64                     `json:"total"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
}

// historicoEventoOrfaoToDTO converts the database row (sqlcgen.HistoricoEvento, already
// with entity_id NULL) into the public orphan shape.
func historicoEventoOrfaoToDTO(e sqlcgen.HistoricoEvento) historicoEventoOrfaoDTO {
	return historicoEventoOrfaoDTO{
		ID:              e.ID,
		Kind:            e.Kind,
		Status:          e.Status,
		ProgressPercent: nf64Ptr(e.ProgressPercent),
		PositionSeconds: nf64Ptr(e.PositionSeconds),
		OccurredAt:      e.OccurredAt,
		Source:          e.Source,
		RawTitle:        nsPtr(e.RawTitle),
		RawYear:         nsPtr(e.RawYear),
		RawExternalIDs: historicoRawExternalIdsDTO{
			Tmdb: e.RawTmdbID.String,
			Imdb: e.RawImdbID.String,
			Tvdb: e.RawTvdbID.String,
		},
	}
}

// listHistoricoOrfaos handles GET /api/v1/historico/orfaos: the paged list of the events
// still without an entity, occurred_at DESC order. 200 always (an empty list is a normal
// state, not an error); 400 invalid limit/offset (the same parsePaginacao as
// GET /entities).
func (s *Server) listHistoricoOrfaos(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()
	limit, offset, ok := parsePaginacao(w, r.URL.Query())
	if !ok {
		return
	}

	eventos, err := s.leitura().ListarHistoricoEventoOrfaos(ctx, sqlcgen.ListarHistoricoEventoOrfaosParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	total, err := s.leitura().ContarHistoricoEventoOrfaos(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	items := make([]historicoEventoOrfaoDTO, 0, len(eventos))
	for _, e := range eventos {
		items = append(items, historicoEventoOrfaoToDTO(e))
	}
	writeJSON(w, http.StatusOK, historicoEventoOrfaoListDTO{
		Items: items, Total: total, Limit: limit, Offset: offset,
	})
}
