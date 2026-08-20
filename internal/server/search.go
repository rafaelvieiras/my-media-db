package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo implementa GET /api/v1/search (briefing 0014): busca LOCAL-FIRST.
// O catálogo é consultado SEMPRE (FTS5 sobre entity_fts), independente da fonte
// externa; a fonte externa (quando há provider) roda em passthrough apenas para
// enriquecer, com resultados marcados "não salvos" (saved:false) — a busca nunca
// cria entidade. Falha/ausência do externo degrada (external:[],
// external_available:false), nunca 5xx.

// ttlCacheExterno é o TTL curto do cache em memória do resultado externo por
// (query,tipo). Volume é baixo (self-hosted): poupa a fonte sem limiter elaborado.
const ttlCacheExterno = 60 * time.Second

// buscarCardsFTSSQL é a query da busca LOCAL. Escrita à mão (não via sqlc) por
// dois motivos: (1) o sqlc v1.31 não parseia o MATCH de tabela inteira do FTS5
// (`entity_fts MATCH ?`), só o de coluna única (`title MATCH ?`), que perderia
// original_title/tags; (2) mantida como const Go ASCII com colunas EXPLÍCITAS,
// evita de vez o bug de corrupção do sqlc com .sql de query. O SHAPE das colunas
// é IDÊNTICO ao de ListarEntityCards (mesma ordem), então o resultado é escaneado
// em sqlcgen.ListarEntityCardsRow e reusa o cardDTO da lista.
//
// O placeholder `?1` (match) chega SANITIZADO (ver sanitizarMatchFTS): cada termo
// do usuário vira uma frase entre aspas com prefixo, de modo que aspas, parênteses
// e operadores do FTS5 no input não quebrem a sintaxe da consulta.
//
// Por PADRÃO a busca só retorna OBRAS DE TOPO: os sub-nós de tiposSubNo
// (ADR-0030/briefing 0112, entities.go — season/episode/album/faixa) ficam de
// fora, senão uma busca por "Simpsons" afogaria a série numa enxurrada de
// episódios/temporadas de várias séries (0033, SUPERSEDE a exclusão dura do
// 0032). A cláusula `e.type NOT IN (/*SLICE:sub_nos*/?) OR e.type IN
// (/*SLICE:incluir*/?)` usa o MESMO mecanismo que o sqlc gera para
// `sqlc.slice` (ver ListarTiposComContagem em entity.sql.go), replicado à mão
// aqui porque esta query é escrita manualmente (motivo (1)/(2) acima) — os
// marcadores são expandidos em Go por expandirSlice antes da execução:
// sub_nos recebe SEMPRE tiposSubNo inteiro (exclusão fixa), incluir recebe os
// sub-nós religados via `?include=` (parseInclude), vazio por padrão (a lista
// vazia expande para `NOT IN (NULL)`/`IN (NULL)`, que nunca casa, preservando
// o só-topo). Os parâmetros posicionais seguem (match, sub_nos..., incluir...,
// tipo, tipo, limit).
//
// owned/file_count contam TAMBEM a juncao media_file_entity (0034): com `?include=
// episode` a busca traz episodios, cujo arquivo tem entity_id=SERIE e liga ao episodio
// so pela juncao -- sem isto o card do episodio viria owned=false/file_count=0. Mesma
// extensao (e mesmas formas UNION que preservam bool/int64 no scan) de ListarEntityCards.
// owned soma tambem qualquer DESCENDENTE na arvore de entity_relation (briefing
// 0116) que possua media_file 'present' -- cobre artista/album de musica, cujo
// unico vinculo com o arquivo fica na FAIXA, dois niveis abaixo (ver nota longa
// em ObterEntityCard, internal/db/queries/entity.sql).
const buscarCardsFTSSQL = `WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT
    e.id, e.type, e.title, e.status,
    EXISTS (
        SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present'
        UNION ALL
        SELECT 1 FROM media_file_entity mfe JOIN media_file mf ON mf.id = mfe.media_file_id
            WHERE mfe.entity_id = e.id AND mf.state = 'present'
        UNION ALL
        SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
            WHERE t.ancestor_id = e.id
    ) AS owned,
    (SELECT COUNT(*) FROM (
        SELECT m.id FROM media_file m WHERE m.entity_id = e.id
        UNION ALL
        SELECT DISTINCT mfe.media_file_id FROM media_file_entity mfe WHERE mfe.entity_id = e.id
    )) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster
FROM entity_fts
JOIN entity e ON e.id = entity_fts.entity_id
WHERE entity_fts MATCH ?
  AND (e.type NOT IN (/*SLICE:sub_nos*/?) OR e.type IN (/*SLICE:incluir*/?))
  AND (? IS NULL OR e.type = ?)
ORDER BY entity_fts.rank
LIMIT ?;`

