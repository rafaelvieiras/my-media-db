package tmdb

import (
	"sort"
	"strings"
)

// This file concentrates the CREDITS CANONICAL of TMDB (MD6 phase A): the structs
// of the cast/crew/company blocks (from append_to_response) and their mapping to the
// canonical keys `cast`/`crew`/`companies`. It is PURE (no network) -- it runs inside
// MapearDetalhe, reusing the same detail call as the other rich blocks. The MOVIE
// brings `credits` (flat cast/crew); the SERIES brings `aggregate_credits` (cast with
// `roles`, crew with `jobs`, aggregated per episode), which we normalize to the same
// scalar canonical format. `character`/`job` are scalar STRING (from the base
// language of the call) -- per-language is a follow-up.

// tetoElenco is the ceiling of CAST rows persisted per work (billing): huge lists
// (movie 76, series 781) are cut at the top-N by `order`. Crew and companies have no
// ceiling.
const tetoElenco = 50

// creditsTMDB is the `credits` block of the MOVIE (append in /movie/{id}): flat cast/crew.
type creditsTMDB struct {
	Cast []castMembroTMDB `json:"cast"`
	Crew []crewMembroTMDB `json:"crew"`
}

// castMembroTMDB is an actor of the MOVIE cast: id, name, character, billing order
// and photo.
type castMembroTMDB struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
}

// crewMembroTMDB is a MOVIE crew member: id, name, department, job and photo.
type crewMembroTMDB struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Department  string `json:"department"`
	Job         string `json:"job"`
	ProfilePath string `json:"profile_path"`
}

// aggregateCreditsTMDB is the `aggregate_credits` block of the SERIES (append in
// /tv/{id}): TMDB aggregates per episode -- the actor has several `roles`, the crew
// member several `jobs`, each one with its episode_count.
type aggregateCreditsTMDB struct {
	Cast []aggCastTMDB `json:"cast"`
	Crew []aggCrewTMDB `json:"crew"`
}

// aggCastTMDB is an actor of the AGGREGATED series cast: the roles (roles) and the
// total of episodes (total_episode_count), besides the billing order.
type aggCastTMDB struct {
	ID                int64       `json:"id"`
	Name              string      `json:"name"`
	Roles             []papelTMDB `json:"roles"`
	TotalEpisodeCount int         `json:"total_episode_count"`
	Order             int         `json:"order"`
	ProfilePath       string      `json:"profile_path"`
}

// papelTMDB is a role of an actor in the series (character + in how many episodes).
type papelTMDB struct {
	Character    string `json:"character"`
	EpisodeCount int    `json:"episode_count"`
}

// aggCrewTMDB is an AGGREGATED series crew member: the jobs (jobs) and the
// department.
type aggCrewTMDB struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Jobs        []jobTMDB `json:"jobs"`
	Department  string    `json:"department"`
	ProfilePath string    `json:"profile_path"`
}

// jobTMDB is a job of a crew member in the series (job + in how many episodes).
type jobTMDB struct {
	Job          string `json:"job"`
	EpisodeCount int    `json:"episode_count"`
}

// companyTMDB is a production company/studio of the detail (production_companies,
// already comes in /movie and in /tv without append).
type companyTMDB struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	LogoPath string `json:"logo_path"`
}

// enriquecerCreditos adds the credit keys to the canonical: `cast` (top-N cast by
// billing), `crew` (crew, dedup by person+job) and `companies` (production
// companies). It prefers the AGGREGATED format (series) when present, falling back to
// the flat one (movie). It only writes non-empty keys -- the consumer distinguishes
// "absent" from "empty".
func enriquecerCreditos(out map[string]any, d detalheTMDB) {
	if cast := elencoCanonico(d); len(cast) > 0 {
		out["cast"] = cast
	}
	if crew := equipeCanonica(d); len(crew) > 0 {
		out["crew"] = crew
	}
	if comps := produtorasCanonicas(d.ProductionCompanies); len(comps) > 0 {
		out["companies"] = comps
	}
}

