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
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
)

// extensoesVideo é o filtro de extensões (ponto de partida, doc 06). Minúsculas.
var extensoesVideo = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true,
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
}

// executarScan roda o pipeline completo de um datasource.
func (m *Manager) executarScan(ctx context.Context, source library.Source, j *job) error {
	r := &scanRun{
		m:          m,
		source:     source,
		scanTS:     m.agora().Format(carimboFormato),
		probeAtivo: m.prober.Disponivel(),
		cache:      make(map[string]string),
		serieCache: make(map[string]serieResolvida),
	}
	if !r.probeAtivo {
		log.Printf("scanner: ffprobe indisponível; mediainfo ficará vazio no scan de %q", source.ID)
	}

	arquivos, err := coletarVideos(source.Path)
	if err != nil {
		// A raiz ficou inacessível: sinaliza offline e NÃO marca nada missing.
		m.persistirEstado(ctx, source, "offline")
		return err
	}
	m.setTotal(j, len(arquivos))

	if err := r.processarLotes(ctx, j, arquivos); err != nil {
		// Cancelado/erro no meio: lotes já commitados persistem; não fechamos o
		// ciclo de vida (nada de missing) nem carimbamos last_scan.
		return err
	}

	return r.finalizarCicloVida(ctx)
}

// processarLotes pré-computa os fatos de cada arquivo (I/O e ffprobe, fora da
// transação) e grava em lotes curtos, checando o cancelamento entre lotes.
func (r *scanRun) processarLotes(ctx context.Context, j *job, arquivos []string) error {
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
			if err != nil {
				log.Printf("scanner: ignorando %q: %v", p, err)
				continue
			}
			fatos = append(fatos, f)
		}
		if err := r.escreverLote(ctx, fatos); err != nil {
			return err
		}
		r.m.addSeen(j, len(lote))
	}
	return nil
}

// computarFacts calcula hash + mediainfo e faz o parse dos nomes de um arquivo.
func (r *scanRun) computarFacts(ctx context.Context, path string) (fileFacts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileFacts{}, err
	}
	hash, err := hashDoArquivo(path, info.Size())
	if err != nil {
		return fileFacts{}, err
	}

	var mi []byte
	if r.probeAtivo {
		if b, perr := r.m.prober.Probe(ctx, path); perr == nil {
			mi = b
		} else {
			log.Printf("scanner: ffprobe falhou em %q (mediainfo vazio): %v", path, perr)
		}
	}

	// Ramifica pelo tipo de biblioteca: séries seguem o novo fluxo (0034); filme
	// (ou default) mantém o caminho de agrupamento por pasta, intacto. A resolução
	// da série (e o auto-import) acontece AQUI, fora da transação de escrita — o
	// import do enrich escreve pela própria conexão, então nada de tx aninhada.
	var f fileFacts
	if r.source.ContentType == contentTypeSeries {
		f = r.factsSerie(ctx, path)
	} else {
		f = montarFacts(r.source.Path, path)
	}
	f.size = info.Size()
	f.mtime = info.ModTime().UTC().Format(time.RFC3339Nano)
	f.hash = hash
	f.mediainfo = mi
	return f, nil
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
	return tx.Commit()
}

// finalizarCicloVida fecha o scan completo (fonte acessível): marca `missing` os
// arquivos não-revistos desta fonte e carimba library_source (online + last_scan).
func (r *scanRun) finalizarCicloVida(ctx context.Context) error {
	q := sqlcgen.New(r.m.db.Write)
	if _, err := q.MarcarMediaFilesNaoVistosMissing(ctx, sqlcgen.MarcarMediaFilesNaoVistosMissingParams{
		LibrarySourceID: nsOpt(r.source.ID),
		LastSeen:        nsOpt(r.scanTS),
	}); err != nil {
		return err
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

// coletarVideos percorre a raiz e devolve os caminhos de arquivos de vídeo, em
// ordem estável. Erro no stat da própria raiz é fatal (fonte offline); diretórios
// internos ilegíveis são pulados (logados), não derrubam o walk.
func coletarVideos(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("scanner: pulando %q: %v", path, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if extensoesVideo[strings.ToLower(filepath.Ext(path))] {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
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

// hashDoArquivo abre o arquivo e calcula o content_hash parcial.
func hashDoArquivo(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return contentHash(f, size)
}
