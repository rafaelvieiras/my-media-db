package server

// artwork_missing.go implements POST /api/v1/entities/{id}/download-missing-artwork:
// it downloads the BEST artwork available for each kind missing in the catalog
// completeness of the item open in the detail. It invents no new
// download/application path -- it reuses the SAME inventory aggregator
// (montarGruposArtwork, extracted from artwork.go) and the SAME
// baixarEMaterializarArte (artwork_select.go). The only genuinely new logic is
// the loop "for each missing kind, try the best candidate, best-effort among
// candidates of the same kind".

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// downloadMissingArtworkResultDTO is the response of POST
// /entities/{id}/download-missing-artwork (openapi.yaml
// DownloadMissingArtworkResult). Both fields are always present (never
// null) -- initialized as an empty slice, not nil, so they serialize as `[]`
// even when there is nothing to do.
type downloadMissingArtworkResultDTO struct {
	Downloaded  []string `json:"downloaded"`
	Unavailable []string `json:"unavailable"`
}

// downloadMissingArtwork handles POST /api/v1/entities/{id}/download-missing-artwork:
// it recalculates catalog_completeness LIVE (same path as the detail,
// resolverEntity/anotarCompletudeCatalogacao -- never the listing cache), filters
// the `artwork:<kind>` keys out of `missing` and, for each kind, tries to
// download the best available option (baixarMelhorArteDoKind). An item that is
// already complete or a draft (CatalogCompleteness nil) returns both lists empty
// without touching anything -- that is not an error. 404 entity does not exist;
// 503 without assetsDir configured; 500 only on a real database/infrastructure
// error (never 502 -- a download failure of ONE candidate tries the next one,
// best-effort).
func (s *Server) downloadMissingArtwork(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}
	if s.assetsIndisponivel(w) {
		return
	}

	// resolverEntity already runs anotarCompletudeCatalogacao on every call --
	// it is the SAME live path as the detail (GET /entities/{id}), never the
	// entity_catalog_completeness cache used by the listing.
	resolvida, encontrada, err := s.resolverEntity(ctx, id, "", true)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !encontrada {
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
		return
	}

	resultado := downloadMissingArtworkResultDTO{Downloaded: []string{}, Unavailable: []string{}}
	if resolvida.CatalogCompleteness == nil || resolvida.CatalogCompleteness.Complete {
		// Draft (nil) or already complete: nothing to do, it is not an
		// error.
		writeJSON(w, http.StatusOK, resultado)
		return
	}

	for _, chave := range resolvida.CatalogCompleteness.Missing {
		kind, ehArtwork := strings.CutPrefix(chave, prefixoArtwork)
		if !ehArtwork {
			continue
		}
		baixou, err := s.baixarMelhorArteDoKind(ctx, id, kind)
		if err != nil {
			respondErroInterno(w)
			return
		}
		if baixou {
			resultado.Downloaded = append(resultado.Downloaded, kind)
		} else {
			resultado.Unavailable = append(resultado.Unavailable, kind)
		}
	}
	writeJSON(w, http.StatusOK, resultado)
}

// baixarMelhorArteDoKind builds the groups of the inventory aggregator
// (montarGruposArtwork, artwork.go) and tries, in "best option" order first,
// to materialize ONE artwork of the `kind` via baixarEMaterializarArte. The
// "best" criterion: the SAME precedence of ordenarGruposArtwork -- is_principal
// desc, is_local_in_library desc, preferred-language/textless/english/other rank
// (rankIdiomaArte) -- with higher Likes (fanart.tv) as a tie-break INSIDE the
// same rank; on a total tie, the stable arrival order of the aggregator
// (sort.SliceStable preserves the original order among equals). Reordering here
// (instead of just reusing the order of montarGruposArtwork) is necessary
// because that aggregator does not consider Likes -- but it must NEVER discard
// is_principal/is_local/language to do so. Best-effort among candidates: a
// DOWNLOAD failure (errBaixarArte -- network/status/invalid type) tries the
// NEXT candidate of the same kind before giving up; it only returns
// (false, nil) after exhausting all of them (or when there is no candidate
// for the kind). A DATABASE/disk error (not a download one) propagates as
// err -- never absorbed.
func (s *Server) baixarMelhorArteDoKind(ctx context.Context, id, kind string) (bool, error) {
	grupos := s.montarGruposArtwork(ctx, id)
	candidatos := grupos[kind]
	if len(candidatos) == 0 {
		return false, nil
	}
	// idiomaDeBusca degrades silently to "" on a real database error (same
	// pattern as montarGruposArtwork) -- it only affects the language rank, it
	// never blocks the download.
	idioma, _ := s.idiomaDeBusca(ctx, id)
	base := idiomaBase(idioma)
	sort.SliceStable(candidatos, func(i, j int) bool {
		if candidatos[i].IsPrincipal != candidatos[j].IsPrincipal {
			return candidatos[i].IsPrincipal
		}
		if candidatos[i].IsLocalInLibrary != candidatos[j].IsLocalInLibrary {
			return candidatos[i].IsLocalInLibrary
		}
		ri, rj := rankIdiomaArte(candidatos[i].Lang, base), rankIdiomaArte(candidatos[j].Lang, base)
		if ri != rj {
			return ri < rj
		}
		if candidatos[i].Likes != candidatos[j].Likes {
			return candidatos[i].Likes > candidatos[j].Likes
		}
		return false // total tie: keep the original order (stable)
	})

	for _, cand := range candidatos {
		if cand.SrcURL == "" {
			// Item already materialized locally (IsLocal) or without a known
			// origin URL -- there is nothing to download, try the next one.
			continue
		}
		if _, err := s.baixarEMaterializarArte(ctx, id, cand.SrcURL, kind); err != nil {
			if errors.Is(err, errBaixarArte) {
				continue // download failure of this candidate -- try the next one
			}
			return false, err // a real disk/database error propagates
		}
		return true, nil
	}
	return false, nil
}
