package scanner

// musica.go is the core of the MUSIC scanner (ADR-0027/ADR-0029): when the
// library is `content_type='music'`, every audio file is ONE track. The flow, per
// file, in the order in which it tries to resolve (it stops at the first one that
// resolves):
//
//  1. Track (recording) MBID embedded in the tag -> ObterEntityPorMbid directly
//     (the file was already cataloged before, or another file of the same album
//     already brought this track in this same execution or in a previous one).
//  2. Track MBID with NO match yet, but the tag ALSO has the artist MBID ->
//     SCOPED import (ADR-0029, enrich.ImportarFaixaPossuida): ONLY the artist
//     (identity) + the ALBUM of that track + the track itself, idempotent --
//     NEVER the whole discography -- and it tries the lookup again.
//  3. Track MBID WITHOUT an artist MBID in the tag -> REVERSE resolution
//     (provider.FaixaResolverProvider) track->artist, then it falls into the same
//     path as step 2.
//  4. WITHOUT ANY embedded MBID (the DOMINANT case in the owner's real files --
//     his personal scripts do not write that tag): textual search of the artist
//     (album_artist, with a fallback to artist) in the provider; a single
//     candidate strong enough triggers the SAME scoped import of step 2, but
//     through the TEXTUAL path (album by name via
//     MusicaAlbumProvider.BuscarAlbumPorNome, track by title/track_number inside
//     it) -- never the whole tree.
//  4.5. WITHOUT a plausible textual match: LAST fallback before the draft. It
//     computes the acoustic fingerprint of the FILE (fpcalc/Chromaprint,
//     subprocess -- internal/fingerprint) and queries the optional capability
//     provider.FingerprintProvider (today only internal/provider/acoustid) --
//     finding a `recording` MBID with confidence >=
//     matching.LimiarConfiancaFingerprint, it reuses the SAME scoped import path
//     of steps 2/3 (importarEAcharPorMbid), NEVER the whole discography. fpcalc
//     missing, a provider without the capability, or a candidate too weak:
//     degrades silently, falls to step 5.
//  5. WITHOUT ANY PLAUSIBLE MATCH: it creates a DRAFT `faixa` entity
//     (status=draft), with only the tag data (or the file name) -- WITHOUT an
//     artist/album entity_relation, it stays loose until a manual correction (the
//     same spirit as acharOuCriarDraftSerie, but without find-or-create by title:
//     every unmatched file becomes its own draft).
//
// The import of the WHOLE tree (complete discography) still exists
// (enrich.Importar, tipo="artista") but it is only triggered by the owner's
// deliberate MANUAL import (search -> import artist) -- never by this scanner.
//
// ALL of this runs in factsMusica (FACTS phase, called by computarFacts in
// scan.go), OUTSIDE the write transaction of the batch -- the enrich import uses
// its own write connection, so no nested transaction (same reason as the series
// path, see serie.go). The file<->track link is 1:1 (media_file.entity_id points
// DIRECTLY to the track, ADR-0027 item 5) -- NEVER the N:M junction of series
// (media_file_entity); gravarArquivoMusica (below) mirrors gravarArquivo of movie
// (match.go), not gravarArquivoSerie.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// contentTypeMusic is the value of library_source.content_type that turns on
	// the music flow (it mirrors contentTypeSeries in serie.go).
	contentTypeMusic = "music"
	// tipoArtista/tipoFaixa are the entity.type of the music vertical that this
	// file creates/queries directly (ADR-0027; they mirror the equivalent private
	// constants of internal/enrich/musica.go, which are not exported -- see
	// AGENTS.md, "do not duplicate creation logic", but the type LITERALS
	// themselves are not logic).
	tipoArtista = "artista"
	tipoFaixa   = "faixa"
	// sourceMusicBrainz is the Nome() of the source used for the `source`
	// parameter of MusicaImportador.Importar (musicbrainz.go: nomeFonte, same
	// value).
	sourceMusicBrainz = "musicbrainz"
	// confMusicaTextual is the confidence of the match by textual search (step 4)
	// -- the same degree of "high guess, not a proof of identity" that
	// confTituloAno has for movie (match.go): an artist name matching the source
	// exactly is a good signal, but it is not an embedded id.
	confMusicaTextual = confTituloAno
	// confFingerprint is the confidence of the match by acoustic fingerprint (step
	// 4.5) -- by principle, STRICTLY below confMusicaTextual (an acoustic
	// similarity score is not a proof of identity as strong as an MBID embedded
	// by the owner NOR as a name matching exactly in the textual search) and ABOVE
	// confDraft (it is still a usable signal, not a guess). It reuses
	// confTituloSoNom (movie match by name only, without year confirmation)
	// instead of inventing a third magic number -- the same spirit of
	// confMusicaTextual above.
	confFingerprint = confTituloSoNom
)

