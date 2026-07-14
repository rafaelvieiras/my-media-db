// Package backup implementa o Handler do job semente `backup_vacuum`
// (briefing 0052): porta a lógica de scripts/backup.sh para dentro do
// binário — VACUUM INTO (cópia consistente mesmo com o app no ar sob WAL,
// ADR-0010, sem downtime) seguido de rotação mantendo os `keep` snapshots
// mais recentes. É a PROVA de ponta a ponta do sistema de jobs: um Handler
// real, não um job de exemplo.
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

// nomeJob é o Handler.Nome() — casa com o slug seedado em job_definition pela
// migration 0011.
const nomeJob = "backup_vacuum"

// keepPadrao é o número de snapshots mantidos quando config_json está
// ausente/inválido/keep<=0 — espelha o default do scripts/backup.sh
// (MMDB_KEEP=7).
const keepPadrao = 7

// prefixoArquivo/sufixoArquivo compõem o nome do snapshot:
// mymediadb-<timestamp>.sqlite (mesma convenção do backup.sh).
const (
	prefixoArquivo = "mymediadb-"
	sufixoArquivo  = ".sqlite"
)

// carimboFormato é o layout do timestamp no nome do arquivo — sem
// dois-pontos (compatível com qualquer filesystem) e lexicamente ordenável,
// mesma nota do backup.sh.
const carimboFormato = "20060102-150405"

// Handler é o Handler do job backup_vacuum. db é o pool de ESCRITA (o
// escritor único da ADR-0010; VACUUM INTO não bloqueia leitores, mas
// serializa pela mesma conexão que todo o resto). dir é o diretório de
// backups já resolvido pelo chamador (env MYMEDIADB_BACKUP_DIR ou
// dirname(DB)/backups — mesma convenção de resolveAssetsDir no main.go).
type Handler struct {
	db  *sql.DB
	dir string
	now func() time.Time
}

// New cria o Handler com o pool de escrita e o diretório de backups.
func New(database *sql.DB, dir string) *Handler {
	return &Handler{db: database, dir: dir, now: func() time.Time { return time.Now().UTC() }}
}

// Nome devolve o slug estável do job.
func (h *Handler) Nome() string { return nomeJob }

// config é o config_json namespaced do job: {"keep": N}.
type config struct {
	Keep int `json:"keep"`
}

// Executar grava um snapshot consistente via VACUUM INTO e rotaciona
// mantendo os `keep` mais recentes (do config_json, default keepPadrao).
// Devolve uma mensagem curta tipo "backup gravado em <arquivo> (N mantidos)".
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
		// O backup em si foi gravado com sucesso; uma falha na rotação não
		// deveria mascarar isso, mas o dono precisa saber que a poda falhou.
		return fmt.Sprintf("backup gravado em %s (rotação falhou: %v)", nome, err), nil
	}

	return fmt.Sprintf("backup gravado em %s (%d mantidos)", nome, mantidos), nil
}

// parseKeep interpreta config_json; default keepPadrao se ausente, inválido
// ou keep<=0.
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

// rotacionar lista os snapshots existentes no diretório (prefixo/sufixo do
// nome), mais recente primeiro (o carimbo no nome é lexicamente ordenável), e
// remove o excedente além de `keep`. Devolve quantos ficaram.
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
