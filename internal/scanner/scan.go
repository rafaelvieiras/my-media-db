package scanner

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
)

// extensoesVideo é o filtro de extensões (ponto de partida, doc 06). Minúsculas.
var extensoesVideo = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true,
}

// extensoesAudio é o filtro de extensões de ÁUDIO (briefing 0107, molde de
// extensoesVideo) — usado no lugar de extensoesVideo quando
// library_source.content_type == contentTypeMusic (ver extensoesPrincipais).
var extensoesAudio = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true,
	".ogg": true, ".opus": true, ".wav": true,
}

// nomesPoster são os nomes de arte local de nível de pasta (Kodi/Jellyfin).
var nomesPoster = []string{"poster.jpg", "poster.png", "folder.jpg", "folder.png"}

// scanRun carrega o contexto de UM scan: evita passar meia dúzia de parâmetros
// por função e concentra o cache de entidades (folderKey -> entityID) que torna
// o re-scan idempotente e agrupa as versões da mesma obra.
type scanRun struct {
	m          *Manager
	source     library.Source
	scanTS     string
	probeAtivo bool
	cache      map[string]string
	// serieCache guarda a resolução de cada série do scan (chave = pasta da série),
	// para não re-buscar/re-importar a mesma série a cada arquivo. Análogo ao cache de
	// folderKey do filme, mas keyed pela pasta da série (0034).
	serieCache map[string]serieResolvida
	// parcial marca que uma SUBPASTA (não a raiz, já validada por Stat) falhou
	// ao ser lida durante o WalkDir (ex.: timeout/desconexão transitória de um
	// mount de rede FUSE/rclone no meio da varredura). Diferente da raiz
	// inacessível (offline, aborta antes de escrever qualquer coisa), aqui os
	// arquivos que FORAM vistos continuam sendo gravados normalmente -- só o
	// fechamento destrutivo do ciclo de vida (marcar `missing` o que não foi
	// revisto) é pulado, para não desconectar arquivos que só não foram
	// listados por instabilidade passageira do mount, não porque sumiram de
	// verdade do disco.
	parcial bool

	// progresso/filesSeen/filesTotal (briefing 0108) são o estado local de
	// progresso desta ÚNICA execução -- substituem o antigo *job em memória
	// no Manager (addSeen/setTotal sob m.mu): não precisam mais ser
	// thread-safe entre execuções concorrentes (a fila garante concorrência
	// 1), só entre o worker desta execução, que é single-threaded aqui.
	// progresso pode ser nil (bypass da fila em teste, ver scanner_test.go);
	// filesSeen incrementa POR ARQUIVO processado (sucesso OU erro/skip),
	// dentro do loop interno de processarLotes -- não mais por lote de 25
	// (era isso que fazia o contador parecer travado em scans de música
	// lentos, achado real na validação em produção do briefing 0107).
	progresso  execqueue.ProgressoFunc
	filesSeen  int
	filesTotal int

	// novos acumula as entities CRIADAS nesta passada de scan, por tipo, para o
	// pós-import (auto-match de filme, materialização opcional) rodar DEPOIS do
	// commit do lote -- briefing 0135/ADR-0031. Antes, o gatilho era chamado de
	// dentro de gravarArquivo, isto é, dentro da transação: o auto-match lia a
	// entity pelo pool de LEITURA e recebia `sql: no rows in result set` (a linha
	// não commitada não existe para ele) e, se tentasse escrever, travaria na
	// conexão ÚNICA de escrita (ADR-0010) -- deadlock. Ver despacharNovos.
	novos []entidadeNova
}

// entidadeNova é uma entity criada por este scan, com o tipo e a confiança já
// resolvidos -- o que despacharNovos precisa para decidir qual gancho chamar.
type entidadeNova struct {
	id   string
	tipo string
	conf float64
}

// registrarNovo guarda uma entity criada nesta passada (dedup por id, porque o
// fluxo de música pode resolver o mesmo álbum para várias faixas).
func (r *scanRun) registrarNovo(e entidadeNova) {
	for _, existente := range r.novos {
		if existente.id == e.id {
			return
		}
	}
	r.novos = append(r.novos, e)
}

// reportarProgresso chama o callback de progresso (se houver) com o estado
// atual de filesSeen/filesTotal desta execução.
func (r *scanRun) reportarProgresso() {
	r.reportarProgressoMSG("")
}

