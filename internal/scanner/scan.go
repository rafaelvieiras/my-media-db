package scanner

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
)

// extensoesVideo is the extension filter (starting point, see
// docs/06-library-and-scan.md). Lowercase.
var extensoesVideo = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true,
}

// extensoesAudio is the AUDIO extension filter (same shape as extensoesVideo) —
// used instead of extensoesVideo when
// library_source.content_type == contentTypeMusic (see extensoesPrincipais).
var extensoesAudio = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true,
	".ogg": true, ".opus": true, ".wav": true,
}

// nomesPoster are the folder-level local artwork names (Kodi/Jellyfin).
var nomesPoster = []string{"poster.jpg", "poster.png", "folder.jpg", "folder.png"}

// pastasIgnoradas are directory names that never hold library content. They are
// skipped whole (fs.SkipDir): Synology metadata (@eaDir), AppleDouble sidecars,
// recycle bins and the lost+found of a repaired filesystem. None of them is media,
// and walking them only produces noise (on a real library @eaDir alone held more
// entries than the movies).
var pastasIgnoradas = map[string]bool{
	"@eadir":                    true,
	".appledouble":              true,
	".trashes":                  true,
	".trash":                    true,
	"#recycle":                  true,
	".recycle":                  true,
	"$recycle.bin":              true,
	"system volume information": true,
	"lost+found":                true,
}

// arquivosIgnorados are file names that are never media: AppleDouble sidecars
// ("._Name.mkv", which used to pass the extension filter and fail ffprobe),
// thumbnail caches and the desktop marker files of each OS.
var arquivosIgnorados = map[string]bool{
	".ds_store":   true,
	"thumbs.db":   true,
	"ehthumbs.db": true,
	"desktop.ini": true,
}

// ehArquivoIgnorado says whether a file must be left out of the scan entirely. It
// is a pure name rule: no stat, no extension.
func ehArquivoIgnorado(nome string) bool {
	l := strings.ToLower(nome)
	return arquivosIgnorados[l] || strings.HasPrefix(nome, "._") || strings.HasPrefix(nome, ".")
}

// scanRun carries the context of ONE scan: it avoids passing half a dozen
// parameters around per function and concentrates the entity cache (folderKey ->
// entityID) that makes the re-scan idempotent and groups the versions of the same
// work.
type scanRun struct {
	m          *Manager
	source     library.Source
	scanTS     string
	probeAtivo bool
	cache      map[string]string
	// serieCache keeps the resolution of each series of the scan (key = series
	// folder), so as not to re-fetch/re-import the same series for every file.
	// Analogous to the movie folderKey cache, but keyed by the series folder
	// (0034).
	serieCache map[string]serieResolvida
	// parcial marks that a SUBFOLDER (not the root, already validated by Stat)
	// failed to be read during the WalkDir (e.g. a transient timeout/disconnection
	// of a FUSE/rclone network mount in the middle of the walk). Unlike the
	// unreachable root (offline, it aborts before writing anything), here the
	// files that WERE seen keep being written normally -- only the destructive
	// closing of the lifecycle (marking `missing` whatever was not reviewed) is
	// skipped, so as not to disconnect files that were only not listed because of
	// a passing mount instability, not because they really disappeared from disk.
	parcial bool

	// progresso/filesSeen/filesTotal are the local progress state of this SINGLE
	// execution -- they replace the old in-memory *job in the Manager
	// (addSeen/setTotal under m.mu): they no longer need to be thread-safe across
	// concurrent executions (the queue guarantees concurrency 1), only across the
	// worker of this execution, which is single-threaded here. progresso may be
	// nil (queue bypass in tests, see scanner_test.go); filesSeen increments PER
	// FILE processed (success OR error/skip), inside the inner loop of
	// processarLotes -- no longer per batch of 25 (that was what made the counter
	// look stuck on slow music scans, a real finding from the production
	// validation of the music vertical).
	progresso  execqueue.ProgressoFunc
	filesSeen  int
	filesTotal int

	// novos accumulates the entities CREATED in this scan pass, by type, so that
	// the post-import (movie auto-match, optional materialization) runs AFTER the
	// batch commit -- see ADR-0031. Before, the trigger was called from inside
	// gravarArquivo, that is, inside the transaction: the auto-match read the
	// entity through the READ pool and got `sql: no rows in result set` (the
	// uncommitted row does not exist for it) and, if it tried to write, it would
	// block on the SINGLE write connection (ADR-0010) -- deadlock. See
	// despacharNovos.
	novos []entidadeNova
}

