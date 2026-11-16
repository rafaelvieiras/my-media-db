package provider

import "encoding/json"

// This file defines the media_external PAYLOAD of the EPISODE: the episode reuses
// the SAME multi-language track of the movie (media_external + 3-layer resolver),
// storing title/overview PER LANGUAGE in payload_json instead of only the default
// scalar. The helper is shared (enrich writes, the providers read) so as NOT to
// duplicate the marshaling/decode of the envelope between sources (avoids a
// collision in the copy detector).

// KindEpisodio is the DISCRIMINATOR stored in EnvelopeEpisodio.EntityKind: it
// isolates the episode payload from the series/movie one of the SAME source
// (e.g. "tvdb") in the provider's MapearDetalhe.
const KindEpisodio = "episode"

// EnvelopeEpisodio is the media_external payload of an episode: the
// language->value maps of title/overview (raw from EpisodioExterno, without
// discarding languages) plus the discriminator and, optionally, the original
// language (anchor of the fallback). Without original_language the resolver
// chain works as [lang, fallback] (the denormalization is documented as
// optional).
type EnvelopeEpisodio struct {
	EntityKind       string            `json:"entity_kind"`
	Title            map[string]string `json:"title,omitempty"`
	Overview         map[string]string `json:"overview,omitempty"`
	OriginalLanguage string            `json:"original_language,omitempty"`
}

// MontarEnvelopeEpisodio serializes the media_external payload of the episode
// from the raw per-language maps of EpisodioExterno. An empty originalLanguage is
// simply omitted. Used by enrich on (re)import -- idempotent by (entity_id,
// source).
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

// MapearEpisodio recognizes the EPISODE payload (through the entity_kind
// discriminator) and maps it to the CANONICAL map of the resolver:
// title/description as map[language]any and the scalar original_language.
// ok=false when the payload is NOT an episode one (the provider goes on to the
// series/movie branch) OR when the JSON is invalid -- it never panics, so a
// legacy payload without a discriminator degrades gracefully (the source is
// skipped upstream).
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

// mapaIdiomaAny copies a map[language]string to map[language]any -- the type the
// translatable containers of the resolver require. nil when the map is empty.
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