// factsMusica assembles the facts of an audio file: it extracts the tags (via the
// mediainfo_json already computed by hashEMediainfo) and resolves the FAIXA entity
// through the matching pipeline above. It does not write the media_file (that
// belongs to the batch transaction, see gravarArquivoMusica) -- it only resolves.
//
// INCREMENTALITY: when a media_file for this SAME path already exists with
// mtime/size EXACTLY equal to the current ones (the same
// mediaFileInalteradoPorPath short-circuit that hashEMediainfo already uses so as
// not to re-hash/re-probe), it reuses the entity_id and the confidence ALREADY
// STORED without running the matching pipeline again -- it never calls Importar
// (network) nor the textual search again on a re-scan with no change at all to
// the file.
func (r *scanRun) factsMusica(ctx context.Context, path string, mi []byte, size int64, mtime string) fileFacts {
	f := fileFacts{path: path, isMusica: true, folderDir: filepath.Dir(path), rawNome: semExtensao(filepath.Base(path))}

	if existente, ok := r.mediaFileInalteradoPorPath(ctx, path, size, mtime); ok {
		f.faixaID = existente.EntityID
		f.faixaConf = existente.MatchConfidence.Float64
		return f
	}

	// The file CHANGED (or is new). If it was already tried and stayed a draft
	// inside the window, keep the draft without spending a single request: a file
	// the source does not have does not become findable by asking again tomorrow,
	// and each attempt costs MusicBrainz calls (1 req/s) plus a fingerprint.
	if id, ok := r.draftRecente(ctx, path); ok {
		f.faixaID = id
		f.faixaConf = confDraft
		return f
	}

	var tags mediainfo.AudioTags
	var duracaoSegundos float64
	if len(mi) > 0 {
		var info mediainfo.Info
		if err := json.Unmarshal(mi, &info); err == nil {
			if info.Tags != nil {
				tags = *info.Tags
			}
			if info.General.Duration > 0 {
				duracaoSegundos = info.General.Duration
			}
		}
	}

	f.faixaID, f.faixaConf = r.resolverFaixa(ctx, path, tags, duracaoSegundos, f.rawNome)
	if f.faixaConf == confDraft && f.faixaID != "" {
		// Stamps the attempt (the same column the retroactive sweep uses), so the
		// next scan of a CHANGED file does not pay the network again inside the
		// window (draftRecente above).
		r.carimbarTentativaFaixa(ctx, f.faixaID)
	}
	return f
}

// janelaNovaTentativaDraft is how long a file that ended in DRAFT is left alone
// before the scanner tries the network for it again. A week: a track the source
// does not have does not appear overnight, and every attempt costs MusicBrainz
// calls (1 req/s) plus fpcalc reading the whole file. Files with UNCHANGED
// mtime/size never reach here at all (the incremental short-circuit above).
const janelaNovaTentativaDraft = 7 * 24 * time.Hour

