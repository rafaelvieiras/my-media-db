package tvext

import (
	"database/sql"
	"testing"
)

// TestNamespace covers the extraction of the core.tv@1 namespace object:
// present, NULL/empty, invalid JSON and absent namespace (all degrading to nil,
// except the happy case).
func TestNamespace(t *testing.T) {
	casos := []struct {
		nome    string
		raw     sql.NullString
		querNil bool
	}{
		{"null", sql.NullString{}, true},
		{"vazio", sql.NullString{Valid: true, String: ""}, true},
		{"json invalido", sql.NullString{Valid: true, String: "{"}, true},
		{"ns ausente", sql.NullString{Valid: true, String: `{"outro@1":{"a":1}}`}, true},
		{"presente", sql.NullString{Valid: true, String: `{"core.tv@1":{"ordering":"official"}}`}, false},
	}
	for _, c := range casos {
		got := Namespace(c.raw, "core.tv@1")
		if c.querNil && got != nil {
			t.Errorf("%s: Namespace = %v, quero nil", c.nome, got)
		}
		if !c.querNil {
			if got == nil {
				t.Fatalf("%s: Namespace = nil, quero o objeto", c.nome)
			}
			if got["ordering"] != "official" {
				t.Errorf("%s: ordering = %v, quero \"official\"", c.nome, got["ordering"])
			}
		}
	}
}

// TestAsInt covers the coercion of JSON numbers (float64) and the rejection of
// non-numbers.
func TestAsInt(t *testing.T) {
	if n, ok := AsInt(float64(5)); !ok || n != 5 {
		t.Errorf("AsInt(float64(5)) = %d,%v; quero 5,true", n, ok)
	}
	if n, ok := AsInt(int64(7)); !ok || n != 7 {
		t.Errorf("AsInt(int64(7)) = %d,%v; quero 7,true", n, ok)
	}
	if n, ok := AsInt(3); !ok || n != 3 {
		t.Errorf("AsInt(3) = %d,%v; quero 3,true", n, ok)
	}
	if _, ok := AsInt("nao-numero"); ok {
		t.Errorf("AsInt(string) devia ser false")
	}
	if _, ok := AsInt(nil); ok {
		t.Errorf("AsInt(nil) devia ser false")
	}
}
