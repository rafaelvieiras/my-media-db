// Package integration defines the FOUNDATION of integration config (ADR-0023):
// a generic, database-writable registry of provider credentials and parameters
// — replacing the hardcode of an environment variable read a single time at
// boot. It mirrors the spirit of internal/provider (N-source Registry, usable
// zero-value), but for CONFIG instead of BEHAVIOR: a Descriptor describes the
// field SCHEMA of an integration (e.g.: TMDB → token/api_key); whoever computes
// the EFFECTIVE VALUE of each field (database → env var → absent) is the
// Resolver (resolver.go).
//
// This package knows nothing about HTTP, nor about sqlc directly in its central
// type (Descriptor/Campo are pure data); only the Resolver touches the database.
package integration

import "context"

// TipoCampo classifies how the UI (ADR-0023) must render a Campo — purely a
// presentation hint, it does not affect value resolution.
type TipoCampo string

const (
	// TipoTexto is a free-text, non-sensitive field.
	TipoTexto TipoCampo = "text"
	// TipoSegredo is a sensitive field (credential/key): the API NEVER returns
	// the raw value, only `definido`/`origem` (see GET/PUT in internal/server/
	// integrations.go).
	TipoSegredo TipoCampo = "secret"
	// TipoBool is an on/off toggle.
	TipoBool TipoCampo = "bool"
	// TipoNumero is a numeric field.
	TipoNumero TipoCampo = "number"
	// TipoSelect is a choice among fixed Opcoes.
	TipoSelect TipoCampo = "select"
)

// Opcao is one alternative of a TipoSelect Campo.
type Opcao struct {
	Valor string
	Label string
}

// Campo describes ONE configurable field of an integration — a static schema,
// not the value. EnvVar is this specific field's bootstrap/fallback env var
// ("" = none, the field can only come from the database).
type Campo struct {
	// Nome is the stable key of the field (used as the key in values_json and in
	// the PUT URL — e.g.: "token", "api_key").
	Nome string
	// Label is the friendly label for the UI.
	Label string
	// Tipo classifies the rendering (see TipoCampo).
	Tipo TipoCampo
	// Obrigatorio is a UI HINT (asterisk); it does NOT decide "configured" —
	// each Descriptor decides its own rule in Configurada (see the comment
	// there).
	Obrigatorio bool
	// Ajuda is an optional supporting text (e.g.: where to obtain the key).
	Ajuda string
	// Placeholder is the optional example text of the field.
	Placeholder string
	// EnvVar is this field's bootstrap/fallback env var. "" = none (the field
	// can only come from the database, with no environment fallback).
	EnvVar string
	// Opcoes is only used when Tipo == TipoSelect.
	Opcoes []Opcao
}

// Descriptor is the SCHEMA of a configurable integration — implemented by each
// consumer (e.g.: internal/provider/tmdb.NovoDescriptorIntegracao()). It
// persists nothing and resolves no value — it only describes. The Registry
// (registry.go) groups Descriptors; the Resolver (resolver.go) computes
// effective values.
type Descriptor interface {
	// Slug identifies the integration in a stable way — it is the URL key
	// (/api/v1/integrations/{slug}) and the registration key in
	// integration_config. Chosen to last (e.g.: "tmdb", never
	// "TMDB"/"tmdb-provider").
	Slug() string
	// Nome is the friendly label for the UI (e.g.: "TMDB").
	Nome() string
	// Descricao is a short text explaining the integration.
	Descricao() string
	// Campos lists the field schema of this integration.
	Campos() []Campo
	// Configurada decides, from the EFFECTIVE values (database-or-env per field,
	// already resolved by Resolver.Valores), whether the integration is
	// operational. Each Descriptor decides its own rule (e.g.: TMDB accepts
	// token OR api_key) — on purpose it is not the framework that decides, so as
	// not to invent a DSL of conditional obligation.
	Configurada(efetivos map[string]string) bool
}

// Testador is an OPTIONAL capability (type-assert, same pattern as the
// internal/provider capabilities) that allows testing the connection with the
// given values, without persisting anything. NOT implemented in this delivery —
// only the interface, so as not to break the type-assert of a follow-up when
// some Descriptor implements it (endpoint POST /integrations/{slug}/test,
// outside the scope of ADR-0023).
type Testador interface {
	Testar(ctx context.Context, valores map[string]string) error
}
