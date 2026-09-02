package acoustid

import (
	"sync"
	"time"
)

// limitador é um limitador de taxa CLIENT-SIDE simples: mutex + timestamp da
// última chamada liberada — MESMA ideia de
// internal/provider/musicbrainz/ratelimit.go (AGENTS.md: "local-first e
// leve", sem dependência nova tipo golang.org/x/time/rate), com um intervalo
// DIFERENTE: o AcoustID documenta no máximo 3 requisições/segundo (briefing
// 0126), contra <= 1 req/s do MusicBrainz.
//
// `agora`/`dormir` são injetáveis para permitir um teste DETERMINÍSTICO e
// RÁPIDO (sem depender de um time.Sleep real): nil usa time.Now/time.Sleep
// reais.
type limitador struct {
	mu        sync.Mutex
	ultima    time.Time
	intervalo time.Duration
	agora     func() time.Time
	dormir    func(time.Duration)
}

// novoLimitador cria um limitador que garante pelo menos `intervalo` entre
// duas chamadas liberadas por esperar().
func novoLimitador(intervalo time.Duration) *limitador {
	return &limitador{intervalo: intervalo}
}

// esperar BLOQUEIA até que `intervalo` tenha passado desde a última chamada
// liberada (a primeira chamada nunca espera). Serializado por mutex:
// chamadas concorrentes esperam a vez, cada uma vendo a `ultima` atualizada
// pela anterior.
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
