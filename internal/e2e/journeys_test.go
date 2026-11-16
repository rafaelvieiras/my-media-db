package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Layer 1 E2E journeys. Each test starts its own harness (clean database, migrated
// from scratch) and drives the real Server over HTTP, covering the same thing that
// used to be validated by hand through curl. Default language: pt-BR (fallback en).

// idExterno reads external_ids[fonte] from the resolved entity.
func idExterno(t *testing.T, ent map[string]any, fonte string) string {
	t.Helper()
	m, ok := ent["external_ids"].(map[string]any)
	if !ok {
		t.Fatalf("external_ids ausente em %v", ent)
	}
	s, _ := m[fonte].(string)
	return s
}

// TestJornadaImportarFilme: importing a movie (tmdb) resolves title/year/poster and
// writes the external identity; reimporting the same id REUSES it (dedup), it does not duplicate.
func TestJornadaImportarFilme(t *testing.T) {
	c := novoHarness(t)

	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)

	if got := str(t, ent, "title"); got != "Matrix" { // pt-BR default
		t.Errorf("title = %q, quero \"Matrix\" (pt-BR)", got)
	}
	if got := idExterno(t, ent, "tmdb"); got != "603" {
		t.Errorf("external_ids.tmdb = %q, quero \"603\"", got)
	}
	if rd, _ := ent["release_date"].(string); len(rd) < 4 || rd[:4] != "1999" {
		t.Errorf("release_date = %v, quero comecar em 1999", ent["release_date"])
	}
	if p, _ := ent["poster_url"].(string); p == "" {
		t.Error("poster_url vazio: o pôster deveria ter sido cacheado no enrich")
	}
	id := str(t, ent, "id")

	// The same id in English serves the en title (resolution by language).
	en := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=en", nil, http.StatusOK)
	if got := str(t, en, "title"); got != "The Matrix" {
		t.Errorf("title(en) = %q, quero \"The Matrix\"", got)
	}

	// Reimporting reuses (200) the SAME entity.
	reuso := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusOK)
	if str(t, reuso, "id") != id {
		t.Errorf("reimport criou entity nova (%q != %q): deveria deduplicar", str(t, reuso, "id"), id)
	}
}

// TestJornadaImportarColecao: importing a collection creates the `collection` entity
// and LINKS the member movies, which come back ordered by year in GET /members.
func TestJornadaImportarColecao(t *testing.T) {
	c := novoHarness(t)

	col := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "2344", "type": "collection"}, http.StatusCreated)
	if got := str(t, col, "type"); got != "collection" {
		t.Fatalf("type = %q, quero \"collection\"", got)
	}
	id := str(t, col, "id")

	membros := c.arr(http.MethodGet, "/api/v1/entities/"+id+"/members", nil, http.StatusOK)
	if len(membros) != 2 {
		t.Fatalf("membros = %d, quero 2 (Matrix + Reloaded)", len(membros))
	}
	// Ordered by year: Matrix (1999) before Reloaded (2003).
	primeiro, _ := membros[0].(map[string]any)
	segundo, _ := membros[1].(map[string]any)
	if t0, _ := primeiro["title"].(string); t0 != "Matrix" {
		t.Errorf("1o membro = %q, quero \"Matrix\" (1999 primeiro)", primeiro["title"])
	}
	if t1, _ := segundo["title"].(string); t1 != "Matrix Reloaded" {
		t.Errorf("2o membro = %q, quero \"Matrix Reloaded\" (2003 depois)", segundo["title"])
	}
}

