// Folder browser over the filesystem that the API process sees (see doc 05):
// used by the datasource creation/edit form to pick a path by clicking instead
// of typing it from memory. It is filesystem-read-only — it does not touch the
// database.
package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxEntradasBrowse is the safety ceiling of entries returned by Browse (it
// keeps the front end from freezing on a pathological folder, e.g. thousands
// of files in a flat folder mounted as root by mistake).
const maxEntradasBrowse = 2000

// BrowseEntry is a subdirectory listed by the folder browser.
type BrowseEntry struct {
	Name     string
	Path     string
	Readable bool
}

// BrowseResult is the response of Browse: the content of a filesystem
// directory, used by the folder browser of the datasource creation/edit form.
type BrowseResult struct {
	Path string
	// Parent is the directory one level above Path; nil when Path is already
	// the filesystem root ("/").
	Parent    *string
	Entries   []BrowseEntry
	Truncated bool
}

// Browse lists the subdirectories of rawPath (or "/" if empty). Only
// directories show up (loose files are omitted); symlinks are followed
// (os.Stat, not os.Lstat) to decide whether they count as a folder, since
// mounts through a symlink are common. Each subdirectory is opened for listing
// just to decide Readable — an entry without permission shows up with
// Readable:false WITHOUT aborting the rest of the listing. Domain errors
// (ErrPathInexistente/ErrNaoDiretorio/ErrPathIlegivel) apply to the rawPath
// itself that was requested, just like in Criar/AtualizarPath.
func (s *Service) Browse(ctx context.Context, rawPath string) (BrowseResult, error) {
	return browse(ctx, rawPath, maxEntradasBrowse)
}

// browse is the non-exported core of Browse, with an injectable cutoff limit —
// it allows the test to exercise the truncation path without having to create
// thousands of real directories.
func browse(_ context.Context, rawPath string, limite int) (BrowseResult, error) {
	if rawPath == "" {
		rawPath = "/"
	}

	abs, err := normalizarPath(rawPath)
	if err != nil {
		return BrowseResult{}, ErrPathInexistente
	}
	if err := validarPath(abs); err != nil {
		return BrowseResult{}, err
	}

	dirEntries, err := os.ReadDir(abs)
	if err != nil {
		// validarPath already confirmed that abs is readable; an error here
		// would be a race condition (removed/permission changed between the
		// two calls). Treated as unreadable, the same domain error family.
		return BrowseResult{}, ErrPathIlegivel
	}

	entries := make([]BrowseEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		full := filepath.Join(abs, de.Name())
		info, statErr := os.Stat(full) // follows symlinks on purpose (mounts through a symlink are common).
		if statErr != nil || !info.IsDir() {
			continue // silently ignored: regular file, broken symlink, etc.
		}
		entries = append(entries, BrowseEntry{
			Name:     de.Name(),
			Path:     full,
			Readable: subdirLegivel(full),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	truncated := false
	if len(entries) > limite {
		entries = entries[:limite]
		truncated = true
	}

	var parent *string
	if dir := filepath.Dir(abs); dir != abs {
		parent = &dir
	}

	return BrowseResult{
		Path:      abs,
		Parent:    parent,
		Entries:   entries,
		Truncated: truncated,
	}, nil
}

// subdirLegivel decides the Readable field of an entry: it tries to open +
// ReadDir(1), the same check as validarPath, but WITHOUT propagating an error —
// it just marks false silently (it never aborts the whole listing because of
// one entry without permission).
func subdirLegivel(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if _, err := f.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	return true
}
