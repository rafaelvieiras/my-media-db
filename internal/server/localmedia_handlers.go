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
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
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
// present elegível (sem pasta de destino). O DIRETÓRIO vem de
// localmedia.DiretorioDestino (raiz real da série, briefing 0081 -- nunca
// mais uma subpasta de temporada); o nome-base do vídeo continua vindo do
// media_file mais antigo (ArquivoMaisAntigo), que é quem nomeia o .nfo/still
// de filme e episódio.
//
// tv/season SEMPRE são "pasta exclusiva" (briefing 0081): a raiz da série
// nunca tem vídeo diretamente dentro dela (0031/TV1-TV5, os episódios ficam
// em subpastas de temporada) -- então EhPastaExclusiva (que conta vídeo
// PRESENTE direto no diretório) sempre devolveria false ali, forçando o
// pôster/fanart da série a nascer prefixado pelo nome do vídeo mais antigo
// (exatamente o bug relatado: "poster.jpg salvo como ...-S01E01-...-
// poster.jpg"). Kodi não tem conceito de "edição"/ambiguidade para arte de
// NÍVEL DE OBRA de série -- é sempre poster.jpg/fanart.jpg bare (a rotação
// numérica de múltiplas artes continua via sufixoRotacao, independente desta
// flag).
func (s *Server) resolverDestino(ctx context.Context, ent sqlcgen.Entity) (dir, nomeBaseVideo string, pastaExclusiva, ok bool, err error) {
	localFiles, ancestors, err := s.carregarMediaFilesLocalmedia(ctx, ent)
	if err != nil {
		return "", "", false, false, err
	}
	entLocal := localmedia.Entity{ID: ent.ID, Type: ent.Type}
	dir, ok = localmedia.DiretorioDestino(entLocal, localFiles, ancestors)
	if !ok {
		return "", "", false, false, nil
	}
	mf, achou := localmedia.ArquivoMaisAntigo(entLocal, localFiles, ancestors)
	if !achou {
		return "", "", false, false, nil
	}
	nomeBaseVideo = localmedia.NomeBaseVideo(mf.Path)

	if ent.Type == tipoEntSerie || ent.Type == tipoEntTemporada {
		return dir, nomeBaseVideo, true, true, nil
	}

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

// ── GET /entities/{id}/theme — tema musical local (briefing 0082) ──────────
//
// Fundação MÍNIMA e LOCAL-ONLY: sem provider (não há fonte confiável
// integrada de tema musical, ver briefing §3) — o dono coloca manualmente um
// `theme.{mp3,ogg,flac}` na pasta-raiz do item (mesma raiz de
// DiretorioDestino, reaproveitada de resolverDestino, já usada por
// materialize/nfo acima) e este endpoint só DETECTA e serve o binário, sem
// materializar asset algum.

// extensoesTema lista, em ordem de preferência, os formatos de tema aceitos —
// mp3 primeiro (o caso comum Kodi/Jellyfin), ogg/flac como alternativas. Lista
// fixa em código (briefing §5: "não exposta como opção").
var extensoesTema = []string{".mp3", ".ogg", ".flac"}

// encontrarArquivoTema procura theme.<ext> na raiz `dir`, na ordem de
// extensoesTema — a primeira que existir (e não for diretório) vence.
func encontrarArquivoTema(dir string) (string, bool) {
	for _, ext := range extensoesTema {
		caminho := filepath.Join(dir, "theme"+ext)
		info, err := os.Stat(caminho)
		if err == nil && !info.IsDir() {
			return caminho, true
		}
	}
	return "", false
}

// contentTypeTema resolve o content-type do tema pela extensão — as três
// aceitas por encontrarArquivoTema têm content-type conhecido e fixo (sem
// precisar farejar bytes como contentTypeImagem faz para a arte).
func contentTypeTema(caminho string) string {
	switch strings.ToLower(filepath.Ext(caminho)) {
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	default:
		return "audio/mpeg"
	}
}

// getEntityTheme trata GET /api/v1/entities/{id}/theme: resolve a pasta-raiz
// do item (resolverDestino — MESMA resolução de DiretorioDestino usada por
// materialize/nfo) e serve o binário do theme.{mp3,ogg,flac} encontrado ali,
// se houver. O caminho nasce 100% da resolução SERVER-SIDE (nunca de input do
// cliente, que só fornece o id) — sem materializar asset, sem tocar o banco.
// 404 quando a entity não existe, não tem pasta de destino (sem media_file
// present) ou não há arquivo de tema na pasta.
func (s *Server) getEntityTheme(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	dir, _, _, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found",
			"sem pasta de destino na biblioteca (nenhum arquivo presente)")
		return
	}

	caminho, ok := encontrarArquivoTema(dir)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "tema musical não encontrado")
		return
	}

	limpo := filepath.Clean(caminho)
	f, err := os.Open(limpo) //nolint:gosec // caminho vem so da resolucao server-side (resolverDestino), nunca de input do cliente
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "arquivo de tema indisponível")
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "not_found", "arquivo de tema indisponível")
		return
	}
	w.Header().Set("Content-Type", contentTypeTema(limpo))
	http.ServeContent(w, r, filepath.Base(limpo), info.ModTime(), f)
}

