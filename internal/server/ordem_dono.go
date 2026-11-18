// The OWNER ORDER endpoints (ADR-0033): the numbering the owner declares for a series
// whose release does not follow any provider order — including the case no provider can
// express, a numbered unit that MERGES several aired episodes (a DVD/streaming re-cut).
//
// STORAGE. It lives in the override layer (media_override.override_json, ADR-0004), in
// the same place a provider value would occupy:
//
//	{"extension_data": {"core.tv@1": {"owner_order": {…}}}}
//
// Two reasons: every re-enrich rewrites `extension_data` and would erase an owner
// number, and the override is the layer the resolver already treats as the final
// authority. The handler edits ONLY this key — the rest of the blob is preserved
// byte-for-byte, so the owner's other overrides survive.
//
// The projections that consume it do NOT read it from here: the scanner reads the
// override row when it builds the match index, and the order discovery/grouping read it
// per request (see montarOrderings and agruparEpisodios).
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/tvext"
)

// maxUnidadesDono caps the units of one owner order: it is a release (hundreds of
// entries), not a database dump. The cap protects the request size and the per-request
// projection cost.
const maxUnidadesDono = 3000

// maxEpisodiosPorUnidade caps how many episodes one unit may merge (a re-cut merges a
// handful; a huge number is a mistake, not a release).
const maxEpisodiosPorUnidade = 50

// ordemDonoDTO is the wire shape of the owner order. The field names are the same ones
// stored in extension_data, so the UI and the blob never diverge.
type ordemDonoDTO struct {
	Name    string           `json:"name"`
	BasedOn string           `json:"based_on"`
	Units   []unidadeDonoDTO `json:"units"`
}

// unidadeDonoDTO is one numbered unit: the (season, number) the owner sees in their
// release and the episodes it holds, in order.
type unidadeDonoDTO struct {
	Season   int      `json:"season"`
	Number   int      `json:"number"`
	Title    string   `json:"title,omitempty"`
	Episodes []string `json:"episodes"`
}

// getOrdemDoDono answers the owner order of the series: the declared one, or an empty
// order (units: []) when the owner never declared one. An entity that is not a series
// answers 422; a missing entity, 404.
func (s *Server) getOrdemDoDono(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}
	if ent.Type != tipoEntSerie {
		writeError(w, http.StatusUnprocessableEntity, "not_a_series", "ordem do dono só existe para série")
		return
	}
	writeJSON(w, http.StatusOK, ordemParaDTO(s.carregarOrdemDoDono(ctx, id)))
}

// putOrdemDoDono writes (or replaces) the owner order of the series. It validates
// everything BEFORE touching the database: the entity is a series, the units are not
// empty, the (season, number) pairs are unique, every episode id belongs to THIS series
// and no episode appears twice. Any failure leaves the previous order intact.
func (s *Server) putOrdemDoDono(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}
	if ent.Type != tipoEntSerie {
		writeError(w, http.StatusUnprocessableEntity, "not_a_series", "ordem do dono só existe para série")
		return
	}

	var body ordemDonoDTO
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	ordem, ok := s.validarOrdemDoDono(w, ctx, id, body)
	if !ok {
		return
	}

	blob, err := s.carregarBlobBruto(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	blobAtualizado, err := blobComOrdemDoDono(blob, ordem)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if err := s.gravarBlob(ctx, id, blobAtualizado); err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, ordemParaDTO(ordem))
}

