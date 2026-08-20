package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// provTmdbArte e um provider Nome "tmdb" cujo MapearDetalhe devolve poster_url + backdrop_url
// (hosts proxyaveis), para o /artwork extrair o poster/fundo da camada externa.
type provTmdbArte struct{}

func (p *provTmdbArte) Nome() string { return "tmdb" }
func (p *provTmdbArte) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (p *provTmdbArte) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (p *provTmdbArte) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{
		"poster_url":   "https://image.tmdb.org/t/p/w342/p.jpg",
		"backdrop_url": "https://image.tmdb.org/t/p/w780/bg.jpg",
	}, nil
}

// semearMediaExternal grava uma camada media_external crua para a entity.
func semearMediaExternal(t *testing.T, d *db.DB, id, entityID, source, payload string) {
	t.Helper()
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: id, EntityID: entityID, Source: source, SourceID: nsOf(""), PayloadJson: nsOf(payload),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal(%s): %v", id, err)
	}
}

// semearAssetOrigin cria um asset com origin/meta explicitos (para exercitar dedup e preferida).
func semearAssetOrigin(t *testing.T, d *db.DB, id, entityID, kind, origin, path, meta string) {
	t.Helper()
	p := sqlcgen.CriarAssetParams{ID: id, EntityID: entityID, Kind: kind, Origin: origin, PathOrUri: path}
	if meta != "" {
		p.MetaJson = nsOf(meta)
	}
	if _, err := escrita(t, d).CriarAsset(context.Background(), p); err != nil {
		t.Fatalf("CriarAsset(%s): %v", id, err)
	}
}

// respostaArtwork decodifica a resposta do /artwork em (grupos por kind, available).
func respostaArtwork(t *testing.T, body []byte) (map[string][]artworkItem, map[string]int) {
	t.Helper()
	var bruto map[string]json.RawMessage
	if err := json.Unmarshal(body, &bruto); err != nil {
		t.Fatalf("decodificar artwork: %v (corpo=%s)", err, body)
	}
	available := map[string]int{}
	_ = json.Unmarshal(bruto["available"], &available)
	grupos := map[string][]artworkItem{}
	for k, raw := range bruto {
		if k == "available" {
			continue
		}
		var itens []artworkItem
		if err := json.Unmarshal(raw, &itens); err != nil {
			t.Fatalf("kind %q invalido: %v", k, err)
		}
		grupos[k] = itens
	}
	return grupos, available
}

// TestArtworkAgrupaMesclaProxyaEDedup: o /artwork mescla assets locais + poster/fundo do
// TMDB + inventario fanart.tv, agrupa por kind, proxya as URLs, deduplica o poster do TMDB
// contra o cache local origin=external e emite `available:{kind:count}`.
func TestArtworkAgrupaMesclaProxyaEDedup(t *testing.T) {
	h, d := setupSearchServer(t, WithSearchProvider(&provTmdbArte{}), WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "e1", "movie", "Matrix")

	// Cache local do poster do TMDB (origin=external, preferido) -> deduplica o poster do TMDB.
	semearAssetOrigin(t, d, "a1", "e1", "poster", "external", "/x/poster.jpg", `{"preferred":true}`)
	// Camada TMDB crua (o MapearDetalhe do provTmdbArte da poster+backdrop).
	semearMediaExternal(t, d, "me-tmdb", "e1", "tmdb", `{}`)
	// Inventario fanart.tv: 2 posters + 1 logo.
	inv, _ := json.Marshal(map[string]any{
		provider.ChaveEnvelopeInventarioArtes: map[string]any{
			"poster": []provider.ArteInventario{
				{URL: "https://assets.fanart.tv/p-en.jpg", Lang: "en", Likes: 10},
				{URL: "https://assets.fanart.tv/p-textless.jpg", Lang: "", Likes: 4},
			},
			"logo": []provider.ArteInventario{{URL: "https://assets.fanart.tv/logo.png", Lang: "en", Likes: 7}},
		},
	})
	semearMediaExternal(t, d, "me-fa", "e1", "fanarttv", string(inv))

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET artwork status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	grupos, available := respostaArtwork(t, rec.Body.Bytes())

	// poster: 1 local (external) + 2 fanart.tv = 3; o poster do TMDB e DEDUPLICADO.
	if available["poster"] != 3 {
		t.Errorf("available.poster = %d, quero 3 (1 local + 2 fanarttv, TMDB deduplicado)", available["poster"])
	}
	// fanart: so o backdrop do TMDB (nao ha fanart local nem fanart.tv).
	if available["fanart"] != 1 {
		t.Errorf("available.fanart = %d, quero 1 (backdrop TMDB)", available["fanart"])
	}
	// logo: 1 do inventario fanart.tv.
	if available["logo"] != 1 {
		t.Errorf("available.logo = %d, quero 1", available["logo"])
	}

	exigirLocalPreferido(t, grupos["poster"])
	exigirProxyFanart(t, grupos["poster"])
	exigirBackdropProxyado(t, grupos["fanart"])
}

