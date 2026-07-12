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

// Este arquivo implementa a ARTE CUSTOM do dono (briefing 0017): upload de
// poster/fanart proprios (origin='user'), a galeria, o servico do binario de um
// asset e a preferencia (flag em meta_json, uma por (entity, kind)). A decisao de
// design: a escolha do dono VENCE — a precedencia de servico do poster passa a ser
//
//	preferida (meta.preferred) -> origin='user' (mais recente) -> origin='external' -> 404
//
// calculada EM GO (escolherAssetServido) sobre ListarAssetsPorEntity, mantendo a
// guarda de path-traversal do 0010. O cache externo (0015) casa so origin='external',
// entao re-enriquecer NUNCA toca a arte do dono (nao-destrutivo, ADR-0012). As
// escritas passam pelo pool de escrita (ADR-0010).

const (
	// kindPoster/kindFanart/kindStill sao os kinds de arte que o dono pode subir;
	// originUser/originExternal etiquetam a proveniencia (dono x cache externo). O
	// still (frame 16:9 do episodio, 0044) e o unico still que vira asset: o still do
	// provider segue lazy em extension_data (0042), so o custom do dono e arquivo.
	kindPoster     = "poster"
	kindFanart     = "fanart"
	kindStill      = "still"
	originUser     = "user"
	originExternal = "external"
	// kindLogo/kindClearart/kindDisc/kindBanner/kindThumb/kindCharacterart sao os
	// kinds de arte EXPANDIDOS (briefing 0048): a fanart.tv traz logo, clearart, disc,
	// banner, thumb e characterart alem de poster/fanart/still. O kind e TEXT livre no
	// banco; parseKind valida a ENTRADA de upload contra o conjunto abaixo.
	kindLogo         = "logo"
	kindClearart     = "clearart"
	kindDisc         = "disc"
	kindBanner       = "banner"
	kindThumb        = "thumb"
	kindCharacterart = "characterart"
	// tamanhoMaxAsset e o teto de bytes de um upload (espelha enrich.tamanhoMaxImagem):
	// 20 MiB cobre com folga um poster/fanart em alta resolucao.
	tamanhoMaxAsset = 20 << 20
)

// assetDTO e um item da galeria (GET /assets) e o corpo devolvido por upload/
// prefer: os campos publicos do asset mais a flag preferred (lida do meta_json) e a
// url do binario (a rota do /file).
type assetDTO struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Origin    string `json:"origin"`
	Preferred bool   `json:"preferred"`
	URL       string `json:"url"`
}

// assetFilePath monta a rota que serve o binario de um asset especifico (miniaturas
// da galeria). Fonte unica do template.
func assetFilePath(entityID, assetID string) string {
	return "/api/v1/entities/" + entityID + "/assets/" + assetID + "/file"
}

// assetToDTO converte um asset no DTO da API, derivando preferred do meta_json e a
// url do binario.
func assetToDTO(a sqlcgen.Asset) assetDTO {
	return assetDTO{
		ID:        a.ID,
		Kind:      a.Kind,
		Origin:    a.Origin,
		Preferred: metaPreferred(a.MetaJson),
		URL:       assetFilePath(a.EntityID, a.ID),
	}
}

// uploadAsset trata POST /api/v1/entities/{id}/assets: recebe um multipart/form-data
// (campo `file` + campo `kind`, default poster), valida o mime pelos MAGIC BYTES e o
// tamanho, grava o binario atomicamente sob {assetsDir}/{entityID}/ e cria o asset
// origin='user'. 201 com o asset; 404 entity inexistente; 415 tipo nao suportado;
// 413 grande demais; 422 corpo malformado/sem arquivo.
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

	// ParseMultipartForm mantem ate tamanhoMaxAsset em memoria e transborda o resto
	// para arquivo temporario; o teto REAL de bytes e reforcado em lerArquivoValidado.
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
		_ = os.Remove(caminho) // best-effort: nao deixa binario orfao se o INSERT falhar
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusCreated, assetToDTO(asset))
}

