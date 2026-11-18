package e2e

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// TestJornadaOrdemDoDono: the OWNER ORDER (ADR-0033), end to end. A release that puts
// two aired episodes in ONE numbered entry is something no provider can express — TMDB
// groups map one entry to one episode, TheTVDB has no combined record. So the owner
// declares it, and both the tree and the next scan must obey the declaration.
//
// Covers: (a) the empty order of a series that never had one; (b) the refusal of an
// episode that is not in the series (422, nothing written); (c) the declaration of
// S01E01 = [101,102]; (d) the MERGED projection (two episodes on the same number, with
// their ordinal inside the unit); (e) the rescan linking the S01E01 file to BOTH
// episodes through the N:M junction; (f) the removal, which brings the series back to
// the provider order.
func TestJornadaOrdemDoDono(t *testing.T) {
	c := novoHarness(t)

	// Two files of the fake provider tree (S01E01..E03 of "Springfield Tales"/71663):
	// only the first two have a file, so the merge below changes a real link set.
	root := t.TempDir()
	base := filepath.Join(root, "Springfield Tales")
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E01.mkv"))
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E02.mkv"))

	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "tv", "content_type": "series"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	home := c.obj(http.MethodGet, "/api/v1/entities", nil, http.StatusOK)
	itens, _ := home["items"].([]any)
	if len(itens) != 1 {
		t.Fatalf("home = %d itens, quero 1 (so a serie importada). tipos=%v", len(itens), tipos(itens))
	}
	serieID := str(t, itens[0].(map[string]any), "id")
	temporadas := c.idsDosMembros(serieID)
	if len(temporadas) != 2 {
		t.Fatalf("temporadas = %d, quero 2", len(temporadas))
	}
	epsT1 := c.idsDosMembros(temporadas[0])
	if len(epsT1) != 3 {
		t.Fatalf("episodios da T1 = %d, quero 3", len(epsT1))
	}
	caminho := "/api/v1/entities/" + serieID + "/owner-order"

	// (a) A series that never had an owner order answers with an empty list (not 404):
	// the editor needs a shape to render, and the owner must be able to CREATE one.
	vazia := c.obj(http.MethodGet, caminho, nil, http.StatusOK)
	if un, _ := vazia["units"].([]any); len(un) != 0 {
		t.Fatalf("unidades iniciais = %d, quero 0 (%v)", len(un), vazia)
	}

	// (b) An episode that does not belong to the series is refused BEFORE writing: the
	// order stays empty (a wrong declaration must never land half-written).
	status, raw := c.bruto(http.MethodPut, caminho, map[string]any{
		"name":     "Corte do dono",
		"based_on": "official",
		"units": []any{
			map[string]any{"season": 1, "number": 1, "episodes": []string{epsT1[0], "nao-existe"}},
		},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("PUT com episodio alheio: status = %d, quero 422 (corpo=%s)", status, raw)
	}
	if !strings.Contains(string(raw), "episode_not_in_series") {
		t.Fatalf("PUT com episodio alheio: codigo esperado episode_not_in_series, corpo=%s", raw)
	}
	if un, _ := c.obj(http.MethodGet, caminho, nil, http.StatusOK)["units"].([]any); len(un) != 0 {
		t.Fatalf("unidades depois da recusa = %d, quero 0 (nada pode ter sido gravado)", len(un))
	}

	// (c) The owner declares that the FIRST entry of their release holds episodes 101
	// and 102 (a re-cut that glued two aired episodes into one).
	c.obj(http.MethodPut, caminho, map[string]any{
		"name":     "Corte do dono",
		"based_on": "official",
		"units": []any{
			map[string]any{"season": 1, "number": 1, "title": "Abertura dupla",
				"episodes": []string{epsT1[0], epsT1[1]}},
			map[string]any{"season": 1, "number": 2, "episodes": []string{epsT1[2]}},
		},
	}, http.StatusOK)

	// (d) The projection shares ONE number between the two merged episodes and tells the
	// position of each inside the unit — the tree shows the owner's numbering, not the
	// provider's.
	arvore := c.obj(http.MethodGet, "/api/v1/entities/"+serieID+"/episodes", nil, http.StatusOK)
	if ord := str(t, arvore, "ordering"); ord != "owner" {
		t.Fatalf("ordering da arvore = %q, quero %q (a ordem do dono vira a padrao)", ord, "owner")
	}
	merged := numeracoesPorEpisodio(t, arvore)
	for i, epID := range []string{epsT1[0], epsT1[1]} {
		n := merged[epID]
		if n.merged != 2 {
			t.Errorf("ep %d: merged_episodes = %d, quero 2", i+1, n.merged)
		}
		if n.ordinal != i+1 {
			t.Errorf("ep %d: merged_ordinal = %d, quero %d", i+1, n.ordinal, i+1)
		}
		if n.number != 1 {
			t.Errorf("ep %d: number = %d, quero 1 (as duas dividem o numero da unidade)", i+1, n.number)
		}
	}

	// (e) The rescan resolves the S01E01 file by the DECLARED order: it links the file to
	// both episodes of the unit (the N:M junction by id), leaving the second file alone.
	c.esperarScan(srcID)
	for i, epID := range []string{epsT1[0], epsT1[1]} {
		if arqs := c.arquivosDaEntity(epID); len(arqs) != 1 {
			t.Errorf("ep %d: arquivos = %d, quero 1 (o arquivo da unidade)", i+1, len(arqs))
		}
	}
	if epID := epsT1[2]; len(c.arquivosDaEntity(epID)) != 0 {
		t.Errorf("ep 3 continua sem arquivo: a unidade nao pode arrastar o episodio esquecido")
	}

	// (f) Removing brings the series back to the provider order — the endpoint is
	// idempotent, so a second DELETE also answers 204.
	c.obj(http.MethodDelete, caminho, nil, http.StatusNoContent)
	c.obj(http.MethodDelete, caminho, nil, http.StatusNoContent)
	if un, _ := c.obj(http.MethodGet, caminho, nil, http.StatusOK)["units"].([]any); len(un) != 0 {
		t.Fatalf("unidades depois do DELETE = %d, quero 0", len(un))
	}
	if ord := str(t, c.obj(http.MethodGet, "/api/v1/entities/"+serieID+"/episodes", nil, http.StatusOK), "ordering"); ord != "official" {
		t.Fatalf("ordering depois do DELETE = %q, quero %q (volta para a ordem do provider)", ord, "official")
	}
}

// numeracaoDono is the projection of one episode card onto the owner order, as the
// series tree returns it.
type numeracaoDono struct {
	season  int
	number  int
	merged  int // merged_episodes
	ordinal int // merged_ordinal
}

// numeracoesPorEpisodio flattens the groups of GET /entities/{id}/episodes into a map
// episode id -> numbering, so the assertions do not depend on the group order.
func numeracoesPorEpisodio(t *testing.T, arvore map[string]any) map[string]numeracaoDono {
	t.Helper()
	out := map[string]numeracaoDono{}
	grupos, _ := arvore["groups"].([]any)
	for _, g := range grupos {
		gm, _ := g.(map[string]any)
		eps, _ := gm["episodes"].([]any)
		for _, e := range eps {
			em, _ := e.(map[string]any)
			n, _ := em["numbering"].(map[string]any)
			out[str(t, em, "id")] = numeracaoDono{
				season:  int(numero(n, "season")),
				number:  int(numero(n, "number")),
				merged:  int(numero(n, "merged_episodes")),
				ordinal: int(numero(n, "merged_ordinal")),
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("arvore sem episodios: %v", arvore)
	}
	return out
}

// numero reads a numeric JSON field, defaulting to 0 when it is absent (the omitempty
// fields of the merged projection are absent on a unit of one episode).
func numero(m map[string]any, chave string) float64 {
	if m == nil {
		return 0
	}
	v, _ := m[chave].(float64)
	return v
}
