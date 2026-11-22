package enrich

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// serieComAlerta seeds a series already matched to `tmdbID` and carrying the tree-mismatch
// alert (the state the post-scan leaves behind), which is the input of this pass.
func serieComAlerta(t *testing.T, svc *Servico, titulo, tmdbID string) string {
	t.Helper()
	ctx := context.Background()
	id := "serie-" + tmdbID
	if _, err := sqlcgen.New(svc.database.Write).CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              id,
		Type:            tipoSerie,
		Title:           titulo,
		ExternalIdsJson: sql.NullString{String: `{"tmdb":"` + tmdbID + `"}`, Valid: true},
	}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	svc.SinalizarArvoreIncompativel(ctx, id, 65, 13)
	return id
}

// TestSugereOutraIdentidadeQuandoElaAbarcaOsArquivos: the folder is "Clube do Terror" of 1990
// with 78 files; the entity carries the 2019 reboot (13 episodes). The suggestion must point at
// the 1992 show, whose 91 episodes can host the files.
func TestSugereOutraIdentidadeQuandoElaAbarcaOsArquivos(t *testing.T) {
	prov := stubSerieIdentidade{
		resultados: []provider.ResultadoExterno{
			{Source: "stub", SourceID: "93693", Type: "tv", Title: "Clube do Terror", Year: ptrAno("2019")},
			{Source: "stub", SourceID: "2913", Type: "tv", Title: "O Clube do Terror", Year: ptrAno("1992")},
		},
		arvores: map[string]provider.SerieArvore{"93693": arvoreDeTeste(13), "2913": arvoreDeTeste(91)},
	}
	svc := svcIdentidade(t, prov)
	id := serieComAlerta(t, svc, "Clube do Terror", "93693")

	res, err := svc.SugerirIdentidadeDaArvore(context.Background(), id,
		AlvoSerie{Nome: "Clube do Terror", Ano: 1990, Arquivos: 78}, []string{"pt-BR"})
	if err != nil {
		t.Fatalf("SugerirIdentidadeDaArvore: %v", err)
	}
	if !res.Sucesso {
		t.Fatalf("sugestao nao gravada: %s", res.Descricao)
	}

	alerta, err := sqlcgen.New(svc.database.Read).ObterMatchSuggestion(context.Background(), id)
	if err != nil {
		t.Fatalf("ObterMatchSuggestion: %v", err)
	}
	if alerta.SourceID != "2913" {
		t.Errorf("candidato = %s:%s, quero tmdb do seriado de 1992 (2913)", alerta.Source, alerta.SourceID)
	}
	if alerta.Reason != MotivoArvoreIncompativel {
		t.Errorf("reason = %q, quero %q (o alerta continua sendo o mesmo alerta)", alerta.Reason, MotivoArvoreIncompativel)
	}
	if alerta.Score < 92 {
		t.Errorf("score = %d, quero >= 92 para o candidato certo", alerta.Score)
	}
}

// TestNaoSugereAIdentidadeQueJaTem: the best candidate IS the current identity, so there is
// nothing to suggest -- and the alert must not turn into "apply yourself".
func TestNaoSugereAIdentidadeQueJaTem(t *testing.T) {
	prov := stubSerieIdentidade{
		resultados: []provider.ResultadoExterno{
			{Source: "stub", SourceID: "2913", Type: "tv", Title: "O Clube do Terror", Year: ptrAno("1992")},
		},
		arvores: map[string]provider.SerieArvore{"2913": arvoreDeTeste(91)},
	}
	svc := svcIdentidade(t, prov)
	id := serieComAlerta(t, svc, "Clube do Terror", "2913")

	res, err := svc.SugerirIdentidadeDaArvore(context.Background(), id,
		AlvoSerie{Nome: "Clube do Terror", Ano: 1990, Arquivos: 78}, []string{"pt-BR"})
	if err != nil {
		t.Fatalf("SugerirIdentidadeDaArvore: %v", err)
	}
	if res.Sucesso {
		t.Fatal("sugeriu a identidade que a serie ja tem")
	}
	alerta, err := sqlcgen.New(svc.database.Read).ObterMatchSuggestion(context.Background(), id)
	if err != nil {
		t.Fatalf("ObterMatchSuggestion: %v", err)
	}
	if alerta.SourceID != "2913" || alerta.Score != 0 {
		t.Errorf("alerta = %s:%s score=%d, quero o alerta intacto (2913, score 0)",
			alerta.Source, alerta.SourceID, alerta.Score)
	}
}

