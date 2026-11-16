package server

// artwork_select.go implements POST /api/v1/entities/{id}/artwork/select:
// the ACTION of the decision "list at import, download ONLY the selected one". An artwork of the
// inventory (fanart.tv/TMDB) is only a URL up to here; this endpoint receives the url chosen by the owner,
// DOWNLOADS the binary and materializes it as an asset with origin='external' of the `kind`, storing the
// origin url in meta_json (traceability + idempotency by url+kind). If `prefer=true`, it marks it
// as preferred (reuses marcarPreferida). It is the bridge from the lazy inventory to disk.
//
// Security (anti-SSRF): the body comes from the user, so the HOST of the url is validated against the
// SAME allowlist as the image proxy (urlImagemPermitida) — host outside → 422, we never
// download from an arbitrary host. The downloaded binary is still SNIFFED (magic bytes) and only
// jpg/png/webp are accepted, as in the owner's upload.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// selectArtworkBody is the body of POST .../artwork/select: which artwork of the
// inventory (url on an allowlisted host) to materialize as an asset with origin='external' of the `kind`, and
// whether it should be marked as preferred.
type selectArtworkBody struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Prefer bool   `json:"prefer"`
}

// selectArtwork handles POST /api/v1/entities/{id}/artwork/select: it downloads the chosen artwork
// (url from the inventory) and creates an asset with origin='external' of the `kind`, with the url in meta_json.
// Idempotent: if an external asset with the same url+kind already exists, it reuses it without re-downloading. With
// prefer=true, it marks it as preferred. 200 with the asset; 404 entity does not exist; 422 kind
// outside the set or host outside the allowlist (anti-SSRF); 502 if the download fails; 503
// without an assets directory.
func (s *Server) selectArtwork(w http.ResponseWriter, r *http.Request) {
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
	kind, urlCanon, prefer, ok := s.parseSelectArtwork(w, r)
	if !ok {
		return
	}

	alvo, existe, err := s.arteExternaPorURL(ctx, id, urlCanon, kind)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !existe {
		if alvo, err = s.baixarEMaterializarArte(ctx, id, urlCanon, kind); err != nil {
			respondErroBaixarArte(w, err)
			return
		}
	}
	if prefer {
		if alvo, err = s.preferirArte(ctx, id, alvo); err != nil {
			respondErroInterno(w)
			return
		}
	}
	writeJSON(w, http.StatusOK, assetToDTO(alvo))
}

// parseSelectArtwork decodes and validates the body: kind against the kindsValidos registry
// (422 outside) and the HOST of the url against the proxy allowlist (urlImagemPermitida — 422 host
// not allowed, the anti-SSRF defense). It already writes the error and returns ok=false. The url comes back
// CANONICAL (the same form the proxy would use).
func (s *Server) parseSelectArtwork(w http.ResponseWriter, r *http.Request) (kind, urlCanon string, prefer, ok bool) {
	var body selectArtworkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return "", "", false, false
	}
	if !kindsValidos[body.Kind] {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"kind invalido (use poster, fanart, still, logo, clearart, disc, banner, thumb ou characterart)")
		return "", "", false, false
	}
	canon, permitido := s.urlImagemPermitida(body.URL)
	if !permitido {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"url ausente ou host nao permitido (allowlist do proxy de imagem)")
		return "", "", false, false
	}
	return body.Kind, canon, body.Prefer, true
}

// arteExternaPorURL looks for an asset with origin='external' of the `kind` whose origin url
// (meta.source_url) is `url` — the idempotent target of select. It returns (asset, true, nil)
// when it finds one; (_, false, nil) when there is none; an error only on database access.
func (s *Server) arteExternaPorURL(ctx context.Context, id, url, kind string) (sqlcgen.Asset, bool, error) {
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		return sqlcgen.Asset{}, false, err
	}
	for _, a := range assets {
		if a.Kind == kind && a.Origin == originExternal && metaSourceURL(a.MetaJson) == url {
			return a, true, nil
		}
	}
	return sqlcgen.Asset{}, false, nil
}

