package db

import (
	"context"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// contaLinhas conta as linhas de uma tabela filtrando por uma coluna = valor.
// Concentra o SELECT count(*) num helper para manter os testes simples.
func contaLinhas(t *testing.T, d *DB, query string, args ...any) int {
	t.Helper()
	var n int
	err := d.Read.QueryRowContext(context.Background(), query, args...).Scan(&n)
	check(t, err, "count")
	return n
}

// TestMediaExternalIndiceLookup prova que o lookup por (source, source_id) usa o
// índice idx_media_external_source (SEARCH, não SCAN) — o caminho de reconciliar
// identidade pelo id externo cacheado.
func TestMediaExternalIndiceLookup(t *testing.T) {
	d, _ := setupNucleo(t)
	detalhe := explicaPlano(t, d,
		"SELECT id FROM media_external WHERE source = ? AND source_id = ?", "tmdb", "603")
	if !strings.Contains(detalhe, "idx_media_external_source") {
		t.Errorf("plano por (source, source_id) = %q, quero mencionar idx_media_external_source", detalhe)
	}
}

// TestMediaExternalUpsertUnique prova a UNIQUE(entity_id, source): um segundo
// upsert com a mesma (entity, source) ATUALIZA a linha (payload + source_id), não
// duplica; uma fonte diferente cria uma segunda linha.
func TestMediaExternalUpsertUnique(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "Matrix", "")

	primeira, err := q.UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID: "ext-1", EntityID: e.ID, Source: "tmdb",
		SourceID: ns("603"), PayloadJson: ns(`{"a":1}`),
	})
	check(t, err, "UpsertMediaExternal 1")

	// Mesma (entity, source), id diferente: o ON CONFLICT atualiza a linha
	// existente (id preservado) em vez de inserir outra.
	segunda, err := q.UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID: "ext-OUTRO", EntityID: e.ID, Source: "tmdb",
		SourceID: ns("603-v2"), PayloadJson: ns(`{"a":2}`),
	})
	check(t, err, "UpsertMediaExternal 2 (conflito)")
	if segunda.ID != primeira.ID {
		t.Errorf("upsert trocou o id (%q -> %q), quero preservar %q", primeira.ID, segunda.ID, primeira.ID)
	}
	if segunda.PayloadJson.String != `{"a":2}` || segunda.SourceID.String != "603-v2" {
		t.Errorf("upsert nao atualizou payload/source_id: %+v", segunda)
	}
	if n := contaLinhas(t, d, "SELECT count(*) FROM media_external WHERE entity_id = ?", e.ID); n != 1 {
		t.Errorf("linhas de media_external para a entity = %d, quero 1 (upsert nao duplica)", n)
	}

	// Fonte diferente: cria uma segunda linha.
	_, err = q.UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID: "ext-2", EntityID: e.ID, Source: "imdb", SourceID: ns("tt0133093"),
	})
	check(t, err, "UpsertMediaExternal outra fonte")
	if n := contaLinhas(t, d, "SELECT count(*) FROM media_external WHERE entity_id = ?", e.ID); n != 2 {
		t.Errorf("linhas apos segunda fonte = %d, quero 2", n)
	}
}

// TestMediaOverride1a1 prova que media_override e 1:1 (PK = entity_id): um segundo
// upsert na mesma entity SUBSTITUI o override, nao cria segunda linha.
func TestMediaOverride1a1(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "Matrix", "")

	_, err := q.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID: e.ID, OverrideJson: ns(`{"title":"A"}`),
	})
	check(t, err, "UpsertMediaOverride 1")
	segunda, err := q.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID: e.ID, OverrideJson: ns(`{"title":"B"}`),
	})
	check(t, err, "UpsertMediaOverride 2")
	if segunda.OverrideJson.String != `{"title":"B"}` {
		t.Errorf("override apos upsert = %q, quero {\"title\":\"B\"}", segunda.OverrideJson.String)
	}
	if n := contaLinhas(t, d, "SELECT count(*) FROM media_override WHERE entity_id = ?", e.ID); n != 1 {
		t.Errorf("linhas de media_override = %d, quero 1 (1:1)", n)
	}
}

// TestCamadasCascata prova as FKs ON DELETE CASCADE: purgar a entity remove as
// linhas de media_external e media_override daquela entity.
func TestCamadasCascata(t *testing.T) {
	d, q := setupNucleo(t)
	ctx := context.Background()
	e := criarEntity(t, q, "ent-1", "Matrix", "")

	_, err := q.UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID: "ext-1", EntityID: e.ID, Source: "tmdb", PayloadJson: ns(`{"a":1}`),
	})
	check(t, err, "UpsertMediaExternal")
	_, err = q.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID: e.ID, OverrideJson: ns(`{"title":"X"}`),
	})
	check(t, err, "UpsertMediaOverride")

	check(t, q.ApagarEntity(ctx, e.ID), "ApagarEntity")

	if n := contaLinhas(t, d, "SELECT count(*) FROM media_external WHERE entity_id = ?", e.ID); n != 0 {
		t.Errorf("media_external orfaos apos purga = %d, quero 0 (cascata)", n)
	}
	if n := contaLinhas(t, d, "SELECT count(*) FROM media_override WHERE entity_id = ?", e.ID); n != 0 {
		t.Errorf("media_override orfaos apos purga = %d, quero 0 (cascata)", n)
	}
}
