// Package matching concentra a PONTUAÇÃO de candidato do auto-match (briefing
// 0050): dado um título/ano de consulta e um título/ano de candidato, decide o
// score final (0-100) e se o candidato deve ser descartado por divergência de
// ano. É um pacote FOLHA (só depende de internal/textsim) para poder ser
// consumido tanto por internal/server (fila de Revisão, endpoints de
// auto-match) quanto por internal/enrich (casamento por nome no import manual,
// briefing 0054) sem criar ciclo de import entre os dois.
package matching

import (
	"strconv"

	"github.com/rafaelvieiras/my-media-db/internal/textsim"
)

// LimiarAutoMatch é o corte de score (escala 0-100, igual à de
// match_confidence) acima do qual o auto-match aplica o candidato SOZINHO.
// Deliberadamente bem acima do limiar de baixa confiança da fila de Revisão
// (70, internal/server/review.go): o custo de um falso-positivo (contamina o
// catálogo em silêncio) é muito maior que o de um falso-negativo (item que já
// estava visível na fila/órfãos continua lá).
const LimiarAutoMatch = 92.0

// Pontuar calcula o score final [0,100] do briefing 0050 §4: similaridade
// Jaro-Winkler do título (textsim) em escala 0-100, ajustada pelo ano.
// descartado=true quando os DOIS anos existem e divergem em MAIS de 1 ano
// (remake/refilmagem não deve colar, mesmo com título idêntico) — o candidato
// nunca entra no ranking nesse caso. Anos iguais somam +5 (cap em 100);
// diferença de exatamente 1 ano não ajusta (tolerância a erro comum de
// catalogação); qualquer ano ausente também não ajusta (nada para comparar).
func Pontuar(tituloConsulta string, anoConsulta int, tituloCandidato string, anoCandidato int) (score float64, descartado bool) {
	score = textsim.Similaridade(tituloConsulta, tituloCandidato) * 100
	if anoConsulta > 0 && anoCandidato > 0 {
		diff := anoConsulta - anoCandidato
		if diff < 0 {
			diff = -diff
		}
		switch {
		case diff > 1:
			return 0, true
		case diff == 0:
			score += 5
			if score > 100 {
				score = 100
			}
		}
	}
	return score, false
}

// AnoDeReleaseDate extrai o ano dos 4 primeiros dígitos de um release_date
// "YYYY-MM-DD" (ou "YYYY"); 0 quando ausente/curto/inválido. Compartilhado
// entre internal/server (lê da coluna já gravada) e internal/enrich (lê do
// mapa canônico do detalhe, antes da entity existir).
func AnoDeReleaseDate(rd string) int {
	if len(rd) < 4 {
		return 0
	}
	ano, err := strconv.Atoi(rd[:4])
	if err != nil {
		return 0
	}
	return ano
}
