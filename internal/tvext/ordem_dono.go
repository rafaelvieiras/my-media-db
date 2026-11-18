// The OWNER ORDER ("release order"): the numbering the owner declares for a series
// when no provider order matches the release they own — the only way to say "these
// three aired episodes are one numbered unit in my box set", which neither TMDB nor
// TheTVDB can express (ADR-0033, docs/13-episode-orderings-and-release-units.md).
//
// WHAT LIVES WHERE. The owner order is owner-declared data, so it lives in the
// OVERRIDE layer (media_override.override_json, ADR-0004) — not in the entity's
// extension_data, which every re-enrich rewrites. Its place inside the blob is the
// same namespace/key a provider value would occupy, so the resolver composes it like
// any other extension field:
//
//	{"extension_data": {"core.tv@1": {"owner_order": { … }}}}
//
// Readers that work off the RAW columns (the scanner's index, the order discovery)
// call OrdemDoDonoDoOverride with the override row directly.
package tvext

import (
	"database/sql"
	"encoding/json"
)

// NsTVCore is the namespace of the series vertical (ADR-0021). It is exported so the
// packages that read the RAW columns (scanner, server) name it the same way this one
// does.
const NsTVCore = "core.tv@1"

// ChaveOrdemDono is the KEY of the owner order in `numbering`/`season_types`. It is
// deliberately not a provider word (official/dvd/absolute/tmdb group slugs): the
// owner order always wins and must be recognizable on its own.
const ChaveOrdemDono = "owner"

// UnidadeDono is ONE numbered unit of the owner order. `Episodes` has 1..N episode
// ids and the ORDER IS MEANINGFUL (it is the `ordinal` of the file link): a unit with
// three episodes is a release entry that merged three aired episodes.
type UnidadeDono struct {
	Season   int
	Number   int
	Title    string
	Episodes []string
}

// OrdemDono is the owner order of a series: a name (free label), the provider order it
// was based on (for comparison) and the ordered units.
type OrdemDono struct {
	Name    string
	BasedOn string
	Units   []UnidadeDono
}

// Vazia tells whether the order carries no unit (nothing to project).
func (o *OrdemDono) Vazia() bool {
	return o == nil || len(o.Units) == 0
}

// NumeroDeEpisodio builds the episode id → numbering map of the order, which is what
// the readers need to project the owner numbering over the tree. The FIRST unit that
// mentions an episode wins (the writer refuses duplicates, so this only guards against
// data written by an older version).
func (o *OrdemDono) NumeroDeEpisodio() map[string]NumeroDono {
	out := map[string]NumeroDono{}
	if o == nil {
		return out
	}
	for _, u := range o.Units {
		for i, id := range u.Episodes {
			if _, existe := out[id]; existe {
				continue
			}
			out[id] = NumeroDono{Season: u.Season, Number: u.Number, Ordinal: i, Episodes: len(u.Episodes)}
		}
	}
	return out
}

// NumeroDono is the projected numbering of ONE episode in the owner order. `Episodes`
// is how many episodes the unit merges (1 = no merge) and `Ordinal` is the position of
// this episode inside the unit.
type NumeroDono struct {
	Season   int
	Number   int
	Ordinal  int
	Episodes int
}

// OrdemDoDonoDoOverride extracts the owner order from the RAW override blob
// (media_override.override_json). It returns nil when there is no override, when the
// blob is unreadable or when the key is absent/empty — every degradation is silent,
// because "no owner order" is the normal state.
func OrdemDoDonoDoOverride(raw sql.NullString) *OrdemDono {
	ns := namespaceDoOverride(raw)
	if ns == nil {
		return nil
	}
	return OrdemDoDono(ns)
}

// namespaceDoOverride walks override_json → extension_data → core.tv@1 and returns the
// namespace object, or nil.
func namespaceDoOverride(raw sql.NullString) map[string]any {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var blob map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw.String), &blob); err != nil {
		return nil
	}
	var ext map[string]json.RawMessage
	if bruto, ok := blob["extension_data"]; !ok || json.Unmarshal(bruto, &ext) != nil {
		return nil
	}
	var ns map[string]any
	if bruto, ok := ext[NsTVCore]; !ok || json.Unmarshal(bruto, &ns) != nil {
		return nil
	}
	return ns
}

// OrdemDoDono reads the `owner_order` of an already extracted core.tv@1 namespace (the
// shape the resolver composes). nil when absent or malformed.
func OrdemDoDono(ns map[string]any) *OrdemDono {
	if ns == nil {
		return nil
	}
	bruto, ok := ns["owner_order"].(map[string]any)
	if !ok {
		return nil
	}
	ordem := &OrdemDono{}
	if s, ok := bruto["name"].(string); ok {
		ordem.Name = s
	}
	if s, ok := bruto["based_on"].(string); ok {
		ordem.BasedOn = s
	}
	unidades, _ := bruto["units"].([]any)
	for _, u := range unidades {
		uni, ok := u.(map[string]any)
		if !ok {
			continue
		}
		season, sOK := AsInt(uni["season"])
		number, nOK := AsInt(uni["number"])
		if !sOK || !nOK {
			continue
		}
		ud := UnidadeDono{Season: season, Number: number}
		if s, ok := uni["title"].(string); ok {
			ud.Title = s
		}
		if eps, ok := uni["episodes"].([]any); ok {
			for _, e := range eps {
				if s, ok := e.(string); ok && s != "" {
					ud.Episodes = append(ud.Episodes, s)
				}
			}
		}
		if len(ud.Episodes) == 0 {
			continue // a unit without episodes numbers nothing
		}
		ordem.Units = append(ordem.Units, ud)
	}
	if len(ordem.Units) == 0 {
		return nil
	}
	return ordem
}
