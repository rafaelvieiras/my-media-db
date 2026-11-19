package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements the match candidate PREVIEW and the supervised application: before
// this feature, "Confirmar" in the Review queue blindly accepted the scanner guess without
// EVER searching/showing a real candidate, and the auto-match only searched AFTER the click,
// with no visible detail when the score did not reach LimiarAutoMatch. The two handlers here
// reuse IN FULL the search+scoring of the auto-match
// (tituloAnoDoArquivo/buscarLocal/buscarExterno/melhorCandidatoLocal/
// melhorCandidatoExterno, automatch.go) -- the difference is that the finders no longer cut
// by LimiarAutoMatch: who decides to apply the cut now is EACH caller (tentarAutoMatch cuts;
// the preview here never cuts, it only flags meets_auto_threshold).

// matchPreviewTargetDTO is the candidate shown by the preview -- local OR external, never
// both. `poster_url`: for a local one, the proxy route of the entity itself (the same as
// EntityCard.poster_url, which comes for free in entityCardDTO/cardDTO); for an external one,
// the RAW URL of the provider (already free in provider.ResultadoExterno.PosterURL) -- the
// frontend routes it through the existing image proxy (arteSrc). No field here requires an
// extra network call beyond the search already made.
type matchPreviewTargetDTO struct {
	Title     string  `json:"title"`
	Year      *int    `json:"year"`
	PosterURL *string `json:"poster_url"`
	LocalID   *string `json:"local_id"`
	Source    *string `json:"source"`
	SourceID  *string `json:"source_id"`
	// MatchedTitle is the title (main or alternative/translated) that actually produced the
	// score shown -- omitted (nil) when the candidate is local, or when the winner is the
	// main title itself (no alternative title beat it).
	MatchedTitle *string `json:"matched_title,omitempty"`
}

// matchPreviewDTO is the response of GET /entities/{id}/match-preview -- ALWAYS 200 (even
// without a candidate: a valid result, not an error, the same convention as the auto-match).
// `meets_auto_threshold` is purely informative: it never filters the result nor prevents
// confirm-candidate.
type matchPreviewDTO struct {
	Found              bool                   `json:"found"`
	Method             string                 `json:"method"` // "local" | "external" | "none"
	Score              float64                `json:"score"`
	MeetsAutoThreshold bool                   `json:"meets_auto_threshold"`
	Target             *matchPreviewTargetDTO `json:"target"`
}

// matchPreviewNone is the neutral result: no candidate in any source (or there was nothing
// to search -- no file/derivable title).
var matchPreviewNone = matchPreviewDTO{Found: false, Method: "none", Score: 0, MeetsAutoThreshold: false, Target: nil}

// confirmCandidateBody is the body of POST /entities/{id}/confirm-candidate: two formats
// depending on `method` -- "local" requires LocalID; "external" requires Source+SourceID.
// A body that does not match the method answers 422.
type confirmCandidateBody struct {
	Method   string `json:"method"`
	LocalID  string `json:"local_id"`
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
}