// ── Trailer: materialização em disco via yt-dlp (briefing 0102, ADR-0025) ──
//
// GET .../trailer detecta e serve um trailer já materializado (mesmo padrão
// de getEntityTheme, MESMA pasta-raiz de resolverDestino). POST
// .../trailer/download BAIXA o primeiro trailer YouTube listado em
// `videos` (TMDB/TVDB, já extraído via richfields) usando o yt-dlp (0101) e
// grava no nome canônico Kodi/Jellyfin. Sem tabela `asset` nova -- mesmo
// precedente de theme.mp3 (0082): detecção por existência de arquivo, sem
// linha de banco dedicada (vídeo não entra no pipeline de imagem de
// assets.go/parseKind/galeria).

// extensoesTrailer lista, em ordem de checagem, os containers de vídeo que
// encontrarArquivoTrailer aceita como "trailer já presente": .mp4 é o
// destino FIXO de todo download feito por este endpoint (extPadraoDeKind);
// .mkv/.webm cobrem um trailer colocado manualmente pelo dono com outro
// container -- também precisa ser detectado (princípio 5: nunca sobrescrever
// sem force), não só o que o download em si produz.
var extensoesTrailer = []string{".mp4", ".mkv", ".webm"}

// encontrarArquivoTrailer procura o nome canônico do trailer (internal/
// localmedia.NomeArquivoArte com KindTrailer) na raiz `dir`, testando cada
// extensão de extensoesTrailer -- a primeira que existir (e não for
// diretório) vence.
func encontrarArquivoTrailer(dir, nomeBaseVideo string, pastaExclusiva bool) (string, bool) {
	for _, ext := range extensoesTrailer {
		nome := localmedia.NomeArquivoArte(localmedia.KindTrailer, true, 0, pastaExclusiva, nomeBaseVideo, ext)
		caminho := filepath.Join(dir, nome)
		info, err := os.Stat(caminho)
		if err == nil && !info.IsDir() {
			return caminho, true
		}
	}
	return "", false
}

// contentTypeTrailer resolve o content-type do trailer pela extensão -- as
// três aceitas por encontrarArquivoTrailer têm content-type conhecido e
// fixo, mesmo espírito de contentTypeTema.
func contentTypeTrailer(caminho string) string {
	switch strings.ToLower(filepath.Ext(caminho)) {
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	default:
		return "video/mp4"
	}
}

// getEntityTrailer trata GET /api/v1/entities/{id}/trailer: resolve a
// pasta-raiz do item (resolverDestino, MESMA resolução de materialize/nfo/
// theme) e serve o binário do trailer encontrado ali, se houver. 404 quando
// a entity não existe, não tem pasta de destino (sem media_file present) ou
// não há trailer materializado na pasta.
func (s *Server) getEntityTrailer(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	dir, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found",
			"sem pasta de destino na biblioteca (nenhum arquivo presente)")
		return
	}

	caminho, ok := encontrarArquivoTrailer(dir, nomeBaseVideo, pastaExclusiva)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "trailer não encontrado")
		return
	}

	limpo := filepath.Clean(caminho)
	f, err := os.Open(limpo) //nolint:gosec // caminho vem so da resolucao server-side (resolverDestino+encontrarArquivoTrailer), nunca de input do cliente
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "arquivo de trailer indisponível")
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "not_found", "arquivo de trailer indisponível")
		return
	}
	w.Header().Set("Content-Type", contentTypeTrailer(limpo))
	http.ServeContent(w, r, filepath.Base(limpo), info.ModTime(), f)
}

