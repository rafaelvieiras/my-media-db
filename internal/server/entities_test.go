package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// setupEntitiesServer sobe um Server com banco temporário migrado e devolve o
// handler raiz E o *db.DB, para os testes semearem dados direto pelas queries
// sqlc (pela conexão de escrita) e exercitarem as rotas de leitura.
func setupEntitiesServer(t *testing.T) (http.Handler, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "entities.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			// Close pode já ter sido fechado por um teste específico; ignora.
			_ = err
		}
	})
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	h, err := New("v-teste", database).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database
}

// escrita devolve os Queries no pool de ESCRITA para semear os testes.
func escrita(t *testing.T, d *db.DB) *sqlcgen.Queries {
	t.Helper()
	return sqlcgen.New(d.Write)
}

// nsOf embrulha uma string num sql.NullString válido.
func nsOf(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// semearEntity cria uma entity mínima e devolve seu id.
func semearEntity(t *testing.T, d *db.DB, id, tipo, titulo string) string {
	t.Helper()
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:    id,
		Type:  tipo,
		Title: titulo,
	})
	if err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
	return id
}

// semearFile cria um media_file para a entity com o state e a confiança dados.
func semearFile(t *testing.T, d *db.DB, id, entityID, path, state string, conf float64, mediainfo string) {
	t.Helper()
	p := sqlcgen.CriarMediaFileParams{
		ID:              id,
		EntityID:        entityID,
		Path:            path,
		State:           state,
		MatchConfidence: sql.NullFloat64{Float64: conf, Valid: true},
	}
	if mediainfo != "" {
		p.MediainfoJson = nsOf(mediainfo)
	}
	if _, err := escrita(t, d).CriarMediaFile(context.Background(), p); err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

// semearAsset cria um asset (ex.: poster) para a entity.
func semearAsset(t *testing.T, d *db.DB, id, entityID, kind, path string) {
	t.Helper()
	_, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID:        id,
		EntityID:  entityID,
		Kind:      kind,
		Origin:    "local",
		PathOrUri: path,
	})
	if err != nil {
		t.Fatalf("CriarAsset(%s): %v", id, err)
	}
}

// semearSource cria um library_source apontando para path.
func semearSource(t *testing.T, d *db.DB, id, path string) {
	t.Helper()
	_, err := escrita(t, d).CriarLibrarySource(context.Background(), sqlcgen.CriarLibrarySourceParams{
		ID:   id,
		Path: path,
		Type: "movie",
	})
	if err != nil {
		t.Fatalf("CriarLibrarySource(%s): %v", id, err)
	}
}

// decodeList decodifica a resposta paginada de GET /api/v1/entities.
func decodeList(t *testing.T, body []byte) entityListDTO {
	t.Helper()
	var out entityListDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar entityListDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// eq compara valores comparáveis; mantém as funções de teste lineares (baixa
// complexidade ciclomática) ao tirar cada asserção do corpo do teste.
func eq[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, quero %v", name, got, want)
	}
}

// eqPtr compara um ponteiro com um valor esperado (nil falha).
func eqPtr[T comparable](t *testing.T, name string, got *T, want T) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, quero %v", name, want)
		return
	}
	eq(t, name, *got, want)
}

