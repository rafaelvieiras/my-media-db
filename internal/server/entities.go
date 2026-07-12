package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
)

// Este arquivo implementa a superfície de LEITURA de /api/v1/entities (briefing
// 0010): lista paginada com posse derivada, detalhe resolvido, arquivos com
// mediainfo e o serviço do binário do pôster. TODAS as consultas passam pelo
// pool de LEITURA (ADR-0010) — nada aqui escreve. As respostas de /{id} já têm o
// shape final do resolver; a Fase 3 só preenche mais camadas.

// Limites de paginação: um default generoso para a UI e um teto para não deixar
// um cliente pedir a biblioteca inteira de uma vez.
const (
	limitPadrao = 50
	limitMaximo = 200
)

// entityCardDTO é um item da lista (GET /api/v1/entities): os campos de card mais
// a posse derivada, a contagem de arquivos, a confiança de match (badge de
// revisão) e o link do pôster (só quando há arte local).
type entityCardDTO struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	Owned           bool     `json:"owned"`
	FileCount       int64    `json:"file_count"`
	MatchConfidence *float64 `json:"match_confidence"`
	PosterURL       *string  `json:"poster_url"`
	// Ownership é a POSSE AGREGADA do card quando o tipo tem agregação na árvore de
	// séries (0036): `season` (episódios diretos) e opcionalmente `tv` (soma across
	// temporadas). Anotada no server pós-listagem em getEntityMembers — NÃO vem da
	// query SQL do card (mesma fronteira do 0034/Ancestors). `omitempty`: ausente
	// (null na UI) para movie/episode/collection, que usam a posse simples de `owned`.
	Ownership *resolver.OwnershipInfo `json:"ownership,omitempty"`
	// Numbering é a numeração REAL do episódio (0039): `season`/`number` projetados na
	// ordenação da temporada-pai (lidos do extension_data core.tv@1 do episódio) e o
	// `absolute_number` quando houver. Anotado no server em montarMembros — como
	// Ownership/Ancestors, fora da query do card. `omitempty`: ausente para todo tipo
	// que não seja `episode` e para episódio sem numbering (draft) — a UI cai no rótulo
	// neutro, sem inventar posição. Substitui a antiga posição ordinal do card.
	Numbering *episodeNumberingDTO `json:"numbering,omitempty"`
	// StillURL é a URL CRUA do still (frame 16:9) do episódio (0042), lida do
	// extension_data core.tv@1 (still_url) — artwork NEUTRO de idioma, gravado no import
	// sem baixar a imagem. O front a serve pelo image proxy (busca+cacheia sob demanda).
	// Anotada no server junto do Numbering (mesma leitura do extension_data, sem
	// multiplicar consultas). `omitempty`: ausente para não-`episode` e para episódio sem
	// still — a UI cai no placeholder.
	StillURL *string `json:"still_url,omitempty"`
}

// episodeNumberingDTO é a numeração real de um episódio no card (0039): `season` e
// `number` na ordenação da temporada-pai; `absolute_number` (opcional) quando a fonte
// informou o número absoluto (útil para anime/ordenação contínua). Distinto da POSIÇÃO
// na lista — reflete o número catalogado, mesmo com buracos na biblioteca.
type episodeNumberingDTO struct {
	Season         int  `json:"season"`
	Number         int  `json:"number"`
	AbsoluteNumber *int `json:"absolute_number,omitempty"`
}

