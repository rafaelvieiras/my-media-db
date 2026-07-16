package server

// localmedia_handlers.go implementa os endpoints de MATERIALIZAÇÃO física do
// briefing 0062: POST .../assets/{assetId}/materialize (copia uma arte já
// cacheada internamente para a pasta REAL da biblioteca, no nome canônico
// Kodi/Emby) e POST .../entities/{id}/nfo (gera/regrava o .nfo da entity). A
// extensão do PUT .../assets/{assetId}/prefer (materializar + rotacionar ao
// trocar a principal) vive em assets.go, ao lado do preferAsset original —
// reusa as mesmas funções deste arquivo. Todo o cálculo de nome/diretório é
// delegado a internal/localmedia (pacote puro); aqui só I/O + orquestração
// de banco.

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// carimboNfoFormato é o layout Go do timestamp gravado em nfo_written_at,
// mesmo formato de carimboRetroativoFormato (retroativo.go) — espelha
// strftime('%Y-%m-%dT%H:%M:%fZ', 'now') do SQL (3 dígitos de milissegundo).
const carimboNfoFormato = "2006-01-02T15:04:05.000Z"

// nowISO devolve o timestamp UTC agora, no mesmo formato que o SQL
// strftime(...) grava — usado por AtualizarEntityNfo (nfo_written_at).
func nowISO() string {
	return time.Now().UTC().Format(carimboNfoFormato)
}

// errSemDestino marca "entity sem media_file present" (sem pasta de destino
// para materializar arte/NFO) — o handler mapeia para 404, distinto de erro
// de I/O real (500).
var errSemDestino = errors.New("localmedia: entity sem media_file presente (sem pasta de destino)")

// materializeAsset trata POST /api/v1/entities/{id}/assets/{assetId}/materialize:
// copia o binário já cacheado (assetsDir) do asset para a pasta REAL da
// biblioteca, no nome canônico (internal/localmedia). Idempotente: se o
// asset já tem local_path, devolve como está (não regrava). 200 com o asset
// atualizado; 404 asset/entity inexistente OU entity sem media_file
// presente; 502 nunca aqui (o download para o cache já aconteceu antes, no
// select/upload — ver nota em materializarAsset); 503 sem assetsDir.
func (s *Server) materializeAsset(w http.ResponseWriter, r *http.Request) {
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
	asset, ok := s.obterAssetDaEntity(w, ctx, id, assetID)
	if !ok {
		return
	}

	atualizado, err := s.materializarAsset(ctx, ent, asset)
	if err != nil {
		respondErroLocalmedia(w, err)
		return
	}
	writeJSON(w, http.StatusOK, assetToDTO(atualizado))
}

// respondErroLocalmedia mapeia os erros dos helpers deste arquivo para a
// resposta HTTP: errSemDestino -> 404; qualquer outro -> 500.
func respondErroLocalmedia(w http.ResponseWriter, err error) {
	if errors.Is(err, errSemDestino) {
		writeError(w, http.StatusNotFound, "not_found",
			"entity sem arquivo presente (sem pasta de destino na biblioteca)")
		return
	}
	respondErroInterno(w)
}

