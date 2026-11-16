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
	"path/filepath"
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
	faixaPath, err := s.leitura().ObterCaminhoDescendentePresente(ctx, entityID)
	if err != nil {
		return false, nil // no owned descendant (defensive -- should not happen here)
	}

	var p string
	switch ent.Type {
	case tipoEntAlbum:
		p = faixaPath // filepath.Dir(faixaPath) = album folder
	case tipoEntArtista:
		p = filepath.Dir(faixaPath) // filepath.Dir(album folder) = artist folder
	default:
		return false, nil
	}

	return s.scanner.RefreshLocalAsset(ctx, entityID, []string{p})
}
