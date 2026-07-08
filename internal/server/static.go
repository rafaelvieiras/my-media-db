package server

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/webui"
)

// spaHandler serve os arquivos estáticos do SPA embutido e implementa o
// fallback de rota: quando o caminho requisitado não corresponde a um arquivo
// existente no FS embutido, devolve o index.html para que o roteamento
// client-side (deep-links) funcione.
//
// Importante: este handler é registrado apenas para rotas NÃO-/api. As rotas
// sob /api são tratadas por handlers próprios (ou pelo 404 JSON), de modo que
// um erro de API nunca seja mascarado pelo index.html.
type spaHandler struct {
	fsys      fs.FS
	fileSrv   http.Handler
	indexHTML []byte
}

// newSPAHandler constrói o handler a partir do FS embutido do pacote webui.
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
	// Normaliza o caminho e remove a barra inicial para consultar o fs.FS,
	// cujos nomes não começam com "/".
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

	if name != "" {
		if f, err := h.fsys.Open(name); err == nil {
			info, statErr := f.Stat()
			_ = f.Close()
			// Se for um arquivo regular existente, delega ao FileServer.
			if statErr == nil && !info.IsDir() {
				h.fileSrv.ServeHTTP(w, r)
				return
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			// Erro inesperado ao abrir (ex.: caminho inválido) — trata como 404
			// do SPA caindo no fallback abaixo.
			_ = err
		}
	}

	// Fallback do SPA: entrega o index.html com 200 e Content-Type HTML.
	h.serveIndex(w)
}

// serveIndex escreve o index.html embutido como resposta 200 text/html.
func (h *spaHandler) serveIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.indexHTML)
}
