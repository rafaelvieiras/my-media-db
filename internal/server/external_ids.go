package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa a EDIÇÃO da identidade externa da entity (briefing 0027):
// PUT /api/v1/entities/{id}/external-ids grava o mapa `external_ids` (source -> id na
// fonte). É a identidade AUTORITATIVA (MD4): as colunas geradas external_id_tmdb/imdb
// re-derivam do JSON gravado, então fixar aqui o id da fonte prepara uma
// sincronização futura (o dono corrige o id quando a fonte muda/apaga/funde um item).
//
// NÃO re-enriquece: só fixa a identidade. Para puxar o detalhe pela nova identidade, o
// dono chama POST /entities/{id}/enrich depois. Escrita pelo pool de ESCRITA (ADR-0010),
// via a query AtualizarExternalIds já existente (usada também pelo enrich).

// updateExternalIdsBody é o corpo de PUT /entities/{id}/external-ids: o mapa COMPLETO
// de identidades (source -> id). Substitui o mapa inteiro (semântica de PUT); um par
// com valor vazio ("") REMOVE aquela fonte (não grava id vazio).
type updateExternalIdsBody struct {
	ExternalIDs map[string]string `json:"external_ids"`
}

// updateExternalIds trata PUT /api/v1/entities/{id}/external-ids. 503 sem banco; 404
// entity inexistente; 400 corpo JSON inválido; 422 quando alguma CHAVE (source) é vazia.
// Pares com valor vazio são descartados (remoção). 200 com a entity RESOLVIDA — a UI
// reflete a nova identidade na hora.
func (s *Server) updateExternalIds(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, err := s.leitura().ObterEntity(ctx, id); err != nil {
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
	s.responderResolvida(w, ctx, id, http.StatusOK)
}

// normalizarExternalIds valida e enxuga o mapa: rejeita chave vazia (422) e descarta
// pares de valor vazio (remoção), aparando espaços de chave e valor. Devolve o mapa
// limpo e ok=true; em erro escreve o 422 e devolve ok=false. Um mapa nil vira `{}`
// (limpa todas as identidades — é a semântica de PUT do mapa completo).
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
			continue // valor vazio = remover aquela fonte
		}
		out[chave] = valor
	}
	return out, true
}
