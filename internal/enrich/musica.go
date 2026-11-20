package enrich

// musica.go is the MUSIC VERTICAL import branch of the enrich (ADR-0027):
// it materializes the artist→album→track hierarchy from a
// provider.MusicaProvider (MusicBrainz). A model of serie.go, yet shallower (no
// competing orderings): artist/album/track are IDEMPOTENT find-or-create by
// MBID (ObterEntityPorMbid), linked through entity_relation with the kinds of
// internal/musicext. It reuses ligarFilho (serie.go) for the idempotent
// parent→child link — the same dedup mechanism (it checks ListarRelacoesFilhas before inserting).
// "Various Artists" (ADR-0027 item 5) has no special case at all: it falls into the same
// find-or-create by MBID as any other artist.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// tipoArtista/tipoAlbum/tipoFaixa are the entity.type of the music tree
	// (ADR-0027 — no CHECK constraint, values by convention).
	tipoArtista = "artista"
	tipoAlbum   = "album"
	tipoFaixa   = "faixa"
)

// importarArtista imports the TREE of an artist (the whole discography): it fetches the
// tree from the source (optional MusicaProvider capability — error → 502 when absent, the
// same pattern as importarColecao/importarSerie), does the find-or-create of the artist by
// MBID and, for each album, the find-or-create + tracks + cover artwork. The failure of
// ONE album/track degrades (logs and continues) — the partial discography is valid, in the
// same spirit as importarSerie.
func (s *Servico) importarArtista(ctx context.Context, source, sourceID string, langs []string) (string, bool, error) {
	arvore, err := s.arvoreDoArtista(ctx, source, sourceID, langs)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar arvore do artista: %w", err)
	}
	artistaID, criada, err := s.acharOuCriarArtista(ctx, arvore.MBID, arvore.Name, arvore.Bio)
	if err != nil {
		return "", false, err
	}
	// ARTIST artwork inventory (fanart.tv): the same best-effort trigger already used by
	// the album (Cover Art Archive) — it crosses through the external_id_mbid that
	// acharOuCriarArtista has just written. Cover Art Archive does not model an
	// artist photo/background (only release covers), so only fanart.tv (when
	// configured) has a chance of answering here.
	s.enriquecerArtes(ctx, artistaID, tipoArtista, langs)
	for i, alb := range arvore.Albuns {
		s.importarAlbum(ctx, artistaID, arvore.MBID, alb, i, langs)
	}
	return artistaID, criada, nil
}

// arvoreDoArtista validates the provider capability and fetches the raw tree (albums +
// tracks) from the source — without touching any entity. It mirrors arvoreDaSerie (serie.go).
func (s *Servico) arvoreDoArtista(ctx context.Context, source, sourceID string, langs []string) (provider.ArtistaArvore, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return provider.ArtistaArvore{}, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	mp, ok := prov.(provider.MusicaProvider)
	if !ok {
		return provider.ArtistaArvore{}, fmt.Errorf("enrich: fonte %q nao suporta musica", source)
	}
	return mp.BuscarArtistaArvore(ctx, sourceID, langs)
}

// importarAlbum creates (or reuses) the album, links it to the main artist
// (kind=music_artista_principal, sort_index=position in the discography), caches the cover
// (Cover Art Archive, through the same artwork pipeline as movie/series — internal/enrich/
// artes.go) and imports each track. Isolated to keep the gocyclo low; failures degrade
// (logs and continues), the partial discography is valid.
func (s *Servico) importarAlbum(ctx context.Context, artistaID, artistaMBID string, alb provider.AlbumExterno, indice int, langs []string) {
	albumID, _, err := s.acharOuCriarAlbum(ctx, alb)
	if err != nil {
		log.Printf("enrich: album %s do artista %s falhou (segue): %v", alb.MBID, artistaID, err)
		return
	}
	s.ligarFilho(ctx, artistaID, albumID, musicext.KindArtistaPrincipal, indice)
	// Artwork inventory (Cover Art Archive): the same best-effort trigger of
	// enriquecerArtes already used by movie/series — it crosses through the external_id_mbid
	// that acharOuCriarAlbum has just written.
	s.enriquecerArtes(ctx, albumID, tipoAlbum, langs)
	for _, fx := range alb.Faixas {
		s.importarFaixa(ctx, albumID, artistaMBID, fx)
	}
}

