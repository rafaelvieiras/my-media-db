package server

// midias_faltantes.go implements the "on demand" materialization reused by the
// midias_faltantes job: for ONE entity, it physically materializes in the REAL
// library folder the artwork WINNING TODAY (same aggregator as the gallery,
// montarGruposArtwork/artwork.go) of each relevant kind, and the .nfo when absent (same
// gravarNFO function of localmedia_handlers.go). It never chooses a candidate
// different from the one the UI would already show as top -- it only applies to disk what
// is already the current choice (explicitly OUT of scope:
// auto-selecting when NOTHING has been resolved yet).

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// kindsMidiasFaltantes are the WORK-LEVEL artwork kinds that the job
// materializes ("poster, fanart, banner, clearlogo, discart, landscape" -- the last
// three use the FILE NAMING of internal/localmedia
// (KindClearlogo/KindDiscart/KindLandscape), not the asset.kind vocabulary stored in the
// database and grouped by montarGruposArtwork (assets.go/fanarttv.go: poster, fanart,
// banner, logo, clearart, disc, thumb, characterart -- "clearlogo"/"discart"/"landscape"
// NEVER appear as a key of that aggregator). Implementation decision (not
// 100% explicit in the docs, resolving this vocabulary divergence):
// we use here the REAL vocabulary of the aggregator, mapping by intent --
// poster, fanart, banner, logo (equivalent of "clearlogo" in the real folder),
// disc (equivalent of "discart") and thumb (16:9 series/movie-level artwork,
// the closest equivalent of "landscape" in the existing vocabulary:
// kindsTV maps "tvthumb"->"thumb" in fanart.tv). `still` stays out --
// it is ALWAYS per EPISODE, outside the scope of this work-level job.
var kindsMidiasFaltantes = []string{kindPoster, kindFanart, kindBanner, kindLogo, kindDisc, kindThumb}

// ListarEntitiesComMediaFilePresente returns the ids of EVERY entity eligible
// for the midias_faltantes job: movie/tv with a direct 'present' media_file
// (ListarEntityIDsComMediaFilePresente -- media_file.entity_id points at the SERIES,
// never season/episode, see internal/scanner/serie.go gravarArquivoSerie), PLUS season
// with at least one present descendant episode (ListarSeasonIDsComEpisodioPresente -- it
// enables the season poster) PLUS episode with a present media_file via the
// media_file_entity join (ListarEpisodeIDsComMediaFilePresente -- it enables the episode
// NFO) PLUS music artist/album with at least one present descendant TRACK
// (ListarEntitiesMusicaComFaixaPossuida -- the media_file.entity_id of music only points
// at the track, ADR-0027; without this the job never reached artist/album, so album cover
// and artist picture were never materialized even with the auto-download toggle on).
// Season appearing here does NOT contradict the gravarNFOSeFaltante guard (season has no
// NFO of its own) -- it only starts materializing season-level artwork.
// ContarMediaFilesEmEstadoMissing returns how many media_file rows are in the 'missing'
// state at the time of the call -- a thin wrapper over the sqlc query, consumed by the
// midias_faltantes job summary to make visible a sign of instability of the remote mount
// that today does not appear anywhere.
func (s *Server) ContarMediaFilesEmEstadoMissing(ctx context.Context) (int64, error) {
	return s.leitura().ContarMediaFilesEmEstadoMissing(ctx)
}

// TituloDaEntity returns the title of ONE entity, by id -- the narration label of
// the midias_faltantes job (midiasfaltantes.Titulador, issue #69). A dedicated
// title-only read instead of ObterEntity: this is called once per entity in the
// loop, and pulling description/external ids/date columns just to fill a label
// would be waste.
func (s *Server) TituloDaEntity(ctx context.Context, entityID string) (string, error) {
	return s.leitura().ObterTituloEntity(ctx, entityID)
}

func (s *Server) ListarEntitiesComMediaFilePresente(ctx context.Context) ([]string, error) {
	diretos, err := s.leitura().ListarEntityIDsComMediaFilePresente(ctx)
	if err != nil {
		return nil, err
	}
	seasons, err := s.leitura().ListarSeasonIDsComEpisodioPresente(ctx)
	if err != nil {
		return nil, err
	}
	episodios, err := s.leitura().ListarEpisodeIDsComMediaFilePresente(ctx)
	if err != nil {
		return nil, err
	}
	musica, err := s.leitura().ListarEntitiesMusicaComFaixaPossuida(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(diretos)+len(seasons)+len(episodios)+len(musica))
	ids = append(ids, diretos...)
	ids = append(ids, seasons...)
	ids = append(ids, episodios...)
	ids = append(ids, musica...)
	return ids, nil
}

