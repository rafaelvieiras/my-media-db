package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements the RETROACTIVE EXTERNAL ENRICHMENT, an extension of the
// saude_biblioteca job: it sweeps `cataloged` entities (or wanted/unreleased) with the
// scanner's confidence 70 (title+year parsed from the file, WITHOUT an embedded external
// id) -- the EXACT complement of ListarRevisao restricted to pure local items with a file
// on disk. Those items NEVER go through any external provider (they do not fall into the
// Review queue, so the auto-match never reaches them) -- they stay "invisible" forever,
// with a pretty title/year but without a poster/synopsis/cast. This file closes that gap:
// local-first search and then external, in a small throttled batch; it applies on its own
// with high confidence (same threshold), flags the Review queue when it found something
// plausible but not reliable enough, or only records the attempt (throttle) when it found
// nothing -- fully reusing the search/scoring engine and the external application sequence
// (aplicarCandidatoExterno, extracted from tentarAutoMatch in automatch.go) that already
// exist.

const (
	// limiteEnriquecimentoRetroativoPorExecucao is the ceiling of items CHECKED
	// (not merely listed) per job run -- throttling of cost/rate-limit of the
	// external provider in a library that may have a large backlog on the first
	// time this runs.
	limiteEnriquecimentoRetroativoPorExecucao = 15

	// limiarSinalizacaoRetroativa is the plausibility floor to write the
	// item into the Review queue (via AtualizarConfidencePorEntity): below
	// that, the candidate is noise (titles that look alike by coincidence) and it is not
	// worth interrupting the owner -- only the attempt is recorded (throttle).
	// A NEW decision of this step: the batch auto-match only needed ONE cutoff (apply
	// or not, the item was already visible anyway); here "not applying" alone would leave
	// the item invisible forever, hence the extra floor just for the "worth flagging"
	// decision.
	limiarSinalizacaoRetroativa = 50.0
)

// carimboRetroativoFormato is the Go layout of the timestamp written by
// AtualizarTentativaMatchExterno (strftime('%Y-%m-%dT%H:%M:%fZ', 'now') in
// SQL -- 3 millisecond digits), used to compute the `desde` cutoff of the
// throttle in Go. Same layout used by scanner.carimboFormato/
// health_system.go (ComputedAt), without nanosecond precision (%f of
// SQLite truncates at milliseconds).
const carimboRetroativoFormato = "2006-01-02T15:04:05.000Z"

// EnriquecerRetroativo sweeps the batch of eligible items (pure local, never
// checked against any provider, respecting the per-item throttle) and
// tries to enrich each one -- applying on its own, flagging to Review or
// only recording the attempt. It satisfies libraryhealth.Matcher (called by the
// saude_biblioteca job, AFTER the existing batch
// auto-match). err is ONLY a real database error in the LISTING; a failure in an
// individual item is absorbed (best-effort, it logs and moves on).
func (s *Server) EnriquecerRetroativo(ctx context.Context) (aplicados, sinalizados int, err error) {
	// Without a configured provider (or without assetsDir -- same guard as
	// tentarAutoMatch), NO real attempt can happen: it returns without
	// touching the database, so as not to stamp last_external_match_attempt_at on
	// items that were not actually checked -- they keep MAXIMUM
	// priority (never tried) as soon as a provider exists again, instead
	// of waiting 30 days of throttle for a period in which there was no
	// provider at all.
	if s.enrich == nil {
		return 0, 0, nil
	}

	// Configurable throttle: GLOBAL interval coming from
	// app_settings (default retryDaysPadrao=15 when never configured); the
	// OPTIONAL per-library override is resolved straight in SQL, inside the
	// query itself (COALESCE per library_source, LOWEST value in ambiguity
	// -- see ListarElegiveisEnriquecimentoRetroativo).
	cfgRetry, err := s.obterRetroactiveEnrichmentSettings(ctx)
	if err != nil {
		return 0, 0, err
	}
	rows, err := s.leitura().ListarElegiveisEnriquecimentoRetroativo(ctx, sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
		Limiar:     sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
		DiasGlobal: int64(cfgRetry.RetryDays),
		Limite:     limiteEnriquecimentoRetroativoPorExecucao,
	})
	if err != nil {
		return 0, 0, err
	}

	for _, row := range rows {
		ent, err := s.leitura().ObterEntity(ctx, row.ID)
		if err != nil {
			// Vanished between the listing and the processing (concurrent purge) --
			// skip it, do not take the batch down (same pattern as autoMatchLote).
			continue
		}

		resultado, _, _, tentarErr := s.tentarEnriquecerOuSinalizar(ctx, ent)
		if tentarErr != nil {
			log.Printf("saude_biblioteca: enriquecimento retroativo falhou para %s (best-effort, ignorado): %v", ent.ID, tentarErr)
		}

		// Always STAMP the attempt at the end (even if tentarErr != nil): the
		// item was actually checked in this run -- best-effort, an error
		// here is only logged, it aborts neither the item nor the batch.
		// carimbarTentativaExterna is the SAME helper reused by the individual
		// enrichMissing endpoint, without duplicating the sqlc call.
		if werr := s.carimbarTentativaExterna(ctx, ent.ID); werr != nil {
			log.Printf("saude_biblioteca: gravar tentativa de enriquecimento retroativo falhou para %s: %v", ent.ID, werr)
		}

		switch resultado {
		case "applied":
			aplicados++
		case "flagged":
			sinalizados++
		}
	}
	return aplicados, sinalizados, nil
}

