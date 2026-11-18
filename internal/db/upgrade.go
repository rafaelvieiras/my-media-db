package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/migrations"
)

// This file makes an UNATTENDED version change survivable, which is the normal case on
// a NAS: the container image is on a moving tag and the host upgrades it on a schedule,
// with nobody watching. Two things can go wrong there and both are cheap to make safe:
//
//  1. the new binary applies a migration that the owner then regrets -- so the database
//     is copied BEFORE any migration runs;
//  2. the owner goes back to an older image later -- so a database NEWER than the binary
//     refuses to boot instead of being written by code that does not know it.

// versaoEmbarcada is the highest migration version packed into the binary: the newest
// schema this executable knows how to read.
func versaoEmbarcada() (int64, error) {
	entradas, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("db: ler migrations embarcadas: %w", err)
	}
	var maior int64
	for _, e := range entradas {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if v, ok := versaoDoArquivo(e.Name()); ok && v > maior {
			maior = v
		}
	}
	return maior, nil
}

// reVersao extracts the numeric prefix of a migration file name (0007_nome.sql -> 7).
var reVersao = regexp.MustCompile(`^(\d+)_`)

func versaoDoArquivo(nome string) (int64, bool) {
	m := reVersao.FindStringSubmatch(nome)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// PlanoDeMigracao reports where the database is and where this binary can take it:
// atual is the schema already applied (0 for a fresh database), alvo is the newest
// embedded migration.
func (d *DB) PlanoDeMigracao(ctx context.Context) (atual, alvo int64, err error) {
	alvo, err = versaoEmbarcada()
	if err != nil {
		return 0, 0, err
	}
	atual, err = d.SchemaVersion(ctx)
	if err != nil {
		return 0, 0, err
	}
	return atual, alvo, nil
}

// ErrBancoMaisNovo is returned when the database was migrated by a NEWER binary. It is
// a downgrade: this executable cannot read a schema it does not know, and writing to it
// would corrupt data that a later upgrade would then inherit.
var ErrBancoMaisNovo = errors.New("banco mais novo que o binario")

// VerificarDowngrade refuses to boot when the database schema is AHEAD of the newest
// migration in the binary. Unattended hosts make this a real scenario: someone rolls the
// image back to a previous tag after an automatic upgrade, and the old binary would
// otherwise start against a schema it does not understand.
func VerificarDowngrade(atual, alvo int64) error {
	if atual > alvo {
		return fmt.Errorf("%w: o banco esta no schema %d e este binario so conhece ate o %d. "+
			"Use a imagem que criou este banco (ou mais nova). O banco NAO foi alterado. "+
			"Se precisar voltar atras de verdade, a versao que fez o upgrade deixou uma copia "+
			"em /data/backups/pre-migration-schema-*.sqlite", ErrBancoMaisNovo, atual, alvo)
	}
	return nil
}

// BackupAntesDeMigrar writes a CONSISTENT copy of the database before a migration and
// prunes the older copies. VACUUM INTO is what makes it safe: it produces a single-file
// snapshot from the live database (WAL included) without stopping the process and
// without needing to copy -wal/-shm by hand.
//
// It returns the path of the copy, or "" when nothing was written (fresh database).
// manter <= 0 falls back to ManterBackupsPadrao.
func (d *DB) BackupAntesDeMigrar(ctx context.Context, dir string, atual, alvo int64, manter int) (string, error) {
	if atual == 0 || atual >= alvo {
		return "", nil // nothing to protect: fresh database, or nothing to apply
	}
	if dir == "" {
		return "", errors.New("db: diretorio de backup vazio")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("db: criar diretorio de backup: %w", err)
	}
	if manter <= 0 {
		manter = ManterBackupsPadrao
	}
	destino := filepath.Join(dir, fmt.Sprintf("pre-migration-schema-%d-%s.sqlite", atual, carimboDeAgora()))
	// VACUUM INTO refuses an existing file. Two boots inside the same second (a crash
	// loop, a restart storm) already have the copy they would write, so this is the same
	// state: keep the one that exists.
	if _, statErr := os.Stat(destino); statErr == nil {
		return "", nil
	}
	if _, err := d.Write.ExecContext(ctx, "VACUUM INTO ?", destino); err != nil {
		return "", fmt.Errorf("db: copiar o banco antes da migracao: %w", err)
	}
	podarBackupsDeMigracao(dir, manter)
	return destino, nil
}

// ManterBackupsPadrao is how many pre-migration copies are kept. Three covers the usual
// "upgrade broke something, and the one before it did too" without filling the volume:
// each copy is the size of the database.
const ManterBackupsPadrao = 3

// reBackupDeMigracao matches the copies this file writes, so pruning never touches the
// backups made by the backup_vacuum job or by hand.
var reBackupDeMigracao = regexp.MustCompile(`^pre-migration-schema-\d+-\d+-\d+\.sqlite$`)

// podarBackupsDeMigracao keeps the NEWEST `manter` copies and deletes the rest. A
// failure here never aborts the boot: the copy that matters is already written.
func podarBackupsDeMigracao(dir string, manter int) {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var copias []string
	for _, e := range entradas {
		if !e.IsDir() && reBackupDeMigracao.MatchString(e.Name()) {
			copias = append(copias, e.Name())
		}
	}
	if len(copias) <= manter {
		return
	}
	// The timestamp is fixed width, so lexical order is chronological order.
	sort.Strings(copias)
	for _, nome := range copias[:len(copias)-manter] {
		_ = os.Remove(filepath.Join(dir, nome))
	}
}

// carimboDeAgora is the timestamp inside the backup name. Kept in one place so the name
// stays sortable, which is what makes the pruning above correct.
var carimboDeAgora = func() string {
	return time.Now().UTC().Format("20060102-150405")
}