// materializarAsset lê o binário do asset do CACHE interno (assetsDir,
// path_or_uri — sempre já local nesta versão: todo asset existente passou
// por upload/select, que já baixam para o cache antes de criar a linha; a
// "materialização de download" do contrato é para um futuro candidato que
// ainda não é asset, e a UI já encadeia select->materialize nesse caso) e
// grava uma CÓPIA física na pasta real, no nome canônico calculado.
// Idempotente: asset já materializado devolve como está.
//
// Decisão de implementação (não 100% explícita no briefing): quando NENHUM
// outro asset do mesmo (entity,kind) já é `is_principal` (o caso comum — a
// primeira arte daquele kind salva na pasta), este asset se torna o
// principal automaticamente (nome BARE, is_principal=1) — é o único jeito de
// satisfazer o critério de aceite "pasta com um único vídeo: salvar na pasta
// grava poster.jpg (bare)" sem exigir um PUT /prefer explícito logo depois.
// Quando já existe um principal do kind, o novo materializado NÃO o disputa
// (nome rotacionado, is_principal=0) — só o PUT /prefer troca o principal.
func (s *Server) materializarAsset(ctx context.Context, ent sqlcgen.Entity, asset sqlcgen.Asset) (sqlcgen.Asset, error) {
	if asset.LocalPath.Valid && asset.LocalPath.String != "" {
		return asset, nil
	}
	jaTemPrincipal, err := s.kindJaTemPrincipal(ctx, ent.ID, asset.Kind)
	if err != nil {
		return sqlcgen.Asset{}, err
	}
	tornaPrincipal := !jaTemPrincipal

	destino, err := s.gravarAssetNaPastaReal(ctx, ent, asset, tornaPrincipal)
	if err != nil {
		return sqlcgen.Asset{}, err
	}
	esc := sqlcgen.New(s.db.Write)
	if err := esc.AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: sql.NullString{String: destino, Valid: true},
		ID:        asset.ID,
	}); err != nil {
		return sqlcgen.Asset{}, err
	}
	asset.LocalPath = sql.NullString{String: destino, Valid: true}

	if tornaPrincipal {
		if err := esc.DesmarcarAssetsPrincipalPorKind(ctx, sqlcgen.DesmarcarAssetsPrincipalPorKindParams{
			EntityID: ent.ID,
			Kind:     asset.Kind,
		}); err != nil {
			return sqlcgen.Asset{}, err
		}
		if err := esc.MarcarAssetPrincipal(ctx, asset.ID); err != nil {
			return sqlcgen.Asset{}, err
		}
		asset.IsPrincipal = 1
	}
	return asset, nil
}

// kindJaTemPrincipal diz se algum asset do (entity,kind) já é is_principal=1.
func (s *Server) kindJaTemPrincipal(ctx context.Context, entityID, kind string) (bool, error) {
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, entityID)
	if err != nil {
		return false, err
	}
	for _, a := range assets {
		if a.Kind == kind && a.IsPrincipal != 0 {
			return true, nil
		}
	}
	return false, nil
}

// gravarAssetNaPastaReal calcula o diretório/nome canônico do asset (via
// internal/localmedia) e grava o binário (lido do cache) atomicamente.
// `principal` decide nome bare vs. rotacionado. Devolve o caminho final.
func (s *Server) gravarAssetNaPastaReal(ctx context.Context, ent sqlcgen.Entity, asset sqlcgen.Asset, principal bool) (string, error) {
	dir, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errSemDestino
	}
	dados, err := s.lerBinarioAssetDoCache(ctx, asset)
	if err != nil {
		return "", err
	}
	kind, nomeBase := s.kindNomeEBase(ctx, ent, asset.Kind, nomeBaseVideo)
	ext := filepath.Ext(asset.PathOrUri)
	existentes, err := s.locaisExistentesDoKind(ctx, ent.ID, asset.Kind, asset.ID)
	if err != nil {
		return "", err
	}
	indice := 0
	if !principal {
		indice = localmedia.IndiceRotacaoLivre(existentes, kind, pastaExclusiva, nomeBase, ext)
	}
	nome := localmedia.NomeArquivoArte(kind, principal, indice, pastaExclusiva, nomeBase, ext)
	destino := filepath.Join(dir, nome)
	if err := localmedia.EscreverArquivoAtomico(destino, dados); err != nil {
		return "", err
	}
	return destino, nil
}

// lerBinarioAssetDoCache lê o binário do asset do CACHE interno
// (path_or_uri), sob a MESMA guarda de path-traversal do servirBinarioAsset
// (raizesConfiaveis/caminhoSobRaizes) — o asset já foi validado como
// pertencente à entity pelo chamador.
func (s *Server) lerBinarioAssetDoCache(ctx context.Context, asset sqlcgen.Asset) ([]byte, error) {
	raizes, err := s.raizesConfiaveis(ctx)
	if err != nil {
		return nil, err
	}
	if !caminhoSobRaizes(asset.PathOrUri, raizes) {
		return nil, errors.New("localmedia: caminho do asset fora das raizes confiaveis")
	}
	return os.ReadFile(filepath.Clean(asset.PathOrUri)) //nolint:gosec // caminho validado por caminhoSobRaizes acima
}