// reportarProgressoMSG é reportarProgresso com uma mensagem livre (usado pela
// etapa de pós-scan, briefing 0135, para o dono ver o que está acontecendo sem
// inventar contagem de arquivos).
func (r *scanRun) reportarProgressoMSG(msg string) {
	if r.progresso != nil {
		r.progresso(r.filesSeen, r.filesTotal, msg)
	}
}

// fileFacts são os fatos pré-computados de um arquivo (fora da transação de
// escrita): hash, mediainfo e o parse dos nomes.
type fileFacts struct {
	path      string
	folderKey string      // chave de agrupamento da obra (dir do filme, ou o próprio arquivo se solto)
	folderDir string      // diretório onde procurar a arte local
	rawNome   string      // nome base cru (título de fallback para draft)
	entidade  naming.Info // parse do NOME DA OBRA (pasta ou arquivo solto)
	arquivo   naming.Info // parse do NOME DO ARQUIVO (edição/parte)
	size      int64
	mtime     string
	hash      string
	mediainfo []byte
	poster    string // caminho da arte local; "" se nenhuma

	// Campos do caminho de SÉRIE (0034), preenchidos só quando content_type='series'.
	isSerie    bool     // marca o fluxo de série (ramifica gravarArquivo)
	serieID    string   // entity da SÉRIE (a "obra"); o media_file aponta para ela
	serieConf  float64  // confiança da resolução da série (100 por id externo, 90 por título/import)
	episodeIDs []string // episódios casados, em ordem (define o ordinal do vínculo)
	// prov guarda a numeração LIDA DO ARQUIVO quando o casamento contra a
	// árvore externa não achou correspondente: é o que permite criar o
	// episódio provisório (briefing 0133). nil quando não há o que criar.
	prov *provisorioEpisodio

	// Campos do caminho de MÚSICA (0107), preenchidos só quando content_type='music'.
	isMusica  bool    // marca o fluxo de música (ramifica gravarArquivo)
	faixaID   string  // entity FAIXA já resolvida; media_file.entity_id aponta DIRETO pra ela (1:1, ADR-0027 item 5)
	faixaConf float64 // confiança do vínculo (confIDExterno/confMusicaTextual/confDraft, ver musica.go)
}

// executarScan roda o pipeline completo de um datasource. progresso (briefing
// 0108) é o callback do execqueue chamado a cada arquivo processado -- pode
// ser nil (bypass da fila em teste, ver scanner_test.go).
func (m *Manager) executarScan(ctx context.Context, source library.Source, progresso execqueue.ProgressoFunc) error {
	r := &scanRun{
		m:          m,
		source:     source,
		scanTS:     m.agora().Format(carimboFormato),
		probeAtivo: m.prober.Disponivel(),
		cache:      make(map[string]string),
		serieCache: make(map[string]serieResolvida),
		progresso:  progresso,
	}
	if !r.probeAtivo {
		log.Printf("scanner: ffprobe indisponível; mediainfo ficará vazio no scan de %q", source.ID)
	}

	arquivos, naoVideos, parcial, err := coletarArquivos(source.Path, r.extensoesPrincipais())
	if err != nil {
		// A raiz ficou inacessível: sinaliza offline e NÃO marca nada missing.
		m.persistirEstado(ctx, source, "offline")
		return err
	}
	r.parcial = parcial
	if parcial {
		log.Printf("scanner: varredura parcial de %q (subpasta(s) ilegível(is) durante o walk); ciclo de vida NÃO marcará arquivos ausentes nesta rodada", source.ID)
	}
	r.filesTotal = len(arquivos)
	r.reportarProgresso()
	// Classificação de arquivo NÃO-vídeo (briefing 0063, extensão do 0062):
	// reaproveita a MESMA passada de WalkDir de coletarArquivos acima -- nunca
	// varre o disco de novo só para isto.
	managed, unmanaged := classificarNaoVideos(arquivos, naoVideos)
	m.setStorageNaoVideo(source.ID, managed, unmanaged)

	if err := r.processarLotes(ctx, arquivos); err != nil {
		// Cancelado/erro no meio: lotes já commitados persistem; não fechamos o
		// ciclo de vida (nada de missing) nem carimbamos last_scan.
		return err
	}

	if err := r.finalizarCicloVida(ctx); err != nil {
		return err
	}

	// Pós-scan (briefing 0135/ADR-0031): FECHADO o ciclo de vida, a própria
	// biblioteca reconcilía o que ficou em rascunho e retenta o que está
	// pendente -- sem esperar o cron diário. Best-effort: nunca derruba o scan
	// (o catálogo já está gravado e o last_scan já foi carimbado).
	r.posScan(ctx)
	return nil
}

