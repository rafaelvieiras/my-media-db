package musicbrainz

import (
	"sync"
	"time"
)

// limitador é um limitador de taxa CLIENT-SIDE simples: mutex + timestamp da última
// chamada liberada, sem dependência nova (AGENTS.md: "local-first e leve" — nada de
// golang.org/x/time/rate para um caso tão simples). O MusicBrainz exige <= 1
// requisição/s por IP e bloqueia por abuso quando o limite é excedido (briefing 0106 §3,
// https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting); o Cover Art Archive NÃO
// documenta um limite equivalente, então o client só aplica este limitador às chamadas a
// musicbrainz.org (ver musicbrainz.go).
//
// `agora`/`dormir` são injetáveis para permitir um teste DETERMINÍSTICO e RÁPIDO (sem
// depender de um time.Sleep real de 1s+): nil usa time.Now/time.Sleep reais.
type limitador struct {
	mu        sync.Mutex
	ultima    time.Time
	intervalo time.Duration
	agora     func() time.Time
	dormir    func(time.Duration)
}

// novoLimitador cria um limitador que garante pelo menos `intervalo` entre duas chamadas
// liberadas por esperar().
func novoLimitador(intervalo time.Duration) *limitador {
	return &limitador{intervalo: intervalo}
}

// esperar BLOQUEIA até que `intervalo` tenha passado desde a última chamada liberada (a
// primeira chamada nunca espera). Serializado por mutex: chamadas concorrentes esperam a
// vez, cada uma vendo a `ultima` atualizada pela anterior.
func (l *limitador) esperar() {
	l.mu.Lock()
	defer l.mu.Unlock()

	agora := l.agoraFn()
	if !l.ultima.IsZero() {
		proxima := l.ultima.Add(l.intervalo)
		if agora.Before(proxima) {
			l.dormirFn(proxima.Sub(agora))
			agora = proxima
		}
	}
	l.ultima = agora
}

func (l *limitador) agoraFn() time.Time {
	if l.agora != nil {
		return l.agora()
	}
	return time.Now()
}

func (l *limitador) dormirFn(d time.Duration) {
	if l.dormir != nil {
		l.dormir(d)
		return
	}
	time.Sleep(d)
}
