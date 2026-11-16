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

// This file implements the enrichment WRITES:
//   POST /api/v1/entities/{id}/enrich  — enriches an existing entity
//   POST /api/v1/import                — find-or-create by external id + enrich
// Both delegate to the internal/enrich service (it writes external, NEVER override; through
// the write pool, ADR-0010) and return the entity RESOLVED by the common path
// (resolverEntity), so the response already reflects the composed external data.

// enrichBody is the OPTIONAL body of POST /entities/{id}/enrich. source/source_id are
// optional: without source it resolves the default source DYNAMICALLY (fonteDefault);
// without source_id it tries entity.external_ids[source]. force (default
// false — the bool zero-value already covers "absent field") skips the 24h cache of the
// external detail and always fetches the latest version from the provider.
type enrichBody struct {
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
	Force    bool   `json:"force"`
}

// importBody is the body of POST /import. type must be movie|tv|collection|artista|
// faixa and source_id non-empty (otherwise 422); without source the default source is
// resolved DYNAMICALLY by capability (fonteDefault). type='collection'
// imports the whole collection: it creates the collection entity and imports/links the
// member movies. type='faixa' (a SINGLE track missing from an album
// that is already internal) requires ParentID: the INTERNAL id of the parent album to
// link (entity_relation kind=music_album) -- the other types ignore this field.
type importBody struct {
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
	Type     string `json:"type"`
	ParentID string `json:"parent_id"`
}

// enrichEntity handles POST /api/v1/entities/{id}/enrich. 404 if the entity does
// not exist; 422 if there is no external id to use; 200 with the entity resolved on
// success. A failure of the source (provider) becomes 502; the image degrades in the
// service.
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

// importar handles POST /api/v1/import: find-or-create by external id (MD4 dedup) and
// enriches. 400 invalid body; 422 type outside movie|tv|collection|artista or
// empty source_id; 201 if it created / 200 if it already existed, with the resolved
// entity.
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
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "type inválido (use movie, tv, collection, artista ou faixa)")
		return
	}
	if body.SourceID == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "source_id é obrigatório")
		return
	}
	// type=faixa requires the INTERNAL id of the parent album: without it
	// there is no way to link the standalone track (entity_relation kind=music_album) --
	// the other types do not even look at body.ParentID.
	if body.Type == "faixa" && body.ParentID == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "parent_id é obrigatório para type=faixa")
		return
	}
	// Without an explicit source: it DYNAMICALLY chooses the default source capable of the
	// type, never assuming "tmdb". No capable provider => a semantic 422 (not 502).
	source, ok := s.resolverFonte(w, body.Type, body.Source)
	if !ok {
		return
	}

	langs, err := s.idiomasPermitidos(ctx, w)
	if err != nil {
		return
	}
	// type=faixa bypasses the generic s.enrich.Importar dispatcher (which does not carry
	// PARENT context): it needs the internal album (body.ParentID) to resolve the
	// release mbid and link the track by entity_relation -- see
	// enrich.ImportarFaixaAvulsa.
	var id string
	var criada bool
	if body.Type == "faixa" {
		id, criada, err = s.enrich.ImportarFaixaAvulsa(ctx, source, body.SourceID, body.ParentID, langs)
	} else {
		id, criada, err = s.enrich.Importar(ctx, source, body.SourceID, body.Type, langs)
	}
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

// responderResolvida assembles and writes the resolved entity (common path of
// enrich/import). 404 if the entity vanished in the middle; 500 on a database error.
func (s *Server) responderResolvida(w http.ResponseWriter, ctx context.Context, id string, status int) {
	// Default language (primary from the settings): enrich/import does not receive
	// ?lang= yet — resolverEntity with "" falls back to the primary.
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

// idiomasPermitidos returns the ALLOWED languages from the settings for
// enrich/import to pull ALL the allowed translations in a single round trip. A real
// database error becomes 500 (err != nil); the caller aborts. The safe default covers the
// missing row, so in normal operation this never fails.
func (s *Server) idiomasPermitidos(ctx context.Context, w http.ResponseWriter) ([]string, error) {
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return nil, err
	}
	return cfg.Allowed, nil
}

// enrichIndisponivel responds 503 when the enrich service is not enabled
// (no external source/credential or no assets directory) — there is nothing to
// import. Protects against a nil deref on s.enrich.
func (s *Server) enrichIndisponivel(w http.ResponseWriter) bool {
	if s.enrich == nil {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"enriquecimento indisponível (fonte externa não configurada)")
		return true
	}
	return false
}

// respondFonteIndisponivel responds 502 when the external source failed to deliver
// the detail (timeout/provider error) — the import could not complete.
func respondFonteIndisponivel(w http.ResponseWriter) {
	writeError(w, http.StatusBadGateway, "external_source_error",
		"a fonte externa falhou ao entregar o detalhe")
}

