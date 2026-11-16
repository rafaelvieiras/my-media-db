// Package curation exposes, as a reusable service, the COUNTS of the curation
// queues (review and orphans) to consumers that are not HTTP -- today only the
// library health job. The queries themselves (ListarRevisao/ListarOrfas,
// internal/db/queries/review.sql) already exist and keep serving the GET
// /review and GET /orphans endpoints of internal/server; this package does NOT
// duplicate the predicate's SQL logic -- it only wraps the SAME sqlc query to
// return the COUNT (len(rows)) instead of the full card shape, which the job
// does not need.
package curation

import (
	"context"
	"database/sql"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// LimiarBaixaConfianca is the confidence cutoff that separates "high guess"
// from "low confidence" (the scanner's 0-100 scale) -- the SAME value as the
// private limiarBaixaConfianca of internal/server/review.go (which feeds the
// GET /review endpoint) and as the front-end LIMIAR_BAIXA_CONFIANCA
// (web/src/lib/entity-badges.ts). The three MUST match; it is a coupling that
// is already documented (a candidate to become a config/endpoint in the
// future), not a duplication of LOGIC -- it is only a mirrored number, the
// query itself (ListarRevisao) is reused in full.
const LimiarBaixaConfianca = 70

// Servico concentrates the reads of the curation queues by COUNT (without the
// full card shape) -- consumed by the library health job
// (internal/job/libraryhealth).
type Servico struct {
	database *db.DB
}

// New creates the Servico from the database pool.
func New(database *db.DB) *Servico {
	return &Servico{database: database}
}

func (s *Servico) leitura() *sqlcgen.Queries { return sqlcgen.New(s.database.Read) }

// ContarRevisao returns how many items are in the review queue (draft OR low
// confidence) -- the SAME sqlc query as GET /review (ListarRevisao), only
// counting the rows instead of returning the full card shape.
func (s *Servico) ContarRevisao(ctx context.Context) (int, error) {
	rows, err := s.leitura().ListarRevisao(ctx, sql.NullFloat64{Float64: LimiarBaixaConfianca, Valid: true})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// ContarOrfas returns how many entities are in the orphans queue -- the SAME
// sqlc query as GET /orphans (ListarOrfas), only counting the rows.
func (s *Servico) ContarOrfas(ctx context.Context) (int, error) {
	rows, err := s.leitura().ListarOrfas(ctx)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}