// importarFaixa creates (or reuses) the track, links it to the album (kind=music_album,
// sort_index=track_number) and, when the REAL artist of the track (the artist-credit of
// the recording) differs from the album's main artist (feat./collaboration — ADR-0027 item
// 4), creates/reuses that other artist entity and links it ADDITIONALLY through
// kind=music_artista_feat (it does not replace the album's music_artista_principal link).
func (s *Servico) importarFaixa(ctx context.Context, albumID, artistaPrincipalMBID string, fx provider.FaixaExterna) {
	faixaID, _, err := s.acharOuCriarFaixa(ctx, artistaPrincipalMBID, fx)
	if err != nil {
		log.Printf("enrich: faixa %s do album %s falhou (segue): %v", fx.MBID, albumID, err)
		return
	}
	s.ligarFilho(ctx, albumID, faixaID, musicext.KindAlbum, fx.TrackNumber)

	if fx.ArtistaCreditoMBID == "" || fx.ArtistaCreditoMBID == artistaPrincipalMBID {
		return // the track's artist == the album's artist (the common case): no feat
	}
	featID, _, err := s.acharOuCriarArtista(ctx, fx.ArtistaCreditoMBID, fx.ArtistaCreditoNome, "")
	if err != nil {
		log.Printf("enrich: artista feat %s da faixa %s falhou (segue): %v", fx.ArtistaCreditoMBID, faixaID, err)
		return
	}
	s.ligarFilho(ctx, featID, faixaID, musicext.KindArtistaFeat, 0)
}

// acharOuCriarArtista performs the IDEMPOTENT find-or-create of an `artista` entity by
// MBID (ObterEntityPorMbid): it reuses the existing one or creates a new one with the
// current name/bio. It does not rewrite the base on reuse (unlike acharOuCriarTemporada in
// serie.go) — the scope requires only idempotence (never duplicating), not a metadata
// refresh on re-import. "Various Artists" (fixed MBID, ADR-0027 item 5) comes in here
// like any other artist, with no special case.
func (s *Servico) acharOuCriarArtista(ctx context.Context, mbid, nome, bio string) (string, bool, error) {
	if existente, err := s.leitura().ObterEntityPorMbid(ctx, nsValid(mbid)); err == nil {
		return existente.ID, false, nil
	} else if !isNoRows(err) {
		return "", false, err
	}
	titulo := nome
	if titulo == "" {
		titulo = mbid
	}
	extIDs, err := json.Marshal(map[string]string{"mbid": mbid})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              novo,
		Type:            tipoArtista,
		Title:           titulo,
		Description:     nsOrNull(bio),
		ExternalIdsJson: nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	return novo, true, nil
}

// acharOuCriarAlbum performs the IDEMPOTENT find-or-create of an `album` entity by MBID
// (the RELEASE-GROUP MBID — the same id Cover Art Archive consumes). It writes the year
// (release_date) and the core.music@1 extension_data (genre, ADR-0027 item 7 — genre lives
// at the album level, not per track). No refresh on reuse, same note as
// acharOuCriarArtista.
func (s *Servico) acharOuCriarAlbum(ctx context.Context, alb provider.AlbumExterno) (string, bool, error) {
	if existente, err := s.leitura().ObterEntityPorMbid(ctx, nsValid(alb.MBID)); err == nil {
		return existente.ID, false, nil
	} else if !isNoRows(err) {
		return "", false, err
	}
	titulo := alb.Name
	if titulo == "" {
		titulo = alb.MBID
	}
	extIDs, err := json.Marshal(map[string]string{"mbid": alb.MBID})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoAlbum,
		Title:             titulo,
		ReleaseDate:       nsOrNull(alb.Year),
		ExtensionDataJson: nsValid(extensaoAlbum(alb)),
		ExternalIdsJson:   nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	return novo, true, nil
}

// acharOuCriarFaixa performs the IDEMPOTENT find-or-create of a `faixa` entity by MBID
// (the RECORDING MBID — never by number/position, ADR-0027 item 1). It writes the
// core.music@1 extension_data (track_number/disc_number/disc_total/isrc + the feat
// metadata when `artistaPrincipalMBID` differs from the artist-credit of the track, see
// extensaoFaixa). No refresh on reuse, same note as acharOuCriarArtista.
func (s *Servico) acharOuCriarFaixa(ctx context.Context, artistaPrincipalMBID string, fx provider.FaixaExterna) (string, bool, error) {
	if existente, err := s.leitura().ObterEntityPorMbid(ctx, nsValid(fx.MBID)); err == nil {
		return existente.ID, false, nil
	} else if !isNoRows(err) {
		return "", false, err
	}
	titulo := fx.Title
	if titulo == "" {
		titulo = fx.MBID
	}
	extIDs, err := json.Marshal(map[string]string{"mbid": fx.MBID})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoFaixa,
		Title:             titulo,
		ExtensionDataJson: nsValid(extensaoFaixa(artistaPrincipalMBID, fx)),
		ExternalIdsJson:   nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	return novo, true, nil
}