// matchPreview handles GET /api/v1/entities/{id}/match-preview: dry-run, read-only, with NO
// side-effect. 404 if the entity does not exist.
func (s *Server) matchPreview(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	preview, err := s.montarMatchPreview(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

// montarMatchPreview derives title/year from the file and searches local-first, then external
// if local found nothing -- the SAME order AND the same search variants as tentarAutoMatch
// (automatch.go), but using the finders WITHOUT the LimiarAutoMatch cut directly (no
// duplication of the ranking logic). err is only a real database/infra error; "no
// candidate" is never err.
func (s *Server) montarMatchPreview(ctx context.Context, ent sqlcgen.Entity) (matchPreviewDTO, error) {
	variantes, err := s.variantesTituloAno(ctx, ent)
	if err != nil {
		return matchPreviewDTO{}, err
	}
	if len(variantes) == 0 {
		return matchPreviewNone, nil
	}

	// Search language: the preview must reflect the SAME language the auto-match would
	// actually use -- the same resolver as the other two flows
	// (automatch.go/retroativo.go).
	lang, err := s.idiomaDeBusca(ctx, ent.ID)
	if err != nil {
		return matchPreviewDTO{}, err
	}

	// 1) Local-first: the same search as the auto-match.
	alvo, achou, err := s.melhorCandidatoLocalEntre(ctx, ent, variantes)
	if err != nil {
		return matchPreviewDTO{}, err
	}
	if achou {
		localID := alvo.ID
		return matchPreviewDTO{
			Found: true, Method: "local", Score: alvo.Score,
			MeetsAutoThreshold: alvo.Score >= LimiarAutoMatch,
			Target: &matchPreviewTargetDTO{
				Title: alvo.Title, Year: alvo.Year, PosterURL: alvo.PosterURL,
				LocalID: &localID,
			},
		}, nil
	}

	// 2) External: only when local found nothing. Without enrich on (or without a provider
	// capable of the type), the preview degrades to "none" -- the same guard as
	// tentarAutoMatch.
	if s.enrich == nil {
		return matchPreviewNone, nil
	}
	buscado, err := s.melhorCandidatoExternoEntre(ctx, ent, variantes, lang)
	if err != nil {
		return matchPreviewDTO{}, err
	}
	if !buscado.Disponivel || !buscado.Achou {
		return matchPreviewNone, nil
	}
	cand, score, tituloVencedor := buscado.Candidato, buscado.Score, buscado.TituloVencedor

	var anoAlvo *int
	if a := anoDeStringPtr(cand.Year); a > 0 {
		anoAlvo = &a
	}
	var matchedTitle *string
	if tituloVencedor != "" {
		matchedTitle = &tituloVencedor
	}
	source, sourceID := cand.Source, cand.SourceID
	return matchPreviewDTO{
		Found: true, Method: "external", Score: score,
		MeetsAutoThreshold: score >= LimiarAutoMatch,
		Target: &matchPreviewTargetDTO{
			Title: cand.Title, Year: anoAlvo, PosterURL: cand.PosterURL,
			Source: &source, SourceID: &sourceID, MatchedTitle: matchedTitle,
		},
	}, nil
}

// confirmCandidate handles POST /api/v1/entities/{id}/confirm-candidate: it applies the
// candidate the preview showed, WITHOUT checking LimiarAutoMatch (an informed human
// decision, not an automatic cut). 404 if the entity does not exist; 422 if the body does not
// match the `method`. Branch `local`: it merges the queue entity (`id`, loser) with
// `local_id` (survivor, the chosen candidate) through fundirEntities -- no collision scenario
// here, both sides were explicitly chosen by the owner. Branch `external`: it reuses
// aplicarIdentidadeExterna (the same sequence as correctEntity, review.go) with the REAL
// ResponseWriter of the request -- a collision becomes a genuine 409.
func (s *Server) confirmCandidate(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	var body confirmCandidateBody
	if ok := decodeOpcional(w, r, &body); !ok {
		return
	}

	switch body.Method {
	case "local":
		if body.LocalID == "" {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body", "local_id é obrigatório para method=local")
			return
		}
		// The survivor is always the local_id of the body (the chosen candidate); the loser
		// is always the queue item.
		if err := s.fundirEntities(ctx, body.LocalID, id); err != nil {
			respondErroInterno(w)
			return
		}
		s.responderResolvida(w, ctx, body.LocalID, http.StatusOK)
	case "external":
		if body.Source == "" || body.SourceID == "" {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body", "source e source_id são obrigatórios para method=external")
			return
		}
		if s.enrichIndisponivel(w) {
			return
		}
		s.aplicarIdentidadeExterna(w, ctx, id, ent.Type, body.Source, body.SourceID)
	default:
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "method deve ser 'local' ou 'external'")
	}
}
