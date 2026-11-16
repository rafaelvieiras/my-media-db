package acoustid

import (
	"sync"
	"time"
)

// limitador is a simple CLIENT-SIDE rate limiter: mutex + timestamp of the last
// released call -- the SAME idea as internal/provider/musicbrainz/ratelimit.go
// (AGENTS.md: "local-first and lightweight", with no new dependency such as
// golang.org/x/time/rate), with a DIFFERENT interval: AcoustID documents at most
// 3 requests per second, against the <= 1 req/s of MusicBrainz.
//
// `agora`/`dormir` are injectable to allow a DETERMINISTIC and FAST test
// (without depending on a real time.Sleep): nil uses the real
// time.Now/time.Sleep.
type limitador struct {
	mu        sync.Mutex
	ultima    time.Time
	intervalo time.Duration
	agora     func() time.Time
	dormir    func(time.Duration)
}

// novoLimitador creates a limiter that guarantees at least `intervalo` between
// two calls released by esperar().
func novoLimitador(intervalo time.Duration) *limitador {
	return &limitador{intervalo: intervalo}
}

// esperar BLOCKS until `intervalo` has elapsed since the last released call
// (the first call never waits). Serialized by mutex: concurrent calls wait for
// their turn, each one seeing the `ultima` updated by the previous one.
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
