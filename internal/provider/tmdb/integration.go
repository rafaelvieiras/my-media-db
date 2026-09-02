package tmdb

import "github.com/rafaelvieiras/my-media-db/internal/integration"

// descriptorIntegracao implementa integration.Descriptor para o TMDB
// (briefing 0095, primeiro consumidor da fundação de integrações
// configuráveis via UI/banco, ADR-0023). Só descreve o SCHEMA de campos
// (token/api_key) e delega Configurada à MESMA regra que Credencial.
// Configurada já usa hoje (token OU api_key) -- não duplica a lógica.
type descriptorIntegracao struct{}

// NovoDescriptorIntegracao cria o integration.Descriptor do TMDB. Registrado
// em cmd/mymediadb/main.go via server.WithIntegrations.
func NovoDescriptorIntegracao() integration.Descriptor {
	return descriptorIntegracao{}
}

// Slug é a chave estável de URL/registro -- "tmdb", nunca "TMDB".
func (descriptorIntegracao) Slug() string { return "tmdb" }

// Nome é o rótulo amigável para a UI (briefing 0096).
func (descriptorIntegracao) Nome() string { return "TMDB" }

// Descricao é o texto curto de apoio da UI.
func (descriptorIntegracao) Descricao() string {
	return "The Movie Database -- provider de busca/enriquecimento de filmes e series."
}

// Campos descreve token (preferido, Read Access Token v4) e api_key
// (alternativa v3) -- mesmos dois campos de Credencial, cada um com sua
// própria env var de bootstrap/fallback.
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

// Configurada delega à MESMA regra que Credencial.Configurada() já usa hoje
// (token OU api_key preenchido) -- reusa em vez de duplicar.
func (descriptorIntegracao) Configurada(efetivos map[string]string) bool {
	return Credencial{Token: efetivos["token"], APIKey: efetivos["api_key"]}.Configurada()
}
