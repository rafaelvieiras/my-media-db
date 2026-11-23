package server

// artwork.go implements GET /api/v1/entities/{id}/artwork: the AVAILABLE artwork of an
// entity GROUPED by kind, merging three sources —
//
//	1. the owner's LOCAL assets (upload origin='user' and cache origin='external'),
//	2. the TMDB poster/background (and the other layers with a provider) already cached as a URL,
//	3. the fanart.tv INVENTORY (media_external source='fanarttv') — lazy, without downloading.
//
// Every URL leaves ALREADY passed through the image proxy (arteSrc/urlImagemServida) so the
// UI does not depend on the source CDN at runtime. The response also carries
// `available: {kind: count}` for the card badges (consumed by the artwork gallery). Read-only.

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// artworkItem is one artwork inside the by-kind grouping of /artwork. The URL already comes
// proxied; lang/likes come from the inventory (fanart.tv) and are empty/zero for the other
// sources. Source is the provenance ('user'/'external'/'tmdb'/'fanarttv'); IsLocal marks the
// materialized asset (served from disk, internal cache), IsPreferred the owner's choice (read
// from meta_json, compat — local asset only). IsLocalInLibrary/IsPrincipal distinguish the
// physical materialization in the library's REAL folder (asset.local_path/is_principal,
// dedicated columns) — always false for inventory/external-URL artwork (IsLocal=false).
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
	// IsLocalInLibrary is true when the materialized asset (IsLocal=true) was also saved
	// physically into the library's REAL folder (asset.local_path != null) — it mirrors the
	// "Saved to folder" badge of the gallery.
	IsLocalInLibrary bool `json:"is_local_in_library"`
	// IsPrincipal is true when this is the is_principal=1 asset of its (entity,kind)
	// (source of truth in its own column — distinct from IsPreferred/meta_json). At most
	// one true per kind.
	IsPrincipal bool `json:"is_principal"`
	// AssetID identifies the materialized asset (only when IsLocal): the UI uses it in
	// PUT /assets/{id}/prefer and DELETE /assets/{id}. Empty for inventory artwork.
	AssetID string `json:"asset_id,omitempty"`
	// SrcURL is the RAW URL at the source (inventory artwork only, not materialized): the
	// UI sends it in POST /artwork/select to download/prefer it. Empty when IsLocal.
	SrcURL string `json:"src_url,omitempty"`
}

// getEntityArtwork handles GET /api/v1/entities/{id}/artwork. 404 for an unknown entity;
// 200 with `{ [kind]: [artworkItem...], ..., available: {kind:count} }`. Merges local assets
// + poster/background from the external layers + fanart.tv inventory, all by kind.
func (s *Server) getEntityArtwork(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	grupos := s.montarGruposArtwork(ctx, id)

	writeJSON(w, http.StatusOK, montarRespostaArtwork(grupos))
}

// montarGruposArtwork concentrates the three collection calls (local assets +
// poster/background from the external layers + fanart.tv inventory) into a single
// kind→[]artworkItem map — extracted from the body of getEntityArtwork to be reused
// NON-HTTP by baixarMelhorArteDoKind (artwork_missing.go), without duplicating the
// source-merging logic. Same observable behavior as before: getEntityArtwork is still
// the only HTTP caller.
func (s *Server) montarGruposArtwork(ctx context.Context, id string) map[string][]artworkItem {
	grupos := map[string][]artworkItem{}
	kindsExternosLocais, sourceURLsMaterializadas := s.coletarArtesLocais(ctx, id, grupos)
	s.coletarArtesDeCamadas(ctx, id, kindsExternosLocais, grupos)
	s.coletarInventarioFanart(ctx, id, sourceURLsMaterializadas, grupos)
	// idiomaDeBusca already handles ambiguity/fallback to the global language --
	// a real database error degrades silently to "" (ordering falls back to
	// textless->en->any other, with no language preference).
	idioma, _ := s.idiomaDeBusca(ctx, id)
	ordenarGruposArtwork(grupos, idioma)
	return grupos
}

