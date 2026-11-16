package server

// grupo_revisao.go annotates the GROUP of each review queue item: the same
// cause/context of the file, so the screen shows ~50 sections instead of 200 loose cards.
// The key comes from `media_file.path` -- and not from the tags -- because 21 of the 185
// real tracks measured have no tag at all, and the path is what the owner recognizes in
// the folder.
//
// Nothing here changes the PREDICATE of the queue (who enters review is still defined by
// the matching flow): it is a presentation annotation, in the same mold as
// `ownership`/`numbering` on the entity card.

import (
	"context"
	"path"
	"strings"
)

// Review group kinds (they mirror the ReviewGroup.kind enum of openapi.yaml).
const (
	grupoRevisaoAlbum      = "album"
	grupoRevisaoSerie      = "serie"
	grupoRevisaoPasta      = "pasta"
	grupoRevisaoSemArquivo = "sem_arquivo"
)

// reviewGroupDTO is the group annotated on each item of GET /api/v1/review.
type reviewGroupDTO struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Label string `json:"label"`
	Size  int64  `json:"size"`
}

// segmentosDeDisco are the subfolder names that separate a DISC inside the album folder --
// grouping by them would fragment the same album into N groups ("Disc 1", "Disc 2").
// Compared against the normalized segment (lowercase, no space).
var segmentosDeDisco = map[string]bool{
	"disc": true, "disk": true, "cd": true, "vol": true, "volume": true,
}

// segmentosDeTemporada are the subfolder names that separate a SEASON inside the series
// folder -- the same reasoning as the disc: the episodes of a series belong to ONE group,
// not one per season.
var segmentosDeTemporada = map[string]bool{
	"season": true, "temporada": true, "series": true, "serie": true,
}

// anotarGruposRevisao fills ReviewGroup in `items` (in the SAME order as `caminhos`) from
// the file path of each item, made relative to the library root that contains it
// (`raizes` = paths of the library_source). The `size` is the number of items in the
// group.
func anotarGruposRevisao(items []entityCardDTO, caminhos []string, raizes []string) {
	tamanhos := make(map[string]int64, len(items))
	grupos := make([]*reviewGroupDTO, len(items))
	for i, item := range items {
		g := grupoDeRevisao(item.Type, caminhos[i], raizes)
		grupos[i] = g
		tamanhos[g.Key]++
	}
	for i := range grupos {
		grupos[i].Size = tamanhos[grupos[i].Key]
		items[i].ReviewGroup = grupos[i]
	}
}

// grupoDeRevisao returns the group of ONE item: kind+key+label from the type and the file
// path. An item without a file falls into a single "Sem arquivo" group.
func grupoDeRevisao(tipo, caminho string, raizes []string) *reviewGroupDTO {
	if caminho == "" {
		return &reviewGroupDTO{Kind: grupoRevisaoSemArquivo, Key: grupoRevisaoSemArquivo, Label: "Sem arquivo"}
	}
	rel := relativizar(caminho, raizes)
	dir := path.Dir(rel)

	switch tipo {
	case tipoEntFaixa:
		dirSemDisco := semSegmento(dir, segmentosDeDisco)
		return &reviewGroupDTO{
			Kind:  grupoRevisaoAlbum,
			Key:   grupoRevisaoAlbum + ":" + dirSemDisco,
			Label: rotuloDe(dirSemDisco, 2),
		}
	case tipoEntEpisodio:
		dirSerie := semSegmento(dir, segmentosDeTemporada)
		return &reviewGroupDTO{
			Kind:  grupoRevisaoSerie,
			Key:   grupoRevisaoSerie + ":" + dirSerie,
			Label: rotuloDe(dirSerie, 1),
		}
	default:
		return &reviewGroupDTO{
			Kind:  grupoRevisaoPasta,
			Key:   grupoRevisaoPasta + ":" + dir,
			Label: rotuloDe(dir, 1),
		}
	}
}

