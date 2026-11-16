// classificacao.go implements the "managed vs. unidentified" classification
// of the artwork convention (see doc 08): given the NAME of a non-video file
// found inside a tracked folder, it decides whether it MATCHES some canonical
// pattern that this package knows how to generate (poster.jpg, fanart2.jpg,
// tvshow.nfo, <video>.nfo, <video>-thumb.jpg, season01-poster.jpg, ...). It
// reuses the SAME tables/constants from naming.go (baseDeKind) -- it does not
// reimplement the list of kinds. Pure: it accesses neither the disk nor the
// database: the caller (internal/scanner, during the SAME walk it already
// performs to find videos) provides the video base names (without extension)
// already seen in the same folder.
package localmedia

import (
	"path/filepath"
	"strings"
)

// extensoesArteConhecidas covers the most common image formats a materialized
// artwork can have: the real extension follows the binary downloaded from the
// provider (see gravarAssetNaPastaReal in internal/server -- it preserves
// filepath.Ext(asset.PathOrUri)), not only ExtensaoPadrao (.jpg/.png). Keys in
// lowercase.
var extensoesArteConhecidas = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".gif": true, ".bmp": true, ".tbn": true,
}

// basesDeArteConhecidas are the FIXED base names of kind->file (see doc 08),
// derived from baseDeKind (naming.go) without duplicating the table --
// "fanart"/"backdrop" collapse into the same "fanart" base, so the final list
// has unique entries.
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

// EhArquivoGerenciado decides whether `nome` (without directory) matches some
// canonical pattern of this package: an artwork name (bare or prefixed by the
// video, with or without a numeric rotation suffix) or an NFO name
// (tvshow.nfo fixed, or "<video>.nfo" for some video already seen in the same
// folder). `nomesBaseVideo` are the base names (without extension, see
// NomeBaseVideo) of the videos ALREADY KNOWN in the SAME folder -- used to
// match the prefixed forms ("<video>-poster.jpg", "<video>-thumb.jpg") and the
// per-video NFO ("<video>.nfo"). The caller (internal/scanner) builds that
// list from the SAME walk that already enumerates the videos -- this package
// does not access the disk.
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

// ehSeasonPosterSemExt recognizes "season{NN}-poster" + an optional numeric
// rotation suffix (see doc 08, NomeSeasonBase+KindSeasonPoster) -- NN is
// exactly 2 digits.
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

// casaBaseComRotacao says whether `semExt` is exactly `base` (bare name) or
// `base` followed only by digits (rotation suffix, see sufixoRotacao in
// naming.go -- never "0" or negative, but here we only need to recognize the
// PATTERN, not validate the value).
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

// isDigits says whether `s` is non-empty and contains only ASCII digits.
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