// processarLotes pré-computa os fatos de cada arquivo (I/O e ffprobe, fora da
// transação) e grava em lotes curtos, checando o cancelamento entre lotes. O
// progresso (briefing 0108) é reportado a cada ARQUIVO processado dentro do
// loop interno (sucesso OU erro/skip -- o arquivo foi "visto" de qualquer
// forma, e filesTotal já conta ele) -- o lote de loteEscrita continua
// existindo só como granularidade de ESCRITA em banco (escreverLote), agora
// desacoplada do reporte de progresso.
func (r *scanRun) processarLotes(ctx context.Context, arquivos []string) error {
	for i := 0; i < len(arquivos); i += loteEscrita {
		if err := ctx.Err(); err != nil {
			return err
		}
		fim := i + loteEscrita
		if fim > len(arquivos) {
			fim = len(arquivos)
		}
		lote := arquivos[i:fim]

		fatos := make([]fileFacts, 0, len(lote))
		for _, p := range lote {
			f, err := r.computarFacts(ctx, p)
			r.filesSeen++
			r.reportarProgresso()
			if err != nil {
				log.Printf("scanner: ignorando %q: %v", p, err)
				continue
			}
			fatos = append(fatos, f)
		}
		if err := r.escreverLote(ctx, fatos); err != nil {
			return err
		}
	}
	return nil
}

// computarFacts calcula hash + mediainfo (ou reusa, ver hashEMediainfo) e faz
// o parse dos nomes de um arquivo.
func (r *scanRun) computarFacts(ctx context.Context, path string) (fileFacts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileFacts{}, err
	}
	mtime := info.ModTime().UTC().Format(time.RFC3339Nano)

	hash, mi, err := r.hashEMediainfo(ctx, path, info.Size(), mtime)
	if err != nil {
		return fileFacts{}, err
	}

	// Ramifica pelo tipo de biblioteca: séries seguem o novo fluxo (0034); filme
	// (ou default) mantém o caminho de agrupamento por pasta, intacto. A resolução
	// da série (e o auto-import) acontece AQUI, fora da transação de escrita — o
	// import do enrich escreve pela própria conexão, então nada de tx aninhada.
	// Isto roda SEMPRE, mesmo quando hash/mediainfo foram reusados (scan
	// incremental, briefing 0064): a resolução/vínculo de entity não depende do
	// conteúdo do arquivo, só do nome/pasta — e o re-scan de vínculos de
	// episódio precisa rodar a cada execução (ver gravarArquivoSerie).
	var f fileFacts
	switch r.source.ContentType {
	case contentTypeSeries:
		f = r.factsSerie(ctx, path)
	case contentTypeMusic:
		// Música (0107) PRECISA do mediainfo (tags de áudio) para casar --
		// diferente de série (só nome/pasta), por isso recebe `mi` (e
		// size/mtime, para o short-circuit de incrementalidade) aqui.
		f = r.factsMusica(ctx, path, mi, info.Size(), mtime)
	default:
		f = montarFacts(r.source.Path, path)
	}
	f.size = info.Size()
	f.mtime = mtime
	f.hash = hash
	f.mediainfo = mi
	return f, nil
}

