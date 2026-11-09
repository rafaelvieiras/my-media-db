package server

// grupo_revisao.go anota o GRUPO de cada item da fila de revisão (briefing 0137):
// a mesma causa/contexto do arquivo, para a tela mostrar ~50 seções em vez de
// 200 cards soltos. A chave sai do `media_file.path` -- e não das tags -- porque
// 21 das 185 faixas reais medidas não têm tag nenhuma, e o caminho é o que o dono
// reconhece na pasta.
//
// Nada aqui muda o PREDICADO da fila (quem entra em revisão continua sendo o do
// briefing 0018): é anotação de apresentação, no mesmo molde de
// `ownership`/`numbering` no card de entities.

import (
	"context"
	"path"
	"strings"
)

// Kinds de grupo de revisão (espelham o enum ReviewGroup.kind do openapi.yaml).
const (
	grupoRevisaoAlbum      = "album"
	grupoRevisaoSerie      = "serie"
	grupoRevisaoPasta      = "pasta"
	grupoRevisaoSemArquivo = "sem_arquivo"
)

// reviewGroupDTO é o grupo anotado em cada item de GET /api/v1/review.
type reviewGroupDTO struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Label string `json:"label"`
	Size  int64  `json:"size"`
}

// segmentosDeDisco são os nomes de subpasta que separam DISCO dentro da pasta do
// álbum -- agrupar por eles fragmentaria o mesmo álbum em N grupos ("Disc 1",
// "Disc 2"). Comparados com o segmento normalizado (minúsculo, sem espaço).
var segmentosDeDisco = map[string]bool{
	"disc": true, "disk": true, "cd": true, "vol": true, "volume": true,
}

// segmentosDeTemporada são os nomes de subpasta que separam TEMPORADA dentro da
// pasta da série -- mesmo raciocínio do disco: os episódios de uma série
// pertencem a UM grupo, não um por temporada.
var segmentosDeTemporada = map[string]bool{
	"season": true, "temporada": true, "series": true, "serie": true,
}

// anotarGruposRevisao preenche ReviewGroup em `items` (na MESMA ordem de
// `caminhos`) a partir do caminho de arquivo de cada item, relativizado contra a
// raiz da biblioteca que o contém (`raizes` = caminhos das library_source). O
// `size` é o número de itens no grupo.
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

// grupoDeRevisao devolve o grupo de UM item: kind+chave+rótulo a partir do tipo e
// do caminho do arquivo. item sem arquivo cai num grupo único "Sem arquivo".
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

// relativizar tira de `caminho` a raiz de biblioteca mais longa que o prefixa --
// o rótulo fica curto e independente de onde o mount está montado. Sem raiz
// correspondente devolve o caminho como veio.
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

// semSegmento remove do FIM de `dir` os segmentos que são "de disco"/"de
// temporada" (com ou sem número: "Disc 1", "CD2", "Season 3", "S03"). Repete
// enquanto o último segmento casar, para cobrir "Disc 1/CD 2" de edições
// esquisitas.
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

// ehSegmentoNumerado diz se o segmento é <palavra do vocabulário> + NÚMERO
// obrigatório ("Disc 1", "CD2", "Season 03", "Temporada 2"), ou o padrão curto
// "S03". Nunca casa com o nome de uma obra ("Disco Voador", "Season of the
// Witch") nem com uma PASTA DE BIBLIOTECA chamada "Series"/"Músicas": o número
// é exigido de propósito.
func ehSegmentoNumerado(segmento string, vocabulario map[string]bool) bool {
	limpo := strings.ToLower(strings.TrimSpace(segmento))
	if limpo == "" {
		return false
	}
	// "S03"/"T02": uma letra + dígitos.
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

// numeroDoSegmento devolve o número de um segmento só-dígitos ("1", "02").
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

// rotuloDe monta o rótulo legível a partir dos últimos `n` segmentos do
// caminho, unidos por " — " (ex.: "Duran Duran — Notorious (Deluxe Edition)").
// Sempre devolve algo não vazio (cai no caminho inteiro).
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
	// inverte para a ordem do caminho (artista antes do álbum)
	for i, j := 0, len(uteis)-1; i < j; i, j = i+1, j-1 {
		uteis[i], uteis[j] = uteis[j], uteis[i]
	}
	return strings.Join(uteis, " — ")
}

// raizesDeBiblioteca devolve os caminhos das library_source cadastradas -- só
// para relativizar o rótulo do grupo (caminho curto, independente do mount).
// Erro de banco degrada para lista vazia (rótulo com o caminho absoluto): o
// agrupamento continua funcionando, só mais verboso.
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
