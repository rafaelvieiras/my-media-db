// Package scanner orquestra a varredura de um datasource (briefing 0009): faz o
// fs walk filtrando extensões de vídeo, calcula o content_hash parcial, extrai
// os fatos técnicos via ffprobe (internal/mediainfo), associa cada arquivo a uma
// Entity pela convenção Kodi/Jellyfin (internal/naming) e descobre a arte local
// — tudo em background, não-destrutivo e honrando o ciclo de vida `missing`
// (ADR-0012). As escritas serializam pela conexão única, em lotes curtos
// (ADR-0010); o parser e o hash são puros e paralelizáveis.
//
// Orquestração (briefing 0108, ADR-0028): o scanner NÃO dispara mais sua
// própria goroutine nem guarda estado de execução em memória (running/
// filesSeen/filesTotal/startedAt/lastError) — isso vive na fila unificada
// (internal/execqueue). Start() só enfileira (ConfigurarFila); o trabalho de
// scan em si roda depois, quando o worker sequencial da fila chama de volta
// o Manager como um execqueue.Runner (Executar) — ver ConfigurarFila/
// ConfigurarLibrary abaixo. O único estado que continua vivendo no Manager é
// StorageNaoVideo (briefing 0063), que é específico do domínio scan e não
// pertence à fila genérica.
package scanner

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// carimboFormato é o formato do timestamp de last_seen usado pelo ciclo de vida.
// A precisão de nanossegundos torna o carimbo de cada scan praticamente único,
// o que a marcação de `missing` usa como sentinela ("visto neste scan?").
const carimboFormato = "2006-01-02T15:04:05.000000000Z"

// loteEscrita é o tamanho do lote curto de escrita (ADR-0010): a cada N arquivos
// a transação é commitada, para que uma interrupção preserve o já gravado.
const loteEscrita = 25

// Triggers conhecidos de Start — espelham execqueue.TriggerManual/Schedule/
// Watch (briefing 0108), mas DEFINIDOS AQUI (não como alias) para que
// pacotes que só dependem de scanner (watch, libraryhealth) não precisem
// importar internal/execqueue só para ter a constante — ver mapTrigger.
const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
	TriggerWatch    = "watch"
)

// Erros de orquestração; sentinelas para o handler mapear ao envelope de erro.
var (
	// ErrScanEmAndamento: já há um scan rodando (ou enfileirado) para este
	// datasource. -> 409. Espelha execqueue.ErrJaEnfileirado na superfície
	// pública do scanner (Start mapeia um para o outro).
	ErrScanEmAndamento = errors.New("já existe um scan em andamento para este datasource")
	// ErrSourceOffline: o datasource está inacessível (não dá para varrer). -> 409
	ErrSourceOffline = errors.New("datasource inacessível (offline)")
	// ErrFilaNaoConfigurada: Start chamado antes de ConfigurarFila -- todo
	// caminho de produção configura a fila no boot (cmd/mymediadb/main.go);
	// isto só deveria aparecer em teste/uso incompleto do pacote.
	ErrFilaNaoConfigurada = errors.New("scanner: fila de execução não configurada (ConfigurarFila)")
)

// Prober é a dependência de mediainfo que o scanner consome (ffprobe). Interface
// para permitir injeção nos testes.
type Prober interface {
	Disponivel() bool
	Probe(ctx context.Context, path string) ([]byte, error)
}

// SerieImporter é o ponto de AUTO-IMPORT que o scanner de séries (0034) consome
// quando a série de uma pasta ainda não está no catálogo: busca pelo nome no
// provider, escolhe o match exato e único e importa a árvore (reusa o enrich da Fase
// 1). Interface para injetar sem acoplar o scanner ao enrich e para testar com um
// stub. Implementado por *enrich.Servico. preferredLang (briefing 0084) é o
// idioma preferido da library_source que disparou o import (vazio = sem
// preferência, usa o idioma global resolvido por `idiomas`).
type SerieImporter interface {
	ImportarSeriePorNome(ctx context.Context, nome string, langs []string, preferredLang string) (string, error)
}

