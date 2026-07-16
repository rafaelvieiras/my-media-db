// Package server monta o roteador HTTP (chi) da aplicação: middlewares comuns,
// a API versionada (/api/v1/...) e o servidor de estáticos do SPA embutido com
// fallback de rota.
package server

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rafaelvieiras/my-media-db/internal/curation"
	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/externaldetail"
	"github.com/rafaelvieiras/my-media-db/internal/job"
	"github.com/rafaelvieiras/my-media-db/internal/job/libraryhealth"
	"github.com/rafaelvieiras/my-media-db/internal/job/midiasfaltantes"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
	"github.com/rafaelvieiras/my-media-db/internal/telemetry"
	"github.com/rafaelvieiras/my-media-db/internal/watch"
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
	// catalogCompleteness é o cache leve (mutex) da config de completude de
	// CATALOGAÇÃO de app_settings (briefing 0057), invalidado no PUT
	// /settings/catalog-completeness. Sempre presente. Não confundir com
	// completude de POSSE (web/src/lib/entity-badges.ts).
	catalogCompleteness *catalogCompletenessCache
	// hostsImagem é a allowlist do proxy de imagem (GET /images; anti-SSRF). Default
	// = hostsImagemPadrao; injetável em teste. httpImagem baixa a imagem (timeout
	// curto). O proxy só opera com assetsDir configurado.
	hostsImagem map[string]bool
	httpImagem  *http.Client
	// jobs é o gerenciador do sistema de jobs (briefing 0052): registro dinâmico
	// de tipos de job (Handler), config/histórico persistidos e o agendador
	// embutido. Construído sempre que há banco (não depende de provider/
	// assetsDir, diferente de enrich/externalDetail); os handlers concretos
	// (ex.: backup_vacuum) entram via WithJobHandlers.
	jobs *job.Manager
	// telemetry calcula o snapshot de saúde operacional (briefing 0055):
	// tamanho do banco + retrato de uso + uptime/memória do processo. Só
	// existe quando há banco (mesmo padrão de s.jobs); sem ele
	// GET /health/system degrada com 503 — GET /health (básico) continua
	// funcionando independente (contrato do 0001/0006 preservado, ver
	// health.go).
	telemetry *telemetry.Servico
	// telemetryCache guarda por TTL curto (ttlTelemetria) o snapshot JÁ
	// SERIALIZADO de GET /health/system, para não recalcular a cada
	// reload/foco de janela da tela de saúde. Cache SINGLETON (uma única
	// entrada — este endpoint não tem parâmetro variável, diferente de
	// cacheExterno). Sempre presente (mesmo sem banco; só nunca é populado
	// nesse caso, já que o handler barra antes com s.telemetry == nil).
	telemetryCache *telemetryCache
	// libraryHealth é o MESMO Handler do job saude_biblioteca registrado por
	// WithLibraryHealthJob (briefing 0053) — guardado à parte (além de já
	// estar no Registry de s.jobs) para health_system.go ler
	// UltimoResultadoStorage (briefing 0063: managed/unmanaged de arquivo
	// NÃO-vídeo da última execução do job, sem recalcular por fora). Nil
	// quando WithLibraryHealthJob não foi aplicada (ou sem banco/library/
	// scanner) — os campos novos de storage degradam para 0 nesse caso, mesmo
	// espírito defensivo do resto do Server.
	libraryHealth *libraryhealth.Handler
	// watch orquestra o watcher fsnotify opcional por library_source
	// (briefing 0064, internal/watch): registrado via WithWatch, acionado no
	// boot (IniciarWatchers) e a QUENTE pelo handler PATCH /library/{id}
	// (aplicarWatchToggle, library.go). Nil quando WithWatch não foi
	// aplicada (ou sem banco/library/scanner) -- o toggle de watch_enabled
	// ainda persiste no banco nesse caso, só o efeito em memória não se
	// aplica (mesma guarda defensiva de libraryHealth).
	watch *watch.Watcher
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

// WithJobHandlers REGISTRA um ou mais tipos de job (briefing 0052) no Registry
// do Manager de jobs. Variádico e ACUMULATIVO (mesmo padrão de
// WithSearchProvider): cada handler entra sob o seu Nome() (chamadas múltiplas
// somam); nil é ignorado. O Manager em si é construído em montarServicos
// (sempre que há banco); esta option só precisa que ele já exista quando
// aplicada — New aplica as options depois de montar os serviços obrigatórios,
// então o Registry pode ficar vazio até lá (Registrar no zero-value funciona).
func WithJobHandlers(handlers ...job.Handler) Option {
	return func(s *Server) {
		if s.jobs == nil {
			s.jobs = job.NewManager(s.db)
		}
		for _, h := range handlers {
			s.jobs.Registry.Registrar(h)
		}
	}
}

