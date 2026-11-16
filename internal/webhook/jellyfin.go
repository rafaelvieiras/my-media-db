// Package webhook implements the PARSER of the media-server webhooks that feed
// the consumption history (ADR-0022). Today only Jellyfin (official
// `jellyfin-plugin-webhook` plugin); Plex/Emby are follow-ups that reuse the
// same historico_evento/matching by external id (only the parser changes). This
// package knows neither database nor HTTP -- it only decodes the raw payload
// into something internal/server knows how to persist.
package webhook

import (
	"encoding/json"
	"strconv"
)

// Kind is the fixed kind written by the events of this webhook: only
// movie/series in this delivery, always "assistir".
const Kind = "assistir"

// Source is the provenance written to historico_evento.source for every event
// originated from this webhook.
const Source = "jellyfin-webhook"

// Possible statuses after mapping the NotificationType (ADR-0022 §H3).
const (
	StatusStarted   = "started"
	StatusPaused    = "paused"
	StatusStopped   = "stopped"
	StatusCompleted = "completed"
)

// limiarCompleted is the minimum progress percentage for a PlaybackStop to
// become "completed" instead of "stopped" -- the same threshold Trakt/Simkl
// use (ADR-0022 §H3).
const limiarCompleted = 80.0

// ticksPorSegundo: Jellyfin reports position/duration in "ticks" of 100ns
// (the same unit as .NET DateTime/TimeSpan) -- 10 million ticks = 1s.
const ticksPorSegundo = 10_000_000.0

// itemTypesSuportados restricts this delivery to movie/episode -- any other
// ItemType of the payload (Audio, Book, etc.) is ignored by the caller.
var itemTypesSuportados = map[string]bool{
	"Movie":   true,
	"Episode": true,
}

// Evento is the payload of jellyfin-plugin-webhook already normalized,
// tolerant of absent or unexpectedly typed fields (the exact shape varies by
// plugin/template version). Absent fields stay at their zero value (empty
// string / nil pointer) and never cause a parse error.
type Evento struct {
	NotificationType string
	ItemType         string
	TmdbID           string
	ImdbID           string
	TvdbID           string
	// Title/Year: best-effort capture of the item's title/year (the "Name"/
	// "Year" keys of the jellyfin-plugin-webhook template) for display in the
	// list of orphan events when no external id matches -- "" when the payload
	// does not carry it (the same tolerant spirit of the rest of the parser, it
	// never fails because of an absent field).
	Title           string
	Year            string
	PositionSeconds *float64
	RunTimeSeconds  *float64
	ProgressPercent *float64
}

// ItemTypeSuportado says whether the ItemType of the event is one of those
// covered by this delivery (Movie/Episode) -- any other one (Audio, Book, etc.)
// must be discarded by the caller without creating a historico_evento.
func (e Evento) ItemTypeSuportado() bool {
	return itemTypesSuportados[e.ItemType]
}

// ParsePayload decodes the raw body (JSON) of the webhook into an Evento,
// best-effort: an individual field that is absent or has an unexpected type is
// simply ignored (it stays at its zero value) -- the parse never fails because
// of ONE field. It only returns an error when the ENTIRE body is not a valid
// JSON object; even in that case the HTTP handler never answers 500 -- it just
// discards and logs.
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
		Title:            stringField(raw, "Name"),
		Year:             stringField(raw, "Year"),
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
		// Without an explicit PlaybackPercentage (some versions of the plugin do
		// not send that field on PlaybackStop): derive it from position/duration.
		v := (*ev.PositionSeconds / *ev.RunTimeSeconds) * 100
		ev.ProgressPercent = &v
	}

	return ev, nil
}

// MapStatus maps the NotificationType of jellyfin-plugin-webhook to the status
// of the historico_evento:
//
//	PlaybackStart/PlaybackProgress -> started
//	PlaybackPause                  -> paused
//	PlaybackStop                   -> completed (progress >= 80%) | stopped
//
// An unknown NotificationType (e.g.: ItemAdded, another kind of event the owner
// accidentally marked in the plugin) returns "" -- the caller discards the
// event without creating a historico_evento.
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

// stringField reads a field tolerating absence or an unexpected type -- it
// returns "" when the field is absent, null, or not decodable as either a
// string or a number (some Provider_* fields may come serialized as a plain
// number when the external id is entirely numeric, depending on the plugin
// version).
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

// numberField reads a numeric field tolerating number OR string (the plugin
// often serializes everything as a string through the template) -- nil when the
// field is absent, empty, or not numeric.
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
