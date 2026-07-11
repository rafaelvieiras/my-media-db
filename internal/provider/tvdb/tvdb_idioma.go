package tvdb

import "strings"

// Este arquivo isola o mapeamento de idioma entre os CÓDIGOS DO APP (0019: pt-BR, en,
// es…) e os do TheTVDB (ISO 639-2/T de 3 letras: por, eng, spa…). O TheTVDB NÃO
// distingue pt-BR/pt-PT — ambos caem em `por`. As traduções são buscadas por 3 letras
// e o canônico é chaveado pelo código do app (para o resolver casar), COM FALLBACK
// (idioma sem tradução simplesmente não entra no mapa).

// tabelaApp3 mapeia o código-base do app (2 letras) para o código TheTVDB (3 letras).
// Cobre os idiomas comuns; um código desconhecido cai no próprio base (melhor esforço).
var tabelaApp3 = map[string]string{
	"pt": "por", "en": "eng", "es": "spa", "de": "deu", "fr": "fra",
	"it": "ita", "ja": "jpn", "zh": "zho", "ru": "rus", "ko": "kor",
	"nl": "nld", "sv": "swe", "no": "nor", "pl": "pol", "hu": "hun",
	"ar": "ara", "he": "heb", "cs": "ces", "fi": "fin", "tr": "tur",
}

// tabela3App é o inverso de tabelaApp3 (3 letras → 2 letras do app), para ancorar o
// idioma ORIGINAL da série (originalLanguage vem em 3 letras) sob o código do app.
var tabela3App = inverter(tabelaApp3)

// inverter troca chaves e valores de um mapa string→string.
func inverter(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

// codigoTVDB converte um código de idioma do app (ex.: "pt-BR", "en-US") no código
// TheTVDB de 3 letras (ex.: "por", "eng"). Usa só o base (antes do "-"). Desconhecido
// devolve o base como veio (melhor esforço); vazio devolve "".
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

// codigoApp converte um código TheTVDB de 3 letras no código-base do app (ex.: "eng"
// → "en"). Desconhecido devolve o próprio código (melhor esforço).
func codigoApp(tvdb3 string) string {
	if a, ok := tabela3App[tvdb3]; ok {
		return a
	}
	return tvdb3
}

// codigosApp devolve as chaves de app sob as quais gravar uma tradução de `appLang`:
// o próprio código (ex.: "pt-BR") e o base (ex.: "pt"), para o resolver casar tanto o
// combinado quanto o base — mesma convenção do provider tmdb.
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