// entidadeNova is an entity created by this scan, with the type and the
// confidence already resolved -- what despacharNovos needs to decide which hook
// to call.
type entidadeNova struct {
	id   string
	tipo string
	conf float64
}

// registrarNovo records an entity created in this pass (dedup by id, because the
// music flow may resolve the same album for several tracks).
func (r *scanRun) registrarNovo(e entidadeNova) {
	for _, existente := range r.novos {
		if existente.id == e.id {
			return
		}
	}
	r.novos = append(r.novos, e)
}

// reportarProgresso calls the progress callback (if any) with the current state
// of filesSeen/filesTotal of this execution.
func (r *scanRun) reportarProgresso() {
	r.reportarProgressoMSG("")
}

// reportarProgressoMSG is reportarProgresso with a free-form message (used by the
// post-scan stage so the owner can see what is going on without inventing a file
// count).
func (r *scanRun) reportarProgressoMSG(msg string) {
	if r.progresso != nil {
		r.progresso(r.filesSeen, r.filesTotal, msg)
	}
}

// fileFacts are the pre-computed facts of a file (outside the write
// transaction): hash, mediainfo and the parsing of the names.
type fileFacts struct {
	path      string
	folderKey string      // grouping key of the work (the movie dir, or the file itself if loose)
	folderDir string      // directory where the local artwork is looked up
	rawNome   string      // raw base name (fallback title for a draft)
	entidade  naming.Info // parse of the NAME OF THE WORK (folder or loose file)
	arquivo   naming.Info // parse of the FILE NAME (edition/part)
	size      int64
	mtime     string
	hash      string
	mediainfo []byte
	poster    string // path of the local artwork; "" if none

	// Fields of the SERIES path (0034), filled only when content_type='series'.
	isSerie    bool     // marks the series flow (branches gravarArquivo)
	serieID    string   // entity of the SERIES (the "work"); the media_file points to it
	serieConf  float64  // confidence of the series resolution (100 by external id, 90 by title/import)
	episodeIDs []string // matched episodes, in order (defines the ordinal of the link)
	// prov keeps the numbering READ FROM THE FILE when the matching against the
	// external tree found no counterpart: it is what allows creating the
	// provisional episode. nil when there is nothing to create.
	prov *provisorioEpisodio

	// Fields of the MUSIC path (0107), filled only when content_type='music'.
	isMusica  bool    // marks the music flow (branches gravarArquivo)
	faixaID   string  // FAIXA entity already resolved; media_file.entity_id points DIRECTLY to it (1:1, ADR-0027 item 5)
	faixaConf float64 // confidence of the link (confIDExterno/confMusicaTextual/confDraft, see musica.go)
}

