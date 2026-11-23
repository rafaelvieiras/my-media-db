package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements the owner's CUSTOM ARTWORK: upload of his own
// poster/fanart (origin='user'), the gallery, the serving of an asset's binary and the
// preference (a flag in meta_json, one per (entity, kind)). The design decision: the
// owner's choice WINS — the poster serving precedence becomes
//
//	preferred (meta.preferred) -> origin='user' (most recent) -> origin='local'
//	(folder/file on disk) -> origin='external' -> 404
//
// computed IN GO (escolherAssetServido) over ListarAssetsPorEntity, keeping the
// path-traversal guard. The external cache matches origin='external' only, so
// re-enriching NEVER touches the owner's artwork (non-destructive, ADR-0012). Writes
// go through the write pool (ADR-0010). origin='local' wins over origin='external': an
// image the owner ALREADY HAS in the folder (folder.jpg/poster.jpg,
// scanner.descobrirPoster) is treated as his decision -- it clobbers nothing of the
// owner's (still stays below user/preferred), but it must not lose to an external
// provider.
const (
	// kindPoster/kindFanart/kindStill are the artwork kinds the owner can upload;
	// originUser/originExternal/originLocal label the provenance (owner x external cache
	// x file found on disk by the scanner). The still (16:9 frame of the episode) is the
	// only still that becomes an asset: the provider's still stays lazy in
	// extension_data, only the owner's custom one is a file.
	kindPoster     = "poster"
	kindFanart     = "fanart"
	kindStill      = "still"
	originUser     = "user"
	originExternal = "external"
	// originLocal labels the asset found by the SCANNER in the work's folder (poster.jpg/
	// folder.jpg) -- distinct from originUser (explicit upload through the UI) and
	// originExternal (provider cache). The same literal "local" that
	// internal/scanner/match.go and recheck.go already write (packages without a cross
	// import -- just a coincidence of value, no shared const).
	originLocal = "local"
	// kindLogo/kindClearart/kindDisc/kindBanner/kindThumb/kindCharacterart are the
	// EXPANDED artwork kinds: fanart.tv brings logo, clearart, disc, banner, thumb and
	// characterart besides poster/fanart/still. The kind stays free TEXT in the
	// database; parseKind validates the upload INPUT against the set below.
	kindLogo         = "logo"
	kindClearart     = "clearart"
	kindDisc         = "disc"
	kindBanner       = "banner"
	kindThumb        = "thumb"
	kindCharacterart = "characterart"
	// tamanhoMaxAsset is the byte ceiling of an upload (mirrors enrich.tamanhoMaxImagem):
	// 20 MiB comfortably covers a high-resolution poster/fanart.
	tamanhoMaxAsset = 20 << 20
)

// assetDTO is one gallery item (GET /assets) and the body returned by upload/
// prefer/materialize: the asset's public fields plus the preferred flag (read from
// meta_json, compat) and the binary url (the /file route). IsPrincipal (source of truth
// in a column, distinct from Preferred/meta_json) and LocalPath (path in the library's
// REAL folder, null when only in the internal cache).
type assetDTO struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Origin      string  `json:"origin"`
	Preferred   bool    `json:"preferred"`
	IsPrincipal bool    `json:"is_principal"`
	LocalPath   *string `json:"local_path"`
	URL         string  `json:"url"`
}

// assetFilePath builds the route that serves the binary of a specific asset (gallery
// thumbnails). Single source of the template.
func assetFilePath(entityID, assetID string) string {
	return "/api/v1/entities/" + entityID + "/assets/" + assetID + "/file"
}

// assetToDTO converts an asset into the API DTO, deriving preferred from meta_json and
// the binary url. IsPrincipal/LocalPath come straight from the columns.
func assetToDTO(a sqlcgen.Asset) assetDTO {
	var localPath *string
	if a.LocalPath.Valid && a.LocalPath.String != "" {
		v := a.LocalPath.String
		localPath = &v
	}
	return assetDTO{
		ID:          a.ID,
		Kind:        a.Kind,
		Origin:      a.Origin,
		Preferred:   metaPreferred(a.MetaJson),
		IsPrincipal: a.IsPrincipal != 0,
		LocalPath:   localPath,
		URL:         assetFilePath(a.EntityID, a.ID),
	}
}

