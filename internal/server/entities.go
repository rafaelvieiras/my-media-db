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
		items = append(items, cardDTO(row))
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

	if _, err := s.leitura().ObterEntity(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}

	rels, err := s.leitura().ListarRelacoesFilhas(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	membros := make([]membroCard, 0, len(rels))
	for _, rel := range rels {
		if rel.Kind != kindColecao {
			continue
		}
		row, err := s.leitura().ObterEntityCard(ctx, rel.ChildID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // membro purgado no meio: ignora
			}
			respondErroInterno(w)
			return
		}
		membros = append(membros, membroCard{dto: cardDTODeMembro(row), ano: row.ReleaseDate})
	}
	sort.SliceStable(membros, func(i, j int) bool { return anoAntes(membros[i].ano, membros[j].ano) })

	out := make([]entityCardDTO, len(membros))
	for i, m := range membros {
		out[i] = m.dto
	}
	writeJSON(w, http.StatusOK, out)
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

	files, err := s.leitura().ListarMediaFilesPorEntity(ctx, id)
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
	return resolvida, true, nil
}

// prioridadeFontes é a ordem de precedência das fontes externas (R2): a primeira
// com valor para um campo forma a base externa dele. Hoje só o TMDB.
var prioridadeFontes = []string{"tmdb"}

// montarExterno carrega as linhas de media_external da entity e as converte em
// camadas canônicas via o provider (raw→canônico é do provider; o resolver não
// conhece o TMDB). Sem provider configurado devolve nil (comportamento do 0010
// preservado). Uma linha cujo mapeamento falha é PULADA (degrada aquela fonte),
// não derruba a leitura. Ordena pela prioridade das fontes.
func (s *Server) montarExterno(ctx context.Context, id string) ([]resolver.CamadaExterna, error) {
	if s.searchProvider == nil {
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

// camadasDaFonte converte as linhas de UMA fonte em camadas canônicas via o
// provider. Devolve nil quando não há mapeador para a fonte (provider de outra
// fonte). Uma linha com payload ausente/inválido é pulada (degrada aquela linha).
func (s *Server) camadasDaFonte(rows []sqlcgen.MediaExternal, fonte string) []resolver.CamadaExterna {
	if s.searchProvider.Nome() != fonte {
		return nil
	}
	var out []resolver.CamadaExterna
	for _, row := range rows {
		if row.Source != fonte || !row.PayloadJson.Valid || row.PayloadJson.String == "" {
			continue
		}
		campos, err := s.searchProvider.MapearDetalhe(json.RawMessage(row.PayloadJson.String))
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

	if _, err := s.leitura().ObterEntity(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}

	files, err := s.leitura().ListarMediaFilesPorEntity(ctx, id)
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
