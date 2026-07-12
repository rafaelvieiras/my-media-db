package enrich

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// camposComCreditos e o canonico fixo (elenco/equipe/produtoras) que o fakeProvider
// devolve nos testes de persistencia. `order` e int (como o mapeamento em processo).
func camposComCreditos() map[string]any {
	return map[string]any{
		"title": "The Matrix",
		"cast": []any{
			map[string]any{"source_id": "6384", "name": "Keanu Reeves", "character": "Neo", "order": 0, "profile_path": "/k.jpg"},
			map[string]any{"source_id": "2975", "name": "Laurence Fishburne", "character": "Morpheus", "order": 1},
		},
		"crew": []any{
			map[string]any{"source_id": "9339", "name": "Lana Wachowski", "department": "Directing", "job": "Director"},
		},
		"companies": []any{
			map[string]any{"source_id": "174", "name": "Warner Bros.", "logo_path": "/wb.png"},
		},
	}
}

// TestPersistirCreditosPopula prova que o enrich popula person/company/credit a partir
// do canonico, com os atributos da aresta (kind/character/ordering).
func TestPersistirCreditosPopula(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "t"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	fake := &fakeProvider{raw: json.RawMessage(`{}`), campos: camposComCreditos()}
	svc, _ := novoServico(t, database, fake)
	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"en"}); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	creds, err := r.ListarCreditosDaEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ListarCreditosDaEntity: %v", err)
	}
	if len(creds) != 4 {
		t.Fatalf("creditos de e1 = %d, quero 4 (2 cast + 1 crew + 1 company)", len(creds))
	}
	verificarDedupExiste(t, r)
	verificarArestas(t, creds)
}

// verificarDedupExiste confere que a person e a company deduplicadas foram gravadas.
func verificarDedupExiste(t *testing.T, r *sqlcgen.Queries) {
	t.Helper()
	ctx := context.Background()
	if _, err := r.ObterPersonPorFonte(ctx, sqlcgen.ObterPersonPorFonteParams{Source: "tmdb", SourceID: "6384"}); err != nil {
		t.Errorf("person Keanu ausente: %v", err)
	}
	if _, err := r.ObterCompanyPorFonte(ctx, sqlcgen.ObterCompanyPorFonteParams{Source: "tmdb", SourceID: "174"}); err != nil {
		t.Errorf("company WB ausente: %v", err)
	}
}

// verificarArestas confere os atributos das arestas: o elenco carrega personagem/ordem;
// o credito de company liga a company (nao a person).
func verificarArestas(t *testing.T, creds []sqlcgen.Credit) {
	t.Helper()
	neo := creditoDe(t, creds, "cast", "6384")
	if neo.Character.String != "Neo" {
		t.Errorf("character = %q, quero 'Neo'", neo.Character.String)
	}
	if neo.Ordering != 0 {
		t.Errorf("ordering = %d, quero 0", neo.Ordering)
	}
	wb := creditoDe(t, creds, "company", "174")
	if wb.CompanyID.String == "" || wb.PersonID.Valid {
		t.Errorf("credito company mal ligado: person=%v company=%v", wb.PersonID, wb.CompanyID)
	}
}

// TestPersistirCreditosDedupPersonEntreObras prova que uma pessoa que aparece em DUAS
// obras tem UMA unica linha em person (dedup por source_id), com um credito por obra.
func TestPersistirCreditosDedupPersonEntreObras(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	w := sqlcgen.New(database.Write)
	for _, id := range []string{"e1", "e2"} {
		if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: id, Type: "movie", Title: "t"}); err != nil {
			t.Fatalf("CriarEntity %s: %v", id, err)
		}
	}
	fake := &fakeProvider{raw: json.RawMessage(`{}`), campos: camposComCreditos()}
	svc, _ := novoServico(t, database, fake)
	for _, id := range []string{"e1", "e2"} {
		if err := svc.Enriquecer(ctx, id, "tmdb", "603", "movie", []string{"en"}); err != nil {
			t.Fatalf("Enriquecer %s: %v", id, err)
		}
	}

	// 3 pessoas distintas (2 cast + 1 crew), nao 6 -- dedup por (source, source_id).
	if n := contarLinhas(t, database, "person"); n != 3 {
		t.Errorf("linhas em person = %d, quero 3 (dedup entre as duas obras)", n)
	}
	if n := contarLinhas(t, database, "company"); n != 1 {
		t.Errorf("linhas em company = %d, quero 1 (dedup)", n)
	}
	// Cada obra tem seus 4 creditos (a aresta nao e deduplicada, so a pessoa/empresa).
	r := sqlcgen.New(database.Read)
	for _, id := range []string{"e1", "e2"} {
		creds, _ := r.ListarCreditosDaEntity(ctx, id)
		if len(creds) != 4 {
			t.Errorf("creditos de %s = %d, quero 4", id, len(creds))
		}
	}
}

