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

// provTmdbArte is a provider named "tmdb" whose MapearDetalhe returns poster_url +
// backdrop_url (proxyable hosts), so /artwork can extract the poster/background of the
// external layer.
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

// semearMediaExternal writes a raw media_external layer for the entity.
func semearMediaExternal(t *testing.T, d *db.DB, id, entityID, source, payload string) {
	t.Helper()
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: id, EntityID: entityID, Source: source, SourceID: nsOf(""), PayloadJson: nsOf(payload),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal(%s): %v", id, err)
	}
}

// semearAssetOrigin creates an asset with explicit origin/meta (to exercise dedup and
// preferred).
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

// respostaArtwork decodes the /artwork response into (groups by kind, available).
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

// TestArtworkAgrupaMesclaProxyaEDedup: /artwork merges local assets + TMDB
// poster/background + fanart.tv inventory, groups by kind, proxies the URLs, dedupes the
// TMDB poster against the local origin=external cache and emits
// `available:{kind:count}`.
func TestArtworkAgrupaMesclaProxyaEDedup(t *testing.T) {
	h, d := setupSearchServer(t, WithSearchProvider(&provTmdbArte{}), WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "e1", "movie", "Matrix")

	// Local cache of the TMDB poster (origin=external, preferred) -> dedupes the TMDB
	// poster.
	semearAssetOrigin(t, d, "a1", "e1", "poster", "external", "/x/poster.jpg", `{"preferred":true}`)
	// Raw TMDB layer (the MapearDetalhe of provTmdbArte gives poster+backdrop).
	semearMediaExternal(t, d, "me-tmdb", "e1", "tmdb", `{}`)
	// fanart.tv inventory: 2 posters + 1 logo.
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

	// poster: 1 local (external) + 2 fanart.tv = 3; the TMDB poster is DEDUPLICATED.
	if available["poster"] != 3 {
		t.Errorf("available.poster = %d, quero 3 (1 local + 2 fanarttv, TMDB deduplicado)", available["poster"])
	}
	// fanart: only the TMDB backdrop (there is no local fanart nor fanart.tv).
	if available["fanart"] != 1 {
		t.Errorf("available.fanart = %d, quero 1 (backdrop TMDB)", available["fanart"])
	}
	// logo: 1 from the fanart.tv inventory.
	if available["logo"] != 1 {
		t.Errorf("available.logo = %d, quero 1", available["logo"])
	}

	exigirLocalPreferido(t, grupos["poster"])
	exigirProxyFanart(t, grupos["poster"])
	exigirBackdropProxyado(t, grupos["fanart"])
}

// exigirLocalPreferido: there is a local item from the owner (route /assets/.../file),
// marked preferred.
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

// exigirProxyFanart: the fanart.tv posters come out proxied and preserve
// lang/likes.
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

// exigirBackdropProxyado: the TMDB background comes out proxied under source=tmdb.
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

// TestArtworkInventarioMultiplasFontes: /artwork merges the inventory of
// ANY source cached in media_external, not just 'fanarttv' -- a real regression found
// in production where the MusicBrainz/Cover Art Archive inventory (source='musicbrainz',
// album cover) was written successfully but NEVER appeared in /artwork because the read
// was hardcoded to source='fanarttv' (before a 2nd artwork source existed). It seeds the
// inventory under TWO different sources and requires both to appear merged.
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

// TestArtworkInventarioDedupeContraMaterializado: a fanart.tv inventory artwork whose
// url has already been materialized (asset origin=external with meta.source_url equal)
// does NOT appear again as a candidate -- only the local version counts.
func TestArtworkInventarioDedupeContraMaterializado(t *testing.T) {
	h, d := setupSearchServer(t, WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "e1", "movie", "Matrix")

	// Already materialized via POST /artwork/select: external asset with source_url
	// pointing to the SAME url still in the fanart.tv inventory.
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

	// poster: 1 local (already materialized) + 1 remaining candidate from the inventory
	// (p-textless) = 2. Without the dedupe it would be 3 (p-en would appear twice: local
	// AND candidate).
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

// TestArtworkEntidadeInexistente: 404 for an entity that does not exist.
func TestArtworkEntidadeInexistente(t *testing.T) {
	h, _ := setupSearchServer(t, WithSearchProvider(&provTmdbArte{}), WithAssetsDir(t.TempDir()))
	rec := do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/artwork", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestArtworkSemFanartFunciona: without a fanart.tv inventory nor an external layer,
// /artwork still responds 200 with the local assets (zero regression).
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

// TestArtworkOrdenaPorIdiomaPreferido: /artwork without any
// explicit language configuration falls back to the pt-BR default
// (defaultLanguageSettings); with pt/en/textless available in the fanart.tv inventory, the
// pt item comes first.
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

// --- Unit tests of ordenarGruposArtwork/idiomaBase/rankIdiomaArte ---

// TestIdiomaBaseNormalizaLocale: idiomaBase extracts the ISO 639-1 from a complete locale
// (format of app_settings.languages/idiomaDeBusca) to match the pure `lang` of the
// fanart.tv inventory.
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

// TestRankIdiomaArte: a normalized preferred language is always rank 0; textless rank 1;
// english rank 2; any other rank 3.
func TestRankIdiomaArte(t *testing.T) {
	casos := []struct {
		lang, preferidoBase string
		quero               int
	}{
		{"pt", "pt", 0},
		{"", "pt", 1},
		{"en", "pt", 2},
		{"fr", "pt", 3},
		{"", "", 1},   // no preference: textless still beats english/other
		{"en", "", 2}, // no preference: english beats "any other"
		{"pt", "", 3}, // no preference (empty base): pt falls into "any other"
	}
	for _, c := range casos {
		if got := rankIdiomaArte(c.lang, c.preferidoBase); got != c.quero {
			t.Errorf("rankIdiomaArte(%q, %q) = %d, quero %d", c.lang, c.preferidoBase, got, c.quero)
		}
	}
}

// TestOrdenarGruposArtworkPrioridadeIdioma: with pt/en/textless available and preferred
// language "pt-BR", the pt artwork comes first (it has to normalize via idiomaBase -- a
// direct comparison "pt-BR" == "pt" would never match).
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

// TestOrdenarGruposArtworkSemPtCaiParaTextless: without any artwork in the preferred
// language, the textless one (lang=="") comes before en and any other language.
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

// TestOrdenarGruposArtworkIsPrincipalVenceIdioma: an artwork already marked is_principal
// in a "worse" language (fr, when the preferred one is pt) keeps winning -- the language
// priority never overrides is_principal/is_local_in_library.
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

// TestOrdenarGruposArtworkEmpateMantemOrdemOriginal: same language rank, same
// is_principal, same is_local_in_library -- the original arrival order is preserved
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
