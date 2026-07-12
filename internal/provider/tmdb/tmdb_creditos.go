package tmdb

import (
	"sort"
	"strings"
)

// Este arquivo concentra o CANONICO DE CREDITOS do TMDB (briefing 0029, MD6 Fase A):
// as structs dos blocos de elenco/equipe/produtoras (do append_to_response) e o
// mapeamento delas para as chaves canonicas `cast`/`crew`/`companies`. E PURO (sem
// rede) -- roda dentro de MapearDetalhe, reusando a mesma chamada de detalhe que os
// demais blocos ricos (0022). O FILME traz `credits` (cast/crew planos); a SERIE traz
// `aggregate_credits` (cast com `roles`, crew com `jobs`, agregados por episodio), que
// normalizamos ao mesmo formato canonico escalar. `character`/`job` sao STRING escalar
// (do idioma base da chamada) -- per-idioma e follow-up.

// tetoElenco e o teto de linhas de ELENCO persistidas por obra (bilhagem): listas
// enormes (filme 76, serie 781) sao cortadas nos top-N por `order`. Equipe e companies
// nao tem teto.
const tetoElenco = 50

// creditsTMDB e o bloco `credits` do FILME (append em /movie/{id}): cast/crew planos.
type creditsTMDB struct {
	Cast []castMembroTMDB `json:"cast"`
	Crew []crewMembroTMDB `json:"crew"`
}

// castMembroTMDB e um ator do elenco de FILME: id, nome, personagem, ordem de bilhagem
// e foto.
type castMembroTMDB struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
}

// crewMembroTMDB e um membro de equipe de FILME: id, nome, departamento, cargo e foto.
type crewMembroTMDB struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Department  string `json:"department"`
	Job         string `json:"job"`
	ProfilePath string `json:"profile_path"`
}

// aggregateCreditsTMDB e o bloco `aggregate_credits` da SERIE (append em /tv/{id}): o
// TMDB agrega por episodio -- o ator tem varios `roles`, o membro de equipe varios
// `jobs`, cada um com seu episode_count.
type aggregateCreditsTMDB struct {
	Cast []aggCastTMDB `json:"cast"`
	Crew []aggCrewTMDB `json:"crew"`
}

// aggCastTMDB e um ator do elenco AGREGADO de serie: os papeis (roles) e o total de
// episodios (total_episode_count), alem da ordem de bilhagem.
type aggCastTMDB struct {
	ID                int64       `json:"id"`
	Name              string      `json:"name"`
	Roles             []papelTMDB `json:"roles"`
	TotalEpisodeCount int         `json:"total_episode_count"`
	Order             int         `json:"order"`
	ProfilePath       string      `json:"profile_path"`
}

// papelTMDB e um papel de um ator na serie (personagem + em quantos episodios).
type papelTMDB struct {
	Character    string `json:"character"`
	EpisodeCount int    `json:"episode_count"`
}

// aggCrewTMDB e um membro de equipe AGREGADO de serie: os cargos (jobs) e o
// departamento.
type aggCrewTMDB struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Jobs        []jobTMDB `json:"jobs"`
	Department  string    `json:"department"`
	ProfilePath string    `json:"profile_path"`
}

// jobTMDB e um cargo de um membro de equipe na serie (cargo + em quantos episodios).
type jobTMDB struct {
	Job          string `json:"job"`
	EpisodeCount int    `json:"episode_count"`
}

// companyTMDB e uma produtora/estudio do detalhe (production_companies, ja vem no
// /movie e no /tv sem append).
type companyTMDB struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	LogoPath string `json:"logo_path"`
}

// enriquecerCreditos acrescenta ao canonico as chaves de credito: `cast` (elenco top-N
// por bilhagem), `crew` (equipe, dedup por pessoa+cargo) e `companies` (produtoras).
// Prefere o formato AGREGADO (serie) quando presente, caindo no plano (filme). So grava
// chaves nao-vazias -- o consumidor distingue "ausente" de "vazio".
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

// elencoCanonico devolve o elenco canonico: o agregado (serie) quando ha, senao o plano
// (filme). Em ambos os casos ja vem ordenado por bilhagem e cortado no teto.
func elencoCanonico(d detalheTMDB) []any {
	if len(d.AggregateCredits.Cast) > 0 {
		return elencoAgregado(d.AggregateCredits.Cast)
	}
	return elencoSimples(d.Credits.Cast)
}

