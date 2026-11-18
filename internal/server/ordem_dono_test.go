package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
)

// Tests of the OWNER ORDER (ADR-0033): the numbering the owner declares, including the
// case no provider can express -- one numbered unit that merges several aired episodes.

// semearSerieTresEpsT1 seeds a series with ONE season of three episodes (S01E01..E03 in
// the official order): the shape of a release that merged three aired episodes into one
// entry.
func semearSerieTresEpsT1(t *testing.T, d *db.DB) {
	t.Helper()
	semearEntity(t, d, "s1", "tv", "Serie")
	semearExtension(t, d, "s1", `{"core.tv@1":{"default_season_type":"official","season_types":["official"]}}`)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	for i, id := range []string{"e1", "e2", "e3"} {
		semearEntity(t, d, id, "episode", "Ep "+id)
		semearExtension(t, d, id, fmt.Sprintf(`{"core.tv@1":{"numbering":{"official":{"season":1,"number":%d}}}}`, i+1))
		ligarFilhoOrdenado(t, d, "r-"+id, "se1", id, "tv_episode", int64(i+1))
	}
}

// ordemDe performs GET /entities/{id}/owner-order.
func ordemDe(t *testing.T, h http.Handler, id string) ordemDonoDTO {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/owner-order", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET owner-order %s: status = %d (corpo=%s)", id, rec.Code, rec.Body.String())
	}
	var out ordemDonoDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar ordem: %v (corpo=%s)", err, rec.Body.String())
	}
	return out
}

