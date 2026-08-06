package enrich

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// svcSerieFake monta o Servico com o provider fake (que implementa SerieProvider) e um
// servidor de imagem para o cache de pôster/still da árvore.
func svcSerieFake(t *testing.T) (*Servico, *db.DB) {
	t.Helper()
	database := setupDB(t)
	img := servidorImagem(t, "image/png", []byte("PNG-FAKE"))
	svc := New(database, provider.NovaRegistry(fake.New(img.URL)), t.TempDir())
	return svc, database
}

// TestImportarSerieCriaArvore: importar type=tv cria a série (tv) + 2 temporadas
// (season, sort_index 1..2) + 6 episódios (episode) com numbering multi-ordem e
// absolute_number, ancorados no id externo.
func TestImportarSerieCriaArvore(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	serieID, criada, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"})
	if err != nil {
		t.Fatalf("Importar serie: %v", err)
	}
	if !criada {
		t.Error("criada = false, quero true (serie nova)")
	}
	r := sqlcgen.New(database.Read)
	exigirSerieRaiz(t, r, serieID)
	exigirTemporadas(t, database, r, serieID)
	exigirEpisodioDivergente(t, r)
}

// exigirSerieRaiz confere a linha-base da serie: type tv, id externo e o extension_data
// core.tv@1 (default_season_type + season_types).
func exigirSerieRaiz(t *testing.T, r *sqlcgen.Queries, serieID string) {
	t.Helper()
	serie, err := r.ObterEntity(context.Background(), serieID)
	if err != nil {
		t.Fatalf("ObterEntity serie: %v", err)
	}
	if serie.Type != tipoSerie {
		t.Errorf("serie.type = %q, quero tv", serie.Type)
	}
	if serie.ExternalIDTmdb.String != fake.SerieFixtureID {
		t.Errorf("serie external_id_tmdb = %q, quero %s", serie.ExternalIDTmdb.String, fake.SerieFixtureID)
	}
	ns := extNS(t, serie.ExtensionDataJson.String)
	if ns["default_season_type"] != "official" {
		t.Errorf("default_season_type = %v, quero official", ns["default_season_type"])
	}
	if tipos := aStrings(ns["season_types"]); len(tipos) != 3 {
		t.Errorf("season_types = %v, quero 3 ordens", ns["season_types"])
	}
}

// exigirTemporadas confere as 2 temporadas (sort_index 1,2), o titulo fallback
// "Temporada N" (sem Name), o Name usado (com Name) e o extension_data da temporada.
func exigirTemporadas(t *testing.T, database *db.DB, r *sqlcgen.Queries, serieID string) {
	t.Helper()
	ctx := context.Background()
	temps := filhosPorKind(t, database, serieID, relacaoTemporada)
	if len(temps) != 2 {
		t.Fatalf("temporadas = %d, quero 2", len(temps))
	}
	if temps[0].SortIndex.Int64 != 1 || temps[1].SortIndex.Int64 != 2 {
		t.Errorf("sort_index das temporadas = %d,%d, quero 1,2", temps[0].SortIndex.Int64, temps[1].SortIndex.Int64)
	}
	t1, _ := r.ObterEntity(ctx, temps[0].ChildID)
	if t1.Type != tipoTemporada || t1.Title != "Temporada 1" {
		t.Errorf("temporada 1 = {type:%q title:%q}, quero {season, Temporada 1}", t1.Type, t1.Title)
	}
	nsT1 := extNS(t, t1.ExtensionDataJson.String)
	if nsT1["season_number"] != float64(1) || nsT1["year"] != "1989" {
		t.Errorf("extension T1 = %v, quero season_number 1 / year 1989", nsT1)
	}
	if t2, _ := r.ObterEntity(ctx, temps[1].ChildID); t2.Title != "Second Season" {
		t.Errorf("temporada 2 title = %q, quero Second Season (usa Name)", t2.Title)
	}
	exigirEpisodiosOrdenados(t, database, temps[0].ChildID)
}

