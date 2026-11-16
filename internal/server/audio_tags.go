package server

// audio_tags.go implements POST /api/v1/entities/{id}/audio-tags/write
// (Manage menu > "Write metadata to the file"): rewrites the audio file's OWN tag
// (ID3v2/Vorbis comment, internal/audiotag) with the ALREADY RESOLVED metadata
// (override ⟵ external ⟵ file, the same path of resolverEntity that the detail page
// uses) — never a raw provider value. For `faixa`, it writes only its own file; for
// `album`, it cascades to ALL OWNED tracks (best-effort: a single failure does not
// abort the others), honoring a divergent artist-credit (feat., ADR-0027 item 4) in
// the ARTIST tag of that specific track — album/year/genre/cover keep coming from the
// album. `type=='artista'` (or any other) is 422: there is no coherent "work"
// granularity to cascade (ADR-0027). Unlike the "no automatic cascade" pattern used
// elsewhere, here cascading the album is the ONLY coherent interpretation of the
// action (an album never has a file of its own, ADR-0027) — it is not an arbitrary
// mass cascade.
//
// After writing each track, it synchronizes media_file.hash/mtime/size reusing
// recheckEntityFilesPorFonte (the SAME function refresh_files.go already uses) from a
// NEW caller — no new query, so the next incremental scan does not see the rewritten
// file as a surprise "change".

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/audiotag"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// audioTagsWriteResultDTO is the response of POST /entities/{id}/audio-tags/write:
// an informational summary — `falhas` carries "faixaID: reason" for each track that was
// not rewritten.
type audioTagsWriteResultDTO struct {
	FaixasRegravadas int      `json:"faixas_regravadas"`
	Falhas           []string `json:"falhas"`
}

// writeAudioTags handles POST /api/v1/entities/{id}/audio-tags/write. 404 if the entity
// does not exist; 422 if `type` is not `faixa`/`album`; 503 if ffmpeg (the write
// mechanism, internal/audiotag) is not available in the environment. No body.
func (s *Server) writeAudioTags(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	writer := s.audioTagWriter
	if writer == nil || !writer.Disponivel() {
		writeError(w, http.StatusServiceUnavailable, "audiotag_unavailable",
			"gravacao de tag de audio indisponivel (ffmpeg ausente no ambiente)")
		return
	}

	var faixaIDs []string
	var albumID string
	switch ent.Type {
	case tipoEntFaixa:
		faixaIDs = []string{id}
		albumID = s.albumPaiDaFaixa(ctx, id)
	case tipoEntAlbum:
		albumID = id
		faixaIDs = s.faixasPossuidasDoAlbum(ctx, id)
	default:
		writeError(w, http.StatusUnprocessableEntity, "unsupported_type",
			"gravacao de tag de audio so suporta entity type=faixa ou type=album")
		return
	}

	albumCtx, err := s.montarContextoAlbum(ctx, albumID)
	if err != nil {
		respondErroInterno(w)
		return
	}

	dto := audioTagsWriteResultDTO{Falhas: []string{}}
	for _, faixaID := range faixaIDs {
		if err := s.gravarTagDaFaixa(ctx, writer, faixaID, albumCtx); err != nil {
			log.Printf("audio-tags/write: faixa %q falhou (segue -- best-effort): %v", faixaID, err)
			dto.Falhas = append(dto.Falhas, fmt.Sprintf("%s: %v", faixaID, err))
			continue
		}
		dto.FaixasRegravadas++
	}
	writeJSON(w, http.StatusOK, dto)
}

// contextoAlbum groups what is common to ALL tracks of an album being rewritten (it
// avoids resolving album/artist/track count again for each track of the cascade).
// AlbumEnt/ArtistaEnt are zero-value (ID=="") when the album was not found or the
// track has no parent album (defensive -- the model, ADR-0027, guarantees every track
// has a parent album).
type contextoAlbum struct {
	albumEnt   resolver.Entidade
	artistaEnt resolver.Entidade
	trackTotal int
}

// albumPaiDaFaixa walks up from a track to the parent album (entity_relation
// kind=music_album) — "" when the track has none (it should not happen in the model,
// but it degrades instead of failing the whole call).
func (s *Server) albumPaiDaFaixa(ctx context.Context, faixaID string) string {
	pais, err := s.leitura().ListarRelacoesPais(ctx, faixaID)
	if err != nil {
		return ""
	}
	for _, p := range pais {
		if p.Kind == musicext.KindAlbum {
			return p.ParentID
		}
	}
	return ""
}