// uploadAsset handles POST /api/v1/entities/{id}/assets: receives a multipart/form-data
// (`file` field + `kind` field, default poster), validates the mime by the MAGIC BYTES
// and the size, writes the binary atomically under {assetsDir}/{entityID}/ and creates
// the origin='user' asset. 201 with the asset; 404 unknown entity; 415 unsupported type;
// 413 too large; 422 malformed body / no file.
func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if s.assetsIndisponivel(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	// ParseMultipartForm keeps up to tamanhoMaxAsset in memory and spills the rest to a
	// temporary file; the REAL byte ceiling is enforced in lerArquivoValidado.
	if err := r.ParseMultipartForm(tamanhoMaxAsset); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "corpo multipart invalido")
		return
	}

	kind, ok := parseKind(w, r)
	if !ok {
		return
	}
	dados, ext, ok := lerArquivoValidado(w, r)
	if !ok {
		return
	}

	assetID := uuid.NewString()
	caminho, err := s.gravarBinarioAsset(id, assetID, ext, dados)
	if err != nil {
		respondErroInterno(w)
		return
	}
	asset, err := sqlcgen.New(s.db.Write).CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID:        assetID,
		EntityID:  id,
		Kind:      kind,
		Origin:    originUser,
		PathOrUri: caminho,
	})
	if err != nil {
		_ = os.Remove(caminho) // best-effort: leaves no orphan binary if the INSERT fails
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusCreated, assetToDTO(asset))
}

// kindsValidos is the REGISTRY of accepted artwork kinds: poster/fanart/still plus the
// fanart.tv ones (logo/clearart/disc/banner/thumb/characterart). The kind in the
// database remains free TEXT; this set only validates the upload INPUT.
var kindsValidos = map[string]bool{
	kindPoster:       true,
	kindFanart:       true,
	kindStill:        true,
	kindLogo:         true,
	kindClearart:     true,
	kindDisc:         true,
	kindBanner:       true,
	kindThumb:        true,
	kindCharacterart: true,
}

// parseKind reads the `kind` field of the multipart form (default poster) and validates
// it against the kindsValidos registry. 422 when outside the set (behavior preserved).
func parseKind(w http.ResponseWriter, r *http.Request) (string, bool) {
	kind := r.FormValue("kind")
	if kind == "" {
		kind = kindPoster
	}
	if !kindsValidos[kind] {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"kind invalido (use poster, fanart, still, logo, clearart, disc, banner, thumb ou characterart)")
		return "", false
	}
	return kind, true
}

// lerArquivoValidado reads the `file` field, applies the size ceiling and detects the
// mime by the MAGIC BYTES (http.DetectContentType — never the extension nor the
// client's header). Returns the bytes and the canonical extension. 422 without
// file/empty; 413 too large; 415 unsupported type.
func lerArquivoValidado(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "campo 'file' ausente")
		return nil, "", false
	}
	defer func() { _ = file.Close() }()

	// Reads up to the ceiling + 1 byte: if there is anything left, the limit was exceeded.
	// The LimitReader prevents loading a whole hostile file into memory.
	dados, err := io.ReadAll(io.LimitReader(file, tamanhoMaxAsset+1))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "falha ao ler o arquivo")
		return nil, "", false
	}
	if len(dados) > tamanhoMaxAsset {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			"arquivo excede o limite de 20 MiB")
		return nil, "", false
	}
	if len(dados) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "arquivo vazio")
		return nil, "", false
	}
	ext, ok := extAssetSuportado(http.DetectContentType(dados))
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"tipo nao suportado (use JPEG, PNG ou WebP)")
		return nil, "", false
	}
	return dados, ext, true
}

// extAssetSuportado maps the content-type SNIFFED from the first bytes to the canonical
// extension. Only image/jpeg|png|webp are accepted.
func extAssetSuportado(contentType string) (string, bool) {
	switch {
	case strings.HasPrefix(contentType, "image/jpeg"):
		return ".jpg", true
	case strings.HasPrefix(contentType, "image/png"):
		return ".png", true
	case strings.HasPrefix(contentType, "image/webp"):
		return ".webp", true
	default:
		return "", false
	}
}

