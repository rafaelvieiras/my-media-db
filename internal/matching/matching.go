// Package matching concentrates the candidate SCORING of the auto-match: given a
// query title/year and a candidate title/year, it decides the final score (0-100) and
// whether the candidate must be discarded for a year mismatch. It is a LEAF package (it
// only depends on internal/textsim) so that it can be consumed both by internal/server
// (Revisão queue, auto-match endpoints) and by internal/enrich (name matching on manual
// import) without creating an import cycle between the two.
package matching

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/rafaelvieiras/my-media-db/internal/textsim"
)

// LimiarAutoMatch is the score cut (0-100 scale, the same as match_confidence) above
// which the auto-match applies the candidate ON ITS OWN.
// Deliberately well above the low-confidence threshold of the Revisão queue
// (70, internal/server/review.go): the cost of a false positive (it silently contaminates the
// catalog) is much higher than that of a false negative (an item that was already
// visible in the queue/orphans stays there).
const LimiarAutoMatch = 92.0

// LimiarSugestao is the score cut (0-100 scale) above which a candidate found by a
// CHEAPER VARIANT of the name (one token dropped of "2 Broken Girls" -> "2 Broke") is worth
// proposing to the owner as a suggested match.
//
// It is deliberately BELOW LimiarAutoMatch, because the two answer different questions:
// auto-match asks "is this certainly the item?", the suggestion asks "is this worth
// showing the human?". A suggestion is never applied on its own (it only feeds the
// "did you mean this?" warning), so the cost of a miss is one dismissed hint.
//
// Calibrated with this package's own scorer (Jaro-Winkler on 0-100): a real typo scores
// 95-100 ("2 Broken Girls"/"2 Broke Girls", "The Offce"/"The Office"), a DROPPED token
// scores 86-89 ("Broke Girls", "Simpsons" against "The Simpsons") -- and dropping a token
// is exactly the variant the fallback generates, so the cut has to sit under 86.
const LimiarSugestao = 80.0

// ToleranciaDuracaoSegundos is the slack (in seconds) used as an additional
// CONFIRMATION of a music track match when two durations (local file
// vs. external candidate) are compared. MusicBrainz
// Picard uses 2s as the default for that same gate; here a little more slack
// (3s) to absorb the imprecision of the CONTAINER duration that ffprobe
// reports (through mediainfo) compared to the exact decoding of the audio.
const ToleranciaDuracaoSegundos = 3.0

// LimiarConfiancaFingerprint is the minimum cut (0-1 scale, the SAME as the score
// that AcoustID returns) to accept a RecordingCandidato from the acoustic fingerprint step
// 4.5 as a valid match. AcoustID already
// filters internally by duration similarity before returning a
// candidate, so the score here mostly reflects the quality of the
// fingerprint itself (noise/capture quality, not metadata
// ambiguity) -- which is why a more permissive cut than LimiarAutoMatch (which
// measures TEXT similarity, subject to similar names of different
// items) is acceptable. 0.6 is the middle of the "good enough" range
// documented by AcoustID/Picard itself (candidates < 0.5 are usually
// noise; >= 0.8 is high confidence) -- a value calibratable later with real
// production data, without changing the contract of this constant.
const LimiarConfiancaFingerprint = 0.6

// Pontuar computes the final score [0,100]: Jaro-Winkler similarity
// of the title (textsim) on a 0-100 scale, adjusted by the year.
// descartado=true when BOTH years exist and differ by MORE than 1 year
// (a remake must not stick, even with an identical title) — the candidate
// never enters the ranking in that case. Equal years add +5 (capped at 100);
// a difference of exactly 1 year does not adjust (tolerance for a common
// cataloguing mistake); any missing year does not adjust either (nothing to compare).
func Pontuar(tituloConsulta string, anoConsulta int, tituloCandidato string, anoCandidato int) (score float64, descartado bool) {
	base := textsim.Similaridade(tituloConsulta, tituloCandidato) * 100
	score, descartado = ajustarPorAno(base, anoConsulta, anoCandidato)
	return score, descartado
}

