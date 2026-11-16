package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
)

// This file implements the EPISODE ORDERING SELECTOR: the discovery of a series'
// available orders (annotated on the detail page, `orderings`) and the endpoint
// GET /entities/{id}/episodes?ordering=X, which returns ALL the series episodes GROUPED
// and sorted by order X — projected ON READ from `numbering` (ADR-0021: alternative
// orders do NOT become season entities; they are grouped here in the server). It reuses
// the projection of the episode numbering (entradaNumbering) and the
// series→season→episode sweep of the existing relations. Nothing here writes (read
// pool).

// episodeGroupsDTO is the response of GET /entities/{id}/episodes: the order actually
// applied (`ordering`, already resolved against the available ones) and the episode
// groups.
type episodeGroupsDTO struct {
	Ordering string            `json:"ordering"`
	Groups   []episodeGroupDTO `json:"groups"`
}

// episodeGroupDTO is a group (season of the REQUESTED order) with its NON-navigable
// label and the episode cards already carrying `numbering` projected onto that order. For
// `absolute` there is a single continuous group (`season` 0, label "Episódios").
type episodeGroupDTO struct {
	Season   int             `json:"season"`
	Label    string          `json:"label"`
	Episodes []entityCardDTO `json:"episodes"`
}

// getEntityEpisodes handles GET /api/v1/entities/{id}/episodes?ordering=X: the SERIES
// episodes grouped/sorted by order X, projected from `numbering`. An absent/invalid
// `ordering` falls back to the series default; `absolute` is a special valid order. 404
// when the entity does not exist; for an entity that is not a series, it returns empty
// groups (the endpoint belongs to the series). `?lang=` is accepted for parity (the card
// uses the base title).
func (s *Server) getEntityEpisodes(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
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

	orderings := s.montarOrderings(ctx, ent)
	ordering := escolherOrdering(r.URL.Query().Get("ordering"), orderings)
	groups, err := s.agruparEpisodios(ctx, ent, ordering)
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, episodeGroupsDTO{Ordering: ordering, Groups: groups})
}

// escolherOrdering resolves the requested order against the series' available ones: it
// returns the param when it is a valid order; otherwise (absent/invalid) it falls back to
// the default. Without `orderings` (an entity that is not a series, or without numbering)
// it returns the param itself (the groups will come out empty anyway).
func escolherOrdering(param string, o *resolver.OrderingsInfo) string {
	if o == nil {
		return param
	}
	for _, a := range o.Available {
		if a == param {
			return param
		}
	}
	return o.Default
}

// montarOrderings discovers the display orders of a SERIES: it sweeps the descendant
// episodes, collects the `numbering` keys and the presence of `absolute_number`, and
// reads the series' `default_season_type` (written at import, enrich/serie.go, and
// finally CONSUMED here). It returns nil for a non-`tv` entity, on a read failure, or
// when the series has no order at all (the UI then hides the selector). `Available` comes
// out with the default FIRST, the rest in alphabetical order and `absolute` at the end;
// the default is the declared one when present, otherwise the first order found.
func (s *Server) montarOrderings(ctx context.Context, ent sqlcgen.Entity) *resolver.OrderingsInfo {
	if ent.Type != tipoEntSerie {
		return nil
	}
	eps, err := s.episodiosDaSerie(ctx, ent.ID)
	if err != nil {
		return nil
	}
	chaves := map[string]bool{}
	temAbsoluto := false
	for _, ep := range eps {
		ns := tvext.Namespace(ep.ExtensionDataJson, nsTVCore)
		if ns == nil {
			continue
		}
		if numbering, ok := ns["numbering"].(map[string]any); ok {
			for k := range numbering {
				chaves[k] = true
			}
		}
		if _, ok := tvext.AsInt(ns["absolute_number"]); ok {
			temAbsoluto = true
		}
	}
	available := ordensDisponiveis(chaves, temAbsoluto, defaultSeasonType(ent))
	if len(available) == 0 {
		return nil
	}
	return &resolver.OrderingsInfo{Default: available[0], Available: available}
}

// ordensDisponiveis builds the stable list of orders: the numbering keys in alphabetical
// order + `absolute` at the end (when there is an absolute one), and then the DEFAULT
// moved to the first position. The default is the declared one (`def`) when it is a real
// order; otherwise the first order of the list (a series always has a default coherent
// with the tree). Empty list when there is no order at all (a series without
// numbering).
func ordensDisponiveis(chaves map[string]bool, temAbsoluto bool, def string) []string {
	ordens := make([]string, 0, len(chaves)+1)
	for k := range chaves {
		// `numbering.absolute` (TheTVDB) and the `absolute_number` field are the SAME
		// absolute axis: it folds into the temAbsoluto flag so `absolute` is not duplicated
		// in the list.
		if k == orderingAbsoluto {
			temAbsoluto = true
			continue
		}
		ordens = append(ordens, k)
	}
	sort.Strings(ordens)
	if temAbsoluto {
		ordens = append(ordens, orderingAbsoluto)
	}
	if len(ordens) == 0 {
		return nil
	}
	if def == "" || !contemOrdem(ordens, def) {
		def = ordens[0]
	}
	out := make([]string, 0, len(ordens))
	out = append(out, def)
	for _, o := range ordens {
		if o != def {
			out = append(out, o)
		}
	}
	return out
}

// contemOrdem tells whether `alvo` is in the list of orders.
func contemOrdem(ordens []string, alvo string) bool {
	for _, o := range ordens {
		if o == alvo {
			return true
		}
	}
	return false
}

