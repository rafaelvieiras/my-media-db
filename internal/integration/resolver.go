package integration

import (
	"context"
	"encoding/json"
	"os"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Resolver calcula o valor EFETIVO de um Campo (briefing 0095, ADR-0023):
// banco (tabela integration_config) → env var de bootstrap/fallback do Campo
// → ausente. Segue o MESMO padrão de internal/curation.Servico: guarda o
// *db.DB e usa o pool de LEITURA (ADR-0010) — o Resolver nunca escreve; o PUT
// HTTP grava direto via sqlcgen (internal/server/integrations.go), o mesmo
// padrão do resto de internal/server/settings.go.
//
// Efetivo/Valores não devolvem erro de propósito (mesma assinatura do
// briefing): uma falha real de leitura no banco (não apenas "slug nunca
// gravado") degrada em silêncio para a env var, em vez de propagar — reflete
// o princípio de nunca bloquear a integração inteira por uma leitura opcional
// (AGENTS.md §6). O zero-value (*Resolver)(nil) também é seguro: Efetivo
// degrada direto para a env var quando não há banco (ex.: testes de rota sem
// banco injetado).
type Resolver struct {
	database *db.DB
}

// NovoResolver cria o Resolver a partir do pool de banco.
func NovoResolver(database *db.DB) *Resolver {
	return &Resolver{database: database}
}

func (r *Resolver) leitura() *sqlcgen.Queries { return sqlcgen.New(r.database.Read) }

// Efetivo calcula o valor efetivo de UM campo de UMA integração: lê
// integration_config pelo slug, checa a chave campo.Nome no values_json; se
// ausente (linha inexistente, JSON corrompido ou chave não presente), cai em
// os.Getenv(campo.EnvVar) quando EnvVar != ""; se nenhum dos dois, ok=false e
// origem="nenhum".
func (r *Resolver) Efetivo(ctx context.Context, slug string, campo Campo) (valor, origem string, ok bool) {
	if r != nil && r.database != nil {
		if v, achou := r.doBanco(ctx, slug, campo.Nome); achou {
			return v, "banco", true
		}
	}
	if campo.EnvVar != "" {
		if v := os.Getenv(campo.EnvVar); v != "" {
			return v, "env", true
		}
	}
	return "", "nenhum", false
}

// doBanco lê integration_config e devolve o valor gravado do campo `nome`,
// se presente. Qualquer erro de leitura (linha ausente, JSON corrompido) é
// tratado como "não achou no banco" — o chamador cai para a env var, nunca
// propaga erro (ver comentário do Resolver).
func (r *Resolver) doBanco(ctx context.Context, slug, nome string) (string, bool) {
	row, err := r.leitura().ObterIntegrationConfig(ctx, slug)
	if err != nil {
		return "", false
	}
	if row.ValuesJson == "" {
		return "", false
	}
	var valores map[string]string
	if err := json.Unmarshal([]byte(row.ValuesJson), &valores); err != nil {
		return "", false
	}
	v, existe := valores[nome]
	if !existe {
		return "", false
	}
	return v, true
}

// Valores agrega Efetivo para TODOS os campos de um Descriptor de uma vez —
// o formato consumido por Descriptor.Configurada. Campos sem valor efetivo
// (ok=false) ficam de fora do mapa.
func (r *Resolver) Valores(ctx context.Context, slug string, campos []Campo) map[string]string {
	out := make(map[string]string, len(campos))
	for _, c := range campos {
		if v, _, ok := r.Efetivo(ctx, slug, c); ok {
			out[c.Nome] = v
		}
	}
	return out
}
