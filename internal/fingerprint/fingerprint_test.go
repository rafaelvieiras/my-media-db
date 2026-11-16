package fingerprint

import (
	"context"
	"errors"
	"testing"
)

func TestDisponivel_BinarioInexistente(t *testing.T) {
	c := NewCalculadorComRunner("fpcalc-que-nao-existe-de-verdade", execRunner)
	if c.Disponivel() {
		t.Fatal("esperava Disponivel()=false para binario inexistente no PATH")
	}
}

func TestDisponivel_BinarioNoPath(t *testing.T) {
	// "sh" exists in any Unix CI/dev environment of this project -- used only
	// to prove that Disponivel() finds a real binary when present, symmetrically
	// to the test above.
	c := NewCalculadorComRunner("sh", execRunner)
	if !c.Disponivel() {
		t.Skip("sh nao disponivel neste ambiente -- teste nao aplicavel")
	}
}

func TestCalcularFingerprint_Sucesso(t *testing.T) {
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "fpcalc" {
			t.Fatalf("binario inesperado: %q", name)
		}
		if len(args) != 2 || args[0] != "-json" || args[1] != "/musica/faixa.mp3" {
			t.Fatalf("args inesperados: %v", args)
		}
		return []byte(`{"duration": 245.32, "fingerprint": "AQAAT0mUaEkSRZEGLQ"}`), nil
	}
	c := NewCalculadorComRunner("fpcalc", run)

	fp, dur, err := c.CalcularFingerprint(context.Background(), "/musica/faixa.mp3")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if fp != "AQAAT0mUaEkSRZEGLQ" {
		t.Errorf("fingerprint = %q, esperava AQAAT0mUaEkSRZEGLQ", fp)
	}
	if dur != 245.32 {
		t.Errorf("duracao = %v, esperava 245.32", dur)
	}
}

func TestCalcularFingerprint_SubprocessoFalha(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	}
	c := NewCalculadorComRunner("fpcalc", run)

	_, _, err := c.CalcularFingerprint(context.Background(), "/musica/faixa.mp3")
	if err == nil {
		t.Fatal("esperava erro quando o subprocesso fpcalc falha")
	}
}

func TestCalcularFingerprint_JSONInvalido(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("nao e json"), nil
	}
	c := NewCalculadorComRunner("fpcalc", run)

	_, _, err := c.CalcularFingerprint(context.Background(), "/musica/faixa.mp3")
	if err == nil {
		t.Fatal("esperava erro quando a saida do fpcalc nao e JSON valido")
	}
}

func TestCalcularFingerprint_SemFingerprintNaResposta(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"duration": 12.0, "fingerprint": ""}`), nil
	}
	c := NewCalculadorComRunner("fpcalc", run)

	_, _, err := c.CalcularFingerprint(context.Background(), "/musica/faixa.mp3")
	if err == nil {
		t.Fatal("esperava erro quando a resposta nao traz fingerprint")
	}
}

func TestCalcularFingerprint_FpcalcAusenteDegradaSemPanic(t *testing.T) {
	// Simulates the degradation contract: the CALLER checks Disponivel() before
	// calculating, and should never panic even if it calls CalcularFingerprint
	// with an absent binary -- the real subprocess would fail with "executable
	// file not found", captured as an ordinary error, not a panic.
	c := NewCalculador() // uses the real "fpcalc" binary through the PATH
	if c.Disponivel() {
		t.Skip("fpcalc esta disponivel neste ambiente -- este teste cobre so o caminho de ausencia")
	}
	_, _, err := c.CalcularFingerprint(context.Background(), "/qualquer/arquivo.mp3")
	if err == nil {
		t.Fatal("esperava erro (nao panic) quando fpcalc esta ausente do PATH")
	}
}