// MaterializarMidiasFaltantes materializes, for ONE entity, the winning artwork
// (montarGruposArtwork aggregator) of each kind of kindsMidiasFaltantes still
// without local_path, and the NFO when entity.nfo_path is null -- the body of the
// midias_faltantes job, also callable in isolation (non-HTTP, that is what
// internal/job/midiasfaltantes.Materializador consumes). An error only
// propagates on a real READ failure of the entity (database down); punctual failures of
// download/materialization of ONE kind or of the NFO are logged and
// skipped (best-effort -- same spirit as the rest of the job system),
// they never abort the others.
func (s *Server) MaterializarMidiasFaltantes(ctx context.Context, entityID string) (artesBaixadas []string, nfoGravado bool, err error) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return nil, false, err
	}

	// ONE aggregation for the whole entity (issue #73). The kind loop used to call
	// montarGruposArtwork itself, so the same three queries plus the same JSON parse ran
	// six times per entity and five of the results were thrown away.
	grupos := s.montarGruposArtwork(ctx, ent.ID)

	// ONE destination resolution per entity, BEFORE any download (issue #79): an entity
	// without a destination folder cannot materialize anything, and paying for the download
	// before discovering that was 301 network fetches per run that wrote nothing and were
	// reported as success. "No destination" is a legitimate no-op (an entity whose only
	// media_file vanished, music without an owned track): false without an error.
	if _, _, _, ok, derr := s.resolverDestino(ctx, ent); derr != nil {
		return nil, false, derr
	} else if !ok {
		return nil, false, nil
	}

	// materializarPosterDeArtista runs BEFORE the kindsMidiasFaltantes loop on purpose:
	// it uses the NOT-YET-MATERIALIZED winner of the `thumb` kind (with SrcURL, to
	// download) -- if it ran later, the loop below itself (kindsMidiasFaltantes includes
	// "thumb") would already have materialized the thumb as a LOCAL asset without SrcURL,
	// and the alias would find nothing to download.
	if ok, kerr := s.materializarPosterDeArtista(ctx, ent, grupos); kerr != nil {
		log.Printf("midias_faltantes: falha ao materializar o poster da entity artista %q (best-effort, ignorado): %v", entityID, kerr)
	} else if ok {
		artesBaixadas = append(artesBaixadas, kindPoster)
	}

	for _, kind := range kindsMidiasFaltantes {
		if kind == kindPoster && ent.Type == tipoEntArtista {
			continue // already handled by materializarPosterDeArtista above (thumb->poster alias) -- reprocessing here would count the same artwork twice in artesBaixadas
		}
		if kind == kindThumb && ent.Type == tipoEntArtista {
			continue // the artist picture IS the thumb: the alias above already wrote it as the poster (folder.jpg); writing the thumb too would put the same image in the folder twice under two names
		}
		ok, kerr := s.materializarKindVencedor(ctx, ent, kind, grupos)
		if kerr != nil {
			log.Printf("midias_faltantes: falha ao materializar o kind %q da entity %q (best-effort, ignorado): %v", kind, entityID, kerr)
			continue
		}
		if ok {
			artesBaixadas = append(artesBaixadas, kind)
		}
	}

	nfoGravado, nerr := s.gravarNFOSeFaltante(ctx, ent, grupos)
	if nerr != nil {
		log.Printf("midias_faltantes: falha ao gravar o NFO da entity %q (best-effort, ignorado): %v", entityID, nerr)
	}
	return artesBaixadas, nfoGravado, nil
}

