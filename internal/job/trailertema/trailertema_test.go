package trailertema

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// entityListerFake is a fake EntityLister, without touching the database.
type entityListerFake struct {
	ids []string
	err error
}

func (f *entityListerFake) ListarEntitiesComMediaFilePresente(context.Context) ([]string, error) {
	return f.ids, f.err
}

// baixadorFake is a fake TrailerTemaBaixador: it returns the configured result
// per entity id, for trailer and theme separately.
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

// TestExecutarProcessaTodasEResume covers the happy path: trailer/theme
// downloaded for some entities, the summary reflects the totals.
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

// TestExecutarErroDeUmaEntityNaoAbortaAsDemais: a REAL failure (network/database)
// on one entity does not prevent the others from being processed -- best-effort.
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

// TestExecutarSemEntitiesElegiveis: an empty universe is not an error.
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

// TestExecutarErroDeListagemPropaga: a failure to list the eligible universe
// (database down) propagates the error -- unlike a per-entity failure.
func TestExecutarErroDeListagemPropaga(t *testing.T) {
	entities := &entityListerFake{err: errors.New("banco indisponível")}
	h := New(entities, novoBaixadorFakeDisponivel())
	if _, err := h.Executar(context.Background(), nil); err == nil {
		t.Fatal("Executar deveria propagar o erro de listagem")
	}
}

// TestExecutarYtdlpIndisponivelNaoTentaNada covers the criterion for the yt-dlp
// availability check: an unavailable yt-dlp is checked ONCE at the start -- no
// fatal error, an informative message, and NO entity is even listed/tried.
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