// extensaoAlbum builds the album extension_data: genre, only when the source brought it
// (ADR-0027 item 7 — the genre belongs to the album/artist, never duplicated per track).
func extensaoAlbum(alb provider.AlbumExterno) string {
	payload := map[string]any{}
	if alb.Genre != "" {
		payload["genre"] = alb.Genre
	}
	return envelopeMusica(payload)
}

// extensaoFaixa builds the track extension_data: numbering (track_number/disc_number/
// disc_total, ADR-0027 item 1) + isrc when the source brought it (item 6 — an auxiliary
// identifier, it does not become a column). When the artist-credit of `fx` DIFFERS from the
// main artist `artistaPrincipalMBID` (feat./collaboration, ADR-0027 item 4), it also writes
// the RAW name/MBID of the collaboration (musicext.FeatArtistNameKey/FeatArtistMbidKey,
// ADR-0029 item 4) — read-only METADATA, independent of whether an entity/entity_relation
// music_artista_feat exists (only the full-tree import creates it, importarFaixa above; the
// scoped import of ONE track, ImportarFaixaPossuida, NEVER creates that entity/relation,
// only this metadata).
func extensaoFaixa(artistaPrincipalMBID string, fx provider.FaixaExterna) string {
	payload := map[string]any{
		"track_number": fx.TrackNumber,
		"disc_number":  fx.DiscNumber,
		"disc_total":   fx.DiscTotal,
	}
	if fx.ISRC != "" {
		payload["isrc"] = fx.ISRC
	}
	if fx.ArtistaCreditoMBID != "" && fx.ArtistaCreditoMBID != artistaPrincipalMBID {
		if fx.ArtistaCreditoNome != "" {
			payload[musicext.FeatArtistNameKey] = fx.ArtistaCreditoNome
		}
		payload[musicext.FeatArtistMbidKey] = fx.ArtistaCreditoMBID
	}
	return envelopeMusica(payload)
}

// envelopeMusica wraps the payload in the core.music@1 namespace (internal/musicext) and
// serializes it. A marshal error (unreachable for these simple maps) degrades to "{}".
func envelopeMusica(payload map[string]any) string {
	b, err := json.Marshal(map[string]any{musicext.Namespace: payload})
	if err != nil {
		return "{}"
	}
	return string(b)
}

// FaixaAlvo describes ONE specific track already identified by the scanner (ADR-0029)
// for the SCOPED import of ImportarFaixaPossuida: either the EXACT MBID of the
// recording (RecordingMBID, the precise path — a file with the track MBID embedded),
// or a TEXTUAL identification — AlbumNome (mandatory on this path) + FaixaTitulo/
// FaixaNumero to locate the right track inside the album (fallback with no embedded MBID,
// the same tag the scanner already read for the textual match). RecordingMBID takes
// priority when present; exactly one of the two paths is used by ImportarFaixaPossuida.
type FaixaAlvo struct {
	// RecordingMBID is the known RECORDING MBID (file tag). When present, it
	// resolves the album through MusicaAlbumProvider.BuscarAlbumDaFaixa and locates the track by
	// EXACT MBID — no other track is considered.
	RecordingMBID string
	// AlbumNome is the album name (tag), used only when RecordingMBID is empty —
	// it resolves the album through MusicaAlbumProvider.BuscarAlbumPorNome.
	AlbumNome string
	// FaixaTitulo is the track title (tag), to locate the right track inside the
	// album already resolved by AlbumNome (similarity match, the same threshold as the
	// scanner). Ignored when RecordingMBID is present.
	FaixaTitulo string
	// FaixaNumero is the track_number (tag), a tiebreaker when FaixaTitulo is empty.
	FaixaNumero int
	// FaixaDisco is the disc_number (tag) of the track in the file; 0 = unknown. It only
	// breaks a tie for a repeated title between discs of a reissue.
	FaixaDisco int
	// ArtistaNome is the album artist (the album_artist tag, with a fallback to
	// artist) -- it scopes the album search by title on the compilation/soundtrack path.
	ArtistaNome string
	// DuracaoSegundos is the known duration of the local file, in seconds (tag/
	// probe), when available; 0 = unknown. Used only as an additional CONFIRMATION
	// signal for the title already chosen by melhorFaixaPorTitulo, never for
	// search/tiebreaking.
	DuracaoSegundos float64
}

