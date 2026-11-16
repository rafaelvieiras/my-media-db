package job

// Registry is the REGISTRY of job types: it maps the job name (Handler.Nome(),
// e.g. "backup_vacuum") to the corresponding Handler — a STRUCTURAL copy of
// internal/provider/registry.go (same pattern, adapted to the job domain). It
// is the pluggability point of the future plugin system: a new job type only
// has to Register itself here, without touching the schema or the endpoints.
//
// The zero-value is usable (Registrar creates the map lazily); the Manager
// keeps it as a plain field (exported, so WithJobHandlers can register straight
// into it). Registration order is preserved (deterministic in All()/Nomes());
// the last one to register under the same name wins (predictable behavior).
type Registry struct {
	porNome map[string]Handler
	ordem   []string
}

// Registrar adds (or replaces) a handler under its Nome(). It preserves the
// order of the name's first appearance. nil is ignored.
func (r *Registry) Registrar(h Handler) {
	if h == nil {
		return
	}
	nome := h.Nome()
	if r.porNome == nil {
		r.porNome = make(map[string]Handler)
	}
	if _, existe := r.porNome[nome]; !existe {
		r.ordem = append(r.ordem, nome)
	}
	r.porNome[nome] = h
}

// Get returns the handler of job `nome` and true when it is registered;
// (nil,false) otherwise — the caller returns ErrJobDesconhecido (mapped to 503:
// job seeded in the database, but the binary has no Handler for it, e.g. a
// downgrade).
func (r Registry) Get(nome string) (Handler, bool) {
	h, ok := r.porNome[nome]
	return h, ok
}

// All returns the handlers in REGISTRATION order (deterministic).
func (r Registry) All() []Handler {
	out := make([]Handler, 0, len(r.ordem))
	for _, n := range r.ordem {
		out = append(out, r.porNome[n])
	}
	return out
}

// Nomes returns the names of the registered jobs in REGISTRATION order (a
// defensive copy).
func (r Registry) Nomes() []string {
	return append([]string(nil), r.ordem...)
}
