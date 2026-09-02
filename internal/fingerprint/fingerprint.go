// Package fingerprint é o wrapper do fpcalc (Chromaprint, briefing 0126):
// roda o binário como subprocesso com saída JSON e devolve a impressão
// digital acústica + a duração do arquivo, para consulta ao AcoustID
// (internal/provider/acoustid) — o passo 4.5 do pipeline de matching de
// música (internal/scanner/musica.go), disparado só quando MBID embutido e
// busca textual já falharam.
//
// fpcalc é dependência de RUNTIME, não de build (mesmo padrão de
// internal/mediainfo para o ffprobe, ADR-0014/ADR-0025): a presença é
// checada no boot; a ausência DEGRADA (scanner pula o passo 4.5, loga um
// aviso), nunca derruba o scan. O caminho do binário e a função de execução
// são injetáveis para permitir testes sem o fpcalc real.
package fingerprint

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// runner executa um comando e devolve o stdout. Injetável para testes (mesmo
// molde de internal/mediainfo.runner).
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Calculador roda o fpcalc. Zero-value inútil: use NewCalculador. Binary e
// run são injetáveis (NewCalculadorComRunner) para testar sem o fpcalc real.
type Calculador struct {
	binary string
	run    runner
}

// NewCalculador cria um Calculador que chama o "fpcalc" do PATH via os/exec.
func NewCalculador() *Calculador {
	return NewCalculadorComRunner("fpcalc", execRunner)
}

// NewCalculadorComRunner cria um Calculador com binário e runner explícitos
// (testes).
func NewCalculadorComRunner(binary string, run runner) *Calculador {
	return &Calculador{binary: binary, run: run}
}

// execRunner é o runner de produção: roda o comando e captura o stdout.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Disponivel diz se o binário do fpcalc está no PATH. O boot (e o scanner,
// indiretamente via provider registrado condicionalmente) usa isso para
// logar um aviso e degradar (capacidade FingerprintProvider não registrada)
// em vez de falhar.
func (c *Calculador) Disponivel() bool {
	if _, err := exec.LookPath(c.binary); err != nil {
		return false
	}
	return true
}

// CalcularFingerprint roda o fpcalc no arquivo `path` e devolve a impressão
// digital acústica (base64, formato Chromaprint compact) e a duração em
// segundos que o PRÓPRIO fpcalc mediu (independente do mediainfo/ffprobe —
// o AcoustID exige a duração REPORTADA JUNTO do fingerprint, não uma de
// outra fonte). Erro se o subprocesso ou o parse falharem — o chamador
// (scanner) decide degradar (cai para o passo de draft) em vez de abortar o
// scan inteiro.
func (c *Calculador) CalcularFingerprint(ctx context.Context, path string) (fingerprintStr string, duracaoSegundos float64, err error) {
	out, err := c.run(ctx, c.binary, "-json", path)
	if err != nil {
		return "", 0, fmt.Errorf("fingerprint: fpcalc falhou em %q: %w", path, err)
	}
	var resp fpcalcJSON
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", 0, fmt.Errorf("fingerprint: saida do fpcalc invalida para %q: %w", path, err)
	}
	if strings.TrimSpace(resp.Fingerprint) == "" {
		return "", 0, fmt.Errorf("fingerprint: fpcalc nao devolveu fingerprint para %q", path)
	}
	return resp.Fingerprint, resp.Duration, nil
}

// fpcalcJSON é a saída crua de `fpcalc -json` — só os dois campos usados
// (a ferramenta não devolve mais nada de relevante para este pacote).
type fpcalcJSON struct {
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}
