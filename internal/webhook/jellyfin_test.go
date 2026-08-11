package webhook

import "testing"

// TestParsePayloadCamposCompletos cobre §5: um payload com Provider_tmdb (e
// os demais campos numéricos como number JSON) decodifica corretamente,
// inclusive a derivação de PositionSeconds/RunTimeSeconds a partir dos
// ticks (10_000_000 ticks = 1s) e ProgressPercent explícito.
func TestParsePayloadCamposCompletos(t *testing.T) {
	body := []byte(`{
		"NotificationType": "PlaybackProgress",
		"ItemType": "Movie",
		"Provider_tmdb": "603",
		"Provider_imdb": "tt0133093",
		"Provider_tvdb": "12345",
		"PlaybackPositionTicks": 50000000,
		"RunTimeTicks": 100000000,
		"PlaybackPercentage": 42.5
	}`)
	ev, err := ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if ev.NotificationType != "PlaybackProgress" {
		t.Errorf("NotificationType = %q", ev.NotificationType)
	}
	if ev.ItemType != "Movie" {
		t.Errorf("ItemType = %q", ev.ItemType)
	}
	if ev.TmdbID != "603" || ev.ImdbID != "tt0133093" || ev.TvdbID != "12345" {
		t.Errorf("ids = tmdb=%q imdb=%q tvdb=%q", ev.TmdbID, ev.ImdbID, ev.TvdbID)
	}
	if ev.PositionSeconds == nil || *ev.PositionSeconds != 5 {
		t.Errorf("PositionSeconds = %v, quero 5", ev.PositionSeconds)
	}
	if ev.RunTimeSeconds == nil || *ev.RunTimeSeconds != 10 {
		t.Errorf("RunTimeSeconds = %v, quero 10", ev.RunTimeSeconds)
	}
	if ev.ProgressPercent == nil || *ev.ProgressPercent != 42.5 {
		t.Errorf("ProgressPercent = %v, quero 42.5 (explicito, nao derivado)", ev.ProgressPercent)
	}
}

// TestParsePayloadTituloAno cobre o briefing 0099: Name/Year do payload
// viram Title/Year no Evento -- usados quando o item nao casa nenhuma
// entity (exibicao na lista de orfaos).
func TestParsePayloadTituloAno(t *testing.T) {
	body := []byte(`{
		"NotificationType": "PlaybackStart",
		"ItemType": "Movie",
		"Name": "The Matrix",
		"Year": 1999
	}`)
	ev, err := ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if ev.Title != "The Matrix" {
		t.Errorf("Title = %q, quero %q", ev.Title, "The Matrix")
	}
	if ev.Year != "1999" {
		t.Errorf("Year = %q, quero %q (numero vira string)", ev.Year, "1999")
	}
}

// TestParsePayloadTituloAnoAusentes cobre o briefing 0099: payload sem
// Name/Year nao falha o parse -- Title/Year ficam "".
func TestParsePayloadTituloAnoAusentes(t *testing.T) {
	ev, err := ParsePayload([]byte(`{"NotificationType":"PlaybackStart","ItemType":"Movie"}`))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if ev.Title != "" || ev.Year != "" {
		t.Errorf("Title/Year deveriam ser vazios: Title=%q Year=%q", ev.Title, ev.Year)
	}
}

// TestParsePayloadCamposComoString cobre §3/§5: algumas versões do plugin
// serializam TUDO como string via template Handlebars -- o parser deve
// tolerar e converter.
func TestParsePayloadCamposComoString(t *testing.T) {
	body := []byte(`{
		"NotificationType": "PlaybackStop",
		"ItemType": "Episode",
		"Provider_tvdb": "9001",
		"PlaybackPositionTicks": "80000000",
		"RunTimeTicks": "100000000"
	}`)
	ev, err := ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if ev.TvdbID != "9001" {
		t.Errorf("TvdbID = %q, quero 9001", ev.TvdbID)
	}
	if ev.PositionSeconds == nil || *ev.PositionSeconds != 8 {
		t.Errorf("PositionSeconds = %v, quero 8", ev.PositionSeconds)
	}
	// Sem PlaybackPercentage explicito: deriva de posicao/duracao (80%).
	if ev.ProgressPercent == nil || *ev.ProgressPercent != 80 {
		t.Errorf("ProgressPercent derivado = %v, quero 80", ev.ProgressPercent)
	}
}

