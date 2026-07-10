package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo implementa a CAPACIDADE de coleção do provider (provider.ColecaoProvider,
// briefing 0024): GET {base}/collection/{id}?language=<lang ativo> e o mapeamento das
// partes para o canônico. É MONO-IDIOMA pragmático (busca no idioma ativo, sem o cache
// re-componível all-translations do 0022) — o multi-idioma pleno da coleção fica como
// follow-up. Sem rede no mapeamento (mapearColecao é PURO).

// colecaoDetalheTMDB espelha /collection/{id} (só os campos usados). As partes são os
// filmes da coleção.
type colecaoDetalheTMDB struct {
	ID           int64              `json:"id"`
	Name         string             `json:"name"`
	Overview     string             `json:"overview"`
	PosterPath   string             `json:"poster_path"`
	BackdropPath string             `json:"backdrop_path"`
	Parts        []parteColecaoTMDB `json:"parts"`
}

// parteColecaoTMDB é um item (parte) da coleção. media_type costuma vir "movie"; os
// campos title/release_date são de filme (mantemos name/first_air_date por robustez).
type parteColecaoTMDB struct {
	ID           int64  `json:"id"`
	MediaType    string `json:"media_type"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
}

// BuscarColecao puxa a coleção pelo id na fonte, localizada em `lang`. Reusa getBytes
// (404 → provider.ErrNaoEncontrado; demais status → erro). Cumpre provider.ColecaoProvider.
func (c *Client) BuscarColecao(ctx context.Context, colecaoID, lang string) (provider.Colecao, error) {
	if colecaoID == "" {
		return provider.Colecao{}, fmt.Errorf("tmdb: colecaoID vazio")
	}
	params := url.Values{}
	if lang != "" {
		params.Set("language", lang)
	}
	body, err := c.getBytes(ctx, "/collection/"+url.PathEscape(colecaoID), params)
	if err != nil {
		return provider.Colecao{}, err
	}
	var d colecaoDetalheTMDB
	if err := json.Unmarshal(body, &d); err != nil {
		return provider.Colecao{}, fmt.Errorf("tmdb: colecao invalida: %w", err)
	}
	return mapearColecao(d), nil
}

// mapearColecao converte a coleção crua no tipo canônico. Cada parte vira um
// ResultadoExterno (filme navegável ao detalhe externo). Descarta partes sem título.
func mapearColecao(d colecaoDetalheTMDB) provider.Colecao {
	col := provider.Colecao{
		Source:      "tmdb",
		SourceID:    itoa(d.ID),
		Name:        d.Name,
		Overview:    ptrNaoVazio(d.Overview),
		PosterURL:   posterURL(d.PosterPath),
		BackdropURL: backdropURL(d.BackdropPath),
		Parts:       make([]provider.ResultadoExterno, 0, len(d.Parts)),
	}
	for _, p := range d.Parts {
		titulo := primeiroNaoVazio(p.Title, p.Name)
		if titulo == "" {
			continue
		}
		col.Parts = append(col.Parts, provider.ResultadoExterno{
			Source:    "tmdb",
			SourceID:  itoa(p.ID),
			Type:      tipoDaParte(p.MediaType),
			Title:     titulo,
			Year:      anoDe(primeiroNaoVazio(p.ReleaseDate, p.FirstAirDate)),
			Overview:  ptrNaoVazio(p.Overview),
			PosterURL: posterURL(p.PosterPath),
		})
	}
	return col
}

// tipoDaParte normaliza o media_type da parte; coleção do TMDB é de filmes, então
// qualquer valor fora de movie|tv cai em "movie".
func tipoDaParte(mediaType string) string {
	if mediaType == "tv" {
		return "tv"
	}
	return "movie"
}

// backdropURL monta a URL direta do fundo (backdrop) da coleção no TMDB, ou nil
// quando não há backdrop_path. Análogo de posterURL, mas no tamanho de destaque.
func backdropURL(backdropPath string) *string {
	if backdropPath == "" {
		return nil
	}
	u := backdropBaseURL + backdropPath
	return &u
}
