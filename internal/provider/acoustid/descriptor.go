package acoustid

import "github.com/rafaelvieiras/my-media-db/internal/integration"

// descriptorIntegracao implements integration.Descriptor for AcoustID (the same
// pattern as internal/provider/tmdb.descriptorIntegracao -- TMDB was the first
// consumer). It only describes the SCHEMA of the fields (a single field, the
// client key) and delegates Configurada to the SAME rule that
// Credencial.Configurada already uses -- it does not duplicate the logic.
type descriptorIntegracao struct{}

// NovoDescriptorIntegracao creates the AcoustID integration.Descriptor.
// Registered in cmd/mymediadb/main.go through server.WithIntegrations.
func NovoDescriptorIntegracao() integration.Descriptor {
	return descriptorIntegracao{}
}

// Slug is the stable URL/registry key -- "acoustid".
func (descriptorIntegracao) Slug() string { return "acoustid" }

// Nome is the friendly label for the UI.
func (descriptorIntegracao) Nome() string { return "AcoustID" }

// Descricao is the short supporting text for the UI.
func (descriptorIntegracao) Descricao() string {
	return "Fingerprint acústico (Chromaprint) como último fallback do matching de faixa de música -- consulta o AcoustID para identificar áudio sem tag confiável."
}

// Campos describes the client key -- the only credential the LOOKUP requires
// (the personal "user key", used only to SUBMIT fingerprints, is out of scope
// here).
func (descriptorIntegracao) Campos() []integration.Campo {
	return []integration.Campo{
		{
			Nome:   "client_key",
			Label:  "Client Key",
			Tipo:   integration.TipoSegredo,
			EnvVar: "MYMEDIADB_ACOUSTID_CLIENT_KEY",
			Ajuda:  "Client key da aplicação, obtida registrando o app em acoustid.org (gratuito, uso não-comercial). Não é a mesma coisa que a 'user key' pessoal (usada só para submeter fingerprints).",
		},
	}
}

// Configurada delegates to the SAME rule that Credencial.Configurada() already
// uses today (client_key filled in) -- reuse instead of duplication.
func (descriptorIntegracao) Configurada(efetivos map[string]string) bool {
	return Credencial{ClientKey: efetivos["client_key"]}.Configurada()
}