// WithLibraryHealthJob REGISTRA o job `saude_biblioteca` (briefing 0053,
// follow-up do 0052) no Registry do Manager de jobs. Diferente de
// WithJobHandlers (que recebe um Handler PRONTO, construído em main.go só
// com dependências de main — ex.: backup.New(db, dir)), o Handler deste job
// PRECISA do scanner.Manager e do library.Service, que são internos ao
// Server (privados, construídos em New ANTES do loop de options — mesmo
// pré-requisito que WithJobHandlers já assume para s.jobs existir). Por
// isso a construção do Handler acontece AQUI DENTRO, no momento em que a
// option roda (com s.library/s.scanner já montados), em vez de expor esses
// serviços para main.go reconstruir por fora (o que duplicaria a fiação).
// main.go só precisa passar server.WithLibraryHealthJob() junto das demais
// options. Nula (sem registrar nada) se não houver banco — mesma guarda de
// WithJobHandlers/montarServicos para os demais serviços opcionais.
func WithLibraryHealthJob() Option {
	return func(s *Server) {
		if s.db == nil || s.library == nil || s.scanner == nil {
			return
		}
		if s.jobs == nil {
			s.jobs = job.NewManager(s.db)
		}
		// s satisfaz tanto libraryhealth.Matcher (auto-match/enriquecimento
		// retroativo, 0054/0056) quanto libraryhealth.CompletenessRecalculator
		// (recalculo do cache de completude de catalogacao, briefing 0058) --
		// os dois parametros recebem o MESMO *Server.
		h := libraryhealth.New(s.library, s.scanner, curation.New(s.db), s, s)
		s.libraryHealth = h
		s.jobs.Registry.Registrar(h)
	}
}

// WithMidiasFaltantesJob REGISTRA o job `midias_faltantes` (briefing 0064,
// fecha o ciclo do 0062 em escala) no Registry do Manager de jobs. Mesmo
// motivo de WithLibraryHealthJob ser uma option DEDICADA (não
// WithJobHandlers): o Handler precisa do próprio *Server como
// EntityLister/Materializador (internal/job/midiasfaltantes), que só existe
// aqui dentro. Nula (sem registrar nada) se não houver banco -- mesma guarda
// dos demais.
func WithMidiasFaltantesJob() Option {
	return func(s *Server) {
		if s.db == nil {
			return
		}
		if s.jobs == nil {
			s.jobs = job.NewManager(s.db)
		}
		// s satisfaz tanto midiasfaltantes.EntityLister quanto
		// midiasfaltantes.Materializador (ver internal/server/midias_faltantes.go).
		h := midiasfaltantes.New(s, s)
		s.jobs.Registry.Registrar(h)
	}
}

// WithWatch REGISTRA o orquestrador do watch fsnotify opcional por fonte
// (briefing 0064, internal/watch). Precisa do scanner.Manager (interno ao
// Server, já construído antes do loop de options) para disparar o scan
// DIRECIONADO de uma fonte quando o debounce expira. Nula (s.watch continua
// nil) se não houver banco/library/scanner -- mesma guarda de
// WithLibraryHealthJob; o chamador (main.go) ainda deve chamar
// IniciarWatchers APÓS srv := server.New(...) para registrar as fontes que
// já nascem com watch_enabled=1 (não é feito aqui dentro para não fazer I/O
// de disco durante a construção do Server).
func WithWatch() Option {
	return func(s *Server) {
		if s.db == nil || s.library == nil || s.scanner == nil {
			return
		}
		s.watch = watch.New(s.scanner, watch.DebounceDefault)
	}
}

// IniciarWatchers lista TODA library_source com watch_enabled=1 e registra o
// watcher fsnotify de cada uma (mesma rotina usada pelo handler PATCH
// /library/{id} quando liga o toggle a quente) -- chamado pelo main ANTES do
// ListenAndServe, mesmo espírito de RunScheduler não atrasar o boot do HTTP.
// Falha ao registrar UMA fonte é logada e NUNCA aborta o boot nem as demais
// (briefing 0064 §4). No-op silencioso se WithWatch não foi aplicada ou não
// há library.Service (sem banco).
func (s *Server) IniciarWatchers(ctx context.Context) {
	if s.watch == nil || s.library == nil {
		return
	}
	sources, err := s.library.Listar(ctx)
	if err != nil {
		log.Printf("watch: falha ao listar fontes no boot (nenhum watcher registrado): %v", err)
		return
	}
	for _, src := range sources {
		if !src.WatchEnabled {
			continue
		}
		if err := s.watch.RegistrarFonte(src); err != nil {
			log.Printf("watch: falha ao registrar a fonte %q no boot (best-effort, fonte segue dependendo do polling periodico): %v", src.ID, err)
		}
	}
}

