package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// tipos coleta o campo `type` de cada item de uma lista de objetos JSON.
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

// TestJornadaArvoreSeriesTopoBreadcrumb: o briefing 0032. Depois de importar a arvore
// da serie (tv + 2 temporadas + 6 episodios), a HOME e a BUSCA local so mostram OBRAS
// DE TOPO (a serie tv), nunca season/episode; e GET /entities/{id} traz os ANCESTRAIS
// (breadcrumb raiz->pai): episodio -> [serie, temporada]; temporada -> [serie]; serie
// -> [] (sem pai).
func TestJornadaArvoreSeriesTopoBreadcrumb(t *testing.T) {
	c := novoHarness(t)

	serie := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "71663", "type": "tv"}, http.StatusCreated)
	serieID := str(t, serie, "id")

	// (a) HOME/biblioteca: so a serie tv aparece; nada de season/episode.
	home := c.obj(http.MethodGet, "/api/v1/entities", nil, http.StatusOK)
	assertHomeSoSerie(t, home, serieID)

	// (b) BUSCA local DEFAULT: "Episodio" casaria os 6 episodios se indexados; sub-nos
	// ficam de fora por padrao -> 0 (0033 preserva o so-topo do 0032 no default).
	resp := c.obj(http.MethodGet, "/api/v1/search?q=Episodio", nil, http.StatusOK)
	locais, _ := resp["local"].([]any)
	if len(locais) != 0 {
		t.Errorf("busca default por \"Episodio\" trouxe %d locais, quero 0 (sub-nos fora). tipos=%v",
			len(locais), tipos(locais))
	}
	// A serie de topo segue encontravel pela busca.
	respSerie := c.obj(http.MethodGet, "/api/v1/search?q=Contos", nil, http.StatusOK)
	locaisSerie, _ := respSerie["local"].([]any)
	semSubNos(t, locaisSerie)
	if len(locaisSerie) != 1 {
		t.Errorf("busca por \"Contos\" = %v, quero 1 resultado (a serie)", tipos(locaisSerie))
	}

	// (b2) include=episode RELIGA os 6 episodios numa secao propria (0033), todos type
	// episode — sem misturar com a serie (a busca por "Episodio" nao casa "Contos").
	respEp := c.obj(http.MethodGet, "/api/v1/search?q=Episodio&include=episode", nil, http.StatusOK)
	locaisEp, _ := respEp["local"].([]any)
	if len(locaisEp) != 6 {
		t.Errorf("include=episode por \"Episodio\" trouxe %d, quero 6 episodios. tipos=%v",
			len(locaisEp), tipos(locaisEp))
	}
	assertTodosTipo(t, locaisEp, "episode")

	// (b3) include=season religa as temporadas: a temporada 1 ("Temporada 1") casa e
	// vem como sub-no season; tudo que retorna e season (nunca serie/episodio).
	respTemp := c.obj(http.MethodGet, "/api/v1/search?q=Temporada&include=season", nil, http.StatusOK)
	locaisTemp, _ := respTemp["local"].([]any)
	if len(locaisTemp) == 0 {
		t.Errorf("include=season por \"Temporada\" trouxe 0, quero ao menos a temporada 1")
	}
	assertTodosTipo(t, locaisTemp, "season")

	// (c) ANCESTRAIS (breadcrumb raiz->pai).
	// Serie (topo): ancestors vazio (mas presente, nunca null).
	if anc := ancestrais(t, serie); len(anc) != 0 {
		t.Errorf("serie ancestors = %v, quero [] (obra de topo)", anc)
	}

	// Temporada 1: ancestors = [serie].
	temporadas := c.arr(http.MethodGet, "/api/v1/entities/"+serieID+"/members", nil, http.StatusOK)
	tempID := str(t, temporadas[0].(map[string]any), "id")
	tempResolvida := c.obj(http.MethodGet, "/api/v1/entities/"+tempID, nil, http.StatusOK)
	ancTemp := ancestrais(t, tempResolvida)
	if len(ancTemp) != 1 {
		t.Fatalf("temporada ancestors = %d, quero 1 (a serie). %v", len(ancTemp), ancTemp)
	}
	assertAncestor(t, ancTemp[0], serieID, "tv", "temporada->raiz")

	// Episodio 1 da temporada 1: ancestors = [serie, temporada] (raiz->pai).
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