// ImportarFaixaPossuida is the SCOPED import of ONE track that the scanner matched
// against a REAL file (ADR-0029): it finds-or-creates only the artist (IDENTITY, with no
// album loop — acharOuCriarArtistaIdentidade), only the ALBUM of that track (through the
// selective capability provider.MusicaAlbumProvider, never the whole tree) and only the
// track ITSELF inside it. Unlike importarArtista (whole tree, an EXPLICIT pull by the owner
// through a manual search — it REMAINS untouched, still the only path the generic dispatcher
// Importar(tipo="artista") uses), no other release of the artist is touched, and no
// entity/entity_relation of a featured artist is created automatically (the collaboration
// metadata stays in extension_data, see extensaoFaixa) — it is the AUTOMATIC trigger of the
// scan, which has to be conservative.
func (s *Servico) ImportarFaixaPossuida(ctx context.Context, source, artistaMBID string, alvo FaixaAlvo, langs []string) (string, bool, error) {
	faixaID, _, criada, err := s.ImportarFaixaPossuidaComAlbum(ctx, source, artistaMBID, alvo, langs)
	return faixaID, criada, err
}

// AlbumImportado is the REUSABLE CONTEXT of a scoped import: the album that was already
// fetched from the source plus the entities it produced. The scanner keeps it per scan (keyed
// by the album tags) so that the NEXT file of the same album resolves with ZERO requests --
// album resolution is the expensive part (1-2 MusicBrainz calls at 1 req/s), not the track.
//
// It is a value (not a pointer) so it can live in a map by copy; Album.Faixas is the
// tracklist already in memory, which is exactly what escolherFaixaAlvo needs.
type AlbumImportado struct {
	AlbumID     string
	ArtistaMBID string
	Album       provider.AlbumExterno
}

// ImportarFaixaPossuidaComAlbum is ImportarFaixaPossuida returning the resolved album context
// so the caller can reuse it (see AlbumImportado). Same semantics, same errors.
// ErrAlbumDoArtistaAusente reports the answer "this artist has no album with that
// name in the source". It is a fact of the (artist, album) PAIR, not of a file, so
// a scan may remember it and skip the question for the rest of the folder. A
// network or provider failure is deliberately NOT this error: it must stay
// unanswered, so the next file retries.
var ErrAlbumDoArtistaAusente = errors.New("album do artista ausente na fonte")

// ErrFaixaForaDoAlbum reports "the album exists, this track is not in it". The
// album context is still returned alongside it, so the scan can remember the
// album (its tracklist is what answers the next file) without repeating the
// resolution.
var ErrFaixaForaDoAlbum = errors.New("faixa alvo fora do album")

func (s *Servico) ImportarFaixaPossuidaComAlbum(ctx context.Context, source, artistaMBID string, alvo FaixaAlvo, langs []string) (string, *AlbumImportado, bool, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return "", nil, false, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	ap, ok := prov.(provider.MusicaAlbumProvider)
	if !ok {
		return "", nil, false, fmt.Errorf("enrich: fonte %q nao suporta album seletivo de musica", source)
	}

	alb, err := s.buscarAlbumAlvo(ctx, ap, artistaMBID, alvo, langs)
	if err != nil {
		return "", nil, false, err
	}
	faixaID, albumID, criada, err := s.importarFaixaComAlbum(ctx, prov, artistaMBID, alb, alvo, langs)
	if err != nil {
		if albumID != "" {
			// The album resolved and the tracklist is in hand: hand it back so the scan
			// answers the NEXT file of this album locally (zero requests), instead of
			// re-resolving the album to learn the same thing file after file.
			return faixaID, &AlbumImportado{AlbumID: albumID, ArtistaMBID: artistaMBID, Album: alb}, criada, err
		}
		return faixaID, nil, criada, err
	}
	return faixaID, &AlbumImportado{AlbumID: albumID, ArtistaMBID: artistaMBID, Album: alb}, criada, nil
}

// ImportarFaixaPorTituloDeAlbum is the COMPILATION/SOUNDTRACK path of the scoped import:
// it resolves the album by TITLE (optional capability
// provider.MusicaAlbumPorTituloProvider) and uses the artist-credit that the SOURCE
// attributes to it as the main artist -- on a compilation that is "Various Artists", which
// the artist search of the normal path does not resolve. After that, the rest is the SAME
// scoped import of ADR-0029 (artist + album + the owned track; never the whole discography).
//
// SILENT degradation (ok=false, err=nil) when: the target has no album name, the source does
// not implement the capability, no release-group passes the threshold, or the release-group
// brought no artist-credit. The scanner then follows the pipeline (step 4.5/fingerprint or
// draft) with no error log -- the same semantics as any optional capability of the project.
func (s *Servico) ImportarFaixaPorTituloDeAlbum(ctx context.Context, source string, alvo FaixaAlvo, langs []string) (string, bool, error) {
	faixaID, _, criada, err := s.ImportarFaixaPorTituloDeAlbumComAlbum(ctx, source, alvo, langs)
	return faixaID, criada, err
}

