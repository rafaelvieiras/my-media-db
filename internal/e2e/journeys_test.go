package e2e

import (
	"net/http"
	"strings"
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

// TestJornadaImportarSerie: importar uma serie (type=tv) cria a arvore
// serie->temporada->episodio (entities tv/season/episode, briefing 0031/ADR-0021) e
// GET /members devolve os filhos ordenados por sort_index — as temporadas na serie e
// os episodios na temporada. Exercita o provider fake como SerieProvider (arvore
// canned), o import da arvore no enrich e a ramificacao de montarMembros por tipo.
func TestJornadaImportarSerie(t *testing.T) {
	c := novoHarness(t)

	// A serie fixture do fake tem id 71663 (2 temporadas de 3 episodios).
	serie := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "71663", "type": "tv"}, http.StatusCreated)
	if got := str(t, serie, "type"); got != "tv" {
		t.Fatalf("type = %q, quero \"tv\"", got)
	}
	serieID := str(t, serie, "id")

	// Temporadas: 2, ordenadas por sort_index (season_number 1 antes de 2).
	temporadas := c.arr(http.MethodGet, "/api/v1/entities/"+serieID+"/members", nil, http.StatusOK)
	if len(temporadas) != 2 {
		t.Fatalf("temporadas = %d, quero 2", len(temporadas))
	}
	t1, _ := temporadas[0].(map[string]any)
	t2, _ := temporadas[1].(map[string]any)
	if tp := str(t, t1, "type"); tp != "season" {
		t.Errorf("1a temporada type = %q, quero \"season\"", tp)
	}
	// Temporada 1 sem Name na fonte -> titulo gerado "Temporada 1" (independe de idioma).
	if got := str(t, t1, "title"); got != "Temporada 1" {
		t.Errorf("1a temporada title = %q, quero \"Temporada 1\"", got)
	}
	if tp := str(t, t2, "type"); tp != "season" {
		t.Errorf("2a temporada type = %q, quero \"season\"", tp)
	}

	// Episodios da 1a temporada: 3, ordenados por numero (official 101,102,103).
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
		// Titulo depende do idioma (pt-BR "Episodio NNN"); asserta o numero, robusto a idioma.
		if got := str(t, m, "title"); !strings.Contains(got, numero) {
			t.Errorf("episodio[%d] title = %q, quero conter %q (ordem por numero)", i, got, numero)
		}
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

// TestJornadaNotaLocal: a NOTA LOCAL do dono (ADR-0012). Importar um filme traz a
// nota da FONTE (vote_average); gravar a nota local via override a expõe em
// local_rating SEM tocar a da fonte; re-enriquecer não a apaga (override congela);
// LIMPAR a nota (DELETE do campo) volta local_rating a null, e a da fonte segue lá.
func TestJornadaNotaLocal(t *testing.T) {
	c := novoHarness(t)

	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// A nota local ainda não existe (o dono não avaliou). O DTO carrega o campo
	// `vote_average` (nota da FONTE), distinto — capturamos seu valor para provar que
	// a nota local não o toca (a nota local é NÃO-DESTRUTIVA, ADR-0012).
	if ent["local_rating"] != nil {
		t.Errorf("local_rating = %v, quero null antes de o dono avaliar", ent["local_rating"])
	}
	if _, temCampo := ent["vote_average"]; !temCampo {
		t.Fatalf("DTO sem o campo vote_average (nota da fonte): %v", ent)
	}
	fonte := ent["vote_average"] // valor da fonte, seja ele qual for (pode ser null)

	// O dono grava a nota local (campo neutro do override).
	c.obj(http.MethodPatch, "/api/v1/entities/"+id+"/override?lang=pt-BR",
		map[string]any{"local_rating": 9.5}, http.StatusOK)

	comNota := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if nl, _ := comNota["local_rating"].(float64); nl != 9.5 {
		t.Errorf("local_rating = %v, quero 9.5 (nota do dono)", comNota["local_rating"])
	}
	// A nota da FONTE não muda ao definir a nota local (campos distintos).
	if comNota["vote_average"] != fonte {
		t.Errorf("vote_average = %v, quero %v (a nota da fonte segue intacta)", comNota["vote_average"], fonte)
	}

	// Re-enriquecer nao apaga a nota do dono (override congela, ADR-0012).
	c.obj(http.MethodPost, "/api/v1/entities/"+id+"/enrich", map[string]any{}, http.StatusOK)
	posEnrich := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if nl, _ := posEnrich["local_rating"].(float64); nl != 9.5 {
		t.Errorf("local_rating pos-enrich = %v, quero 9.5 (override congela)", posEnrich["local_rating"])
	}

	// LIMPAR a nota local (DELETE do campo): volta a null; a da fonte permanece.
	c.obj(http.MethodDelete, "/api/v1/entities/"+id+"/override/local_rating", nil, http.StatusOK)
	limpa := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if limpa["local_rating"] != nil {
		t.Errorf("local_rating apos limpar = %v, quero null (volta a mostrar a da fonte)", limpa["local_rating"])
	}
	if limpa["vote_average"] != fonte {
		t.Errorf("vote_average apos limpar = %v, quero %v", limpa["vote_average"], fonte)
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
