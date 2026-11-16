// Package midiasfaltantes implements the Handler of the `midias_faltantes`
// job (it closes the media materialization cycle at scale): periodic, it only
// READS the database (it never lists a directory -- that is the boundary with
// the scan), and materializes in the REAL library folder the "current winning"
// art (the same gallery aggregator, montarGruposArtwork) and the .nfo of every
// entity that already has a present file but does not have that media on disk
// yet. Same pattern of small injected interfaces as internal/job/libraryhealth
// -- it avoids an import cycle (the Handler is built FROM INSIDE the server,
// server.WithMidiasFaltantesJob, with *server.Server itself structurally
// satisfying the two interfaces below).
package midiasfaltantes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// nomeJob is the Handler.Nome() -- it matches the slug seeded in job_definition
// by migration 0016.
const nomeJob = "midias_faltantes"

// EntityLister is the subset of *server.Server that the Handler consumes: the
// eligible universe (every entity with at least one 'present' media_file). A
// small interface so a fake can be used in a test without a real database.
type EntityLister interface {
	ListarEntitiesComMediaFilePresente(ctx context.Context) ([]string, error)
}

// Materializador is the subset of *server.Server that the Handler consumes:
// materialize, for ONE entity, the winning art (by kind) that was still missing
// on disk plus the NFO when absent -- it reuses the SAME mechanisms as the media
// materialization job (materializarAsset/gravarNFO) and the gallery aggregator
// (montarGruposArtwork), and never picks a new candidate. artesBaixadas lists
// the kinds effectively materialized in this call (empty when nothing was
// missing or there was no candidate); nfoGravado indicates whether the .nfo was
// (re)written (only when it was absent).
type Materializador interface {
	MaterializarMidiasFaltantes(ctx context.Context, entityID string) (artesBaixadas []string, nfoGravado bool, err error)
}

// ContadorMissing is the subset of *server.Server that the Handler consumes
// (the media_file 'missing' case): how many media_file are in the 'missing'
// state at scan time -- a sign of instability of the remote mount
// (rclone/gdrive) that may have silently excluded some entity from the eligible
// universe of this execution (ListarEntitiesComMediaFilePresente requires
// state='present'). A small interface kept separate from EntityLister (same
// spirit as the others in this package): the count is only informative, it never
// decides what gets processed.
type ContadorMissing interface {
	ContarMediaFilesEmEstadoMissing(ctx context.Context) (int64, error)
}

// Handler is the Handler of the midias_faltantes job.
type Handler struct {
	entities       EntityLister
	materializador Materializador
	contador       ContadorMissing
}

// New creates the Handler from the lister of eligible entities, the
// materializer and the 'missing' media_file counter -- injected by the caller
// (server.WithMidiasFaltantesJob), the same pattern as libraryhealth.New.
func New(entities EntityLister, materializador Materializador, contador ContadorMissing) *Handler {
	return &Handler{entities: entities, materializador: materializador, contador: contador}
}

// Nome returns the stable slug of the job.
func (h *Handler) Nome() string { return nomeJob }

// Executar processes EVERY eligible entity in a single execution (no dedicated
// pagination -- the real cost is network/download, not iterating the database,
// the same reasoning that shaped this job) and summarizes how many were
// processed, how many arts were downloaded in total and how many NFOs were
// written. A failure ON ONE entity (network, disk) does not abort the others --
// best-effort, it only logs and moves on (same spirit as the rest of the job
// system); an error is only returned if the listing of eligible entities itself
// fails (with no universe there is nothing to process).
func (h *Handler) Executar(ctx context.Context, _ json.RawMessage) (string, error) {
	ids, err := h.entities.ListarEntitiesComMediaFilePresente(ctx)
	if err != nil {
		return "", fmt.Errorf("listar entities elegiveis: %w", err)
	}

	var processadas, artesTotal, nfosGravados, falhas int
	for _, id := range ids {
		artes, nfo, err := h.materializador.MaterializarMidiasFaltantes(ctx, id)
		if err != nil {
			falhas++
			log.Printf("midias_faltantes: falha ao materializar a entity %q (best-effort, ignorado): %v", id, err)
			continue
		}
		processadas++
		artesTotal += len(artes)
		if nfo {
			nfosGravados++
		}
	}

	// The media_file 'missing' case: best-effort count -- a failure here (database
	// unavailable in the middle of the execution) does not invalidate the
	// processing already done above, it only omits the datum from the summary
	// (missing=-1 signals "not determined" to montarMensagem).
	missing := int64(-1)
	if h.contador != nil {
		if n, cerr := h.contador.ContarMediaFilesEmEstadoMissing(ctx); cerr != nil {
			log.Printf("midias_faltantes: falha ao contar media_file 'missing' (best-effort, ignorado): %v", cerr)
		} else {
			missing = n
		}
	}

	return montarMensagem(len(ids), processadas, artesTotal, nfosGravados, falhas, missing), nil
}

// montarMensagem composes the final summary: how many eligible entities, how
// many processed successfully (with the total of failures, if any), how many
// arts materialized in total and how many NFOs written. When `missing > 0`, it
// appends a note that there were media_file outside the 'present' state at scan
// time -- a sign that the eligible universe of this execution may have been left
// incomplete by a race with another job (e.g. saude_biblioteca) over the same
// remote mount. missing < 0 (count not determined) or == 0 appends nothing.
func montarMensagem(total, processadas, artesTotal, nfosGravados, falhas int, missing int64) string {
	base := fmt.Sprintf("%d entity(ies) elegível(is), %d processada(s), %d arte(s) materializada(s), %d NFO(s) gravado(s)",
		total, processadas, artesTotal, nfosGravados)
	if falhas > 0 {
		base = fmt.Sprintf("%s, %d falha(s)", base, falhas)
	}
	if missing > 0 {
		base = fmt.Sprintf("%s, %d media_file(s) em estado 'missing' no momento do scan (pode ter excluído alguma entity deste ciclo)", base, missing)
	}
	return base
}
