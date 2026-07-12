package server

// artwork.go implementa GET /api/v1/entities/{id}/artwork (briefing 0048): a arte
// DISPONÍVEL de uma entity AGRUPADA por kind, mesclando três fontes —
//
//	1. os assets LOCAIS do dono (upload origin='user' e cache origin='external', 0017/0015),
//	2. o pôster/fundo do TMDB (e demais camadas com provider) já cacheados como URL,
//	3. o INVENTÁRIO da fanart.tv (media_external source='fanarttv', 0048) — lazy, sem baixar.
//
// Toda URL sai JÁ passada pelo proxy de imagem (arteSrc/urlImagemServida) para a UI não
// depender do CDN da fonte em runtime. A resposta traz também `available: {kind: count}`
// para os selos do card (consumido pelo 0049). É só leitura.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// artworkItem é uma arte no agrupamento por kind do /artwork. URL já vem proxyada; lang/
// likes vêm do inventário (fanart.tv) e são vazios/zero para as demais fontes. Source é a
// proveniência ('user'/'external'/'tmdb'/'fanarttv'); IsLocal marca o asset materializado
// (servido do disco), IsPreferred a escolha do dono (só para asset local).
type artworkItem struct {
	URL         string `json:"url"`
	Lang        string `json:"lang,omitempty"`
	Likes       int    `json:"likes,omitempty"`
	Source      string `json:"source"`
	DiscType    string `json:"disc_type,omitempty"`
	Disc        int    `json:"disc,omitempty"`
	Season      string `json:"season,omitempty"`
	IsPreferred bool   `json:"is_preferred"`
	IsLocal     bool   `json:"is_local"`
	// AssetID identifica o asset materializado (só quando IsLocal): a UI usa em
	// PUT /assets/{id}/prefer e DELETE /assets/{id}. Vazio para arte de inventário.
	AssetID string `json:"asset_id,omitempty"`
	// SrcURL é a URL CRUA na fonte (só para arte de inventário, não materializada): a
	// UI a envia no POST /artwork/select para baixar/preferir. Vazio quando IsLocal.
	SrcURL string `json:"src_url,omitempty"`
}

// getEntityArtwork trata GET /api/v1/entities/{id}/artwork. 404 entity inexistente; 200
// com `{ [kind]: [artworkItem...], ..., available: {kind:count} }`. Mescla assets locais
// + pôster/fundo das camadas externas + inventário fanart.tv, tudo por kind.
func (s *Server) getEntityArtwork(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	grupos := map[string][]artworkItem{}
	kindsExternosLocais := s.coletarArtesLocais(ctx, id, grupos)
	s.coletarArtesDeCamadas(ctx, id, kindsExternosLocais, grupos)
	s.coletarInventarioFanart(ctx, id, grupos)

	writeJSON(w, http.StatusOK, montarRespostaArtwork(grupos))
}

// coletarArtesLocais adiciona os assets materializados da entity (origin user/external)
// agrupados por kind e devolve o CONJUNTO de kinds que já têm um asset origin='external'
// (para não duplicar o mesmo pôster/fundo do TMDB, que é a origem daquele cache).
func (s *Server) coletarArtesLocais(ctx context.Context, id string, grupos map[string][]artworkItem) map[string]bool {
	externos := map[string]bool{}
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		return externos
	}
	for _, a := range assets {
		grupos[a.Kind] = append(grupos[a.Kind], artworkItem{
			URL:         assetFilePath(a.EntityID, a.ID),
			Source:      a.Origin,
			IsPreferred: metaPreferred(a.MetaJson),
			IsLocal:     true,
			AssetID:     a.ID,
		})
		if a.Origin == originExternal {
			externos[a.Kind] = true
		}
	}
	return externos
}

