package server

// localmedia_handlers.go implements the physical MATERIALIZATION endpoints:
// POST .../assets/{assetId}/materialize (copies artwork already cached internally into
// the library's REAL folder, under the canonical Kodi/Emby name) and
// POST .../entities/{id}/nfo (generates/rewrites the entity's .nfo). The extension of
// PUT .../assets/{assetId}/prefer (materialize + rotate when switching the principal one)
// lives in assets.go, next to the original preferAsset — it reuses the same functions of
// this file. All the name/directory computation is delegated to internal/localmedia (a
// pure package); here there is only I/O + database orchestration.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// carimboNfoFormato is the Go layout of the timestamp written into nfo_written_at, the
// same format as carimboRetroativoFormato (retroativo.go) — it mirrors the SQL
// strftime('%Y-%m-%dT%H:%M:%fZ', 'now') (3 millisecond digits).
const carimboNfoFormato = "2006-01-02T15:04:05.000Z"

// nowISO returns the current UTC timestamp, in the same format the SQL strftime(...)
// writes — used by AtualizarEntityNfo (nfo_written_at).
func nowISO() string {
	return time.Now().UTC().Format(carimboNfoFormato)
}

// errSemDestino marks "entity without a present media_file" (no destination folder to
// materialize artwork/NFO) — the handler maps it to 404, distinct from a real I/O error
// (500).
var errSemDestino = errors.New("localmedia: entity sem media_file presente (sem pasta de destino)")

// "Nothing to do" sentinels returned by baixarTrailerEntityComOpcoes/BaixarTemaEntity — each
// one maps to a specific HTTP status in postEntityTrailerDownload/postEntityThemeDownload
// (respondErroBaixarTrailer/respondErroBaixarTema, via errors.Is) and, at the same time,
// are recognized as "not a real failure" by trailerTemaErroBenigno (used by the
// trailer_tema_faltantes job and by the automatic scan trigger, which only want to know
// whether it downloaded or not, without distinguishing the reason).
var (
	errEntityNaoEncontrada = errors.New("localmedia: entity nao encontrada")
	errYtdlpIndisponivel   = errors.New("localmedia: yt-dlp indisponivel")
	errSemTrailerYoutube   = errors.New("localmedia: sem trailer do youtube disponivel")
	errTrailerJaExiste     = errors.New("localmedia: trailer ja baixado")
	errSemCandidatoTema    = errors.New("localmedia: sem candidato de tema musical")
	errTemaJaExiste        = errors.New("localmedia: tema musical ja presente")
	errDownloadFalhou      = errors.New("localmedia: falha ao baixar")
	errLangInvalida        = errors.New("localmedia: lang fora dos idiomas permitidos")
)

// trailerTemaErroBenigno tells whether `err` is one of the "nothing to do" sentinels
// (entity vanished, yt-dlp unavailable, no candidate available, already present locally,
// no destination folder) -- used by BaixarTrailerEntity/BaixarTemaEntity (trailer_tema
// job and automatic scan trigger, ADR-0026) to NOT treat those expected cases as a real
// failure (only a genuine failure -- network, database -- should become a propagated or
// logged error).
func trailerTemaErroBenigno(err error) bool {
	for _, sentinela := range []error{
		errEntityNaoEncontrada,
		errYtdlpIndisponivel,
		errSemTrailerYoutube,
		errTrailerJaExiste,
		errSemDestino,
		errSemCandidatoTema,
		errTemaJaExiste,
	} {
		if errors.Is(err, sentinela) {
			return true
		}
	}
	return false
}

// materializeAsset handles POST /api/v1/entities/{id}/assets/{assetId}/materialize:
// it copies the already cached binary (assetsDir) of the asset into the library's REAL
// folder, under the canonical name (internal/localmedia). Idempotent: if the asset already
// has a local_path, it returns it as is (no rewrite). 200 with the updated asset; 404
// unknown asset/entity OR entity without a present media_file; 502 never here (the
// download into the cache already happened before, at select/upload — see the note in
// materializarAsset); 503 without assetsDir.
func (s *Server) materializeAsset(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if s.assetsIndisponivel(w) {
		return
	}
	id := chi.URLParam(r, "id")
	assetID := chi.URLParam(r, "assetId")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}
	asset, ok := s.obterAssetDaEntity(w, ctx, id, assetID)
	if !ok {
		return
	}

	atualizado, err := s.materializarAsset(ctx, ent, asset)
	if err != nil {
		respondErroLocalmedia(w, err)
		return
	}
	writeJSON(w, http.StatusOK, assetToDTO(atualizado))
}

// respondErroLocalmedia maps the errors of this file's helpers to the HTTP response:
// errSemDestino -> 404; anything else -> 500.
func respondErroLocalmedia(w http.ResponseWriter, err error) {
	if errors.Is(err, errSemDestino) {
		writeError(w, http.StatusNotFound, "not_found",
			"entity sem arquivo presente (sem pasta de destino na biblioteca)")
		return
	}
	respondErroInterno(w)
}

// materializarAsset reads the asset binary from the internal CACHE (assetsDir,
// path_or_uri — always local in this version: every existing asset went through
// upload/select, which already download into the cache before creating the row; the
// "download materialization" of the contract is for a future candidate that is not yet an
// asset, and the UI already chains select->materialize in that case) and writes a physical
// COPY into the real folder, under the computed canonical name. Idempotent: an already
// materialized asset returns as is.
//
// Implementation decision (not 100% explicit in the spec): when NO other asset of the same
// (entity,kind) is `is_principal` yet (the common case — the first artwork of that kind
// saved into the folder), this asset becomes the principal automatically (BARE name,
// is_principal=1) — it is the only way to satisfy the acceptance criterion "folder with a
// single video: saving into the folder writes poster.jpg (bare)" without requiring an
// explicit PUT /prefer right after. When a principal of the kind already exists, the newly
// materialized one does NOT contest it (rotated name, is_principal=0) — only PUT /prefer
// switches the principal.
func (s *Server) materializarAsset(ctx context.Context, ent sqlcgen.Entity, asset sqlcgen.Asset) (sqlcgen.Asset, error) {
	if asset.LocalPath.Valid && asset.LocalPath.String != "" {
		if arquivoLocalAindaExiste(asset.LocalPath.String) {
			return asset, nil
		}
		return s.regravarBinarioAssetNoLocalPathExistente(ctx, asset)
	}
	jaTemPrincipal, err := s.kindJaTemPrincipal(ctx, ent.ID, asset.Kind)
	if err != nil {
		return sqlcgen.Asset{}, err
	}
	tornaPrincipal := !jaTemPrincipal

	destino, err := s.gravarAssetNaPastaReal(ctx, ent, asset, tornaPrincipal)
	if err != nil {
		return sqlcgen.Asset{}, err
	}
	esc := sqlcgen.New(s.db.Write)
	if err := esc.AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: sql.NullString{String: destino, Valid: true},
		ID:        asset.ID,
	}); err != nil {
		return sqlcgen.Asset{}, err
	}
	asset.LocalPath = sql.NullString{String: destino, Valid: true}

	if tornaPrincipal {
		if err := esc.DesmarcarAssetsPrincipalPorKind(ctx, sqlcgen.DesmarcarAssetsPrincipalPorKindParams{
			EntityID: ent.ID,
			Kind:     asset.Kind,
		}); err != nil {
			return sqlcgen.Asset{}, err
		}
		if err := esc.MarcarAssetPrincipal(ctx, asset.ID); err != nil {
			return sqlcgen.Asset{}, err
		}
		asset.IsPrincipal = 1
	}
	return asset, nil
}

