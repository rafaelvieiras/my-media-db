package scanner

import "database/sql"

// Conversion helpers for the nullable types of sqlc. Concentrated here to keep the
// write functions readable and of low complexity.

// nsOpt wraps a string in a sql.NullString, treating "" as NULL.
func nsOpt(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nsBytes wraps a []byte (JSON) in a sql.NullString, treating nil/empty as NULL.
func nsBytes(b []byte) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

// niInt wraps an int64 in a valid sql.NullInt64.
func niInt(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

// niOptInt wraps an int in a sql.NullInt64, treating 0 as NULL (absent).
func niOptInt(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

// nfFloat wraps a float64 in a valid sql.NullFloat64.
func nfFloat(v float64) sql.NullFloat64 {
	return sql.NullFloat64{Float64: v, Valid: true}
}