// diasReentativaRetroativa resolves the throttle interval (in days)
// applicable to an entity: it looks at the library of the FIRST
// 'present' media_file (same tie-break criterion as idiomaDeBusca --
// created_at ASC, id ASC, inherited from the ordering of
// ListarMediaFilesPorEntity); with an override defined in some origin library,
// it uses the LOWEST value among them; with no override in ANY (or with no
// media_file at all), it falls back to the global one. Unlike idiomaDeBusca, entities
// with media_files from libraries with DIFFERENT overrides use the LOWEST value
// (never the global one) -- a deliberate decision: here ambiguity only affects WHEN
// to retry, never the search result, so erring towards "retry
// sooner" is safe. It is not called by
// EnriquecerRetroativo (the batch query already resolves the same rule in SQL);
// it exists for informative decisions/unit tests and for eventual future
// use (e.g.: "next attempt predicted" per item).
func (s *Server) diasReentativaRetroativa(ctx context.Context, entityID string) (int, error) {
	cfgRetry, err := s.obterRetroactiveEnrichmentSettings(ctx)
	if err != nil {
		return 0, err
	}

	files, err := s.leitura().ListarMediaFilesPorEntity(ctx, entityID)
	if err != nil {
		return 0, err
	}

	menor := -1
	for _, f := range files {
		if f.State != "present" || !f.LibrarySourceID.Valid || f.LibrarySourceID.String == "" {
			continue
		}
		src, err := s.leitura().ObterLibrarySource(ctx, f.LibrarySourceID.String)
		if err != nil {
			// The referenced source vanished (very rare): it degrades silently, same
			// pattern as idiomaDeBusca.
			continue
		}
		if !src.RetroactiveEnrichmentDays.Valid {
			continue
		}
		v := int(src.RetroactiveEnrichmentDays.Int64)
		if menor == -1 || v < menor {
			menor = v
		}
	}

	if menor == -1 {
		return cfgRetry.RetryDays, nil
	}
	return menor, nil
}

// termoBuscaRetroativo derives the search term (title/year) for ONE eligible
// entity, dispatched by type: `artista` never has its OWN
// media_file (ADR-0027, only the track links directly) -- tituloAnoDoArquivo
// (automatch.go) would always return ok=false for that type, since it depends on
// a file on disk. An artist is born with a readable title (folder name
// recognized by the music scanner), so it uses ent.Title directly, without a year
// (0 -- matching.ajustarPorAno already degrades without bonus nor discard when either
// year is <= 0, existing behavior, now exercised with real music data here).
// Any other type (movie/tv, and any future top type with its own file) reuses
// tituloAnoDoArquivo as it always has.
func (s *Server) termoBuscaRetroativo(ctx context.Context, ent sqlcgen.Entity) (titulo string, ano int, ok bool, err error) {
	if ent.Type == tipoEntArtista {
		return ent.Title, 0, ent.Title != "", nil
	}
	return s.tituloAnoDoArquivo(ctx, ent)
}

