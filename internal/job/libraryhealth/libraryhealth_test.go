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

func acelerarPoll(t *testing.T) {
	t.Helper()
	original := pollInterval
	pollInterval = 5 * time.Millisecond
	t.Cleanup(func() { pollInterval = original })
}

func TestNome(t *testing.T) {
	h := New(&libraryFake{}, newScanFake(), &contadorFake{})
	if h.Nome() != "saude_biblioteca" {
		t.Fatalf("Nome() = %q, quero saude_biblioteca", h.Nome())
	}
}

// TestExecutarSemFontes prova que zero library_source cadastradas nao e erro
// -- so uma mensagem informativa + as contagens atuais.
func TestExecutarSemFontes(t *testing.T) {
	h := New(&libraryFake{}, newScanFake(), &contadorFake{revisao: 3, orfas: 2})

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
	h := New(lib, scn, cont)

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
	h := New(lib, scn, cont)

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
	h := New(lib, scn, &contadorFake{})

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
	h := New(lib, scn, &contadorFake{})

	_, err := h.Executar(context.Background(), nil)
	if err == nil {
		t.Fatal("Executar deveria devolver erro quando TODAS as fontes falham")
	}
}

// TestExecutarErroListarFontes prova que erro ao listar as fontes aborta
// imediatamente (nada para escanear).
func TestExecutarErroListarFontes(t *testing.T) {
	lib := &libraryFake{err: errors.New("banco indisponível")}
	h := New(lib, newScanFake(), &contadorFake{})

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
	h := New(lib, scn, &contadorFake{})

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
	h := New(&libraryFake{}, newScanFake(), &contadorFake{err: fmt.Errorf("banco indisponível")})

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar: %v", err)
	}
	if msg == "" {
		t.Fatal("mensagem vazia")
	}
}
