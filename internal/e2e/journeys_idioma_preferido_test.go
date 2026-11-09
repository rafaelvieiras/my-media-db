package e2e

// journeys_idioma_preferido_test.go cobre o briefing 0084: idioma preferido
// OPCIONAL por library_source (busca E exibição, §4) e a pontuação de candidato
// contra títulos alternativos/traduzidos (achado colateral #2, §1). Três jornadas:
//
//   - TestJornadaIdiomaPreferidoBibliotecaNaBusca: uma biblioteca com
//     preferred_language definido faz o idioma chegar ao PROVIDER na busca
//     (match-preview) -- e uma sem preferência continua caindo no idioma global
//     (primary), nunca mais o lang="" cego de antes deste briefing.
//   - TestJornadaIdiomaPreferidoBibliotecaNaExibicao: GET /entities/{id} SEM
//     ?lang= usa o idioma preferido da biblioteca de origem como default de
//     EXIBIÇÃO; ?lang= explícito continua vencendo sempre.
//   - TestJornadaTituloAlternativoVenceNoScore: um candidato externo cujo título
//     ALTERNATIVO bate muito melhor que o principal (caso "The Pink Panther" do
//     briefing §1) faz matched_title sair preenchido na prévia de match; um
//     candidato cujo título principal já é o melhor não gera esse ruído.

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// acharRevisaoMovie devolve o id do (único) item type=movie na fila de revisão
// (GET /api/v1/review) cujo título bate exatamente com `titulo` -- o rascunho que o
// scanner criou por não ter id externo embutido no arquivo. t.Fatalf se não achar.
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

// acharMoviePorTitulo acha o id de um movie na LISTA de entities (não na fila de
// revisão) por qualquer dos títulos aceitos. Desde o briefing 0135/ADR-0031 o
// auto-match roda de verdade no scan, então o item que antes ficava em rascunho
// já sai RESOLVIDO (com o título no idioma principal) -- a jornada não pode mais
// depender de encontrá-lo na fila.
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

// ultimoIdioma devolve o ÚLTIMO idioma registrado em c.prov.IdiomasBuscados()
// (a chamada Buscar mais recente) -- usado para provar qual idioma efetivamente
// chegou ao provider numa jornada com múltiplas buscas em sequência.
func ultimoIdioma(t *testing.T, c *cliente) string {
	t.Helper()
	langs := c.prov.IdiomasBuscados()
	if len(langs) == 0 {
		t.Fatalf("nenhuma busca registrada no provider fake")
	}
	return langs[len(langs)-1]
}

// TestJornadaIdiomaPreferidoBibliotecaNaBusca: o achado colateral #1 do briefing
// 0084 -- automatch/retroativo/match-preview chamavam buscarExterno com lang=""
// (nem o idioma global era respeitado). Agora s.idiomaDeBusca resolve por item:
// uma biblioteca com preferred_language="en" faz o idioma "en" chegar ao provider
// na prévia de match, mesmo com o idioma global (primary) em "pt-BR" (default);
// uma biblioteca SEM preferência continua caindo no idioma global, nunca mais em
// "" cego -- os dois comportamentos provados no MESMO harness, via
// fake.Provider.IdiomasBuscados (efeito só visível ali; nenhum campo do corpo
// HTTP de match-preview expõe o idioma da consulta).
func TestJornadaIdiomaPreferidoBibliotecaNaBusca(t *testing.T) {
	c := novoHarness(t)

	// Biblioteca 1: preferred_language="en" explícito. Título SEM fixture no
	// catálogo fake -- o item fica em rascunho (é o que a prévia do rascunho
	// exercita aqui; com fixture, o auto-match do próprio scan já o resolveria).
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

	// Biblioteca 2: SEM preferred_language -- deve cair no idioma global (primary
	// "pt-BR", default de app_settings), NUNCA mais em "" cego (briefing 0084 §1).
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

// TestJornadaIdiomaPreferidoBibliotecaNaExibicao: com um item resolvido cuja
// biblioteca de origem tem preferred_language="en", GET /entities/{id} SEM ?lang=
// devolve o título no idioma da BIBLIOTECA (en), não no idioma global (pt-BR,
// default) -- e ?lang= explícito continua vencendo sempre, em qualquer item.
func TestJornadaIdiomaPreferidoBibliotecaNaExibicao(t *testing.T) {
	c := novoHarness(t)

	root := t.TempDir()
	escreverVideo(t, filepath.Join(root, "Matrix.mkv"))
	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "movie", "preferred_language": "en"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// Desde o 0135 o auto-match roda no próprio scan: a entity já sai identificada
	// com a fixture 603 ("The Matrix"/"Matrix"), multi-idioma, sem clique nenhum.
	id := acharMoviePorTitulo(t, c, "Matrix", "The Matrix")

	// SEM ?lang=: cai no idioma preferido da biblioteca (en) -- "The Matrix", não
	// o "Matrix" do idioma global (pt-BR).
	semLang := c.obj(http.MethodGet, "/api/v1/entities/"+id, nil, http.StatusOK)
	if got := str(t, semLang, "title"); got != "The Matrix" {
		t.Errorf("title sem ?lang= = %q, quero \"The Matrix\" (idioma preferido da biblioteca, en)", got)
	}

	// ?lang= explícito SEMPRE vence, mesmo sobre a preferência da biblioteca.
	comLang := c.obj(http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil, http.StatusOK)
	if got := str(t, comLang, "title"); got != "Matrix" {
		t.Errorf("title com ?lang=pt-BR = %q, quero \"Matrix\" (override explicito vence)", got)
	}
}

// TestJornadaTituloAlternativoVenceNoScore: o achado colateral #2 do briefing
// 0084 -- quando o título retornado como PRINCIPAL do candidato (sempre "en" no
// fake, tituloPreferido) não bate com o arquivo, mas um título ALTERNATIVO que o
// provider só entrega via BuscarTitulosAlternativos bate muito melhor, o score
// final usa o alternativo (matching.PontuarComAlternativos) e a prévia de match
// expõe QUAL título venceu em matched_title -- a fixture "605" do fake
// ("A Pantera Cor-De-Rosa", ver internal/provider/fake/catalogo.go) é a prova
// disso, análoga ao caso citado no §1 do briefing. Em contraste, quando o
// candidato encontrado (Matrix, 603) já tem o título principal como melhor
// match, matched_title sai vazio -- nenhum ruído quando não precisa.
func TestJornadaTituloAlternativoVenceNoScore(t *testing.T) {
	c := novoHarness(t)

	// (a) Arquivo nomeado EXATAMENTE como o titulo alternativo da fixture "605" --
	// o titulo PRINCIPAL dela ("The Rose Prowler"/"O Perseguidor Rosa") é bem
	// diferente e pontuaria baixo sozinho.
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

	// (b) Contraste: arquivo nomeado como o titulo PRINCIPAL de um candidato (a
	// Matrix, 603) -- o principal ja e o melhor, matched_title deve sair vazio
	// mesmo com titulos alternativos disponiveis e consultados (fixture 603 tem
	// "Matrix" como alternativo, mas nao bate melhor que o "The Matrix" exato).
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
