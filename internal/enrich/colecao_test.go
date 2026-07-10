package enrich

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// fakeColProvider estende o fakeProvider com a capacidade OPCIONAL de coleção
// (provider.ColecaoProvider): BuscarColecao devolve uma coleção fixa. Satisfaz
// provider.Provider pelos métodos promovidos do fakeProvider embutido.
type fakeColProvider struct {
	*fakeProvider
	col         provider.Colecao
	colChamadas int
}

func (f *fakeColProvider) BuscarColecao(_ context.Context, _, _ string) (provider.Colecao, error) {
	f.colChamadas++
	return f.col, nil
}

// colDeTeste monta a "Matrix Coleção" com 2 filmes membros; posterURL opcional (nil
// pula o cache de arte).
func colDeTeste(posterURL *string) provider.Colecao {
	return provider.Colecao{
		Source: "tmdb", SourceID: "2344", Name: "Matrix Colecao", PosterURL: posterURL,
		Parts: []provider.ResultadoExterno{
			{Source: "tmdb", SourceID: "603", Type: "movie", Title: "Matrix"},
			{Source: "tmdb", SourceID: "604", Type: "movie", Title: "Matrix Reloaded"},
		},
	}
}

// relacoesColecao conta as relações kind='collection' sob um pai.
func relacoesColecao(t *testing.T, database *db.DB, parentID string) int {
	t.Helper()
	rels, err := sqlcgen.New(database.Read).ListarRelacoesFilhas(context.Background(), parentID)
	if err != nil {
		t.Fatalf("ListarRelacoesFilhas: %v", err)
	}
	n := 0
	for _, rel := range rels {
		if rel.Kind == relacaoColecao {
			n++
		}
	}
	return n
}

// TestImportarColecaoCriaEntityERelacoes: importar type=collection cria a entity
// 'collection', importa os filmes membros (dedup por tmdb id), liga-os por
// entity_relation(kind='collection') e cacheia o pôster da coleção.
func TestImportarColecaoCriaEntityERelacoes(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-COL"))
	poster := img.URL + "/c.jpg"
	fake := &fakeColProvider{
		fakeProvider: &fakeProvider{raw: json.RawMessage(`{"title":"Filme"}`), campos: map[string]any{"title": "Filme"}},
		col:          colDeTeste(&poster),
	}
	svc, dir := novoServico(t, database, fake)

	colID, criada, err := svc.Importar(ctx, "tmdb", "2344", "collection", []string{"pt-BR"})
	if err != nil {
		t.Fatalf("Importar colecao: %v", err)
	}
	if !criada {
		t.Error("criada = false, quero true (colecao nova)")
	}
	exigirColecaoImportada(t, database, colID, dir)
}

// exigirColecaoImportada verifica a entity coleção (type/title/id), as 2 relações, os 2
// filmes membros importados e o pôster cacheado sob assetsDir.
func exigirColecaoImportada(t *testing.T, database *db.DB, colID, dir string) {
	t.Helper()
	ctx := context.Background()
	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, colID)
	if err != nil {
		t.Fatalf("ObterEntity colecao: %v", err)
	}
	if ent.Type != tipoColecao || ent.Title != "Matrix Colecao" || ent.ExternalIDTmdb.String != "2344" {
		t.Errorf("colecao = {type:%q title:%q tmdb:%q}, quero {collection, Matrix Colecao, 2344}",
			ent.Type, ent.Title, ent.ExternalIDTmdb.String)
	}
	if n := relacoesColecao(t, database, colID); n != 2 {
		t.Errorf("relacoes collection = %d, quero 2", n)
	}
	for _, tmdbID := range []string{"603", "604"} {
		if _, err := r.ObterEntityPorTmdb(ctx, nsValid(tmdbID)); err != nil {
			t.Errorf("filme %s nao importado: %v", tmdbID, err)
		}
	}
	asset, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: colID, Kind: kindPoster, Origin: originExternal,
	})
	if err != nil {
		t.Fatalf("poster da colecao nao cacheado: %v", err)
	}
	if !strings.HasPrefix(asset.PathOrUri, dir) {
		t.Errorf("path do poster = %q, quero sob %q", asset.PathOrUri, dir)
	}
}

// TestReimportarColecaoIdempotente: re-importar a mesma coleção reusa a entity (dedup
// por tmdb id) e NÃO duplica as relações (guarda de já-ligados + UNIQUE).
func TestReimportarColecaoIdempotente(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeColProvider{
		fakeProvider: &fakeProvider{raw: json.RawMessage(`{"title":"Filme"}`), campos: map[string]any{"title": "Filme"}},
		col:          colDeTeste(nil),
	}
	svc, _ := novoServico(t, database, fake)

	colID1, criada1, err := svc.Importar(ctx, "tmdb", "2344", "collection", []string{"pt-BR"})
	if err != nil || !criada1 {
		t.Fatalf("1o import: id=%s criada=%v err=%v", colID1, criada1, err)
	}
	colID2, criada2, err := svc.Importar(ctx, "tmdb", "2344", "collection", []string{"pt-BR"})
	if err != nil {
		t.Fatalf("2o import: %v", err)
	}
	if colID2 != colID1 {
		t.Errorf("reimport criou nova colecao: %s != %s", colID2, colID1)
	}
	if criada2 {
		t.Error("criada = true no reimport, quero false")
	}
	if n := relacoesColecao(t, database, colID1); n != 2 {
		t.Errorf("relacoes apos reimport = %d, quero 2 (sem duplicar)", n)
	}
}