// locaisExistentesDoKind lista os `local_path` já usados por OUTROS assets
// (exceto `excetoID`) do mesmo (entity,kind) — a base do cálculo do índice
// de rotação livre (localmedia.IndiceRotacaoLivre).
func (s *Server) locaisExistentesDoKind(ctx context.Context, entityID, kind, excetoID string) ([]string, error) {
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, entityID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range assets {
		if a.ID == excetoID || a.Kind != kind || !a.LocalPath.Valid || a.LocalPath.String == "" {
			continue
		}
		out = append(out, a.LocalPath.String)
	}
	return out, nil
}

// resolverDestino calcula o diretório de destino, o nome-base do vídeo e se
// a pasta é EXCLUSIVA (um único vídeo) para uma entity — a base compartilhada
// por materialize/prefer/nfo. ok=false quando a entity não tem media_file
// present elegível (sem pasta de destino).
func (s *Server) resolverDestino(ctx context.Context, ent sqlcgen.Entity) (dir, nomeBaseVideo string, pastaExclusiva, ok bool, err error) {
	localFiles, ancestors, err := s.carregarMediaFilesLocalmedia(ctx, ent)
	if err != nil {
		return "", "", false, false, err
	}
	mf, achou := localmedia.ArquivoMaisAntigo(localmedia.Entity{ID: ent.ID, Type: ent.Type}, localFiles, ancestors)
	if !achou {
		return "", "", false, false, nil
	}
	dir = filepath.Dir(mf.Path)
	nomeBaseVideo = localmedia.NomeBaseVideo(mf.Path)

	todosPresentes, err := s.leitura().ListarMediaFilesPresentes(ctx)
	if err != nil {
		return "", "", false, false, err
	}
	todosLocal := make([]localmedia.MediaFile, 0, len(todosPresentes))
	for _, f := range todosPresentes {
		todosLocal = append(todosLocal, localmedia.MediaFile{Path: f.Path, State: f.State})
	}
	pastaExclusiva = localmedia.EhPastaExclusiva(dir, todosLocal)
	return dir, nomeBaseVideo, pastaExclusiva, true, nil
}

// carregarMediaFilesLocalmedia devolve (mediaFiles, ancestors) no shape do
// pacote internal/localmedia para a entity dada: reusa listarFilesDaEntity
// (que já resolve a complexidade do episódio via junção
// media_file_entity/ADR-0021) — mas para `season`, que não tem media_file
// PRÓPRIO (todo arquivo de episódio aponta entity_id para a SÉRIE), resolve
// primeiro o ancestral "tv" (montarAncestrais) e carrega os arquivos DELE.
// Cada MediaFile devolvido tem EntityID relabeled para a entity cujos
// arquivos foram carregados (série para season/tv, a própria para
// movie/episode) — consistente com o que localmedia.DiretorioDestino espera
// filtrar.
func (s *Server) carregarMediaFilesLocalmedia(ctx context.Context, ent sqlcgen.Entity) ([]localmedia.MediaFile, []localmedia.Entity, error) {
	ancestorRefs := s.montarAncestrais(ctx, ent.ID)
	ancestors := make([]localmedia.Entity, len(ancestorRefs))
	for i, a := range ancestorRefs {
		ancestors[i] = localmedia.Entity{ID: a.ID, Type: a.Type}
	}

	alvoEnt := ent
	if ent.Type == tipoEntTemporada {
		if serieID, ok := serieAncestralID(ancestors); ok {
			serieEnt, err := s.leitura().ObterEntity(ctx, serieID)
			if err == nil {
				alvoEnt = serieEnt
			}
		}
	}
	files, err := s.listarFilesDaEntity(ctx, alvoEnt)
	if err != nil {
		return nil, nil, err
	}
	out := make([]localmedia.MediaFile, 0, len(files))
	for _, f := range files {
		out = append(out, localmedia.MediaFile{
			EntityID:  alvoEnt.ID,
			Path:      f.Path,
			State:     f.State,
			CreatedAt: f.CreatedAt,
		})
	}
	return out, ancestors, nil
}

// serieAncestralID acha o id do ancestral mais próximo do tipo "tv" na
// trilha RAIZ->PAI — espelha localmedia.serieAncestral (não-exportada), para
// o server resolver QUAL entity carregar os media_file antes de chamar
// localmedia (que só faz o filtro puro sobre a lista já carregada).
func serieAncestralID(ancestors []localmedia.Entity) (string, bool) {
	for i := len(ancestors) - 1; i >= 0; i-- {
		if ancestors[i].Type == localmedia.TipoSerie {
			return ancestors[i].ID, true
		}
	}
	return "", false
}

