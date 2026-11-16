package telemetry

import "time"

// UptimeSegundos returns how many seconds the process has been up, counted
// from the instant injected in New (iniciadoEm) -- see the New doc about the
// choice of injection (instead of capturing it in a global init()) for
// testability.
func (s *Servico) UptimeSegundos() int64 {
	return int64(time.Since(s.iniciadoEm).Seconds())
}
