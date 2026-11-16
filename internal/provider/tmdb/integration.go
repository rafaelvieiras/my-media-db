package tmdb

import "github.com/rafaelvieiras/my-media-db/internal/integration"

// descriptorIntegracao implements integration.Descriptor for TMDB (the first
// consumer of the foundation of integrations configurable through the UI/database,
// ADR-0023). It only describes the SCHEMA of the fields (token/api_key) and
// delegates Configurada to the SAME rule that Credencial.Configurada already uses
// today (token OR api_key) -- it does not duplicate the logic.
type descriptorIntegracao struct{}

// NovoDescriptorIntegracao creates the TMDB integration.Descriptor. Registered in
// cmd/mymediadb/main.go through server.WithIntegrations.
func NovoDescriptorIntegracao() integration.Descriptor {
	return descriptorIntegracao{}
}

// Slug is the stable URL/registry key -- "tmdb", never "TMDB".
func (descriptorIntegracao) Slug() string { return "tmdb" }

// Nome is the friendly label for the UI.
func (descriptorIntegracao) Nome() string { return "TMDB" }

// Descricao is the short supporting text for the UI.
func (descriptorIntegracao) Descricao() string {
	return "The Movie Database -- provider de busca/enriquecimento de filmes e series."
}

// Campos describes token (preferred, Read Access Token v4) and api_key
// (v3 alternative) -- the same two fields of Credencial, each one with its own
// bootstrap/fallback env var.
func (descriptorIntegracao) Campos() []integration.Campo {
	return []integration.Campo{
		{
			Nome:   "token",
			Label:  "Read Access Token (v4)",
			Tipo:   integration.TipoSegredo,
			EnvVar: "MYMEDIADB_TMDB_TOKEN",
			Ajuda:  "Preferido sobre api_key -- token Bearer da API v4 do TMDB (Settings > API no site do TMDB).",
		},
		{
			Nome:   "api_key",
			Label:  "API Key (v3)",
			Tipo:   integration.TipoSegredo,
			EnvVar: "MYMEDIADB_TMDB_API_KEY",
			Ajuda:  "Alternativa ao token, para quem só tem a chave v3.",
		},
	}
}

// Configurada delegates to the SAME rule that Credencial.Configurada() already
// uses today (token OR api_key filled in) -- reuse instead of duplication.
func (descriptorIntegracao) Configurada(efetivos map[string]string) bool {
	return Credencial{Token: efetivos["token"], APIKey: efetivos["api_key"]}.Configurada()
}