func TestListEntitiesPosseEContagem(t *testing.T) {
	h, d := setupEntitiesServer(t)

	// A: filme com 2 arquivos, um present -> owned, file_count=2.
	semearEntity(t, d, "a", "movie", "Alfa")
	semearFile(t, d, "a1", "a", "/lib/a1.mkv", "present", 0.9, "")
	semearFile(t, d, "a2", "a", "/lib/a2.mkv", "missing", 0.5, "")
	// B: filme com 1 arquivo missing -> NÃO owned, file_count=1.
	semearEntity(t, d, "b", "movie", "Beta")
	semearFile(t, d, "b1", "b", "/lib/b1.mkv", "missing", 0.3, "")
	// C: série sem arquivos -> NÃO owned, file_count=0.
	semearEntity(t, d, "c", "tv", "Gama")

	rec := do(t, h, http.MethodGet, "/api/v1/entities", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeList(t, rec.Body.Bytes())
	eq(t, "total", out.Total, 3)
	eq(t, "limit", out.Limit, limitPadrao)
	eq(t, "offset", out.Offset, 0)
	byID := map[string]entityCardDTO{}
	for _, it := range out.Items {
		byID[it.ID] = it
	}
	eq(t, "A.owned", byID["a"].Owned, true)
	eq(t, "A.file_count", byID["a"].FileCount, 2)
	eq(t, "B.owned", byID["b"].Owned, false)
	eq(t, "B.file_count", byID["b"].FileCount, 1)
	eq(t, "C.owned", byID["c"].Owned, false)
	eq(t, "C.file_count", byID["c"].FileCount, 0)
	// match_confidence = MAX das confianças de A (0.9).
	eqPtr(t, "A.match_confidence", byID["a"].MatchConfidence, 0.9)
}

func TestListEntitiesFiltroType(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "a", "movie", "Alfa")
	semearEntity(t, d, "b", "movie", "Beta")
	semearEntity(t, d, "c", "tv", "Gama")

	rec := do(t, h, http.MethodGet, "/api/v1/entities?type=movie", nil)
	out := decodeList(t, rec.Body.Bytes())
	if out.Total != 2 || len(out.Items) != 2 {
		t.Fatalf("type=movie: total=%d items=%d, quero 2/2", out.Total, len(out.Items))
	}
	for _, it := range out.Items {
		if it.Type != "movie" {
			t.Errorf("item type = %q, quero movie", it.Type)
		}
	}
}

func TestListEntitiesFiltroOwned(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "a", "movie", "Alfa")
	semearFile(t, d, "a1", "a", "/lib/a1.mkv", "present", 1, "")
	semearEntity(t, d, "b", "movie", "Beta")
	semearFile(t, d, "b1", "b", "/lib/b1.mkv", "missing", 1, "")

	recTrue := do(t, h, http.MethodGet, "/api/v1/entities?owned=true", nil)
	outTrue := decodeList(t, recTrue.Body.Bytes())
	if outTrue.Total != 1 || outTrue.Items[0].ID != "a" {
		t.Fatalf("owned=true: total=%d, quero só A", outTrue.Total)
	}

	recFalse := do(t, h, http.MethodGet, "/api/v1/entities?owned=false", nil)
	outFalse := decodeList(t, recFalse.Body.Bytes())
	if outFalse.Total != 1 || outFalse.Items[0].ID != "b" {
		t.Fatalf("owned=false: total=%d, quero só B", outFalse.Total)
	}
}

func TestListEntitiesPaginacaoEOrdenacao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Títulos fora de ordem para provar a ordenação por título.
	semearEntity(t, d, "id-c", "movie", "Charlie")
	semearEntity(t, d, "id-a", "movie", "Alfa")
	semearEntity(t, d, "id-b", "movie", "Bravo")

	// Ascendente, página 1 (limit=2): Alfa, Bravo.
	rec := do(t, h, http.MethodGet, "/api/v1/entities?sort=title&limit=2&offset=0", nil)
	out := decodeList(t, rec.Body.Bytes())
	if out.Total != 3 || len(out.Items) != 2 {
		t.Fatalf("asc pg1: total=%d items=%d, quero 3/2", out.Total, len(out.Items))
	}
	if out.Items[0].Title != "Alfa" || out.Items[1].Title != "Bravo" {
		t.Errorf("asc pg1 = [%s,%s], quero [Alfa,Bravo]", out.Items[0].Title, out.Items[1].Title)
	}
	// Ascendente, página 2 (offset=2): Charlie.
	rec2 := do(t, h, http.MethodGet, "/api/v1/entities?sort=title&limit=2&offset=2", nil)
	out2 := decodeList(t, rec2.Body.Bytes())
	if len(out2.Items) != 1 || out2.Items[0].Title != "Charlie" {
		t.Fatalf("asc pg2 = %+v, quero [Charlie]", out2.Items)
	}
	// Descendente: Charlie, Bravo, Alfa.
	recD := do(t, h, http.MethodGet, "/api/v1/entities?sort=-title", nil)
	outD := decodeList(t, recD.Body.Bytes())
	if len(outD.Items) != 3 || outD.Items[0].Title != "Charlie" || outD.Items[2].Title != "Alfa" {
		t.Fatalf("desc = %+v, quero [Charlie,Bravo,Alfa]", outD.Items)
	}
}

