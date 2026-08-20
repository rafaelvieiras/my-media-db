package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Testes do briefing 0047 (robustez de fonte-default): sem `source` explícito, a fonte
// é resolvida DINAMICAMENTE por capacidade — nunca assume "tmdb". E o interno-first casa
// itens de QUALQUER fonte com coluna gerada (tvdb inclusive), não só tmdb.

// provNaoTmdb é um provider de teste com Nome() CONFIGURÁVEL (≠ "tmdb") e um detalhe
// fixo — exercita o caminho de import por uma fonte que não é o TMDB.
type provNaoTmdb struct{ nome string }

func (p *provNaoTmdb) Nome() string { return p.nome }

func (p *provNaoTmdb) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (p *provNaoTmdb) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{"title":"Item Externo"}`), nil
}

func (p *provNaoTmdb) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{"title": "Item Externo"}, nil
}

// provSerieNaoTmdb estende provNaoTmdb com a capacidade SerieProvider (árvore vazia
// basta ao type-assert de capacidade — o conteúdo não importa para a seleção de fonte).
type provSerieNaoTmdb struct{ provNaoTmdb }

func (p *provSerieNaoTmdb) BuscarSerieArvore(context.Context, string, []string) (provider.SerieArvore, error) {
	return provider.SerieArvore{}, nil
}

// provBuscaFonte devolve resultados de busca FIXOS sob um Nome() configurável, para o
// interno-first casar a fonte real do resultado (r.Source) contra a biblioteca.
type provBuscaFonte struct {
	nome string
	res  []provider.ResultadoExterno
}

func (p *provBuscaFonte) Nome() string { return p.nome }

func (p *provBuscaFonte) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return p.res, nil
}

func (p *provBuscaFonte) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return nil, errors.New("não implementado")
}

func (p *provBuscaFonte) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// TestFonteDefaultResolveDinamicamente: fonteDefault escolhe o primeiro provider CAPAZ do
// tipo (por prioridade), sem assumir "tmdb". Um SerieProvider não-tmdb resolve 'movie' E
// 'tv'; um provider core-only (sem SerieProvider) resolve 'movie' mas NÃO 'tv'.
func TestFonteDefaultResolveDinamicamente(t *testing.T) {
	_, d := setupEntitiesServer(t)

	// Só um provider capaz de série, chamado "tvdb" (não-tmdb).
	sCapaz := New("v-teste", d, WithSearchProvider(&provSerieNaoTmdb{provNaoTmdb{nome: "tvdb"}}))
	if src, ok := sCapaz.fonteDefault("movie"); !ok || src != "tvdb" {
		t.Errorf("fonteDefault(movie) = (%q,%v), quero (tvdb,true)", src, ok)
	}
	if src, ok := sCapaz.fonteDefault("tv"); !ok || src != "tvdb" {
		t.Errorf("fonteDefault(tv) = (%q,%v), quero (tvdb,true)", src, ok)
	}

	// Só um provider core-only ("letterboxd"): resolve movie, mas não série.
	sCore := New("v-teste", d, WithSearchProvider(&provNaoTmdb{nome: "letterboxd"}))
	if src, ok := sCore.fonteDefault("movie"); !ok || src != "letterboxd" {
		t.Errorf("fonteDefault(movie) core = (%q,%v), quero (letterboxd,true)", src, ok)
	}
	if src, ok := sCore.fonteDefault("tv"); ok {
		t.Errorf("fonteDefault(tv) core = (%q,%v), quero (\"\",false) sem SerieProvider", src, ok)
	}

	// Sem provider algum: nenhuma fonte para nenhum tipo.
	sVazio := New("v-teste", d)
	if src, ok := sVazio.fonteDefault("movie"); ok {
		t.Errorf("fonteDefault(movie) vazio = (%q,%v), quero (\"\",false)", src, ok)
	}
}

// TestImportSemSourceUsaFonteNaoTmdb: um ambiente com SÓ um provider não-tmdb ("tvdb")
// importa um filme SEM `source` explícito pela fonte realmente registrada — NÃO cai em
// 502 por assumir "tmdb". A identidade externa gravada é a da fonte escolhida.
func TestImportSemSourceUsaFonteNaoTmdb(t *testing.T) {
	h, _ := setupSearchServer(t, WithSearchProvider(&provNaoTmdb{nome: "tvdb"}), WithAssetsDir(t.TempDir()))

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "500", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import sem source status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	eq(t, "external_ids.tvdb", ent.ExternalIDs["tvdb"], "500")
	if _, temTmdb := ent.ExternalIDs["tmdb"]; temTmdb {
		t.Errorf("external_ids não deveria ter tmdb (fonte foi tvdb): %v", ent.ExternalIDs)
	}
}

// TestImportTvSemFonteCapaz422: import de série SEM `source` num ambiente cujo único
// provider é core-only (sem SerieProvider) responde 422 SEMÂNTICO ("nenhuma fonte
// disponível para tv"), NÃO o 502 genérico de fonte indisponível.
func TestImportTvSemFonteCapaz422(t *testing.T) {
	h, _ := setupSearchServer(t, WithSearchProvider(&provNaoTmdb{nome: "letterboxd"}), WithAssetsDir(t.TempDir()))

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "1", "type": "tv"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("import tv sem fonte capaz status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "no_source_for_type")
}

// provMusicaNaoTmdb estende provNaoTmdb com as capacidades de música (MusicaProvider +
// MusicaAlbumProvider, ADR-0027) -- exercita provedorCapaz("artista"/"album"/"faixa"),
// achado ao validar o botão "Baixar Metadados" pra artista/álbum em produção: sem essas
// capacidades explícitas em provedorCapaz, o `default: true` aceitava QUALQUER provider
// registrado (inclusive um "tmdb" sem nada a ver com música) como capaz de 'artista'/
// 'album', e fonteDefault o escolhia por prioridade mesmo sem saber servir o tipo.
type provMusicaNaoTmdb struct{ provNaoTmdb }

func (p *provMusicaNaoTmdb) BuscarArtistaArvore(context.Context, string, []string) (provider.ArtistaArvore, error) {
	return provider.ArtistaArvore{}, nil
}

func (p *provMusicaNaoTmdb) BuscarAlbumDaFaixa(context.Context, string, []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (p *provMusicaNaoTmdb) BuscarAlbumPorNome(context.Context, string, string, []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (p *provMusicaNaoTmdb) BuscarAlbumPorMbid(context.Context, string, []string) (provider.AlbumExterno, error) {
	return provider.AlbumExterno{}, nil
}

// TestFonteDefaultMusicaNaoEscolheProviderSemCapacidade: com um provider SEM música
// registrado ANTES (maior prioridade) de um provider DE música, fonteDefault('artista'/
// 'album'/'faixa') pula o primeiro e escolhe o musical -- nunca assume "o primeiro
// registrado serve" para os tipos de música (regressão do bug relatado acima).
func TestFonteDefaultMusicaNaoEscolheProviderSemCapacidade(t *testing.T) {
	_, d := setupEntitiesServer(t)

	s := New("v-teste", d, WithSearchProvider(
		&provNaoTmdb{nome: "tmdb"},
		&provMusicaNaoTmdb{provNaoTmdb{nome: "musicbrainz"}},
	))

	for _, tipo := range []string{"artista", "album", "faixa"} {
		if src, ok := s.fonteDefault(tipo); !ok || src != "musicbrainz" {
			t.Errorf("fonteDefault(%s) = (%q,%v), quero (musicbrainz,true)", tipo, src, ok)
		}
	}
}

// TestFonteDefaultMusicaSemProviderCapaz422: um ambiente cujo único provider registrado
// não modela música (core-only, tipo "tmdb") não tem fonte-default para 'artista'/
// 'album'/'faixa' -- 422 semântico, nunca um 502/422 "sem source_id" por escolher uma
// fonte incapaz.
func TestFonteDefaultMusicaSemProviderCapaz422(t *testing.T) {
	_, d := setupEntitiesServer(t)
	s := New("v-teste", d, WithSearchProvider(&provNaoTmdb{nome: "tmdb"}))

	for _, tipo := range []string{"artista", "album", "faixa"} {
		if src, ok := s.fonteDefault(tipo); ok {
			t.Errorf("fonteDefault(%s) = (%q,%v), quero (\"\",false) sem provider de música", tipo, src, ok)
		}
	}
}

// TestIdInternoItemFonteTvdb: o interno-first resolve um item importado de fonte NÃO-tmdb
// (tvdb, via coluna gerada external_id_tvdb) — não só tmdb. Fonte sem coluna sai nil.
func TestIdInternoItemFonteTvdb(t *testing.T) {
	_, d := setupEntitiesServer(t)
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              "ent-tvdb-1",
		Type:            "tv",
		Title:           "Serie Só-TVDB",
		ExternalIdsJson: nsOf(`{"tvdb":"9001"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity(tvdb): %v", err)
	}
	s := New("v-teste", d)
	ctx := context.Background()

	if got := s.idInternoItem(ctx, "tvdb", "9001"); got == nil || *got != "ent-tvdb-1" {
		t.Errorf("idInternoItem(tvdb,9001) = %v, quero ent-tvdb-1", got)
	}
	if got := s.idInternoItem(ctx, "tvdb", "404"); got != nil {
		t.Errorf("idInternoItem(tvdb,404) = %v, quero nil (inexistente)", got)
	}
	if got := s.idInternoItem(ctx, "letterboxd", "9001"); got != nil {
		t.Errorf("idInternoItem(letterboxd,9001) = %v, quero nil (fonte sem coluna gerada)", got)
	}
}

