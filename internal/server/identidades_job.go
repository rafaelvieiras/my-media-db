package server

// identidades_job.go wires the `sugerir_identidades` job (internal/job/identidades) to the
// pieces of Server it needs: the census of series whose tree does not fit their files (the same
// one the post-scan uses to raise the alert) and the re-scoring that writes a candidate into
// the hint.
//
// The job NEVER changes an identity -- the owner's decision (21/nov/2026) was to keep ADR-0038
// and let the alert carry the candidate, applied by one click on the item screen.

import (
	"context"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/job"
	"github.com/rafaelvieiras/my-media-db/internal/job/identidades"
)

// ListarIdentidadesSuspeitas implements identidades.Alvos: every series the census already
// flags as "the tree does not fit the files". Using the census and not the alert table is what
// keeps job and alert from disagreeing about who is suspect.
func (s *Server) ListarIdentidadesSuspeitas(ctx context.Context) ([]identidades.Suspeita, error) {
	linhas, err := s.leitura().ListarSeriesComArvoreIncompativel(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]identidades.Suspeita, 0, len(linhas))
	for _, l := range linhas {
		out = append(out, identidades.Suspeita{ID: l.EntityID, Titulo: l.Title})
	}
	return out, nil
}

// SugerirIdentidadeDaSerie implements identidades.Sugestor: re-scores ONE suspect series with
// the evidence the auto-import uses (the name of the FOLDER, the year the folder states and how
// many files it holds) and rewrites its hint when a different identity fits better.
//
// A series without a present file has no folder to derive from: the answer is "nothing to say",
// not an error -- the census would not have listed it anyway.
func (s *Server) SugerirIdentidadeDaSerie(ctx context.Context, entityID string) (identidades.Resultado, error) {
	if s.enrich == nil {
		return identidades.Resultado{Descricao: "enriquecimento desligado"}, nil
	}
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return identidades.Resultado{}, err
	}
	if ent.Type != tipoEntSerie {
		return identidades.Resultado{Descricao: "nao e serie"}, nil
	}
	nome, ano, ok := s.nomeDaPastaDaSerie(ctx, ent)
	if !ok {
		return identidades.Resultado{Descricao: "sem pasta para derivar o nome"}, nil
	}
	arquivos, err := s.leitura().ContarArquivosDaSerie(ctx, entityID)
	if err != nil {
		return identidades.Resultado{}, err
	}
	langs, err := s.idiomasDeBusca(ctx)
	if err != nil {
		return identidades.Resultado{}, err
	}
	res, err := s.enrich.SugerirIdentidadeDaArvore(ctx, entityID,
		enrich.AlvoSerie{Nome: nome, Ano: ano, Arquivos: int(arquivos)}, langs)
	if err != nil {
		return identidades.Resultado{}, err
	}
	return identidades.Resultado{Sucesso: res.Sucesso, Descricao: res.Descricao}, nil
}

// nomeDaPastaDaSerie derives the name and the year of the series FOLDER the owner wrote. A
// series has no media_file of its own in production (the files hang on the episodes), so the
// generic variantesTituloAno comes out empty for it -- the difference between a job that says
// "sem pasta" fourteen times and one that finds the candidate.
func (s *Server) nomeDaPastaDaSerie(ctx context.Context, ent sqlcgen.Entity) (string, int, bool) {
	if variantes, err := s.variantesTituloAno(ctx, ent); err == nil && len(variantes) > 0 {
		return variantes[0].Titulo, variantes[0].Ano, true
	}
	caminho, err := s.leitura().ObterCaminhoDeArquivoDaSerie(ctx, ent.ID)
	if err != nil || caminho == "" {
		return "", 0, false
	}
	return s.tituloAnoDaPastaDaSerie(ctx, ent, sqlcgen.MediaFile{Path: caminho})
}

// idiomasDeBusca is the allowed-language list without an http.ResponseWriter in hand (the job
// has no request): the same settings the HTTP flows read.
func (s *Server) idiomasDeBusca(ctx context.Context) ([]string, error) {
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return nil, err
	}
	return cfg.Allowed, nil
}

// WithIdentidadesJob REGISTERS the `sugerir_identidades` job. It is a DEDICATED option (not
// WithJobHandlers) because the Handler needs *Server itself as Alvos/Sugestor, which only
// exists in here -- the same reason WithMidiasFaltantesJob is one.
func WithIdentidadesJob() Option {
	return func(s *Server) {
		// Only the DATABASE is required here. s.enrich is built by montarServicos, which runs
		// AFTER the options -- guarding on it would silently skip the registration for good
		// (measured in production on 0.18.0: `job_handler_unavailable` on the first run). The
		// methods below degrade at CALL time when enrichment is off.
		if s.db == nil {
			return
		}
		if s.jobs == nil {
			s.jobs = job.NewManager(s.db)
		}
		s.jobs.Registry.Registrar(identidades.New(s, s))
	}
}