// draftRecente reports the entity of the track this path already points to when
// it is a DRAFT whose last external attempt is inside janelaNovaTentativaDraft.
func (r *scanRun) draftRecente(ctx context.Context, path string) (string, bool) {
	q := sqlcgen.New(r.m.db.Read)
	existente, err := q.ObterMediaFilePorPath(ctx, path)
	if err != nil || existente.EntityID == "" {
		return "", false
	}
	ent, err := q.ObterEntity(ctx, existente.EntityID)
	if err != nil || ent.Status != "draft" {
		return "", false
	}
	if !ent.LastExternalMatchAttemptAt.Valid || ent.LastExternalMatchAttemptAt.String == "" {
		return "", false
	}
	tentativa, err := time.Parse(time.RFC3339, ent.LastExternalMatchAttemptAt.String)
	if err != nil {
		return "", false
	}
	if r.m.agora().Sub(tentativa) < janelaNovaTentativaDraft {
		return ent.ID, true
	}
	return "", false
}

// carimbarTentativaFaixa records that the network was tried for this track
// (best-effort: a failure here only costs a retry later, it never breaks the scan).
func (r *scanRun) carimbarTentativaFaixa(ctx context.Context, faixaID string) {
	if err := sqlcgen.New(r.m.db.Write).AtualizarTentativaMatchExterno(ctx, faixaID); err != nil {
		log.Printf("scanner: carimbo da tentativa de match da faixa %s falhou: %v", faixaID, err)
	}
}

// resolverFaixa runs the matching pipeline described in the package comment, in
// order: track MBID directly -> track MBID + import (with or without an embedded
// artist MBID) -> textual search -> acoustic fingerprint -> draft. It always
// returns a usable faixaID (step 5 never fails, except on a database error -- in
// that case "" and the caller logs and skips the file, see gravarArquivoMusica).
// path is the path of the file on disk, needed only by step 4.5 (fpcalc reads the
// raw audio, not the tags already extracted). duracaoSegundos is the duration of
// the local file extracted by the probe (info.General.Duration, factsMusica
// above), 0 when unknown -- passed along as an ADDITIONAL CONFIRMATION signal by
// duration (matching.ToleranciaDuracaoSegundos) down to the enrich.FaixaAlvo
// built at the scoped import points below
// (importarEAcharPorMbid/matchTextualFaixa/matchPorFingerprint); never used here
// to decide anything on its own.
func (r *scanRun) resolverFaixa(ctx context.Context, path string, tags mediainfo.AudioTags, duracaoSegundos float64, rawNome string) (string, float64) {
	langs := r.langsPermitidos(ctx)

	if tags.MusicBrainzTrackID != "" {
		if id, ok := r.acharFaixaPorMbid(ctx, tags.MusicBrainzTrackID); ok {
			return id, confIDExterno
		}
		if id, ok := r.importarEAcharPorMbid(ctx, tags, duracaoSegundos, langs); ok {
			return id, confIDExterno
		}
	}

	// The album of this file was already resolved in THIS scan (another file of the
	// same album came first): the track is picked from the tracklist already in
	// memory and created locally -- zero requests (ADR-0042).
	if id, ok := r.matchPorAlbumEmCache(ctx, tags, duracaoSegundos, langs); ok {
		return id, confMusicaTextual
	}

	if id, ok := r.matchTextual(ctx, tags, duracaoSegundos, langs); ok {
		return id, confMusicaTextual
	}

	if id, ok := r.matchPorFingerprint(ctx, path, duracaoSegundos, langs); ok {
		return id, confFingerprint
	}

	return r.criarFaixaDraft(ctx, tags, rawNome), confDraft
}

// acharFaixaPorMbid is the PURE lookup (no network) by recording MBID -- the
// cheapest path: the file was already cataloged before, or the same file
// (renamed/moved) already resolved this track before in this same execution.
func (r *scanRun) acharFaixaPorMbid(ctx context.Context, trackMBID string) (string, bool) {
	e, err := sqlcgen.New(r.m.db.Read).ObterEntityPorMbid(ctx, nsOpt(trackMBID))
	if err != nil {
		return "", false
	}
	return e.ID, true
}

