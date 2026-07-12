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

// Este arquivo implementa o SELETOR DE ORDENAÇÃO de episódios (briefing 0040): a
// descoberta das ordens disponíveis de uma série (anotada no detalhe, `orderings`) e
// o endpoint GET /entities/{id}/episodes?ordering=X, que devolve TODOS os episódios
// da série AGRUPADOS e ordenados pela ordem X — projetados na LEITURA a partir do
// `numbering` (ADR-0021: ordens alternativas NÃO viram entidade de temporada; são
// agrupadas aqui no server). Reusa a projeção do 0039 (entradaNumbering) e a varredura
// série→temporada→episódio das relações existentes. Nada aqui escreve (pool de leitura).

// episodeGroupsDTO é a resposta de GET /entities/{id}/episodes: a ordem efetivamente
// aplicada (`ordering`, já resolvida contra as disponíveis) e os grupos de episódios.
type episodeGroupsDTO struct {
	Ordering string            `json:"ordering"`
	Groups   []episodeGroupDTO `json:"groups"`
}

// episodeGroupDTO é um grupo (temporada da ORDEM pedida) com seu rótulo NÃO-navegável
// e os cards de episódio já com `numbering` projetado naquela ordem. Para `absolute` há
// um único grupo corrido (`season` 0, label "Episódios").
type episodeGroupDTO struct {
	Season   int             `json:"season"`
	Label    string          `json:"label"`
	Episodes []entityCardDTO `json:"episodes"`
}

// getEntityEpisodes trata GET /api/v1/entities/{id}/episodes?ordering=X: os episódios
// da SÉRIE agrupados/ordenados pela ordem X, projetados do `numbering` (0040). `ordering`
// ausente/inválido cai no default da série; `absolute` é uma ordem válida especial. 404
// quando a entity não existe; para uma entity que não é série, devolve grupos vazios (o
// endpoint é da série). O `?lang=` é aceito para paridade (o card usa o título base).
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

// escolherOrdering resolve a ordem pedida contra as disponíveis da série: devolve o
// param quando ele é uma ordem válida; senão (ausente/inválido) cai no default. Sem
// `orderings` (entity que não é série ou sem numbering) devolve o próprio param (os
// grupos sairão vazios de qualquer forma).
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

// montarOrderings descobre as ordens de exibição de uma SÉRIE (0040): varre os
// episódios descendentes, coleta as chaves de `numbering` e a presença de
// `absolute_number`, e lê o `default_season_type` da série (gravado no import,
// enrich/serie.go, e finalmente CONSUMIDO aqui). Devolve nil para não-`tv`, em falha
// de leitura, ou quando a série não tem nenhuma ordem (a UI então esconde o seletor).
// `Available` sai com o default PRIMEIRO, o resto em ordem alfabética e `absolute` ao
// fim; o default é o declarado quando presente, senão a primeira ordem encontrada.
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

// ordensDisponiveis monta a lista estável de ordens: as chaves de numbering em ordem
// alfabética + `absolute` ao fim (quando há absoluto), e então o DEFAULT movido para a
// primeira posição. O default é o declarado (`def`) quando ele é uma ordem real;
// senão a primeira ordem da lista (a série sempre tem um default coerente com a árvore).
// Lista vazia quando não há nenhuma ordem (série sem numbering).
func ordensDisponiveis(chaves map[string]bool, temAbsoluto bool, def string) []string {
	ordens := make([]string, 0, len(chaves)+1)
	for k := range chaves {
		// `numbering.absolute` (TheTVDB) e o campo `absolute_number` sao o MESMO eixo
		// absoluto: dobra na flag temAbsoluto para nao duplicar `absolute` na lista.
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

// contemOrdem diz se `alvo` está na lista de ordens.
func contemOrdem(ordens []string, alvo string) bool {
	for _, o := range ordens {
		if o == alvo {
			return true
		}
	}
	return false
}

// defaultSeasonType lê o `default_season_type` do extension_data da série (core.tv@1),
// gravado no import (enrich/serie.go extensaoSerie) mas até o 0040 nunca consumido.
// "" quando a série não tem o namespace ou o campo (o caller cai na 1ª ordem).
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

// episodiosDaSerie varre a árvore série→temporadas→episódios via as relações já
// existentes (ListarRelacoesFilhas) e devolve as entities dos episódios (com o
// extension_data, para projetar o numbering). É a varredura N+1 nas relações — aceitável
// para a escala atual (centenas de episódios/série, self-hosted); TODO: query agregada
// única se a biblioteca crescer (0040 §7, mesmo precedente do 0037/0039). Reusada pela
// descoberta de ordens e pelo agrupamento. Pula um episódio purgado no meio (NoRows).
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

// episodioProjetado é um card de episódio já com a numeração da ordem pedida, guardado
// junto do próprio numbering para ordenar por (season, number) antes de agrupar.
type episodioProjetado struct {
	card entityCardDTO
	num  *episodeNumberingDTO
}

// agruparEpisodios monta os grupos de episódios da série na ordem pedida (0040):
// projeta cada episódio na ordem (estrita para official/dvd, absoluta para `absolute`),
// ordena os numerados por (season, number) e agrupa por season. Episódio SEM numeração
// naquela ordem (draft, ou ordem que a fonte não catalogou) vai num grupo "Sem
// numeração" ao FINAL — presente mas separado, para o dono ver que existe sem inventar
// posição. `absolute` colapsa num único grupo corrido ("Episódios").
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
			continue // episódio purgado no meio
		}
		num := projetarParaOrdem(tvext.Namespace(ep.ExtensionDataJson, nsTVCore), ordering)
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

// antesNaOrdem ordena por (season, number): temporada primeiro, número em seguida. No
// absoluto todos têm season 0, então cai no número (o próprio absolute_number).
func antesNaOrdem(a, b *episodeNumberingDTO) bool {
	if a.Season != b.Season {
		return a.Season < b.Season
	}
	return a.Number < b.Number
}

// agruparPorSeason quebra a lista JÁ ordenada em grupos por season, com o rótulo
// não-navegável de cada grupo (0040): "Temporada N" na ordem normal, "Episódios" no
// absoluto (grupo único). Assume a entrada ordenada por (season, number).
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

// labelDoGrupo devolve o rótulo NÃO-navegável do cabeçalho do grupo: "Episódios" no
// absoluto (lista corrida única) e "Temporada N" na ordem por temporada.
func labelDoGrupo(season int, absoluto bool) string {
	if absoluto {
		return "Episódios"
	}
	return "Temporada " + strconv.Itoa(season)
}

// cardDeEpisodio carrega o card de um episódio (o MESMO shape de /members) a partir de
// ObterEntityCard. ok=false quando o episódio foi purgado no meio (NoRows) — o caller
// pula. Sem posse agregada (episódio é folha; a posse vem do `owned` do próprio card).
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