// expandirSlice substitui o marcador `/*SLICE:<marcador>*/?` de buscarCardsFTSSQL por
// uma lista `?,?,...` do tamanho de valores (e acrescenta cada valor em args, na
// mesma ordem) ou por `NULL` quando valores está vazia (mesmo mecanismo que o sqlc
// gera para `sqlc.slice` em query gerada — replicado à mão aqui porque esta query é
// escrita manualmente). `x NOT IN (NULL)`/`x IN (NULL)` nunca casa nenhuma linha,
// então uma slice vazia é sempre "nenhuma exceção"/"nada incluído", nunca erro de
// sintaxe SQL.
func expandirSlice(query, marcador string, valores []string, args []any) (string, []any) {
	m := "/*SLICE:" + marcador + "*/?"
	if len(valores) == 0 {
		return strings.Replace(query, m, "NULL", 1), args
	}
	for _, v := range valores {
		args = append(args, v)
	}
	return strings.Replace(query, m, strings.Repeat(",?", len(valores))[1:], 1), args
}

// externalResultDTO é um resultado da fonte EXTERNA (briefing 0014). saved é
// sempre false: resultado externo nunca é entidade do catálogo até o import (0015).
// InternalID (0027) é o id da entity interna quando o item externo JÁ foi importado —
// nil caso contrário: a UI mostra "na biblioteca" e linka ao interno em vez de reimportar.
type externalResultDTO struct {
	Source     string  `json:"source"`
	SourceID   string  `json:"source_id"`
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Year       *string `json:"year"`
	Overview   *string `json:"overview"`
	PosterURL  *string `json:"poster_url"`
	Saved      bool    `json:"saved"`
	InternalID *string `json:"internal_id"`
}

// searchResponseDTO é a resposta de GET /api/v1/search. local são cards do
// catálogo (mesmo shape da lista); external são resultados "não salvos".
// external_available separa "fonte indisponível" de "zero resultados".
type searchResponseDTO struct {
	Query             string              `json:"query"`
	Local             []entityCardDTO     `json:"local"`
	External          []externalResultDTO `json:"external"`
	ExternalAvailable bool                `json:"external_available"`
}

// search trata GET /api/v1/search?q=&type=. q é obrigatório (vazio => 400). A
// busca local roda sempre; a externa só quando há provider e degrada em silêncio.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	q := r.URL.Query()
	termo := strings.TrimSpace(q.Get("q"))
	if termo == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "o parâmetro q é obrigatório")
		return
	}
	tipo := parseTipoFiltro(q) // string | nil
	incluir := parseInclude(q) // sub-nós de tiposSubNo a religar na busca local

	ctx := r.Context()
	// Idioma de leitura ATIVO (0021): valida o ?lang= contra os permitidos (400 se
	// fora) e resolve o efetivo (default = principal). A busca EXTERNA o repassa à
	// fonte; a busca local serve o card neutro (mesmo contrato de /entities).
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	lang, ok := s.parseLang(w, cfg, q.Get("lang"))
	if !ok {
		return
	}

	locais, err := s.buscarLocal(ctx, termo, tipo, incluir)
	if err != nil {
		respondErroInterno(w)
		return
	}

	tipoStr, _ := tipo.(string)
	externos, disponivel := s.buscarExterno(ctx, termo, tipoStr, lang)

	writeJSON(w, http.StatusOK, searchResponseDTO{
		Query:             termo,
		Local:             locais,
		External:          s.mapearExternos(ctx, externos),
		ExternalAvailable: disponivel,
	})
}

// parseInclude lê o parâmetro `include` (CSV, ex.: "season,episode" ou
// "album,faixa") e devolve os sub-nós de tiposSubNo (ADR-0030/briefing 0112) a
// RELIGAR na busca local. Generalizado no 0112: aceita QUALQUER valor de
// tiposSubNo, não mais só season/episode — schema novo que vira sub-nó (ex.
// uma vertical futura) já religa via este mesmo parâmetro, sem tocar aqui.
// Aceita repetição (idempotente, dedup) e ignora silenciosamente qualquer
// valor fora de tiposSubNo. Por padrão (sem include) devolve vazio — os
// sub-nós continuam de fora (0033).
func parseInclude(q url.Values) []string {
	permitido := make(map[string]bool, len(tiposSubNo))
	for _, t := range tiposSubNo {
		permitido[t] = true
	}
	vistos := make(map[string]bool)
	incluir := make([]string, 0, len(tiposSubNo))
	for _, raw := range q["include"] {
		for _, v := range strings.Split(raw, ",") {
			v = strings.TrimSpace(v)
			if v == "" || !permitido[v] || vistos[v] {
				continue
			}
			vistos[v] = true
			incluir = append(incluir, v)
		}
	}
	return incluir
}

