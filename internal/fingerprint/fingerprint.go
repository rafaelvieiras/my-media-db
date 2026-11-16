// Package fingerprint is the fpcalc wrapper (Chromaprint): it runs the binary
// as a subprocess with JSON output and returns the acoustic fingerprint plus the
// duration of the file, for a query to AcoustID
// (internal/provider/acoustid) — step 4.5 of the music matching pipeline
// (internal/scanner/musica.go), triggered only when the embedded MBID and the
// textual search have already failed.
//
// fpcalc is a RUNTIME dependency, not a build one (same pattern as
// internal/mediainfo for ffprobe, ADR-0014/ADR-0025): its presence is checked at
// boot; absence DEGRADES (the scanner skips step 4.5 and logs a warning), it
// never takes the scan down. The binary path and the execution function are
// injectable to allow tests without the real fpcalc.
package fingerprint

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// runner runs a command and returns stdout. Injectable for tests (same mold as
// internal/mediainfo.runner).
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Calculador runs fpcalc. Useless zero value: use NewCalculador. Binary and run
// are injectable (NewCalculadorComRunner) to test without the real fpcalc.
type Calculador struct {
	binary string
	run    runner
}

// NewCalculador creates a Calculador that calls the "fpcalc" from PATH through
// os/exec.
func NewCalculador() *Calculador {
	return NewCalculadorComRunner("fpcalc", execRunner)
}

// NewCalculadorComRunner creates a Calculador with an explicit binary and
// runner (tests).
func NewCalculadorComRunner(binary string, run runner) *Calculador {
	return &Calculador{binary: binary, run: run}
}

// execRunner is the production runner: it runs the command and captures stdout.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Disponivel tells whether the fpcalc binary is in the PATH. The boot (and the
// scanner, indirectly through the conditionally registered provider) uses it to
// log a warning and degrade (FingerprintProvider capability not registered)
// instead of failing.
func (c *Calculador) Disponivel() bool {
	if _, err := exec.LookPath(c.binary); err != nil {
		return false
	}
	return true
}

// CalcularFingerprint runs fpcalc on the file `path` and returns the acoustic
// fingerprint (base64, Chromaprint compact format) and the duration in seconds
// that fpcalc ITSELF measured (independent of mediainfo/ffprobe — AcoustID
// requires the duration REPORTED ALONGSIDE the fingerprint, not one from another
// source). It errors if the subprocess or the parse fails — the caller (scanner)
// decides to degrade (falling back to the draft step) instead of aborting the
// whole scan.
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

// fpcalcJSON is the raw output of `fpcalc -json` — only the two fields used
// (the tool returns nothing else relevant to this package).
type fpcalcJSON struct {
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}
