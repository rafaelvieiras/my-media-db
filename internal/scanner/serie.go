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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
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
	// tipoEntTemporada/tipoEntEpisodio/statusRascunho: valores do banco usados
	// só pelo caminho do episódio provisório (briefing 0133).
	tipoEntTemporada = "season"
	tipoEntEpisodio  = "episode"
	statusRascunho   = "draft"
)

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
		if !res.emRevisao && res.index != nil {
			// Sem SxxEyy no nome, mas a pasta pode dizer a temporada: o arquivo
			// ainda vira episódio provisório (briefing 0133).
			f.prov = provisorioDe(path, base)
			f.serieConf = res.conf
		}
		return f
	}
	if res.emRevisao || res.index == nil {
		return f // série em revisão: media_file fica pendente, sem vínculo
	}
	ids, matched := tvmatch.Casar(res.index, ep)
	if !matched {
		log.Printf("scanner: episódio de %q não casou na árvore externa; criando provisório: %+v", path, ep)
		f.prov = provisorioDe(path, base)
		f.serieConf = res.conf
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
	if dir != root && naming.EhPastaTemporada(base) {
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
		return serieResolvida{serieID: id, index: r.m.carregarIndex(ctx, id), conf: confIDExterno}
	}
	if id, ok := acharSerieResolvidaPorTitulo(ctx, q, serieNome); ok {
		return serieResolvida{serieID: id, index: r.m.carregarIndex(ctx, id), conf: confSerieEp}
	}

	if res, ok := r.m.tentarImportarSerie(ctx, serieNome, r.langsPermitidos(ctx), r.preferredLangDaFonte()); ok {
		return res
	}

	return serieResolvida{serieID: r.acharOuCriarDraftSerie(ctx, serieNome), conf: confRevisaoSerie, emRevisao: true}
}

// tentarImportarSerie tenta o auto-import de uma série pelo NOME (0034):
// extraído de resolverSerieMiss (briefing 0120) para ser reutilizável pela
// reconciliação de séries-rascunho (reconciliar.go), que precisa da MESMA
// tentativa de import fora do contexto de um scanRun. ok=false em qualquer
// degradação/falha -- sem SerieImporter configurado, nome vazio, ou o import
// falhou (ambíguo/não encontrado/erro de rede) -- com os MESMOS 3 logs de
// sempre. Sucesso: dispara a materialização automática opcional (best-effort,
// nunca aborta a resolução) e devolve a serieResolvida já com o índice de
// episódios carregado.
func (m *Manager) tentarImportarSerie(ctx context.Context, serieNome string, langs []string, preferredLang string) (serieResolvida, bool) {
	if m.serieImporter == nil || serieNome == "" {
		return serieResolvida{}, false
	}
	id, err := m.serieImporter.ImportarSeriePorNome(ctx, serieNome, langs, preferredLang)
	if err != nil {
		if errors.Is(err, enrich.ErrSerieAmbigua) {
			log.Printf("scanner: série %q ambígua no provider — vai para revisão", serieNome)
		} else if errors.Is(err, enrich.ErrSerieNaoEncontrada) {
			log.Printf("scanner: série %q não encontrada no provider — vai para revisão", serieNome)
		} else {
			log.Printf("scanner: auto-import da série %q falhou (%v) — vai para revisão", serieNome, err)
		}
		return serieResolvida{}, false
	}
	// Materialização automática opcional (0089): best-effort, nunca
	// aborta a resolução -- a série já foi importada com sucesso.
	if m.materializador != nil {
		if merr := m.materializador.MaterializarSeAutoDownloadLigado(ctx, id); merr != nil {
			log.Printf("scanner: materialização automática da série %q falhou (%v)", serieNome, merr)
		}
	}
	return serieResolvida{serieID: id, index: m.carregarIndex(ctx, id), conf: confSerieEp}, true
}

// langsPermitidos devolve os idiomas permitidos (settings) para o auto-import, ou nil
// quando o scanner opera sem essa fonte (o provider cai no idioma default).
func (r *scanRun) langsPermitidos(ctx context.Context) []string {
	if r.m.idiomas == nil {
		return nil
	}
	return r.m.idiomas(ctx)
}