// kindNomeEBase decide o kind/nome-base a passar para
// localmedia.NomeArquivoArte: pôster de uma TEMPORADA usa o kind sintético
// KindSeasonPoster + "season{NN}" (nunca o nome do vídeo); os demais casos
// usam o kind/nome-base do vídeo normalmente.
func (s *Server) kindNomeEBase(ctx context.Context, ent sqlcgen.Entity, assetKind, nomeBaseVideo string) (kind, nomeBase string) {
	if ent.Type == tipoEntTemporada && assetKind == kindPoster {
		return localmedia.KindSeasonPoster, localmedia.NomeSeasonBase(s.numeroDaTemporada(ctx, ent.ID))
	}
	return assetKind, nomeBaseVideo
}

// numeroDaTemporada lê o season_number (entity_relation.sort_index do vínculo
// série->temporada) de uma entity season. 0 (defensivo) quando ausente/sem
// pai — degrada para "season00" em vez de falhar a materialização.
func (s *Server) numeroDaTemporada(ctx context.Context, seasonID string) int {
	pais, err := s.leitura().ListarRelacoesPais(ctx, seasonID)
	if err != nil || len(pais) == 0 || !pais[0].SortIndex.Valid {
		return 0
	}
	return int(pais[0].SortIndex.Int64)
}

// ── POST /entities/{id}/nfo ──────────────────────────────────────────────

// nfoWriteResult é o corpo de POST /entities/{id}/nfo (NfoWriteResult no
// contrato): espelha entity.nfo_path/nfo_written_at.
type nfoWriteResult struct {
	NfoPath      string `json:"nfo_path"`
	NfoWrittenAt string `json:"nfo_written_at"`
}

// writeEntityNfo trata POST /api/v1/entities/{id}/nfo: (re)gera e grava o
// .nfo da entity na pasta real, a partir do MESMO resolver.Entidade que
// resolverEntity já produz. Idempotente (sempre sobrescreve por completo).
// 200 com {nfo_path,nfo_written_at}; 404 entity inexistente OU sem
// media_file presente.
func (s *Server) writeEntityNfo(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	lang, ok := s.parseLang(w, cfg, r.URL.Query().Get("lang"))
	if !ok {
		return
	}

	resolvida, encontrada, err := s.resolverEntity(ctx, id, lang, false)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !encontrada {
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
		return
	}
	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	// season não tem NFO próprio neste briefing (não há media_file nem
	// convenção de nome-de-vídeo próprios — DiretorioDestino resolveria a
	// raiz da série, mas o nome de arquivo colidiria com o tvshow.nfo/NFO de
	// episódio ali gravados). Trata como "sem pasta de destino" (404) — a UI
	// não deveria expor o botão "Salvar NFO" para uma temporada.
	if ent.Type == tipoEntTemporada {
		respondErroLocalmedia(w, errSemDestino)
		return
	}

	resultado, err := s.gravarNFO(ctx, ent, resolvida)
	if err != nil {
		respondErroLocalmedia(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resultado)
}

// gravarNFO calcula o destino (DiretorioDestino/NomeArquivoNFO), monta o XML
// (localmedia.MontarNFO) e grava atomicamente, atualizando
// entity.nfo_path/nfo_written_at. errSemDestino quando a entity não tem
// media_file present (sem pasta de destino).
//
// Antes de montar o XML, materializa (best-effort) o pôster e o fundo
// VENCEDORES na pasta real (mesma lógica de MaterializarMidiasFaltantes,
// briefing 0064/0066): assim o .nfo nunca precisa de <thumb>/<fanart>
// apontando para a API interna -- o Kodi/Jellyfin descobrem a arte sozinhos
// pela convenção de nome de arquivo. Falha de materialização (rede,
// permissão) é logada e IGNORADA -- nunca impede a gravação do .nfo.
//
// Antes de sobrescrever, lê o .nfo que já estiver no destino (se houver) e
// preserva os campos LOCAIS do Kodi/Emby/Jellyfin -- avaliação do usuário,
// contagem/estado de assistido, última reprodução, data de adição e ponto de
// retomada (localmedia.ExtrairCamposUsuario) -- para NÃO destruir edição
// feita fora do MyMediaDB (à mão ou pelo próprio Kodi) ao regravar. Leitura
// é best-effort: arquivo ausente ou corrompido não impede a gravação, só
// resulta em zero-value (nada a preservar).
func (s *Server) gravarNFO(ctx context.Context, ent sqlcgen.Entity, e resolver.Entidade) (nfoWriteResult, error) {
	dir, nomeBaseVideo, _, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return nfoWriteResult{}, err
	}
	if !ok {
		return nfoWriteResult{}, errSemDestino
	}

	if _, err := s.materializarKindVencedor(ctx, ent, kindPoster); err != nil {
		log.Printf("gravarNFO: falha ao materializar poster da entity %q (best-effort, ignorado): %v", ent.ID, err)
	}
	if _, err := s.materializarKindVencedor(ctx, ent, kindFanart); err != nil {
		log.Printf("gravarNFO: falha ao materializar fanart da entity %q (best-effort, ignorado): %v", ent.ID, err)
	}

	nome := localmedia.NomeArquivoNFO(ent.Type, nomeBaseVideo)
	destino := filepath.Join(dir, nome)
	existente, _ := os.ReadFile(destino)
	usuario := localmedia.ExtrairCamposUsuario(existente)

	corpo, err := localmedia.MontarNFO(e, usuario)
	if err != nil {
		return nfoWriteResult{}, err
	}
	if err := localmedia.EscreverArquivoAtomico(destino, corpo); err != nil {
		return nfoWriteResult{}, err
	}

	agora := nowISO()
	if err := sqlcgen.New(s.db.Write).AtualizarEntityNfo(ctx, sqlcgen.AtualizarEntityNfoParams{
		NfoPath:      sql.NullString{String: destino, Valid: true},
		NfoWrittenAt: sql.NullString{String: agora, Valid: true},
		ID:           ent.ID,
	}); err != nil {
		return nfoWriteResult{}, err
	}
	return nfoWriteResult{NfoPath: destino, NfoWrittenAt: agora}, nil
}

