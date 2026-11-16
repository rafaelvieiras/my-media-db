package server

// Tests of POST /api/v1/entities/{id}/audio-tags/write:
// successful write of a standalone track, album cascade with partial
// success, error for type==artista, 404 for a nonexistent id, media_file
// synchronization after the write and resolution of the feat. artist in the tag. It runs
// the REAL ffmpeg (same pattern as internal/mediainfo/mediainfo_test.go and
// internal/audiotag/writer_real_test.go: self-skip if the binary is missing from the
// environment) — the empirical validation of the written tag is what proves the
// behavior, not just the HTTP response.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupAudioTagsServer starts a Server with a temporary migrated database and
// assetsDir configured (same pattern as setupRefreshFilesServer).
func setupAudioTagsServer(t *testing.T) (http.Handler, *Server) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audio-tags.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	srv := New("v-teste", database, WithAssetsDir(t.TempDir()))
	h, err := srv.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, srv
}

// pularSemFfmpegServer skips the test if ffmpeg/ffprobe are missing from the
// environment (the real handler depends on the binary via internal/audiotag).
func pularSemFfmpegServer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg ausente; pulando teste de integracao de audio-tags")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe ausente; pulando teste de integracao de audio-tags")
	}
}

// gerarClipeMp3 generates a synthetic 1s .mp3 at the given path.
func gerarClipeMp3(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-y", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=1", "-c:a", "libmp3lame", path)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg nao gerou o clipe %q: %v", path, err)
	}
}

// ffprobeFormatTags reads the format tags of an audio file via ffprobe.
func ffprobeFormatTags(t *testing.T, path string) map[string]string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", path).Output()
	if err != nil {
		t.Fatalf("ffprobe -show_format %q: %v", path, err)
	}
	var parsed struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal ffprobe: %v", err)
	}
	return parsed.Format.Tags
}

// ── Seed helpers for the music tree with MBIDs/extension_data ───────────────

func semearArtistaMusica(t *testing.T, d *db.DB, id, titulo, mbid string) {
	t.Helper()
	extIDs, err := json.Marshal(map[string]string{"mbid": mbid})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: "artista", Title: titulo, ExternalIdsJson: nsOf(string(extIDs)),
	}); err != nil {
		t.Fatalf("CriarEntity artista(%s): %v", id, err)
	}
}

func semearAlbumMusica(t *testing.T, d *db.DB, id, titulo, mbid, releaseDate, genre string) {
	t.Helper()
	extIDs, err := json.Marshal(map[string]string{"mbid": mbid})
	if err != nil {
		t.Fatal(err)
	}
	extData, err := json.Marshal(map[string]any{"core.music@1": map[string]any{"genre": genre}})
	if err != nil {
		t.Fatal(err)
	}
	p := sqlcgen.CriarEntityParams{
		ID: id, Type: "album", Title: titulo,
		ExternalIdsJson:   nsOf(string(extIDs)),
		ExtensionDataJson: nsOf(string(extData)),
	}
	if releaseDate != "" {
		p.ReleaseDate = nsOf(releaseDate)
	}
	if _, err := escrita(t, d).CriarEntity(context.Background(), p); err != nil {
		t.Fatalf("CriarEntity album(%s): %v", id, err)
	}
}

// semearFaixaMusica creates a track with track/disc/isrc in extension_data;
// featNome/featMbid (optional, "" when absent) write
// feat_artist_name/feat_artist_mbid -- the signal of a diverging artist-credit that
// does NOT go through entity_relation.
func semearFaixaMusica(t *testing.T, d *db.DB, id, titulo, mbid string, trackNumber, discNumber, discTotal int, isrc, featNome, featMbid string) {
	t.Helper()
	extIDs, err := json.Marshal(map[string]string{"mbid": mbid})
	if err != nil {
		t.Fatal(err)
	}
	ns := map[string]any{"track_number": trackNumber, "disc_number": discNumber, "disc_total": discTotal}
	if isrc != "" {
		ns["isrc"] = isrc
	}
	if featNome != "" {
		ns["feat_artist_name"] = featNome
	}
	if featMbid != "" {
		ns["feat_artist_mbid"] = featMbid
	}
	extData, err := json.Marshal(map[string]any{"core.music@1": ns})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: "faixa", Title: titulo,
		ExternalIdsJson:   nsOf(string(extIDs)),
		ExtensionDataJson: nsOf(string(extData)),
	}); err != nil {
		t.Fatalf("CriarEntity faixa(%s): %v", id, err)
	}
}

// ── Tests ───────────────────────────────────────────────────────────────