// hashEMediainfo calcula o content_hash (hashDoArquivoFn, indireto só para
// instrumentação de teste) e o mediainfo_json de `path`, OU reusa os valores
// JÁ GRAVADOS quando o media_file existente para este MESMO path (UNIQUE,
// ObterMediaFilePorPath) tem EXATAMENTE o mesmo mtime e size (scan
// incremental, briefing 0064) — nesse caso nem abre o arquivo nem chama
// ffprobe de novo.
//
// Isto é uma HEURÍSTICA de melhor esforço, não uma garantia criptográfica: um
// arquivo poderia, em teoria, mudar de conteúdo sem que mtime OU size mudem
// (ex. algum editor exótico que preserva os dois, ou uma janela de
// sincronização de `rclone --vfs-cache-mode full` onde o mtime local só
// atualiza depois do upload terminar — rclone/rclone#8894). Aceito
// conscientemente (briefing 0064 §6): nunca pior que o comportamento antigo
// que sempre re-hasheava tudo, e hash de conteúdo como fallback anularia o
// ganho de performance que motivou esta mudança — não reabra esta discussão.
// Qualquer divergência (arquivo novo, ou mtime/size diferentes) cai no
// caminho normal (hash + ffprobe completos).
func (r *scanRun) hashEMediainfo(ctx context.Context, path string, size int64, mtime string) (hash string, mediainfo []byte, err error) {
	if existente, ok := r.mediaFileInalteradoPorPath(ctx, path, size, mtime); ok {
		return existente.ContentHash.String, []byte(existente.MediainfoJson.String), nil
	}
	return r.computarHashEMediainfo(ctx, path, size)
}

// computarHashEMediainfo SEMPRE calcula o content_hash e o mediainfo_json de
// `path` (SEM checar o short-circuit de mediaFileInalteradoPorPath) — extraído
// de hashEMediainfo para ser reusado também pelo recheck forçado de paths
// específicos (RecheckEntityFiles, briefing 0088, ver recheck.go), que
// precisa ignorar mtime/size DE PROPÓSITO. O caminho normal do scan
// (hashEMediainfo acima) continua chamando isto só depois que o
// short-circuit falha — nenhuma mudança de comportamento no scan incremental
// de datasource inteiro.
func (r *scanRun) computarHashEMediainfo(ctx context.Context, path string, size int64) (hash string, mediainfo []byte, err error) {
	hash, err = hashDoArquivoFn(path, size)
	if err != nil {
		return "", nil, err
	}
	if r.probeAtivo {
		if b, perr := r.m.prober.Probe(ctx, path); perr == nil {
			mediainfo = b
		} else {
			log.Printf("scanner: ffprobe falhou em %q (mediainfo vazio): %v", path, perr)
		}
	}
	return hash, mediainfo, nil
}

// mediaFileInalteradoPorPath busca o media_file já gravado para `path` (pela
// conexão de LEITURA — computarFacts roda fora da transação de escrita) e
// devolve ok=true só quando ele tem content_hash gravado E mtime E size
// batem EXATAMENTE com os valores atuais do arquivo no disco — o
// short-circuit do scan incremental (briefing 0064). Ausente (arquivo novo
// neste path), sem hash gravado (defensivo) ou qualquer divergência de
// mtime/size: ok=false, caminho normal (hash + ffprobe completos).
func (r *scanRun) mediaFileInalteradoPorPath(ctx context.Context, path string, size int64, mtime string) (sqlcgen.MediaFile, bool) {
	existente, err := sqlcgen.New(r.m.db.Read).ObterMediaFilePorPath(ctx, path)
	if err != nil {
		return sqlcgen.MediaFile{}, false
	}
	if !existente.ContentHash.Valid || existente.ContentHash.String == "" {
		return sqlcgen.MediaFile{}, false
	}
	if !existente.Size.Valid || existente.Size.Int64 != size {
		return sqlcgen.MediaFile{}, false
	}
	if !existente.Mtime.Valid || existente.Mtime.String != mtime {
		return sqlcgen.MediaFile{}, false
	}
	return existente, true
}

// montarFacts decide o agrupamento (pasta = obra; arquivo solto na raiz = obra
// própria) e faz os parses de nome.
func montarFacts(root, path string) fileFacts {
	dir := filepath.Dir(path)
	base := semExtensao(filepath.Base(path))

	f := fileFacts{path: path, arquivo: naming.Parse(base)}
	if dir != root {
		f.folderKey = dir
		f.folderDir = dir
		f.rawNome = filepath.Base(dir)
		f.entidade = naming.Parse(f.rawNome)
	} else {
		f.folderKey = path
		f.folderDir = root
		f.rawNome = base
		f.entidade = naming.Parse(base)
	}
	f.poster = descobrirPoster(f.folderDir, base)
	return f
}