// PontuarComAlternativos is the variant of Pontuar that also
// considers the ALTERNATIVE/TRANSLATED TITLES the provider knows for the
// candidate -- not only the title in the queried language. The textual similarity
// uses the HIGHEST score between `tituloCandidato` and each item of
// `titulosAlternativos`; tituloVencedor returns WHICH string produced that
// score (so the UI can explain the origin of the guess — "via alternative title").
// tituloVencedor is always one of the compared titles (never empty), even
// when the winner was the main title itself -- it is up to the CALLER to decide
// whether to display that (e.g.: matched_title is only filled when the winner
// differs from the main title). The year rule is the SAME as Pontuar
// (shared through ajustarPorAno, without duplicating the diff logic).
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

// ajustarPorAno applies the year adjustment to a base score of
// textual similarity (0-100): discards (score 0, descartado=true) when
// BOTH years exist and differ by MORE than 1 year; adds +5 (capped at 100) when
// the years match exactly; does not adjust in any other case (a difference of 1
// year, or a year missing on one side). Extracted to be shared by
// Pontuar and PontuarComAlternativos, without duplicating the rule in two places.
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

// AnoDeReleaseDate extracts the year from the first 4 digits of a release_date
// "YYYY-MM-DD" (or "YYYY"); 0 when absent/short/invalid. Shared
// between internal/server (reads from the column already written) and internal/enrich (reads
// from the canonical map of the detail, before the entity exists).
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

// ── Normalization of WORK names ──────────────────────────────────────────────

// tokensEdicao are the qualifiers that a TAGGER writes into the album/
// track name and that MusicBrainz normally does NOT carry in the canonical title of the
// release-group/track: "(Deluxe Edition)", "(Bonus Edition)", "(Remastered)",
// "(Expanded)", "(Collector's Edition)", " - Remastered 2011", "[Explicit]".
// Compared against the content of a segment between parentheses/brackets (or a
// " - ..." suffix), lowercased and already reduced to words.
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

// NormalizarNome removes from `s` the EDITION/VERSION segments -- between
// parentheses, brackets or after a trailing " - " -- whose content is only the word(s)
// of tokensEdicao (or an isolated year). It is the preparation for comparing a tag name
// with the canonical title of the source, and it NEVER returns empty: if the cleanup zeroes the
// string, it returns the original (never worse than comparing raw).
//
// Examples: "Hybrid Theory (Bonus Edition)" → "Hybrid Theory";
// "Rio (Collector's Edition)" → "Rio"; "The Singles (Expanded)" → "The Singles";
// "Notorious (1984)" → "Notorious"; "Alright (Remastered)" → "Alright".
// It does NOT touch qualifiers that change the WORK (e.g.: "(Ao Vivo)", "(Live)",
// "(Acoustic)"): only edition/version enter the list.
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

