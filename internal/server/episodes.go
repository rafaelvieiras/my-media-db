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

	cards, err := s.cardsDeEpisodiosDaSerie(ctx, ent.ID)
	if err != nil {
		respondErroInterno(w)
		return
	}
	orderings := s.montarOrderingsDeCards(ctx, ent, cards)
	ordering := escolherOrdering(r.URL.Query().Get("ordering"), orderings)
	groups := s.agruparEpisodiosDeCards(ctx, ent.ID, ordering, cards)
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
	cards, err := s.cardsDeEpisodiosDaSerie(ctx, ent.ID)
	if err != nil {
		return nil
	}
	return s.montarOrderingsDeCards(ctx, ent, cards)
}

// montarOrderingsDeCards is montarOrderings over already loaded cards: the episode
// endpoint loads them once and shares them with the grouping.
func (s *Server) montarOrderingsDeCards(ctx context.Context, ent sqlcgen.Entity, cards []sqlcgen.ListarCardsDeEpisodiosDaSerieRow) *resolver.OrderingsInfo {
	if ent.Type != tipoEntSerie {
		return nil
	}
	chaves := map[string]bool{}
	temAbsoluto := false
	for _, ep := range cards {
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
	// The OWNER order (ADR-0033) is not in the episodes' numbering: it lives in the
	// override, and when it exists it is both available and the DEFAULT — it is the
	// numbering the owner actually sees in their release.
	if ordemDono := s.carregarOrdemDoDono(ctx, ent.ID); !ordemDono.Vazia() {
		chaves[tvext.ChaveOrdemDono] = true
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
	// The owner order, when present, is the default: it is the numbering of the
	// release the owner owns (ADR-0033).
	if chaves[tvext.ChaveOrdemDono] {
		def = tvext.ChaveOrdemDono
	}
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

// episodioProjetado is an episode card already carrying the requested-order numbering,
// kept together with that numbering to sort by (season, number) before grouping.
type episodioProjetado struct {
	card entityCardDTO
	num  *episodeNumberingDTO
}

// agruparEpisodiosDeCards builds the episode groups of the series in the requested order: it
// projects each episode onto the order (strict for official/dvd, absolute for
// `absolute`), sorts the numbered ones by (season, number) and groups by season. An
// episode WITHOUT numbering in that order (a draft, or an order the source did not
// catalog) goes into a "Sem numeração" group at the END — present but separate, so the
// owner sees that it exists without a made-up position. `absolute` collapses into a
// single continuous group ("Episódios").
func (s *Server) agruparEpisodiosDeCards(ctx context.Context, serieID, ordering string, cards []sqlcgen.ListarCardsDeEpisodiosDaSerieRow) []episodeGroupDTO {
	absoluto := ordering == orderingAbsoluto
	// In the owner order the numbering comes from the DECLARED UNITS, not from the
	// episodes: a unit may merge several episodes, and then all of them carry the same
	// (season, number) — the merge is visible and the order of the unit is the ordinal.
	var numerosDoDono map[string]tvext.NumeroDono
	if ordering == tvext.ChaveOrdemDono {
		numerosDoDono = s.carregarOrdemDoDono(ctx, serieID).NumeroDeEpisodio()
	}
	var comNum []episodioProjetado
	var semNum []entityCardDTO
	for _, ep := range cards {
		card := cardDeEpisodioDTO(ep)
		// A single read of the namespace serves both numbering and the still.
		ns := tvext.Namespace(ep.ExtensionDataJson, nsTVCore)
		card.StillURL = stillDoNS(ns)
		num := projetarParaOrdem(ns, ordering)
		if numerosDoDono != nil {
			num = numeracaoDoDono(numerosDoDono, ep.ID)
		}
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
	return groups
}

// cardsDeEpisodiosDaSerie loads the cards of every episode of the series in ONE query
// (ListarCardsDeEpisodiosDaSerie). It replaces the per-episode sweep: the relation walk
// plus one full row read per episode used to cost minutes on a big series served from a
// network share.
func (s *Server) cardsDeEpisodiosDaSerie(ctx context.Context, serieID string) ([]sqlcgen.ListarCardsDeEpisodiosDaSerieRow, error) {
	return s.leitura().ListarCardsDeEpisodiosDaSerie(ctx, serieID)
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