// ImportarFaixaPorTituloDeAlbumComAlbum is ImportarFaixaPorTituloDeAlbum returning the album
// context (see AlbumImportado) so the files after the first one of a compilation resolve
// without a single request.
func (s *Servico) ImportarFaixaPorTituloDeAlbumComAlbum(ctx context.Context, source string, alvo FaixaAlvo, langs []string) (string, *AlbumImportado, bool, error) {
	if alvo.AlbumNome == "" {
		return "", nil, false, nil
	}
	prov, ok := s.provFonte(source)
	if !ok {
		return "", nil, false, nil
	}
	tp, ok := prov.(provider.MusicaAlbumPorTituloProvider)
	if !ok {
		return "", nil, false, nil
	}
	ac, achou, err := tp.BuscarAlbumPorTitulo(ctx, alvo.AlbumNome, provider.AlvoFaixaAlbum{
		RecordingMBID: alvo.RecordingMBID,
		FaixaTitulo:   alvo.FaixaTitulo,
		FaixaNumero:   alvo.FaixaNumero,
		FaixaDisco:    alvo.FaixaDisco,
		ArtistaNome:   alvo.ArtistaNome,
		DuracaoMs:     int(alvo.DuracaoSegundos * 1000),
	}, langs)
	if err != nil {
		return "", nil, false, err
	}
	if !achou || ac.ArtistaMBID == "" {
		return "", nil, false, nil
	}
	faixaID, albumID, criada, err := s.importarFaixaComAlbum(ctx, prov, ac.ArtistaMBID, ac.Album, alvo, langs)
	if err != nil {
		return faixaID, nil, criada, err
	}
	return faixaID, &AlbumImportado{AlbumID: albumID, ArtistaMBID: ac.ArtistaMBID, Album: ac.Album}, criada, nil
}

// importarFaixaComAlbum is the common body of the scoped import (ADR-0029), extracted
// so that it can be reused by BOTH album resolution paths (by artist + album name, and by
// album title on compilations): it guarantees the artist (identity + artwork inventory),
// the album (find-or-create + artwork + artist→album link) and the owned track
// (find-or-create + album→track link). No new logic: it is exactly the sequence
// ImportarFaixaPossuida already executed, now without the "which album" in the middle.
func (s *Servico) importarFaixaComAlbum(ctx context.Context, prov provider.Provider, artistaMBID string, alb provider.AlbumExterno, alvo FaixaAlvo, langs []string) (string, string, bool, error) {
	artistaID, _, err := s.acharOuCriarArtistaIdentidade(ctx, prov, artistaMBID, langs)
	if err != nil {
		return "", "", false, err
	}
	// ARTIST artwork inventory (fanart.tv) — the same best-effort trigger as the album
	// below, see the long note in importarArtista.
	s.enriquecerArtes(ctx, artistaID, tipoArtista, langs)

	albumID, _, err := s.acharOuCriarAlbum(ctx, alb)
	if err != nil {
		return "", "", false, err
	}
	// sort_index=0: the scoped import does not know the position of the album in the
	// complete discography (only the whole tree, importarArtista, has that order) — ligarFilho
	// is idempotent by (parent,child,kind), re-importing does not duplicate the link.
	s.ligarFilho(ctx, artistaID, albumID, musicext.KindArtistaPrincipal, 0)
	s.enriquecerArtes(ctx, albumID, tipoAlbum, langs)

	fx, ok := escolherFaixaAlvo(alb.Faixas, alvo)
	if !ok {
		// albumID goes back with the error on purpose: the ALBUM resolved, only the
		// track is not in it, and the caller can remember the album (same shape as a
		// successful import, minus the track).
		return "", albumID, false, fmt.Errorf("%w: faixa alvo nao encontrada no album %s", ErrFaixaForaDoAlbum, alb.MBID)
	}
	faixaID, criada, err := s.acharOuCriarFaixa(ctx, artistaMBID, fx)
	if err != nil {
		return "", "", false, err
	}
	s.ligarFilho(ctx, albumID, faixaID, musicext.KindAlbum, fx.TrackNumber)
	return faixaID, albumID, criada, nil
}

// ImportarFaixaDesteAlbum imports another OWNED track into an album that is ALREADY RESOLVED
// (AlbumImportado): it reuses the in-memory tracklist to pick the target track exactly as the
// first file of the album did, then find-or-creates the track and its album link.
//
// ZERO network: acharOuCriarFaixa is a local find-by-MBID plus an insert. This is the whole
// point of the album cache -- one album costs 1-2 requests, not 1-2 per track.
//
// Silent degradation (ok=false) when the album tracklist has no track matching the target: the
// caller falls back to its own pipeline, which will resolve the album again if it must.
func (s *Servico) ImportarFaixaDesteAlbum(ctx context.Context, alb AlbumImportado, alvo FaixaAlvo, langs []string) (string, bool) {
	fx, ok := escolherFaixaAlvo(alb.Album.Faixas, alvo)
	if !ok {
		return "", false
	}
	faixaID, _, err := s.acharOuCriarFaixa(ctx, alb.ArtistaMBID, fx)
	if err != nil {
		log.Printf("enrich: faixa %q do album %s (cache da varredura) falhou: %v", fx.Title, alb.Album.MBID, err)
		return "", false
	}
	s.ligarFilho(ctx, alb.AlbumID, faixaID, musicext.KindAlbum, fx.TrackNumber)
	return faixaID, true
}

