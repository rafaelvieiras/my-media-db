package scanner

// recheck.go implements two NEW and ISOLATED entry points of the whole-datasource
// scan (Manage menu > "Verificar Atualizações de Arquivos"): (1)
// RecheckEntityFiles forces the re-hash/mediainfo of an EXPLICIT LIST of paths
// (the media_file of ONE entity), deliberately ignoring the
// mediaFileInalteradoPorPath short-circuit (incremental scan) — a one-off action
// of the owner, not the periodic job; (2) RefreshLocalAsset rechecks the folder of
// each media_file of an ALREADY CATALOGED entity looking for a new/updated local
// poster, covering the gap that descobrirPoster only runs inside criarEntity
// (match.go) on the first walk of that folder. Neither of them does WalkDir nor
// touches the lifecycle (missing/last_scan) of the datasource -- Start/executarScan
// stay 100% incremental, with no behavior change.

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// RecheckEntityFiles forces the re-hash/mediainfo of each path in `paths` —
// media_file ALREADY KNOWN (by path) — even when mtime/size have not changed
// since the last time (it ON PURPOSE ignores the incremental scan
// short-circuit). `source` is the library_source these files came from (resolved
// by the CALLER, see internal/server/refresh_files.go — the same resolution
// pattern used in internal/server/retroativo.go), used only to know whether
// ffprobe is enabled and to stamp library_source_id/last_seen like the normal
// scan. A path that disappeared from disk OR whose media_file no longer exists
// (it disappeared between the caller's listing and this call) is ignored —
// best-effort, it is not an error. It returns how many paths were effectively
// re-hashed.
func (m *Manager) RecheckEntityFiles(ctx context.Context, source library.Source, paths []string) (int, error) {
	r := &scanRun{
		m:          m,
		source:     source,
		scanTS:     m.agora().Format(carimboFormato),
		probeAtivo: m.prober.Disponivel(),
	}
	rechecked := 0
	for _, p := range paths {
		ok, err := r.recheckPath(ctx, p)
		if err != nil {
			return rechecked, err
		}
		if ok {
			rechecked++
		}
	}
	return rechecked, nil
}

// recheckPath re-hashes ONE already known path (existing media_file), updating the
// row via AtualizarMediaFileConteudoMudou — the SAME query the normal scan uses
// when the content of an already-known path changes (atualizarConteudoMudou,
// match.go), reused here without duplicating the write. It preserves
// edition/part_no/match_confidence already stored — only the recheck of
// hash/mediainfo was requested, not a new match resolution. ok=false without an
// error when the path has no known media_file or disappeared from disk
// (best-effort).
func (r *scanRun) recheckPath(ctx context.Context, path string) (bool, error) {
	existente, err := sqlcgen.New(r.m.db.Read).ObterMediaFilePorPath(ctx, path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		log.Printf("scanner: recheck ignorando %q (sumiu do disco): %v", path, statErr)
		return false, nil
	}
	mtime := info.ModTime().UTC().Format(time.RFC3339Nano)

	hash, mediainfo, err := r.computarHashEMediainfo(ctx, path, info.Size())
	if err != nil {
		return false, err
	}

	_, err = sqlcgen.New(r.m.db.Write).AtualizarMediaFileConteudoMudou(ctx, sqlcgen.AtualizarMediaFileConteudoMudouParams{
		ContentHash:     nsOpt(hash),
		Size:            niInt(info.Size()),
		Mtime:           nsOpt(mtime),
		Edition:         existente.Edition,
		PartNo:          existente.PartNo,
		MatchConfidence: existente.MatchConfidence,
		MediainfoJson:   nsBytes(mediainfo),
		LastSeen:        nsOpt(r.scanTS),
		LibrarySourceID: existente.LibrarySourceID,
		ID:              existente.ID,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// RefreshLocalAsset rechecks the folder of each path in `paths` (the media_file of
// an ALREADY CATALOGED entity) looking for a local poster — the SAME criteria/file
// names of descobrirPoster (poster.jpg/folder.jpg in the directory of the work +
// <file-name>-poster.jpg/.png) — and makes an UPSERT of the asset kind=poster
// origin=local: it creates it if the entity does not have one yet; it updates
// path_or_uri if the local file found changed. It does NOT touch
// is_principal/precedence — it only guarantees that the origin='local' row reflects
// what is on disk. For the FIRST path whose folder has a local poster — the same
// "first wins" semantics that criarEntity (match.go) already uses on the first walk,
// here only reapplied to an entity that already existed. It returns found=true when
// an asset was created OR had its path_or_uri updated; found=false when it found
// nothing new (no local poster in any folder, or the existing asset already reflects
// the disk).
func (m *Manager) RefreshLocalAsset(ctx context.Context, entityID string, paths []string) (bool, error) {
	var encontrado string
	for _, p := range paths {
		dir := filepath.Dir(p)
		base := semExtensao(filepath.Base(p))
		if poster := descobrirPoster(dir, base); poster != "" {
			encontrado = poster
			break
		}
	}
	if encontrado == "" {
		return false, nil
	}

	leitura := sqlcgen.New(m.db.Read)
	existente, err := leitura.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: entityID,
		Kind:     "poster",
		Origin:   "local",
	})
	escrita := sqlcgen.New(m.db.Write)
	if err == nil {
		if existente.PathOrUri == encontrado {
			return false, nil // already reflects the disk, nothing to do
		}
		if err := escrita.AtualizarAssetPath(ctx, sqlcgen.AtualizarAssetPathParams{
			PathOrUri: encontrado,
			MetaJson:  existente.MetaJson, // preserves the `preferred` flag already stored
			ID:        existente.ID,
		}); err != nil {
			return false, err
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	if _, err := escrita.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID:        m.novoID(),
		EntityID:  entityID,
		Kind:      "poster",
		Origin:    "local",
		PathOrUri: encontrado,
	}); err != nil {
		return false, err
	}
	return true, nil
}