// TestSearchInternoFirstFonteTvdb: um resultado de busca de fonte tvdb, cujo item já está
// na biblioteca, ganha internal_id ("na biblioteca") — o interno-first não é mais só-tmdb.
func TestSearchInternoFirstFonteTvdb(t *testing.T) {
	prov := &provBuscaFonte{
		nome: "tvdb",
		res: []provider.ResultadoExterno{
			{Source: "tvdb", SourceID: "9001", Type: "tv", Title: "Serie Só-TVDB"},
			{Source: "tvdb", SourceID: "8888", Type: "tv", Title: "Serie Ausente"},
		},
	}
	h, d := setupSearchServer(t, WithSearchProvider(prov))
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              "ent-tvdb-1",
		Type:            "tv",
		Title:           "Serie Só-TVDB",
		ExternalIdsJson: nsOf(`{"tvdb":"9001"}`),
	})
	if err != nil {
		t.Fatalf("CriarEntity(tvdb): %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/search?q=serie", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeSearch(t, rec.Body.Bytes())
	byID := map[string]externalResultDTO{}
	for _, e := range out.External {
		byID[e.SourceID] = e
	}
	if e := byID["9001"]; e.InternalID == nil || *e.InternalID != "ent-tvdb-1" {
		t.Errorf("resultado tvdb 9001 internal_id = %v, quero ent-tvdb-1", e.InternalID)
	}
	if e := byID["8888"]; e.InternalID != nil {
		t.Errorf("resultado tvdb 8888 internal_id = %v, quero nil (não importado)", e.InternalID)
	}
}
