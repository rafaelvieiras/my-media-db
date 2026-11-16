// Package trailertema implements the Handler of the `trailer_tema_faltantes`
// job (ADR-0026): periodic, best-effort, it sweeps the whole library (the same
// eligible universe as midiasfaltantes, ListarEntitiesComMediaFilePresente)
// downloading missing trailer/theme music -- it reuses 100% of the download
// logic that already exists
// (server.BaixarTrailerEntity/BaixarTemaEntity, extracted from the manual
// buttons), and never picks a different candidate from the one already cached
// (media_external, source=tmdb/tvdb/themerrdb). Same pattern of small injected
// interfaces as internal/job/midiasfaltantes -- it avoids an import cycle (the
// Handler is built FROM INSIDE the server, via server.WithTrailerTemaJob, with
// *server.Server itself structurally satisfying the two interfaces below).
package trailertema

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// nomeJob is the Handler.Nome() -- it matches the slug seeded in job_definition
// by this job's migration.
const nomeJob = "trailer_tema_faltantes"

// EntityLister is the subset of *server.Server that the Handler consumes: the
// eligible universe (every entity with at least one 'present' media_file, the
// same criterion as midiasfaltantes.EntityLister). A small interface so a fake
// can be used in a test without a real database.
type EntityLister interface {
	ListarEntitiesComMediaFilePresente(ctx context.Context) ([]string, error)
}

// YtdlpDisponivel is checked ONCE at the start of Executar (not per entity) --
// it avoids hitting the yt-dlp binary (--version) once per library entity when
// it is simply unavailable. disponivel=false is not a job failure: Executar ends
// early with an informative message, without trying to download anything.
type YtdlpDisponivel interface {
	YtdlpDisponivel(ctx context.Context) (disponivel bool, detalhe string)
}

// TrailerTemaBaixador is the subset of *server.Server that the Handler consumes:
// download (best-effort) trailer/theme for ONE entity -- it reuses the SAME pure
// functions extracted from the manual buttons, without picking a different
// candidate from the one the UI/the manual button would already use.
// baixado=false, err=nil covers ALL the "nothing to do" cases (no candidate,
// already available locally, no destination, yt-dlp unavailable); err only
// carries a real failure (network, database).
type TrailerTemaBaixador interface {
	YtdlpDisponivel
	BaixarTrailerEntity(ctx context.Context, entityID string) (baixado bool, err error)
	BaixarTemaEntity(ctx context.Context, entityID string) (baixado bool, err error)
}

// Handler is the Handler of the trailer_tema_faltantes job.
type Handler struct {
	entities EntityLister
	baixador TrailerTemaBaixador
}

// New creates the Handler from the lister of eligible entities and the
// downloader -- injected by the caller (server.WithTrailerTemaJob), the same
// pattern as midiasfaltantes.New.
func New(entities EntityLister, baixador TrailerTemaBaixador) *Handler {
	return &Handler{entities: entities, baixador: baixador}
}

// Nome returns the stable slug of the job.
func (h *Handler) Nome() string { return nomeJob }

// Executar processes EVERY eligible entity in a single execution, best-effort
// per entity (an error on one does not abort the others -- same spirit as
// midias_faltantes), and summarizes how many trailers/themes were downloaded.
// The yt-dlp status is checked ONCE at the start (not per entity) -- if it is
// unavailable, the execution ends early with an informative message, without
// trying anything (and without counting it as a failure). An error is only
// returned if the listing of eligible entities itself fails (with no universe
// there is nothing to process).
func (h *Handler) Executar(ctx context.Context, _ json.RawMessage) (string, error) {
	if disponivel, detalhe := h.baixador.YtdlpDisponivel(ctx); !disponivel {
		msg := "yt-dlp indisponivel"
		if detalhe != "" {
			msg += ": " + detalhe
		}
		return msg + " -- nenhuma tentativa realizada", nil
	}

	ids, err := h.entities.ListarEntitiesComMediaFilePresente(ctx)
	if err != nil {
		return "", fmt.Errorf("listar entities elegiveis: %w", err)
	}

	var trailersBaixados, temasBaixados, falhas int
	for _, id := range ids {
		if baixou, err := h.baixador.BaixarTrailerEntity(ctx, id); err != nil {
			falhas++
			log.Printf("trailer_tema_faltantes: falha ao baixar trailer da entity %q (best-effort, ignorado): %v", id, err)
		} else if baixou {
			trailersBaixados++
		}

		if baixou, err := h.baixador.BaixarTemaEntity(ctx, id); err != nil {
			falhas++
			log.Printf("trailer_tema_faltantes: falha ao baixar tema da entity %q (best-effort, ignorado): %v", id, err)
		} else if baixou {
			temasBaixados++
		}
	}

	return montarMensagem(len(ids), trailersBaixados, temasBaixados, falhas), nil
}

// montarMensagem composes the final summary: how many eligible entities, how
// many trailers and how many themes downloaded in total.
func montarMensagem(total, trailersBaixados, temasBaixados, falhas int) string {
	base := fmt.Sprintf("%d entity(ies) elegível(is), %d trailer(s) baixado(s), %d tema(s) musical(is) baixado(s)",
		total, trailersBaixados, temasBaixados)
	if falhas > 0 {
		base = fmt.Sprintf("%s, %d falha(s)", base, falhas)
	}
	return base
}
