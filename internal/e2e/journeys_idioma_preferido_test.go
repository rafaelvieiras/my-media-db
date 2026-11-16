package e2e

// journeys_idioma_preferido_test.go covers the language model (ADR-0018/ADR-0019)
// in its two per-library_source facets: the OPTIONAL preferred language for the
// search AND the display, and the candidate scoring against
// alternative/translated titles (side finding #2). Three journeys:
//
//   - TestJornadaIdiomaPreferidoBibliotecaNaBusca: a library with
//     preferred_language set makes the language reach the PROVIDER on the search
//     (match-preview) -- and one without a preference keeps falling back to the global
//     language (primary), never again the blind lang="" of before.
//   - TestJornadaIdiomaPreferidoBibliotecaNaExibicao: GET /entities/{id} WITHOUT
//     ?lang= uses the preferred language of the source library as the DISPLAY
//     default; an explicit ?lang= still always wins.
//   - TestJornadaTituloAlternativoVenceNoScore: an external candidate whose
//     ALTERNATIVE title matches much better than the main one (the "The Pink
//     Panther" case) makes matched_title come out filled in the match preview; a
//     candidate whose main title is already the best does not generate that noise.

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// acharRevisaoMovie returns the id of the (only) type=movie item in the review queue
// (GET /api/v1/review) whose title matches `titulo` exactly -- the draft the scanner
// created because the file had no embedded external id. t.Fatalf if not found.
func acharRevisaoMovie(t *testing.T, c *cliente, titulo string) string {
	t.Helper()
	fila := c.arr(http.MethodGet, "/api/v1/review", nil, http.StatusOK)
	for _, item := range fila {
		m, _ := item.(map[string]any)
		if str(t, m, "type") == "movie" && str(t, m, "title") == titulo {
			return str(t, m, "id")
		}
	}
	t.Fatalf("fila de revisao sem item movie com titulo %q: %v", titulo, fila)
	return ""
}

// acharMoviePorTitulo finds the id of a movie in the list of entities (not in the
// review queue) by any of the accepted titles. Since ADR-0031 the auto-match really
// runs in the scan, so the item that used to stay as a draft already comes out
// RESOLVED (with the title in the primary language) -- the journey can no longer
// depend on finding it in the queue.
func acharMoviePorTitulo(t *testing.T, c *cliente, titulos ...string) string {
	t.Helper()
	resposta := c.obj(http.MethodGet, "/api/v1/entities?type=movie&limit=1000", nil, http.StatusOK)
	itens, _ := resposta["items"].([]any)
	for _, item := range itens {
		m, _ := item.(map[string]any)
		for _, alvo := range titulos {
			if str(t, m, "title") == alvo {
				return str(t, m, "id")
			}
		}
	}
	t.Fatalf("nenhum movie com titulo em %v na lista de entities: %v", titulos, itens)
	return ""
}

// ultimoIdioma returns the LAST language recorded in c.prov.IdiomasBuscados()
// (the most recent Buscar call) -- used to prove which language actually reached the
// provider in a journey with multiple searches in sequence.
func ultimoIdioma(t *testing.T, c *cliente) string {
	t.Helper()
	langs := c.prov.IdiomasBuscados()
	if len(langs) == 0 {
		t.Fatalf("nenhuma busca registrada no provider fake")
	}
	return langs[len(langs)-1]
}

