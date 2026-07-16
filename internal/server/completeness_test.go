package server

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Testes da AVALIAÇÃO DE COMPLETUDE DE CATALOGAÇÃO por item (briefing 0058):
// avaliarCompletude isolado (sem banco) e os dois caminhos que a consomem —
// ao vivo no detalhe (GET /entities/{id}) e o cache em lote (completeness_
// recalc_test.go). Ver a nota longa em completeness.go sobre por que os dois
// caminhos existem.

// ── avaliarCompletude (pura, sem banco) ─────────────────────────────────────

// TestAvaliarCompletudeTipoAusenteNaConfig cobre a regra central do 0057/0058:
// tipo sem entrada na config nunca é incompleto, mesmo sem nenhum dado.
func TestAvaliarCompletudeTipoAusenteNaConfig(t *testing.T) {
	got := avaliarCompletude(tipoCompletenessConfig{Description: true}, false, dadosCompletude{})
	if !got.Complete {
		t.Errorf("complete = false, quero true (tipo ausente = nada exigido)")
	}
	if len(got.Missing) != 0 {
		t.Errorf("missing = %v, quero []", got.Missing)
	}
}

// TestAvaliarCompletudeCadaBool cobre cada bool de tipoCompletenessConfig,
// isolado: ausente -> aparece em Missing; presente -> não aparece.
func TestAvaliarCompletudeCadaBool(t *testing.T) {
	desc := "uma sinopse"
	descVazia := "   "
	runtime := 90
	cert := "L"

	casos := []struct {
		nome  string
		cfg   tipoCompletenessConfig
		dados dadosCompletude
		quer  []string
	}{
		{"description ausente (nil)", tipoCompletenessConfig{Description: true}, dadosCompletude{}, []string{"description"}},
		{"description vazia conta como ausente", tipoCompletenessConfig{Description: true}, dadosCompletude{Description: &descVazia}, []string{"description"}},
		{"description presente", tipoCompletenessConfig{Description: true}, dadosCompletude{Description: &desc}, nil},
		{"runtime ausente", tipoCompletenessConfig{Runtime: true}, dadosCompletude{}, []string{"runtime"}},
		{"runtime presente", tipoCompletenessConfig{Runtime: true}, dadosCompletude{Runtime: &runtime}, nil},
		{"certification ausente", tipoCompletenessConfig{Certification: true}, dadosCompletude{}, []string{"certification"}},
		{"certification presente", tipoCompletenessConfig{Certification: true}, dadosCompletude{Certification: &cert}, nil},
		{"keywords ausente (vazia)", tipoCompletenessConfig{Keywords: true}, dadosCompletude{}, []string{"keywords"}},
		{"keywords presente", tipoCompletenessConfig{Keywords: true}, dadosCompletude{Keywords: []string{"acao"}}, nil},
		{"cast ausente", tipoCompletenessConfig{Cast: true}, dadosCompletude{}, []string{"cast"}},
		{"cast presente", tipoCompletenessConfig{Cast: true}, dadosCompletude{TemElenco: true}, nil},
		{"media_file ausente", tipoCompletenessConfig{MediaFile: true}, dadosCompletude{}, []string{"media_file"}},
		{"media_file presente", tipoCompletenessConfig{MediaFile: true}, dadosCompletude{TemArquivo: true}, nil},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := avaliarCompletude(c.cfg, true, c.dados)
			if !slices.Equal(got.Missing, c.quer) {
				t.Errorf("missing = %v, quero %v", got.Missing, c.quer)
			}
			querCompleto := len(c.quer) == 0
			if got.Complete != querCompleto {
				t.Errorf("complete = %v, quero %v", got.Complete, querCompleto)
			}
		})
	}
}

// TestAvaliarCompletudeArtwork cobre cada kind de artwork isolado: só os
// kinds AUSENTES de ArtesPresentes entram em Missing (um item por kind, não
// um "artwork" agregado), na ordem de cfg.Artwork.
func TestAvaliarCompletudeArtwork(t *testing.T) {
	cfg := tipoCompletenessConfig{Artwork: []string{"poster", "fanart", "logo"}}
	dados := dadosCompletude{ArtesPresentes: map[string]bool{"fanart": true}}

	got := avaliarCompletude(cfg, true, dados)
	quero := []string{"artwork:poster", "artwork:logo"}
	if !slices.Equal(got.Missing, quero) {
		t.Errorf("missing = %v, quero %v (fanart presente não deveria aparecer)", got.Missing, quero)
	}
	if got.Complete {
		t.Error("complete = true, quero false (2 artes faltando)")
	}
}

// TestAvaliarCompletudeArtworkTodasPresentes cobre o caso completo de
// artwork: todas as kinds configuradas presentes -> nenhuma entrada.
func TestAvaliarCompletudeArtworkTodasPresentes(t *testing.T) {
	cfg := tipoCompletenessConfig{Artwork: []string{"poster", "fanart"}}
	dados := dadosCompletude{ArtesPresentes: map[string]bool{"poster": true, "fanart": true}}

	got := avaliarCompletude(cfg, true, dados)
	if !got.Complete || len(got.Missing) != 0 {
		t.Errorf("got = %+v, quero {true, []}", got)
	}
}

