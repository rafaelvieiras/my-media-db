package tmdb

// This file implements the ALTERNATIVE ORDERINGS of TMDB -- the "episode groups"
// (ADR-0033). TMDB's own tree numbers episodes by the official order only; the
// reorders the community created (DVD, absolute, digital, TV, story arc…) live in
// episode groups, two endpoints away:
//
//	GET /tv/{id}/episode_groups      → the list of groups of the series
//	GET /tv/episode_group/{group_id} → the items of ONE group
//
// The mapping to our model is direct: each SUB-GROUP of the group (a volume or a
// season) becomes the `season` of the numbering, and the position of the episode
// INSIDE the sub-group becomes its `number`. That is what the read-time projection
// of ADR-0021 expects — alternative orders never become season entities, they are
// only another entry in `numbering`.
//
// Two limits are normative here, both measured against the live API (2026-11-18,
// docs/13-episode-orderings-and-release-units.md):
//
//   - ONE ENTRY IS ONE EPISODE. There is no field to attach two episodes to one
//     entry (no combined/parent/sub-episodes), so a group can reorder but can never
//     merge. Merging is the owner's job (the owner order).
//   - GROUPS ARE COMMUNITY DATA. They may be absent, may be named loosely and the
//     `type` is chosen by whoever created the group — "Alternate Order" is filed as
//     type 1 (original air date) in The Office. So the KEY comes from the group NAME
//     (readable in the UI) and the type is only a hint for the import priority.
//
// Cost: one request for the list + one per group imported, capped by
// maxGruposTMDB, and only on the import of the tree (never on a read).

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// maxGruposTMDB caps how many episode groups are imported per series, so a
	// popular series (The Office has 10) does not multiply the requests and the
	// extension_data of every episode. The list is ordered by tipoPreferido.
	maxGruposTMDB = 8
	// grupoEpisodioTMDBPath is the path of one group, kept here because it is only
	// used by this file.
	grupoEpisodioTMDBPath = "/tv/episode_group/"
)

// tiposPreferidos is the import priority by group TYPE. The type is a hint chosen by
// the group creator, so this only decides WHICH groups win the cap — never the
// meaning of the data (that comes from the name and from the items themselves).
// 3 DVD, 2 absolute, 1 original air date, 7 TV, 4 digital, 6 production, 5 story arc.
var tiposPreferidos = map[int]int{3: 0, 2: 1, 1: 2, 7: 3, 4: 4, 6: 5, 5: 6}

// grupoResumoTMDB is one entry of /tv/{id}/episode_groups.
type grupoResumoTMDB struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         int    `json:"type"`
	GroupCount   int    `json:"group_count"`
	EpisodeCount int    `json:"episode_count"`
}

// respGruposTMDB wraps the list of groups ({results:[…]}).
type respGruposTMDB struct {
	Results []grupoResumoTMDB `json:"results"`
}

// grupoTMDB is the body of /tv/episode_group/{id}: the group plus its sub-groups.
type grupoTMDB struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Type         int            `json:"type"`
	GroupCount   int            `json:"group_count"`
	EpisodeCount int            `json:"episode_count"`
	Groups       []subgrupoTMDB `json:"groups"`
}

// subgrupoTMDB is a volume/season INSIDE the group: `Order` is the 1-based position
// of the sub-group (it is the `season` of our numbering) and the episodes come in
// the group's own sequence.
type subgrupoTMDB struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Order    int           `json:"order"`
	Episodes []grupoEpTMDB `json:"episodes"`
}

// grupoEpTMDB is one item of a sub-group. `Order` is 0-BASED and restarts in every
// sub-group — that is why the numbering is {Season: subgrupo.Order, Number: Order+1}.
// ID is the tmdb episode id, the anchor we already have from the tree.
type grupoEpTMDB struct {
	ID            int64  `json:"id"`
	Order         int    `json:"order"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"`
}

// mesclarGruposTMDB imports the episode groups of the series into the numbering of
// the episodes already built in `temporadas`, and returns the keys of the orders that
// were really added (one per group that matched at least one episode of the tree).
//
// BEST-EFFORT by design: a series without groups (404 or an empty list), a group that
// fails, or a group whose episodes are all absent from the tree degrade to "that
// order does not exist" — the tree keeps the official numbering and the import
// succeeds. Only errors that would already fail the tree propagate (there are none
// here: every call is wrapped).
func (c *Client) mesclarGruposTMDB(ctx context.Context, serieID string, temporadas []provider.TemporadaExterna) []string {
	resumos, err := c.listarGruposTMDB(ctx, serieID)
	if err != nil || len(resumos) == 0 {
		return nil
	}
	indice := indicePorTMDB(temporadas)
	if len(indice) == 0 {
		return nil
	}
	usados := chavesDeOrdemExistentes(temporadas)
	var ordens []string
	for _, resumo := range ordenarGrupos(resumos) {
		if len(ordens) >= maxGruposTMDB {
			break
		}
		grupo, err := c.buscarGrupoTMDB(ctx, resumo.ID)
		if err != nil {
			continue // one broken group never costs the other orders
		}
		chave := chaveDeOrdem(resumo, usados)
		n := 0
		for _, sub := range grupo.Groups {
			for _, item := range sub.Episodes {
				alvo, ok := indice[item.ID]
				if !ok {
					continue // episode of a season that this tree does not have
				}
				if alvo.Numbering == nil {
					alvo.Numbering = map[string]provider.Numero{}
				}
				alvo.Numbering[chave] = provider.Numero{Season: sub.Order, Number: item.Order + 1}
				n++
			}
		}
		if n == 0 {
			delete(usados, chave) // it matched nothing: the key is free again
			continue
		}
		usados[chave] = true
		ordens = append(ordens, chave)
	}
	return ordens
}