// exigirLocalPreferido: ha um item local do dono (rota /assets/.../file), marcado preferido.
func exigirLocalPreferido(t *testing.T, itens []artworkItem) {
	t.Helper()
	for _, it := range itens {
		if it.IsLocal {
			if !strings.Contains(it.URL, "/api/v1/entities/e1/assets/a1/file") {
				t.Errorf("URL local = %q, quero a rota do binario do asset", it.URL)
			}
			if !it.IsPreferred || it.Source != "external" {
				t.Errorf("item local = %+v, quero is_preferred=true source=external", it)
			}
			return
		}
	}
	t.Error("nenhum item local no grupo poster")
}

// exigirProxyFanart: os posters da fanart.tv saem proxyados e preservam lang/likes.
func exigirProxyFanart(t *testing.T, itens []artworkItem) {
	t.Helper()
	var achou bool
	for _, it := range itens {
		if it.Source != "fanarttv" {
			continue
		}
		achou = true
		if !strings.HasPrefix(it.URL, rotaProxyImagem+"?src=") {
			t.Errorf("URL fanart.tv = %q, quero proxyada (%s?src=)", it.URL, rotaProxyImagem)
		}
		if it.Lang == "en" && it.Likes != 10 {
			t.Errorf("item en likes = %d, quero 10", it.Likes)
		}
	}
	if !achou {
		t.Error("nenhum poster fanart.tv no grupo")
	}
}

// exigirBackdropProxyado: o fundo do TMDB sai proxyado sob source=tmdb.
func exigirBackdropProxyado(t *testing.T, itens []artworkItem) {
	t.Helper()
	if len(itens) != 1 {
		t.Fatalf("fanart itens = %d, quero 1", len(itens))
	}
	it := itens[0]
	if it.Source != "tmdb" || !strings.HasPrefix(it.URL, rotaProxyImagem+"?src=") {
		t.Errorf("backdrop = %+v, quero source=tmdb proxyado", it)
	}
}

