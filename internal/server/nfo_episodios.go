package server

// nfo_episodios.go implements ONE .nfo per video FILE (issue #80).
//
// Kodi/Jellyfin name the episode NFO after the VIDEO and take the episode numbering from the
// FILE NAME: a file that holds several episodes (S02E01-E02-E03) is ONE file and therefore has
// ONE .nfo, with one <episodedetails> block per episode (localmedia.MontarNFOEpisodios, the
// format Kodi's own library export writes).
//
// Writing one NFO per ENTITY made the siblings overwrite each other: the file ended up with a
// mix (the title of one episode, the number another one had parsed out of the file name) and
// the convergence check could never be satisfied for more than one of them, so every run
// rewrote all 322 shared files -- 689 writes for 314 files per run on the production library,
// most of the tail of the job. With the document owned by the FILE, the first sibling that
// reaches it writes and the other two find it convergent.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
)

// documentoNFODeEpisodios assembles the destination path, the expected document and the group
// of episodes (with their CURRENT database row, for the old nfo_path) of the video file that
// holds `ent`. errSemDestino when the episode has no 'present' media_file: without the file
// there is no name to give the .nfo.
//
// The user fields of EACH episode are preserved from the block that declares the same
// (temporada, episodio); a legacy single-block file (written before this change, possibly with
// one episode's title and another one's number) is matched to the FIRST episode of the file --
// that is the closest thing to "whose state was in there" and it is exactly what the old code
// would have preserved.
func (s *Server) documentoNFODeEpisodios(ctx context.Context, ent sqlcgen.Entity) (destino string, corpo []byte, irmaos []sqlcgen.Entity, err error) {
	mf, err := s.leitura().ObterMediaFileDoEpisodio(ctx, ent.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, nil, errSemDestino
		}
		return "", nil, nil, err
	}
	linhas, err := s.leitura().ListarEpisodiosDoMediaFile(ctx, mf.ID)
	if err != nil {
		return "", nil, nil, err
	}
	if len(linhas) == 0 {
		return "", nil, nil, errSemDestino
	}

	destino = localmedia.CaminhoNFODeVideo(mf.Path)
	existente, _ := os.ReadFile(destino)
	blocosExistentes := localmedia.ExtrairBlocosDeEpisodio(existente)
	legado := camposDoArquivoLegado(blocosExistentes, linhas)

	// Third tier of the numbering: the VIDEO FILE NAME states SxxEyy, and the episodes the
	// tree cannot number are exactly the ones that lost the tag in a previous write. Without
	// this tier the document and the damaged file agree on "no number", so the damage is
	// permanent -- the file converges with a document that says nothing.
	//
	// Only for a file that holds ONE episode: with several, the name states the FIRST number
	// and offsetting by index would be a guess that a wrong NFO does not survive.
	temporadaArquivo, episodioArquivo := 0, 0
	if len(linhas) == 1 {
		if info, ok := naming.ParseEpisode(filepath.Base(mf.Path)); ok && info.Season > 0 && len(info.Episodes) == 1 {
			temporadaArquivo, episodioArquivo = info.Season, info.Episodes[0]
		}
	}

	blocos := make([]localmedia.BlocoEpisodioNFO, 0, len(linhas))
	for indice, linha := range linhas {
		irmao, erro := s.leitura().ObterEntity(ctx, linha.ID)
		if erro != nil {
			return "", nil, nil, erro
		}
		resolvida, encontrada, erro := s.resolverEntity(ctx, linha.ID, "", false)
		if erro != nil {
			return "", nil, nil, erro
		}
		if !encontrada {
			continue // defensive: a row of the junction without an entity cannot happen
		}
		temporada, episodio := numeracaoDoBloco(linha, blocosExistentes, indice, temporadaArquivo, episodioArquivo)
		blocos = append(blocos, localmedia.BlocoEpisodioNFO{
			Entidade:  resolvida,
			Temporada: temporada,
			Episodio:  episodio,
			Usuario:   camposDoBloco(blocosExistentes, temporada, episodio, indice, legado),
		})
		irmaos = append(irmaos, irmao)
	}
	if len(blocos) == 0 {
		return "", nil, nil, errSemDestino
	}

	corpo, err = localmedia.MontarNFOEpisodios(blocos)
	if err != nil {
		return "", nil, nil, err
	}
	return destino, corpo, irmaos, nil
}

// numeracaoDoBloco answers "which season/episode number goes into this block": the one in the model
// (ADR-0021: entity_relation.sort_index) when it EXISTS, and otherwise the one the file already
// declares at that position.
//
// The second half is not a detail. 430 episodes of the owner's library carry no season/episode at
// all in the tree (both 0), and the old code already wrote those files: their .nfo has real
// numbers, put there by whichever manager indexed them. Omitting the tags "because the model does
// not know" would make every one of those files diverge from the document -- the job would rewrite
// 360 files on every single run, forever, to say nothing new. Taking the number the file has makes
// the document converge and changes nothing the media server reads.
func numeracaoDoBloco(linha sqlcgen.ListarEpisodiosDoMediaFileRow, existentes []localmedia.BlocoUsuarioNFO, indice, temporadaArquivo, episodioArquivo int) (temporada, episodio int) {
	// 1. The tree (provider data) always wins.
	if linha.Temporada > 0 && linha.Episodio > 0 {
		return int(linha.Temporada), int(linha.Episodio)
	}
	// 2. What the file already declares at this block stays (nobody loses a number a previous
	// manager wrote, and the damaged files from the regression of v0.15.4 are exactly the ones
	// that declare nothing).
	if indice < len(existentes) {
		t, e := existentes[indice].Temporada, existentes[indice].Episodio
		if t > 0 || e > 0 {
			return t, e
		}
	}
	// 3. The file name (single-episode file only, see documentoNFODeEpisodios): this is what
	// RECOVERS the tag that a previous run dropped, and what Kodi reads anyway.
	if temporadaArquivo > 0 && episodioArquivo > 0 {
		return temporadaArquivo, episodioArquivo
	}
	return 0, 0
}

