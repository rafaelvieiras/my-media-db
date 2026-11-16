package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
)

// pessoaProviderFake implements provider.Provider + provider.PessoaProvider without network,
// to exercise filmography_external (the second section of the person page) deterministically.
// The Provider methods outside BuscarFilmografia are not exercised here — stubs only to
// satisfy the interface.
type pessoaProviderFake struct {
	nome     string
	creditos []provider.CreditoFilmografia
	err      error
}

func (f *pessoaProviderFake) Nome() string { return f.nome }
func (f *pessoaProviderFake) Buscar(_ context.Context, _, _, _ string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (f *pessoaProviderFake) BuscarDetalhe(_ context.Context, _, _ string, _ []string) (json.RawMessage, error) {
	return nil, errors.New("nao implementado")
}
func (f *pessoaProviderFake) MapearDetalhe(_ json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *pessoaProviderFake) BuscarFilmografia(_ context.Context, _, _ string) ([]provider.CreditoFilmografia, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.creditos, nil
}

// Tests of GET /api/v1/person/{id} and of the internal-first logic of the credits
// (anotarCreditosInternos, creditos_interno.go).

// semearPersonTmdb creates a deduplicated person (source='tmdb') and returns its local id.
func semearPersonTmdb(t *testing.T, d *db.DB, id, sourceID, nome string) string {
	t.Helper()
	got, err := escrita(t, d).UpsertPerson(context.Background(), sqlcgen.UpsertPersonParams{
		ID:       id,
		Source:   "tmdb",
		SourceID: sourceID,
		Name:     nome,
	})
	if err != nil {
		t.Fatalf("UpsertPerson(%s): %v", id, err)
	}
	return got
}

// semearCreditoCast creates a CAST credit linking entityID to the person personID.
func semearCreditoCast(t *testing.T, d *db.DB, id, entityID, personID, character string, ordering int64) {
	t.Helper()
	err := escrita(t, d).CriarCredito(context.Background(), sqlcgen.CriarCreditoParams{
		ID:        id,
		EntityID:  entityID,
		PersonID:  nsOf(personID),
		Kind:      kindCastCredito,
		Character: nsOf(character),
		Ordering:  ordering,
		Source:    "tmdb",
		SourceID:  nsOf("cred-" + id),
	})
	if err != nil {
		t.Fatalf("CriarCredito(cast %s): %v", id, err)
	}
}

// semearCreditoCrew creates a CREW credit linking entityID to the person personID.
func semearCreditoCrew(t *testing.T, d *db.DB, id, entityID, personID, job, department string) {
	t.Helper()
	err := escrita(t, d).CriarCredito(context.Background(), sqlcgen.CriarCreditoParams{
		ID:         id,
		EntityID:   entityID,
		PersonID:   nsOf(personID),
		Kind:       kindCrewCredito,
		Job:        nsOf(job),
		Department: nsOf(department),
		Source:     "tmdb",
		SourceID:   nsOf("cred-" + id),
	})
	if err != nil {
		t.Fatalf("CriarCredito(crew %s): %v", id, err)
	}
}

// decodePerson decodes the response of GET /person/{id}.
func decodePerson(t *testing.T, body []byte) personDTO {
	t.Helper()
	var out personDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar personDTO: %v (corpo=%s)", err, body)
	}
	return out
}

