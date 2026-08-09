package server

// artwork_missing.go implementa POST /api/v1/entities/{id}/download-missing-artwork
// (briefing 0059): baixa a MELHOR arte disponível para cada kind faltando na
// completude de catalogação (briefing 0058) do item aberto no detalhe. Não
// inventa nenhum caminho novo de download/aplicação -- reusa o MESMO
// agregador de inventário do 0048 (montarGruposArtwork, extraído de
// artwork.go) e o MESMO baixarEMaterializarArte (0048, artwork_select.go).
// A única lógica genuinamente nova é o loop "para cada kind faltando, tenta
// a melhor candidata, best-effort entre candidatas do mesmo kind".

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// downloadMissingArtworkResultDTO é a resposta de POST
// /entities/{id}/download-missing-artwork (briefing 0059, openapi.yaml
// DownloadMissingArtworkResult). Ambos os campos são sempre presentes (nunca
// null) -- inicializados como slice vazio, não nil, para serializar `[]`
// mesmo quando não há nada a fazer.
type downloadMissingArtworkResultDTO struct {
	Downloaded  []string `json:"downloaded"`
	Unavailable []string `json:"unavailable"`
}

// downloadMissingArtwork trata POST /api/v1/entities/{id}/download-missing-artwork:
// recalcula catalog_completeness AO VIVO (mesmo caminho do detalhe,
// resolverEntity/anotarCompletudeCatalogacao, briefing 0058 -- nunca o cache
// da listagem), filtra as chaves `artwork:<kind>` de `missing` e, para cada
// kind, tenta baixar a melhor opção disponível (baixarMelhorArteDoKind).
// Item já completo ou draft (CatalogCompleteness nil) devolve as duas listas
// vazias sem tocar nada -- não é erro. 404 entity inexistente; 503 sem
// assetsDir configurado; 500 só em erro real de banco/infra (nunca 502 --
// falha de download de UMA candidata tenta a próxima, best-effort).
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

	// resolverEntity já roda anotarCompletudeCatalogacao (0058) a cada
	// chamada -- é o MESMO caminho ao vivo do detalhe (GET /entities/{id}),
	// nunca o cache de entity_catalog_completeness usado pela listagem.
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
		// Draft (nil) ou já completo: nada a fazer, não é erro (briefing
		// 0059 §4).
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

// baixarMelhorArteDoKind monta os grupos do agregador de inventário
// (montarGruposArtwork, artwork.go) e tenta, em ordem de "melhor opção"
// primeiro, materializar UMA arte do `kind` via baixarEMaterializarArte
// (0048). Critério de "melhor" (briefing 0059 §6, refinado pelo 0089): a
// MESMA precedência de ordenarGruposArtwork -- is_principal desc,
// is_local_in_library desc, rank de idioma preferido/textless/inglês/outro
// (rankIdiomaArte) -- com maior Likes (fanart.tv) como desempate DENTRO do
// mesmo rank; em empate total, a ordem estável de chegada do agregador
// (sort.SliceStable preserva a ordem original entre iguais). Reordenar aqui
// (em vez de só reusar a ordem de montarGruposArtwork) é necessário porque
// aquele agregador não considera Likes -- mas NUNCA pode descartar
// is_principal/is_local/idioma para fazer isso. Best-effort entre
// candidatas: uma falha de DOWNLOAD (errBaixarArte -- rede/status/tipo
// inválido) tenta a PRÓXIMA candidata do mesmo kind antes de desistir; só
// devolve (false, nil) depois de esgotar todas (ou quando não há nenhuma
// candidata para o kind). Erro de BANCO/disco (não de download) propaga como
// err -- nunca absorvido.
func (s *Server) baixarMelhorArteDoKind(ctx context.Context, id, kind string) (bool, error) {
	grupos := s.montarGruposArtwork(ctx, id)
	candidatos := grupos[kind]
	if len(candidatos) == 0 {
		return false, nil
	}
	// idiomaDeBusca degrada em silêncio pra "" em erro real de banco (mesmo
	// padrão de montarGruposArtwork) -- só afeta o rank de idioma, nunca
	// impede o download.
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
		return false // empate total: mantém a ordem original (stable)
	})

	for _, cand := range candidatos {
		if cand.SrcURL == "" {
			// Item já materializado localmente (IsLocal) ou sem URL de
			// origem conhecida -- não há o que baixar, tenta a próxima.
			continue
		}
		if _, err := s.baixarEMaterializarArte(ctx, id, cand.SrcURL, kind); err != nil {
			if errors.Is(err, errBaixarArte) {
				continue // falha de download desta candidata -- tenta a próxima
			}
			return false, err // erro real de disco/banco propaga
		}
		return true, nil
	}
	return false, nil
}