// materializarKindVencedor takes the FIRST item of the aggregator
// (montarGruposArtwork, SAME precedence as the gallery --
// ordenarGruposArtwork already sorts is_principal desc, is_local_in_library desc)
// for `kind` and materializes it physically in the real folder when it was still missing --
// it never chooses a candidate different from the one the UI would already show as top.
// It returns ok=false without an error when there is no candidate for the kind OR the
// winner is already materialized (local_path filled) -- both "nothing to
// do", not a failure.
func (s *Server) materializarKindVencedor(ctx context.Context, ent sqlcgen.Entity, kind string, grupos map[string][]artworkItem) (bool, error) {
	candidatos := grupos[kind]
	if len(candidatos) == 0 {
		return false, nil
	}
	vencedor := candidatos[0]

	if vencedor.IsLocal {
		asset, err := s.leitura().ObterAsset(ctx, vencedor.AssetID)
		if err != nil {
			return false, err
		}
		if vencedor.IsLocalInLibrary {
			return s.reconfirmarAssetLocalNaLibrary(ctx, ent, asset)
		}
		return s.materializarAssetOuCache(ctx, ent, asset)
	}

	if vencedor.SrcURL == "" {
		return false, nil // no known origin URL -- nothing to download
	}
	asset, err := s.baixarEMaterializarArte(ctx, ent.ID, vencedor.SrcURL, kind)
	if err != nil {
		if errors.Is(err, errBaixarArte) {
			// DOWNLOAD failure (network/status/type) -- best-effort, it is not an
			// infrastructure error (same treatment as baixarMelhorArteDoKind).
			return false, nil
		}
		return false, err
	}
	return s.materializarAssetOuCache(ctx, ent, asset)
}

// reconfirmarAssetLocalNaLibrary handles the case "the database says the winner is
// already materialized in the real folder" (vencedor.IsLocalInLibrary) -- extracted
// from materializarKindVencedor to keep the cyclomatic complexity low.
// When the asset already IS the principal of the kind, it delegates to
// convergirNomeCanonico (the DIRECTORY + name may have changed since the
// materialization, e.g.: a legacy local_path pointing into "Season 01" instead of the
// series root) -- it persists the new local_path when the destination changes. It
// deliberately does NOT reuse renomearParaBare (used by PUT .../prefer): that function
// preserves the current DIRECTORY of the asset by design (it only resolves
// bare-vs-rotated inside the already correct folder) -- here the directory itself may be
// wrong. A NON-principal local asset (edge case: no asset of the kind is principal)
// only gets the simple self-heal (rewrite in place if the
// file disappeared) -- recalculating the bare name for it would collide with the name the
// real principal should occupy.
func (s *Server) reconfirmarAssetLocalNaLibrary(ctx context.Context, ent sqlcgen.Entity, asset sqlcgen.Asset) (bool, error) {
	if asset.IsPrincipal != 0 {
		haviaSumido := !arquivoLocalAindaExiste(asset.LocalPath.String)
		destino, err := s.convergirNomeCanonico(ctx, ent, asset)
		if err != nil {
			return false, err
		}
		if destino == asset.LocalPath.String {
			return haviaSumido, nil // no name drift; convergirNomeCanonico already covers the self-heal (rewritten in place if the file had disappeared)
		}
		if err := sqlcgen.New(s.db.Write).AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
			LocalPath: sql.NullString{String: destino, Valid: true},
			ID:        asset.ID,
		}); err != nil {
			return false, err
		}
		return true, nil // convergence: file renamed to the current canonical destination
	}

	// The database column says "already materialized", but the file
	// may have physically disappeared (remote rclone/gdrive mount, external
	// deletion) -- recheck via os.Stat before skipping.
	if arquivoLocalAindaExiste(asset.LocalPath.String) {
		return false, nil // already materialized, nothing to do
	}
	if _, err := s.regravarBinarioAssetNoLocalPathExistente(ctx, asset); err != nil {
		return false, err
	}
	return true, nil // self-heal: rewritten at the same local_path
}

