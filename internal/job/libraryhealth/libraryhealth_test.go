package libraryhealth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// libraryFake e um LibraryLister fake, sem tocar filesystem.
type libraryFake struct {
	sources []library.Source
	err     error
}

func (f *libraryFake) Listar(context.Context) ([]library.Source, error) {
	return f.sources, f.err
}

// scanFake e um ScanRunner fake: cada Start() "termina" apos uma pequena
// espera assincrona (goroutine), simulando o scanner de verdade rodando em
// background e reportando via Status(). startErr[sourceID] forca Start() a
// falhar para aquela fonte; runErr[sourceID] forca o scan a terminar com
// erro (LastError) em vez de sucesso.
type scanFake struct {
	startErr map[string]error
	runErr   map[string]error
	duracao  time.Duration

	mu     sync.Mutex
	status map[string]scanner.Status
}

func newScanFake() *scanFake {
	return &scanFake{
		startErr: map[string]error{},
		runErr:   map[string]error{},
		status:   map[string]scanner.Status{},
	}
}

func (f *scanFake) Start(source library.Source) (string, error) {
	if err := f.startErr[source.ID]; err != nil {
		return "", err
	}
	f.mu.Lock()
	f.status[source.ID] = scanner.Status{Existe: true, Running: true}
	f.mu.Unlock()

	go func() {
		if f.duracao > 0 {
			time.Sleep(f.duracao)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		st := scanner.Status{Existe: true, Running: false}
		if err := f.runErr[source.ID]; err != nil {
			st.LastError = err.Error()
		}
		f.status[source.ID] = st
	}()
	return "job-" + source.ID, nil
}

func (f *scanFake) Status(sourceID string) scanner.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status[sourceID]
}

// contadorFake e um Contador fake com valores fixos (ou erro).
type contadorFake struct {
	revisao, orfas int
	err            error
}

func (c *contadorFake) ContarRevisao(context.Context) (int, error) { return c.revisao, c.err }
func (c *contadorFake) ContarOrfas(context.Context) (int, error)   { return c.orfas, c.err }

// matcherFake e um Matcher fake (briefing 0054/0056): valores fixos de
// resolved/pending (auto-match) e aplicados/sinalizados (enriquecimento
// retroativo), ou erro para exercitar o caminho best-effort do job. Os dois
// metodos tem contadores de chamada separados (chamadasAutoMatch/
// chamadasRetroativo) para os testes provarem que cada um roda EXATAMENTE
// uma vez por execucao.
type matcherFake struct {
	resolved, pending int
	err               error
	chamadasAutoMatch int

	aplicados, sinalizados int
	errRetroativo          error
	chamadasRetroativo     int
}

func (m *matcherFake) AutoMatchElegiveis(context.Context) (int, int, error) {
	m.chamadasAutoMatch++
	return m.resolved, m.pending, m.err
}

func (m *matcherFake) EnriquecerRetroativo(context.Context) (int, int, error) {
	m.chamadasRetroativo++
	return m.aplicados, m.sinalizados, m.errRetroativo
}

// completenessFake e um CompletenessRecalculator fake (briefing 0058):
// valores fixos de avaliados/incompletos, ou erro para exercitar o caminho
// best-effort do job. chamadas conta quantas vezes o metodo rodou, para os
// testes provarem que roda EXATAMENTE uma vez por execucao.
type completenessFake struct {
	avaliados, incompletos int
	err                    error
	chamadas               int
}

func (c *completenessFake) RecalcularCompletudeCatalogacao(context.Context) (int, int, error) {
	c.chamadas++
	return c.avaliados, c.incompletos, c.err
}

// contadorSequencialFake devolve um valor de ContarRevisao por chamada, na
// ordem de revisaoValores (a última chamada extra repete o último valor) --
// usado para provar que o job RECONTA a fila DEPOIS do auto-match (briefing
// 0054), refletindo a queda de itens (contadorFake, com valor fixo, não
// consegue distinguir "recontou" de "não recontou").
type contadorSequencialFake struct {
	revisaoValores []int
	orfas          int
	chamada        int
}

func (c *contadorSequencialFake) ContarRevisao(context.Context) (int, error) {
	v := c.revisaoValores[c.chamada]
	if c.chamada < len(c.revisaoValores)-1 {
		c.chamada++
	}
	return v, nil
}

