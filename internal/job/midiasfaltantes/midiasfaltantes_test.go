package midiasfaltantes

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/job"
)

// entityListerFake is a fake EntityLister, without touching the database.
type entityListerFake struct {
	ids []string
	err error
}

func (f *entityListerFake) ListarEntitiesComMediaFilePresente(context.Context) ([]string, error) {
	return f.ids, f.err
}

// materializadorFake is a fake Materializador: it returns the configured result
// per entity id, or an error for the ids in errPorID.
type materializadorFake struct {
	artesPorID map[string][]string
	nfoPorID   map[string]bool
	errPorID   map[string]error

	chamadas []string
}

func (f *materializadorFake) MaterializarMidiasFaltantes(_ context.Context, entityID string) ([]string, bool, error) {
	f.chamadas = append(f.chamadas, entityID)
	if err := f.errPorID[entityID]; err != nil {
		return nil, false, err
	}
	return f.artesPorID[entityID], f.nfoPorID[entityID], nil
}

// contadorMissingFake is a fake ContadorMissing, without touching the database.
type contadorMissingFake struct {
	n   int64
	err error
}

func (f *contadorMissingFake) ContarMediaFilesEmEstadoMissing(context.Context) (int64, error) {
	return f.n, f.err
}

func TestNomeEstavel(t *testing.T) {
	h := New(&entityListerFake{}, &materializadorFake{}, &contadorMissingFake{}, nil)
	if h.Nome() != "midias_faltantes" {
		t.Errorf("Nome() = %q, quero \"midias_faltantes\"", h.Nome())
	}
}