// TestJornadaIdiomaPreferidoBibliotecaNaBusca: side finding #1 of the language work
// -- automatch/retroactive/match-preview called buscarExterno with lang="" (not even
// the global language was respected). Now s.idiomaDeBusca resolves per item: a
// library with preferred_language="en" makes the "en" language reach the provider
// in the match preview, even with the global language (primary) in "pt-BR"
// (default); a library WITHOUT a preference keeps falling back to the global
// language, never again to a blind "" -- both behaviors proven in the SAME
// harness, through fake.Provider.IdiomasBuscados (an effect only visible there; no
// field of the match-preview HTTP body exposes the language of the query).
func TestJornadaIdiomaPreferidoBibliotecaNaBusca(t *testing.T) {
	c := novoHarness(t)

	// Library 1: explicit preferred_language="en". Title WITHOUT a fixture in the
	// fake catalog -- the item stays as a draft (that is what the draft preview
	// exercises here; with a fixture, the auto-match of the scan itself would
	// already resolve it).
	rootEN := t.TempDir()
	escreverVideo(t, filepath.Join(rootEN, "Alien Anos Oitenta.mkv"))
	srcEN := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": rootEN, "type": "movie", "preferred_language": "en"}, http.StatusCreated)
	if got := str(t, srcEN, "preferred_language"); got != "en" {
		t.Fatalf("preferred_language no cadastro = %q, quero \"en\"", got)
	}
	srcENID := str(t, srcEN, "id")
	c.esperarScan(srcENID)

	idEN := acharRevisaoMovie(t, c, "Alien Anos Oitenta")
	c.obj(http.MethodGet, "/api/v1/entities/"+idEN+"/match-preview", nil, http.StatusOK)
	if got := ultimoIdioma(t, c); got != "en" {
		t.Errorf("idioma buscado (biblioteca com preferred_language=en) = %q, quero \"en\"", got)
	}

	// Library 2: WITHOUT preferred_language -- it must fall back to the global
	// language (primary "pt-BR", app_settings default), NEVER again to a blind "".
	rootGlobal := t.TempDir()
	escreverVideo(t, filepath.Join(rootGlobal, "Filme Sem Catalogo Nenhum.mkv"))
	srcGlobal := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": rootGlobal, "type": "movie"}, http.StatusCreated)
	if srcGlobal["preferred_language"] != nil {
		t.Fatalf("preferred_language sem o campo no corpo = %v, quero null (sem preferencia)", srcGlobal["preferred_language"])
	}
	srcGlobalID := str(t, srcGlobal, "id")
	c.esperarScan(srcGlobalID)

	idGlobal := acharRevisaoMovie(t, c, "Filme Sem Catalogo Nenhum")
	c.obj(http.MethodGet, "/api/v1/entities/"+idGlobal+"/match-preview", nil, http.StatusOK)
	if got := ultimoIdioma(t, c); got != "pt-BR" {
		t.Errorf("idioma buscado (biblioteca sem preferencia) = %q, quero \"pt-BR\" (global primary, nunca \"\")", got)
	}
}

// TestJornadaIdiomaPreferidoBibliotecaNaExibicao: with a resolved item whose source
// library has preferred_language="en", GET /entities/{id} WITHOUT ?lang= returns the
// title in the LIBRARY language (en), not in the global language (pt-BR, default) --
// and an explicit ?lang= still always wins, on any item.
func TestJornadaIdiomaPreferidoBibliotecaNaExibicao(t *testing.T) {
	c := novoHarness(t)

	root := t.TempDir()
	escreverVideo(t, filepath.Join(root, "Matrix.mkv"))
	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "movie", "preferred_language": "en"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// Since ADR-0031 the auto-match runs in the scan itself: the entity already comes
	// out identified with the 603 fixture ("The Matrix"/"Matrix"), multi-language,
	// with no click at all.
	id := acharMoviePorTitulo(t, c, "Matrix", "The Matrix")

	// WITHOUT ?lang=: it falls back to the library preferred language (en) -- "The
	// Matrix", not the "Matrix" of the global language (pt-BR).
	semLang := c.obj(http.MethodGet, "/api/v1/entities/"+id, nil, http.StatusOK)
	if got := str(t, semLang, "title"); got != "The Matrix" {
		t.Errorf("title sem ?lang= = %q, quero \"The Matrix\" (idioma preferido da biblioteca, en)", got)
	}

	// An explicit ?lang= ALWAYS wins, even over the library preference.
	comLang := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if got := str(t, comLang, "title"); got != "Matrix" {
		t.Errorf("title com ?lang=pt-BR = %q, quero \"Matrix\" (override explicito vence)", got)
	}
}

