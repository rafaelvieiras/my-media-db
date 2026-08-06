package server

// Este arquivo implementa o resolvedor de IDIOMA DE BUSCA por item (briefing
// 0084): dado um entity.ID, descobre a(s) biblioteca(s) de origem dos seus
// arquivos PRESENTES e devolve o idioma preferido delas, quando houver um
// único valor não-ambíguo -- senão cai no idioma PRINCIPAL global
// (app_settings.languages.primary), o mesmo comportamento de hoje. É o ponto
// único que substitui os `lang=""` cegos do auto-match (automatch.go), do
// enriquecimento retroativo (retroativo.go) e da prévia de match
// (match_preview.go), e também alimenta o default de EXIBIÇÃO de
// GET /entities/{id} quando `?lang=` não vem explícito (entities.go).

import "context"

// idiomaDeBusca resolve o idioma-alvo de busca/exibição para `entityID`:
//  1. cfg = idioma global (fallback final, sempre válido).
//  2. Lista os media_file da entity (já ordenados por created_at, id),
//     filtrando só os `state == "present"`.
//  3. Para cada arquivo present com library_source_id válido, lê a fonte e
//     coleta seu preferred_language (quando não-vazio).
//  4. Exatamente UM valor distinto entre as fontes ⇒ usa esse idioma.
//     ZERO (nenhuma fonte com preferência) ou DOIS-OU-MAIS valores
//     distintos (ambíguo, entity com arquivos de bibliotecas diferentes que
//     discordam) ⇒ usa cfg.Primary -- mesmo comportamento de hoje, sem tela
//     de decisão manual (briefing 0084 §6).
//
// Erro real de banco propaga; entity sem nenhum arquivo present cai direto em
// cfg.Primary (nunca erro só por isso).
func (s *Server) idiomaDeBusca(ctx context.Context, entityID string) (string, error) {
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return "", err
	}

	files, err := s.leitura().ListarMediaFilesPorEntity(ctx, entityID)
	if err != nil {
		return "", err
	}

	distintos := make(map[string]struct{}, 2)
	for _, f := range files {
		if f.State != "present" || !f.LibrarySourceID.Valid || f.LibrarySourceID.String == "" {
			continue
		}
		src, err := s.leitura().ObterLibrarySource(ctx, f.LibrarySourceID.String)
		if err != nil {
			// Fonte referenciada sumiu (raríssimo -- FK sem CASCADE aqui não
			// se aplica, mas degrada em silêncio em vez de propagar um erro
			// que não é real problema de dado desta entity).
			continue
		}
		if src.PreferredLanguage.Valid && src.PreferredLanguage.String != "" {
			distintos[src.PreferredLanguage.String] = struct{}{}
		}
	}

	if len(distintos) == 1 {
		for lang := range distintos {
			return lang, nil
		}
	}
	return cfg.Primary, nil
}
