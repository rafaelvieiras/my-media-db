package midiasfaltantes

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// entityListerFake é um EntityLister fake, sem tocar banco.
type entityListerFake struct {
	ids []string
	err error
}

func (f *entityListerFake) ListarEntitiesComMediaFilePresente(context.Context) ([]string, error) {
	return f.ids, f.err
}

// materializadorFake é um Materializador fake: devolve o resultado
// configurado por entity id, ou um erro para os ids em errPorID.
type materializadorFake struct {
	artesPorID map[string][]string
	nfoPorID   map[string]bool
	errPorID   map[string]error

	chamadas []string
}

func (f *materializadorFake) MaterializarMidiasFaltantes(_ context.Context, entityID string) ([]string, bool, error) {
	f.chamadas = append(f.chamadas, entityID)
	if err := f.errPorID[entityID]; err != nil {
		return nil, false, err
	}
	return f.artesPorID[entityID], f.nfoPorID[entityID], nil
}

// contadorMissingFake é um ContadorMissing fake, sem tocar banco.
type contadorMissingFake struct {
	n   int64
	err error
}

func (f *contadorMissingFake) ContarMediaFilesEmEstadoMissing(context.Context) (int64, error) {
	return f.n, f.err
}

func TestNomeEstavel(t *testing.T) {
	h := New(&entityListerFake{}, &materializadorFake{}, &contadorMissingFake{})
	if h.Nome() != "midias_faltantes" {
		t.Errorf("Nome() = %q, quero \"midias_faltantes\"", h.Nome())
	}
}

// TestExecutarProcessaTodasEResume cobre o caminho feliz: entities com arte
// resolvida e sem NFO ganham as duas coisas, o resumo reflete os totais.
func TestExecutarProcessaTodasEResume(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1", "e2", "e3"}}
	mat := &materializadorFake{
		artesPorID: map[string][]string{
			"e1": {"poster", "fanart"},
			"e2": {}, // sem candidato pra nenhum kind -- não é erro
			"e3": {"poster"},
		},
		nfoPorID: map[string]bool{"e1": true, "e3": false},
		errPorID: map[string]error{},
	}
	h := New(entities, mat, &contadorMissingFake{})

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if len(mat.chamadas) != 3 {
		t.Fatalf("materializador chamado %d vez(es), quero 3", len(mat.chamadas))
	}
	for _, want := range []string{"3 entity", "3 processada", "3 arte(s) materializada", "1 NFO(s) gravado"} {
		if !strings.Contains(msg, want) {
			t.Errorf("mensagem %q não contém %q", msg, want)
		}
	}
}

// TestExecutarErroDeUmaEntityNaoAbortaAsDemais: uma falha em UMA entity
// (rede/disco) não impede o processamento das outras -- best-effort.
func TestExecutarErroDeUmaEntityNaoAbortaAsDemais(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1", "e2"}}
	mat := &materializadorFake{
		artesPorID: map[string][]string{"e2": {"poster"}},
		nfoPorID:   map[string]bool{},
		errPorID:   map[string]error{"e1": errors.New("falha de rede")},
	}
	h := New(entities, mat, &contadorMissingFake{})

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa de UMA entity: %v", err)
	}
	if len(mat.chamadas) != 2 {
		t.Fatalf("materializador chamado %d vez(es), quero 2 (ambas tentadas)", len(mat.chamadas))
	}
	if !strings.Contains(msg, "1 falha") {
		t.Errorf("mensagem %q deveria reportar 1 falha", msg)
	}
	if !strings.Contains(msg, "1 processada") {
		t.Errorf("mensagem %q deveria reportar 1 processada com sucesso", msg)
	}
}

// TestExecutarSemEntitiesElegiveis: universo vazio não é erro.
func TestExecutarSemEntitiesElegiveis(t *testing.T) {
	h := New(&entityListerFake{ids: nil}, &materializadorFake{}, &contadorMissingFake{})
	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if !strings.Contains(msg, "0 entity") {
		t.Errorf("mensagem %q deveria reportar 0 entities elegíveis", msg)
	}
}

// TestExecutarErroDeListagemPropaga: falha ao listar o universo elegível
// (banco fora do ar) propaga o erro -- diferente de falha por-entity.
func TestExecutarErroDeListagemPropaga(t *testing.T) {
	entities := &entityListerFake{err: errors.New("banco indisponível")}
	h := New(entities, &materializadorFake{}, &contadorMissingFake{})
	if _, err := h.Executar(context.Background(), nil); err == nil {
		t.Fatal("Executar deveria propagar o erro de listagem")
	}
}

// TestExecutarResumoIncluiMissingQuandoMaiorQueZero cobre o briefing 0128
// (Caso A): quando há media_file(s) 'missing' no momento do scan, o resumo
// anexa a contagem -- sinal de que o universo elegível desta execução pode
// ter ficado incompleto por uma corrida com outro job sobre o mesmo mount.
func TestExecutarResumoIncluiMissingQuandoMaiorQueZero(t *testing.T) {
	h := New(&entityListerFake{}, &materializadorFake{}, &contadorMissingFake{n: 3})
	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if !strings.Contains(msg, "3 media_file(s) em estado 'missing'") {
		t.Errorf("mensagem %q deveria conter a contagem de missing", msg)
	}
}

// TestExecutarResumoOmiteMissingQuandoZero cobre a contrapartida: sem
// nenhum media_file 'missing', o resumo não ganha ruído extra.
func TestExecutarResumoOmiteMissingQuandoZero(t *testing.T) {
	h := New(&entityListerFake{}, &materializadorFake{}, &contadorMissingFake{n: 0})
	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if strings.Contains(msg, "missing") {
		t.Errorf("mensagem %q não deveria mencionar missing quando a contagem é 0", msg)
	}
}

// TestExecutarResumoOmiteMissingQuandoContagemFalha cobre que uma falha
// best-effort na contagem (banco indisponível no meio da execução) não
// aborta o job nem propaga erro -- só omite o dado do resumo.
func TestExecutarResumoOmiteMissingQuandoContagemFalha(t *testing.T) {
	h := New(&entityListerFake{}, &materializadorFake{}, &contadorMissingFake{err: errors.New("banco indisponível")})
	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa da contagem de missing: %v", err)
	}
	if strings.Contains(msg, "missing") {
		t.Errorf("mensagem %q não deveria mencionar missing quando a contagem falhou", msg)
	}
}