// elencoCanonico returns the canonical cast: the aggregated one (series) when there
// is one, otherwise the flat one (movie). In both cases it already comes sorted by
// billing and cut at the ceiling.
func elencoCanonico(d detalheTMDB) []any {
	if len(d.AggregateCredits.Cast) > 0 {
		return elencoAgregado(d.AggregateCredits.Cast)
	}
	return elencoSimples(d.Credits.Cast)
}

// elencoSimples maps the MOVIE cast: it sorts by `order` (billing), discards those
// without a name and cuts at the ceiling. profile_path and episode_count are optional
// (a movie has no episode_count).
func elencoSimples(cast []castMembroTMDB) []any {
	ordenado := make([]castMembroTMDB, len(cast))
	copy(ordenado, cast)
	sort.SliceStable(ordenado, func(i, j int) bool { return ordenado[i].Order < ordenado[j].Order })
	out := make([]any, 0, tetoElenco)
	for _, m := range ordenado {
		if m.Name == "" {
			continue
		}
		item := map[string]any{
			"source_id": itoa(m.ID),
			"name":      m.Name,
			"character": m.Character,
			"order":     m.Order,
		}
		porFotoSePresente(item, m.ProfilePath)
		out = append(out, item)
		if len(out) >= tetoElenco {
			break
		}
	}
	return out
}

// elencoAgregado maps the SERIES cast (aggregate_credits): it normalizes the multiple
// `roles` of the actor into a single `character` joined by " / " and uses
// total_episode_count as episode_count. It sorts by billing and cuts at the ceiling.
func elencoAgregado(cast []aggCastTMDB) []any {
	ordenado := make([]aggCastTMDB, len(cast))
	copy(ordenado, cast)
	sort.SliceStable(ordenado, func(i, j int) bool { return ordenado[i].Order < ordenado[j].Order })
	out := make([]any, 0, tetoElenco)
	for _, m := range ordenado {
		if m.Name == "" {
			continue
		}
		item := map[string]any{
			"source_id":     itoa(m.ID),
			"name":          m.Name,
			"character":     personagemAgregado(m.Roles),
			"order":         m.Order,
			"episode_count": m.TotalEpisodeCount,
		}
		porFotoSePresente(item, m.ProfilePath)
		out = append(out, item)
		if len(out) >= tetoElenco {
			break
		}
	}
	return out
}

// personagemAgregado joins the characters of the multiple roles of the same actor with
// " / " (e.g. "Peter / Brian / Stewie (voice)"), discarding empty roles.
func personagemAgregado(roles []papelTMDB) string {
	nomes := make([]string, 0, len(roles))
	for _, r := range roles {
		if r.Character != "" {
			nomes = append(nomes, r.Character)
		}
	}
	return strings.Join(nomes, " / ")
}

// tetoEquipe bounds the persisted/displayed crew: the crew is LESS relevant than the
// cast and large productions bring hundreds of credits (Inception: 736). We curate the
// key jobs and cut the long tail (assistants etc.).
const tetoEquipe = 40

// rankCargo orders the jobs by relevance (lower = more important); TMDB uses the
// labels in English. Jobs outside fall into the rank of the department
// (rankDepartamentoCrew).
var rankCargo = map[string]int{
	"Director": 0, "Writer": 1, "Screenplay": 1, "Story": 2, "Novel": 2,
	"Producer": 3, "Executive Producer": 4, "Co-Producer": 5,
	"Director of Photography": 6, "Editor": 7,
	"Original Music Composer": 8, "Music": 8,
	"Production Design": 9, "Costume Design": 10,
}

// rankDepartamentoCrew is the base rank per department (for jobs outside rankCargo):
// the creative departments come first; the rest goes to the end.
var rankDepartamentoCrew = map[string]int{
	"Directing": 20, "Writing": 21, "Production": 22, "Camera": 23,
	"Editing": 24, "Sound": 25, "Art": 26,
}

