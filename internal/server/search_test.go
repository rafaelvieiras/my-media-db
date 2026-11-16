package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// fakeProvider implements provider.Provider without network: it returns fixed results (or an
// error), to exercise the search handler deterministically.
type fakeProvider struct {
	resultados []provider.ResultadoExterno
	err        error
	chamadas   int
}

func (f *fakeProvider) Nome() string { return "fake" }

// BuscarDetalhe/MapearDetalhe are not exercised in the search tests; they are stubs only to
// satisfy the extended Provider interface.
func (f *fakeProvider) BuscarDetalhe(_ context.Context, _, _ string, _ []string) (json.RawMessage, error) {
	return nil, errors.New("não implementado")
}

func (f *fakeProvider) MapearDetalhe(_ json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (f *fakeProvider) Buscar(_ context.Context, _, tipo, _ string) ([]provider.ResultadoExterno, error) {
	f.chamadas++
	if f.err != nil {
		return nil, f.err
	}
	if tipo == "" {
		return f.resultados, nil
	}
	out := make([]provider.ResultadoExterno, 0, len(f.resultados))
	for _, r := range f.resultados {
		if r.Type == tipo {
			out = append(out, r)
		}
	}
	return out, nil
}

// setupSearchServer starts a Server with a migrated database and the given options.
func setupSearchServer(t *testing.T, opts ...Option) (http.Handler, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "search.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	h, err := New("v-teste", database, opts...).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database
}

// ptr returns the address of a value (for *string in the fake results).
func ptr[T any](v T) *T { return &v }

// decodeSearch decodes the response of GET /api/v1/search.
func decodeSearch(t *testing.T, body []byte) searchResponseDTO {
	t.Helper()
	var out searchResponseDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar searchResponseDTO: %v (corpo=%s)", err, body)
	}
	return out
}

func TestSearchQObrigatorio(t *testing.T) {
	h, _ := setupSearchServer(t)
	for _, alvo := range []string{"/api/v1/search", "/api/v1/search?q=", "/api/v1/search?q=%20%20"} {
		rec := do(t, h, http.MethodGet, alvo, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, quero 400", alvo, rec.Code)
		}
		checkErrorShape(t, rec, "invalid_body")
	}
}

func TestSearchLocalFTS(t *testing.T) {
	h, d := setupSearchServer(t) // no provider
	semearEntity(t, d, "m", "movie", "The Matrix")
	semearFile(t, d, "mf", "m", "/lib/matrix.mkv", "present", 0.8, "")
	semearEntity(t, d, "o", "movie", "Outro Filme")

	rec := do(t, h, http.MethodGet, "/api/v1/search?q=matrix", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeSearch(t, rec.Body.Bytes())
	eq(t, "query", out.Query, "matrix")
	if len(out.Local) != 1 || out.Local[0].ID != "m" {
		t.Fatalf("local = %+v, quero so o Matrix", out.Local)
	}
	// Card with derived ownership/count (the same shape as the list).
	eq(t, "local[0].owned", out.Local[0].Owned, true)
	eq(t, "local[0].file_count", out.Local[0].FileCount, 1)
	// No provider: external unavailable, empty list (never null).
	eq(t, "external_available", out.ExternalAvailable, false)
	if out.External == nil || len(out.External) != 0 {
		t.Errorf("external = %v, quero [] nao-null", out.External)
	}
}

func TestSearchLocalCasaOriginalTitleETags(t *testing.T) {
	h, d := setupSearchServer(t)
	// original_title and tags different from the title, to prove the multi-column search.
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:            "x",
		Type:          "movie",
		Title:         "Cidade de Deus",
		OriginalTitle: nsOf("City of God"),
		TagsJson:      nsOf(`["favela","crime"]`),
	})
	if err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	// It matches by original_title.
	rec := do(t, h, http.MethodGet, "/api/v1/search?q=city", nil)
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out.Local) != 1 || out.Local[0].ID != "x" {
		t.Errorf("busca por original_title: local = %+v, quero x", out.Local)
	}
	// It matches by tag.
	rec2 := do(t, h, http.MethodGet, "/api/v1/search?q=favela", nil)
	out2 := decodeSearch(t, rec2.Body.Bytes())
	if len(out2.Local) != 1 || out2.Local[0].ID != "x" {
		t.Errorf("busca por tag: local = %+v, quero x", out2.Local)
	}
}

