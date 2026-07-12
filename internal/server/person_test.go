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

// pessoaProviderFake implementa provider.Provider + provider.PessoaProvider sem rede,
// para exercitar filmography_external (segunda seção da página da pessoa) de forma
// determinística. Os métodos de Provider fora de BuscarFilmografia não são exercitados
// aqui — stubs só para satisfazer a interface.
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

// TestGetPersonFilmografiaExternaExcluiImportados prova a segunda seção (0029 Fase D):
// dois créditos do provider, um cuja obra JÁ está importada (source_id "603", casando
// uma entity semeada com external_id_tmdb=603) e outro ainda não. Só o segundo aparece
// em filmography_external, com poster_url PROXYADO (assetsDir presente).
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

// TestGetPersonFilmografiaExternaSemProvider prova a DEGRADAÇÃO para `[]` (nunca null)
// quando nenhum provider está registrado (busca externa desligada).
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

// TestGetPersonFilmografiaExternaProviderSemCapacidadeDegrada prova que um provider
// registrado sob a fonte da pessoa mas SEM a capacidade PessoaProvider (o fakeProvider
// de search_test.go, que só cumpre provider.Provider) também degrada para `[]` — o
// handler faz type-assert e não assume a capacidade presente. Como fakeProvider.Nome()
// é fixo "fake", a pessoa é semeada com source="fake" (não pelo helper
// semearPersonTmdb, que fixa "tmdb").
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

// TestGetPersonFilmografiaExternaErroDegrada prova que um erro do provider (rede/
// status) na busca da filmografia degrada para `[]` — a página não cai por causa da
// segunda seção (a filmografia importada, `filmography`, segue funcionando).
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