// TestJornadaImportarSerie: importing a series (type=tv) creates the tree
// series->season->episode (entities tv/season/episode, ADR-0021) and GET /members
// returns the children ordered by sort_index — the seasons in the series and the
// episodes in the season. It exercises the fake provider as SerieProvider (canned
// tree), the import of the tree in the enrich and the branching of montarMembros by
// type.
func TestJornadaImportarSerie(t *testing.T) {
	c := novoHarness(t)

	// The fake series fixture has id 71663 (2 seasons of 3 episodes).
	serie := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "71663", "type": "tv"}, http.StatusCreated)
	if got := str(t, serie, "type"); got != "tv" {
		t.Fatalf("type = %q, quero \"tv\"", got)
	}
	serieID := str(t, serie, "id")

	// Seasons: 2, ordered by sort_index (season_number 1 before 2).
	temporadas := c.arr(http.MethodGet, "/api/v1/entities/"+serieID+"/members", nil, http.StatusOK)
	if len(temporadas) != 2 {
		t.Fatalf("temporadas = %d, quero 2", len(temporadas))
	}
	t1, _ := temporadas[0].(map[string]any)
	t2, _ := temporadas[1].(map[string]any)
	if tp := str(t, t1, "type"); tp != "season" {
		t.Errorf("1a temporada type = %q, quero \"season\"", tp)
	}
	// Season 1 with no Name in the source -> generated title "Temporada 1" (language independent).
	if got := str(t, t1, "title"); got != "Temporada 1" {
		t.Errorf("1a temporada title = %q, quero \"Temporada 1\"", got)
	}
	if tp := str(t, t2, "type"); tp != "season" {
		t.Errorf("2a temporada type = %q, quero \"season\"", tp)
	}

	// Episodes of the 1st season: 3, ordered by number (official 101,102,103).
	seasonID := str(t, t1, "id")
	eps := c.arr(http.MethodGet, "/api/v1/entities/"+seasonID+"/members", nil, http.StatusOK)
	if len(eps) != 3 {
		t.Fatalf("episodios = %d, quero 3", len(eps))
	}
	for i, numero := range []string{"101", "102", "103"} {
		m, _ := eps[i].(map[string]any)
		if tp := str(t, m, "type"); tp != "episode" {
			t.Errorf("episodio[%d] type = %q, quero \"episode\"", i, tp)
		}
		// The title depends on the language (pt-BR "Episodio NNN"); it asserts the number, robust to language.
		if got := str(t, m, "title"); !strings.Contains(got, numero) {
			t.Errorf("episodio[%d] title = %q, quero conter %q (ordem por numero)", i, got, numero)
		}
	}
}

// TestJornadaItemManualSincroniza: creating a LOCAL item with no source, marking an
// owner tag, pinning the external id (tmdb) and RE-ENRICHING pulls the source data
// through the pinned id — the neutral title yields to the external one and the NEW
// tags from the source come in, WITHOUT deleting the owner tag. It closes the
// synchronization cycle.
func TestJornadaItemManualSincroniza(t *testing.T) {
	c := novoHarness(t)

	item := c.obj(http.MethodPost, "/api/v1/entities",
		map[string]string{"type": "movie", "title": "Meu Filme Local"}, http.StatusCreated)
	id := str(t, item, "id")

	// The owner marks their own tag (delta add) in the reading language.
	c.obj(http.MethodPatch, "/api/v1/entities/"+id+"/override?lang=pt-BR",
		map[string]any{"tags": map[string]any{"add": []string{"MinhaTag"}, "remove": []string{}}}, http.StatusOK)

	// Pins the tmdb identity and re-enriches (the enrich reads external_ids[tmdb]).
	c.obj(http.MethodPut, "/api/v1/entities/"+id+"/external-ids",
		map[string]any{"external_ids": map[string]string{"tmdb": "603"}}, http.StatusOK)
	c.obj(http.MethodPost, "/api/v1/entities/"+id+"/enrich", map[string]any{}, http.StatusOK)

	got := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if title := str(t, got, "title"); title != "Matrix" {
		t.Errorf("title pos-enrich = %q, quero \"Matrix\" (externo cede sobre o neutro)", title)
	}
	tags := strs(t, got, "tags")
	if !contem(tags, "MinhaTag") {
		t.Errorf("tags = %v, deveria preservar a tag do dono \"MinhaTag\"", tags)
	}
	if !contem(tags, "Ficcao cientifica") {
		t.Errorf("tags = %v, deveria receber a tag NOVA da fonte \"Ficcao cientifica\"", tags)
	}
}

