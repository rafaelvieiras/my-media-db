package server

// Este arquivo implementa GET /api/v1/external/{source}/album/{id} (briefing 0115
// Parte 2, ADR-0029): a tela de "faixas faltando" do álbum -- molde ESTRUTURAL de
// external_colecao.go (GET .../collection/{id}), adaptado à vertical de música: em
// vez de coleção→filmes, é álbum→faixas oficiais do release. A fonte é consultada
// via a capacidade OPCIONAL provider.MusicaAlbumProvider (BuscarAlbumPorMbid, o
// caminho DIRETO por MBID de release-group -- a entity `album` interna já grava
// esse MBID em external_ids); fontes sem essa capacidade degradam com 503.
// `internal_id` (interno-first, 0026) é preenchido tanto no álbum quanto em cada
// faixa já catalogada, casando por MBID (idInternoPorMbid, interno.go) -- o mesmo
// MBID que ImportarFaixaPossuida/importarFaixa/ImportarFaixaAvulsa gravam em
// external_ids ao criar a faixa. O segmento estático "album" tem precedência sobre
// o {type} de GET /external/{source}/{type}/{id} no chi (mesmo mecanismo do
// segmento "collection", ver server.go).

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// albumExternoDTO é a resposta de GET /external/{source}/album/{id}: os metadados
// do álbum e suas faixas oficiais (release escolhido pelo provider). `tracks` sai
// sempre como `[]` (nunca null).
type albumExternoDTO struct {
	Source   string  `json:"source"`
	SourceID string  `json:"source_id"`
	Name     string  `json:"name"`
	Year     *string `json:"year"`
	Genre    *string `json:"genre"`
	// InternalID é o id da entity álbum interna quando este álbum JÁ foi
	// importado; nil caso contrário. Na prática esta tela só é alcançada a partir
	// de um álbum já interno (link da TracklistAlbum), mas o campo segue o mesmo
	// contrato de coleção/série (interno-first, 0026).
	InternalID *string           `json:"internal_id"`
	Tracks     []faixaExternaDTO `json:"tracks"`
}

// faixaExternaDTO é uma faixa oficial do release (recording do MusicBrainz).
type faixaExternaDTO struct {
	SourceID    string  `json:"source_id"`
	Title       string  `json:"title"`
	TrackNumber int     `json:"track_number"`
	DiscNumber  int     `json:"disc_number"`
	ISRC        *string `json:"isrc"`
	// InternalID é o id da entity faixa interna quando esta faixa JÁ foi
	// importada (ligada ao álbum via entity_relation kind=music_album); nil caso
	// contrário -- a UI então oferece "Importar" para essa faixa.
	InternalID *string `json:"internal_id"`
}

// getExternalAlbum trata GET /api/v1/external/{source}/album/{id}?lang=. 503 sem
// fonte externa (ou fonte sem a capacidade seletiva de álbum); 404 álbum
// inexistente na fonte; 502 fonte falhou; 200 com o álbum e suas faixas.
func (s *Server) getExternalAlbum(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	source := chi.URLParam(r, "source")
	id := chi.URLParam(r, "id")

	prov, ok := s.providers.Get(source)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"album externo indisponivel (fonte externa nao configurada)")
		return
	}
	ap, ok := prov.(provider.MusicaAlbumProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"album externo indisponivel (fonte externa nao configurada)")
		return
	}

	ctx := r.Context()
	langs, err := s.idiomasPermitidos(ctx, w)
	if err != nil {
		return
	}

	alb, err := ap.BuscarAlbumPorMbid(ctx, id, langs)
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			writeError(w, http.StatusNotFound, "not_found", "album nao encontrado na fonte")
			return
		}
		respondFonteIndisponivel(w)
		return
	}

	writeJSON(w, http.StatusOK, s.montarAlbumExterno(ctx, source, alb))
}

// montarAlbumExterno compõe o DTO do álbum: anota o id interno do álbum e de cada
// faixa já importada (interno-first, 0026), casando por MBID.
func (s *Server) montarAlbumExterno(ctx context.Context, source string, alb provider.AlbumExterno) albumExternoDTO {
	faixas := make([]faixaExternaDTO, 0, len(alb.Faixas))
	for _, fx := range alb.Faixas {
		faixas = append(faixas, faixaExternaDTO{
			SourceID:    fx.MBID,
			Title:       fx.Title,
			TrackNumber: fx.TrackNumber,
			DiscNumber:  fx.DiscNumber,
			ISRC:        strOuNil(fx.ISRC),
			InternalID:  s.idInternoPorMbid(ctx, fx.MBID),
		})
	}
	return albumExternoDTO{
		Source:     source,
		SourceID:   alb.MBID,
		Name:       alb.Name,
		Year:       strOuNil(alb.Year),
		Genre:      strOuNil(alb.Genre),
		InternalID: s.idInternoPorMbid(ctx, alb.MBID),
		Tracks:     faixas,
	}
}

// strOuNil converte uma string vazia (campo ausente na fonte) em nil, para o JSON
// sair `null` em vez de `""` -- mesmo contrato de ponteiro opcional que
// AlbumExterno.Year/Genre e FaixaExterna.ISRC usam.
func strOuNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
