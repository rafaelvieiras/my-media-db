package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// gravarSugestaoTeste inserts a hint straight into the table: the tests below are about the
// HTTP contract (expose, dismiss), not about how the hint is computed.
func gravarSugestaoTeste(t *testing.T, d *sql.DB, entityID string) {
	t.Helper()
	if err := sqlcgen.New(d).UpsertMatchSuggestion(context.Background(), sqlcgen.UpsertMatchSuggestionParams{
		EntityID:  entityID,
		Source:    "tmdb",
		SourceID:  "39340",
		Title:     "Duas Garotas em Apuros",
		Year:      sql.NullString{String: "2011", Valid: true},
		PosterUrl: sql.NullString{String: "https://image.tmdb.org/t/p/w342/broken.jpg", Valid: true},
		Score:     95,
		Query:     "2 Brok",
	}); err != nil {
		t.Fatalf("UpsertMatchSuggestion: %v", err)
	}
}

// sugestaoDoCorpo decodes GET /entities/{id} and returns the hint (nil when absent).
func sugestaoDoCorpo(t *testing.T, corpo []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(corpo, &out); err != nil {
		t.Fatalf("json: %v (%s)", err, corpo)
	}
	sug, _ := out["match_suggestion"].(map[string]any)
	return sug
}

// TestEntityExpoeSugestaoDeMatch: the "did you mean this?" hint travels with the item, with
// everything the screen needs to render it (poster, title, year and the score that justifies
// the guess).
func TestEntityExpoeSugestaoDeMatch(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "serie-1", "tv", "2 Broken Girls")
	gravarSugestaoTeste(t, d.Write, "serie-1")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/serie-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (%s)", rec.Code, rec.Body.String())
	}
	sug := sugestaoDoCorpo(t, rec.Body.Bytes())
	if sug == nil {
		t.Fatal("match_suggestion ausente no corpo")
	}
	if sug["source"] != "tmdb" || sug["source_id"] != "39340" {
		t.Errorf("candidato = %v, quero tmdb/39340", sug)
	}
	if sug["title"] != "Duas Garotas em Apuros" {
		t.Errorf("title = %v", sug["title"])
	}
	if sug["query"] != "2 Brok" {
		t.Errorf("query = %v, quero a variante que achou", sug["query"])
	}
	if score, ok := sug["score"].(float64); !ok || score != 95 {
		t.Errorf("score = %v, quero 95", sug["score"])
	}
}

// TestEntitySemSugestaoNaoInventa: without a hint (and with no source configured) the item
// screen still answers 200 and simply has no hint -- the fallback never breaks the screen.
func TestEntitySemSugestaoNaoInventa(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "serie-2", "tv", "Serie Qualquer")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/serie-2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	if sug := sugestaoDoCorpo(t, rec.Body.Bytes()); sug != nil {
		t.Errorf("match_suggestion = %v, quero ausente", sug)
	}
}

// TestDescartarSugestaoDeMatch: DELETE marks the hint as dismissed (204, idempotent) and the
// screen stops showing it. The row STAYS, so opening the item again neither re-queries the
// sources nor brings the warning back.
func TestDescartarSugestaoDeMatch(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "serie-3", "tv", "2 Broken Girls")
	gravarSugestaoTeste(t, d.Write, "serie-3")

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/serie-3/match-suggestion", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, quero 204 (%s)", rec.Code, rec.Body.String())
	}
	depois := do(t, h, http.MethodGet, "/api/v1/entities/serie-3", nil)
	if sug := sugestaoDoCorpo(t, depois.Body.Bytes()); sug != nil {
		t.Errorf("match_suggestion = %v depois do descarte, quero ausente", sug)
	}
	// The row is kept on purpose (dismissed=1): a second open must not query the sources.
	var dismissed int
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT dismissed FROM match_suggestion WHERE entity_id = 'serie-3'").Scan(&dismissed); err != nil {
		t.Fatalf("linha da sugestão sumiu: %v", err)
	}
	if dismissed != 1 {
		t.Errorf("dismissed = %d, quero 1", dismissed)
	}

	// Idempotent: a second DELETE is still 204.
	if rec := do(t, h, http.MethodDelete, "/api/v1/entities/serie-3/match-suggestion", nil); rec.Code != http.StatusNoContent {
		t.Errorf("segundo DELETE = %d, quero 204", rec.Code)
	}
}

