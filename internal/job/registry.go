package job

// Registry é o REGISTRO de tipos de job (briefing 0052): mapeia o nome do job
// (Handler.Nome(), ex.: "backup_vacuum") ao Handler correspondente — cópia
// ESTRUTURAL de internal/provider/registry.go (mesmo padrão, adaptado ao
// domínio de job). É o ponto de plugabilidade do futuro sistema de plugins: um
// novo tipo de job só precisa se Registrar aqui, sem tocar o schema nem os
// endpoints.
//
// O zero-value é utilizável (Registrar cria o mapa preguiçosamente); o Manager
// o guarda como campo simples (exportado, para WithJobHandlers registrar
// direto). A ordem de registro é preservada (determinística em All()/Nomes());
// o último a registrar sob o mesmo nome vence (comportamento previsível).
type Registry struct {
	porNome map[string]Handler
	ordem   []string
}

// Registrar adiciona (ou substitui) um handler sob o seu Nome(). Preserva a
// ordem de primeira aparição do nome. nil é ignorado.
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

// Get devolve o handler do job `nome` e true quando registrado; (nil,false)
// caso contrário — o chamador devolve ErrJobDesconhecido (mapeado a 503: job
// seedado no banco, mas o binário não tem o Handler, ex. downgrade).
func (r Registry) Get(nome string) (Handler, bool) {
	h, ok := r.porNome[nome]
	return h, ok
}

// All devolve os handlers na ORDEM de registro (determinística).
func (r Registry) All() []Handler {
	out := make([]Handler, 0, len(r.ordem))
	for _, n := range r.ordem {
		out = append(out, r.porNome[n])
	}
	return out
}

// Nomes devolve os nomes dos jobs registrados na ORDEM de registro (cópia
// defensiva).
func (r Registry) Nomes() []string {
	return append([]string(nil), r.ordem...)
}