// TestJornadaEnrichNaoDestrutivo: the heart of ADR-0012. Edit the title (override)
// and the tags (delta add/remove) and then RE-ENRICH: the edited title REMAINS, the
// removed tag does NOT come back, the added tag stays, and the untouched external
// tags stay alive underneath. It encodes the guarantee of the 3-layer resolver.
func TestJornadaEnrichNaoDestrutivo(t *testing.T) {
	c := novoHarness(t)

	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// Edits the title (the override wins over everything) and the tags (add Favorito,
	// remove Acao).
	c.obj(http.MethodPatch, "/api/v1/entities/"+id+"/override?lang=pt-BR",
		map[string]any{"title": "Matrix EDITADO"}, http.StatusOK)
	c.obj(http.MethodPatch, "/api/v1/entities/"+id+"/override?lang=pt-BR",
		map[string]any{"tags": map[string]any{"add": []string{"Favorito"}, "remove": []string{"Acao"}}}, http.StatusOK)

	// Re-enriches from the SAME source: nothing the owner edited may change.
	c.obj(http.MethodPost, "/api/v1/entities/"+id+"/enrich", map[string]any{}, http.StatusOK)

	got := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if title := str(t, got, "title"); title != "Matrix EDITADO" {
		t.Errorf("title pos-enrich = %q, quero \"Matrix EDITADO\" (override congela)", title)
	}
	tags := strs(t, got, "tags")
	if !contem(tags, "Favorito") {
		t.Errorf("tags = %v, deveria manter a tag adicionada \"Favorito\"", tags)
	}
	if contem(tags, "Acao") {
		t.Errorf("tags = %v, a tag removida \"Acao\" NAO deveria voltar apos enrich", tags)
	}
	if !contem(tags, "Ficcao cientifica") {
		t.Errorf("tags = %v, a tag externa intocada \"Ficcao cientifica\" deveria seguir viva", tags)
	}
}

// TestJornadaNotaLocal: the owner's LOCAL RATING (ADR-0012). Importing a movie brings
// the SOURCE rating (vote_average); writing the local rating through the override
// exposes it in local_rating WITHOUT touching the source one; re-enriching does not
// erase it (the override freezes); CLEARING the rating (DELETE of the field) brings
// local_rating back to null, and the source one stays there.
func TestJornadaNotaLocal(t *testing.T) {
	c := novoHarness(t)

	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// The local rating does not exist yet (the owner has not rated it). The DTO carries
	// the `vote_average` field (SOURCE rating), distinct — we capture its value to
	// prove that the local rating does not touch it (the local rating is
	// NON-DESTRUCTIVE, ADR-0012).
	if ent["local_rating"] != nil {
		t.Errorf("local_rating = %v, quero null antes de o dono avaliar", ent["local_rating"])
	}
	if _, temCampo := ent["vote_average"]; !temCampo {
		t.Fatalf("DTO sem o campo vote_average (nota da fonte): %v", ent)
	}
	fonte := ent["vote_average"] // the source value, whatever it is (it may be null)

	// The owner writes the local rating (neutral override field).
	c.obj(http.MethodPatch, "/api/v1/entities/"+id+"/override?lang=pt-BR",
		map[string]any{"local_rating": 9.5}, http.StatusOK)

	comNota := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if nl, _ := comNota["local_rating"].(float64); nl != 9.5 {
		t.Errorf("local_rating = %v, quero 9.5 (nota do dono)", comNota["local_rating"])
	}
	// The SOURCE rating does not change when the local rating is set (distinct fields).
	if comNota["vote_average"] != fonte {
		t.Errorf("vote_average = %v, quero %v (a nota da fonte segue intacta)", comNota["vote_average"], fonte)
	}

	// Re-enriching does not erase the owner rating (the override freezes, ADR-0012).
	c.obj(http.MethodPost, "/api/v1/entities/"+id+"/enrich", map[string]any{}, http.StatusOK)
	posEnrich := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if nl, _ := posEnrich["local_rating"].(float64); nl != 9.5 {
		t.Errorf("local_rating pos-enrich = %v, quero 9.5 (override congela)", posEnrich["local_rating"])
	}

	// CLEARING the local rating (DELETE of the field): it goes back to null; the source
	// one remains.
	c.obj(http.MethodDelete, "/api/v1/entities/"+id+"/override/local_rating", nil, http.StatusOK)
	limpa := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if limpa["local_rating"] != nil {
		t.Errorf("local_rating apos limpar = %v, quero null (volta a mostrar a da fonte)", limpa["local_rating"])
	}
	if limpa["vote_average"] != fonte {
		t.Errorf("vote_average apos limpar = %v, quero %v", limpa["vote_average"], fonte)
	}
}

