package server

// artwork_select.go implementa POST /api/v1/entities/{id}/artwork/select (briefing 0048):
// a AÇÃO da decisão "listar no import, baixar SÓ a selecionada". Uma arte do inventário
// (fanart.tv/TMDB) é só uma URL até aqui; este endpoint recebe a url escolhida pelo dono,
// BAIXA o binário e o materializa como asset origin='external' do `kind`, guardando a url
// de origem no meta_json (rastreio + idempotência por url+kind). Se `prefer=true`, marca-a
// como preferida (reusa marcarPreferida). É a ponte do inventário lazy para o disco.
//
// Segurança (anti-SSRF): o corpo é do usuário, então o HOST da url é validado contra a
// MESMA allowlist do proxy de imagem (urlImagemPermitida) — host fora → 422, jamais
// baixamos de host arbitrário. O binário baixado ainda é FAREJADO (magic bytes) e só
// jpg/png/webp são aceitos, como no upload do dono.

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

// selectArtworkBody é o corpo do POST .../artwork/select: qual arte do inventário
// (url num host da allowlist) materializar como asset origin='external' do `kind`, e
// se deve marcá-la como preferida.
type selectArtworkBody struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Prefer bool   `json:"prefer"`
}

// selectArtwork trata POST /api/v1/entities/{id}/artwork/select: baixa a arte escolhida
// (url do inventário) e cria um asset origin='external' do `kind`, com a url no meta_json.
// Idempotente: se já existe asset external com a mesma url+kind, reusa sem rebaixar. Com
// prefer=true, marca-a como preferida. 200 com o asset; 404 entity inexistente; 422 kind
// fora do conjunto ou host fora da allowlist (anti-SSRF); 502 se o download falhar; 503
// sem diretório de assets.
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

// parseSelectArtwork decodifica e valida o corpo: kind contra o registro kindsValidos
// (422 fora) e o HOST da url contra a allowlist do proxy (urlImagemPermitida — 422 host
// não permitido, a defesa anti-SSRF). Já escreve o erro e devolve ok=false. A url volta
// CANÔNICA (a mesma forma que o proxy usaria).
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

// arteExternaPorURL procura um asset origin='external' do `kind` cuja url de origem
// (meta.source_url) seja `url` — o alvo idempotente do select. Devolve (asset, true, nil)
// quando encontra; (_, false, nil) quando não há; erro só no acesso ao banco.
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

// baixarEMaterializarArte baixa o binário de `url` (host já validado), grava-o
// atomicamente sob {assetsDir}/{entityID}/ e cria o asset origin='external' do `kind`
// com a url no meta_json. Erro de download (rede/status/tipo) vem embrulhado em
// errBaixarArte para o handler mapear a 502; erro de disco/banco é interno (500).
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
		_ = os.Remove(destino) // best-effort: nao deixa binario orfao se o INSERT falhar
		return sqlcgen.Asset{}, err
	}
	return asset, nil
}

// baixarArte faz o GET da arte (timeout do httpImagem + teto de bytes), exige 200 e um
// binário que FAREJE como jpg/png/webp (magic bytes — nunca o header do servidor),
// devolvendo os bytes e a extensão canônica. Status != 200, I/O, tamanho ou tipo
// inválido é erro (o handler degrada com 502).
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

// preferirArte marca `alvo` como preferida do seu kind (reusa marcarPreferida: limpa a
// flag dos outros do mesmo (entity, kind) e devolve o alvo com o meta atualizado).
func (s *Server) preferirArte(ctx context.Context, id string, alvo sqlcgen.Asset) (sqlcgen.Asset, error) {
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		return sqlcgen.Asset{}, err
	}
	return s.marcarPreferida(ctx, assets, alvo)
}

// errBaixarArte marca a falha de DOWNLOAD da arte (rede/status/tipo) para o handler
// diferenciar do erro de disco/banco e responder 502 em vez de 500.
var errBaixarArte = fmt.Errorf("select: falha ao baixar a arte")

// respondErroBaixarArte responde 502 quando o download da arte falhou (errBaixarArte)
// e 500 para qualquer outra falha (disco/banco).
func respondErroBaixarArte(w http.ResponseWriter, err error) {
	if errors.Is(err, errBaixarArte) {
		writeError(w, http.StatusBadGateway, "external_source_error",
			"nao foi possivel baixar a arte selecionada")
		return
	}
	respondErroInterno(w)
}

// metaComSourceURL monta o meta_json de um asset external do select: guarda a url de
// origem (source_url) para a idempotência (arteExternaPorURL) e o rastreio.
func metaComSourceURL(url string) sql.NullString {
	b, err := json.Marshal(map[string]string{"source_url": url})
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

// metaSourceURL lê a url de origem (source_url) de um meta_json cru; "" quando NULL/
// vazio/malformado ou ausente.
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
