// Package server monta o roteador HTTP (chi) da aplicação: middlewares comuns,
// a API versionada (/api/v1/...) e o servidor de estáticos do SPA embutido com
// fallback de rota.
package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/externaldetail"
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
	// providers é o REGISTRO multi-provider das fontes externas (briefing 0031 /
	// ADR-0002, R2): nome→Provider. Vazio = busca externa desligada: a busca local
	// segue funcionando e external_available:false. Cada fonte (tmdb/tvdb) é
	// escolhida PELO nome do dado (camada externa, detalhe, coleção); a busca
	// consulta TODAS e mescla. Alimentado via WithSearchProvider (variádico/acumula).
	providers provider.Registry
	// cacheExterno guarda por TTL curto o resultado externo por (query,tipo),
	// para poupar a fonte. Só existe quando há provider.
	cacheExterno *cacheExterno
	// assetsDir é o diretório confiável onde o cache de imagem grava os binários
	// (briefing 0015). Injetado via WithAssetsDir; também entra na guarda de path
	// do endpoint de pôster (além dos datasources).
	assetsDir string
	// enrich é o serviço de enriquecimento (import/enrich; briefing 0015). Só
	// existe quando há provider E assetsDir E banco — senão os handlers degradam
	// com 503. Construído em New após aplicar as options.
	enrich *enrich.Servico
	// externalDetail resolve o DETALHE EXTERNO RICO com cache-first (external_detail,
	// briefing 0022). Só existe quando há banco + provider (não precisa de assetsDir);
	// sem ele o handler GET /external degrada com 503. Compartilha a tabela de cache
	// com o enrich (o estado é o banco).
	externalDetail *externaldetail.Servico
	// settings é o cache leve (mutex) da config de idioma de app_settings (briefing
	// 0019), invalidado no PUT /settings/languages. Sempre presente.
	settings *settingsCache
	// hostsImagem é a allowlist do proxy de imagem (GET /images; anti-SSRF). Default
	// = hostsImagemPadrao; injetável em teste. httpImagem baixa a imagem (timeout
	// curto). O proxy só opera com assetsDir configurado.
	hostsImagem map[string]bool
	httpImagem  *http.Client
}

// Option configura opcionalmente o Server na construção, preservando a
// assinatura base New(version, database). Ver WithSearchProvider.
type Option func(*Server)

// WithSearchProvider REGISTRA uma ou mais fontes externas (ex.: TMDB, TheTVDB) no
// registro multi-provider e liga o cache externo. Variádico e ACUMULATIVO: cada
// provider entra sob o seu Nome() (chamadas múltiplas somam); nil é ignorado. Sem
// nenhum provider o registro fica vazio (busca externa desligada). O nome preserva
// a compatibilidade dos chamadores singulares (main e testes passam 1 provider).
func WithSearchProvider(provs ...provider.Provider) Option {
	return func(s *Server) {
		registrou := false
		for _, p := range provs {
			if p == nil {
				continue
			}
			s.providers.Registrar(p)
			registrou = true
		}
		if registrou && s.cacheExterno == nil {
			s.cacheExterno = novoCacheExterno(ttlCacheExterno)
		}
	}
}

// WithAssetsDir injeta o diretório confiável de assets (cache de imagem do
// briefing 0015). O main o resolve (env MYMEDIADB_ASSETS_DIR ou dirname(DB)/assets)
// e cria o diretório antes de injetar.
func WithAssetsDir(dir string) Option {
	return func(s *Server) {
		s.assetsDir = dir
	}
}

