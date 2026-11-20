package libraryhealth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// libraryFake is a fake LibraryLister, without touching the filesystem.
type libraryFake struct {
	sources []library.Source
	err     error
}

func (f *libraryFake) Listar(context.Context) ([]library.Source, error) {
	return f.sources, f.err
}

// scanFake is a fake ScanRunner (LAYER 3: Executar runs SYNCHRONOUSLY, with no
// queue -- see the comment on ScanRunner about the deadlock that avoids).
// startErr[sourceID] forces Executar to fail IMMEDIATELY for that source
// (equivalent to the old Start() failing); runErr[sourceID] forces Executar to
// fail AFTER `duracao` (equivalent to the old LastError of a scan that ran and
// ended with an error).
type scanFake struct {
	startErr map[string]error
	runErr   map[string]error
	duracao  time.Duration

	mu      sync.Mutex
	storage map[string][2]int64 // sourceID -> [managed, unmanaged]
}

func newScanFake() *scanFake {
	return &scanFake{
		startErr: map[string]error{},
		runErr:   map[string]error{},
		storage:  map[string][2]int64{},
	}
}

// StorageNaoVideo satisfies ScanRunner: it returns the value configured in
// `storage` for the source, or ok=false when absent -- the same "there has never
// been a scan" semantics as the real scanner.Manager.
func (f *scanFake) StorageNaoVideo(sourceID string) (managedBytes, unmanagedBytes int64, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, existe := f.storage[sourceID]
	if !existe {
		return 0, 0, false
	}
	return v[0], v[1], true
}