// preferredLangDaFonte devolve o idioma preferido da library_source que
// disparou este scan (briefing 0084), ou "" quando a fonte não tem
// preferência definida -- nesse caso o SerieImporter cai no idioma global
// (langsPermitidos/Manager.idiomas), mesmo comportamento de antes deste
// briefing.
func (r *scanRun) preferredLangDaFonte() string {
	if r.source.PreferredLanguage == nil {
		return ""
	}
	return *r.source.PreferredLanguage
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
// SEM filtro de status, de propósito: usada EXCLUSIVAMENTE por
// acharOuCriarDraftSerie (find-or-create do rascunho de revisão), que precisa
// continuar reencontrando o MESMO draft já criado para não duplicá-lo a cada
// scan. O caminho de RESOLUÇÃO (resolverSerieMiss) usa
// acharSerieResolvidaPorTitulo abaixo, que filtra rascunho -- NÃO reuse esta
// função ali (briefing 0120).
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

// acharSerieResolvidaPorTitulo procura a série já RESOLVIDA de verdade
// (type='tv', título, qualquer status DIFERENTE de 'draft') -- usada SÓ pelo
// caminho de RESOLUÇÃO (resolverSerieMiss). Bug corrigido no briefing 0120: um
// rascunho de revisão (status='draft', criado por acharOuCriarDraftSerie)
// nunca deve ser reencontrado aqui como se fosse identidade resolvida --
// antes deste fix, isso bloqueava PERMANENTEMENTE a tentativa de auto-import
// real (tentarImportarSerie nunca era alcançado de novo depois que o
// rascunho existia). "", false tanto para "não achou" quanto para "achou,
// mas é rascunho".
func acharSerieResolvidaPorTitulo(ctx context.Context, q *sqlcgen.Queries, nome string) (string, bool) {
	if nome == "" {
		return "", false
	}
	e, err := q.ObterEntityPorTituloTipo(ctx, sqlcgen.ObterEntityPorTituloTipoParams{Type: tipoSerieTV, Title: nome})
	if err != nil || e.Status == "draft" {
		return "", false
	}
	return e.ID, true
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
// depois, sem SQL. Método de Manager (não scanRun, briefing 0120) para ser
// reutilizável fora de um scan (reconciliar.go, tentarImportarSerie) -- não usa
// nenhum estado específico de scanRun.
func (m *Manager) carregarIndex(ctx context.Context, serieID string) *tvmatch.Index {
	q := sqlcgen.New(m.db.Read)
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
		m.indexarTemporada(ctx, q, idx, t.ChildID)
	}
	return idx
}

// indexarTemporada percorre os episódios de UMA temporada e os insere no índice.
// Temporada ilegível é pulada (degrada sem derrubar o resto do índice).
func (m *Manager) indexarTemporada(ctx context.Context, q *sqlcgen.Queries, idx *tvmatch.Index, tempID string) {
	eps, err := q.ListarRelacoesFilhas(ctx, tempID)
	if err != nil {
		return
	}
	for _, e := range eps {
		if e.Kind != kindVinculoEpisodio {
			continue
		}
		m.indexarEpisodio(ctx, q, idx, e.ChildID)
	}
}

// indexarEpisodio lê o extension_data de um episódio e delega a montagem em memória
// (numbering + absolute_number) ao internal/tvmatch. Episódio ilegível é pulado.
func (m *Manager) indexarEpisodio(ctx context.Context, q *sqlcgen.Queries, idx *tvmatch.Index, epID string) {
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

	// A árvore externa não tinha o episódio, mas o arquivo declara a numeração:
	// cria o provisório e segue como um vínculo normal (briefing 0133).
	if len(f.episodeIDs) == 0 && f.prov != nil {
		ids, perr := r.acharOuCriarProvisorios(ctx, q, f.serieID, *f.prov)
		if perr != nil {
			log.Printf("scanner: falha ao criar o episódio provisório de %q (fica em revisão): %v", f.path, perr)
		} else {
			f.episodeIDs = ids
		}
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

// provisorioEpisodio é a numeração que o ARQUIVO declara e a árvore externa
// não tem (briefing 0133): temporada + números lidos do nome, ou só a
// temporada da pasta quando o nome não traz SxxEyy. titulo é o nome do arquivo
// sem extensão, usado como título do episódio provisório.
type provisorioEpisodio struct {
	season  int
	numeros []int
	titulo  string
	arquivo string // nome do arquivo sem extensão: chave de dedup quando não há número
}

// provisorioDe monta o provisório a partir do parse do nome do arquivo ou, sem
// SxxEyy, da pasta de temporada em que ele vive. Devolve nil quando não há
// temporada confiável (arquivo solto na raiz, pasta que não é de temporada).
func provisorioDe(path, base string) *provisorioEpisodio {
	if ep, ok := naming.ParseEpisode(base); ok && len(ep.Episodes) > 0 && !ep.IsAbsolute {
		return &provisorioEpisodio{season: ep.Season, numeros: ep.Episodes, titulo: tituloDoArquivo(base), arquivo: base}
	}
	if pareceExtra(base) {
		// Extras (featurette, sample, trailer...) moram nas pastas de temporada e
		// não são episódios: viram provisório só quando o nome diz o número.
		return nil
	}
	if n, ok := naming.NumeroDaPastaTemporada(filepath.Base(filepath.Dir(path))); ok {
		return &provisorioEpisodio{season: n, titulo: tituloDoArquivo(base), arquivo: base}
	}
	return nil
}

// tituloDoArquivo limpa o nome do arquivo para servir de título do provisório
// (remove tokens técnicos como resolução e fonte). O número do episódio, quando
// existe, entra no título pelo chamador ("Episódio N"), o mesmo padrão que o
// enriquecimento usa quando a fonte não dá título — o nome cru do arquivo fica
// no media_file.path de qualquer forma.
func tituloDoArquivo(base string) string {
	if t := naming.Parse(base).Title; t != "" {
		return t
	}
	return base
}

// palavrasDeExtra são os nomes que marcam material extra dentro de uma pasta de
// temporada. Sem elas, um "Season 01/featurette bonus.mkv" viraria episódio
// provisório — o que seria mentira sobre a biblioteca.
var palavrasDeExtra = []string{
	"featurette", "sample", "trailer", "teaser", "extras", "bonus", "deleted",
	"behind the scenes", "behind-the-scenes", "bloopers", "outtakes", "interview",
	"making of", "making-of", "promo", "recap",
}

// pareceExtra diz se o nome do arquivo marca material extra.
func pareceExtra(base string) bool {
	l := strings.ToLower(base)
	for _, p := range palavrasDeExtra {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// acharOuCriarProvisorios devolve os ids dos episódios provisórios para o
// arquivo: um por número lido do nome, ou um só sem numeração (dedup por
// título dentro da temporada). Cria a temporada quando ela também não existe
// na série. Idempotente: re-scan acha o que já foi criado.
func (r *scanRun) acharOuCriarProvisorios(ctx context.Context, q *sqlcgen.Queries, serieID string, prov provisorioEpisodio) ([]string, error) {
	tempID, err := r.acharOuCriarTemporadaProvisoria(ctx, q, serieID, prov.season)
	if err != nil {
		return nil, err
	}
	if len(prov.numeros) == 0 {
		id, err := r.acharOuCriarEpisodioProvisorio(ctx, q, tempID, prov.season, 0, false, prov.titulo, prov.arquivo)
		if err != nil {
			return nil, err
		}
		return []string{id}, nil
	}
	ids := make([]string, 0, len(prov.numeros))
	for _, n := range prov.numeros {
		id, err := r.acharOuCriarEpisodioProvisorio(ctx, q, tempID, prov.season, n, true, fmt.Sprintf("Episódio %d", n), prov.arquivo)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// acharOuCriarTemporadaProvisoria faz o find-or-create da temporada pelo
// sort_index dentro da série (mesma chave que o enrich usa em
// acharOuCriarTemporada, sem id de fonte).
func (r *scanRun) acharOuCriarTemporadaProvisoria(ctx context.Context, q *sqlcgen.Queries, serieID string, season int) (string, error) {
	rels, err := q.ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		return "", err
	}
	for _, rel := range rels {
		if rel.Kind == kindRelTemporada && rel.SortIndex.Valid && int(rel.SortIndex.Int64) == season {
			return rel.ChildID, nil
		}
	}
	id := r.m.novoID()
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:    id,
		Type:  tipoEntTemporada,
		Title: tituloTemporadaProvisoria(season),
	}); err != nil {
		return "", err
	}
	if _, err := q.CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID:        r.m.novoID(),
		ParentID:  serieID,
		ChildID:   id,
		Kind:      kindRelTemporada,
		SortIndex: sql.NullInt64{Int64: int64(season), Valid: true},
	}); err != nil {
		return "", err
	}
	return id, nil
}

// acharOuCriarEpisodioProvisorio faz o find-or-create do episódio na temporada:
// com número, a chave é o sort_index da relação (como o enrich liga); sem
// número, é o título exato dentro da temporada. O provisório nasce
// status='draft' e marca a origem no extension_data, para a UI e a
// reconciliação (briefing 0120) saberem que o número veio do arquivo.
func (r *scanRun) acharOuCriarEpisodioProvisorio(ctx context.Context, q *sqlcgen.Queries, tempID string, season, number int, comNumero bool, titulo, arquivo string) (string, error) {
	rels, err := q.ListarRelacoesFilhas(ctx, tempID)
	if err != nil {
		return "", err
	}
	for _, rel := range rels {
		if rel.Kind != kindVinculoEpisodio {
			continue
		}
		if comNumero {
			if rel.SortIndex.Valid && int(rel.SortIndex.Int64) == number {
				r.atualizarTituloDoProvisorio(ctx, q, rel.ChildID, titulo)
				return rel.ChildID, nil
			}
			continue
		}
		// Sem número a chave é o NOME DO ARQUIVO gravado no extension_data, não
		// o título: o título é regra de apresentação e pode mudar de uma versão
		// para outra (foi o que criou provisório duplicado na primeira rodada).
		existente, oerr := q.ObterEntity(ctx, rel.ChildID)
		if oerr == nil && strings.Contains(existente.ExtensionDataJson.String, `"arquivo":"`+arquivo+`"`) {
			return rel.ChildID, nil
		}
	}
	id := r.m.novoID()
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                id,
		Type:              tipoEntEpisodio,
		Title:             titulo,
		ExtensionDataJson: nsBytes(extensionProvisorio(season, number, comNumero, arquivo)),
	}); err != nil {
		return "", err
	}
	// CriarEntity não recebe status; o provisório nasce 'draft' num UPDATE, o
	// mesmo caminho que a faixa rascunho da música usa (musica.go).
	if _, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID:                id,
		Type:              tipoEntEpisodio,
		Title:             titulo,
		Status:            statusRascunho,
		ExtensionDataJson: nsBytes(extensionProvisorio(season, number, comNumero, arquivo)),
	}); err != nil {
		return "", err
	}
	if _, err := q.CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID:        r.m.novoID(),
		ParentID:  tempID,
		ChildID:   id,
		Kind:      kindVinculoEpisodio,
		SortIndex: sql.NullInt64{Int64: int64(number), Valid: true},
	}); err != nil {
		return "", err
	}
	log.Printf("scanner: episódio provisório %q -> S%02dE%02d (a árvore externa não tem esse episódio)", titulo, season, number)
	return id, nil
}