// TestDescartarSugestaoEntityInexistente: the DELETE follows the API rule -- 404 when the
// ENTITY does not exist, so a typo in the id is not silent.
func TestDescartarSugestaoEntityInexistente(t *testing.T) {
	h, _ := setupSearchServer(t)
	rec := do(t, h, http.MethodDelete, "/api/v1/entities/nao-existe/match-suggestion", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
}

// TestPodeSugerirCobreFilme: the hint used to be series-only, which left a movie with a
// distorted folder title (the measured "101 Dálmatas O Filme") with no search result AND no
// hint. It now covers movies too, and still refuses everything else: a resolved item needs no
// hint, an episode is not a work, and a server without a source has nothing to ask.
func TestPodeSugerirCobreFilme(t *testing.T) {
	comFonte := &Server{enrich: &enrich.Servico{}}
	semFonte := &Server{}

	casos := []struct {
		nome string
		s    *Server
		ent  resolver.Entidade
		want bool
	}{
		{"filme sem id", comFonte, resolver.Entidade{Type: "movie", Title: "101 Dálmatas O Filme"}, true},
		{"série sem id continua valendo", comFonte, resolver.Entidade{Type: "tv", Title: "2 Broken Girls"}, true},
		{"filme já resolvido", comFonte, resolver.Entidade{Type: "movie", Title: "Matrix", ExternalIDs: map[string]string{"tmdb": "603"}}, false},
		{"episódio", comFonte, resolver.Entidade{Type: "episode", Title: "Genesis Undone"}, false},
		{"sem fonte configurada", semFonte, resolver.Entidade{Type: "movie", Title: "Toy Story"}, false},
		{"filme sem título", comFonte, resolver.Entidade{Type: "movie"}, false},
	}
	for _, c := range casos {
		if got := c.s.podeSugerir(c.ent); got != c.want {
			t.Errorf("%s: podeSugerir = %v, quero %v", c.nome, got, c.want)
		}
	}
}

// semearSerieComTmdb creates a series entity that ALREADY carries a tmdb identity — the
// state a "did you mean this?" hint becomes stale in (issue #97).
func semearSerieComTmdb(t *testing.T, d *db.DB, id, tmdbID, titulo string) {
	t.Helper()
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              id,
		Type:            "tv",
		Title:           titulo,
		ExternalIdsJson: nsOf(`{"tmdb":"` + tmdbID + `"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

// gravarSugestaoComMotivo inserts a hint with an explicit reason (name/empty_tree/...).
func gravarSugestaoComMotivo(t *testing.T, d *sql.DB, entityID, sourceID, motivo string) {
	t.Helper()
	if err := sqlcgen.New(d).UpsertMatchSuggestion(context.Background(), sqlcgen.UpsertMatchSuggestionParams{
		EntityID: entityID,
		Source:   "tmdb",
		SourceID: sourceID,
		Title:    "Ragnarok",
		Year:     sql.NullString{String: "2004", Valid: true},
		Score:    100,
		Query:    "Ragnarok The",
		Reason:   motivo,
	}); err != nil {
		t.Fatalf("UpsertMatchSuggestion: %v", err)
	}
}

// TestSugestaoDeNomeVencidaNaoAparece: the hint was stored while the item had no identity and
// its candidate is the identity the item has NOW -- "is this the right item?" about the item
// itself (Ragnarok: tmdb 45615 suggested to tmdb 45615, measured in production). It must not
// be rendered, and it must be dropped so it does not come back.
func TestSugestaoDeNomeVencidaNaoAparece(t *testing.T) {
	h, d := setupSearchServer(t)
	semearSerieComTmdb(t, d, "serie-vencida", "45615", "Ragnarok")
	gravarSugestaoComMotivo(t, d.Write, "serie-vencida", "45615", enrich.MotivoNome)

	rec := do(t, h, http.MethodGet, "/api/v1/entities/serie-vencida", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (%s)", rec.Code, rec.Body.String())
	}
	if sug := sugestaoDoCorpo(t, rec.Body.Bytes()); sug != nil {
		t.Fatalf("match_suggestion = %v, queria ausente (o candidato é a própria identidade)", sug)
	}
	if _, err := sqlcgen.New(d.Write).ObterMatchSuggestion(context.Background(), "serie-vencida"); err == nil {
		t.Fatal("a linha vencida continuou na tabela, queria removida")
	}
}

// TestSugestaoDeArvoreContinuaAparecendo: the TREE hints describe the current match ON
// PURPOSE (ADR-0038) and carry their own sentence on the screen -- the stale-name rule must
// not delete them.
func TestSugestaoDeArvoreContinuaAparecendo(t *testing.T) {
	h, d := setupSearchServer(t)
	semearSerieComTmdb(t, d, "serie-arvore", "45615", "Ragnarok")
	gravarSugestaoComMotivo(t, d.Write, "serie-arvore", "45615", enrich.MotivoArvoreIncompativel)

	rec := do(t, h, http.MethodGet, "/api/v1/entities/serie-arvore", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (%s)", rec.Code, rec.Body.String())
	}
	sug := sugestaoDoCorpo(t, rec.Body.Bytes())
	if sug == nil {
		t.Fatal("match_suggestion ausente, queria o aviso de árvore incompatível")
	}
	if sug["reason"] != enrich.MotivoArvoreIncompativel {
		t.Errorf("reason = %v", sug["reason"])
	}
	if aplicavel, _ := sug["can_apply"].(bool); aplicavel {
		t.Error("can_apply = true, quero false (o candidato é a identidade atual)")
	}
}
