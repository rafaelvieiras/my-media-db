// Navegador de pastas do filesystem que o processo da API enxerga (briefing
// 0071): usado pelo formulário de cadastro/edição de datasource para escolher
// um path clicando em vez de digitar de cor. É só leitura de filesystem — não
// toca no banco.
package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxEntradasBrowse é o teto de segurança de entradas devolvidas por Browse
// (evita travar o front numa pasta patológica, ex. milhares de arquivos numa
// pasta plana montada por engano como raiz).
const maxEntradasBrowse = 2000

// BrowseEntry é um subdiretório listado pelo navegador de pastas.
type BrowseEntry struct {
	Name     string
	Path     string
	Readable bool
}

// BrowseResult é a resposta de Browse: conteúdo de um diretório do
// filesystem, usado pelo navegador de pastas do formulário de cadastro/edição
// de datasource.
type BrowseResult struct {
	Path string
	// Parent é o diretório um nível acima de Path; nil quando Path já é a
	// raiz do filesystem ("/").
	Parent    *string
	Entries   []BrowseEntry
	Truncated bool
}

// Browse lista os subdiretórios de rawPath (ou "/" se vazio). Só diretórios
// aparecem (arquivos soltos são omitidos); symlinks são seguidos (os.Stat,
// não os.Lstat) para decidir se contam como pasta, já que mounts via symlink
// são comuns. Cada subdiretório tenta ser aberto para listagem só para
// decidir Readable — uma entrada sem permissão aparece com Readable:false SEM
// abortar o restante da listagem. Erros de domínio (ErrPathInexistente/
// ErrNaoDiretorio/ErrPathIlegivel) valem para o PRÓPRIO rawPath pedido, igual
// a Criar/AtualizarPath.
func (s *Service) Browse(ctx context.Context, rawPath string) (BrowseResult, error) {
	return browse(ctx, rawPath, maxEntradasBrowse)
}

// browse é o núcleo não-exportado de Browse, com o limite de corte
// injetável — permite que o teste exercite o caminho de truncamento sem
// precisar criar milhares de diretórios reais.
func browse(_ context.Context, rawPath string, limite int) (BrowseResult, error) {
	if rawPath == "" {
		rawPath = "/"
	}

	abs, err := normalizarPath(rawPath)
	if err != nil {
		return BrowseResult{}, ErrPathInexistente
	}
	if err := validarPath(abs); err != nil {
		return BrowseResult{}, err
	}

	dirEntries, err := os.ReadDir(abs)
	if err != nil {
		// validarPath já confirmou que abs é legível; um erro aqui seria uma
		// condição de corrida (removido/permissão mudou entre as duas
		// chamadas). Trata como ilegível, mesma família de erro de domínio.
		return BrowseResult{}, ErrPathIlegivel
	}

	entries := make([]BrowseEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		full := filepath.Join(abs, de.Name())
		info, statErr := os.Stat(full) // segue symlink de propósito (mounts via symlink são comuns).
		if statErr != nil || !info.IsDir() {
			continue // ignora silenciosamente: arquivo comum, symlink quebrado, etc.
		}
		entries = append(entries, BrowseEntry{
			Name:     de.Name(),
			Path:     full,
			Readable: subdirLegivel(full),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	truncated := false
	if len(entries) > limite {
		entries = entries[:limite]
		truncated = true
	}

	var parent *string
	if dir := filepath.Dir(abs); dir != abs {
		parent = &dir
	}

	return BrowseResult{
		Path:      abs,
		Parent:    parent,
		Entries:   entries,
		Truncated: truncated,
	}, nil
}

// subdirLegivel decide o campo Readable de uma entrada: tenta abrir +
// ReadDir(1), mesma checagem de validarPath, mas SEM propagar erro — só
// marca false silenciosamente (nunca aborta a listagem inteira por causa de
// uma entrada sem permissão).
func subdirLegivel(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if _, err := f.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	return true
}