// executarScan runs the complete pipeline of a datasource. progresso is the
// execqueue callback called for each processed file -- it may be nil (queue
// bypass in tests, see scanner_test.go).
func (m *Manager) executarScan(ctx context.Context, source library.Source, progresso execqueue.ProgressoFunc) error {
	r := &scanRun{
		m:          m,
		source:     source,
		scanTS:     m.agora().Format(carimboFormato),
		probeAtivo: m.prober.Disponivel(),
		cache:      make(map[string]string),
		serieCache: make(map[string]serieResolvida),
		progresso:  progresso,
	}
	if !r.probeAtivo {
		log.Printf("scanner: ffprobe indisponível; mediainfo ficará vazio no scan de %q", source.ID)
	}

	arquivos, naoVideos, parcial, err := coletarArquivos(source.Path, r.extensoesPrincipais())
	if err != nil {
		// The root became unreachable: it signals offline and does NOT mark
		// anything missing.
		m.persistirEstado(ctx, source, "offline")
		return err
	}
	r.parcial = parcial
	if parcial {
		log.Printf("scanner: varredura parcial de %q (subpasta(s) ilegível(is) durante o walk); ciclo de vida NÃO marcará arquivos ausentes nesta rodada", source.ID)
	}
	r.filesTotal = len(arquivos)
	r.reportarProgresso()
	// NON-video file classification (an extension of the managed/unmanaged
	// storage accounting): it reuses the SAME WalkDir pass of coletarArquivos
	// above -- it never walks the disk again just for this.
	managed, unmanaged := classificarNaoVideos(arquivos, naoVideos)
	m.setStorageNaoVideo(source.ID, managed, unmanaged)

	if err := r.processarLotes(ctx, arquivos); err != nil {
		// Cancelled/error in the middle: batches already committed persist; we do
		// not close the lifecycle (no missing at all) nor stamp last_scan.
		return err
	}

	if err := r.finalizarCicloVida(ctx); err != nil {
		return err
	}

	// Post-scan (ADR-0031): with the lifecycle CLOSED, the library itself
	// reconciles whatever stayed as a draft and retries whatever is pending --
	// without waiting for the daily cron. Best-effort: it never takes the scan
	// down (the catalog is already written and the last_scan has already been
	// stamped).
	r.posScan(ctx)
	return nil
}

// processarLotes pre-computes the facts of each file (I/O and ffprobe, outside
// the transaction) and writes in short batches, checking for cancellation
// between batches. Progress is reported for each FILE processed inside the inner
// loop (success OR error/skip -- the file was "seen" either way, and filesTotal
// already counts it) -- the loteEscrita batch still exists only as the
// granularity of DATABASE writes (escreverLote), now decoupled from the progress
// reporting.
func (r *scanRun) processarLotes(ctx context.Context, arquivos []string) error {
	for i := 0; i < len(arquivos); i += loteEscrita {
		if err := ctx.Err(); err != nil {
			return err
		}
		fim := i + loteEscrita
		if fim > len(arquivos) {
			fim = len(arquivos)
		}
		lote := arquivos[i:fim]

		fatos := make([]fileFacts, 0, len(lote))
		for _, p := range lote {
			f, err := r.computarFacts(ctx, p)
			r.filesSeen++
			r.reportarProgresso()
			if err != nil {
				log.Printf("scanner: ignorando %q: %v", p, err)
				continue
			}
			fatos = append(fatos, f)
		}
		if err := r.escreverLote(ctx, fatos); err != nil {
			return err
		}
	}
	return nil
}

// computarFacts computes hash + mediainfo (or reuses them, see hashEMediainfo)
// and parses the names of a file.
func (r *scanRun) computarFacts(ctx context.Context, path string) (fileFacts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileFacts{}, err
	}
	mtime := info.ModTime().UTC().Format(time.RFC3339Nano)

	hash, mi, err := r.hashEMediainfo(ctx, path, info.Size(), mtime)
	if err != nil {
		return fileFacts{}, err
	}

	// It branches by library type: series follow the new flow (0034); movie (or
	// default) keeps the folder-based grouping path, intact. The series
	// resolution (and the auto-import) happens HERE, outside the write
	// transaction -- the enrich import writes through its own connection, so no
	// nested tx. This ALWAYS runs, even when hash/mediainfo were reused
	// (incremental scan): the entity resolution/link does not depend on the file
	// content, only on the name/folder -- and the re-scan of episode links needs
	// to run on every execution (see gravarArquivoSerie).
	var f fileFacts
	switch r.source.ContentType {
	case contentTypeSeries:
		f = r.factsSerie(ctx, path)
	case contentTypeMusic:
		// Music (0107) NEEDS the mediainfo (audio tags) in order to match --
		// unlike series (name/folder only), that is why it receives `mi` (and
		// size/mtime, for the incrementality short-circuit) here.
		f = r.factsMusica(ctx, path, mi, info.Size(), mtime)
	default:
		f = montarFacts(r.source.Path, path)
	}
	f.size = info.Size()
	f.mtime = mtime
	f.hash = hash
	f.mediainfo = mi
	return f, nil
}

