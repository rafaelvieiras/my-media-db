package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Este arquivo implementa as ESCRITAS de enriquecimento (briefing 0015):
//   POST /api/v1/entities/{id}/enrich  — enriquece uma entity existente
//   POST /api/v1/import                — find-or-create por id externo + enrich
// Ambos delegam ao serviço internal/enrich (escreve external, NUNCA override; pelo
// pool de escrita, ADR-0010) e devolvem a entity RESOLVIDA pelo caminho comum
// (resolverEntity), então a resposta já reflete os dados externos compostos.

// enrichBody é o corpo OPCIONAL de POST /entities/{id}/enrich. Ambos os campos são
// opcionais: sem source usa "tmdb"; sem source_id tenta entity.external_ids[source].
type enrichBody struct {
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
}

// importBody é o corpo de POST /import. type deve ser movie|tv e source_id
// não-vazio (senão 422); source default "tmdb".
type importBody struct {
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
	Type     string `json:"type"`
}

// enrichEntity trata POST /api/v1/entities/{id}/enrich. 404 se a entity não
// existe; 422 se não há id externo para usar; 200 com a entity resolvida em
// sucesso. A fonte da falha (provider) vira 502; a imagem degrada no serviço.
func (s *Server) enrichEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if s.enrichIndisponivel(w) {
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

	var body enrichBody
	if ok := decodeOpcional(w, r, &body); !ok {
		return
	}
	source := body.Source
	if source == "" {
		source = "tmdb"
	}
	sourceID := body.SourceID
	if sourceID == "" {
		sourceID = externalIDDe(ent.ExternalIdsJson, source)
	}
	if sourceID == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"sem source_id: informe no corpo ou tenha external_ids["+source+"] na entity")
		return
	}

	langs, err := s.idiomasPermitidos(ctx, w)
	if err != nil {
		return
	}
	if err := s.enrich.Enriquecer(ctx, id, source, sourceID, ent.Type, langs); err != nil {
		respondFonteIndisponivel(w)
		return
	}

	s.responderResolvida(w, ctx, id, http.StatusOK)
}

// importar trata POST /api/v1/import: find-or-create por id externo (dedup MD4) e
// enriquece. 400 corpo inválido; 422 type fora de movie|tv ou source_id vazio;
// 201 se criou / 200 se já existia, com a entity resolvida.
func (s *Server) importar(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if s.enrichIndisponivel(w) {
		return
	}
	ctx := r.Context()

	var body importBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	source := body.Source
	if source == "" {
		source = "tmdb"
	}
	if body.Type != "movie" && body.Type != "tv" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "type inválido (use movie ou tv)")
		return
	}
	if body.SourceID == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "source_id é obrigatório")
		return
	}

	langs, err := s.idiomasPermitidos(ctx, w)
	if err != nil {
		return
	}
	id, criada, err := s.enrich.Importar(ctx, source, body.SourceID, body.Type, langs)
	if err != nil {
		respondFonteIndisponivel(w)
		return
	}

	status := http.StatusOK
	if criada {
		status = http.StatusCreated
	}
	s.responderResolvida(w, ctx, id, status)
}

// responderResolvida monta e escreve a entity resolvida (caminho comum de
// enrich/import). 404 se a entity sumiu no meio; 500 em erro de banco.
func (s *Server) responderResolvida(w http.ResponseWriter, ctx context.Context, id string, status int) {
	// Idioma default (principal das settings): o enrich/import ainda não recebe
	// ?lang= — resolverEntity com "" cai no principal.
	resolvida, encontrada, err := s.resolverEntity(ctx, id, "")
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !encontrada {
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
		return
	}
	writeJSON(w, status, resolvida)
}

// idiomasPermitidos devolve os idiomas PERMITIDOS das settings (0019) para o
// enrich/import puxar TODAS as traduções permitidas numa só ida (0020). Um erro
// real de banco vira 500 (err != nil); o chamador aborta. O default seguro cobre a
// linha ausente, então em operação normal isto nunca falha.
func (s *Server) idiomasPermitidos(ctx context.Context, w http.ResponseWriter) ([]string, error) {
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return nil, err
	}
	return cfg.Allowed, nil
}

// enrichIndisponivel responde 503 quando o serviço de enrich não está ligado
// (sem fonte externa/credencial ou sem diretório de assets) — não há o que
// importar. Protege contra nil deref em s.enrich.
func (s *Server) enrichIndisponivel(w http.ResponseWriter) bool {
	if s.enrich == nil {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"enriquecimento indisponível (fonte externa não configurada)")
		return true
	}
	return false
}

// respondFonteIndisponivel responde 502 quando a fonte externa falhou ao entregar
// o detalhe (timeout/erro do provider) — o import não pôde completar.
func respondFonteIndisponivel(w http.ResponseWriter) {
	writeError(w, http.StatusBadGateway, "external_source_error",
		"a fonte externa falhou ao entregar o detalhe")
}

// decodeOpcional decodifica um corpo JSON OPCIONAL em v: corpo vazio (EOF) é ok
// (v fica no zero-value); corpo presente mas malformado responde 422 e devolve
// false. Usado pelo enrich, cujo body é opcional.
func decodeOpcional(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	writeError(w, http.StatusUnprocessableEntity, "invalid_body", "corpo JSON inválido")
	return false
}

// externalIDDe extrai external_ids[source] do external_ids_json (string→string).
// Vazio quando ausente/inválido. Usado quando o enrich não recebe source_id no
// corpo (item já casado pelo scanner, id na identidade).
func externalIDDe(ns sql.NullString, source string) string {
	if !ns.Valid || ns.String == "" {
		return ""
	}
	m := map[string]any{}
	if json.Unmarshal([]byte(ns.String), &m) != nil {
		return ""
	}
	if v, ok := m[source].(string); ok {
		return v
	}
	return ""
}