// ordensDaSerie returns the orders the detail announces for the series.
func ordensDaSerie(t *testing.T, h http.Handler, id string) []string {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("detalhe %s: status = %d (corpo=%s)", id, rec.Code, rec.Body.String())
	}
	var corpo struct {
		Orderings *struct {
			Default   string   `json:"default"`
			Available []string `json:"available"`
		} `json:"orderings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &corpo); err != nil {
		t.Fatalf("decodificar detalhe: %v", err)
	}
	if corpo.Orderings == nil {
		t.Fatal("orderings ausente no detalhe")
	}
	out := append([]string{}, corpo.Orderings.Available...)
	out = append(out, "default="+corpo.Orderings.Default)
	return out
}

// The owner declares ONE unit that merges the three aired episodes: the API returns it,
// the blob carries exactly that key, the order becomes the default and the grouping
// projects the merged numbering.
func TestOrdemDoDonoMesclaTresEpisodios(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearSerieTresEpsT1(t, d)

	corpo := `{"name":"HBO 2004","based_on":"official","units":[
		{"season":1,"number":1,"title":"Blackout","episodes":["e1","e2","e3"]}]}`
	rec := do(t, h, http.MethodPut, "/api/v1/entities/s1/owner-order", []byte(corpo))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT owner-order: status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}

	got := ordemDe(t, h, "s1")
	eq(t, "name", got.Name, "HBO 2004")
	if len(got.Units) != 1 || len(got.Units[0].Episodes) != 3 {
		t.Fatalf("unidades = %+v, quero 1 unidade com 3 episodios", got.Units)
	}
	eq(t, "primeiro episodio da unidade", got.Units[0].Episodes[0], "e1")

	// The blob holds the owner order under the provider namespace.
	blob := getBlob(t, h, "s1")
	var ext map[string]map[string]json.RawMessage
	if err := json.Unmarshal(blob["extension_data"], &ext); err != nil {
		t.Fatalf("blob.extension_data ilegivel: %v", err)
	}
	if ext["core.tv@1"]["owner_order"] == nil {
		t.Fatalf("owner_order ausente no blob: %s", string(blob["extension_data"]))
	}

	// The order is available and it is the DEFAULT.
	ordens := ordensDaSerie(t, h, "s1")
	if ordens[0] != "owner" || ordens[len(ordens)-1] != "default=owner" {
		t.Errorf("ordens = %v, quero owner primeiro e default=owner", ordens)
	}

	// The grouping projects the MERGED numbering: same S01E01 on the three cards,
	// marked as a unit of three, in the declared order.
	grupos := episodesDe(t, h, "s1", "owner")
	var numerados []episodeNumberingDTO
	for _, grp := range grupos.Groups {
		for _, card := range grp.Episodes {
			if card.Numbering != nil {
				numerados = append(numerados, *card.Numbering)
			}
		}
	}
	if len(numerados) != 3 {
		t.Fatalf("episodios numerados = %d, quero 3 (grupos=%+v)", len(numerados), grupos.Groups)
	}
	for i, n := range numerados {
		if n.Season != 1 || n.Number != 1 {
			t.Errorf("numeracao[%d] = %+v, quero S01E01 (unidade mesclada)", i, n)
		}
		if n.MergedEpisodes != 3 || n.MergedOrdinal != i+1 {
			t.Errorf("numeracao[%d] merged = %d/%d, quero 3/%d", i, n.MergedEpisodes, n.MergedOrdinal, i+1)
		}
	}
}

// A PUT never destroys the other overrides of the blob, and a DELETE removes only the
// owner order (the namespace disappears when it becomes empty).
func TestOrdemDoDonoPreservaOsOutrosOverrides(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearSerieTresEpsT1(t, d)

	if rec := patchOv(t, h, "s1", "pt-BR", `{"title":"Minha Serie"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH titulo: status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}

	corpo := `{"units":[{"season":1,"number":1,"episodes":["e1"]},{"season":1,"number":2,"episodes":["e2","e3"]}]}`
	if rec := do(t, h, http.MethodPut, "/api/v1/entities/s1/owner-order", []byte(corpo)); rec.Code != http.StatusOK {
		t.Fatalf("PUT owner-order: status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "s1")
	if blob["title"] == nil {
		t.Fatal("o override de titulo foi perdido ao gravar a ordem")
	}

	if rec := do(t, h, http.MethodDelete, "/api/v1/entities/s1/owner-order", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE owner-order: status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob = getBlob(t, h, "s1")
	if blob["title"] == nil {
		t.Fatal("o override de titulo foi perdido ao remover a ordem")
	}
	if _, tem := blob["extension_data"]; tem {
		t.Errorf("extension_data deveria ter desaparecido (so tinha a ordem): %s", string(blob["extension_data"]))
	}
	if got := ordemDe(t, h, "s1"); len(got.Units) != 0 {
		t.Errorf("ordem apos DELETE = %+v, quero vazia", got.Units)
	}
}

// The validation refuses what cannot be projected, and nothing is written on refusal.
func TestOrdemDoDonoRecusaOrdemInvalida(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearSerieTresEpsT1(t, d)
	semearEntity(t, d, "outra-serie", "tv", "Outra")
	semearEntity(t, d, "ep-alheio", "episode", "Ep de outra")

	casos := []struct {
		nome   string
		corpo  string
		status int
		codigo string
	}{
		{"sem unidade", `{"units":[]}`, http.StatusUnprocessableEntity, "empty_units"},
		{"episodio de outra serie", `{"units":[{"season":1,"number":1,"episodes":["ep-alheio"]}]}`, http.StatusUnprocessableEntity, "episode_not_in_series"},
		{"episodio repetido", `{"units":[{"season":1,"number":1,"episodes":["e1"]},{"season":1,"number":2,"episodes":["e1"]}]}`, http.StatusUnprocessableEntity, "duplicate_episode"},
		{"par repetido", `{"units":[{"season":1,"number":1,"episodes":["e1"]},{"season":1,"number":1,"episodes":["e2"]}]}`, http.StatusUnprocessableEntity, "duplicate_number"},
		{"unidade vazia", `{"units":[{"season":1,"number":1,"episodes":[]}]}`, http.StatusUnprocessableEntity, "empty_unit"},
		{"numero zero", `{"units":[{"season":1,"number":0,"episodes":["e1"]}]}`, http.StatusUnprocessableEntity, "invalid_number"},
		{"corpo invalido", `{"units":`, http.StatusBadRequest, "invalid_body"},
	}
	for _, caso := range casos {
		rec := do(t, h, http.MethodPut, "/api/v1/entities/s1/owner-order", []byte(caso.corpo))
		if rec.Code != caso.status {
			t.Errorf("%s: status = %d, quero %d (corpo=%s)", caso.nome, rec.Code, caso.status, rec.Body.String())
			continue
		}
		var erro errorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &erro)
		if erro.Error.Code != caso.codigo {
			t.Errorf("%s: codigo = %q, quero %q", caso.nome, erro.Error.Code, caso.codigo)
		}
	}
	if got := ordemDe(t, h, "s1"); len(got.Units) != 0 {
		t.Errorf("ordem apos as recusas = %+v, quero vazia", got.Units)
	}
}

// The owner order does not exist for a non-series entity.
func TestOrdemDoDonoSoExisteParaSerie(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "m1", "movie", "Filme")

	if rec := do(t, h, http.MethodGet, "/api/v1/entities/m1/owner-order", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("GET em filme: status = %d, quero 422", rec.Code)
	}
	corpo := `{"units":[{"season":1,"number":1,"episodes":["m1"]}]}`
	if rec := do(t, h, http.MethodPut, "/api/v1/entities/m1/owner-order", []byte(corpo)); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("PUT em filme: status = %d, quero 422", rec.Code)
	}
}