// baixarEMaterializarArte downloads the binary of `url` (host already validated), writes it
// atomically under {assetsDir}/{entityID}/ and creates the asset with origin='external' of the `kind`
// with the url in meta_json. A download error (network/status/type) comes wrapped in
// errBaixarArte so the handler can map it to 502; a disk/database error is internal (500).
func (s *Server) baixarEMaterializarArte(ctx context.Context, id, url, kind string) (sqlcgen.Asset, error) {
	dados, ext, err := s.baixarArte(ctx, url)
	if err != nil {
		return sqlcgen.Asset{}, fmt.Errorf("%w: %v", errBaixarArte, err)
	}
	assetID := uuid.NewString()
	dir := filepath.Join(s.assetsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return sqlcgen.Asset{}, err
	}
	destino := filepath.Join(dir, "external-"+assetID+ext)
	if err := escreverAtomico(destino, dados); err != nil {
		return sqlcgen.Asset{}, err
	}
	asset, err := sqlcgen.New(s.db.Write).CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID:        assetID,
		EntityID:  id,
		Kind:      kind,
		Origin:    originExternal,
		PathOrUri: destino,
		MetaJson:  metaComSourceURL(url),
	})
	if err != nil {
		_ = os.Remove(destino) // best-effort: do not leave an orphan binary if the INSERT fails
		return sqlcgen.Asset{}, err
	}
	return asset, nil
}

// baixarArte performs the GET of the artwork (httpImagem timeout + byte ceiling), requires 200 and a
// binary that SNIFFS as jpg/png/webp (magic bytes — never the server header),
// returning the bytes and the canonical extension. Status != 200, I/O, size or an invalid type
// is an error (the handler degrades with 502).
func (s *Server) baixarArte(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.httpImagem.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("select: status %d ao baixar arte", resp.StatusCode)
	}
	dados, err := io.ReadAll(io.LimitReader(resp.Body, tamanhoMaxAsset+1))
	if err != nil {
		return nil, "", err
	}
	if len(dados) == 0 || len(dados) > tamanhoMaxAsset {
		return nil, "", fmt.Errorf("select: arte vazia ou acima de %d bytes", tamanhoMaxAsset)
	}
	ext, ok := extAssetSuportado(http.DetectContentType(dados))
	if !ok {
		return nil, "", fmt.Errorf("select: tipo nao suportado (use JPEG, PNG ou WebP)")
	}
	return dados, ext, nil
}

// preferirArte marks `alvo` as preferred of its kind (reuses marcarPreferida: it clears the
// flag of the others of the same (entity, kind) and returns the target with the updated
// meta).
func (s *Server) preferirArte(ctx context.Context, id string, alvo sqlcgen.Asset) (sqlcgen.Asset, error) {
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		return sqlcgen.Asset{}, err
	}
	return s.marcarPreferida(ctx, assets, alvo)
}

// errBaixarArte marks the DOWNLOAD failure of the artwork (network/status/type) so the handler
// can tell it apart from a disk/database error and respond 502 instead of 500.
var errBaixarArte = fmt.Errorf("select: falha ao baixar a arte")

// respondErroBaixarArte responds 502 when the artwork download failed (errBaixarArte)
// and 500 for any other failure (disk/database).
func respondErroBaixarArte(w http.ResponseWriter, err error) {
	if errors.Is(err, errBaixarArte) {
		writeError(w, http.StatusBadGateway, "external_source_error",
			"nao foi possivel baixar a arte selecionada")
		return
	}
	respondErroInterno(w)
}

// metaComSourceURL builds the meta_json of an external asset from select: it stores the origin
// url (source_url) for idempotency (arteExternaPorURL) and for traceability.
func metaComSourceURL(url string) sql.NullString {
	b, err := json.Marshal(map[string]string{"source_url": url})
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

// metaSourceURL reads the origin url (source_url) from a raw meta_json; "" when NULL/
// empty/malformed or absent.
func metaSourceURL(ns sql.NullString) string {
	if !ns.Valid || ns.String == "" {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(ns.String), &m) != nil {
		return ""
	}
	raw, ok := m["source_url"]
	if !ok {
		return ""
	}
	var u string
	_ = json.Unmarshal(raw, &u)
	return u
}
