// Package watch implementa o WATCH de filesystem opcional por library_source
// (briefing 0064): fsnotify (sem recursão nativa) registrado manualmente por
// subdiretório, com DEBOUNCE por fonte coalescendo rajadas de eventos numa
// única disparada de scan DIRECIONADO (scanner.Manager.Start só daquela
// fonte) -- nunca o pipeline completo do job saude_biblioteca (0053). É
// "aceleração" pura: nunca substitui o job periódico -- mounts remotos
// (rclone/gdrive, o deploy real do projeto) não emitem evento nenhum via
// fsnotify para mudanças originadas do lado remoto (limitação de
// kernel/libfuse, issues rclone/rclone #4381 e #2882 -- ver AGENTS.md).
// Estado fica TODO em memória (mapa source_id -> watcher+timer de debounce),
// reconstruído no boot pelo chamador (internal/server) a partir de
// library_source.watch_enabled -- nenhuma tabela nova neste briefing.
package watch

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// DebounceDefault é o intervalo de debounce por fonte (briefing 0064 §6):
// curto o bastante pra "sentir" rápido, longo o bastante pra coalescer uma
// rajada típica (cópia de vários arquivos de uma vez).
const DebounceDefault = 15 * time.Second

// ScanStarter é o subconjunto de scanner.Manager que o Watcher consome:
// disparar um scan DIRECIONADO de UMA fonte. Interface pequena (mesmo padrão
// de libraryhealth.ScanRunner) para permitir um fake em teste sem varrer
// disco de verdade, e para este pacote nunca importar internal/scanner
// diretamente (evita acoplar o watch ao pipeline completo do scan).
type ScanStarter interface {
	Start(source library.Source, trigger string) (string, error)
}

// adder é o subconjunto de *fsnotify.Watcher usado por registrarSubdiretorios
// -- interface pequena só para o teste poder injetar falhas de Add()
// determinísticas (critério de aceite: "falha simulada de watcher.Add() não
// impede o registro das demais subpastas").
type adder interface {
	Add(name string) error
}

// fonteAtiva é o estado em memória de UMA library_source com watch ligado: o
// *fsnotify.Watcher dedicado + o timer de debounce corrente (nil até o
// primeiro evento chegar).
type fonteAtiva struct {
	fsw   *fsnotify.Watcher
	timer *time.Timer
}

// Watcher orquestra o watch fsnotify de TODAS as fontes com watch_enabled=1:
// um *fsnotify.Watcher e um timer de debounce POR FONTE (nunca um watcher
// global) -- RegistrarFonte/DesregistrarFonte de uma fonte nunca afeta as
// demais.
type Watcher struct {
	scanner  ScanStarter
	debounce time.Duration

	mu     sync.Mutex
	fontes map[string]*fonteAtiva // source_id -> estado
}

// New cria o Watcher. debounce<=0 usa DebounceDefault (o chamador de
// produção nunca passa <=0; testes encurtam para não esperar 15s de
// verdade).
func New(scanner ScanStarter, debounce time.Duration) *Watcher {
	if debounce <= 0 {
		debounce = DebounceDefault
	}
	return &Watcher{scanner: scanner, debounce: debounce, fontes: make(map[string]*fonteAtiva)}
}

// RegistrarFonte inicia (ou REINICIA, se já havia um watcher desta fonte) o
// watch fsnotify de UMA library_source: WalkDir uma vez, Add() em cada
// subdiretório (falha de Add() de UM subdiretório é logada e IGNORADA -- não
// aborta a fonte nem o boot, briefing 0064 §4). Chamado tanto no boot do
// servidor (para cada fonte com watch_enabled=1) quanto pelo handler PATCH
// /api/v1/library/{id} quando liga watch_enabled de false->true numa fonte já
// em execução -- a MESMA rotina, SEM restart do servidor (briefing 0064,
// critério de aceite do toggle).
func (w *Watcher) RegistrarFonte(source library.Source) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	registrarSubdiretorios(fsw, source.Path)

	w.mu.Lock()
	if antiga, ok := w.fontes[source.ID]; ok {
		fecharFonte(antiga)
	}
	fa := &fonteAtiva{fsw: fsw}
	w.fontes[source.ID] = fa
	w.mu.Unlock()

	go w.escutar(source, fa)
	return nil
}

// DesregistrarFonte fecha o watcher e o timer de debounce (se houver) de UMA
// fonte e remove sua entrada do mapa em memória -- chamado pelo handler PATCH
// quando desliga watch_enabled de true->false (briefing 0064, SEM restart do
// servidor). No-op silencioso se a fonte não tinha watch registrado.
func (w *Watcher) DesregistrarFonte(sourceID string) {
	w.mu.Lock()
	fa, ok := w.fontes[sourceID]
	if ok {
		delete(w.fontes, sourceID)
	}
	w.mu.Unlock()
	if ok {
		fecharFonte(fa)
	}
}

