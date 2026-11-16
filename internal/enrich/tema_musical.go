package enrich

// tema_musical.go is the enrich branch that fetches and persists the theme-music
// CANDIDATE (ADR-0025): after the primary import/enrich, it iterates the providers that
// implement provider.TemaMusicalProvider (ThemerrDB), fetches the item's candidate by the
// external ids and PERSISTS it (or the absence of a match) as a media_external envelope
// per theme source — the same technique as the artwork inventory (artes.go), but simpler:
// a single candidate, not a map per kind. It is idempotent (UPSERT by
// UNIQUE(entity_id, source)) and best-effort: without the capability, without an id or
// without a match, it degrades silently (zero regression). The actual download of the
// binary is lazy, triggered only by POST .../theme/download (internal/server).

import (
	"context"
	"encoding/json"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// AtualizarTemaMusical is the PUBLIC wrapper of enriquecerTemaMusical (same spirit as
// AtualizarInventarioArtes): it allows a future HTTP caller to trigger a new query to the
// registered TemaMusicalProvider(s) without going through the whole Enriquecer. Best-effort
// by nature (same contract as AtualizarInventarioArtes): it never returns a real error
// today; the `error` exists only for a stable signature.
func (s *Servico) AtualizarTemaMusical(ctx context.Context, entityID, tipo string, langs []string) error {
	s.enriquecerTemaMusical(ctx, entityID, tipo, langs)
	return nil
}

// enriquecerTemaMusical fetches and persists the theme-music candidate of `entityID`
// (type movie|tv) in each provider with the TemaMusicalProvider capability. Best-effort:
// an error/absence degrades silently. It is called at the SAME points as enriquecerArtes
// (enrich.go/serie.go): the candidate follows the same lifecycle as the artwork
// inventory, since both depend only on the external_ids of the item, with no step of
// their own.
func (s *Servico) enriquecerTemaMusical(ctx context.Context, entityID, tipo string, langs []string) {
	_ = langs // reserved (same signature as enriquecerArtes); ThemerrDB does not use a language
	ids, ok := s.idsExternosDe(ctx, entityID)
	if !ok {
		return // no external id at all: nothing to cross
	}
	for _, prov := range s.providers.All() {
		tp, ok := prov.(provider.TemaMusicalProvider)
		if !ok {
			continue
		}
		tema, achou, err := tp.BuscarTemaMusical(ctx, ids, tipo)
		if err != nil {
			log.Printf("enrich: tema musical de %s na fonte %q falhou (segue): %v", entityID, prov.Nome(), err)
			continue
		}
		if err := s.persistirCandidatoTema(ctx, entityID, prov.Nome(), tema, achou); err != nil {
			log.Printf("enrich: persistir candidato de tema musical de %s (fonte %q) falhou (segue): %v", entityID, prov.Nome(), err)
		}
	}
}

// persistirCandidatoTema writes the candidate envelope (or the absence of a match) into
// media_external, under the source `source`. It also writes when achou=false (an empty
// envelope, without the key) -- so that GET .../theme/candidato knows the query already
// happened, without hitting the API again on every screen load; the refresh only occurs in
// the next normal enrichment cycle (outside that scope). Idempotent by (entity_id, source).
func (s *Servico) persistirCandidatoTema(ctx context.Context, entityID, source string, tema provider.TemaMusical, achou bool) error {
	corpo := map[string]any{}
	if achou && tema.URL != "" {
		corpo[provider.ChaveEnvelopeTemaMusical] = tema.URL
	}
	payload, err := json.Marshal(corpo)
	if err != nil {
		return err
	}
	_, err = s.escrita().UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID:          s.novoID(),
		EntityID:    entityID,
		Source:      source,
		SourceID:    nsValid(""),
		PayloadJson: nsValid(string(payload)),
	})
	return err
}
