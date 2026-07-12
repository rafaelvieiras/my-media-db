// Package tvext concentra a LEITURA do extension_data da vertical de séries
// (namespace core.tv@1, ADR-0021): extrair o objeto do namespace e coagir os
// números de JSON. É o parser ÚNICO reusado pelo scanner (índice de episódios,
// 0034) e pelo server (numbering real no card, 0039) — para não duplicar a
// decodificação do envelope namespace→objeto em cada consumidor.
package tvext

import (
	"database/sql"
	"encoding/json"
)

// Namespace extrai o objeto de um namespace (ex.: "core.tv@1") de um
// extension_data_json (envelope namespace→objeto). Devolve nil quando o raw é
// NULL/vazio, ilegível, ou não contém o namespace — o chamador degrada (a
// entity sem aquele namespace simplesmente não tem os dados).
func Namespace(raw sql.NullString, ns string) map[string]any {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw.String), &env); err != nil {
		return nil
	}
	bruto, ok := env[ns]
	if !ok {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(bruto, &m); err != nil {
		return nil
	}
	return m
}

// AsInt coage um valor decodificado de JSON para int (números de JSON viram
// float64 no unmarshal genérico). (0,false) quando não é número.
func AsInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}
