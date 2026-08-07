package enrich

// artes.go é o ramo do enrich que povoa o INVENTÁRIO DE ARTES (briefing 0048): após o
// import/enrich primário, itera os providers que implementam provider.ArteProvider (a
// fanart.tv), busca a lista de artes do item pelos ids externos e PERSISTE o inventário
// como uma camada media_external por fonte de arte — SEM baixar binários (lazy: o
// download real é só da arte selecionada, via cachearArte). É idempotente (UPSERT por
// UNIQUE(entity_id, source)) e best-effort: sem a chave configurada, sem id ou sem match,
// degrada em silêncio (regressão zero). As artes de temporada (Season) são roteadas à
// entity da temporada correspondente.

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// AtualizarInventarioArtes é o wrapper PÚBLICO de enriquecerArtes (briefing 0088, ação
// "Baixar Artes Faltantes" do menu Gerenciar): permite ao chamador (o handler HTTP de
// POST /entities/{id}/materialize-missing-media) disparar uma nova consulta ao(s)
// ArteProvider(s) registrado(s) SEM passar por todo o Enriquecer (que também re-puxaria
// metadados/árvore). Delega inteiramente à função já existente — sem duplicar lógica.
// Best-effort por natureza (mesmo contrato de enriquecerArtes): nunca retorna erro, já
// que a ausência de id externo/capacidade/match é degradação silenciosa esperada
// (AGENTS.md §6). O `error` de retorno existe só para uma assinatura estável caso um
// erro real (ex.: banco fora do ar) precise propagar no futuro; hoje é sempre nil.
func (s *Servico) AtualizarInventarioArtes(ctx context.Context, entityID, tipo string, langs []string) error {
	s.enriquecerArtes(ctx, entityID, tipo, langs)
	return nil
}

// enriquecerArtes busca e persiste o inventário de artes de `entityID` (tipo movie|tv)
// em cada provider com a capacidade ArteProvider. Best-effort: erro/ausência degrada em
// silêncio. Chamado após o import primário do filme e após a árvore da série (quando as
// temporadas já existem, para rotear as artes de temporada).
func (s *Servico) enriquecerArtes(ctx context.Context, entityID, tipo string, langs []string) {
	ids, ok := s.idsExternosDe(ctx, entityID)
	if !ok {
		return // sem nenhum id externo: nada a cruzar
	}
	for _, prov := range s.providers.All() {
		ap, ok := prov.(provider.ArteProvider)
		if !ok {
			continue
		}
		artes, err := ap.BuscarArtes(ctx, ids, tipo, langs)
		if err != nil {
			log.Printf("enrich: artes de %s na fonte %q falharam (segue): %v", entityID, prov.Nome(), err)
			continue
		}
		if len(artes) == 0 {
			continue
		}
		if err := s.persistirInventarioArtes(ctx, entityID, prov.Nome(), artes); err != nil {
			log.Printf("enrich: persistir inventario de artes de %s (fonte %q) falhou (segue): %v", entityID, prov.Nome(), err)
		}
	}
}

// idsExternosDe lê o external_ids da entity e monta o provider.ExternalIDs (tmdb/imdb/
// tvdb). ok=false quando não há NENHUM id útil (o ArteProvider não teria como cruzar).
func (s *Servico) idsExternosDe(ctx context.Context, entityID string) (provider.ExternalIDs, bool) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return provider.ExternalIDs{}, false
	}
	ids := provider.ExternalIDs{}
	if ent.ExternalIdsJson.Valid && ent.ExternalIdsJson.String != "" {
		bruto := map[string]any{}
		if json.Unmarshal([]byte(ent.ExternalIdsJson.String), &bruto) == nil {
			ids.Tmdb = comoString(bruto["tmdb"])
			ids.Imdb = comoString(bruto["imdb"])
			ids.Tvdb = comoString(bruto["tvdb"])
		}
	}
	if ids.Tmdb == "" && ids.Imdb == "" && ids.Tvdb == "" {
		return provider.ExternalIDs{}, false
	}
	return ids, true
}