// coletarArtesDeCamadas adiciona o pôster/fundo das camadas media_external que têm
// provider (ex.: TMDB): mapeia cada payload para o canônico e extrai poster_url→poster e
// backdrop_url→fanart, servidos via proxy. Pula o kind que já tem asset origin='external'
// local (`kindsExternos`) — é a mesma imagem já materializada. Camadas de inventário
// (fanart.tv) devolvem canônico vazio no MapearDetalhe e são ignoradas aqui.
func (s *Server) coletarArtesDeCamadas(ctx context.Context, id string, kindsExternos map[string]bool, grupos map[string][]artworkItem) {
	camadas, err := s.leitura().ListarMediaExternalPorEntity(ctx, id)
	if err != nil {
		return
	}
	for _, c := range camadas {
		prov, ok := s.providers.Get(c.Source)
		if !ok || !c.PayloadJson.Valid {
			continue
		}
		campos, err := prov.MapearDetalhe(json.RawMessage(c.PayloadJson.String))
		if err != nil {
			continue
		}
		s.adicionarArteDeCampo(campos, "poster_url", kindPoster, c.Source, kindsExternos, grupos)
		s.adicionarArteDeCampo(campos, "backdrop_url", kindFanart, c.Source, kindsExternos, grupos)
	}
}

// adicionarArteDeCampo adiciona a arte de UM campo do canônico (poster_url/backdrop_url)
// ao grupo do `kind`, proxyando a URL. Não duplica quando o kind já tem cache local
// origin='external' (kindsExternos) nem quando o campo é vazio/ausente.
func (s *Server) adicionarArteDeCampo(campos map[string]any, campo, kind, source string, kindsExternos map[string]bool, grupos map[string][]artworkItem) {
	if kindsExternos[kind] {
		return
	}
	u, _ := campos[campo].(string)
	if u == "" {
		return
	}
	grupos[kind] = append(grupos[kind], artworkItem{
		URL:    arteSrc(s.urlImagemServida(&u)),
		Source: source,
		SrcURL: u,
	})
}

// coletarInventarioFanart adiciona o inventário lazy da fanart.tv (media_external
// source='fanarttv'): parseia o envelope e emite um artworkItem por arte de cada kind,
// com lang/likes/disc/season preservados e a URL proxyada. Ausente/vazio degrada (sem
// inventário — nada a adicionar).
func (s *Server) coletarInventarioFanart(ctx context.Context, id string, grupos map[string][]artworkItem) {
	row, err := s.leitura().ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: id,
		Source:   "fanarttv",
	})
	if err != nil || !row.PayloadJson.Valid {
		return
	}
	inv := parseInventarioFanart(row.PayloadJson.String)
	for kind, artes := range inv {
		for _, a := range artes {
			url := a.URL
			grupos[kind] = append(grupos[kind], artworkItem{
				URL:      arteSrc(s.urlImagemServida(&url)),
				Lang:     a.Lang,
				Likes:    a.Likes,
				Source:   "fanarttv",
				DiscType: a.DiscType,
				Disc:     a.Disc,
				Season:   a.Season,
				SrcURL:   a.URL,
			})
		}
	}
}

// parseInventarioFanart extrai o mapa kind→[]ArteInventario do envelope de inventário
// (ChaveEnvelopeInventarioArtes). Payload malformado/ausente → mapa vazio.
func parseInventarioFanart(payload string) map[string][]provider.ArteInventario {
	var env map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &env) != nil {
		return nil
	}
	blob, ok := env[provider.ChaveEnvelopeInventarioArtes]
	if !ok || len(blob) == 0 {
		return nil
	}
	var inv map[string][]provider.ArteInventario
	if json.Unmarshal(blob, &inv) != nil {
		return nil
	}
	return inv
}

// montarRespostaArtwork serializa os grupos por kind mais o `available: {kind:count}`. As
// chaves de kind e a chave "available" coexistem no topo (nenhum kind se chama
// "available"), como o contrato do briefing 0048.
func montarRespostaArtwork(grupos map[string][]artworkItem) map[string]any {
	out := map[string]any{}
	available := map[string]int{}
	for kind, itens := range grupos {
		out[kind] = itens
		available[kind] = len(itens)
	}
	out["available"] = available
	return out
}

// arteSrc desreferencia a URL já resolvida pelo proxy (urlImagemServida devolve *string),
// devolvendo "" quando nil. Isola o padrão de deref para o /artwork.
func arteSrc(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