// arquivoLocalAindaExiste confirms through os.Stat whether the physically registered path
// still exists. Errors that are NOT "does not exist" (permission, temporarily unavailable
// FUSE/rclone mount) are treated as "assume it exists" -- best-effort, it avoids rewriting
// on top of a transient glitch of the remote mount (AGENTS.md already documents that this
// mount emits no change event; the same care applies here: stat silence != certainty that
// it vanished).
func arquivoLocalAindaExiste(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

// regravarBinarioAssetNoLocalPathExistente re-reads the asset binary from the internal
// CACHE (lerBinarioAssetDoCache) and rewrites it atomically at the SAME
// asset.LocalPath.String already registered -- used when the materialized file physically
// vanished from disk (rclone/gdrive remote mount, external deletion) but the database still
// points at it. It NEVER recomputes name/index/rotation through gravarAssetNaPastaReal: the
// asset rewritten here may already be is_principal=1, and recomputing would wrongly treat
// that asset as "still non-principal" (it counts itself in kindJaTemPrincipal), producing a
// ROTATED name different from the original bare one. It does not rewrite local_path in the
// database (the value does not change) -- it returns the asset unchanged, the same return
// signature as materializarAsset.
func (s *Server) regravarBinarioAssetNoLocalPathExistente(ctx context.Context, asset sqlcgen.Asset) (sqlcgen.Asset, error) {
	dados, err := s.lerBinarioAssetDoCache(ctx, asset)
	if err != nil {
		return sqlcgen.Asset{}, err
	}
	if err := localmedia.EscreverArquivoAtomico(asset.LocalPath.String, dados); err != nil {
		return sqlcgen.Asset{}, err
	}
	return asset, nil
}

// kindJaTemPrincipal tells whether some asset of the (entity,kind) is already
// is_principal=1.
func (s *Server) kindJaTemPrincipal(ctx context.Context, entityID, kind string) (bool, error) {
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, entityID)
	if err != nil {
		return false, err
	}
	for _, a := range assets {
		if a.Kind == kind && a.IsPrincipal != 0 {
			return true, nil
		}
	}
	return false, nil
}

// gravarAssetNaPastaReal computes the canonical directory/name of the asset (through
// internal/localmedia) and writes the binary (read from the cache) atomically. `principal`
// decides between the bare and the rotated name. It returns the final path.
func (s *Server) gravarAssetNaPastaReal(ctx context.Context, ent sqlcgen.Entity, asset sqlcgen.Asset, principal bool) (string, error) {
	dir, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errSemDestino
	}
	dados, err := s.lerBinarioAssetDoCache(ctx, asset)
	if err != nil {
		return "", err
	}
	kind, nomeBase := s.kindNomeEBase(ctx, ent, asset.Kind, nomeBaseVideo)
	ext := filepath.Ext(asset.PathOrUri)
	existentes, err := s.locaisExistentesDoKind(ctx, ent.ID, asset.Kind, asset.ID)
	if err != nil {
		return "", err
	}
	indice := 0
	if !principal {
		indice = localmedia.IndiceRotacaoLivre(existentes, kind, pastaExclusiva, nomeBase, ext)
	}
	nome := localmedia.NomeArquivoArte(kind, principal, indice, pastaExclusiva, nomeBase, ext)
	destino := filepath.Join(dir, nome)
	if err := localmedia.EscreverArquivoAtomico(destino, dados); err != nil {
		return "", err
	}
	return destino, nil
}

// lerBinarioAssetDoCache reads the asset binary from the internal CACHE (path_or_uri),
// under the SAME path-traversal guard as servirBinarioAsset
// (raizesConfiaveis/caminhoSobRaizes) — the asset was already validated as belonging to
// the entity by the caller.
func (s *Server) lerBinarioAssetDoCache(ctx context.Context, asset sqlcgen.Asset) ([]byte, error) {
	raizes, err := s.raizesConfiaveis(ctx)
	if err != nil {
		return nil, err
	}
	if !caminhoSobRaizes(asset.PathOrUri, raizes) {
		return nil, errors.New("localmedia: caminho do asset fora das raizes confiaveis")
	}
	return os.ReadFile(filepath.Clean(asset.PathOrUri)) //nolint:gosec // path validated by caminhoSobRaizes above
}

// locaisExistentesDoKind lists the `local_path` already used by OTHER assets (except
// `excetoID`) of the same (entity,kind) — the basis for computing the free rotation index
// (localmedia.IndiceRotacaoLivre).
func (s *Server) locaisExistentesDoKind(ctx context.Context, entityID, kind, excetoID string) ([]string, error) {
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, entityID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range assets {
		if a.ID == excetoID || a.Kind != kind || !a.LocalPath.Valid || a.LocalPath.String == "" {
			continue
		}
		out = append(out, a.LocalPath.String)
	}
	return out, nil
}

// resolverDestino computes the destination directory, the video base name and whether the
// folder is EXCLUSIVE (a single video) for an entity — the basis shared by
// materialize/prefer/nfo. ok=false when the entity has no eligible present media_file (no
// destination folder). The DIRECTORY comes from localmedia.DiretorioDestino (the series'
// real root — never a season subfolder again); the video base name keeps coming from the
// oldest media_file (ArquivoMaisAntigo), which is what names the .nfo/still of a movie and
// of an episode.
//
// tv/season are ALWAYS an "exclusive folder": the series root never has a video directly
// inside it (the episodes live in season subfolders) -- so the exclusivity rule (which
// counts videos PRESENT directly in the directory) would always return false there,
// forcing the series poster/fanart to be born prefixed by the oldest video name (exactly
// the reported bug: "poster.jpg saved as ...-S01E01-...-poster.jpg"). Kodi has no concept
// of "edition"/ambiguity for WORK-LEVEL artwork of a series -- it is always a bare
// poster.jpg/fanart.jpg (the numeric rotation of multiple artworks still goes through
// sufixoRotacao, independently of this flag).
//
// The exclusivity answer is ONE indexed count (issue #73). It used to load every present
// media_file of the library into memory (~150 ms, 20.075 rows) to count the files of a
// single folder, and this function runs 2 to 5 times per entity in the midias_faltantes
// job: about 50.000 full table scans per run, which was most of its 2 hours.
func (s *Server) resolverDestino(ctx context.Context, ent sqlcgen.Entity) (dir, nomeBaseVideo string, pastaExclusiva, ok bool, err error) {
	localFiles, ancestors, err := s.carregarMediaFilesLocalmedia(ctx, ent)
	if err != nil {
		return "", "", false, false, err
	}
	entLocal := localmedia.Entity{ID: ent.ID, Type: ent.Type}
	dir, ok = localmedia.DiretorioDestino(entLocal, localFiles, ancestors)
	if !ok {
		return "", "", false, false, nil
	}
	mf, achou := localmedia.ArquivoMaisAntigo(entLocal, localFiles, ancestors)
	if !achou {
		return "", "", false, false, nil
	}
	nomeBaseVideo = localmedia.NomeBaseVideo(mf.Path)

	if ent.Type == tipoEntSerie || ent.Type == tipoEntTemporada {
		return dir, nomeBaseVideo, true, true, nil
	}

	pastaExclusiva, err = s.pastaExclusivaNoDisco(ctx, dir)
	if err != nil {
		return "", "", false, false, err
	}
	return dir, nomeBaseVideo, pastaExclusiva, true, nil
}

