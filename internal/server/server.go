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
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// Server agrega as dependências necessárias para construir o handler HTTP.
type Server struct {
	version string
	db      *db.DB
	// library é o serviço de datasources (/api/v1/library). Nil quando não há
	// banco injetado (ex.: alguns testes de rota); os handlers degradam com 503.
	library *library.Service
	// scanner é o gerenciador de jobs de scan (/api/v1/library/{id}/scan). Nil
	// quando não há banco injetado.
	scanner *scanner.Manager
	// searchProvider é a fonte externa opcional da busca (/api/v1/search;
	// briefing 0014). Nil = busca externa desligada: a busca local segue
	// funcionando e external_available:false. Injetado via WithSearchProvider.
	searchProvider provider.Provider
	// cacheExterno guarda por TTL curto o resultado externo por (query,tipo),
	// para poupar a fonte. Só existe quando há provider.
	cacheExterno *cacheExterno
}

// Option configura opcionalmente o Server na construção, preservando a
// assinatura base New(version, database). Ver WithSearchProvider.
type Option func(*Server)

// WithSearchProvider injeta a fonte externa da busca (ex.: TMDB) e liga o cache
// externo. Chamado pelo main só quando há credencial; nil é ignorado.
func WithSearchProvider(p provider.Provider) Option {
	return func(s *Server) {
		if p == nil {
			return
		}
		s.searchProvider = p
		s.cacheExterno = novoCacheExterno(ttlCacheExterno)
	}
}

// New cria um Server com a versão do binário e os pools de banco (repassados
// pelo main). O db pode ser nil em cenários sem persistência (ex.: alguns
// testes de rota); o health degrada graciosamente nesse caso. Options são
// extensões opcionais (ex.: WithSearchProvider) — a forma New(version, database)
// segue válida.
func New(version string, database *db.DB, opts ...Option) *Server {
	s := &Server{version: version, db: database}
	if database != nil {
		s.library = library.New(database)
		s.scanner = scanner.NewManager(database)
	}
	for _, opt := range opts {
		opt(s)
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

			// Busca local-first (briefing 0014): FTS5 no catálogo + passthrough
			// opcional à fonte externa (resultados "não salvos").
			v1.Get("/search", s.search)

			// Datasources da biblioteca (briefing 0008). Escritas (POST/DELETE)
			// pela conexão de escrita, leituras (GET) pelo pool — encapsulado no
			// serviço internal/library (ADR-0010).
			v1.Route("/library", func(lib chi.Router) {
				lib.Post("/", s.createLibrary)
				lib.Get("/", s.listLibrary)
				lib.Get("/{id}", s.getLibrary)
				lib.Delete("/{id}", s.deleteLibrary)

				// Scan em background por datasource (briefing 0009): dispara
				// (202) e consulta o progresso.
				lib.Post("/{id}/scan", s.startScan)
				lib.Get("/{id}/scan", s.getScanStatus)
			})

			// Superfície de LEITURA do catálogo (briefing 0010). Só GETs, todos
			// pelo pool de leitura (ADR-0010): lista paginada, detalhe resolvido,
			// arquivos+mediainfo e o binário do pôster.
			v1.Route("/entities", func(ent chi.Router) {
				ent.Get("/", s.listEntities)
				ent.Get("/{id}", s.getEntity)
				ent.Get("/{id}/files", s.getEntityFiles)
				ent.Get("/{id}/assets/poster", s.getEntityPoster)
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