// equipeCanonica returns the curated canonical crew: the aggregated one (series) when
// there is one, otherwise the flat one (movie), prioritized by job/department and cut
// at tetoEquipe.
func equipeCanonica(d detalheTMDB) []any {
	var crew []any
	if len(d.AggregateCredits.Crew) > 0 {
		crew = equipeAgregada(d.AggregateCredits.Crew)
	} else {
		crew = equipeSimples(d.Credits.Crew)
	}
	return curarEquipe(crew)
}

// curarEquipe sorts the crew by relevance (key job, otherwise department) STABLY
// (preserving the source order within the same rank) and limits it to tetoEquipe.
func curarEquipe(crew []any) []any {
	rank := func(m any) int {
		item, _ := m.(map[string]any)
		if r, ok := rankCargo[strTMDB(item["job"])]; ok {
			return r
		}
		if r, ok := rankDepartamentoCrew[strTMDB(item["department"])]; ok {
			return r
		}
		return 99
	}
	sort.SliceStable(crew, func(i, j int) bool { return rank(crew[i]) < rank(crew[j]) })
	if len(crew) > tetoEquipe {
		crew = crew[:tetoEquipe]
	}
	return crew
}

// strTMDB extracts a string from an any value of the canonical map (empty if absent/not a string).
func strTMDB(v any) string {
	s, _ := v.(string)
	return s
}

// equipeSimples maps the MOVIE crew: dedup by (source_id, job) -- the same person may
// have two jobs, each one becomes a row; exact repetitions are discarded.
func equipeSimples(crew []crewMembroTMDB) []any {
	vistos := map[string]bool{}
	out := make([]any, 0, len(crew))
	for _, m := range crew {
		if m.Name == "" {
			continue
		}
		chave := itoa(m.ID) + "|" + m.Job
		if vistos[chave] {
			continue
		}
		vistos[chave] = true
		item := map[string]any{
			"source_id":  itoa(m.ID),
			"name":       m.Name,
			"department": m.Department,
			"job":        m.Job,
		}
		porFotoSePresente(item, m.ProfilePath)
		out = append(out, item)
	}
	return out
}

// equipeAgregada maps the SERIES crew (aggregate_credits): it picks, among the `jobs`
// of the member, the job with the HIGHEST episode_count (the most relevant) and uses
// it as job + episode_count. Dedup by (source_id, job).
func equipeAgregada(crew []aggCrewTMDB) []any {
	vistos := map[string]bool{}
	out := make([]any, 0, len(crew))
	for _, m := range crew {
		if m.Name == "" {
			continue
		}
		job, eps := jobPrincipal(m.Jobs)
		chave := itoa(m.ID) + "|" + job
		if vistos[chave] {
			continue
		}
		vistos[chave] = true
		item := map[string]any{
			"source_id":     itoa(m.ID),
			"name":          m.Name,
			"department":    m.Department,
			"job":           job,
			"episode_count": eps,
		}
		porFotoSePresente(item, m.ProfilePath)
		out = append(out, item)
	}
	return out
}

// jobPrincipal returns the job with the HIGHEST episode_count of the member (and that
// counter). An empty list returns ("", 0).
func jobPrincipal(jobs []jobTMDB) (string, int) {
	melhor := ""
	maxEps := 0
	achou := false
	for _, j := range jobs {
		if !achou || j.EpisodeCount > maxEps {
			maxEps = j.EpisodeCount
			melhor = j.Job
			achou = true
		}
	}
	return melhor, maxEps
}

// produtorasCanonicas maps production_companies to the canonical `companies`
// ({source_id, name, logo_path?}), discarding those without a name.
func produtorasCanonicas(comps []companyTMDB) []any {
	out := make([]any, 0, len(comps))
	for _, c := range comps {
		if c.Name == "" {
			continue
		}
		item := map[string]any{
			"source_id": itoa(c.ID),
			"name":      c.Name,
		}
		if c.LogoPath != "" {
			item["logo_path"] = c.LogoPath
		}
		out = append(out, item)
	}
	return out
}

// porFotoSePresente writes profile_path in the credit item only when non-empty
// (optional in the canonical -- the consumer distinguishes absent from empty).
func porFotoSePresente(item map[string]any, profilePath string) {
	if profilePath != "" {
		item["profile_path"] = profilePath
	}
}