// ordenarGruposArtwork applies the ordering (is_principal desc, is_local_in_library desc)
// plus the language priority: when is_principal/is_local_in_library tie, the artwork in the
// owner's preferred language (`idiomaPreferido`, full locale format such as "pt-BR") wins;
// then textless (lang==""), then English, then any other. The last tiebreak preserves the
// original arrival order (sort.SliceStable -- the sources already come in the
// local->external->inventory order).
func ordenarGruposArtwork(grupos map[string][]artworkItem, idiomaPreferido string) {
	base := idiomaBase(idiomaPreferido) // "pt-BR" -> "pt"
	for _, itens := range grupos {
		sort.SliceStable(itens, func(i, j int) bool {
			if itens[i].IsPrincipal != itens[j].IsPrincipal {
				return itens[i].IsPrincipal // true first
			}
			if itens[i].IsLocalInLibrary != itens[j].IsLocalInLibrary {
				return itens[i].IsLocalInLibrary // true first
			}
			ri, rj := rankIdiomaArte(itens[i].Lang, base), rankIdiomaArte(itens[j].Lang, base)
			if ri != rj {
				return ri < rj
			}
			return false // tie: keeps the original order (stable)
		})
	}
}

// idiomaBase extracts the ISO 639-1 code from a full locale (the format used by
// app_settings.languages/idiomaDeBusca, e.g. "pt-BR"), lowercased: the part before the
// first "-". "pt-BR" -> "pt"; "en" -> "en"; "" -> "".
// Needed because the `lang` stored in the fanart.tv inventory is pure ISO 639-1
// ("pt", "en", "") -- comparing it directly against the full locale would never match.
func idiomaBase(locale string) string {
	if locale == "" {
		return ""
	}
	base, _, _ := strings.Cut(locale, "-")
	return strings.ToLower(base)
}

// rankIdiomaArte orders by language priority: 0=owner's preferred language, 1=textless
// (empty lang, common on fanart.tv for artwork without text), 2=English, 3=any other.
// `preferidoBase` already comes normalized (idiomaBase) so that it matches the pure
// ISO 639-1 `lang` of the inventory.
func rankIdiomaArte(lang, preferidoBase string) int {
	switch {
	case preferidoBase != "" && lang == preferidoBase:
		return 0
	case lang == "":
		return 1
	case lang == "en":
		return 2
	default:
		return 3
	}
}

// coletarArtesLocais adds the entity's materialized assets (origin user/external) grouped by
// kind and returns (1) the SET of kinds that already have an origin='external' asset (so the
// same TMDB poster/background, which is the origin of that cache, is not duplicated) and (2)
// per kind, the SET of source_url (meta.source_url) already materialized as an
// origin='external' asset — used by coletarInventarioFanart to avoid repeating, in the lazy
// inventory, artwork that was already downloaded through POST /artwork/select.
func (s *Server) coletarArtesLocais(ctx context.Context, id string, grupos map[string][]artworkItem) (map[string]bool, map[string]map[string]bool) {
	externos := map[string]bool{}
	sourceURLs := map[string]map[string]bool{}
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		return externos, sourceURLs
	}
	for _, a := range assets {
		grupos[a.Kind] = append(grupos[a.Kind], artworkItem{
			URL:              assetFilePath(a.EntityID, a.ID),
			Source:           a.Origin,
			IsPreferred:      metaPreferred(a.MetaJson),
			IsLocal:          true,
			IsLocalInLibrary: a.LocalPath.Valid && a.LocalPath.String != "",
			IsPrincipal:      a.IsPrincipal != 0,
			AssetID:          a.ID,
		})
		if a.Origin == originExternal {
			externos[a.Kind] = true
			if u := metaSourceURL(a.MetaJson); u != "" {
				if sourceURLs[a.Kind] == nil {
					sourceURLs[a.Kind] = map[string]bool{}
				}
				sourceURLs[a.Kind][u] = true
			}
		}
	}
	return externos, sourceURLs
}