// ── Extensão de PUT /assets/{assetId}/prefer (briefing 0062) ────────────────
//
// A rotação física ao trocar a arte principal: materializarERotacionarPrincipal
// é chamada por preferAsset (assets.go). A invariante "no máximo um
// is_principal=1 por (entity,kind)" é aplicada em CÓDIGO — zera o antigo
// ANTES de setar o novo — sequencialmente no pool de ESCRITOR ÚNICO
// (SetMaxOpenConns(1), ADR-0010): não há `sql.Tx` explícito porque nenhum
// outro ponto do codebase usa (o pool já serializa toda escrita, mesmo
// padrão de marcarPreferida).

// materializarERotacionarPrincipal materializa fisicamente o alvo (se
// necessário) e RENOMEIA os arquivos do mesmo (entity,kind) para refletir a
// nova ordem: o antigo principal (se houver, e != alvo) vira o próximo
// índice de rotação LIVRE; o alvo assume (ou é renomeado para) o nome bare.
// Seta is_principal=1 no alvo e 0 no antigo, na coluna nova. Devolve o alvo
// ATUALIZADO (local_path/is_principal refletidos em memória).
func (s *Server) materializarERotacionarPrincipal(ctx context.Context, ent sqlcgen.Entity, assets []sqlcgen.Asset, alvo sqlcgen.Asset) (sqlcgen.Asset, error) {
	if antigo, ok := principalAtualDoKind(assets, alvo.Kind, alvo.ID); ok {
		if err := s.rotacionarAntigoPrincipal(ctx, ent, antigo); err != nil {
			return sqlcgen.Asset{}, err
		}
	}

	novoLocalPath, err := s.assumirNomeBare(ctx, ent, alvo)
	if err != nil {
		return sqlcgen.Asset{}, err
	}

	esc := sqlcgen.New(s.db.Write)
	if err := esc.DesmarcarAssetsPrincipalPorKind(ctx, sqlcgen.DesmarcarAssetsPrincipalPorKindParams{
		EntityID: ent.ID,
		Kind:     alvo.Kind,
	}); err != nil {
		return sqlcgen.Asset{}, err
	}
	if err := esc.MarcarAssetPrincipal(ctx, alvo.ID); err != nil {
		return sqlcgen.Asset{}, err
	}

	alvo.LocalPath = sql.NullString{String: novoLocalPath, Valid: true}
	alvo.IsPrincipal = 1
	return alvo, nil
}

