package server

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/webui"
)

// spaHandler serves the static files of the embedded SPA and implements the route
// fallback: when the requested path does not correspond to an existing file
// in the embedded FS, it returns index.html so that the client-side routing
// (deep links) works.
//
// Important: this handler is registered only for NON-/api routes. The routes
// under /api are handled by their own handlers (or by the JSON 404), so that
// an API error is never masked by index.html.
type spaHandler struct {
	fsys      fs.FS
	fileSrv   http.Handler
	indexHTML []byte
}

// newSPAHandler builds the handler from the embedded FS of the webui package.
func newSPAHandler() (*spaHandler, error) {
	index, err := fs.ReadFile(webui.Dist, "index.html")
	if err != nil {
		return nil, err
	}
	return &spaHandler{
		fsys:      webui.Dist,
		fileSrv:   http.FileServer(http.FS(webui.Dist)),
		indexHTML: index,
	}, nil
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Normalizes the path and removes the leading slash to query the fs.FS,
	// whose names do not start with "/".
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

	if name != "" {
		if f, err := h.fsys.Open(name); err == nil {
			info, statErr := f.Stat()
			_ = f.Close()
			// If it is an existing regular file, delegate to the FileServer.
			if statErr == nil && !info.IsDir() {
				h.fileSrv.ServeHTTP(w, r)
				return
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			// Unexpected error when opening (e.g.: invalid path) — treat it as a SPA
			// 404 falling back below.
			_ = err
		}
	}

	// SPA fallback: deliver index.html with 200 and HTML Content-Type.
	h.serveIndex(w)
}

// serveIndex writes the embedded index.html as a 200 text/html response.
func (h *spaHandler) serveIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.indexHTML)
}