// kindsValidos e o REGISTRO de kinds de arte aceitos (briefing 0048): poster/fanart/
// still (0017/0044) mais os da fanart.tv (logo/clearart/disc/banner/thumb/characterart).
// O kind no banco continua TEXT livre; este conjunto so valida a ENTRADA de upload.
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

// parseKind le o campo `kind` do multipart (default poster) e o valida contra o registro
// kindsValidos. 422 quando fora do conjunto (comportamento preservado do 0017/0044).
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

// lerArquivoValidado le o campo `file`, aplica o teto de tamanho e detecta o mime
// pelos MAGIC BYTES (http.DetectContentType — nunca a extensao nem o header do
// cliente). Devolve os bytes e a extensao canonica. 422 sem arquivo/vazio; 413
// grande demais; 415 tipo nao suportado.
func lerArquivoValidado(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "campo 'file' ausente")
		return nil, "", false
	}
	defer func() { _ = file.Close() }()

	// Le ate o teto + 1 byte: se sobrar, estourou o limite. O LimitReader impede
	// carregar um arquivo hostil inteiro na memoria.
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

// extAssetSuportado mapeia o content-type FAREJADO nos primeiros bytes para a
// extensao canonica. So image/jpeg|png|webp sao aceitos.
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

// gravarBinarioAsset grava o binario ATOMICAMENTE: escreve num arquivo temporario no
// MESMO diretorio e o renomeia (os.Rename e atomico dentro do mesmo filesystem, sem
// meio-arquivo visivel). Layout: {assetsDir}/{entityID}/user-{assetID}.{ext}.
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

// listAssets trata GET /api/v1/entities/{id}/assets: a galeria — lista JSON dos
// assets da entity (id, kind, origin, preferred, url). 200 ([] se nenhum); 404
// entity inexistente.
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

// getAssetFile trata GET /api/v1/entities/{id}/assets/{assetId}/file: serve o
// binario de UM asset com o content-type correto e a MESMA guarda de path-traversal
// do poster. 404 se o asset nao existe/nao e da entity/binario sumiu; 403 fora das
// raizes confiaveis.
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

