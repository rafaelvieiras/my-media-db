package server

// This file implements the per-item SEARCH LANGUAGE resolver: given an entity.ID, it
// finds the origin library(ies) of its PRESENT files and returns their preferred
// language, when there is a single non-ambiguous value -- otherwise it falls back to the
// global PRIMARY language (app_settings.languages.primary), the same behavior as before.
// It is the single point that replaces the blind `lang=""` of the auto-match
// (automatch.go), of the retroactive enrichment (retroativo.go) and of the match preview
// (match_preview.go), and it also feeds the DISPLAY default of GET /entities/{id} when
// `?lang=` does not come explicitly (entities.go).

import "context"

// idiomaDeBusca resolves the target search/display language for `entityID`:
//  1. cfg = global language (final fallback, always valid).
//  2. Lists the entity's media_file rows (already ordered by created_at, id),
//     filtering only the `state == "present"` ones.
//  3. For each present file with a valid library_source_id, it reads the source and
//     collects its preferred_language (when non-empty).
//  4. Exactly ONE distinct value among the sources ⇒ it uses that language.
//     ZERO (no source with a preference) or TWO-OR-MORE distinct values
//     (ambiguous, an entity with files from different libraries that disagree) ⇒ it uses
//     cfg.Primary -- the same behavior as before, with no manual decision screen.
//
// A real database error propagates; an entity without any present file falls straight back
// to cfg.Primary (never an error just for that).
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
			// The referenced source vanished (extremely rare -- the FK without CASCADE
			// does not apply here, but it degrades silently instead of propagating an
			// error that is not a real data problem of this entity).
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