// FilmeImportador é o ponto de AUTO-MATCH que o scanner de filme consome
// logo após criar uma entity NOVA: tenta casar+enriquecer (metadados +
// inventário de artes) contra os providers configurados, reusando o MESMO
// limiar de confiança do auto-match em lote (LimiarAutoMatch, saude_biblioteca).
// Sem candidato forte o bastante, a entity permanece cataloged/draft,
// pendente na fila de Revisão -- igual hoje. Interface para não acoplar o
// scanner ao pacote server. Implementado por *Server.
type FilmeImportador interface {
	// TentarAutoMatchFilme tenta casar+enriquecer a entity; matched=true só
	// quando um candidato >= LimiarAutoMatch foi de fato aplicado.
	TentarAutoMatchFilme(ctx context.Context, entityID string) (matched bool, err error)
}

// Materializador é o ponto OPCIONAL que o scanner consome depois de
// QUALQUER auto-import bem-sucedido (filme ou série) para, se o dono tiver
// ligado "materializar artes automaticamente" (app_settings), materializar
// na hora as artes que o enrich acabou de descobrir -- sem esperar o job
// periódico midias_faltantes. Toggle desligado (default) = no-op.
// Implementado por *Server.
type Materializador interface {
	MaterializarSeAutoDownloadLigado(ctx context.Context, entityID string) error
}

// MusicaImportador é o ponto de IMPORT da vertical de música que o scanner
// consome. Importar (briefing 0107/ADR-0027) é o método genérico que toda
// vertical já usa (0015/0031/0106) -- hoje só usado pelo import MANUAL
// deliberado (busca → importar artista, dispatcher type="artista"), que traz
// a discografia inteira e PERMANECE intocado. ImportarFaixaPossuida (ADR-0029,
// briefing 0115) é o import ESCOPADO que o gatilho AUTOMÁTICO do scan usa
// desde então: uma faixa possuída traz só o artista (identidade) + o ÁLBUM
// daquela faixa + a própria faixa, nunca a árvore inteira. Interface estreita
// (mesmo padrão de SerieImporter/FilmeImportador) para não acoplar o scanner
// ao pacote enrich inteiro (só o tipo FaixaAlvo é exposto). Implementado por
// *enrich.Servico.
type MusicaImportador interface {
	Importar(ctx context.Context, source, sourceID, tipo string, langs []string) (id string, criada bool, err error)
	ImportarFaixaPossuida(ctx context.Context, source, artistaMBID string, alvo enrich.FaixaAlvo, langs []string) (faixaID string, criada bool, err error)
}

// LibraryGetter é o subconjunto de library.Service que o scanner consome
// para RE-RESOLVER um library.Source a partir só do id (briefing 0108): o
// worker da fila chama Manager.Executar bem depois do Enqueue original, só
// com o refID (string) em mãos -- precisa buscar de novo o Source completo
// (path, content_type) para poder escanear. *library.Service satisfaz isto
// estruturalmente (mesma assinatura usada em internal/server/scan.go).
type LibraryGetter interface {
	Obter(ctx context.Context, id string) (library.Source, error)
}

// Status é a fotografia consultável de um scan (superfície da API GET .../scan).
type Status struct {
	// Existe é falso quando nunca houve scan para o datasource.
	Existe     bool
	Running    bool
	FilesSeen  int
	FilesTotal int
	StartedAt  time.Time
	LastError  string
}

// storageResultado é o resultado da classificação de arquivo NÃO-vídeo
// (briefing 0063) da ÚLTIMA passada de WalkDir de UMA fonte -- dado
// específico do domínio scan (não pertence à fila genérica execqueue),
// mantido em memória no próprio Manager, DESACOPLADO do ciclo de vida do
// item de fila (sobrevive mesmo que o histórico do item em si já tenha sido
// consultado/rotacionado).
type storageResultado struct {
	managedBytes   int64
	unmanagedBytes int64
}