// acharOuCriarArtistaIdentidade performs the find-or-create of the artist WITHOUT touching
// any album (unlike importarArtista, which requires the whole tree through MusicaProvider):
// it reuses the existing entity by MBID (a local check, no network); without it, it pulls
// only the IDENTITY through the core of the Provider (BuscarDetalhe/MapearDetalhe — the
// same pair any generic import uses for the initial title, 1 cheap call, with NO
// release-groups) and delegates the creation to acharOuCriarArtista.
func (s *Servico) acharOuCriarArtistaIdentidade(ctx context.Context, prov provider.Provider, mbid string, langs []string) (string, bool, error) {
	if existente, err := s.leitura().ObterEntityPorMbid(ctx, nsValid(mbid)); err == nil {
		return existente.ID, false, nil
	} else if !isNoRows(err) {
		return "", false, err
	}
	raw, err := prov.BuscarDetalhe(ctx, mbid, tipoArtista, langs)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar detalhe do artista %s: %w", mbid, err)
	}
	campos, err := prov.MapearDetalhe(raw)
	if err != nil {
		return "", false, fmt.Errorf("enrich: mapear detalhe do artista %s: %w", mbid, err)
	}
	nome, _ := campos["title"].(string)
	return s.acharOuCriarArtista(ctx, mbid, nome, "")
}

// buscarAlbumAlvo chooses between BuscarAlbumDaFaixa (preferential, when the scanner already
// knows the recording MBID) and BuscarAlbumPorNome (textual fallback, when only the album
// name from the tag is available) — the two paths FaixaAlvo models. On the textual path it
// forwards FaixaTitulo/FaixaNumero/DuracaoMs (from alvo.DuracaoSegundos) as
// provider.AlvoFaixaAlbum — the provider can then try alternative releases of the same
// release-group when the default release does not contain the track this import is after.
func (s *Servico) buscarAlbumAlvo(ctx context.Context, ap provider.MusicaAlbumProvider, artistaMBID string, alvo FaixaAlvo, langs []string) (provider.AlbumExterno, error) {
	if alvo.RecordingMBID != "" {
		alb, achou, err := ap.BuscarAlbumDaFaixa(ctx, alvo.RecordingMBID, langs)
		if err != nil {
			return provider.AlbumExterno{}, err
		}
		if !achou {
			return provider.AlbumExterno{}, fmt.Errorf("enrich: album da faixa %s nao encontrado na fonte", alvo.RecordingMBID)
		}
		return alb, nil
	}
	if alvo.AlbumNome == "" {
		return provider.AlbumExterno{}, fmt.Errorf("enrich: alvo de faixa sem mbid de recording e sem nome de album")
	}
	alb, achou, err := ap.BuscarAlbumPorNome(ctx, artistaMBID, alvo.AlbumNome, provider.AlvoFaixaAlbum{
		FaixaTitulo: alvo.FaixaTitulo,
		FaixaNumero: alvo.FaixaNumero,
		DuracaoMs:   int(alvo.DuracaoSegundos * 1000),
	}, langs)
	if err != nil {
		return provider.AlbumExterno{}, err
	}
	if !achou {
		return provider.AlbumExterno{}, fmt.Errorf("%w: album %q do artista %s nao encontrado na fonte", ErrAlbumDoArtistaAusente, alvo.AlbumNome, artistaMBID)
	}
	return alb, nil
}

// desempatePorDiscoEFaixa returns the SINGLE tied track whose position (disc,
// track) matches the one of the file; ok=false when the position is unknown or
// ambiguous on both sides.
func desempatePorDiscoEFaixa(faixas []provider.FaixaExterna, disco, numero int) (provider.FaixaExterna, bool) {
	// 1st attempt: EXACT position (disc, track). With no disc tag, disc 1 is the
	// safe default (a single-disc release also reports 1).
	if numero > 0 {
		alvoDisco := disco
		if alvoDisco <= 0 {
			alvoDisco = 1
		}
		var porPosicao []provider.FaixaExterna
		for _, fx := range faixas {
			if fx.TrackNumber == numero && discoDe(fx) == alvoDisco {
				porPosicao = append(porPosicao, fx)
			}
		}
		if len(porPosicao) == 1 {
			return porPosicao[0], true
		}
	}
	// 2nd attempt: only the DISC, and only when the FILE declared which one it is. The
	// numbering of the file comes from tags/folder and may not match the order of the
	// release, but the disc usually does -- and it is what separates the track of disc 1 from the
	// SAME track on disc 2 of a reissue.
	if disco <= 0 {
		return provider.FaixaExterna{}, false
	}
	var porDisco []provider.FaixaExterna
	for _, fx := range faixas {
		if discoDe(fx) == disco {
			porDisco = append(porDisco, fx)
		}
	}
	if len(porDisco) == 1 {
		return porDisco[0], true
	}
	return provider.FaixaExterna{}, false
}

