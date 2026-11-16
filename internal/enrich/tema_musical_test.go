package enrich

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// errTemaFake simulates a real failure (network/status) of the TemaMusicalProvider.
var errTemaFake = errors.New("themerrdb: falha simulada")

// fakeTemaMusicalProvider implements the provider.Provider core as a NO-OP (Nome
// "themerrdb", like the real ThemerrDB) PLUS the
// provider.TemaMusicalProvider capability, returning a canned candidate (or none,
// simulating "no match"). It records the ids/type received for the assertions.
type fakeTemaMusicalProvider struct {
	tema     provider.TemaMusical
	achou    bool
	err      error
	recebeu  provider.ExternalIDs
	tipo     string
	chamadas int
}

func (f *fakeTemaMusicalProvider) Nome() string { return "themerrdb" }

func (f *fakeTemaMusicalProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (f *fakeTemaMusicalProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (f *fakeTemaMusicalProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (f *fakeTemaMusicalProvider) BuscarTemaMusical(_ context.Context, ids provider.ExternalIDs, tipo string) (provider.TemaMusical, bool, error) {
	f.chamadas++
	f.recebeu = ids
	f.tipo = tipo
	if f.err != nil {
		return provider.TemaMusical{}, false, f.err
	}
	return f.tema, f.achou, nil
}

// lerCandidatoTema reads the theme-music candidate envelope
// (media_external, source=themerrdb) of an entity. ok=false when the row
// does not exist.
func lerCandidatoTema(t *testing.T, database *sqlcgen.Queries, entityID string) (map[string]any, bool) {
	t.Helper()
	row, err := database.ObterMediaExternalPorFonte(context.Background(), sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: entityID,
		Source:   "themerrdb",
	})
	if err != nil {
		return nil, false
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(row.PayloadJson.String), &env); err != nil {
		t.Fatalf("envelope invalido: %v", err)
	}
	return env, true
}

// TestImportarFilmeComTemaMusicalPersisteCandidato: importing a movie with a
// TemaMusicalProvider registered (with a match) writes the candidate into
// media_external source=themerrdb, with the URL under ChaveEnvelopeTemaMusical --
// and the provider received the correct external id and type (acceptance
// criterion of ADR-0025).
func TestImportarFilmeComTemaMusicalPersisteCandidato(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-TMDB"))

	tmdb := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Matrix"}`),
		campos: map[string]any{"title": "Matrix", "poster_url": img.URL + "/p.jpg"},
	}
	tema := &fakeTemaMusicalProvider{
		tema:  provider.TemaMusical{URL: "https://www.youtube.com/watch?v=matrix-theme"},
		achou: true,
	}
	svc := New(database, provider.NovaRegistry(tmdb, tema), t.TempDir())

	id, _, err := svc.Importar(ctx, "tmdb", "603", "movie", []string{"en"})
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}

	if tema.recebeu.Tmdb != "603" || tema.tipo != "movie" {
		t.Errorf("BuscarTemaMusical recebeu ids=%+v tipo=%q, quero Tmdb=603 tipo=movie", tema.recebeu, tema.tipo)
	}

	r := sqlcgen.New(database.Read)
	env, ok := lerCandidatoTema(t, r, id)
	if !ok {
		t.Fatal("candidato de tema musical ausente apos import")
	}
	if url, _ := env[provider.ChaveEnvelopeTemaMusical].(string); url != "https://www.youtube.com/watch?v=matrix-theme" {
		t.Errorf("URL do candidato = %q, inesperada", url)
	}
}

