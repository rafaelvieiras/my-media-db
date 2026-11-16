package tvdb

import "strings"

// This file isolates the language mapping between the APP CODES (pt-BR, en, es…) and the
// ones of TheTVDB (ISO 639-2/T with 3 letters: por, eng, spa…). TheTVDB does NOT
// distinguish pt-BR/pt-PT -- both fall into `por`. The translations are fetched by the 3
// letters and the canonical is keyed by the app code (so the resolver matches), WITH
// FALLBACK (a language without a translation simply does not enter the map).

// tabelaApp3 maps the base code of the app (2 letters) to the TheTVDB code (3 letters).
// It covers the common languages; an unknown code falls back to the base itself (best
// effort).
var tabelaApp3 = map[string]string{
	"pt": "por", "en": "eng", "es": "spa", "de": "deu", "fr": "fra",
	"it": "ita", "ja": "jpn", "zh": "zho", "ru": "rus", "ko": "kor",
	"nl": "nld", "sv": "swe", "no": "nor", "pl": "pol", "hu": "hun",
	"ar": "ara", "he": "heb", "cs": "ces", "fi": "fin", "tr": "tur",
}

// tabela3App is the inverse of tabelaApp3 (3 letters → 2 letters of the app), to anchor the
// ORIGINAL language of the series (originalLanguage comes in 3 letters) under the app
// code.
var tabela3App = inverter(tabelaApp3)

// inverter swaps keys and values of a string→string map.
func inverter(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

// codigoTVDB converts an app language code (e.g. "pt-BR", "en-US") into the 3-letter
// TheTVDB code (e.g. "por", "eng"). It uses only the base (before the "-"). Unknown
// returns the base as it came (best effort); empty returns "".
func codigoTVDB(appLang string) string {
	base := strings.ToLower(strings.SplitN(appLang, "-", 2)[0])
	if base == "" {
		return ""
	}
	if c, ok := tabelaApp3[base]; ok {
		return c
	}
	return base
}

// codigoApp converts a 3-letter TheTVDB code into the base code of the app (e.g. "eng" →
// "en"). Unknown returns the code itself (best effort).
func codigoApp(tvdb3 string) string {
	if a, ok := tabela3App[tvdb3]; ok {
		return a
	}
	return tvdb3
}

// codigosApp returns the app keys under which to write a translation of `appLang`: the
// code itself (e.g. "pt-BR") and the base one (e.g. "pt"), so the resolver matches both
// the combined and the base -- the same convention as the tmdb provider.
func codigosApp(appLang string) []string {
	if appLang == "" {
		return nil
	}
	base := strings.SplitN(appLang, "-", 2)[0]
	if base == appLang || base == "" {
		return []string{appLang}
	}
	return []string{appLang, base}
}