// respondSemFonteParaTipo responds 422 when the request did not inform `source` and NO
// registered provider is capable of the requested `tipo`: a CLEAR SEMANTIC error, instead of
// the old 502 that appeared when assuming "tmdb" in an environment that does not have it
// registered.
func respondSemFonteParaTipo(w http.ResponseWriter, tipo string) {
	writeError(w, http.StatusUnprocessableEntity, "no_source_for_type",
		"nenhuma fonte disponível para "+tipo)
}

// fonteDefault DYNAMICALLY resolves the default source of a `tipo` when the request does not
// inform `source`: it returns the Nome() of the FIRST registered provider — in priority
// order (providersOrdenados) — CAPABLE of the type, replacing the old "tmdb" hardcode.
// ok=false when no provider is capable: the handler responds a semantic 422, never a 502
// for assuming an absent source. That way a TVDB-only environment (or any set) imports
// through the source actually registered.
func (s *Server) fonteDefault(tipo string) (string, bool) {
	for _, p := range s.providersOrdenados() {
		if provedorCapaz(p, tipo) {
			return p.Nome(), true
		}
	}
	return "", false
}

// resolverFonte returns the source to use for a `tipo`: `sourceExplicito` when the
// request informs it, otherwise the capable default source (fonteDefault). When no
// source is capable, it responds a semantic 422 and returns ok=false — the handler only needs
// `if !ok { return }`. It centralizes the pattern previously copied in
// enrich/import/correct.
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

// tipoImportavel says whether `tipo` can be imported via POST /import (find-or-create):
// movie, tv, collection, artista (ADR-0027 -- importing an artist imports the
// whole discography via MusicaProvider) or faixa (a SINGLE track missing from an album
// that is already internal, it requires body.ParentID; see ImportarFaixaAvulsa
// in importar() above). The other fine grains (season/episode/album) keep
// coming in only through the provider tree (series/music), not through a direct import.
func tipoImportavel(tipo string) bool {
	return tipo == "movie" || tipo == "tv" || tipo == "collection" || tipo == "artista" || tipo == "faixa"
}

// resolverSourceID returns the source_id to use: the one from the body (`sourceIDBody`),
// otherwise the entity's external_ids[source] (`externalIdsJson`). It responds 422 and
// returns ok=false when neither is available. Extracted from enrichEntity to keep the
// handler lean.
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

// provedorCapaz says whether the provider `p` knows how to import/enrich the requested
// `tipo`, via a type-assert of the corresponding optional capability:
// 'tv' requires provider.SerieProvider (the tree), 'collection' requires
// provider.ColecaoProvider; 'artista'/'album'/'faixa' require the music capability
// (ADR-0027) -- without it, a registered provider that does not model music (e.g.: TMDB)
// was wrongly accepted by the `default` below as "capable", and /enrich without an explicit
// `source` chose it as the default source for a music entity, hitting a 422 "no source_id"
// (the entity's external_ids[mbid] does not match external_ids[tmdb] that the handler
// was looking for) -- found while validating the "Download Metadata" button on
// artist/album, a gap left pre-existing when the "faixa" case was added. For 'movie' (and
// other grains, such as season/episode) the core Provider is enough — every registered
// provider fulfills it.
func provedorCapaz(p provider.Provider, tipo string) bool {
	switch tipo {
	case "tv":
		_, ok := p.(provider.SerieProvider)
		return ok
	case "collection":
		_, ok := p.(provider.ColecaoProvider)
		return ok
	case "artista":
		_, ok := p.(provider.MusicaProvider)
		return ok
	case "album", "faixa":
		_, ok := p.(provider.MusicaAlbumProvider)
		return ok
	default:
		return true
	}
}

// decodeOpcional decodes an OPTIONAL JSON body into v: an empty body (EOF) is ok
// (v stays at the zero value); a present but malformed body responds 422 and returns
// false. Used by enrich, whose body is optional.
func decodeOpcional(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	writeError(w, http.StatusUnprocessableEntity, "invalid_body", "corpo JSON inválido")
	return false
}

// externalIDDe extracts external_ids[source] from external_ids_json (string→string).
// Empty when absent/invalid. Used when enrich does not receive source_id in the
// body (item already matched by the scanner, id in the identity).
//
// "musicbrainz" is the only source whose key in external_ids diverges from the provider's
// Nome(): ADR-0027 stores the id under "mbid" (not "musicbrainz") in every other part of
// the code (internal/enrich/musica.go, external_ids.mbid in the frontend) -- found while
// validating "Download Metadata" without an explicit source_id on an already
// cataloged artist/album/track: without this deviation, the lookup of
// external_ids["musicbrainz"] never finds the already stored id and the button always hits
// a 422 "no source_id".
func externalIDDe(ns sql.NullString, source string) string {
	if !ns.Valid || ns.String == "" {
		return ""
	}
	m := map[string]any{}
	if json.Unmarshal([]byte(ns.String), &m) != nil {
		return ""
	}
	chave := source
	if source == "musicbrainz" {
		chave = "mbid"
	}
	if v, ok := m[chave].(string); ok {
		return v
	}
	return ""
}