// escreverLote grava um lote de arquivos numa transação curta pela conexão de
// escrita (ADR-0010). Rollback em qualquer erro; o commit torna o lote durável.
func (r *scanRun) escreverLote(ctx context.Context, fatos []fileFacts) error {
	if len(fatos) == 0 {
		return nil
	}
	tx, err := r.m.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	q := sqlcgen.New(tx)
	for _, f := range fatos {
		if err := r.gravarArquivo(ctx, q, f); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// DEPOIS do commit (briefing 0135/ADR-0031): os ganchos de enriquecimento
	// (auto-match, materialização) precisam da linha visível para o pool de
	// leitura E da conexão de escrita livre. Chamá-los dentro da transação é
	// bug -- foi a causa real do item novo sair cru em produção (08/nov/2026).
	r.despacharNovos(ctx)
	return nil
}

// finalizarCicloVida fecha o scan (fonte acessível): carimba library_source
// (online + last_scan) e, só quando a varredura foi COMPLETA (r.parcial ==
// false), marca `missing` os arquivos não-revistos desta fonte. Numa
// varredura parcial (subpasta ilegível no meio do walk, ver r.parcial), essa
// marcação é pulada de propósito -- "não vi este arquivo agora" não é prova
// de que ele sumiu do disco quando nem toda a árvore pôde ser lida.
func (r *scanRun) finalizarCicloVida(ctx context.Context) error {
	q := sqlcgen.New(r.m.db.Write)
	if !r.parcial {
		if _, err := q.MarcarMediaFilesNaoVistosMissing(ctx, sqlcgen.MarcarMediaFilesNaoVistosMissingParams{
			LibrarySourceID: nsOpt(r.source.ID),
			LastSeen:        nsOpt(r.scanTS),
		}); err != nil {
			return err
		}
	}
	_, err := q.AtualizarLibrarySource(ctx, sqlcgen.AtualizarLibrarySourceParams{
		Type:     r.source.Type,
		LastScan: nsOpt(r.m.agora().Format(time.RFC3339)),
		State:    "online",
		ID:       r.source.ID,
	})
	return err
}

// persistirEstado grava o state do datasource (usado para sinalizar offline),
// preservando o último last_scan conhecido.
func (m *Manager) persistirEstado(ctx context.Context, source library.Source, state string) {
	var last string
	if source.LastScan != nil {
		last = *source.LastScan
	}
	q := sqlcgen.New(m.db.Write)
	if _, err := q.AtualizarLibrarySource(ctx, sqlcgen.AtualizarLibrarySourceParams{
		Type:     source.Type,
		LastScan: nsOpt(last),
		State:    state,
		ID:       source.ID,
	}); err != nil {
		log.Printf("scanner: falha ao persistir estado %q de %q: %v", state, source.ID, err)
	}
}

// arquivoBruto é um arquivo NÃO-vídeo encontrado durante coletarArquivos,
// pré-computado (path + tamanho) para a classificação managed/unmanaged do
// briefing 0063 -- sem reabrir o arquivo depois.
type arquivoBruto struct {
	Path string
	Size int64
}

// extensoesPrincipais devolve o conjunto de extensões tratadas como o
// CONTEÚDO principal do datasource nesta varredura (0107): extensoesVideo
// para filme/série (default) ou extensoesAudio quando content_type='music'.
// coletarArquivos usa isto para decidir o que alimenta o pipeline de
// matching (vs. o que cai em naoVideos, só para a classificação
// managed/unmanaged de storage do briefing 0063).
func (r *scanRun) extensoesPrincipais() map[string]bool {
	if r.source.ContentType == contentTypeMusic {
		return extensoesAudio
	}
	return extensoesVideo
}

// coletarArquivos percorre a raiz UMA ÚNICA VEZ (WalkDir) e separa o
// CONTEÚDO principal (`extensoesConteudo` -- vídeo ou áudio, ver
// extensoesPrincipais) do resto (para a classificação managed/unmanaged do
// briefing 0063, ver classificarNaoVideos) -- nunca duas passadas de disco.
// Erro no stat da própria raiz é fatal (fonte offline); diretórios internos
// ilegíveis são pulados (logados), não derrubam o walk -- mas fazem
// `parcial` voltar true, para o chamador saber que a lista de arquivos
// vistos pode estar incompleta (ex.: timeout transitório de um mount de rede
// FUSE/rclone no meio da varredura) e não tratar "não visto" como "removido"
// no fechamento do ciclo de vida.
func coletarArquivos(root string, extensoesConteudo map[string]bool) (videos []string, naoVideos []arquivoBruto, parcial bool, err error) {
	if _, err := os.Stat(root); err != nil {
		return nil, nil, false, err
	}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("scanner: pulando %q: %v", path, err)
			parcial = true
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if extensoesConteudo[strings.ToLower(filepath.Ext(path))] {
			videos = append(videos, path)
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			log.Printf("scanner: nao foi possivel obter o tamanho de %q (ignorado na classificacao de armazenamento): %v", path, ierr)
			return nil
		}
		naoVideos = append(naoVideos, arquivoBruto{Path: path, Size: info.Size()})
		return nil
	})
	if walkErr != nil {
		return nil, nil, parcial, walkErr
	}
	sort.Strings(videos)
	return videos, naoVideos, parcial, nil
}

