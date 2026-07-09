package library

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// normalizarPath transforma um caminho informado pelo usuário em sua forma
// absoluta e limpa (filepath.Abs já aplica filepath.Clean). Guardar sempre a
// forma normalizada é o que faz o UNIQUE de library_source (briefing 0007)
// casar: dois caminhos como "/a/b" e "/a/b/../b" viram a MESMA string e a
// duplicata não passa mascarada.
func normalizarPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path vazio")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// validarPath valida que o caminho JÁ normalizado existe, é um diretório e é
// legível. Devolve o erro de domínio correspondente (mapeado para 422 no
// handler). A checagem de legibilidade tenta abrir o diretório e ler uma
// entrada: um diretório com permissão 0000 falha já no os.Open (precisa de
// leitura para listar), o que cobre o caso "sem permissão".
func validarPath(abs string) error {
	info, err := os.Stat(abs)
	if err != nil {
		// Não existe (ou erro de acesso ao próprio stat): tratamos como
		// caminho inválido/inexistente.
		if os.IsPermission(err) {
			return ErrPathIlegivel
		}
		return ErrPathInexistente
	}
	if !info.IsDir() {
		return ErrNaoDiretorio
	}
	f, err := os.Open(abs)
	if err != nil {
		return ErrPathIlegivel
	}
	defer func() { _ = f.Close() }()
	// ReadDir(1) devolve io.EOF para um diretório legível porém vazio; isso
	// NÃO é falha. Qualquer outro erro (ex.: permissão negada) sim.
	if _, err := f.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
		return ErrPathIlegivel
	}
	return nil
}

// avaliarState deriva o estado online/offline de uma fonte por um stat barato
// do path, SEM persistir (decisão 0008 sec.6: persistir offline é do 0009).
// online = existe + é diretório + legível; qualquer falha => offline.
func avaliarState(abs string) string {
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return stateOffline
	}
	f, err := os.Open(abs)
	if err != nil {
		return stateOffline
	}
	_ = f.Close()
	return stateOnline
}