// discoDe is the disc of the track at the source, with 0 treated as disc 1 (file/single-disc
// release).
func discoDe(fx provider.FaixaExterna) int {
	if fx.DiscNumber <= 0 {
		return 1
	}
	return fx.DiscNumber
}

// duracoesCompativeis tells whether the duration of the file agrees with the one of the
// source within the confirmation tolerance. An unknown duration on either side does not
// reject: the signal simply does not exist.
func duracoesCompativeis(alvoSegundos float64, fonteMs int) bool {
	if alvoSegundos <= 0 || fonteMs <= 0 {
		return true
	}
	diff := alvoSegundos*1000 - float64(fonteMs)
	if diff < 0 {
		diff = -diff
	}
	return diff <= matching.ToleranciaDuracaoSegundos*1000
}

// escolherFaixaAlvo locates, inside `faixas` (those of the SINGLE album already resolved),
// the track that corresponds to `alvo`: by EXACT MBID when alvo.RecordingMBID is present
// (the precise path, with no possible ambiguity); by closest title (the same engine/threshold
// of the scanner's matching.LimiarAutoMatch) when there is a FaixaTitulo; by track_number as
// a last tiebreaker. ok=false with no applicable criterion or with no plausible match — it
// never applies a "similar" one (the same rationale as the scanner).
func escolherFaixaAlvo(faixas []provider.FaixaExterna, alvo FaixaAlvo) (provider.FaixaExterna, bool) {
	if alvo.RecordingMBID != "" {
		for _, fx := range faixas {
			if fx.MBID == alvo.RecordingMBID {
				return fx, true
			}
		}
		return provider.FaixaExterna{}, false
	}
	if alvo.FaixaTitulo != "" {
		return melhorFaixaPorTitulo(alvo, faixas)
	}
	if alvo.FaixaNumero > 0 {
		for _, fx := range faixas {
			if fx.TrackNumber == alvo.FaixaNumero {
				return fx, true
			}
		}
	}
	return provider.FaixaExterna{}, false
}

// ImportarFaixaAvulsa imports ONE MISSING track of an album that is ALREADY INTERNAL (the
// external "missing tracks" screen — GET /external/{source}/album/{id} + POST /import
// type=faixa with parent_id): given the RECORDING MBID `recordingMBID` (the item source_id
// on that screen) and the INTERNAL id of the parent album `albumID` (the `album` entity
// already catalogued, with external_ids["mbid"] written), it resolves the album again at
// the source through MusicaAlbumProvider.BuscarAlbumPorMbid (reusing the mbid of the parent
// album — it never enumerates the artist discography), locates the track by EXACT MBID
// among the tracks of the release, and does find-or-create + links it to the parent
// (kind=music_album, sort_index=track_number) — the same pattern of
// acharOuCriarFaixa/ligarFilho used by importarFaixa/ImportarFaixaPossuida. The MAIN artist
// of the album (for the feat detection in extensaoFaixa) is resolved from the parent of the
// album in the tree (artistaPrincipalDoAlbum) — "" when absent degrades silently (the track
// is created without the feat metadata, it never fails just because of that).
func (s *Servico) ImportarFaixaAvulsa(ctx context.Context, source, recordingMBID, albumID string, langs []string) (string, bool, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	ap, ok := prov.(provider.MusicaAlbumProvider)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q nao suporta album seletivo de musica", source)
	}

	ids, ok := s.idsExternosDe(ctx, albumID)
	if !ok || ids.Mbid == "" {
		return "", false, fmt.Errorf("enrich: album pai %s sem mbid externo", albumID)
	}

	alb, err := ap.BuscarAlbumPorMbid(ctx, ids.Mbid, langs)
	if err != nil {
		return "", false, err
	}

	fx, achou := escolherFaixaAlvo(alb.Faixas, FaixaAlvo{RecordingMBID: recordingMBID})
	if !achou {
		return "", false, fmt.Errorf("enrich: faixa %s nao encontrada no album %s", recordingMBID, ids.Mbid)
	}

	faixaID, criada, err := s.acharOuCriarFaixa(ctx, s.artistaPrincipalDoAlbum(ctx, albumID), fx)
	if err != nil {
		return "", false, err
	}
	s.ligarFilho(ctx, albumID, faixaID, musicext.KindAlbum, fx.TrackNumber)
	return faixaID, criada, nil
}

