package server

// Testes de POST /api/v1/entities/{id}/audio-tags/write (briefing 0125):
// escrita bem-sucedida de faixa avulsa, cascata de álbum com sucesso
// parcial, erro para type==artista, 404 pra id inexistente, sincronização
// de media_file pós-escrita e resolução do artista feat. na tag. Roda o
// ffmpeg DE VERDADE (mesmo padrão de internal/mediainfo/mediainfo_test.go e
// internal/audiotag/writer_real_test.go: self-skip se o binário faltar no
// ambiente) — a validação empírica da tag escrita é o que prova o
// comportamento, não só a resposta HTTP.

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

// setupAudioTagsServer sobe um Server com banco temporário migrado e
// assetsDir configurado (mesmo padrão de setupRefreshFilesServer).
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

// pularSemFfmpegServer pula o teste se ffmpeg/ffprobe faltarem no ambiente
// (o handler real depende do binário via internal/audiotag).
func pularSemFfmpegServer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg ausente; pulando teste de integracao de audio-tags")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe ausente; pulando teste de integracao de audio-tags")
	}
}

// gerarClipeMp3 gera um .mp3 sintético de 1s no path dado.
func gerarClipeMp3(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-y", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=1", "-c:a", "libmp3lame", path)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg nao gerou o clipe %q: %v", path, err)
	}
}

// ffprobeFormatTags lê as tags de format de um arquivo de áudio via ffprobe.
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

// ── Helpers de seed da árvore de música com MBIDs/extension_data ───────────

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

// semearFaixaMusica cria uma faixa com track/disc/isrc no extension_data;
// featNome/featMbid (opcionais, "" quando ausentes) gravam
// feat_artist_name/feat_artist_mbid (ADR-0029 item 4/briefing 0115) -- o
// sinal de artist-credit divergente que NÃO passa por entity_relation.
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

// ── Testes ───────────────────────────────────────────────────────────────

// TestWriteAudioTagsEntityInexistenteDevolve404 cobre o critério "404 se
// entity não existe".
func TestWriteAudioTagsEntityInexistenteDevolve404(t *testing.T) {
	h, _ := setupAudioTagsServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/inexistente/audio-tags/write", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestWriteAudioTagsTypeArtistaDevolve422 cobre o critério "type==artista
// retorna erro, sem tentar cascatear" (briefing 0125 §3/§5).
func TestWriteAudioTagsTypeArtistaDevolve422(t *testing.T) {
	h, srv := setupAudioTagsServer(t)
	semearArtistaMusica(t, srv.db, "art1", "Duran Duran", "mbid-artista")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/art1/audio-tags/write", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestWriteAudioTagsTypeMovieDevolve422 cobre que QUALQUER outro tipo (não
// só artista) é recusado -- o endpoint só suporta faixa/album.
func TestWriteAudioTagsTypeMovieDevolve422(t *testing.T) {
	h, srv := setupAudioTagsServer(t)
	semearEntity(t, srv.db, "m1", "movie", "Um Filme")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/m1/audio-tags/write", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestWriteAudioTagsFaixaAvulsaRegravaTagESincronizaMediaFile cobre o
// critério de aceite principal: type=faixa regrava a tag do arquivo
// vinculado com título/artista/álbum/ano/faixa-disco/gênero/MBIDs/ISRC
// resolvidos, e sincroniza media_file.hash/mtime/size no mesmo golpe.
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
	eqAudioTag(t, tags, "track", "3/1") // trackTotal=1: unica faixa ligada ao album neste seed
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

// TestWriteAudioTagsAlbumCascataSucessoParcial cobre o critério "uma falha
// isolada ao regravar UMA faixa do álbum não aborta as demais -- o resumo
// da resposta reporta sucesso parcial": fx1 tem um .mp3 válido, fx2 tem um
// arquivo que NÃO é áudio de verdade (ffmpeg falha ao processá-lo).
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

	// fx2 aponta para um arquivo .mp3 que na verdade não é áudio nenhum --
	// o ffmpeg falha ao tentar remuxar/regravar a tag dele.
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

// TestWriteAudioTagsResolveArtistaFeatNaTag cobre o critério de aceite mais
// específico do briefing: faixa com artist-credit divergente (feat.) leva o
// artista da PRÓPRIA faixa na tag de ARTISTA, não o artista principal do
// álbum -- AlbumArtist continua sendo o do álbum.
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

// writeGarbageFile grava bytes que NÃO formam um arquivo de áudio válido --
// usado para provar que o ffmpeg falha ao regravar a tag desse arquivo
// (critério de sucesso PARCIAL da cascata de álbum).
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
