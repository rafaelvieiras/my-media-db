package scanner

import (
	"context"
	"database/sql"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
)

// TestAcharPorIDExternoNaoCasaOutroTipo: the external id is only unique WITHIN a type.
// TMDB episode ids and series ids share the same numeric space (measured: series 13943 =
// "Beavis and Butt-Head", episode 13943 = an episode of series 252), so the movie lookup
// must not accept a series that carries the same number — before the fix it did, and the
// file was attached to the wrong work.
func TestAcharPorIDExternoNaoCasaOutroTipo(t *testing.T) {
	d := abrirDB(t)
	ctx := context.Background()
	q := sqlcgen.New(d.Write)

	const idCompartilhado = "13943"
	const serieID = "serie-13943"
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              serieID,
		Type:            tipoSerieTV,
		Title:           "Beavis e Butt-Head",
		ExternalIdsJson: sql.NullString{String: `{"tmdb":"13943"}`, Valid: true},
	}); err != nil {
		t.Fatalf("criar serie: %v", err)
	}

	id, achou, err := acharPorIDExterno(ctx, q, naming.Info{TMDBID: idCompartilhado})
	if err != nil {
		t.Fatalf("acharPorIDExterno: %v", err)
	}
	if achou {
		t.Errorf("o filme casou com a série %q que carrega o mesmo id — a busca precisa ser escopada por tipo", id)
	}

	// The series path keeps working for a series with that id, and still refuses the same
	// number when the folder is a movie: both directions of the same rule.
	if id, ok := acharSeriePorIDExterno(ctx, q, naming.Info{TMDBID: idCompartilhado}); !ok || id != serieID {
		t.Errorf("acharSeriePorIDExterno = (%q,%v), quero (%q,true)", id, ok, serieID)
	}
}