// Manager guarda as dependências do scan e o pequeno estado de storage em
// memória (StorageNaoVideo). A orquestração de execução (fila, concorrência,
// progresso, histórico) vive em internal/execqueue -- ver ConfigurarFila. As
// escritas de banco usam sempre o pool de escrita (db.Write) — o escritor
// único da ADR-0010.
type Manager struct {
	db     *db.DB
	prober Prober
	novoID func() string
	agora  func() time.Time

	// fila é a fila de execução unificada (briefing 0108): Start enfileira
	// nela em vez de disparar uma goroutine própria. nil só em uso
	// incompleto do pacote (todo caminho de produção chama ConfigurarFila no
	// boot) -- Start devolve ErrFilaNaoConfigurada nesse caso.
	fila *execqueue.Manager
	// library resolve library.Source por id -- necessário porque Executar
	// (chamado pelo worker da fila) só recebe o refID, não o Source inteiro.
	library LibraryGetter

	// serieImporter e idiomas alimentam o caminho de SÉRIE (0034): auto-import da
	// árvore pelo nome e os idiomas permitidos das settings. Ambos nil quando o
	// enrich está desligado — sem eles, uma série ausente do catálogo cai na fila de
	// revisão (rascunho) em vez de ser importada. Configurados via ConfigurarSeries.
	serieImporter SerieImporter
	idiomas       func(ctx context.Context) []string

	// filmeImportador alimenta o caminho de FILME (0089): auto-match imediato
	// disparado logo após o scanner criar uma entity nova. nil quando o
	// enrich está desligado -- sem ele, o filme fica cataloged/draft até um
	// job periódico ou ação manual (comportamento pré-0089). Configurado via
	// ConfigurarFilmes.
	filmeImportador FilmeImportador

	// materializador é OPCIONAL (0089): quando configurado e o toggle
	// "materializar_artes_automaticamente" está ligado, materializa as
	// artes recém-descobertas logo após um auto-import bem-sucedido (filme
	// ou série) do scan. nil = no-op, igual ao comportamento pré-0089.
	// Configurado via ConfigurarMaterializacao.
	materializador Materializador

	// musicaImportador/musicaProvider alimentam o caminho de MÚSICA (0107): o
	// import por MBID (idempotente, via MusicaImportador) e o provider
	// MusicBrainz em si (para a busca textual no fallback sem MBID embutido,
	// Provider.Buscar, e a capacidade opcional provider.FaixaResolverProvider
	// -- resolução reversa faixa→artista). Ambos nil = scanner de música opera
	// em modo degradado (toda faixa vira draft, sem tentar casar) -- mesmo
	// padrão de nil-safe de serieImporter/filmeImportador. Configurados via
	// ConfigurarMusica.
	musicaImportador MusicaImportador
	musicaProvider   provider.Provider

	// muStorage protege storageNaoVideo -- pequeno estado em memória, POR
	// FONTE, do resultado da classificação managed/unmanaged do briefing
	// 0063 (ver StorageNaoVideo). Mutex próprio, separado de qualquer
	// orquestração de execução (que agora vive no execqueue).
	muStorage       sync.Mutex
	storageNaoVideo map[string]storageResultado
}

// ConfigurarFila injeta a fila de execução unificada (briefing 0108,
// ADR-0028) e registra o próprio Manager como o execqueue.Runner do kind
// "scan" -- chamado pelo boot do processo (cmd/mymediadb/main.go). Sem isto,
// Start devolve ErrFilaNaoConfigurada.
func (m *Manager) ConfigurarFila(fila *execqueue.Manager) {
	m.fila = fila
	fila.RegistrarRunner(execqueue.KindScan, m)
}

// ConfigurarLibrary injeta o resolvedor de library.Source por id (briefing
// 0108) -- chamado pelo boot do processo. Sem isto, Executar (chamado pelo
// worker da fila) falha ao tentar re-resolver o Source do refID.
func (m *Manager) ConfigurarLibrary(lister LibraryGetter) {
	m.library = lister
}

// ConfigurarSeries injeta o auto-import de séries (0034): o importador por nome e a
// fonte dos idiomas permitidos. Chamado pelo server após montar o enrich; sem isso o
// scanner de séries opera em modo degradado (não importa, só casa o que já existe e
// manda o resto para revisão).
func (m *Manager) ConfigurarSeries(imp SerieImporter, idiomas func(ctx context.Context) []string) {
	m.serieImporter = imp
	m.idiomas = idiomas
}

// ConfigurarFilmes injeta o auto-match imediato de filme (0089): chamado pelo
// server após montar o enrich; sem isso o scanner de filme opera em modo
// degradado (não tenta auto-match no scan, só cria a entity cataloged/draft
// como antes deste briefing).
func (m *Manager) ConfigurarFilmes(imp FilmeImportador) {
	m.filmeImportador = imp
}

