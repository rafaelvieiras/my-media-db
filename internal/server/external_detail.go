package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// Este arquivo implementa GET /api/v1/external/{source}/{type}/{id} (briefing 0022):
// o DETALHE EXTERNO RICO que o dono vê ANTES de importar (como o TMDB). Puxa o item
// completo da fonte, PERSISTE na 1ª chamada (cache external_detail, via o serviço
// externaldetail) e serve do cache nas seguintes. Resolve os campos TRADUZÍVEIS
// (título, sinopse, slogan, gêneros, keywords) no idioma ATIVO pela cadeia de fallback
// das settings; os NEUTROS (duração, classificação, backdrop, vídeos, coleção,
// similares, nota, links) vêm uma vez. Ver o externo NUNCA cria entity.
//
// A resolução/tipos dos campos ricos vive em internal/richfields — COMPARTILHADA com
// o resolver da entity (GET /entities/{id}), para as duas telas terem o MESMO shape.
// A classificação indicativa é por REGIÃO (decisão da revisão): região do idioma ativo
// (pt-BR→BR), com FALLBACK US quando o idioma não carrega região ou a fonte não a traz.

// detalheExternoDTO é a resposta de GET /external/{source}/{type}/{id}: o conjunto
// rico do TMDB, resolvido no idioma ativo. Listas saem sempre como `[]` (nunca null)
// para simplificar o front; ponteiros saem como null quando ausentes. SEM créditos
// (MD6) nem nota local — decisão explícita, cada um vira briefing próprio.
type detalheExternoDTO struct {
	Source        string  `json:"source"`
	SourceID      string  `json:"source_id"`
	Type          string  `json:"type"`
	Lang          string  `json:"lang"`
	Title         string  `json:"title"`
	OriginalTitle *string `json:"original_title"`
	Description   *string `json:"description"`
	Tagline       *string `json:"tagline"`
	ReleaseDate   *string `json:"release_date"`
	Year          *string `json:"year"`
	Runtime       *int    `json:"runtime"`
	Certification *string `json:"certification"`
	// AiringStatus é a SITUAÇÃO de exibição da série (0045): string CRUA da fonte,
	// verbatim (paridade com o preview interno; a UI traduz p/ pt-BR). omitempty/ausente
	// para filme ou quando a fonte não a informa.
	AiringStatus     *string  `json:"airing_status,omitempty"`
	OriginalLanguage *string  `json:"original_language"`
	VoteAverage      *float64 `json:"vote_average"`

	Genres   []string `json:"genres"`
	Keywords []string `json:"keywords"`

	PosterURL   *string `json:"poster_url"`
	BackdropURL *string `json:"backdrop_url"`

	Videos        []richfields.Video     `json:"videos"`
	Collection    *richfields.Collection `json:"collection"`
	Similar       []richfields.Similar   `json:"similar"`
	Cast          []richfields.Cast      `json:"cast"`
	Crew          []richfields.Crew      `json:"crew"`
	Companies     []richfields.Company   `json:"companies"`
	ExternalLinks map[string]string      `json:"external_links"`

	// ResolvedLanguages é a proveniência de idioma dos campos traduzíveis (ADR-0019),
	// `""` = sem idioma efetivo. Espelha o mesmo campo do detalhe da entity.
	ResolvedLanguages map[string]string `json:"resolved_languages"`
}

// provedorDetalhe resolve o provider da FONTE da rota para o detalhe externo. Escreve
// 503 e devolve ok=false quando o detalhe está desligado (sem cache) OU a fonte não tem
// provider registrado — a mesma porta de "fonte externa não configurada". Get(source)
// já cobre o registro vazio (nenhum provider => sempre !ok).
func (s *Server) provedorDetalhe(w http.ResponseWriter, source string) (provider.Provider, bool) {
	if s.externalDetail != nil {
		if prov, ok := s.providers.Get(source); ok {
			return prov, true
		}
	}
	writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
		"detalhe externo indisponível (fonte externa não configurada)")
	return nil, false
}

// getExternalDetail trata GET /api/v1/external/{source}/{type}/{id}?lang=. 503 sem
// fonte externa; 422 type fora de movie|tv; 400 lang fora dos permitidos; 404 item
// inexistente na fonte; 502 fonte falhou; 200 com o detalhe rico resolvido.
func (s *Server) getExternalDetail(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	source := chi.URLParam(r, "source")
	tipo := chi.URLParam(r, "type")
	id := chi.URLParam(r, "id")
	if tipo != "movie" && tipo != "tv" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "type inválido (use movie ou tv)")
		return
	}
	prov, ok := s.provedorDetalhe(w, source)
	if !ok {
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

	campos, err := prov.MapearDetalhe(raw)
	if err != nil {
		// Payload cru inválido: trata como falha da fonte (o cache re-tenta na próxima).
		respondFonteIndisponivel(w)
		return
	}

	dto := s.montarDetalheExterno(ctx, source, id, tipo, lang, cfg, campos)
	writeJSON(w, http.StatusOK, dto)
}

