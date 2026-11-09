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
	"strings"
	"unicode"

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

// LimiarConfiancaFingerprint é o corte mínimo (escala 0-1, a MESMA do escore
// que o AcoustID devolve) para aceitar um RecordingCandidato do passo 4.5 de
// fingerprint acústico (briefing 0126) como match válido. O AcoustID já
// filtra internamente por similaridade de duração antes de devolver um
// candidato, então o escore aqui reflete majoritariamente a qualidade do
// próprio fingerprint (ruído/qualidade de captura, não ambiguidade de
// metadado) -- por isso um corte mais permissivo que LimiarAutoMatch (que
// mede similaridade de TEXTO, sujeita a nomes parecidos de itens
// diferentes) é aceitável. 0.6 é o meio do intervalo "bom o bastante"
// documentado pela própria AcoustID/Picard (candidatos < 0.5 costumam ser
// ruído; >= 0.8 é alta confiança) -- valor calibrável depois com dados reais
// de produção, sem mudar o contrato desta constante.
const LimiarConfiancaFingerprint = 0.6

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

// ── Normalização de nome de OBRA (briefing 0136) ─────────────────────────────

// tokensEdicao são os qualificadores que um TAGUEADOR grava no nome do álbum/
// faixa e que o MusicBrainz normalmente NÃO carrega no título canônico do
// release-group/faixa: "(Deluxe Edition)", "(Bonus Edition)", "(Remastered)",
// "(Expanded)", "(Collector's Edition)", " - Remastered 2011", "[Explicit]".
// Comparados contra o conteúdo de um segmento entre parênteses/colchetes (ou um
// sufixo " - ..."), em minúsculas e já reduzido a palavras.
var tokensEdicao = map[string]bool{
	"deluxe": true, "bonus": true, "expanded": true, "remaster": true,
	"remastered": true, "remasterizado": true, "remasterizada": true,
	"collector": true, "collectors": true, "special": true, "anniversary": true,
	"edition": true, "edicao": true, "version": true, "versao": true,
	"explicit": true, "ep": true, "single": true, "international": true,
	"reissue": true, "reedicao": true, "legacy": true, "definitive": true,
	"super": true, "digital": true, "remixes": true, "remix": true,
	"soundtrack": true, "ost": true, "disc": true, "cd": true,
	"album": true, "original": true,
}

// NormalizarNome remove de `s` os segmentos de EDIÇÃO/VERSÃO -- entre
// parênteses, colchetes ou após um " - " final -- cujo conteúdo é só palavra(s)
// de tokensEdicao (ou um ano isolado). É a preparação para comparar nome de tag
// com título canônico da fonte, e NUNCA devolve vazio: se a limpeza zerar a
// string, devolve o original (nunca pior que comparar cru).
//
// Exemplos: "Hybrid Theory (Bonus Edition)" → "Hybrid Theory";
// "Rio (Collector's Edition)" → "Rio"; "The Singles (Expanded)" → "The Singles";
// "Notorious (1984)" → "Notorious"; "Alright (Remastered)" → "Alright".
// NÃO toca em qualificadores que mudam a OBRA (ex.: "(Ao Vivo)", "(Live)",
// "(Acoustic)"): só edição/versão entram na lista.
func NormalizarNome(s string) string {
	if s == "" {
		return s
	}
	limpo := removerSegmentosDeEdicao(s)
	limpo = strings.TrimSpace(limpo)
	if apenasSeparadores(limpo) {
		return s
	}
	return limpo
}

