package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
)

// Estes testes cobrem o briefing 0100 (ADR-0024): a separação entre a
// capacidade de BUSCA AO VIVO (exige credencial, registro `providers`) e a
// capacidade de PARSE de payload já cacheado (registro `codecs`, sempre
// disponível independente de credencial). Um item já catalogado (media_external/
// external_detail já gravados) deve continuar rico na tela mesmo sem NENHUM
// provider de busca ao vivo registrado -- só WithCodecRegistry, o que o main.go
// faz incondicionalmente via provedoresConhecidos().

// TestEntityRicaComCodecRegistrySemBuscaAoVivo (critério de aceite (a)): uma
// entity com media_external já cacheado continua trazendo campos ricos (aqui,
// "similar", que já é fiado via camadasDaFonte/codecPara) quando a fonte só
// está registrada em WithCodecRegistry -- NENHUM provider de busca ao vivo
// (WithSearchProvider) está presente.
func TestEntityRicaComCodecRegistrySemBuscaAoVivo(t *testing.T) {
	prov := &fakeProviderComSimilar{
		fakeProviderSimilar: fakeProviderSimilar{nome: "tmdb"},
		sims:                simSims(),
	}
	// Só o codec -- prova que a leitura NÃO depende de busca ao vivo.
	h, d := setupSearchServer(t, WithCodecRegistry(prov))
	semearEntity(t, d, "x", "movie", "Matrix")
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: "me-x", EntityID: "x", Source: "tmdb",
		SourceID: nsOf("603"), PayloadJson: nsOf(`{}`),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal: %v", err)
	}

	ent := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/x", nil))
	if len(ent.Similar) != 1 {
		t.Fatalf("similar = %+v, quero 1 item (codec deveria resolver mesmo sem busca ao vivo)", ent.Similar)
	}
	eq(t, "similar.source_id", ent.Similar[0].SourceID, "604")
}

// TestExternalDetailStaleSemBuscaAoVivo (critério de aceite (b)): com um cache
// external_detail já gravado e EXPIRADO (fetched_at além do TTL), e a fonte só
// registrada em WithCodecRegistry (sem WithSearchProvider), GET
// /external/{source}/{type}/{id} serve o payload STALE (200) em vez de
// 503/404 -- o dado já visitado não some por falta de credencial.
func TestExternalDetailStaleSemBuscaAoVivo(t *testing.T) {
	prov := tmdb.New(tmdb.Credencial{}, nil, "") // sem credencial: só parse (codec)
	h, d := setupSearchServer(t, WithCodecRegistry(prov))

	// O payload persistido pelo serviço é sempre o ENVELOPE AUMENTADO (0020) que
	// BuscarDetalhe monta ({detail,tags_by_lang}), não o JSON legado cru -- casa o
	// formato que MapearDetalhe realmente espera para resolver por idioma.
	envelope := `{"detail":` + filmeRicoJSON + `,"tags_by_lang":{}}`
	if err := sqlcgen.New(d.Write).UpsertExternalDetail(context.Background(), sqlcgen.UpsertExternalDetailParams{
		Source: "tmdb", SourceID: "603", Type: "movie",
		PayloadJson: nsOf(envelope), LangsJson: nsOf(`["pt-BR","en"]`),
	}); err != nil {
		t.Fatalf("UpsertExternalDetail: %v", err)
	}
	// Forca o carimbo bem no passado -- além do TTLPadrao de 24h.
	if _, err := d.Write.ExecContext(context.Background(),
		`UPDATE external_detail SET fetched_at = '2000-01-01T00:00:00.000Z' WHERE source='tmdb' AND source_id='603'`); err != nil {
		t.Fatalf("forcar staleness: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (stale servido) (corpo=%s)", rec.Code, rec.Body.String())
	}
	d2 := decodeDetalheExterno(t, rec.Body.Bytes())
	eq(t, "title", d2.Title, "Matrix")
}

// TestExternalDetail503FonteTotalmenteDesconhecida garante que uma fonte sem
// NENHUM registro (nem codecs nem providers) continua 503 -- o comportamento
// não regride para fontes que o binário nem sabe interpretar.
func TestExternalDetail503FonteTotalmenteDesconhecida(t *testing.T) {
	h, _ := setupSearchServer(t) // nem codec, nem busca ao vivo
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "external_source_unavailable")
}
