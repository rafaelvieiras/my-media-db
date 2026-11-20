// Package localmedia materializes metadata/artwork into the library's REAL
// folder (see doc 08), following the Kodi/Emby convention: canonical artwork
// file names (poster.jpg, fanartN.jpg, seasonNN-poster.jpg,
// <video>-thumb.jpg), numbered rotation without reindexing when the "main"
// artwork is replaced, and atomic file writing (temp + rename). It is a PURE
// package — no disk I/O and no SQL — except EscreverArquivoAtomico (the only
// function that touches the disk, isolated so the caller can test/mock the
// others without it). The Entity/MediaFile types here are MINIMAL projections
// of sqlcgen.Entity/sqlcgen.MediaFile (id/type and
// path/state/created_at/entity_id): the package does not import
// internal/db/sqlcgen so that the naming decision is not coupled to the
// schema — the caller (internal/server) converts.
package localmedia

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Entity types relevant to DiretorioDestino/NomeArquivoArte — they mirror the
// constants of the same name in internal/server/entities.go (tipoEntSerie/
// tipoEntTemporada/tipoEntEpisodio), duplicated here because this package does
// not import internal/server (avoids a cycle — server imports localmedia).
const (
	TipoSerie     = "tv"
	TipoTemporada = "season"
	TipoEpisodio  = "episode"
)

// Artwork kinds known by the kind->base-name table of the artwork naming
// convention (see doc 08). They mirror
// (without importing) the kindX constants of internal/server/assets.go.
const (
	KindPoster    = "poster"
	KindFanart    = "fanart"
	KindBackdrop  = "backdrop" // INPUT synonym of KindFanart (fanart.tv/TMDB use both terms)
	KindBanner    = "banner"
	KindClearlogo = "clearlogo"
	KindDiscart   = "discart"
	KindLandscape = "landscape"
	KindThumb     = "thumb" // still/thumb of an EPISODE
	// KindSeasonPoster is a SYNTHETIC kind — it does not exist in asset.kind
	// (which stays 'poster' for a season): it tells NomeArquivoArte/
	// IndiceRotacaoLivre that the target is the POSTER OF A SEASON (Kodi
	// convention season{NN}-poster.jpg, always in the series root, never
	// prefixed by the video). The caller (handler) uses this kind ONLY when
	// the entity is a season AND the asset kind is poster; in every other
	// case the kind comes straight from asset.Kind.
	KindSeasonPoster = "season_poster"
	// KindTrailer is the kind of the trailer materialized on disk (ADR-0025)
	// — it is NOT an `asset` kind (it does not enter the `asset` table, see
	// the note in internal/server/localmedia_handlers.go): it is an UNKNOWN
	// kind for baseDeKind, so baseComPrefixo already falls into the
	// `base = kind` fallback (line ~100 below), producing
	// `<videoBaseName>-trailer.<ext>` (non-exclusive folder) or
	// `trailer.<ext>` (exclusive folder) without needing any new rule.
	KindTrailer = "trailer"
)

// baseDeKind is the FIXED kind->base-name table of the artwork naming
// convention (see doc 08). The special
// kinds (episode thumb, season poster) are handled apart in baseComPrefixo —
// they do not enter here.
var baseDeKind = map[string]string{
	KindPoster:    "poster",
	KindFanart:    "fanart",
	KindBackdrop:  "fanart", // input synonym
	KindBanner:    "banner",
	KindClearlogo: "clearlogo",
	KindDiscart:   "discart",
	KindLandscape: "landscape",
}

// extPadraoDeKind are the exceptions to the default .jpg extension (see
// doc 08). KindTrailer (ADR-0025) uses .mp4 as the VIDEO default — the final
// name written by the download itself is always this one (fixed destination,
// see postEntityTrailerDownload); detecting a trailer already present in the
// folder (encontrarArquivoTrailer) tests additional extensions (.mkv/.webm) for
// the case where the owner put the file there manually with another container.
var extPadraoDeKind = map[string]string{
	KindClearlogo: ".png",
	KindDiscart:   ".png",
	KindTrailer:   ".mp4",
}

// ExtensaoPadrao returns the DEFAULT extension of the kind (.png for
// clearlogo/discart, .jpg for the others) — the caller uses it when it does
// not know the real extension of the binary yet (e.g.: before downloading).
func ExtensaoPadrao(kind string) string {
	if e, ok := extPadraoDeKind[kind]; ok {
		return e
	}
	return ".jpg"
}

