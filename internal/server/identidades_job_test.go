package server

// Tests of the `sugerir_identidades` job wiring (internal/job/identidades): the census the
// post-scan uses is the job's universe, and the candidate it writes is the one the owner applies
// with one click. Nothing here changes an identity.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// serieSuspeita seeds a series matched to `tmdbID` with `n` draft episodes, each holding a
// present file -- the shape the census counts as "the tree does not fit the files".
func serieSuspeita(t *testing.T, d *db.DB, id, titulo, tmdbID string, n int) {
	t.Helper()
	ctx := context.Background()
	semearEntity(t, d, id, "tv", titulo)
	if _, err := d.Write.ExecContext(ctx,
		`UPDATE entity SET external_ids_json = ? WHERE id = ?`, `{"tmdb":"`+tmdbID+`"}`, id); err != nil {
		t.Fatalf("external_ids de %s: %v", id, err)
	}
	season := "se-" + id
	semearEntity(t, d, season, "season", "Temporada 1")
	ligarFilhoOrdenado(t, d, "r-"+season, id, season, "tv_season", 1)
	for i := 1; i <= n; i++ {
		ep := id + "-ep" + string(rune('0'+i))
		semearEntity(t, d, ep, "episode", "Episódio "+string(rune('0'+i)))
		rebaixarParaDraft(t, d, ep, "episode", "Episódio "+string(rune('0'+i)))
		ligarFilhoOrdenado(t, d, "r-"+ep, season, ep, "tv_episode", int64(i))
		arq := "f-" + ep
		semearFile(t, d, arq, ep, "/media/series/"+titulo+"/Temporada 01/"+string(rune('0'+i))+".mkv", "present", 0, "")
		vincularArquivo(t, d, arq, ep)
	}
}

// TestSugerirIdentidadesPoeOCandidatoNoAlerta: the suspect series is listed by the census, and
// the suggestion written by the job points at a DIFFERENT identity than the one it has -- which
// is what makes the item screen offer "apply" (can_apply is derived from that difference).
func TestSugerirIdentidadesPoeOCandidatoNoAlerta(t *testing.T) {
	database, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "sugere.sqlite")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// The fake source answers the folder name with its canned series (6 episodes in 2 seasons).
	// WithIdentidadesJob is part of the setup ON PURPOSE: a guard that silently skips the
	// registration (measured in production, 0.18.0 -> `job_handler_unavailable`) has to fail
	// here, not in the owner's queue.
	s := New("v-teste", database, WithSearchProvider(fake.New("")), WithAssetsDir(t.TempDir()),
		WithIdentidadesJob())
	// The registration is the assertion: without it the owner gets
	// `job_handler_unavailable` from the queue, which is exactly what production did.
	if _, ok := s.jobs.Registry.Get("sugerir_identidades"); !ok {
		t.Fatal("handler de sugerir_identidades nao registrado (WithIdentidadesJob pulou o registro)")
	}

	// A pasta leva o nome que a fonte falsa devolve ("Springfield Tales"): o que esta errado e a
	// IDENTIDADE (tmdb 999), nao o nome -- exatamente o caso que o alerta descreve.
	serieSuspeita(t, database, "s1", "Springfield Tales", "999", 6)

	suspeitas, err := s.ListarIdentidadesSuspeitas(ctx)
	if err != nil {
		t.Fatalf("ListarIdentidadesSuspeitas: %v", err)
	}
	if len(suspeitas) != 1 || suspeitas[0].ID != "s1" {
		t.Fatalf("suspeitas = %+v, quero a s1 (o censo do pos-scan)", suspeitas)
	}

	res, err := s.SugerirIdentidadeDaSerie(ctx, "s1")
	if err != nil {
		t.Fatalf("SugerirIdentidadeDaSerie: %v", err)
	}
	if !res.Sucesso {
		t.Fatalf("nenhuma sugestao: %s", res.Descricao)
	}

	alerta, err := sqlcgen.New(database.Read).ObterMatchSuggestion(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterMatchSuggestion: %v", err)
	}
	if alerta.SourceID == "999" {
		t.Fatal("o alerta ainda aponta a identidade atual: o botao de aplicar nao apareceria")
	}
	if alerta.SourceID != fake.SerieFixtureID {
		t.Errorf("candidato = %s:%s, quero a serie do fixture (%s)", alerta.Source, alerta.SourceID, fake.SerieFixtureID)
	}
	if alerta.Score == 0 {
		t.Error("score = 0: a sugestao precisa da nota que a sustenta")
	}
}
