package integration

// Registry é o REGISTRO multi-integração: mapeia o Slug() de um Descriptor ao
// Descriptor correspondente. MESMO MOLDE de provider.Registry
// (internal/provider/registry.go) — zero-value utilizável (Registrar cria o
// mapa preguiçosamente), ordem de registro preservada em All()/Nomes(), e um
// Descriptor cujo Slug() colide com outro já registrado o SUBSTITUI mantendo
// a posição na ordem (o último a registrar vence).
type Registry struct {
	porSlug map[string]Descriptor
	ordem   []string
}

// NovaRegistry cria um Registry já com os Descriptors dados (nil é ignorado).
func NovaRegistry(descs ...Descriptor) Registry {
	var r Registry
	for _, d := range descs {
		r.Registrar(d)
	}
	return r
}

// Registrar adiciona (ou substitui) um Descriptor sob o seu Slug(). Preserva
// a ordem de primeira aparição do slug. nil é ignorado.
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

// Get devolve o Descriptor do `slug` e true quando registrado; (nil,false)
// caso contrário — o chamador (handler HTTP) degrada com 404.
func (r Registry) Get(slug string) (Descriptor, bool) {
	d, ok := r.porSlug[slug]
	return d, ok
}

// All devolve os Descriptors na ORDEM de registro (determinística) — base da
// listagem GET /integrations. Vazio quando não há nenhum.
func (r Registry) All() []Descriptor {
	out := make([]Descriptor, 0, len(r.ordem))
	for _, s := range r.ordem {
		out = append(out, r.porSlug[s])
	}
	return out
}

// Nomes devolve os slugs na ORDEM de registro (cópia defensiva).
func (r Registry) Nomes() []string {
	return append([]string(nil), r.ordem...)
}

// Vazio diz se não há nenhum Descriptor registrado.
func (r Registry) Vazio() bool { return len(r.porSlug) == 0 }