// defaultSeasonType reads the `default_season_type` from the series extension_data
// (core.tv@1), written at import (enrich/serie.go extensaoSerie) but never consumed
// until now. "" when the series has neither the namespace nor the field (the caller
// falls back to the 1st order).
func defaultSeasonType(ent sqlcgen.Entity) string {
	ns := tvext.Namespace(ent.ExtensionDataJson, nsTVCore)
	if ns == nil {
		return ""
	}
	if v, ok := ns["default_season_type"].(string); ok {
		return v
	}
	return ""
}

// episodiosDaSerie sweeps the series→seasons→episodes tree through the existing
// relations (ListarRelacoesFilhas) and returns the episode entities (with the
// extension_data, to project the numbering). It is the N+1 sweep over the relations —
// acceptable at the current scale (hundreds of episodes/series, self-hosted); TODO: a
// single aggregate query if the library grows (the same precedent as the earlier
// episode/* sweeps). Reused by the order discovery and by the grouping. It skips an
// episode purged in the middle (NoRows).
func (s *Server) episodiosDaSerie(ctx context.Context, serieID string) ([]sqlcgen.Entity, error) {
	temporadas, err := s.leitura().ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		return nil, err
	}
	var eps []sqlcgen.Entity
	for _, ts := range temporadas {
		if ts.Kind != kindTemporada {
			continue
		}
		filhos, err := s.leitura().ListarRelacoesFilhas(ctx, ts.ChildID)
		if err != nil {
			return nil, err
		}
		for _, fe := range filhos {
			if fe.Kind != kindEpisodio {
				continue
			}
			ep, err := s.leitura().ObterEntity(ctx, fe.ChildID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return nil, err
			}
			eps = append(eps, ep)
		}
	}
	return eps, nil
}

// episodioProjetado is an episode card already carrying the requested-order numbering,
// kept together with that numbering to sort by (season, number) before grouping.
type episodioProjetado struct {
	card entityCardDTO
	num  *episodeNumberingDTO
}

// agruparEpisodios builds the episode groups of the series in the requested order: it
// projects each episode onto the order (strict for official/dvd, absolute for
// `absolute`), sorts the numbered ones by (season, number) and groups by season. An
// episode WITHOUT numbering in that order (a draft, or an order the source did not
// catalog) goes into a "Sem numeração" group at the END — present but separate, so the
// owner sees that it exists without a made-up position. `absolute` collapses into a
// single continuous group ("Episódios").
func (s *Server) agruparEpisodios(ctx context.Context, ent sqlcgen.Entity, ordering string) ([]episodeGroupDTO, error) {
	eps, err := s.episodiosDaSerie(ctx, ent.ID)
	if err != nil {
		return nil, err
	}
	absoluto := ordering == orderingAbsoluto
	var comNum []episodioProjetado
	var semNum []entityCardDTO
	for _, ep := range eps {
		card, ok, err := s.cardDeEpisodio(ctx, ep.ID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue // episode purged in the middle
		}
		// A single read of the namespace serves both numbering and still: the still gets a
		// ride on the existing walk, without multiplying queries against extension_data.
		ns := tvext.Namespace(ep.ExtensionDataJson, nsTVCore)
		card.StillURL = stillDoNS(ns)
		num := projetarParaOrdem(ns, ordering)
		card.Numbering = num
		if num == nil {
			semNum = append(semNum, card)
			continue
		}
		comNum = append(comNum, episodioProjetado{card: card, num: num})
	}
	sort.SliceStable(comNum, func(i, j int) bool { return antesNaOrdem(comNum[i].num, comNum[j].num) })
	groups := agruparPorSeason(comNum, absoluto)
	if len(semNum) > 0 {
		groups = append(groups, episodeGroupDTO{Season: 0, Label: "Sem numeração", Episodes: semNum})
	}
	return groups, nil
}

// antesNaOrdem sorts by (season, number): season first, then the number. In the absolute
// order everyone has season 0, so it falls back to the number (the absolute_number
// itself).
func antesNaOrdem(a, b *episodeNumberingDTO) bool {
	if a.Season != b.Season {
		return a.Season < b.Season
	}
	return a.Number < b.Number
}

// agruparPorSeason breaks the ALREADY sorted list into groups per season, with the
// non-navigable label of each group: "Temporada N" in the normal order, "Episódios" in
// the absolute one (a single group). It assumes the input sorted by (season, number).
func agruparPorSeason(itens []episodioProjetado, absoluto bool) []episodeGroupDTO {
	var groups []episodeGroupDTO
	for _, it := range itens {
		season := it.num.Season
		if len(groups) == 0 || groups[len(groups)-1].Season != season {
			groups = append(groups, episodeGroupDTO{Season: season, Label: labelDoGrupo(season, absoluto)})
		}
		g := &groups[len(groups)-1]
		g.Episodes = append(g.Episodes, it.card)
	}
	return groups
}

// labelDoGrupo returns the NON-navigable label of the group header: "Episódios" in the
// absolute one (a single continuous list) and "Temporada N" in the per-season order.
func labelDoGrupo(season int, absoluto bool) string {
	if absoluto {
		return "Episódios"
	}
	return "Temporada " + strconv.Itoa(season)
}

// cardDeEpisodio loads the card of an episode (the SAME shape as /members) from
// ObterEntityCard. ok=false when the episode was purged in the middle (NoRows) — the
// caller skips it. No aggregate ownership (an episode is a leaf; the ownership comes from
// the card's own `owned`).
func (s *Server) cardDeEpisodio(ctx context.Context, id string) (entityCardDTO, bool, error) {
	row, err := s.leitura().ObterEntityCard(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return entityCardDTO{}, false, nil
		}
		return entityCardDTO{}, false, err
	}
	return cardDTODeMembro(row), true, nil
}