func TestGetPersonNaoExiste404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/person/naoexiste", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestGetPersonComFilmografia proves the page shape: person + filmography with one cast credit
// (character filled in, job/department nil) and one crew credit (job/department filled in,
// character nil), each one with the entity card (title/year/poster_url), in the order by
// type/ordering.
func TestGetPersonComFilmografia(t *testing.T) {
	h, d := setupEntitiesServer(t)

	personID := semearPersonTmdb(t, d, "p1", "6384", "Keanu Reeves")
	semearEntity(t, d, "e-serie", "tv", "John Wick: The Series")
	// A release_date through a direct update does not exist in semearEntity; it uses a raw
	// CriarEntity with ReleaseDate so the year shows up in the filmography.
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: "e-com-ano", Type: "movie", Title: "Speed", ReleaseDate: nsOf("1994-06-10"),
	}); err != nil {
		t.Fatalf("CriarEntity(e-com-ano): %v", err)
	}
	semearAsset(t, d, "a1", "e-com-ano", "poster", "/poster.jpg")

	semearCreditoCast(t, d, "c1", "e-com-ano", personID, "Jack Traven", 0)
	semearCreditoCrew(t, d, "c2", "e-serie", personID, "Producer", "Production")

	rec := do(t, h, http.MethodGet, "/api/v1/person/"+personID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	pessoa := decodePerson(t, rec.Body.Bytes())

	eq(t, "person.id", pessoa.ID, personID)
	eq(t, "person.name", pessoa.Name, "Keanu Reeves")
	eq(t, "person.source", pessoa.Source, "tmdb")
	eq(t, "person.source_id", pessoa.SourceID, "6384")
	if pessoa.ProfileURL != nil {
		t.Errorf("person.profile_url = %v, quero nil (sem profile_path)", *pessoa.ProfileURL)
	}
	if len(pessoa.Filmography) != 2 {
		t.Fatalf("filmography = %d itens, quero 2 (corpo=%s)", len(pessoa.Filmography), rec.Body.String())
	}

	// Ordered by entity.type: "movie" < "tv" (ASCII), so "e-com-ano" (movie) comes before
	// "e-filme" (tv).
	cast := pessoa.Filmography[0]
	eq(t, "cast.entity_id", cast.EntityID, "e-com-ano")
	eq(t, "cast.type", cast.Type, "movie")
	eq(t, "cast.title", cast.Title, "Speed")
	eqPtr(t, "cast.year", cast.Year, "1994")
	eqPtr(t, "cast.poster_url", cast.PosterURL, posterPath("e-com-ano"))
	eq(t, "cast.kind", cast.Kind, "cast")
	eqPtr(t, "cast.character", cast.Character, "Jack Traven")
	if cast.Job != nil || cast.Department != nil {
		t.Errorf("cast.job/department = %v/%v, quero nil (kind=cast)", cast.Job, cast.Department)
	}

	crew := pessoa.Filmography[1]
	eq(t, "crew.entity_id", crew.EntityID, "e-serie")
	eq(t, "crew.type", crew.Type, "tv")
	eq(t, "crew.title", crew.Title, "John Wick: The Series")
	eq(t, "crew.kind", crew.Kind, "crew")
	eqPtr(t, "crew.job", crew.Job, "Producer")
	eqPtr(t, "crew.department", crew.Department, "Production")
	if crew.Character != nil {
		t.Errorf("crew.character = %v, quero nil (kind=crew)", *crew.Character)
	}
	if crew.Year != nil {
		t.Errorf("crew.year = %v, quero nil (sem release_date)", *crew.Year)
	}
}

// TestGetPersonAgrupaCreditosDaMesmaObra proves that several credits of the SAME work (a
// director who also writes and produces) become ONE card in the filmography, with the roles
// gathered (" · ", without duplicating), instead of the work repeating per credit.
func TestGetPersonAgrupaCreditosDaMesmaObra(t *testing.T) {
	h, d := setupEntitiesServer(t)

	personID := semearPersonTmdb(t, d, "pn", "525", "Christopher Nolan")
	semearEntity(t, d, "e-filme", "movie", "A Origem")
	semearCreditoCrew(t, d, "cd", "e-filme", personID, "Director", "Directing")
	semearCreditoCrew(t, d, "cw", "e-filme", personID, "Writer", "Writing")
	semearCreditoCrew(t, d, "cp", "e-filme", personID, "Producer", "Production")
	// A repeated role (another "Director" credit of the same work) must not duplicate.
	semearCreditoCrew(t, d, "cd2", "e-filme", personID, "Director", "Directing")

	pessoa := decodePerson(t, do(t, h, http.MethodGet, "/api/v1/person/"+personID, nil).Body.Bytes())
	if len(pessoa.Filmography) != 1 {
		t.Fatalf("filmography = %d, quero 1 (obra agrupada)", len(pessoa.Filmography))
	}
	obra := pessoa.Filmography[0]
	eq(t, "obra.entity_id", obra.EntityID, "e-filme")
	eq(t, "obra.kind", obra.Kind, "crew")
	eqPtr(t, "obra.job (cargos reunidos, sem duplicar)", obra.Job, "Director · Writer · Producer")
}

// TestGetPersonSemFilmografia proves that a person without any credit returns
// filmography=[] (never null).
func TestGetPersonSemFilmografia(t *testing.T) {
	h, d := setupEntitiesServer(t)
	personID := semearPersonTmdb(t, d, "p2", "1", "Ninguém Creditou")

	rec := do(t, h, http.MethodGet, "/api/v1/person/"+personID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decodificar corpo bruto: %v", err)
	}
	if string(raw["filmography"]) != "[]" {
		t.Errorf("filmography bruto = %s, quero []", raw["filmography"])
	}
}

