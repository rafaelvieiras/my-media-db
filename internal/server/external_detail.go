package server

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo implementa GET /api/v1/external/{source}/{type}/{id} (briefing 0022):
// o DETALHE EXTERNO RICO que o dono vê ANTES de importar (como o TMDB). Puxa o item
// completo da fonte, PERSISTE na 1ª chamada (cache external_detail, via o serviço
// externaldetail) e serve do cache nas seguintes. Resolve os campos TRADUZÍVEIS
// (título, sinopse, slogan, gêneros, keywords) no idioma ATIVO pela cadeia de fallback
// das settings; os NEUTROS (duração, classificação, backdrop, vídeos, coleção,
// similares, nota, links) vêm uma vez. Ver o externo NUNCA cria entity.
//
// A classificação indicativa é por REGIÃO (decisão da revisão): escolhe a região do
// idioma ativo (pt-BR→BR), com FALLBACK para US quando o idioma não carrega região
// (ex.: en) ou a fonte não traz aquela região.

// regiaoFallback é a região usada quando o idioma ativo não carrega região ou a fonte
// não traz a certificação da região pedida (decisão da revisão da Fase 3).
const regiaoFallback = "US"

// videoExternoDTO é um vídeo (trailer/clip) do detalhe externo. url é nil quando o
// site do provedor é desconhecido (a UI mostra só nome/tipo nesse caso).
type videoExternoDTO struct {
	Name     string  `json:"name"`
	Key      string  `json:"key"`
	Site     string  `json:"site"`
	Type     string  `json:"type"`
	Official bool    `json:"official"`
	URL      *string `json:"url"`
}

// colecaoExternaDTO é a coleção (≠ playlist) a que o item pertence. source_id permite
// NAVEGAR à coleção (follow-up do 0023); poster_url é opcional.
type colecaoExternaDTO struct {
	SourceID  string  `json:"source_id"`
	Name      string  `json:"name"`
	PosterURL *string `json:"poster_url"`
}

