package server

// arte_local_musica.go implements LOCAL ARTWORK DISCOVERY for music artist/album:
// the same mechanism movie/series already have (poster.jpg/folder.jpg in the work's
// folder, scanner.descobrirPoster/RefreshLocalAsset, asset origin='local') had never
// been wired into the music vertical -- artist/album NEVER have a media_file
// DIRECTLY (ADR-0027, only the track links), so the "work's" folder has to be
// DERIVED from the file of a descendant track: the ALBUM folder is the directory of
// the track file; the ARTIST folder is one level up (the parent of the album
// folder). Each artwork source request now has THREE possible sources, all of them
// complementing each other (none of them exclusive) -- local (this one),
// MusicBrainz/Cover Art Archive and fanart.tv (enriquecerArtes) -- and origin='local'
// WINS over origin='external' in the serving precedence (escolherAssetServido,
// assets.go): an image the owner already has in the folder is treated as his
// decision, not clobbered by any provider.

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// DescobrirArteLocalMusica sweeps EVERY artist/album entity with at least one owned
// descendant track (ListarEntitiesMusicaComFaixaPossuida, the same universe as the
// midias_faltantes job) and rechecks the corresponding folder looking for a local
// poster (scanner.RefreshLocalAsset, the same mechanism as movie/series).
// Idempotent (RefreshLocalAsset does an UPSERT by (entity,kind,origin)) and
// best-effort: a one-off failure on an entity is logged and skipped, never aborts
// the batch. err only propagates on a real LISTING failure (database down).
func (s *Server) DescobrirArteLocalMusica(ctx context.Context) (encontrados int, err error) {
	if s.scanner == nil {
		return 0, nil
	}
	ids, err := s.leitura().ListarEntitiesMusicaComFaixaPossuida(ctx)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		ok, err := s.descobrirArteLocalMusicaDe(ctx, id)
		if err != nil {
			log.Printf("arte local musica: falha ao verificar a entity %q (best-effort, ignorado): %v", id, err)
			continue
		}
		if ok {
			encontrados++
		}
	}
	return encontrados, nil
}

// descobrirArteLocalMusicaDe resolves the canonical folder of ONE entity
// (artist/album) from the path of any descendant track
// (ObterCaminhoDescendentePresente) and delegates to scanner.RefreshLocalAsset --
// which already knows how to look for poster.jpg/folder.jpg in the folder and do the
// UPSERT of the origin='local' asset. `p` is built so that filepath.Dir(p) resolves
// exactly to the right folder: for an album, the path OF THE TRACK (its dir already
// IS the album folder); for an artist, the path of the ALBUM FOLDER (one level above
// it is the artist folder) -- RefreshLocalAsset always applies filepath.Dir(p)
// internally, so "p" here is only a vector to reach the right folder, never a real
// file that has to exist.
func (s *Server) descobrirArteLocalMusicaDe(ctx context.Context, entityID string) (bool, error) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return false, err
	}
	dir, faixaPath, ok := s.pastaDeMusica(ctx, ent)
	if !ok {
		return false, nil // no owned descendant, or the folder is not on disk
	}
	// RefreshLocalAsset always applies filepath.Dir(p) internally, so p here is only a
	// vector to reach the right folder -- keeping the track's file name preserves the
	// "<video>-poster" variants descobrirPoster also looks for.
	return s.scanner.RefreshLocalAsset(ctx, entityID, []string{filepath.Join(dir, filepath.Base(faixaPath))})
}

// ehMusica answers "is this entity part of the music vertical?" for the album/artist
// pair -- the two types whose media_file only exists on the descendant TRACK (ADR-0027),
// which is what makes their artwork destination (pastaDeMusica) and their file names
// (NomeArquivoArteMusica: folder.jpg, cdart.png...) different from the video ones (issue
// #79). tipoEntFaixa is deliberately NOT here: a track has its own media_file, so it
// follows the video rules.
func ehMusica(tipo string) bool {
	return tipo == tipoEntAlbum || tipo == tipoEntArtista
}

// pastaDeMusica resolves the folder that receives the artwork of an album or an artist,
// plus the track path it was derived from: the directory of a descendant track file (album)
// or its PARENT (artist) -- the SAME rule the local-artwork discovery has always used, now
// shared with the write side (issue #79).
//
// ok=false when: the entity has no owned descendant track (artist/album never have a
// media_file of their own, ADR-0027); the folder is not on disk right now (remote mount
// offline) -- writing would fail, and creating folders in the library is not our call; or,
// for an ARTIST, when the album folder IS a library root, i.e. the tracks are not inside an
// album subfolder and going up one level would drop the artist picture in the root of the
// library. A missing destination is "nothing to do", never an error.
func (s *Server) pastaDeMusica(ctx context.Context, ent sqlcgen.Entity) (dir, faixaPath string, ok bool) {
	faixaPath, err := s.leitura().ObterCaminhoDescendentePresente(ctx, ent.ID)
	if err != nil || faixaPath == "" {
		return "", "", false // sql.ErrNoRows: no owned track (defensive)
	}
	switch ent.Type {
	case tipoEntAlbum:
		// A multi-disc release keeps its tracks in "CD 1"/"Disc 2"/"Digital Media 01"
		// subfolders: the cover belongs to the ALBUM, one level up -- the same shape of rule
		// the series root already has for "Season NN" (subirEnquantoPastaDeTemporada).
		albumDir := subirEnquantoPastaDeDisco(filepath.Dir(faixaPath))
		if !diretorioExiste(albumDir) {
			return "", "", false
		}
		return albumDir, faixaPath, true
	case tipoEntArtista:
		albumDir := subirEnquantoPastaDeDisco(filepath.Dir(faixaPath))
		raizes, err := s.raizesConfiaveis(ctx)
		if err != nil {
			return "", "", false
		}
		if ehRaizDeBiblioteca(raizes, albumDir) {
			return "", "", false
		}
		// No extra check of the parent: it exists by construction when the album folder does,
		// and going up can only reach a real folder.
		return filepath.Dir(albumDir), faixaPath, true
	}
	return "", "", false
}

// regexPastaDeDisco matches the subfolder names a multi-disc release uses for one disc
// ("CD 1", "Disc 02", "Disk 3", "Disco 1", "Digital Media 01") -- see
// subirEnquantoPastaDeDisco.
var regexPastaDeDisco = regexp.MustCompile(`(?i)^(cd|disc|disk|disco|digital media)\s*\d+$`)

// subirEnquantoPastaDeDisco goes up one level while the folder name still looks like a DISC
// subfolder of a multi-disc release, so the cover of the album lands on the album itself
// instead of being duplicated inside "CD 1" and "CD 2". It mirrors
// localmedia.subirEnquantoPastaDeTemporada (the same idea for "Season NN").
func subirEnquantoPastaDeDisco(dir string) string {
	for dir != "" && regexPastaDeDisco.MatchString(filepath.Base(dir)) {
		pai := filepath.Dir(dir)
		if pai == dir {
			break // filesystem root: nothing left to go up (defensive)
		}
		dir = pai
	}
	return dir
}

// diretorioExiste reports whether `dir` is a directory ON DISK right now.
func diretorioExiste(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// ehRaizDeBiblioteca reports whether `dir` is one of the library roots (a configured
// library_source path or the assets dir).
func ehRaizDeBiblioteca(raizes []string, dir string) bool {
	limpo := filepath.Clean(dir)
	for _, raiz := range raizes {
		if raiz != "" && filepath.Clean(raiz) == limpo {
			return true
		}
	}
	return false
}