// TestExecutarProcessaTodasEResume covers the happy path: entities with art
// resolved and no NFO get both things, the summary reflects the totals.
func TestExecutarProcessaTodasEResume(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1", "e2", "e3"}}
	mat := &materializadorFake{
		artesPorID: map[string][]string{
			"e1": {"poster", "fanart"},
			"e2": {}, // sem candidato pra nenhum kind -- não é erro
			"e3": {"poster"},
		},
		nfoPorID: map[string]bool{"e1": true, "e3": false},
		errPorID: map[string]error{},
	}
	h := New(entities, mat, &contadorMissingFake{}, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if len(mat.chamadas) != 3 {
		t.Fatalf("materializador chamado %d vez(es), quero 3", len(mat.chamadas))
	}
	for _, want := range []string{"3 entity", "3 processada", "3 arte(s) materializada", "1 NFO(s) gravado"} {
		if !strings.Contains(msg, want) {
			t.Errorf("mensagem %q não contém %q", msg, want)
		}
	}
}

// TestExecutarErroDeUmaEntityNaoAbortaAsDemais: a failure on ONE entity
// (network/disk) does not prevent the others from being processed --
// best-effort.
func TestExecutarErroDeUmaEntityNaoAbortaAsDemais(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1", "e2"}}
	mat := &materializadorFake{
		artesPorID: map[string][]string{"e2": {"poster"}},
		nfoPorID:   map[string]bool{},
		errPorID:   map[string]error{"e1": errors.New("falha de rede")},
	}
	h := New(entities, mat, &contadorMissingFake{}, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa de UMA entity: %v", err)
	}
	if len(mat.chamadas) != 2 {
		t.Fatalf("materializador chamado %d vez(es), quero 2 (ambas tentadas)", len(mat.chamadas))
	}
	if !strings.Contains(msg, "1 falha") {
		t.Errorf("mensagem %q deveria reportar 1 falha", msg)
	}
	if !strings.Contains(msg, "1 processada") {
		t.Errorf("mensagem %q deveria reportar 1 processada com sucesso", msg)
	}
}

// TestExecutarSemEntitiesElegiveis: an empty universe is not an error.
func TestExecutarSemEntitiesElegiveis(t *testing.T) {
	h := New(&entityListerFake{ids: nil}, &materializadorFake{}, &contadorMissingFake{}, nil)
	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if !strings.Contains(msg, "0 entity") {
		t.Errorf("mensagem %q deveria reportar 0 entities elegíveis", msg)
	}
}

// TestExecutarErroDeListagemPropaga: a failure to list the eligible universe
// (database down) propagates the error -- unlike a per-entity failure.
func TestExecutarErroDeListagemPropaga(t *testing.T) {
	entities := &entityListerFake{err: errors.New("banco indisponível")}
	h := New(entities, &materializadorFake{}, &contadorMissingFake{}, nil)
	if _, err := h.Executar(context.Background(), nil); err == nil {
		t.Fatal("Executar deveria propagar o erro de listagem")
	}
}

// TestExecutarResumoIncluiMissingQuandoMaiorQueZero covers the 'missing'
// media_file case: when there are 'missing' media_file at scan time, the summary
// appends the count -- a sign that the eligible universe of this execution may
// have been left incomplete by a race with another job over the same mount.
func TestExecutarResumoIncluiMissingQuandoMaiorQueZero(t *testing.T) {
	h := New(&entityListerFake{}, &materializadorFake{}, &contadorMissingFake{n: 3}, nil)
	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if !strings.Contains(msg, "3 media_file(s) em estado 'missing'") {
		t.Errorf("mensagem %q deveria conter a contagem de missing", msg)
	}
}

// TestExecutarResumoOmiteMissingQuandoZero covers the counterpart: with no
// 'missing' media_file, the summary does not gain extra noise.
func TestExecutarResumoOmiteMissingQuandoZero(t *testing.T) {
	h := New(&entityListerFake{}, &materializadorFake{}, &contadorMissingFake{n: 0}, nil)
	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if strings.Contains(msg, "missing") {
		t.Errorf("mensagem %q não deveria mencionar missing quando a contagem é 0", msg)
	}
}

// TestExecutarResumoOmiteMissingQuandoContagemFalha covers that a best-effort
// failure in the count (database unavailable in the middle of the execution)
// neither aborts the job nor propagates an error -- it only omits the datum from
// the summary.
func TestExecutarResumoOmiteMissingQuandoContagemFalha(t *testing.T) {
	h := New(&entityListerFake{}, &materializadorFake{}, &contadorMissingFake{err: errors.New("banco indisponível")}, nil)
	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa da contagem de missing: %v", err)
	}
	if strings.Contains(msg, "missing") {
		t.Errorf("mensagem %q não deveria mencionar missing quando a contagem falhou", msg)
	}
}

// tituladorFake is a fake Titulador: the configured title per id, or an error
// for the ids in errPorID (the narration must fall back to the id then).
type tituladorFake struct {
	tituloPorID map[string]string
	errPorID    map[string]error
}

func (f *tituladorFake) TituloDaEntity(_ context.Context, entityID string) (string, error) {
	if err := f.errPorID[entityID]; err != nil {
		return "", err
	}
	return f.tituloPorID[entityID], nil
}

