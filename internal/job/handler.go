// Package job is the foundation of the job system: job types registered in Go
// code (Handler), dynamic configuration and generic history persisted in the
// database (job_definition/job_execution), and a built-in scheduler (goroutine
// + ticker, without depending on the OS cron). It is the explicit basis for the
// future plugin system: a new job type only has to register its Handler
// (Registry) and seed a job_definition row — without touching the schema or the
// endpoints (ADR-0005: a core that is agnostic to the config_json format of
// each type).
package job

import (
	"context"
	"encoding/json"
)

// Handler is the contract that ANY job type implements — the seed job
// backup_vacuum (internal/job/backup), the future library health job, an
// eventual third-party plugin, and so on. Nome() is the STABLE slug that
// matches job_definition.id (data curated via seed/migration, never generated
// at runtime — just as Handler.Nome() of provider.Provider matches the name of
// the source).
type Handler interface {
	// Nome returns the stable slug of the job type (e.g. "backup_vacuum").
	Nome() string

	// Executar does the actual work. cfg is the RAW config_json of the
	// definition (it may be empty/nil when the config is absent) — each
	// Handler interprets its own format, namespaced, without the core knowing
	// it. On success, mensagem becomes job_execution.message (a short text,
	// e.g. "backup gravado em ... (7 mantidos)"); on error, err becomes the
	// failure message (no stack trace).
	Executar(ctx context.Context, cfg json.RawMessage) (mensagem string, err error)
}