// hashEMediainfo computes the content_hash (hashDoArquivoFn, indirect only for
// test instrumentation) and the mediainfo_json of `path`, OR reuses the values
// ALREADY STORED when the existing media_file for this SAME path (UNIQUE,
// ObterMediaFilePorPath) has EXACTLY the same mtime and size (incremental scan)
// — in that case it neither opens the file nor calls ffprobe again.
//
// This is a BEST-EFFORT HEURISTIC, not a cryptographic guarantee: a file could,
// in theory, change content without mtime OR size changing (e.g. some exotic
// editor that preserves both, or a synchronization window of `rclone
// --vfs-cache-mode full` where the local mtime only updates after the upload
// finishes — rclone/rclone#8894). Knowingly accepted: never worse than the old
// behavior that always re-hashed everything, and content hash as a fallback
// would cancel the performance gain that motivated this change — do not reopen
// this discussion. Any divergence (new file, or different mtime/size) falls into
// the normal path (full hash + ffprobe).
func (r *scanRun) hashEMediainfo(ctx context.Context, path string, size int64, mtime string) (hash string, mediainfo []byte, err error) {
	if existente, ok := r.mediaFileInalteradoPorPath(ctx, path, size, mtime); ok {
		return existente.ContentHash.String, []byte(existente.MediainfoJson.String), nil
	}
	return r.computarHashEMediainfo(ctx, path, size)
}

// computarHashEMediainfo ALWAYS computes the content_hash and the mediainfo_json
// of `path` (WITHOUT checking the mediaFileInalteradoPorPath short-circuit) —
// extracted from hashEMediainfo so it can also be reused by the forced recheck of
// specific paths (RecheckEntityFiles, see recheck.go), which needs to ignore
// mtime/size ON PURPOSE. The normal scan path (hashEMediainfo above) keeps
// calling this only after the short-circuit fails — no behavior change in the
// incremental scan of a whole datasource.
func (r *scanRun) computarHashEMediainfo(ctx context.Context, path string, size int64) (hash string, mediainfo []byte, err error) {
	hash, err = hashDoArquivoFn(path, size)
	if err != nil {
		return "", nil, err
	}
	if r.probeAtivo {
		if b, perr := r.m.prober.Probe(ctx, path); perr == nil {
			mediainfo = b
		} else {
			log.Printf("scanner: ffprobe falhou em %q (mediainfo vazio): %v", path, perr)
		}
	}
	return hash, mediainfo, nil
}

// mediaFileInalteradoPorPath fetches the media_file already stored for `path`
// (through the READ connection — computarFacts runs outside the write
// transaction) and returns ok=true only when it has a stored content_hash AND
// mtime AND size match EXACTLY the current values of the file on disk — the
// short-circuit of the incremental scan. Absent (new file at this path), without
// a stored hash (defensive) or any divergence in mtime/size: ok=false, normal
// path (full hash + ffprobe).
func (r *scanRun) mediaFileInalteradoPorPath(ctx context.Context, path string, size int64, mtime string) (sqlcgen.MediaFile, bool) {
	existente, err := sqlcgen.New(r.m.db.Read).ObterMediaFilePorPath(ctx, path)
	if err != nil {
		return sqlcgen.MediaFile{}, false
	}
	if !existente.ContentHash.Valid || existente.ContentHash.String == "" {
		return sqlcgen.MediaFile{}, false
	}
	if !existente.Size.Valid || existente.Size.Int64 != size {
		return sqlcgen.MediaFile{}, false
	}
	if !existente.Mtime.Valid || existente.Mtime.String != mtime {
		return sqlcgen.MediaFile{}, false
	}
	return existente, true
}

// montarFacts decides the grouping (folder = work; a loose file at the root =
// its own work) and parses the names.
func montarFacts(root, path string) fileFacts {
	dir := filepath.Dir(path)
	base := semExtensao(filepath.Base(path))

	f := fileFacts{path: path, arquivo: naming.Parse(base)}
	if dir != root {
		f.folderKey = dir
		f.folderDir = dir
		f.rawNome = filepath.Base(dir)
		f.entidade = naming.Parse(f.rawNome)
	} else {
		f.folderKey = path
		f.folderDir = root
		f.rawNome = base
		f.entidade = naming.Parse(base)
	}
	f.poster = descobrirPoster(f.folderDir, base)
	return f
}