// baseComPrefixo builds the base name (no rotation suffix, no extension) of
// an artwork file, applying the special-kind rules:
//   - episode thumb: ALWAYS prefixed by nomeBaseVideo (no bare variant — an
//     episode is always named after the video), ignoring pastaExclusiva.
//   - season poster: ALWAYS "season{NN}-poster" (here nomeBaseVideo is the
//     "season{NN}" pre-formatted by the caller — NomeSeasonBase), NEVER
//     prefixed by the video, ignoring pastaExclusiva.
//   - other kinds: pastaExclusiva=true -> bare name (<base>); otherwise
//     prefixed by the video (<nomeBaseVideo>-<base>).
func baseComPrefixo(kind string, pastaExclusiva bool, nomeBaseVideo string) string {
	switch kind {
	case KindThumb:
		return nomeBaseVideo + "-thumb"
	case KindSeasonPoster:
		return nomeBaseVideo + "-poster"
	}
	base, ok := baseDeKind[kind]
	if !ok {
		base = kind // unknown kind (extensible, e.g. a future "theme"): use the name itself
	}
	if pastaExclusiva {
		return base
	}
	return nomeBaseVideo + "-" + base
}

// sufixoRotacao returns the numeric suffix of the rotation: "" when it is the
// main artwork (bare name), or the index when it is not (indiceRotacao <= 0
// becomes 1, defensive — it never generates a "0" or negative suffix).
func sufixoRotacao(principal bool, indiceRotacao int) string {
	if principal {
		return ""
	}
	if indiceRotacao <= 0 {
		indiceRotacao = 1
	}
	return strconv.Itoa(indiceRotacao)
}

// NomeArquivoArte builds the NAME (without directory) of the canonical
// artwork file (see doc 08). kind is one of
// the Kind* above (accepts "backdrop" as a synonym of KindFanart); an empty
// ext falls back to the ExtensaoPadrao of the kind.
//
// Rules: episode thumb never has a bare variant (always prefixed by the
// video); season poster is never prefixed (it uses
// nomeBaseVideo="season{NN}", see NomeSeasonBase); the other kinds use the
// bare name when pastaExclusiva AND principal, are prefixed by nomeBaseVideo
// when the folder is not exclusive, and always carry the rotation suffix when
// not principal.
func NomeArquivoArte(kind string, principal bool, indiceRotacao int, pastaExclusiva bool, nomeBaseVideo, ext string) string {
	if ext == "" {
		ext = ExtensaoPadrao(kind)
	}
	return baseComPrefixo(kind, pastaExclusiva, nomeBaseVideo) + sufixoRotacao(principal, indiceRotacao) + ext
}

// ── Music folders (album/artist) ─────────────────────────────────────────
//
// In music there is no video file lending its name to the artwork: the FOLDER already
// identifies the work, and the convention the library has on disk (what Ember/MediaElch
// write and Kodi/Jellyfin read) is a set of BARE names, not the "<video>-poster" shape of
// the video convention (doc 08):
//
//	folder.jpg    album cover / artist picture   cdart.png   disc art
//	backdrop.jpg  artist fanart                  logo.png    (clear)logo
//	banner.jpg    artist banner
//
// Those are the names the music library already uses, so writing "poster.jpg" next to an
// existing "folder.jpg" would duplicate the same artwork under two names instead of
// converging with what the owner has.
//
// Both spellings of the two kinds the artwork aggregator uses (assets.go: "logo", "disc")
// and the ones of this package (KindClearlogo, KindDiscart) are mapped to the same name.
var baseMusicaDeKind = map[string]string{
	KindPoster:    "folder",
	KindFanart:    "backdrop",
	KindBackdrop:  "backdrop",
	KindBanner:    "banner",
	KindClearlogo: "logo",
	"logo":        "logo",
	KindDiscart:   "cdart",
	"disc":        "cdart",
	KindLandscape: "landscape",
	KindThumb:     "thumb",
}

// baseMusica is the kind->bare-name lookup of a music folder; an unknown kind keeps its
// own name (extensible, e.g. a future "folder-artist").
func baseMusica(kind string) string {
	if base, ok := baseMusicaDeKind[kind]; ok {
		return base
	}
	return kind
}

// NomeArquivoArteMusica builds the NAME (without directory) of an artwork file in a MUSIC
// folder (album/artist). Unlike NomeArquivoArte there is no pastaExclusiva decision: a
// music folder IS the work's own folder, so the name is always the bare one; the rotation
// suffix still applies to a non-principal artwork (folder2.jpg, folder3.jpg...), with the
// same never-reindex rule of doc 08.
func NomeArquivoArteMusica(kind string, principal bool, indiceRotacao int, ext string) string {
	if ext == "" {
		ext = ExtensaoPadrao(kind)
	}
	return baseMusica(kind) + sufixoRotacao(principal, indiceRotacao) + ext
}

