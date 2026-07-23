package scanner

// serie.go é o núcleo do scanner de SÉRIES (briefing 0034): quando a biblioteca é
// `content_type='series'`, cada arquivo de vídeo é um (ou mais) episódio(s). O fluxo,
// por arquivo:
//
//  1. deriva a IDENTIDADE da série do nome da PASTA da série (a pasta acima de
//     `Season NN/` quando houver);
//  2. RESOLVE a entity da série — id externo embutido → catálogo por título →
//     auto-import do provider (enrich). Ambíguo/não encontrado ⇒ rascunho de revisão;
//  3. carrega a árvore de episódios da série UMA VEZ (cache por série) e casa o
//     número do arquivo ao `episode_id` EM MEMÓRIA (ordenação default, com fallback
//     às demais e ao absoluto);
//  4. persiste o media_file apontando para a SÉRIE e os vínculos N:M por episódio
//     (`media_file_entity`, kind='tv_episode'), ANCORADOS no id (ADR-0021): trocar a
//     ordenação depois re-projeta números sem tocar vínculo nenhum.
//
// Tudo o que ESCREVE fora da junção (o auto-import, o rascunho de revisão) roda na
// fase de fatos, fora da transação de escrita do lote — o import do enrich usa a
// própria conexão de escrita, então não há transação aninhada.

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
	"github.com/rafaelvieiras/my-media-db/internal/tvmatch"
)

const (
	// contentTypeSeries é o valor de library_source.content_type que liga o fluxo de
	// série (o resto — inclusive o default "movie" — segue o caminho de filme).
	contentTypeSeries = "series"
	// tipoSerieTV é o entity.type da série (espelha enrich.tipoSerie).
	tipoSerieTV = "tv"
	// kindRelTemporada/kindVinculoEpisodio: o kind de entity_relation série→temporada
	// e temporada→episódio (enrich) e o kind do vínculo arquivo↔episódio na junção
	// media_file_entity. Mesmo rótulo "tv_episode" nos dois papéis, de propósito.
	kindRelTemporada    = "tv_season"
	kindVinculoEpisodio = "tv_episode"
	// nsTV é o namespace do extension_data da vertical de séries (espelha enrich.nsTV).
	nsTV = "core.tv@1"
	// Confiança do caminho de série (escala 0–100 do confianca()): número casado sem
	// ambiguidade ≈ 90; série resolvida por id externo embutido = confIDExterno (100).
	// Sem episódio casado (revisão) cai para confRevisaoSerie, abaixo do limiar do
	// front (70), o que joga o media_file para a fila de revisão.
	confSerieEp      = 90
	confRevisaoSerie = 40
)

// reSeasonFolder reconhece a pasta de temporada (Kodi/Jellyfin/Plex): "Season 01",
// "Temporada 1", "S01". Usada só para SUBIR à pasta da série; a temporada real do
// episódio vem do nome do arquivo (ParseEpisode), não daqui.
var reSeasonFolder = regexp.MustCompile(`(?i)^(season|temporada|s)[ ._-]*\d{1,4}$`)

// serieResolvida é o resultado (cacheado por série no scan) de resolver a entity de
// uma série e carregar sua árvore de episódios. emRevisao marca a série que não pôde
// ser identificada (ambígua/não encontrada) — um rascunho, sem árvore.
type serieResolvida struct {
	serieID   string
	index     *tvmatch.Index
	conf      float64
	emRevisao bool
}

// factsSerie monta os fatos de um arquivo de série: deriva a pasta/nome da série,
// resolve a entity da série (cacheada) e casa o número do arquivo aos episódios. Não
// escreve o media_file/vínculo (isso é da transação do lote) — só resolve e casa.
func (r *scanRun) factsSerie(ctx context.Context, path string) fileFacts {
	base := semExtensao(filepath.Base(path))
	f := fileFacts{path: path, isSerie: true, folderDir: filepath.Dir(path)}

	serieFolder, serieNome, folderInfo := derivarSerie(r.source.Path, path)
	f.rawNome = serieNome

	res := r.resolverSerie(ctx, serieFolder, serieNome, folderInfo)
	f.serieID = res.serieID
	f.serieConf = res.conf
	if res.serieID == "" {
		return f // resolução falhou por completo (sem entity): o gravador pula o arquivo
	}

	ep, ok := naming.ParseEpisode(base)
	if !ok {
		log.Printf("scanner: %q sem padrão de episódio reconhecível; vai para revisão", path)
		return f
	}
	if res.emRevisao || res.index == nil {
		return f // série em revisão: media_file fica pendente, sem vínculo
	}
	ids, matched := tvmatch.Casar(res.index, ep)
	if !matched {
		log.Printf("scanner: episódio de %q não casou (revisão): %+v", path, ep)
		return f
	}
	f.episodeIDs = ids
	return f
}

