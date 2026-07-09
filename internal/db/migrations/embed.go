// Package migrations empacota, via //go:embed, os arquivos .sql de migration do
// goose dentro do binário. Assim as migrations viajam junto do executável e são
// aplicadas no próprio boot, sem processo externo (ver ADR-0017).
package migrations

import "embed"

// FS contém todos os arquivos .sql deste diretório, numerados e versionados no
// repo. É consumido tanto pelo goose (via goose.SetBaseFS) quanto pelo sqlc
// (que lê o mesmo DDL para gerar o acesso tipado).
//
//go:embed *.sql
var FS embed.FS
