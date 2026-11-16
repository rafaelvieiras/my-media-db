package tmdb

import "testing"

// TestDescriptorIntegracaoSlugENome covers the basic shape of the Descriptor.
func TestDescriptorIntegracaoSlugENome(t *testing.T) {
	d := NovoDescriptorIntegracao()
	if d.Slug() != "tmdb" {
		t.Errorf("Slug() = %q, quero tmdb", d.Slug())
	}
	if d.Nome() == "" {
		t.Error("Nome() não deveria ser vazio")
	}
	if d.Descricao() == "" {
		t.Error("Descricao() não deveria ser vazia")
	}
}

// TestDescriptorIntegracaoCampos covers the two expected fields (token and
// api_key), each one secret with its own env var.
func TestDescriptorIntegracaoCampos(t *testing.T) {
	campos := NovoDescriptorIntegracao().Campos()
	if len(campos) != 2 {
		t.Fatalf("Campos() tem %d campos, quero 2", len(campos))
	}
	porNome := map[string]string{}
	for _, c := range campos {
		porNome[c.Nome] = c.EnvVar
		if string(c.Tipo) != "secret" {
			t.Errorf("campo %q tipo = %q, quero secret", c.Nome, c.Tipo)
		}
	}
	if porNome["token"] != "MYMEDIADB_TMDB_TOKEN" {
		t.Errorf("token.EnvVar = %q, quero MYMEDIADB_TMDB_TOKEN", porNome["token"])
	}
	if porNome["api_key"] != "MYMEDIADB_TMDB_API_KEY" {
		t.Errorf("api_key.EnvVar = %q, quero MYMEDIADB_TMDB_API_KEY", porNome["api_key"])
	}
}

// TestDescriptorIntegracaoConfigurada covers the rule "token OR api_key" (the SAME
// one Credencial.Configurada already uses), including the empty case.
func TestDescriptorIntegracaoConfigurada(t *testing.T) {
	d := NovoDescriptorIntegracao()

	casos := []struct {
		nome      string
		efetivos  map[string]string
		querFinal bool
	}{
		{"vazio", map[string]string{}, false},
		{"so token", map[string]string{"token": "abc"}, true},
		{"so api_key", map[string]string{"api_key": "abc"}, true},
		{"ambos", map[string]string{"token": "abc", "api_key": "def"}, true},
	}
	for _, c := range casos {
		if got := d.Configurada(c.efetivos); got != c.querFinal {
			t.Errorf("%s: Configurada(%v) = %v, quero %v", c.nome, c.efetivos, got, c.querFinal)
		}
	}
}