// derivarSerie decide a pasta e o nome da série de um arquivo. Se o arquivo está
// numa pasta de temporada ("Season 01"), a série é a pasta ACIMA; senão é a pasta
// imediata. O nome sai do naming.Parse (título limpo + id externo embutido). Arquivo
// solto na raiz cai no nome do próprio arquivo (série mal identificada → revisão).
func derivarSerie(root, path string) (serieFolder, serieNome string, info naming.Info) {
	dir := filepath.Dir(path)
	base := filepath.Base(dir)
	if dir != root && ehPastaTemporada(base) {
		if parent := filepath.Dir(dir); parent != "." && parent != string(filepath.Separator) {
			serieFolder = parent
		} else {
			serieFolder = dir
		}
	} else {
		serieFolder = dir
	}

	nomeBase := filepath.Base(serieFolder)
	if serieFolder == root {
		nomeBase = semExtensao(filepath.Base(path))
	}
	info = naming.Parse(nomeBase)
	serieNome = info.Title
	if serieNome == "" {
		serieNome = nomeBase
	}
	return serieFolder, serieNome, info
}

// ehPastaTemporada diz se o nome de pasta é uma pasta de temporada (para subir à
// pasta da série). Inclui os specials (temporada 0).
func ehPastaTemporada(nome string) bool {
	l := strings.ToLower(strings.TrimSpace(nome))
	if l == "specials" || l == "especiais" {
		return true
	}
	return reSeasonFolder.MatchString(nome)
}

// resolverSerie devolve a resolução da série, do cache do scan quando já vista. Chave
// = pasta da série, para que todos os arquivos daquela série reusem a mesma entity e
// o mesmo índice sem re-buscar/re-importar.
func (r *scanRun) resolverSerie(ctx context.Context, serieFolder, serieNome string, folderInfo naming.Info) serieResolvida {
	if v, ok := r.serieCache[serieFolder]; ok {
		return v
	}
	res := r.resolverSerieMiss(ctx, serieNome, folderInfo)
	r.serieCache[serieFolder] = res
	return res
}

// resolverSerieMiss resolve a série ausente do cache. Ordem (0034 §C.3): id externo
// embutido (type=tv) → catálogo por título (type=tv) → auto-import pelo nome. Sucesso
// carrega o índice de episódios. Ambíguo/não encontrado/import desligado → rascunho
// de revisão (find-or-create por título), SEM árvore.
func (r *scanRun) resolverSerieMiss(ctx context.Context, serieNome string, folderInfo naming.Info) serieResolvida {
	q := sqlcgen.New(r.m.db.Read)

	if id, ok := acharSeriePorIDExterno(ctx, q, folderInfo); ok {
		return serieResolvida{serieID: id, index: r.carregarIndex(ctx, id), conf: confIDExterno}
	}
	if id, ok := acharSeriePorTitulo(ctx, q, serieNome); ok {
		return serieResolvida{serieID: id, index: r.carregarIndex(ctx, id), conf: confSerieEp}
	}

	if r.m.serieImporter != nil && serieNome != "" {
		id, err := r.m.serieImporter.ImportarSeriePorNome(ctx, serieNome, r.langsPermitidos(ctx))
		if err == nil {
			return serieResolvida{serieID: id, index: r.carregarIndex(ctx, id), conf: confSerieEp}
		}
		if errors.Is(err, enrich.ErrSerieAmbigua) {
			log.Printf("scanner: série %q ambígua no provider — vai para revisão", serieNome)
		} else if errors.Is(err, enrich.ErrSerieNaoEncontrada) {
			log.Printf("scanner: série %q não encontrada no provider — vai para revisão", serieNome)
		} else {
			log.Printf("scanner: auto-import da série %q falhou (%v) — vai para revisão", serieNome, err)
		}
	}

	return serieResolvida{serieID: r.acharOuCriarDraftSerie(ctx, serieNome), conf: confRevisaoSerie, emRevisao: true}
}

