// Package integration define a FUNDAÇÃO de config de integração (briefing
// 0095, ADR-0023): um registro genérico, gravável em banco, de credenciais e
// parâmetros de provider — substituindo o hardcode de variável de ambiente
// lida uma única vez no boot. Espelha o espírito de internal/provider
// (Registry N-fontes, zero-value utilizável), mas para CONFIG em vez de
// COMPORTAMENTO: um Descriptor descreve o SCHEMA de campos de uma integração
// (ex.: TMDB → token/api_key); quem calcula o VALOR efetivo de cada campo
// (banco → env var → ausente) é o Resolver (resolver.go).
//
// Este pacote não sabe nada de HTTP nem de sqlc diretamente no seu tipo
// central (Descriptor/Campo são dados puros); só o Resolver toca o banco.
package integration

import "context"

// TipoCampo classifica como a UI (briefing 0096) deve renderizar um Campo —
// puramente um hint de apresentação, não afeta a resolução de valor.
type TipoCampo string

const (
	// TipoTexto é um campo de texto livre, não-sensível.
	TipoTexto TipoCampo = "text"
	// TipoSegredo é um campo sensível (credencial/chave): a API NUNCA devolve
	// o valor cru, só `definido`/`origem` (ver GET/PUT em internal/server/
	// integrations.go).
	TipoSegredo TipoCampo = "secret"
	// TipoBool é um toggle on/off.
	TipoBool TipoCampo = "bool"
	// TipoNumero é um campo numérico.
	TipoNumero TipoCampo = "number"
	// TipoSelect é uma escolha entre Opcoes fixas.
	TipoSelect TipoCampo = "select"
)

// Opcao é uma alternativa de um Campo TipoSelect.
type Opcao struct {
	Valor string
	Label string
}

// Campo descreve UM campo configurável de uma integração — schema estático,
// não o valor. EnvVar é a env var de bootstrap/fallback deste campo
// especificamente ("" = nenhuma, o campo só pode vir do banco).
type Campo struct {
	// Nome é a chave estável do campo (usada como chave em values_json e na
	// URL do PUT — ex.: "token", "api_key").
	Nome string
	// Label é o rótulo amigável para a UI.
	Label string
	// Tipo classifica a renderização (ver TipoCampo).
	Tipo TipoCampo
	// Obrigatorio é um HINT de UI (asterisco); NÃO decide "configurada" — cada
	// Descriptor decide sua própria regra em Configurada (ver comentário lá).
	Obrigatorio bool
	// Ajuda é um texto de apoio opcional (ex.: onde obter a chave).
	Ajuda string
	// Placeholder é o texto de exemplo opcional do campo.
	Placeholder string
	// EnvVar é a env var de bootstrap/fallback deste campo. "" = nenhuma (o
	// campo só pode vir do banco, sem fallback de ambiente).
	EnvVar string
	// Opcoes só é usado quando Tipo == TipoSelect.
	Opcoes []Opcao
}

// Descriptor é o SCHEMA de uma integração configurável — implementado por
// cada consumidor (ex.: internal/provider/tmdb.NovoDescriptorIntegracao()).
// Não persiste nada nem resolve valor — só descreve. O Registry (registry.go)
// agrupa Descriptors; o Resolver (resolver.go) calcula valores efetivos.
type Descriptor interface {
	// Slug identifica a integração de forma estável — é a chave de URL
	// (/api/v1/integrations/{slug}) e de registro em integration_config.
	// Escolhido para durar (ex.: "tmdb", nunca "TMDB"/"tmdb-provider").
	Slug() string
	// Nome é o rótulo amigável para a UI (ex.: "TMDB").
	Nome() string
	// Descricao é um texto curto explicando a integração.
	Descricao() string
	// Campos lista o schema de campos desta integração.
	Campos() []Campo
	// Configurada decide, a partir dos valores EFETIVOS (banco-ou-env por
	// campo, já resolvidos pelo Resolver.Valores), se a integração está
	// operante. Cada Descriptor decide sua própria regra (ex.: TMDB aceita
	// token OU api_key) — de propósito não é o framework quem decide, para
	// não inventar uma DSL de obrigatoriedade condicional.
	Configurada(efetivos map[string]string) bool
}

// Testador é uma capacidade OPCIONAL (type-assert, mesmo padrão das
// capacidades de internal/provider) que permite testar a conexão com os
// valores dados, sem persistir nada. NÃO implementada nesta entrega — só a
// interface, para não quebrar o type-assert do 0096/follow-up quando algum
// Descriptor a implementar (endpoint POST /integrations/{slug}/test, fora de
// escopo do briefing 0095).
type Testador interface {
	Testar(ctx context.Context, valores map[string]string) error
}
