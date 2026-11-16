// Package webui packs the static files of the SPA (Vite build) inside the Go
// binary via //go:embed. That way the backend serves the frontend on the same
// origin, without depending on external files at runtime.
package webui

import (
	"embed"
	"io/fs"
)

// embedded contains everything in the dist/ directory, including hidden files
// (the "all:" prefix guarantees the inclusion of names starting with "." or "_").
//
//go:embed all:dist
var embedded embed.FS

// Dist exposes the content of dist/ as an fs.FS already pointing inside the
// directory, so that paths like "index.html" and "assets/..." work directly,
// without the "dist/" prefix.
var Dist fs.FS = mustSub(embedded, "dist")

// mustSub extracts a subdirectory from the embedded FS; it fails at startup if the
// expected directory does not exist (build/packaging error).
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("webui: não foi possível abrir o subdiretório embutido '" + dir + "': " + err.Error())
	}
	return sub
}