// IndiceRotacaoLivreMusica is IndiceRotacaoLivre for the music naming: same next-free-index
// rule, with the prefix coming from baseMusica instead of baseComPrefixo.
func IndiceRotacaoLivreMusica(caminhosExistentes []string, kind, ext string) int {
	if ext == "" {
		ext = ExtensaoPadrao(kind)
	}
	prefixo := baseMusica(kind)
	maior := 0
	for _, caminho := range caminhosExistentes {
		nome := strings.TrimSuffix(filepath.Base(caminho), ext)
		sufixo := strings.TrimPrefix(nome, prefixo)
		if sufixo == "" || sufixo == nome {
			continue // bare name (index 0) or it does not match the prefix
		}
		n, err := strconv.Atoi(sufixo)
		if err != nil || n <= 0 {
			continue
		}
		if n > maior {
			maior = n
		}
	}
	return maior + 1
}

// NomeSeasonBase formats the "season{NN}" (NN = season_number zero-padded to 2
// digits) that NomeArquivoArte expects in nomeBaseVideo when kind==
// KindSeasonPoster — the single source of that format.
func NomeSeasonBase(seasonNumber int) string {
	return "season" + pad2(seasonNumber)
}

// pad2 zero-pads a non-negative integer to 2 digits (season_number is usually
// 0-99; above that it overflows without breaking — it just loses the
// zero-padding).
func pad2(n int) string {
	s := strconv.Itoa(n)
	if len(s) < 2 {
		return strings.Repeat("0", 2-len(s)) + s
	}
	return s
}

// IndiceRotacaoLivre computes the NEXT FREE numeric suffix (highest index
// already used among `caminhosExistentes` plus 1 — it NEVER reindexes/compacts
// holes, see doc 08) for the base-name+ext of the given kind.
// `caminhosExistentes` are the `local_path` (or just the file name) of the
// OTHER assets already materialized for the same (entity,kind) — the caller
// lists them from the database; this package accesses neither the disk nor the
// database. It starts at 1 when none of them has a numeric suffix yet (the bare
// name does not count as index 0 for this calculation).
func IndiceRotacaoLivre(caminhosExistentes []string, kind string, pastaExclusiva bool, nomeBaseVideo, ext string) int {
	if ext == "" {
		ext = ExtensaoPadrao(kind)
	}
	prefixo := baseComPrefixo(kind, pastaExclusiva, nomeBaseVideo)
	maior := 0
	for _, caminho := range caminhosExistentes {
		nome := strings.TrimSuffix(filepath.Base(caminho), ext)
		sufixo := strings.TrimPrefix(nome, prefixo)
		if sufixo == "" || sufixo == nome {
			continue // bare name (index 0, does not count) or it does not match the prefix
		}
		n, err := strconv.Atoi(sufixo)
		if err != nil || n <= 0 {
			continue
		}
		if n > maior {
			maior = n
		}
	}
	return maior + 1
}

// Entity is the MINIMAL projection of sqlcgen.Entity that DiretorioDestino
// needs.
type Entity struct {
	ID   string
	Type string
}

// MediaFile is the MINIMAL projection of sqlcgen.MediaFile that
// DiretorioDestino/EhPastaExclusiva need. CreatedAt is the ISO8601 timestamp
// (strftime('%Y-%m-%dT%H:%M:%fZ')) used by the rest of the database —
// lexicographically comparable without parsing.
type MediaFile struct {
	EntityID  string
	Path      string
	State     string
	CreatedAt string
}

const estadoPresente = "present"

