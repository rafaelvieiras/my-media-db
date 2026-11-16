// assets_dir.go implements TamanhoDiretorio: the size of the application's
// internal cache (assetsDir) for storage.app_cache_bytes of
// GET /health/system. Unlike UsoDisco (which reuses the SUM already collected
// by the library scanner), assetsDir is a directory of the application ALONE
// -- small compared to the user's library -- so a simple and direct
// filepath.WalkDir is enough, with no need for incremental optimization or for
// reusing any existing scan.
package telemetry

import (
	"io/fs"
	"os"
	"path/filepath"
)

// TamanhoDiretorio adds up the size of ALL regular files under `dir`
// (recursive). A missing `dir` returns (0, nil) -- assetsDir may not have been
// created yet (fresh installation, no asset downloaded yet). Read errors of an
// individual file/subdirectory (e.g.: removed between the WalkDir listing and
// the Info) are ignored (best-effort, the same best-effort spirit as the rest
// of this package) -- only an error on the ROOT itself (that is not "does not
// exist") aborts with an error.
func TamanhoDiretorio(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				if os.IsNotExist(err) {
					return filepath.SkipAll
				}
				return err
			}
			return nil // unreadable subitem: ignore it, do not abort the sum.
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
