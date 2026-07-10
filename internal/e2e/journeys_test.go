package e2e

import (
	"net/http"
	"testing"
)

// Jornadas E2E da Camada 1 (briefing 0028 §7). Cada teste sobe um harness próprio
// (banco limpo, migrado do zero) e dirige o Server real por HTTP, cobrindo o mesmo
// que antes era validado à mão via curl. Idioma default: pt-BR (fallback en).

// idExterno lê external_ids[fonte] da entity resolvida.
func idExterno(t *testing.T, ent map[string]any, fonte string) string {
	t.Helper()
	m, ok := ent["external_ids"].(map[string]any)
	if !ok {
		t.Fatalf("external_ids ausente em %v", ent)
	}
	s, _ := m[fonte].(string)
	return s
}

// TestJornadaImportarFilme: importar um filme (tmdb) resolve título/ano/pôster e
// grava a identidade externa; reimportar o mesmo id REUSA (dedup), não duplica.
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

	// Mesmo id em inglês serve o título en (resolução por idioma).
	en := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=en", nil, http.StatusOK)
	if got := str(t, en, "title"); got != "The Matrix" {
		t.Errorf("title(en) = %q, quero \"The Matrix\"", got)
	}

	// Reimportar reusa (200) a MESMA entity.
	reuso := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusOK)
	if str(t, reuso, "id") != id {
		t.Errorf("reimport criou entity nova (%q != %q): deveria deduplicar", str(t, reuso, "id"), id)
	}
}

// TestJornadaImportarColecao: importar uma coleção cria a entity `collection` e
// LIGA os filmes membros, que voltam ordenados por ano no GET /members.
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
	// Ordenados por ano: Matrix (1999) antes de Reloaded (2003).
	primeiro, _ := membros[0].(map[string]any)
	segundo, _ := membros[1].(map[string]any)
	if t0, _ := primeiro["title"].(string); t0 != "Matrix" {
		t.Errorf("1o membro = %q, quero \"Matrix\" (1999 primeiro)", primeiro["title"])
	}
	if t1, _ := segundo["title"].(string); t1 != "Matrix Reloaded" {
		t.Errorf("2o membro = %q, quero \"Matrix Reloaded\" (2003 depois)", segundo["title"])
	}
}

// TestJornadaItemManualSincroniza: criar um item LOCAL sem fonte, marcar uma tag do
// dono, fixar o id externo (tmdb) e RE-ENRIQUECER puxa os dados da fonte pelo id
// fixado — o título neutro cede ao externo e as tags NOVAS da fonte entram, SEM
// apagar a tag do dono. Fecha o ciclo de sincronização do briefing 0027.
func TestJornadaItemManualSincroniza(t *testing.T) {
	c := novoHarness(t)

	item := c.obj(http.MethodPost, "/api/v1/entities",
		map[string]string{"type": "movie", "title": "Meu Filme Local"}, http.StatusCreated)
	id := str(t, item, "id")

	// O dono marca uma tag própria (delta add) no idioma de leitura.
	c.obj(http.MethodPatch, "/api/v1/entities/"+id+"/override?lang=pt-BR",
		map[string]any{"tags": map[string]any{"add": []string{"MinhaTag"}, "remove": []string{}}}, http.StatusOK)

	// Fixa a identidade tmdb e re-enriquece (o enrich lê external_ids[tmdb]).
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

// TestJornadaEnrichNaoDestrutivo: o coração do ADR-0012. Editar o título (override)
// e as tags (delta add/remove) e depois RE-ENRIQUECER: o título editado PERMANECE, a
// tag removida NÃO volta, a tag adicionada fica, e as tags externas intocadas seguem
// vivas por baixo. Codifica a garantia do resolver de 3 camadas.
func TestJornadaEnrichNaoDestrutivo(t *testing.T) {
	c := novoHarness(t)

	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// Edita o título (override vence tudo) e as tags (add Favorito, remove Acao).
	c.obj(http.MethodPatch, "/api/v1/entities/"+id+"/override?lang=pt-BR",
		map[string]any{"title": "Matrix EDITADO"}, http.StatusOK)
	c.obj(http.MethodPatch, "/api/v1/entities/"+id+"/override?lang=pt-BR",
		map[string]any{"tags": map[string]any{"add": []string{"Favorito"}, "remove": []string{"Acao"}}}, http.StatusOK)

	// Re-enriquece a MESMA fonte: nada do que o dono editou pode mudar.
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

// TestJornadaInternoFirstNaBusca: um item já importado aparece na busca externa com
// internal_id apontando ao interno; um não-importado vem com internal_id nulo. É a
// base do interno-first (0026/0027): a UI mostra "na biblioteca" e linka ao interno.
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
