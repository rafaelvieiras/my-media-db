// Package server monta o roteador HTTP (chi) da aplicação: middlewares comuns,
// a API versionada (/api/v1/...) e o servidor de estáticos do SPA embutido com
// fallback de rota.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rafaelvieiras/my-media-db/internal/db"
)

// Server agrega as dependências necessárias para construir o handler HTTP.
type Server struct {
	version string
	db      *db.DB
}

// New cria um Server com a versão do binário e os pools de banco (repassados
// pelo main). O db pode ser nil em cenários sem persistência (ex.: alguns
// testes de rota); o health degrada graciosamente nesse caso.
func New(version string, database *db.DB) *Server {
	return &Server{version: version, db: database}
}

// Handler constrói e devolve o http.Handler raiz da aplicação.
func (s *Server) Handler() (http.Handler, error) {
	r := chi.NewRouter()

	// Middlewares base: log de requisições e recuperação de panics.
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// API versionada. Rotas sob /api que não casarem devolvem 404 em JSON,
	// nunca o index.html — para não mascarar erros de API como página.
	r.Route("/api", func(api chi.Router) {
		api.NotFound(apiNotFound)
		api.MethodNotAllowed(apiNotFound)

		api.Route("/v1", func(v1 chi.Router) {
			v1.Get("/health", s.healthHandler)
		})
	})

	// SPA: todas as demais rotas servem estáticos embutidos, com fallback para
	// o index.html (deep-links do roteamento client-side).
	spa, err := newSPAHandler()
	if err != nil {
		return nil, err
	}
	r.Handle("/*", spa)

	return r, nil
}

// apiNotFound responde 404 em JSON para qualquer rota /api não resolvida.
func apiNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}