func TestListEntitiesQueryInvalida(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	for _, qs := range []string{"?limit=abc", "?offset=-1", "?owned=talvez", "?sort=ano"} {
		rec := do(t, h, http.MethodGet, "/api/v1/entities"+qs, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, quero 400", qs, rec.Code)
		}
		checkErrorShape(t, rec, "invalid_query")
	}
}

func TestGetEntityResolvido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Entity com títulos, tags e external_ids, um arquivo present e um poster.
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              "x",
		Type:            "movie",
		Title:           "Matrix",
		OriginalTitle:   nsOf("The Matrix"),
		Description:     nsOf("Neo acorda."),
		ReleaseDate:     nsOf("1999-03-31"),
		TagsJson:        nsOf(`["ação","ficção"]`),
		ExternalIdsJson: nsOf(`{"tmdb":"603","imdb":"tt0133093"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	semearFile(t, d, "xf", "x", "/lib/matrix.mkv", "present", 1, "")
	semearAsset(t, d, "xa", "x", "poster", "/lib/matrix.jpg")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/x", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar Entidade: %v (corpo=%s)", err, rec.Body.String())
	}
	eq(t, "id", ent.ID, "x")
	eq(t, "title", ent.Title, "Matrix")
	eqPtr(t, "original_title", ent.OriginalTitle, "The Matrix")
	eq(t, "owned", ent.Owned, true)
	eq(t, "file_count", ent.FileCount, 1)
	eq(t, "len(tags)", len(ent.Tags), 2)
	eq(t, "external_ids.tmdb", ent.ExternalIDs["tmdb"], "603")
	eq(t, "external_ids.imdb", ent.ExternalIDs["imdb"], "tt0133093")
	eqPtr(t, "poster_url", ent.PosterURL, "/api/v1/entities/x/assets/poster")
}

func TestGetEntityNaoEncontrado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/nao-existe", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

func TestGetEntityFiles(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Filme")
	mi := `{"general":{"format":"matroska","duration_seconds":120.5,"size_bytes":1024,"bitrate":800},` +
		`"video":[{"codec":"hevc","width":3840,"height":2160,"hdr":"HDR10"}],` +
		`"audio":[{"codec":"eac3","channels":6,"language":"por"}]}`
	// Arquivo com edition/part_no e mediainfo.
	_, err := escrita(t, d).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID:            "f1",
		EntityID:      "e",
		Path:          "/lib/filme.mkv",
		State:         "present",
		Edition:       nsOf("Director's Cut"),
		PartNo:        sql.NullInt64{Int64: 1, Valid: true},
		Size:          sql.NullInt64{Int64: 1024, Valid: true},
		MediainfoJson: nsOf(mi),
	})
	if err != nil {
		t.Fatalf("CriarMediaFile: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/files", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out filesListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar filesListDTO: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d, quero 1", len(out.Items))
	}
	f := out.Items[0]
	eqPtr(t, "edition", f.Edition, "Director's Cut")
	eqPtr(t, "part_no", f.PartNo, int64(1))
	eqPtr(t, "size", f.Size, int64(1024))
	// Mediainfo repassado: precisa conter os fatos técnicos (codec/resolução/HDR).
	var parsed map[string]any
	if err := json.Unmarshal(f.Mediainfo, &parsed); err != nil {
		t.Fatalf("mediainfo não é JSON válido: %v (%s)", err, f.Mediainfo)
	}
	if parsed["video"] == nil {
		t.Errorf("mediainfo sem faixa de vídeo: %s", f.Mediainfo)
	}
}

func TestGetEntityFilesEntityNaoEncontrada(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/nada/files", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

func TestGetPosterServeComContentType(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Datasource real no disco; poster dentro dele.
	root := t.TempDir()
	posterPath := filepath.Join(root, "poster.png")
	// Assinatura PNG + bytes arbitrários (o content-type sai da extensão).
	conteudo := append([]byte("\x89PNG\r\n\x1a\n"), []byte("dados-do-poster")...)
	if err := os.WriteFile(posterPath, conteudo, 0o644); err != nil {
		t.Fatalf("WriteFile poster: %v", err)
	}
	semearSource(t, d, "src", root)
	semearEntity(t, d, "e", "movie", "Filme")
	semearAsset(t, d, "pa", "e", "poster", posterPath)

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/assets/poster", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, quero image/png", ct)
	}
	if !bytesEqual(rec.Body.Bytes(), conteudo) {
		t.Errorf("corpo do poster diverge do arquivo (len=%d vs %d)", rec.Body.Len(), len(conteudo))
	}
}

func TestGetPosterBloqueiaPathTraversal(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Datasource registrado.
	root := t.TempDir()
	semearSource(t, d, "src", root)

	// (1) Arquivo FORA de qualquer datasource (outro tempdir não registrado).
	fora := t.TempDir()
	arqFora := filepath.Join(fora, "segredo.png")
	if err := os.WriteFile(arqFora, []byte("secreto"), 0o644); err != nil {
		t.Fatalf("WriteFile fora: %v", err)
	}
	semearEntity(t, d, "e1", "movie", "Fora")
	semearAsset(t, d, "a1", "e1", "poster", arqFora)
	rec1 := do(t, h, http.MethodGet, "/api/v1/entities/e1/assets/poster", nil)
	if rec1.Code != http.StatusForbidden {
		t.Fatalf("fora do datasource: status = %d, quero 403 (corpo=%s)", rec1.Code, rec1.Body.String())
	}
	checkErrorShape(t, rec1, "forbidden")

	// (2) Caminho com '..' escapando do datasource: root/../<algo>/etc-fake.
	escapa := filepath.Join(root, "..", "escapa.png")
	semearEntity(t, d, "e2", "movie", "Escapa")
	semearAsset(t, d, "a2", "e2", "poster", escapa)
	rec2 := do(t, h, http.MethodGet, "/api/v1/entities/e2/assets/poster", nil)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("path traversal '..': status = %d, quero 403", rec2.Code)
	}
	checkErrorShape(t, rec2, "forbidden")
}

func TestGetPosterAusente404(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "SemArte")
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/assets/poster", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestLeiturasNaoUsamConexaoDeEscrita prova que TODAS as rotas de leitura usam só
// o pool de leitura: após semear, fechamos o pool de ESCRITA e as leituras
// seguem respondendo 200. Se algum handler tocasse d.Write, quebraria aqui.
func TestLeiturasNaoUsamConexaoDeEscrita(t *testing.T) {
	h, d := setupEntitiesServer(t)
	root := t.TempDir()
	posterPath := filepath.Join(root, "p.png")
	if err := os.WriteFile(posterPath, []byte("\x89PNG\r\n\x1a\nx"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	semearSource(t, d, "src", root)
	semearEntity(t, d, "e", "movie", "Filme")
	semearFile(t, d, "f", "e", "/lib/f.mkv", "present", 1, "")
	semearAsset(t, d, "pa", "e", "poster", posterPath)

	// Fecha SÓ o pool de escrita; o de leitura continua vivo.
	if err := d.Write.Close(); err != nil {
		t.Fatalf("fechar Write: %v", err)
	}

	casos := []string{
		"/api/v1/entities",
		"/api/v1/entities/e",
		"/api/v1/entities/e/files",
		"/api/v1/entities/e/assets/poster",
	}
	for _, alvo := range casos {
		rec := do(t, h, http.MethodGet, alvo, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s após fechar Write: status = %d, quero 200 (corpo=%s)", alvo, rec.Code, rec.Body.String())
		}
	}
}

// bytesEqual compara dois slices de bytes (evita dependência extra).
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
