package telemetry

import "time"

// UptimeSegundos devolve ha quantos segundos o processo esta no ar, a partir
// do instante injetado em New (iniciadoEm) -- ver doc de New sobre a escolha
// de injecao (em vez de captura num init() global) para testabilidade.
func (s *Servico) UptimeSegundos() int64 {
	return int64(time.Since(s.iniciadoEm).Seconds())
}
