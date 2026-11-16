package integration

// Registry is the multi-integration REGISTRY: it maps the Slug() of a
// Descriptor to the corresponding Descriptor. SAME MOLD as provider.Registry
// (internal/provider/registry.go) — usable zero-value (Registrar creates the map
// lazily), registration order preserved in All()/Nomes(), and a Descriptor whose
// Slug() collides with an already registered one REPLACES it while keeping the
// position in the order (the last one to register wins).
type Registry struct {
	porSlug map[string]Descriptor
	ordem   []string
}

// NovaRegistry creates a Registry already holding the given Descriptors (nil is
// ignored).
func NovaRegistry(descs ...Descriptor) Registry {
	var r Registry
	for _, d := range descs {
		r.Registrar(d)
	}
	return r
}

// Registrar adds (or replaces) a Descriptor under its Slug(). It preserves the
// order of the slug's first appearance. nil is ignored.
func (r *Registry) Registrar(d Descriptor) {
	if d == nil {
		return
	}
	slug := d.Slug()
	if r.porSlug == nil {
		r.porSlug = make(map[string]Descriptor)
	}
	if _, existe := r.porSlug[slug]; !existe {
		r.ordem = append(r.ordem, slug)
	}
	r.porSlug[slug] = d
}

// Get returns the Descriptor for `slug` and true when it is registered;
// (nil,false) otherwise — the caller (HTTP handler) degrades with a 404.
func (r Registry) Get(slug string) (Descriptor, bool) {
	d, ok := r.porSlug[slug]
	return d, ok
}

// All returns the Descriptors in REGISTRATION order (deterministic) — the basis
// of the GET /integrations listing. Empty when there is none.
func (r Registry) All() []Descriptor {
	out := make([]Descriptor, 0, len(r.ordem))
	for _, s := range r.ordem {
		out = append(out, r.porSlug[s])
	}
	return out
}

// Nomes returns the slugs in REGISTRATION order (defensive copy).
func (r Registry) Nomes() []string {
	return append([]string(nil), r.ordem...)
}

// Vazio tells whether there is no registered Descriptor.
func (r Registry) Vazio() bool { return len(r.porSlug) == 0 }