// convergirNomeCanonico recalculates the CURRENT canonical destination (DIRECTORY + bare
// name) of an already local asset via resolverDestino, and RENAMES (rename, never
// re-copies bytes) the physical file if it diverges from the registered local_path.
// Unlike renomearParaBare
// (localmedia_handlers.go, used by PUT .../prefer): that function only
// resolves bare-vs-rotated KEEPING the current directory (`filepath.Dir`)
// by design, because in the principal-swap flow the asset is already in the right
// folder; here the DIRECTORY itself may be wrong (legacy naming,
// e.g.: an asset written inside "Season 01" when it should
// be at the series root) -- recalculating only the name without recalculating the
// directory would NOT close that gap. When the recalculated destination is equal to the
// current one, it falls into the SAME self-heal (rewrites in place if the
// file has disappeared from disk).
func (s *Server) convergirNomeCanonico(ctx context.Context, ent sqlcgen.Entity, alvo sqlcgen.Asset) (string, error) {
	dir, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errSemDestino
	}
	ext := filepath.Ext(alvo.LocalPath.String)
	kind, nomeBase := s.kindNomeEBase(ctx, ent, alvo.Kind, nomeBaseVideo)
	nome := localmedia.NomeArquivoArte(kind, true, 0, pastaExclusiva, nomeBase, ext)
	if ehMusica(ent.Type) {
		nome = localmedia.NomeArquivoArteMusica(alvo.Kind, true, 0, ext)
	}
	destino := filepath.Join(dir, nome)
	if destino == alvo.LocalPath.String {
		if arquivoLocalAindaExiste(alvo.LocalPath.String) {
			return destino, nil
		}
		if _, err := s.regravarBinarioAssetNoLocalPathExistente(ctx, alvo); err != nil {
			return "", err
		}
		return destino, nil
	}
	if err := localmedia.RenomearArquivo(alvo.LocalPath.String, destino); err != nil {
		return "", err
	}
	return destino, nil
}

