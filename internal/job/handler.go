// Package job é a fundação do sistema de jobs (briefing 0052): tipos de job
// registrados em código Go (Handler), configuração dinâmica e histórico
// genérico persistidos no banco (job_definition/job_execution), agendador
// embutido (goroutine + ticker, sem depender de cron do SO). É a base
// explícita do futuro sistema de plugins: um novo tipo de job só precisa
// registrar seu Handler (Registry) e semear uma linha de job_definition — sem
// tocar o schema nem os endpoints (ADR-0005: núcleo agnóstico ao formato do
// config_json de cada tipo).
package job

import (
	"context"
	"encoding/json"
)

// Handler é o contrato que QUALQUER tipo de job implementa — o job semente
// backup_vacuum (internal/job/backup), o futuro job de saúde da biblioteca,
// um eventual plugin de terceiro, etc. Nome() é o slug ESTÁVEL que casa com
// job_definition.id (dado curado via seed/migration, nunca gerado em
// runtime — como Handler.Nome() de provider.Provider casa com o nome da
// fonte).
type Handler interface {
	// Nome devolve o slug estável do tipo de job (ex.: "backup_vacuum").
	Nome() string

	// Executar roda o trabalho de fato. cfg é o config_json BRUTO da
	// definição (pode ser vazio/nil quando a config está ausente) — cada
	// Handler interpreta o seu próprio formato, namespaced, sem o núcleo
	// conhecê-lo. Em sucesso, mensagem vira job_execution.message (texto
	// curto, ex.: "backup gravado em ... (7 mantidos)"); em erro, err vira a
	// mensagem de falha (sem stack trace).
	Executar(ctx context.Context, cfg json.RawMessage) (mensagem string, err error)
}