// TestImportarFilmeSemMatchDeTemaGravaEnvelopeVazioSemQuebrar covers the other
// criterion: an item enriched WITHOUT a match in ThemerrDB does not break the import
// (best-effort) -- and the envelope written does not carry the URL key (to
// differentiate it from "not queried yet" it is enough to know that the row
// exists; no consumer depends on more than that in this delivery).
func TestImportarFilmeSemMatchDeTemaGravaEnvelopeVazioSemQuebrar(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-TMDB"))

	tmdb := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Sem Tema"}`),
		campos: map[string]any{"title": "Sem Tema", "poster_url": img.URL + "/p.jpg"},
	}
	tema := &fakeTemaMusicalProvider{achou: false}
	svc := New(database, provider.NovaRegistry(tmdb, tema), t.TempDir())

	id, _, err := svc.Importar(ctx, "tmdb", "604", "movie", []string{"en"})
	if err != nil {
		t.Fatalf("Importar nao deveria falhar sem match de tema: %v", err)
	}

	r := sqlcgen.New(database.Read)
	env, ok := lerCandidatoTema(t, r, id)
	if !ok {
		t.Fatal("linha de media_external(source=themerrdb) ausente -- esperava envelope vazio gravado")
	}
	if _, temURL := env[provider.ChaveEnvelopeTemaMusical]; temURL {
		t.Errorf("envelope sem match carregou a chave da URL: %+v", env)
	}
}

// TestImportarFilmeSemTemaMusicalProviderNaoQuebra covers the silent
// degradation when NO registered provider implements TemaMusicalProvider
// (the real scenario with no ThemerrDB registered): the import proceeds normally, without any
// media_external(source=themerrdb) row.
func TestImportarFilmeSemTemaMusicalProviderNaoQuebra(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-TMDB"))

	tmdb := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Matrix"}`),
		campos: map[string]any{"title": "Matrix", "poster_url": img.URL + "/p.jpg"},
	}
	svc := New(database, provider.NovaRegistry(tmdb), t.TempDir())

	id, _, err := svc.Importar(ctx, "tmdb", "603", "movie", []string{"en"})
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}

	r := sqlcgen.New(database.Read)
	if _, ok := lerCandidatoTema(t, r, id); ok {
		t.Error("linha de media_external(source=themerrdb) presente sem NENHUM TemaMusicalProvider registrado")
	}
}

// TestImportarFilmeErroDoProviderDeTemaDegradaEmSilencio covers a REAL error
// (network/status) of the provider: it propagates only to the log (best-effort), it does not interrupt
// the import.
func TestImportarFilmeErroDoProviderDeTemaDegradaEmSilencio(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-TMDB"))

	tmdb := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Matrix"}`),
		campos: map[string]any{"title": "Matrix", "poster_url": img.URL + "/p.jpg"},
	}
	tema := &fakeTemaMusicalProvider{err: errTemaFake}
	svc := New(database, provider.NovaRegistry(tmdb, tema), t.TempDir())

	id, _, err := svc.Importar(ctx, "tmdb", "603", "movie", []string{"en"})
	if err != nil {
		t.Fatalf("Importar nao deveria propagar erro do TemaMusicalProvider: %v", err)
	}
	if tema.chamadas != 1 {
		t.Errorf("chamadas = %d, quero 1", tema.chamadas)
	}
	r := sqlcgen.New(database.Read)
	if _, ok := lerCandidatoTema(t, r, id); ok {
		t.Error("candidato gravado apesar do erro do provider -- esperava nada persistido")
	}
}

// TestEnriquecerSerieComTemaMusicalPersisteCandidato covers the series trigger
// (serie.go, after the tree): the series receives the same candidate with the "tv" type.
func TestEnriquecerSerieComTemaMusicalPersisteCandidato(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/png", []byte("PNG"))

	tema := &fakeTemaMusicalProvider{
		tema:  provider.TemaMusical{URL: "https://www.youtube.com/watch?v=simpsons-theme"},
		achou: true,
	}
	svc := New(database, provider.NovaRegistry(fake.New(img.URL), tema), t.TempDir())

	serieID, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"})
	if err != nil {
		t.Fatalf("Importar serie: %v", err)
	}
	if tema.tipo != "tv" {
		t.Errorf("BuscarTemaMusical tipo = %q, quero tv", tema.tipo)
	}

	r := sqlcgen.New(database.Read)
	env, ok := lerCandidatoTema(t, r, serieID)
	if !ok {
		t.Fatal("candidato de tema musical ausente para a serie")
	}
	if url, _ := env[provider.ChaveEnvelopeTemaMusical].(string); url != "https://www.youtube.com/watch?v=simpsons-theme" {
		t.Errorf("URL do candidato da serie = %q, inesperada", url)
	}
}