// trailerDownloadRequest é o corpo (opcional) de POST .../trailer/download.
// Key seleciona um vídeo específico por `key` do YouTube (porta aberta para
// um seletor futuro no frontend, briefing §3 "fora"); vazio cai no default
// (primeiro trailer, richfields.PrimeiroTrailerYoutube). Force pede
// re-download mesmo com um trailer já presente (princípio 5: sem Force, um
// trailer existente -- baixado antes OU colocado manualmente pelo dono --
// NUNCA é sobrescrito).
type trailerDownloadRequest struct {
	Key   string `json:"key"`
	Force bool   `json:"force"`
}

// trailerDownloadResult é o corpo de sucesso de POST .../trailer/download.
type trailerDownloadResult struct {
	LocalPath string `json:"local_path"`
}

// escolherTrailerYoutube decide QUAL vídeo baixar: com `key` não-vazio, o
// vídeo do YouTube com essa key exata; vazio cai no default (primeiro
// trailer reprodutível, MESMO critério do frontend --
// richfields.PrimeiroTrailerYoutube).
func escolherTrailerYoutube(videos []richfields.Video, key string) (richfields.Video, bool) {
	if key == "" {
		return richfields.PrimeiroTrailerYoutube(videos)
	}
	for _, v := range videos {
		if v.Site == "YouTube" && v.Key == key {
			return v, true
		}
	}
	return richfields.Video{}, false
}

// ytdlpDisponivel responde 503 (SEM tocar disco) quando não há um binário
// yt-dlp utilizável agora -- checado ANTES de resolver trailer/destino, para
// a feature indisponível nunca gastar trabalho (nem I/O) à toa. Nunca 500:
// indisponibilidade é um estado normal (ADR-0025), não um erro interno.
func (s *Server) ytdlpDisponivel(ctx context.Context, w http.ResponseWriter) bool {
	if s.ytdlp == nil {
		writeError(w, http.StatusServiceUnavailable, "ytdlp_indisponivel", "yt-dlp indisponível")
		return false
	}
	disponivel, _, detalhe := s.ytdlp.Status(ctx)
	if !disponivel {
		msg := "yt-dlp indisponível"
		if detalhe != "" {
			msg += ": " + detalhe
		}
		writeError(w, http.StatusServiceUnavailable, "ytdlp_indisponivel", msg)
		return false
	}
	return true
}

// resolverVideoTrailer resolve o trailer YouTube do item (idioma preferido +
// richfields.Videos, MESMO shape de GET /entities/{id}) já escrevendo a
// resposta de erro apropriada quando falha em qualquer etapa -- extraído de
// postEntityTrailerDownload só para reduzir a complexidade ciclomática do
// handler (cada guard aqui já é exercitado pelos mesmos cenários de erro que
// o handler cobria antes da extração).
func (s *Server) resolverVideoTrailer(w http.ResponseWriter, r *http.Request, id, key string) (richfields.Video, bool) {
	ctx := r.Context()
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return richfields.Video{}, false
	}
	lang, ok := s.parseLang(w, cfg, r.URL.Query().Get("lang"))
	if !ok {
		return richfields.Video{}, false
	}
	resolvida, encontrada, err := s.resolverEntity(ctx, id, lang, false)
	if err != nil {
		respondErroInterno(w)
		return richfields.Video{}, false
	}
	if !encontrada {
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
		return richfields.Video{}, false
	}
	video, ok := escolherTrailerYoutube(resolvida.Videos, key)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "sem_trailer",
			"nenhum trailer do YouTube disponível para este item")
		return richfields.Video{}, false
	}
	return video, true
}

// resolverDestinoTrailerLivre resolve a pasta de destino (resolverDestino) e
// confirma que não há trailer já materializado ali sem `force` -- escreve a
// resposta de erro apropriada (500/404/409) e devolve ok=false quando
// qualquer checagem falha. Extraído de postEntityTrailerDownload pelo mesmo
// motivo de resolverVideoTrailer: reduzir a complexidade ciclomática do
// handler, mantendo cada guard testado pelos mesmos cenários de antes.
func (s *Server) resolverDestinoTrailerLivre(w http.ResponseWriter, ctx context.Context, ent sqlcgen.Entity, force bool) (dir, nomeBaseVideo string, pastaExclusiva, ok bool) {
	dir, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return "", "", false, false
	}
	if !ok {
		respondErroLocalmedia(w, errSemDestino)
		return "", "", false, false
	}
	if _, jaExiste := encontrarArquivoTrailer(dir, nomeBaseVideo, pastaExclusiva); jaExiste && !force {
		writeError(w, http.StatusConflict, "ja_existe",
			"trailer já baixado (use force=true para re-baixar)")
		return "", "", false, false
	}
	return dir, nomeBaseVideo, pastaExclusiva, true
}