// TestWriteAudioTagsEntityInexistenteDevolve404 covers the "404 if the entity does not
// exist" criterion.
func TestWriteAudioTagsEntityInexistenteDevolve404(t *testing.T) {
	h, _ := setupAudioTagsServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/inexistente/audio-tags/write", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestWriteAudioTagsTypeArtistaDevolve422 covers the "type==artista returns an error,
// without trying to cascade" criterion.
func TestWriteAudioTagsTypeArtistaDevolve422(t *testing.T) {
	h, srv := setupAudioTagsServer(t)
	semearArtistaMusica(t, srv.db, "art1", "Duran Duran", "mbid-artista")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/art1/audio-tags/write", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestWriteAudioTagsTypeMovieDevolve422 covers that ANY other type (not
// only artista) is refused -- the endpoint only supports track/album.
func TestWriteAudioTagsTypeMovieDevolve422(t *testing.T) {
	h, srv := setupAudioTagsServer(t)
	semearEntity(t, srv.db, "m1", "movie", "Um Filme")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/m1/audio-tags/write", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestWriteAudioTagsFaixaAvulsaRegravaTagESincronizaMediaFile covers the
// main acceptance criterion: type=faixa rewrites the tag of the linked
// file with title/artist/album/year/track-disc/genre/MBIDs/ISRC
// resolved, and synchronizes media_file.hash/mtime/size in the same stroke.
func TestWriteAudioTagsFaixaAvulsaRegravaTagESincronizaMediaFile(t *testing.T) {
	pularSemFfmpegServer(t)
	h, srv := setupAudioTagsServer(t)
	ctx := context.Background()

	semearArtistaMusica(t, srv.db, "art1", "Duran Duran", "mbid-artista")
	semearAlbumMusica(t, srv.db, "alb1", "Rio", "mbid-album", "1982-05-10", "New Wave")
	semearFaixaMusica(t, srv.db, "fx1", "Hungry Like the Wolf", "mbid-faixa", 3, 1, 1, "GBAYE8200093", "", "")
	ligarFilhoOrdenado(t, srv.db, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, srv.db, "r2", "alb1", "fx1", "music_album", 3)

	pasta := t.TempDir()
	semearSource(t, srv.db, "src1", pasta)
	clipe := filepath.Join(pasta, "03.mp3")
	gerarClipeMp3(t, clipe)
	semearFileComFonte(t, srv.db, "mf1", "fx1", clipe, "present", 1, "src1")

	var hashAntes sql.NullString
	if err := srv.db.Read.QueryRowContext(ctx, "SELECT content_hash FROM media_file WHERE id='mf1'").Scan(&hashAntes); err != nil {
		t.Fatalf("ler content_hash antes: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/fx1/audio-tags/write", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var dto audioTagsWriteResultDTO
	decodeInto(t, rec, &dto)
	if dto.FaixasRegravadas != 1 {
		t.Errorf("faixas_regravadas = %d, quero 1 (falhas=%v)", dto.FaixasRegravadas, dto.Falhas)
	}
	if len(dto.Falhas) != 0 {
		t.Errorf("falhas = %v, quero vazio", dto.Falhas)
	}

	tags := ffprobeFormatTags(t, clipe)
	eqAudioTag(t, tags, "title", "Hungry Like the Wolf")
	eqAudioTag(t, tags, "artist", "Duran Duran")
	eqAudioTag(t, tags, "album_artist", "Duran Duran")
	eqAudioTag(t, tags, "album", "Rio")
	eqAudioTag(t, tags, "genre", "New Wave")
	eqAudioTag(t, tags, "track", "3/1") // trackTotal=1: single track linked to the album in this seed
	eqAudioTag(t, tags, "disc", "1/1")
	eqAudioTag(t, tags, "TSRC", "GBAYE8200093")
	eqAudioTag(t, tags, "MusicBrainz Track Id", "mbid-faixa")
	eqAudioTag(t, tags, "MusicBrainz Album Id", "mbid-album")
	eqAudioTag(t, tags, "MusicBrainz Artist Id", "mbid-artista")

	var hashDepois sql.NullString
	if err := srv.db.Read.QueryRowContext(ctx, "SELECT content_hash FROM media_file WHERE id='mf1'").Scan(&hashDepois); err != nil {
		t.Fatalf("ler content_hash depois: %v", err)
	}
	if !hashDepois.Valid || hashDepois.String == "" {
		t.Error("content_hash vazio apos audio-tags/write, quero preenchido (recheck pos-escrita)")
	}
	if hashAntes.Valid && hashAntes.String == hashDepois.String {
		t.Error("content_hash nao mudou apos regravar a tag -- esperava refletir o arquivo pos-escrita")
	}
}

// TestWriteAudioTagsAlbumCascataSucessoParcial covers the "an isolated failure
// when rewriting ONE track of the album does not abort the others -- the response summary
// reports partial success" criterion: fx1 has a valid .mp3, fx2 has a
// file that is NOT really audio (ffmpeg fails to process it).
func TestWriteAudioTagsAlbumCascataSucessoParcial(t *testing.T) {
	pularSemFfmpegServer(t)
	h, srv := setupAudioTagsServer(t)

	semearArtistaMusica(t, srv.db, "art1", "Duran Duran", "mbid-artista")
	semearAlbumMusica(t, srv.db, "alb1", "Rio", "mbid-album", "1982", "New Wave")
	semearFaixaMusica(t, srv.db, "fx1", "Hungry Like the Wolf", "mbid-fx1", 1, 1, 1, "", "", "")
	semearFaixaMusica(t, srv.db, "fx2", "Faixa Corrompida", "mbid-fx2", 2, 1, 1, "", "", "")
	ligarFilhoOrdenado(t, srv.db, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, srv.db, "r2", "alb1", "fx1", "music_album", 1)
	ligarFilhoOrdenado(t, srv.db, "r3", "alb1", "fx2", "music_album", 2)

	pasta := t.TempDir()
	semearSource(t, srv.db, "src1", pasta)
	clipeOk := filepath.Join(pasta, "01.mp3")
	gerarClipeMp3(t, clipeOk)
	semearFileComFonte(t, srv.db, "mf1", "fx1", clipeOk, "present", 1, "src1")

	// fx2 points to an .mp3 file that is not actually audio at all --
	// ffmpeg fails when trying to remux/rewrite its tag.
	clipeRuim := filepath.Join(pasta, "02.mp3")
	writeGarbageFile(t, clipeRuim)
	semearFileComFonte(t, srv.db, "mf2", "fx2", clipeRuim, "present", 1, "src1")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/alb1/audio-tags/write", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var dto audioTagsWriteResultDTO
	decodeInto(t, rec, &dto)
	if dto.FaixasRegravadas != 1 {
		t.Errorf("faixas_regravadas = %d, quero 1 (fx1 ok, fx2 falha) -- falhas=%v", dto.FaixasRegravadas, dto.Falhas)
	}
	if len(dto.Falhas) != 1 {
		t.Fatalf("falhas = %v, quero exatamente 1 entrada (fx2)", dto.Falhas)
	}
}

// TestWriteAudioTagsResolveArtistaFeatNaTag covers the most
// specific acceptance criterion: a track with a diverging artist-credit (feat.) puts the
// artist of the TRACK ITSELF in the ARTIST tag, not the main artist of
// the album -- AlbumArtist remains the album's.
func TestWriteAudioTagsResolveArtistaFeatNaTag(t *testing.T) {
	pularSemFfmpegServer(t)
	h, srv := setupAudioTagsServer(t)

	semearArtistaMusica(t, srv.db, "art1", "Artista Principal", "mbid-principal")
	semearAlbumMusica(t, srv.db, "alb1", "Album Colab", "mbid-album", "2020", "Pop")
	semearFaixaMusica(t, srv.db, "fx1", "Faixa Feat", "mbid-fx1", 1, 1, 1, "", "Artista Convidado", "mbid-feat")
	ligarFilhoOrdenado(t, srv.db, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, srv.db, "r2", "alb1", "fx1", "music_album", 1)

	pasta := t.TempDir()
	semearSource(t, srv.db, "src1", pasta)
	clipe := filepath.Join(pasta, "01.mp3")
	gerarClipeMp3(t, clipe)
	semearFileComFonte(t, srv.db, "mf1", "fx1", clipe, "present", 1, "src1")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/fx1/audio-tags/write", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var dto audioTagsWriteResultDTO
	decodeInto(t, rec, &dto)
	if dto.FaixasRegravadas != 1 {
		t.Fatalf("faixas_regravadas = %d, quero 1 (falhas=%v)", dto.FaixasRegravadas, dto.Falhas)
	}

	tags := ffprobeFormatTags(t, clipe)
	eqAudioTag(t, tags, "artist", "Artista Convidado")
	eqAudioTag(t, tags, "album_artist", "Artista Principal")
	eqAudioTag(t, tags, "MusicBrainz Artist Id", "mbid-feat")
	eqAudioTag(t, tags, "MusicBrainz Album Artist Id", "mbid-principal")
}

// writeGarbageFile writes bytes that do NOT form a valid audio file --
// used to prove that ffmpeg fails to rewrite the tag of that file
// (PARTIAL success criterion of the album cascade).
func writeGarbageFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("isto nao e um arquivo de audio valido"), 0o644); err != nil {
		t.Fatalf("writeGarbageFile(%s): %v", path, err)
	}
}

func eqAudioTag(t *testing.T, tags map[string]string, chave, quer string) {
	t.Helper()
	if got := tags[chave]; got != quer {
		t.Errorf("tag %q = %q, quero %q (tags=%v)", chave, got, quer, tags)
	}
}