// faixasPossuidasDoAlbum lists the ids of the child tracks (entity_relation
// kind=music_album, same sort_index=track_number order that ListarRelacoesFilhas
// already returns) that have at least one 'present' media_file — the cascade only writes
// an OWNED track (without a file, there is nothing to rewrite). A read error degrades
// to an empty list (best-effort, the same spirit as the rest of the endpoint).
func (s *Server) faixasPossuidasDoAlbum(ctx context.Context, albumID string) []string {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, albumID)
	if err != nil {
		log.Printf("audio-tags/write: listar faixas do album %q falhou: %v", albumID, err)
		return nil
	}
	var ids []string
	for _, r := range rels {
		if r.Kind != musicext.KindAlbum {
			continue
		}
		files, err := s.leitura().ListarMediaFilesPorEntity(ctx, r.ChildID)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.State == "present" {
				ids = append(ids, r.ChildID)
				break
			}
		}
	}
	return ids
}

// montarContextoAlbum resolves the album (RESOLVED, the same path as resolverEntity) and
// its MAIN artist (entity_relation kind=music_artista_principal), plus the total number
// of tracks of the album (count of kind=music_album children — it becomes the tag's
// "total tracks", a field the model, ADR-0027, does not persist per track). albumID==""
// returns the zero-value without an error (a standalone track without a parent album, a
// defensive case). The error only propagates if the album's OWN resolution really fails
// (not NoRows -- that case already degrades to the zero-value, the album may have
// vanished between the caller's check and now).
func (s *Server) montarContextoAlbum(ctx context.Context, albumID string) (contextoAlbum, error) {
	if albumID == "" {
		return contextoAlbum{}, nil
	}
	albumEnt, achado, err := s.resolverEntity(ctx, albumID, "", false)
	if err != nil {
		return contextoAlbum{}, err
	}
	if !achado {
		return contextoAlbum{}, nil
	}

	var artistaEnt resolver.Entidade
	if pais, err := s.leitura().ListarRelacoesPais(ctx, albumID); err == nil {
		for _, p := range pais {
			if p.Kind != musicext.KindArtistaPrincipal {
				continue
			}
			if a, ok, err := s.resolverEntity(ctx, p.ParentID, "", false); err == nil && ok {
				artistaEnt = a
			}
			break
		}
	}

	total := 0
	if rels, err := s.leitura().ListarRelacoesFilhas(ctx, albumID); err == nil {
		for _, r := range rels {
			if r.Kind == musicext.KindAlbum {
				total++
			}
		}
	}

	return contextoAlbum{albumEnt: albumEnt, artistaEnt: artistaEnt, trackTotal: total}, nil
}

// gravarTagDaFaixa resolves the present file + the resolved metadata of ONE track and
// calls the Writer; on success, it synchronizes media_file through
// recheckEntityFilesPorFonte (reused here). The returned error belongs only to that
// track (the caller -- writeAudioTags -- handles it best-effort, it does not abort the
// cascade).
func (s *Server) gravarTagDaFaixa(ctx context.Context, writer *audiotag.Writer, faixaID string, albumCtx contextoAlbum) error {
	files, err := s.leitura().ListarMediaFilesPorEntity(ctx, faixaID)
	if err != nil {
		return err
	}
	var path string
	for _, f := range files {
		if f.State == "present" {
			path = f.Path
			break
		}
	}
	if path == "" {
		return errors.New("sem arquivo present vinculado")
	}

	faixaEnt, achada, err := s.resolverEntity(ctx, faixaID, "", false)
	if err != nil {
		return err
	}
	if !achada {
		return errors.New("faixa nao encontrada")
	}

	meta := s.montarMetadadoFaixa(ctx, faixaEnt, albumCtx)
	if err := writer.Escrever(ctx, path, meta); err != nil {
		return err
	}

	// Synchronizes media_file.hash/mtime/size AFTER the write: best-effort -- the tag
	// was already written successfully; a failure here only means the next incremental
	// scan rechecks the file on its own (the behavior of BEFORE this endpoint).
	s.recheckEntityFilesPorFonte(ctx, files)
	return nil
}

// musicaNamespace extracts the ALREADY RESOLVED core.music@1 namespace (map[string]any,
// it came from resolver.Entidade.ExtensionData -- unlike musicext.ExtractNamespace, which
// reads the entity's RAW extension_data_json; here the value already went through the
// override⟵external⟵file composition). nil when the entity has no such namespace.
func musicaNamespace(ent resolver.Entidade) map[string]any {
	if ent.ExtensionData == nil {
		return nil
	}
	ns, _ := ent.ExtensionData[musicext.Namespace].(map[string]any)
	return ns
}