// langsPermitidos devolve os idiomas permitidos (settings) para o auto-import, ou nil
// quando o scanner opera sem essa fonte (o provider cai no idioma default).
func (r *scanRun) langsPermitidos(ctx context.Context) []string {
	if r.m.idiomas == nil {
		return nil
	}
	return r.m.idiomas(ctx)
}

// acharSeriePorIDExterno procura a série pelo id embutido no nome da pasta (tmdb tem
// prioridade sobre imdb), FILTRANDO type='tv' — um id numérico igual ao de um filme
// não deve casar a série.
func acharSeriePorIDExterno(ctx context.Context, q *sqlcgen.Queries, info naming.Info) (string, bool) {
	if info.TMDBID != "" {
		if e, err := q.ObterEntityPorTmdb(ctx, nsOpt(info.TMDBID)); err == nil && e.Type == tipoSerieTV {
			return e.ID, true
		}
	}
	if info.IMDBID != "" {
		if e, err := q.ObterEntityPorImdb(ctx, nsOpt(info.IMDBID)); err == nil && e.Type == tipoSerieTV {
			return e.ID, true
		}
	}
	return "", false
}

// acharSeriePorTitulo procura a série já no catálogo pelo par (type='tv', título),
// para reusar uma série importada num scan anterior sem novo round-trip ao provider.
func acharSeriePorTitulo(ctx context.Context, q *sqlcgen.Queries, nome string) (string, bool) {
	if nome == "" {
		return "", false
	}
	e, err := q.ObterEntityPorTituloTipo(ctx, sqlcgen.ObterEntityPorTituloTipoParams{Type: tipoSerieTV, Title: nome})
	if err == nil {
		return e.ID, true
	}
	return "", false
}

// acharOuCriarDraftSerie é o FALLBACK de revisão: reusa (find) a série rascunho pelo
// título ou cria uma entity type='tv' status='draft' (não-identificada, visível na
// fila de revisão/órfãs — 0018). É a âncora NOT NULL do media_file quando a série não
// pôde ser resolvida; NÃO é uma série "fantasma" com episódios inventados. "" só
// quando nem isso pôde ser criado (o gravador então pula o arquivo).
func (r *scanRun) acharOuCriarDraftSerie(ctx context.Context, nome string) string {
	if id, ok := acharSeriePorTitulo(ctx, sqlcgen.New(r.m.db.Read), nome); ok {
		return id
	}
	titulo := nome
	if titulo == "" {
		titulo = "Série não identificada"
	}
	q := sqlcgen.New(r.m.db.Write)
	id := r.m.novoID()
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: id, Type: tipoSerieTV, Title: titulo}); err != nil {
		log.Printf("scanner: falha ao criar série rascunho %q: %v", titulo, err)
		return ""
	}
	if _, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID: id, Type: tipoSerieTV, Title: titulo, Status: "draft",
	}); err != nil {
		log.Printf("scanner: falha ao rebaixar série rascunho %q para draft: %v", titulo, err)
	}
	return id
}

// carregarIndex monta o índice número→episode_id de uma série, lendo a série (para as
// ordenações) e cada episódio (numbering + absolute_number no extension_data). É a
// leitura ÚNICA por série do scan; o casamento em memória (internal/tvmatch) vem
// depois, sem SQL.
func (r *scanRun) carregarIndex(ctx context.Context, serieID string) *tvmatch.Index {
	q := sqlcgen.New(r.m.db.Read)
	idx := tvmatch.NovoIndex()

	serie, err := q.ObterEntity(ctx, serieID)
	if err != nil {
		log.Printf("scanner: série %s ilegível ao montar índice: %v", serieID, err)
		return idx
	}
	idx.PreencherOrdenacoes(extrairTVNS(serie.ExtensionDataJson))

	temps, err := q.ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		log.Printf("scanner: temporadas da série %s ilegíveis: %v", serieID, err)
		return idx
	}
	for _, t := range temps {
		if t.Kind != kindRelTemporada {
			continue
		}
		r.indexarTemporada(ctx, q, idx, t.ChildID)
	}
	return idx
}

