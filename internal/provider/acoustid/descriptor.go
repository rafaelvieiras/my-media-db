package acoustid

import "github.com/rafaelvieiras/my-media-db/internal/integration"

// descriptorIntegracao implementa integration.Descriptor para o AcoustID
// (briefing 0126, mesmo padrão de internal/provider/tmdb.descriptorIntegracao
// -- primeiro consumidor foi o TMDB, briefing 0095). Só descreve o SCHEMA de
// campos (um único campo, a client key) e delega Configurada à MESMA regra
// que Credencial.Configurada já usa -- não duplica a lógica.
type descriptorIntegracao struct{}

// NovoDescriptorIntegracao cria o integration.Descriptor do AcoustID.
// Registrado em cmd/mymediadb/main.go via server.WithIntegrations.
func NovoDescriptorIntegracao() integration.Descriptor {
	return descriptorIntegracao{}
}

// Slug é a chave estável de URL/registro -- "acoustid".
func (descriptorIntegracao) Slug() string { return "acoustid" }

// Nome é o rótulo amigável para a UI (briefing 0096).
func (descriptorIntegracao) Nome() string { return "AcoustID" }

// Descricao é o texto curto de apoio da UI.
func (descriptorIntegracao) Descricao() string {
	return "Fingerprint acustico (Chromaprint) como ultimo fallback do matching de faixa de musica -- consulta o AcoustID para identificar audio sem tag confiavel."
}

// Campos descreve a client key -- a única credencial que o LOOKUP exige (a
// "user key" pessoal, usada só para SUBMETER fingerprints, está fora de
// escopo deste briefing).
func (descriptorIntegracao) Campos() []integration.Campo {
	return []integration.Campo{
		{
			Nome:   "client_key",
			Label:  "Client Key",
			Tipo:   integration.TipoSegredo,
			EnvVar: "MYMEDIADB_ACOUSTID_CLIENT_KEY",
			Ajuda:  "Client key da aplicacao, obtida registrando o app em acoustid.org (gratuito, uso nao-comercial). Nao e a mesma coisa que a 'user key' pessoal (usada so para submeter fingerprints).",
		},
	}
}

// Configurada delega à MESMA regra que Credencial.Configurada() já usa hoje
// (client_key preenchida) -- reusa em vez de duplicar.
func (descriptorIntegracao) Configurada(efetivos map[string]string) bool {
	return Credencial{ClientKey: efetivos["client_key"]}.Configurada()
}