// Executar simulates the synchronous scan of ONE source: it fails immediately if
// startErr is configured; otherwise it "runs" for `duracao` (honoring ctx.Done)
// and fails with runErr, if configured, at the end.
func (f *scanFake) Executar(ctx context.Context, refID, _ string, _ execqueue.Registro) (string, error) {
	if err := f.startErr[refID]; err != nil {
		return "", err
	}
	if f.duracao > 0 {
		select {
		case <-time.After(f.duracao):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err := f.runErr[refID]; err != nil {
		return "", err
	}
	return "", nil
}

// contadorFake is a fake Contador with fixed values (or an error).
type contadorFake struct {
	revisao, orfas int
	err            error
}

func (c *contadorFake) ContarRevisao(context.Context) (int, error) { return c.revisao, c.err }
func (c *contadorFake) ContarOrfas(context.Context) (int, error)   { return c.orfas, c.err }

// matcherFake is a fake Matcher: fixed values of resolved/pending (auto-match)
// and aplicados/sinalizados (retroactive enrichment), or an error to exercise
// the best-effort path of the job. The two methods have separate call counters
// (chamadasAutoMatch/chamadasRetroativo) so the tests can prove that each one
// runs EXACTLY once per execution.
type matcherFake struct {
	resolved, pending int
	err               error
	chamadasAutoMatch int

	aplicados, sinalizados int
	errRetroativo          error
	chamadasRetroativo     int

	artesProcessadas       int
	errArtes               error
	chamadasReprocessArtes int

	arteLocalEncontrada int
	errArteLocal        error
	chamadasArteLocal   int

	reconciliados       int
	errReconciliar      error
	chamadasReconciliar int
}

func (m *matcherFake) AutoMatchElegiveis(context.Context, execqueue.Registro) (int, int, error) {
	m.chamadasAutoMatch++
	return m.resolved, m.pending, m.err
}

func (m *matcherFake) EnriquecerRetroativo(context.Context, execqueue.Registro) (int, int, error) {
	m.chamadasRetroativo++
	return m.aplicados, m.sinalizados, m.errRetroativo
}

func (m *matcherFake) ReprocessarArtesFaltantes(context.Context) (int, error) {
	m.chamadasReprocessArtes++
	return m.artesProcessadas, m.errArtes
}

func (m *matcherFake) DescobrirArteLocalMusica(context.Context) (int, error) {
	m.chamadasArteLocal++
	return m.arteLocalEncontrada, m.errArteLocal
}

func (m *matcherFake) ReconciliarFilhosOrfaos(context.Context) (int, error) {
	m.chamadasReconciliar++
	return m.reconciliados, m.errReconciliar
}

// completenessFake is a fake CompletenessRecalculator: fixed values of
// avaliados/incompletos, or an error to exercise the best-effort path of the
// job. chamadas counts how many times the method ran, so the tests can prove it
// runs EXACTLY once per execution.
type completenessFake struct {
	avaliados, incompletos int
	err                    error
	chamadas               int
}

func (c *completenessFake) RecalcularCompletudeCatalogacao(context.Context) (int, int, error) {
	c.chamadas++
	return c.avaliados, c.incompletos, c.err
}

// contadorSequencialFake returns one ContarRevisao value per call, in the order
// of revisaoValores (the last extra call repeats the last value) -- used to
// prove that the job RECOUNTS the queue AFTER the auto-match, reflecting the
// drop in items (contadorFake, with a fixed value, cannot distinguish
// "recounted" from "did not recount").
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

func TestNome(t *testing.T) {
	h := New(&libraryFake{}, newScanFake(), &contadorFake{}, &matcherFake{}, nil)
	if h.Nome() != "saude_biblioteca" {
		t.Fatalf("Nome() = %q, quero saude_biblioteca", h.Nome())
	}
}

// TestExecutarSemFontes proves that zero registered library_source is not an
// error -- only an informative message plus the current counts.
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

// TestExecutarTodasSucesso proves the happy path: it triggers the scan of each
// source, WAITS for each one to finish before counting the queues (the real
// assertion is indirect -- if the Handler did not wait, the test would be
// non-deterministic and would fail sporadically under -race/-count=10).
func TestExecutarTodasSucesso(t *testing.T) {
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

// TestExecutarFalhaParcial proves that an offline source (Start fails) does not
// abort the other one, and the final message reports the partial failure.
func TestExecutarFalhaParcial(t *testing.T) {
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

// TestExecutarErroDeScanNoMeio proves that a scan that STARTS but ends with an
// error (LastError) also counts as a failure of that source, without aborting
// the others.
func TestExecutarErroDeScanNoMeio(t *testing.T) {
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

// TestExecutarTodasFalham proves the criterion: if ALL sources fail, the job
// returns an ERROR (not a lying success).
func TestExecutarTodasFalham(t *testing.T) {
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

// TestExecutarErroListarFontes proves that an error listing the sources aborts
// immediately (nothing to scan).
func TestExecutarErroListarFontes(t *testing.T) {
	lib := &libraryFake{err: errors.New("banco indisponível")}
	h := New(lib, newScanFake(), &contadorFake{}, &matcherFake{}, nil)

	if _, err := h.Executar(context.Background(), nil); err == nil {
		t.Fatal("Executar deveria propagar o erro de listar fontes")
	}
}

// TestExecutarAguardaAntesDeContar is the most important test of this Handler:
// it proves that the Handler REALLY waits for the scan to finish before
// counting the queues -- if it did not wait, the count below would be captured
// BEFORE scanFake marks running=false, but as the duration is longer than a
// synchronous call, any implementation that did not wait would pass by luck;
// repeating it with -count=10 in CI catches the regression.
func TestExecutarAguardaAntesDeContar(t *testing.T) {
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

// TestExecutarContagemFalha proves that an error counting the queues does not
// become a job error (the scan itself worked) -- the message reports the
// problem.
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

// TestExecutarAcionaAutoMatchEReflete covers the central criterion of the
// auto-match: after counting review/orphans, the job calls the Matcher and the
// final message reflects "<M> resolvidos automaticamente" -- with the review
// count RECOUNTED after the auto-match (3, not the 5 from before it ran),
// proving that the job does not use the stale count.
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

// TestExecutarFalhaDoAutoMatchNaoAbortaJob covers the best-effort criterion: a
// failure of the Matcher does not bring the job down -- the original
// scan/count summary (without "resolvidos automaticamente", since nothing was
// resolved) still comes out.
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

// TestExecutarAcionaEnriquecimentoRetroativoEReflete covers the central
// criterion of the retroactive enrichment: after the batch auto-match, the job
// calls EnriquecerRetroativo and the final message gains the clause
// "enriquecimento retroativo: <A> aplicado(s), <S> sinalizado(s) para revisão"
// -- with the review count RECOUNTED again after this step (3, not the 5 from
// before), proving that the job reflects the FINAL state (flagged items entered
// the queue).
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

// TestExecutarEnriquecimentoRetroativoOmiteClausulaQuandoZero covers the
// omission criterion: with nothing applied or flagged, the final message does
// NOT gain the extra clause (same spirit as omitting "resolvidos
// automaticamente" when resolvidos==0).
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

// TestExecutarFalhaDoEnriquecimentoRetroativoNaoAbortaJob covers the best-effort
// criterion: a failure of the Matcher in the retroactive step does not bring the
// job down -- the original summary (without the extra clause) still comes out.
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

// TestExecutarSemMatcherNaoRodaEnriquecimentoRetroativo covers the matcher==nil
// guard (the same guard as rodarAutoMatch): with no Matcher injected, the
// retroactive step degrades to (0, 0) without panicking.
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

// TestExecutarAcionaReprocessamentoArtesEReflete covers the central criterion of
// the art reprocessing: after the retroactive enrichment, the job calls
// ReprocessarArtesFaltantes and the final message gains the clause "<N>
// entity(ies) com inventário de arte reprocessado".
func TestExecutarAcionaReprocessamentoArtesEReflete(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	matcher := &matcherFake{artesProcessadas: 7}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s); 7 entity(ies) com inventário de arte reprocessado"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
	if matcher.chamadasReprocessArtes != 1 {
		t.Errorf("ReprocessarArtesFaltantes chamado %d vez(es), quero exatamente 1", matcher.chamadasReprocessArtes)
	}
}

// TestExecutarReprocessamentoArtesOmiteClausulaQuandoZero covers the omission
// criterion: with no entity reprocessed, the final message does NOT gain the
// extra clause.
func TestExecutarReprocessamentoArtesOmiteClausulaQuandoZero(t *testing.T) {
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

// TestExecutarFalhaDoReprocessamentoArtesNaoAbortaJob covers the best-effort
// criterion: a failure of the Matcher in this step does not bring the job
// down -- the original summary (without the extra clause) still comes out.
func TestExecutarFalhaDoReprocessamentoArtesNaoAbortaJob(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	matcher := &matcherFake{errArtes: errors.New("erro simulado no reprocessamento de artes")}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa do reprocessamento de artes (best-effort): %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarSemMatcherNaoRodaReprocessamentoArtes covers the matcher==nil
// guard (the same guard as
// rodarAutoMatch/rodarEnriquecimentoRetroativo): with no Matcher injected, the
// step degrades to 0 without panicking.
func TestExecutarSemMatcherNaoRodaReprocessamentoArtes(t *testing.T) {
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

// TestExecutarAcionaDescobertaArteLocalMusicaEReflete covers the central
// criterion of the local music art discovery: after the art reprocessing, the
// job calls DescobrirArteLocalMusica and the final message gains the clause
// "<N> entity(ies) de música com pôster local encontrado".
func TestExecutarAcionaDescobertaArteLocalMusicaEReflete(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	matcher := &matcherFake{arteLocalEncontrada: 3}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s); 3 entity(ies) de música com pôster local encontrado"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
	if matcher.chamadasArteLocal != 1 {
		t.Errorf("DescobrirArteLocalMusica chamado %d vez(es), quero exatamente 1", matcher.chamadasArteLocal)
	}
}

// TestExecutarDescobertaArteLocalMusicaOmiteClausulaQuandoZero covers the
// omission criterion: with no local poster found, the final message does NOT
// gain the extra clause.
func TestExecutarDescobertaArteLocalMusicaOmiteClausulaQuandoZero(t *testing.T) {
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

// TestExecutarFalhaDaDescobertaArteLocalMusicaNaoAbortaJob covers the best-effort
// criterion: a failure of the Matcher in this step does not bring the job
// down -- the original summary (without the extra clause) still comes out.
func TestExecutarFalhaDaDescobertaArteLocalMusicaNaoAbortaJob(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	matcher := &matcherFake{errArteLocal: errors.New("erro simulado na descoberta de arte local")}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa da descoberta de arte local (best-effort): %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarSemMatcherNaoRodaDescobertaArteLocalMusica covers the matcher==nil
// guard: with no Matcher injected, the step degrades to 0 without panicking.
func TestExecutarSemMatcherNaoRodaDescobertaArteLocalMusica(t *testing.T) {
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

// TestExecutarAcionaReconciliacaoFilhosOrfaosEReflete covers the central
// criterion of the reconciliation of orphan children: after the local music art
// discovery, the job calls ReconciliarFilhosOrfaos and the final message gains
// the clause "<N> faixa(s)/episódio(s) religado(s) à árvore" -- with the review
// count RECOUNTED again after this step (3, not the 5 from before), proving that
// the reconciliation really removes items from the queue.
func TestExecutarAcionaReconciliacaoFilhosOrfaosEReflete(t *testing.T) {
	cont := &contadorSequencialFake{revisaoValores: []int{5, 3}, orfas: 1}
	matcher := &matcherFake{reconciliados: 2}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 3 em revisão, 1 órfã(s); 2 faixa(s)/episódio(s) religado(s) à árvore"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
	if matcher.chamadasReconciliar != 1 {
		t.Errorf("ReconciliarFilhosOrfaos chamado %d vez(es), quero exatamente 1", matcher.chamadasReconciliar)
	}
}

// TestExecutarReconciliacaoFilhosOrfaosOmiteClausulaQuandoZero covers the
// omission criterion: with no item re-linked, the final message does NOT gain
// the extra clause.
func TestExecutarReconciliacaoFilhosOrfaosOmiteClausulaQuandoZero(t *testing.T) {
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

// TestExecutarFalhaDaReconciliacaoFilhosOrfaosNaoAbortaJob covers the best-effort
// criterion: a failure of the Matcher in this step does not bring the job
// down -- the original summary (without the extra clause) still comes out.
func TestExecutarFalhaDaReconciliacaoFilhosOrfaosNaoAbortaJob(t *testing.T) {
	cont := &contadorFake{revisao: 4, orfas: 2}
	matcher := &matcherFake{errReconciliar: errors.New("erro simulado na reconciliação de filhos órfãos")}
	h := New(&libraryFake{}, newScanFake(), cont, matcher, nil)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa da reconciliação de filhos órfãos (best-effort): %v", err)
	}
	quero := "nenhuma fonte de biblioteca cadastrada; 4 em revisão, 2 órfã(s)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestExecutarSemMatcherNaoRodaReconciliacaoFilhosOrfaos covers the matcher==nil
// guard: with no Matcher injected, the step degrades to 0 without panicking.
func TestExecutarSemMatcherNaoRodaReconciliacaoFilhosOrfaos(t *testing.T) {
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

// TestExecutarAcionaRecalculoCompletudeEReflete covers the central criterion of
// the completeness recalculation: after the retroactive enrichment, the job
// calls RecalcularCompletudeCatalogacao and the final message gains the clause
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

// TestExecutarRecalculoCompletudeClausulaComZeroIncompletos proves that the
// clause appears even with incompletos==0, as long as avaliados>0 (0 incompletos
// is relevant positive information — unlike aplicados/sinalizados==0, which only
// means "nothing to do").
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

// TestExecutarFalhaDoRecalculoCompletudeNaoAbortaJob covers the best-effort
// criterion: a failure of the CompletenessRecalculator does not bring the job
// down -- the original summary (without the extra clause) still comes out.
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

// TestExecutarSemCompletenessRecalculatorNaoRecalcula covers the
// completeness==nil guard (the same guard as rodarAutoMatch/
// rodarEnriquecimentoRetroativo): with no CompletenessRecalculator injected, the
// step degrades to (0, 0) without panicking and without the extra clause.
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

// ── Differentiated storage ───────────────────────────────────────────────────

// TestExecutarAgregaStorageNaoVideoDeTodasAsFontes proves that, after a
// successful execution, UltimoResultadoStorage adds up managed/unmanaged from
// ALL registered sources.
func TestExecutarAgregaStorageNaoVideoDeTodasAsFontes(t *testing.T) {
	scn := newScanFake()
	scn.storage["s1"] = [2]int64{100, 10}
	scn.storage["s2"] = [2]int64{200, 20}
	lib := &libraryFake{sources: []library.Source{
		{ID: "s1", Path: "/lib/um"},
		{ID: "s2", Path: "/lib/dois"},
	}}
	h := New(lib, scn, &contadorFake{}, &matcherFake{}, nil)

	if _, err := h.Executar(context.Background(), nil); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	managed, unmanaged, computadoEm, ok := h.UltimoResultadoStorage()
	if !ok {
		t.Fatal("UltimoResultadoStorage ok=false apos execucao bem-sucedida")
	}
	if managed != 300 {
		t.Errorf("managedBytes = %d, quero 300 (100+200)", managed)
	}
	if unmanaged != 30 {
		t.Errorf("unmanagedBytes = %d, quero 30 (10+20)", unmanaged)
	}
	if computadoEm.IsZero() {
		t.Error("computadoEm zero, quero preenchido")
	}
}

// TestUltimoResultadoStorageAntesDeExecutarDevolveOkFalse proves that, before
// ANY execution, the freshly created Handler has no storage result (freshly
// started process, the job has not run yet) -- the caller (health_system) must
// treat that as "no data", not as zero.
func TestUltimoResultadoStorageAntesDeExecutarDevolveOkFalse(t *testing.T) {
	h := New(&libraryFake{}, newScanFake(), &contadorFake{}, &matcherFake{}, nil)
	if _, _, _, ok := h.UltimoResultadoStorage(); ok {
		t.Fatal("ok = true antes de qualquer execucao, quero false")
	}
}

// TestExecutarStorageMantemUltimoValorDeFonteOffline proves the
// "keeps the last calculated value" policy: a source that fails in THIS run
// (offline) keeps the LAST known value (already present in the scanner.Manager
// from a previous successful run) instead of zeroing it -- with no extra
// "offline" logic in this package, it is a direct consequence of Start() never
// overwriting the storage when it fails (same spirit as the disk usage/Statfs
// reading).
func TestExecutarStorageMantemUltimoValorDeFonteOffline(t *testing.T) {
	scn := newScanFake()
	// s-offline already has a value from a previous successful run -- Start()
	// will fail in this run, but scn.storage is not touched (the same behavior
	// as the real scanner.Manager).
	scn.storage["s-offline"] = [2]int64{500, 50}
	scn.startErr["s-offline"] = errors.New("datasource inacessível (offline)")
	scn.storage["s-ok"] = [2]int64{100, 10}
	lib := &libraryFake{sources: []library.Source{
		{ID: "s-ok", Path: "/lib/ok"},
		{ID: "s-offline", Path: "/lib/offline"},
	}}
	h := New(lib, scn, &contadorFake{}, &matcherFake{}, nil)

	if _, err := h.Executar(context.Background(), nil); err != nil {
		t.Fatalf("Executar não deveria falhar com sucesso parcial: %v", err)
	}

	managed, unmanaged, _, ok := h.UltimoResultadoStorage()
	if !ok {
		t.Fatal("ok=false, quero true (pelo menos s-ok tem dado)")
	}
	if managed != 600 {
		t.Errorf("managedBytes = %d, quero 600 (100 de s-ok + 500 mantido de s-offline)", managed)
	}
	if unmanaged != 60 {
		t.Errorf("unmanagedBytes = %d, quero 60 (10+50)", unmanaged)
	}
}