// pastaExclusivaNoDisco answers "does `dir` hold exactly ONE present media file?" with one
// indexed range scan on media_file.path instead of the whole library in memory (issue #73).
// Same rule as localmedia.EhPastaExclusiva, which stays for callers that already have the
// files in hand; here the only caller wants one folder.
//
// The prefix pair is "dir/" and "dir0": '/' + 1 is '0', so the range covers exactly the
// paths that start with "dir/". No file system access and no LIKE, so a folder whose name
// contains `%` or `_` cannot change the answer.
func (s *Server) pastaExclusivaNoDisco(ctx context.Context, dir string) (bool, error) {
	// A destination is always a folder under a source root; the guard keeps a degenerate
	// value (empty, or the file system root itself) from turning into a prefix that covers
	// the whole disk.
	if limpo := strings.TrimSuffix(dir, "/"); limpo != "" && limpo != "/" {
		dir = limpo
	} else {
		return false, nil
	}
	n, err := s.leitura().ContarMediaFilesPresentesNaPasta(ctx, sqlcgen.ContarMediaFilesPresentesNaPastaParams{
		Prefixo:    dir + "/",
		PrefixoFim: dir + "0",
	})
	if err != nil {
		return false, err
	}
	return localmedia.PastaExclusivaPorContagem(int(n)), nil
}

// carregarMediaFilesLocalmedia returns (mediaFiles, ancestors) in the shape of the
// internal/localmedia package for the given entity: it reuses listarFilesDaEntity (which
// already solves the episode complexity through the media_file_entity join/ADR-0021) — but
// for `season`, which has no media_file OF ITS OWN (every episode file points its entity_id
// at the SERIES), it first resolves the "tv" ancestor (montarAncestrais) and loads ITS
// files. Each returned MediaFile has EntityID relabeled to the entity whose files were
// loaded (series for season/tv, the entity itself for movie/episode) — consistent with what
// localmedia.DiretorioDestino expects to filter.
func (s *Server) carregarMediaFilesLocalmedia(ctx context.Context, ent sqlcgen.Entity) ([]localmedia.MediaFile, []localmedia.Entity, error) {
	ancestorRefs := s.montarAncestrais(ctx, ent.ID)
	ancestors := make([]localmedia.Entity, len(ancestorRefs))
	for i, a := range ancestorRefs {
		ancestors[i] = localmedia.Entity{ID: a.ID, Type: a.Type}
	}

	alvoEnt := ent
	if ent.Type == tipoEntTemporada {
		if serieID, ok := serieAncestralID(ancestors); ok {
			serieEnt, err := s.leitura().ObterEntity(ctx, serieID)
			if err == nil {
				alvoEnt = serieEnt
			}
		}
	}
	files, err := s.listarFilesDaEntity(ctx, alvoEnt)
	if err != nil {
		return nil, nil, err
	}
	out := make([]localmedia.MediaFile, 0, len(files))
	for _, f := range files {
		out = append(out, localmedia.MediaFile{
			EntityID:  alvoEnt.ID,
			Path:      f.Path,
			State:     f.State,
			CreatedAt: f.CreatedAt,
		})
	}
	return out, ancestors, nil
}

// serieAncestralID finds the id of the nearest "tv" ancestor in the ROOT->PARENT trail —
// it mirrors localmedia.serieAncestral (non-exported), so the server resolves WHICH entity
// to load the media_file from before calling localmedia (which only does the pure filter
// over the already loaded list).
func serieAncestralID(ancestors []localmedia.Entity) (string, bool) {
	for i := len(ancestors) - 1; i >= 0; i-- {
		if ancestors[i].Type == localmedia.TipoSerie {
			return ancestors[i].ID, true
		}
	}
	return "", false
}

// kindNomeEBase decides the kind/base name to pass to localmedia.NomeArquivoArte: the
// poster of a SEASON uses the synthetic kind KindSeasonPoster + "season{NN}" (never the
// video name); the other cases use the video's kind/base name as usual.
func (s *Server) kindNomeEBase(ctx context.Context, ent sqlcgen.Entity, assetKind, nomeBaseVideo string) (kind, nomeBase string) {
	if ent.Type == tipoEntTemporada && assetKind == kindPoster {
		return localmedia.KindSeasonPoster, localmedia.NomeSeasonBase(s.numeroDaTemporada(ctx, ent.ID))
	}
	return assetKind, nomeBaseVideo
}

// numeroDaTemporada reads the season_number (entity_relation.sort_index of the
// series->season link) of a season entity. 0 (defensive) when absent/without a parent — it
// degrades to "season00" instead of failing the materialization.
func (s *Server) numeroDaTemporada(ctx context.Context, seasonID string) int {
	pais, err := s.leitura().ListarRelacoesPais(ctx, seasonID)
	if err != nil || len(pais) == 0 || !pais[0].SortIndex.Valid {
		return 0
	}
	return int(pais[0].SortIndex.Int64)
}

// ── POST /entities/{id}/nfo ──────────────────────────────────────────────

// nfoWriteResult is the body of POST /entities/{id}/nfo (NfoWriteResult in the contract):
// it mirrors entity.nfo_path/nfo_written_at.
type nfoWriteResult struct {
	NfoPath      string `json:"nfo_path"`
	NfoWrittenAt string `json:"nfo_written_at"`
}