// gravarBinarioAsset writes the binary ATOMICALLY: writes to a temporary file in the SAME
// directory and renames it (os.Rename is atomic within the same filesystem, no half
// file ever visible). Layout: {assetsDir}/{entityID}/user-{assetID}.{ext}.
func (s *Server) gravarBinarioAsset(entityID, assetID, ext string, dados []byte) (string, error) {
	dir := filepath.Join(s.assetsDir, entityID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	destino := filepath.Join(dir, "user-"+assetID+ext)
	tmp, err := os.CreateTemp(dir, "upload-*.tmp")
	if err != nil {
		return "", err
	}
	tmpNome := tmp.Name()
	if _, err := tmp.Write(dados); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpNome)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpNome)
		return "", err
	}
	if err := os.Rename(tmpNome, destino); err != nil {
		_ = os.Remove(tmpNome)
		return "", err
	}
	return destino, nil
}

// listAssets handles GET /api/v1/entities/{id}/assets: the gallery — JSON list of the
// entity's assets (id, kind, origin, preferred, url). 200 ([] when none); 404 unknown
// entity.
func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	out := make([]assetDTO, 0, len(assets))
	for _, a := range assets {
		out = append(out, assetToDTO(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// getAssetFile handles GET /api/v1/entities/{id}/assets/{assetId}/file: serves the binary
// of ONE asset with the correct content-type and the SAME path-traversal guard as the
// poster. 404 if the asset does not exist / is not from the entity / the binary is
// gone; 403 outside the trusted roots.
func (s *Server) getAssetFile(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	assetID := chi.URLParam(r, "assetId")
	ctx := r.Context()

	asset, ok := s.obterAssetDaEntity(w, ctx, id, assetID)
	if !ok {
		return
	}
	s.servirBinarioAsset(w, r, ctx, asset)
}

// preferAsset handles PUT /api/v1/entities/{id}/assets/{assetId}/prefer: marks the asset
// as preferred for its kind (meta.preferred=true) and CLEARS the flag on the others of
// the same (entity, kind), preserving the other keys of meta_json. It also materializes
// the target physically (if it has no local_path yet) and RENAMES the files of the same
// (entity,kind) to reflect the new order (the former principal becomes the next free
// rotation index; the new one takes the bare name), setting is_principal=1 on it (and 0
// on the former one) — the source of truth in a column, kept IN SYNC with
// meta.preferred (read by escolherAssetServido/artesLocais everywhere else, still the
// source of truth FOR SERVING the binary). 200 with the asset; 404 unknown asset/entity
// OR entity without a present media_file (no destination folder); 502/503 never here
// (the download already happened at upload/select — see the note in materializarAsset);
// 503 without assetsDir configured.
func (s *Server) preferAsset(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if s.assetsIndisponivel(w) {
		return
	}
	id := chi.URLParam(r, "id")
	assetID := chi.URLParam(r, "assetId")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}
	alvo, ok := s.obterAssetDaEntity(w, ctx, id, assetID)
	if !ok {
		return
	}
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}

	alvo, err = s.materializarERotacionarPrincipal(ctx, ent, assets, alvo)
	if err != nil {
		respondErroLocalmedia(w, err)
		return
	}
	// Reload: the rotation may have changed the local_path of ANOTHER asset of the
	// same kind (the former principal) — marcarPreferida only touches meta_json, but
	// the list has to reflect the current physical local_path.
	assets, err = s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}

	atualizado, err := s.marcarPreferida(ctx, assets, alvo)
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, assetToDTO(atualizado))
}