// exigirEpisodiosOrdenados confere que a T1 tem 3 episodios com sort_index = numero
// official 1,2,3.
func exigirEpisodiosOrdenados(t *testing.T, database *db.DB, tempID string) {
	t.Helper()
	eps := filhosPorKind(t, database, tempID, relacaoEpisodio)
	if len(eps) != 3 {
		t.Fatalf("episodios T1 = %d, quero 3", len(eps))
	}
	for i, want := range []int64{1, 2, 3} {
		if eps[i].SortIndex.Int64 != want {
			t.Errorf("sort_index ep[%d] = %d, quero %d", i, eps[i].SortIndex.Int64, want)
		}
	}
}

// exigirEpisodioDivergente confere o ep 103: type episode, id externo (tvdb),
// absolute_number e o numbering DIVERGENTE entre official{1,3} e dvd{1,5}.
func exigirEpisodioDivergente(t *testing.T, r *sqlcgen.Queries) {
	t.Helper()
	ep, err := r.ObterEntityPorTmdb(context.Background(), nsValid("103"))
	if err != nil {
		t.Fatalf("episodio 103 nao encontrado: %v", err)
	}
	if ep.Type != tipoEpisodio || ep.Title != "Episode 103" {
		t.Errorf("ep103 = {type:%q title:%q}, quero {episode, Episode 103}", ep.Type, ep.Title)
	}
	if ep.ExternalIDTvdb.String != "9103" {
		t.Errorf("ep103 external_id_tvdb = %q, quero 9103", ep.ExternalIDTvdb.String)
	}
	ns := extNS(t, ep.ExtensionDataJson.String)
	if ns["absolute_number"] != float64(3) {
		t.Errorf("absolute_number ep103 = %v, quero 3", ns["absolute_number"])
	}
	numbering, _ := ns["numbering"].(map[string]any)
	official, _ := numbering["official"].(map[string]any)
	dvd, _ := numbering["dvd"].(map[string]any)
	if official["season"] != float64(1) || official["number"] != float64(3) {
		t.Errorf("official ep103 = %v, quero {1,3}", official)
	}
	if dvd["season"] != float64(1) || dvd["number"] != float64(5) {
		t.Errorf("dvd ep103 = %v, quero {1,5} (numero divergente)", dvd)
	}
}

// TestImportarSerieGravaMediaExternalDoEpisodio (0041): cada episódio da árvore grava
// UMA linha media_external per-fonte cujo payload carrega os mapas title/overview POR
// IDIOMA (não só o escalar default) sob o discriminador entity_kind — o trilho i18n do
// filme aplicado ao episódio. Sem isso o detalhe do episódio não traduziria por ?lang=.
func TestImportarSerieGravaMediaExternalDoEpisodio(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("Importar serie: %v", err)
	}
	env := envelopeEpisodioDe(t, database, "103")
	if env.EntityKind != provider.KindEpisodio {
		t.Errorf("entity_kind = %q, quero %q", env.EntityKind, provider.KindEpisodio)
	}
	// O fake serve title en+pt-BR+pt e overview en+pt-BR para o episódio (idiomas que a
	// árvore trouxe) — o payload preserva TODOS, não só o idioma default.
	if env.Title["en"] != "Episode 103" || env.Title["pt-BR"] != "Episodio 103" || env.Title["pt"] != "Episodio 103" {
		t.Errorf("title por idioma = %v, quero en/pt-BR/pt preenchidos", env.Title)
	}
	if env.Overview["en"] != "Overview of episode 103" || env.Overview["pt-BR"] != "Sinopse do episodio 103" {
		t.Errorf("overview por idioma = %v, quero en/pt-BR preenchidos", env.Overview)
	}
}