// importarEAcharPorMbid resolves the MBID of the ARTIST (embedded in the tag, or
// via reverse resolution track->artist when only the track MBID is present) and
// fires the SCOPED import (ADR-0029, enrich.ImportarFaixaPossuida): only the
// artist (identity) + the ALBUM of that track (resolved by the recording MBID via
// MusicaAlbumProvider.BuscarAlbumDaFaixa) + the track itself -- NEVER the whole
// discography. ok=false on any degradation (no importer configured, no resolvable
// artist MBID, or the scoped import failed -- no plausible match in the source, no
// support for the selective capability, or a network/database error).
// duracaoSegundos (0 = unknown) is only passed along in the enrich.FaixaAlvo as
// an ADDITIONAL confirmation signal -- it never decides anything on its own in
// this method.
func (r *scanRun) importarEAcharPorMbid(ctx context.Context, tags mediainfo.AudioTags, duracaoSegundos float64, langs []string) (string, bool) {
	if r.m.musicaImportador == nil {
		return "", false
	}
	artistaMBID, ok := r.resolverArtistaMBID(ctx, tags)
	if !ok {
		return "", false
	}
	alvo := enrich.FaixaAlvo{RecordingMBID: tags.MusicBrainzTrackID, DuracaoSegundos: duracaoSegundos}
	faixaID, _, err := r.m.musicaImportador.ImportarFaixaPossuida(ctx, sourceMusicBrainz, artistaMBID, alvo, langs)
	if err != nil {
		log.Printf("scanner: import escopado da faixa %s (artista %s) falhou: %v", tags.MusicBrainzTrackID, artistaMBID, err)
		return "", false
	}
	return faixaID, faixaID != ""
}

// resolverArtistaMBID returns the MBID of the artist: straight from the tag when
// present, or via the optional capability provider.FaixaResolverProvider (reverse
// resolution from the track MBID) when absent -- the case of step 3 of the
// pipeline (a tagger only writes the recording MBID).
func (r *scanRun) resolverArtistaMBID(ctx context.Context, tags mediainfo.AudioTags) (string, bool) {
	if tags.MusicBrainzArtistID != "" {
		return tags.MusicBrainzArtistID, true
	}
	if r.m.musicaProvider == nil {
		return "", false
	}
	resolver, ok := r.m.musicaProvider.(provider.FaixaResolverProvider)
	if !ok {
		return "", false
	}
	mbid, err := resolver.ResolverArtistaDaFaixa(ctx, tags.MusicBrainzTrackID)
	if err != nil {
		log.Printf("scanner: resolucao reversa artista<-faixa %s falhou: %v", tags.MusicBrainzTrackID, err)
		return "", false
	}
	return mbid, mbid != ""
}

// matchTextual is the FALLBACK without an embedded MBID (the dominant case in the
// owner's real files, see AGENTS.md) -- a thin pass-through to
// Manager.matchTextualFaixa, extracted so it can be reused by the reconciliation
// of draft tracks outside a scanRun (reconciliar.go).
func (r *scanRun) matchTextual(ctx context.Context, tags mediainfo.AudioTags, duracaoSegundos float64, langs []string) (string, bool) {
	faixaID, album, ok := r.m.matchTextualFaixa(ctx, tags, duracaoSegundos, langs)
	if ok && album != nil {
		r.guardarAlbumNoCache(tags, *album)
	}
	return faixaID, ok
}

// chaveDoAlbum is the cache key of an album inside one scan: the album ARTIST plus the
// album NAME, normalized -- the two tags every file of the same album shares. A file
// without album name has no key (it cannot be grouped).
func chaveDoAlbum(tags mediainfo.AudioTags) string {
	album := strings.ToLower(strings.TrimSpace(tags.Album))
	if album == "" {
		return ""
	}
	artista := strings.ToLower(strings.TrimSpace(nomeArtistaDoAlbum(tags)))
	return artista + "\x00" + album
}

