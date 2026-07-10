// Package richfields concentra os TIPOS e a RESOLUÇÃO dos campos ricos de um item
// (vídeos, coleção, similares, classificação por região, slogan, keywords, nota,
// duração, backdrop, links) a partir do mapa CANÔNICO produzido pelo provider
// (MapearDetalhe). É consumido por DOIS lugares que precisam do MESMO shape:
//
//   - o handler GET /external/{source}/{type}/{id} (detalhe do externo ANTES de
//     importar, briefings 0022/0023), e
//   - o resolver da entity IMPORTADA (GET /entities/{id}), que expõe os mesmos
//     campos ricos para a tela de detalhe do catálogo ter paridade visual.
//
// Centralizar aqui evita duas fontes de verdade divergirem: os JSONs do externo e
// da entity carregam os campos ricos com o MESMO formato. Todas as funções são
// PURAS sobre `map[string]any` canônico — sem I/O, sem SQL, sem dependência do
// provider concreto.
package richfields

import "sort"

// RegiaoFallback é a região usada quando o idioma ativo não carrega região (ex.:
// `en`) ou a fonte não traz a certificação da região pedida (decisão da revisão
// da Fase 3: cai em US).
const RegiaoFallback = "US"

// Video é um vídeo (trailer/clip) do item. URL é nil quando o site do provedor é
// desconhecido (a UI mostra só nome/tipo nesse caso).
type Video struct {
	Name     string  `json:"name"`
	Key      string  `json:"key"`
	Site     string  `json:"site"`
	Type     string  `json:"type"`
	Official bool    `json:"official"`
	URL      *string `json:"url"`
}

// Collection é a coleção (≠ playlist) a que o item pertence. SourceID permite
// NAVEGAR à coleção; PosterURL é opcional. InternalID é o id da entity coleção
// interna quando ela JÁ foi importada (briefing 0026) — a UI então linka para o
// destino interno em vez da página externa. ColecaoDe segue PURA e o deixa nil;
// quem tem acesso a banco (o handler / o resolverEntity no server) o preenche.
type Collection struct {
	SourceID   string  `json:"source_id"`
	Name       string  `json:"name"`
	PosterURL  *string `json:"poster_url"`
	InternalID *string `json:"internal_id"`
}

