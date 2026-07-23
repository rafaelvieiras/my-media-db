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
	// WatchEnabled liga/desliga o watcher fsnotify desta fonte (briefing
	// 0064). Default false; editável só via PATCH /api/v1/library/{id}.
	WatchEnabled bool `json:"watch_enabled"`
}

// patchSourceRequest é o corpo aceito por PATCH /api/v1/library/{id}
// (briefing 0064 + 0070, PatchSource no contrato): PATCH PARCIAL -- campo
// ausente (ponteiro nil) fica inalterado. Path e watch_enabled podem vir
// juntos ou cada um sozinho; quando os dois vêm no mesmo corpo, o path é
// aplicado PRIMEIRO (recasco em cascata) e o watch_enabled depois.
type patchSourceRequest struct {
	WatchEnabled *bool   `json:"watch_enabled"`
	Path         *string `json:"path"`
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

// browseEntryDTO é a forma de serialização de uma entrada do navegador de
// pastas (schema BrowseEntry do openapi.yaml, briefing 0071).
type browseEntryDTO struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Readable bool   `json:"readable"`
}

// browseResultDTO é a resposta de GET /api/v1/library/browse (schema
// BrowseResult do openapi.yaml, briefing 0071).
type browseResultDTO struct {
	Path      string           `json:"path"`
	Parent    *string          `json:"parent"`
	Entries   []browseEntryDTO `json:"entries"`
	Truncated bool             `json:"truncated"`
}

// toBrowseDTO converte o BrowseResult de domínio para a forma serializável.
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

// toDTO converte a Source de domínio para a forma serializável.
func toDTO(s library.Source) sourceDTO {
	return sourceDTO{
		ID:           s.ID,
		Path:         s.Path,
		Type:         s.Type,
		ContentType:  s.ContentType,
		State:        s.State,
		LastScan:     s.LastScan,
		WatchEnabled: s.WatchEnabled,
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

// browseLibraryPath trata GET /api/v1/library/browse (briefing 0071):
// navegador de pastas do filesystem que o processo da API enxerga, usado
// pelo formulário de cadastro/edição de datasource para escolher um path
// clicando em vez de digitar de cor. path ausente/vazio usa "/" como raiz;
// erros reaproveitam mapLibraryError (422 invalid_path/not_a_directory/
// path_unreadable), mesmos códigos de createLibrary/patchLibrary.
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

// patchLibrary trata PATCH /api/v1/library/{id} (briefing 0064 + 0070): PATCH
// PARCIAL -- aceita path e/ou watch_enabled, cada um sozinho ou os dois
// juntos. Campo ausente do corpo (ponteiro nil) fica inalterado. Quando os
// dois vêm juntos, path é aplicado PRIMEIRO (recasco em cascata de
// media_file/asset/entity para o novo prefixo) e watch_enabled depois, na
// MESMA resposta final. Mudar watch_enabled aplica A QUENTE, sem restart do
// servidor (critério de aceite do briefing 0064): false->true chama a MESMA
// rotina de registro do boot para aquela fonte (s.watch.RegistrarFonte);
// true->false chama Close() no watcher da fonte e remove sua entrada do mapa
// em memória (s.watch.DesregistrarFonte). 200 com a fonte atualizada; 404
// fonte inexistente; 400/422/409 conforme o erro de domínio (path vazio/
// inválido/duplicado); 422 corpo malformado.
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

	src, err := s.aplicarPatchLibrary(r.Context(), id, req)
	if err != nil {
		respondLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(src))
}

// aplicarPatchLibrary aplica o patch parcial (path e/ou watch_enabled) e
// devolve a fonte final -- extraído de patchLibrary para manter a
// complexidade ciclomática baixa. Corpo vazio (os dois nil) só devolve o
// estado atual, sem tocar nada. path é aplicado PRIMEIRO (recasco em
// cascata via AtualizarPath); watch_enabled depois, aplicado A QUENTE pelo
// watcher fsnotify (aplicarWatchToggle).
func (s *Server) aplicarPatchLibrary(ctx context.Context, id string, req patchSourceRequest) (library.Source, error) {
	if req.Path == nil && req.WatchEnabled == nil {
		return s.library.Obter(ctx, id)
	}

	src, err := s.aplicarPatchPath(ctx, id, req.Path)
	if err != nil || req.WatchEnabled == nil {
		return src, err
	}

	src, err = s.library.AtualizarWatch(ctx, id, *req.WatchEnabled)
	if err != nil {
		return library.Source{}, err
	}
	s.aplicarWatchToggle(src, *req.WatchEnabled)
	return src, nil
}

// aplicarPatchPath reaponta a fonte (briefing 0070) quando path foi
// informado; caso contrário, só relê o estado atual (nenhuma mudança de path
// pedida -- o corpo trouxe só watch_enabled).
func (s *Server) aplicarPatchPath(ctx context.Context, id string, path *string) (library.Source, error) {
	if path == nil {
		return s.library.Obter(ctx, id)
	}
	return s.library.AtualizarPath(ctx, id, *path)
}

// aplicarWatchToggle aciona o watcher fsnotify em memória (internal/watch)
// para refletir a mudança de watch_enabled que ACABOU de ser persistida --
// sem isso, o toggle só valeria depois de um restart (o que o briefing 0064
// explicitamente proíbe). s.watch é nil quando o servidor não tem
// banco/library/scanner (mesma guarda defensiva de WithLibraryHealthJob);
// nesse caso degrada em silêncio (a coluna já foi persistida, só o efeito em
// memória não se aplica -- cenário só de teste de rota sem banco real).
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