// guardarAlbumNoCache stores an album already resolved so the NEXT file of it is local.
func (r *scanRun) guardarAlbumNoCache(tags mediainfo.AudioTags, alb enrich.AlbumImportado) {
	if chave := chaveDoAlbum(tags); chave != "" {
		r.albumCache[chave] = alb
	}
}

// matchPorAlbumEmCache is the check BEFORE any request: if this album was already
// resolved in this scan, the track is picked from the tracklist already in memory
// (escolherFaixaAlvo, the same selection the first file of the album used) and
// created locally through the enrich. Zero network.
func (r *scanRun) matchPorAlbumEmCache(ctx context.Context, tags mediainfo.AudioTags, duracaoSegundos float64, langs []string) (string, bool) {
	chave := chaveDoAlbum(tags)
	if chave == "" {
		return "", false
	}
	alb, ok := r.albumCache[chave]
	if !ok {
		return "", false
	}
	alvo := enrich.FaixaAlvo{
		AlbumNome:       tags.Album,
		FaixaTitulo:     tags.Title,
		FaixaNumero:     tags.TrackNumber,
		FaixaDisco:      tags.DiscNumber,
		ArtistaNome:     nomeArtistaDoAlbum(tags),
		DuracaoSegundos: duracaoSegundos,
	}
	return r.m.musicaImportador.ImportarFaixaDesteAlbum(ctx, alb, alvo, langs)
}

// matchTextualFaixa searches the artist by name (album_artist, with a fallback to
// artist) in the provider and, when there is a SINGLE candidate whose title meets
// the matching.LimiarAutoMatch threshold (92%, the SAME threshold already used by
// the movie auto-match -- deliberately reused so as not to invent a second magic
// number) against the name returned by the source, fires the SCOPED import
// (ADR-0029: enrich.ImportarFaixaPossuida) through the TEXTUAL path -- album
// located by name (tags.Album, mandatory here) and the track by
// title/track_number inside it, NEVER the whole tree. Ambiguous (>1 candidate tied
// at the top), no album in the tag, or no candidate strong enough: ok=false (it
// falls to the draft, step 5) -- it never applies a "similar" match. A Manager
// method (not scanRun) so it can be called both from the scan (matchTextual above)
// and from the reconciliation of an orphan draft track (reconciliar.go).
// duracaoSegundos (0 = unknown) is the duration of the local file, passed along
// in the enrich.FaixaAlvo as an ADDITIONAL confirmation signal
// (matching.ToleranciaDuracaoSegundos) -- it never decides anything on its own in
// this method.
func (m *Manager) matchTextualFaixa(ctx context.Context, tags mediainfo.AudioTags, duracaoSegundos float64, langs []string) (string, *enrich.AlbumImportado, bool) {
	if m.musicaProvider == nil || m.musicaImportador == nil {
		return "", nil, false
	}
	if tags.Album == "" {
		return "", nil, false
	}
	alvo := enrich.FaixaAlvo{
		AlbumNome:       tags.Album,
		FaixaTitulo:     tags.Title,
		FaixaNumero:     tags.TrackNumber,
		FaixaDisco:      tags.DiscNumber,
		ArtistaNome:     nomeArtistaDoAlbum(tags),
		DuracaoSegundos: duracaoSegundos,
	}

	if id, album, ok := m.matchPorArtista(ctx, tags, alvo, langs); ok {
		return id, album, true
	}
	// Step 4b: the path by ARTIST did not resolve (name missing, tie without
	// evidence, homonymous artist without this album). It tries the album by
	// TITLE -- the compilation/soundtrack path, where `album_artist` is "Various
	// Artists" and the artist search never returns a single identity.
	return m.matchPorTituloDeAlbum(ctx, alvo, langs)
}

// nomeArtistaDoAlbum is the artist of the ALBUM in the tag: album_artist (the
// correct one for an album/compilation) with a fallback to artist. It is the same
// name used to scope the album search by title.
func nomeArtistaDoAlbum(tags mediainfo.AudioTags) string {
	if tags.AlbumArtist != "" {
		return tags.AlbumArtist
	}
	return tags.Artist
}