// FecharWatchers fecha todos os watchers fsnotify registrados -- chamado pelo
// main no shutdown gracioso do servidor (mesmo instante de httpServer.Shutdown).
// No-op se WithWatch não foi aplicada.
func (s *Server) FecharWatchers() {
	if s.watch == nil {
		return
	}
	s.watch.Close()
}

// Jobs devolve o gerenciador de jobs (nil quando não há banco injetado) —
// usado pelo main para disparar o agendador embutido (RunScheduler) antes do
// ListenAndServe.
func (s *Server) Jobs() *job.Manager {
	return s.jobs
}

// New cria um Server com a versão do binário e os pools de banco (repassados
// pelo main). O db pode ser nil em cenários sem persistência (ex.: alguns
// testes de rota); o health degrada graciosamente nesse caso. Options são
// extensões opcionais (ex.: WithSearchProvider) — a forma New(version, database)
// segue válida.
func New(version string, database *db.DB, opts ...Option) *Server {
	s := &Server{version: version, db: database, settings: &settingsCache{},
		catalogCompleteness: &catalogCompletenessCache{},
		hostsImagem:         hostsImagemPadrao, httpImagem: clienteImagemPadrao(),
		telemetryCache: novoTelemetryCache(ttlTelemetria)}
	if database != nil {
		s.library = library.New(database)
		s.scanner = scanner.NewManager(database)
	}
	for _, opt := range opts {
		opt(s)
	}
	s.montarServicos(database)
	return s
}

// montarServicos constrói os serviços OPCIONAIS (enrich, detalhe externo rico e o
// auto-import de séries do scanner) depois de aplicadas as options. Cada um exige
// seu próprio conjunto de dependências (banco/provider/assetsDir); sem elas fica
// nil e o handler correspondente degrada com 503. Extraído de New para manter a
// construção legível (uma responsabilidade por método).
func (s *Server) montarServicos(database *db.DB) {
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
		s.scanner.ConfigurarSeries(s.enrich, s.idiomasScanner)
	}
	// O Manager de jobs (briefing 0052) só precisa de banco -- diferente de
	// enrich/externalDetail, não depende de provider nem assetsDir (o job
	// semente backup_vacuum não usa fonte externa). WithJobHandlers pode já
	// tê-lo construído (para registrar os handlers antes deste ponto); aqui só
	// cobre o caso de nenhuma option ter sido aplicada (a listagem GET /jobs
	// ainda precisa funcionar, mesmo sem nenhum Handler registrado).
	if database != nil && s.jobs == nil {
		s.jobs = job.NewManager(database)
	}
	// A telemetria de sistema (briefing 0055) só precisa de banco — não
	// depende de provider/assetsDir (diferente de enrich/externalDetail).
	// iniciadoEm=time.Now() aqui aproxima o "início do processo" (o uptime
	// reportado): o Server é construído uma única vez no boot, então este é
	// o melhor instante disponível sem plumbing adicional em main.go.
	if database != nil && s.telemetry == nil {
		s.telemetry = telemetry.New(database, curation.New(database), time.Now())
	}
}