// ConfigurarMaterializacao injeta a materialização automática opcional
// (0089), consumida depois de qualquer auto-import bem-sucedido do scan
// (filme ou série). Sem chamar isto, o scan nunca materializa artes
// automaticamente, mesmo com o toggle de settings ligado.
func (m *Manager) ConfigurarMaterializacao(mat Materializador) {
	m.materializador = mat
}

// ConfigurarMusica injeta a vertical de música (0107): o importador por MBID
// (find-or-create idempotente da árvore inteira, via *enrich.Servico.Importar)
// e o provider MusicBrainz em si (busca textual + resolução reversa
// faixa→artista, quando o provider implementar provider.FaixaResolverProvider
// -- ver internal/scanner/musica.go). Sem chamar isto, o scanner de música
// opera em modo degradado: todo arquivo de áudio vira uma entity `faixa`
// rascunho, sem tentar casar contra a fonte (mesmo padrão de
// ConfigurarSeries/ConfigurarFilmes).
func (m *Manager) ConfigurarMusica(imp MusicaImportador, prov provider.Provider) {
	m.musicaImportador = imp
	m.musicaProvider = prov
}

// NewManager cria um Manager com o ffprobe real do PATH.
func NewManager(database *db.DB) *Manager {
	return NewManagerComProber(database, mediainfo.NewProber())
}

// NewManagerComProber cria um Manager com um Prober explícito (testes).
func NewManagerComProber(database *db.DB, prober Prober) *Manager {
	return &Manager{
		db:              database,
		prober:          prober,
		novoID:          func() string { return uuid.NewString() },
		agora:           func() time.Time { return time.Now().UTC() },
		storageNaoVideo: make(map[string]storageResultado),
	}
}

// Start enfileira o scan de um datasource (briefing 0108: não dispara mais
// goroutine própria) e devolve o id do item de fila (202 na API).
// ErrSourceOffline se o datasource estiver inacessível — nesse caso o estado
// offline é PERSISTIDO e nada é marcado missing (ADR-0012); essa checagem
// continua SÍNCRONA (stat barato, não precisa de fila). ErrScanEmAndamento
// se já houver um item queued/running para esta fonte (dedup do execqueue,
// mapeado para o erro sentinela público de sempre). ErrFilaNaoConfigurada se
// ConfigurarFila nunca foi chamado.
func (m *Manager) Start(source library.Source, trigger string) (string, error) {
	if !acessivel(source.Path) {
		m.persistirEstado(context.Background(), source, "offline")
		return "", ErrSourceOffline
	}
	if m.fila == nil {
		return "", ErrFilaNaoConfigurada
	}

	id, err := m.fila.Enqueue(context.Background(), execqueue.KindScan, source.ID, mapTrigger(trigger))
	if err != nil {
		if errors.Is(err, execqueue.ErrJaEnfileirado) {
			return "", ErrScanEmAndamento
		}
		return "", err
	}
	return id, nil
}

// mapTrigger traduz um scanner.TriggerX para o execqueue.TriggerX
// equivalente -- valores string IGUAIS hoje, mas mapeados explicitamente
// (não por alias/const compartilhada) para que scanner.TriggerX continue
// definido neste pacote sem depender da representação interna do execqueue.
// Um trigger fora do conjunto conhecido é repassado como está -- Enqueue
// devolve ErrTriggerInvalido, que sobe como erro normal de Start.
func mapTrigger(trigger string) string {
	switch trigger {
	case TriggerManual:
		return execqueue.TriggerManual
	case TriggerSchedule:
		return execqueue.TriggerSchedule
	case TriggerWatch:
		return execqueue.TriggerWatch
	default:
		return trigger
	}
}

