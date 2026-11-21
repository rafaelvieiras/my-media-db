package server

import "github.com/rafaelvieiras/my-media-db/internal/execqueue"

// orNullRegistry turns a nil registrar into the null one, so a sweep method can
// report unconditionally: observability never breaks the work it observes
// (ADR-0040). The health job passes the registrar of ITS queue item down to every
// step, so the owner watches one bar per step of the same item -- never a second
// item, and never a silent step.
func orNullRegistry(registro execqueue.Registro) execqueue.Registro {
	if registro == nil {
		return execqueue.RegistroNulo()
	}
	return registro
}