// indexarTemporada percorre os episódios de UMA temporada e os insere no índice.
// Temporada ilegível é pulada (degrada sem derrubar o resto do índice).
func (r *scanRun) indexarTemporada(ctx context.Context, q *sqlcgen.Queries, idx *tvmatch.Index, tempID string) {
	eps, err := q.ListarRelacoesFilhas(ctx, tempID)
	if err != nil {
		return
	}
	for _, e := range eps {
		if e.Kind != kindVinculoEpisodio {
			continue
		}
		r.indexarEpisodio(ctx, q, idx, e.ChildID)
	}
}

// indexarEpisodio lê o extension_data de um episódio e delega a montagem em memória
// (numbering + absolute_number) ao internal/tvmatch. Episódio ilegível é pulado.
func (r *scanRun) indexarEpisodio(ctx context.Context, q *sqlcgen.Queries, idx *tvmatch.Index, epID string) {
	ent, err := q.ObterEntity(ctx, epID)
	if err != nil {
		return
	}
	idx.IndexarEpisodio(epID, extrairTVNS(ent.ExtensionDataJson))
}

// gravarArquivoSerie persiste o arquivo de série: o media_file aponta para a SÉRIE (a
// obra), e os vínculos por episódio vivem na junção N:M. Idempotente: dedup por
// content_hash (rename/move) e re-scan limpo dos vínculos por kind antes de recriar
// — os ids re-projetados no scan atual, nunca números. Sem episódio casado, o
// media_file entra com confiança de revisão (aparece pendente na fila).
func (r *scanRun) gravarArquivoSerie(ctx context.Context, q *sqlcgen.Queries, f fileFacts) error {
	if f.serieID == "" {
		log.Printf("scanner: pulando %q: série não resolvível e sem rascunho", f.path)
		return nil
	}

	matchConf := float64(confRevisaoSerie)
	if len(f.episodeIDs) > 0 {
		matchConf = f.serieConf
	}

	mediaFileID, err := r.upsertMediaFileSerie(ctx, q, f, matchConf)
	if err != nil {
		return err
	}

	// Re-scan limpo dos vínculos de episódio deste arquivo (não toca outros kinds) e
	// recria pelos ids casados agora — a âncora do ADR-0021.
	if err := q.RemoverVinculosDoArquivoPorKind(ctx, sqlcgen.RemoverVinculosDoArquivoPorKindParams{
		MediaFileID: mediaFileID,
		Kind:        kindVinculoEpisodio,
	}); err != nil {
		return err
	}
	for i, epID := range f.episodeIDs {
		if err := q.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
			MediaFileID: mediaFileID,
			EntityID:    epID,
			Kind:        kindVinculoEpisodio,
			Ordinal:     int64(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

// upsertMediaFileSerie cria o media_file (ou reencontra pelo content_hash e atualiza
// o path/estado). Devolve o id do media_file para os vínculos.
func (r *scanRun) upsertMediaFileSerie(ctx context.Context, q *sqlcgen.Queries, f fileFacts, matchConf float64) (string, error) {
	existente, err := q.ObterMediaFilePorContentHash(ctx, nsOpt(f.hash))
	if err == nil {
		_, uerr := q.AtualizarMediaFileVisto(ctx, sqlcgen.AtualizarMediaFileVistoParams{
			Path:            f.path,
			Size:            niInt(f.size),
			Mtime:           nsOpt(f.mtime),
			LastSeen:        nsOpt(r.scanTS),
			LibrarySourceID: nsOpt(r.source.ID),
			ID:              existente.ID,
		})
		return existente.ID, uerr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id := r.m.novoID()
	_, cerr := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID:              id,
		EntityID:        f.serieID,
		Path:            f.path,
		ContentHash:     nsOpt(f.hash),
		Size:            niInt(f.size),
		Mtime:           nsOpt(f.mtime),
		MatchConfidence: nfFloat(matchConf),
		MediainfoJson:   nsBytes(f.mediainfo),
		State:           "present",
		LastSeen:        nsOpt(r.scanTS),
		LibrarySourceID: nsOpt(r.source.ID),
	})
	return id, cerr
}

// extrairTVNS extrai o objeto do namespace core.tv@1 do extension_data. nil quando
// ausente/ilegível (o chamador degrada — série/episódio sem numeração não indexa).
// Delega ao parser único de tvext (mesmo envelope namespace→objeto reusado pelo
// server no 0039); mantido como nome local pela clareza dos chamadores do scanner.
func extrairTVNS(raw sql.NullString) map[string]any {
	return tvext.Namespace(raw, nsTV)
}