// tentarEnriquecerOuSinalizar tries to enrich ONE eligible entity: it derives
// title/year from the file, searches local-first and then external, using the BEST
// score even below the threshold (it needs the value to decide "flag").
// melhorCandidatoLocal/melhorCandidatoExterno
// (automatch.go) already return the highest-scored one WITHOUT the LimiarAutoMatch
// cutoff built in -- the caller decides whether to apply -- so this file reuses both
// directly, without duplicating the ranking logic. resultado is
// "applied"/"flagged"/"none"; method (only in "applied", "local"/"external") and
// score (in "applied"/"flagged") feed the public DTO of enrichMissing
// -- EnriquecerRetroativo (the batch job) ignores both,
// interested only in resultado for the aggregated counts. err is ONLY a real
// database/infrastructure error.
func (s *Server) tentarEnriquecerOuSinalizar(ctx context.Context, ent sqlcgen.Entity) (resultado, method string, score float64, err error) {
	titulo, ano, ok, err := s.termoBuscaRetroativo(ctx, ent)
	if err != nil {
		return "", "", 0, err
	}
	if !ok {
		return "none", "", 0, nil
	}

	// Search language: same resolver as the auto-match -- the
	// origin library may have a preferred language; without one, it falls back to the
	// global PRIMARY language (never the blind "" of before).
	lang, err := s.idiomaDeBusca(ctx, ent.ID)
	if err != nil {
		return "", "", 0, err
	}

	// 1) Local-first (same order as tentarAutoMatch): it never duplicates an entity
	// already cataloged.
	candidatosLocais, err := s.buscarLocal(ctx, titulo, ent.Type, nil)
	if err != nil {
		return "", "", 0, err
	}
	melhorLocal, achouLocal := s.melhorCandidatoLocal(ctx, candidatosLocais, ent.ID, titulo, ano)
	if achouLocal && melhorLocal.Score >= LimiarAutoMatch {
		if err := s.fundirEntities(ctx, melhorLocal.ID, ent.ID); err != nil {
			return "", "", 0, err
		}
		return "applied", "local", melhorLocal.Score, nil
	}

	// 2) External: only when local did not hit the application threshold.
	melhorExternoScore := -1.0
	achouExterno := false
	if s.enrich != nil {
		externos, disponivel := s.buscarExterno(ctx, titulo, ent.Type, lang)
		if disponivel {
			cand, scoreExterno, _, achou := s.melhorCandidatoExterno(ctx, externos, titulo, ano)
			if achou {
				melhorExternoScore, achouExterno = scoreExterno, true
				if scoreExterno >= LimiarAutoMatch {
					aplicado, err := s.aplicarCandidatoExterno(ctx, ent, cand, scoreExterno)
					if err != nil {
						return "", "", 0, err
					}
					if aplicado.Resolved {
						return "applied", "external", scoreExterno, nil
					}
					// aplicarCandidatoExterno degraded (collision with
					// the owner's investment in the other entity, or the source
					// failed to deliver the detail): there is no way to apply it
					// blindly -- it falls back to the flagging decision below, with the
					// same score already calculated (the candidate remains
					// plausible, it just could not be applied on its own).
				}
			}
		}
	}

	// 3) No strong candidate in any source (or external application
	// degraded above): decide to flag by the HIGHEST score between local and
	// external, already calculated.
	melhorGeral := -1.0
	if achouLocal {
		melhorGeral = melhorLocal.Score
	}
	if achouExterno && melhorExternoScore > melhorGeral {
		melhorGeral = melhorExternoScore
	}
	if melhorGeral < limiarSinalizacaoRetroativa {
		return "none", "", 0, nil
	}

	// Ceiling of the value WRITTEN to the database: without it, a candidate scored
	// between limiarBaixaConfianca and LimiarAutoMatch (it found something good, but not
	// good enough for the 92 of the auto-match) would be written with a
	// match_confidence that does NOT match `< limiar` -- the item would stay
	// invisible, the opposite of what was intended. The score
	// RETURNED to the caller (for the enrichMissing DTO) is the real melhorGeral,
	// not the ceiling -- the owner wants to see the actual score of the candidate,
	// not the value artificially lowered just for the Review queue.
	valorGravado := melhorGeral
	if valorGravado > limiarBaixaConfianca-1 {
		valorGravado = limiarBaixaConfianca - 1
	}
	if err := sqlcgen.New(s.db.Write).AtualizarConfidencePorEntity(ctx, sqlcgen.AtualizarConfidencePorEntityParams{
		MatchConfidence: sql.NullFloat64{Float64: valorGravado, Valid: true},
		EntityID:        ent.ID,
	}); err != nil {
		return "", "", 0, err
	}
	return "flagged", "", melhorGeral, nil
}

