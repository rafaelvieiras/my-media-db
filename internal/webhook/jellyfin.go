// Package webhook implementa o PARSER dos webhooks de media server que
// alimentam o histórico de consumo (briefing 0093, ADR-0022). Hoje só
// Jellyfin (plugin oficial `jellyfin-plugin-webhook`); Plex/Emby são
// follow-ups que reaproveitam o mesmo historico_evento/casamento por id
// externo (só o parser muda). Este pacote não conhece banco nem HTTP — só
// decodifica o payload cru em algo que internal/server sabe persistir.
package webhook

import (
	"encoding/json"
	"strconv"
)

// Kind e o kind fixo gravado pelos eventos deste webhook (briefing 0093
// §3): so filme/serie nesta entrega, sempre "assistir".
const Kind = "assistir"

// Source e a provenencia gravada em historico_evento.source para todo
// evento originado deste webhook.
const Source = "jellyfin-webhook"

// Status possiveis apos o mapeamento de NotificationType (briefing 0093
// §3, ADR-0022 §H3).
const (
	StatusStarted   = "started"
	StatusPaused    = "paused"
	StatusStopped   = "stopped"
	StatusCompleted = "completed"
)

// limiarCompleted e o percentual minimo de progresso para um PlaybackStop
// virar "completed" em vez de "stopped" -- mesmo limiar que Trakt/Simkl
// usam (ADR-0022 §H3, briefing 0093 §3).
const limiarCompleted = 80.0

// ticksPorSegundo: o Jellyfin reporta posicao/duracao em "ticks" de 100ns
// (mesma unidade do .NET DateTime/TimeSpan) -- 10 milhoes de ticks = 1s.
const ticksPorSegundo = 10_000_000.0

// itemTypesSuportados restringe esta entrega a filme/episodio (briefing
// 0093 §3) -- qualquer outro ItemType do payload (Audio, Book, etc.) e
// ignorado pelo chamador.
var itemTypesSuportados = map[string]bool{
	"Movie":   true,
	"Episode": true,
}

// Evento e o payload do jellyfin-plugin-webhook ja normalizado, tolerante a
// campos ausentes ou tipados de forma inesperada (a forma exata varia por
// versao do plugin/template -- ver briefing 0093 §3). Campos ausentes ficam
// no zero-value (string vazia / ponteiro nil), nunca causam erro de parse.
type Evento struct {
	NotificationType string
	ItemType         string
	TmdbID           string
	ImdbID           string
	TvdbID           string
	PositionSeconds  *float64
	RunTimeSeconds   *float64
	ProgressPercent  *float64
}

// ItemTypeSuportado diz se o ItemType do evento e um dos cobertos por esta
// entrega (Movie/Episode) -- qualquer outro (Audio, Book, etc.) deve ser
// descartado pelo chamador sem criar historico_evento.
func (e Evento) ItemTypeSuportado() bool {
	return itemTypesSuportados[e.ItemType]
}

// ParsePayload decodifica o corpo cru (JSON) do webhook num Evento,
// best-effort: um campo individual ausente ou com tipo inesperado e
// simplesmente ignorado (fica no zero-value) -- nunca falha o parse por
// causa de UM campo. So devolve erro quando o corpo INTEIRO nao e um objeto
// JSON valido; mesmo nesse caso o handler HTTP nunca responde 500 (ver
// briefing 0093 §3) -- so descarta e loga.
func ParsePayload(body []byte) (Evento, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return Evento{}, err
	}

	ev := Evento{
		NotificationType: stringField(raw, "NotificationType"),
		ItemType:         stringField(raw, "ItemType"),
		TmdbID:           stringField(raw, "Provider_tmdb"),
		ImdbID:           stringField(raw, "Provider_imdb"),
		TvdbID:           stringField(raw, "Provider_tvdb"),
	}

	if posTicks := numberField(raw, "PlaybackPositionTicks"); posTicks != nil {
		v := *posTicks / ticksPorSegundo
		ev.PositionSeconds = &v
	}
	if runTicks := numberField(raw, "RunTimeTicks"); runTicks != nil {
		v := *runTicks / ticksPorSegundo
		ev.RunTimeSeconds = &v
	}

	switch pct := numberField(raw, "PlaybackPercentage"); {
	case pct != nil:
		ev.ProgressPercent = pct
	case ev.PositionSeconds != nil && ev.RunTimeSeconds != nil && *ev.RunTimeSeconds > 0:
		// Sem PlaybackPercentage explicito (algumas versoes do plugin nao
		// mandam esse campo em PlaybackStop): deriva de posicao/duracao.
		v := (*ev.PositionSeconds / *ev.RunTimeSeconds) * 100
		ev.ProgressPercent = &v
	}

	return ev, nil
}

// MapStatus mapeia o NotificationType do jellyfin-plugin-webhook para o
// status do historico_evento (briefing 0093 §3):
//
//	PlaybackStart/PlaybackProgress -> started
//	PlaybackPause                  -> paused
//	PlaybackStop                   -> completed (progress >= 80%) | stopped
//
// NotificationType desconhecido (ex.: ItemAdded, outro tipo de evento que o
// dono tenha marcado sem querer no plugin) devolve "" -- o chamador
// descarta o evento sem criar historico_evento.
func MapStatus(notificationType string, progressPercent *float64) string {
	switch notificationType {
	case "PlaybackStart", "PlaybackProgress":
		return StatusStarted
	case "PlaybackPause":
		return StatusPaused
	case "PlaybackStop":
		if progressPercent != nil && *progressPercent >= limiarCompleted {
			return StatusCompleted
		}
		return StatusStopped
	default:
		return ""
	}
}

// stringField le um campo tolerando ausencia ou tipo inesperado -- devolve
// "" quando ausente, null, ou nao decodificavel nem como string nem como
// numero (alguns campos Provider_* podem vir serializados como numero puro
// quando o id externo e todo numerico, dependendo da versao do plugin).
func stringField(raw map[string]json.RawMessage, key string) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(v, &n) == nil {
		return n.String()
	}
	return ""
}

// numberField le um campo numerico tolerando number OU string (o plugin
// frequentemente serializa tudo como string via template) -- nil quando
// ausente, vazio, ou nao numerico.
func numberField(raw map[string]json.RawMessage, key string) *float64 {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	var f float64
	if json.Unmarshal(v, &f) == nil {
		return &f
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		if s == "" {
			return nil
		}
		if parsed, err := strconv.ParseFloat(s, 64); err == nil {
			return &parsed
		}
	}
	return nil
}