// principalAtualDoKind acha o asset is_principal=1 do MESMO kind, exceto
// `excetoID` (o alvo que está virando o novo principal) — o "antigo
// principal" que precisa ser rotacionado para um índice numérico.
func principalAtualDoKind(assets []sqlcgen.Asset, kind, excetoID string) (sqlcgen.Asset, bool) {
	for _, a := range assets {
		if a.Kind == kind && a.ID != excetoID && a.IsPrincipal != 0 {
			return a, true
		}
	}
	return sqlcgen.Asset{}, false
}

// rotacionarAntigoPrincipal renomeia o arquivo físico do antigo principal
// (nome bare) para o próximo índice de rotação LIVRE, e atualiza
// asset.local_path. No-op quando o antigo não tinha local_path (nunca foi
// materializado — só a flag is_principal muda, sem arquivo pra renomear).
func (s *Server) rotacionarAntigoPrincipal(ctx context.Context, ent sqlcgen.Entity, antigo sqlcgen.Asset) error {
	if !antigo.LocalPath.Valid || antigo.LocalPath.String == "" {
		return nil
	}
	dir := filepath.Dir(antigo.LocalPath.String)
	ext := filepath.Ext(antigo.LocalPath.String)
	_, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return err
	}
	if !ok {
		return errSemDestino
	}
	kind, nomeBase := s.kindNomeEBase(ctx, ent, antigo.Kind, nomeBaseVideo)
	existentes, err := s.locaisExistentesDoKind(ctx, ent.ID, antigo.Kind, antigo.ID)
	if err != nil {
		return err
	}
	indice := localmedia.IndiceRotacaoLivre(existentes, kind, pastaExclusiva, nomeBase, ext)
	novoDestino := filepath.Join(dir, localmedia.NomeArquivoArte(kind, false, indice, pastaExclusiva, nomeBase, ext))
	if err := localmedia.RenomearArquivo(antigo.LocalPath.String, novoDestino); err != nil {
		return err
	}
	return sqlcgen.New(s.db.Write).AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: sql.NullString{String: novoDestino, Valid: true},
		ID:        antigo.ID,
	})
}

// assumirNomeBare garante que o alvo tenha o nome BARE do seu kind: se já
// tem local_path (materializado, ainda que rotacionado), RENOMEIA (nunca
// recopia bytes); senão, materializa do zero direto com o nome bare.
// Persiste local_path e devolve o caminho final.
func (s *Server) assumirNomeBare(ctx context.Context, ent sqlcgen.Entity, alvo sqlcgen.Asset) (string, error) {
	var destino string
	var err error
	if !alvo.LocalPath.Valid || alvo.LocalPath.String == "" {
		destino, err = s.gravarAssetNaPastaReal(ctx, ent, alvo, true)
	} else {
		destino, err = s.renomearParaBare(ctx, ent, alvo)
	}
	if err != nil {
		return "", err
	}
	if err := sqlcgen.New(s.db.Write).AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: sql.NullString{String: destino, Valid: true},
		ID:        alvo.ID,
	}); err != nil {
		return "", err
	}
	return destino, nil
}

// renomearParaBare renomeia o arquivo JÁ materializado do alvo para o nome
// bare do seu kind (RENAME, nunca recopia bytes) — idempotente: se o arquivo
// já é o bare, no-op (devolve o mesmo caminho).
func (s *Server) renomearParaBare(ctx context.Context, ent sqlcgen.Entity, alvo sqlcgen.Asset) (string, error) {
	dir := filepath.Dir(alvo.LocalPath.String)
	ext := filepath.Ext(alvo.LocalPath.String)
	_, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errSemDestino
	}
	kind, nomeBase := s.kindNomeEBase(ctx, ent, alvo.Kind, nomeBaseVideo)
	destino := filepath.Join(dir, localmedia.NomeArquivoArte(kind, true, 0, pastaExclusiva, nomeBase, ext))
	if destino == alvo.LocalPath.String {
		return destino, nil
	}
	if err := localmedia.RenomearArquivo(alvo.LocalPath.String, destino); err != nil {
		return "", err
	}
	return destino, nil
}
