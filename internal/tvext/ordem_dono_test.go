package tvext

import (
	"database/sql"
	"testing"
)

// ordemBruta is a realistic override blob: the owner order next to another override, so
// the tests also prove the parser reads only its own key.
const ordemBruta = `{
  "title": {"pt-BR": "Minha Serie"},
  "extension_data": {"core.tv@1": {"owner_order": {
    "name": "HBO 2004",
    "based_on": "official",
    "units": [
      {"season": 5, "number": 5, "title": "Blackout", "episodes": ["a", "b", "c"]},
      {"season": 5, "number": 6, "episodes": ["d"]}
    ]
  }}}
}`

// TestOrdemDoDonoDoOverride: the order comes out of the override layer with its units,
// titles and the episode ids in the declared order.
func TestOrdemDoDonoDoOverride(t *testing.T) {
	ordem := OrdemDoDonoDoOverride(sql.NullString{Valid: true, String: ordemBruta})
	if ordem == nil {
		t.Fatal("ordem nil, quero a ordem do override")
	}
	if ordem.Name != "HBO 2004" || ordem.BasedOn != "official" {
		t.Errorf("nome/base = %q/%q, quero HBO 2004/official", ordem.Name, ordem.BasedOn)
	}
	if len(ordem.Units) != 2 {
		t.Fatalf("unidades = %d, quero 2", len(ordem.Units))
	}
	u := ordem.Units[0]
	if u.Season != 5 || u.Number != 5 || u.Title != "Blackout" {
		t.Errorf("unidade 1 = %+v, quero S05E05 Blackout", u)
	}
	if len(u.Episodes) != 3 || u.Episodes[0] != "a" || u.Episodes[2] != "c" {
		t.Errorf("episodios da unidade 1 = %v, quero [a b c]", u.Episodes)
	}
}

// TestOrdemDoDonoDegrada: absent override, unreadable blob, absent key and a unit without
// episodes all mean "no owner order" (nil), never an error.
func TestOrdemDoDonoDegrada(t *testing.T) {
	casos := map[string]sql.NullString{
		"sem override":    {},
		"vazio":           {Valid: true, String: ""},
		"json quebrado":   {Valid: true, String: `{"extension_data":`},
		"sem a chave":     {Valid: true, String: `{"extension_data":{"core.tv@1":{"x":1}}}`},
		"unidades vazias": {Valid: true, String: `{"extension_data":{"core.tv@1":{"owner_order":{"units":[]}}}}`},
		"unidade sem ep":  {Valid: true, String: `{"extension_data":{"core.tv@1":{"owner_order":{"units":[{"season":1,"number":1,"episodes":[]}]}}}}`},
		"sem numero":      {Valid: true, String: `{"extension_data":{"core.tv@1":{"owner_order":{"units":[{"season":1,"episodes":["a"]}]}}}}`},
	}
	for nome, raw := range casos {
		if got := OrdemDoDonoDoOverride(raw); got != nil {
			t.Errorf("%s: ordem = %+v, quero nil", nome, got)
		}
	}
}

// TestNumeroDeEpisodio: the projection gives every episode of a merged unit the same
// (season, number), with its position inside the unit — which is what the UI needs to
// show that one number holds three episodes.
func TestNumeroDeEpisodio(t *testing.T) {
	ordem := OrdemDoDonoDoOverride(sql.NullString{Valid: true, String: ordemBruta})
	numeros := ordem.NumeroDeEpisodio()
	if len(numeros) != 4 {
		t.Fatalf("numeros = %d, quero 4", len(numeros))
	}
	for i, id := range []string{"a", "b", "c"} {
		n, ok := numeros[id]
		if !ok {
			t.Fatalf("episodio %s sem numeracao", id)
		}
		if n.Season != 5 || n.Number != 5 {
			t.Errorf("%s = %+v, quero S05E05", id, n)
		}
		if n.Episodes != 3 || n.Ordinal != i {
			t.Errorf("%s = episodios %d ordinal %d, quero 3/%d", id, n.Episodes, n.Ordinal, i)
		}
	}
	if d := numeros["d"]; d.Season != 5 || d.Number != 6 || d.Episodes != 1 || d.Ordinal != 0 {
		t.Errorf("d = %+v, quero S05E06 sem merge", d)
	}
}

// TestOrdemDoDonoVazia: the zero value and a nil pointer are both "no order", so the
// callers never need a nil check beyond this method.
func TestOrdemDoDonoVazia(t *testing.T) {
	var nilOrdem *OrdemDono
	if !nilOrdem.Vazia() {
		t.Error("nil deveria ser vazia")
	}
	if !(&OrdemDono{}).Vazia() {
		t.Error("sem unidades deveria ser vazia")
	}
	if (&OrdemDono{Units: []UnidadeDono{{Season: 1, Number: 1, Episodes: []string{"a"}}}}).Vazia() {
		t.Error("com unidade nao deveria ser vazia")
	}
}