// camposDoBloco picks the preserved user fields of one episode of the file: the block that
// declares the same numbering. A SINGLE block without any numbering (a file written by a manager
// that did not number the episodes) is matched to the FIRST episode of the file -- the closest
// thing to "whose state was in there", and exactly what the old code preserved. A single block
// WITH numbering is matched by it: a hand-edited file that kept only one episode's block must not
// hand that episode's watch count to its neighbour.
func camposDoBloco(existentes []localmedia.BlocoUsuarioNFO, temporada, episodio, indice int, legado *localmedia.CamposUsuarioNFO) localmedia.CamposUsuarioNFO {
	for _, bloco := range existentes {
		if bloco.Temporada == temporada && bloco.Episodio == episodio {
			return bloco.Campos
		}
	}
	if legado != nil && indice == 0 {
		return *legado
	}
	return localmedia.CamposUsuarioNFO{}
}

// camposDoArquivoLegado answers "the existing file carries ONE block whose numbering belongs to no
// episode of the file?" -- a legacy single-block .nfo, whose <episode> came from the FILE NAME and
// may point at nothing. Such a block belongs to the FIRST episode of the file: that is whose state
// the old code preserved, and throwing it away would silently reset the watch count of files the
// owner has been watching for years.
func camposDoArquivoLegado(existentes []localmedia.BlocoUsuarioNFO, linhas []sqlcgen.ListarEpisodiosDoMediaFileRow) *localmedia.CamposUsuarioNFO {
	if len(existentes) != 1 {
		return nil
	}
	for _, linha := range linhas {
		if int(linha.Temporada) == existentes[0].Temporada && int(linha.Episodio) == existentes[0].Episodio {
			return nil // the block DOES belong to an episode of the file: matched by numbering
		}
	}
	campos := existentes[0].Campos
	return &campos
}

// gravarNFOEpisodios writes the document of the FILE that holds `ent` UNCONDITIONALLY
// (idempotent by content, the same contract as gravarNFO) and stamps EVERY episode of the file,
// because all of them share the one .nfo -- nfo_path/nfo_written_at describe the file, not the
// entity. The legacy .nfo of a sibling is removed when its destination moved.
func (s *Server) gravarNFOEpisodios(ctx context.Context, ent sqlcgen.Entity, grupos map[string][]artworkItem) (nfoWriteResult, error) {
	destino, corpo, irmaos, err := s.documentoNFODeEpisodios(ctx, ent)
	if err != nil {
		return nfoWriteResult{}, err
	}

	// Artwork of the CURRENT episode only (episode stills are per episode, not per file):
	// best-effort, exactly like gravarNFOComGrupos.
	if _, err := s.materializarKindVencedor(ctx, ent, kindPoster, grupos); err != nil {
		log.Printf("gravarNFO: falha ao materializar poster da entity %q (best-effort, ignorado): %v", ent.ID, err)
	}
	if _, err := s.materializarKindVencedor(ctx, ent, kindFanart, grupos); err != nil {
		log.Printf("gravarNFO: falha ao materializar fanart da entity %q (best-effort, ignorado): %v", ent.ID, err)
	}

	if err := localmedia.EscreverArquivoAtomico(destino, corpo); err != nil {
		return nfoWriteResult{}, err
	}
	agora := nowISO()
	escrita := sqlcgen.New(s.db.Write)
	for _, irmao := range irmaos {
		if err := escrita.AtualizarEntityNfo(ctx, sqlcgen.AtualizarEntityNfoParams{
			NfoPath:      sql.NullString{String: destino, Valid: true},
			NfoWrittenAt: sql.NullString{String: agora, Valid: true},
			ID:           irmao.ID,
		}); err != nil {
			return nfoWriteResult{}, err
		}
		removerNfoOrfaoSeMudou(irmao.NfoPath.Valid && irmao.NfoPath.String != "", irmao.NfoPath.String, destino)
	}
	return nfoWriteResult{NfoPath: destino, NfoWrittenAt: agora}, nil
}

// nfoDeEpisodiosConvergente answers "is the file's .nfo already exactly what we would write?"
// byte by byte. "No destination" is not an error: it is a no-op (the caller reports false).
func (s *Server) nfoDeEpisodiosConvergente(ctx context.Context, ent sqlcgen.Entity) (bool, error) {
	destino, corpo, _, err := s.documentoNFODeEpisodios(ctx, ent)
	if err != nil {
		if errors.Is(err, errSemDestino) {
			return false, nil
		}
		return false, err
	}
	existente, err := os.ReadFile(destino)
	if err != nil {
		return false, nil // vanished or unreadable: rewrite from scratch
	}
	if bytes.Equal(existente, corpo) {
		return true, nil
	}
	// The same document has more than one valid LAYOUT: an .nfo written before the numbered
	// fields existed keeps <season>/<episode> among the unknown tags, wherever the manager
	// that wrote it put them. Comparing the canonical form is what keeps the first run after
	// this change from rewriting the ~12 thousand single-episode files -- while a file whose
	// numbering or block count really differs still does not converge.
	canonico, err := localmedia.CanonicalizarNFOEpisodios(existente)
	if err != nil {
		return false, nil // not a document we can read: rewrite
	}
	return bytes.Equal(canonico, corpo), nil
}
