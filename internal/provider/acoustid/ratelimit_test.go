package acoustid

import (
	"testing"
	"time"
)

// relogioFake is a deterministic clock that can be advanced by hand -- the same
// pattern as internal/provider/musicbrainz/ratelimit_test.go.
type relogioFake struct{ agora time.Time }

func (r *relogioFake) now() time.Time          { return r.agora }
func (r *relogioFake) avancar(d time.Duration) { r.agora = r.agora.Add(d) }

func TestLimitador_PrimeiraChamadaNaoEspera(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	dormiu := false
	lim := &limitador{
		intervalo: intervaloRateLimit,
		agora:     rel.now,
		dormir:    func(time.Duration) { dormiu = true },
	}
	lim.esperar()
	if dormiu {
		t.Fatal("primeira chamada nao deveria dormir")
	}
}

func TestLimitador_SegundaChamadaEsperaOIntervalo(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	lim := &limitador{
		intervalo: intervaloRateLimit, // 3 req/s => ~333ms
		agora:     rel.now,
		dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
	}
	lim.esperar() // t=0, sem espera
	lim.esperar() // precisa esperar o intervalo inteiro

	if len(dorm) != 1 {
		t.Fatalf("esperava 1 espera, houve %d", len(dorm))
	}
	if dorm[0] != intervaloRateLimit {
		t.Fatalf("espera = %v, esperava %v", dorm[0], intervaloRateLimit)
	}
}

func TestLimitador_TresChamadasRespeitamTresPorSegundo(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	lim := &limitador{
		intervalo: intervaloRateLimit,
		agora:     rel.now,
		dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
	}
	lim.esperar()
	lim.esperar()
	lim.esperar()

	if len(dorm) != 2 {
		t.Fatalf("esperava 2 esperas (so a 1a chamada eh gratis), houve %d", len(dorm))
	}
	// 3 calls must occupy >= 2 x interval on the clock -- that is, at most 3 requests per
	// second, never more.
	total := dorm[0] + dorm[1]
	minimoEsperado := 2 * intervaloRateLimit
	if total < minimoEsperado {
		t.Fatalf("tempo total esperado = %v, esperava >= %v (3 req/s respeitado)", total, minimoEsperado)
	}
}

func TestNovoLimitador_UsaRelogioReal(t *testing.T) {
	lim := novoLimitador(intervaloRateLimit)
	inicio := time.Now()
	lim.esperar() // primeira chamada: nunca dorme
	if time.Since(inicio) > 100*time.Millisecond {
		t.Fatalf("primeira chamada nao deveria demorar, levou %v", time.Since(inicio))
	}
}