// matchPorArtista is the classic step 4: it searches the ARTIST by name and, for
// EACH candidate strong enough (the ones tied at the top, up to
// maxCandidatosArtista), tries the scoped import with the album from the tag. The
// first one that works wins -- a tie stops being a give-up and starts being
// resolved by EVIDENCE (the album that contains the track), without applying
// anything blindly: if no candidate contains the album, it falls to the next step.
func (m *Manager) matchPorArtista(ctx context.Context, tags mediainfo.AudioTags, alvo enrich.FaixaAlvo, langs []string) (string, *enrich.AlbumImportado, bool) {
	nomeArtista := nomeArtistaDoAlbum(tags)
	if nomeArtista == "" {
		return "", nil, false
	}
	resultados, err := m.musicaProvider.Buscar(ctx, nomeArtista, "artista", "")
	if err != nil {
		log.Printf("scanner: busca textual do artista %q falhou: %v", nomeArtista, err)
		return "", nil, false
	}
	candidatos := make([]candidatoTexto, 0, len(resultados))
	for _, res := range resultados {
		candidatos = append(candidatos, candidatoTexto{id: res.SourceID, titulo: res.Title})
	}
	artistas := melhoresCandidatosTexto(nomeArtista, candidatos)
	if len(artistas) == 0 {
		return "", nil, false
	}
	for _, artistaMBID := range artistas {
		if err := ctx.Err(); err != nil {
			return "", nil, false
		}
		faixaID, album, _, err := m.musicaImportador.ImportarFaixaPossuidaComAlbum(ctx, sourceMusicBrainz, artistaMBID, alvo, langs)
		if err != nil {
			log.Printf("scanner: import escopado (artista %s, album %q) falhou: %v", artistaMBID, tags.Album, err)
			continue
		}
		if faixaID != "" {
			return faixaID, album, true
		}
	}
	return "", nil, false
}

// matchPorTituloDeAlbum is step 4b: it resolves the album by TITLE in the source
// and uses its artist-credit (compilations/soundtracks). Silent degradation when
// the source does not implement the capability -- the caller moves on to step 4.5.
func (m *Manager) matchPorTituloDeAlbum(ctx context.Context, alvo enrich.FaixaAlvo, langs []string) (string, *enrich.AlbumImportado, bool) {
	faixaID, album, _, err := m.musicaImportador.ImportarFaixaPorTituloDeAlbumComAlbum(ctx, sourceMusicBrainz, alvo, langs)
	if err != nil {
		log.Printf("scanner: import escopado por titulo de album %q falhou: %v", alvo.AlbumNome, err)
		return "", nil, false
	}
	return faixaID, album, faixaID != ""
}

// candidatoTexto is an (id,title) pair candidate to a textual match of the ARTIST
// (result of Provider.Buscar) -- the textual match of album/track inside the
// already resolved release is the job of the provider
// (musicbrainz.melhorReleaseGroupPorTitulo) and of the enrich
// (melhorFaixaPorTitulo), not of this package.
type candidatoTexto struct {
	id     string
	titulo string
}

// maxCandidatosArtista is the ceiling of TIED artists the pipeline tries per
// track. Each attempt costs calls to MusicBrainz (1 req/s), so the ceiling exists
// to make the worst case predictable; in practice a tie is rare (e.g. two
// "Linkin Park" in the catalog of the source) and 2 or 3 already covers it.
const maxCandidatosArtista = 3