// DiretorioDestino computes the DESTINATION DIRECTORY (the library's real
// folder) of an entity, following the artwork/materialization convention
// (see doc 08) as corrected by the series root rule (ADR-0021 — the previous
// version used the directory of the oldest media_file, which in a series tree
// with per-season subfolders lands INSIDE `Season NN/`, never on the real
// root):
//
//   - it resolves the target (the entity itself, or the "tv" ancestor for a
//     season — the same resolution as always, see resolverAlvo) and gathers
//     the directories (filepath.Dir) of ALL the 'present' media_file of that
//     target, without duplicating.
//   - it computes the COMMON ANCESTOR (common path prefix) of those
//     directories — in a series with 2+ SIBLING season folders, the common
//     prefix already falls naturally on the series root (see the series root
//     rule, ADR-0021).
//   - edge case: a series with a single season (or every present file
//     concentrated in one subfolder) still makes the common prefix fall on
//     the season folder itself — it detects that by the NAME of the resulting
//     folder (regexPastaTemporada, tolerant of "Season 01"/"S01"/etc.) and
//     goes up one more level, repeating while the name still matches
//     (defensive against nesting).
//
// ok=false when there is not a single eligible present media_file (no
// destination folder — 404 in the handler).
//
// The "go up one more level" fallback (regexPastaTemporada) only applies to
// tv/season — it is the resolution of the SERIES ROOT that can collide with a
// season subfolder. For movie/episode ("leaf" types), the destination folder
// LEGITIMATELY is a folder named "Season NN" (the Kodi convention of the
// episode itself) — applying the same fallback there would incorrectly go up
// to the series root.
func DiretorioDestino(entity Entity, mediaFiles []MediaFile, ancestors []Entity) (string, bool) {
	dirs := diretoriosPresentesDoAlvo(entity, mediaFiles, ancestors)
	if len(dirs) == 0 {
		return "", false
	}
	raiz := prefixoComumDeDiretorios(dirs)
	if entity.Type == TipoSerie || entity.Type == TipoTemporada {
		raiz = subirEnquantoPastaDeTemporada(raiz)
	}
	return raiz, true
}

// regexPastaTemporada matches season folder names (Kodi/Jellyfin, see the
// series root rule, ADR-0021): "Season 1", "Season 01", "season12" (with or
// without a space) or the short form "S01"/"s1" — tolerant of zero-padding
// and case variation.
var regexPastaTemporada = regexp.MustCompile(`(?i)^season\s*\d+$|^s\d{1,2}$`)