// marcarPreferida sets preferred=true on the target and removes the flag from the other
// assets of the SAME kind (merge preserving the other keys of each meta_json), writing
// only the rows that actually change. Returns the target with its meta_json already
// updated.
func (s *Server) marcarPreferida(ctx context.Context, assets []sqlcgen.Asset, alvo sqlcgen.Asset) (sqlcgen.Asset, error) {
	esc := sqlcgen.New(s.db.Write)
	for _, a := range assets {
		if a.Kind != alvo.Kind {
			continue
		}
		querPref := a.ID == alvo.ID
		if metaPreferred(a.MetaJson) == querPref {
			continue // already in the desired state: nothing to write
		}
		nova, err := comPreferred(a.MetaJson, querPref)
		if err != nil {
			return sqlcgen.Asset{}, err
		}
		if err := esc.AtualizarAssetMeta(ctx, sqlcgen.AtualizarAssetMetaParams{
			MetaJson: nova,
			ID:       a.ID,
		}); err != nil {
			return sqlcgen.Asset{}, err
		}
		if a.ID == alvo.ID {
			alvo.MetaJson = nova
		}
	}
	return alvo, nil
}

// deleteAsset handles DELETE /api/v1/entities/{id}/assets/{assetId}: removes an
// origin='user' artwork (deletes the row + the binary best-effort). Refuses (422) to
// delete origin='external' — a re-fetchable cache that disappears on re-enrich
// (non-destructive, ADR-0012). 200 with the removed asset; 404 if it does not exist /
// is not from the entity.
func (s *Server) deleteAsset(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	assetID := chi.URLParam(r, "assetId")
	ctx := r.Context()

	asset, ok := s.obterAssetDaEntity(w, ctx, id, assetID)
	if !ok {
		return
	}
	if asset.Origin != originUser {
		writeError(w, http.StatusUnprocessableEntity, "asset_nao_removivel",
			"arte externa e cache re-puxavel e nao pode ser removida aqui")
		return
	}
	if err := sqlcgen.New(s.db.Write).ApagarAsset(ctx, assetID); err != nil {
		respondErroInterno(w)
		return
	}
	_ = os.Remove(asset.PathOrUri) // best-effort: the binary is removed from disk
	writeJSON(w, http.StatusOK, assetToDTO(asset))
}

// ── Shared helpers ───────────────────────────────────────────────────────────

// obterAssetDaEntity loads an asset requiring that it EXISTS and belongs to `id`
// (404 when absent or from another entity — never leaks a binary through a crossed id).
// Common guard of file/prefer/delete.
func (s *Server) obterAssetDaEntity(w http.ResponseWriter, ctx context.Context, id, assetID string) (sqlcgen.Asset, bool) {
	asset, err := s.leitura().ObterAsset(ctx, assetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "asset nao encontrado")
			return sqlcgen.Asset{}, false
		}
		respondErroInterno(w)
		return sqlcgen.Asset{}, false
	}
	if asset.EntityID != id {
		writeError(w, http.StatusNotFound, "not_found", "asset nao encontrado")
		return sqlcgen.Asset{}, false
	}
	return asset, true
}

// servirBinarioAsset serves the binary of an asset (poster or gallery) with the
// PATH-TRAVERSAL guard (caminhoSobRaizes/raizesConfiaveis) and the correct content-type.
// 403 when the path escaped the trusted roots; 404 when the binary is gone from disk.
// Single path shared by getEntityPoster and getAssetFile.
func (s *Server) servirBinarioAsset(w http.ResponseWriter, r *http.Request, ctx context.Context, asset sqlcgen.Asset) {
	raizes, err := s.raizesConfiaveis(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !caminhoSobRaizes(asset.PathOrUri, raizes) {
		writeError(w, http.StatusForbidden, "forbidden",
			"o caminho do asset esta fora dos datasources conhecidos")
		return
	}
	limpo := filepath.Clean(asset.PathOrUri)
	f, err := os.Open(limpo) //nolint:gosec // path validated by caminhoSobRaizes above
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "arquivo do asset indisponivel")
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "not_found", "arquivo do asset indisponivel")
		return
	}
	w.Header().Set("Content-Type", contentTypeImagem(limpo, f))
	// no-cache is "revalidate before using", not "do not store": ServeContent already answers
	// 304 from Last-Modified, so an unchanged poster costs one conditional request, while a
	// REPLACED one (the identity of the item changed) actually reaches the screen. Without
	// this header the browser applied heuristic freshness to a stable URL and kept showing the
	// previous show's cover for days (measured, 23/nov/2026).
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, filepath.Base(limpo), info.ModTime(), f)
}