// materializarAssetOuCache calls materializarAsset (writes to the REAL library folder).
//
// "No destination" (errSemDestino) is NOT success (issue #79): it means nothing was written,
// and reporting it as materialized made the job print "301 arte(s) materializada(s)" on every
// run while not a single image reached the library -- and kept re-downloading the artwork every
// run, because the asset local_path stayed empty. The callers now resolve the destination BEFORE
// downloading (MaterializarMidiasFaltantes), so errSemDestino here means a transient condition
// (the folder vanished between the two calls) and the honest answer is false, not true. The
// asset binary stays in the internal CACHE (baixarEMaterializarArte wrote it there), which is
// all has_poster/the card need.
func (s *Server) materializarAssetOuCache(ctx context.Context, ent sqlcgen.Entity, asset sqlcgen.Asset) (bool, error) {
	if _, err := s.materializarAsset(ctx, ent, asset); err != nil {
		if errors.Is(err, errSemDestino) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// materializarPosterDeArtista materializes the "poster" of an artist entity from the
// winner of the `thumb` kind: fanart.tv does not model a poster kind of its own
// for a music artist (kindsArtista, internal/provider/fanarttv/fanarttv.go, only
// has thumb/fanart/logo/banner), but the card/has_poster
// (internal/db/queries/entity.sql) only sees kind='poster' -- without this step, an artist
// would NEVER have has_poster=true even with a picture available in the inventory. No-op
// (false, nil) for any type != artista, if there is already something in the `poster` slot
// (materialized or not -- the normal loop of kindsMidiasFaltantes takes care of the rest)
// or if there is no candidate/origin URL in the `thumb` kind. It always downloads (it never
// reuses a local asset from ANOTHER kind): the already materialized thumb cannot become a
// poster just by swapping the database row, because materializarAsset names the file in
// the real folder by the kind.
func (s *Server) materializarPosterDeArtista(ctx context.Context, ent sqlcgen.Entity, grupos map[string][]artworkItem) (bool, error) {
	if ent.Type != tipoEntArtista {
		return false, nil
	}
	if len(grupos[kindPoster]) > 0 {
		return false, nil // it already has a poster (the normal kindsMidiasFaltantes loop takes care of the rest)
	}
	candidatos := grupos[kindThumb]
	if len(candidatos) == 0 || candidatos[0].SrcURL == "" {
		return false, nil // no thumb candidate, or the winner is a local asset (no SrcURL)
	}

	asset, err := s.baixarEMaterializarArte(ctx, ent.ID, candidatos[0].SrcURL, kindPoster)
	if err != nil {
		if errors.Is(err, errBaixarArte) {
			return false, nil
		}
		return false, err
	}
	return s.materializarAssetOuCache(ctx, ent, asset)
}

// MaterializarSeAutoDownloadLigado is the adapter that satisfies
// scanner.Materializador: called by the scanner right after
// ANY successful auto-import (movie via TentarAutoMatchFilme, series
// via ImportarSeriePorNome). Toggle off (default) ⇒ no-op, without touching the
// disk -- only when the owner turned on "materialize artwork automatically" in
// Settings does the materialization actually run, reusing
// MaterializarMidiasFaltantes without new logic. err only propagates on a real
// failure reading the config/entity; punctual download failures are already
// absorbed (best-effort) inside MaterializarMidiasFaltantes.
func (s *Server) MaterializarSeAutoDownloadLigado(ctx context.Context, entityID string) error {
	s.dispararTrailerTemaAutomaticoSeLigado(ctx, entityID)

	cfg, err := s.obterAutoMaterializeArtworkSettings(ctx)
	if err != nil || !cfg.Enabled {
		return err
	}
	_, _, err = s.MaterializarMidiasFaltantes(ctx, entityID)
	return err
}

// dispararTrailerTemaAutomaticoSeLigado triggers, best-effort and WITHOUT
// blocking the caller, an attempt to download trailer/theme for `entityID`
// when the "baixar_trailer_tema_automaticamente" toggle is on (ADR-0026
// item 3). Reading the toggle is synchronous (settings are already cached,
// negligible cost); the download itself runs in a goroutine with its OWN context
// (context.Background(), NEVER the one of the HTTP request of the scan -- which dies as
// soon as the response is sent) and a generous timeout, since downloading video via
// yt-dlp is orders of magnitude slower than an image. A failure (network, no candidate,
// yt-dlp unavailable) is only logged -- the periodic trailer_tema_faltantes job covers it
// later, if enabled (ADR-0026 item 4).
func (s *Server) dispararTrailerTemaAutomaticoSeLigado(ctx context.Context, entityID string) {
	cfg, err := s.obterAutoDownloadTrailerTemaSettings(ctx)
	if err != nil || !cfg.Enabled {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := s.BaixarTrailerEntity(bgCtx, entityID); err != nil {
			log.Printf("trailer_tema_faltantes: falha ao baixar trailer automatico da entity %q (best-effort, ignorado): %v", entityID, err)
		}
		if _, err := s.BaixarTemaEntity(bgCtx, entityID); err != nil {
			log.Printf("trailer_tema_faltantes: falha ao baixar tema automatico da entity %q (best-effort, ignorado): %v", entityID, err)
		}
	}()
}

// nfoConvergenteNoDisco decides whether the already registered nfo_path
// (nfoPathAntigo) is at the CURRENT canonical destination and with the CONTENT the
// database would produce now -- used by gravarNFOSeFaltante to skip the rewrite only when
// nothing changed.
//
// Until recently this check looked only at the path and the presence of the
// file: since entity.nfo_path never goes back to null, an .nfo written once
// stayed frozen forever, including a 0-byte .nfo. Now it assembles the
// expected XML (SAME resolverEntity + MontarNFO of gravarNFO, with the
// local Kodi/Emby fields of the file on disk preserved) and compares byte by byte.
//
// Degradation: a failure to recalculate the destination, to resolve the entity or to read
// the file does not take the job down -- it falls back to the old guard (file exists ⇒
// convergent) and the caller logs it. err only comes from resolverDestino, which is a real
// database/disk error.
func (s *Server) nfoConvergenteNoDisco(ctx context.Context, ent sqlcgen.Entity, resolvida resolver.Entidade, nfoPathAntigo string) (bool, error) {
	dir, nomeBaseVideo, _, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return arquivoLocalAindaExiste(nfoPathAntigo), err
	}
	if !ok {
		return arquivoLocalAindaExiste(nfoPathAntigo), nil
	}
	destinoAtual := filepath.Join(dir, localmedia.NomeArquivoNFO(ent.Type, nomeBaseVideo))
	if destinoAtual != nfoPathAntigo {
		return false, nil // divergent destination: the normal flow rewrites
	}
	existente, err := os.ReadFile(nfoPathAntigo)
	if err != nil {
		return false, nil // vanished or unreadable: rewrite from scratch
	}
	esperado, err := localmedia.MontarNFO(resolvida, localmedia.ExtrairCamposUsuario(existente))
	if err != nil {
		return true, nil // cannot assemble the expected content: keep the old behavior
	}
	return bytes.Equal(esperado, existente), nil
}

// gravarNFOSeFaltante generates the entity NFO (same gravarNFO function)
// when it does not exist yet (entity.nfo_path null). season never has an NFO
// of its own (same guard as writeEntityNfo) -- since season
// STARTS to appear in the universe of ListarEntitiesComMediaFilePresente (for the
// season poster), this guard stopped being merely defensive: it is what
// prevents the job from trying to write a season NFO. Episode, on the other
// hand, also started to appear here and NOW gets an NFO normally (kind already
// supported by nomeRaizNFO/raizEpisode, it was just never reached before). An error only
// propagates on a real failure (database/disk); "no destination folder"
// (errSemDestino -- entity without a present media_file, it should never happen
// here, but defensive) degrades to (false, nil).
func (s *Server) gravarNFOSeFaltante(ctx context.Context, ent sqlcgen.Entity, grupos map[string][]artworkItem) (bool, error) {
	if ent.Type == tipoEntTemporada {
		return false, nil
	}
	// Music album/artist: the destination folder NOW resolves (pastaDeMusica, issue #79), but
	// this codebase has no music NFO convention yet (MontarNFO only covers
	// movie/tv/episode) -- without this guard the job would write a nameless ".nfo" next to
	// the tracks.
	if ehMusica(ent.Type) {
		return false, nil
	}

	nfoPathAntigo := ent.NfoPath.String
	temNfoRegistrado := ent.NfoPath.Valid && nfoPathAntigo != ""

	// An EPISODE's .nfo belongs to the FILE, not to the entity (issue #80): the convergence
	// check and the write have to look at the whole group of episodes that share the file.
	if ent.Type == tipoEntEpisodio {
		convergente, cerr := s.nfoDeEpisodiosConvergente(ctx, ent)
		if cerr != nil {
			log.Printf("gravarNFOSeFaltante: falha ao conferir a convergencia do NFO do episodio %q (assumindo reescrita): %v", ent.ID, cerr)
		} else if convergente {
			return false, nil
		}
		if _, werr := s.gravarNFOEpisodios(ctx, ent, grupos); werr != nil {
			if errors.Is(werr, errSemDestino) {
				return false, nil
			}
			return false, werr
		}
		return true, nil
	}

	resolvida, encontrada, err := s.resolverEntity(ctx, ent.ID, "", false)
	if err != nil {
		return false, err
	}
	if !encontrada {
		return false, nil
	}

	if temNfoRegistrado {
		if convergente, cerr := s.nfoConvergenteNoDisco(ctx, ent, resolvida, nfoPathAntigo); cerr != nil {
			log.Printf("gravarNFOSeFaltante: falha ao conferir a convergencia do NFO da entity %q (assumindo o guard de presenca): %v", ent.ID, cerr)
		} else if convergente {
			return false, nil
		}
	}
	// no NFO registered, OR divergent content/destination (legacy naming or
	// database updated after the write), OR the right destination
	// but the file missing -- it falls into the normal flow below, which always writes at
	// the freshly calculated destination (gravarNFO is idempotent by content).
	resultado, err := s.gravarNFOComGrupos(ctx, ent, resolvida, grupos)
	if err != nil {
		if errors.Is(err, errSemDestino) {
			return false, nil
		}
		return false, err
	}
	removerNfoOrfaoSeMudou(temNfoRegistrado, nfoPathAntigo, resultado.NfoPath)
	return true, nil
}

// removerNfoOrfaoSeMudou removes (best-effort) the old .nfo when
// gravarNFOSeFaltante rewrote it at a DIFFERENT destination from the registered one
// (legacy naming) -- without this, the wrong .nfo stays orphaned next to the new one, in
// the right place, and Kodi/Emby may scan both. Safe to remove: an NFO is always a
// one-way export of the system, never edited by the owner outside the MyMediaDB flow
// (AGENTS.md). No-op when there was no nfo_path registered before, or when the destination
// did not change.
func removerNfoOrfaoSeMudou(temNfoRegistrado bool, nfoPathAntigo, novoPath string) {
	if !temNfoRegistrado || nfoPathAntigo == novoPath {
		return
	}
	if rerr := os.Remove(nfoPathAntigo); rerr != nil && !os.IsNotExist(rerr) {
		log.Printf("gravarNFOSeFaltante: falha ao remover NFO legado orfao %q (best-effort, ignorado): %v", nfoPathAntigo, rerr)
	}
}