// TestArtworkInventarioMultiplasFontes (briefing 0116): o /artwork mescla o inventario de
// QUALQUER fonte cacheada em media_external, nao so 'fanarttv' -- regressao real achada em
// producao onde o inventario do MusicBrainz/Cover Art Archive (source='musicbrainz', capa
// de album) era gravado com sucesso mas NUNCA aparecia no /artwork porque a leitura estava
// hardcoded a source='fanarttv' (briefing 0048, antes de existir uma 2a fonte de arte).
// Semeia inventario sob DUAS fontes diferentes e exige que ambas apareçam mescladas.
func TestArtworkInventarioMultiplasFontes(t *testing.T) {
	h, d := setupSearchServer(t, WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "alb1", "album", "Collected")

	invFanart, _ := json.Marshal(map[string]any{
		provider.ChaveEnvelopeInventarioArtes: map[string]any{
			"logo": []provider.ArteInventario{{URL: "https://assets.fanart.tv/logo.png", Likes: 5}},
		},
	})
	semearMediaExternal(t, d, "me-fa", "alb1", "fanarttv", string(invFanart))

	invMusicbrainz, _ := json.Marshal(map[string]any{
		provider.ChaveEnvelopeInventarioArtes: map[string]any{
			"poster": []provider.ArteInventario{{URL: "https://coverartarchive.org/release/x/front.jpg"}},
			"back":   []provider.ArteInventario{{URL: "https://coverartarchive.org/release/x/back.jpg"}},
		},
	})
	semearMediaExternal(t, d, "me-mb", "alb1", "musicbrainz", string(invMusicbrainz))

	rec := do(t, h, http.MethodGet, "/api/v1/entities/alb1/artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET artwork status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	grupos, available := respostaArtwork(t, rec.Body.Bytes())

	if available["poster"] != 1 {
		t.Errorf("available.poster = %d, quero 1 (Cover Art Archive/musicbrainz)", available["poster"])
	}
	if available["back"] != 1 {
		t.Errorf("available.back = %d, quero 1 (Cover Art Archive/musicbrainz)", available["back"])
	}
	if available["logo"] != 1 {
		t.Errorf("available.logo = %d, quero 1 (fanart.tv)", available["logo"])
	}
	if len(grupos["poster"]) != 1 || grupos["poster"][0].Source != "musicbrainz" {
		t.Errorf("grupos[poster] = %+v, quero 1 item source=musicbrainz", grupos["poster"])
	}
	if len(grupos["logo"]) != 1 || grupos["logo"][0].Source != "fanarttv" {
		t.Errorf("grupos[logo] = %+v, quero 1 item source=fanarttv", grupos["logo"])
	}
}

// TestArtworkInventarioDedupeContraMaterializado: uma arte do inventario fanart.tv cujo
// url ja foi materializado (asset origin=external com meta.source_url igual) NAO aparece
// de novo como candidato -- so a versao local conta (briefing 0072).
func TestArtworkInventarioDedupeContraMaterializado(t *testing.T) {
	h, d := setupSearchServer(t, WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "e1", "movie", "Matrix")

	// Ja materializado via POST /artwork/select: asset external com o source_url apontando
	// para a MESMA url que ainda esta no inventario fanart.tv.
	semearAssetOrigin(t, d, "a1", "e1", "poster", "external", "/x/poster.jpg",
		`{"source_url":"https://assets.fanart.tv/p-en.jpg"}`)

	inv, _ := json.Marshal(map[string]any{
		provider.ChaveEnvelopeInventarioArtes: map[string]any{
			"poster": []provider.ArteInventario{
				{URL: "https://assets.fanart.tv/p-en.jpg", Lang: "en", Likes: 10},
				{URL: "https://assets.fanart.tv/p-textless.jpg", Lang: "", Likes: 4},
			},
		},
	})
	semearMediaExternal(t, d, "me-fa", "e1", "fanarttv", string(inv))

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET artwork status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	grupos, available := respostaArtwork(t, rec.Body.Bytes())

	// poster: 1 local (ja materializado) + 1 candidato restante do inventario (p-textless) = 2.
	// Sem o dedupe seria 3 (o p-en apareceria duas vezes: local E candidato).
	if available["poster"] != 2 {
		t.Errorf("available.poster = %d, quero 2 (dedupe do candidato ja materializado)", available["poster"])
	}
	var candidatosFanart int
	for _, it := range grupos["poster"] {
		if it.Source == "fanarttv" {
			candidatosFanart++
			if it.SrcURL == "https://assets.fanart.tv/p-en.jpg" {
				t.Errorf("candidato ja materializado (p-en.jpg) nao deveria reaparecer no inventario")
			}
		}
	}
	if candidatosFanart != 1 {
		t.Errorf("candidatos fanart.tv restantes = %d, quero 1 (p-textless)", candidatosFanart)
	}
}

// TestArtworkEntidadeInexistente: 404 para entity que nao existe.
func TestArtworkEntidadeInexistente(t *testing.T) {
	h, _ := setupSearchServer(t, WithSearchProvider(&provTmdbArte{}), WithAssetsDir(t.TempDir()))
	rec := do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/artwork", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestArtworkSemFanartFunciona: sem inventario fanart.tv nem camada externa, o /artwork
// ainda responde 200 com os assets locais (regressao zero).
func TestArtworkSemFanartFunciona(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e2", "movie", "Item")
	semearAsset(t, d, "a2", "e2", "poster", "/x/local.jpg")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e2/artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	_, available := respostaArtwork(t, rec.Body.Bytes())
	if available["poster"] != 1 {
		t.Errorf("available.poster = %d, quero 1 (so o asset local)", available["poster"])
	}
}

// TestArtworkOrdenaPorIdiomaPreferido (briefing 0089): /artwork sem nenhuma
// configuracao explicita de idioma cai no default pt-BR (defaultLanguageSettings);
// com pt/en/textless disponiveis no inventario fanart.tv, o item em pt vem primeiro.
func TestArtworkOrdenaPorIdiomaPreferido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e3", "movie", "Item")

	inv, _ := json.Marshal(map[string]any{
		provider.ChaveEnvelopeInventarioArtes: map[string]any{
			"poster": []provider.ArteInventario{
				{URL: "https://assets.fanart.tv/p-en.jpg", Lang: "en", Likes: 10},
				{URL: "https://assets.fanart.tv/p-textless.jpg", Lang: "", Likes: 4},
				{URL: "https://assets.fanart.tv/p-pt.jpg", Lang: "pt", Likes: 1},
			},
		},
	})
	semearMediaExternal(t, d, "me-fa", "e3", "fanarttv", string(inv))

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e3/artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	grupos, _ := respostaArtwork(t, rec.Body.Bytes())
	itens := grupos["poster"]
	if len(itens) != 3 {
		t.Fatalf("poster itens = %d, quero 3", len(itens))
	}
	if itens[0].Lang != "pt" {
		t.Errorf("primeiro item lang = %q, quero \"pt\" (idioma preferido default pt-BR)", itens[0].Lang)
	}
}

// --- Testes unitarios de ordenarGruposArtwork/idiomaBase/rankIdiomaArte (briefing 0089) ---

// TestIdiomaBaseNormalizaLocale: idiomaBase extrai o ISO 639-1 de um locale completo
// (formato de app_settings.languages/idiomaDeBusca) para bater com o `lang` puro do
// inventario fanart.tv.
func TestIdiomaBaseNormalizaLocale(t *testing.T) {
	casos := []struct{ entrada, quero string }{
		{"pt-BR", "pt"},
		{"en", "en"},
		{"", ""},
		{"PT-br", "pt"},
		{"es-ES", "es"},
	}
	for _, c := range casos {
		if got := idiomaBase(c.entrada); got != c.quero {
			t.Errorf("idiomaBase(%q) = %q, quero %q", c.entrada, got, c.quero)
		}
	}
}

// TestRankIdiomaArte: idioma preferido normalizado sempre rank 0; textless rank 1;
// ingles rank 2; qualquer outro rank 3.
func TestRankIdiomaArte(t *testing.T) {
	casos := []struct {
		lang, preferidoBase string
		quero               int
	}{
		{"pt", "pt", 0},
		{"", "pt", 1},
		{"en", "pt", 2},
		{"fr", "pt", 3},
		{"", "", 1},   // sem preferencia: textless ainda vence ingles/outro
		{"en", "", 2}, // sem preferencia: ingles vence "qualquer outro"
		{"pt", "", 3}, // sem preferencia (base vazia): pt cai em "qualquer outro"
	}
	for _, c := range casos {
		if got := rankIdiomaArte(c.lang, c.preferidoBase); got != c.quero {
			t.Errorf("rankIdiomaArte(%q, %q) = %d, quero %d", c.lang, c.preferidoBase, got, c.quero)
		}
	}
}

// TestOrdenarGruposArtworkPrioridadeIdioma: com pt/en/textless disponiveis e idioma
// preferido "pt-BR", a arte pt vem primeiro (precisa normalizar via idiomaBase -- uma
// comparacao direta "pt-BR" == "pt" nunca bateria).
func TestOrdenarGruposArtworkPrioridadeIdioma(t *testing.T) {
	grupos := map[string][]artworkItem{
		"poster": {
			{SrcURL: "en", Lang: "en"},
			{SrcURL: "textless", Lang: ""},
			{SrcURL: "pt", Lang: "pt"},
		},
	}
	ordenarGruposArtwork(grupos, "pt-BR")
	if got := grupos["poster"][0].Lang; got != "pt" {
		t.Errorf("primeiro item lang = %q, quero \"pt\"", got)
	}
}

// TestOrdenarGruposArtworkSemPtCaiParaTextless: sem nenhuma arte no idioma preferido,
// a textless (lang=="") vem antes da en e de qualquer outro idioma.
func TestOrdenarGruposArtworkSemPtCaiParaTextless(t *testing.T) {
	grupos := map[string][]artworkItem{
		"poster": {
			{SrcURL: "outro", Lang: "fr"},
			{SrcURL: "en", Lang: "en"},
			{SrcURL: "textless", Lang: ""},
		},
	}
	ordenarGruposArtwork(grupos, "pt-BR")
	itens := grupos["poster"]
	if itens[0].Lang != "" {
		t.Errorf("primeiro item lang = %q, quero \"\" (textless)", itens[0].Lang)
	}
	if itens[1].Lang != "en" {
		t.Errorf("segundo item lang = %q, quero \"en\"", itens[1].Lang)
	}
	if itens[2].Lang != "fr" {
		t.Errorf("terceiro item lang = %q, quero \"fr\" (qualquer outro, por ultimo)", itens[2].Lang)
	}
}

// TestOrdenarGruposArtworkIsPrincipalVenceIdioma: uma arte ja marcada is_principal num
// idioma "pior" (fr, quando o preferido e pt) continua vencendo -- a prioridade de idioma
// nunca derruba is_principal/is_local_in_library.
func TestOrdenarGruposArtworkIsPrincipalVenceIdioma(t *testing.T) {
	grupos := map[string][]artworkItem{
		"poster": {
			{SrcURL: "pt", Lang: "pt"},
			{SrcURL: "principal-fr", Lang: "fr", IsPrincipal: true},
		},
	}
	ordenarGruposArtwork(grupos, "pt-BR")
	if got := grupos["poster"][0].SrcURL; got != "principal-fr" {
		t.Errorf("primeiro item = %q, quero \"principal-fr\" (is_principal vence idioma)", got)
	}

	gruposLocal := map[string][]artworkItem{
		"poster": {
			{SrcURL: "pt", Lang: "pt"},
			{SrcURL: "local-fr", Lang: "fr", IsLocalInLibrary: true},
		},
	}
	ordenarGruposArtwork(gruposLocal, "pt-BR")
	if got := gruposLocal["poster"][0].SrcURL; got != "local-fr" {
		t.Errorf("primeiro item = %q, quero \"local-fr\" (is_local_in_library vence idioma)", got)
	}
}

// TestOrdenarGruposArtworkEmpateMantemOrdemOriginal: mesmo rank de idioma, mesmo
// is_principal, mesmo is_local_in_library -- a ordem original de chegada e preservada
// (sort.SliceStable).
func TestOrdenarGruposArtworkEmpateMantemOrdemOriginal(t *testing.T) {
	grupos := map[string][]artworkItem{
		"poster": {
			{SrcURL: "primeiro", Lang: "fr"},
			{SrcURL: "segundo", Lang: "fr"},
			{SrcURL: "terceiro", Lang: "fr"},
		},
	}
	ordenarGruposArtwork(grupos, "pt-BR")
	itens := grupos["poster"]
	quero := []string{"primeiro", "segundo", "terceiro"}
	for i, q := range quero {
		if itens[i].SrcURL != q {
			t.Errorf("posicao %d = %q, quero %q (ordem original preservada)", i, itens[i].SrcURL, q)
		}
	}
}