// writeEntityNfo handles POST /api/v1/entities/{id}/nfo: it (re)generates and writes the
// entity's .nfo into the real folder, from the SAME resolver.Entidade that resolverEntity
// already produces. Idempotent (it always overwrites completely). 200 with
// {nfo_path,nfo_written_at}; 404 unknown entity OR without a present media_file.
func (s *Server) writeEntityNfo(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	lang, ok := s.parseLang(w, cfg, r.URL.Query().Get("lang"))
	if !ok {
		return
	}

	resolvida, encontrada, err := s.resolverEntity(ctx, id, lang, false)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !encontrada {
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
		return
	}
	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	// a season has no NFO of its own (there is no media_file nor a video-name convention
	// of its own -- DiretorioDestino would resolve the series root, but the file name would
	// collide with the tvshow.nfo/episode NFO written there). It is treated as "no
	// destination folder" (404) — the UI should not expose the "Salvar NFO" button for a
	// season.
	if ent.Type == tipoEntTemporada {
		respondErroLocalmedia(w, errSemDestino)
		return
	}

	resultado, err := s.gravarNFO(ctx, ent, resolvida)
	if err != nil {
		respondErroLocalmedia(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resultado)
}

// gravarNFO computes the destination (DiretorioDestino/NomeArquivoNFO), builds the XML
// (localmedia.MontarNFO) and writes it atomically, updating
// entity.nfo_path/nfo_written_at. errSemDestino when the entity has no present media_file
// (no destination folder).
//
// Before building the XML, it materializes (best-effort) the WINNING poster and background
// into the real folder (the same logic as MaterializarMidiasFaltantes): that way the .nfo
// never needs a <thumb>/<fanart> pointing at the internal API -- Kodi/Jellyfin discover the
// artwork on their own through the file name convention. A materialization failure
// (network, permission) is logged and IGNORED -- it never prevents writing the .nfo.
//
// Before overwriting, it reads the .nfo already at the destination (if any) and preserves
// the LOCAL Kodi/Emby/Jellyfin fields -- user rating, watched count/state, last played,
// date added and resume point (localmedia.ExtrairCamposUsuario) -- so it does NOT destroy
// editing done outside MyMediaDB (by hand or by Kodi itself) when rewriting. The read is
// best-effort: a missing or corrupted file does not prevent the write, it only results in a
// zero-value (nothing to preserve).
func (s *Server) gravarNFO(ctx context.Context, ent sqlcgen.Entity, e resolver.Entidade) (nfoWriteResult, error) {
	return s.gravarNFOComGrupos(ctx, ent, e, s.montarGruposArtwork(ctx, ent.ID))
}

// gravarNFOComGrupos is gravarNFO with the artwork groups already in hand: the two
// materializations below need the poster and the fanart winner, and a caller that already
// aggregated the entity (the midias_faltantes job does, once per entity) must not pay for
// it again -- nor for a third aggregation inside each materializarKindVencedor.
func (s *Server) gravarNFOComGrupos(ctx context.Context, ent sqlcgen.Entity, e resolver.Entidade, grupos map[string][]artworkItem) (nfoWriteResult, error) {
	dir, nomeBaseVideo, _, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return nfoWriteResult{}, err
	}
	if !ok {
		return nfoWriteResult{}, errSemDestino
	}

	if _, err := s.materializarKindVencedor(ctx, ent, kindPoster, grupos); err != nil {
		log.Printf("gravarNFO: falha ao materializar poster da entity %q (best-effort, ignorado): %v", ent.ID, err)
	}
	if _, err := s.materializarKindVencedor(ctx, ent, kindFanart, grupos); err != nil {
		log.Printf("gravarNFO: falha ao materializar fanart da entity %q (best-effort, ignorado): %v", ent.ID, err)
	}

	nome := localmedia.NomeArquivoNFO(ent.Type, nomeBaseVideo)
	destino := filepath.Join(dir, nome)
	existente, _ := os.ReadFile(destino)
	usuario := localmedia.ExtrairCamposUsuario(existente)

	corpo, err := localmedia.MontarNFO(e, usuario)
	if err != nil {
		return nfoWriteResult{}, err
	}
	if err := localmedia.EscreverArquivoAtomico(destino, corpo); err != nil {
		return nfoWriteResult{}, err
	}

	agora := nowISO()
	if err := sqlcgen.New(s.db.Write).AtualizarEntityNfo(ctx, sqlcgen.AtualizarEntityNfoParams{
		NfoPath:      sql.NullString{String: destino, Valid: true},
		NfoWrittenAt: sql.NullString{String: agora, Valid: true},
		ID:           ent.ID,
	}); err != nil {
		return nfoWriteResult{}, err
	}
	return nfoWriteResult{NfoPath: destino, NfoWrittenAt: agora}, nil
}

// ── Extension of PUT /assets/{assetId}/prefer ───────────────────────────────
//
// The physical rotation when switching the principal artwork:
// materializarERotacionarPrincipal is called by preferAsset (assets.go). The invariant "at
// most one is_principal=1 per (entity,kind)" is enforced IN CODE — it clears the old one
// BEFORE setting the new one — sequentially in the SINGLE-WRITER pool
// (SetMaxOpenConns(1), ADR-0010): there is no explicit `sql.Tx` because no other point of
// the codebase uses one (the pool already serializes every write, the same pattern as
// marcarPreferida).

// materializarERotacionarPrincipal materializes the target physically (if needed) and
// RENAMES the files of the same (entity,kind) to reflect the new order: the former
// principal (if any, and != target) becomes the next FREE rotation index; the target takes
// (or is renamed to) the bare name. It sets is_principal=1 on the target and 0 on the
// former one, in the column. It returns the UPDATED target (local_path/is_principal
// reflected in memory).
func (s *Server) materializarERotacionarPrincipal(ctx context.Context, ent sqlcgen.Entity, assets []sqlcgen.Asset, alvo sqlcgen.Asset) (sqlcgen.Asset, error) {
	if antigo, ok := principalAtualDoKind(assets, alvo.Kind, alvo.ID); ok {
		if err := s.rotacionarAntigoPrincipal(ctx, ent, antigo); err != nil {
			return sqlcgen.Asset{}, err
		}
	}

	novoLocalPath, err := s.assumirNomeBare(ctx, ent, alvo)
	if err != nil {
		return sqlcgen.Asset{}, err
	}

	esc := sqlcgen.New(s.db.Write)
	if err := esc.DesmarcarAssetsPrincipalPorKind(ctx, sqlcgen.DesmarcarAssetsPrincipalPorKindParams{
		EntityID: ent.ID,
		Kind:     alvo.Kind,
	}); err != nil {
		return sqlcgen.Asset{}, err
	}
	if err := esc.MarcarAssetPrincipal(ctx, alvo.ID); err != nil {
		return sqlcgen.Asset{}, err
	}

	alvo.LocalPath = sql.NullString{String: novoLocalPath, Valid: true}
	alvo.IsPrincipal = 1
	return alvo, nil
}

// principalAtualDoKind finds the is_principal=1 asset of the SAME kind, except `excetoID`
// (the target that is becoming the new principal) — the "former principal" that needs to
// be rotated to a numeric index.
func principalAtualDoKind(assets []sqlcgen.Asset, kind, excetoID string) (sqlcgen.Asset, bool) {
	for _, a := range assets {
		if a.Kind == kind && a.ID != excetoID && a.IsPrincipal != 0 {
			return a, true
		}
	}
	return sqlcgen.Asset{}, false
}