// postEntityTrailerDownload trata POST /api/v1/entities/{id}/trailer/download
// (briefing 0102, ADR-0025): resolve o trailer YouTube do item (videos já
// extraído via richfields, MESMO shape de GET /entities/{id}), baixa via
// yt-dlp (s.ytdlp.Baixar, 0101) para um arquivo TEMPORÁRIO no destino final e
// só renomeia para o nome canônico em caso de sucesso (nunca deixa um
// arquivo parcial com o nome canônico). 503 sem yt-dlp disponível; 422 sem
// trailer YouTube no item; 404 sem pasta de destino (sem media_file
// present); 409 quando já existe um trailer e `force` não foi pedido; 502
// quando o próprio download falha (rede, vídeo removido do YouTube, etc.).
func (s *Server) postEntityTrailerDownload(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}
	if !s.ytdlpDisponivel(ctx, w) {
		return
	}

	var body trailerDownloadRequest
	if ok := decodeOpcional(w, r, &body); !ok {
		return
	}

	video, ok := s.resolverVideoTrailer(w, r, id, body.Key)
	if !ok {
		return
	}

	dir, nomeBaseVideo, pastaExclusiva, ok := s.resolverDestinoTrailerLivre(w, ctx, ent, body.Force)
	if !ok {
		return
	}

	destino := filepath.Join(dir, localmedia.NomeArquivoArte(localmedia.KindTrailer, true, 0, pastaExclusiva, nomeBaseVideo, ""))
	if err := s.baixarTrailerAtomico(ctx, video, destino); err != nil {
		writeError(w, http.StatusBadGateway, "youtube_download_failed",
			"falha ao baixar o trailer: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, trailerDownloadResult{LocalPath: destino})
}

// baixarTrailerAtomico baixa `video` (s.ytdlp.Baixar) para um arquivo
// TEMPORÁRIO (destino+".tmp", mesmo diretório do destino final) e só
// RENOMEIA para o nome canônico em caso de sucesso -- nunca deixa um
// binário parcial com o nome canônico (mesmo princípio de
// EscreverArquivoAtomico/RenomearArquivo, adaptado: aqui é o yt-dlp que
// escreve os bytes, não este processo). Falha em qualquer etapa remove o
// temporário (best-effort) e propaga o erro.
func (s *Server) baixarTrailerAtomico(ctx context.Context, video richfields.Video, destino string) error {
	urlOrigem := video.URL
	if urlOrigem == nil || *urlOrigem == "" {
		u := "https://www.youtube.com/watch?v=" + video.Key
		urlOrigem = &u
	}
	destinoTemp := destino + ".tmp"
	if err := s.ytdlp.Baixar(ctx, *urlOrigem, destinoTemp); err != nil {
		_ = os.Remove(destinoTemp)
		return err
	}
	if err := localmedia.RenomearArquivo(destinoTemp, destino); err != nil {
		_ = os.Remove(destinoTemp)
		return err
	}
	return nil
}

// ── Tema musical automático via ThemerrDB (briefing 0103, ADR-0025) ────────
//
// GET .../theme/candidato expõe SE há um candidato do ThemerrDB (sem vazar a
// URL do YouTube ao cliente — decisão conservadora do briefing §6; o download
// acontece inteiramente no servidor) e se já existe um theme.* local
// (reaproveita encontrarArquivoTema). POST .../theme/download baixa o
// candidato via yt-dlp (0101) para theme.mp3 na raiz da pasta — MESMO path
// que GET /entities/{id}/theme (0082) já serve. SEM `force`: tema é singular
// por pasta, então um theme.* já presente (baixado antes OU colocado à mão
// pelo dono) NUNCA é sobrescrito — o dono apaga manualmente antes de
// re-baixar (briefing §6, ao contrário do trailer).

// nomeFonteThemerrdb é o `source` hardcoded do envelope de candidato em
// media_external — hoje só a ThemerrDB implementa provider.TemaMusicalProvider
// (mesmo precedente de "fanarttv" hardcoded em coletarInventarioFanart,
// internal/server/artwork.go).
const nomeFonteThemerrdb = "themerrdb"

// themeCandidatoResult é o corpo de GET .../theme/candidato.
type themeCandidatoResult struct {
	Disponivel bool `json:"disponivel"`
	JaTemLocal bool `json:"ja_tem_local"`
}

// candidatoTemaURL lê o envelope de candidato de tema musical (media_external,
// source=themerrdb, chave provider.ChaveEnvelopeTemaMusical) gravado pelo
// enrich (internal/enrich/tema_musical.go) — NUNCA consulta o ThemerrDB ao
// vivo. ok=false quando não há linha, o payload é ilegível, ou o envelope não
// carrega URL (candidato "sem match" gravado do mesmo jeito, best-effort).
func (s *Server) candidatoTemaURL(ctx context.Context, entityID string) (string, bool) {
	row, err := s.leitura().ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: entityID,
		Source:   nomeFonteThemerrdb,
	})
	if err != nil || !row.PayloadJson.Valid {
		return "", false
	}
	var env map[string]any
	if json.Unmarshal([]byte(row.PayloadJson.String), &env) != nil {
		return "", false
	}
	url, ok := env[provider.ChaveEnvelopeTemaMusical].(string)
	if !ok || url == "" {
		return "", false
	}
	return url, true
}