// TestAnotarCreditosInternoDoExterno proves the internal-first logic on the EXTERNAL detail: a
// credit whose person is ALREADY cataloged (source=tmdb, matching source_id) gets a person_id;
// a credit whose person is NOT cataloged stays with a nil person_id. It uses the
// servidorTMDBRico fixture (cast source_id=6384 "Keanu Reeves", crew source_id=9339 "Lana
// Wachowski") — it only seeds the cast person.
func TestAnotarCreditosInternoDoExterno(t *testing.T) {
	srv := servidorTMDBRico(t, nil)
	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	h, d := setupSearchServer(t, WithSearchProvider(prov))

	personID := semearPersonTmdb(t, d, "pjacatalogada", "6384", "Keanu Reeves")

	ext := decodeDetalheExterno(t, do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil).Body.Bytes())
	if len(ext.Cast) != 1 || len(ext.Crew) != 1 {
		t.Fatalf("cast/crew = %d/%d, quero 1/1", len(ext.Cast), len(ext.Crew))
	}
	eqPtr(t, "cast[0].person_id (catalogada)", ext.Cast[0].PersonID, personID)
	if ext.Crew[0].PersonID != nil {
		t.Errorf("crew[0].person_id = %v, quero nil (pessoa não catalogada)", *ext.Crew[0].PersonID)
	}
}

// TestImportPreenchePersonIDDoProprioCredito proves the internal-first logic on the ENTITY
// side (resolverEntity): the import PERSISTS cast/crew (internal/enrich/creditos.go, which
// already upserts the person), so the following GET of the entity itself already sees the
// person_id of the credit it just created — without needing a second import.
func TestImportPreenchePersonIDDoProprioCredito(t *testing.T) {
	srv := servidorTMDBRico(t, nil)
	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	h, _ := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	id := decodeEntidade(t, rec).ID

	ent := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil))
	if len(ent.Cast) != 1 || len(ent.Crew) != 1 {
		t.Fatalf("cast/crew = %d/%d, quero 1/1", len(ent.Cast), len(ent.Crew))
	}
	if ent.Cast[0].PersonID == nil {
		t.Errorf("cast[0].person_id = nil, quero preenchido (pessoa criada no próprio import)")
	}
	if ent.Crew[0].PersonID == nil {
		t.Errorf("crew[0].person_id = nil, quero preenchido (pessoa criada no próprio import)")
	}

	// The cast person's page sees this work in the filmography.
	if ent.Cast[0].PersonID != nil {
		pessoa := decodePerson(t, do(t, h, http.MethodGet, "/api/v1/person/"+*ent.Cast[0].PersonID, nil).Body.Bytes())
		if len(pessoa.Filmography) != 1 || pessoa.Filmography[0].EntityID != id {
			t.Errorf("filmografia da pessoa = %+v, quero 1 item apontando para %s", pessoa.Filmography, id)
		}
	}
}

// TestGetPersonHandlerSemDB proves the standard 503 of the read handlers without an injected
// database (the same entitiesUnavailable guard as the others, see TestCuradoriaSemBanco503).
func TestGetPersonHandlerSemDB(t *testing.T) {
	h, err := New("v-teste", nil).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodGet, "/api/v1/person/qualquer", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
}

// TestGetPersonFilmografiaExternaExcluiImportados proves the second section: two credits from
// the provider, one whose work is ALREADY imported (source_id "603", matching a seeded entity
// with external_id_tmdb=603) and another one not yet. Only the second shows up in
// filmography_external, with a PROXIED poster_url (assetsDir present).
func TestGetPersonFilmografiaExternaExcluiImportados(t *testing.T) {
	fake := &pessoaProviderFake{nome: "tmdb", creditos: []provider.CreditoFilmografia{
		{Source: "tmdb", SourceID: "603", Type: "movie", Title: "Já Importado", Kind: "cast", Character: ptr("X")},
		{
			Source: "tmdb", SourceID: "999", Type: "movie", Title: "Ainda Não Importado",
			Year: ptr("2020"), PosterURL: ptr("https://image.tmdb.org/t/p/w185/x.jpg"),
			Kind: "cast", Character: ptr("Y"),
		},
	}}
	h, d := setupSearchServer(t, WithSearchProvider(fake), WithAssetsDir(t.TempDir()))

	personID := semearPersonTmdb(t, d, "pf1", "6384", "Ator Externo")
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: "e-ja-importado", Type: "movie", Title: "Já Importado", ExternalIdsJson: nsOf(`{"tmdb":"603"}`),
	}); err != nil {
		t.Fatalf("CriarEntity(e-ja-importado): %v", err)
	}

	pessoa := decodePerson(t, do(t, h, http.MethodGet, "/api/v1/person/"+personID, nil).Body.Bytes())
	if len(pessoa.FilmographyExternal) != 1 {
		t.Fatalf("filmography_external = %d itens, quero 1 (603 excluido, ja importado) — %+v",
			len(pessoa.FilmographyExternal), pessoa.FilmographyExternal)
	}
	ext := pessoa.FilmographyExternal[0]
	eq(t, "ext.source", ext.Source, "tmdb")
	eq(t, "ext.source_id", ext.SourceID, "999")
	eq(t, "ext.type", ext.Type, "movie")
	eq(t, "ext.title", ext.Title, "Ainda Não Importado")
	eqPtr(t, "ext.year", ext.Year, "2020")
	eq(t, "ext.kind", ext.Kind, "cast")
	eqPtr(t, "ext.character", ext.Character, "Y")
	if ext.Job != nil {
		t.Errorf("ext.job = %v, quero nil (kind=cast)", *ext.Job)
	}
	if ext.PosterURL == nil || *ext.PosterURL == "https://image.tmdb.org/t/p/w185/x.jpg" {
		t.Errorf("ext.poster_url = %v, quero reescrito pelo proxy local (assetsDir presente)", ext.PosterURL)
	}
}

