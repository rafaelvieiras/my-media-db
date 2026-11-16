package provider

// Registry is the multi-provider REGISTRY (ADR-0002, R2): it maps the name of
// the source (Provider.Nome(), e.g. "tmdb"/"tvdb") to the corresponding
// Provider, replacing the SINGLE singular provider of the MVP. It is the
// pluggability point per source: the search queries ALL providers and merges;
// the detail/collection/external layer choose the provider BY THE SOURCE of the
// data. The registration order is preserved (deterministic order in
// All()/Nomes()), for results that are stable across runs.
//
// The zero value is usable (Registrar creates the map lazily; Vazio()/Get() work
// on an uninitialized Registry): the Server keeps it as a plain field.
type Registry struct {
	porNome map[string]Provider
	ordem   []string
}

// NovaRegistry creates a Registry already holding the given providers (nil is
// ignored). A provider whose Nome() collides with one already registered
// REPLACES it, keeping its position in the order -- the last one to register
// wins (predictable behavior).
func NovaRegistry(provs ...Provider) Registry {
	var r Registry
	for _, p := range provs {
		r.Registrar(p)
	}
	return r
}

// Registrar adds (or replaces) a provider under its Nome(). It preserves the
// order of first appearance of the name. nil is ignored (keeping compatibility
// with WithSearchProvider, which already ignored a nil provider).
func (r *Registry) Registrar(p Provider) {
	if p == nil {
		return
	}
	nome := p.Nome()
	if r.porNome == nil {
		r.porNome = make(map[string]Provider)
	}
	if _, existe := r.porNome[nome]; !existe {
		r.ordem = append(r.ordem, nome)
	}
	r.porNome[nome] = p
}

// Get returns the provider of source `nome` and true when it is registered;
// (nil,false) otherwise -- the caller degrades (empty external layer / 503 as
// appropriate).
func (r Registry) Get(nome string) (Provider, bool) {
	p, ok := r.porNome[nome]
	return p, ok
}

// All returns the providers in REGISTRATION ORDER (deterministic) -- the basis
// of the search-merge. Empty when there is none.
func (r Registry) All() []Provider {
	out := make([]Provider, 0, len(r.ordem))
	for _, n := range r.ordem {
		out = append(out, r.porNome[n])
	}
	return out
}

// Nomes returns the source names in REGISTRATION ORDER (defensive copy).
func (r Registry) Nomes() []string {
	return append([]string(nil), r.ordem...)
}

// Vazio reports whether there is no provider registered -- the analogue of the
// old `searchProvider == nil` (external search off; enrich/detail degrade with
// 503).
func (r Registry) Vazio() bool { return len(r.porNome) == 0 }