// deleteOrdemDoDono removes the owner order of the series, keeping every other override
// of the blob. 204 even when there was no order (idempotent).
func (s *Server) deleteOrdemDoDono(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if _, err := s.leitura().ObterEntity(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}
	blob, err := s.carregarBlobBruto(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	blobAtualizado, err := blobSemOrdemDoDono(blob)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if err := s.gravarBlob(ctx, id, blobAtualizado); err != nil {
		respondErroInterno(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validarOrdemDoDono checks the body against the series and returns the CLEAN order
// (trimmed ids/titles, the name defaulting to "owner"). It writes the error and returns
// ok=false on the first violation — nothing is written until the whole body passes.
func (s *Server) validarOrdemDoDono(w http.ResponseWriter, ctx context.Context, serieID string, body ordemDonoDTO) (*tvext.OrdemDono, bool) {
	if len(body.Units) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "empty_units", "a ordem precisa de ao menos uma unidade")
		return nil, false
	}
	if len(body.Units) > maxUnidadesDono {
		writeError(w, http.StatusUnprocessableEntity, "too_many_units", "unidades demais na ordem")
		return nil, false
	}
	// The episodes of THIS series, to refuse an id from another work (or another type).
	eps, err := s.episodiosDaSerie(ctx, serieID)
	if err != nil {
		respondErroInterno(w)
		return nil, false
	}
	daSerie := make(map[string]bool, len(eps))
	for _, ep := range eps {
		daSerie[ep.ID] = true
	}

	validador := &validadorDeOrdem{daSerie: daSerie, vistos: map[string]bool{}, pares: map[[2]int]bool{}}
	ordem := &tvext.OrdemDono{Name: body.Name, BasedOn: body.BasedOn}
	for _, u := range body.Units {
		unidade, ok := validador.unidade(w, u)
		if !ok {
			return nil, false
		}
		ordem.Units = append(ordem.Units, unidade)
	}
	return ordem, true
}

// validadorDeOrdem carries the three sets that make a repeated (season, number),
// episode or unit detectable while the body is walked — state that would otherwise be
// four maps threaded through one long function.
type validadorDeOrdem struct {
	daSerie map[string]bool
	vistos  map[string]bool
	pares   map[[2]int]bool
}

// unidade validates ONE unit and returns it clean. It writes the error and returns
// ok=false on the first violation.
func (v *validadorDeOrdem) unidade(w http.ResponseWriter, u unidadeDonoDTO) (tvext.UnidadeDono, bool) {
	if u.Season < 0 || u.Number < 1 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_number", "temporada inválida ou número menor que 1")
		return tvext.UnidadeDono{}, false
	}
	par := [2]int{u.Season, u.Number}
	if v.pares[par] {
		writeError(w, http.StatusUnprocessableEntity, "duplicate_number", "duas unidades com a mesma temporada e número")
		return tvext.UnidadeDono{}, false
	}
	v.pares[par] = true
	if len(u.Episodes) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "empty_unit", "unidade sem episódio")
		return tvext.UnidadeDono{}, false
	}
	if len(u.Episodes) > maxEpisodiosPorUnidade {
		writeError(w, http.StatusUnprocessableEntity, "too_many_episodes", "episódios demais numa unidade")
		return tvext.UnidadeDono{}, false
	}
	unidade := tvext.UnidadeDono{Season: u.Season, Number: u.Number, Title: u.Title}
	for _, epID := range u.Episodes {
		if !v.daSerie[epID] {
			writeError(w, http.StatusUnprocessableEntity, "episode_not_in_series", "episódio não pertence a esta série")
			return tvext.UnidadeDono{}, false
		}
		if v.vistos[epID] {
			writeError(w, http.StatusUnprocessableEntity, "duplicate_episode", "episódio repetido na ordem")
			return tvext.UnidadeDono{}, false
		}
		v.vistos[epID] = true
		unidade.Episodes = append(unidade.Episodes, epID)
	}
	return unidade, true
}

// carregarOrdemDoDono reads the owner order of the series from the override layer. A
// read failure or an absent order is nil (the caller treats it as "no owner order").
func (s *Server) carregarOrdemDoDono(ctx context.Context, serieID string) *tvext.OrdemDono {
	ov, err := s.leitura().ObterMediaOverride(ctx, serieID)
	if err != nil {
		return nil
	}
	return tvext.OrdemDoDonoDoOverride(ov.OverrideJson)
}