// similarExternoDTO é um item similar, com o `type` para NAVEGAR ao seu detalhe
// externo (decisão da revisão) — o mesmo GET, recursivo, sem catalogar.
type similarExternoDTO struct {
	SourceID  string  `json:"source_id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Year      *string `json:"year"`
	PosterURL *string `json:"poster_url"`
}

// detalheExternoDTO é a resposta de GET /external/{source}/{type}/{id}: o conjunto
// rico do TMDB, resolvido no idioma ativo. Listas saem sempre como `[]` (nunca null)
// para simplificar o front; ponteiros saem como null quando ausentes. SEM créditos
// (MD6) nem nota local — decisão explícita, cada um vira briefing próprio.
type detalheExternoDTO struct {
	Source           string   `json:"source"`
	SourceID         string   `json:"source_id"`
	Type             string   `json:"type"`
	Lang             string   `json:"lang"`
	Title            string   `json:"title"`
	OriginalTitle    *string  `json:"original_title"`
	Description      *string  `json:"description"`
	Tagline          *string  `json:"tagline"`
	ReleaseDate      *string  `json:"release_date"`
	Year             *string  `json:"year"`
	Runtime          *int     `json:"runtime"`
	Certification    *string  `json:"certification"`
	OriginalLanguage *string  `json:"original_language"`
	VoteAverage      *float64 `json:"vote_average"`

	Genres   []string `json:"genres"`
	Keywords []string `json:"keywords"`

	PosterURL   *string `json:"poster_url"`
	BackdropURL *string `json:"backdrop_url"`

	Videos        []videoExternoDTO   `json:"videos"`
	Collection    *colecaoExternaDTO  `json:"collection"`
	Similar       []similarExternoDTO `json:"similar"`
	ExternalLinks map[string]string   `json:"external_links"`

	// ResolvedLanguages é a proveniência de idioma dos campos traduzíveis (ADR-0019),
	// `""` = sem idioma efetivo. Espelha o mesmo campo do detalhe da entity.
	ResolvedLanguages map[string]string `json:"resolved_languages"`
}

// getExternalDetail trata GET /api/v1/external/{source}/{type}/{id}?lang=. 503 sem
// fonte externa; 422 type fora de movie|tv; 400 lang fora dos permitidos; 404 item
// inexistente na fonte; 502 fonte falhou; 200 com o detalhe rico resolvido.
func (s *Server) getExternalDetail(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if s.externalDetail == nil || s.searchProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"detalhe externo indisponível (fonte externa não configurada)")
		return
	}

	source := chi.URLParam(r, "source")
	tipo := chi.URLParam(r, "type")
	id := chi.URLParam(r, "id")
	if tipo != "movie" && tipo != "tv" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "type inválido (use movie ou tv)")
		return
	}

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

	// Puxa (ou serve do cache) TODAS as traduções permitidas numa só ida (0020); a
	// resolução por idioma acontece aqui, no read-time.
	raw, err := s.externalDetail.PayloadFresco(ctx, source, id, tipo, cfg.Allowed)
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			writeError(w, http.StatusNotFound, "not_found", "item não encontrado na fonte")
			return
		}
		respondFonteIndisponivel(w)
		return
	}

	campos, err := s.searchProvider.MapearDetalhe(raw)
	if err != nil {
		// Payload cru inválido: trata como falha da fonte (o cache re-tenta na próxima).
		respondFonteIndisponivel(w)
		return
	}

	dto := s.montarDetalheExterno(source, id, tipo, lang, cfg, campos)
	writeJSON(w, http.StatusOK, dto)
}

// montarDetalheExterno compõe o DTO rico a partir do mapa canônico: resolve os
// traduzíveis pela cadeia de fallback (ancorada no original_language do item) e extrai
// os neutros. A certificação usa a região do idioma ativo com fallback US.
func (s *Server) montarDetalheExterno(source, id, tipo, lang string, cfg languageSettings, campos map[string]any) detalheExternoDTO {
	original := comoStringMapa(campos, "original_language")
	cadeia := cfg.cadeiaDeIdiomas(lang, nsDe(original))

	titulo, provTitulo := resolverEscalarTraduzivel(campos, "title", cadeia)
	descricao, provDesc := resolverEscalarTraduzivel(campos, "description", cadeia)
	slogan, provSlogan := resolverEscalarTraduzivel(campos, "tagline", cadeia)
	generos, provGeneros := resolverListaTraduzivel(campos, "tags", cadeia)
	keywords, provKeywords := resolverListaTraduzivel(campos, "keywords", cadeia)

	releaseDate := strPtrDe(campos, "release_date")

	dto := detalheExternoDTO{
		Source:           source,
		SourceID:         id,
		Type:             tipo,
		Lang:             lang,
		Title:            derefOuVazio(titulo),
		OriginalTitle:    strPtrDe(campos, "original_title"),
		Description:      descricao,
		Tagline:          slogan,
		ReleaseDate:      releaseDate,
		Year:             anoDePtr(releaseDate),
		Runtime:          intPtrDe(campos, "runtime"),
		Certification:    certificacaoDaRegiao(campos, lang),
		OriginalLanguage: ptrSeNaoVazio(original),
		VoteAverage:      floatPtrDe(campos, "vote_average"),
		Genres:           ouListaVazia(generos),
		Keywords:         ouListaVazia(keywords),
		PosterURL:        strPtrDe(campos, "poster_url"),
		BackdropURL:      strPtrDe(campos, "backdrop_url"),
		Videos:           videosDe(campos),
		Collection:       colecaoDe(campos),
		Similar:          similaresDe(campos),
		ExternalLinks:    linksExternosDe(campos, source, tipo, id),
		ResolvedLanguages: map[string]string{
			"title":       provTitulo,
			"description": provDesc,
			"tagline":     provSlogan,
			"genres":      provGeneros,
			"keywords":    provKeywords,
		},
	}
	return dto
}

// ── Resolução de campos traduzíveis ──────────────────────────────────────────

// resolverEscalarTraduzivel resolve um campo escalar traduzível (title/description/
// tagline) percorrendo a cadeia; cai em "qualquer idioma presente" (ordem estável) se
// a cadeia não casar. Devolve (valor, idioma efetivo). Tolera o campo como escalar
// simples (payload legado) — nesse caso proveniência "".
func resolverEscalarTraduzivel(campos map[string]any, campo string, cadeia []string) (*string, string) {
	v, ok := campos[campo]
	if !ok || v == nil {
		return nil, ""
	}
	switch m := v.(type) {
	case string:
		if m == "" {
			return nil, ""
		}
		return &m, ""
	case map[string]any:
		return escalarDoMapaIdioma(m, cadeia)
	}
	return nil, ""
}

// escalarDoMapaIdioma escolhe o texto do mapa idioma→valor: o primeiro idioma da
// cadeia com texto, senão qualquer idioma presente (ordem estável). Devolve o idioma
// efetivo como proveniência.
func escalarDoMapaIdioma(m map[string]any, cadeia []string) (*string, string) {
	for _, l := range cadeia {
		if s, ok := m[l].(string); ok && s != "" {
			return &s, l
		}
	}
	for _, l := range chavesOrdenadas(m) {
		if s, ok := m[l].(string); ok && s != "" {
			return &s, l
		}
	}
	return nil, ""
}

// resolverListaTraduzivel resolve um campo-lista traduzível (tags/keywords) pela
// cadeia, com o mesmo fallback "qualquer idioma presente" (ordem estável). Devolve
// (lista, idioma efetivo). Tolera lista simples (legado) — proveniência "".
func resolverListaTraduzivel(campos map[string]any, campo string, cadeia []string) ([]string, string) {
	v, ok := campos[campo]
	if !ok || v == nil {
		return nil, ""
	}
	m, ehMap := v.(map[string]any)
	if !ehMap {
		return comoLista(v), ""
	}
	for _, l := range cadeia {
		if lst := comoLista(m[l]); len(lst) > 0 {
			return lst, l
		}
	}
	for _, l := range chavesOrdenadas(m) {
		if lst := comoLista(m[l]); len(lst) > 0 {
			return lst, l
		}
	}
	return nil, ""
}

// ── Extração de campos neutros ───────────────────────────────────────────────

// certificacaoDaRegiao escolhe a classificação da REGIÃO do idioma ativo (pt-BR→BR),
// com fallback US quando o idioma não tem região ou a fonte não traz aquela região.
func certificacaoDaRegiao(campos map[string]any, lang string) *string {
	m, ok := campos["certification_by_region"].(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	regiao := regiaoDoIdioma(lang)
	if s, ok := m[regiao].(string); ok && s != "" {
		return &s
	}
	if s, ok := m[regiaoFallback].(string); ok && s != "" {
		return &s
	}
	return nil
}

// regiaoDoIdioma extrai a região (iso_3166_1) de um código de idioma: o sufixo após o
// "-" (pt-BR→BR); sem sufixo, o fallback US (decisão da revisão).
func regiaoDoIdioma(lang string) string {
	if i := strings.LastIndex(lang, "-"); i >= 0 && i < len(lang)-1 {
		return strings.ToUpper(lang[i+1:])
	}
	return regiaoFallback
}

// videosDe converte a lista canônica de vídeos no DTO.
func videosDe(campos map[string]any) []videoExternoDTO {
	itens := comoListaAny(campos["videos"])
	out := make([]videoExternoDTO, 0, len(itens))
	for _, it := range itens {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, videoExternoDTO{
			Name:     comoStringMapa(m, "name"),
			Key:      comoStringMapa(m, "key"),
			Site:     comoStringMapa(m, "site"),
			Type:     comoStringMapa(m, "type"),
			Official: comoBool(m["official"]),
			URL:      ptrSeNaoVazio(comoStringMapa(m, "url")),
		})
	}
	return out
}

// colecaoDe converte a coleção canônica no DTO, ou nil quando ausente.
func colecaoDe(campos map[string]any) *colecaoExternaDTO {
	m, ok := campos["collection"].(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	return &colecaoExternaDTO{
		SourceID:  comoStringMapa(m, "source_id"),
		Name:      comoStringMapa(m, "name"),
		PosterURL: ptrSeNaoVazio(comoStringMapa(m, "poster_url")),
	}
}

// similaresDe converte a lista canônica de similares no DTO (com o type para navegar).
func similaresDe(campos map[string]any) []similarExternoDTO {
	itens := comoListaAny(campos["similar"])
	out := make([]similarExternoDTO, 0, len(itens))
	for _, it := range itens {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, similarExternoDTO{
			SourceID:  comoStringMapa(m, "source_id"),
			Type:      comoStringMapa(m, "type"),
			Title:     comoStringMapa(m, "title"),
			Year:      ptrSeNaoVazio(comoStringMapa(m, "year")),
			PosterURL: ptrSeNaoVazio(comoStringMapa(m, "poster_url")),
		})
	}
	return out
}

// linksExternosDe monta o mapa de links, acrescentando o link do próprio TMDB (o
// handler tem o tipo+id da rota; o provider mapeou só o imdb).
func linksExternosDe(campos map[string]any, source, tipo, id string) map[string]string {
	out := map[string]string{}
	if m, ok := campos["external_links"].(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok && s != "" {
				out[k] = s
			}
		}
	}
	if source == "tmdb" {
		out["tmdb"] = "https://www.themoviedb.org/" + tipo + "/" + id
	}
	return out
}

// ── Helpers de conversão ─────────────────────────────────────────────────────

// nsDe embrulha uma string num sql.NullString (inválido quando vazia) — para a cadeia
// de idiomas ignorar a âncora ausente.
func nsDe(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// comoStringMapa lê uma chave string do mapa ("" quando ausente/tipo errado).
func comoStringMapa(m map[string]any, chave string) string {
	if s, ok := m[chave].(string); ok {
		return s
	}
	return ""
}

// comoBool lê um bool dinâmico (false quando ausente/tipo errado).
func comoBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// comoLista converte um valor dinâmico numa fatia de strings não-nil (aceita []string
// e []any de strings).
func comoLista(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

// comoListaAny devolve uma fatia []any (nil quando o valor não é lista).
func comoListaAny(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// strPtrDe lê uma chave string do mapa como *string (nil quando ausente/vazia).
func strPtrDe(m map[string]any, chave string) *string {
	return ptrSeNaoVazio(comoStringMapa(m, chave))
}

// intPtrDe lê uma chave numérica do mapa como *int (nil quando ausente). O canônico
// grava runtime como int; toleramos float64 (caso venha de um round-trip JSON).
func intPtrDe(m map[string]any, chave string) *int {
	switch n := m[chave].(type) {
	case int:
		v := n
		return &v
	case float64:
		v := int(n)
		return &v
	default:
		return nil
	}
}

// floatPtrDe lê uma chave numérica do mapa como *float64 (nil quando ausente).
func floatPtrDe(m map[string]any, chave string) *float64 {
	switch n := m[chave].(type) {
	case float64:
		v := n
		return &v
	case int:
		v := float64(n)
		return &v
	default:
		return nil
	}
}

// ptrSeNaoVazio devolve *string para uma string, ou nil quando vazia.
func ptrSeNaoVazio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// derefOuVazio deref um *string ("" quando nil) — para o campo title, não-nulável.
func derefOuVazio(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ouListaVazia troca nil por fatia vazia (JSON sai como `[]`, nunca null).
func ouListaVazia(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// anoDePtr extrai os 4 primeiros dígitos (ano) de uma data ISO apontada, ou nil.
func anoDePtr(data *string) *string {
	if data == nil || len(*data) < 4 {
		return nil
	}
	ano := (*data)[:4]
	return &ano
}

// chavesOrdenadas devolve as chaves do mapa em ordem estável (determinismo no fallback
// "qualquer idioma presente").
func chavesOrdenadas(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
