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

// fakeProvider implementa provider.Provider sem rede: devolve resultados fixos
// (ou um erro), para exercitar o handler de busca de forma determinista.
type fakeProvider struct {
	resultados []provider.ResultadoExterno
	err        error
	chamadas   int
}

func (f *fakeProvider) Nome() string { return "fake" }

// BuscarDetalhe/MapearDetalhe não são exercitados nos testes de busca (0014);
// stubs só para satisfazer a interface Provider estendida no 0015.
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

// setupSearchServer sobe um Server com banco migrado e as options dadas.
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

// ptr devolve o endereco de um valor (para *string nos resultados fake).
func ptr[T any](v T) *T { return &v }

// decodeSearch decodifica a resposta de GET /api/v1/search.
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
	h, d := setupSearchServer(t) // sem provider
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
	// Card com posse/contagem derivadas (mesmo shape da lista).
	eq(t, "local[0].owned", out.Local[0].Owned, true)
	eq(t, "local[0].file_count", out.Local[0].FileCount, 1)
	// Sem provider: externo indisponivel, lista vazia (nunca null).
	eq(t, "external_available", out.ExternalAvailable, false)
	if out.External == nil || len(out.External) != 0 {
		t.Errorf("external = %v, quero [] nao-null", out.External)
	}
}

func TestSearchLocalCasaOriginalTitleETags(t *testing.T) {
	h, d := setupSearchServer(t)
	// original_title e tags diferentes do title, para provar a busca multi-coluna.
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

	// Casa por original_title.
	rec := do(t, h, http.MethodGet, "/api/v1/search?q=city", nil)
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out.Local) != 1 || out.Local[0].ID != "x" {
		t.Errorf("busca por original_title: local = %+v, quero x", out.Local)
	}
	// Casa por tag.
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

// TestSearchCardSerieOwnershipRaso (0037): o card de SERIE na BUSCA carrega o mesmo
// ownership RASO da listagem (temporadas com conteudo / total de temporadas). Serie
// com 2 temporadas, so a se1 com episodio possuido -> {1,2}. Reusa anotarPosseCard.
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

// TestSearchSubNosDefaultForaEIncluidos: por padrao a busca so traz obras de topo
// (0033) — season/episode ficam de fora; o parametro include=season/episode religa
// cada sub-no numa consulta so, sem misturar (a UI separa por type).
func TestSearchSubNosDefaultForaEIncluidos(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "serie", "tv", "Simpsons Serie")
	semearEntity(t, d, "temp", "season", "Simpsons Temporada 1")
	semearEntity(t, d, "ep", "episode", "Simpsons Episodio 1")

	// (a) Default: so a serie de topo; nada de season/episode.
	out := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons", nil).Body.Bytes())
	if ids := idsLocais(out); len(ids) != 1 || ids[0] != "serie" {
		t.Fatalf("default: local = %v, quero so [serie] (sub-nos fora)", ids)
	}

	// (b) include=season: serie + temporada, sem episodio.
	outS := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&include=season", nil).Body.Bytes())
	if ids := idsLocais(outS); !contemTodos(ids, "serie", "temp") || contem(ids, "ep") {
		t.Fatalf("include=season: local = %v, quero [serie, temp] sem ep", ids)
	}

	// (c) include=episode: serie + episodio, sem temporada.
	outE := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&include=episode", nil).Body.Bytes())
	if ids := idsLocais(outE); !contemTodos(ids, "serie", "ep") || contem(ids, "temp") {
		t.Fatalf("include=episode: local = %v, quero [serie, ep] sem temp", ids)
	}

	// (d) include=season,episode (CSV): traz os tres.
	outAll := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&include=season,episode", nil).Body.Bytes())
	if ids := idsLocais(outAll); !contemTodos(ids, "serie", "temp", "ep") {
		t.Fatalf("include=season,episode: local = %v, quero os tres", ids)
	}

	// (e) Valor invalido no include e ignorado (comportamento = default).
	outInv := decodeSearch(t, do(t, h, http.MethodGet, "/api/v1/search?q=Simpsons&include=movie,lixo", nil).Body.Bytes())
	if ids := idsLocais(outInv); len(ids) != 1 || ids[0] != "serie" {
		t.Fatalf("include invalido: local = %v, quero so [serie]", ids)
	}
}

// TestSearchEpisodioPossuidoPelaJuncao: com include=episode, o card do episodio na
// busca marca a posse vinda da juncao media_file_entity (0034) -- o arquivo tem
// entity_id=SERIE e liga ao episodio so pela juncao. Sem a extensao, viria
// owned=false/file_count=0, divergindo do detalhe. Consistente com ListarEntityCards.
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

// TestSearchTypeSubNoComInclude: type=season combinado com include=season traz a
// temporada (antes o NOT IN dura a mataria); type=season sozinho continua vazio.
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

// idsLocais coleta os ids dos cards locais da resposta de busca.
func idsLocais(out searchResponseDTO) []string {
	ids := make([]string, 0, len(out.Local))
	for _, c := range out.Local {
		ids = append(ids, c.ID)
	}
	return ids
}

// contem diz se a fatia tem o valor.
func contem(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// contemTodos diz se a fatia tem TODOS os valores dados.
func contemTodos(xs []string, vs ...string) bool {
	for _, v := range vs {
		if !contem(xs, v) {
			return false
		}
	}
	return true
}

// TestSearchInputMalicioso garante que aspas/parenteses/operadores do FTS5 no
// input NAO quebram a rota (nada de 5xx) — a sanitizacao os neutraliza.
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
	// saved FORCADO a false mesmo que o provider devolva true.
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
	// Erro no externo NUNCA vira 5xx.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 mesmo com externo falho (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeSearch(t, rec.Body.Bytes())
	eq(t, "external_available", out.ExternalAvailable, false)
	if len(out.External) != 0 {
		t.Errorf("external = %+v, quero []", out.External)
	}
	// Local segue respondendo.
	if len(out.Local) != 1 || out.Local[0].ID != "m" {
		t.Errorf("local = %+v, quero o Matrix", out.Local)
	}
}

// encodeQuery escapa um valor para uso em query string de teste.
func encodeQuery(v string) string {
	return url.QueryEscape(v)
}
