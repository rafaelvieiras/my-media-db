package scanner

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
)

// semearOrdemDoDono writes the owner order (ADR-0033) straight into the override layer,
// which is where the endpoint puts it. `unidades` is the list of units in the wire shape.
func semearOrdemDoDono(t *testing.T, d *db.DB, serieID string, unidades []map[string]any) {
	t.Helper()
	corpo := map[string]any{
		"extension_data": map[string]any{
			"core.tv@1": map[string]any{
				"owner_order": map[string]any{"name": "release", "units": unidades},
			},
		},
	}
	bruto, err := json.Marshal(corpo)
	if err != nil {
		t.Fatalf("marshal override: %v", err)
	}
	if _, err := d.Write.ExecContext(context.Background(),
		"INSERT INTO media_override (entity_id, override_json) VALUES (?, ?)", serieID, string(bruto)); err != nil {
		t.Fatalf("gravar override: %v", err)
	}
}

// idDaSerie returns the single series of the catalog.
func idDaSerie(t *testing.T, d *db.DB) string {
	t.Helper()
	var id string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT id FROM entity WHERE type='tv' LIMIT 1").Scan(&id); err != nil {
		t.Fatalf("id da serie: %v", err)
	}
	return id
}

// TestScanOrdemDoDonoMesclaEpisodios: the owner declares ONE unit (S01E01) that merges
// three aired episodes; a file named S01E01 then links the THREE of them, with ordinal
// 0/1/2 in the declared order. This is the case no provider order can express — the
// reason the owner order exists (ADR-0033).
func TestScanOrdemDoDonoMesclaEpisodios(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	alvo := filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E01.mkv")
	escrever(t, alvo)

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)

	// 1st scan: the auto-import builds the tree and links the episode by the official
	// numbering (one link).
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	if n := contarVinculos(t, d); n != 1 {
		t.Fatalf("vinculos antes da ordem do dono = %d, quero 1", n)
	}

	// The owner merges the three episodes into a single unit.
	serieID := idDaSerie(t, d)
	semearOrdemDoDono(t, d, serieID, []map[string]any{{
		"season": 1, "number": 1,
		"episodes": []string{epIDPorTmdb(t, d, "101"), epIDPorTmdb(t, d, "102"), epIDPorTmdb(t, d, "103")},
	}})

	// 2nd scan: the same file now matches the owner unit and links all three.
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if n := contarVinculos(t, d); n != 3 {
		t.Fatalf("vinculos com a ordem do dono = %d, quero 3 (unidade mesclada)", n)
	}
	for i, tmdb := range []string{"101", "102", "103"} {
		if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=? AND ordinal=?",
			epIDPorTmdb(t, d, tmdb), i); n != 1 {
			t.Errorf("ep %s deveria ter ordinal %d (ordem declarada pelo dono)", tmdb, i)
		}
	}
}

// TestScanOrdemDoDonoNaoAtrapalhaForaDaUnidade: the owner order only decides where it
// declares a unit. An episode its units do not cover keeps being matched by the official
// numbering, so declaring one merged unit never breaks the rest of the library.
func TestScanOrdemDoDonoNaoAtrapalhaForaDaUnidade(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E01.mkv"))
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S02E01.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}

	serieID := idDaSerie(t, d)
	// Only S01E01 is re-declared; S02E01 (another season) stays with the provider numbering.
	semearOrdemDoDono(t, d, serieID, []map[string]any{{
		"season": 1, "number": 1,
		"episodes": []string{epIDPorTmdb(t, d, "101"), epIDPorTmdb(t, d, "102")},
	}})
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}

	// File S01E01 → 2 episodes (the unit); file S02E01 → 1 episode (fallback to official).
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=?", epIDPorTmdb(t, d, "101")); n != 1 {
		t.Errorf("101 deveria estar vinculado 1 vez, tem %d", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=?", epIDPorTmdb(t, d, "102")); n != 1 {
		t.Errorf("102 (segundo da unidade) deveria estar vinculado 1 vez, tem %d", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=?", epIDPorTmdb(t, d, "201")); n != 1 {
		t.Errorf("201 fora da unidade deveria seguir a numeracao oficial, tem %d", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=?", epIDPorTmdb(t, d, "103")); n != 0 {
		t.Errorf("103 nao tem arquivo, deveria estar sem vinculo, tem %d", n)
	}
}
