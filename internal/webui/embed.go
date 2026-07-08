// Package webui empacota os arquivos estáticos do SPA (build do Vite) dentro do
// binário Go via //go:embed. Assim o backend serve o frontend no mesmo origin,
// sem depender de arquivos externos em tempo de execução.
package webui

import (
	"embed"
	"io/fs"
)

// embedded contém tudo que está no diretório dist/, inclusive arquivos ocultos
// (o prefixo "all:" garante a inclusão de nomes iniciados por "." ou "_").
//
//go:embed all:dist
var embedded embed.FS

// Dist expõe o conteúdo de dist/ como um fs.FS já apontando para dentro do
// diretório, de modo que caminhos como "index.html" e "assets/..." funcionem
// diretamente, sem o prefixo "dist/".
var Dist fs.FS = mustSub(embedded, "dist")

// mustSub extrai um subdiretório do FS embutido; falha na inicialização se o
// diretório esperado não existir (erro de build/empacotamento).
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("webui: não foi possível abrir o subdiretório embutido '" + dir + "': " + err.Error())
	}
	return sub
}