// rotacionarAntigoPrincipal renames the physical file of the former principal (bare name)
// to the next FREE rotation index, and updates asset.local_path. No-op when the former one
// had no local_path (never materialized — only the is_principal flag changes, with no file
// to rename).
func (s *Server) rotacionarAntigoPrincipal(ctx context.Context, ent sqlcgen.Entity, antigo sqlcgen.Asset) error {
	if !antigo.LocalPath.Valid || antigo.LocalPath.String == "" {
		return nil
	}
	dir := filepath.Dir(antigo.LocalPath.String)
	ext := filepath.Ext(antigo.LocalPath.String)
	_, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return err
	}
	if !ok {
		return errSemDestino
	}
	kind, nomeBase := s.kindNomeEBase(ctx, ent, antigo.Kind, nomeBaseVideo)
	existentes, err := s.locaisExistentesDoKind(ctx, ent.ID, antigo.Kind, antigo.ID)
	if err != nil {
		return err
	}
	indice := localmedia.IndiceRotacaoLivre(existentes, kind, pastaExclusiva, nomeBase, ext)
	novoDestino := filepath.Join(dir, localmedia.NomeArquivoArte(kind, false, indice, pastaExclusiva, nomeBase, ext))
	if err := localmedia.RenomearArquivo(antigo.LocalPath.String, novoDestino); err != nil {
		return err
	}
	return sqlcgen.New(s.db.Write).AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: sql.NullString{String: novoDestino, Valid: true},
		ID:        antigo.ID,
	})
}

// assumirNomeBare ensures the target has the BARE name of its kind: if it already has a
// local_path (materialized, even if rotated), it RENAMES it (never recopies bytes);
// otherwise it materializes from scratch straight with the bare name. It persists
// local_path and returns the final path.
func (s *Server) assumirNomeBare(ctx context.Context, ent sqlcgen.Entity, alvo sqlcgen.Asset) (string, error) {
	var destino string
	var err error
	if !alvo.LocalPath.Valid || alvo.LocalPath.String == "" {
		destino, err = s.gravarAssetNaPastaReal(ctx, ent, alvo, true)
	} else {
		destino, err = s.renomearParaBare(ctx, ent, alvo)
	}
	if err != nil {
		return "", err
	}
	if err := sqlcgen.New(s.db.Write).AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: sql.NullString{String: destino, Valid: true},
		ID:        alvo.ID,
	}); err != nil {
		return "", err
	}
	return destino, nil
}

// renomearParaBare renames the ALREADY materialized file of the target to the bare name of
// its kind (RENAME, never recopies bytes) — idempotent: if the file is already the bare
// one, no-op (it returns the same path).
//
// When the target is already the bare one (destination == current local_path — the common
// case of PUT .../prefer called again on the asset that is already principal, with no
// rotation to do), that "no-op" is exactly the other point of the system that decides
// "already materialized, nothing to do" by looking only at the database column — so it
// reconfirms through arquivoLocalAindaExiste and rewrites in place
// (regravarBinarioAssetNoLocalPathExistente) if the file vanished from disk, instead of
// returning the path as if nothing had changed.
func (s *Server) renomearParaBare(ctx context.Context, ent sqlcgen.Entity, alvo sqlcgen.Asset) (string, error) {
	dir := filepath.Dir(alvo.LocalPath.String)
	ext := filepath.Ext(alvo.LocalPath.String)
	_, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errSemDestino
	}
	kind, nomeBase := s.kindNomeEBase(ctx, ent, alvo.Kind, nomeBaseVideo)
	destino := filepath.Join(dir, localmedia.NomeArquivoArte(kind, true, 0, pastaExclusiva, nomeBase, ext))
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

// ── GET /entities/{id}/theme — local theme song ─────────────────────────────
//
// MINIMAL and LOCAL-ONLY foundation: no provider (there is no reliable integrated source of
// theme songs) — the owner places a `theme.{mp3,ogg,flac}` by hand in the item's root
// folder (the same root as DiretorioDestino, reused from resolverDestino, already used by
// materialize/nfo above) and this endpoint only DETECTS and serves the binary, without
// materializing any asset.

// extensoesTema lists, in order of preference, the accepted theme formats — mp3 first (the
// common Kodi/Jellyfin case), ogg/flac as alternatives. A list fixed in code: it is
// deliberately not exposed as an option.
var extensoesTema = []string{".mp3", ".ogg", ".flac"}

// encontrarArquivoTema looks for theme.<ext> in the root `dir`, in the order of
// extensoesTema — the first one that exists (and is not a directory) wins.
func encontrarArquivoTema(dir string) (string, bool) {
	for _, ext := range extensoesTema {
		caminho := filepath.Join(dir, "theme"+ext)
		info, err := os.Stat(caminho)
		if err == nil && !info.IsDir() {
			return caminho, true
		}
	}
	return "", false
}

// contentTypeTema resolves the theme content-type by the extension — the three accepted by
// encontrarArquivoTema have a known, fixed content-type (no need to sniff bytes the way
// contentTypeImagem does for artwork).
func contentTypeTema(caminho string) string {
	switch strings.ToLower(filepath.Ext(caminho)) {
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	default:
		return "audio/mpeg"
	}
}

// getEntityTheme handles GET /api/v1/entities/{id}/theme: it resolves the item's root
// folder (resolverDestino — the SAME DiretorioDestino resolution used by
// materialize/nfo) and serves the binary of the theme.{mp3,ogg,flac} found there, if any.
// The path comes 100% from the SERVER-SIDE resolution (never from client input, which only
// provides the id) — without materializing an asset, without touching the database. 404
// when the entity does not exist, has no destination folder (no present media_file) or
// there is no theme file in the folder.
func (s *Server) getEntityTheme(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	dir, _, _, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found",
			"sem pasta de destino na biblioteca (nenhum arquivo presente)")
		return
	}

	caminho, ok := encontrarArquivoTema(dir)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "tema musical não encontrado")
		return
	}

	limpo := filepath.Clean(caminho)
	f, err := os.Open(limpo) //nolint:gosec // path comes only from the server-side resolution (resolverDestino), never from client input
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "arquivo de tema indisponível")
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "not_found", "arquivo de tema indisponível")
		return
	}
	w.Header().Set("Content-Type", contentTypeTema(limpo))
	http.ServeContent(w, r, filepath.Base(limpo), info.ModTime(), f)
}

