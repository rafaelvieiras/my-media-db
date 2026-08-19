package musicext

import (
	"database/sql"
	"testing"
)

// TestExtractNamespace cobre a extracao do objeto do namespace core.music@1:
// presente, NULL/vazio, JSON invalido e namespace ausente (todos degradando
// para nil, menos o caso feliz).
func TestExtractNamespace(t *testing.T) {
	casos := []struct {
		nome    string
		raw     sql.NullString
		querNil bool
	}{
		{"null", sql.NullString{}, true},
		{"vazio", sql.NullString{Valid: true, String: ""}, true},
		{"json invalido", sql.NullString{Valid: true, String: "{"}, true},
		{"ns ausente", sql.NullString{Valid: true, String: `{"outro@1":{"a":1}}`}, true},
		{"presente", sql.NullString{Valid: true, String: `{"core.music@1":{"track_number":3,"disc_number":1,"disc_total":2,"isrc":"USRC17607839"}}`}, false},
	}
	for _, c := range casos {
		got := ExtractNamespace(c.raw, Namespace)
		if c.querNil && got != nil {
			t.Errorf("%s: ExtractNamespace = %v, quero nil", c.nome, got)
		}
		if !c.querNil {
			if got == nil {
				t.Fatalf("%s: ExtractNamespace = nil, quero o objeto", c.nome)
			}
			track, ok := AsInt(got["track_number"])
			if !ok || track != 3 {
				t.Errorf("%s: track_number = %v,%v, quero 3,true", c.nome, track, ok)
			}
			disco, ok := AsInt(got["disc_number"])
			if !ok || disco != 1 {
				t.Errorf("%s: disc_number = %v,%v, quero 1,true", c.nome, disco, ok)
			}
			total, ok := AsInt(got["disc_total"])
			if !ok || total != 2 {
				t.Errorf("%s: disc_total = %v,%v, quero 2,true", c.nome, total, ok)
			}
			isrc, ok := AsString(got["isrc"])
			if !ok || isrc != "USRC17607839" {
				t.Errorf("%s: isrc = %v,%v, quero USRC17607839,true", c.nome, isrc, ok)
			}
		}
	}
}

// TestAsInt cobre a coercao de numeros de JSON (float64) e o rejeito de nao-numeros.
func TestAsInt(t *testing.T) {
	if n, ok := AsInt(float64(5)); !ok || n != 5 {
		t.Errorf("AsInt(float64(5)) = %d,%v; quero 5,true", n, ok)
	}
	if n, ok := AsInt(int64(7)); !ok || n != 7 {
		t.Errorf("AsInt(int64(7)) = %d,%v; quero 7,true", n, ok)
	}
	if n, ok := AsInt(3); !ok || n != 3 {
		t.Errorf("AsInt(3) = %d,%v; quero 3,true", n, ok)
	}
	if _, ok := AsInt("nao-numero"); ok {
		t.Errorf("AsInt(string) devia ser false")
	}
	if _, ok := AsInt(nil); ok {
		t.Errorf("AsInt(nil) devia ser false")
	}
}

// TestAsString cobre a coercao de string e o rejeito de nao-string.
func TestAsString(t *testing.T) {
	if s, ok := AsString("USRC17607839"); !ok || s != "USRC17607839" {
		t.Errorf("AsString = %q,%v; quero USRC17607839,true", s, ok)
	}
	if _, ok := AsString(float64(5)); ok {
		t.Errorf("AsString(float64) devia ser false")
	}
	if _, ok := AsString(nil); ok {
		t.Errorf("AsString(nil) devia ser false")
	}
}

// TestConstantes cobre os valores canonicos que o ADR-0027 fixa (kinds e o
// MBID de Various Artists) — regressao contra edicao acidental.
func TestConstantes(t *testing.T) {
	if Namespace != "core.music@1" {
		t.Errorf("Namespace = %q, quero core.music@1", Namespace)
	}
	if KindAlbum != "music_album" {
		t.Errorf("KindAlbum = %q, quero music_album", KindAlbum)
	}
	if KindArtistaPrincipal != "music_artista_principal" {
		t.Errorf("KindArtistaPrincipal = %q, quero music_artista_principal", KindArtistaPrincipal)
	}
	if KindArtistaFeat != "music_artista_feat" {
		t.Errorf("KindArtistaFeat = %q, quero music_artista_feat", KindArtistaFeat)
	}
	if MBIDVariousArtists != "89ad4ac3-39f7-470e-963a-56509c546377" {
		t.Errorf("MBIDVariousArtists = %q, valor canonico da MusicBrainz mudou", MBIDVariousArtists)
	}
}
