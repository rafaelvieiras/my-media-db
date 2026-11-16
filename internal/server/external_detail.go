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

// This file implements GET /api/v1/external/{source}/{type}/{id}: the RICH EXTERNAL
// DETAIL the owner sees BEFORE importing (like TMDB). It pulls the complete item from
// the source, PERSISTS on the 1st call (external_detail cache, through the
// externaldetail service) and serves from the cache on the following ones. It resolves
// the TRANSLATABLE fields (title, synopsis, tagline, genres, keywords) in the ACTIVE
// language through the settings fallback chain; the NEUTRAL ones (runtime,
// certification, backdrop, videos, collection, similars, rating, links) come once.
// Viewing the external item NEVER creates an entity.
//
// The resolution/types of the rich fields live in internal/richfields — SHARED with the
// entity resolver (GET /entities/{id}), so both screens have the SAME shape. The age
// rating is per REGION: the region of the active language (pt-BR→BR), with a US FALLBACK
// when the language carries no region or the source does not provide one.

// detalheExternoDTO is the response of GET /external/{source}/{type}/{id}: the rich
// TMDB set, resolved in the active language. Lists always come out as `[]` (never null)
// to simplify the frontend; pointers come out as null when absent. WITHOUT credits nor a
// local rating — an explicit decision, each one being tracked separately.
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
	// AiringStatus is the series' AIRING situation: string RAW from the source, verbatim
	// (parity with the internal preview; the UI translates it to pt-BR). omitempty/absent
	// for a movie or when the source does not report it.
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

	// ResolvedLanguages is the language provenance of the translatable fields
	// (ADR-0019), `""` = no effective language. It mirrors the same field of the entity
	// detail.
	ResolvedLanguages map[string]string `json:"resolved_languages"`
}

// provedorDetalhe resolves the PARSE provider (codecPara, ADR-0024) of the route's
// SOURCE for the external detail. It writes 503 and returns ok=false when the detail is
// turned off (no database) OR the source is totally unknown to the binary (neither in
// `codecs` nor in `providers`) -- in those cases PayloadFresco would not even be called
// with any benefit. Unlike before, it NO LONGER gates on a registered live search: a
// source without a credential can still serve an already cached payload (PayloadFresco
// decides fresh/stale/error on its own).
func (s *Server) provedorDetalhe(w http.ResponseWriter, source string) (provider.Provider, bool) {
	if s.externalDetail != nil {
		if prov, ok := s.codecPara(source); ok {
			return prov, true
		}
	}
	writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
		"detalhe externo indisponível (fonte externa não configurada)")
	return nil, false
}

// getExternalDetail handles GET /api/v1/external/{source}/{type}/{id}?lang=. 503 without
// an external source; 422 type outside movie|tv; 400 lang outside the allowed ones; 404
// item that does not exist in the source; 502 the source failed; 200 with the rich
// detail resolved.
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

	// Pulls (or serves from the cache) ALL the allowed translations in a single round
	// trip; the per-language resolution happens here, at read-time.
	raw, err := s.externalDetail.PayloadFresco(ctx, source, id, tipo, cfg.Allowed, false)
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
		// Invalid raw payload: treated as a source failure (the cache retries next time).
		respondFonteIndisponivel(w)
		return
	}
	preencherSimilares(prov, raw, source, id, campos)

	dto := s.montarDetalheExterno(ctx, source, id, tipo, lang, cfg, campos)
	writeJSON(w, http.StatusOK, dto)
}

// montarDetalheExterno composes the rich DTO from the canonical map: it resolves the
// translatable fields through the fallback chain (anchored on the item's
// original_language) and extracts the neutral ones through richfields (shared with the
// entity resolver). The certification uses the region of the active language with a US
// fallback.
func (s *Server) montarDetalheExterno(ctx context.Context, source, id, tipo, lang string, cfg languageSettings, campos map[string]any) detalheExternoDTO {
	original := richfields.ComoStringMapa(campos, "original_language")
	cadeia := cfg.cadeiaDeIdiomas(lang, nsDe(original))

	titulo, provTitulo := richfields.ResolverEscalarTraduzivel(campos, "title", cadeia)
	descricao, provDesc := richfields.ResolverEscalarTraduzivel(campos, "description", cadeia)
	slogan, provSlogan := richfields.ResolverEscalarTraduzivel(campos, "tagline", cadeia)
	generos, provGeneros := richfields.ResolverListaTraduzivel(campos, "tags", cadeia)
	keywords, provKeywords := richfields.ResolverListaTraduzivel(campos, "keywords", cadeia)

	releaseDate := richfields.StrPtrDe(campos, "release_date")

	// Franchise collection: when it exists and was already imported, it annotates the
	// internal id so the UI links to the internal destination instead of the external page.
	// ColecaoDe is pure (it leaves InternalID nil); the DB fill-in happens here.
	colecao := richfields.ColecaoDe(campos)
	if colecao != nil {
		colecao.InternalID = s.idInternoColecao(ctx, source, colecao.SourceID)
	}

	// Internal-aware similars: each already imported similar gets the internal id, so the
	// UI links to the internal destination (/entities/{id}) instead of reopening the
	// external one. SimilaresDe is pure (InternalID nil); the DB fill-in happens here. N
	// is small (the source limits the similars) and the lookup is indexed — fine for
	// self-hosted.
	similares := richfields.SimilaresDe(campos)
	for i := range similares {
		similares[i].InternalID = s.idInternoItem(ctx, source, similares[i].SourceID)
	}

	// Credits (cast/crew/production companies): extracted pure through richfields (the same
	// shape as the entity resolver). The internal-first logic annotates the person_id of
	// each credit already cataloged by SOME import — the source is that of the route itself
	// (the canonical map came from ONE call to this source). Then it proxies
	// profile_url/logo_url, like the poster/backdrop — richfields has no access to the
	// proxy.
	cast := richfields.CastDe(campos)
	crew := richfields.CrewDe(campos)
	companies := richfields.CompaniesDe(campos)
	s.anotarCreditosInternos(ctx, source, cast, crew)
	s.proxyarCreditos(cast, crew, companies)

	// TMDB link only when the source is TMDB itself (the handler has the route's type+id).
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
		// The HERO artwork (poster/background) goes through the local proxy when there is an
		// assetsDir and the host is proxyable: the UI serves them from the cache, without
		// hitting the source CDN. Without that, they come out as the direct URL
		// (degradation). Similars/collection (many thumbnails) stay direct on purpose — it
		// avoids a burst of proxying.
		PosterURL:   s.urlImagemServida(richfields.StrPtrDe(campos, "poster_url")),
		BackdropURL: s.urlImagemServida(richfields.StrPtrDe(campos, "backdrop_url")),
		Videos:      richfields.VideosDe(campos),
		Collection:  colecao,
		Similar:     similares,
		Cast:        cast,
		Crew:        crew,
		Companies:   companies,
		// empty imdb/tvdb: the EXTERNAL detail (an item not yet imported) has no
		// cross-source IDs resolved the way the resolver has (only what the source itself
		// already mapped into `external_links` shows up here).
		ExternalLinks: richfields.LinksExternos(campos, tmdbTipo, tmdbID, "", ""),
		ResolvedLanguages: map[string]string{
			"title":       provTitulo,
			"description": provDesc,
			"tagline":     provSlogan,
			"genres":      provGeneros,
			"keywords":    provKeywords,
		},
	}
}

// nsDe wraps a string in a sql.NullString (invalid when empty) — so the language chain
// ignores the absent anchor. It lives in the server (it depends on database/sql).
func nsDe(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