// assertHomeSoSerie afirma que a home lista SO a serie de topo (`serieID`), sem
// nenhum sub-no (season/episode), e que o total da paginacao bate com a pagina.
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

// assertTodosTipo afirma que TODOS os itens da lista tem o type esperado (usado para
// provar que uma secao de sub-no religado por include= nao mistura outros tipos).
func assertTodosTipo(t *testing.T, itens []any, want string) {
	t.Helper()
	for _, tp := range tipos(itens) {
		if tp != want {
			t.Errorf("resultado com type %q, quero so %q", tp, want)
		}
	}
}

// semSubNos afirma que nenhum item da lista e season/episode (so obras de topo).
func semSubNos(t *testing.T, itens []any) {
	t.Helper()
	for _, tp := range tipos(itens) {
		if tp == "season" || tp == "episode" {
			t.Errorf("listou sub-no %q: so obras de topo (movie/tv/collection) sao permitidas", tp)
		}
	}
}

// assertAncestor afirma o id e o type de um no ancestral do breadcrumb.
func assertAncestor(t *testing.T, node map[string]any, wantID, wantType, rotulo string) {
	t.Helper()
	if id := str(t, node, "id"); id != wantID {
		t.Errorf("%s: ancestor id = %q, quero %q", rotulo, id, wantID)
	}
	if tp := str(t, node, "type"); tp != wantType {
		t.Errorf("%s: ancestor type = %q, quero %q", rotulo, tp, wantType)
	}
}

// ancestrais extrai o array `ancestors` de uma entity resolvida como []map (exige
// presenca do campo — nunca null, por contrato do 0032).
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

// escreverVideo cria um arquivo de video de conteudo unico (o proprio caminho), como
// o escrever() do pacote scanner: extensao de video reconhecida (.mkv) e content_hash
// distinto por arquivo. O conteudo nao e um video real; o ffprobe (se presente) falha
// de forma graciosa e o mediainfo fica vazio — o caminho de match nao depende dele.
func escreverVideo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("video-"+path), 0o644); err != nil {
		t.Fatalf("escrever %q: %v", path, err)
	}
}

// esperarScan dispara o scan de um datasource (POST .../scan, 202) e aguarda concluir.
// O scan roda em background (goroutine): o sinal DEFINITIVO de conclusao com sucesso e
// o `last_scan` do datasource, carimbado so em finalizarCicloVida. Faz polling curto.
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

// arquivosDaEntity lista os arquivos de uma entity (GET .../files) como []map. Para
// EPISODIO, vem dos vinculos N:M (media_file_entity, kind='tv_episode') — a prova de
// que o scanner ligou arquivo->episodio pelo id (0034).
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

// posse extrai o objeto `ownership` {owned_count, total_count} de uma entity resolvida
// (temporada/serie, 0034). Exige presenca do campo (nunca ausente para tv/season).
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

// idsDosMembros devolve os ids dos filhos (GET .../members) de uma entity, em ordem.
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