// persistirInventarioArtes agrupa as artes por entity ALVO (a própria entity para as
// artes gerais e "all"; a entity da temporada para as artes com Season numérica) e grava
// um envelope de inventário por alvo, sob a fonte de arte `source`. Idempotente por
// (entity_id, source).
func (s *Servico) persistirInventarioArtes(ctx context.Context, entityID, source string, artes []provider.Arte) error {
	porAlvo := s.agruparArtesPorAlvo(ctx, entityID, artes)
	for alvo, lista := range porAlvo {
		payload, err := json.Marshal(map[string]any{
			provider.ChaveEnvelopeInventarioArtes: inventarioPorKind(lista),
		})
		if err != nil {
			return err
		}
		if _, err := s.escrita().UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
			ID:          s.novoID(),
			EntityID:    alvo,
			Source:      source,
			SourceID:    nsValid(""),
			PayloadJson: nsValid(string(payload)),
		}); err != nil {
			return err
		}
	}
	return nil
}

// agruparArtesPorAlvo mapeia cada arte à entity ALVO: a arte SEM Season (ou Season="all")
// vai à própria entity; a arte com Season NUMÉRICA vai à entity da temporada cujo
// season_number casa. Uma temporada ausente (arte de temporada não importada) faz a arte
// ser DESCARTADA (não há onde anexar) — degradação silenciosa. Devolve alvo→[]Arte.
func (s *Servico) agruparArtesPorAlvo(ctx context.Context, entityID string, artes []provider.Arte) map[string][]provider.Arte {
	temporadas := s.temporadasPorNumero(ctx, entityID)
	out := map[string][]provider.Arte{}
	for _, a := range artes {
		alvo, ok := alvoDaArte(entityID, a, temporadas)
		if !ok {
			continue
		}
		out[alvo] = append(out[alvo], a)
	}
	return out
}

// alvoDaArte resolve a entity alvo de UMA arte. Season vazia ou "all" → a própria entity
// (`entityID`); Season numérica → a temporada correspondente (ok=false quando ela não foi
// importada). Season não-numérica desconhecida → a própria entity (defensivo).
func alvoDaArte(entityID string, a provider.Arte, temporadas map[int]string) (string, bool) {
	if a.Season == "" || a.Season == "all" {
		return entityID, true
	}
	n, err := strconv.Atoi(a.Season)
	if err != nil {
		return entityID, true // valor inesperado: não perde a arte, anexa à entity
	}
	tempID, ok := temporadas[n]
	if !ok {
		return "", false // temporada não importada: descarta a arte
	}
	return tempID, true
}

// temporadasPorNumero mapeia season_number → id da entity temporada, lido das relações
// filhas (kind=tv_season, sort_index=season_number). Vazio para filme (sem temporadas) —
// nesse caso nenhuma arte tem Season, então o mapa nem é consultado.
func (s *Servico) temporadasPorNumero(ctx context.Context, entityID string) map[int]string {
	out := map[int]string{}
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, entityID)
	if err != nil {
		return out
	}
	for _, r := range rels {
		if r.Kind == relacaoTemporada && r.SortIndex.Valid {
			out[int(r.SortIndex.Int64)] = r.ChildID
		}
	}
	return out
}

// inventarioPorKind converte a lista de artes num mapa kind→[]ArteInventario (o corpo do
// envelope). Preserva lang/likes/disc/season de todas (o /artwork filtra depois; o
// multi-idioma é fundacional). A ordem dentro de cada kind segue a da fonte.
func inventarioPorKind(artes []provider.Arte) map[string][]provider.ArteInventario {
	out := map[string][]provider.ArteInventario{}
	for _, a := range artes {
		out[a.Kind] = append(out[a.Kind], provider.ArteInventario{
			URL:      a.URL,
			Lang:     a.Lang,
			Likes:    a.Likes,
			DiscType: a.DiscType,
			Disc:     a.Disc,
			Season:   a.Season,
		})
	}
	return out
}