// apenasSeparadores tells whether the string has no letter/digit at all (only spaces,
// hyphens, colons...) -- in that case the normalization brought no value.
func apenasSeparadores(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// removerSegmentosDeEdicao is the body of NormalizarNome: it walks the string once,
// discarding the groups (...) [...] {...} whose CONTENT is only
// edition/year, and then the " - <edition>" suffix of the end (repeatedly, for
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
						// discards the whole group; the surrounding space is
						// collapsed at the end
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
	// Open group without a close: return the rest as it was (never "eat" the
	// text because of a stray parenthesis).
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

// cortarSufixoDeEdicao removes a " - <edition segment>" from the END of the string.
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

// palavrasDeVersao are the endings that LABEL the version of the same recording without
// changing it: "(Twilight Soundtrack Version)", "(Album Version)", "(2nd single
// version)", "(Alternate Version)". Such a segment is not
// pure edition (it has the soundtrack name/the edition number before it), so the
// "all words in the list" rule did not catch it -- as measured in production: 20
// of the 37 tracks that were left over fell exactly here.
var palavrasDeVersao = map[string]bool{
	"version": true, "versao": true, "versão": true,
}

// palavrasDiferenciadoras mark a segment that is ANOTHER recording, not another version
// of the same one: live, acoustic, demo, remix, instrumental. With any of them
// the segment is PRESERVED even when it ends in a version word -- "Full Moon
// (Appleblim & Komonazmuk Remix)" is another track, not the same one with a label.
var palavrasDiferenciadoras = map[string]bool{
	"live": true, "vivo": true, "vivO": true, "acoustic": true, "acustico": true,
	"acústico": true, "demo": true, "remix": true, "remixed": true,
	"instrumental": true, "karaoke": true, "cover": true, "unplugged": true,
	"session": true, "sessions": true, "rehearsal": true, "extended": true,
	"reprise": true, "medley": true, "mashup": true, "aovivo": true,
}

// ehSegmentoDeEdicao tells whether the content of a segment is only edition/version
// word(s) and/or a year (4 digits) -- "Bonus Edition", "Remastered 2011",
// "1984", "Deluxe" -- or whether it is a VERSION LABEL of the same recording ending in
// a version word with no differentiator ("Twilight Soundtrack Version").
// "edit"/"edição" are NOT in palavrasDeVersao on purpose: a radio edit is
// ANOTHER recording (shorter) and accepting it as the same track would make the match stop
// at the wrong release -- two existing release-selection tests catch this.
// Any other word ("Ao Vivo", "Live in Rio") makes the segment be PRESERVED.
func ehSegmentoDeEdicao(segmento string) bool {
	if ehRotuloDeVersao(segmento) {
		return true
	}
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
		// A single-letter fragment is a leftover of an apostrophe/contraction
		// ("Collector's" -> "collector" + "s"): ignore it, otherwise the cleanup never
		// happens on real tags.
		if len([]rune(palavra)) == 1 {
			continue
		}
		if !tokensEdicao[palavra] {
			return false
		}
	}
	return true
}

// ehRotuloDeVersao recognizes the segment that ENDS in a version word and has
// no differentiator ("Twilight Soundtrack Version" yes, "Live at BBC1"
// no, "Suffer the Children (Demo)" no).
func ehRotuloDeVersao(segmento string) bool {
	palavras := palavrasDoSegmento(segmento)
	if len(palavras) == 0 {
		return false
	}
	for _, palavra := range palavras {
		if palavrasDiferenciadoras[palavra] {
			return false
		}
	}
	return palavrasDeVersao[palavras[len(palavras)-1]]
}

// palavrasDoSegmento splits the content of a segment into lowercase words,
// ignoring separators and single-letter fragments (leftover of an apostrophe).
func palavrasDoSegmento(segmento string) []string {
	brutas := strings.FieldsFunc(strings.ToLower(segmento), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(brutas))
	for _, palavra := range brutas {
		if len([]rune(palavra)) == 1 {
			continue
		}
		out = append(out, palavra)
	}
	return out
}

// ehAno tells whether the word is a 4-digit year (1900-2099).
func ehAno(p string) bool {
	if len(p) != 4 {
		return false
	}
	ano, err := strconv.Atoi(p)
	return err == nil && ano >= 1900 && ano <= 2099
}

// colapsarEspacos reduces runs of whitespace to one space and trims the ends --
// including the spaces left over where a segment was removed.
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

// SimilaridadeNormalizada compares `a` and `b` at the BEST of both worlds: the
// maximum between the raw similarity (the usual behaviour) and the similarity of the names
// normalized by NormalizarNome, on a 0-100 scale (the same as
// LimiarAutoMatch). Never worse than comparing raw -- the edition qualifier
// can only HELP to clear the threshold, never hurt a name that already matched.
func SimilaridadeNormalizada(a, b string) float64 {
	crua := textsim.Similaridade(a, b) * 100
	normalizada := textsim.Similaridade(NormalizarNome(a), NormalizarNome(b)) * 100
	if normalizada > crua {
		return normalizada
	}
	return crua
}
