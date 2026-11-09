package scanner

// Testes do briefing 0135/ADR-0031: o pós-import do scan (auto-match de filme e
// materialização opcional) roda DEPOIS do commit do lote, nunca dentro da
// transação. Antes da correção, o gatilho lia a entity pelo pool de LEITURA
// (linha ainda não commitada) e recebia `sql: no rows in result set` -- caso real
// em produção (08/nov/2026) -- e, se tentasse escrever, travava na conexão única
// de escrita (ADR-0010), deadlock que o primeiro teste abaixo reproduz.

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// importadorQueLeEEscreve simula o *server.Server (TentarAutoMatchFilme): ele LÊ
// a entity (o enrich faz isso por s.leitura().ObterEntity) e ESCREVE pela
// conexão de escrita (enrich.Enriquecer). Dentro da transação do lote, a leitura
// devolve sql.ErrNoRows e a escrita trava -- é exatamente esse par que este
// teste trava contra regressão.
type importadorQueLeEEscreve struct {
	d        *db.DB
	vistos   []string // ids em que a LEITURA funcionou
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

// TestScanFilmeNovoAutoMatchRodaAposCommit cobre o critério central do 0135:
// quando o auto-match é chamado, a entity JÁ é visível ao pool de leitura e a
// conexão de escrita está livre -- e uma escrita do importador não trava o scan.
func TestScanFilmeNovoAutoMatchRodaAposCommit(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Enquanto Voce Dormia (1995)")
	escrever(t, filepath.Join(pasta, "Enquanto Voce Dormia (1995) - 1080p.mkv"))

	imp := &importadorQueLeEEscreve{d: d}
	m := managerFake(t, d)
	m.ConfigurarFilmes(imp)
	src := criarSource(t, d, root)

	// Guarda de tempo: se o gatilho voltar para dentro da transação, o teste
	// falha por deadlock em vez de pendurar a suíte.
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

// TestScanFilmeNovoMaterializaAposAutoMatch preserva o comportamento do 0089
// (materialização opcional só quando o auto-match casou) agora no lugar certo.
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