// montarDetalheExterno compõe o DTO rico a partir do mapa canônico: resolve os
// traduzíveis pela cadeia de fallback (ancorada no original_language do item) e extrai
// os neutros via richfields (compartilhado com o resolver da entity). A certificação
// usa a região do idioma ativo com fallback US.
func (s *Server) montarDetalheExterno(ctx context.Context, source, id, tipo, lang string, cfg languageSettings, campos map[string]any) detalheExternoDTO {
	original := richfields.ComoStringMapa(campos, "original_language")
	cadeia := cfg.cadeiaDeIdiomas(lang, nsDe(original))

	titulo, provTitulo := richfields.ResolverEscalarTraduzivel(campos, "title", cadeia)
	descricao, provDesc := richfields.ResolverEscalarTraduzivel(campos, "description", cadeia)
	slogan, provSlogan := richfields.ResolverEscalarTraduzivel(campos, "tagline", cadeia)
	generos, provGeneros := richfields.ResolverListaTraduzivel(campos, "tags", cadeia)
	keywords, provKeywords := richfields.ResolverListaTraduzivel(campos, "keywords", cadeia)

	releaseDate := richfields.StrPtrDe(campos, "release_date")

	// Coleção da banda: quando existe e já foi importada, anota o id interno (0026)
	// para a UI linkar ao destino interno em vez da página externa. ColecaoDe é pura
	// (deixa InternalID nil); o preenchimento com DB é aqui.
	colecao := richfields.ColecaoDe(campos)
	if colecao != nil {
		colecao.InternalID = s.idInternoColecao(ctx, source, colecao.SourceID)
	}

	// Similares interno-aware (0027): cada similar já importado ganha o id interno,
	// para a UI linkar ao destino interno (/entities/{id}) em vez de reabrir o externo.
	// SimilaresDe é pura (InternalID nil); o preenchimento com DB é aqui. N é pequeno
	// (a fonte limita os similares) e o lookup é indexado — ok para self-hosted.
	similares := richfields.SimilaresDe(campos)
	for i := range similares {
		similares[i].InternalID = s.idInternoItem(ctx, source, similares[i].SourceID)
	}

	// Créditos (elenco/equipe/produtoras, 0029): extraídos puros via richfields (mesmo
	// shape do resolver da entity) e então proxyados (profile_url/logo_url), como o
	// pôster/backdrop — richfields não tem acesso ao proxy.
	cast := richfields.CastDe(campos)
	crew := richfields.CrewDe(campos)
	companies := richfields.CompaniesDe(campos)
	s.proxyarCreditos(cast, crew, companies)

	// Link do TMDB só quando a fonte é o próprio TMDB (o handler tem tipo+id da rota).
	tmdbTipo, tmdbID := "", ""
	if source == "tmdb" {
		tmdbTipo, tmdbID = tipo, id
	}

	return detalheExternoDTO{
		Source:           source,
		SourceID:         id,
		Type:             tipo,
		Lang:             lang,
		Title:            richfields.DerefOuVazio(titulo),
		OriginalTitle:    richfields.StrPtrDe(campos, "original_title"),
		Description:      descricao,
		Tagline:          slogan,
		ReleaseDate:      releaseDate,
		Year:             richfields.AnoDePtr(releaseDate),
		Runtime:          richfields.IntPtrDe(campos, "runtime"),
		Certification:    richfields.CertificacaoDaRegiao(campos, lang),
		AiringStatus:     richfields.StrPtrDe(campos, "airing_status"),
		OriginalLanguage: richfields.PtrSeNaoVazio(original),
		VoteAverage:      richfields.FloatPtrDe(campos, "vote_average"),
		Genres:           richfields.OuListaVazia(generos),
		Keywords:         richfields.OuListaVazia(keywords),
		// As artes do HERO (pôster/fundo) passam pelo proxy local quando há assetsDir
		// e o host é proxyável (0024): a UI as serve do cache, sem bater no CDN da
		// fonte. Sem isso, saem como a URL direta (degradação). Similares/coleção
		// (muitas miniaturas) seguem diretas de propósito — evita rajada de proxy.
		PosterURL:     s.urlImagemServida(richfields.StrPtrDe(campos, "poster_url")),
		BackdropURL:   s.urlImagemServida(richfields.StrPtrDe(campos, "backdrop_url")),
		Videos:        richfields.VideosDe(campos),
		Collection:    colecao,
		Similar:       similares,
		Cast:          cast,
		Crew:          crew,
		Companies:     companies,
		ExternalLinks: richfields.LinksExternos(campos, tmdbTipo, tmdbID),
		ResolvedLanguages: map[string]string{
			"title":       provTitulo,
			"description": provDesc,
			"tagline":     provSlogan,
			"genres":      provGeneros,
			"keywords":    provKeywords,
		},
	}
}

// nsDe embrulha uma string num sql.NullString (inválido quando vazia) — para a cadeia
// de idiomas ignorar a âncora ausente. Fica no server (depende de database/sql).
func nsDe(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