// TestNaoSugereArvoreQueNaoAbarca: a candidate that scores well but has 3 episodes for 26 files
// is the measured "Arrow" case -- the shape gate refuses it instead of proposing another wrong
// identity with a confident score.
func TestNaoSugereArvoreQueNaoAbarca(t *testing.T) {
	prov := stubSerieIdentidade{
		resultados: []provider.ResultadoExterno{
			{Source: "stub", SourceID: "280224", Type: "tv", Title: "Arrow", Year: ptrAno("2009")},
		},
		arvores: map[string]provider.SerieArvore{"280224": arvoreDeTeste(3)},
	}
	svc := svcIdentidade(t, prov)
	id := serieComAlerta(t, svc, "Arrow", "999")

	res, err := svc.SugerirIdentidadeDaArvore(context.Background(), id,
		AlvoSerie{Nome: "Arrow", Ano: 2012, Arquivos: 26}, []string{"pt-BR"})
	if err != nil {
		t.Fatalf("SugerirIdentidadeDaArvore: %v", err)
	}
	if res.Sucesso {
		t.Fatalf("sugeriu uma arvore de 3 episodios para 26 arquivos: %s", res.Descricao)
	}
}

// TestSugestaoPreservaOCensoDoAlerta: the "N of M files have nowhere to go" numbers are the
// measurement the alert already made -- the suggestion must not lose them by overwriting the
// row with the size of the folder only.
func TestSugestaoPreservaOCensoDoAlerta(t *testing.T) {
	prov := stubSerieIdentidade{
		resultados: []provider.ResultadoExterno{
			{Source: "stub", SourceID: "2913", Type: "tv", Title: "O Clube do Terror", Year: ptrAno("1992")},
		},
		arvores: map[string]provider.SerieArvore{"2913": arvoreDeTeste(91)},
	}
	svc := svcIdentidade(t, prov)
	id := serieComAlerta(t, svc, "Clube do Terror", "93693") // censo 65/78 gravado por serieComAlerta

	if _, err := svc.SugerirIdentidadeDaArvore(context.Background(), id,
		AlvoSerie{Nome: "Clube do Terror", Ano: 1990, Arquivos: 78}, []string{"pt-BR"}); err != nil {
		t.Fatalf("SugerirIdentidadeDaArvore: %v", err)
	}
	alerta, err := sqlcgen.New(svc.database.Read).ObterMatchSuggestion(context.Background(), id)
	if err != nil {
		t.Fatalf("ObterMatchSuggestion: %v", err)
	}
	if alerta.FilesUnmatched != 65 || alerta.FilesTotal != 78 {
		t.Errorf("censo = %d/%d depois da sugestao, quero 65/78", alerta.FilesUnmatched, alerta.FilesTotal)
	}
}

// TestSugereSemIdentidadeAtualENada: an entity without any external id cannot be compared with a
// candidate, so the pass stays quiet instead of proposing anything.
func TestSugereSemIdentidadeAtualENada(t *testing.T) {
	svc := svcIdentidade(t, stubSerieIdentidade{})
	ctx := context.Background()
	if _, err := sqlcgen.New(svc.database.Write).CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "s-sem-id", Type: tipoSerie, Title: "Serie Solta",
	}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	res, err := svc.SugerirIdentidadeDaArvore(ctx, "s-sem-id", AlvoSerie{Nome: "Serie Solta", Arquivos: 10}, nil)
	if err != nil {
		t.Fatalf("SugerirIdentidadeDaArvore: %v", err)
	}
	if res.Sucesso {
		t.Fatal("sugeriu algo para uma serie sem identidade atual")
	}
	if _, err := sqlcgen.New(svc.database.Read).ObterMatchSuggestion(ctx, "s-sem-id"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("alerta gravado para serie sem identidade: err=%v", err)
	}
}
