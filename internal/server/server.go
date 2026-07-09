// Package server monta o roteador HTTP (chi) da aplicação: middlewares comuns,
// a API versionada (/api/v1/...) e o servidor de estáticos do SPA embutido com
// fallback de rota.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// Server agrega as dependências necessárias para construir o handler HTTP.
type Server struct {
	version string
	db      *db.DB
	// library é o serviço de datasources (/api/v1/library). Nil quando não há
	// banco injetado (ex.: alguns testes de rota); os handlers degradam com 503.
	library *library.Service
}

// New cria um Server com a versão do binário e os pools de banco (repassados
// pelo main). O db pode ser nil em cenários sem persistência (ex.: alguns
// testes de rota); o health degrada graciosamente nesse caso.
func New(version string, database *db.DB) *Server {
	s := &Server{version: version, db: database}
	if database != nil {
		s.library = library.New(database)
	}
	return s
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

			// Datasources da biblioteca (briefing 0008). Escritas (POST/DELETE)
			// pela conexão de escrita, leituras (GET) pelo pool — encapsulado no
			// serviço internal/library (ADR-0010).
			v1.Route("/library", func(lib chi.Router) {
				lib.Post("/", s.createLibrary)
				lib.Get("/", s.listLibrary)
				lib.Get("/{id}", s.getLibrary)
				lib.Delete("/{id}", s.deleteLibrary)
			})
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

// apiNotFound responde 404 em JSON para qualquer rota /api não resolvida, já no
// shape de erro padrão ({error:{code,message}}) definido no briefing 0008 — o
// mesmo de todos os erros /api/v1. (Antes respondia {"error":"not found"},
// shape antigo/inconsistente, agora aposentado.)
func apiNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "recurso não encontrado")
}