// escreverLote writes a batch of files in a short transaction through the write
// connection (ADR-0010). Rollback on any error; the commit makes the batch
// durable.
func (r *scanRun) escreverLote(ctx context.Context, fatos []fileFacts) error {
	if len(fatos) == 0 {
		return nil
	}
	tx, err := r.m.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	q := sqlcgen.New(tx)
	for _, f := range fatos {
		if err := r.gravarArquivo(ctx, q, f); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// AFTER the commit (ADR-0031): the enrichment hooks (auto-match,
	// materialization) need the row visible to the read pool AND the write
	// connection free. Calling them inside the transaction is a bug -- it was the
	// real cause of the new item coming out raw in production (Nov 8, 2026).
	r.despacharNovos(ctx)
	return nil
}

// finalizarCicloVida closes the scan (source reachable): it stamps library_source
// (online + last_scan) and, only when the walk was COMPLETE (r.parcial == false),
// marks `missing` the files not reviewed in this source. In a partial walk
// (unreadable subfolder in the middle of the walk, see r.parcial), that marking
// is skipped on purpose -- "I did not see this file now" is not proof that it
// disappeared from disk when not even the whole tree could be read.
func (r *scanRun) finalizarCicloVida(ctx context.Context) error {
	q := sqlcgen.New(r.m.db.Write)
	if !r.parcial {
		if _, err := q.MarcarMediaFilesNaoVistosMissing(ctx, sqlcgen.MarcarMediaFilesNaoVistosMissingParams{
			LibrarySourceID: nsOpt(r.source.ID),
			LastSeen:        nsOpt(r.scanTS),
		}); err != nil {
			return err
		}
	}
	_, err := q.AtualizarLibrarySource(ctx, sqlcgen.AtualizarLibrarySourceParams{
		Type:     r.source.Type,
		LastScan: nsOpt(r.m.agora().Format(time.RFC3339)),
		State:    "online",
		ID:       r.source.ID,
	})
	return err
}

// persistirEstado writes the state of the datasource (used to signal offline),
// preserving the last known last_scan.
func (m *Manager) persistirEstado(ctx context.Context, source library.Source, state string) {
	var last string
	if source.LastScan != nil {
		last = *source.LastScan
	}
	q := sqlcgen.New(m.db.Write)
	if _, err := q.AtualizarLibrarySource(ctx, sqlcgen.AtualizarLibrarySourceParams{
		Type:     source.Type,
		LastScan: nsOpt(last),
		State:    state,
		ID:       source.ID,
	}); err != nil {
		log.Printf("scanner: falha ao persistir estado %q de %q: %v", state, source.ID, err)
	}
}

// arquivoBruto is a NON-video file found during coletarArquivos, pre-computed
// (path + size) for the managed/unmanaged classification -- without reopening the
// file afterwards.
type arquivoBruto struct {
	Path string
	Size int64
}

// extensoesPrincipais returns the set of extensions treated as the MAIN CONTENT
// of the datasource in this walk (0107): extensoesVideo for movie/series
// (default) or extensoesAudio when content_type='music'. coletarArquivos uses it
// to decide what feeds the matching pipeline (vs. what falls into naoVideos, only
// for the managed/unmanaged storage classification).
func (r *scanRun) extensoesPrincipais() map[string]bool {
	if r.source.ContentType == contentTypeMusic {
		return extensoesAudio
	}
	return extensoesVideo
}

// coletarArquivos walks the root A SINGLE TIME (WalkDir) and separates the MAIN
// CONTENT (`extensoesConteudo` -- video or audio, see extensoesPrincipais) from
// the rest (for the managed/unmanaged classification, see
// classificarNaoVideos) -- never two disk passes. An error in the stat of the
// root itself is fatal (offline source); unreadable internal directories are
// skipped (logged), they do not take the walk down -- but they make `parcial`
// return true, so the caller knows the file list may be incomplete (e.g. a
// transient timeout of a FUSE/rclone network mount in the middle of the walk) and
// does not treat "not seen" as "removed" when closing the lifecycle.
func coletarArquivos(root string, extensoesConteudo map[string]bool) (videos []string, naoVideos []arquivoBruto, parcial bool, err error) {
	if _, err := os.Stat(root); err != nil {
		return nil, nil, false, err
	}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("scanner: pulando %q: %v", path, err)
			parcial = true
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if pastasIgnoradas[strings.ToLower(d.Name())] {
				return fs.SkipDir
			}
			return nil
		}
		if ehArquivoIgnorado(d.Name()) {
			return nil
		}
		if extensoesConteudo[strings.ToLower(filepath.Ext(path))] {
			videos = append(videos, path)
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			log.Printf("scanner: nao foi possivel obter o tamanho de %q (ignorado na classificacao de armazenamento): %v", path, ierr)
			return nil
		}
		naoVideos = append(naoVideos, arquivoBruto{Path: path, Size: info.Size()})
		return nil
	})
	if walkErr != nil {
		return nil, nil, parcial, walkErr
	}
	sort.Strings(videos)
	return videos, naoVideos, parcial, nil
}

