package identidades

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// alvosFake lists a fixed set of suspects.
type alvosFake struct {
	suspeitas []Suspeita
	err       error
}

func (a alvosFake) ListarIdentidadesSuspeitas(context.Context) ([]Suspeita, error) {
	return a.suspeitas, a.err
}

// sugestorFake answers per entity id: a Resultado, or an error to exercise the failure path.
type sugestorFake struct {
	porID map[string]Resultado
	erro  map[string]error
	visto []string
}

func (s *sugestorFake) SugerirIdentidadeDaSerie(_ context.Context, entityID string) (Resultado, error) {
	s.visto = append(s.visto, entityID)
	if err, ok := s.erro[entityID]; ok {
		return Resultado{}, err
	}
	return s.porID[entityID], nil
}

// TestExecutarResumeOSugeridasESemNovidade: the summary separates the three outcomes, because
// "suggested 4 of 14" is the number that tells the owner the job found something.
func TestExecutarResumeOSugeridasESemNovidade(t *testing.T) {
	sug := &sugestorFake{porID: map[string]Resultado{
		"a": {Sucesso: true, Descricao: "O Clube do Terror (1992) score 100 - 91 episodio(s) para 78 arquivo(s)"},
		"b": {Descricao: "nenhum candidato acima de 80"},
		"c": {Sucesso: true, Descricao: "Arquivo Morto (2003) score 100"},
	}}
	h := New(alvosFake{suspeitas: []Suspeita{{ID: "a", Titulo: "A"}, {ID: "b"}, {ID: "c"}}}, sug)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if !strings.Contains(msg, "3 serie(s)") || !strings.Contains(msg, "2 com identidade sugerida") ||
		!strings.Contains(msg, "1 sem novidade") {
		t.Errorf("mensagem = %q", msg)
	}
	if len(sug.visto) != 3 {
		t.Errorf("sugeriu para %d series, quero 3", len(sug.visto))
	}
}

// TestExecutarSobreviveAFalhaDeUmaSerie: one source that answers garbage must not stop the rest
// -- the same best-effort contract of the other jobs.
func TestExecutarSobreviveAFalhaDeUmaSerie(t *testing.T) {
	sug := &sugestorFake{
		porID: map[string]Resultado{"b": {Sucesso: true, Descricao: "ok"}},
		erro:  map[string]error{"a": errors.New("fonte fora do ar")},
	}
	h := New(alvosFake{suspeitas: []Suspeita{{ID: "a"}, {ID: "b"}}}, sug)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if !strings.Contains(msg, "1 falha(s)") || !strings.Contains(msg, "1 com identidade sugerida") {
		t.Errorf("mensagem = %q", msg)
	}
}

// TestExecutarSemAlvos: an empty census is a success with nothing to say, never an error.
func TestExecutarSemAlvos(t *testing.T) {
	h := New(alvosFake{}, &sugestorFake{})
	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if !strings.Contains(msg, "0 serie(s)") {
		t.Errorf("mensagem = %q", msg)
	}
}

// TestExecutarPropagaFalhaDoCenso: without the list there is nothing to process, so the error
// goes up (the only case that fails the job).
func TestExecutarPropagaFalhaDoCenso(t *testing.T) {
	h := New(alvosFake{err: errors.New("banco fora")}, &sugestorFake{})
	if _, err := h.Executar(context.Background(), nil); err == nil {
		t.Fatal("erro do censo engolido")
	}
}

// TestNomeDoJob: the slug is the contract with the job_definition seeded by migration 0031.
func TestNomeDoJob(t *testing.T) {
	if nome := New(alvosFake{}, &sugestorFake{}).Nome(); nome != "sugerir_identidades" {
		t.Errorf("Nome() = %q, quero sugerir_identidades", nome)
	}
}