// coletarArtesDeCamadas adds the poster/background of the media_external layers that have a
// provider (e.g. TMDB): maps each payload to the canonical shape and extracts
// poster_url→poster and backdrop_url→fanart, served through the proxy. Skips the kind that
// already has a local origin='external' asset (`kindsExternos`) — it is the same image
// already materialized. Inventory layers (fanart.tv) return an empty canonical in
// MapearDetalhe and are ignored here.
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

// adicionarArteDeCampo adds the artwork of ONE canonical field (poster_url/backdrop_url) to
// the group of `kind`, proxying the URL. It does not duplicate when the kind already has a
// local origin='external' cache (kindsExternos) nor when the field is empty/absent.
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

// coletarInventarioFanart adds the lazy inventory of ANY source with an artwork envelope
// already cached in media_external: until then it was hardcoded to source='fanarttv' (the
// only artwork source that existed at the time), which meant the inventory of
// MusicBrainz/Cover Art Archive (source='musicbrainz', a capability later gained for album
// covers) was never read back even when it had been written successfully by the write side
// (enrich.enriquecerArtes was already generic to any ArteProvider) — a real bug found in
// production: a music album never showed a cover even with the source answering with real
// data. Reads ALL the entity's cached layers (ListarMediaExternalPorEntity) and filters by
// the ENVELOPE (ChaveEnvelopeInventarioArtes), not by the source being registered RIGHT NOW
// — the same spirit of "cache survives without a provider" (ADR-0024): an inventory
// fetched yesterday stays visible even if that source's credential was removed later.
// Generic to any future source (e.g. Spotify) without requiring a change here. Parses each
// source's envelope and emits one artworkItem per artwork of each kind, with
// lang/likes/disc/season preserved and the URL proxied. A source without an inventory
// degrades silently (nothing to add). Skips artwork whose `url` was already materialized as
// an origin='external' asset of that kind (`materializadas`) — it avoids the same binary
// showing up twice (the local version already covers the card; the inventory candidate
// disappears).
func (s *Server) coletarInventarioFanart(ctx context.Context, id string, materializadas map[string]map[string]bool, grupos map[string][]artworkItem) {
	camadas, err := s.leitura().ListarMediaExternalPorEntity(ctx, id)
	if err != nil {
		return
	}
	for _, camada := range camadas {
		s.coletarInventarioDeFonte(camada, materializadas, grupos)
	}
}

// coletarInventarioDeFonte is the per-layer body of coletarInventarioFanart (extracted to
// iterate each media_external row without duplicating the parse/merge). A layer without the
// artwork envelope (a regular detail payload, e.g. tmdb/tvdb) degrades silently through
// parseInventarioFanart returning nil.
func (s *Server) coletarInventarioDeFonte(camada sqlcgen.MediaExternal, materializadas map[string]map[string]bool, grupos map[string][]artworkItem) {
	if !camada.PayloadJson.Valid {
		return
	}
	inv := parseInventarioFanart(camada.PayloadJson.String)
	for kind, artes := range inv {
		for _, a := range artes {
			if materializadas[kind][a.URL] {
				continue
			}
			url := a.URL
			grupos[kind] = append(grupos[kind], artworkItem{
				URL:      arteSrc(s.urlImagemServida(&url)),
				Lang:     a.Lang,
				Likes:    a.Likes,
				Source:   camada.Source,
				DiscType: a.DiscType,
				Disc:     a.Disc,
				Season:   a.Season,
				SrcURL:   a.URL,
			})
		}
	}
}

// parseInventarioFanart extracts the kind→[]ArteInventario map from the inventory envelope
// (ChaveEnvelopeInventarioArtes). A malformed/absent payload → empty map.
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

// montarRespostaArtwork serializes the groups by kind plus `available: {kind:count}`. The
// kind keys and the "available" key coexist at the top level (no kind is called
// "available"), as in the /artwork contract.
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

// arteSrc dereferences the URL already resolved by the proxy (urlImagemServida returns
// *string), returning "" when nil. Isolates the deref pattern for /artwork.
func arteSrc(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