// Executar implementa execqueue.Runner: é chamado pelo worker sequencial da
// fila (bem depois do Enqueue original de Start), com só o refID
// (library_source.id) em mãos -- re-resolve o Source completo via
// LibraryGetter e roda o pipeline de scan de sempre, síncrono (o worker JÁ É
// a goroutine de background; o scanner não precisa mais da própria). Pânico
// dentro do pipeline é recuperado pelo PRÓPRIO execqueue
// (executarComRecover) -- não duplicamos recover aqui.
func (m *Manager) Executar(ctx context.Context, refID string, _ string, progresso execqueue.ProgressoFunc) (string, error) {
	if m.library == nil {
		return "", errors.New("scanner: library getter não configurado (ConfigurarLibrary)")
	}
	source, err := m.library.Obter(ctx, refID)
	if err != nil {
		return "", err
	}
	if err := m.executarScan(ctx, source, progresso); err != nil {
		return "", err
	}
	return "", nil
}

// Status devolve a fotografia do scan do datasource (Existe=false se nunca
// houve item de fila para esta fonte), traduzindo execqueue.QueueItemStatus
// para o shape público de sempre -- ver internal/server/scan.go, que
// continua consumindo isto sem mudança de contrato JSON.
func (m *Manager) Status(sourceID string) Status {
	if m.fila == nil {
		return Status{}
	}
	qis, err := m.fila.Status(context.Background(), execqueue.KindScan, sourceID)
	if err != nil || !qis.Existe {
		return Status{}
	}

	st := Status{
		Existe: true,
		// Running=true tanto para 'queued' quanto 'running': um item recém
		// enfileirado pode não ter sido pego pelo worker ainda (algo mais
		// rodando), mas o chamador (libraryhealth.aguardarConclusao) não deve
		// ver Running=false prematuramente só por causa disso -- preserva o
		// comportamento observável de antes (Start() criava o job síncrono
		// já com running=true).
		Running:    qis.Status == execqueue.StatusQueued || qis.Status == execqueue.StatusRunning,
		FilesSeen:  qis.ProgressCurrent,
		FilesTotal: qis.ProgressTotal,
	}

	// started_at: vazio enquanto o item ainda está 'queued' (nunca começou a
	// rodar de verdade). O job antigo sempre tinha startedAt setado desde a
	// CRIAÇÃO -- para preservar essa garantia (Existe=true nunca teve
	// started_at zero antes), cai para enqueued_at quando started_at ainda
	// não existe.
	ts := qis.StartedAt
	if ts == "" {
		ts = qis.EnqueuedAt
	}
	if t, perr := time.Parse(time.RFC3339, ts); perr == nil {
		st.StartedAt = t
	}

	// LastError só em erro -- não vaza qis.Message de uma finalização com
	// sucesso (o campo Message também carrega a mensagem final de sucesso,
	// que aqui é sempre "" pelo Runner, mas defensivamente checamos o
	// status mesmo assim).
	if qis.Status == execqueue.StatusError {
		st.LastError = qis.Message
	}
	return st
}

// setStorageNaoVideo grava o resultado da classificação managed/unmanaged de
// arquivo NÃO-vídeo (briefing 0063) desta passada de WalkDir, sob muStorage.
func (m *Manager) setStorageNaoVideo(sourceID string, managedBytes, unmanagedBytes int64) {
	m.muStorage.Lock()
	m.storageNaoVideo[sourceID] = storageResultado{managedBytes: managedBytes, unmanagedBytes: unmanagedBytes}
	m.muStorage.Unlock()
}

// StorageNaoVideo devolve os bytes de arquivo NÃO-vídeo classificados como
// managed (nome casa com um padrão canônico do briefing 0062) e unmanaged
// (resto) na ÚLTIMA varredura completa desta fonte -- briefing 0063, sem
// varrer o disco de novo (o valor já foi calculado DENTRO de executarScan,
// na MESMA passada de WalkDir que coletarArquivos já fazia). ok=false quando
// nunca houve scan para esta fonte -- o chamador (libraryhealth.Handler)
// trata isso como "sem dado ainda", não como zero.
func (m *Manager) StorageNaoVideo(sourceID string) (managedBytes, unmanagedBytes int64, ok bool) {
	m.muStorage.Lock()
	defer m.muStorage.Unlock()
	res, existe := m.storageNaoVideo[sourceID]
	if !existe {
		return 0, 0, false
	}
	return res.managedBytes, res.unmanagedBytes, true
}

// acessivel diz se um caminho existe, é diretório e é legível (stat barato).
func acessivel(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
