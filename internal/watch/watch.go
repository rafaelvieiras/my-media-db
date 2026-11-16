// Package watch implements the optional filesystem WATCH per library_source:
// fsnotify (no native recursion) registered manually per subdirectory, with a
// per-source DEBOUNCE coalescing bursts of events into a single TARGETED scan
// trigger (scanner.Manager.Start for that source only) -- never the full
// pipeline of the saude_biblioteca job. It is pure "acceleration": it never
// replaces the periodic job -- remote mounts (rclone/gdrive, the project's real
// deployment) emit no event at all via fsnotify for changes originated on the
// remote side (a kernel/libfuse limitation, rclone/rclone issues #4381 and
// #2882 -- see AGENTS.md). All the state stays in memory (map source_id ->
// watcher + debounce timer), rebuilt at boot by the caller (internal/server)
// from library_source.watch_enabled -- no new table for this.
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

// DebounceDefault is the debounce interval per source: short enough to "feel"
// fast, long enough to coalesce a typical burst (copying several files at once).
const DebounceDefault = 15 * time.Second

// ScanStarter is the subset of scanner.Manager that the Watcher consumes: trigger
// a TARGETED scan of ONE source. A small interface (same pattern as
// libraryhealth.ScanRunner) so a fake can be used in a test without really
// sweeping the disk, and so this package never imports internal/scanner directly
// (it avoids coupling the watch to the full scan pipeline).
type ScanStarter interface {
	Start(source library.Source, trigger string) (string, error)
}

// adder is the subset of *fsnotify.Watcher used by registrarSubdiretorios -- a
// small interface only so the test can inject deterministic Add() failures
// (acceptance criterion: "a simulated watcher.Add() failure does not prevent the
// registration of the remaining subfolders").
type adder interface {
	Add(name string) error
}

// fonteAtiva is the in-memory state of ONE library_source with watch enabled:
// the dedicated *fsnotify.Watcher plus the current debounce timer (nil until the
// first event arrives).
type fonteAtiva struct {
	fsw   *fsnotify.Watcher
	timer *time.Timer
}

// Watcher orchestrates the fsnotify watch of EVERY source with watch_enabled=1:
// one *fsnotify.Watcher and one debounce timer PER SOURCE (never a global
// watcher) -- RegistrarFonte/DesregistrarFonte on one source never affects the
// others.
type Watcher struct {
	scanner  ScanStarter
	debounce time.Duration

	mu     sync.Mutex
	fontes map[string]*fonteAtiva // source_id -> estado
}

// New creates the Watcher. debounce<=0 uses DebounceDefault (the production
// caller never passes <=0; tests shorten it to avoid waiting a real 15s).
func New(scanner ScanStarter, debounce time.Duration) *Watcher {
	if debounce <= 0 {
		debounce = DebounceDefault
	}
	return &Watcher{scanner: scanner, debounce: debounce, fontes: make(map[string]*fonteAtiva)}
}

// RegistrarFonte starts (or RESTARTS, if there already was a watcher for this
// source) the fsnotify watch of ONE library_source: WalkDir once, Add() on every
// subdirectory (an Add() failure on ONE subdirectory is logged and IGNORED -- it
// does not abort the source nor the boot). Called both at server boot (for every
// source with watch_enabled=1) and by the PATCH /api/v1/library/{id} handler when
// it turns watch_enabled from false to true on a source already running -- the
// SAME routine, with NO server restart (the acceptance criterion of the toggle).
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

// DesregistrarFonte closes the watcher and the debounce timer (if any) of ONE
// source and removes its entry from the in-memory map -- called by the PATCH
// handler when it turns watch_enabled from true to false (WITHOUT restarting the
// server). A silent no-op if the source had no watch registered.
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

// Close closes ALL registered watchers/timers -- called on the graceful shutdown
// of the server (the same lifecycle pattern as the other long-running
// components).
func (w *Watcher) Close() {
	w.mu.Lock()
	fontes := w.fontes
	w.fontes = make(map[string]*fonteAtiva)
	w.mu.Unlock()
	for _, fa := range fontes {
		fecharFonte(fa)
	}
}

// fecharFonte closes the *fsnotify.Watcher (which also ends the escutar
// goroutine, as the Events/Errors channels close) and stops the current debounce
// timer, if any.
func fecharFonte(fa *fonteAtiva) {
	if fa.timer != nil {
		fa.timer.Stop()
	}
	if err := fa.fsw.Close(); err != nil {
		log.Printf("watch: erro ao fechar watcher: %v", err)
	}
}

// registrarSubdiretorios walks root ONCE (WalkDir) and calls Add() on EVERY
// subdirectory (fsnotify has no native recursion -- confirmed in research). An
// Add() error on a specific subdirectory (e.g. fs.inotify.max_user_watches,
// permissions, a path removed between the walk and the add) is LOGGED and
// IGNORED -- it carries on with the rest and never aborts the registration of
// the whole source. An error LISTING a subdirectory (unreadable) skips that
// subtree (fs.SkipDir), the same pattern as coletarArquivos
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

// escutar is the event loop of ONE source: it runs until fsw.Events/Errors
// close (fecharFonte -> fsw.Close()). New folders (directory Create) get a
// dynamic Add() (it covers folders created after boot/registration); ANY event
// (Create/Write/Remove/Rename) restarts the debounce timer of the source. Errors
// from the Errors channel are logged -- the watch NEVER stops the server nor
// tries to recover itself in a loop; the source simply keeps receiving events
// from the watches still valid (or starts relying only on periodic polling, if
// the watcher died for good).
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

// agendarScan restarts the debounce timer of the source (coalescing bursts of
// events into a single trigger). When it expires, it triggers
// scanner.Manager.Start(source) for THAT source only (never the full pipeline of
// the saude_biblioteca job) -- a failure is logged, best-effort (the watch never
// brings anything down; the source keeps relying on periodic polling).
func (w *Watcher) agendarScan(source library.Source, fa *fonteAtiva) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// The source may have been unregistered (or re-registered, becoming another
	// *fonteAtiva) between the event arriving and this lock -- a benign race: if
	// it is no longer the current instance in the map, do not reschedule anything
	// (the old watcher is already being closed/was replaced).
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
