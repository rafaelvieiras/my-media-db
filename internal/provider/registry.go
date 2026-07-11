package provider

// Registry é o REGISTRO multi-provider (briefing 0031 / ADR-0002, R2): mapeia o
// nome da fonte (Provider.Nome(), ex.: "tmdb"/"tvdb") ao Provider correspondente,
// substituindo o provider ÚNICO singular do MVP. É o ponto de plugabilidade por
// fonte: a busca consulta TODOS os providers e mescla; o detalhe/coleção/camada
// externa escolhem o provider PELA FONTE do dado. A ordem de registro é preservada
// (ordem determinística em All()/Nomes()), para resultados estáveis entre execuções.
//
// O zero-value é utilizável (Registrar cria o mapa preguiçosamente; Vazio()/Get()
// funcionam num Registry não inicializado): o Server o guarda como campo simples.
type Registry struct {
	porNome map[string]Provider
	ordem   []string
}

// NovaRegistry cria um Registry já com os providers dados (nil é ignorado). Um
// provider cujo Nome() colide com outro já registrado o SUBSTITUI, mantendo a
// posição na ordem — o último a registrar vence (comportamento previsível).
func NovaRegistry(provs ...Provider) Registry {
	var r Registry
	for _, p := range provs {
		r.Registrar(p)
	}
	return r
}

// Registrar adiciona (ou substitui) um provider sob o seu Nome(). Preserva a ordem
// de primeira aparição do nome. nil é ignorado (mantém a compatibilidade com o
// WithSearchProvider, que já ignorava provider nil).
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

// Get devolve o provider da fonte `nome` e true quando registrado; (nil,false) caso
// contrário — o chamador degrada (camada externa vazia / 503 apropriado).
func (r Registry) Get(nome string) (Provider, bool) {
	p, ok := r.porNome[nome]
	return p, ok
}

// All devolve os providers na ORDEM de registro (determinística) — base da busca-
// mescla. Vazio quando não há nenhum.
func (r Registry) All() []Provider {
	out := make([]Provider, 0, len(r.ordem))
	for _, n := range r.ordem {
		out = append(out, r.porNome[n])
	}
	return out
}

// Nomes devolve os nomes das fontes na ORDEM de registro (cópia defensiva).
func (r Registry) Nomes() []string {
	return append([]string(nil), r.ordem...)
}

// Vazio diz se não há provider registrado — o análogo do antigo `searchProvider ==
// nil` (busca externa desligada; enrich/detalhe degradam com 503).
func (r Registry) Vazio() bool { return len(r.porNome) == 0 }
