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

// ToleranciaDuracaoSegundos é a folga (em segundos) usada como CONFIRMAÇÃO
// adicional de match de faixa de música quando duas durações (arquivo local
// vs. candidato externo) são comparadas (briefing 0121 §8). O MusicBrainz
// Picard usa 2s como padrão para esse mesmo gate; aqui um pouco mais folgado
// (3s) para absorver a imprecisão de duração de CONTAINER que o ffprobe
// reporta (via mediainfo) frente à decodificação exata do áudio.
const ToleranciaDuracaoSegundos = 3.0

// Pontuar calcula o score final [0,100] do briefing 0050 §4: similaridade
// Jaro-Winkler do título (textsim) em escala 0-100, ajustada pelo ano.
// descartado=true quando os DOIS anos existem e divergem em MAIS de 1 ano
// (remake/refilmagem não deve colar, mesmo com título idêntico) — o candidato
// nunca entra no ranking nesse caso. Anos iguais somam +5 (cap em 100);
// diferença de exatamente 1 ano não ajusta (tolerância a erro comum de
// catalogação); qualquer ano ausente também não ajusta (nada para comparar).
func Pontuar(tituloConsulta string, anoConsulta int, tituloCandidato string, anoCandidato int) (score float64, descartado bool) {
	base := textsim.Similaridade(tituloConsulta, tituloCandidato) * 100
	score, descartado = ajustarPorAno(base, anoConsulta, anoCandidato)
	return score, descartado
}

// PontuarComAlternativos é a variante de Pontuar (briefing 0084) que também
// considera os TÍTULOS ALTERNATIVOS/TRADUZIDOS que o provedor conhece do
// candidato -- não só o título no idioma consultado. A similaridade textual
// usa o MAIOR score entre `tituloCandidato` e cada item de
// `titulosAlternativos`; tituloVencedor devolve QUAL string produziu esse
// score (para a UI explicar a origem do palpite — "via título alternativo").
// tituloVencedor é sempre um dos títulos comparados (nunca vazio), mesmo
// quando o vencedor foi o próprio título principal -- cabe ao CHAMADOR decidir
// se deve exibir isso (ex.: matched_title só sai preenchido quando o vencedor
// difere do título principal). A regra de ano é a MESMA de Pontuar
// (compartilhada via ajustarPorAno, sem duplicar a lógica de diff).
func PontuarComAlternativos(tituloConsulta string, anoConsulta int, tituloCandidato string, titulosAlternativos []string, anoCandidato int) (score float64, tituloVencedor string, descartado bool) {
	melhorTitulo := tituloCandidato
	melhorSim := textsim.Similaridade(tituloConsulta, tituloCandidato)
	for _, alt := range titulosAlternativos {
		if alt == "" {
			continue
		}
		if sim := textsim.Similaridade(tituloConsulta, alt); sim > melhorSim {
			melhorSim, melhorTitulo = sim, alt
		}
	}
	base := melhorSim * 100
	score, descartado = ajustarPorAno(base, anoConsulta, anoCandidato)
	return score, melhorTitulo, descartado
}

// ajustarPorAno aplica o ajuste de ano do briefing 0050 §4 a um score base de
// similaridade textual (0-100): descarta (score 0, descartado=true) quando os
// DOIS anos existem e divergem em MAIS de 1 ano; soma +5 (cap em 100) quando
// os anos batem exatamente; não ajusta em qualquer outro caso (diferença de 1
// ano, ou ano ausente de um dos lados). Extraído para ser compartilhado por
// Pontuar e PontuarComAlternativos, sem duplicar a regra em dois lugares.
func ajustarPorAno(scoreBase float64, anoConsulta, anoCandidato int) (score float64, descartado bool) {
	if anoConsulta <= 0 || anoCandidato <= 0 {
		return scoreBase, false
	}
	diff := anoConsulta - anoCandidato
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff > 1:
		return 0, true
	case diff == 0:
		scoreBase += 5
		if scoreBase > 100 {
			scoreBase = 100
		}
	}
	return scoreBase, false
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
