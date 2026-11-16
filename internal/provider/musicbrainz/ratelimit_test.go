package musicbrainz

import (
	"testing"
	"time"
)

// relogioFake is a clock controllable by the test: `sleep` ADVANCES the clock by the
// requested duration (it simulates time "passing" during the real time.Sleep, without really
// waiting), and the test can advance it by hand between calls to simulate slow external work
// (e.g. an HTTP request that already consumed >= the interval).
type relogioFake struct {
	agora time.Time
}

func (r *relogioFake) now() time.Time { return r.agora }

func (r *relogioFake) avancar(d time.Duration) { r.agora = r.agora.Add(d) }

// TestLimitadorPrimeiraChamadaNaoEspera confirms that the first call to esperar() never
// sleeps (there is no "last call" yet).
func TestLimitadorPrimeiraChamadaNaoEspera(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	lim := &limitador{
		intervalo: time.Second,
		agora:     rel.now,
		dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
	}

	lim.esperar()

	if len(dorm) != 0 {
		t.Fatalf("primeira chamada nao deveria dormir, dormiu %v", dorm)
	}
}

// TestLimitadorSegundaChamadaImediataEspera confirms that two consecutive calls, without any
// real time passing between them, force a wait of exactly the configured interval (never two
// requests at the same instant).
func TestLimitadorSegundaChamadaImediataEspera(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	lim := &limitador{
		intervalo: time.Second,
		agora:     rel.now,
		dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
	}

	lim.esperar()
	lim.esperar()

	if len(dorm) != 1 {
		t.Fatalf("esperava 1 espera, houve %d (%v)", len(dorm), dorm)
	}
	if dorm[0] != time.Second {
		t.Fatalf("esperava dormir 1s, dormiu %v", dorm[0])
	}
}

// TestLimitadorNaoEsperaSeIntervaloJaPassou confirms that, when enough time has already
// passed between calls (e.g. the previous HTTP request took longer than the interval), the
// next call does NOT sleep.
func TestLimitadorNaoEsperaSeIntervaloJaPassou(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	lim := &limitador{
		intervalo: time.Second,
		agora:     rel.now,
		dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
	}

	lim.esperar()
	rel.avancar(2 * time.Second) // simulates slow external work
	lim.esperar()

	if len(dorm) != 0 {
		t.Fatalf("nao deveria ter dormido (intervalo ja passou), dormiu %v", dorm)
	}
}

// TestLimitadorEsperaParcial confirms that, when only a fraction of the interval has passed,
// the wait is only the REMAINDER (not the whole interval again).
func TestLimitadorEsperaParcial(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	lim := &limitador{
		intervalo: time.Second,
		agora:     rel.now,
		dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
	}

	lim.esperar()
	rel.avancar(400 * time.Millisecond)
	lim.esperar()

	if len(dorm) != 1 {
		t.Fatalf("esperava 1 espera, houve %d (%v)", len(dorm), dorm)
	}
	if dorm[0] != 600*time.Millisecond {
		t.Fatalf("esperava dormir 600ms (restante do intervalo), dormiu %v", dorm[0])
	}
}

// TestLimitadorTresChamadasEmSequencia confirms the chained behavior (more than two calls),
// without any real time.Sleep -- purely deterministic through the fake clock.
func TestLimitadorTresChamadasEmSequencia(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	lim := &limitador{
		intervalo: time.Second,
		agora:     rel.now,
		dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
	}

	lim.esperar() // t=0, no wait
	lim.esperar() // t=0 -> must wait 1s -> t=1s
	lim.esperar() // t=1s -> must wait 1s -> t=2s

	if len(dorm) != 2 {
		t.Fatalf("esperava 2 esperas, houve %d (%v)", len(dorm), dorm)
	}
	for i, d := range dorm {
		if d != time.Second {
			t.Fatalf("espera[%d] = %v, esperava 1s", i, d)
		}
	}
	if !rel.agora.Equal(time.Unix(2, 0)) {
		t.Fatalf("relogio final = %v, esperava t=2s", rel.agora)
	}
}

// TestNovoLimitadorUsaRelogioReal guarantees that, WITHOUT injection (real use, outside a
// test), the limiter does not break when using the real time.Now/time.Sleep -- it only
// exercises the "first call does not wait" path so as not to introduce a real sleep >= 1s in
// this test.
func TestNovoLimitadorUsaRelogioReal(t *testing.T) {
	lim := novoLimitador(time.Second)
	inicio := time.Now()
	lim.esperar() // first call: never sleeps
	if time.Since(inicio) > 100*time.Millisecond {
		t.Fatalf("primeira chamada nao deveria demorar, levou %v", time.Since(inicio))
	}
}