func TestSearchFiltroType(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "f", "movie", "Alfa Filme")
	semearEntity(t, d, "s", "tv", "Alfa Serie")

	rec := do(t, h, http.MethodGet, "/api/v1/search?q=alfa&type=tv", nil)
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out.Local) != 1 || out.Local[0].ID != "s" {
		t.Errorf("type=tv: local = %+v, quero so a serie", out.Local)
	}
}

// TestSearchCardSerieOwnershipRaso: the SERIES card in the SEARCH carries the same SHALLOW
// ownership as the listing (seasons with content / total of seasons). A series with 2 seasons,
// only se1 with an owned episode -> {1,2}. It reuses anotarPosseCard.
func TestSearchCardSerieOwnershipRaso(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "s", "tv", "Alfa Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "se2", "season", "Temporada 2")
	semearEntity(t, d, "e1", "episode", "Ep 1")
	semearEntity(t, d, "e2", "episode", "Ep 2")
	ligarFilhoOrdenado(t, d, "r-se1", "s", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-se2", "s", "se2", "tv_season", 2)
	ligarFilhoOrdenado(t, d, "r-e1", "se1", "e1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-e2", "se2", "e2", "tv_episode", 1)
	semearFile(t, d, "mf1", "s", "/lib/s01e01.mkv", "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "e1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	out := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=alfa", nil).Body.Bytes())
	if len(out.Local) != 1 || out.Local[0].ID != "s" {
		t.Fatalf("local = %+v, quero so a serie", out.Local)
	}
	if out.Local[0].Ownership == nil {
		t.Fatalf("serie na busca sem ownership; quero {1,2} temporadas")
	}
	eq(t, "busca s owned (temporadas com conteudo)", out.Local[0].Ownership.OwnedCount, 1)
	eq(t, "busca s total (temporadas)", out.Local[0].Ownership.TotalCount, 2)
}

// TestSearchSubNosDefaultForaEIncluidos: by default the search only brings top-level works —
// season/episode are left out; the include=season/episode parameter wires each sub-node back in
// a single query, without mixing them (the UI separates by type).
func TestSearchSubNosDefaultForaEIncluidos(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "serie", "tv", "Simpsons Serie")
	semearEntity(t, d, "temp", "season", "Simpsons Temporada 1")
	semearEntity(t, d, "ep", "episode", "Simpsons Episodio 1")

	// (a) Default: only the top-level series; no season/episode.
	out := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons", nil).Body.Bytes())
	if ids := idsLocais(out); len(ids) != 1 || ids[0] != "serie" {
		t.Fatalf("default: local = %v, quero so [serie] (sub-nos fora)", ids)
	}

	// (b) include=season: series + season, without episode.
	outS := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&include=season", nil).Body.Bytes())
	if ids := idsLocais(outS); !contemTodos(ids, "serie", "temp") || contem(ids, "ep") {
		t.Fatalf("include=season: local = %v, quero [serie, temp] sem ep", ids)
	}

	// (c) include=episode: series + episode, without season.
	outE := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&include=episode", nil).Body.Bytes())
	if ids := idsLocais(outE); !contemTodos(ids, "serie", "ep") || contem(ids, "temp") {
		t.Fatalf("include=episode: local = %v, quero [serie, ep] sem temp", ids)
	}

	// (d) include=season,episode (CSV): brings all three.
	outAll := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&include=season,episode", nil).Body.Bytes())
	if ids := idsLocais(outAll); !contemTodos(ids, "serie", "temp", "ep") {
		t.Fatalf("include=season,episode: local = %v, quero os tres", ids)
	}

	// (e) An invalid value in include is ignored (behavior = default).
	outInv := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&include=movie,lixo", nil).Body.Bytes())
	if ids := idsLocais(outInv); len(ids) != 1 || ids[0] != "serie" {
		t.Fatalf("include invalido: local = %v, quero so [serie]", ids)
	}
}

// TestSearchAlbumFaixaSubNoDefaultForaEIncluidos: the same guarantee as
// TestSearchSubNosDefaultForaEIncluidos, but for the sub-nodes of the music vertical
// (ADR-0027/ADR-0030) -- album/track NEVER show up by default in the search (a regression of
// the finding in production: neither of the two old exclusions covered music), and the
// generalized `include` (no longer only season/episode) wires them back.
func TestSearchAlbumFaixaSubNoDefaultForaEIncluidos(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "artista", "artista", "Zeze Musico")
	semearEntity(t, d, "album", "album", "Zeze Album")
	semearEntity(t, d, "faixa", "faixa", "Zeze Faixa")

	// (a) Default: only the top-level artist; no album/track.
	out := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Zeze", nil).Body.Bytes())
	if ids := idsLocais(out); len(ids) != 1 || ids[0] != "artista" {
		t.Fatalf("default: local = %v, quero so [artista] (sub-nos fora)", ids)
	}

	// (b) include=album: artist + album, without track.
	outA := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Zeze&include=album", nil).Body.Bytes())
	if ids := idsLocais(outA); !contemTodos(ids, "artista", "album") || contem(ids, "faixa") {
		t.Fatalf("include=album: local = %v, quero [artista, album] sem faixa", ids)
	}

	// (c) include=album,faixa (CSV): brings all three.
	outAll := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Zeze&include=album,faixa", nil).Body.Bytes())
	if ids := idsLocais(outAll); !contemTodos(ids, "artista", "album", "faixa") {
		t.Fatalf("include=album,faixa: local = %v, quero os tres", ids)
	}
}