// ── Trailer: on-disk materialization through yt-dlp (ADR-0025) ────────────
//
// GET .../trailer detects and serves an already materialized trailer (the same pattern as
// getEntityTheme, the SAME root folder of resolverDestino). POST .../trailer/download
// DOWNLOADS the first YouTube trailer listed in `videos` (TMDB/TVDB, already extracted
// through richfields) using yt-dlp and writes it under the canonical Kodi/Jellyfin name. No
// new `asset` table -- the same precedent as theme.mp3: detection by file existence, with
// no dedicated database row (a video does not enter the image pipeline of
// assets.go/parseKind/gallery).

// extensoesTrailer lists, in checking order, the video containers that encontrarArquivoTrailer
// accepts as "trailer already present": .mp4 is the FIXED destination of every download made
// by this endpoint (extPadraoDeKind); .mkv/.webm cover a trailer placed manually by the
// owner in another container -- it also has to be detected (principle 5: never overwrite
// without force), not only what the download itself produces.
var extensoesTrailer = []string{".mp4", ".mkv", ".webm"}

// encontrarArquivoTrailer looks for the canonical trailer name
// (internal/localmedia.NomeArquivoArte with KindTrailer) in the root `dir`, trying each
// extension of extensoesTrailer -- the first one that exists (and is not a directory)
// wins.
func encontrarArquivoTrailer(dir, nomeBaseVideo string, pastaExclusiva bool) (string, bool) {
	for _, ext := range extensoesTrailer {
		nome := localmedia.NomeArquivoArte(localmedia.KindTrailer, true, 0, pastaExclusiva, nomeBaseVideo, ext)
		caminho := filepath.Join(dir, nome)
		info, err := os.Stat(caminho)
		if err == nil && !info.IsDir() {
			return caminho, true
		}
	}
	return "", false
}

// contentTypeTrailer resolves the trailer content-type by the extension -- the three
// accepted by encontrarArquivoTrailer have a known, fixed content-type, the same spirit as
// contentTypeTema.
func contentTypeTrailer(caminho string) string {
	switch strings.ToLower(filepath.Ext(caminho)) {
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	default:
		return "video/mp4"
	}
}

// getEntityTrailer handles GET /api/v1/entities/{id}/trailer: it resolves the item's root
// folder (resolverDestino, the SAME resolution as materialize/nfo/theme) and serves the
// binary of the trailer found there, if any. 404 when the entity does not exist, has no
// destination folder (no present media_file) or there is no trailer materialized in the
// folder.
func (s *Server) getEntityTrailer(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	dir, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found",
			"sem pasta de destino na biblioteca (nenhum arquivo presente)")
		return
	}

	caminho, ok := encontrarArquivoTrailer(dir, nomeBaseVideo, pastaExclusiva)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "trailer não encontrado")
		return
	}

	limpo := filepath.Clean(caminho)
	f, err := os.Open(limpo) //nolint:gosec // path comes only from the server-side resolution (resolverDestino+encontrarArquivoTrailer), never from client input
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "arquivo de trailer indisponível")
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "not_found", "arquivo de trailer indisponível")
		return
	}
	w.Header().Set("Content-Type", contentTypeTrailer(limpo))
	http.ServeContent(w, r, filepath.Base(limpo), info.ModTime(), f)
}

// trailerDownloadRequest is the (optional) body of POST .../trailer/download. Key selects a
// specific video by its YouTube `key` (an open door for a future selector in the frontend,
// explicitly out of scope for now); empty falls back to the default (first trailer,
// richfields.PrimeiroTrailerYoutube). Force asks for a re-download even with a trailer
// already present (principle 5: without Force, an existing trailer -- downloaded before OR
// placed manually by the owner -- is NEVER overwritten).
type trailerDownloadRequest struct {
	Key   string `json:"key"`
	Force bool   `json:"force"`
}

// trailerDownloadResult is the success body of POST .../trailer/download.
type trailerDownloadResult struct {
	LocalPath string `json:"local_path"`
}

// escolherTrailerYoutube decides WHICH video to download: with a non-empty `key`, the
// YouTube video with that exact key; empty falls back to the default (first playable
// trailer, the SAME criterion as the frontend -- richfields.PrimeiroTrailerYoutube).
func escolherTrailerYoutube(videos []richfields.Video, key string) (richfields.Video, bool) {
	if key == "" {
		return richfields.PrimeiroTrailerYoutube(videos)
	}
	for _, v := range videos {
		if v.Site == "YouTube" && v.Key == key {
			return v, true
		}
	}
	return richfields.Video{}, false
}

// baixarTrailerEntityComOpcoes is the PURE implementation (no http.ResponseWriter) of
// "download the YouTube trailer for ONE entity": it resolves the video (language +
// richfields.Videos, the SAME shape as GET /entities/{id}), the destination in the library's
// real folder and downloads it through yt-dlp (s.ytdlp.Baixar) into a TEMPORARY file, only
// renaming it to the canonical name on success (it never leaves a partial file with the
// canonical name). Reused by postEntityTrailerDownload (which translates the returned error
// into an HTTP status through respondErroBaixarTrailer/errors.Is) and by
// BaixarTrailerEntity (trailer_tema_faltantes job and automatic scan trigger). An empty
// `lang` falls back to the primary language; an empty `key` picks the first available
// trailer (richfields.PrimeiroTrailerYoutube); `force` ignores an already present trailer.
// It returns the sentinels
// errEntityNaoEncontrada/errYtdlpIndisponivel/errSemTrailerYoutube/errSemDestino/
// errTrailerJaExiste for the "nothing to do" cases; any other error is a real failure
// (network, database).
func (s *Server) baixarTrailerEntityComOpcoes(ctx context.Context, entityID, lang, key string, force bool) (destino string, err error) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errEntityNaoEncontrada
		}
		return "", err
	}

	if s.ytdlp == nil {
		return "", errYtdlpIndisponivel
	}
	disponivel, _, detalhe := s.ytdlp.Status(ctx)
	if !disponivel {
		if detalhe != "" {
			return "", fmt.Errorf("%w: %s", errYtdlpIndisponivel, detalhe)
		}
		return "", errYtdlpIndisponivel
	}

	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return "", err
	}
	if lang != "" && !cfg.permite(lang) {
		return "", errLangInvalida
	}
	if lang == "" {
		lang = cfg.Primary
	}
	resolvida, encontrada, err := s.resolverEntity(ctx, entityID, lang, false)
	if err != nil {
		return "", err
	}
	if !encontrada {
		return "", errEntityNaoEncontrada
	}
	video, ok := escolherTrailerYoutube(resolvida.Videos, key)
	if !ok {
		return "", errSemTrailerYoutube
	}

	dir, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errSemDestino
	}
	if _, jaExiste := encontrarArquivoTrailer(dir, nomeBaseVideo, pastaExclusiva); jaExiste && !force {
		return "", errTrailerJaExiste
	}

	dest := filepath.Join(dir, localmedia.NomeArquivoArte(localmedia.KindTrailer, true, 0, pastaExclusiva, nomeBaseVideo, ""))
	if err := s.baixarTrailerAtomico(ctx, video, dest); err != nil {
		return "", fmt.Errorf("%w: %v", errDownloadFalhou, err)
	}
	return dest, nil
}

