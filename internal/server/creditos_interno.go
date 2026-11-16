package server

import (
	"context"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// This file resolves the interno-first of the CREDITS: when the person of a
// cast/crew credit has already been cataloged by SOME import (person
// deduplicated by source+source_id, see internal/enrich/creditos.go), the credit gets
// `person_id` — the signal the UI uses to link to the person page (GET /person/{id})
// instead of leaving the name unclickable. It mirrors idInternoItem/idInternoColecao
// (interno.go): same resilience (a real database error degrades to nil, sql.ErrNoRows
// is the normal "person not cataloged" case and does not log).

// anotarCreditosInternos fills the PersonID of each `cast`/`crew` credit whose person
// is already cataloged under `source`. An empty `source` (unidentified source) is a no-op —
// without a source there is no way to build the (source, source_id) key of the lookup. It mutates the slices
// in memory (in place), like proxyarCreditos.
func (s *Server) anotarCreditosInternos(ctx context.Context, source string, cast []richfields.Cast, crew []richfields.Crew) {
	if source == "" {
		return
	}
	for i := range cast {
		cast[i].PersonID = s.idPessoaPorFonte(ctx, source, cast[i].SourceID)
	}
	for i := range crew {
		crew[i].PersonID = s.idPessoaPorFonte(ctx, source, crew[i].SourceID)
	}
}

// idPessoaPorFonte returns the local id of the person cataloged under (source, sourceID), or
// nil when it does not exist, sourceID is empty, or a real database error degrades the lookup
// (it logs; the screen read never falls over because of this optional enrichment).
func (s *Server) idPessoaPorFonte(ctx context.Context, source, sourceID string) *string {
	if sourceID == "" {
		return nil
	}
	p, err := s.leitura().ObterPersonPorFonte(ctx, sqlcgen.ObterPersonPorFonteParams{
		Source:   source,
		SourceID: sourceID,
	})
	if err != nil {
		logSeErroReal("id interno da pessoa do credito", sourceID, err)
		return nil
	}
	return &p.ID
}

// fonteDoCampoExterno finds out which source (CamadaExterna) provided the NON-NIL value
// of `campo` in the priority fusion the resolver performs (same rule as
// camposExternosMesclados in internal/resolver/resolver.go: the FIRST of the list —
// index 0 = highest priority — with a non-nil value wins). "" when no layer has
// the field. Necessary because resolver.Resolver already returns the credits FLATTENED (without
// provenance) — but person.source must match the `source` that fed that
// credit in enrich (see internal/enrich/creditos.go: persistirCreditos writes
// source=source of the media_external that brought the cast/crew of that call).
func fonteDoCampoExterno(externo []resolver.CamadaExterna, campo string) string {
	for _, c := range externo {
		if v, ok := c.Campos[campo]; ok && v != nil {
			return c.Fonte
		}
	}
	return ""
}
