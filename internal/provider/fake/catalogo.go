package fake

// catalogo.go holds the FIXTURES of the fake provider: compile-time data,
// immutable and deterministic. The Matrix franchise serves the E2E journeys:
// movie import, collection with members ordered by year, internal-first in the
// search and the non-destructiveness of enrich (editable title/tag vs. new tags
// from the source).
//
// Languages: every translatable field carries "en" and "pt-BR" (plus the base
// "pt"), exercising the per-language resolution. Real dates for the member
// ordering by year.

// catalogoFilmes is the sourceID -> filme map.
func catalogoFilmes() map[string]filme {
	return map[string]filme{
		"603": {
			sourceID: "603",
			tipo:     "movie",
			title: map[string]string{
				"en": "The Matrix", "pt-BR": "Matrix", "pt": "Matrix",
			},
			description: map[string]string{
				"en":    "A hacker learns the true nature of his reality.",
				"pt-BR": "Um hacker descobre a verdadeira natureza da sua realidade.",
				"pt":    "Um hacker descobre a verdadeira natureza da sua realidade.",
			},
			tags: map[string][]string{
				"en":    {"Science Fiction", "Action"},
				"pt-BR": {"Ficcao cientifica", "Acao"},
				"pt":    {"Ficcao cientifica", "Acao"},
			},
			originalTitle:    "The Matrix",
			originalLanguage: "en",
			releaseDate:      "1999-03-31",
			posterPath:       "/matrix.png",
			colecaoID:        "2344",
			// Known alternative (pt-BR aka) that does NOT beat the main title itself
			// for a search on "The Matrix" -- it proves the "the candidate already is
			// the best" path of PontuarComAlternativos (matched_title must come out
			// empty even with alternatives available and consulted).
			titulosAlternativos: []string{"Matrix"},
		},
		"604": {
			sourceID: "604",
			tipo:     "movie",
			title: map[string]string{
				"en": "The Matrix Reloaded", "pt-BR": "Matrix Reloaded", "pt": "Matrix Reloaded",
			},
			description: map[string]string{
				"en":    "Neo and the rebels fight to defend Zion.",
				"pt-BR": "Neo e os rebeldes lutam para defender Zion.",
				"pt":    "Neo e os rebeldes lutam para defender Zion.",
			},
			tags: map[string][]string{
				"en":    {"Science Fiction", "Action", "Adventure"},
				"pt-BR": {"Ficcao cientifica", "Acao", "Aventura"},
				"pt":    {"Ficcao cientifica", "Acao", "Aventura"},
			},
			originalTitle:    "The Matrix Reloaded",
			originalLanguage: "en",
			releaseDate:      "2003-05-15",
			posterPath:       "/matrix-reloaded.png",
			colecaoID:        "2344",
		},
		// SERIES (tipo "tv"): the baseline row of the tree import (BuscarSerieArvore
		// serves the seasons/episodes; this detail gives the series its
		// title/poster/original_language, like a movie). The id matches
		// fake.SerieFixtureID.
		"71663": {
			sourceID: "71663",
			tipo:     "tv",
			title: map[string]string{
				"en": "Springfield Tales", "pt-BR": "Contos de Springfield", "pt": "Contos de Springfield",
			},
			description: map[string]string{
				"en":    "A canned series fixture for offline series-import tests.",
				"pt-BR": "Uma serie de fixture para testes offline de import de series.",
				"pt":    "Uma serie de fixture para testes offline de import de series.",
			},
			tags: map[string][]string{
				"en": {"Animation", "Comedy"}, "pt-BR": {"Animacao", "Comedia"}, "pt": {"Animacao", "Comedia"},
			},
			originalTitle:    "Springfield Tales",
			originalLanguage: "en",
			releaseDate:      "1989-12-17",
			posterPath:       "/springfield.png",
		},
		// "605" is the fixture of side finding #2: a candidate whose MAIN title
		// (always the "en" one, see tituloPreferido) is very different from the
		// ALTERNATIVE/regional title that the provider only delivers through
		// BuscarTitulosAlternativos -- analogous to the real case "The Pink Panther"
		// vs "A Pantera Cor-De-Rosa" from the original real-world case. originalTitle carries the search
		// term (it lets Buscar find the candidate by substring, like any other
		// fixture) and deliberately matches the single item of titulosAlternativos:
		// only by comparing the file against the ALTERNATIVE (never against "The Rose
		// Prowler"/"O Perseguidor Rosa", the "official" titles) does the score rise
		// enough to prove PontuarComAlternativos.
		"605": {
			sourceID: "605",
			tipo:     "movie",
			title: map[string]string{
				"en": "The Rose Prowler", "pt-BR": "O Perseguidor Rosa", "pt": "O Perseguidor Rosa",
			},
			description: map[string]string{
				"en":    "A masked prowler stalks the city in a rose-pink coat.",
				"pt-BR": "Um perseguidor mascarado ronda a cidade com um sobretudo rosa.",
				"pt":    "Um perseguidor mascarado ronda a cidade com um sobretudo rosa.",
			},
			tags: map[string][]string{
				"en": {"Comedy", "Crime"}, "pt-BR": {"Comedia", "Crime"}, "pt": {"Comedia", "Crime"},
			},
			originalTitle:       "A Pantera Cor-De-Rosa",
			originalLanguage:    "pt-BR",
			releaseDate:         "2011-06-10",
			posterPath:          "/pink-prowler.png",
			titulosAlternativos: []string{"A Pantera Cor-De-Rosa"},
		},
	}
}

// catalogoColecoes is the id -> collection map. The parts come in chronological
// order (the server reorders them by year when it builds the entity members).
func catalogoColecoes() map[string]colecaoFix {
	return map[string]colecaoFix{
		"2344": {
			id:         "2344",
			name:       "The Matrix Collection",
			overview:   "A saga completa de Matrix.",
			posterPath: "/matrix-collection.png",
			parts:      []string{"603", "604"},
		},
	}
}
