package integration

import (
	"context"
	"encoding/json"
	"os"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Resolver computes the EFFECTIVE value of a Campo (ADR-0023): database (table
// integration_config) → the Campo's bootstrap/fallback env var → absent. It
// follows the SAME pattern as internal/curation.Servico: it holds the *db.DB and
// uses the READ pool (ADR-0010) — the Resolver never writes; the HTTP PUT writes
// directly via sqlcgen (internal/server/integrations.go), the same pattern as
// the rest of internal/server/settings.go.
//
// Efetivo/Valores do not return an error on purpose (the same signature the
// decision prescribes): a real read failure in the database (not merely "slug
// never written") degrades silently to the env var, instead of propagating — it
// reflects the principle of never blocking the whole integration because of an
// optional read (AGENTS.md §6). The zero-value (*Resolver)(nil) is also safe:
// Efetivo degrades straight to the env var when there is no database (e.g.:
// route tests with no injected database).
type Resolver struct {
	database *db.DB
}

// NovoResolver creates the Resolver from the database pool.
func NovoResolver(database *db.DB) *Resolver {
	return &Resolver{database: database}
}

func (r *Resolver) leitura() *sqlcgen.Queries { return sqlcgen.New(r.database.Read) }

// Efetivo computes the effective value of ONE field of ONE integration: it reads
// integration_config by slug and checks the campo.Nome key in values_json; if
// absent (missing row, corrupted JSON or key not present), it falls back to
// os.Getenv(campo.EnvVar) when EnvVar != ""; if neither has it, ok=false and
// origem="nenhum".
func (r *Resolver) Efetivo(ctx context.Context, slug string, campo Campo) (valor, origem string, ok bool) {
	if r != nil && r.database != nil {
		if v, achou := r.doBanco(ctx, slug, campo.Nome); achou {
			return v, "banco", true
		}
	}
	if campo.EnvVar != "" {
		if v := os.Getenv(campo.EnvVar); v != "" {
			return v, "env", true
		}
	}
	return "", "nenhum", false
}

// doBanco reads integration_config and returns the stored value of the `nome`
// field, if present. Any read error (missing row, corrupted JSON) is treated as
// "not found in the database" — the caller falls back to the env var and never
// propagates an error (see the Resolver comment).
func (r *Resolver) doBanco(ctx context.Context, slug, nome string) (string, bool) {
	row, err := r.leitura().ObterIntegrationConfig(ctx, slug)
	if err != nil {
		return "", false
	}
	if row.ValuesJson == "" {
		return "", false
	}
	var valores map[string]string
	if err := json.Unmarshal([]byte(row.ValuesJson), &valores); err != nil {
		return "", false
	}
	v, existe := valores[nome]
	if !existe {
		return "", false
	}
	return v, true
}

// Valores aggregates Efetivo for ALL fields of a Descriptor at once — the format
// consumed by Descriptor.Configurada. Fields with no effective value (ok=false)
// stay out of the map.
func (r *Resolver) Valores(ctx context.Context, slug string, campos []Campo) map[string]string {
	out := make(map[string]string, len(campos))
	for _, c := range campos {
		if v, _, ok := r.Efetivo(ctx, slug, c); ok {
			out[c.Nome] = v
		}
	}
	return out
}