// featCredenciadoPorRelacao walks up from a track to the COLLABORATION artist
// (entity_relation kind=music_artista_feat, ADR-0027 item 4) when it exists — returning
// his RESOLVED name/MBID (the same resolverEntity path). ("", "") when there is no feat
// relation (the common case).
func (s *Server) featCredenciadoPorRelacao(ctx context.Context, faixaID string) (nome, mbid string) {
	pais, err := s.leitura().ListarRelacoesPais(ctx, faixaID)
	if err != nil {
		return "", ""
	}
	for _, p := range pais {
		if p.Kind != musicext.KindArtistaFeat {
			continue
		}
		ent, ok, err := s.resolverEntity(ctx, p.ParentID, "", false)
		if err != nil || !ok {
			continue
		}
		return ent.Title, ent.ExternalIDs["mbid"]
	}
	return "", ""
}

// montarMetadadoFaixa builds the audiotag.Metadado of ONE track from its RESOLVED entity
// (faixaEnt) + the already resolved album context (albumCtx, shared by all the tracks of
// a cascade). The tag artist is the track's one (honoring a feat. divergence, ADR-0027
// item 4: the entity_relation kind=music_artista_feat wins, with a fallback to the
// feat_artist_name/feat_artist_mbid keys of extension_data when there is no relation);
// AlbumArtist is always the album's MAIN artist.
func (s *Server) montarMetadadoFaixa(ctx context.Context, faixaEnt resolver.Entidade, albumCtx contextoAlbum) audiotag.Metadado {
	nsFaixa := musicaNamespace(faixaEnt)
	nsAlbum := musicaNamespace(albumCtx.albumEnt)

	trackNumber, _ := musicext.AsInt(nsFaixa["track_number"])
	discNumber, _ := musicext.AsInt(nsFaixa["disc_number"])
	discTotal, _ := musicext.AsInt(nsFaixa["disc_total"])
	isrc, _ := musicext.AsString(nsFaixa["isrc"])
	genre, _ := musicext.AsString(nsAlbum["genre"])

	featRelNome, featRelMbid := s.featCredenciadoPorRelacao(ctx, faixaEnt.ID)
	featNsNome, _ := musicext.AsString(nsFaixa[musicext.FeatArtistNameKey])
	featNsMbid, _ := musicext.AsString(nsFaixa[musicext.FeatArtistMbidKey])

	artista := albumCtx.artistaEnt.Title
	artistaMbid := albumCtx.artistaEnt.ExternalIDs["mbid"]
	if nome := primeiroNaoVazio(featRelNome, featNsNome); nome != "" {
		artista = nome
	}
	if mbid := primeiroNaoVazio(featRelMbid, featNsMbid); mbid != "" {
		artistaMbid = mbid
	}

	albumArtistaMbid := albumCtx.artistaEnt.ExternalIDs["mbid"]

	return audiotag.Metadado{
		Title:            faixaEnt.Title,
		Artist:           artista,
		Album:            albumCtx.albumEnt.Title,
		AlbumArtist:      albumCtx.artistaEnt.Title,
		Date:             strOrEmpty(albumCtx.albumEnt.ReleaseDate),
		Genre:            genre,
		TrackNumber:      trackNumber,
		TrackTotal:       albumCtx.trackTotal,
		DiscNumber:       discNumber,
		DiscTotal:        discTotal,
		Compilation:      albumArtistaMbid != "" && albumArtistaMbid == musicext.MBIDVariousArtists,
		ISRC:             isrc,
		MBRecordingID:    faixaEnt.ExternalIDs["mbid"],
		MBReleaseGroupID: albumCtx.albumEnt.ExternalIDs["mbid"],
		MBArtistID:       artistaMbid,
		MBAlbumArtistID:  albumArtistaMbid,
		CoverPath:        s.capaDoAlbum(ctx, albumCtx.albumEnt.ID),
	}
}

// capaDoAlbum resolves the on-disk path of the album's WINNING cover (same artwork
// serving precedence as internal/server/assets.go, escolherAssetServido --
// preferred→user→local→external→base), validated against the trusted roots (the same
// path-traversal guard as getAssetFile) and confirmed on disk. "" when the album has no
// poster at all or the path fails the guard / no longer exists (best-effort: the tag
// goes out without a cover instead of failing the whole track).
func (s *Server) capaDoAlbum(ctx context.Context, albumID string) string {
	if albumID == "" {
		return ""
	}
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, albumID)
	if err != nil {
		return ""
	}
	asset, ok := escolherAssetServido(assets, kindPoster)
	if !ok {
		return ""
	}
	raizes, err := s.raizesConfiaveis(ctx)
	if err != nil || !caminhoSobRaizes(asset.PathOrUri, raizes) {
		return ""
	}
	if _, err := os.Stat(asset.PathOrUri); err != nil {
		return ""
	}
	return asset.PathOrUri
}

// primeiroNaoVazio returns `a` when non-empty, otherwise `b`.
func primeiroNaoVazio(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// strOrEmpty derefs a *string ("" when nil) -- the resolved release_date
// (resolver.Entidade.ReleaseDate) is *string.
func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