// TestReEnriquecerCreditosIdempotente prova que re-enriquecer APAGA e repopula os
// creditos de (entity, source) sem duplicar: contagens estaveis apos duas passagens.
func TestReEnriquecerCreditosIdempotente(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "t"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	fake := &fakeProvider{raw: json.RawMessage(`{}`), campos: camposComCreditos()}
	svc, _ := novoServico(t, database, fake)

	for i := 0; i < 2; i++ {
		if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"en"}); err != nil {
			t.Fatalf("Enriquecer #%d: %v", i, err)
		}
	}

	r := sqlcgen.New(database.Read)
	creds, _ := r.ListarCreditosDaEntity(ctx, "e1")
	if len(creds) != 4 {
		t.Errorf("creditos apos re-enrich = %d, quero 4 (sem duplicar)", len(creds))
	}
	if n := contarLinhas(t, database, "person"); n != 3 {
		t.Errorf("person apos re-enrich = %d, quero 3 (upsert reusa)", n)
	}
}

// TestPersistirCreditosSerieNormalizado prova o caminho de SERIE: o canonico agregado
// (character unido, episode_count) e persistido no no serie.
func TestPersistirCreditosSerieNormalizado(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: "tv", Title: "t"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	campos := map[string]any{
		"title": "Family Guy",
		"cast": []any{
			map[string]any{"source_id": "1", "name": "Seth MacFarlane", "character": "Peter / Brian", "order": 0, "episode_count": 400},
		},
	}
	fake := &fakeProvider{raw: json.RawMessage(`{}`), campos: campos}
	svc, _ := novoServico(t, database, fake)
	if err := svc.Enriquecer(ctx, "s1", "tmdb", "1434", "tv", []string{"en"}); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	creds, _ := r.ListarCreditosDaEntity(ctx, "s1")
	if len(creds) != 1 {
		t.Fatalf("creditos = %d, quero 1", len(creds))
	}
	if creds[0].Character.String != "Peter / Brian" {
		t.Errorf("character = %q, quero 'Peter / Brian'", creds[0].Character.String)
	}
	if !creds[0].EpisodeCount.Valid || creds[0].EpisodeCount.Int64 != 400 {
		t.Errorf("episode_count = %v, quero 400", creds[0].EpisodeCount)
	}
}

// creditoDe acha o credito de um kind ligado a uma person/company de source_id dado.
func creditoDe(t *testing.T, creds []sqlcgen.Credit, kind, sourceID string) sqlcgen.Credit {
	t.Helper()
	for _, c := range creds {
		if c.Kind == kind && c.SourceID.String == sourceID {
			return c
		}
	}
	t.Fatalf("credito kind=%s source_id=%s ausente", kind, sourceID)
	return sqlcgen.Credit{}
}

// contarLinhas conta as linhas de uma tabela via SQL cru (nao ha query sqlc de contagem
// global -- e so para o teste de dedup).
func contarLinhas(t *testing.T, database *db.DB, tabela string) int {
	t.Helper()
	var n int
	if err := database.Read.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+tabela).Scan(&n); err != nil {
		t.Fatalf("contar %s: %v", tabela, err)
	}
	return n
}
