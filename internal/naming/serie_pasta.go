// Pasta de série: a regra de "subir um nível quando a pasta é de temporada"
// vivia só em internal/scanner (derivarSerie) e passou a ser necessária também
// em internal/server, que precisa do NOME DA SÉRIE para montar a consulta do
// auto-match (briefing 0129). Fica aqui, no pacote folha de parsing de nome,
// para ter uma dona só — o scanner e o server usam a MESMA regra, nunca duas
// cópias que podem divergir.
package naming

import (
	"path/filepath"
	"regexp"
	"strings"
)

// reSeasonFolder reconhece a pasta de temporada (Kodi/Jellyfin/Plex):
// "Season 01", "Temporada 1", "S01". Usada só para SUBIR à pasta da série; a
// temporada real do episódio vem do nome do arquivo (ParseEpisode), não daqui.
var reSeasonFolder = regexp.MustCompile(`(?i)^(season|temporada|s)[ ._-]*\d{1,4}$`)

// EhPastaTemporada diz se o nome de pasta é uma pasta de temporada (para subir
// à pasta da série). Inclui os specials (temporada 0).
func EhPastaTemporada(nome string) bool {
	l := strings.ToLower(strings.TrimSpace(nome))
	if l == "specials" || l == "especiais" {
		return true
	}
	return reSeasonFolder.MatchString(nome)
}

// PastaDaSerie devolve a pasta da série de um arquivo: a pasta imediata ou, se
// ela for pasta de temporada, a pasta ACIMA. Sem noção de raiz da fonte (quem
// chama decide o que fazer quando o resultado for a própria raiz); usada pelo
// auto-match para derivar título/ano da série (briefing 0129).
func PastaDaSerie(path string) string {
	dir := filepath.Dir(path)
	if !EhPastaTemporada(filepath.Base(dir)) {
		return dir
	}
	if parent := filepath.Dir(dir); parent != "." && parent != string(filepath.Separator) {
		return parent
	}
	return dir
}