// Close fecha TODOS os watchers/timers registrados -- chamado no shutdown
// gracioso do servidor (mesmo padrão de lifecycle dos demais componentes
// long-running).
func (w *Watcher) Close() {
	w.mu.Lock()
	fontes := w.fontes
	w.fontes = make(map[string]*fonteAtiva)
	w.mu.Unlock()
	for _, fa := range fontes {
		fecharFonte(fa)
	}
}

// fecharFonte fecha o *fsnotify.Watcher (o que também encerra a goroutine
// escutar, via os canais Events/Errors fechando) e para o timer de debounce
// corrente, se houver.
func fecharFonte(fa *fonteAtiva) {
	if fa.timer != nil {
		fa.timer.Stop()
	}
	if err := fa.fsw.Close(); err != nil {
		log.Printf("watch: erro ao fechar watcher: %v", err)
	}
}

// registrarSubdiretorios percorre root UMA VEZ (WalkDir) e chama Add() em
// CADA subdiretório (fsnotify não observa recursão nativa -- confirmado em
// pesquisa, briefing 0064 §4). Erro de Add() num subdiretório específico
// (ex.: fs.inotify.max_user_watches, permissão, path removido entre o walk e
// o add) é LOGADO e IGNORADO -- continua para os demais, nunca aborta o
// registro da fonte inteira. Erro ao LISTAR um subdiretório (ilegível) pula
// aquela subárvore (fs.SkipDir), mesmo padrão de coletarArquivos
// (internal/scanner/scan.go).
func registrarSubdiretorios(fsw adder, root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("watch: pulando %q (best-effort): %v", path, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if aerr := fsw.Add(path); aerr != nil {
			log.Printf("watch: falha ao registrar watch em %q (best-effort, ignorado): %v", path, aerr)
		}
		return nil
	})
}

// escutar é o loop de eventos de UMA fonte: roda até fsw.Events/Errors
// fecharem (fecharFonte -> fsw.Close()). Pastas novas (Create de diretório)
// ganham Add() dinâmico (cobre pastas criadas depois do boot/registro);
// QUALQUER evento (Create/Write/Remove/Rename) reinicia o timer de debounce
// da fonte. Erros do canal Errors são logados -- o watch NUNCA para o
// servidor nem tenta se auto-recuperar em loop; a fonte simplesmente segue
// recebendo eventos dos watches ainda válidos (ou passa a depender só do
// polling periódico, se o watcher morreu de vez).
func (w *Watcher) escutar(source library.Source, fa *fonteAtiva) {
	for {
		select {
		case event, ok := <-fa.fsw.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create == fsnotify.Create {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if aerr := fa.fsw.Add(event.Name); aerr != nil {
						log.Printf("watch: falha ao adicionar %q dinamicamente (best-effort, ignorado): %v", event.Name, aerr)
					}
				}
			}
			w.agendarScan(source, fa)
		case err, ok := <-fa.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watch: erro do fsnotify na fonte %q (%s): %v -- fonte segue dependendo do polling periodico", source.ID, source.Path, err)
		}
	}
}

// agendarScan reinicia o timer de debounce da fonte (coalescendo rajadas de
// eventos numa única disparada, briefing 0064 §4/§6). Ao expirar, dispara
// scanner.Manager.Start(source) SÓ daquela fonte (nunca o pipeline completo
// do job saude_biblioteca) -- falha é logada, best-effort (o watch nunca
// derruba nada; a fonte continua dependendo do polling periódico).
func (w *Watcher) agendarScan(source library.Source, fa *fonteAtiva) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// A fonte pode ter sido desregistrada (ou re-registrada, virando outra
	// *fonteAtiva) entre o evento chegar e este lock -- corrida benigna: se
	// não é mais a instância corrente do mapa, não reagenda nada (o watcher
	// antigo já está sendo fechado/foi substituído).
	if atual, ok := w.fontes[source.ID]; !ok || atual != fa {
		return
	}
	if fa.timer != nil {
		fa.timer.Stop()
	}
	fa.timer = time.AfterFunc(w.debounce, func() {
		if _, err := w.scanner.Start(source, scanner.TriggerWatch); err != nil {
			log.Printf("watch: falha ao disparar scan de %q apos evento fsnotify (best-effort): %v", source.Path, err)
		}
	})
}