// TestJornadaScanSerie: a jornada ponta-a-ponta do SCANNER DE SERIES (briefing 0034,
// Fase 2). Uma pasta de serie NAO importada, marcada content_type='series', e varrida:
// o scanner auto-importa a arvore do provider (fake, "Springfield Tales"/71663), casa
// cada arquivo ao(s) episodio(s) pelo numero e grava os vinculos N:M ancorados no id.
// Cobre: (a) auto-import; (b) arquivo unico -> 1 vinculo; (c) RANGE S02E01-E03 -> 3
// episodios ligados ao MESMO arquivo; (d) arquivos por episodio via a juncao; (e)
// posse agregada coerente (owned<=total) na temporada e na serie.
func TestJornadaScanSerie(t *testing.T) {
	c := novoHarness(t)

	// Estrutura de disco: pasta da serie, temporada, 2 episodios avulsos na T1 e um
	// arquivo de RANGE cobrindo os 3 episodios da T2 (official S02E01..E03).
	root := t.TempDir()
	base := filepath.Join(root, "Springfield Tales")
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E01.mkv"))
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E02.mkv"))
	escreverVideo(t, filepath.Join(base, "Season 02", "Springfield Tales S02E01-E03.mkv"))

	// (2) Cadastra o datasource marcado como serie e (3) dispara+aguarda o scan.
	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "tv", "content_type": "series"}, http.StatusCreated)
	if got := str(t, src, "content_type"); got != "series" {
		t.Fatalf("content_type = %q, quero \"series\" (persistido no cadastro)", got)
	}
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// (4a) A serie foi AUTO-IMPORTADA: a home lista exatamente 1 obra de topo, a tv.
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

	// Estrutura importada: 2 temporadas; T1 com eps [101,102,103], T2 com [201,202,203].
	temporadas := c.idsDosMembros(serieID)
	if len(temporadas) != 2 {
		t.Fatalf("temporadas = %d, quero 2", len(temporadas))
	}
	epsT1 := c.idsDosMembros(temporadas[0])
	epsT2 := c.idsDosMembros(temporadas[1])
	if len(epsT1) != 3 || len(epsT2) != 3 {
		t.Fatalf("episodios = T1:%d T2:%d, quero 3 e 3", len(epsT1), len(epsT2))
	}

	// (4b) Arquivos avulsos viraram vinculos: ep 101 e 102 possuidos (1 arquivo cada),
	// ep 103 NAO possuido (nenhum arquivo cobriu S01E03).
	assertFileCount(t, c, epsT1[0], 1) // S01E01 -> ep 101
	assertFileCount(t, c, epsT1[1], 1) // S01E02 -> ep 102
	assertFileCount(t, c, epsT1[2], 0) // S01E03 sem arquivo

	// (4c) RANGE: o unico arquivo S02E01-E03 gerou 3 vinculos, um por episodio da T2,
	// TODOS apontando para o MESMO media_file (1 arquivo -> N episodios, ancorado no id).
	assertRangeUmArquivoTresEps(t, c, epsT2, "S02E01-E03")

	// (4d/4e) Posse agregada coerente (owned<=total): T1 possui 2/3; T2 possui 3/3; a
	// serie agrega 5/6 (nunca owned>total).
	assertPosse(t, c, temporadas[0], 2, 3)
	assertPosse(t, c, temporadas[1], 3, 3)
	assertPosse(t, c, serieID, 5, 6)
}

// assertFileCount afirma o file_count de um episodio resolvido (GET /entities/{id}) —
// os arquivos do episodio vem dos vinculos N:M (0034), nao de media_file.entity_id.
func assertFileCount(t *testing.T, c *cliente, epID string, quero int) {
	t.Helper()
	ent := c.obj(http.MethodGet, "/api/v1/entities/"+epID, nil, http.StatusOK)
	fc, _ := ent["file_count"].(float64)
	if int(fc) != quero {
		t.Errorf("ep %s file_count = %v, quero %d", epID, ent["file_count"], quero)
	}
}

// assertRangeUmArquivoTresEps afirma que TODOS os episodios recebem exatamente 1
// arquivo, que seu path contem o marcador do range, e que e o MESMO media_file (id)
// para todos — a prova ponta-a-ponta de 1 arquivo -> N episodios (vinculos N:M).
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

// assertPosse afirma a posse agregada (owned/total) de uma entity-pai (temporada/serie)
// e a coerencia owned<=total (0034).
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

// TestJornadaHealthSystem: GET /api/v1/health/system devolve o snapshot de
// telemetria (banco+uso+armazenamento) refletindo o catálogo real -- prova
// ponta-a-ponta de internal/telemetry + internal/server/health_system.go
// (briefing 0055) contra o MESMO Server real do harness. Também confere que
// a segunda chamada imediata bate no cache TTL (telemetry_cache.go).
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

	// Segunda chamada imediata deve bater no cache TTL (mesmo computed_at).
	again := c.obj(http.MethodGet, "/api/v1/health/system", nil, http.StatusOK)
	if str(t, again, "computed_at") != str(t, body, "computed_at") {
		t.Error("segunda chamada imediata deveria vir do cache (mesmo computed_at)")
	}
}