// entityListDTO é a resposta paginada de GET /api/v1/entities.
type entityListDTO struct {
	Items  []entityCardDTO `json:"items"`
	Total  int64           `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// mediaFileDTO é um arquivo em GET /api/v1/entities/{id}/files. Mediainfo é o
// JSON já extraído pelo scanner (ffprobe) repassado cru — `null` quando ausente.
type mediaFileDTO struct {
	ID        string          `json:"id"`
	Path      string          `json:"path"`
	Size      *int64          `json:"size"`
	Edition   *string         `json:"edition"`
	PartNo    *int64          `json:"part_no"`
	State     string          `json:"state"`
	Mediainfo json.RawMessage `json:"mediainfo"`
}

// filesListDTO é a resposta de GET /api/v1/entities/{id}/files.
type filesListDTO struct {
	Items []mediaFileDTO `json:"items"`
}

// entitiesUnavailable responde 503 quando não há banco injetado (cenários de
// teste de rota). Espelha libraryUnavailable — protege contra nil deref.
func (s *Server) entitiesUnavailable(w http.ResponseWriter) bool {
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "db_unavailable",
			"persistência indisponível")
		return true
	}
	return false
}

// leitura devolve os Queries ligados ao POOL DE LEITURA (ADR-0010). Todas as
// rotas deste arquivo usam só esta conexão — nenhuma escrita.
func (s *Server) leitura() *sqlcgen.Queries { return sqlcgen.New(s.db.Read) }

// posterPath monta a referência (relativa) do endpoint que serve o pôster local
// de uma entity. Fonte única do template — usada na lista e no detalhe.
func posterPath(id string) string {
	return "/api/v1/entities/" + id + "/assets/poster"
}

// fanartPath monta a referência do endpoint que serve o FUNDO (fanart/backdrop)
// local de uma entity — o análogo do pôster para o hero do detalhe rico.
func fanartPath(id string) string {
	return "/api/v1/entities/" + id + "/assets/fanart"
}

// stillPath monta a referência do endpoint que serve o STILL (frame 16:9) custom
// do EPISÓDIO (0044) — o análogo do pôster/fanart para o still `user` do dono.
func stillPath(id string) string {
	return "/api/v1/entities/" + id + "/assets/still"
}

// artesLocais devolve as rotas locais do pôster e do FUNDO quando há asset servível
// daquele kind (precedência preferida→user→external, 0017); nil quando não há. Fonte
// única para o resolver saber que a entity tem arte local a servir.
func artesLocais(assets []sqlcgen.Asset, id string) (poster, backdrop *string) {
	if _, ok := escolherAssetServido(assets, kindPoster); ok {
		u := posterPath(id)
		poster = &u
	}
	if _, ok := escolherAssetServido(assets, kindFanart); ok {
		u := fanartPath(id)
		backdrop = &u
	}
	return poster, backdrop
}

// listEntities trata GET /api/v1/entities: lista paginada com filtros opcionais
// (type, owned), ordenação por título (sort=title|-title) e a posse/contagem/
// confiança derivadas numa ÚNICA query (sem N+1).
func (s *Server) listEntities(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	q := r.URL.Query()

	limit, offset, ok := parsePaginacao(w, q)
	if !ok {
		return
	}
	tipoFiltro := parseTipoFiltro(q)
	ownedFiltro, ok := parseOwnedFiltro(w, q)
	if !ok {
		return
	}
	desc, ok := parseSort(w, q)
	if !ok {
		return
	}

	ctx := r.Context()
	// Valida o ?lang= contra os permitidos (400 quando fora). Os cards da lista
	// servem o título NEUTRO (arquivo) — a resolução por idioma é do detalhe
	// (/entities/{id}); aqui o lang só é validado para um contrato coerente.
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if _, ok := s.parseLang(w, cfg, q.Get("lang")); !ok {
		return
	}
	total, err := s.leitura().ContarEntityCards(ctx, sqlcgen.ContarEntityCardsParams{
		Type:  tipoFiltro,
		Owned: ownedFiltro,
	})
	if err != nil {
		respondErroInterno(w)
		return
	}

	rows, err := s.listarCards(ctx, tipoFiltro, ownedFiltro, int64(limit), int64(offset), desc)
	if err != nil {
		respondErroInterno(w)
		return
	}

	items := make([]entityCardDTO, 0, len(rows))
	for _, row := range rows {
		card := cardDTO(row)
		// Posse RASA por tipo (0036/0037): season→episódios, tv→temporadas. Anotada
		// pós-listagem (a query SQL do card não muda); degrada para null em falha.
		s.anotarPosseCard(ctx, &card)
		items = append(items, card)
	}
	writeJSON(w, http.StatusOK, entityListDTO{Items: items, Total: total, Limit: limit, Offset: offset})
}

// listarCards escolhe a query pela direção de ordenação (o sqlc não liga
// parâmetro dentro de ORDER BY, então há uma query por direção) e normaliza o
// resultado para o mesmo tipo de linha.
func (s *Server) listarCards(ctx context.Context, tipo, owned any, limit, offset int64, desc bool) ([]sqlcgen.ListarEntityCardsRow, error) {
	if desc {
		rows, err := s.leitura().ListarEntityCardsDesc(ctx, sqlcgen.ListarEntityCardsDescParams{
			Type: tipo, Owned: owned, Limit: limit, Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		out := make([]sqlcgen.ListarEntityCardsRow, len(rows))
		for i, r := range rows {
			out[i] = sqlcgen.ListarEntityCardsRow(r)
		}
		return out, nil
	}
	return s.leitura().ListarEntityCards(ctx, sqlcgen.ListarEntityCardsParams{
		Type: tipo, Owned: owned, Limit: limit, Offset: offset,
	})
}

// cardDTO converte uma linha da query de cards no DTO da API, montando o
// poster_url só quando há arte local.
func cardDTO(row sqlcgen.ListarEntityCardsRow) entityCardDTO {
	dto := entityCardDTO{
		ID:              row.ID,
		Type:            row.Type,
		Title:           row.Title,
		Status:          row.Status,
		Owned:           row.Owned,
		FileCount:       row.FileCount,
		MatchConfidence: floatPtr(row.MatchConfidence),
	}
	if row.HasPoster {
		u := posterPath(row.ID)
		dto.PosterURL = &u
	}
	return dto
}

// kindColecao é o `kind` da entity_relation que liga uma coleção aos seus filmes
// (briefing 0025). Espelha o relacaoColecao do serviço enrich (fontes distintas, mesmo
// valor — a ligação nasce lá e é lida aqui).
const kindColecao = "collection"

// Tipos de entity-pai e os kinds de relação dos seus filhos na árvore de séries
// (briefing 0031 / ADR-0021). A leitura de membros escolhe o kind e a ORDENAÇÃO pelo
// tipo do pai: coleção ordena por ANO; série/temporada seguem o sort_index (número da
// temporada/episódio), a ordem que ListarRelacoesFilhas já devolve.
const (
	tipoEntSerie     = "tv"
	tipoEntTemporada = "season"
	kindTemporada    = "tv_season"
	kindEpisodio     = "tv_episode"
	// nsTVCore é o namespace do extension_data da vertical de séries (core.tv@1): a
	// temporada guarda `ordering`/`season_number`, o episódio guarda `numbering`
	// (ordem→{season,number}) + `absolute_number`. Lido aqui para o numbering do card
	// (0039); a ESCRITA nasce no enrich (mesmo valor, fontes distintas).
	nsTVCore = "core.tv@1"
	// orderingAbsoluto é a ordem VIRTUAL do seletor (0040) que numera os episódios
	// pelo `absolute_number` (contínuo através das temporadas, anime), num grupo único
	// — não é uma chave de `numbering`, é derivada da presença de absolute_number.
	orderingAbsoluto = "absolute"
)

// membroCard carrega o card já montado + o release_date do membro, só para ordenar por
// ano sem vazar o campo no DTO (o card de membro tem o MESMO shape do card da lista).
type membroCard struct {
	dto entityCardDTO
	ano sql.NullString
}

// getEntityMembers trata GET /api/v1/entities/{id}/members: os MEMBROS de uma coleção
// (briefing 0025) como cards (o MESMO shape de GET /entities), ordenados por ANO
// ascendente (sem ano ao fim). `[]` quando a entity não é coleção ou não tem membros;
// 404 quando a entity não existe. O `?lang=` é aceito para paridade, mas o card usa o
// título BASE (como a lista) — a localização por idioma do card é follow-up.
func (s *Server) getEntityMembers(w http.ResponseWriter, r *http.Request) {
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

	out, err := s.montarMembros(ctx, id, ent.Type)
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// montarMembros carrega os FILHOS de uma entity-pai como cards (o MESMO shape de GET
// /entities). O kind da relação e a ORDENAÇÃO dependem do tipo do pai (kindEOrdemMembros):
// coleção → membros por ANO; série → temporadas na ordem de sort_index; temporada →
// episódios numerados na ordem de sort_index (a ordem que ListarRelacoesFilhas já
// devolve). `[]` quando a entity não tem filhos dessa relação. É o caminho único reusado
// pela leitura (getEntityMembers) e pelas mutações da Fase B (add/removeMember). Pula um
// filho purgado no meio (NoRows).
func (s *Server) montarMembros(ctx context.Context, id, tipoPai string) ([]entityCardDTO, error) {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, id)
	if err != nil {
		return nil, err
	}
	kind, ordenarPorAno := kindEOrdemMembros(tipoPai)
	// Quando o pai é uma temporada, os episódios ganham o numbering REAL (0039) projetado
	// na ordenação da PRÓPRIA temporada — lida uma única vez do extension_data dela.
	ordem, seasonNumber := s.ordenacaoDaTemporada(ctx, id, tipoPai)
	membros := make([]membroCard, 0, len(rels))
	for _, rel := range rels {
		if rel.Kind != kind {
			continue
		}
		row, err := s.leitura().ObterEntityCard(ctx, rel.ChildID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // filho purgado no meio: ignora
			}
			return nil, err
		}
		card := cardDTODeMembro(row)
		s.anotarPosseCard(ctx, &card)
		s.anotarNumbering(ctx, &card, ordem, seasonNumber)
		membros = append(membros, membroCard{dto: card, ano: row.ReleaseDate})
	}
	// Série/temporada preservam a ordem de sort_index (número); só a coleção reordena por
	// ano — os episódios devem ficar 1,2,3 e não por data de exibição.
	if ordenarPorAno {
		sort.SliceStable(membros, func(i, j int) bool { return anoAntes(membros[i].ano, membros[j].ano) })
	}

	out := make([]entityCardDTO, len(membros))
	for i, m := range membros {
		out[i] = m.dto
	}
	return out, nil
}

// ordenacaoDaTemporada devolve a ordenação default (`ordering`) e o `season_number` da
// temporada-pai, lidos do extension_data dela (core.tv@1, gravado no import por
// extensaoTemporada). Só faz sentido quando o pai é `season`; para os demais tipos
// devolve ("",0) e o numbering não é anotado. Degrada silenciosamente ("",0) se a
// temporada não existir ou não tiver o namespace — o card cai no rótulo neutro.
func (s *Server) ordenacaoDaTemporada(ctx context.Context, seasonID, tipoPai string) (ordering string, seasonNumber int) {
	if tipoPai != tipoEntTemporada {
		return "", 0
	}
	ent, err := s.leitura().ObterEntity(ctx, seasonID)
	if err != nil {
		return "", 0
	}
	ns := tvext.Namespace(ent.ExtensionDataJson, nsTVCore)
	if ns == nil {
		return "", 0
	}
	if o, ok := ns["ordering"].(string); ok {
		ordering = o
	}
	if n, ok := tvext.AsInt(ns["season_number"]); ok {
		seasonNumber = n
	}
	return ordering, seasonNumber
}

// anotarNumbering preenche card.Numbering (numeração REAL do episódio, 0039) e
// card.StillURL (frame 16:9, 0042) a partir do MESMO extension_data do episódio: uma
// única leitura serve os dois (o still ganhou carona no walk do numbering, sem
// multiplicar consultas — 0042). Só age em cards `episode` (leitura extra por episódio,
// escala de dezenas por temporada, aceitável; TODO: query agregada única se pesar, ver
// 0039 §7). Degrada em qualquer falha (a UI omite número/still, nunca inventa).
func (s *Server) anotarNumbering(ctx context.Context, card *entityCardDTO, ordering string, seasonNumber int) {
	if card.Type != tipoEntEpisodio {
		return
	}
	ent, err := s.leitura().ObterEntity(ctx, card.ID)
	if err != nil {
		return
	}
	ns := tvext.Namespace(ent.ExtensionDataJson, nsTVCore)
	card.Numbering = projetarNumbering(ns, ordering, seasonNumber)
	// Precedência do still (0044): still custom (asset `user`) vence o cru do provider.
	// Leitura extra por episódio (aceitável, mesma escala do numbering — 0042/0039 §7).
	assets, _ := s.leitura().ListarAssetsPorEntity(ctx, card.ID)
	card.StillURL = stillDoEpisodio(assets, card.ID, ns)
}

// stillDoNS extrai a URL CRUA do still (0042) do namespace core.tv@1 do episódio:
// `still_url` quando é string não-vazia, senão nil (episódio sem still → placeholder na
// UI). Ponto ÚNICO reusado pelo card (/members e /episodes) e pelo detalhe do episódio.
func stillDoNS(ns map[string]any) *string {
	if ns == nil {
		return nil
	}
	if u, ok := ns["still_url"].(string); ok && u != "" {
		return &u
	}
	return nil
}

// projetarNumbering é a projeção PURA do numbering do episódio na ordenação da
// temporada-pai (0039). Preferência: a entrada ESTRITA `numbering[ordering]` (via
// entradaNumbering). Fallback (0039): sem entrada para a ordenação, mas com
// `absolute_number`, usa o absoluto como número (Season = o da temporada-pai). Sem
// nenhum dos dois (draft) → nil (rótulo neutro na UI). O fallback ao absoluto é o
// que distingue este da projeção do seletor (0040), que agrupa por ordem ESTRITA.
func projetarNumbering(ns map[string]any, ordering string, seasonNumber int) *episodeNumberingDTO {
	if dto := entradaNumbering(ns, ordering); dto != nil {
		return dto
	}
	if ns == nil {
		return nil
	}
	if a, ok := tvext.AsInt(ns["absolute_number"]); ok {
		return &episodeNumberingDTO{Season: seasonNumber, Number: a}
	}
	return nil
}

// entradaNumbering extrai a numeração ESTRITA do episódio para uma ordenação: a
// entrada `numbering[ordering]` → {season,number} (+ o absoluto quando houver). nil
// quando NÃO há entrada para aquela ordem — sem fallback ao absoluto (o caller
// decide o que fazer com o episódio "sem numeração naquela ordem"). É o núcleo
// reusado pela projeção do 0039 (com fallback) e pelo seletor do 0040 (estrito).
func entradaNumbering(ns map[string]any, ordering string) *episodeNumberingDTO {
	if ns == nil {
		return nil
	}
	var abs *int
	if a, ok := tvext.AsInt(ns["absolute_number"]); ok {
		abs = &a
	}
	numbering, ok := ns["numbering"].(map[string]any)
	if !ok {
		return nil
	}
	par, ok := numbering[ordering].(map[string]any)
	if !ok {
		return nil
	}
	season, sOK := tvext.AsInt(par["season"])
	number, nOK := tvext.AsInt(par["number"])
	if !sOK || !nOK {
		return nil
	}
	return &episodeNumberingDTO{Season: season, Number: number, AbsoluteNumber: abs}
}

// numeracaoAbsoluta projeta o episódio na ordem ABSOLUTA (0040): number =
// absolute_number, sem temporada (Season 0 → um grupo único corrido). nil quando o
// episódio não tem absolute_number (cai no grupo "Sem numeração").
func numeracaoAbsoluta(ns map[string]any) *episodeNumberingDTO {
	if ns == nil {
		return nil
	}
	a, ok := tvext.AsInt(ns["absolute_number"])
	if !ok {
		return nil
	}
	return &episodeNumberingDTO{Season: 0, Number: a, AbsoluteNumber: &a}
}

// projetarParaOrdem escolhe a projeção do episódio conforme a ordem pedida: a
// ABSOLUTA usa numeracaoAbsoluta (grupo único); qualquer outra usa a entrada ESTRITA
// `numbering[ordering]`. Em ambos os casos nil = episódio "sem numeração nesta ordem".
func projetarParaOrdem(ns map[string]any, ordering string) *episodeNumberingDTO {
	if ordering == orderingAbsoluto {
		return numeracaoAbsoluta(ns)
	}
	return entradaNumbering(ns, ordering)
}

// kindEOrdemMembros escolhe o kind de relação dos filhos e se eles são reordenados por
// ANO, conforme o tipo do pai: série → temporadas (sort_index); temporada → episódios
// (sort_index); qualquer outro (coleção) → membros por ano. O default preserva o
// comportamento de coleção para os callers que não são da árvore de séries.
func kindEOrdemMembros(tipoPai string) (kind string, ordenarPorAno bool) {
	switch tipoPai {
	case tipoEntSerie:
		return kindTemporada, false
	case tipoEntTemporada:
		return kindEpisodio, false
	default:
		return kindColecao, true
	}
}

// cardDTODeMembro reusa o cardDTO da lista a partir do row de ObterEntityCard (mesmas
// colunas derivadas + release_date), descartando o release_date (usado só p/ ordenar).
func cardDTODeMembro(row sqlcgen.ObterEntityCardRow) entityCardDTO {
	return cardDTO(sqlcgen.ListarEntityCardsRow{
		ID:              row.ID,
		Type:            row.Type,
		Title:           row.Title,
		Status:          row.Status,
		Owned:           row.Owned,
		FileCount:       row.FileCount,
		MatchConfidence: row.MatchConfidence,
		HasPoster:       row.HasPoster,
	})
}

// anotarPosseCard preenche o card com a posse RASA (1 nível) — "quanto do nível de
// baixo eu tenho" — quando o tipo tem agregação na árvore de séries:
//   - `season` (0036): episódios diretos com arquivo / total de episódios.
//   - `tv` (0037): temporadas COM conteúdo / total de temporadas — NÃO a soma
//     profunda de episódios. Isso torna o card sempre raso e consistente (glanceável),
//     enquanto o DETALHE (ResolvedEntity/montarPosseAgregada) permanece PROFUNDO
//     (episódios, ex. 2/875) e não muda.
//
// Cada leitura degrada para nil em qualquer falha: a UI apenas omite o selo, NUNCA
// derruba a lista. Demais tipos (movie/episode/collection) seguem sem ownership.
func (s *Server) anotarPosseCard(ctx context.Context, card *entityCardDTO) {
	switch card.Type {
	case tipoEntTemporada:
		if info, err := s.posseDaTemporada(ctx, card.ID); err == nil {
			card.Ownership = info
		}
	case tipoEntSerie:
		card.Ownership = s.posseRasaDaSerie(ctx, card.ID)
	}
}

// posseRasaDaSerie calcula a posse RASA do card de SÉRIE (0037): total = nº de filhos
// tv_season; owned = quantas dessas temporadas têm ao menos um episódio na biblioteca
// (posseDaTemporada().owned > 0). É a contagem GLANCEÁVEL do card ("X/Y temporadas"),
// distinta da posse PROFUNDA do detalhe (episódios), que segue em montarPosseAgregada
// e NÃO muda. Custo ~1 + nº de temporadas leituras (self-hosted; follow-up: query
// agregada única se a biblioteca crescer). Degrada para nil em qualquer falha.
func (s *Server) posseRasaDaSerie(ctx context.Context, serieID string) *resolver.OwnershipInfo {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		return nil
	}
	total, owned := 0, 0
	for _, rel := range rels {
		if rel.Kind != kindTemporada {
			continue
		}
		total++
		info, err := s.posseDaTemporada(ctx, rel.ChildID)
		if err != nil {
			return nil
		}
		if info.OwnedCount > 0 {
			owned++
		}
	}
	return &resolver.OwnershipInfo{OwnedCount: owned, TotalCount: total}
}

// anoAntes ordena por release_date ISO ascendente (a string ISO ordena
// cronologicamente); datas ausentes/vazias vão para o FIM.
func anoAntes(a, b sql.NullString) bool {
	av := a.Valid && a.String != ""
	bv := b.Valid && b.String != ""
	if av != bv {
		return av
	}
	return a.String < b.String
}

// getEntity trata GET /api/v1/entities/{id}: devolve a entity RESOLVIDA (todas as
// camadas: arquivo ⟵ externo ⟵ override) ou 404.
func (s *Server) getEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	lang, ok := s.parseLang(w, cfg, r.URL.Query().Get("lang"))
	if !ok {
		return
	}

	resolvida, encontrada, err := s.resolverEntity(ctx, id, lang)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !encontrada {
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, resolvida)
}

// resolverEntity monta a resposta RESOLVIDA de uma entity a partir de TODAS as
// camadas (arquivo ⟵ externo ⟵ override) — o caminho único reusado por getEntity,
// enrich e import. Devolve encontrada=false (sem erro) quando a entity não existe.
// É AQUI que a camada externa fica viva de ponta a ponta (a fiação adiada do 0013).
func (s *Server) resolverEntity(ctx context.Context, id, lang string) (resolver.Entidade, bool, error) {
	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resolver.Entidade{}, false, nil
		}
		return resolver.Entidade{}, false, err
	}

	// Contexto de IDIOMA (briefing 0019): resolve o lang efetivo (default =
	// principal) e deriva a cadeia de fallback a partir das settings + a âncora
	// original_language da entity.
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return resolver.Entidade{}, false, err
	}
	if lang == "" {
		lang = cfg.Primary
	}
	cadeia := cfg.cadeiaDeIdiomas(lang, ent.OriginalLanguage)

	files, err := s.listarFilesDaEntity(ctx, ent)
	if err != nil {
		return resolver.Entidade{}, false, err
	}

	// Arte local: pôster e FUNDO (fanart/backdrop) são servidos por rotas locais
	// quando existe asset (a mesma precedência preferida→user→external do serviço).
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		return resolver.Entidade{}, false, err
	}
	posterURL, backdropURL := artesLocais(assets, id)

	// Camada OVERRIDE: o patch cru do dono (media_override). Ausência de linha não
	// é erro (sem override, herda a base).
	override, err := s.carregarOverride(ctx, id)
	if err != nil {
		return resolver.Entidade{}, false, err
	}

	// Camada EXTERNA (fiação do 0015): mapeia cada media_external raw→canônico via
	// o provider, ordenada pela prioridade das fontes.
	externo, err := s.montarExterno(ctx, id)
	if err != nil {
		return resolver.Entidade{}, false, err
	}

	resolvida := resolver.Resolver(resolver.Camadas{
		Entity:          ent,
		Files:           files,
		PosterURL:       posterURL,
		BackdropURL:     backdropURL,
		Override:        override,
		Externo:         externo,
		LangPedido:      lang,
		Cadeia:          cadeia,
		IdiomaPrincipal: cfg.Primary,
	})
	s.anotarInternos(ctx, &resolvida)
	// Interno-first dos créditos (0029 Fase D): a fonte que alimentou cast/crew não é
	// necessariamente a mesma (cada campo vence na fusão por prioridade — R2), então
	// descobre-se por campo em vez de assumir a fonte de maior prioridade da entity.
	s.anotarCreditosInternos(ctx, fonteDoCampoExterno(externo, "cast"), resolvida.Cast, nil)
	s.anotarCreditosInternos(ctx, fonteDoCampoExterno(externo, "crew"), nil, resolvida.Crew)
	s.proxyarCreditos(resolvida.Cast, resolvida.Crew, resolvida.Companies)
	resolvida.Ancestors = s.montarAncestrais(ctx, id)
	resolvida.Ownership = s.montarPosseAgregada(ctx, ent)
	resolvida.Orderings = s.montarOrderings(ctx, ent)
	resolvida.StillURL = stillDaEntity(assets, ent)
	s.herdarFanartTemporada(ctx, &resolvida, assets, ent)
	return resolvida, true, nil
}

// herdarFanartTemporada preenche o BackdropURL de uma TEMPORADA sem fanart própria
// com a fanart da SÉRIE PAI (herança de DISPLAY do 0044, como o tinyMediaManager):
// não inventa dado nem baixa nada — reusa o asset já baixado da série. No-op para
// tipos != season e para a temporada que já tem asset `fanart` servível próprio (aí
// o artesLocais já preencheu o BackdropURL com a dela). Anotação no server, fora do
// resolver puro (mesma fronteira de Ancestors/Ownership/still).
func (s *Server) herdarFanartTemporada(ctx context.Context, e *resolver.Entidade, assets []sqlcgen.Asset, ent sqlcgen.Entity) {
	if ent.Type != tipoEntTemporada {
		return
	}
	if _, ok := escolherAssetServido(assets, kindFanart); ok {
		return // tem a sua fanart própria: nada a herdar
	}
	if serieID, ok := s.fanartDaSeriePai(ctx, ent.ID); ok {
		u := fanartPath(serieID)
		e.BackdropURL = &u
	}
}

// fanartDaSeriePai sobe da temporada para a SÉRIE PAI (via ListarRelacoesPais — a
// temporada é filha da série) e devolve o id da série quando ela tem asset `fanart`
// SERVÍVEL. false quando não há pai, o pai não tem fanart servível, ou em falha de
// leitura (degrada para "sem herança", nunca inventa). Não baixa nada.
func (s *Server) fanartDaSeriePai(ctx context.Context, seasonID string) (string, bool) {
	pais, err := s.leitura().ListarRelacoesPais(ctx, seasonID)
	if err != nil || len(pais) == 0 {
		return "", false
	}
	serieID := pais[0].ParentID
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, serieID)
	if err != nil {
		return "", false
	}
	if _, ok := escolherAssetServido(assets, kindFanart); !ok {
		return "", false
	}
	return serieID, true
}

// stillDaEntity devolve a URL do still do episódio para o detalhe, com a PRECEDÊNCIA
// do 0044: still `user` (rota local /assets/still) VENCE o still_url cru do provider
// (extension_data core.tv@1, 0042). Só para `episode`, nil para os demais tipos. O
// resolver é PURO (não conhece core.tv); a anotação vive aqui, na mesma fronteira de
// Ancestors/Ownership/Orderings. Sem still custom, mantém o cru (front proxya).
func stillDaEntity(assets []sqlcgen.Asset, ent sqlcgen.Entity) *string {
	if ent.Type != tipoEntEpisodio {
		return nil
	}
	return stillDoEpisodio(assets, ent.ID, tvext.Namespace(ent.ExtensionDataJson, nsTVCore))
}

// stillDoEpisodio aplica a precedência do still (0044): se há asset `still` SERVÍVEL
// para o episódio, devolve a rota local (/assets/still); senão cai no still_url cru do
// provider (ns). Ponto ÚNICO reusado pelo detalhe (stillDaEntity) e pelo card
// (anotarNumbering) — a mesma regra custom→provider dos dois lados.
func stillDoEpisodio(assets []sqlcgen.Asset, id string, ns map[string]any) *string {
	if _, ok := escolherAssetServido(assets, kindStill); ok {
		u := stillPath(id)
		return &u
	}
	return stillDoNS(ns)
}

// tipoEntEpisodio é o `type` da entity de episódio (grão fino da árvore de séries,
// 0031/ADR-0021). Um arquivo de série tem `media_file.entity_id` = série, e os
// vínculos por episódio vivem em media_file_entity (kind='tv_episode'); por isso a
// área de arquivos do EPISÓDIO vem da junção, não de media_file.entity_id.
const tipoEntEpisodio = "episode"

// listarFilesDaEntity carrega os media_file que compõem a POSSE e a lista de
// arquivos de uma entity. Para EPISÓDIO, os arquivos vêm dos vínculos N:M
// (media_file_entity, kind='tv_episode') via ListarArquivosDaEntidade — porque o
// arquivo de série aponta `entity_id` para a SÉRIE, não para o episódio (0034).
// Para os demais tipos (filme etc.), o caminho clássico media_file.entity_id.
func (s *Server) listarFilesDaEntity(ctx context.Context, ent sqlcgen.Entity) ([]sqlcgen.MediaFile, error) {
	if ent.Type == tipoEntEpisodio {
		rows, err := s.leitura().ListarArquivosDaEntidade(ctx, sqlcgen.ListarArquivosDaEntidadeParams{
			EntityID: ent.ID,
			Kind:     kindEpisodio,
		})
		if err != nil {
			return nil, err
		}
		files := make([]sqlcgen.MediaFile, len(rows))
		for i, r := range rows {
			files[i] = arquivoDaEntidadeParaMediaFile(r)
		}
		return files, nil
	}
	return s.leitura().ListarMediaFilesPorEntity(ctx, ent.ID)
}

// arquivoDaEntidadeParaMediaFile reprojeta a linha do JOIN (ListarArquivosDaEntidade)
// no shape sqlcgen.MediaFile, para reusar o MESMO fileDTO/posse do filme. O `ordinal`
// (ordem do episódio dentro do arquivo, em ranges) não entra no DTO de arquivo.
func arquivoDaEntidadeParaMediaFile(r sqlcgen.ListarArquivosDaEntidadeRow) sqlcgen.MediaFile {
	return sqlcgen.MediaFile{
		ID:              r.ID,
		EntityID:        r.EntityID,
		Path:            r.Path,
		ContentHash:     r.ContentHash,
		Size:            r.Size,
		Mtime:           r.Mtime,
		Edition:         r.Edition,
		PartNo:          r.PartNo,
		MatchConfidence: r.MatchConfidence,
		MediainfoJson:   r.MediainfoJson,
		State:           r.State,
		LastSeen:        r.LastSeen,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
		LibrarySourceID: r.LibrarySourceID,
	}
}

// montarPosseAgregada calcula a posse agregada de uma entity-pai da árvore de
// séries (0034): retorna nil para tipos sem agregação (filme, episódio, coleção).
//   - TEMPORADA: total = nº de episódios filhos (kind='tv_episode'); owned = quantos
//     desses têm ao menos um arquivo 'present' vinculado (ContarEntidadesComPosse).
//   - SÉRIE: soma across temporadas — a query direta só conta filhos DIRETOS (que na
//     série são temporadas, sem vínculo próprio), então iteramos as temporadas e
//     somamos total/owned de cada uma. Volume pequeno (self-hosted); N+1 tolerável.
//
// Defensivo: qualquer falha de leitura degrada para nil (a UI só omite o badge), sem
// derrubar o detalhe — coerente com a montagem pós-resolução de Ancestors.
func (s *Server) montarPosseAgregada(ctx context.Context, ent sqlcgen.Entity) *resolver.OwnershipInfo {
	switch ent.Type {
	case tipoEntTemporada:
		info, err := s.posseDaTemporada(ctx, ent.ID)
		if err != nil {
			return nil
		}
		return info
	case tipoEntSerie:
		total, owned := 0, 0
		rels, err := s.leitura().ListarRelacoesFilhas(ctx, ent.ID)
		if err != nil {
			return nil
		}
		for _, rel := range rels {
			if rel.Kind != kindTemporada {
				continue
			}
			info, err := s.posseDaTemporada(ctx, rel.ChildID)
			if err != nil {
				return nil
			}
			total += info.TotalCount
			owned += info.OwnedCount
		}
		return &resolver.OwnershipInfo{OwnedCount: owned, TotalCount: total}
	default:
		return nil
	}
}

// posseDaTemporada devolve (episódios possuídos / total) de UMA temporada: total é o
// nº de filhos kind='tv_episode'; owned vem de ContarEntidadesComPosse (filhos com
// arquivo 'present' vinculado). Reusado pela série (soma por temporada).
func (s *Server) posseDaTemporada(ctx context.Context, seasonID string) (*resolver.OwnershipInfo, error) {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, seasonID)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, rel := range rels {
		if rel.Kind == kindEpisodio {
			total++
		}
	}
	owned, err := s.leitura().ContarEntidadesComPosse(ctx, sqlcgen.ContarEntidadesComPosseParams{
		ParentID: seasonID,
		Kind:     kindEpisodio,
	})
	if err != nil {
		return nil, err
	}
	return &resolver.OwnershipInfo{OwnedCount: int(owned), TotalCount: total}, nil
}

// maxProfundidadeAncestrais limita o walk-up do breadcrumb (0032) para NUNCA laçar
// num ciclo de entity_relation defeituoso. 8 é folgado: a árvore real tem no máximo
// 2 níveis (episódio→temporada→série).
const maxProfundidadeAncestrais = 8

// montarAncestrais sobe a árvore de séries a partir de `id`, pegando o PRIMEIRO pai
// de cada nível (ListarRelacoesPais) até a raiz, e devolve a trilha invertida para
// RAIZ→PAI — o breadcrumb do 0032. Cada nó traz {id, title, type} com o título BASE
// da linha (barato; resolver o título por idioma de cada ancestral é follow-up).
// Sempre não-nil (`[]` para movie/tv/collection, que não têm pai). Defensivo: para na
// primeira falha/ausência de pai e limita a profundidade (maxProfundidadeAncestrais)
// para jamais laçar. Montado aqui (server), não no resolver puro (fronteira do 0026/0031).
func (s *Server) montarAncestrais(ctx context.Context, id string) []resolver.AncestorRef {
	trilha := make([]resolver.AncestorRef, 0)
	atual := id
	for i := 0; i < maxProfundidadeAncestrais; i++ {
		pais, err := s.leitura().ListarRelacoesPais(ctx, atual)
		if err != nil || len(pais) == 0 {
			break
		}
		pai, err := s.leitura().ObterEntity(ctx, pais[0].ParentID)
		if err != nil {
			break // pai purgado no meio: para a trilha aqui
		}
		trilha = append(trilha, resolver.AncestorRef{ID: pai.ID, Title: pai.Title, Type: pai.Type})
		atual = pai.ID
	}
	// Coletamos de baixo para cima (pai, avô, …); inverte para RAIZ→PAI.
	for i, j := 0, len(trilha)-1; i < j; i, j = i+1, j-1 {
		trilha[i], trilha[j] = trilha[j], trilha[i]
	}
	return trilha
}

// anotarInternos preenche os `internal_id` da coleção e dos similares (0026/0027): o
// resolver é PURO (não acessa banco), então a resolução externo→interno é anotada
// aqui, pós-resolução. Coleção e similares da entity vêm do TMDB (o link externo já
// assume isso), então a resolução é por id tmdb. Isolado para não inflar o
// resolverEntity (gocyclo).
func (s *Server) anotarInternos(ctx context.Context, e *resolver.Entidade) {
	if e.Collection != nil {
		e.Collection.InternalID = s.idInternoColecao(ctx, "tmdb", e.Collection.SourceID)
	}
	for i := range e.Similar {
		e.Similar[i].InternalID = s.idInternoItem(ctx, "tmdb", e.Similar[i].SourceID)
	}
}

// prioridadeFontes é a ordem de precedência global das fontes externas (R2 / ADR-0002):
// por campo, a PRIMEIRA fonte da lista com valor forma a base externa dele; também dita
// a ordem determinística da busca-mescla (search.go). É o PONTO ÚNICO da política —
// tornar configurável (via settings) é follow-up; por ora é o default do briefing 0031.
var prioridadeFontes = []string{"tmdb", "tvdb"}

// montarExterno carrega as linhas de media_external da entity e as converte em
// camadas canônicas via o provider (raw→canônico é do provider; o resolver não
// conhece o TMDB). Sem provider configurado devolve nil (comportamento do 0010
// preservado). Uma linha cujo mapeamento falha é PULADA (degrada aquela fonte),
// não derruba a leitura. Ordena pela prioridade das fontes.
func (s *Server) montarExterno(ctx context.Context, id string) ([]resolver.CamadaExterna, error) {
	if s.providers.Vazio() {
		return nil, nil
	}
	rows, err := s.leitura().ListarMediaExternalPorEntity(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	var camadas []resolver.CamadaExterna
	for _, fonte := range prioridadeFontes {
		camadas = append(camadas, s.camadasDaFonte(rows, fonte)...)
	}
	return camadas, nil
}

// camadasDaFonte converte as linhas de UMA fonte em camadas canônicas via o provider
// DAQUELA fonte (escolhido no registro pelo nome). Devolve nil quando a fonte não tem
// provider registrado (degrada: camada vazia). Uma linha com payload ausente/inválido
// é pulada (degrada aquela linha).
func (s *Server) camadasDaFonte(rows []sqlcgen.MediaExternal, fonte string) []resolver.CamadaExterna {
	prov, ok := s.providers.Get(fonte)
	if !ok {
		return nil
	}
	var out []resolver.CamadaExterna
	for _, row := range rows {
		if row.Source != fonte || !row.PayloadJson.Valid || row.PayloadJson.String == "" {
			continue
		}
		campos, err := prov.MapearDetalhe(json.RawMessage(row.PayloadJson.String))
		if err != nil {
			continue // payload cru inválido: degrada esta fonte
		}
		out = append(out, resolver.CamadaExterna{Fonte: row.Source, Campos: campos})
	}
	return out
}

// carregarOverride lê o patch de override da entity (media_override) e o decodifica
// para o mapa esparso cru que o resolver espera. Sem linha (sql.ErrNoRows) ou com
// override_json vazio/inválido devolve nil (sem override, herda a base) — apenas um
// erro real de banco é propagado.
func (s *Server) carregarOverride(ctx context.Context, id string) (map[string]json.RawMessage, error) {
	ov, err := s.leitura().ObterMediaOverride(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !ov.OverrideJson.Valid || ov.OverrideJson.String == "" {
		return nil, nil
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal([]byte(ov.OverrideJson.String), &patch); err != nil {
		// Override malformado degrada para "sem override" em vez de derrubar a
		// leitura (a base editorial/arquivo segue servível).
		return nil, nil
	}
	return patch, nil
}

// getEntityFiles trata GET /api/v1/entities/{id}/files: lista os media_file com
// os fatos técnicos do mediainfo. 404 se a entity não existir.
func (s *Server) getEntityFiles(w http.ResponseWriter, r *http.Request) {
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

	// Episódio: arquivos vêm dos vínculos N:M (o arquivo aponta entity_id para a
	// série); demais tipos: o caminho clássico media_file.entity_id (mesmo helper
	// usado na resolução, fonte única).
	files, err := s.listarFilesDaEntity(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	items := make([]mediaFileDTO, 0, len(files))
	for _, f := range files {
		items = append(items, fileDTO(f))
	}
	writeJSON(w, http.StatusOK, filesListDTO{Items: items})
}

// fileDTO converte um media_file no DTO de arquivo, repassando o mediainfo_json
// cru (ou `null`).
func fileDTO(f sqlcgen.MediaFile) mediaFileDTO {
	dto := mediaFileDTO{
		ID:      f.ID,
		Path:    f.Path,
		Size:    ni64Ptr(f.Size),
		Edition: nsPtr(f.Edition),
		PartNo:  ni64Ptr(f.PartNo),
		State:   f.State,
	}
	if f.MediainfoJson.Valid && f.MediainfoJson.String != "" {
		dto.Mediainfo = json.RawMessage(f.MediainfoJson.String)
	}
	return dto
}

// getEntityPoster trata GET /api/v1/entities/{id}/assets/poster: serve o binário do
// pôster local com o content-type correto, aplicando a PRECEDÊNCIA de serviço do
// briefing 0017 (preferida → user → external → 404) sobre a lista de assets do kind
// 'poster'. 404 quando não há pôster (a UI cai no placeholder); 403 quando o caminho
// gravado NÃO está sob um datasource conhecido (guarda de PATH-TRAVERSAL: jamais
// servimos caminho arbitrário). A escolha do dono vence — vale inclusive para o que
// o Jellyfin consome deste endpoint.
func (s *Server) getEntityPoster(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	asset, ok := escolherAssetServido(assets, kindPoster)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "pôster não encontrado")
		return
	}
	s.servirBinarioAsset(w, r, ctx, asset)
}

// getEntityFanart trata GET /api/v1/entities/{id}/assets/fanart: serve o binário do
// FUNDO (backdrop) local — o análogo de getEntityPoster para o kind 'fanart', com a
// MESMA precedência de serviço (preferida → user → external → 404) e a mesma guarda
// de path-traversal. 404 quando não há fundo (a UI cai na URL direta/placeholder).
func (s *Server) getEntityFanart(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	asset, ok := escolherAssetServido(assets, kindFanart)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "fundo não encontrado")
		return
	}
	s.servirBinarioAsset(w, r, ctx, asset)
}

// getEntityStill trata GET /api/v1/entities/{id}/assets/still: serve o binário do
// STILL custom (frame 16:9) do episódio — o análogo de getEntityPoster/Fanart para o
// kind 'still' (0044), com a MESMA precedência de serviço (preferida → user →
// external → 404) e a mesma guarda de path-traversal. 404 quando não há still custom
// (o detalhe cai no still_url cru do provider — 0042).
func (s *Server) getEntityStill(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	asset, ok := escolherAssetServido(assets, kindStill)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "still não encontrado")
		return
	}
	s.servirBinarioAsset(w, r, ctx, asset)
}

// raizesConfiaveis é a lista de diretórios sob os quais um pôster PODE ser
// servido: as raízes dos datasources (arte local do scanner) mais o diretório de
// assets (cache de imagem externo, briefing 0015), quando configurado. É a base da
// guarda de path-traversal do endpoint de pôster.
func (s *Server) raizesConfiaveis(ctx context.Context) ([]string, error) {
	fontes, err := s.leitura().ListarLibrarySources(ctx)
	if err != nil {
		return nil, err
	}
	raizes := make([]string, 0, len(fontes)+1)
	for _, f := range fontes {
		raizes = append(raizes, f.Path)
	}
	if s.assetsDir != "" {
		raizes = append(raizes, s.assetsDir)
	}
	return raizes, nil
}

// contentTypeImagem determina o content-type do pôster: primeiro pela extensão
// (barato e preciso para os formatos comuns); se não resolver para image/*,
// fareja os primeiros bytes. Volta o ponteiro do arquivo ao início para o
// ServeContent reenviar do zero.
func contentTypeImagem(path string, f *os.File) string {
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	_, _ = f.Seek(0, 0)
	return http.DetectContentType(buf[:n])
}

// caminhoSobRaizes diz se `alvo` está contido em ALGUMA das raízes, comparando
// caminhos LIMPOS e ABSOLUTOS via filepath.Rel — o caminho relativo não pode
// começar por ".." nem ser absoluto para estar contido. É a checagem central da
// guarda de path-traversal.
func caminhoSobRaizes(alvo string, raizes []string) bool {
	ap, err := filepath.Abs(filepath.Clean(alvo))
	if err != nil {
		return false
	}
	for _, raiz := range raizes {
		rp, err := filepath.Abs(filepath.Clean(raiz))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rp, ap)
		if err != nil {
			continue
		}
		if rel == "." {
			return true
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

// ── Parse dos parâmetros de query ────────────────────────────────────────────

// parsePaginacao lê limit/offset com defaults e teto. Valores não-numéricos ou
// negativos respondem 400 invalid_query (e ok=false).
func parsePaginacao(w http.ResponseWriter, q url.Values) (limit, offset int, ok bool) {
	limit = limitPadrao
	offset = 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "limit inválido")
			return 0, 0, false
		}
		limit = n
	}
	if limit > limitMaximo {
		limit = limitMaximo
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "offset inválido")
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

// parseTipoFiltro devolve o filtro de type como valor para o sqlc (string) ou
// nil quando ausente (sem filtro).
func parseTipoFiltro(q url.Values) any {
	if v := q.Get("type"); v != "" {
		return v
	}
	return nil
}

// parseOwnedFiltro traduz owned=true|false|1|0 no inteiro 1/0 que a query
// compara ao EXISTS. Ausente => nil (sem filtro). Valor inválido => 400.
func parseOwnedFiltro(w http.ResponseWriter, q url.Values) (any, bool) {
	v := q.Get("owned")
	if v == "" {
		return nil, true
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", "owned inválido (use true ou false)")
		return nil, false
	}
	if b {
		return int64(1), true
	}
	return int64(0), true
}

// parseSort lê sort=title|-title. Default (ausente/"title") é ascendente;
// "-title"/"title_desc" é descendente. Outro valor => 400.
func parseSort(w http.ResponseWriter, q url.Values) (desc, ok bool) {
	switch q.Get("sort") {
	case "", "title", "title_asc":
		return false, true
	case "-title", "title_desc":
		return true, true
	default:
		writeError(w, http.StatusBadRequest, "invalid_query", "sort inválido (use title ou -title)")
		return false, false
	}
}

// ── Helpers de conversão ─────────────────────────────────────────────────────

// respondErroInterno responde 500 no shape padrão sem vazar o detalhe interno.
func respondErroInterno(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "internal_error",
		"erro interno ao processar a requisição")
}

// floatPtr extrai um *float64 do valor dinâmico do MAX(match_confidence) (REAL
// ou NULL). Sem match => nil.
func floatPtr(v any) *float64 {
	switch x := v.(type) {
	case float64:
		return &x
	case int64:
		f := float64(x)
		return &f
	default:
		return nil
	}
}

// nsPtr converte sql.NullString em *string (nil quando NULL).
func nsPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// ni64Ptr converte sql.NullInt64 em *int64 (nil quando NULL).
func ni64Ptr(ni sql.NullInt64) *int64 {
	if !ni.Valid {
		return nil
	}
	v := ni.Int64
	return &v
}
