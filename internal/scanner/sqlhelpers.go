package scanner

import "database/sql"

// Helpers de conversão para os tipos nuláveis do sqlc. Concentrados aqui para
// manter as funções de escrita legíveis e de baixa complexidade.

// nsOpt embrulha uma string num sql.NullString, tratando "" como NULO.
func nsOpt(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nsBytes embrulha um []byte (JSON) num sql.NullString, tratando nil/vazio como NULO.
func nsBytes(b []byte) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

// niInt embrulha um int64 num sql.NullInt64 válido.
func niInt(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

// niOptInt embrulha um int como sql.NullInt64, tratando 0 como NULO (ausente).
func niOptInt(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

// nfFloat embrulha um float64 num sql.NullFloat64 válido.
func nfFloat(v float64) sql.NullFloat64 {
	return sql.NullFloat64{Float64: v, Valid: true}
}
