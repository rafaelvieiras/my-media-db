package enrich

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file covers the name matching on manual import:
// TestImportarCasaComDraftLocalPorNome reproduces the reported bug (a movie title)
// -- a draft with a file, without external_ids + the import of the same title/year by a
// different external id must enrich THE SAME draft, never create a second
// entity. The other tests cover the 3 "does not match" cases of the acceptance criteria.

// criarDraftComArquivo creates a PURE DRAFT entity (without external_ids) with a
// 'present' media_file linked to it -- the exact shape of the reported bug (the scanner found
// the file, still without metadata).
func criarDraftComArquivo(t *testing.T, database *db.DB, id, titulo, releaseDate string) {
	t.Helper()
	ctx := context.Background()
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: id, Type: "movie", Title: titulo, ReleaseDate: nsValid(releaseDate),
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
	if _, err := w.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: id + "-mf", EntityID: id, Path: "/lib/" + titulo + ".mkv", State: "present",
	}); err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

// contarEntities returns the total number of entities in the database (dedup check).
func contarEntities(t *testing.T, database *db.DB) int {
	t.Helper()
	r := sqlcgen.New(database.Read)
	todas, err := r.ListarEntities(context.Background(), sqlcgen.ListarEntitiesParams{Limit: 1000, Offset: 0})
	if err != nil {
		t.Fatalf("ListarEntities: %v", err)
	}
	return len(todas)
}

// TestImportarCasaComDraftLocalPorNome reproduces the reported bug: a local
// draft (file present, without external_ids) + the import of the SAME
// title/year under an external id the draft does not have yet -> ONE entity only, with
// both the file AND the metadata; no orphan created.
func TestImportarCasaComDraftLocalPorNome(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	criarDraftComArquivo(t, database, "e1", "O Foragido", "1998-08-07")

	img := servidorImagem(t, "image/jpeg", []byte("poster"))
	fake := &fakeProvider{
		raw: json.RawMessage(`{"title":"O Foragido"}`),
		campos: map[string]any{
			"title":        "O Foragido",
			"release_date": "1998-08-07",
			"poster_url":   img.URL + "/p.jpg",
		},
	}
	svc, _ := novoServico(t, database, fake)

	id, criada, err := svc.Importar(ctx, "tmdb", "9909", "movie", []string{"pt-BR"})
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}
	if criada {
		t.Error("Importar deveria REUSAR o draft (criada=false), nao criar")
	}
	if id != "e1" {
		t.Errorf("id = %q, quero o draft existente 'e1'", id)
	}
	if n := contarEntities(t, database); n != 1 {
		t.Errorf("total de entities = %d, quero 1 (draft enriquecido no lugar, sem duplicar)", n)
	}

	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTmdb.String != "9909" {
		t.Errorf("external_id_tmdb = %q, quero 9909 (draft ganhou a identidade)", ent.ExternalIDTmdb.String)
	}
	// The draft's file stays linked -- the whole point of the bug (a draft with a
	// file + an imported item with metadata turned into TWO entities).
	files, err := r.ListarMediaFilesPorEntity(ctx, "e1")
	if err != nil || len(files) != 1 {
		t.Errorf("media_file de e1 = %v (err=%v), quero 1 (o arquivo do draft preservado)", files, err)
	}
	// media_external (raw payload) was persisted under the SAME id.
	if _, err := r.ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{EntityID: "e1", Source: "tmdb"}); err != nil {
		t.Errorf("media_external ausente em e1: %v", err)
	}
}

