package enrich

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// tvdbArvoreFake e a fonte da ARVORE de serie (source="tvdb"): implementa SerieProvider.
// MapearDetalhe devolve a identidade (title/description/original_language) + poster, que
// o enrich rico NAO pode clobbar. Arvore minima (sem temporadas) — o foco e o no serie.
type tvdbArvoreFake struct{ imgURL string }

func (tvdbArvoreFake) Nome() string { return "tvdb" }
func (tvdbArvoreFake) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (tvdbArvoreFake) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{"tvdb":true}`), nil
}
func (f tvdbArvoreFake) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{
		"title":             "The Simpsons",
		"description":       "Springfield family.",
		"original_language": "en",
		"poster_url":        f.imgURL + "/poster.jpg",
	}, nil
}
func (tvdbArvoreFake) BuscarSerieArvore(context.Context, string, []string) (provider.SerieArvore, error) {
	return provider.SerieArvore{DefaultSeasonType: "official", SeasonTypes: []string{"official"}}, nil
}

// tmdbRicoFake e o provider "tmdb" que expoe SO a capacidade rica de serie (0045): NAO e
// a fonte da arvore. Devolve o canonico rico (so as chaves ricas) para o tvdbID esperado.
type tmdbRicoFake struct {
	backdropURL string
	chamadas    int
	ultimoTvdb  string
}

func (tmdbRicoFake) Nome() string { return "tmdb" }
func (tmdbRicoFake) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (tmdbRicoFake) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (tmdbRicoFake) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *tmdbRicoFake) BuscarSerieDetalheRico(_ context.Context, tvdbID string, _ []string) (map[string]any, bool, error) {
	f.chamadas++
	f.ultimoTvdb = tvdbID
	if tvdbID != "71663" {
		return nil, false, nil
	}
	return map[string]any{
		"tagline":                 map[string]any{"en": "Slogan"},
		"keywords":                map[string]any{"en": []string{"satire"}},
		"certification_by_region": map[string]any{"US": "TV-PG"},
		"tags":                    map[string]any{"en": []string{"Animation"}},
		"similar":                 []any{map[string]any{"source_id": "1434", "type": "tv", "title": "Family Guy"}},
		"backdrop_url":            f.backdropURL + "/bg.jpg",
		"airing_status":           "Returning Series",
	}, true, nil
}

// svcSerieRico monta o Servico com a arvore TVDB + o enriquecedor rico TMDB e um servidor
// de imagem (poster do TVDB e fanart do TMDB baixam dele).
func svcSerieRico(t *testing.T) (*Servico, *db.DB, *tmdbRicoFake) {
	t.Helper()
	database := setupDB(t)
	img := servidorImagem(t, "image/jpeg", []byte("ART-BYTES"))
	rico := &tmdbRicoFake{backdropURL: img.URL}
	svc := New(database, provider.NovaRegistry(tvdbArvoreFake{imgURL: img.URL}, rico), t.TempDir())
	return svc, database, rico
}

// TestImportarSerieMesclaRicoTMDB (0045): importar a serie do TVDB grava, ALEM da camada
// TVDB, a camada media_external "tmdb" com o canonico rico (tagline/keywords/similar/
// classificacao/tags/situacao/backdrop) SEM clobbar a identidade TVDB, e baixa a fanart.
func TestImportarSerieMesclaRicoTMDB(t *testing.T) {
	svc, database, rico := svcSerieRico(t)
	ctx := context.Background()

	serieID, criada, err := svc.Importar(ctx, "tvdb", "71663", "tv", []string{"en"})
	if err != nil || !criada {
		t.Fatalf("Importar serie: id=%s criada=%v err=%v", serieID, criada, err)
	}
	if rico.chamadas != 1 || rico.ultimoTvdb != "71663" {
		t.Errorf("BuscarSerieDetalheRico chamadas=%d tvdb=%q, quero 1 chamada com 71663", rico.chamadas, rico.ultimoTvdb)
	}

	r := sqlcgen.New(database.Read)
	// Identidade da arvore TVDB intacta (nao clobbada pelo rico).
	if serie, err := r.ObterEntity(ctx, serieID); err != nil || serie.Title != "The Simpsons" {
		t.Fatalf("serie.title = %q err=%v, quero The Simpsons (TVDB)", serie.Title, err)
	}

	exigirCamadaRica(t, serieRicoPersistido(t, database, serieID))
	exigirArtesSerie(t, r, serieID)

	// Idempotente (0043): re-importar reescreve a MESMA linha (sem duplicar).
	if _, _, err := svc.Importar(ctx, "tvdb", "71663", "tv", []string{"en"}); err != nil {
		t.Fatalf("reimport: %v", err)
	}
	_ = serieRicoPersistido(t, database, serieID) // ObterMediaExternalPorFonte e :one — erraria se duplicasse
}

// exigirCamadaRica confere que o canonico rico persistido carrega AS chaves do contrato
// e a situacao crua, e NAO carrega title/description/original_language (clobbaria a arvore).
func exigirCamadaRica(t *testing.T, rico map[string]any) {
	t.Helper()
	for _, chave := range []string{"tagline", "keywords", "certification_by_region", "tags", "similar", "backdrop_url", "airing_status"} {
		if _, ok := rico[chave]; !ok {
			t.Errorf("canonico rico persistido sem %q: %v", chave, rico)
		}
	}
	if rico["airing_status"] != "Returning Series" {
		t.Errorf("airing_status = %v, quero Returning Series", rico["airing_status"])
	}
	for _, proibido := range []string{"title", "description", "original_language"} {
		if _, existe := rico[proibido]; existe {
			t.Errorf("camada rica NAO deveria carregar %q (clobbaria o TVDB)", proibido)
		}
	}
}

// exigirArtesSerie confere que a FANART foi baixada do backdrop_url (fecha o buraco do
// 0044) e que o POSTER do TVDB coexiste (as duas artes convivem).
func exigirArtesSerie(t *testing.T, r *sqlcgen.Queries, serieID string) {
	t.Helper()
	ctx := context.Background()
	fan, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{EntityID: serieID, Kind: "fanart", Origin: "external"})
	if err != nil {
		t.Fatalf("fanart da serie ausente: %v", err)
	}
	if b, _ := os.ReadFile(fan.PathOrUri); string(b) != "ART-BYTES" {
		t.Errorf("binario da fanart diverge do baixado")
	}
	if _, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{EntityID: serieID, Kind: "poster", Origin: "external"}); err != nil {
		t.Errorf("poster do TVDB deveria coexistir com a fanart: %v", err)
	}
}

// serieRicoPersistido le a camada media_external "tmdb" do no serie e desembrulha o
// canonico rico do envelope ChaveEnvelopeSerieRico.
func serieRicoPersistido(t *testing.T, database *db.DB, serieID string) map[string]any {
	t.Helper()
	row, err := sqlcgen.New(database.Read).ObterMediaExternalPorFonte(context.Background(), sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: serieID,
		Source:   "tmdb",
	})
	if err != nil {
		t.Fatalf("media_external tmdb do no serie: %v", err)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(row.PayloadJson.String), &env); err != nil {
		t.Fatalf("payload rico invalido (%q): %v", row.PayloadJson.String, err)
	}
	blob, ok := env[provider.ChaveEnvelopeSerieRico]
	if !ok {
		t.Fatalf("payload sem chave %q: %q", provider.ChaveEnvelopeSerieRico, row.PayloadJson.String)
	}
	var campos map[string]any
	if err := json.Unmarshal(blob, &campos); err != nil {
		t.Fatalf("canonico rico invalido: %v", err)
	}
	return campos
}