// TestExecutarComProgressoNarraContagemEItem covers issue #69: the universe is
// known before the loop, so `total` is honest from the first snapshot, `done`
// grows to the end, the current item carries the entity TITLE (not the uuid) and
// every outcome lands in the trail (ok | skip | error).
func TestExecutarComProgressoNarraContagemEItem(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1", "e2", "e3"}}
	mat := &materializadorFake{
		artesPorID: map[string][]string{"e1": {"poster"}, "e3": {"fanart", "logo"}},
		nfoPorID:   map[string]bool{"e3": true},
		errPorID:   map[string]error{},
	}
	titulos := &tituladorFake{tituloPorID: map[string]string{"e1": "Mortal Kombat (1995)", "e2": "A Jornada Começa", "e3": "Duran Duran"}}
	h := New(entities, mat, &contadorMissingFake{}, titulos)
	registro := execqueue.NovoRegistroGravador()

	msg, err := h.ExecutarComProgresso(context.Background(), nil, job.NovoProgresso(registro))
	if err != nil {
		t.Fatalf("ExecutarComProgresso: %v", err)
	}
	if !strings.Contains(msg, "3 processada") {
		t.Errorf("mensagem %q deveria resumir as 3 processadas", msg)
	}

	progressos := registro.Progressos()
	if len(progressos) == 0 {
		t.Fatal("nenhum progresso reportado")
	}
	// Cada snapshot é um DELTA (campo zero = "não mudou"), que é como o registro
	// real faz o merge na linha da fila; aqui dobramos os deltas para ler o
	// estado acumulado, que é o que o dono vê na tela.
	estado := execqueue.Progresso{}
	dobrados := make([]execqueue.Progresso, 0, len(progressos))
	for _, p := range progressos {
		if p.Etapa != "" || p.EtapaTotal != 0 {
			estado.Etapa, estado.EtapaIndice, estado.EtapaTotal = p.Etapa, p.EtapaIndice, p.EtapaTotal
		}
		if p.Total != 0 || p.Atual != 0 {
			estado.Atual, estado.Total = p.Atual, p.Total
		}
		if p.Item != "" {
			estado.Item = p.Item
		}
		dobrados = append(dobrados, estado)
	}
	// O total tem que ser o universo ANTES de processar qualquer entity (é o
	// defeito da #69): o snapshot da etapa ainda não traz contagem, o seguinte
	// já traz "0 de 3".
	primeiroComTotal := -1
	for i, p := range dobrados {
		if p.Total != 0 {
			primeiroComTotal = i
			break
		}
	}
	if primeiroComTotal != 1 {
		t.Fatalf("primeiro snapshot com total = índice %d, quero o segundo (logo após anunciar a etapa)", primeiroComTotal)
	}
	if dobrados[primeiroComTotal].Total != 3 || dobrados[primeiroComTotal].Atual != 0 {
		t.Errorf("contagem inicial = %d/%d, quero 0/3", dobrados[primeiroComTotal].Atual, dobrados[primeiroComTotal].Total)
	}
	ultimo := dobrados[len(dobrados)-1]
	if ultimo.Atual != 3 || ultimo.Total != 3 {
		t.Errorf("contagem final = %d/%d, quero 3/3", ultimo.Atual, ultimo.Total)
	}
	// Contagem monotônica: o estado acumulado nunca anda para trás.
	for i := 1; i < len(dobrados); i++ {
		if dobrados[i].Atual < dobrados[i-1].Atual {
			t.Fatalf("contagem não-monotônica em %d: %d -> %d", i, dobrados[i-1].Atual, dobrados[i].Atual)
		}
	}
	// O item corrente é o TÍTULO, não o uuid (o último item narrado é o e3).
	if ultimo.Item != "Duran Duran" {
		t.Errorf("último item = %q, quero o título \"Duran Duran\"", ultimo.Item)
	}
	itens := map[string]bool{}
	for _, p := range progressos {
		if p.Item != "" {
			itens[p.Item] = true
		}
	}
	if !itens["Mortal Kombat (1995)"] {
		t.Errorf("itens narrados = %v, quero o título \"Mortal Kombat (1995)\"", itens)
	}
	if itens["e1"] {
		t.Error("o uuid não deveria aparecer como item quando o título está disponível")
	}

	// Trilha: uma linha por entity COM trabalho, mais UM resumo das que não tinham
	// nada (issue #73: 19.000 linhas idênticas de "nada faltando" custavam mais que o
	// próprio trabalho, e diziam o mesmo que um número).
	eventos := registro.Eventos()
	if len(eventos) != 3 {
		t.Fatalf("eventos = %d, quero 3 (2 com trabalho + 1 resumo dos que não tinham)", len(eventos))
	}
	porRotulo := map[string]execqueue.Evento{}
	var resumo *execqueue.Evento
	for i, e := range eventos {
		porRotulo[e.Label] = e
		if e.Status == execqueue.EventoSkip {
			resumo = &eventos[i]
		}
	}
	if e := porRotulo["Mortal Kombat (1995)"]; e.Status != execqueue.EventoOK || !strings.Contains(e.Detail, "poster") {
		t.Errorf("evento de e1 = %+v, quero ok com o detalhe das artes", e)
	}
	if e := porRotulo["Duran Duran"]; e.Status != execqueue.EventoOK || !strings.Contains(e.Detail, "nfo") {
		t.Errorf("evento de e3 = %+v, quero ok mencionando o nfo", e)
	}
	if _, existe := porRotulo["A Jornada Começa"]; existe {
		t.Error("a entity sem nada faltando não deveria ter linha própria na trilha (só o resumo)")
	}
	if resumo == nil {
		t.Fatal("faltou o resumo das entities sem nada faltando")
	}
	if resumo.Label != "1 entity(ies)" || !strings.Contains(resumo.Detail, "nada faltando") {
		t.Errorf("resumo = %+v, quero \"1 entity(ies)\" com \"nada faltando no disco\"", *resumo)
	}
}