// preferAsset trata PUT /api/v1/entities/{id}/assets/{assetId}/prefer: marca o asset
// como preferido do seu kind (meta.preferred=true) e LIMPA a flag dos outros do
// mesmo (entity, kind), preservando as demais chaves do meta_json. 200 com o asset;
// 404 asset/entity inexistente.
func (s *Server) preferAsset(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	assetID := chi.URLParam(r, "assetId")
	ctx := r.Context()

	alvo, ok := s.obterAssetDaEntity(w, ctx, id, assetID)
	if !ok {
		return
	}
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
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

// marcarPreferida seta preferred=true no alvo e remove a flag dos demais assets do
// MESMO kind (merge preservando as outras chaves do meta_json de cada), gravando so
// as linhas que realmente mudam. Devolve o alvo ja com o meta_json atualizado.
func (s *Server) marcarPreferida(ctx context.Context, assets []sqlcgen.Asset, alvo sqlcgen.Asset) (sqlcgen.Asset, error) {
	esc := sqlcgen.New(s.db.Write)
	for _, a := range assets {
		if a.Kind != alvo.Kind {
			continue
		}
		querPref := a.ID == alvo.ID
		if metaPreferred(a.MetaJson) == querPref {
			continue // ja no estado desejado: nada a gravar
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

// deleteAsset trata DELETE /api/v1/entities/{id}/assets/{assetId}: remove uma arte
// origin='user' (apaga a linha + o binario best-effort). Recusa (422) apagar
// origin='external' — cache re-puxavel que some no re-enrich (nao-destrutivo,
// ADR-0012). 200 com o asset removido; 404 se nao existe/nao e da entity.
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
	_ = os.Remove(asset.PathOrUri) // best-effort: some o binario do disco
	writeJSON(w, http.StatusOK, assetToDTO(asset))
}

// ── Helpers compartilhados ───────────────────────────────────────────────────

// obterAssetDaEntity carrega um asset exigindo que ele EXISTA e pertenca a `id`
// (404 quando ausente ou de outra entity — nao vaza binario por id cruzado). Guarda
// comum de file/prefer/delete.
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

// servirBinarioAsset serve o binario de um asset (poster ou galeria) com a guarda de
// PATH-TRAVERSAL (caminhoSobRaizes/raizesConfiaveis) e o content-type correto. 403
// quando o caminho escapou das raizes confiaveis; 404 quando o binario sumiu do
// disco. Caminho unico compartilhado por getEntityPoster e getAssetFile.
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
	f, err := os.Open(limpo) //nolint:gosec // caminho validado por caminhoSobRaizes acima
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
	http.ServeContent(w, r, filepath.Base(limpo), info.ModTime(), f)
}

// escolherAssetServido aplica a PRECEDENCIA DE SERVICO do briefing 0017 sobre os
// assets de um kind: preferida (meta.preferred) -> origin='user' (mais recente) ->
// origin='external' (primeiro) -> qualquer outro (base, ex.: arte 'local' do
// scanner) -> nada. `assets` vem de ListarAssetsPorEntity (ordenado por created_at
// asc), entao "mais recente" = o ultimo do slice e "primeiro" = o primeiro. Helper
// PURO — testado isoladamente. Subir arte ja a faz vencer a externa (o upload e a
// intencao); marcar preferida so desempata entre varias do dono.
func escolherAssetServido(assets []sqlcgen.Asset, kind string) (sqlcgen.Asset, bool) {
	c := coletarCandidatos(assets, kind)
	switch {
	case c.temP:
		return c.preferida, true
	case c.temU:
		return c.user, true
	case c.temE:
		return c.external, true
	case c.temBase:
		return c.base, true
	default:
		return sqlcgen.Asset{}, false
	}
}

// candidatosAsset guarda os candidatos por camada de precedencia enquanto o slice e
// varrido uma vez (ver escolherAssetServido/coletarCandidatos).
type candidatosAsset struct {
	preferida, user, external, base sqlcgen.Asset
	temP, temU, temE, temBase       bool
}

// coletarCandidatos varre os assets do kind uma vez e preenche cada camada da
// precedencia (preferida/user/external/base).
func coletarCandidatos(assets []sqlcgen.Asset, kind string) candidatosAsset {
	var c candidatosAsset
	for _, a := range assets {
		if a.Kind == kind {
			c.considerar(a)
		}
	}
	return c
}

// considerar classifica UM asset nas camadas: base = o primeiro de qualquer origem
// (ultima defesa antes do 404); preferida = ultima marcada; user = a mais recente;
// external = a primeira.
func (c *candidatosAsset) considerar(a sqlcgen.Asset) {
	if !c.temBase {
		c.base, c.temBase = a, true
	}
	if metaPreferred(a.MetaJson) {
		c.preferida, c.temP = a, true // ultima preferida (defensivo: deveria haver so uma)
	}
	switch a.Origin {
	case originUser:
		c.user, c.temU = a, true // ultimo = mais recente
	case originExternal:
		if !c.temE {
			c.external, c.temE = a, true // primeiro external
		}
	}
}

// metaPreferred le a flag `preferred` de um meta_json cru (false quando NULL/vazio/
// malformado ou ausente).
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

// comPreferred devolve o meta_json com a flag `preferred` setada ou removida,
// PRESERVANDO as demais chaves. Quando o mapa esvazia, devolve um NULL (sem meta) em
// vez de guardar um "{}" inocuo.
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

// assetsIndisponivel responde 503 quando nao ha diretorio de assets configurado
// (WithAssetsDir) — nao ha onde gravar o binario do upload. Protege o POST.
func (s *Server) assetsIndisponivel(w http.ResponseWriter) bool {
	if s.assetsDir == "" {
		writeError(w, http.StatusServiceUnavailable, "assets_unavailable",
			"upload de arte indisponivel (diretorio de assets nao configurado)")
		return true
	}
	return false
}
