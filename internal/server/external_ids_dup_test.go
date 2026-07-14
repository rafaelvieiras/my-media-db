package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// TestCorrigirExternalIdParaIdJaImportadoDuplicaEntity reproduz o bug relatado:
//  1. o scan achou um arquivo e criou uma entity 'draft' (sem external_ids) com o
//     media_file ligado;
//  2. o dono buscou o titulo manualmente e clicou "importar" -- como a entity
//     draft nao tem external_ids ainda, o find-or-create (dedup MD4) do Importar
//     nao a encontra e cria uma entity NOVA (sem arquivo);
//  3. o dono entao abre a entity com o arquivo e grava o MESMO external_id (tmdb)
//     que acabou de importar, via PUT /entities/{id}/external-ids.
//
// O passo 3 detecta que "777" ja pertence a entity importada (sem investimento
// do dono) e a FUNDE em "e-arquivo" (briefing 0051): a duplicata desaparece, o
// arquivo permanece ligado a uma unica entity com a identidade correta.
func TestCorrigirExternalIdParaIdJaImportadoDuplicaEntity(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	ctx := context.Background()
	w := sqlcgen.New(d.Write)

	// 1) Entity draft com arquivo (simula o orfao do scan).
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

	// 2) Busca manual + "importar" -- o find-or-create nao acha a draft (sem
	// external_ids) e cria uma entity nova, sem arquivo.
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

	// 3) Dono corrige a identidade da entity COM arquivo para o mesmo id externo
	// que acabou de importar.
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

// TestCorrigirExternalIdParaIdComInvestimentoDoDonoNaoFunde cobre o outro lado
// do briefing 0051: quando a entity que ja tem o id externo TEM investimento do
// dono (aqui, um media_override), a fusao automatica NAO acontece -- a escrita
// responde 409 e nao grava nada (nem a identidade, nem apaga a outra entity).
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

	// A entity importada TEM investimento do dono: um override.
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

// TestCorrigirExternalIdComDuplicataJaCorrompidaNoBanco cobre um caso residual
// do briefing 0051 encontrado em producao: a duplicata ja existia no BANCO
// ANTES deste fix (o bug original deixava duas entities com o MESMO
// external_id_tmdb, ja que a coluna gerada nao e UNIQUE, so indexada). Quando o
// dono reenvia o MESMO id que a entity com arquivo ja tinha (ex.: clica "Salvar
// e re-enriquecer" de novo), a deteccao de colisao tem que achar a OUTRA
// entity mesmo que uma query :one sem ORDER BY pudesse ter devolvido a propria
// entity-alvo primeiro -- ver o comentario de acharColisaoExterna.
func TestCorrigirExternalIdComDuplicataJaCorrompidaNoBanco(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	ctx := context.Background()
	w := sqlcgen.New(d.Write)

	// e-arquivo JA nasce com external_id_tmdb=777 (simula o dado corrompido).
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

	// e-duplicada TAMBEM ja tem tmdb=777 -- as duas ja colidem antes de qualquer
	// chamada ao endpoint. Ganha um asset fanart (do fanart.tv) para validar que
	// a fusao TRANSFERE o que so a perdedora tem.
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

	// Dono reenvia o MESMO id que e-arquivo ja tinha (ex.: reabre o editor e
	// clica "Salvar e re-enriquecer" sem mudar nada).
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
