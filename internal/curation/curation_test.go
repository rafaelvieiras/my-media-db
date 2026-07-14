package curation

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupBanco sobe um banco de teste migrado, isolado por teste.
func setupBanco(t *testing.T) *db.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "curation.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}

func semearEntity(t *testing.T, d *db.DB, id, tipo, titulo string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipo, Title: titulo,
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

func definirStatus(t *testing.T, d *db.DB, id, status string) {
	t.Helper()
	if _, err := d.Write.ExecContext(context.Background(),
		"UPDATE entity SET status = ? WHERE id = ?", status, id); err != nil {
		t.Fatalf("definirStatus(%s=%s): %v", id, status, err)
	}
}

func semearFile(t *testing.T, d *db.DB, id, entityID, path, state string, conf float64) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID: id, EntityID: entityID, Path: path, State: state,
		MatchConfidence: sql.NullFloat64{Float64: conf, Valid: true},
	}); err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

// TestContarRevisaoEOrfas prova que o Servico devolve as MESMAS contagens que
// a fila completa (draft + baixa confianca para revisao; auto-criada+missing+
// sem investimento para orfas) -- sem duplicar o predicado, so agregando.
func TestContarRevisaoEOrfas(t *testing.T) {
	d := setupBanco(t)
	s := New(d)
	ctx := context.Background()

	// e-draft: nao identificado -> entra em revisao.
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")

	// e-baixa: present com confianca 40 (< 70) -> entra em revisao.
	semearEntity(t, d, "e-baixa", "movie", "Baixa Confianca")
	semearFile(t, d, "f-baixa", "e-baixa", "/x/baixa.mkv", "present", 40)

	// e-alta: present com confianca 95 -> NAO entra em revisao nem orfas.
	semearEntity(t, d, "e-alta", "movie", "Alta Confianca")
	semearFile(t, d, "f-alta", "e-alta", "/x/alta.mkv", "present", 95)

	// e-orfa: cataloged, todos os arquivos missing, sem investimento -> orfa.
	semearEntity(t, d, "e-orfa", "movie", "Orfa")
	semearFile(t, d, "f-orfa", "e-orfa", "/x/orfa.mkv", "missing", 0)

	revisao, err := s.ContarRevisao(ctx)
	if err != nil {
		t.Fatalf("ContarRevisao: %v", err)
	}
	if revisao != 2 {
		t.Fatalf("ContarRevisao = %d, quero 2 (draft + baixa confianca)", revisao)
	}

	orfas, err := s.ContarOrfas(ctx)
	if err != nil {
		t.Fatalf("ContarOrfas: %v", err)
	}
	if orfas != 1 {
		t.Fatalf("ContarOrfas = %d, quero 1", orfas)
	}
}

// TestContarVazio prova que um banco sem entities devolve zero nas duas
// contagens (sem erro).
func TestContarVazio(t *testing.T) {
	d := setupBanco(t)
	s := New(d)
	ctx := context.Background()

	revisao, err := s.ContarRevisao(ctx)
	if err != nil || revisao != 0 {
		t.Fatalf("ContarRevisao = (%d, %v), quero (0, nil)", revisao, err)
	}
	orfas, err := s.ContarOrfas(ctx)
	if err != nil || orfas != 0 {
		t.Fatalf("ContarOrfas = (%d, %v), quero (0, nil)", orfas, err)
	}
}
