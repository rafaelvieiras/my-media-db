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
			// Alternativo conhecido (aka pt-BR) que NÃO bate melhor que o próprio
			// título principal para uma busca em "The Matrix" -- prova o caminho
			// "candidato já é o melhor" de PontuarComAlternativos (matched_title
			// deve sair vazio mesmo com alternativos disponíveis e consultados).
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
		// "605" é a fixture do achado colateral #2 do briefing 0084: um candidato cujo
		// título PRINCIPAL (sempre o "en", ver tituloPreferido) é bem diferente do
		// título ALTERNATIVO/regional que o provedor só entrega via
		// BuscarTitulosAlternativos -- análogo ao caso real "The Pink Panther" vs "A
		// Pantera Cor-De-Rosa" citado no §1 do briefing. originalTitle carrega o termo
		// de busca (permite Buscar achar o candidato por substring, como qualquer
		// outra fixture) e coincide de propósito com o único item de
		// titulosAlternativos: só comparando o arquivo contra o ALTERNATIVO (nunca
		// contra "The Rose Prowler"/"O Perseguidor Rosa", os títulos "oficiais") o
		// score sobe o suficiente para provar PontuarComAlternativos.
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
