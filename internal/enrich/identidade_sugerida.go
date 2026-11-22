package enrich

// identidade_sugerida.go is the "who else could this folder be?" pass: the post-scan already
// knows WHICH series have a tree that does not fit their files (the `tree_mismatch` alert),
// and this is the other half of that fact -- a search for the identity that WOULD fit.
//
// It never applies anything. The owner's decision (21/nov/2026) was to keep the hint model of
// ADR-0038 and give the alert a real candidate: the item screen shows
// "suggested: O Clube do Terror (1992), score 100, fits 78/78" and one click applies it through
// the existing confirm-candidate.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// SugestaoIdentidade is the outcome of re-scoring ONE suspect series. Sucesso=false is a
// legitimate result -- the current identity is already the best the score knows, or nothing
// reached the cut -- and it is never an error.
type SugestaoIdentidade struct {
	Sucesso   bool
	Descricao string
}

// SugerirIdentidadeDaArvore re-scores `entityID` with the same evidence the auto-import uses
// (title in any language, the folder's year, the size of the folder against the tree) and, when
// a DIFFERENT identity fits the files, rewrites the hint with it so the owner can apply it in
// one click.
//
// The bar here is LimiarSugestao (80), not LimiarAutoMatch (92): a suggestion is a question the
// owner answers, not a decision the app takes. What keeps a bad suggestion out is the SHAPE
// gate -- the candidate's tree must host at least half the folder's files -- because that is
// the one piece of evidence a score cannot express.
func (s *Servico) SugerirIdentidadeDaArvore(ctx context.Context, entityID string, alvo AlvoSerie, langs []string) (SugestaoIdentidade, error) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return SugestaoIdentidade{}, fmt.Errorf("enrich: ler %s: %w", entityID, err)
	}
	origem, origemID, ok := identidadePrincipal(ent.ExternalIdsJson)
	if !ok {
		return SugestaoIdentidade{Descricao: "sem identidade atual para comparar"}, nil
	}

	melhor, achou := s.melhorCandidatoDiferente(ctx, alvo, langs, origemID)
	if !achou {
		return SugestaoIdentidade{Descricao: fmt.Sprintf("nenhum candidato acima de %.0f", matching.LimiarSugestao)}, nil
	}

	// The SHAPE gate: a tree of 3 episodes cannot host 26 files, however good the name is.
	arvore, err := s.arvoreDaSerie(ctx, melhor.cand.Source, melhor.cand.SourceID, langs)
	if err != nil {
		return SugestaoIdentidade{}, fmt.Errorf("enrich: arvore de %s:%s: %w", melhor.cand.Source, melhor.cand.SourceID, err)
	}
	episodios := episodiosDaArvore(arvore)
	if episodios == 0 {
		return SugestaoIdentidade{Descricao: fmt.Sprintf("%q tem arvore vazia", melhor.cand.Title)}, nil
	}
	if alvo.Arquivos > 0 && episodios*2 < alvo.Arquivos {
		return SugestaoIdentidade{Descricao: fmt.Sprintf("%q tem %d episodio(s) para %d arquivo(s)",
			melhor.cand.Title, episodios, alvo.Arquivos)}, nil
	}

	// The hint now points at the CANDIDATE, not at the current identity: that is what makes
	// the screen offer "apply" (the API derives can_apply from "the candidate differs from the
	// identity the entity already has").
	params := sqlcgen.UpsertMatchSuggestionParams{
		EntityID:   entityID,
		Source:     melhor.cand.Source,
		SourceID:   melhor.cand.SourceID,
		Title:      melhor.cand.Title,
		Year:       nsDeTexto(melhor.cand.Year),
		PosterUrl:  posterDe(melhor.cand, arvore),
		Score:      int64(math.Round(melhor.score)),
		Query:      alvo.Nome,
		Reason:     MotivoArvoreIncompativel,
		FilesTotal: int64(alvo.Arquivos),
		// The census already measured what does NOT fit the current tree; keep that number so
		// the screen keeps saying "N files have nowhere to go" while proposing the fix.
		FilesUnmatched: int64(alvo.Arquivos),
	}
	if atual, err := s.leitura().ObterMatchSuggestion(ctx, entityID); err == nil {
		params.FilesUnmatched = atual.FilesUnmatched
		params.FilesTotal = atual.FilesTotal
	}
	if err := s.gravarAlerta(ctx, params); err != nil {
		return SugestaoIdentidade{}, fmt.Errorf("enrich: gravar sugestao de %s: %w", entityID, err)
	}
	log.Printf("enrich: serie %q (%s:%s) -- sugerida %q (%s:%s, score %.2f, %d episodio(s) para %d arquivo(s))",
		ent.Title, origem, origemID, melhor.cand.Title, melhor.cand.Source, melhor.cand.SourceID,
		melhor.score, episodios, alvo.Arquivos)
	return SugestaoIdentidade{
		Sucesso: true,
		Descricao: fmt.Sprintf("%s (%s) score %.0f - %d episodio(s) para %d arquivo(s)",
			melhor.cand.Title, anoDeTexto(melhor.cand.Year), melhor.score, episodios, alvo.Arquivos),
	}, nil
}