// atualizarTituloDoProvisorio mantém o título de um provisório já criado em dia
// com a regra atual, sem tocar em episódio vindo da fonte (a marca
// `provisorio` no extension_data é o que distingue os dois). Erro é logado e
// ignorado: título defasado não pode derrubar o scan.
func (r *scanRun) atualizarTituloDoProvisorio(ctx context.Context, q *sqlcgen.Queries, epID, titulo string) {
	ep, err := q.ObterEntity(ctx, epID)
	if err != nil || ep.Title == titulo {
		return
	}
	if !strings.Contains(ep.ExtensionDataJson.String, `"provisorio"`) {
		return
	}
	if _, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID:                epID,
		Type:              ep.Type,
		Title:             titulo,
		Status:            ep.Status,
		ExtensionDataJson: ep.ExtensionDataJson,
	}); err != nil {
		log.Printf("scanner: falha ao atualizar o título do episódio provisório %q: %v", epID, err)
	}
}

// tituloTemporadaProvisoria nomeia a temporada criada pelo scanner com a mesma
// convenção do enrich ("Especiais" para a 0).
func tituloTemporadaProvisoria(season int) string {
	if season == 0 {
		return "Especiais"
	}
	return fmt.Sprintf("Temporada %d", season)
}

// extensionProvisorio monta o core.tv@1 do episódio provisório: a numeração
// oficial e a marca de que ela veio do arquivo, não da fonte externa.
func extensionProvisorio(season, number int, comNumero bool, arquivo string) []byte {
	numero := map[string]any{"season": season}
	if comNumero {
		numero["number"] = number
	}
	doc := map[string]any{
		nsTV: map[string]any{
			"numbering":  map[string]any{"official": numero},
			"provisorio": map[string]any{"origem": "arquivo", "arquivo": arquivo},
		},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	return b
}
