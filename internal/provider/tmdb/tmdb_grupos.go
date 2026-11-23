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
// Three limits are normative here, both measured against the live API (2026-11-18,
// docs/13-episode-orderings-and-release-units.md):
//
//   - ONE ENTRY IS ONE EPISODE. There is no field to attach two episodes to one
//     entry (no combined/parent/sub-episodes), so a group can reorder but can never
//     merge. Merging is the owner's job (the owner order).
//   - GROUPS ARE COMMUNITY DATA. They may be absent, may be named loosely and the
//     `type` is chosen by whoever created the group — "Alternate Order" is filed as
//     type 1 (original air date) in The Office. So the KEY comes from the group NAME
//     (readable in the UI) and the type is only a hint for the import priority.
//   - EVERY GROUP THE PROVIDER PUBLISHES IS IMPORTED (owner, 23/nov/2026: "sempre que
//     os metadados de uma série forem baixados, ela precisa baixar logo tudo
//     disponível no provedor, inclusive as variações de grupos"). The old cap of 8
//     silently dropped real orders on a popular series (The Office publishes 10), and
//     a dropped group and a missing group looked exactly the same. There is no cap
//     now: one request per group, and ONE RECORD PER GROUP in the returned tree.
//
// Cost: one request for the list + one per group, only on the import of the tree
// (never on a read).

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
	// episodeGroupTMDBPath is the path of one group, kept here because it is only used
	// by this file.
	episodeGroupTMDBPath = "/tv/episode_group/"
)

// preferredTypes is the import priority by group TYPE. The type is a hint chosen by the
// group creator, so this only decides the ORDER the groups are imported -- never the
// meaning of the data (that comes from the name and from the items themselves).
// 3 DVD, 2 absolute, 1 original air date, 7 TV, 4 digital, 6 production, 5 story arc.
var preferredTypes = map[int]int{3: 0, 2: 1, 1: 2, 7: 3, 4: 4, 6: 5, 5: 6}

// episodeGroupSummaryTMDB is one entry of /tv/{id}/episode_groups.
type episodeGroupSummaryTMDB struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         int    `json:"type"`
	GroupCount   int    `json:"group_count"`
	EpisodeCount int    `json:"episode_count"`
}

// episodeGroupsRespTMDB wraps the list of groups ({results:[…]}).
type episodeGroupsRespTMDB struct {
	Results []episodeGroupSummaryTMDB `json:"results"`
}

// episodeGroupTMDB is the body of /tv/episode_group/{id}: the group plus its
// sub-groups.
type episodeGroupTMDB struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Type         int            `json:"type"`
	GroupCount   int            `json:"group_count"`
	EpisodeCount int            `json:"episode_count"`
	Groups       []subgroupTMDB `json:"groups"`
}

// subgroupTMDB is a volume/season INSIDE the group: `Order` is the 1-based position of
// the sub-group (it is the `season` of our numbering) and the episodes come in the
// group's own sequence.
type subgroupTMDB struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Order    int             `json:"order"`
	Episodes []groupItemTMDB `json:"episodes"`
}

// groupItemTMDB is one item of a sub-group. `Order` is 0-BASED and restarts in every
// sub-group — that is why the numbering is {Season: subgroup.Order, Number: Order+1}.
// ID is the tmdb episode id, the anchor we already have from the tree.
type groupItemTMDB struct {
	ID            int64  `json:"id"`
	Order         int    `json:"order"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"`
}

// mergeEpisodeGroups imports the episode groups of the series into the numbering of the
// episodes already built in `seasons`, and returns the keys of the orders that were
// really added plus ONE RECORD PER GROUP THE PROVIDER PUBLISHED — mapped or not.
//
// BEST-EFFORT by design: a series without groups (404 or an empty list) or a group that
// fails degrade to "that order does not exist" — the tree keeps the official numbering
// and the import succeeds. No error propagates from here.
//
// An item is anchored by its tmdb episode id, the id the tree already carries. There is
// deliberately NO fallback by the official (season, episode) the item also carries: on a
// provisional tree those numbers came from the FILE NAMES, so anchoring by them would
// stamp an alternative ordering onto an episode that may be a different one. An
// unmapped group is RECORDED instead (provider.EpisodeGroup with Mapped=0 and Note),
// which is what makes "the source has no group" and "the group was dropped" countable.
func (c *Client) mergeEpisodeGroups(ctx context.Context, seriesID string, seasons []provider.TemporadaExterna) ([]string, []provider.EpisodeGroup) {
	summaries, err := c.listEpisodeGroups(ctx, seriesID)
	if err != nil || len(summaries) == 0 {
		return nil, nil
	}
	byID := episodeIDIndex(seasons)
	inUse := existingOrderKeys(seasons)
	seen := make([]provider.EpisodeGroup, 0, len(summaries))
	var orders []string
	for _, summary := range sortGroups(summaries) {
		group, err := c.fetchEpisodeGroup(ctx, summary.ID)
		if err != nil {
			seen = append(seen, provider.EpisodeGroup{
				SourceID: summary.ID, Name: summary.Name, Kind: strconv.Itoa(summary.Type),
				Episodes: summary.EpisodeCount, Note: "fetch failed",
			})
			continue
		}
		key := orderKey(summary, inUse)
		mapped, total := 0, 0
		for _, sub := range group.Groups {
			for _, item := range sub.Episodes {
				total++
				target, ok := byID[item.ID]
				if !ok {
					continue // an episode this tree does not have (a season removed upstream)
				}
				if target.Numbering == nil {
					target.Numbering = map[string]provider.Numero{}
				}
				target.Numbering[key] = provider.Numero{Season: sub.Order, Number: item.Order + 1}
				mapped++
			}
		}
		record := provider.EpisodeGroup{
			SourceID: group.ID, Name: summary.Name, Kind: strconv.Itoa(summary.Type),
			Episodes: total, Mapped: mapped,
		}
		if mapped == 0 {
			delete(inUse, key) // it matched nothing: the key is free again
			record.Note = unmappedNote(len(byID))
			seen = append(seen, record)
			continue
		}
		inUse[key] = true
		orders = append(orders, key)
		seen = append(seen, record)
	}
	return orders, seen
}