// apenasSeparadores diz se a string não tem nenhuma letra/dígito (só espaço,
// hífen, dois-pontos...) -- nesse caso a normalização não trouxe valor.
func apenasSeparadores(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// removerSegmentosDeEdicao é o corpo de NormalizarNome: percorre a string uma
// vez, descartando os grupos (...) [...] {...} cujo CONTEÚDO seja só de
// edição/ano, e depois o sufixo " - <edição>" do fim (repetidamente, para
// " - Remastered 2011 - Deluxe").
func removerSegmentosDeEdicao(s string) string {
	var b strings.Builder
	profundidade := 0
	var segmento strings.Builder
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			if profundidade == 0 {
				segmento.Reset()
			}
			profundidade++
			continue
		case ')', ']', '}':
			if profundidade > 0 {
				profundidade--
				if profundidade == 0 {
					if ehSegmentoDeEdicao(segmento.String()) {
						// descarta o grupo inteiro; o espaço em volta é
						// colapsado no fim
						continue
					}
					abre := ' '
					switch r {
					case ')':
						abre = '('
					case ']':
						abre = '['
					case '}':
						abre = '{'
					}
					b.WriteRune(abre)
					b.WriteString(segmento.String())
				}
				if profundidade > 0 {
					segmento.WriteRune(r)
					continue
				}
				b.WriteRune(r)
				continue
			}
		}
		if profundidade > 0 {
			segmento.WriteRune(r)
			continue
		}
		b.WriteRune(r)
	}
	// Grupo aberto sem fechamento: devolve o resto como estava (nunca "come" o
	// texto por causa de um parêntese solto).
	if profundidade > 0 {
		b.WriteRune('(')
		b.WriteString(segmento.String())
	}

	out := colapsarEspacos(b.String())
	for {
		cortado, mudou := cortarSufixoDeEdicao(out)
		out = cortado
		if !mudou {
			break
		}
	}
	return out
}

// cortarSufixoDeEdicao remove um " - <segmento de edição>" do FIM da string.
func cortarSufixoDeEdicao(s string) (string, bool) {
	i := strings.LastIndex(s, " - ")
	if i <= 0 {
		return s, false
	}
	if !ehSegmentoDeEdicao(s[i+3:]) {
		return s, false
	}
	return strings.TrimSpace(s[:i]), true
}

// ehSegmentoDeEdicao diz se o conteúdo de um segmento é só palavra(s) de
// edição/versão e/ou um ano (4 dígitos) -- "Bonus Edition", "Remastered 2011",
// "1984", "Deluxe". Qualquer outra palavra ("Ao Vivo", "Live in Rio") faz o
// segmento ser PRESERVADO.
func ehSegmentoDeEdicao(segmento string) bool {
	palavras := strings.FieldsFunc(strings.ToLower(segmento), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(palavras) == 0 {
		return false
	}
	for _, palavra := range palavras {
		if ehAno(palavra) {
			continue
		}
		// Fragmento de uma letra só é resto de apóstrofo/contração
		// ("Collector's" -> "collector" + "s"): ignora, senão a limpeza nunca
		// acontece nas tags reais.
		if len([]rune(palavra)) == 1 {
			continue
		}
		if !tokensEdicao[palavra] {
			return false
		}
	}
	return true
}

// ehAno diz se a palavra é um ano de 4 dígitos (1900-2099).
func ehAno(p string) bool {
	if len(p) != 4 {
		return false
	}
	ano, err := strconv.Atoi(p)
	return err == nil && ano >= 1900 && ano <= 2099
}

// colapsarEspacos reduz sequências de espaço em branco a um espaço e apara as
// pontas -- inclusive os espaços que sobraram onde um segmento foi removido.
func colapsarEspacos(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	ultimoEspaco := true
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !ultimoEspaco {
				b.WriteRune(' ')
				ultimoEspaco = true
			}
			continue
		}
		b.WriteRune(r)
		ultimoEspaco = false
	}
	return strings.TrimSpace(b.String())
}

// SimilaridadeNormalizada compara `a` e `b` no MELHOR dos dois mundos: o
// máximo entre a similaridade crua (comportamento de sempre) e a dos nomes
// normalizados por NormalizarNome, em escala 0-100 (mesma de
// LimiarAutoMatch). Nunca pior que comparar cru -- o qualificador de edição
// só pode AJUDAR a passar do limiar, jamais atrapalhar um nome que já casava.
func SimilaridadeNormalizada(a, b string) float64 {
	crua := textsim.Similaridade(a, b) * 100
	normalizada := textsim.Similaridade(NormalizarNome(a), NormalizarNome(b)) * 100
	if normalizada > crua {
		return normalizada
	}
	return crua
}
