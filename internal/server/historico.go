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

// Este arquivo implementa a FUNDAÇÃO do histórico de consumo (briefing 0092,
// ADR-0022): os 4 endpoints REST sobre `historico_evento` (registrar, listar,
// estado derivado, apagar). Segue o mesmo padrão de validação/404/shape de
// resposta já usado em assets.go/creditos_interno.go — nada de padrão novo.
// `kind`/`status`/`source` são TEXT livre (princípio 3 do AGENTS.md): a
// validação aqui é só "não-vazio", nunca um enum fechado. A derivação de
// "estado atual" (GET .../historico/estado) agrupa em GO sobre
// ListarUltimoHistoricoEventoPorKind (autorizado pelo briefing — volume
// baixo por entity, não compensa SQL avançado).

// historicoSourcePadrao é o valor de `source` quando o corpo do POST não
// informa nenhum (default "manual" — o briefing 0093 usa "jellyfin-webhook").
const historicoSourcePadrao = "manual"

// createHistoricoEventoRequest é o corpo de POST /entities/{id}/historico.
type createHistoricoEventoRequest struct {
	Kind            string           `json:"kind"`
	Status          string           `json:"status"`
	ProgressPercent *float64         `json:"progress_percent"`
	PositionSeconds *float64         `json:"position_seconds"`
	OccurredAt      string           `json:"occurred_at"`
	Source          string           `json:"source"`
	Payload         *json.RawMessage `json:"payload"`
}

// historicoEventoDTO é o shape público de um evento (POST 201 e itens do
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

// historicoEventoListDTO é a resposta de GET /entities/{id}/historico.
type historicoEventoListDTO struct {
	Eventos []historicoEventoDTO `json:"eventos"`
}

// historicoEstadoItemDTO é a projeção do evento mais recente de UM kind
// (GET .../historico/estado) — sem id/payload, só o resumo usado pela UI.
type historicoEstadoItemDTO struct {
	Status          string   `json:"status"`
	ProgressPercent *float64 `json:"progress_percent"`
	PositionSeconds *float64 `json:"position_seconds"`
	OccurredAt      string   `json:"occurred_at"`
	Source          string   `json:"source"`
}

// historicoEstadoDTO é a resposta de GET .../historico/estado: um item por
// kind distinto já visto no histórico da entity.
type historicoEstadoDTO struct {
	Estado map[string]historicoEstadoItemDTO `json:"estado"`
}

// historicoEventoToDTO converte a linha do banco no shape público, decodificando
// payload_json (quando presente) de volta para objeto.
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

// nf64Ptr converte sql.NullFloat64 em *float64 (nil quando NULL).
func nf64Ptr(nf sql.NullFloat64) *float64 {
	if !nf.Valid {
		return nil
	}
	v := nf.Float64
	return &v
}

// createHistoricoEvento trata POST /api/v1/entities/{id}/historico: cria um
// historico_evento genérico. `source` ausente -> "manual"; `occurred_at`
// ausente -> timestamp do servidor no momento da chamada. 201 com o evento
// criado; 404 entity inexistente; 422 kind/status vazios ou progress_percent
// fora de [0,100] ou occurred_at malformado.
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
		EntityID:        id,
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
	writeJSON(w, http.StatusCreated, historicoEventoToDTO(evento))
}

// validarCreateHistoricoEvento valida o corpo de POST .../historico e devolve
// `occurred_at`/`source` já normalizados (default aplicado). Extraído do
// handler só para manter a complexidade ciclomática baixa (mesmo padrão de
// exigirEntity/obterAssetDaEntity) — `ok=false` traz o código/mensagem
// prontos pro `writeError` do chamador.
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

// f64ToNull converte *float64 em sql.NullFloat64 (NULL quando nil).
func f64ToNull(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

// listHistoricoEventos trata GET /api/v1/entities/{id}/historico: lista os
// eventos da entity em ordem occurred_at DESC, com filtro opcional ?kind=.
// 200 ({"eventos":[]} quando nao ha nenhum); 404 entity inexistente.
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
		EntityID: id,
		Kind:     kindFiltro,
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	out := make([]historicoEventoDTO, 0, len(eventos))
	for _, e := range eventos {
		out = append(out, historicoEventoToDTO(e))
	}
	writeJSON(w, http.StatusOK, historicoEventoListDTO{Eventos: out})
}

// getHistoricoEstado trata GET /api/v1/entities/{id}/historico/estado:
// resumo derivado — um item por kind distinto, com o evento de occurred_at
// mais recente daquele kind. O agrupamento acontece AQUI EM GO (autorizado
// pelo briefing 0092: volume baixo por entity, nao compensa SQL avancado)
// sobre ListarUltimoHistoricoEventoPorKind, que ja vem ordenado
// occurred_at DESC, id DESC — o primeiro evento de cada kind visto na
// varredura e o mais recente daquele kind, mesmo com kinds intercalados
// fora de ordem de insercao. {} quando nao ha historico; 404 entity
// inexistente.
func (s *Server) getHistoricoEstado(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	eventos, err := s.leitura().ListarUltimoHistoricoEventoPorKind(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, historicoEstadoDTO{Estado: derivarHistoricoEstado(eventos)})
}

// derivarHistoricoEstado agrupa eventos (ja ordenados occurred_at DESC, id
// DESC) por kind, mantendo so o PRIMEIRO visto de cada kind (o mais
// recente). Helper PURO, testado isoladamente.
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

// deleteHistoricoEvento trata DELETE /api/v1/entities/{id}/historico/{eventoId}:
// remove um evento pontual (correcao do dono, principio 5 do AGENTS.md
// preservado -- nao e purga em massa). 204 sucesso; 404 entity/evento
// inexistente ou evento de outra entity.
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

// obterHistoricoEventoDaEntity carrega um evento exigindo que ele EXISTA e
// pertenca a `id` (404 quando ausente ou de outra entity). Guarda comum do
// DELETE (espelha obterAssetDaEntity em assets.go).
func (s *Server) obterHistoricoEventoDaEntity(w http.ResponseWriter, ctx context.Context, id, eventoID string) (sqlcgen.HistoricoEvento, bool) {
	evento, err := s.leitura().BuscarHistoricoEventoPorID(ctx, eventoID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "evento de historico nao encontrado")
			return sqlcgen.HistoricoEvento{}, false
		}
		respondErroInterno(w)
		return sqlcgen.HistoricoEvento{}, false
	}
	if evento.EntityID != id {
		writeError(w, http.StatusNotFound, "not_found", "evento de historico nao encontrado")
		return sqlcgen.HistoricoEvento{}, false
	}
	return evento, true
}
