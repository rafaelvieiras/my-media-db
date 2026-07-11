package fake

// catalogo.go guarda as FIXTURES do provider fake (briefing 0028): dado de
// compilação, imutável e determinístico. A franquia Matrix serve às jornadas E2E:
// import de filme, coleção com membros ordenados por ano, interno-first na busca e a
// não-destrutividade do enrich (título/tag editáveis vs. tags novas da fonte).
//
// Idiomas: cada traduzível traz "en" e "pt-BR" (mais o "pt" base), exercitando a
// resolução por idioma. Datas reais para a ordenação de membros por ano.

// catalogoFilmes é o mapa sourceID -> filme.
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
		// SÉRIE (tipo "tv"): a linha-base do import da árvore (BuscarSerieArvore serve as
		// temporadas/episódios; este detalhe dá título/pôster/original_language à série,
		// como um filme). O id casa fake.SerieFixtureID.
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
	}
}

// catalogoColecoes é o mapa id -> coleção. As partes vêm em ordem cronológica (o
// server reordena por ano ao montar os membros da entity).
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