// melhoresCandidatosTexto returns the candidates whose title meets the
// matching.LimiarAutoMatch threshold (92%, 0-100 scale, via
// matching.SimilaridadeNormalizada -- the SAME engine of the movie/series
// auto-match, now tolerant to edition/version qualifiers), from best to worst,
// limited to maxCandidatosArtista.
//
// CHANGE: before, a TIE at the top was treated as "ambiguous, give up" -- which
// threw whole libraries into draft when the source has two artists with the same
// name (a real case: "Linkin Park", score 100 on two candidates). Now the tie
// becomes EVIDENCE to test (does the album of the track exist in one of them?),
// never a blind application: whoever does not contain the album simply does not
// resolve and the pipeline moves on. The threshold remains strict.
func melhoresCandidatosTexto(alvo string, candidatos []candidatoTexto) []string {
	type pontuado struct {
		id    string
		score float64
	}
	var ordenados []pontuado
	for _, c := range candidatos {
		score := matching.SimilaridadeNormalizada(alvo, c.titulo)
		if score < matching.LimiarAutoMatch {
			continue
		}
		ordenados = append(ordenados, pontuado{id: c.id, score: score})
	}
	sort.SliceStable(ordenados, func(i, j int) bool { return ordenados[i].score > ordenados[j].score })

	out := make([]string, 0, maxCandidatosArtista)
	vistos := make(map[string]bool, len(ordenados))
	for _, p := range ordenados {
		if vistos[p.id] {
			continue
		}
		vistos[p.id] = true
		out = append(out, p.id)
		if len(out) == maxCandidatosArtista {
			break
		}
	}
	return out
}

// matchPorFingerprint is step 4.5: the LAST fallback before the draft, fired ONLY
// when step 4 (textual) has already failed -- never before, never in parallel
// (the caller, resolverFaixa, guarantees the order). It computes the acoustic
// fingerprint of the FILE (fpcalc, subprocess -- internal/fingerprint) and queries
// the optional capability provider.FingerprintProvider (today only
// internal/provider/acoustid) against it. Any degradation -- calculator/provider
// not configured, fpcalc unavailable, calculation error, provider without the
// capability, no candidate, or confidence below
// matching.LimiarConfiancaFingerprint -- returns ok=false WITHOUT a fatal error:
// the caller falls to step 5 (draft), the same silent degradation contract of any
// optional capability of the project. Finding a plausible candidate, it reuses the
// SAME SCOPED import path of steps 2/3 (importarEAcharPorMbid, ADR-0029): it
// assembles a SYNTHETIC AudioTags with only the MusicBrainzTrackID of the
// candidate -- ZERO new import logic, the fingerprint only produces a
// RecordingMBID that enters the same pipe steps 2/3 already use (reverse
// resolution artist<-track via provider.FaixaResolverProvider, when necessary, +
// enrich.ImportarFaixaPossuida).
func (r *scanRun) matchPorFingerprint(ctx context.Context, path string, duracaoSegundos float64, langs []string) (string, bool) {
	if r.m.fingerprintCalculador == nil || r.m.fingerprintProvider == nil {
		return "", false
	}
	fpProv, ok := r.m.fingerprintProvider.(provider.FingerprintProvider)
	if !ok {
		return "", false
	}
	if !r.m.fingerprintCalculador.Disponivel() {
		return "", false
	}

	fp, _, err := r.m.fingerprintCalculador.CalcularFingerprint(ctx, path)
	if err != nil {
		log.Printf("scanner: calculo de fingerprint acustico de %q falhou: %v", path, err)
		return "", false
	}

	cand, ok, err := fpProv.IdentificarPorFingerprint(ctx, fp, duracaoSegundos)
	if err != nil {
		log.Printf("scanner: lookup de fingerprint acustico de %q falhou: %v", path, err)
		return "", false
	}
	if !ok || cand.Confianca < matching.LimiarConfiancaFingerprint {
		return "", false
	}

	log.Printf("scanner: fingerprint acustico identificou %q como recording %s (confianca=%.2f)", path, cand.RecordingMBID, cand.Confianca)
	tagsSinteticas := mediainfo.AudioTags{MusicBrainzTrackID: cand.RecordingMBID}
	return r.importarEAcharPorMbid(ctx, tagsSinteticas, duracaoSegundos, langs)
}