// TestReimportarEpisodioReescreveMediaExternal (0041): re-importar a série REESCREVE o
// media_external do episódio já existente (idempotente por (entity_id, source)) — não
// duplica linha nem retorna cedo sem reescrever (o bug do caminho de reuse).
func TestReimportarEpisodioReescreveMediaExternal(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("1o import: %v", err)
	}
	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("2o import: %v", err)
	}
	// A UNIQUE(entity_id, source) garante uma linha só; ObterMediaExternalPorFonte é :one
	// (erraria se houvesse duplicata) e o payload segue com os mapas per-idioma.
	env := envelopeEpisodioDe(t, database, "103")
	if env.EntityKind != provider.KindEpisodio || env.Title["pt-BR"] != "Episodio 103" {
		t.Errorf("payload apos reimport = %+v, quero episodio traduzido intacto", env)
	}
}

// envelopeEpisodioDe lê o media_external (fonte tmdb) do episódio de tmdbID e decodifica
// o EnvelopeEpisodio do payload_json.
func envelopeEpisodioDe(t *testing.T, database *db.DB, tmdbID string) provider.EnvelopeEpisodio {
	t.Helper()
	r := sqlcgen.New(database.Read)
	ep, err := r.ObterEntityPorTmdb(context.Background(), nsValid(tmdbID))
	if err != nil {
		t.Fatalf("episodio %s nao encontrado: %v", tmdbID, err)
	}
	row, err := r.ObterMediaExternalPorFonte(context.Background(), sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: ep.ID,
		Source:   "tmdb",
	})
	if err != nil {
		t.Fatalf("media_external do episodio %s: %v", tmdbID, err)
	}
	var env provider.EnvelopeEpisodio
	if err := json.Unmarshal([]byte(row.PayloadJson.String), &env); err != nil {
		t.Fatalf("payload_json invalido (%q): %v", row.PayloadJson.String, err)
	}
	return env
}

// TestReimportarSerieIdempotente: re-importar a mesma série reusa a série, as
// temporadas (dedup por numero) e os episódios (dedup por id externo) — sem duplicar.
func TestReimportarSerieIdempotente(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	id1, criada1, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"})
	if err != nil || !criada1 {
		t.Fatalf("1o import: id=%s criada=%v err=%v", id1, criada1, err)
	}
	id2, criada2, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"})
	if err != nil {
		t.Fatalf("2o import: %v", err)
	}
	if id2 != id1 {
		t.Errorf("reimport criou nova serie: %s != %s", id2, id1)
	}
	if criada2 {
		t.Error("criada = true no reimport, quero false")
	}
	temps := filhosPorKind(t, database, id1, relacaoTemporada)
	if len(temps) != 2 {
		t.Errorf("temporadas apos reimport = %d, quero 2 (sem duplicar)", len(temps))
	}
	total := 0
	for _, tp := range temps {
		total += len(filhosPorKind(t, database, tp.ChildID, relacaoEpisodio))
	}
	if total != 6 {
		t.Errorf("episodios apos reimport = %d, quero 6 (sem duplicar)", total)
	}
}

// TestImportarSerieSemSuporte: uma fonte que NAO implementa SerieProvider devolve erro
// (o handler traduz para 502).
func TestImportarSerieSemSuporte(t *testing.T) {
	database := setupDB(t)
	fakeProv := &fakeProvider{raw: json.RawMessage(`{}`), campos: map[string]any{}}
	svc, _ := novoServico(t, database, fakeProv)
	if _, _, err := svc.Importar(context.Background(), "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err == nil {
		t.Fatal("quero erro: provider sem suporte a serie")
	}
}