// TestJornadaInternoFirstNaBusca: an already imported item shows up in the external
// search with internal_id pointing at the internal one; a non-imported one comes with
// a null internal_id. It is the base of internal-first: the UI shows "na biblioteca"
// and links to the internal item.
func TestJornadaInternoFirstNaBusca(t *testing.T) {
	c := novoHarness(t)

	imp := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	idInterno := str(t, imp, "id")

	resp := c.obj(http.MethodGet, "/api/v1/search?q=matrix&type=movie", nil, http.StatusOK)
	externos, ok := resp["external"].([]any)
	if !ok || len(externos) == 0 {
		t.Fatalf("busca externa vazia: %v", resp["external"])
	}

	var achou603, achou604 bool
	for _, e := range externos {
		m, _ := e.(map[string]any)
		switch m["source_id"] {
		case "603":
			achou603 = true
			if got, _ := m["internal_id"].(string); got != idInterno {
				t.Errorf("603 internal_id = %v, quero %q (ja importado)", m["internal_id"], idInterno)
			}
		case "604":
			achou604 = true
			if m["internal_id"] != nil {
				t.Errorf("604 internal_id = %v, quero nulo (nao importado)", m["internal_id"])
			}
		}
	}
	if !achou603 || !achou604 {
		t.Errorf("busca deveria trazer 603 e 604 (achou603=%v achou604=%v)", achou603, achou604)
	}
}

// tipos collects the `type` field of each item of a list of JSON objects.
func tipos(itens []any) []string {
	out := make([]string, 0, len(itens))
	for _, it := range itens {
		m, _ := it.(map[string]any)
		if tp, ok := m["type"].(string); ok {
			out = append(out, tp)
		}
	}
	return out
}