// melhorCandidatoDiferente ranks the candidates of every source and returns the best one that is
// NOT the identity the entity already has and that reaches LimiarSugestao.
func (s *Servico) melhorCandidatoDiferente(ctx context.Context, alvo AlvoSerie, langs []string, origemID string) (candidatoSerie, bool) {
	melhor := candidatoSerie{}
	achou := false
	for _, prov := range s.providers.All() {
		if _, ok := prov.(provider.SerieProvider); !ok {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		res, err := prov.Buscar(ctx, alvo.Nome, tipoSerie, idiomaDeBuscaDe(langs))
		if err != nil {
			log.Printf("enrich: busca de %q em %q falhou (segue): %v", alvo.Nome, prov.Nome(), err)
			continue
		}
		for _, cand := range s.ranquearSeries(ctx, prov, res, alvo.Nome, alvo.Ano) {
			// The identity it ALREADY has is not a suggestion. The id alone decides, even when
			// the source differs: a proposal the screen would refuse to apply (can_apply is
			// derived from "the candidate differs from the identity") is noise, and hiding one
			// legitimate suggestion behind an id coincidence is the cheaper mistake.
			if cand.cand.SourceID == origemID {
				continue
			}
			if cand.score < matching.LimiarSugestao {
				continue
			}
			if !achou || cand.score > melhor.score {
				melhor, achou = cand, true
			}
			break // ranquearSeries is sorted: the first acceptable one is the best of this source
		}
	}
	return melhor, achou
}

// idiomaDeBuscaDe picks the language the search runs in: the same rule the rest of the package
// uses -- the first allowed language, since the extra languages are used for the CROSS payload,
// not for the query.
func idiomaDeBuscaDe(langs []string) string {
	if len(langs) == 0 {
		return ""
	}
	return langs[0]
}

// posterDe is the poster of the suggestion: the tree's own when the source publishes one, empty
// otherwise (the screen falls back to the placeholder).
func posterDe(cand provider.ResultadoExterno, arvore provider.SerieArvore) sql.NullString {
	if cand.PosterURL != nil && *cand.PosterURL != "" {
		return nsDeTexto(cand.PosterURL)
	}
	for _, t := range arvore.Temporadas {
		if t.PosterURL != "" {
			poster := t.PosterURL
			return nsDeTexto(&poster)
		}
	}
	return nsDeTexto(nil)
}

// anoDeTexto renders the year for the narration ("(1992)", or "(s/ano)").
func anoDeTexto(ano *string) string {
	if ano == nil || *ano == "" {
		return "s/ano"
	}
	return *ano
}