// TestImportarSeriePorNomeCasaExato: o nome exato de uma série no fake resolve para o
// candidato tv único e importa a árvore (o wiring que o scanner de 0034 consome).
func TestImportarSeriePorNomeCasaExato(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	id, err := svc.ImportarSeriePorNome(ctx, "Springfield Tales", []string{"en"}, "")
	if err != nil {
		t.Fatalf("ImportarSeriePorNome: %v", err)
	}
	r := sqlcgen.New(database.Read)
	serie, err := r.ObterEntity(ctx, id)
	if err != nil || serie.Type != tipoSerie {
		t.Fatalf("serie importada invalida: %+v err=%v", serie, err)
	}
	if serie.ExternalIDTmdb.String != fake.SerieFixtureID {
		t.Errorf("serie external_id_tmdb = %q, quero %s", serie.ExternalIDTmdb.String, fake.SerieFixtureID)
	}
	// A árvore veio junto (2 temporadas).
	if temps := filhosPorKind(t, database, id, relacaoTemporada); len(temps) != 2 {
		t.Errorf("temporadas = %d, quero 2 (árvore importada)", len(temps))
	}
}

// TestImportarSeriePorNomeNaoEncontrada: um nome sem candidato nenhum devolve o
// sentinela ErrSerieNaoEncontrada (o scanner roteia para revisão).
func TestImportarSeriePorNomeNaoEncontrada(t *testing.T) {
	svc, _ := svcSerieFake(t)
	_, err := svc.ImportarSeriePorNome(context.Background(), "Serie Inexistente XYZ", []string{"en"}, "")
	if !errors.Is(err, ErrSerieNaoEncontrada) {
		t.Fatalf("err = %v, quero ErrSerieNaoEncontrada", err)
	}
}

// TestImportarSeriePorNomeAmbigua: dois candidatos de título exato na mesma fonte
// devolvem ErrSerieAmbigua — o scanner NÃO adivinha.
func TestImportarSeriePorNomeAmbigua(t *testing.T) {
	database := setupDB(t)
	svc := New(database, provider.NovaRegistry(serieDupla{}), t.TempDir())
	_, err := svc.ImportarSeriePorNome(context.Background(), "Dup Show", []string{"en"}, "")
	if !errors.Is(err, ErrSerieAmbigua) {
		t.Fatalf("err = %v, quero ErrSerieAmbigua", err)
	}
}

// serieDupla é um provider de teste que devolve DOIS resultados tv de título idêntico
// para "Dup Show" — o caso ambíguo. Implementa SerieProvider para não ser pulado.
type serieDupla struct{}

func (serieDupla) Nome() string { return "dup" }
func (serieDupla) Buscar(_ context.Context, q, tipo, _ string) ([]provider.ResultadoExterno, error) {
	if tipo == tipoSerie && q == "Dup Show" {
		return []provider.ResultadoExterno{
			{Source: "dup", SourceID: "1", Type: "tv", Title: "Dup Show"},
			{Source: "dup", SourceID: "2", Type: "tv", Title: "Dup Show"},
		}, nil
	}
	return nil, nil
}
func (serieDupla) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (serieDupla) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}
func (serieDupla) BuscarSerieArvore(context.Context, string, []string) (provider.SerieArvore, error) {
	return provider.SerieArvore{}, nil
}

// --- helpers de teste ---

// extNS decodifica o extension_data_json e devolve o objeto do namespace core.tv@1.
func extNS(t *testing.T, raw string) map[string]any {
	t.Helper()
	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("extension_data invalido (%q): %v", raw, err)
	}
	ns, ok := envelope[nsTV]
	if !ok {
		t.Fatalf("extension_data sem namespace %s: %q", nsTV, raw)
	}
	return ns
}

// filhosPorKind lista as relações de um pai filtradas por kind, na ordem de
// ListarRelacoesFilhas (sort_index).
func filhosPorKind(t *testing.T, database *db.DB, parentID, kind string) []sqlcgen.EntityRelation {
	t.Helper()
	rels, err := sqlcgen.New(database.Read).ListarRelacoesFilhas(context.Background(), parentID)
	if err != nil {
		t.Fatalf("ListarRelacoesFilhas: %v", err)
	}
	var out []sqlcgen.EntityRelation
	for _, r := range rels {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// aStrings coage um []any (de JSON) para []string.
func aStrings(v any) []string {
	lista, _ := v.([]any)
	out := make([]string, 0, len(lista))
	for _, e := range lista {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