// TestJornadaArvoreSeriesTopoBreadcrumb: ADR-0030. After importing the series tree
// (tv + 2 seasons + 6 episodes), the HOME and the local SEARCH only show TOP-LEVEL
// WORKS (the tv series), never season/episode; and GET /entities/{id} brings the
// ANCESTORS (root->parent breadcrumb): episode -> [series, season]; season -> [series];
// series -> [] (no parent).
func TestJornadaArvoreSeriesTopoBreadcrumb(t *testing.T) {
	c := novoHarness(t)

	serie := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "71663", "type": "tv"}, http.StatusCreated)
	serieID := str(t, serie, "id")

	// (a) HOME/library: only the tv series shows up; no season/episode.
	home := c.obj(http.MethodGet, "/api/v1/entities", nil, http.StatusOK)
	assertHomeSoSerie(t, home, serieID)

	// (b) DEFAULT local SEARCH: "Episodio" would match the 6 episodes if they were
	// indexed; sub-nodes are left out by default -> 0 (the default preserves the
	// top-level-only behavior).
	resp := c.obj(http.MethodGet, "/api/v1/search?q=Episodio", nil, http.StatusOK)
	locais, _ := resp["local"].([]any)
	if len(locais) != 0 {
		t.Errorf("busca default por \"Episodio\" trouxe %d locais, quero 0 (sub-nos fora). tipos=%v",
			len(locais), tipos(locais))
	}
	// The top-level series is still findable through the search.
	respSerie := c.obj(http.MethodGet, "/api/v1/search?q=Contos", nil, http.StatusOK)
	locaisSerie, _ := respSerie["local"].([]any)
	semSubNos(t, locaisSerie)
	if len(locaisSerie) != 1 {
		t.Errorf("busca por \"Contos\" = %v, quero 1 resultado (a serie)", tipos(locaisSerie))
	}

	// (b2) include=episode RELINKS the 6 episodes into their own section, all of type
	// episode — without mixing with the series (the search for "Episodio" does not match
	// "Contos").
	respEp := c.obj(http.MethodGet, "/api/v1/search?q=Episodio&include=episode", nil, http.StatusOK)
	locaisEp, _ := respEp["local"].([]any)
	if len(locaisEp) != 6 {
		t.Errorf("include=episode por \"Episodio\" trouxe %d, quero 6 episodios. tipos=%v",
			len(locaisEp), tipos(locaisEp))
	}
	assertTodosTipo(t, locaisEp, "episode")

	// (b3) include=season relinks the seasons: season 1 ("Temporada 1") matches and
	// comes as a season sub-node; everything that returns is season (never series/episode).
	respTemp := c.obj(http.MethodGet, "/api/v1/search?q=Temporada&include=season", nil, http.StatusOK)
	locaisTemp, _ := respTemp["local"].([]any)
	if len(locaisTemp) == 0 {
		t.Errorf("include=season por \"Temporada\" trouxe 0, quero ao menos a temporada 1")
	}
	assertTodosTipo(t, locaisTemp, "season")

	// (c) ANCESTORS (root->parent breadcrumb).
	// Series (top-level): empty ancestors (but present, never null).
	if anc := ancestrais(t, serie); len(anc) != 0 {
		t.Errorf("serie ancestors = %v, quero [] (obra de topo)", anc)
	}

	// Season 1: ancestors = [series].
	temporadas := c.arr(http.MethodGet, "/api/v1/entities/"+serieID+"/members", nil, http.StatusOK)
	tempID := str(t, temporadas[0].(map[string]any), "id")
	tempResolvida := c.obj(http.MethodGet, "/api/v1/entities/"+tempID, nil, http.StatusOK)
	ancTemp := ancestrais(t, tempResolvida)
	if len(ancTemp) != 1 {
		t.Fatalf("temporada ancestors = %d, quero 1 (a serie). %v", len(ancTemp), ancTemp)
	}
	assertAncestor(t, ancTemp[0], serieID, "tv", "temporada->raiz")

	// Episode 1 of season 1: ancestors = [series, season] (root->parent).
	eps := c.arr(http.MethodGet, "/api/v1/entities/"+tempID+"/members", nil, http.StatusOK)
	epID := str(t, eps[0].(map[string]any), "id")
	epResolvido := c.obj(http.MethodGet, "/api/v1/entities/"+epID, nil, http.StatusOK)
	ancEp := ancestrais(t, epResolvido)
	if len(ancEp) != 2 {
		t.Fatalf("episodio ancestors = %d, quero 2 [serie, temporada]. %v", len(ancEp), ancEp)
	}
	assertAncestor(t, ancEp[0], serieID, "tv", "episodio->raiz")
	assertAncestor(t, ancEp[1], tempID, "season", "episodio->pai")
}

// assertHomeSoSerie asserts that the home lists ONLY the top-level series (`serieID`),
// without any sub-node (season/episode), and that the pagination total matches the page.
func assertHomeSoSerie(t *testing.T, home map[string]any, serieID string) {
	t.Helper()
	itens, _ := home["items"].([]any)
	semSubNos(t, itens)
	if len(itens) != 1 || str(t, itens[0].(map[string]any), "id") != serieID {
		t.Fatalf("home = %v, quero so a serie %q (sub-nos excluidos)", tipos(itens), serieID)
	}
	if total, _ := home["total"].(float64); total != 1 {
		t.Errorf("home total = %v, quero 1 (bate com a pagina, sub-nos excluidos)", home["total"])
	}
}

// assertTodosTipo asserts that ALL items of the list have the expected type (used to
// prove that a sub-node section relinked by include= does not mix other types).
func assertTodosTipo(t *testing.T, itens []any, want string) {
	t.Helper()
	for _, tp := range tipos(itens) {
		if tp != want {
			t.Errorf("resultado com type %q, quero so %q", tp, want)
		}
	}
}

