package provider

import "testing"

// TestInterpretarReferencia covers the shapes a human actually types/pastes. The point of
// the parser is to be STRICT: anything ambiguous stays a title search.
func TestInterpretarReferencia(t *testing.T) {
	casos := []struct {
		nome  string
		termo string
		quero ReferenciaDeID
		ok    bool
	}{
		{"prefixo tmdb", "tmdb:39340", ReferenciaDeID{Fonte: "tmdb", ID: "39340"}, true},
		{"prefixo tmdb com espaço", "TMDB: 39340", ReferenciaDeID{Fonte: "tmdb", ID: "39340"}, true},
		{"prefixo themoviedb", "themoviedb:603", ReferenciaDeID{Fonte: "tmdb", ID: "603"}, true},
		{"prefixo tvdb", "tvdb:248741", ReferenciaDeID{Fonte: "tvdb", ID: "248741"}, true},
		{"prefixo thetvdb", "TheTVDB:248741", ReferenciaDeID{Fonte: "tvdb", ID: "248741"}, true},
		{"prefixo imdb", "imdb:tt1845307", ReferenciaDeID{Fonte: "imdb", ID: "tt1845307"}, true},
		{"imdb solto", "tt1845307", ReferenciaDeID{Fonte: "imdb", ID: "tt1845307"}, true},
		{"imdb solto maiúsculo", "TT1845307", ReferenciaDeID{Fonte: "imdb", ID: "tt1845307"}, true},
		{"url tmdb série", "https://www.themoviedb.org/tv/39340-2-broke-girls", ReferenciaDeID{Fonte: "tmdb", ID: "39340", Tipo: "tv"}, true},
		{"url tmdb filme", "https://www.themoviedb.org/movie/603", ReferenciaDeID{Fonte: "tmdb", ID: "603", Tipo: "movie"}, true},
		{"url tmdb sem esquema", "www.themoviedb.org/tv/39340", ReferenciaDeID{Fonte: "tmdb", ID: "39340", Tipo: "tv"}, true},
		{"url tvdb dereferrer", "https://www.thetvdb.com/dereferrer/series/248741", ReferenciaDeID{Fonte: "tvdb", ID: "248741", Tipo: "tv"}, true},
		{"url tvdb com slug", "https://thetvdb.com/series/248741-2-broke-girls", ReferenciaDeID{Fonte: "tvdb", ID: "248741", Tipo: "tv"}, true},
		{"url imdb", "https://www.imdb.com/title/tt1845307/", ReferenciaDeID{Fonte: "imdb", ID: "tt1845307"}, true},
		{"título normal", "2 Broke Girls", ReferenciaDeID{}, false},
		{"título com dois pontos", "Star Trek: Discovery", ReferenciaDeID{}, false},
		{"prefixo desconhecido", "letterboxd:123", ReferenciaDeID{}, false},
		{"prefixo sem id", "tmdb:", ReferenciaDeID{}, false},
		{"id não numérico", "tmdb:abc", ReferenciaDeID{}, false},
		{"imdb inválido", "imdb:1845307", ReferenciaDeID{}, false},
		{"numero solto (ambíguo)", "39340", ReferenciaDeID{}, false},
		{"url com número no início do slug", "https://www.themoviedb.org/tv/2-broke-girls", ReferenciaDeID{}, false},
		{"url só com id", "https://www.themoviedb.org/tv/2", ReferenciaDeID{Fonte: "tmdb", ID: "2", Tipo: "tv"}, true},
		{"url de outro site", "https://example.com/tv/39340", ReferenciaDeID{}, false},
		{"vazio", "   ", ReferenciaDeID{}, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, ok := InterpretarReferencia(c.termo)
			if ok != c.ok {
				t.Fatalf("ok = %v, quero %v (%+v)", ok, c.ok, got)
			}
			if !c.ok {
				return
			}
			if got != c.quero {
				t.Errorf("referência = %+v, quero %+v", got, c.quero)
			}
		})
	}
}