// TestJornadaTituloAlternativoVenceNoScore: side finding #2 of the language work --
// when the title returned as the candidate's MAIN one (always "en" in the fake,
// tituloPreferido) does not match the file, but an ALTERNATIVE title that the
// provider only delivers through BuscarTitulosAlternativos matches much better, the
// final score uses the alternative (matching.PontuarComAlternativos) and the match
// preview exposes WHICH title won in matched_title -- the fake's "605" fixture
// ("A Pantera Cor-De-Rosa", see internal/provider/fake/catalogo.go) is the proof of
// that, analogous to the case described in ADR-0018. By contrast, when the candidate
// found (Matrix, 603) already has the main title as the best match, matched_title
// comes out empty -- no noise when it is not needed.
func TestJornadaTituloAlternativoVenceNoScore(t *testing.T) {
	c := novoHarness(t)

	// (a) File named EXACTLY like the alternative title of the "605" fixture -- its
	// MAIN title ("The Rose Prowler"/"O Perseguidor Rosa") is quite different and
	// would score low on its own.
	rootAlt := t.TempDir()
	escreverVideo(t, filepath.Join(rootAlt, "A Pantera Cor-De-Rosa.mkv"))
	srcAlt := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": rootAlt, "type": "movie"}, http.StatusCreated)
	c.esperarScan(str(t, srcAlt, "id"))

	idAlt := acharMoviePorTitulo(t, c, "O Perseguidor Rosa", "A Pantera Cor-De-Rosa", "The Rose Prowler")
	previewAlt := c.obj(http.MethodGet, "/api/v1/entities/"+idAlt+"/match-preview", nil, http.StatusOK)
	if got := str(t, previewAlt, "method"); got != "external" {
		t.Fatalf("method = %q, quero \"external\" (so a fixture 605 casa por titulo)", got)
	}
	targetAlt, ok := previewAlt["target"].(map[string]any)
	if !ok {
		t.Fatalf("target ausente ou nao-objeto em %v", previewAlt)
	}
	if got := str(t, targetAlt, "source_id"); got != "605" {
		t.Fatalf("source_id = %q, quero \"605\" (fixture do titulo alternativo)", got)
	}
	if got := str(t, targetAlt, "matched_title"); got != "A Pantera Cor-De-Rosa" {
		t.Errorf("matched_title = %q, quero \"A Pantera Cor-De-Rosa\" (o alternativo venceu, nao o titulo principal \"The Rose Prowler\")", got)
	}
	if score, _ := previewAlt["score"].(float64); score < server.LimiarAutoMatch {
		t.Errorf("score = %v, quero >= %v (o alternativo bate quase perfeito)", previewAlt["score"], server.LimiarAutoMatch)
	}

	// (b) Contrast: a file named like the MAIN title of a candidate (the Matrix,
	// 603) -- the main one is already the best, matched_title must come out empty
	// even with alternative titles available and queried (fixture 603 has "Matrix"
	// as an alternative, but it does not match better than the exact "The Matrix").
	rootPrincipal := t.TempDir()
	escreverVideo(t, filepath.Join(rootPrincipal, "The Matrix.mkv"))
	srcPrincipal := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": rootPrincipal, "type": "movie"}, http.StatusCreated)
	c.esperarScan(str(t, srcPrincipal, "id"))

	idPrincipal := acharMoviePorTitulo(t, c, "Matrix", "The Matrix")
	previewPrincipal := c.obj(http.MethodGet, "/api/v1/entities/"+idPrincipal+"/match-preview", nil, http.StatusOK)
	if got := str(t, previewPrincipal, "method"); got != "external" {
		t.Fatalf("method = %q, quero \"external\"", got)
	}
	targetPrincipal, ok := previewPrincipal["target"].(map[string]any)
	if !ok {
		t.Fatalf("target ausente ou nao-objeto em %v", previewPrincipal)
	}
	if got := str(t, targetPrincipal, "source_id"); got != "603" {
		t.Fatalf("source_id = %q, quero \"603\" (Matrix)", got)
	}
	if _, temCampo := targetPrincipal["matched_title"]; temCampo {
		t.Errorf("matched_title = %v, quero ausente (o titulo principal ja era o melhor, sem ruido)", targetPrincipal["matched_title"])
	}
}