// TestSearchEpisodioPossuidoPelaJuncao: with include=episode, the episode card in the search
// marks the ownership coming from the media_file_entity join -- the file has entity_id=SERIES
// and links to the episode only through the join. Without the extension it would come out
// owned=false/file_count=0, diverging from the detail. Consistent with ListarEntityCards.
func TestSearchEpisodioPossuidoPelaJuncao(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "serie", "tv", "Simpsons Serie")
	semearEntity(t, d, "ep", "episode", "Simpsons Episodio 1")
	semearFile(t, d, "mf1", "serie", "/lib/simpsons/s01e01.mkv", "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "ep", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	out := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&include=episode", nil).Body.Bytes())
	var epCard *entityCardDTO
	for i := range out.Local {
		if out.Local[i].ID == "ep" {
			epCard = &out.Local[i]
		}
	}
	if epCard == nil {
		t.Fatalf("busca include=episode nao trouxe o episodio: local = %v", idsLocais(out))
	}
	eq(t, "ep busca owned", epCard.Owned, true)
	eq(t, "ep busca file_count", epCard.FileCount, 1)
}

// TestSearchTypeSubNoComInclude: type=season combined with include=season brings the season
// (before, the hard NOT IN would kill it); type=season alone stays empty.
func TestSearchTypeSubNoComInclude(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "serie", "tv", "Simpsons Serie")
	semearEntity(t, d, "temp", "season", "Simpsons Temporada 1")

	comInclude := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&type=season&include=season", nil).Body.Bytes())
	if ids := idsLocais(comInclude); len(ids) != 1 || ids[0] != "temp" {
		t.Fatalf("type=season&include=season: local = %v, quero so [temp]", ids)
	}

	semInclude := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&type=season", nil).Body.Bytes())
	if ids := idsLocais(semInclude); len(ids) != 0 {
		t.Fatalf("type=season sem include: local = %v, quero [] (sub-no nao religado)", ids)
	}
}

