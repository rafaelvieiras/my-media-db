package musicbrainz

import (
	"testing"
	"time"
)

// relogioFake é um relógio controlável pelo teste: `sleep` AVANÇA o relógio pela duração
// pedida (simula o tempo "passando" durante o time.Sleep real, sem esperar de verdade), e
// o teste pode avançá-lo manualmente entre chamadas para simular trabalho externo lento
// (ex.: uma requisição HTTP que já consumiu >= intervalo).
type relogioFake struct {
	agora time.Time
}

func (r *relogioFake) now() time.Time { return r.agora }

func (r *relogioFake) avancar(d time.Duration) { r.agora = r.agora.Add(d) }

// TestLimitadorPrimeiraChamadaNaoEspera confirma que a primeira chamada a esperar() nunca
// dorme (não há "última chamada" ainda).
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

// TestLimitadorSegundaChamadaImediataEspera confirma que duas chamadas em sequência, sem
// tempo real algum passando entre elas, forçam uma espera de exatamente o intervalo
// configurado (nunca duas requisições no mesmo instante).
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

// TestLimitadorNaoEsperaSeIntervaloJaPassou confirma que, quando tempo suficiente já
// passou entre chamadas (ex.: a requisição HTTP anterior demorou mais que o intervalo), a
// próxima chamada NÃO dorme.
func TestLimitadorNaoEsperaSeIntervaloJaPassou(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	lim := &limitador{
		intervalo: time.Second,
		agora:     rel.now,
		dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
	}

	lim.esperar()
	rel.avancar(2 * time.Second) // simula trabalho externo lento
	lim.esperar()

	if len(dorm) != 0 {
		t.Fatalf("nao deveria ter dormido (intervalo ja passou), dormiu %v", dorm)
	}
}

// TestLimitadorEsperaParcial confirma que, quando só uma fração do intervalo passou, a
// espera é só o RESTANTE (não o intervalo inteiro de novo).
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

// TestLimitadorTresChamadasEmSequencia confirma o comportamento em cadeia (mais de duas
// chamadas), sem nenhum time.Sleep real — puramente determinístico via o relógio fake.
func TestLimitadorTresChamadasEmSequencia(t *testing.T) {
	rel := &relogioFake{agora: time.Unix(0, 0)}
	var dorm []time.Duration
	lim := &limitador{
		intervalo: time.Second,
		agora:     rel.now,
		dormir:    func(d time.Duration) { dorm = append(dorm, d); rel.avancar(d) },
	}

	lim.esperar() // t=0, sem espera
	lim.esperar() // t=0 -> precisa esperar 1s -> t=1s
	lim.esperar() // t=1s -> precisa esperar 1s -> t=2s

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

// TestNovoLimitadorUsaRelogioReal garante que, SEM injeção (uso real, fora de teste), o
// limitador não quebra ao usar time.Now/time.Sleep de verdade -- só exercita o caminho
// "primeira chamada nao espera" para não introduzir um sleep real >= 1s neste teste.
func TestNovoLimitadorUsaRelogioReal(t *testing.T) {
	lim := novoLimitador(time.Second)
	inicio := time.Now()
	lim.esperar() // primeira chamada: nunca dorme
	if time.Since(inicio) > 100*time.Millisecond {
		t.Fatalf("primeira chamada nao deveria demorar, levou %v", time.Since(inicio))
	}
}