// ordemParaDTO converts the parsed order to the wire shape (an absent order becomes an
// empty one, so the client always has the same shape to render).
func ordemParaDTO(o *tvext.OrdemDono) ordemDonoDTO {
	dto := ordemDonoDTO{Units: []unidadeDonoDTO{}}
	if o == nil {
		return dto
	}
	dto.Name = o.Name
	dto.BasedOn = o.BasedOn
	for _, u := range o.Units {
		dto.Units = append(dto.Units, unidadeDonoDTO{
			Season:   u.Season,
			Number:   u.Number,
			Title:    u.Title,
			Episodes: u.Episodes,
		})
	}
	return dto
}

// blobComOrdemDoDono returns a COPY of the override blob with the owner order written
// under extension_data → core.tv@1, preserving every other field, namespace and key.
func blobComOrdemDoDono(blob map[string]json.RawMessage, ordem *tvext.OrdemDono) (map[string]json.RawMessage, error) {
	ext, err := mapaAninhado(blob, "extension_data")
	if err != nil {
		return nil, err
	}
	if ext == nil {
		ext = map[string]json.RawMessage{}
	}
	ns, err := mapaAninhado(ext, tvext.NsTVCore)
	if err != nil {
		return nil, err
	}
	if ns == nil {
		ns = map[string]json.RawMessage{}
	}
	bruto, err := json.Marshal(ordemParaDTO(ordem))
	if err != nil {
		return nil, err
	}
	ns["owner_order"] = bruto
	if err := gravarMapaAninhado(ext, tvext.NsTVCore, ns); err != nil {
		return nil, err
	}
	if err := gravarMapaAninhado(blob, "extension_data", ext); err != nil {
		return nil, err
	}
	return blob, nil
}

// blobSemOrdemDoDono returns a COPY of the override blob without the owner order key. A
// namespace or a field left empty by the removal is removed too, so the blob goes back
// to what it was before the order existed (an empty blob ⇒ the row is dropped).
func blobSemOrdemDoDono(blob map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	ext, err := mapaAninhado(blob, "extension_data")
	if err != nil {
		return nil, err
	}
	if ext == nil {
		return blob, nil
	}
	ns, err := mapaAninhado(ext, tvext.NsTVCore)
	if err != nil {
		return nil, err
	}
	if ns == nil {
		return blob, nil
	}
	delete(ns, "owner_order")
	if len(ns) == 0 {
		delete(ext, tvext.NsTVCore)
	} else if err := gravarMapaAninhado(ext, tvext.NsTVCore, ns); err != nil {
		return nil, err
	}
	if len(ext) == 0 {
		delete(blob, "extension_data")
	} else if err := gravarMapaAninhado(blob, "extension_data", ext); err != nil {
		return nil, err
	}
	return blob, nil
}

// mapaAninhado decodes blob[chave] into a mutable map. Absent key → (nil, nil), which
// the callers treat as "nothing there yet". A value that is not an object is an error:
// overwriting it silently would drop another field.
func mapaAninhado(blob map[string]json.RawMessage, chave string) (map[string]json.RawMessage, error) {
	bruto, ok := blob[chave]
	if !ok {
		return nil, nil
	}
	if string(bruto) == "null" {
		return nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(bruto, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// gravarMapaAninhado writes map[chave] = m serialized, creating the path when needed.
func gravarMapaAninhado(blob map[string]json.RawMessage, chave string, m map[string]json.RawMessage) error {
	bruto, err := json.Marshal(m)
	if err != nil {
		return err
	}
	blob[chave] = bruto
	return nil
}

// numeracaoDoDono projects the owner numbering of ONE episode: the (season, number) of
// the unit that holds it, plus how many episodes that unit merges and the position of
// this episode inside it. Nil when the episode is in no unit (the caller then keeps it
// in the "without numbering" group, exactly as in any other order).
func numeracaoDoDono(numeros map[string]tvext.NumeroDono, epID string) *episodeNumberingDTO {
	n, ok := numeros[epID]
	if !ok {
		return nil
	}
	dto := &episodeNumberingDTO{Season: n.Season, Number: n.Number}
	if n.Episodes > 1 {
		// A merged unit: the UI labels the cards so the owner sees that this number is
		// shared by N episodes of the release.
		dto.MergedEpisodes = n.Episodes
		dto.MergedOrdinal = n.Ordinal + 1
	}
	return dto
}