// TestExecutarComProgressoContaFalhaNaTrilha: a failing entity is counted in the
// snapshot counters and lands in the trail as an error, with the message -- the
// 90 failures of a real run must be visible, not only in the final summary.
func TestExecutarComProgressoContaFalhaNaTrilha(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1", "e2"}}
	mat := &materializadorFake{
		artesPorID: map[string][]string{"e2": {"poster"}},
		nfoPorID:   map[string]bool{},
		errPorID:   map[string]error{"e1": errors.New("permission denied")},
	}
	h := New(entities, mat, &contadorMissingFake{}, nil)
	registro := execqueue.NovoRegistroGravador()

	if _, err := h.ExecutarComProgresso(context.Background(), nil, job.NovoProgresso(registro)); err != nil {
		t.Fatalf("ExecutarComProgresso não deveria falhar por causa de UMA entity: %v", err)
	}
	// Sem titulador, o rótulo é o id -- e a trilha continua correta.
	eventos := registro.Eventos()
	if len(eventos) != 2 {
		t.Fatalf("eventos = %d, quero 2", len(eventos))
	}
	if eventos[0].Status != execqueue.EventoErro || !strings.Contains(eventos[0].Detail, "permission denied") {
		t.Errorf("evento de falha = %+v, quero erro com a mensagem", eventos[0])
	}
	// O último snapshot com contagem é o 2/2 do fim do loop (os anteriores são
	// deltas de Item, com contagem zero).
	var atual, total int
	for _, p := range registro.Progressos() {
		if p.Total != 0 || p.Atual != 0 {
			atual, total = p.Atual, p.Total
		}
	}
	if atual != 2 || total != 2 {
		t.Errorf("contagem final = %d/%d, quero 2/2", atual, total)
	}
}

// TestExecutarComProgressoTituloIndisponivelUsaID: the title lookup is
// narration-only -- when it fails, the label falls back to the id and nothing
// else changes.
func TestExecutarComProgressoTituloIndisponivelUsaID(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1"}}
	mat := &materializadorFake{
		artesPorID: map[string][]string{"e1": {"poster"}},
		nfoPorID:   map[string]bool{},
		errPorID:   map[string]error{},
	}
	titulos := &tituladorFake{errPorID: map[string]error{"e1": errors.New("banco indisponível")}}
	h := New(entities, mat, &contadorMissingFake{}, titulos)
	registro := execqueue.NovoRegistroGravador()

	msg, err := h.ExecutarComProgresso(context.Background(), nil, job.NovoProgresso(registro))
	if err != nil {
		t.Fatalf("um erro de título não pode falhar o job: %v", err)
	}
	if !strings.Contains(msg, "1 arte(s) materializada") {
		t.Errorf("mensagem %q deveria seguir contando as artes", msg)
	}
	itens := map[string]bool{}
	for _, p := range registro.Progressos() {
		if p.Item != "" {
			itens[p.Item] = true
		}
	}
	if !itens["e1"] {
		t.Errorf("itens = %v, quero o id como fallback", itens)
	}
}
