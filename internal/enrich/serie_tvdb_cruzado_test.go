package enrich

// serie_tvdb_cruzado_test.go cobre a causa 2 do briefing 0087: quando a arvore de uma
// serie vem SO do TMDB (sem TVDB configurado, o cenario de independencia de provider do
// 0046), o cross-id tvdb do append external_ids (que o TMDB ja busca no detalhe rico)
// deve ser capturado e mesclado em entity.external_ids_json -- o que destrava o
// ArteProvider fanart.tv na TV (exige tvdb_id exclusivamente). Ver mesclarTvdbCruzado em
// serie.go.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// tmdbBaseComTvdbFake e a fonte-base da arvore com Nome()="tmdb" (cenario 0046: sem TVDB
// registrado, o TMDB e o SerieProvider) que TAMBEM implementa provider.TvdbCruzadoProvider
// -- o BuscarDetalhe devolve um payload com external_ids.tvdb_id, no MESMO formato do
// append real do TMDB (external_ids: {tvdb_id: "..."}), e TvdbIDDoDetalheSerie o extrai
// SEM rede, espelhando o metodo real do client tmdb.
type tmdbBaseComTvdbFake struct {
	imgURL string
	tvdbID string
}

func (tmdbBaseComTvdbFake) Nome() string { return "tmdb" }
func (tmdbBaseComTvdbFake) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (f tmdbBaseComTvdbFake) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	payload, _ := json.Marshal(map[string]any{
		"external_ids": map[string]any{"tvdb_id": f.tvdbID},
	})
	return json.RawMessage(payload), nil
}
func (f tmdbBaseComTvdbFake) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{
		"title":             "Serie Sem TVDB Configurado",
		"original_language": "en",
		"poster_url":        f.imgURL + "/poster.jpg",
	}, nil
}
func (tmdbBaseComTvdbFake) BuscarSerieArvore(context.Context, string, []string) (provider.SerieArvore, error) {
	return provider.SerieArvore{DefaultSeasonType: "official", SeasonTypes: []string{"official"}}, nil
}
func (f tmdbBaseComTvdbFake) TvdbIDDoDetalheSerie(raw json.RawMessage) (string, bool) {
	var d struct {
		ExternalIds struct {
			TvdbID string `json:"tvdb_id"`
		} `json:"external_ids"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return "", false
	}
	return d.ExternalIds.TvdbID, d.ExternalIds.TvdbID != ""
}

// TestEnriquecerSerieSemTvdbCapturaCrossId (0087): uma serie cuja arvore vem SO do TMDB
// (sem TVDB configurado) ganha "tvdb" em external_ids_json apos o enriquecimento, com o
// valor batendo o tvdb_id do append external_ids do TMDB -- e o inventario fanart.tv
// passa a ser gravado (o ArteProvider recebe o tvdb recem-capturado, destravando
// rotaETabela case "tv").
func TestEnriquecerSerieSemTvdbCapturaCrossId(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-BYTES"))
	base := tmdbBaseComTvdbFake{imgURL: img.URL, tvdbID: "81189"}
	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "poster", URL: "https://assets.fanart.tv/tv.jpg"},
	}}
	svc := New(database, provider.NovaRegistry(base, arte), t.TempDir())

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Serie"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	if err := svc.Enriquecer(ctx, "s1", "tmdb", "9999", tipoSerie, []string{"en"}); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTvdb.String != "81189" {
		t.Errorf("external_id_tvdb = %q, quero 81189 (capturado do append external_ids do TMDB)", ent.ExternalIDTvdb.String)
	}
	// O ArteProvider recebeu o tvdb recem-capturado NA MESMA passada (idsExternosDe leu o
	// external_ids_json JA mesclado antes de enriquecerArtes da serie rodar).
	if arte.recebeu.Tvdb != "81189" {
		t.Errorf("BuscarArtes recebeu Tvdb=%q, quero 81189", arte.recebeu.Tvdb)
	}
	// Inventario fanart.tv gravado (antes do fix, rotaETabela degradava em silencio por
	// falta de tvdb_id -- ObterMediaExternalPorFonte e :one, erraria se ausente/duplicado).
	lerInventario(t, r, "s1")
}

// TestEnriquecerSerieSemTvdbIdempotente: repetir o enriquecimento nao duplica nem quebra
// -- a mescla e sempre um ACRESCIMO idempotente (mesclarExternalIdSeAusente).
func TestEnriquecerSerieSemTvdbIdempotente(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-BYTES"))
	base := tmdbBaseComTvdbFake{imgURL: img.URL, tvdbID: "81189"}
	svc := New(database, provider.NovaRegistry(base), t.TempDir())

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Serie"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := svc.Enriquecer(ctx, "s1", "tmdb", "9999", tipoSerie, []string{"en"}); err != nil {
			t.Fatalf("Enriquecer #%d: %v", i, err)
		}
	}
	ent, err := sqlcgen.New(database.Read).ObterEntity(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTvdb.String != "81189" {
		t.Errorf("external_id_tvdb = %q, quero 81189 (estavel apos re-enriquecer)", ent.ExternalIDTvdb.String)
	}
}

// TestSerieComTvdbConfiguradoNaoRegride (0087, criterio de nao-regressao): quando a
// fonte-base da arvore JA E' o TVDB (o client tvdb nao implementa TvdbCruzadoProvider),
// mesclarTvdbCruzado e' um no-op -- o tvdb continua vindo, como sempre veio, de
// mesclarExternalIds(source="tvdb"). O inventario fanart.tv segue gravado normalmente.
func TestSerieComTvdbConfiguradoNaoRegride(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-BYTES"))
	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "poster", URL: "https://assets.fanart.tv/tv.jpg"},
	}}
	svc := New(database, provider.NovaRegistry(tvdbArvoreFake{imgURL: img.URL}, arte), t.TempDir())

	serieID, criada, err := svc.Importar(ctx, "tvdb", "71663", "tv", []string{"en"})
	if err != nil || !criada {
		t.Fatalf("Importar serie: id=%s criada=%v err=%v", serieID, criada, err)
	}

	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, serieID)
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTvdb.String != "71663" {
		t.Errorf("external_id_tvdb = %q, quero 71663 (fonte-base, sem alteracao)", ent.ExternalIDTvdb.String)
	}
	if arte.recebeu.Tvdb != "71663" {
		t.Errorf("BuscarArtes recebeu Tvdb=%q, quero 71663 (sem regressao)", arte.recebeu.Tvdb)
	}
	lerInventario(t, r, serieID)
}