// YtdlpDisponivel exposes the yt-dlp status (s.ytdlp.Status) so the trailer_tema job
// (internal/job/trailertema) can check it ONCE per run, before iterating the entities -- it
// avoids hitting the binary (--version) once per library entity when it is simply
// unavailable. Without s.ytdlp injected, it degrades to unavailable (the same defensive
// scenario as ytdlpDisponivel/montarYtdlpStatusResponse).
func (s *Server) YtdlpDisponivel(ctx context.Context) (disponivel bool, detalhe string) {
	if s.ytdlp == nil {
		return false, "yt-dlp nao configurado neste servidor"
	}
	disponivel, _, detalhe = s.ytdlp.Status(ctx)
	return disponivel, detalhe
}

// BaixarTrailerEntity downloads (best-effort) the default YouTube trailer (no explicit
// video selection, no force, primary language) for `entityID` -- used by the
// trailer_tema_faltantes job (internal/job/trailertema) and by the automatic scan trigger
// (ADR-0026). The "nothing to do" sentinels (no trailer available, already present
// locally, no destination, yt-dlp unavailable, entity vanished) come back as
// baixado=false, err=nil -- only a REAL failure (network, database) propagates err, so the
// caller (job/goroutine) counts and logs it as a genuine failure.
func (s *Server) BaixarTrailerEntity(ctx context.Context, entityID string) (bool, error) {
	_, err := s.baixarTrailerEntityComOpcoes(ctx, entityID, "", "", false)
	if err == nil {
		return true, nil
	}
	if trailerTemaErroBenigno(err) {
		return false, nil
	}
	return false, err
}

// respondErroBaixarTrailer maps the sentinels of baixarTrailerEntityComOpcoes to the HTTP
// status of the POST .../trailer/download contract: missing entity -> 404; lang outside the
// allowed ones -> 400; yt-dlp unavailable -> 503; no YouTube trailer on the item -> 422; no
// destination folder -> 404; trailer already present (without force) -> 409; failure of the
// download itself -> 502; any other error (database, settings) -> 500.
func respondErroBaixarTrailer(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errEntityNaoEncontrada):
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
	case errors.Is(err, errLangInvalida):
		writeError(w, http.StatusBadRequest, "invalid_query", "lang fora dos idiomas permitidos")
	case errors.Is(err, errYtdlpIndisponivel):
		writeError(w, http.StatusServiceUnavailable, "ytdlp_indisponivel", err.Error())
	case errors.Is(err, errSemTrailerYoutube):
		writeError(w, http.StatusUnprocessableEntity, "sem_trailer",
			"nenhum trailer do YouTube disponível para este item")
	case errors.Is(err, errSemDestino):
		respondErroLocalmedia(w, errSemDestino)
	case errors.Is(err, errTrailerJaExiste):
		writeError(w, http.StatusConflict, "ja_existe",
			"trailer já baixado (use force=true para re-baixar)")
	case errors.Is(err, errDownloadFalhou):
		writeError(w, http.StatusBadGateway, "youtube_download_failed",
			"falha ao baixar o trailer: "+err.Error())
	default:
		respondErroInterno(w)
	}
}

// postEntityTrailerDownload handles POST /api/v1/entities/{id}/trailer/download (ADR-0025):
// it only decodes the optional body and translates the return of
// baixarTrailerEntityComOpcoes into an HTTP status (respondErroBaixarTrailer) — all the
// logic of resolving video/destination/download lives in the pure function.
func (s *Server) postEntityTrailerDownload(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")

	var body trailerDownloadRequest
	if ok := decodeOpcional(w, r, &body); !ok {
		return
	}

	destino, err := s.baixarTrailerEntityComOpcoes(r.Context(), id, r.URL.Query().Get("lang"), body.Key, body.Force)
	if err != nil {
		respondErroBaixarTrailer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, trailerDownloadResult{LocalPath: destino})
}

// baixarTrailerAtomico downloads `video` (s.ytdlp.Baixar) into a TEMPORARY file
// (destino+".tmp", the same directory as the final destination) and only RENAMES it to the
// canonical name on success -- it never leaves a partial binary with the canonical name
// (the same principle as EscreverArquivoAtomico/RenomearArquivo, adapted: here it is yt-dlp
// that writes the bytes, not this process). A failure at any step removes the temporary
// file (best-effort) and propagates the error.
func (s *Server) baixarTrailerAtomico(ctx context.Context, video richfields.Video, destino string) error {
	urlOrigem := video.URL
	if urlOrigem == nil || *urlOrigem == "" {
		u := "https://www.youtube.com/watch?v=" + video.Key
		urlOrigem = &u
	}
	destinoTemp := destino + ".tmp"
	if err := s.ytdlp.Baixar(ctx, *urlOrigem, destinoTemp); err != nil {
		_ = os.Remove(destinoTemp)
		return err
	}
	if err := localmedia.RenomearArquivo(destinoTemp, destino); err != nil {
		_ = os.Remove(destinoTemp)
		return err
	}
	return nil
}

// ── Automatic theme song through ThemerrDB (ADR-0025) ──────────────────────
//
// GET .../theme/candidato exposes WHETHER there is a ThemerrDB candidate (without leaking
// the YouTube URL to the client — a conservative decision; the download happens entirely on
// the server) and whether a local theme.* already exists (reusing encontrarArquivoTema).
// POST .../theme/download downloads the candidate through yt-dlp into theme.mp3 at the root
// of the folder — the SAME path that GET /entities/{id}/theme already serves. WITHOUT
// `force`: the theme is singular per folder, so an already present theme.* (downloaded
// before OR placed by hand by the owner) is NEVER overwritten — the owner deletes it
// manually before re-downloading (unlike the trailer).

// nomeFonteThemerrdb is the hardcoded `source` of the candidate envelope in
// media_external — today only ThemerrDB implements provider.TemaMusicalProvider (the same
// precedent as "fanarttv" hardcoded in coletarInventarioFanart,
// internal/server/artwork.go).
const nomeFonteThemerrdb = "themerrdb"

// themeCandidatoResult is the body of GET .../theme/candidato.
type themeCandidatoResult struct {
	Disponivel bool `json:"disponivel"`
	JaTemLocal bool `json:"ja_tem_local"`
}

// candidatoTemaURL reads the theme song candidate envelope (media_external,
// source=themerrdb, key provider.ChaveEnvelopeTemaMusical) written by enrich
// (internal/enrich/tema_musical.go) — it NEVER queries ThemerrDB live. ok=false when there
// is no row, the payload is unreadable, or the envelope carries no URL (a "no match"
// candidate is written anyway, best-effort).
func (s *Server) candidatoTemaURL(ctx context.Context, entityID string) (string, bool) {
	row, err := s.leitura().ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: entityID,
		Source:   nomeFonteThemerrdb,
	})
	if err != nil || !row.PayloadJson.Valid {
		return "", false
	}
	var env map[string]any
	if json.Unmarshal([]byte(row.PayloadJson.String), &env) != nil {
		return "", false
	}
	url, ok := env[provider.ChaveEnvelopeTemaMusical].(string)
	if !ok || url == "" {
		return "", false
	}
	return url, true
}