// unmappedNote says WHY a fetched group anchored nothing, so the record is readable
// without digging into the payload.
func unmappedNote(anchors int) string {
	if anchors == 0 {
		return "tree has no episode ids"
	}
	return "no item of the group is in the tree"
}

// listEpisodeGroups pulls the list of episode groups of the series. A series without
// groups answers 404 in some cases and an empty list in others — both are "no groups"
// (nil, nil), not an error.
func (c *Client) listEpisodeGroups(ctx context.Context, seriesID string) ([]episodeGroupSummaryTMDB, error) {
	body, err := c.getBytes(ctx, "/tv/"+url.PathEscape(seriesID)+"/episode_groups", nil)
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			return nil, nil
		}
		return nil, err
	}
	var resp episodeGroupsRespTMDB
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// fetchEpisodeGroup pulls the items of ONE group.
func (c *Client) fetchEpisodeGroup(ctx context.Context, groupID string) (episodeGroupTMDB, error) {
	body, err := c.getBytes(ctx, episodeGroupTMDBPath+url.PathEscape(groupID), nil)
	if err != nil {
		return episodeGroupTMDB{}, err
	}
	var group episodeGroupTMDB
	if err := json.Unmarshal(body, &group); err != nil {
		return episodeGroupTMDB{}, err
	}
	return group, nil
}

// sortGroups sorts the list by import priority (preferredTypes) and then by size, so the
// order keys are assigned deterministically (the same list always produces the same
// keys — that is what makes a re-import idempotent). The input order is preserved for
// ties.
func sortGroups(summaries []episodeGroupSummaryTMDB) []episodeGroupSummaryTMDB {
	out := make([]episodeGroupSummaryTMDB, len(summaries))
	copy(out, summaries)
	sort.SliceStable(out, func(i, j int) bool {
		pi, oki := preferredTypes[out[i].Type]
		pj, okj := preferredTypes[out[j].Type]
		if !oki {
			pi = len(preferredTypes) + out[i].Type
		}
		if !okj {
			pj = len(preferredTypes) + out[j].Type
		}
		if pi != pj {
			return pi < pj
		}
		return out[i].EpisodeCount > out[j].EpisodeCount
	})
	return out
}

// episodeIDIndex builds the tmdb episode id → episode index across ALL the seasons of
// the tree (the group mixes seasons, so a per-season index is not enough).
func episodeIDIndex(seasons []provider.TemporadaExterna) map[int64]*provider.EpisodioExterno {
	index := map[int64]*provider.EpisodioExterno{}
	for si := range seasons {
		for ei := range seasons[si].Episodios {
			ep := &seasons[si].Episodios[ei]
			// id 0 is what a source that omits the field produces after the tree parse --
			// it is not an anchor, and indexing it would turn "this tree has no ids" into a
			// misleading "the group is not in the tree" (measured by
			// TestTreeWithoutEpisodeIDsRecordsTheGroups).
			if id, err := strconv.ParseInt(ep.SourceID, 10, 64); err == nil && id != 0 {
				index[id] = ep
			}
		}
	}
	return index
}

// existingOrderKeys collects the order keys already in use on the tree (the official
// one, at least), so a group can never overwrite an existing order.
func existingOrderKeys(seasons []provider.TemporadaExterna) map[string]bool {
	inUse := map[string]bool{ordemOficial: true}
	for _, season := range seasons {
		for _, ep := range season.Episodios {
			for k := range ep.Numbering {
				inUse[k] = true
			}
		}
	}
	return inUse
}

// orderKey builds the order key of a group from its NAME ("Blu-ray Order" →
// "blu-ray-order"), because that is what the UI shows. An empty or symbol-only name
// falls back to "group-<type>". Collisions get a numeric suffix.
func orderKey(summary episodeGroupSummaryTMDB, inUse map[string]bool) string {
	base := slug(summary.Name)
	if base == "" {
		base = "group-" + strconv.Itoa(summary.Type)
	}
	if !inUse[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := base + "-" + strconv.Itoa(i)
		if !inUse[candidate] {
			return candidate
		}
	}
}

// slug reduces a free name to a key: lowercase ASCII letters and digits, with single
// hyphens between words ("Blu-ray Order" → "blu-ray-order", "Ítens" → "itens").
func slug(name string) string {
	var b strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if ascii := unaccent(r); ascii != "" {
				b.WriteString(ascii)
				lastHyphen = false
				continue
			}
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 32 {
		out = strings.Trim(out[:32], "-")
	}
	return out
}

// unaccent maps an accented rune to its ASCII form (only the letters a group name is
// likely to carry).
func unaccent(r rune) string {
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
