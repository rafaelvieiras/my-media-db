package enrich

// alertas_test.go covers ADR-0038: the two match alerts that migration 0029 added to the
// suggested match.
//
//   empty_tree    the source answers the series with ZERO episodes. Measured in production
//                 on 19/nov/2026: a phantom TMDb entry (0 seasons, 0 episodes, empty
//                 overview) won the exact-name comparison and 86 files with a clean
//                 S01E01 became provisional drafts. The identity is kept (refusing it
//                 outright would make the next scan pick it again) and the owner gets the
//                 warning with a sibling candidate when one exists.
//   tree_mismatch the tree exists but does not fit the files: the count is the evidence.

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// arvoreVaziaFake is a base source whose tree has NO episodes and whose name search returns
// the siblings of the phantom entry (what TMDb really answers for "Aladdin": a stub and the
// real animated series).
type arvoreVaziaFake struct {
	siblings []provider.ResultadoExterno
}

func (arvoreVaziaFake) Nome() string { return "tmdb" }

func (f arvoreVaziaFake) Buscar(_ context.Context, _ string, tipo, _ string) ([]provider.ResultadoExterno, error) {
	if tipo != tipoSerie {
		return nil, nil
	}
	return f.siblings, nil
}

func (arvoreVaziaFake) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	payload, _ := json.Marshal(map[string]any{"name": "Aladdin"})
	return json.RawMessage(payload), nil
}

func (arvoreVaziaFake) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{"title": "Aladdin", "original_language": "en"}, nil
}

// BuscarSerieArvore is the defect: the entry exists but carries no episode at all.
func (arvoreVaziaFake) BuscarSerieArvore(context.Context, string, []string) (provider.SerieArvore, error) {
	return provider.SerieArvore{DefaultSeasonType: "official", SeasonTypes: []string{"official"}}, nil
}

// TestEnriquecerSerieComArvoreVaziaSinaliza covers the whole path: the enrichment does NOT
// apply the empty tree, keeps the identity and persists the alert with the sibling that the
// name search offers.
func TestEnriquecerSerieComArvoreVaziaSinaliza(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	ano := "1994"
	irmao := provider.ResultadoExterno{
		Source: "tmdb", SourceID: "2745", Type: tipoSerie,
		Title: "Aladdin: A Série Animada", Year: &ano,
	}
	svc := New(database, provider.NovaRegistry(arvoreVaziaFake{siblings: []provider.ResultadoExterno{irmao}}), t.TempDir())

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Aladdin"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	if err := svc.Enriquecer(ctx, "s1", "tmdb", "14383", tipoSerie, []string{"en"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	// The tree was NOT applied: no season entity came out of a source with zero episodes.
	var temporadas int
	if err := database.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM entity WHERE type = 'season'").Scan(&temporadas); err != nil {
		t.Fatalf("contar temporadas: %v", err)
	}
	if temporadas != 0 {
		t.Errorf("temporadas criadas = %d, quero 0 (arvore vazia nao pode ser aplicada)", temporadas)
	}

	// The identity stays (revoking it would make the next scan pick the same entry again).
	ent, err := r.ObterEntity(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTmdb.String != "14383" {
		t.Errorf("external_id_tmdb = %q, quero 14383 (a identidade fica; o alerta e que denuncia)", ent.ExternalIDTmdb.String)
	}

	// The alert points at the sibling, so the owner can fix it in one click.
	alerta, err := r.ObterMatchSuggestion(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterMatchSuggestion: %v", err)
	}
	if alerta.Reason != MotivoArvoreVazia {
		t.Errorf("reason = %q, quero %q", alerta.Reason, MotivoArvoreVazia)
	}
	if alerta.SourceID != "2745" || alerta.Title != "Aladdin: A Série Animada" {
		t.Errorf("candidato = %s:%s (%q), quero tmdb:2745 (Aladdin: A Série Animada)",
			alerta.Source, alerta.SourceID, alerta.Title)
	}
	if alerta.Score < 80 {
		t.Errorf("score = %d, quero >= 80 (o irmao de mesmo nome precisa passar do limiar)", alerta.Score)
	}
}

// TestEnriquecerSerieComArvoreVaziaSemIrmao still signals even with no better candidate: the
// row describes the match itself, and the server hides the apply button (can_apply=false).
func TestEnriquecerSerieComArvoreVaziaSemIrmao(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	svc := New(database, provider.NovaRegistry(arvoreVaziaFake{}), t.TempDir())

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Esquadrilha Aérea"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	if err := svc.Enriquecer(ctx, "s1", "tmdb", "5565", tipoSerie, []string{"pt-BR"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	alerta, err := sqlcgen.New(database.Read).ObterMatchSuggestion(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterMatchSuggestion: %v", err)
	}
	if alerta.Reason != MotivoArvoreVazia {
		t.Errorf("reason = %q, quero %q", alerta.Reason, MotivoArvoreVazia)
	}
	if alerta.SourceID != "5565" {
		t.Errorf("source_id = %q, quero 5565 (sem irmao, a linha descreve o proprio match)", alerta.SourceID)
	}
	if alerta.Query != "" || alerta.Score != 0 {
		t.Errorf("query/score = %q/%d, quero vazio/0 (nada foi encontrado)", alerta.Query, alerta.Score)
	}
}

// TestSinalizarArvoreIncompativelGravaCenso covers the 'tree_mismatch' row: the counts are
// the evidence and the candidate is the CURRENT identity (which is what the API turns into
// can_apply=false).
func TestSinalizarArvoreIncompativelGravaCenso(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	svc := New(database, provider.NovaRegistry(), t.TempDir())

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              "s1",
		Type:            tipoSerie,
		Title:           "Batman",
		ExternalIdsJson: sql.NullString{String: `{"tmdb":"2022"}`, Valid: true},
		ReleaseDate:     sql.NullString{String: "2004-09-11", Valid: true},
	}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	svc.SinalizarArvoreIncompativel(ctx, "s1", 81, 78)

	alerta, err := sqlcgen.New(database.Read).ObterMatchSuggestion(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterMatchSuggestion: %v", err)
	}
	if alerta.Reason != MotivoArvoreIncompativel {
		t.Errorf("reason = %q, quero %q", alerta.Reason, MotivoArvoreIncompativel)
	}
	if alerta.Source != "tmdb" || alerta.SourceID != "2022" {
		t.Errorf("candidato = %s:%s, quero tmdb:2022 (a identidade sob suspeita)", alerta.Source, alerta.SourceID)
	}
	if alerta.FilesUnmatched != 81 || alerta.FilesTotal != 159 {
		t.Errorf("censo = %d/%d, quero 81/159", alerta.FilesUnmatched, alerta.FilesTotal)
	}
	if !alerta.Year.Valid || alerta.Year.String != "2004" {
		t.Errorf("year = %+v, quero 2004 (do release_date)", alerta.Year)
	}
}
