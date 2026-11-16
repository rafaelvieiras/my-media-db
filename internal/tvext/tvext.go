// Package tvext concentrates the READING of the extension_data of the series
// vertical (namespace core.tv@1, ADR-0021): extract the namespace object and
// coerce the JSON numbers. It is the SINGLE parser reused by the scanner
// (episode index) and by the server (real numbering in the card) — so as not to
// duplicate the decoding of the namespace→object envelope in every consumer.
package tvext

import (
	"database/sql"
	"encoding/json"
)

// Namespace extracts the object of a namespace (e.g. "core.tv@1") from an
// extension_data_json (namespace→object envelope). It returns nil when the raw
// is NULL/empty, unreadable, or does not contain the namespace — the caller
// degrades (the entity without that namespace simply does not have the data).
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

// AsInt coerces a JSON-decoded value to int (JSON numbers become float64 in the
// generic unmarshal). (0,false) when it is not a number.
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
