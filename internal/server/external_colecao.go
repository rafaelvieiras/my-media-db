package server

// Este arquivo implementa GET /api/v1/external/{source}/collection/{id} (briefing 0024,
// follow-up do 0023): a COLEÇÃO externa como página própria — nome/sinopse/artes do
// conjunto e a lista das PARTES (filmes) navegáveis ao detalhe externo. A fonte é
// consultada via a capacidade OPCIONAL provider.ColecaoProvider (type-assert); fontes
// sem coleção degradam com 503. Mono-idioma pragmático (idioma ativo), sem cache
// re-componível — o multi-idioma pleno da coleção fica como follow-up.

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// colecaoExternaDTO é a resposta de GET /external/{source}/collection/{id}: os
// metadados da coleção e suas partes. As artes do HERO (pôster/fundo) passam pelo
// proxy local (0024); as miniaturas das partes seguem DIRETAS (consistente com
// similares/coleção do detalhe externo). `parts` sai sempre como `[]` (nunca null).
type colecaoExternaDTO struct {
	Source      string  `json:"source"`
	SourceID    string  `json:"source_id"`
	Name        string  `json:"name"`
	Overview    *string `json:"overview"`
	PosterURL   *string `json:"poster_url"`
	BackdropURL *string `json:"backdrop_url"`
	// InternalID é o id da entity coleção interna quando esta coleção JÁ foi
	// importada (0025/0026); nil caso contrário. A UI então troca "Importar coleção"
	// por "Ver coleção" (link ao destino interno).
	InternalID *string           `json:"internal_id"`
	Parts      []parteColecaoDTO `json:"parts"`
}

// parteColecaoDTO é um item da coleção (mesmo shape enxuto de um resultado de busca):
// id na fonte, tipo, título, ano, sinopse e a miniatura direta.
type parteColecaoDTO struct {
	SourceID  string  `json:"source_id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Year      *string `json:"year"`
	Overview  *string `json:"overview"`
	PosterURL *string `json:"poster_url"`
	// InternalID é o id da entity interna desta parte quando ela JÁ foi importada
	// (0026); nil caso contrário. A UI então mostra "na biblioteca" e linka ao
	// destino interno em vez de oferecer Importar.
	InternalID *string `json:"internal_id"`
}

// getExternalCollection trata GET /api/v1/external/{source}/collection/{id}?lang=. 503
// sem fonte externa (ou fonte sem a capacidade de coleção); 400 lang fora dos
// permitidos; 404 coleção inexistente na fonte; 502 fonte falhou; 200 com a coleção.
func (s *Server) getExternalCollection(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cp, ok := s.searchProvider.(provider.ColecaoProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"colecao externa indisponivel (fonte externa nao configurada)")
		return
	}

	source := chi.URLParam(r, "source")
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

	col, err := cp.BuscarColecao(ctx, id, lang)
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			writeError(w, http.StatusNotFound, "not_found", "colecao nao encontrada na fonte")
			return
		}
		respondFonteIndisponivel(w)
		return
	}

	writeJSON(w, http.StatusOK, s.montarColecaoExterna(ctx, source, col))
}

// montarColecaoExterna compõe o DTO da coleção: reescreve as artes do hero pelo proxy
// local (urlImagemServida), anota o id interno da coleção e de cada parte já
// importada (interno-first, 0026) e copia as partes com a miniatura direta.
func (s *Server) montarColecaoExterna(ctx context.Context, source string, col provider.Colecao) colecaoExternaDTO {
	partes := make([]parteColecaoDTO, 0, len(col.Parts))
	for _, p := range col.Parts {
		partes = append(partes, parteColecaoDTO{
			SourceID:   p.SourceID,
			Type:       p.Type,
			Title:      p.Title,
			Year:       p.Year,
			Overview:   p.Overview,
			PosterURL:  p.PosterURL,
			InternalID: s.idInternoItem(ctx, source, p.SourceID),
		})
	}
	return colecaoExternaDTO{
		Source:      source,
		SourceID:    col.SourceID,
		Name:        col.Name,
		Overview:    col.Overview,
		PosterURL:   s.urlImagemServida(col.PosterURL),
		BackdropURL: s.urlImagemServida(col.BackdropURL),
		InternalID:  s.idInternoColecao(ctx, source, col.SourceID),
		Parts:       partes,
	}
}