func (c *contadorSequencialFake) ContarOrfas(context.Context) (int, error) { return c.orfas, nil }

func acelerarPoll(t *testing.T) {
	t.Helper()
	original := pollInterval
	pollInterval = 5 * time.Millisecond
	t.Cleanup(func() { pollInterval = original })
}

func TestNome(t *testing.T) {
	h := New(&libraryFake{}, newScanFake(), &contadorFake{}, &matcherFake{}, nil)
	if h.Nome() != "saude_biblioteca" {
		t.Fatalf("Nome() = %q, quero saude_biblioteca", h.Nome())
	}
}

// TestExecutarSemFontes prova que zero library_source cadastradas nao e erro
// -- so uma mensagem informativa + as contagens atuais.
func TestExecutarSemFontes(t *testing.T) {
	h := New(&libraryFake{}, newScanFake(), &contadorFake{revisao: 3, orfas: 2}, &matcherFake{}, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 3 em revisão, 2 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarTodasSucesso prova o caminho feliz: dispara o scan de cada
// fonte, ESPERA cada uma terminar antes de contar as filas (a asserção real
// é indireta -- se o Handler não esperasse, o teste seria não-determinístico
// e falharia esporadicamente sob -race/-count=10).
func TestExecutarTodasSucesso(t *testing.T) {
	acelerarPoll(t)
	scn := newScanFake()
	scn.duracao = 30 * time.Millisecond
	lib := &libraryFake{sources: []library.Source{
		{ID: "s1", Path: "/lib/um"},
		{ID: "s2", Path: "/lib/dois"},
	}}
	cont := &contadorFake{revisao: 5, orfas: 1}
	h := New(lib, scn, cont, &matcherFake{}, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "2 fonte(s) escaneada(s) com sucesso; 5 em revisão, 1 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarFalhaParcial prova que uma fonte offline (Start falha) nao
// aborta a outra, e a mensagem final reporta a falha parcial.
func TestExecutarFalhaParcial(t *testing.T) {
	acelerarPoll(t)
	scn := newScanFake()
	scn.startErr["s-offline"] = errors.New("datasource inacessível (offline)")
	lib := &libraryFake{sources: []library.Source{
		{ID: "s-ok", Path: "/lib/ok"},
		{ID: "s-offline", Path: "/lib/offline"},
	}}
	cont := &contadorFake{revisao: 0, orfas: 0}
	h := New(lib, scn, cont, &matcherFake{}, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar com sucesso parcial: %v", err)
	}
	quero := "1 de 2 fonte(s) escaneada(s) com sucesso (1 falharam: /lib/offline: datasource inacessível (offline)); 0 em revisão, 0 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarErroDeScanNoMeio prova que um scan que INICIA mas termina com
// erro (LastError) também conta como falha daquela fonte, sem abortar as
// outras.
func TestExecutarErroDeScanNoMeio(t *testing.T) {
	acelerarPoll(t)
	scn := newScanFake()
	scn.runErr["s-quebra"] = errors.New("permissão negada")
	lib := &libraryFake{sources: []library.Source{
		{ID: "s-ok", Path: "/lib/ok"},
		{ID: "s-quebra", Path: "/lib/quebra"},
	}}
	h := New(lib, scn, &contadorFake{}, &matcherFake{}, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if msg == "" {
		t.Fatal("mensagem vazia")
	}
	quero := "1 de 2 fonte(s) escaneada(s) com sucesso (1 falharam: /lib/quebra: permissão negada); 0 em revisão, 0 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarTodasFalham prova o critério: se TODAS as fontes falharem, o
// job devolve ERRO (não sucesso mentiroso).
func TestExecutarTodasFalham(t *testing.T) {
	acelerarPoll(t)
	scn := newScanFake()
	scn.startErr["s1"] = errors.New("offline")
	scn.startErr["s2"] = errors.New("offline")
	lib := &libraryFake{sources: []library.Source{
		{ID: "s1", Path: "/lib/um"},
		{ID: "s2", Path: "/lib/dois"},
	}}
	h := New(lib, scn, &contadorFake{}, &matcherFake{}, nil)

	_, err := h.Executar(context.Background(), nil)
	if err == nil {
		t.Fatal("Executar deveria devolver erro quando TODAS as fontes falham")
	}
}

// TestExecutarErroListarFontes prova que erro ao listar as fontes aborta
// imediatamente (nada para escanear).
func TestExecutarErroListarFontes(t *testing.T) {
	lib := &libraryFake{err: errors.New("banco indisponível")}
	h := New(lib, newScanFake(), &contadorFake{}, &matcherFake{}, nil)

	if _, err := h.Executar(context.Background(), nil); err == nil {
		t.Fatal("Executar deveria propagar o erro de listar fontes")
	}
}

// TestExecutarAguardaAntesDeContar e o teste mais importante do briefing
// 0053: prova que o Handler REALMENTE espera o scan terminar (via polling de
// Status) antes de contar as filas -- se ele não esperasse, a contagem abaixo
// seria capturada ANTES do scanFake marcar running=false, mas como o duracao
// é maior que o tempo de uma chamada síncrona, qualquer implementação que não
// espere passaria por sorte; repetir com -count=10 no CI pega regressão.
func TestExecutarAguardaAntesDeContar(t *testing.T) {
	acelerarPoll(t)
	scn := newScanFake()
	scn.duracao = 50 * time.Millisecond
	lib := &libraryFake{sources: []library.Source{{ID: "s1", Path: "/lib/um"}}}
	h := New(lib, scn, &contadorFake{}, &matcherFake{}, nil)

	inicio := time.Now()
	if _, err := h.Executar(context.Background(), nil); err != nil {
		t.Fatalf("Executar: %v", err)
	}
	decorrido := time.Since(inicio)
	if decorrido < scn.duracao {
		t.Fatalf("Executar retornou em %s, antes da duração simulada do scan (%s) -- não esperou o scan terminar", decorrido, scn.duracao)
	}
}

// TestExecutarContagemFalha prova que erro ao contar as filas não vira erro
// do job (o scan em si funcionou) -- a mensagem reporta o problema.
func TestExecutarContagemFalha(t *testing.T) {
	h := New(&libraryFake{}, newScanFake(), &contadorFake{err: fmt.Errorf("banco indisponível")}, &matcherFake{}, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar: %v", err)
	}
	if msg == "" {
		t.Fatal("mensagem vazia")
	}
}

// TestExecutarAcionaAutoMatchEReflete cobre o critério central do briefing
// 0054: depois de contar revisão/órfãs, o job aciona o Matcher e a mensagem
// final reflete "<M> resolvidos automaticamente" -- com a contagem de revisão
// RECONTADA após o auto-match (3, não os 5 de antes de rodar), provando que o
// job não usa a contagem obsoleta.
func TestExecutarAcionaAutoMatchEReflete(t *testing.T) {
	cont := &contadorSequencialFake{revisaoValores: []int{5, 3}, orfas: 1}
	matcher := &matcherFake{resolved: 2, pending: 3}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 3 em revisão (2 resolvidos automaticamente), 1 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
	if matcher.chamadasAutoMatch != 1 {
		t.Errorf("matcher chamado %d vez(es), quero exatamente 1", matcher.chamadasAutoMatch)
	}
}

// TestExecutarFalhaDoAutoMatchNaoAbortaJob cobre o critério best-effort: uma
// falha do Matcher não derruba o job -- o resumo de scan/contagem original
// (sem "resolvidos automaticamente", já que nada foi resolvido) ainda sai.
func TestExecutarFalhaDoAutoMatchNaoAbortaJob(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	matcher := &matcherFake{err: errors.New("erro simulado no auto-match")}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa do auto-match (best-effort): %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarAcionaEnriquecimentoRetroativoEReflete cobre o critério central
// do briefing 0056: depois do auto-match em lote, o job aciona
// EnriquecerRetroativo e a mensagem final ganha a cláusula "enriquecimento
// retroativo: <A> aplicado(s), <S> sinalizado(s) para revisão" -- com a
// contagem de revisão RECONTADA de novo após este passo (3, não os 5 de
// antes), provando que o job reflete o estado FINAL (itens sinalizados
// entraram na fila).
func TestExecutarAcionaEnriquecimentoRetroativoEReflete(t *testing.T) {
	cont := &contadorSequencialFake{revisaoValores: []int{5, 3}, orfas: 1}
	matcher := &matcherFake{aplicados: 3, sinalizados: 2}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 3 em revisão, 1 órfã(s); enriquecimento retroativo: 3 aplicado(s), 2 sinalizado(s) para revisão"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
	if matcher.chamadasRetroativo != 1 {
		t.Errorf("EnriquecerRetroativo chamado %d vez(es), quero exatamente 1", matcher.chamadasRetroativo)
	}
}

// TestExecutarEnriquecimentoRetroativoOmiteClausulaQuandoZero cobre o
// critério de omissão: sem nada aplicado nem sinalizado, a mensagem final
// NÃO ganha a cláusula extra (mesmo espírito da omissão de "resolvidos
// automaticamente" quando resolvidos==0).
func TestExecutarEnriquecimentoRetroativoOmiteClausulaQuandoZero(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	matcher := &matcherFake{}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarFalhaDoEnriquecimentoRetroativoNaoAbortaJob cobre o critério
// best-effort: uma falha do Matcher no passo retroativo não derruba o job --
// o resumo original (sem a cláusula extra) ainda sai.
func TestExecutarFalhaDoEnriquecimentoRetroativoNaoAbortaJob(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	matcher := &matcherFake{errRetroativo: errors.New("erro simulado no enriquecimento retroativo")}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa do enriquecimento retroativo (best-effort): %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarSemMatcherNaoRodaEnriquecimentoRetroativo cobre a guarda
// matcher==nil (mesma guarda de rodarAutoMatch): sem Matcher injetado, o
// passo retroativo degrada para (0, 0) sem panicar.
func TestExecutarSemMatcherNaoRodaEnriquecimentoRetroativo(t *testing.T) {
	h := New(&libraryFake{}, newScanFake(), &contadorFake{revisao: 1, orfas: 0}, nil, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 1 em revisão, 0 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarAcionaRecalculoCompletudeEReflete cobre o critério central do
// briefing 0058: depois do enriquecimento retroativo, o job aciona
// RecalcularCompletudeCatalogacao e a mensagem final ganha a cláusula
// "<N> itens avaliados, <M> incompletos em catalogação".
func TestExecutarAcionaRecalculoCompletudeEReflete(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	completeness := &completenessFake{avaliados: 10, incompletos: 3}
	h := New(&libraryFake{}, newScanFake(), cont, &matcherFake{}, completeness)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s); 10 itens avaliados, 3 incompletos em catalogação"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
	if completeness.chamadas != 1 {
		t.Errorf("RecalcularCompletudeCatalogacao chamado %d vez(es), quero exatamente 1", completeness.chamadas)
	}
}

// TestExecutarRecalculoCompletudeClausulaComZeroIncompletos prova que a
// cláusula aparece mesmo com incompletos==0, desde que avaliados>0 (0
// incompletos é informação positiva relevante — diferente de
// aplicados/sinalizados==0, que só significa "nada a fazer").
func TestExecutarRecalculoCompletudeClausulaComZeroIncompletos(t *testing.T) {
	cont := &contadorFake{revisao: 0, orfas: 0}
	completeness := &completenessFake{avaliados: 5, incompletos: 0}
	h := New(&libraryFake{}, newScanFake(), cont, &matcherFake{}, completeness)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 0 em revisão, 0 órfã(s); 5 itens avaliados, 0 incompletos em catalogação"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarFalhaDoRecalculoCompletudeNaoAbortaJob cobre o critério
// best-effort: uma falha do CompletenessRecalculator não derruba o job -- o
// resumo original (sem a cláusula extra) ainda sai.
func TestExecutarFalhaDoRecalculoCompletudeNaoAbortaJob(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	completeness := &completenessFake{err: errors.New("erro simulado no recalculo de completude")}
	h := New(&libraryFake{}, newScanFake(), cont, &matcherFake{}, completeness)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa do recalculo de completude (best-effort): %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarSemCompletenessRecalculatorNaoRecalcula cobre a guarda
// completeness==nil (mesma guarda de rodarAutoMatch/
// rodarEnriquecimentoRetroativo): sem CompletenessRecalculator injetado, o
// passo degrada para (0, 0) sem panicar e sem a cláusula extra.
func TestExecutarSemCompletenessRecalculatorNaoRecalcula(t *testing.T) {
	h := New(&libraryFake{}, newScanFake(), &contadorFake{revisao: 1, orfas: 0}, &matcherFake{}, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 1 em revisão, 0 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}