// getEntityThemeCandidato handles GET /api/v1/entities/{id}/theme/candidato: it tells
// whether there is a ThemerrDB candidate and whether a local theme.* already exists. 404
// when the entity does not exist. It never fails because the entity has no destination
// folder (missing media_file) — in that case `ja_tem_local` simply comes out false, along
// with whatever `disponivel` already indicates.
func (s *Server) getEntityThemeCandidato(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	_, disponivel := s.candidatoTemaURL(ctx, id)

	jaTemLocal := false
	if dir, _, _, achouDestino, err := s.resolverDestino(ctx, ent); err == nil && achouDestino {
		_, jaTemLocal = encontrarArquivoTema(dir)
	}

	writeJSON(w, http.StatusOK, themeCandidatoResult{
		Disponivel: disponivel,
		JaTemLocal: jaTemLocal,
	})
}

// themeDownloadResult is the success body of POST .../theme/download.
type themeDownloadResult struct {
	LocalPath string `json:"local_path"`
}

// postEntityThemeDownload handles POST /api/v1/entities/{id}/theme/download (ADR-0025):
// it downloads the cached candidate (media_external, source=themerrdb) through yt-dlp into
// theme.mp3 at the root of the folder — the SAME path that GET /entities/{id}/theme already
// serves. 503 without yt-dlp available; 422 without a candidate; 404 without a destination
// folder (no present media_file); 409 when a local theme.* already exists (downloaded before
// OR placed by hand by the owner) — WITHOUT a `force` parameter in this delivery (the theme
// is singular per folder); 502 when the download itself fails.
func (s *Server) postEntityThemeDownload(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")

	destino, err := s.baixarTemaEntityComOpcoes(r.Context(), id)
	if err != nil {
		respondErroBaixarTema(w, err)
		return
	}
	writeJSON(w, http.StatusOK, themeDownloadResult{LocalPath: destino})
}

// baixarTemaEntityComOpcoes is the PURE implementation (no http.ResponseWriter) of
// "download the theme song for ONE entity": it reads the cached candidate (media_external,
// source=themerrdb — it NEVER queries ThemerrDB live, candidatoTemaURL), resolves the
// destination folder and downloads through yt-dlp into theme.mp3 at the root of the folder —
// the SAME path that GET /entities/{id}/theme already serves. WITHOUT `force`: the theme is
// singular per folder, an already present theme.* is NEVER overwritten. Reused by
// postEntityThemeDownload (it translates the error into an HTTP status through
// respondErroBaixarTema/errors.Is) and by BaixarTemaEntity (trailer_tema_faltantes job and
// automatic scan trigger).
func (s *Server) baixarTemaEntityComOpcoes(ctx context.Context, entityID string) (destino string, err error) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errEntityNaoEncontrada
		}
		return "", err
	}

	if s.ytdlp == nil {
		return "", errYtdlpIndisponivel
	}
	disponivel, _, detalhe := s.ytdlp.Status(ctx)
	if !disponivel {
		if detalhe != "" {
			return "", fmt.Errorf("%w: %s", errYtdlpIndisponivel, detalhe)
		}
		return "", errYtdlpIndisponivel
	}

	urlOrigem, ok := s.candidatoTemaURL(ctx, entityID)
	if !ok {
		return "", errSemCandidatoTema
	}

	dir, _, _, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errSemDestino
	}
	if _, jaExiste := encontrarArquivoTema(dir); jaExiste {
		return "", errTemaJaExiste
	}

	dest := filepath.Join(dir, "theme.mp3")
	if err := s.baixarTemaAtomico(ctx, urlOrigem, dest); err != nil {
		return "", fmt.Errorf("%w: %v", errDownloadFalhou, err)
	}
	return dest, nil
}

// BaixarTemaEntity downloads (best-effort) the theme song of `entityID` -- used by the
// trailer_tema_faltantes job (internal/job/trailertema) and by the automatic scan trigger
// (ADR-0026). The "nothing to do" sentinels (no candidate, already present locally, no
// destination, yt-dlp unavailable, entity vanished) come back as baixado=false, err=nil --
// only a REAL failure (network, database) propagates err.
func (s *Server) BaixarTemaEntity(ctx context.Context, entityID string) (bool, error) {
	_, err := s.baixarTemaEntityComOpcoes(ctx, entityID)
	if err == nil {
		return true, nil
	}
	if trailerTemaErroBenigno(err) {
		return false, nil
	}
	return false, err
}

// respondErroBaixarTema maps the sentinels of baixarTemaEntityComOpcoes to the HTTP status
// of the POST .../theme/download contract: missing entity -> 404; yt-dlp unavailable -> 503;
// no candidate -> 422; no destination folder -> 404; theme already present -> 409; failure
// of the download itself -> 502; any other error (database) -> 500.
func respondErroBaixarTema(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errEntityNaoEncontrada):
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
	case errors.Is(err, errYtdlpIndisponivel):
		writeError(w, http.StatusServiceUnavailable, "ytdlp_indisponivel", err.Error())
	case errors.Is(err, errSemCandidatoTema):
		writeError(w, http.StatusUnprocessableEntity, "sem_candidato",
			"nenhum candidato de tema musical disponível para este item")
	case errors.Is(err, errSemDestino):
		respondErroLocalmedia(w, errSemDestino)
	case errors.Is(err, errTemaJaExiste):
		writeError(w, http.StatusConflict, "ja_existe",
			"tema musical já presente (apague o arquivo manualmente para re-baixar)")
	case errors.Is(err, errDownloadFalhou):
		writeError(w, http.StatusBadGateway, "youtube_download_failed",
			"falha ao baixar o tema musical: "+err.Error())
	default:
		respondErroInterno(w)
	}
}

// baixarTemaAtomico downloads `urlOrigem` (s.ytdlp.Baixar) into a TEMPORARY file in the
// same directory as the final destination and only RENAMES it to the canonical name on
// success — the same principle as baixarTrailerAtomico, adapted: here the URL already
// arrives ready from the cached candidate, with no richfields.Video involved.
func (s *Server) baixarTemaAtomico(ctx context.Context, urlOrigem, destino string) error {
	destinoTemp := destino + ".tmp"
	if err := s.ytdlp.Baixar(ctx, urlOrigem, destinoTemp); err != nil {
		_ = os.Remove(destinoTemp)
		return err
	}
	if err := localmedia.RenomearArquivo(destinoTemp, destino); err != nil {
		_ = os.Remove(destinoTemp)
		return err
	}
	return nil
}