// getEntityThemeCandidato trata GET /api/v1/entities/{id}/theme/candidato
// (briefing 0103): diz se há um candidato do ThemerrDB e se já existe um
// theme.* local. 404 quando a entity não existe. Nunca falha por a entity não
// ter pasta de destino (media_file ausente) — nesse caso `ja_tem_local`
// simplesmente vem false, junto do que `disponivel` já indicar.
func (s *Server) getEntityThemeCandidato(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	_, disponivel := s.candidatoTemaURL(ctx, id)

	jaTemLocal := false
	if dir, _, _, achouDestino, err := s.resolverDestino(ctx, ent); err == nil && achouDestino {
		_, jaTemLocal = encontrarArquivoTema(dir)
	}

	writeJSON(w, http.StatusOK, themeCandidatoResult{
		Disponivel: disponivel,
		JaTemLocal: jaTemLocal,
	})
}

// themeDownloadResult é o corpo de sucesso de POST .../theme/download.
type themeDownloadResult struct {
	LocalPath string `json:"local_path"`
}

// postEntityThemeDownload trata POST /api/v1/entities/{id}/theme/download
// (briefing 0103, ADR-0025): baixa o candidato cacheado (media_external,
// source=themerrdb) via yt-dlp para theme.mp3 na raiz da pasta — MESMO path
// que GET /entities/{id}/theme (0082) já serve. 503 sem yt-dlp disponível;
// 422 sem candidato; 404 sem pasta de destino (sem media_file present); 409
// quando já existe um theme.* local (baixado antes OU colocado à mão pelo
// dono) — SEM parâmetro `force` nesta entrega (briefing §6: tema é singular
// por pasta); 502 quando o próprio download falha.
func (s *Server) postEntityThemeDownload(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}
	if !s.ytdlpDisponivel(ctx, w) {
		return
	}

	urlOrigem, ok := s.candidatoTemaURL(ctx, id)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "sem_candidato",
			"nenhum candidato de tema musical disponível para este item")
		return
	}

	dir, _, _, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !ok {
		respondErroLocalmedia(w, errSemDestino)
		return
	}

	if _, jaExiste := encontrarArquivoTema(dir); jaExiste {
		writeError(w, http.StatusConflict, "ja_existe",
			"tema musical já presente (apague o arquivo manualmente para re-baixar)")
		return
	}

	destino := filepath.Join(dir, "theme.mp3")
	if err := s.baixarTemaAtomico(ctx, urlOrigem, destino); err != nil {
		writeError(w, http.StatusBadGateway, "youtube_download_failed",
			"falha ao baixar o tema musical: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, themeDownloadResult{LocalPath: destino})
}

// baixarTemaAtomico baixa `urlOrigem` (s.ytdlp.Baixar) para um arquivo
// TEMPORÁRIO no mesmo diretório do destino final e só RENOMEIA para o nome
// canônico em caso de sucesso — mesmo princípio de baixarTrailerAtomico
// (0102), adaptado: aqui a URL já chega pronta do candidato cacheado, sem
// richfields.Video envolvido.
func (s *Server) baixarTemaAtomico(ctx context.Context, urlOrigem, destino string) error {
	destinoTemp := destino + ".tmp"
	if err := s.ytdlp.Baixar(ctx, urlOrigem, destinoTemp); err != nil {
		_ = os.Remove(destinoTemp)
		return err
	}
	if err := localmedia.RenomearArquivo(destinoTemp, destino); err != nil {
		_ = os.Remove(destinoTemp)
		return err
	}
	return nil
}