// semSubNos asserts that no item of the list is season/episode (only top-level works).
func semSubNos(t *testing.T, itens []any) {
	t.Helper()
	for _, tp := range tipos(itens) {
		if tp == "season" || tp == "episode" {
			t.Errorf("listou sub-no %q: so obras de topo (movie/tv/collection) sao permitidas", tp)
		}
	}
}

// assertAncestor asserts the id and the type of an ancestor node of the breadcrumb.
func assertAncestor(t *testing.T, node map[string]any, wantID, wantType, rotulo string) {
	t.Helper()
	if id := str(t, node, "id"); id != wantID {
		t.Errorf("%s: ancestor id = %q, quero %q", rotulo, id, wantID)
	}
	if tp := str(t, node, "type"); tp != wantType {
		t.Errorf("%s: ancestor type = %q, quero %q", rotulo, tp, wantType)
	}
}

// ancestrais extracts the `ancestors` array of a resolved entity as []map (it requires
// the field to be present — never null, by contract).
func ancestrais(t *testing.T, ent map[string]any) []map[string]any {
	t.Helper()
	bruto, ok := ent["ancestors"].([]any)
	if !ok {
		t.Fatalf("campo ancestors ausente ou nao-array em %v", ent["ancestors"])
	}
	out := make([]map[string]any, 0, len(bruto))
	for _, e := range bruto {
		m, _ := e.(map[string]any)
		out = append(out, m)
	}
	return out
}

// escreverVideo creates a video file with unique content (the path itself), like
// escrever() of the scanner package: a recognized video extension (.mkv) and a
// distinct content_hash per file. The content is not a real video; ffprobe (if
// present) fails gracefully and the mediainfo stays empty — the match path does not
// depend on it.
func escreverVideo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("video-"+path), 0o644); err != nil {
		t.Fatalf("escrever %q: %v", path, err)
	}
}