// idsLocais collects the ids of the local cards of the search response.
func idsLocais(out searchResponseDTO) []string {
	ids := make([]string, 0, len(out.Local))
	for _, c := range out.Local {
		ids = append(ids, c.ID)
	}
	return ids
}

// contem tells whether the slice has the value.
func contem(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// contemTodos tells whether the slice has ALL the given values.
func contemTodos(xs []string, vs ...string) bool {
	for _, v := range vs {
		if !contem(xs, v) {
			return false
		}
	}
	return true
}

// TestSearchInputMalicioso ensures that quotes/parentheses/FTS5 operators in the input do NOT
// break the route (no 5xx) — the sanitization neutralizes them.
func TestSearchInputMalicioso(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "m", "movie", "The Matrix")

	casos := []string{
		`matrix" OR 1=1`,
		`(matrix)`,
		`NEAR("a" "b")`,
		`matrix*`,
		`"`,
		`() {} : ^ -`,
		`AND OR NOT`,
		`matrix AND (reloaded`,
	}
	for _, q := range casos {
		rec := do(t, h, http.MethodGet, "/api/v1/search?q="+encodeQuery(q), nil)
		if rec.Code != http.StatusOK {
			t.Errorf("q=%q: status = %d, quero 200 (corpo=%s)", q, rec.Code, rec.Body.String())
		}
	}
}

func TestSearchExternoFakeDisponivel(t *testing.T) {
	fake := &fakeProvider{resultados: []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "603", Type: "movie", Title: "The Matrix", Year: ptr("1999"), PosterURL: ptr("https://img/x.jpg"), Saved: true},
	}}
	h, _ := setupSearchServer(t, WithSearchProvider(fake))

	rec := do(t, h, http.MethodGet, "/api/v1/search?q=matrix", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	out := decodeSearch(t, rec.Body.Bytes())
	eq(t, "external_available", out.ExternalAvailable, true)
	if len(out.External) != 1 {
		t.Fatalf("external = %+v, quero 1", out.External)
	}
	ext := out.External[0]
	eq(t, "external[0].source", ext.Source, "tmdb")
	eq(t, "external[0].source_id", ext.SourceID, "603")
	// saved FORCED to false even if the provider returns true.
	eq(t, "external[0].saved", ext.Saved, false)
	eqPtr(t, "external[0].year", ext.Year, "1999")
}

func TestSearchExternoCacheReusa(t *testing.T) {
	fake := &fakeProvider{resultados: []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "1", Type: "movie", Title: "X"},
	}}
	h, _ := setupSearchServer(t, WithSearchProvider(fake))

	for i := 0; i < 3; i++ {
		rec := do(t, h, http.MethodGet, "/api/v1/search?q=repete", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if fake.chamadas != 1 {
		t.Errorf("provider chamado %d vezes, quero 1 (cache)", fake.chamadas)
	}
}

func TestSearchExternoErroDegrada(t *testing.T) {
	fake := &fakeProvider{err: errors.New("timeout")}
	h, d := setupSearchServer(t, WithSearchProvider(fake))
	semearEntity(t, d, "m", "movie", "The Matrix")

	rec := do(t, h, http.MethodGet, "/api/v1/search?q=matrix", nil)
	// An error in the external one NEVER becomes a 5xx.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 mesmo com externo falho (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeSearch(t, rec.Body.Bytes())
	eq(t, "external_available", out.ExternalAvailable, false)
	if len(out.External) != 0 {
		t.Errorf("external = %+v, quero []", out.External)
	}
	// Local keeps answering.
	if len(out.Local) != 1 || out.Local[0].ID != "m" {
		t.Errorf("local = %+v, quero o Matrix", out.Local)
	}
}

// encodeQuery escapes a value for use in a test query string.
func encodeQuery(v string) string {
	return url.QueryEscape(v)
}