// classificarNaoVideos groups the videos by directory (base name without
// extension, see localmedia.NomeBaseVideo) and classifies each NON-video file
// found in the SAME walk as managed (the name matches a canonical naming pattern,
// see localmedia.EhArquivoGerenciado) or unmanaged (the rest) -- a sum of bytes
// per category. Pure (it does not touch the disk): the sizes already came from
// coletarArquivos.
func classificarNaoVideos(videos []string, naoVideos []arquivoBruto) (managedBytes, unmanagedBytes int64) {
	basesPorDir := make(map[string][]string, len(videos))
	for _, v := range videos {
		dir := filepath.Dir(v)
		basesPorDir[dir] = append(basesPorDir[dir], localmedia.NomeBaseVideo(v))
	}
	for _, nv := range naoVideos {
		dir := filepath.Dir(nv.Path)
		nome := filepath.Base(nv.Path)
		if localmedia.EhArquivoGerenciado(nome, basesPorDir[dir]) {
			managedBytes += nv.Size
		} else {
			unmanagedBytes += nv.Size
		}
	}
	return managedBytes, unmanagedBytes
}

// descobrirPoster looks for the local artwork: poster.jpg/folder.jpg in the
// directory of the work and <file-name>-poster.jpg (Kodi/Jellyfin convention).
// "" if none.
func descobrirPoster(dir, base string) string {
	candidatos := append([]string{}, nomesPoster...)
	candidatos = append(candidatos, base+"-poster.jpg", base+"-poster.png")
	for _, nome := range candidatos {
		p := filepath.Join(dir, nome)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// semExtensao removes the extension of a file name.
func semExtensao(nome string) string {
	return strings.TrimSuffix(nome, filepath.Ext(nome))
}

// hashDoArquivoFn is hashDoArquivo indirected through a variable (never
// overwritten in production): it exists only so the incremental scan TESTS can
// COUNT/instrument how many times the real hash runs, without having to rewrite
// the pipeline — the same spirit of m.agora/m.novoID being injectable function
// fields.
var hashDoArquivoFn = hashDoArquivo

// hashDoArquivo opens the file and computes the partial content_hash.
func hashDoArquivo(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return contentHash(f, size)
}

// posScan calls the post-scan hook (ADR-0031) at the end of executarScan:
// reconciliation of the drafts and retry of the pendings OF THAT library. It
// reports the stage as a progress message (the counter keeps counting FILES --
// the stage does not inflate files_total). Best-effort: an error only logs, it
// never takes down a scan that already wrote and stamped everything.
func (r *scanRun) posScan(ctx context.Context) {
	if r.m.posScan == nil {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	r.reportarProgressoMSG("reconciliando pendentes da biblioteca")
	if err := r.m.posScan.ProcessarPosScan(ctx, r.source.ID); err != nil {
		log.Printf("scanner: pós-scan da biblioteca %q falhou (scan concluído mesmo assim): %v", r.source.ID, err)
	}
}