// elencoSimples mapeia o elenco de FILME: ordena por `order` (bilhagem), descarta sem
// nome e corta no teto. profile_path e episode_count sao opcionais (filme nao tem
// episode_count).
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

// elencoAgregado mapeia o elenco de SERIE (aggregate_credits): normaliza os multiplos
// `roles` do ator num unico `character` unido por " / " e usa total_episode_count como
// episode_count. Ordena por bilhagem e corta no teto.
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

// personagemAgregado une os personagens dos multiplos papeis do mesmo ator com " / "
// (ex.: "Peter / Brian / Stewie (voz)"), descartando papeis vazios.
func personagemAgregado(roles []papelTMDB) string {
	nomes := make([]string, 0, len(roles))
	for _, r := range roles {
		if r.Character != "" {
			nomes = append(nomes, r.Character)
		}
	}
	return strings.Join(nomes, " / ")
}

// tetoEquipe limita a equipe persistida/exibida (0029): a equipe e MENOS relevante que o
// elenco e producoes grandes trazem centenas de creditos (Inception: 736). Curamos os
// cargos-chave e cortamos a cauda longa (assistentes etc.).
const tetoEquipe = 40

// rankCargo ordena os cargos por relevancia (menor = mais importante); o TMDB usa os
// rotulos em ingles. Cargos fora caem no rank do departamento (rankDepartamentoCrew).
var rankCargo = map[string]int{
	"Director": 0, "Writer": 1, "Screenplay": 1, "Story": 2, "Novel": 2,
	"Producer": 3, "Executive Producer": 4, "Co-Producer": 5,
	"Director of Photography": 6, "Editor": 7,
	"Original Music Composer": 8, "Music": 8,
	"Production Design": 9, "Costume Design": 10,
}

// rankDepartamentoCrew e o rank base por departamento (para cargos fora de rankCargo):
// os departamentos criativos vem antes; o resto vai para o fim.
var rankDepartamentoCrew = map[string]int{
	"Directing": 20, "Writing": 21, "Production": 22, "Camera": 23,
	"Editing": 24, "Sound": 25, "Art": 26,
}

// equipeCanonica devolve a equipe canonica curada: a agregada (serie) quando ha, senao a
// plana (filme), priorizada por cargo/departamento e cortada em tetoEquipe.
func equipeCanonica(d detalheTMDB) []any {
	var crew []any
	if len(d.AggregateCredits.Crew) > 0 {
		crew = equipeAgregada(d.AggregateCredits.Crew)
	} else {
		crew = equipeSimples(d.Credits.Crew)
	}
	return curarEquipe(crew)
}

// curarEquipe ordena a equipe por relevancia (cargo-chave, senao departamento) de forma
// ESTAVEL (preserva a ordem da fonte dentro do mesmo rank) e limita a tetoEquipe.
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

// strTMDB extrai uma string de um valor any do mapa canonico (vazio se ausente/nao-string).
func strTMDB(v any) string {
	s, _ := v.(string)
	return s
}

// equipeSimples mapeia a equipe de FILME: dedup por (source_id, job) -- a mesma pessoa
// pode ter dois cargos, cada um vira uma linha; repeticoes exatas sao descartadas.
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

// equipeAgregada mapeia a equipe de SERIE (aggregate_credits): escolhe, entre os `jobs`
// do membro, o cargo de MAIOR episode_count (o mais relevante) e o usa como job +
// episode_count. Dedup por (source_id, job).
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

// jobPrincipal devolve o cargo de MAIOR episode_count do membro (e esse contador). Lista
// vazia devolve ("", 0).
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

// produtorasCanonicas mapeia production_companies para o canonico `companies`
// ({source_id, name, logo_path?}), descartando sem nome.
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

// porFotoSePresente grava profile_path no item de credito so quando nao-vazio (opcional
// no canonico -- o consumidor distingue ausente de vazio).
func porFotoSePresente(item map[string]any, profilePath string) {
	if profilePath != "" {
		item["profile_path"] = profilePath
	}
}
