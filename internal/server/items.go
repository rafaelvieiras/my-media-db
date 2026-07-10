package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa a criação de um ITEM LOCAL manual (briefing 0027): POST
// /api/v1/entities cria uma entity `type=movie|tv` SEM fonte externa — o dono cataloga
// algo que a fonte não tem (ou ainda não casou). Combina com a edição de external_ids
// (external_ids.go): cria-se o item e, depois, fixa-se o id da fonte para uma
// sincronização futura. É o análogo de POST /collections (Fase B do 0025) para
// filme/série. Escrita pelo pool de ESCRITA (ADR-0010); sem enrich (não há fonte).

// tiposItemManual são os tipos aceitos na criação manual: coleção tem o seu próprio
// endpoint (POST /collections), então aqui é só o conteúdo de mídia.
var tiposItemManual = map[string]bool{"movie": true, "tv": true}

// createItemBody é o corpo de POST /api/v1/entities: type (movie|tv) e título são
// obrigatórios; os demais campos base são opcionais (vazio = herda/NULL).
type createItemBody struct {
	Type          string `json:"type"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	Description   string `json:"description"`
	ReleaseDate   string `json:"release_date"`
}

// createItem trata POST /api/v1/entities: cria uma entity LOCAL de mídia (movie|tv) sem
// external_ids, com o status DEFAULT ('cataloged'). 400 corpo JSON inválido; 422 type
// fora de movie|tv ou título vazio (só espaços conta como vazio). 201 com a entity
// resolvida (mesmo shape do detalhe).
func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()

	var body createItemBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if !tiposItemManual[body.Type] {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "type inválido (use movie ou tv)")
		return
	}
	titulo := strings.TrimSpace(body.Title)
	if titulo == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "título é obrigatório")
		return
	}

	id := uuid.NewString()
	if _, err := sqlcgen.New(s.db.Write).CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:            id,
		Type:          body.Type,
		Title:         titulo,
		OriginalTitle: nsOpcional(body.OriginalTitle),
		Description:   nsOpcional(body.Description),
		ReleaseDate:   nsOpcional(body.ReleaseDate),
	}); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderResolvida(w, ctx, id, http.StatusCreated)
}

// nsOpcional embrulha um campo de texto opcional num sql.NullString VÁLIDO só quando
// não é vazio (após trim); vazio vira NULL (herda a base, sem gravar string vazia).
// Compartilhado pelas criações locais (item manual e coleção da Fase B).
func nsOpcional(v string) sql.NullString {
	limpo := strings.TrimSpace(v)
	if limpo == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: limpo, Valid: true}
}
