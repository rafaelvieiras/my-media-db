package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// TestCorrigirExternalIdParaIdJaImportadoDuplicaEntity reproduces the reported bug:
//  1. the scan found a file and created a 'draft' entity (without external_ids) with
//     the media_file linked;
//  2. the owner searched the title manually and clicked "importar" -- since the draft
//     entity has no external_ids yet, the find-or-create (import dedup) does not find it
//     and creates a NEW entity (without a file);
//  3. the owner then opens the entity with the file and writes the SAME external_id
//     (tmdb) he just imported, through PUT /entities/{id}/external-ids.
//
// Step 3 detects that "777" already belongs to the imported entity (without the owner's
// investment) and MERGES it into "e-arquivo": the duplicate disappears, the file stays
// linked to a single entity with the correct identity.
func TestCorrigirExternalIdParaIdJaImportadoDuplicaEntity(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	ctx := context.Background()
	w := sqlcgen.New(d.Write)

	// 1) Draft entity with a file (simulates the orphan from the scan).
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "e-arquivo", Type: "movie", Title: "arquivo.mkv",
	}); err != nil {
		t.Fatalf("CriarEntity draft: %v", err)
	}
	if _, err := w.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID: "e-arquivo", Type: "movie", Title: "arquivo.mkv", Status: "draft",
	}); err != nil {
		t.Fatalf("rebaixar p/ draft: %v", err)
	}
	if _, err := w.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf1", EntityID: "e-arquivo", Path: "/lib/arquivo.mkv", State: "present",
	}); err != nil {
		t.Fatalf("CriarMediaFile: %v", err)
	}

	// 2) Manual search + "importar" -- the find-or-create does not find the draft
	// (without external_ids) and creates a new entity, without a file.
	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{
		"source": "tmdb", "source_id": "777", "type": "movie",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	importada := decodeEntidade(t, rec)
	if importada.ID == "e-arquivo" {
		t.Fatalf("import deveria ter criado uma entity NOVA, nao reusado e-arquivo")
	}

	// 3) The owner corrects the identity of the entity WITH the file to the same external
	// id he just imported.
	rec2 := do(t, h, http.MethodPut, "/api/v1/entities/e-arquivo/external-ids", map[string]any{
		"external_ids": map[string]string{"tmdb": "777"},
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("PUT external-ids status = %d, quero 200 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	if got := rec2.Header().Get(headerDuplicataFundida); got != importada.ID {
		t.Errorf("header %s = %q, quero o id da entity importada (%q)", headerDuplicataFundida, got, importada.ID)
	}

	r := sqlcgen.New(d.Read)
	todas, err := r.ListarEntities(ctx, sqlcgen.ListarEntitiesParams{Limit: 100, Offset: 0})
	if err != nil {
		t.Fatalf("ListarEntities: %v", err)
	}
	comEsseTmdb := 0
	for _, e := range todas {
		if e.Type == "movie" && e.ExternalIDTmdb.Valid && e.ExternalIDTmdb.String == "777" {
			comEsseTmdb++
		}
	}
	if comEsseTmdb != 1 {
		t.Errorf("entities 'movie' com external_id_tmdb=777 = %d, quero 1 (a duplicata deveria ter sido fundida)", comEsseTmdb)
	}
	if _, err := r.ObterEntity(ctx, importada.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("entity importada (%q) deveria ter sido apagada pela fusao, err=%v", importada.ID, err)
	}
	if _, err := r.ObterMediaFile(ctx, "mf1"); err != nil {
		t.Errorf("ObterMediaFile mf1: %v (deveria continuar existindo, ligado a e-arquivo)", err)
	}
	entArquivo, err := r.ObterEntity(ctx, "e-arquivo")
	if err != nil {
		t.Fatalf("ObterEntity e-arquivo: %v", err)
	}
	if !entArquivo.ExternalIDTmdb.Valid || entArquivo.ExternalIDTmdb.String != "777" {
		t.Errorf("e-arquivo.external_id_tmdb = %v, quero 777", entArquivo.ExternalIDTmdb)
	}
}

// TestExternalIdsMbidDetectaColisaoEFunde mirrors
// TestCorrigirExternalIdParaIdJaImportadoDuplicaEntity for the "mbid" key: before this
// fix, acharColisaoExterna only covered tmdb/imdb/tvdb -- a LEGACY `artista` entity
// (without external_ids, data from before MusicBrainz existed) that got, through
// PUT /entities/{id}/external-ids, the SAME mbid as another already cataloged entity was
// not detected as a collision: both ended up with the same mbid, a silent duplicate of
// the same real artist.
func TestExternalIdsMbidDetectaColisaoEFunde(t *testing.T) {
	h, d := setupSearchServer(t)
	ctx := context.Background()
	w := sqlcgen.New(d.Write)

	const mbid = "aaaaaaaa-0000-0000-0000-000000000099"
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "artista-existente", Type: "artista", Title: "Ja Catalogado",
		ExternalIdsJson: sql.NullString{String: `{"mbid":"` + mbid + `"}`, Valid: true},
	}); err != nil {
		t.Fatalf("CriarEntity existente: %v", err)
	}
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "artista-legado", Type: "artista", Title: "Legado sem mbid",
	}); err != nil {
		t.Fatalf("CriarEntity legado: %v", err)
	}

	rec := do(t, h, http.MethodPut, "/api/v1/entities/artista-legado/external-ids", map[string]any{
		"external_ids": map[string]string{"mbid": mbid},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT external-ids status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(headerDuplicataFundida); got != "artista-existente" {
		t.Errorf("header %s = %q, quero artista-existente", headerDuplicataFundida, got)
	}

	r := sqlcgen.New(d.Read)
	if _, err := r.ObterEntity(ctx, "artista-existente"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("artista-existente deveria ter sido fundido/apagado, err=%v", err)
	}
	legado, err := r.ObterEntity(ctx, "artista-legado")
	if err != nil {
		t.Fatalf("ObterEntity artista-legado: %v", err)
	}
	if !legado.ExternalIDMbid.Valid || legado.ExternalIDMbid.String != mbid {
		t.Errorf("artista-legado.external_id_mbid = %v, quero %q", legado.ExternalIDMbid, mbid)
	}
}

// TestCorrigirExternalIdParaIdComInvestimentoDoDonoNaoFunde covers the other side of the
// safety net: when the entity that already has the external id HAS the owner's
// investment (here, a media_override), the automatic merge does NOT happen -- the write
// answers 409 and writes nothing (neither the identity, nor deleting the other
// entity).
func TestCorrigirExternalIdParaIdComInvestimentoDoDonoNaoFunde(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	ctx := context.Background()
	w := sqlcgen.New(d.Write)

	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "e-arquivo", Type: "movie", Title: "arquivo.mkv",
	}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	if _, err := w.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf1", EntityID: "e-arquivo", Path: "/lib/arquivo.mkv", State: "present",
	}); err != nil {
		t.Fatalf("CriarMediaFile: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{
		"source": "tmdb", "source_id": "888", "type": "movie",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	importada := decodeEntidade(t, rec)

	// The imported entity HAS the owner's investment: an override.
	if _, err := w.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID:     importada.ID,
		OverrideJson: sql.NullString{String: `{"title":"Titulo Curado"}`, Valid: true},
	}); err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}

	rec2 := do(t, h, http.MethodPut, "/api/v1/entities/e-arquivo/external-ids", map[string]any{
		"external_ids": map[string]string{"tmdb": "888"},
	})
	if rec2.Code != http.StatusConflict {
		t.Fatalf("PUT external-ids status = %d, quero 409 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	checkErrorShape(t, rec2, "conflict_duplicate_identity")

	r := sqlcgen.New(d.Read)
	entArquivo, err := r.ObterEntity(ctx, "e-arquivo")
	if err != nil {
		t.Fatalf("ObterEntity e-arquivo: %v", err)
	}
	if entArquivo.ExternalIDTmdb.Valid {
		t.Errorf("e-arquivo.external_id_tmdb = %v, quero NULL (nada deveria ter sido gravado)", entArquivo.ExternalIDTmdb)
	}
	if _, err := r.ObterEntity(ctx, importada.ID); err != nil {
		t.Errorf("entity importada (%q) nao deveria ter sido apagada, err=%v", importada.ID, err)
	}
}

// TestCorrigirExternalIdComDuplicataJaCorrompidaNoBanco covers a residual case found in
// production: the duplicate already existed in the DATABASE BEFORE this fix (the original
// bug left two entities with the SAME external_id_tmdb, since the generated column is not
// UNIQUE, only indexed). When the owner submits the SAME id the entity with the file
// already had (e.g. clicks "Salvar e re-enriquecer" again), the collision detection has
// to find the OTHER entity even though a :one query without ORDER BY could have returned
// the target entity itself first -- see the comment of acharColisaoExterna.
func TestCorrigirExternalIdComDuplicataJaCorrompidaNoBanco(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	ctx := context.Background()
	w := sqlcgen.New(d.Write)

	// e-arquivo is BORN with external_id_tmdb=777 (simulates the corrupted data).
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "e-arquivo", Type: "movie", Title: "arquivo.mkv",
		ExternalIdsJson: sql.NullString{String: `{"tmdb":"777"}`, Valid: true},
	}); err != nil {
		t.Fatalf("CriarEntity e-arquivo: %v", err)
	}
	if _, err := w.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf1", EntityID: "e-arquivo", Path: "/lib/arquivo.mkv", State: "present",
	}); err != nil {
		t.Fatalf("CriarMediaFile: %v", err)
	}

	// e-duplicada ALSO already has tmdb=777 -- the two already collide before any call to
	// the endpoint. It gets a fanart asset (from fanart.tv) to validate that the merge
	// TRANSFERS what only the loser has.
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "e-duplicada", Type: "movie", Title: "Filme Duplicado",
		ExternalIdsJson: sql.NullString{String: `{"tmdb":"777"}`, Valid: true},
	}); err != nil {
		t.Fatalf("CriarEntity e-duplicada: %v", err)
	}
	if _, err := w.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID: "asset-fanart", EntityID: "e-duplicada", Kind: "fanart", Origin: "provider",
		PathOrUri: "https://assets.fanart.tv/fanart.jpg",
	}); err != nil {
		t.Fatalf("CriarAsset: %v", err)
	}

	// The owner submits the SAME id e-arquivo already had (e.g. reopens the editor and
	// clicks "Salvar e re-enriquecer" without changing anything).
	rec := do(t, h, http.MethodPut, "/api/v1/entities/e-arquivo/external-ids", map[string]any{
		"external_ids": map[string]string{"tmdb": "777"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT external-ids status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(headerDuplicataFundida); got != "e-duplicada" {
		t.Errorf("header %s = %q, quero %q", headerDuplicataFundida, got, "e-duplicada")
	}

	r := sqlcgen.New(d.Read)
	if _, err := r.ObterEntity(ctx, "e-duplicada"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("e-duplicada deveria ter sido apagada pela fusao, err=%v", err)
	}
	assets, err := r.ListarAssetsPorEntity(ctx, "e-arquivo")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	temFanart := false
	for _, a := range assets {
		if a.Kind == "fanart" {
			temFanart = true
		}
	}
	if !temFanart {
		t.Errorf("e-arquivo deveria ter herdado o asset fanart da duplicata fundida")
	}
	if _, err := r.ObterMediaFile(ctx, "mf1"); err != nil {
		t.Errorf("ObterMediaFile mf1: %v (deveria continuar existindo)", err)
	}
}
