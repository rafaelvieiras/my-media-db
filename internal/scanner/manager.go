// Package scanner orquestra a varredura de um datasource (briefing 0009): faz o
// fs walk filtrando extensões de vídeo, calcula o content_hash parcial, extrai
// os fatos técnicos via ffprobe (internal/mediainfo), associa cada arquivo a uma
// Entity pela convenção Kodi/Jellyfin (internal/naming) e descobre a arte local
// — tudo em background, não-destrutivo e honrando o ciclo de vida `missing`
// (ADR-0012). As escritas serializam pela conexão única, em lotes curtos
// (ADR-0010); o parser e o hash são puros e paralelizáveis.
//
// O mecanismo de job é mínimo (ADR-0010/doc 03): uma goroutine por datasource
// com estado em memória e um contexto cancelável; um scan por datasource por
// vez. Uma fila persistente com retry/agenda é evolução (§7 do briefing).
package scanner

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
)

// carimboFormato é o formato do timestamp de last_seen usado pelo ciclo de vida.
// A precisão de nanossegundos torna o carimbo de cada scan praticamente único,
// o que a marcação de `missing` usa como sentinela ("visto neste scan?").
const carimboFormato = "2006-01-02T15:04:05.000000000Z"

// loteEscrita é o tamanho do lote curto de escrita (ADR-0010): a cada N arquivos
// a transação é commitada, para que uma interrupção preserve o já gravado.
const loteEscrita = 25

