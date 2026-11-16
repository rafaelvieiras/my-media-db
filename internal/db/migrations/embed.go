// Package migrations packs, via //go:embed, the goose .sql migration files into
// the binary. That way the migrations travel with the executable and are applied
// on boot itself, without an external process (see ADR-0017).
package migrations

import "embed"

// FS holds every .sql file of this directory, numbered and versioned in the
// repo. It is consumed both by goose (through goose.SetBaseFS) and by sqlc
// (which reads the same DDL to generate the typed access layer).
//
//go:embed *.sql
var FS embed.FS