// classificarNaoVideos agrupa os vídeos por diretório (nome-base sem
// extensão, ver localmedia.NomeBaseVideo) e classifica cada arquivo
// NÃO-vídeo encontrado na MESMA varredura em managed (nome casa com um
// padrão canônico do briefing 0062, ver localmedia.EhArquivoGerenciado) ou
// unmanaged (resto) -- soma de bytes por categoria. Puro (não acessa disco):
// os tamanhos já vieram de coletarArquivos.
func classificarNaoVideos(videos []string, naoVideos []arquivoBruto) (managedBytes, unmanagedBytes int64) {
	basesPorDir := make(map[string][]string, len(videos))
	for _, v := range videos {
		dir := filepath.Dir(v)
		basesPorDir[dir] = append(basesPorDir[dir], localmedia.NomeBaseVideo(v))
	}
	for _, nv := range naoVideos {
		dir := filepath.Dir(nv.Path)
		nome := filepath.Base(nv.Path)
		if localmedia.EhArquivoGerenciado(nome, basesPorDir[dir]) {
			managedBytes += nv.Size
		} else {
			unmanagedBytes += nv.Size
		}
	}
	return managedBytes, unmanagedBytes
}

// descobrirPoster procura a arte local: poster.jpg/folder.jpg no diretório da
// obra e <nome-do-arquivo>-poster.jpg (convenção Kodi/Jellyfin). "" se nenhuma.
func descobrirPoster(dir, base string) string {
	candidatos := append([]string{}, nomesPoster...)
	candidatos = append(candidatos, base+"-poster.jpg", base+"-poster.png")
	for _, nome := range candidatos {
		p := filepath.Join(dir, nome)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// semExtensao remove a extensão de um nome de arquivo.
func semExtensao(nome string) string {
	return strings.TrimSuffix(nome, filepath.Ext(nome))
}

// hashDoArquivoFn é hashDoArquivo indireto por variável (nunca sobrescrita em
// produção): existe só para os TESTES do scan incremental (briefing 0064)
// poderem CONTAR/instrumentar quantas vezes o hash de verdade roda, sem
// precisar reescrever o pipeline — mesmo espírito de m.agora/m.novoID serem
// campos função injetáveis.
var hashDoArquivoFn = hashDoArquivo

// hashDoArquivo abre o arquivo e calcula o content_hash parcial.
func hashDoArquivo(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return contentHash(f, size)
}

// posScan chama o gancho de pós-scan (briefing 0135/ADR-0031) ao fim de
// executarScan: reconciliação dos rascunhos e retry dos pendentes DAQUELA
// biblioteca. Reporta a etapa como mensagem de progresso (o contador continua
// sendo de ARQUIVOS -- a etapa não infla files_total). Best-effort: erro só
// loga, nunca derruba um scan que já gravou e carimbou tudo.
func (r *scanRun) posScan(ctx context.Context) {
	if r.m.posScan == nil {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	r.reportarProgressoMSG("reconciliando pendentes da biblioteca")
	if err := r.m.posScan.ProcessarPosScan(ctx, r.source.ID); err != nil {
		log.Printf("scanner: pós-scan da biblioteca %q falhou (scan concluído mesmo assim): %v", r.source.ID, err)
	}
}
