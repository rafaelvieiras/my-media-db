package provider

import "encoding/json"

// Este arquivo define o PAYLOAD media_external do EPISODIO (briefing 0041): o episodio
// reusa o MESMO trilho multi-idioma do filme (media_external + resolver de 3 camadas),
// guardando title/overview POR IDIOMA no payload_json em vez de so o escalar default.
// O helper e compartilhado (enrich escreve, os providers leem) para NAO duplicar o
// marshaling/decode do envelope entre fontes (evita colisao no detector de copia).

// KindEpisodio e o DISCRIMINADOR gravado em EnvelopeEpisodio.EntityKind: isola o payload
// de episodio do de serie/filme da MESMA fonte (ex.: "tvdb") no MapearDetalhe do provider.
const KindEpisodio = "episode"

// EnvelopeEpisodio e o payload media_external de um episodio: os mapas idioma->valor de
// title/overview (crus do EpisodioExterno, sem descartar idiomas) mais o discriminador
// e, opcionalmente, o idioma original (ancora do fallback). Sem original_language a cadeia
// do resolver funciona como [lang, fallback] (0041 documenta a denormalizacao como opcional).
type EnvelopeEpisodio struct {
	EntityKind       string            `json:"entity_kind"`
	Title            map[string]string `json:"title,omitempty"`
	Overview         map[string]string `json:"overview,omitempty"`
	OriginalLanguage string            `json:"original_language,omitempty"`
}

// MontarEnvelopeEpisodio serializa o payload media_external do episodio a partir dos
// mapas per-idioma crus do EpisodioExterno. originalLanguage vazio e simplesmente
// omitido. Usado pelo enrich no (re)import — idempotente por (entity_id, source).
func MontarEnvelopeEpisodio(ep EpisodioExterno, originalLanguage string) (json.RawMessage, error) {
	env := EnvelopeEpisodio{
		EntityKind:       KindEpisodio,
		Title:            ep.Title,
		Overview:         ep.Overview,
		OriginalLanguage: originalLanguage,
	}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// MapearEpisodio reconhece o payload de EPISODIO (pelo discriminador entity_kind) e o
// mapeia para o mapa CANONICO do resolver: title/description como map[idioma]any e o
// original_language escalar. ok=false quando o payload NAO e de episodio (o provider
// segue para o ramo de serie/filme) OU quando o JSON e invalido — nunca entra em panico,
// entao um payload legado sem discriminador degrada gracioso (a fonte e pulada a montante).
func MapearEpisodio(raw json.RawMessage) (map[string]any, bool) {
	var env EnvelopeEpisodio
	if err := json.Unmarshal(raw, &env); err != nil || env.EntityKind != KindEpisodio {
		return nil, false
	}
	out := map[string]any{}
	if m := mapaIdiomaAny(env.Title); len(m) > 0 {
		out["title"] = m
	}
	if m := mapaIdiomaAny(env.Overview); len(m) > 0 {
		out["description"] = m
	}
	if env.OriginalLanguage != "" {
		out["original_language"] = env.OriginalLanguage
	}
	return out, true
}

// mapaIdiomaAny copia um map[idioma]string para map[idioma]any — o tipo que os
// containers traduziveis do resolver exigem. nil quando o mapa e vazio.
func mapaIdiomaAny(m map[string]string) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