// esperarScan fires the scan of a datasource (POST .../scan, 202) and waits for it to
// finish. The scan runs in the background (goroutine): the DEFINITIVE signal of
// successful completion is the datasource `last_scan`, stamped only in
// finalizarCicloVida. It does a short poll.
func (c *cliente) esperarScan(id string) {
	c.t.Helper()
	c.obj(http.MethodPost, "/api/v1/library/"+id+"/scan", nil, http.StatusAccepted)
	prazo := time.Now().Add(30 * time.Second)
	for time.Now().Before(prazo) {
		src := c.obj(http.MethodGet, "/api/v1/library/"+id, nil, http.StatusOK)
		if src["last_scan"] != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	c.t.Fatalf("scan de %s nao concluiu no prazo", id)
}

// arquivosDaEntity lists the files of an entity (GET .../files) as []map. For an
// EPISODE, they come from the N:M links (media_file_entity, kind='tv_episode') — the
// proof that the scanner linked file->episode by id.
func (c *cliente) arquivosDaEntity(id string) []map[string]any {
	c.t.Helper()
	resp := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/files", nil, http.StatusOK)
	bruto, _ := resp["items"].([]any)
	out := make([]map[string]any, 0, len(bruto))
	for _, e := range bruto {
		m, _ := e.(map[string]any)
		out = append(out, m)
	}
	return out
}

// posse extracts the `ownership` object {owned_count, total_count} from a resolved
// entity (season/series). It requires the field to be present (never absent for
// tv/season).
func posse(t *testing.T, ent map[string]any) (owned, total int) {
	t.Helper()
	o, ok := ent["ownership"].(map[string]any)
	if !ok {
		t.Fatalf("campo ownership ausente ou nao-objeto em %v", ent["ownership"])
	}
	oc, _ := o["owned_count"].(float64)
	tc, _ := o["total_count"].(float64)
	return int(oc), int(tc)
}

// idsDosMembros returns the ids of the children (GET .../members) of an entity, in order.
func (c *cliente) idsDosMembros(id string) []string {
	c.t.Helper()
	membros := c.arr(http.MethodGet, "/api/v1/entities/"+id+"/members", nil, http.StatusOK)
	out := make([]string, 0, len(membros))
	for _, m := range membros {
		mm, _ := m.(map[string]any)
		out = append(out, str(c.t, mm, "id"))
	}
	return out
}

// TestJornadaScanSerie: the end-to-end journey of the SERIES SCANNER (ADR-0021,
// Phase 2). A NON-imported series folder, marked content_type='series', is swept: the
// scanner auto-imports the provider tree (fake, "Springfield Tales"/71663), matches
// each file to the episode(s) by number and writes the N:M links anchored on the id.
// Covers: (a) auto-import; (b) single file -> 1 link; (c) RANGE S02E01-E03 -> 3
// episodes linked to the SAME file; (d) files per episode through the junction; (e)
// coherent aggregate ownership (owned<=total) on the season and on the series.
func TestJornadaScanSerie(t *testing.T) {
	c := novoHarness(t)

	// Disk structure: series folder, season, 2 standalone episodes in S1 and a RANGE
	// file covering the 3 episodes of S2 (official S02E01..E03).
	root := t.TempDir()
	base := filepath.Join(root, "Springfield Tales")
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E01.mkv"))
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E02.mkv"))
	escreverVideo(t, filepath.Join(base, "Season 02", "Springfield Tales S02E01-E03.mkv"))

	// (2) Registers the datasource marked as a series and (3) fires+awaits the scan.
	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "tv", "content_type": "series"}, http.StatusCreated)
	if got := str(t, src, "content_type"); got != "series" {
		t.Fatalf("content_type = %q, quero \"series\" (persistido no cadastro)", got)
	}
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// (4a) The series was AUTO-IMPORTED: the home lists exactly 1 top-level work, the tv one.
	home := c.obj(http.MethodGet, "/api/v1/entities", nil, http.StatusOK)
	itens, _ := home["items"].([]any)
	if len(itens) != 1 {
		t.Fatalf("home = %d itens, quero 1 (so a serie auto-importada). tipos=%v", len(itens), tipos(itens))
	}
	serie := itens[0].(map[string]any)
	if tp := str(t, serie, "type"); tp != "tv" {
		t.Fatalf("obra de topo type = %q, quero \"tv\"", tp)
	}
	serieID := str(t, serie, "id")

	// Imported structure: 2 seasons; S1 with eps [101,102,103], S2 with [201,202,203].
	temporadas := c.idsDosMembros(serieID)
	if len(temporadas) != 2 {
		t.Fatalf("temporadas = %d, quero 2", len(temporadas))
	}
	epsT1 := c.idsDosMembros(temporadas[0])
	epsT2 := c.idsDosMembros(temporadas[1])
	if len(epsT1) != 3 || len(epsT2) != 3 {
		t.Fatalf("episodios = T1:%d T2:%d, quero 3 e 3", len(epsT1), len(epsT2))
	}

	// (4b) Standalone files became links: eps 101 and 102 owned (1 file each), ep 103
	// NOT owned (no file covered S01E03).
	assertFileCount(t, c, epsT1[0], 1) // S01E01 -> ep 101
	assertFileCount(t, c, epsT1[1], 1) // S01E02 -> ep 102
	assertFileCount(t, c, epsT1[2], 0) // S01E03 with no file

	// (4c) RANGE: the single S02E01-E03 file generated 3 links, one per episode of S2,
	// ALL of them pointing at the SAME media_file (1 file -> N episodes, anchored on the id).
	assertRangeUmArquivoTresEps(t, c, epsT2, "S02E01-E03")

	// (4d/4e) Coherent aggregate ownership (owned<=total): S1 owns 2/3; S2 owns 3/3; the
	// series aggregates 5/6 (never owned>total).
	assertPosse(t, c, temporadas[0], 2, 3)
	assertPosse(t, c, temporadas[1], 3, 3)
	assertPosse(t, c, serieID, 5, 6)
}

