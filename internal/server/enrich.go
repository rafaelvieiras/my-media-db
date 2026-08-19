package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo implementa as ESCRITAS de enriquecimento (briefing 0015):
//   POST /api/v1/entities/{id}/enrich  — enriquece uma entity existente
//   POST /api/v1/import                — find-or-create por id externo + enrich
// Ambos delegam ao serviço internal/enrich (escreve external, NUNCA override; pelo
// pool de escrita, ADR-0010) e devolvem a entity RESOLVIDA pelo caminho comum
// (resolverEntity), então a resposta já reflete os dados externos compostos.

// enrichBody é o corpo OPCIONAL de POST /entities/{id}/enrich. source/source_id são
// opcionais: sem source resolve a fonte-default DINAMICAMENTE (fonteDefault, 0047);
// sem source_id tenta entity.external_ids[source]. force (briefing 0088, default
// false — o zero-value do bool já cobre "campo ausente") pula o cache de 24h do
// detalhe externo e busca sempre a versão mais recente do provider.
type enrichBody struct {
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
	Force    bool   `json:"force"`
}

// importBody é o corpo de POST /import. type deve ser movie|tv|collection e
// source_id não-vazio (senão 422); sem source a fonte-default é resolvida
// DINAMICAMENTE por capacidade (fonteDefault, 0047). type='collection' (0025)
// importa a coleção inteira: cria a entity da coleção e importa/liga os filmes membros.
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
	source, ok := s.resolverFonte(w, ent.Type, body.Source)
	if !ok {
		return
	}
	sourceID, ok := resolverSourceID(w, source, ent.ExternalIdsJson, body.SourceID)
	if !ok {
		return
	}

	langs, err := s.idiomasPermitidos(ctx, w)
	if err != nil {
		return
	}
	if err := s.enrich.Enriquecer(ctx, id, source, sourceID, ent.Type, langs, body.Force); err != nil {
		respondFonteIndisponivel(w)
		return
	}

	s.responderResolvida(w, ctx, id, http.StatusOK)
}

// importar trata POST /api/v1/import: find-or-create por id externo (dedup MD4) e
// enriquece. 400 corpo inválido; 422 type fora de movie|tv|collection|artista ou
// source_id vazio; 201 se criou / 200 se já existia, com a entity resolvida.
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
	if !tipoImportavel(body.Type) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "type inválido (use movie, tv ou collection)")
		return
	}
	if body.SourceID == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "source_id é obrigatório")
		return
	}
	// Sem source explícito: escolhe DINAMICAMENTE a fonte-default capaz do tipo (0047),
	// nunca assume "tmdb". Nenhum provider capaz => 422 semântico (não 502).
	source, ok := s.resolverFonte(w, body.Type, body.Source)
	if !ok {
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
	resolvida, encontrada, err := s.resolverEntity(ctx, id, "", true)
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

// respondSemFonteParaTipo responde 422 quando o request não informou `source` e NENHUM
// provider registrado é capaz do `tipo` pedido (0047): erro SEMÂNTICO e claro, em vez do
// antigo 502 que surgia ao assumir "tmdb" num ambiente que não o tem registrado.
func respondSemFonteParaTipo(w http.ResponseWriter, tipo string) {
	writeError(w, http.StatusUnprocessableEntity, "no_source_for_type",
		"nenhuma fonte disponível para "+tipo)
}

// fonteDefault resolve DINAMICAMENTE a fonte-default de um `tipo` quando o request não
// informa `source` (0047): devolve o Nome() do PRIMEIRO provider registrado — na ordem de
// prioridade (providersOrdenados) — CAPAZ do tipo, substituindo o antigo hardcode "tmdb".
// ok=false quando nenhum provider é capaz: o handler responde 422 semântico, jamais 502
// por assumir uma fonte ausente. Assim um ambiente só-TVDB (ou qualquer conjunto) importa
// pela fonte realmente registrada.
func (s *Server) fonteDefault(tipo string) (string, bool) {
	for _, p := range s.providersOrdenados() {
		if provedorCapaz(p, tipo) {
			return p.Nome(), true
		}
	}
	return "", false
}

// resolverFonte devolve a fonte a usar para um `tipo`: a `sourceExplicito` quando o
// request a informa, senão a fonte-default capaz (fonteDefault, 0047). Quando nenhuma
// fonte é capaz, responde 422 semântico e devolve ok=false — o handler só precisa de
// `if !ok { return }`. Centraliza o padrão antes copiado em enrich/import/correct.
func (s *Server) resolverFonte(w http.ResponseWriter, tipo, sourceExplicito string) (string, bool) {
	if sourceExplicito != "" {
		return sourceExplicito, true
	}
	src, ok := s.fonteDefault(tipo)
	if !ok {
		respondSemFonteParaTipo(w, tipo)
		return "", false
	}
	return src, true
}

// tipoImportavel diz se `tipo` pode ser importado via POST /import (find-or-create):
// movie, tv, collection ou artista (briefing 0106, ADR-0027 -- artista importa a
// discografia inteira via MusicaProvider). Grãos finos (season/episode/album/faixa)
// entram pela árvore do provider (série/música), não por import direto.
func tipoImportavel(tipo string) bool {
	return tipo == "movie" || tipo == "tv" || tipo == "collection" || tipo == "artista"
}

// resolverSourceID devolve o source_id a usar: o do corpo (`sourceIDBody`), senão o
// external_ids[source] da entity (`externalIdsJson`). Responde 422 e devolve ok=false
// quando nenhum está disponível. Extraído de enrichEntity para manter o handler enxuto.
func resolverSourceID(w http.ResponseWriter, source string, externalIdsJson sql.NullString, sourceIDBody string) (string, bool) {
	sourceID := sourceIDBody
	if sourceID == "" {
		sourceID = externalIDDe(externalIdsJson, source)
	}
	if sourceID == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"sem source_id: informe no corpo ou tenha external_ids["+source+"] na entity")
		return "", false
	}
	return sourceID, true
}

// provedorCapaz diz se o provider `p` sabe importar o `tipo` pedido, via type-assert da
// capacidade opcional correspondente (0031/0046): 'tv' exige provider.SerieProvider (a
// árvore), 'collection' exige provider.ColecaoProvider; para 'movie' (e demais grãos, como
// season/episode) basta o core Provider — todo provider registrado o cumpre.
func provedorCapaz(p provider.Provider, tipo string) bool {
	switch tipo {
	case "tv":
		_, ok := p.(provider.SerieProvider)
		return ok
	case "collection":
		_, ok := p.(provider.ColecaoProvider)
		return ok
	default:
		return true
	}
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
