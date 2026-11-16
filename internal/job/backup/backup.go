// Package backup implements the Handler of the seed job `backup_vacuum`: it
// ports the logic of scripts/backup.sh into the binary — VACUUM INTO (a
// consistent copy even with the app up under WAL, ADR-0010, with no downtime)
// followed by rotation keeping the `keep` most recent snapshots. It is the
// end-to-end PROOF of the job system: a real Handler, not a sample job.
package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// nomeJob is the Handler.Nome() — it matches the slug seeded in job_definition
// by migration 0011.
const nomeJob = "backup_vacuum"

// keepPadrao is the number of snapshots kept when config_json is
// absent/invalid/keep<=0 — it mirrors the default of scripts/backup.sh
// (MMDB_KEEP=7).
const keepPadrao = 7

// prefixoArquivo/sufixoArquivo make up the snapshot name:
// mymediadb-<timestamp>.sqlite (the same convention as backup.sh).
const (
	prefixoArquivo = "mymediadb-"
	sufixoArquivo  = ".sqlite"
)

// carimboFormato is the timestamp layout in the file name — with no colons
// (compatible with any filesystem) and lexically sortable, the same note as in
// backup.sh.
const carimboFormato = "20060102-150405"

// Handler is the Handler of the backup_vacuum job. db is the WRITE pool (the
// single writer of ADR-0010; VACUUM INTO does not block readers, but it is
// serialized through the same connection as everything else). dir is the backup
// directory already resolved by the caller (env MYMEDIADB_BACKUP_DIR or
// dirname(DB)/backups — the same convention as resolveAssetsDir in main.go).
type Handler struct {
	db  *sql.DB
	dir string
	now func() time.Time
}

// New creates the Handler with the write pool and the backup directory.
func New(database *sql.DB, dir string) *Handler {
	return &Handler{db: database, dir: dir, now: func() time.Time { return time.Now().UTC() }}
}

// Nome returns the stable slug of the job.
func (h *Handler) Nome() string { return nomeJob }

// config is the namespaced config_json of the job: {"keep": N}.
type config struct {
	Keep int `json:"keep"`
}

// Executar writes a consistent snapshot via VACUUM INTO and rotates, keeping
// the `keep` most recent ones (from config_json, default keepPadrao). It
// returns a short message like "backup gravado em <arquivo> (N mantidos)".
func (h *Handler) Executar(ctx context.Context, cfg json.RawMessage) (string, error) {
	keep := parseKeep(cfg)

	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		return "", fmt.Errorf("criar diretório de backups: %w", err)
	}

	nome := prefixoArquivo + h.now().Format(carimboFormato) + sufixoArquivo
	caminho := filepath.Join(h.dir, nome)

	if _, err := h.db.ExecContext(ctx, "VACUUM INTO ?", caminho); err != nil {
		return "", fmt.Errorf("VACUUM INTO: %w", err)
	}

	mantidos, err := h.rotacionar(keep)
	if err != nil {
		// The backup itself was written successfully; a rotation failure should
		// not mask that, but the owner needs to know that the pruning failed.
		return fmt.Sprintf("backup gravado em %s (rotação falhou: %v)", nome, err), nil
	}

	return fmt.Sprintf("backup gravado em %s (%d mantidos)", nome, mantidos), nil
}

// parseKeep interprets config_json; default keepPadrao if absent, invalid or
// keep<=0.
func parseKeep(cfg json.RawMessage) int {
	if len(cfg) == 0 {
		return keepPadrao
	}
	var c config
	if err := json.Unmarshal(cfg, &c); err != nil || c.Keep <= 0 {
		return keepPadrao
	}
	return c.Keep
}

// rotacionar lists the existing snapshots in the directory (prefix/suffix of
// the name), most recent first (the stamp in the name is lexically sortable),
// and removes whatever exceeds `keep`. It returns how many were left.
func (h *Handler) rotacionar(keep int) (int, error) {
	entradas, err := os.ReadDir(h.dir)
	if err != nil {
		return 0, err
	}
	var nomes []string
	for _, e := range entradas {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, prefixoArquivo) && strings.HasSuffix(n, sufixoArquivo) {
			nomes = append(nomes, n)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(nomes)))

	if len(nomes) > keep {
		for _, antigo := range nomes[keep:] {
			if err := os.Remove(filepath.Join(h.dir, antigo)); err != nil {
				return 0, err
			}
		}
		nomes = nomes[:keep]
	}
	return len(nomes), nil
}
