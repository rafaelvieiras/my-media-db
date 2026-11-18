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

// svcSerieFake builds the Servico with the fake provider (which implements SerieProvider)
// and an image server for the poster/still cache of the tree.
func svcSerieFake(t *testing.T) (*Servico, *db.DB) {
	t.Helper()
	database := setupDB(t)
	img := servidorImagem(t, "image/png", []byte("PNG-FAKE"))
	svc := New(database, provider.NovaRegistry(fake.New(img.URL)), t.TempDir())
	return svc, database
}

// TestImportarSerieCriaArvore: importing type=tv creates the series (tv) + 2 seasons
// (season, sort_index 1..2) + 6 episodes (episode) with multi-order numbering and
// absolute_number, anchored on the external id.
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

// exigirSerieRaiz checks the base row of the series: type tv, external id and the
// core.tv@1 extension_data (default_season_type + season_types).
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

// exigirTemporadas checks the 2 seasons (sort_index 1,2), the "Temporada N" fallback
// title (without a Name), the Name used (with a Name) and the extension_data of the season.
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

// exigirEpisodiosOrdenados checks that S1 has 3 episodes with sort_index = the official
// number 1,2,3.
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

// exigirEpisodioDivergente checks ep 103: type episode, external id (tvdb),
// absolute_number and the numbering DIVERGING between official{1,3} and dvd{1,5}.
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

// TestImportarSerieGravaMediaExternalDoEpisodio: every episode of the tree writes
// ONE per-source media_external row whose payload carries the PER-LANGUAGE title/overview
// maps (not only the default scalar) under the entity_kind discriminator — the i18n track
// of the movie applied to the episode. Without it the episode detail would not translate
// through ?lang=.
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
	// The fake serves title en+pt-BR+pt and overview en+pt-BR for the episode (languages that
	// the tree brought) — the payload preserves ALL of them, not only the default language.
	if env.Title["en"] != "Episode 103" || env.Title["pt-BR"] != "Episodio 103" || env.Title["pt"] != "Episodio 103" {
		t.Errorf("title por idioma = %v, quero en/pt-BR/pt preenchidos", env.Title)
	}
	if env.Overview["en"] != "Overview of episode 103" || env.Overview["pt-BR"] != "Sinopse do episodio 103" {
		t.Errorf("overview por idioma = %v, quero en/pt-BR preenchidos", env.Overview)
	}
}

// TestReimportarEpisodioReescreveMediaExternal: re-importing the series REWRITES the
// media_external of the already existing episode (idempotent by (entity_id, source)) — it
// neither duplicates the row nor returns early without rewriting (the bug of the reuse path).
func TestReimportarEpisodioReescreveMediaExternal(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("1o import: %v", err)
	}
	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("2o import: %v", err)
	}
	// The UNIQUE(entity_id, source) guarantees a single row; ObterMediaExternalPorFonte is :one
	// (it would fail if there were a duplicate) and the payload keeps the per-language maps.
	env := envelopeEpisodioDe(t, database, "103")
	if env.EntityKind != provider.KindEpisodio || env.Title["pt-BR"] != "Episodio 103" {
		t.Errorf("payload apos reimport = %+v, quero episodio traduzido intacto", env)
	}
}

// envelopeEpisodioDe reads the media_external (source tmdb) of the episode with tmdbID and
// decodes the EnvelopeEpisodio of payload_json.
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

// TestReimportarSerieIdempotente: re-importing the same series reuses the series, the
// seasons (dedup by number) and the episodes (dedup by external id) — without duplicating.
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

// TestImportarSerieSemSuporte: a source that does NOT implement SerieProvider returns an
// error (the handler translates it to 502).
func TestImportarSerieSemSuporte(t *testing.T) {
	database := setupDB(t)
	fakeProv := &fakeProvider{raw: json.RawMessage(`{}`), campos: map[string]any{}}
	svc, _ := novoServico(t, database, fakeProv)
	if _, _, err := svc.Importar(context.Background(), "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err == nil {
		t.Fatal("quero erro: provider sem suporte a serie")
	}
}

// TestImportarSeriePorNomeCasaExato: the exact name of a series in the fake resolves to
// the single tv candidate and imports the tree (the wiring the series scanner consumes).
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
	// The tree came along (2 seasons).
	if temps := filhosPorKind(t, database, id, relacaoTemporada); len(temps) != 2 {
		t.Errorf("temporadas = %d, quero 2 (árvore importada)", len(temps))
	}
}