// New cria um Server com a versão do binário e os pools de banco (repassados
// pelo main). O db pode ser nil em cenários sem persistência (ex.: alguns
// testes de rota); o health degrada graciosamente nesse caso. Options são
// extensões opcionais (ex.: WithSearchProvider) — a forma New(version, database)
// segue válida.
func New(version string, database *db.DB, opts ...Option) *Server {
	s := &Server{version: version, db: database, settings: &settingsCache{},
		hostsImagem: hostsImagemPadrao, httpImagem: clienteImagemPadrao()}
	if database != nil {
		s.library = library.New(database)
		s.scanner = scanner.NewManager(database)
	}
	for _, opt := range opts {
		opt(s)
	}
	// O enriquecimento (briefing 0015) exige banco + fonte externa + diretório de
	// assets. Sem qualquer um deles fica nil e os handlers /enrich e /import
	// degradam com 503 (busca externa desligada => não há o que importar).
	if database != nil && !s.providers.Vazio() && s.assetsDir != "" {
		s.enrich = enrich.New(database, s.providers, s.assetsDir)
	}
	// O detalhe externo rico (briefing 0022) precisa só de banco + fonte externa
	// (não escreve binários, então dispensa assetsDir). Sem qualquer um deles fica
	// nil e o handler GET /external degrada com 503.
	if database != nil && !s.providers.Vazio() {
		s.externalDetail = externaldetail.New(database, s.providers, 0)
	}
	// Liga o AUTO-IMPORT de séries no scanner (0034): o importador por nome (enrich) e
	// os idiomas permitidos das settings. Só quando o enrich existe; sem ele o scanner
	// de séries degrada (casa o que já está no catálogo, o resto vai para revisão).
	if s.scanner != nil && s.enrich != nil {
		s.scanner.ConfigurarSeries(s.enrich, func(ctx context.Context) []string {
			cfg, err := s.obterLanguageSettings(ctx)
			if err != nil {
				return nil
			}
			return cfg.Allowed
		})
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

			// Import explícito (briefing 0015): find-or-create por id externo,
			// depois enriquece. Escreve external, nunca override.
			v1.Post("/import", s.importar)

			// Criação de coleção LOCAL manual (briefing 0025, Fase B): o dono
			// monta uma coleção do zero (sem fonte externa) e recebe a entity
			// `type='collection'` resolvida. Os membros entram/saem depois pelas
			// rotas /entities/{id}/members abaixo.
			v1.Post("/collections", s.createCollection)

			// Detalhe externo RICO antes de importar (briefing 0022): puxa o item
			// completo da fonte, PERSISTE na 1ª chamada (cache external_detail) e
			// serve do cache nas seguintes, resolvido no idioma ativo. Ver != catalogar
			// (nunca cria entity).
			v1.Get("/external/{source}/{type}/{id}", s.getExternalDetail)

			// Colecao externa como PAGINA propria (briefing 0024, follow-up do
			// 0023): nome/artes do conjunto + as partes (filmes) navegaveis ao
			// detalhe externo. O segmento estatico "collection" tem precedencia
			// sobre o {type} da rota acima no chi. Via a capacidade opcional
			// provider.ColecaoProvider; fontes sem colecao degradam com 503.
			v1.Get("/external/{source}/collection/{id}", s.getExternalCollection)

			// Proxy READ-THROUGH de imagem (briefing 0024): baixa e cacheia em
			// disco a imagem de um host permitido (allowlist anti-SSRF) para a
			// tela de detalhe do EXTERNO (pré-import) não depender do CDN da fonte
			// em runtime — como o pôster/fanart cacheados da entity (0015). 503 sem
			// assetsDir; a resolução vive em image_proxy.go.
			v1.Get("/images", s.getImageProxy)

			// Filas de CURADORIA (briefing 0018): revisão (draft ou baixa
			// confiança) e órfãs (auto-criadas, todas missing, sem investimento
			// do dono). Só leitura; mesmo shape de card da lista. As ações
			// (confirmar/corrigir/purgar) vivem sob /entities.
			v1.Get("/review", s.listReview)
			v1.Get("/orphans", s.listOrphans)

			// Descritor de FORMULÁRIO editorial (briefing 0030): metadado estático
			// que diz ao front COMO renderizar cada campo editável, derivado da fonte
			// única resolver.CamposEditaveis() + o registro de apresentação. Só
			// leitura, sem banco; cacheável (imutável entre deploys).
			v1.Get("/schema/entity-fields", s.getEntityFieldsSchema)

			// Configurações da aplicação (briefing 0019). Hoje só a config de
			// idioma (principal/fallback/permitidos), base da resolução por idioma.
			v1.Route("/settings", func(cfg chi.Router) {
				cfg.Get("/languages", s.getLanguageSettings)
				cfg.Put("/languages", s.putLanguageSettings)
			})

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

				// Criação de ITEM LOCAL manual (briefing 0027): o dono cataloga um
				// filme/série (type=movie|tv) SEM fonte externa e recebe a entity
				// resolvida. Combina com o PUT .../external-ids abaixo (fixar o id da
				// fonte depois, para sincronizar). Coleção manual tem o /collections.
				ent.Post("/", s.createItem)

				ent.Get("/{id}", s.getEntity)
				ent.Get("/{id}/files", s.getEntityFiles)

				// Membros de uma COLEÇÃO (briefing 0025): os filmes ligados por
				// entity_relation(kind='collection'), como cards (mesmo shape da
				// lista), ordenados por ano. `[]` para não-coleção. O POST liga um
				// item e o DELETE o desliga (Fase B, edição manual dos membros):
				// ambos validam que o pai é coleção e devolvem a lista atualizada.
				ent.Get("/{id}/members", s.getEntityMembers)
				ent.Post("/{id}/members", s.addMember)
				ent.Delete("/{id}/members/{childId}", s.removeMember)

				// Episódios da série AGRUPADOS por ordem (briefing 0040): o seletor
				// de ordenação (official/dvd/absolute) reprojeta a série na LEITURA a
				// partir do `numbering` — cabeçalhos de temporada da ordem NÃO viram
				// entidade (ADR-0021), só o episódio é navegável. `?ordering=` ausente
				// ou inválido cai no default da série; `absolute` é ordem especial.
				ent.Get("/{id}/episodes", s.getEntityEpisodes)
				ent.Get("/{id}/assets/poster", s.getEntityPoster)
				ent.Get("/{id}/assets/fanart", s.getEntityFanart)
				ent.Get("/{id}/assets/still", s.getEntityStill)

				// Arte custom do dono (briefing 0017): upload (origin='user'),
				// galeria, servico do binario de um asset, preferencia (flag em
				// meta_json) e remocao. A precedencia de servico (preferida → user →
				// external → 404) e aplicada no /assets/poster acima. Escritas pelo
				// pool de escrita (ADR-0010).
				ent.Post("/{id}/assets", s.uploadAsset)
				ent.Get("/{id}/assets", s.listAssets)
				ent.Get("/{id}/assets/{assetId}/file", s.getAssetFile)
				ent.Put("/{id}/assets/{assetId}/prefer", s.preferAsset)
				ent.Delete("/{id}/assets/{assetId}", s.deleteAsset)

				// Enriquecimento de uma entity existente (briefing 0015): puxa o
				// detalhe do provider e grava a base externa + pôster cacheado.
				ent.Post("/{id}/enrich", s.enrichEntity)

				// Curadoria da Fase 3 (briefing 0018): confirmar sobe a confiança
				// para 100; corrigir grava a identidade correta + re-enriquece;
				// DELETE purga a entity (CASCADE + limpeza de binários); /purge
				// purga em lote. A purga é a única porta destrutiva (ADR-0012).
				ent.Post("/{id}/confirm", s.confirmEntity)
				ent.Post("/{id}/correct", s.correctEntity)
				ent.Delete("/{id}", s.deleteEntity)
				ent.Post("/purge", s.purgeBatch)

				// Edição da IDENTIDADE externa (briefing 0027): grava o mapa
				// external_ids (source -> id na fonte). As colunas geradas re-derivam;
				// fixar o id prepara sincronização futura. NÃO re-enriquece (o dono
				// chama /enrich depois). Escrita pelo pool de escrita (ADR-0010).
				ent.Put("/{id}/external-ids", s.updateExternalIds)

				// Editor de override POR IDIOMA (briefing 0016): o dono escreve o
				// patch esparso que vence tudo. PATCH funde por idioma (?lang=), os
				// DELETE limpam (campo/idioma ou reset), o GET devolve o blob cru.
				// Escritas pelo pool de escrita (ADR-0010).
				ent.Patch("/{id}/override", s.patchOverride)
				ent.Get("/{id}/override", s.getOverride)
				ent.Delete("/{id}/override", s.deleteOverride)
				ent.Delete("/{id}/override/{campo}", s.deleteOverrideCampo)
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