// idiomasScanner devolve os idiomas permitidos das settings (nil em erro), usado como
// fonte de idiomas do auto-import de séries do scanner.
func (s *Server) idiomasScanner(ctx context.Context) []string {
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return nil
	}
	return cfg.Allowed
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

			// Telemetria de sistema (briefing 0055): tamanho do banco + retrato
			// operacional + uso de disco por fonte, em SNAPSHOT sob demanda
			// (cache de 60s). Separado de /health de propósito — este é mais caro
			// (N syscalls Statfs + agregações SQL) e não pode pesar o HEALTHCHECK
			// do Docker (que bate em /health a cada 10s).
			v1.Get("/health/system", s.systemHealthHandler)

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

			// AUTO-MATCH em LOTE (briefing 0050): tenta resolver sozinho todos os
			// elegíveis da fila (ou o subconjunto de `ids` no corpo), reusando a
			// MESMA lógica do individual em /entities/{id}/auto-match abaixo.
			// Sempre 200 com o resumo agregado.
			v1.Post("/review/auto-match", s.autoMatchReview)

			// Página da PESSOA (briefing 0029 Fase D, interno-first): a pessoa
			// deduplicada no catálogo + a filmografia (obras já importadas que a
			// creditam). Destino do card de elenco/equipe quando o crédito traz
			// `person_id`. Read-only; 404 quando a pessoa não existe no catálogo.
			v1.Get("/person/{id}", s.getPerson)

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

				// Completude de CATALOGAÇÃO por tipo (briefing 0057): quais campos/
				// artes o dono considera desejáveis para um item estar bem
				// catalogado. Diferente de completude de POSSE (front).
				cfg.Get("/catalog-completeness", s.getCatalogCompletenessSettings)
				cfg.Put("/catalog-completeness", s.putCatalogCompletenessSettings)
			})

			// Datasources da biblioteca (briefing 0008). Escritas (POST/PATCH/
			// DELETE) pela conexão de escrita, leituras (GET) pelo pool —
			// encapsulado no serviço internal/library (ADR-0010). PATCH
			// (briefing 0064) edita watch_enabled e aplica o watcher fsnotify
			// A QUENTE (aplicarWatchToggle, library.go), sem restart.
			v1.Route("/library", func(lib chi.Router) {
				lib.Post("/", s.createLibrary)
				lib.Get("/", s.listLibrary)
				lib.Get("/{id}", s.getLibrary)
				lib.Patch("/{id}", s.patchLibrary)
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

				// Arte DISPONIVEL agrupada por kind (briefing 0048): mescla os assets
				// locais + o poster/fundo das camadas externas (TMDB) + o inventario lazy
				// da fanart.tv, com URLs ja proxyadas e `available:{kind:count}` para os
				// selos. So leitura; consumida pela galeria/selos do 0049.
				ent.Get("/{id}/artwork", s.getEntityArtwork)

				// AÇÃO "baixar só a selecionada" (briefing 0048): recebe a url de UMA
				// arte do inventario (host validado contra a allowlist do proxy —
				// anti-SSRF), baixa o binario e a materializa como asset origin='external'
				// do kind (idempotente por url+kind), opcionalmente marcando-a preferida.
				ent.Post("/{id}/artwork/select", s.selectArtwork)

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

				// Materialização física na pasta REAL da biblioteca (briefing 0062):
				// copia uma arte já cacheada internamente para o disco da biblioteca,
				// no nome canônico Kodi/Emby (internal/localmedia). NÃO mexe em
				// is_principal (isso é o PUT /prefer acima, estendido no mesmo
				// briefing). "Salvar informações (NFO)" (grupo de ações rápidas do
				// 0059) grava/regrava o .nfo da entity na pasta real — export
				// one-way (banco → disco), idempotente.
				ent.Post("/{id}/assets/{assetId}/materialize", s.materializeAsset)
				ent.Post("/{id}/nfo", s.writeEntityNfo)

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

				// AUTO-MATCH individual (briefing 0050): tenta resolver sozinho um
				// item da fila — local-first (fundirEntities) ou externo (mesma
				// sequência do "Corrigir"), só quando o score bate LimiarAutoMatch.
				// Sempre 200 (nunca 5xx por "não achou candidato"); 404 inexistente.
				ent.Post("/{id}/auto-match", s.autoMatchEntity)

				// Prévia de candidato + aplicação supervisionada (briefing
				// 0060): a prévia reusa a MESMA busca/pontuação do auto-match
				// acima, mas SEM o corte de LimiarAutoMatch (dry-run, nunca
				// aplica nada); confirm-candidate aplica o candidato mostrado
				// mesmo abaixo do limiar -- decisão humana informada, terceiro
				// endpoint, não uma reforma de /confirm nem /correct.
				ent.Get("/{id}/match-preview", s.matchPreview)
				ent.Post("/{id}/confirm-candidate", s.confirmCandidate)

				// Ações rápidas SOB DEMANDA de UM item aberto no detalhe
				// (briefing 0059): enrich-missing dispara a MESMA orquestração
				// de tentarEnriquecerOuSinalizar (0056), fora do lote/throttle
				// do job; download-missing-artwork baixa a melhor arte
				// disponível para os kinds que faltam na completude de
				// catalogação (0058), via o mesmo agregador/download do 0048.
				ent.Post("/{id}/enrich-missing", s.enrichMissing)
				ent.Post("/{id}/download-missing-artwork", s.downloadMissingArtwork)

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

			// Sistema de JOBS (briefing 0052): tipos de job registrados em código
			// (Handler/Registry), configuração dinâmica (habilitado/agendamento) e
			// histórico genérico no banco, com agendador embutido (goroutine +
			// ticker, sem cron do SO). Lista/edita a config, dispara execução
			// manual e lista o histórico paginado — mesmo envelope de erro padrão
			// do resto da API.
			v1.Route("/jobs", func(jobs chi.Router) {
				jobs.Get("/", s.listJobs)
				jobs.Patch("/{id}", s.patchJob)
				jobs.Post("/{id}/run", s.runJob)
				jobs.Get("/{id}/executions", s.listJobExecutions)
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