// TestParsePayloadCamposAusentes cobre §3/§5: payload minimo (so os campos
// que existem) nao falha o parse -- os ausentes ficam no zero-value.
func TestParsePayloadCamposAusentes(t *testing.T) {
	ev, err := ParsePayload([]byte(`{"NotificationType":"PlaybackStart"}`))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if ev.NotificationType != "PlaybackStart" {
		t.Errorf("NotificationType = %q", ev.NotificationType)
	}
	if ev.TmdbID != "" || ev.ImdbID != "" || ev.TvdbID != "" {
		t.Errorf("ids deveriam ser vazios: tmdb=%q imdb=%q tvdb=%q", ev.TmdbID, ev.ImdbID, ev.TvdbID)
	}
	if ev.PositionSeconds != nil || ev.RunTimeSeconds != nil || ev.ProgressPercent != nil {
		t.Error("campos numericos ausentes deveriam ser nil")
	}
}

// TestParsePayloadVazio cobre §5: objeto JSON totalmente vazio nao falha.
func TestParsePayloadVazio(t *testing.T) {
	ev, err := ParsePayload([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if ev.NotificationType != "" || ev.ItemType != "" {
		t.Errorf("evento deveria ser zero-value: %+v", ev)
	}
}

// TestParsePayloadNaoJSON cobre §5: corpo que nao e um objeto JSON valido
// devolve erro (o handler HTTP degrada para 204/log, nunca 500).
func TestParsePayloadNaoJSON(t *testing.T) {
	if _, err := ParsePayload([]byte(`isto nao e json`)); err == nil {
		t.Error("esperava erro para corpo nao-JSON")
	}
	if _, err := ParsePayload([]byte(``)); err == nil {
		t.Error("esperava erro para corpo vazio")
	}
}

// TestItemTypeSuportado cobre §3: so Movie/Episode sao suportados.
func TestItemTypeSuportado(t *testing.T) {
	casos := map[string]bool{
		"Movie":   true,
		"Episode": true,
		"Audio":   false,
		"Book":    false,
		"":        false,
	}
	for tipo, quer := range casos {
		ev := Evento{ItemType: tipo}
		if got := ev.ItemTypeSuportado(); got != quer {
			t.Errorf("ItemTypeSuportado(%q) = %v, quero %v", tipo, got, quer)
		}
	}
}

// TestMapStatus cobre §3/§5: o mapeamento NotificationType -> status,
// inclusive o limiar de 80% em PlaybackStop.
func TestMapStatus(t *testing.T) {
	pct := func(v float64) *float64 { return &v }

	casos := []struct {
		nome             string
		notificationType string
		progress         *float64
		quer             string
	}{
		{"start", "PlaybackStart", nil, StatusStarted},
		{"progress", "PlaybackProgress", pct(50), StatusStarted},
		{"pause", "PlaybackPause", nil, StatusPaused},
		{"stop abaixo do limiar", "PlaybackStop", pct(79.9), StatusStopped},
		{"stop exatamente no limiar", "PlaybackStop", pct(80), StatusCompleted},
		{"stop acima do limiar", "PlaybackStop", pct(95), StatusCompleted},
		{"stop sem progresso", "PlaybackStop", nil, StatusStopped},
		{"tipo desconhecido", "ItemAdded", nil, ""},
		{"tipo vazio", "", nil, ""},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := MapStatus(c.notificationType, c.progress)
			if got != c.quer {
				t.Errorf("MapStatus(%q, %v) = %q, quero %q", c.notificationType, c.progress, got, c.quer)
			}
		})
	}
}