// escolherAssetServido applies the SERVING PRECEDENCE over the assets of one kind:
// preferred (meta.preferred) -> origin='user' (most recent) -> origin='local' (first,
// file found by the scanner in the folder) -> origin='external' (first) -> any other
// (base) -> nothing. `assets` comes from ListarAssetsPorEntity (ordered by created_at
// asc), so "most recent" = the last of the slice and "first" = the first one. PURE
// helper — tested in isolation. Uploading artwork already makes it beat the local /
// external one (the upload is the intention); marking it preferred only breaks ties
// between several of the owner's own.
func escolherAssetServido(assets []sqlcgen.Asset, kind string) (sqlcgen.Asset, bool) {
	c := coletarCandidatos(assets, kind)
	switch {
	case c.temP:
		return c.preferida, true
	case c.temU:
		return c.user, true
	case c.temL:
		return c.local, true
	case c.temE:
		return c.external, true
	case c.temBase:
		return c.base, true
	default:
		return sqlcgen.Asset{}, false
	}
}

// candidatosAsset keeps the candidates per precedence layer while the slice is swept
// once (see escolherAssetServido/coletarCandidatos).
type candidatosAsset struct {
	preferida, user, local, external, base sqlcgen.Asset
	temP, temU, temL, temE, temBase        bool
}

// coletarCandidatos sweeps the assets of the kind once and fills each layer of the
// precedence (preferida/user/local/external/base).
func coletarCandidatos(assets []sqlcgen.Asset, kind string) candidatosAsset {
	var c candidatosAsset
	for _, a := range assets {
		if a.Kind == kind {
			c.considerar(a)
		}
	}
	return c
}

// considerar classifies ONE asset into the layers: base = the first one of any origin
// (last defense before the 404); preferida = the last marked one; user = the most
// recent; local = the first; external = the first.
func (c *candidatosAsset) considerar(a sqlcgen.Asset) {
	if !c.temBase {
		c.base, c.temBase = a, true
	}
	if metaPreferred(a.MetaJson) {
		c.preferida, c.temP = a, true // last preferred (defensive: there should be only one)
	}
	switch a.Origin {
	case originUser:
		c.user, c.temU = a, true // last = most recent
	case originLocal:
		if !c.temL {
			c.local, c.temL = a, true // first local
		}
	case originExternal:
		if !c.temE {
			c.external, c.temE = a, true // first external
		}
	}
}

// metaPreferred reads the `preferred` flag from a raw meta_json (false when NULL/empty/
// malformed or absent).
func metaPreferred(ns sql.NullString) bool {
	if !ns.Valid || ns.String == "" {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(ns.String), &m) != nil {
		return false
	}
	raw, ok := m["preferred"]
	if !ok {
		return false
	}
	var b bool
	_ = json.Unmarshal(raw, &b)
	return b
}

// comPreferred returns the meta_json with the `preferred` flag set or removed,
// PRESERVING the other keys. When the map empties out, it returns a NULL (no meta)
// instead of keeping a harmless "{}".
func comPreferred(ns sql.NullString, preferred bool) (sql.NullString, error) {
	m := map[string]json.RawMessage{}
	if ns.Valid && ns.String != "" {
		_ = json.Unmarshal([]byte(ns.String), &m)
	}
	if preferred {
		m["preferred"] = json.RawMessage("true")
	} else {
		delete(m, "preferred")
	}
	if len(m) == 0 {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

// assetsIndisponivel answers 503 when there is no assets directory configured
// (WithAssetsDir) — there is nowhere to write the upload binary. Protects the POST.
func (s *Server) assetsIndisponivel(w http.ResponseWriter) bool {
	if s.assetsDir == "" {
		writeError(w, http.StatusServiceUnavailable, "assets_unavailable",
			"upload de arte indisponivel (diretorio de assets nao configurado)")
		return true
	}
	return false
}
