package scanner

import (
	"context"
	"path/filepath"
	"testing"
)

// TestRepararArvoreProvisoria plants the shape the old scanner left behind -- a draft series
// with the files flat on it, no season, no episode -- plus the duplicated provisional episodes
// of the dedupe bug, and proves the repair builds the tree, links the files, removes the
// leftovers and is idempotent.
func TestRepararArvoreProvisoria(t *testing.T) {
	d := abrirDB(t)
	ctx := context.Background()
	m := managerFake(t, d)

	// The flat series: 2 files of season 01 in the folder, straight on the series.
	if _, err := d.Write.ExecContext(ctx,
		`INSERT INTO entity (id, type, title, status) VALUES ('serie-flat','tv','Some Show','draft')`); err != nil {
		t.Fatalf("serie: %v", err)
	}
	for i, caminho := range []string{
		filepath.Join("/lib", "Some Show", "Temporada 01", "Some Show - S01E01.mkv"),
		filepath.Join("/lib", "Some Show", "Temporada 01", "Some Show - S01E02.mkv"),
	} {
		if _, err := d.Write.ExecContext(ctx,
			`INSERT INTO media_file (id, entity_id, path, state) VALUES (?, 'serie-flat', ?, 'present')`,
			"mf-"+string(rune('1'+i)), caminho); err != nil {
			t.Fatalf("media_file: %v", err)
		}
	}

	// Another series, this one WITH a tree, holding two identical provisional episodes and no
	// file: one stays, one leaves.
	if _, err := d.Write.ExecContext(ctx,
		`INSERT INTO entity (id, type, title, status) VALUES ('serie-dup','tv','Other Show','draft')`); err != nil {
		t.Fatalf("serie dup: %v", err)
	}
	if _, err := d.Write.ExecContext(ctx,
		`INSERT INTO entity (id, type, title, status) VALUES ('temp-1','season','Temporada 0','cataloged')`); err != nil {
		t.Fatalf("temporada: %v", err)
	}
	if _, err := d.Write.ExecContext(ctx,
		`INSERT INTO entity_relation (id, parent_id, child_id, kind) VALUES ('r-t','serie-dup','temp-1','tv_season')`); err != nil {
		t.Fatalf("rel temporada: %v", err)
	}
	extensao := `{"core.tv@1":{"numbering":{"official":{"season":0}},"provisorio":{"origem":"arquivo","arquivo":"Making & Of.mkv"}}}`
	for i, id := range []string{"ep-a", "ep-b"} {
		if _, err := d.Write.ExecContext(ctx,
			`INSERT INTO entity (id, type, title, status, extension_data_json) VALUES (?, 'episode', 'Making & Of', 'draft', ?)`,
			id, extensao); err != nil {
			t.Fatalf("episódio duplicado: %v", err)
		}
		if _, err := d.Write.ExecContext(ctx,
			`INSERT INTO entity_relation (id, parent_id, child_id, kind, sort_index) VALUES (?, 'temp-1', ?, 'tv_episode', 0)`,
			"r-"+id+string(rune('0'+i)), id); err != nil {
			t.Fatalf("rel episódio: %v", err)
		}
	}

	rel, err := m.RepararArvoreProvisoria(ctx, false)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if rel.Series != 1 || rel.Arquivos != 2 || rel.DuplicadosRemovidos != 1 {
		t.Fatalf("dry run = %+v, quero 1 série, 2 arquivos, 1 duplicado", rel)
	}
	if n := contar(t, d, "SELECT COUNT(*) FROM entity WHERE type='season'"); n != 1 {
		t.Fatalf("o dry run escreveu temporada (total %d)", n)
	}

	rel, err = m.RepararArvoreProvisoria(ctx, true)
	if err != nil {
		t.Fatalf("aplicar: %v", err)
	}
	if rel.TemporadasCriadas != 1 || rel.EpisodiosCriados != 2 || rel.VinculosCriados != 2 {
		t.Errorf("aplicado = %+v, quero 1 temporada, 2 episódios, 2 vínculos", rel)
	}
	if n := contar(t, d, "SELECT COUNT(*) FROM entity WHERE type='season'"); n != 2 {
		t.Errorf("temporadas = %d, quero 2 (a de especiais + a nova)", n)
	}
	if n := contar(t, d, "SELECT COUNT(*) FROM entity WHERE type='episode'"); n != 3 {
		t.Errorf("episódios = %d, quero 3 (2 do arquivo + 1 duplicado que ficou)", n)
	}
	if n := contar(t, d, "SELECT COUNT(*) FROM media_file_entity WHERE kind='tv_episode'"); n != 2 {
		t.Errorf("vínculos = %d, quero 2 (um por arquivo)", n)
	}
	// Idempotent: the second apply finds the tree it built and removes nothing.
	rel, err = m.RepararArvoreProvisoria(ctx, true)
	if err != nil {
		t.Fatalf("segundo apply: %v", err)
	}
	if rel.Series != 0 || rel.DuplicadosRemovidos != 0 {
		t.Errorf("segundo apply = %+v, quero 0 série e 0 duplicado", rel)
	}
}