// artistaPrincipalDoAlbum resolves the MBID of the MAIN artist of the album `albumID`
// (through the entity_relation kind=music_artista_principal, parent of the album) — used
// only by ImportarFaixaAvulsa to detect a feat on the imported standalone track
// (extensaoFaixa). "" when the album has no parent linked (it should not happen for an
// already catalogued album, but it degrades silently — it never fails because of that).
func (s *Servico) artistaPrincipalDoAlbum(ctx context.Context, albumID string) string {
	pais, err := s.leitura().ListarRelacoesPais(ctx, albumID)
	if err != nil {
		return ""
	}
	for _, r := range pais {
		if r.Kind != musicext.KindArtistaPrincipal {
			continue
		}
		if ids, ok := s.idsExternosDe(ctx, r.ParentID); ok {
			return ids.Mbid
		}
	}
	return ""
}

// melhorFaixaPorTitulo picks, among `faixas`, the one whose title is closest to `alvo`
// (Jaro-Winkler through internal/textsim, matching.LimiarAutoMatch threshold — the same
// engine/number as the scanner). Below the threshold: (zero,false), never a "similar" match.
// Once the best candidate by title is chosen, it applies a CONFIRMATION gate
// by duration (matching.ToleranciaDuracaoSegundos): when the target duration and the
// candidate duration (FaixaExterna.DuracaoMs) are BOTH known and differ beyond the
// tolerance, it rejects even with a matching title. Either side unknown (0) skips the gate,
// preserving the previous behaviour.
//
// Real releases may have 2+ tracks with the SAME title (e.g.: a
// remix/edit that kept the "clean" title on MusicBrainz) -- all of them tie at the
// highest score. In that case the duration becomes a TIEBREAK criterion (not just a
// confirmation): among the tied ones, it only accepts (and returns) when exactly 1 has a
// known duration AND within the tolerance; without a target duration, or with 0/2+
// plausible candidates, it stays ambiguous -- (zero,false), it never chooses
// arbitrarily by iteration order.
func melhorFaixaPorTitulo(alvo FaixaAlvo, faixas []provider.FaixaExterna) (provider.FaixaExterna, bool) {
	melhorScore := -1.0
	var empatados []provider.FaixaExterna
	for _, fx := range faixas {
		score := matching.SimilaridadeNormalizada(alvo.FaixaTitulo, fx.Title)
		switch {
		case score > melhorScore:
			melhorScore = score
			empatados = []provider.FaixaExterna{fx}
		case score == melhorScore:
			empatados = append(empatados, fx)
		}
	}
	if melhorScore < matching.LimiarAutoMatch {
		return provider.FaixaExterna{}, false
	}
	if len(empatados) == 1 {
		melhor := empatados[0]
		if alvo.DuracaoSegundos > 0 && melhor.DuracaoMs > 0 {
			diffMs := alvo.DuracaoSegundos*1000 - float64(melhor.DuracaoMs)
			if diffMs < 0 {
				diffMs = -diffMs
			}
			if diffMs > matching.ToleranciaDuracaoSegundos*1000 {
				return provider.FaixaExterna{}, false
			}
		}
		return melhor, true
	}
	// 2+ tied at the top. The (disc, track) of the FILE breaks the tie first: on a
	// reissue the same track appears on more than one disc and the position is the only
	// signal that says which of them the file is (real case "The Hurting
	// (Deluxe Edition)": disc 2 does not exist in the single release).
	if unica, ok := desempatePorDiscoEFaixa(empatados, alvo.FaixaDisco, alvo.FaixaNumero); ok {
		return unica, duracoesCompativeis(alvo.DuracaoSegundos, unica.DuracaoMs)
	}
	// With no usable position: the duration breaks the tie, it does not confirm. Without a
	// target duration there is no signal to decide -- it rejects (the same safe behaviour as
	// when there is no duration).
	if alvo.DuracaoSegundos <= 0 {
		return provider.FaixaExterna{}, false
	}
	duracaoAlvoMs := alvo.DuracaoSegundos * 1000
	var unico provider.FaixaExterna
	compativeis := 0
	for _, fx := range empatados {
		if fx.DuracaoMs <= 0 {
			continue
		}
		diffMs := duracaoAlvoMs - float64(fx.DuracaoMs)
		if diffMs < 0 {
			diffMs = -diffMs
		}
		if diffMs <= matching.ToleranciaDuracaoSegundos*1000 {
			compativeis++
			unico = fx
		}
	}
	if compativeis == 1 {
		return unico, true
	}
	return provider.FaixaExterna{}, false
}
