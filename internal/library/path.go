package library

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// normalizarPath turns a path informed by the user into its absolute and clean
// form (filepath.Abs already applies filepath.Clean). Always storing the
// normalized form is what makes the UNIQUE constraint of library_source match:
// two paths like "/a/b" and "/a/b/../b" become the SAME string and the
// duplicate does not slip through disguised.
func normalizarPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path vazio")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// validarPath validates that the ALREADY normalized path exists, is a
// directory and is readable. It returns the corresponding domain error (mapped
// to 422 in the handler). The readability check tries to open the directory and
// read one entry: a directory with mode 0000 already fails at os.Open (it needs
// read permission to list), which covers the "no permission" case.
func validarPath(abs string) error {
	info, err := os.Stat(abs)
	if err != nil {
		// Does not exist (or an error accessing the stat itself): treated as
		// an invalid/nonexistent path.
		if os.IsPermission(err) {
			return ErrPathIlegivel
		}
		return ErrPathInexistente
	}
	if !info.IsDir() {
		return ErrNaoDiretorio
	}
	f, err := os.Open(abs)
	if err != nil {
		return ErrPathIlegivel
	}
	defer func() { _ = f.Close() }()
	// ReadDir(1) returns io.EOF for a readable but empty directory; that is
	// NOT a failure. Any other error (e.g.: permission denied) is.
	if _, err := f.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
		return ErrPathIlegivel
	}
	return nil
}

// avaliarState derives the online/offline state of a source through a cheap
// stat of the path, WITHOUT persisting it (API decision, §6: persisting
// offline belongs to the scanner). online = exists + is a directory + is
// readable; any failure => offline.
func avaliarState(abs string) string {
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return stateOffline
	}
	f, err := os.Open(abs)
	if err != nil {
		return stateOffline
	}
	_ = f.Close()
	return stateOnline
}
