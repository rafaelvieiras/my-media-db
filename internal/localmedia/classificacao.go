// classificacao.go implementa a classificação "gerenciado vs. não
// identificado" do briefing 0063 (extensão do 0062): dado o NOME de um
// arquivo não-vídeo encontrado dentro de uma pasta rastreada, decide se ele
// CASA com algum padrão canônico que este pacote sabe gerar (poster.jpg,
// fanart2.jpg, tvshow.nfo, <vídeo>.nfo, <vídeo>-thumb.jpg,
// season01-poster.jpg, ...). Reusa as MESMAS tabelas/constantes de
// naming.go (baseDeKind) -- não reimplementa a lista de kinds. Puro: não
// acessa disco nem banco: o chamador (internal/scanner, durante a MESMA
// varredura que já faz para achar vídeo) informa os nomes-base de vídeo
// (sem extensão) já vistos na mesma pasta.
package localmedia

import (
	"path/filepath"
	"strings"
)

// extensoesArteConhecidas cobre os formatos de imagem mais comuns que uma
// arte materializada pode ter: a extensão real acompanha o binário baixado
// do provider (ver gravarAssetNaPastaReal em internal/server -- preserva
// filepath.Ext(asset.PathOrUri)), não só ExtensaoPadrao (.jpg/.png). Chaves
// em minúsculas.
var extensoesArteConhecidas = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".gif": true, ".bmp": true, ".tbn": true,
}

// basesDeArteConhecidas são os nomes-base FIXOS de kind->arquivo (briefing
// 0062 §4), derivados de baseDeKind (naming.go) sem duplicar a tabela --
// "fanart"/"backdrop" colapsam na mesma base "fanart", então a lista final
// tem entradas únicas.
var basesDeArteConhecidas = basesUnicas(baseDeKind)

func basesUnicas(m map[string]string) []string {
	visto := make(map[string]bool, len(m))
	var out []string
	for _, base := range m {
		if !visto[base] {
			visto[base] = true
			out = append(out, base)
		}
	}
	return out
}

// EhArquivoGerenciado decide se `nome` (sem diretório) casa com algum padrão
// canônico deste pacote: nome de arte (bare ou prefixado por vídeo, com ou
// sem sufixo de rotação numérica) ou nome de NFO (tvshow.nfo fixo, ou
// "<vídeo>.nfo" para algum vídeo já visto na mesma pasta). `nomesBaseVideo`
// são os nomes-base (sem extensão, ver NomeBaseVideo) dos vídeos JÁ
// CONHECIDOS na MESMA pasta -- usados para casar as formas prefixadas
// ("<vídeo>-poster.jpg", "<vídeo>-thumb.jpg") e o NFO por vídeo
// ("<vídeo>.nfo"). O chamador (internal/scanner) monta essa lista a partir
// da MESMA varredura que já enumera os vídeos -- este pacote não acessa
// disco.
func EhArquivoGerenciado(nome string, nomesBaseVideo []string) bool {
	if nome == "tvshow.nfo" {
		return true
	}

	ext := strings.ToLower(filepath.Ext(nome))
	semExt := strings.TrimSuffix(nome, filepath.Ext(nome))

	if ext == ".nfo" {
		for _, vb := range nomesBaseVideo {
			if semExt == vb {
				return true
			}
		}
		return false
	}

	if !extensoesArteConhecidas[ext] {
		return false
	}

	if ehSeasonPosterSemExt(semExt) {
		return true
	}

	for _, base := range basesDeArteConhecidas {
		if casaBaseComRotacao(semExt, base) {
			return true
		}
		for _, vb := range nomesBaseVideo {
			if casaBaseComRotacao(semExt, vb+"-"+base) {
				return true
			}
		}
	}
	for _, vb := range nomesBaseVideo {
		if casaBaseComRotacao(semExt, vb+"-thumb") {
			return true
		}
	}
	return false
}

// ehSeasonPosterSemExt reconhece "season{NN}-poster" + sufixo de rotação
// numérico opcional (briefing 0062 §4, NomeSeasonBase+KindSeasonPoster) --
// NN são exatamente 2 dígitos.
func ehSeasonPosterSemExt(semExt string) bool {
	const prefixo = "season"
	const meio = "-poster"
	if !strings.HasPrefix(semExt, prefixo) {
		return false
	}
	resto := semExt[len(prefixo):]
	if len(resto) < 2 || !isDigits(resto[:2]) {
		return false
	}
	resto = resto[2:]
	if !strings.HasPrefix(resto, meio) {
		return false
	}
	sufixo := resto[len(meio):]
	return sufixo == "" || isDigits(sufixo)
}

// casaBaseComRotacao diz se `semExt` é exatamente `base` (nome bare) ou
// `base` seguido só de dígitos (sufixo de rotação, ver sufixoRotacao em
// naming.go -- nunca "0" ou negativo, mas aqui só precisamos reconhecer o
// PADRÃO, não validar o valor).
func casaBaseComRotacao(semExt, base string) bool {
	if semExt == base {
		return true
	}
	if !strings.HasPrefix(semExt, base) {
		return false
	}
	resto := semExt[len(base):]
	return resto != "" && isDigits(resto)
}

// isDigits diz se `s` é não-vazio e só contém dígitos ASCII.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