// criarFaixaDraft is the final FALLBACK (step 5): it creates the DRAFT `faixa`
// entity (status=draft) with only the available tag data (or the file name, when
// the title tag comes out empty) -- WITHOUT an artist/album entity_relation, it
// stays loose until a manual correction (the generic review/orphan queue already
// covers this, without a new screen). Unlike acharOuCriarDraftSerie (serie.go), it
// does NOT do find-or-create by title: a "track" without a plausible match does
// not have a title unique enough for a safe dedup (two tracks from different
// artists may be named "Intro") -- every unmatched file becomes its own draft. It
// writes through the WRITE connection directly (outside the batch transaction),
// the same pattern of acharOuCriarDraftSerie -- it runs in the facts phase, not in
// the writing phase.
func (r *scanRun) criarFaixaDraft(ctx context.Context, tags mediainfo.AudioTags, rawNome string) string {
	titulo := tags.Title
	if titulo == "" {
		titulo = rawNome
	}
	q := sqlcgen.New(r.m.db.Write)
	id := r.m.novoID()
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: id, Type: tipoFaixa, Title: titulo}); err != nil {
		log.Printf("scanner: falha ao criar faixa rascunho %q: %v", titulo, err)
		return ""
	}
	if _, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID: id, Type: tipoFaixa, Title: titulo, Status: "draft",
	}); err != nil {
		log.Printf("scanner: falha ao rebaixar faixa rascunho %q para draft: %v", titulo, err)
	}
	return id
}

// gravarArquivoMusica persists the audio file: a 1:1 link with the FAIXA already
// resolved by factsMusica (media_file.entity_id points DIRECTLY to it -- ADR-0027
// item 5, same pattern as movie, NEVER the N:M junction of series). Dedup by
// content_hash (rename/move) and by path (content swapped in place) -- it reuses
// reencontrar/atualizarConteudoMudou from match.go, the SAME movie mechanism (no
// new dedup logic).
func (r *scanRun) gravarArquivoMusica(ctx context.Context, q *sqlcgen.Queries, f fileFacts) error {
	if f.faixaID == "" {
		log.Printf("scanner: pulando %q: faixa nao resolvivel (nem rascunho -- provavel falha de escrita)", f.path)
		return nil
	}

	existente, err := q.ObterMediaFilePorContentHash(ctx, nsOpt(f.hash))
	if err == nil {
		return r.reencontrar(ctx, q, existente, f)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	porPath, err := q.ObterMediaFilePorPath(ctx, f.path)
	if err == nil {
		if err := r.atualizarConteudoMudou(ctx, q, porPath, f); err != nil {
			return err
		}
		// In music the entity link is DERIVED from the CONTENT (the tags): a file
		// that changed and now resolves another track has to move its link. The movie
		// path deliberately preserves `entity_id` (the identity comes from the FOLDER,
		// which did not change) -- that is why the update above does not touch it.
		// Without this, a draft file that gets resolved after a retag would keep
		// pointing at the old draft forever.
		if porPath.EntityID != f.faixaID {
			return q.AtualizarMediaFileEntityID(ctx, sqlcgen.AtualizarMediaFileEntityIDParams{
				EntityID:        f.faixaID,
				MatchConfidence: nfFloat(f.faixaConf),
				ID:              porPath.ID,
			})
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID:              r.m.novoID(),
		EntityID:        f.faixaID,
		Path:            f.path,
		ContentHash:     nsOpt(f.hash),
		Size:            niInt(f.size),
		Mtime:           nsOpt(f.mtime),
		MatchConfidence: nfFloat(f.faixaConf),
		MediainfoJson:   nsBytes(f.mediainfo),
		State:           "present",
		LastSeen:        nsOpt(r.scanTS),
		LibrarySourceID: nsOpt(r.source.ID),
	})
	if err != nil {
		return err
	}
	// New track in this pass: it records it so the post-commit can materialize the
	// cover of the resolved album, when the toggle is on. A draft (conf 0) has no
	// parent album, so it enters anyway and the dispatch filters it out.
	r.registrarNovo(entidadeNova{id: f.faixaID, tipo: tipoFaixa, conf: f.faixaConf})
	return nil
}