// diretoriosPresentesDoAlvo gathers, in order of appearance and without
// duplicating, the directories (filepath.Dir) of every 'present' media_file of
// the target resolved for `entity` (resolverAlvo).
func diretoriosPresentesDoAlvo(entity Entity, mediaFiles []MediaFile, ancestors []Entity) []string {
	alvo := resolverAlvo(entity, ancestors)
	vistos := make(map[string]bool, len(mediaFiles))
	var dirs []string
	for _, mf := range mediaFiles {
		if mf.EntityID != alvo || mf.State != estadoPresente {
			continue
		}
		dir := filepath.Dir(mf.Path)
		if !vistos[dir] {
			vistos[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// subirEnquantoPastaDeTemporada goes up one level from `raiz` while the folder
// NAME still looks like a season folder (regexPastaTemporada) — see
// DiretorioDestino (only called for tv/season).
func subirEnquantoPastaDeTemporada(raiz string) string {
	for raiz != "" && regexPastaTemporada.MatchString(filepath.Base(raiz)) {
		pai := filepath.Dir(raiz)
		if pai == raiz {
			break // already at the filesystem root -- nothing left to go up (defensive)
		}
		raiz = pai
	}
	return raiz
}

// prefixoComumDeDiretorios computes the path prefix common to every directory
// in `dirs`, comparing by path SEGMENT (never by raw substring, which would
// cut folder names in half). A single directory returns itself.
func prefixoComumDeDiretorios(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	comuns := strings.Split(filepath.Clean(dirs[0]), string(filepath.Separator))
	for _, d := range dirs[1:] {
		comuns = prefixoComumDeSegmentos(comuns, strings.Split(filepath.Clean(d), string(filepath.Separator)))
	}
	raiz := strings.Join(comuns, string(filepath.Separator))
	if raiz == "" {
		raiz = string(filepath.Separator)
	}
	return raiz
}

// prefixoComumDeSegmentos returns the common prefix of two slices of path
// segments (positional comparison, stopping at the first divergent one).
func prefixoComumDeSegmentos(a, b []string) []string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// resolverAlvo returns the id of the entity whose media_file must be
// considered for DiretorioDestino/ArquivoMaisAntigo: the entity itself, except
// for season, which uses the "tv" ancestor (the series has no media_file of
// its own — every episode file points entity_id at IT, ADR-0021).
func resolverAlvo(entity Entity, ancestors []Entity) string {
	if entity.Type == TipoTemporada {
		if serieID, ok := serieAncestral(ancestors); ok {
			return serieID
		}
	}
	return entity.ID
}

// ArquivoMaisAntigo returns the oldest 'present' media_file (created_at asc)
// eligible for the entity (the same target resolution that DiretorioDestino
// uses — the entity itself, or the "tv" ancestor for a season). ok=false when
// none is eligible. Exposed separately from DiretorioDestino because the
// caller of the NFO/video file name also needs ONE representative video (base
// name), not only the directory — since the series root rule, DiretorioDestino
// no longer uses this result internally (the real series root may not be the
// directory of this file).
func ArquivoMaisAntigo(entity Entity, mediaFiles []MediaFile, ancestors []Entity) (MediaFile, bool) {
	alvo := resolverAlvo(entity, ancestors)
	var maisAntigo MediaFile
	achou := false
	for _, mf := range mediaFiles {
		if mf.EntityID != alvo || mf.State != estadoPresente {
			continue
		}
		if !achou || mf.CreatedAt < maisAntigo.CreatedAt {
			maisAntigo, achou = mf, true
		}
	}
	return maisAntigo, achou
}

// serieAncestral finds the id of the closest ancestor of type "tv" (the
// series) in the ROOT->PARENT trail (ancestors). false when the trail has no
// series (defensive — a season without a series in the middle of an
// incomplete migration, etc.).
func serieAncestral(ancestors []Entity) (string, bool) {
	for i := len(ancestors) - 1; i >= 0; i-- {
		if ancestors[i].Type == TipoSerie {
			return ancestors[i].ID, true
		}
	}
	return "", false
}

// NomeBaseVideo returns the video file name WITHOUT extension (to prefix
// artwork/NFO): filepath.Base(path) minus filepath.Ext(path).
func NomeBaseVideo(path string) string {
	nome := filepath.Base(path)
	return strings.TrimSuffix(nome, filepath.Ext(nome))
}

// EhPastaExclusiva says whether `dir` is the folder of a SINGLE video
// (media_file.path) of ANY entity — a folder with 1 isolated movie/episode
// uses the bare name; a folder with 2+ videos (multiple editions of the same
// entity, or distinct entities in the same folder) uses the name prefixed by
// the video. It only counts media_files with state=present ("ghost"/missing
// files do not really occupy the folder). `mediaFiles` is the WHOLE relevant
// base (from any entity) that the caller already loaded — this package does
// not query the database.
func EhPastaExclusiva(dir string, mediaFiles []MediaFile) bool {
	n := 0
	for _, mf := range mediaFiles {
		if mf.State != estadoPresente {
			continue
		}
		if filepath.Dir(mf.Path) == dir {
			n++
			if n > 1 {
				return false
			}
		}
	}
	return n == 1
}

// PastaExclusivaPorContagem is the SQL-backed form of EhPastaExclusiva: the same rule
// ("exactly ONE present media file sits directly in the folder"), answered with a count of
// that folder instead of the whole library in a slice. Keeping the bare number 1 in one
// place is the point -- the two forms must never disagree.
func PastaExclusivaPorContagem(n int) bool { return n == 1 }

// EscreverArquivoAtomico writes `dados` to `destino` through a temporary file
// in the SAME directory (suffix ".tmp-<random>") + os.Rename — the only I/O
// point of this package. It never lets a reader see a partial write nor leaves
// an orphan `.tmp-*` behind on failure (the temporary file is removed on any
// error before a successful rename, see doc 09). It creates the destination
// directory if it does not exist yet (same pattern as
// gravarBinarioAsset/escreverAtomico in internal/server, duplicated here so the
// package does not depend on internal/server).
func EscreverArquivoAtomico(destino string, dados []byte) error {
	dir := filepath.Dir(destino)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(destino)+".tmp-*")
	if err != nil {
		return err
	}
	nome := tmp.Name()
	if _, err := tmp.Write(dados); err != nil {
		_ = tmp.Close()
		_ = os.Remove(nome)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(nome)
		return err
	}
	if err := os.Rename(nome, destino); err != nil {
		_ = os.Remove(nome)
		return err
	}
	return nil
}

// RenomearArquivo renames `origem` to `destino` (same directory, artwork
// rotation): always os.Rename, never copies the bytes again (see doc 08).
// Exposed as a named function (instead of the caller using os.Rename directly)
// only so that the package concentrates every artwork file manipulation in a
// single place.
func RenomearArquivo(origem, destino string) error {
	return os.Rename(origem, destino)
}

// NomeArquivoNFO decides the NAME of the .nfo (see doc 09): a fixed
// "tvshow.nfo" for a series (in the root of the series folder);
// "<nomeBaseVideo>.nfo" for the other types that have a video of their own
// (movie/episode).
func NomeArquivoNFO(entityType, nomeBaseVideo string) string {
	if entityType == TipoSerie {
		return "tvshow.nfo"
	}
	return nomeBaseVideo + ".nfo"
}