// Similar é um item similar, com o Type para NAVEGAR ao seu detalhe externo — o
// mesmo GET, recursivo, sem catalogar.
type Similar struct {
	SourceID  string  `json:"source_id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Year      *string `json:"year"`
	PosterURL *string `json:"poster_url"`
	// InternalID é o id da entity interna quando o similar já foi importado (0027,
	// estende o interno-first do 0026). SimilaresDe é PURA e o deixa nil; quem tem DB
	// (o handler do detalhe externo e o resolver da entity, via server) o preenche.
	InternalID *string `json:"internal_id"`
}

// ── Resolução de campos traduzíveis ──────────────────────────────────────────

// ResolverEscalarTraduzivel resolve um campo escalar traduzível (title/description/
// tagline) percorrendo a cadeia; cai em "qualquer idioma presente" (ordem estável)
// se a cadeia não casar. Devolve (valor, idioma efetivo). Tolera o campo como
// escalar simples (payload legado) — nesse caso proveniência "".
func ResolverEscalarTraduzivel(campos map[string]any, campo string, cadeia []string) (*string, string) {
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
// cadeia com texto, senão qualquer idioma presente (ordem estável). Devolve o
// idioma efetivo como proveniência.
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

// ResolverListaTraduzivel resolve um campo-lista traduzível (tags/keywords) pela
// cadeia, com o mesmo fallback "qualquer idioma presente" (ordem estável). Devolve
// (lista, idioma efetivo). Tolera lista simples (legado) — proveniência "".
func ResolverListaTraduzivel(campos map[string]any, campo string, cadeia []string) ([]string, string) {
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

// ── Campos neutros ricos ─────────────────────────────────────────────────────

// CertificacaoDaRegiao escolhe a classificação da REGIÃO do idioma ativo (pt-BR→BR),
// com fallback US quando o idioma não tem região ou a fonte não traz aquela região.
func CertificacaoDaRegiao(campos map[string]any, lang string) *string {
	m, ok := campos["certification_by_region"].(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	regiao := RegiaoDoIdioma(lang)
	if s, ok := m[regiao].(string); ok && s != "" {
		return &s
	}
	if s, ok := m[RegiaoFallback].(string); ok && s != "" {
		return &s
	}
	return nil
}

// RegiaoDoIdioma extrai a região (iso_3166_1) de um código de idioma: o sufixo após
// o "-" (pt-BR→BR); sem sufixo, o fallback US (decisão da revisão).
func RegiaoDoIdioma(lang string) string {
	if i := lastIndexByte(lang, '-'); i >= 0 && i < len(lang)-1 {
		return toUpper(lang[i+1:])
	}
	return RegiaoFallback
}

// VideosDe converte a lista canônica de vídeos.
func VideosDe(campos map[string]any) []Video {
	itens := comoListaAny(campos["videos"])
	out := make([]Video, 0, len(itens))
	for _, it := range itens {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Video{
			Name:     ComoStringMapa(m, "name"),
			Key:      ComoStringMapa(m, "key"),
			Site:     ComoStringMapa(m, "site"),
			Type:     ComoStringMapa(m, "type"),
			Official: comoBool(m["official"]),
			URL:      PtrSeNaoVazio(ComoStringMapa(m, "url")),
		})
	}
	return out
}

// ColecaoDe converte a coleção canônica, ou nil quando ausente.
func ColecaoDe(campos map[string]any) *Collection {
	m, ok := campos["collection"].(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	return &Collection{
		SourceID:  ComoStringMapa(m, "source_id"),
		Name:      ComoStringMapa(m, "name"),
		PosterURL: PtrSeNaoVazio(ComoStringMapa(m, "poster_url")),
	}
}

// SimilaresDe converte a lista canônica de similares (com o type para navegar).
func SimilaresDe(campos map[string]any) []Similar {
	itens := comoListaAny(campos["similar"])
	out := make([]Similar, 0, len(itens))
	for _, it := range itens {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Similar{
			SourceID:  ComoStringMapa(m, "source_id"),
			Type:      ComoStringMapa(m, "type"),
			Title:     ComoStringMapa(m, "title"),
			Year:      PtrSeNaoVazio(ComoStringMapa(m, "year")),
			PosterURL: PtrSeNaoVazio(ComoStringMapa(m, "poster_url")),
		})
	}
	return out
}

// LinksExternos monta o mapa de links a partir do canônico (ex.: imdb do provider),
// acrescentando o link do próprio TMDB quando há id TMDB — o chamador tem o tipo e o
// id (o provider mapeou só o imdb). `tmdbID` vazio omite o link do TMDB.
func LinksExternos(campos map[string]any, tmdbTipo, tmdbID string) map[string]string {
	out := map[string]string{}
	if m, ok := campos["external_links"].(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok && s != "" {
				out[k] = s
			}
		}
	}
	if tmdbID != "" && tmdbTipo != "" {
		out["tmdb"] = "https://www.themoviedb.org/" + tmdbTipo + "/" + tmdbID
	}
	return out
}

// ── Helpers de conversão (puros sobre o canônico) ────────────────────────────

// ComoStringMapa lê uma chave string do mapa ("" quando ausente/tipo errado).
func ComoStringMapa(m map[string]any, chave string) string {
	if s, ok := m[chave].(string); ok {
		return s
	}
	return ""
}

// StrPtrDe lê uma chave string do mapa como *string (nil quando ausente/vazia).
func StrPtrDe(m map[string]any, chave string) *string {
	return PtrSeNaoVazio(ComoStringMapa(m, chave))
}

// IntPtrDe lê uma chave numérica do mapa como *int (nil quando ausente). O canônico
// grava runtime como int; toleramos float64 (round-trip JSON).
func IntPtrDe(m map[string]any, chave string) *int {
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

// FloatPtrDe lê uma chave numérica do mapa como *float64 (nil quando ausente).
func FloatPtrDe(m map[string]any, chave string) *float64 {
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

// PtrSeNaoVazio devolve *string para uma string, ou nil quando vazia.
func PtrSeNaoVazio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// DerefOuVazio deref um *string ("" quando nil) — para campos não-nuláveis (title).
func DerefOuVazio(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// OuListaVazia troca nil por fatia vazia (JSON sai como `[]`, nunca null).
func OuListaVazia(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// AnoDePtr extrai os 4 primeiros dígitos (ano) de uma data ISO apontada, ou nil.
func AnoDePtr(data *string) *string {
	if data == nil || len(*data) < 4 {
		return nil
	}
	ano := (*data)[:4]
	return &ano
}

// comoBool lê um bool dinâmico (false quando ausente/tipo errado).
func comoBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// comoLista converte um valor dinâmico numa fatia de strings não-nil (aceita
// []string e []any de strings).
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

// chavesOrdenadas devolve as chaves do mapa em ordem estável (determinismo no
// fallback "qualquer idioma presente").
func chavesOrdenadas(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// lastIndexByte e toUpper evitam importar strings só para duas operações triviais
// (mantém o pacote enxuto e sem dependências além de sort).
func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func toUpper(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'a' && out[i] <= 'z' {
			out[i] -= 'a' - 'A'
		}
	}
	return string(out)
}
