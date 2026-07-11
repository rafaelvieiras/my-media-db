package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// sourceDTO é a forma de serialização de uma fonte na API (briefing 0008 sec.4):
// { id, path, type, state, last_scan }. last_scan é null enquanto o scanner
// (0009) não rodar.
type sourceDTO struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Type string `json:"type"`
	// ContentType e a natureza da biblioteca (0034): "movie" | "series". O scanner
	// ramifica o fluxo de match por este valor. Default "movie".
	ContentType string  `json:"content_type"`
	State       string  `json:"state"`
	LastScan    *string `json:"last_scan"`
}

// listDTO é a resposta de GET /api/v1/library.
type listDTO struct {
	Items []sourceDTO `json:"items"`
}

// createRequest é o corpo aceito por POST /api/v1/library.
type createRequest struct {
	Path string `json:"path"`
	Type string `json:"type"`
	// ContentType e opcional (0034): "movie" | "series". Omitido => "movie" (compat).
	ContentType string `json:"content_type"`
}

// toDTO converte a Source de domínio para a forma serializável.
func toDTO(s library.Source) sourceDTO {
	return sourceDTO{
		ID:          s.ID,
		Path:        s.Path,
		Type:        s.Type,
		ContentType: s.ContentType,
		State:       s.State,
		LastScan:    s.LastScan,
	}
}

// mapLibraryError traduz um erro de domínio do pacote library no par
// (status HTTP, code do envelope). Erros não mapeados viram 500 internal_error.
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

// respondLibraryError escreve o erro de domínio no shape padrão. Para 500
// oculta o detalhe interno (mensagem genérica), preservando a causa no log via
// middleware.
func respondLibraryError(w http.ResponseWriter, err error) {
	status, code := mapLibraryError(err)
	msg := err.Error()
	if status == http.StatusInternalServerError {
		msg = "erro interno ao processar a requisição"
	}
	writeError(w, status, code, msg)
}

// libraryUnavailable responde quando o serviço não foi inicializado (sem banco
// injetado). Não deve ocorrer em produção; protege cenários de teste de rota.
func (s *Server) libraryUnavailable(w http.ResponseWriter) bool {
	if s.library == nil {
		writeError(w, http.StatusServiceUnavailable, "db_unavailable",
			"persistência indisponível")
		return true
	}
	return false
}

// createLibrary trata POST /api/v1/library.
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

	src, err := s.library.Criar(r.Context(), req.Path, req.Type, req.ContentType)
	if err != nil {
		respondLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDTO(src))
}

// listLibrary trata GET /api/v1/library.
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

// getLibrary trata GET /api/v1/library/{id}.
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

// deleteLibrary trata DELETE /api/v1/library/{id}.
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
