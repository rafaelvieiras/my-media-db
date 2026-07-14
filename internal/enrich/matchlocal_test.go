package enrich

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo cobre o casamento por nome no import manual (briefing 0054):
// TestImportarCasaComDraftLocalPorNome reproduz o bug relatado ("O Foragido")
// -- draft com arquivo, sem external_ids + import do mesmo titulo/ano por id
// externo diferente deve enriquecer O MESMO draft, nunca criar uma segunda
// entity. Os demais testes cobrem os 3 "nao casa" do criterio de aceite.

// criarDraftComArquivo cria uma entity DRAFT PURA (sem external_ids) com um
// media_file 'present' ligado -- o shape exato do bug relatado (scanner achou
// o arquivo, ainda sem metadados).
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

// contarEntities devolve o total de entities no banco (dedup check).
func contarEntities(t *testing.T, database *db.DB) int {
	t.Helper()
	r := sqlcgen.New(database.Read)
	todas, err := r.ListarEntities(context.Background(), sqlcgen.ListarEntitiesParams{Limit: 1000, Offset: 0})
	if err != nil {
		t.Fatalf("ListarEntities: %v", err)
	}
	return len(todas)
}

// TestImportarCasaComDraftLocalPorNome reproduz o bug relatado no 0054: um
// draft local (arquivo presente, sem external_ids) + import do MESMO
// titulo/ano por um id externo que o draft nao tem ainda -> UMA entity so, com
// arquivo E metadados; nenhuma orfa criada.
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
	// O arquivo do draft continua ligado -- o ponto inteiro do bug (draft com
	// arquivo + importado com metadados viravam DUAS entities).
	files, err := r.ListarMediaFilesPorEntity(ctx, "e1")
	if err != nil || len(files) != 1 {
		t.Errorf("media_file de e1 = %v (err=%v), quero 1 (o arquivo do draft preservado)", files, err)
	}
	// media_external (payload cru) foi persistido no MESMO id.
	if _, err := r.ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{EntityID: "e1", Source: "tmdb"}); err != nil {
		t.Errorf("media_external ausente em e1: %v", err)
	}
}

// TestImportarSemCandidatoLocalCriaEntityNova garante que o caminho comum
// (sem draft correspondente) continua criando uma entity nova, exatamente
// como antes do 0054 -- nenhuma regressao. Ha um draft no banco, mas de titulo
// bem diferente: o FTS nem deveria trazê-lo como candidato.
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

// TestImportarAnoDivergenteNaoCasa cobre a regra do 0050 herdada pelo import:
// candidato com o MESMO titulo mas ano divergindo em mais de 1 ano nunca casa
// (remake/refilmagem) -- cria entity nova, o draft fica intacto na fila.
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

// TestImportarDraftComExternalIdExistenteNuncaCasa cobre a restricao central
// do 0054: uma entity que JA tem QUALQUER external_id (de OUTRA fonte) nunca e
// candidata do casamento por nome, mesmo com titulo/ano identicos -- so
// drafts PUROS entram no FTS filtrado. Import cria entity nova.
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