// buscarLocal roda o MATCH FTS5 sanitizado no pool de LEITURA e devolve os cards.
// incluir são os sub-nós de tiposSubNo a RELIGAR na busca (ADR-0030/0112, vindo de
// parseInclude); por padrão (nil/vazio) os sub-nós ficam de fora (0033). Input sem
// nenhum termo pesquisável (só pontuação/operadores) devolve lista vazia sem tocar
// o banco — nunca erro.
func (s *Server) buscarLocal(ctx context.Context, termo string, tipo any, incluir []string) ([]entityCardDTO, error) {
	match, ok := sanitizarMatchFTS(termo)
	if !ok {
		return []entityCardDTO{}, nil
	}
	query := buscarCardsFTSSQL
	args := []any{match}
	query, args = expandirSlice(query, "sub_nos", tiposSubNo, args)
	query, args = expandirSlice(query, "incluir", incluir, args)
	args = append(args, tipo, tipo, int64(limitPadrao))
	rows, err := s.db.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]entityCardDTO, 0)
	for rows.Next() {
		var row sqlcgen.ListarEntityCardsRow
		if err := rows.Scan(
			&row.ID, &row.Type, &row.Title, &row.Status,
			&row.Owned, &row.FileCount, &row.MatchConfidence, &row.HasPoster,
		); err != nil {
			return nil, err
		}
		card := cardDTO(row)
		items = append(items, card)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Posse RASA (0036/0037) nos cards tv/season da busca — mesma anotação da
	// listagem, feita após fechar o cursor (evita consultas aninhadas no mesmo
	// Read). Degrada para null em falha; nunca derruba a busca.
	for i := range items {
		s.anotarPosseCard(ctx, &items[i])
	}
	return items, nil
}

// buscarExterno consulta TODAS as fontes do registro com cache TTL curto e MESCLA os
// resultados (dedup por (source, source_id); ordem determinística por prioridadeFontes,
// briefing 0031). Devolve (resultados, disponivel). Registro vazio => (nil, false).
// disponivel é true quando AO MENOS UMA fonte respondeu (erro/timeout de todas =>
// false): o handler então degrada para external:[], external_available:false. Só o
// resultado mesclado de sucesso entra no cache.
func (s *Server) buscarExterno(ctx context.Context, termo, tipo, lang string) ([]provider.ResultadoExterno, bool) {
	if s.providers.Vazio() {
		return nil, false
	}
	// A chave inclui o idioma: a mesma busca em pt-BR e es tem resultados distintos
	// e não pode compartilhar entrada de cache.
	chave := termo + "\x00" + tipo + "\x00" + lang
	if res, ok := s.cacheExterno.obter(chave); ok {
		return res, true
	}
	res, algum := s.mesclarBuscas(ctx, termo, tipo, lang)
	if !algum {
		return nil, false
	}
	s.cacheExterno.guardar(chave, res)
	return res, true
}

// mesclarBuscas consulta cada provider (na ordem de prioridade) e funde os resultados,
// deduplicando por (source, source_id) — o primeiro a trazer um par vence. `algum` diz
// se ALGUMA fonte respondeu (sucesso), separando "todas falharam" de "zero resultados".
// A falha de UMA fonte degrada aquela fonte (pula), não a busca inteira.
func (s *Server) mesclarBuscas(ctx context.Context, termo, tipo, lang string) ([]provider.ResultadoExterno, bool) {
	var out []provider.ResultadoExterno
	vistos := make(map[string]bool)
	algum := false
	for _, p := range s.providersOrdenados() {
		res, err := p.Buscar(ctx, termo, tipo, lang)
		if err != nil {
			continue // degrada esta fonte
		}
		algum = true
		for _, r := range res {
			chave := r.Source + "\x00" + r.SourceID
			if vistos[chave] {
				continue
			}
			vistos[chave] = true
			out = append(out, r)
		}
	}
	return out, algum
}

