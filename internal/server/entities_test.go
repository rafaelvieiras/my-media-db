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

// definirCreatedAt sobrescreve created_at direto no banco (CriarEntity nasce
// sempre com o timestamp do DEFAULT, sem parâmetro dedicado) -- usado pelos
// testes de sort=-created_at (0109/0110) para garantir uma ordem
// determinística entre entities criadas na mesma janela de milissegundos.
func definirCreatedAt(t *testing.T, d *db.DB, id, createdAt string) {
	t.Helper()
	if _, err := d.Write.ExecContext(context.Background(),
		"UPDATE entity SET created_at = ? WHERE id = ?", createdAt, id); err != nil {
		t.Fatalf("definirCreatedAt(%s=%s): %v", id, createdAt, err)
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

// TestListEntitiesSortCreatedAtDesc cobre sort=-created_at (briefing 0109): a
// home usa esta ordem para o carrossel "adicionados recentemente". A ordem de
// CRIAÇÃO na semeadura é deliberadamente diferente da ordem de created_at
// forçada abaixo, para provar que a ordenação é por created_at e não por
// ordem de inserção/id.
func TestListEntitiesSortCreatedAtDesc(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "id-x", "movie", "Xeque")
	definirCreatedAt(t, d, "id-x", "2026-01-01T00:00:00.000Z")
	semearEntity(t, d, "id-y", "movie", "Yankee")
	definirCreatedAt(t, d, "id-y", "2026-03-01T00:00:00.000Z")
	semearEntity(t, d, "id-z", "movie", "Zulu")
	definirCreatedAt(t, d, "id-z", "2026-02-01T00:00:00.000Z")

	rec := do(t, h, http.MethodGet, "/api/v1/entities?sort=-created_at", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeList(t, rec.Body.Bytes())
	if len(out.Items) != 3 {
		t.Fatalf("items = %d, quero 3", len(out.Items))
	}
	gotIDs := []string{out.Items[0].ID, out.Items[1].ID, out.Items[2].ID}
	wantIDs := []string{"id-y", "id-z", "id-x"} // mar > fev > jan
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("ordem = %v, quero %v", gotIDs, wantIDs)
		}
	}

	// limit pequeno (uso real do carrossel): só os 2 mais recentes.
	recLim := do(t, h, http.MethodGet, "/api/v1/entities?sort=-created_at&limit=2", nil)
	outLim := decodeList(t, recLim.Body.Bytes())
	if len(outLim.Items) != 2 || outLim.Items[0].ID != "id-y" || outLim.Items[1].ID != "id-z" {
		t.Fatalf("limit=2 = %+v, quero [id-y,id-z]", outLim.Items)
	}
}

// TestListEntityTypes cobre GET /entities/types (briefings 0109/0110/0112):
// tipos DE TOPO distintos catalogados (status != 'draft') com a contagem
// certa -- sem lista fixa hardcoded no frontend. Um item 'wanted' conta
// (catalogado, ainda que sem arquivo); um item 'draft' NÃO conta. album/
// faixa são SUB-NÓS (tiposSubNo, ADR-0030/0112) e NUNCA aparecem, mesmo
// catalogados -- regressão do achado em produção (nenhuma das duas exclusões
// hardcoded antigas cobria música).
func TestListEntityTypes(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "m1", "movie", "M1")
	semearEntity(t, d, "m2", "movie", "M2")
	semearEntity(t, d, "s1", "tv", "S1")
	semearEntity(t, d, "ar1", "artista", "AR1")
	semearEntity(t, d, "al1", "album", "AL1")
	semearEntity(t, d, "al2", "album", "AL2")
	semearEntity(t, d, "al3", "album", "AL3")
	semearEntity(t, d, "fx1", "faixa", "FX1")
	semearEntity(t, d, "w1", "tv", "W1 wanted")
	definirStatus(t, d, "w1", "wanted")
	semearEntity(t, d, "dr1", "artista", "DR1 draft")
	definirStatus(t, d, "dr1", "draft")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/types", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out []entityTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar []entityTypeDTO: %v (corpo=%s)", err, rec.Body.Bytes())
	}
	byType := map[string]int64{}
	for _, tt := range out {
		byType[tt.Type] = tt.Count
	}
	eq(t, "movie count", byType["movie"], int64(2))
	eq(t, "tv count", byType["tv"], int64(2)) // s1 (cataloged) + w1 (wanted)
	eq(t, "artista count", byType["artista"], int64(1))
	if _, ok := byType["album"]; ok {
		t.Errorf("album (sub-nó) não deveria aparecer, byType=%+v", byType)
	}
	if _, ok := byType["faixa"]; ok {
		t.Errorf("faixa (sub-nó) não deveria aparecer, byType=%+v", byType)
	}
}