// TestGetPersonFilmografiaExternaSemProvider proves the DEGRADATION to `[]` (never null) when
// no provider is registered (external search off).
func TestGetPersonFilmografiaExternaSemProvider(t *testing.T) {
	h, d := setupSearchServer(t)
	personID := semearPersonTmdb(t, d, "pf2", "1", "Sem Provider")

	var raw map[string]json.RawMessage
	body := do(t, h, http.MethodGet, "/api/v1/person/"+personID, nil).Body.Bytes()
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decodificar corpo bruto: %v", err)
	}
	if string(raw["filmography_external"]) != "[]" {
		t.Errorf("filmography_external bruto = %s, quero []", raw["filmography_external"])
	}
}

// TestGetPersonFilmografiaExternaProviderSemCapacidadeDegrada proves that a provider registered
// under the person's source but WITHOUT the PessoaProvider capability (the fakeProvider of
// search_test.go, which only satisfies provider.Provider) also degrades to `[]` — the handler
// does a type-assert and does not assume the capability is present. Since fakeProvider.Nome()
// is fixed to "fake", the person is seeded with source="fake" (not through the semearPersonTmdb
// helper, which fixes "tmdb").
func TestGetPersonFilmografiaExternaProviderSemCapacidadeDegrada(t *testing.T) {
	h, d := setupSearchServer(t, WithSearchProvider(&fakeProvider{}))

	personID, err := escrita(t, d).UpsertPerson(context.Background(), sqlcgen.UpsertPersonParams{
		ID: "pf3", Source: "fake", SourceID: "1", Name: "Sem Capacidade",
	})
	if err != nil {
		t.Fatalf("UpsertPerson: %v", err)
	}

	var raw map[string]json.RawMessage
	body := do(t, h, http.MethodGet, "/api/v1/person/"+personID, nil).Body.Bytes()
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decodificar corpo bruto: %v", err)
	}
	if string(raw["filmography_external"]) != "[]" {
		t.Errorf("filmography_external bruto = %s, quero []", raw["filmography_external"])
	}
}

// TestGetPersonFilmografiaExternaErroDegrada proves that a provider error (network/status) in
// the filmography lookup degrades to `[]` — the page does not fail because of the second
// section (the imported filmography, `filmography`, keeps working).
func TestGetPersonFilmografiaExternaErroDegrada(t *testing.T) {
	fake := &pessoaProviderFake{nome: "tmdb", err: errors.New("tmdb: fonte fora do ar")}
	h, d := setupSearchServer(t, WithSearchProvider(fake))

	personID := semearPersonTmdb(t, d, "pf4", "6384", "Fonte Fora Do Ar")
	semearEntity(t, d, "e-fpf4", "movie", "Obra Importada")
	semearCreditoCast(t, d, "cfpf4", "e-fpf4", personID, "Papel", 0)

	pessoa := decodePerson(t, do(t, h, http.MethodGet, "/api/v1/person/"+personID, nil).Body.Bytes())
	if len(pessoa.FilmographyExternal) != 0 {
		t.Errorf("filmography_external = %+v, quero [] (provider falhou)", pessoa.FilmographyExternal)
	}
	if len(pessoa.Filmography) != 1 {
		t.Errorf("filmography = %+v, quero 1 (nao afetada pela falha da secao externa)", pessoa.Filmography)
	}
}