// TestImportarSemCandidatoLocalCriaEntityNova guarantees that the common path
// (with no matching draft) keeps creating a new entity, exactly
// as before -- no regression. There is a draft in the database, but with a very
// different title: FTS should not even bring it as a candidate.
func TestImportarSemCandidatoLocalCriaEntityNova(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	criarDraftComArquivo(t, database, "e1", "O Auto da Compadecida", "2000-08-18")

	fake := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Matrix"}`),
		campos: map[string]any{"title": "Matrix", "release_date": "1999-03-31"},
	}
	svc, _ := novoServico(t, database, fake)

	id, criada, err := svc.Importar(ctx, "tmdb", "603", "movie", []string{"pt-BR"})
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}
	if !criada {
		t.Error("Importar deveria CRIAR (criada=true) -- sem candidato local forte")
	}
	if id == "e1" {
		t.Error("nao deveria reusar o draft de titulo diferente")
	}
	if n := contarEntities(t, database); n != 2 {
		t.Errorf("total de entities = %d, quero 2 (draft intacto + item novo)", n)
	}

	r := sqlcgen.New(database.Read)
	draft, err := r.ObterEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ObterEntity(e1): %v", err)
	}
	if draft.ExternalIdsJson.Valid {
		t.Errorf("draft nao deveria ganhar external_ids: %q", draft.ExternalIdsJson.String)
	}
}

// TestImportarAnoDivergenteNaoCasa covers the scoring rule inherited by the import:
// a candidate with the SAME title but a year differing by more than 1 year never matches
// (remake) -- it creates a new entity, the draft stays intact in the queue.
func TestImportarAnoDivergenteNaoCasa(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	criarDraftComArquivo(t, database, "e1", "King Kong", "1933-03-02")

	fake := &fakeProvider{
		raw:    json.RawMessage(`{"title":"King Kong"}`),
		campos: map[string]any{"title": "King Kong", "release_date": "2005-12-14"},
	}
	svc, _ := novoServico(t, database, fake)

	id, criada, err := svc.Importar(ctx, "tmdb", "254", "movie", []string{"pt-BR"})
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}
	if !criada {
		t.Error("Importar deveria CRIAR (criada=true) -- ano diverge > 1 ano, mesmo titulo identico")
	}
	if id == "e1" {
		t.Error("nao deveria casar com o draft de ano divergente")
	}
	if n := contarEntities(t, database); n != 2 {
		t.Errorf("total de entities = %d, quero 2 (draft intacto + item novo, anos nao casam)", n)
	}

	r := sqlcgen.New(database.Read)
	draft, err := r.ObterEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ObterEntity(e1): %v", err)
	}
	if draft.ExternalIdsJson.Valid {
		t.Errorf("draft nao deveria ganhar external_ids: %q", draft.ExternalIdsJson.String)
	}
}

// TestImportarDraftComExternalIdExistenteNuncaCasa covers the central restriction
// of the name matching: an entity that ALREADY has ANY external_id (from ANOTHER source) is never
// a candidate of the name matching, even with identical title/year -- only
// PURE drafts enter the filtered FTS. Import creates a new entity.
func TestImportarDraftComExternalIdExistenteNuncaCasa(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "e1", Type: "movie", Title: "Duna", ReleaseDate: nsValid("2021-10-01"),
		ExternalIdsJson: nsValid(`{"imdb":"tt1160419"}`),
	}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	fake := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Duna"}`),
		campos: map[string]any{"title": "Duna", "release_date": "2021-10-01"},
	}
	svc, _ := novoServico(t, database, fake)

	id, criada, err := svc.Importar(ctx, "tmdb", "438631", "movie", []string{"pt-BR"})
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}
	if !criada {
		t.Error("Importar deveria CRIAR (criada=true) -- e1 ja tem external_id de outra fonte, nunca e candidata")
	}
	if id == "e1" {
		t.Error("nao deveria casar com entity que ja tem external_id (mesmo com titulo/ano identicos)")
	}
	if n := contarEntities(t, database); n != 2 {
		t.Errorf("total de entities = %d, quero 2 (e1 preservada + item novo)", n)
	}

	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("ObterEntity(e1): %v", err)
	}
	if ent.ExternalIDTmdb.Valid {
		t.Errorf("e1 nao deveria ganhar external_id_tmdb: %q", ent.ExternalIDTmdb.String)
	}
	if ent.ExternalIdsJson.String != `{"imdb":"tt1160419"}` {
		t.Errorf("external_ids_json de e1 mudou: %q, quero intacto", ent.ExternalIdsJson.String)
	}
}
