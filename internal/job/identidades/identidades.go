// Package identidades implements the Handler of the `sugerir_identidades` job: it walks the
// series the post-scan already flagged as "the tree does not fit the files" and asks the same
// scoring the auto-import uses (title in any language, the folder's year, the size of the
// folder against the tree) WHO ELSE the folder could be.
//
// It NEVER changes an identity. The owner's decision (21/nov/2026) was the middle ground
// between "the alert says something is wrong" and "something changes without me": the job
// rewrites the hint with the candidate it found, so the item screen shows
// "suggested: O Clube do Terror (1992) - 91 episodes, fits 78/78" and one click applies it.
// The hint is still a hint, so ADR-0038 holds.
//
// Same pattern of small injected interfaces as internal/job/midiasfaltantes: it avoids an
// import cycle because the Handler is built from INSIDE the server
// (server.WithIdentidadesJob), with *server.Server structurally satisfying both interfaces
// below.
package identidades

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/job"
)

// nomeJob is the Handler.Nome() -- it matches the slug seeded in job_definition by
// migration 0031.
const nomeJob = "sugerir_identidades"

// Suspeita is one series to re-examine: the id and a label for the narration.
type Suspeita struct {
	ID     string
	Titulo string
}

// Alvos is the subset of *server.Server that the Handler consumes: the series that carry an
// active "the tree does not fit the files" alert. It is the ALERT table, not the post-scan
// census -- the census lists only series WITHOUT a row, so it answers "nobody" for exactly the
// series that already have an alert (measured on the first production run: "0 series
// signalled" while 14 alerts were on screen).
type Alvos interface {
	ListarIdentidadesSuspeitas(ctx context.Context) ([]Suspeita, error)
}

// Resultado is what happened with one series. Vazio means "nothing to say" (the current
// identity is already the best candidate, or no candidate reached the cut); Sucesso means the
// hint was rewritten with a candidate the owner can apply.
type Resultado struct {
	// Sucesso is true when a DIFFERENT identity that fits the files was written into the hint.
	Sucesso bool
	// Descricao is the one-line summary shown in the queue trail.
	Descricao string
}

// Sugestor is the subset of *server.Server that the Handler consumes: re-score ONE series and
// rewrite its hint. It never applies anything.
type Sugestor interface {
	SugerirIdentidadeDaSerie(ctx context.Context, entityID string) (Resultado, error)
}

// Handler is the Handler of the sugerir_identidades job.
type Handler struct {
	alvos  Alvos
	sugere Sugestor
}

// New creates the Handler from the lister of suspect series and the suggester.
func New(alvos Alvos, sugere Sugestor) *Handler {
	return &Handler{alvos: alvos, sugere: sugere}
}

// Nome returns the stable slug of the job.
func (h *Handler) Nome() string { return nomeJob }

// The narration capability is OPTIONAL (ADR-0040): this assertion makes the discovery by type
// assertion in job.Manager a compile-time fact.
var _ job.HandlerComProgresso = (*Handler)(nil)

// Executar is the plain entry point: without a queue behind it the narration is discarded and
// the pipeline is literally the same method.
func (h *Handler) Executar(ctx context.Context, cfg json.RawMessage) (string, error) {
	return h.executar(ctx, cfg, job.NovoProgresso(nil))
}

// ExecutarComProgresso implements job.HandlerComProgresso.
func (h *Handler) ExecutarComProgresso(ctx context.Context, cfg json.RawMessage, p *job.Progresso) (string, error) {
	return h.executar(ctx, cfg, p)
}

func (h *Handler) executar(ctx context.Context, _ json.RawMessage, p *job.Progresso) (string, error) {
	alvos, err := h.alvos.ListarIdentidadesSuspeitas(ctx)
	if err != nil {
		return "", fmt.Errorf("listar identidades suspeitas: %w", err)
	}

	p.Etapa(1, 1, "sugerir identidade para as séries sinalizadas")
	p.Contagem(0, len(alvos))

	var sugeridas, semNovidade, falhas int
	for i, alvo := range alvos {
		if ctx.Err() != nil {
			break
		}
		rotulo := alvo.Titulo
		if rotulo == "" {
			rotulo = alvo.ID
		}
		p.Item(rotulo)
		res, err := h.sugere.SugerirIdentidadeDaSerie(ctx, alvo.ID)
		switch {
		case err != nil:
			falhas++
			p.Falhou(rotulo, err)
			log.Printf("sugerir_identidades: %q (%s) falhou (best-effort, segue): %v", rotulo, alvo.ID, err)
		case res.Sucesso:
			sugeridas++
			p.OkDetalhe(rotulo, res.Descricao)
		default:
			semNovidade++
			p.Ignorado(rotulo, res.Descricao)
		}
		p.Contagem(i+1, len(alvos))
	}

	return montarMensagem(len(alvos), sugeridas, semNovidade, falhas), nil
}

// montarMensagem is the summary of the execution: what the owner reads in the queue.
func montarMensagem(total, sugeridas, semNovidade, falhas int) string {
	return fmt.Sprintf("%d serie(s) sinalizada(s), %d com identidade sugerida, %d sem novidade, %d falha(s)",
		total, sugeridas, semNovidade, falhas)
}