// TestImportarSeriePorNomeNaoEncontrada: a name with no candidate at all returns the
// ErrSerieNaoEncontrada sentinel (the scanner routes it to review).
func TestImportarSeriePorNomeNaoEncontrada(t *testing.T) {
	svc, _ := svcSerieFake(t)
	_, err := svc.ImportarSeriePorNome(context.Background(), "Serie Inexistente XYZ", []string{"en"}, "")
	if !errors.Is(err, ErrSerieNaoEncontrada) {
		t.Fatalf("err = %v, quero ErrSerieNaoEncontrada", err)
	}
}

// TestImportarSeriePorNomeAmbigua: two candidates with an exact title in the same source
// return ErrSerieAmbigua — the scanner does NOT guess.
func TestImportarSeriePorNomeAmbigua(t *testing.T) {
	database := setupDB(t)
	svc := New(database, provider.NovaRegistry(serieDupla{}), t.TempDir())
	_, err := svc.ImportarSeriePorNome(context.Background(), "Dup Show", []string{"en"}, "")
	if !errors.Is(err, ErrSerieAmbigua) {
		t.Fatalf("err = %v, quero ErrSerieAmbigua", err)
	}
}

// serieDupla is a test provider that returns TWO tv results with an identical title
// for "Dup Show" — the ambiguous case. It implements SerieProvider so that it is not skipped.
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

// --- test helpers ---

// extNS decodes extension_data_json and returns the object of the core.tv@1 namespace.
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

// filhosPorKind lists the relations of a parent filtered by kind, in the order of
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

// aStrings coerces a []any (from JSON) into a []string.
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

// TestImportarSeriePorIDIgnoraOTitulo proves the id door of the IMPORT: the folder names
// the series by id, so the (possibly misspelled) title is never searched. This is the fix
// for "2 Broken Girls": TMDb answers zero for that name, but the id resolves.
func TestImportarSeriePorIDIgnoraOTitulo(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	id, err := svc.ImportarSeriePorID(ctx, "tmdb", fake.SerieFixtureID, []string{"en"})
	if err != nil {
		t.Fatalf("ImportarSeriePorID: %v", err)
	}
	serie, err := sqlcgen.New(database.Read).ObterEntity(ctx, id)
	if err != nil || serie.Type != tipoSerie {
		t.Fatalf("serie importada invalida: %+v err=%v", serie, err)
	}
	if serie.ExternalIDTmdb.String != fake.SerieFixtureID {
		t.Errorf("external_id_tmdb = %q, quero %s", serie.ExternalIDTmdb.String, fake.SerieFixtureID)
	}
	if temps := filhosPorKind(t, database, id, relacaoTemporada); len(temps) != 2 {
		t.Errorf("temporadas = %d, quero 2 (árvore importada)", len(temps))
	}
}

// TestImportarSeriePorIDNaoImportaFilme: the same numeric id can belong to a FILM; the
// series scanner must never import the film and call it a series.
func TestImportarSeriePorIDNaoImportaFilme(t *testing.T) {
	svc, database := svcSerieFake(t)
	_, err := svc.ImportarSeriePorID(context.Background(), "tmdb", "603", []string{"en"})
	if !errors.Is(err, ErrSerieNaoEncontrada) {
		t.Fatalf("err = %v, quero ErrSerieNaoEncontrada", err)
	}
	var n int
	if err := database.Read.QueryRowContext(context.Background(),
		"SELECT count(*) FROM entity WHERE type = 'tv'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("series criadas = %d, quero 0", n)
	}
}

// TestImportarSeriePorIDFonteDesconhecida: an id from a source that is not registered is a
// failure, not a guess (the scanner then falls back to the name path).
func TestImportarSeriePorIDFonteDesconhecida(t *testing.T) {
	svc, _ := svcSerieFake(t)
	if _, err := svc.ImportarSeriePorID(context.Background(), "tvdb", "248741", []string{"en"}); !errors.Is(err, ErrSerieNaoEncontrada) {
		t.Fatalf("err = %v, quero ErrSerieNaoEncontrada", err)
	}
	if _, err := svc.ImportarSeriePorID(context.Background(), "letterboxd", "1", []string{"en"}); !errors.Is(err, ErrSerieNaoEncontrada) {
		t.Fatalf("err = %v, quero ErrSerieNaoEncontrada", err)
	}
}

// TestImportarSeriePorIDInexistente: an id that the source does not know degrades to "not
// found" (zero results), never to a panic or a wrong import.
func TestImportarSeriePorIDInexistente(t *testing.T) {
	svc, _ := svcSerieFake(t)
	if _, err := svc.ImportarSeriePorID(context.Background(), "tmdb", "999999", []string{"en"}); !errors.Is(err, ErrSerieNaoEncontrada) {
		t.Fatalf("err = %v, quero ErrSerieNaoEncontrada", err)
	}
}