// Erros de orquestração; sentinelas para o handler mapear ao envelope de erro.
var (
	// ErrScanEmAndamento: já há um scan rodando para este datasource. -> 409
	ErrScanEmAndamento = errors.New("já existe um scan em andamento para este datasource")
	// ErrSourceOffline: o datasource está inacessível (não dá para varrer). -> 409
	ErrSourceOffline = errors.New("datasource inacessível (offline)")
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
// stub. Implementado por *enrich.Servico.
type SerieImporter interface {
	ImportarSeriePorNome(ctx context.Context, nome string, langs []string) (string, error)
}

// job é o estado em memória de um scan de um datasource.
type job struct {
	id         string
	running    bool
	filesSeen  int
	filesTotal int
	startedAt  time.Time
	lastError  string
	cancel     context.CancelFunc

	// naoVideoManagedBytes/naoVideoUnmanagedBytes são o resultado da
	// classificação de arquivo NÃO-vídeo (briefing 0063) da ÚLTIMA passada de
	// WalkDir deste job -- gravados por classificarNaoVideos/
	// setStorageNaoVideo, ANTES do scan terminar (não esperam
	// finalizarCicloVida), para sobreviverem mesmo se o scan for cancelado no
	// meio dos lotes de escrita.
	naoVideoManagedBytes   int64
	naoVideoUnmanagedBytes int64
}

// Status é a fotografia consultável de um job (superfície da API GET .../scan).
type Status struct {
	// Existe é falso quando nunca houve scan para o datasource.
	Existe     bool
	Running    bool
	FilesSeen  int
	FilesTotal int
	StartedAt  time.Time
	LastError  string
}

// Manager guarda os jobs em memória e serializa o acesso a eles. As escritas de
// banco usam sempre o pool de escrita (db.Write) — o escritor único da ADR-0010.
type Manager struct {
	db     *db.DB
	prober Prober
	novoID func() string
	agora  func() time.Time

	// serieImporter e idiomas alimentam o caminho de SÉRIE (0034): auto-import da
	// árvore pelo nome e os idiomas permitidos das settings. Ambos nil quando o
	// enrich está desligado — sem eles, uma série ausente do catálogo cai na fila de
	// revisão (rascunho) em vez de ser importada. Configurados via ConfigurarSeries.
	serieImporter SerieImporter
	idiomas       func(ctx context.Context) []string

	mu   sync.Mutex
	jobs map[string]*job
}

// ConfigurarSeries injeta o auto-import de séries (0034): o importador por nome e a
// fonte dos idiomas permitidos. Chamado pelo server após montar o enrich; sem isso o
// scanner de séries opera em modo degradado (não importa, só casa o que já existe e
// manda o resto para revisão).
func (m *Manager) ConfigurarSeries(imp SerieImporter, idiomas func(ctx context.Context) []string) {
	m.serieImporter = imp
	m.idiomas = idiomas
}

// NewManager cria um Manager com o ffprobe real do PATH.
func NewManager(database *db.DB) *Manager {
	return NewManagerComProber(database, mediainfo.NewProber())
}

// NewManagerComProber cria um Manager com um Prober explícito (testes).
func NewManagerComProber(database *db.DB, prober Prober) *Manager {
	return &Manager{
		db:     database,
		prober: prober,
		novoID: func() string { return uuid.NewString() },
		agora:  func() time.Time { return time.Now().UTC() },
		jobs:   make(map[string]*job),
	}
}

// Start dispara o scan de um datasource em background. Devolve o id do job (202
// na API). ErrScanEmAndamento se já houver scan rodando; ErrSourceOffline se o
// datasource estiver inacessível — nesse caso o estado offline é PERSISTIDO e
// nada é marcado missing (ADR-0012).
func (m *Manager) Start(source library.Source) (string, error) {
	if !acessivel(source.Path) {
		m.persistirEstado(context.Background(), source, "offline")
		return "", ErrSourceOffline
	}

	m.mu.Lock()
	if j, ok := m.jobs[source.ID]; ok && j.running {
		m.mu.Unlock()
		return "", ErrScanEmAndamento
	}
	jobID := m.novoID()
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{id: jobID, running: true, startedAt: m.agora(), cancel: cancel}
	m.jobs[source.ID] = j
	m.mu.Unlock()

	go m.run(ctx, source, j)
	return jobID, nil
}

// Status devolve a fotografia do job do datasource (ou Existe=false se nunca
// houve scan).
func (m *Manager) Status(sourceID string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[sourceID]
	if !ok {
		return Status{}
	}
	return Status{
		Existe:     true,
		Running:    j.running,
		FilesSeen:  j.filesSeen,
		FilesTotal: j.filesTotal,
		StartedAt:  j.startedAt,
		LastError:  j.lastError,
	}
}

// run executa o scan e, ao fim, marca o job como parado (registrando o erro, se
// houver). Recupera de pânico para nunca deixar um job "running" preso.
func (m *Manager) run(ctx context.Context, source library.Source, j *job) {
	defer func() {
		if rec := recover(); rec != nil {
			m.finalizarJob(j, errors.New("pânico durante o scan"))
			log.Printf("scanner: pânico no scan de %q: %v", source.ID, rec)
		}
	}()
	err := m.executarScan(ctx, source, j)
	m.finalizarJob(j, err)
	if err != nil {
		log.Printf("scanner: scan de %q terminou com erro: %v", source.ID, err)
	}
}

// finalizarJob marca o job como parado e grava o último erro, sob o lock.
func (m *Manager) finalizarJob(j *job, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j.running = false
	if err != nil {
		j.lastError = err.Error()
	}
}

// setTotal / addSeen atualizam o progresso do job sob o lock.
func (m *Manager) setTotal(j *job, total int) {
	m.mu.Lock()
	j.filesTotal = total
	m.mu.Unlock()
}

func (m *Manager) addSeen(j *job, n int) {
	m.mu.Lock()
	j.filesSeen += n
	m.mu.Unlock()
}

// setStorageNaoVideo grava o resultado da classificação managed/unmanaged de
// arquivo NÃO-vídeo (briefing 0063) desta passada de WalkDir, sob o lock.
func (m *Manager) setStorageNaoVideo(j *job, managedBytes, unmanagedBytes int64) {
	m.mu.Lock()
	j.naoVideoManagedBytes = managedBytes
	j.naoVideoUnmanagedBytes = unmanagedBytes
	m.mu.Unlock()
}

// StorageNaoVideo devolve os bytes de arquivo NÃO-vídeo classificados como
// managed (nome casa com um padrão canônico do briefing 0062) e unmanaged
// (resto) na ÚLTIMA varredura completa desta fonte -- briefing 0063, sem
// varrer o disco de novo (o valor já foi calculado DENTRO de executarScan,
// na MESMA passada de WalkDir que coletarArquivos já fazia). ok=false quando
// nunca houve scan para esta fonte -- o chamador (libraryhealth.Handler)
// trata isso como "sem dado ainda", não como zero.
func (m *Manager) StorageNaoVideo(sourceID string) (managedBytes, unmanagedBytes int64, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, existe := m.jobs[sourceID]
	if !existe {
		return 0, 0, false
	}
	return j.naoVideoManagedBytes, j.naoVideoUnmanagedBytes, true
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