// providersOrdenados devolve os providers do registro em ordem DETERMINÍSTICA: primeiro
// os de prioridadeFontes (R2), na ordem da lista; depois quaisquer fontes registradas
// fora da lista (na ordem de registro), para nunca sumir com uma fonte configurada.
func (s *Server) providersOrdenados() []provider.Provider {
	out := make([]provider.Provider, 0, len(s.providers.Nomes()))
	vistos := make(map[string]bool)
	for _, nome := range prioridadeFontes {
		if p, ok := s.providers.Get(nome); ok {
			out = append(out, p)
			vistos[nome] = true
		}
	}
	for _, nome := range s.providers.Nomes() {
		if vistos[nome] {
			continue
		}
		if p, ok := s.providers.Get(nome); ok {
			out = append(out, p)
		}
	}
	return out
}

// mapearExternos converte os resultados canônicos do provider no DTO externo,
// forçando saved:false (nunca é entidade até o import) e anotando o internal_id
// interno-first (0027): cada resultado externo que já foi importado ganha o id da
// entity correspondente (via idInternoItem), para a UI mostrar "na biblioteca" e
// linkar ao interno. O lookup casa por (r.Source, r.SourceID) contra a identidade
// indexada — QUALQUER fonte com coluna gerada resolve (tmdb E tvdb; 0047), não só tmdb.
func (s *Server) mapearExternos(ctx context.Context, res []provider.ResultadoExterno) []externalResultDTO {
	out := make([]externalResultDTO, 0, len(res))
	for _, r := range res {
		out = append(out, externalResultDTO{
			Source:     r.Source,
			SourceID:   r.SourceID,
			Type:       r.Type,
			Title:      r.Title,
			Year:       r.Year,
			Overview:   r.Overview,
			PosterURL:  r.PosterURL,
			Saved:      false,
			InternalID: s.idInternoItem(ctx, r.Source, r.SourceID),
		})
	}
	return out
}

// sanitizarMatchFTS transforma o texto do usuário numa expressão MATCH SEGURA do
// FTS5. Estratégia robusta: remove aspas (viram separador), quebra em termos por
// espaço, descarta termos sem nenhuma letra/dígito e envolve cada termo restante
// numa FRASE entre aspas com prefixo (`"termo"*`). Assim:
//   - aspas, parênteses, `:`, `{}`, `*`, `AND`/`OR`/`NEAR` etc. no input viram
//     conteúdo literal de frase e NÃO são interpretados como sintaxe/operador;
//   - o prefixo `*` dá busca incremental (casa começo de palavra).
//
// Devolve ok=false quando não sobra termo pesquisável — o chamador então nem
// consulta o banco (resultado local vazio, sem erro).
func sanitizarMatchFTS(termo string) (string, bool) {
	limpo := strings.ReplaceAll(termo, `"`, " ")
	campos := strings.Fields(limpo)
	frases := make([]string, 0, len(campos))
	for _, c := range campos {
		if !temAlfanumerico(c) {
			continue
		}
		frases = append(frases, `"`+c+`"*`)
	}
	if len(frases) == 0 {
		return "", false
	}
	return strings.Join(frases, " "), true
}

// temAlfanumerico diz se a string tem ao menos uma letra ou dígito unicode —
// garante que a frase FTS5 gerada não seja vazia (evita erro de sintaxe).
func temAlfanumerico(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// ── cache externo ─────────────────────────────────────────────────────────────

// cacheExterno é um cache em memória, protegido por mutex, do resultado externo
// por chave (query+tipo), com expiração por TTL. Simples de propósito (volume
// baixo, self-hosted).
type cacheExterno struct {
	mu    sync.Mutex
	ttl   time.Duration
	itens map[string]entradaCacheExterno
	agora func() time.Time
}

// entradaCacheExterno é uma entrada do cache: os resultados e o instante de
// expiração.
type entradaCacheExterno struct {
	resultados []provider.ResultadoExterno
	expira     time.Time
}

// novoCacheExterno cria um cache com o TTL dado.
func novoCacheExterno(ttl time.Duration) *cacheExterno {
	return &cacheExterno{ttl: ttl, itens: make(map[string]entradaCacheExterno), agora: time.Now}
}

// obter devolve os resultados cacheados e true quando a chave existe e não
// expirou; caso contrário (nil, false).
func (c *cacheExterno) obter(chave string) ([]provider.ResultadoExterno, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.itens[chave]
	if !ok || c.agora().After(e.expira) {
		return nil, false
	}
	return e.resultados, true
}

// guardar grava (ou sobrescreve) os resultados da chave com nova expiração.
func (c *cacheExterno) guardar(chave string, res []provider.ResultadoExterno) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.itens[chave] = entradaCacheExterno{resultados: res, expira: c.agora().Add(c.ttl)}
}
