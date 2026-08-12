package trailertema

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

// baixadorFake é um TrailerTemaBaixador fake: devolve o resultado
// configurado por entity id, para trailer e tema separadamente.
type baixadorFake struct {
	ytdlpDisponivel bool
	ytdlpDetalhe    string

	trailerPorID    map[string]bool
	trailerErrPorID map[string]error
	temaPorID       map[string]bool
	temaErrPorID    map[string]error

	chamadasTrailer []string
	chamadasTema    []string
}

func (f *baixadorFake) YtdlpDisponivel(context.Context) (bool, string) {
	return f.ytdlpDisponivel, f.ytdlpDetalhe
}

func (f *baixadorFake) BaixarTrailerEntity(_ context.Context, entityID string) (bool, error) {
	f.chamadasTrailer = append(f.chamadasTrailer, entityID)
	if err := f.trailerErrPorID[entityID]; err != nil {
		return false, err
	}
	return f.trailerPorID[entityID], nil
}

func (f *baixadorFake) BaixarTemaEntity(_ context.Context, entityID string) (bool, error) {
	f.chamadasTema = append(f.chamadasTema, entityID)
	if err := f.temaErrPorID[entityID]; err != nil {
		return false, err
	}
	return f.temaPorID[entityID], nil
}

func novoBaixadorFakeDisponivel() *baixadorFake {
	return &baixadorFake{
		ytdlpDisponivel: true,
		trailerPorID:    map[string]bool{},
		trailerErrPorID: map[string]error{},
		temaPorID:       map[string]bool{},
		temaErrPorID:    map[string]error{},
	}
}

func TestNomeEstavel(t *testing.T) {
	h := New(&entityListerFake{}, novoBaixadorFakeDisponivel())
	if h.Nome() != "trailer_tema_faltantes" {
		t.Errorf("Nome() = %q, quero \"trailer_tema_faltantes\"", h.Nome())
	}
}

// TestExecutarProcessaTodasEResume cobre o caminho feliz: trailer/tema
// baixados em algumas entities, resumo reflete os totais.
func TestExecutarProcessaTodasEResume(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1", "e2", "e3"}}
	baix := novoBaixadorFakeDisponivel()
	baix.trailerPorID = map[string]bool{"e1": true, "e3": true}
	baix.temaPorID = map[string]bool{"e1": true}
	h := New(entities, baix)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if len(baix.chamadasTrailer) != 3 || len(baix.chamadasTema) != 3 {
		t.Fatalf("baixador chamado trailer=%d tema=%d, quero 3/3", len(baix.chamadasTrailer), len(baix.chamadasTema))
	}
	for _, want := range []string{"3 entity", "2 trailer(s) baixado", "1 tema(s) musical"} {
		if !strings.Contains(msg, want) {
			t.Errorf("mensagem %q não contém %q", msg, want)
		}
	}
}

// TestExecutarErroDeUmaEntityNaoAbortaAsDemais: uma falha REAL (rede/banco)
// numa entity não impede o processamento das outras -- best-effort.
func TestExecutarErroDeUmaEntityNaoAbortaAsDemais(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1", "e2"}}
	baix := novoBaixadorFakeDisponivel()
	baix.trailerErrPorID = map[string]error{"e1": errors.New("falha de rede")}
	baix.trailerPorID = map[string]bool{"e2": true}
	h := New(entities, baix)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar por causa de UMA entity: %v", err)
	}
	if len(baix.chamadasTrailer) != 2 {
		t.Fatalf("baixador chamado %d vez(es), quero 2 (ambas tentadas)", len(baix.chamadasTrailer))
	}
	if !strings.Contains(msg, "1 falha") {
		t.Errorf("mensagem %q deveria reportar 1 falha", msg)
	}
	if !strings.Contains(msg, "1 trailer(s) baixado") {
		t.Errorf("mensagem %q deveria reportar 1 trailer baixado", msg)
	}
}

// TestExecutarSemEntitiesElegiveis: universo vazio não é erro.
func TestExecutarSemEntitiesElegiveis(t *testing.T) {
	h := New(&entityListerFake{ids: nil}, novoBaixadorFakeDisponivel())
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
	h := New(entities, novoBaixadorFakeDisponivel())
	if _, err := h.Executar(context.Background(), nil); err == nil {
		t.Fatal("Executar deveria propagar o erro de listagem")
	}
}

// TestExecutarYtdlpIndisponivelNaoTentaNada cobre o critério do briefing
// 0104 §4: yt-dlp indisponível é checado UMA VEZ no início -- sem erro
// fatal, mensagem informativa, NENHUMA entity é sequer listada/tentada.
func TestExecutarYtdlpIndisponivelNaoTentaNada(t *testing.T) {
	entities := &entityListerFake{ids: []string{"e1", "e2"}}
	baix := novoBaixadorFakeDisponivel()
	baix.ytdlpDisponivel = false
	baix.ytdlpDetalhe = "binario nao encontrado"
	h := New(entities, baix)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar não deveria falhar quando yt-dlp está indisponível: %v", err)
	}
	if len(baix.chamadasTrailer) != 0 || len(baix.chamadasTema) != 0 {
		t.Fatalf("nenhuma entity deveria ser tentada com yt-dlp indisponível, chamou trailer=%d tema=%d",
			len(baix.chamadasTrailer), len(baix.chamadasTema))
	}
	if !strings.Contains(msg, "indisponivel") {
		t.Errorf("mensagem %q deveria mencionar yt-dlp indisponivel", msg)
	}
	if !strings.Contains(msg, "binario nao encontrado") {
		t.Errorf("mensagem %q deveria propagar o detalhe", msg)
	}
}