// assertFileCount asserts the file_count of a resolved episode (GET /entities/{id}) —
// the episode files come from the N:M links, not from media_file.entity_id.
func assertFileCount(t *testing.T, c *cliente, epID string, quero int) {
	t.Helper()
	ent := c.obj(http.MethodGet, "/api/v1/entities/"+epID, nil, http.StatusOK)
	fc, _ := ent["file_count"].(float64)
	if int(fc) != quero {
		t.Errorf("ep %s file_count = %v, quero %d", epID, ent["file_count"], quero)
	}
}

// assertRangeUmArquivoTresEps asserts that ALL episodes receive exactly 1 file, that
// its path contains the range marker, and that it is the SAME media_file (id) for all
// of them — the end-to-end proof of 1 file -> N episodes (N:M links).
func assertRangeUmArquivoTresEps(t *testing.T, c *cliente, eps []string, marcador string) {
	t.Helper()
	var idRange string
	for i, epID := range eps {
		arqs := c.arquivosDaEntity(epID)
		if len(arqs) != 1 {
			t.Fatalf("ep[%d]: arquivos = %d, quero 1 (o arquivo de range)", i, len(arqs))
		}
		if p := str(t, arqs[0], "path"); !strings.Contains(p, marcador) {
			t.Errorf("ep[%d]: arquivo = %q, quero o arquivo de range %q", i, p, marcador)
		}
		id := str(t, arqs[0], "id")
		if i == 0 {
			idRange = id
		} else if id != idRange {
			t.Errorf("ep[%d]: media_file id = %q, quero o MESMO do range (%q)", i, id, idRange)
		}
	}
}

// assertPosse asserts the aggregate ownership (owned/total) of a parent entity
// (season/series) and the owned<=total coherence.
func assertPosse(t *testing.T, c *cliente, id string, owned, total int) {
	t.Helper()
	ent := c.obj(http.MethodGet, "/api/v1/entities/"+id, nil, http.StatusOK)
	o, tt := posse(t, ent)
	if o != owned || tt != total {
		t.Errorf("posse de %s = %d/%d, quero %d/%d", id, o, tt, owned, total)
	}
	if o > tt {
		t.Errorf("posse incoerente em %s: owned (%d) > total (%d)", id, o, tt)
	}
}

// TestJornadaHealthSystem: GET /api/v1/health/system returns the telemetry snapshot
// (database+usage+storage) reflecting the real catalog -- an end-to-end proof of
// internal/telemetry + internal/server/health_system.go against the SAME real Server
// of the harness. It also checks that the immediate second call hits the TTL cache
// (telemetry_cache.go).
func TestJornadaHealthSystem(t *testing.T) {
	c := novoHarness(t)

	c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)

	body := c.obj(http.MethodGet, "/api/v1/health/system", nil, http.StatusOK)

	if got := str(t, body, "computed_at"); got == "" {
		t.Error("computed_at vazio")
	}

	dbInfo, ok := body["database"].(map[string]any)
	if !ok {
		t.Fatalf("database ausente ou não-objeto em %v", body)
	}
	if sz, _ := dbInfo["size_bytes"].(float64); sz <= 0 {
		t.Errorf("database.size_bytes = %v, quero > 0", dbInfo["size_bytes"])
	}
	if sv, _ := dbInfo["schema_version"].(float64); sv <= 0 {
		t.Errorf("database.schema_version = %v, quero > 0", dbInfo["schema_version"])
	}

	usage, ok := body["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage ausente ou não-objeto em %v", body)
	}
	entUso, ok := usage["entities"].(map[string]any)
	if !ok {
		t.Fatalf("usage.entities ausente ou não-objeto em %v", usage)
	}
	if total, _ := entUso["total"].(float64); total < 1 {
		t.Errorf("usage.entities.total = %v, quero >= 1 (filme importado)", entUso["total"])
	}

	if _, ok := body["storage"].(map[string]any); !ok {
		t.Fatalf("storage ausente ou não-objeto em %v", body)
	}

	// The immediate second call must hit the TTL cache (same computed_at).
	again := c.obj(http.MethodGet, "/api/v1/health/system", nil, http.StatusOK)
	if str(t, again, "computed_at") != str(t, body, "computed_at") {
		t.Error("segunda chamada imediata deveria vir do cache (mesmo computed_at)")
	}
}