// relativizar strips from `caminho` the longest library root that prefixes it -- the label
// stays short and independent of where the mount is mounted. Without a matching root it
// returns the path as it came.
func relativizar(caminho string, raizes []string) string {
	melhor := ""
	for _, raiz := range raizes {
		if raiz == "" {
			continue
		}
		if caminho == raiz || strings.HasPrefix(caminho, raiz+"/") {
			if len(raiz) > len(melhor) {
				melhor = raiz
			}
		}
	}
	if melhor == "" {
		return caminho
	}
	return strings.TrimPrefix(strings.TrimPrefix(caminho, melhor), "/")
}

// semSegmento removes from the END of `dir` the segments that are "disc"/"season"
// ones (with or without a number: "Disc 1", "CD2", "Season 3", "S03"). It repeats
// while the last segment matches, to cover "Disc 1/CD 2" of weird editions.
func semSegmento(dir string, vocabulario map[string]bool) string {
	atual := dir
	for {
		base := path.Base(atual)
		if base == "." || base == "/" || base == "" || !ehSegmentoNumerado(base, vocabulario) {
			return atual
		}
		pai := path.Dir(atual)
		if pai == atual {
			return atual
		}
		atual = pai
	}
}

// ehSegmentoNumerado tells whether the segment is <vocabulary word> + a mandatory NUMBER
// ("Disc 1", "CD2", "Season 03", "Temporada 2"), or the short pattern "S03". It
// never matches the name of a work ("Disco Voador", "Season of the Witch") nor a
// LIBRARY FOLDER called "Series"/"Músicas": the number is required on purpose.
func ehSegmentoNumerado(segmento string, vocabulario map[string]bool) bool {
	limpo := strings.ToLower(strings.TrimSpace(segmento))
	if limpo == "" {
		return false
	}
	// "S03"/"T02": one letter + digits.
	if len(limpo) > 1 && (limpo[0] == 's' || limpo[0] == 't') {
		if _, ok := numeroDoSegmento(limpo[1:]); ok {
			return true
		}
	}
	palavras := strings.FieldsFunc(limpo, func(r rune) bool { return r == ' ' || r == '_' || r == '-' })
	if len(palavras) == 0 || len(palavras) > 2 {
		return false
	}
	if !vocabulario[palavras[0]] || len(palavras) != 2 {
		return false
	}
	_, ok := numeroDoSegmento(palavras[1])
	return ok
}

// numeroDoSegmento returns the number of a digits-only segment ("1", "02").
func numeroDoSegmento(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// rotuloDe builds the readable label from the last `n` segments of the path, joined by
// " — " (e.g. "Duran Duran — Notorious (Deluxe Edition)"). It always returns something
// non-empty (it falls back to the whole path).
func rotuloDe(dir string, n int) string {
	partes := strings.Split(strings.Trim(dir, "/"), "/")
	uteis := make([]string, 0, n)
	for i := len(partes) - 1; i >= 0 && len(uteis) < n; i-- {
		if partes[i] == "" || partes[i] == "." {
			continue
		}
		uteis = append(uteis, partes[i])
	}
	if len(uteis) == 0 {
		return dir
	}
	// reverses to the path order (artist before album)
	for i, j := 0, len(uteis)-1; i < j; i, j = i+1, j-1 {
		uteis[i], uteis[j] = uteis[j], uteis[i]
	}
	return strings.Join(uteis, " — ")
}

// raizesDeBiblioteca returns the paths of the registered library_source -- only to make
// the group label relative (short path, independent of the mount). A database error
// degrades to an empty list (label with the absolute path): the grouping keeps working,
// just more verbose.
func (s *Server) raizesDeBiblioteca(ctx context.Context) []string {
	if s.library == nil {
		return nil
	}
	fontes, err := s.library.Listar(ctx)
	if err != nil {
		return nil
	}
	raizes := make([]string, 0, len(fontes))
	for _, fonte := range fontes {
		if fonte.Path != "" {
			raizes = append(raizes, strings.TrimSuffix(fonte.Path, "/"))
		}
	}
	return raizes
}
