package scanner

// Tests of the post-import of the scan (movie auto-match and optional
// materialization): it runs AFTER the commit of the batch, never inside the
// transaction (ADR-0031). Before the fix, the trigger read the entity through the
// READ pool (row not committed yet) and got `sql: no rows in result set` -- a real
// case in production (Nov 8, 2026) -- and, if it tried to write, it blocked on the
// single write connection (ADR-0010), a deadlock that the first test below
// reproduces.

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// importadorQueLeEEscreve simulates the *server.Server (TentarAutoMatchFilme): it
// READS the entity (the enrich does that through s.leitura().ObterEntity) and
// WRITES through the write connection (enrich.Enriquecer). Inside the batch
// transaction, the read returns sql.ErrNoRows and the write blocks -- it is exactly
// that pair this test guards against regression.
type importadorQueLeEEscreve struct {
	d        *db.DB
	vistos   []string // ids on which the READ worked
	escritas int
}

func (i *importadorQueLeEEscreve) TentarAutoMatchFilme(ctx context.Context, entityID string) (bool, error) {
	if _, err := sqlcgen.New(i.d.Read).ObterEntity(ctx, entityID); err != nil {
		return false, fmt.Errorf("auto-match: entity %s ilegivel na leitura: %w", entityID, err)
	}
	i.vistos = append(i.vistos, entityID)
	if _, err := i.d.Write.ExecContext(ctx,
		`UPDATE entity SET status = 'cataloged', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`,
		entityID); err != nil {
		return false, fmt.Errorf("auto-match: escrita falhou: %w", err)
	}
	i.escritas++
	return true, nil
}

// TestScanFilmeNovoAutoMatchRodaAposCommit covers the central criterion of the
// post-commit dispatch: by the time the auto-match is called, the entity IS already
// visible to the read pool and the write connection is free -- and a write by the
// importer does not block the scan.
func TestScanFilmeNovoAutoMatchRodaAposCommit(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Enquanto Voce Dormia (1995)")
	escrever(t, filepath.Join(pasta, "Enquanto Voce Dormia (1995) - 1080p.mkv"))

	imp := &importadorQueLeEEscreve{d: d}
	m := managerFake(t, d)
	m.ConfigurarFilmes(imp)
	src := criarSource(t, d, root)

	// Time guard: if the trigger goes back inside the transaction, the test fails
	// by deadlock instead of hanging the suite.
	done := make(chan error, 1)
	go func() { done <- scanSync(t, m, src) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("scan não terminou em 30s: auto-match provavelmente voltou para dentro da transação (deadlock na conexão única de escrita)")
	}

	if len(imp.vistos) != 1 {
		t.Fatalf("auto-match chamado %d vez(es), quero 1 (uma entity nova)", len(imp.vistos))
	}
	if imp.escritas != 1 {
		t.Fatalf("escritas do importador = %d, quero 1", imp.escritas)
	}
}

// TestScanFilmeNovoMaterializaAposAutoMatch preserves the behavior of the
// optional materialization (only when the auto-match matched) now in the right
// place.
func TestScanFilmeNovoMaterializaAposAutoMatch(t *testing.T) {
	casos := []struct {
		nome        string
		matched     bool
		queroChamad int
	}{
		{nome: "casou", matched: true, queroChamad: 1},
		{nome: "nao casou", matched: false, queroChamad: 0},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			d := abrirDB(t)
			root := t.TempDir()
			pasta := filepath.Join(root, "Duna (2021) [tmdbid-438631]")
			escrever(t, filepath.Join(pasta, "Duna (2021) [tmdbid-438631] - 1080p.mkv"))

			imp := &filmeImportadorFake{matched: c.matched}
			mat := &materializadorFake{}
			m := managerFake(t, d)
			m.ConfigurarFilmes(imp)
			m.ConfigurarMaterializacao(mat)
			src := criarSource(t, d, root)

			if err := scanSync(t, m, src); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if mat.chamadas != c.queroChamad {
				t.Errorf("materializações = %d, quero %d", mat.chamadas, c.queroChamad)
			}
		})
	}
}
