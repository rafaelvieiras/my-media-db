package enrich

// artes.go is the enrich branch that populates the ARTWORK INVENTORY: after the
// primary import/enrich, it iterates the providers that implement provider.ArteProvider
// (fanart.tv), fetches the list of artworks of the item by the external ids and PERSISTS
// the inventory as a media_external layer per artwork source — WITHOUT downloading
// binaries (lazy: the real download is only of the selected artwork, through cachearArte).
// It is idempotent (UPSERT by UNIQUE(entity_id, source)) and best-effort: with no
// configured key, no id or no match, it degrades silently (zero regression). The season
// artworks (Season) are routed to the entity of the matching season.

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// AtualizarInventarioArtes is the PUBLIC wrapper of enriquecerArtes (the "Baixar Artes
// Faltantes" action of the Gerenciar menu): it allows the caller (the HTTP handler of
// POST /entities/{id}/materialize-missing-media) to trigger a new query to the
// registered ArteProvider(s) WITHOUT going through the whole Enriquecer (which would also
// re-pull metadata/tree). It delegates entirely to the existing function — without
// duplicating logic. Best-effort by nature (same contract as enriquecerArtes): it never
// returns an error, since the absence of an external id/capability/match is the expected
// silent degradation (AGENTS.md §6). The returned `error` exists only for a stable
// signature in case a real error (e.g.: database down) needs to propagate in the future;
// today it is always nil.
func (s *Servico) AtualizarInventarioArtes(ctx context.Context, entityID, tipo string, langs []string) error {
	s.enriquecerArtes(ctx, entityID, tipo, langs)
	return nil
}

// enriquecerArtes fetches and persists the artwork inventory of `entityID` (type
// movie|tv) in each provider with the ArteProvider capability. Best-effort: an
// error/absence degrades silently. It is called after the primary import of the movie and
// after the series tree (when the seasons already exist, so that the season artworks can
// be routed).
func (s *Servico) enriquecerArtes(ctx context.Context, entityID, tipo string, langs []string) {
	ids, ok := s.idsExternosDe(ctx, entityID)
	if !ok {
		return // no external id at all: nothing to cross
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

// idsExternosDe reads the external_ids of the entity and builds the provider.ExternalIDs
// (tmdb/imdb/tvdb). ok=false when there is NO useful id at all (the ArteProvider would
// have nothing to cross).
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
			// Mbid (ADR-0027 item 6): MusicBrainz MBID of the music item
			// (artist/album/track) -- without it the MusicBrainz ArteProvider
			// (Cover Art Archive) never receives the id it needs to cross the
			// album cover.
			ids.Mbid = comoString(bruto["mbid"])
		}
	}
	if ids.Tmdb == "" && ids.Imdb == "" && ids.Tvdb == "" && ids.Mbid == "" {
		return provider.ExternalIDs{}, false
	}
	return ids, true
}

// persistirInventarioArtes groups the artworks by TARGET entity (the entity itself for
// the general and "all" artworks; the season entity for the artworks with a numeric
// Season) and writes one inventory envelope per target, under the artwork source
// `source`. Idempotent by (entity_id, source).
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

// agruparArtesPorAlvo maps each artwork to the TARGET entity: an artwork WITHOUT Season
// (or with Season="all") goes to the entity itself; an artwork with a NUMERIC Season goes
// to the entity of the season whose season_number matches. A missing season (season
// artwork not imported) makes the artwork be DISCARDED (there is nowhere to attach it) —
// silent degradation. It returns target→[]Arte.
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

// alvoDaArte resolves the target entity of ONE artwork. Empty Season or "all" → the
// entity itself (`entityID`); numeric Season → the matching season (ok=false when it was
// not imported). Unknown non-numeric Season → the entity itself (defensive).
func alvoDaArte(entityID string, a provider.Arte, temporadas map[int]string) (string, bool) {
	if a.Season == "" || a.Season == "all" {
		return entityID, true
	}
	n, err := strconv.Atoi(a.Season)
	if err != nil {
		return entityID, true // unexpected value: do not lose the artwork, attach it to the entity
	}
	tempID, ok := temporadas[n]
	if !ok {
		return "", false // season not imported: discard the artwork
	}
	return tempID, true
}

// temporadasPorNumero maps season_number → id of the season entity, read from the child
// relations (kind=tv_season, sort_index=season_number). Empty for a movie (no seasons) —
// in that case no artwork has a Season, so the map is not even consulted.
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

// inventarioPorKind converts the list of artworks into a kind→[]ArteInventario map (the
// body of the envelope). It preserves lang/likes/disc/season of all of them (the /artwork
// endpoint filters later; multi-language is foundational). The order within each kind
// follows the source.
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
