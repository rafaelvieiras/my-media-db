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
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/job"
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

// Titulador is the subset of *server.Server that the Handler consumes for the
// NARRATION ONLY: the title of one entity, so the queue's `current_item` shows
// "<Title> (1995)" instead of a bare UUID (issue #69). It is optional on
// purpose -- a nil Titulador (tests, direct call) makes the narration fall back
// to the id and changes nothing in the work.
//
// A dedicated small interface instead of widening EntityLister: the eligible
// universe comes from four different queries merged in the server, so a name
// there would mean a join in all four; a title-by-id read is one indexed lookup
// per entity, and this job is network-bound, not database-bound.
type Titulador interface {
	TituloDaEntity(ctx context.Context, entityID string) (string, error)
}

// passoUnico is the single step of this job: it processes one flat list, so the
// owner only needs "3 of 19018", never a step count.
const passoUnico = 1

// cadaQuantosLog is how many entities pass between heartbeat log lines. The
// queue snapshot answers "where am I now"; the log answers "where was it when it
// died" -- a run of this job lasts hours, and `docker logs` outlives the item.
const cadaQuantosLog = 100

// Handler is the Handler of the midias_faltantes job.
type Handler struct {
	entities       EntityLister
	materializador Materializador
	contador       ContadorMissing
	titulador      Titulador
}

// New creates the Handler from the lister of eligible entities, the
// materializer, the 'missing' media_file counter and the title lookup -- injected
// by the caller (server.WithMidiasFaltantesJob), the same pattern as
// libraryhealth.New.
func New(entities EntityLister, materializador Materializador, contador ContadorMissing, titulador Titulador) *Handler {
	return &Handler{entities: entities, materializador: materializador, contador: contador, titulador: titulador}
}

// Nome returns the stable slug of the job.
func (h *Handler) Nome() string { return nomeJob }

// The narration capability is OPTIONAL (ADR-0040): this assertion makes the
// discovery by type assertion in job.Manager a compile-time fact, so a signature
// drift breaks the build instead of silently going back to 0/0 (issue #69).
var _ job.HandlerComProgresso = (*Handler)(nil)

// Executar processes EVERY eligible entity in a single execution (no dedicated
// pagination -- the real cost is network/download, not iterating the database,
// the same reasoning that shaped this job) and summarizes how many were
// processed, how many arts were downloaded in total and how many NFOs were
// written. A failure ON ONE entity (network, disk) does not abort the others --
// best-effort, it only logs and moves on (same spirit as the rest of the job
// system); an error is only returned if the listing of eligible entities itself
// fails (with no universe there is nothing to process).
// Executar is the plain Handler entry point: without a queue behind it (tests,
// direct call) the narration is discarded and the pipeline is literally the same
// method.
func (h *Handler) Executar(ctx context.Context, cfg json.RawMessage) (string, error) {
	return h.executar(ctx, cfg, job.NovoProgresso(nil))
}

// ExecutarComProgresso implements job.HandlerComProgresso (ADR-0040, issue #69):
// the same pipeline as Executar, narrating the loop to the queue item so the
// owner can see how many entities are done out of how many, which one is being
// processed and which ones failed.
func (h *Handler) ExecutarComProgresso(ctx context.Context, cfg json.RawMessage, p *job.Progresso) (string, error) {
	return h.executar(ctx, cfg, p)
}

func (h *Handler) executar(ctx context.Context, _ json.RawMessage, p *job.Progresso) (string, error) {
	ids, err := h.entities.ListarEntitiesComMediaFilePresente(ctx)
	if err != nil {
		return "", fmt.Errorf("listar entities elegiveis: %w", err)
	}

	// The universe is known before the loop, so the total is honest from the first
	// second -- the whole point of issue #69: a 2h38 run used to report 0/0 until
	// it ended.
	p.Etapa(1, passoUnico, "materializar mídias faltantes")
	p.Contagem(0, len(ids))

	var processadas, artesTotal, nfosGravados, falhas, semPendencias int
	for i, id := range ids {
		rotulo := h.rotulo(ctx, id)
		p.Item(rotulo)
		artes, nfo, err := h.materializador.MaterializarMidiasFaltantes(ctx, id)
		if err != nil {
			falhas++
			p.Falhou(rotulo, err)
			log.Printf("midias_faltantes: falha ao materializar a entity %q (best-effort, ignorado): %v", id, err)
		} else {
			processadas++
			artesTotal += len(artes)
			if nfo {
				nfosGravados++
			}
			if !h.registrarDesfecho(p, rotulo, artes, nfo) {
				semPendencias++
			}
		}
		p.Contagem(i+1, len(ids))
		if (i+1)%cadaQuantosLog == 0 {
			log.Printf("midias_faltantes: %d/%d processadas, %d arte(s), %d NFO(s), %d falha(s)",
				i+1, len(ids), artesTotal, nfosGravados, falhas)
		}
	}

	// The entities that needed nothing are COUNTED, not written one row each (issue
	// #73). A trail row costs an INSERT plus the retention DELETE on the same table
	// -- ~30 ms of writing on the production array -- and 19.000 identical "nada
	// faltando no disco" rows say nothing that one number does not. The interesting
	// rows (art or NFO written, failures) keep their own line.
	if semPendencias > 0 {
		p.Ignorado(fmt.Sprintf("%d entity(ies)", semPendencias), "nada faltando no disco")
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

// registrarDesfecho tells the trail what happened to ONE entity that did not
// fail: something was written (ok, with the detail of what) or nothing was
// missing (returns false, and the CALLER counts it). The distinction matters when
// the run is read backwards -- "300 arts over 19.018 entities" only makes sense
// next to how many needed nothing.
//
// The "nothing was missing" case deliberately does NOT write a trail row: with
// 19.018 entities that would be 19.018 rows saying the same thing, each one an
// INSERT plus the retention DELETE on the production array (issue #73). The caller
// reports the total once, at the end.
func (h *Handler) registrarDesfecho(p *job.Progresso, rotulo string, artes []string, nfo bool) bool {
	if len(artes) == 0 && !nfo {
		return false
	}
	var detalhe string
	if len(artes) > 0 && nfo {
		detalhe = fmt.Sprintf("%s + nfo", joinArtes(artes))
	} else if len(artes) > 0 {
		detalhe = joinArtes(artes)
	} else {
		detalhe = "nfo"
	}
	p.OkDetalhe(rotulo, detalhe)
	return true
}

// joinArtes renders the materialized kinds as "poster, fanart" -- short, because
// the trail row is a label, not a report.
func joinArtes(artes []string) string {
	return strings.Join(artes, ", ")
}

// rotulo is what the owner reads as the current item: the entity title when the
// lookup works, the id otherwise. A failure here is never the run's failure --
// the narration must not break the work it observes.
func (h *Handler) rotulo(ctx context.Context, id string) string {
	if h.titulador == nil {
		return id
	}
	titulo, err := h.titulador.TituloDaEntity(ctx, id)
	if err != nil || titulo == "" {
		return id
	}
	return titulo
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
