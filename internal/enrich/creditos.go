package enrich

// creditos.go persists the CAST/CREW/STUDIOS (MD6 Phase A) from the provider's
// canonical (keys cast/crew/companies). It is a RE-COMPOSABLE CACHE: the owner's data
// never lives here (it lives in the override). Idempotent on re-enrichment: it deletes the
// credits of (entity, source) and repopulates them, without duplicating. person/company are
// deduplicated by (source, source_id) through an upsert -- the same person in two works has
// ONE row. Everything is best-effort: the failure of one part degrades (logs and continues),
// it never aborts the enrich.

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

const (
	// kindCast/kindCrew/kindCompany discriminate the role of the credit (credit.kind column).
	kindCast    = "cast"
	kindCrew    = "crew"
	kindCompany = "company"
)

// dadosCredito aggregates the fields of a credit for criarCredito -- a single insertion
// point (cast/crew/company fill the fields that belong to them, the rest stays NULL).
type dadosCredito struct {
	entityID     string
	personID     sql.NullString
	companyID    sql.NullString
	kind         string
	department   sql.NullString
	job          sql.NullString
	character    sql.NullString
	ordering     int64
	episodeCount sql.NullInt64
	source       string
	sourceID     sql.NullString
}

// persistirCreditos deletes the credits of (entityID, source) and repopulates
// cast/crew/studios from the canonical. Best-effort: a failure of the delete aborts only
// this block (not the enrich); the others degrade per item. It is called on the
// import/enrich of a MOVIE and on the SERIES node (the aggregate cast belongs to the whole
// series).
func (s *Servico) persistirCreditos(ctx context.Context, entityID, source string, campos map[string]any) {
	if err := s.escrita().ApagarCreditosDaEntityFonte(ctx, sqlcgen.ApagarCreditosDaEntityFonteParams{
		EntityID: entityID,
		Source:   source,
	}); err != nil {
		log.Printf("enrich: apagar creditos de %s (%s) falhou (segue sem creditos): %v", entityID, source, err)
		return
	}
	s.persistirElenco(ctx, entityID, source, listaDeMapas(campos["cast"]))
	s.persistirEquipe(ctx, entityID, source, listaDeMapas(campos["crew"]))
	s.persistirProdutoras(ctx, entityID, source, listaDeMapas(campos["companies"]))
}

// persistirElenco creates the CAST credits (kind='cast'), each one linked to a deduplicated
// person. Items without a person (no source_id/name) are skipped.
func (s *Servico) persistirElenco(ctx context.Context, entityID, source string, itens []map[string]any) {
	for _, it := range itens {
		personID, ok := s.upsertPessoa(ctx, source, it)
		if !ok {
			continue
		}
		s.criarCredito(ctx, dadosCredito{
			entityID:     entityID,
			personID:     nsValid(personID),
			kind:         kindCast,
			character:    nsOrNull(comoString(it["character"])),
			ordering:     comoInt(it["order"]),
			episodeCount: comoNullInt(it["episode_count"]),
			source:       source,
			sourceID:     nsOrNull(comoString(it["source_id"])),
		})
	}
}

// persistirEquipe creates the CREW credits (kind='crew'), each one linked to a
// deduplicated person.
func (s *Servico) persistirEquipe(ctx context.Context, entityID, source string, itens []map[string]any) {
	for _, it := range itens {
		personID, ok := s.upsertPessoa(ctx, source, it)
		if !ok {
			continue
		}
		s.criarCredito(ctx, dadosCredito{
			entityID:     entityID,
			personID:     nsValid(personID),
			kind:         kindCrew,
			department:   nsOrNull(comoString(it["department"])),
			job:          nsOrNull(comoString(it["job"])),
			episodeCount: comoNullInt(it["episode_count"]),
			source:       source,
			sourceID:     nsOrNull(comoString(it["source_id"])),
		})
	}
}