// carimbarTentativaExterna writes last_external_match_attempt_at for `id` via
// AtualizarTentativaMatchExterno -- extracted from the previously inline body of
// EnriquecerRetroativo to be reused WITHOUT duplicating the
// sqlc call by the individual enrichMissing endpoint (which stamps
// unconditionally, even ignoring the batch job throttle).
func (s *Server) carimbarTentativaExterna(ctx context.Context, id string) error {
	return sqlcgen.New(s.db.Write).AtualizarTentativaMatchExterno(ctx, id)
}

// enrichMissingResultDTO is the response of POST /entities/{id}/enrich-missing
// (openapi.yaml EnrichMissingResult) -- it mirrors the `resultado`
// of tentarEnriquecerOuSinalizar, reused as the public DTO of this
// endpoint (no new vocabulary). Method is only present when
// outcome=="applied"; Score is present when outcome is "applied" or
// "flagged"; neither in "none".
type enrichMissingResultDTO struct {
	Outcome string   `json:"outcome"`
	Method  *string  `json:"method,omitempty"`
	Score   *float64 `json:"score,omitempty"`
}

// enrichMissing handles POST /api/v1/entities/{id}/enrich-missing:
// it triggers ON DEMAND, for ONE item open in the detail, the SAME
// search/application/flagging orchestration of the health job
// (tentarEnriquecerOuSinalizar) -- WITHOUT any eligibility guard
// by status/throttle (an explicit, punctual action of the owner, unlike the
// batch sweep; the justification for not replicating the job filter here is
// documented). It always stamps
// last_external_match_attempt_at at the end (even on outcome=="none"), so the
// NEXT job batch does not compete for the item the owner just resolved
// manually. 404 if the entity does not exist; 200 always after that (never
// 5xx just for "no candidate found" -- same pattern as /auto-match); 500 only on a
// real database/infrastructure error (in the search OR in the stamping).
func (s *Server) enrichMissing(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	resultado, method, score, tentarErr := s.tentarEnriquecerOuSinalizar(ctx, ent)
	// Always STAMPED, even if tentarErr != nil -- the item was actually
	// checked in this call (same semantics as EnriquecerRetroativo, only
	// here a database error in ANY of the two steps becomes 500 (it is
	// not a batch best-effort -- it is an individual HTTP call).
	carimbarErr := s.carimbarTentativaExterna(ctx, ent.ID)
	if tentarErr != nil || carimbarErr != nil {
		respondErroInterno(w)
		return
	}

	dto := enrichMissingResultDTO{Outcome: resultado}
	switch resultado {
	case "applied":
		m, sc := method, score
		dto.Method, dto.Score = &m, &sc
	case "flagged":
		sc := score
		dto.Score = &sc
	}
	writeJSON(w, http.StatusOK, dto)
}