// TestListEntityTypesNaoColideComGetPorId garante que a rota estática
// /entities/types não é capturada pelo wildcard /entities/{id} (briefing
// 0109/0110): sem uma entity literal de id "types" no banco, o handler de
// tipos responde 200 com a lista -- se a colisão existisse, cairia no
// getEntity e devolveria 404 "entidade não encontrada".
func TestListEntityTypesNaoColideComGetPorId(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "m1", "movie", "M1")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/types", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s) -- rota /types caiu no wildcard /{id}?", rec.Code, rec.Body.String())
	}
	var out []entityTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar []entityTypeDTO: %v (corpo=%s)", err, rec.Body.Bytes())
	}
	if len(out) != 1 || out[0].Type != "movie" || out[0].Count != 1 {
		t.Fatalf("out = %+v, quero [{movie 1}]", out)
	}

	// Um id literal "types" nunca existiu -- GET /entities/{id} normal para um
	// id qualquer que não seja "types" segue 404, provando que a rota estática
	// não "roubou" o wildcard para outros ids.
	rec404 := do(t, h, http.MethodGet, "/api/v1/entities/inexistente", nil)
	if rec404.Code != http.StatusNotFound {
		t.Errorf("GET /entities/inexistente: status = %d, quero 404", rec404.Code)
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

// TestGetEntityOverrideComposto cobre a fiação ponta a ponta do override no
// handler: grava uma linha em media_override (via UPSERT do 0012) e confere que
// GET /{id} reflete o valor composto — título sobreposto — e que extension_data
// aparece como objeto composto.
func TestGetEntityOverrideComposto(t *testing.T) {
	h, d := setupEntitiesServer(t)
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:                "x",
		Type:              "movie",
		Title:             "Título Original",
		TagsJson:          nsOf(`["a","b"]`),
		ExtensionDataJson: nsOf(`{"music_video":{"bpm":120}}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	// Patch do dono: sobrepõe o título, remove a tag "a" e ajusta o bpm.
	_, err = escrita(t, d).UpsertMediaOverride(context.Background(), sqlcgen.UpsertMediaOverrideParams{
		EntityID:     "x",
		OverrideJson: nsOf(`{"title":"Título do Dono","tags":{"remove":["a"]},"extension_data":{"music_video":{"bpm":128}}}`),
	})
	if err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/x", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar Entidade: %v (corpo=%s)", err, rec.Body.String())
	}
	eq(t, "title", ent.Title, "Título do Dono")
	// tags: base [a,b], remove a → [b].
	if len(ent.Tags) != 1 || ent.Tags[0] != "b" {
		t.Errorf("tags = %v, quero [b]", ent.Tags)
	}
	mv, ok := ent.ExtensionData["music_video"].(map[string]any)
	if !ok {
		t.Fatalf("extension_data.music_video ausente: %#v", ent.ExtensionData)
	}
	if bpm, _ := mv["bpm"].(float64); bpm != 128 {
		t.Errorf("bpm = %v, quero 128 (override)", mv["bpm"])
	}
}

// TestGetEntitySemOverrideExtensionDataVazio garante que, sem override, o shape
// resolvido traz extension_data como objeto (nunca null).
func TestGetEntitySemOverrideExtensionDataVazio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Filme")
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	ed, ok := raw["extension_data"]
	if !ok || string(ed) != "{}" {
		t.Errorf("extension_data = %s, quero {} (presente e não-null)", ed)
	}
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

// obterResolvida faz GET /entities/{id} e decodifica o ResolvedEntity (helper local
// para os testes de leitura da arvore de series).
func obterResolvida(t *testing.T, h http.Handler, id string) resolver.Entidade {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, quero 200 (corpo=%s)", id, rec.Code, rec.Body.String())
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar Entidade(%s): %v", id, err)
	}
	return ent
}

// TestSerieLeituraPosseEArquivosDeEpisodio cobre a LEITURA do 0034 (Passo 3):
//   - o arquivo de serie tem media_file.entity_id = SERIE, e o vinculo por episodio
//     vive em media_file_entity (kind='tv_episode'); o detalhe/arquivos do EPISODIO
//     vem da juncao, nao de media_file.entity_id;
//   - temporada expoe posse agregada (episodios possuidos / total dos filhos diretos);
//   - serie soma across temporadas (a query direta contaria zero);
//   - filme nao tem ownership (null).
func TestSerieLeituraPosseEArquivosDeEpisodio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "ep1", "episode", "Ep 1")
	semearEntity(t, d, "ep2", "episode", "Ep 2")
	semearEntity(t, d, "ep3", "episode", "Ep 3")
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-ep1", "se1", "ep1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-ep2", "se1", "ep2", "tv_episode", 2)
	ligarFilhoOrdenado(t, d, "r-ep3", "se1", "ep3", "tv_episode", 3)
	// Arquivo de serie: entity_id = SERIE (a obra), vinculado ao episodio 1 pela juncao.
	semearFile(t, d, "mf1", "s1", "/lib/serie/s01e01.mkv", "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "ep1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	// Episodio 1: /files traz o arquivo via juncao, mesmo com entity_id apontando a serie.
	rec := do(t, h, http.MethodGet, "/api/v1/entities/ep1/files", nil)
	var files filesListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("decodificar filesListDTO: %v", err)
	}
	if len(files.Items) != 1 || files.Items[0].Path != "/lib/serie/s01e01.mkv" {
		t.Fatalf("arquivos do ep1 = %+v, quero 1 arquivo s01e01", files.Items)
	}
	ep1 := obterResolvida(t, h, "ep1")
	eq(t, "ep1 owned", ep1.Owned, true)
	eq(t, "ep1 file_count", ep1.FileCount, 1)
	if ep1.Ownership != nil {
		t.Errorf("ep1 ownership = %+v, quero nil (episodio nao agrega)", ep1.Ownership)
	}

	// Episodio 2: sem arquivo vinculado.
	ep2 := obterResolvida(t, h, "ep2")
	eq(t, "ep2 owned", ep2.Owned, false)
	eq(t, "ep2 file_count", ep2.FileCount, 0)

	// Temporada: 1 de 3 episodios possuidos.
	se1 := obterResolvida(t, h, "se1")
	if se1.Ownership == nil {
		t.Fatalf("se1 ownership = nil, quero {1,3}")
	}
	eq(t, "se1 owned_count", se1.Ownership.OwnedCount, 1)
	eq(t, "se1 total_count", se1.Ownership.TotalCount, 3)

	// Serie: soma across temporadas (a query direta contaria zero) -> 1 de 3.
	s1 := obterResolvida(t, h, "s1")
	if s1.Ownership == nil {
		t.Fatalf("s1 ownership = nil, quero {1,3}")
	}
	eq(t, "s1 owned_count", s1.Ownership.OwnedCount, 1)
	eq(t, "s1 total_count", s1.Ownership.TotalCount, 3)

	// Filme: sem agregacao de posse (ownership null).
	semearEntity(t, d, "m1", "movie", "Filme")
	m1 := obterResolvida(t, h, "m1")
	if m1.Ownership != nil {
		t.Errorf("m1 ownership = %+v, quero nil (filme)", m1.Ownership)
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

func TestGetEntityFanartServe(t *testing.T) {
	h, d := setupEntitiesServer(t)
	root := t.TempDir()
	fanartArq := filepath.Join(root, "fanart.png")
	conteudo := append([]byte("\x89PNG\r\n\x1a\n"), []byte("dados-do-fundo")...)
	if err := os.WriteFile(fanartArq, conteudo, 0o644); err != nil {
		t.Fatalf("WriteFile fanart: %v", err)
	}
	semearSource(t, d, "src", root)
	semearEntity(t, d, "e", "movie", "Filme")
	semearAsset(t, d, "fa", "e", "fanart", fanartArq)

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/assets/fanart", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if !bytesEqual(rec.Body.Bytes(), conteudo) {
		t.Errorf("corpo do fundo diverge do arquivo (len=%d)", rec.Body.Len())
	}
}

func TestGetEntityFanartAusente404(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "SemFundo")
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e/assets/fanart", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestResolveBackdropLocalQuandoHaFanart: com asset fanart, GET /{id} serve o
// backdrop_url como a rota LOCAL (não a URL direta da fonte).
func TestResolveBackdropLocalQuandoHaFanart(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "ComFundo")
	semearAsset(t, d, "fa", "e", "fanart", "/lib/fundo.jpg")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar Entidade: %v", err)
	}
	eqPtr(t, "backdrop_url", ent.BackdropURL, "/api/v1/entities/e/assets/fanart")
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