// persistirProdutoras creates the STUDIO credits (kind='company'), each one linked to a
// deduplicated company.
func (s *Servico) persistirProdutoras(ctx context.Context, entityID, source string, itens []map[string]any) {
	for _, it := range itens {
		companyID, ok := s.upsertProdutora(ctx, source, it)
		if !ok {
			continue
		}
		s.criarCredito(ctx, dadosCredito{
			entityID:  entityID,
			companyID: nsValid(companyID),
			kind:      kindCompany,
			source:    source,
			sourceID:  nsOrNull(comoString(it["source_id"])),
		})
	}
}

// upsertPessoa deduplicates the person by (source, source_id) and returns the row id
// (existing or new). ok=false when the item has no source_id/name (nothing to link) or the
// upsert fails (degrades, logs).
func (s *Servico) upsertPessoa(ctx context.Context, source string, it map[string]any) (string, bool) {
	sourceID := comoString(it["source_id"])
	nome := comoString(it["name"])
	if sourceID == "" || nome == "" {
		return "", false
	}
	payload, _ := json.Marshal(it)
	id, err := s.escrita().UpsertPerson(ctx, sqlcgen.UpsertPersonParams{
		ID:          s.novoID(),
		Source:      source,
		SourceID:    sourceID,
		Name:        nome,
		ProfilePath: nsOrNull(comoString(it["profile_path"])),
		PayloadJson: nsValid(string(payload)),
	})
	if err != nil {
		log.Printf("enrich: upsert person %s (%s) falhou (segue): %v", sourceID, source, err)
		return "", false
	}
	return id, true
}

// upsertProdutora deduplicates the company by (source, source_id) and returns the row id.
// ok=false when source_id/name is missing or the upsert fails.
func (s *Servico) upsertProdutora(ctx context.Context, source string, it map[string]any) (string, bool) {
	sourceID := comoString(it["source_id"])
	nome := comoString(it["name"])
	if sourceID == "" || nome == "" {
		return "", false
	}
	payload, _ := json.Marshal(it)
	id, err := s.escrita().UpsertCompany(ctx, sqlcgen.UpsertCompanyParams{
		ID:          s.novoID(),
		Source:      source,
		SourceID:    sourceID,
		Name:        nome,
		LogoPath:    nsOrNull(comoString(it["logo_path"])),
		PayloadJson: nsValid(string(payload)),
	})
	if err != nil {
		log.Printf("enrich: upsert company %s (%s) falhou (segue): %v", sourceID, source, err)
		return "", false
	}
	return id, true
}

// criarCredito inserts ONE credit. Best-effort: a failure degrades (logs and continues) --
// one lost credit brings down neither the others nor the enrich.
func (s *Servico) criarCredito(ctx context.Context, d dadosCredito) {
	if err := s.escrita().CriarCredito(ctx, sqlcgen.CriarCreditoParams{
		ID:           s.novoID(),
		EntityID:     d.entityID,
		PersonID:     d.personID,
		CompanyID:    d.companyID,
		Kind:         d.kind,
		Department:   d.department,
		Job:          d.job,
		Character:    d.character,
		Ordering:     d.ordering,
		EpisodeCount: d.episodeCount,
		Source:       d.source,
		SourceID:     d.sourceID,
	}); err != nil {
		log.Printf("enrich: criar credito %s de %s falhou (segue): %v", d.kind, d.entityID, err)
	}
}

// listaDeMapas converts a canonical value ([]any of map[string]any) into the slice of maps.
// It tolerates a missing/other-typed value by returning nil (the consumer iterates over
// nothing).
func listaDeMapas(v any) []map[string]any {
	lista, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(lista))
	for _, it := range lista {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// comoInt returns the value as int64, tolerating int/int64/float64 (`order` comes as int
// from the in-process mapping, but as float64 when the canonical went through JSON). 0 for
// anything else.
func comoInt(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

// comoNullInt returns the value as a valid sql.NullInt64 (int/int64/float64) or NULL
// (missing/other type) -- for the optional episode_count (series only).
func comoNullInt(v any) sql.NullInt64 {
	switch n := v.(type) {
	case int:
		return sql.NullInt64{Int64: int64(n), Valid: true}
	case int64:
		return sql.NullInt64{Int64: n, Valid: true}
	case float64:
		return sql.NullInt64{Int64: int64(n), Valid: true}
	}
	return sql.NullInt64{}
}