// listarGruposTMDB pulls the list of episode groups of the series. A series without
// groups answers 404 in some cases and an empty list in others — both are "no
// groups" (nil, nil), not an error.
func (c *Client) listarGruposTMDB(ctx context.Context, serieID string) ([]grupoResumoTMDB, error) {
	body, err := c.getBytes(ctx, "/tv/"+url.PathEscape(serieID)+"/episode_groups", nil)
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			return nil, nil
		}
		return nil, err
	}
	var resp respGruposTMDB
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// buscarGrupoTMDB pulls the items of ONE group.
func (c *Client) buscarGrupoTMDB(ctx context.Context, groupID string) (grupoTMDB, error) {
	body, err := c.getBytes(ctx, grupoEpisodioTMDBPath+url.PathEscape(groupID), nil)
	if err != nil {
		return grupoTMDB{}, err
	}
	var grupo grupoTMDB
	if err := json.Unmarshal(body, &grupo); err != nil {
		return grupoTMDB{}, err
	}
	return grupo, nil
}

// ordenarGrupos sorts the list by import priority (tipoPreferido) and then by size,
// so the cap keeps the most useful orders. The input order is preserved for ties.
func ordenarGrupos(resumos []grupoResumoTMDB) []grupoResumoTMDB {
	out := make([]grupoResumoTMDB, len(resumos))
	copy(out, resumos)
	sort.SliceStable(out, func(i, j int) bool {
		pi, oki := tiposPreferidos[out[i].Type]
		pj, okj := tiposPreferidos[out[j].Type]
		if !oki {
			pi = len(tiposPreferidos) + out[i].Type
		}
		if !okj {
			pj = len(tiposPreferidos) + out[j].Type
		}
		if pi != pj {
			return pi < pj
		}
		return out[i].EpisodeCount > out[j].EpisodeCount
	})
	return out
}

// indicePorTMDB builds the tmdb episode id → episode index across ALL the seasons of
// the tree (the group mixes seasons, so a per-season index is not enough).
func indicePorTMDB(temporadas []provider.TemporadaExterna) map[int64]*provider.EpisodioExterno {
	indice := map[int64]*provider.EpisodioExterno{}
	for ti := range temporadas {
		for ei := range temporadas[ti].Episodios {
			ep := &temporadas[ti].Episodios[ei]
			if id, err := strconv.ParseInt(ep.SourceID, 10, 64); err == nil {
				indice[id] = ep
			}
		}
	}
	return indice
}

// chavesDeOrdemExistentes collects the order keys already in use on the tree (the
// official one, at least), so a group can never overwrite an existing order.
func chavesDeOrdemExistentes(temporadas []provider.TemporadaExterna) map[string]bool {
	usados := map[string]bool{ordemOficial: true}
	for _, temp := range temporadas {
		for _, ep := range temp.Episodios {
			for k := range ep.Numbering {
				usados[k] = true
			}
		}
	}
	return usados
}

// chaveDeOrdem builds the order key of a group from its NAME ("Blu-ray Order" →
// "blu-ray-order"), because that is what the UI shows. An empty or symbol-only name
// falls back to "group-<type>". Collisions get a numeric suffix.
func chaveDeOrdem(resumo grupoResumoTMDB, usados map[string]bool) string {
	base := slug(resumo.Name)
	if base == "" {
		base = "group-" + strconv.Itoa(resumo.Type)
	}
	if !usados[base] {
		return base
	}
	for i := 2; ; i++ {
		candidata := base + "-" + strconv.Itoa(i)
		if !usados[candidata] {
			return candidata
		}
	}
}

// slug reduces a free name to a key: lowercase ASCII letters and digits, with single
// hyphens between words ("Blu-ray Order" → "blu-ray-order", "Ítens" → "itens").
func slug(nome string) string {
	var b strings.Builder
	ultimoHifen := true
	for _, r := range strings.ToLower(nome) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			ultimoHifen = false
		default:
			if ascii := desacentuar(r); ascii != "" {
				b.WriteString(ascii)
				ultimoHifen = false
				continue
			}
			if !ultimoHifen {
				b.WriteByte('-')
				ultimoHifen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 32 {
		out = strings.Trim(out[:32], "-")
	}
	return out
}

// desacentuar maps an accented rune to its ASCII form (only the letters a group name
// is likely to carry).
func desacentuar(r rune) string {
	switch r {
	case 'á', 'à', 'â', 'ã', 'ä':
		return "a"
	case 'é', 'è', 'ê', 'ë':
		return "e"
	case 'í', 'ì', 'î', 'ï':
		return "i"
	case 'ó', 'ò', 'ô', 'õ', 'ö':
		return "o"
	case 'ú', 'ù', 'û', 'ü':
		return "u"
	case 'ç':
		return "c"
	case 'ñ':
		return "n"
	}
	return ""
}
