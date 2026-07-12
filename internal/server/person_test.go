package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
)

// Testes de GET /api/v1/person/{id} (briefing 0029 Fase D) e do interno-first dos
// créditos (anotarCreditosInternos, creditos_interno.go).

// semearPersonTmdb cria uma pessoa deduplicada (source='tmdb') e devolve seu id local.
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

// semearCreditoCast cria um crédito de ELENCO ligando entityID à pessoa personID.
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

// semearCreditoCrew cria um crédito de EQUIPE ligando entityID à pessoa personID.
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

// decodePerson decodifica a resposta de GET /person/{id}.
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

// TestGetPersonComFilmografia prova o shape da página: pessoa + filmografia com um
// crédito de elenco (character preenchido, job/department nil) e um de equipe
// (job/department preenchidos, character nil), cada um com o card da entity (title/
// year/poster_url), na ordem por type/ordering.
func TestGetPersonComFilmografia(t *testing.T) {
	h, d := setupEntitiesServer(t)

	personID := semearPersonTmdb(t, d, "p1", "6384", "Keanu Reeves")
	semearEntity(t, d, "e-serie", "tv", "John Wick: The Series")
	// release_date via update direto não existe em semearEntity; usa CriarEntity cru
	// com ReleaseDate para o ano aparecer na filmografia.
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

	// Ordenado por entity.type: "movie" < "tv" (ASCII), então "e-com-ano" (movie) vem
	// antes de "e-filme" (tv).
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

// TestGetPersonAgrupaCreditosDaMesmaObra prova que vários créditos da MESMA obra (um
// diretor que também roteiriza e produz) viram UM card na filmografia, com os cargos
// reunidos (" · ", sem duplicar), em vez de a obra se repetir por crédito.
func TestGetPersonAgrupaCreditosDaMesmaObra(t *testing.T) {
	h, d := setupEntitiesServer(t)

	personID := semearPersonTmdb(t, d, "pn", "525", "Christopher Nolan")
	semearEntity(t, d, "e-filme", "movie", "A Origem")
	semearCreditoCrew(t, d, "cd", "e-filme", personID, "Director", "Directing")
	semearCreditoCrew(t, d, "cw", "e-filme", personID, "Writer", "Writing")
	semearCreditoCrew(t, d, "cp", "e-filme", personID, "Producer", "Production")
	// Cargo repetido (outro crédito "Director" da mesma obra) não deve duplicar.
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

// TestGetPersonSemFilmografia prova que uma pessoa sem nenhum crédito devolve
// filmography=[] (nunca null).
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

// TestAnotarCreditosInternoDoExterno prova o interno-first (0029 Fase D) no detalhe
// EXTERNO: um crédito cuja pessoa JÁ está catalogada (source=tmdb, source_id casando)
// ganha person_id; um crédito cuja pessoa NÃO está catalogada fica com person_id nil.
// Usa a fixture servidorTMDBRico (cast source_id=6384 "Keanu Reeves", crew
// source_id=9339 "Lana Wachowski") — só semeia a pessoa do elenco.
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

// TestImportPreenchePersonIDDoProprioCredito prova o interno-first no lado da ENTITY
// (resolverEntity): o import PERSISTE elenco/equipe (internal/enrich/creditos.go, que
// já upserta a pessoa), então o GET seguinte da própria entity já enxerga o person_id
// do crédito que ela mesma acabou de criar — sem precisar de um segundo import.
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

	// A página da pessoa do elenco enxerga esta obra na filmografia.
	if ent.Cast[0].PersonID != nil {
		pessoa := decodePerson(t, do(t, h, http.MethodGet, "/api/v1/person/"+*ent.Cast[0].PersonID, nil).Body.Bytes())
		if len(pessoa.Filmography) != 1 || pessoa.Filmography[0].EntityID != id {
			t.Errorf("filmografia da pessoa = %+v, quero 1 item apontando para %s", pessoa.Filmography, id)
		}
	}
}

// TestGetPersonHandlerSemDB prova o 503 padrão dos handlers de leitura sem banco
// injetado (mesma guarda de entitiesUnavailable dos demais, ver TestCuradoriaSemBanco503).
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