// TestAvaliarCompletudeOrdemFixa cobre a ordem ESTÁVEL de Missing (briefing
// 0058 §4): description, runtime, certification, keywords, cast, media_file,
// artwork:<kind...> (na ordem de cfg.Artwork, não alfabética).
func TestAvaliarCompletudeOrdemFixa(t *testing.T) {
	cfg := tipoCompletenessConfig{
		Description: true, Runtime: true, Certification: true, Keywords: true,
		Cast: true, MediaFile: true, Artwork: []string{"fanart", "poster"},
	}
	got := avaliarCompletude(cfg, true, dadosCompletude{})
	quero := []string{
		"description", "runtime", "certification", "keywords", "cast", "media_file",
		"artwork:fanart", "artwork:poster",
	}
	if !slices.Equal(got.Missing, quero) {
		t.Errorf("missing = %v, quero %v (ordem fixa)", got.Missing, quero)
	}
	if got.Complete {
		t.Error("complete = true, quero false")
	}
}

// TestAvaliarCompletudeTudoPresenteEhCompleto prova o caso feliz: todos os
// campos/artes exigidos presentes -> {true, []}.
func TestAvaliarCompletudeTudoPresenteEhCompleto(t *testing.T) {
	desc, cert := "sinopse", "L"
	runtime := 120
	cfg := tipoCompletenessConfig{
		Description: true, Runtime: true, Certification: true, Keywords: true,
		Cast: true, MediaFile: true, Artwork: []string{"poster"},
	}
	dados := dadosCompletude{
		Description: &desc, Runtime: &runtime, Certification: &cert,
		Keywords: []string{"acao"}, TemElenco: true, TemArquivo: true,
		ArtesPresentes: map[string]bool{"poster": true},
	}
	got := avaliarCompletude(cfg, true, dados)
	if !got.Complete || len(got.Missing) != 0 {
		t.Errorf("got = %+v, quero {true, []}", got)
	}
}

// ── caminho ao vivo (detalhe, GET /entities/{id}) ───────────────────────────

// TestDetalheItemNaoDraftCalculaCompletudeAoVivo cobre o critério central do
// 0058: o detalhe calcula NA HORA, refletindo o estado ATUAL sem depender do
// job — preencher o que falta muda o PRÓXIMO GET, sem recálculo em lote.
func TestDetalheItemNaoDraftCalculaCompletudeAoVivo(t *testing.T) {
	h, d := setupEntitiesServer(t)
	corpo := catalogCompletenessSettings{
		"movie": {Description: true, Artwork: []string{"poster"}},
	}
	if rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo); rec.Code != http.StatusOK {
		t.Fatalf("PUT settings status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	semearEntity(t, d, "m1", "movie", "Filme")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/m1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	if ent.CatalogCompleteness == nil {
		t.Fatal("catalog_completeness = nil, quero calculado (item não-draft)")
	}
	if ent.CatalogCompleteness.Complete {
		t.Error("complete = true, quero false (sem descrição nem pôster)")
	}
	quero := []string{"description", "artwork:poster"}
	if !slices.Equal(ent.CatalogCompleteness.Missing, quero) {
		t.Errorf("missing = %v, quero %v", ent.CatalogCompleteness.Missing, quero)
	}

	// Preenche o que falta -- o PRÓXIMO GET reflete ao vivo, sem rodar job.
	if _, err := escrita(t, d).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		ID: "m1", Type: "movie", Title: "Filme", Status: "cataloged",
		Description: nsOf("Uma sinopse qualquer"),
	}); err != nil {
		t.Fatalf("AtualizarEntity: %v", err)
	}
	semearAsset(t, d, "asset-poster", "m1", "poster", "/data/poster.jpg")

	rec2 := do(t, h, http.MethodGet, "/api/v1/entities/m1", nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET#2 status = %d (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	ent2 := decodeEntidade(t, rec2)
	if ent2.CatalogCompleteness == nil || !ent2.CatalogCompleteness.Complete || len(ent2.CatalogCompleteness.Missing) != 0 {
		t.Errorf("após preencher, catalog_completeness = %+v, quero {true, []}", ent2.CatalogCompleteness)
	}
}

// TestDetalheItemDraftCatalogCompletenessNil cobre o critério: item draft
// (nunca casado) nunca tem completude avaliada -- o campo fica null.
func TestDetalheItemDraftCatalogCompletenessNil(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "d1", "movie", "Draft")
	definirStatus(t, d, "d1", "draft")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/d1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	if ent.CatalogCompleteness != nil {
		t.Errorf("catalog_completeness = %+v, quero nil (item draft)", ent.CatalogCompleteness)
	}
}

// TestDetalheTipoAusenteDaConfigEhCompleto cobre a regra "tipo sem régua
// nunca é incompleto" também no caminho ao vivo (não só na função pura).
func TestDetalheTipoAusenteDaConfigEhCompleto(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// "collection" não tem entrada na config (default de fábrica só cobre
	// movie/tv, briefing 0057 §6) -- nada foi exigido.
	semearEntity(t, d, "col1", "collection", "Coleção")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/col1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	if ent.CatalogCompleteness == nil || !ent.CatalogCompleteness.Complete || len(ent.CatalogCompleteness.Missing) != 0 {
		t.Errorf("catalog_completeness = %+v, quero {true, []} (tipo sem régua)", ent.CatalogCompleteness)
	}
}
