package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa a PRÉVIA de candidato de match e a aplicação
// supervisionada (briefing 0060): antes deste briefing, "Confirmar" na fila
// de Revisão aceitava cegamente o palpite do scanner sem NUNCA buscar/mostrar
// um candidato real, e o auto-match (0050) só buscava DEPOIS de clicar, sem
// detalhe visível quando o score não batia LimiarAutoMatch. Os dois handlers
// aqui reusam INTEGRALMENTE a busca+pontuação do 0050
// (tituloAnoDoArquivo/buscarLocal/buscarExterno/melhorCandidatoLocal/
// melhorCandidatoExterno, automatch.go) -- a diferença é que os finders, desde
// a extração deste mesmo briefing, já não cortam por LimiarAutoMatch: quem
// decide aplicar o corte agora é CADA chamador (tentarAutoMatch corta; a
// prévia aqui não corta nunca, só marca meets_auto_threshold).

// matchPreviewTargetDTO é o candidato mostrado pela prévia -- local OU
// externo, nunca os dois. `poster_url`: para local, a rota do proxy da
// própria entity (mesma de EntityCard.poster_url, já vem de graça em
// entityCardDTO/cardDTO); para externo, a URL CRUA do provider (já vem de
// graça em provider.ResultadoExterno.PosterURL) -- o front roteia pelo proxy
// de imagem existente (arteSrc). Nenhum campo aqui exige chamada de rede
// extra além da busca já feita.
type matchPreviewTargetDTO struct {
	Title     string  `json:"title"`
	Year      *int    `json:"year"`
	PosterURL *string `json:"poster_url"`
	LocalID   *string `json:"local_id"`
	Source    *string `json:"source"`
	SourceID  *string `json:"source_id"`
	// MatchedTitle é o título (principal ou alternativo/traduzido) que
	// efetivamente produziu o score mostrado (briefing 0084) -- omitido
	// (nil) quando o candidato é local, ou quando o vencedor é o próprio
	// título principal (nada de alternativo bateu melhor).
	MatchedTitle *string `json:"matched_title,omitempty"`
}

// matchPreviewDTO é a resposta de GET /entities/{id}/match-preview -- SEMPRE
// 200 (mesmo sem candidato: resultado válido, não erro, mesma convenção do
// auto-match/0050). `meets_auto_threshold` é puramente informativo: nunca
// filtra o resultado nem impede confirm-candidate.
type matchPreviewDTO struct {
	Found              bool                   `json:"found"`
	Method             string                 `json:"method"` // "local" | "external" | "none"
	Score              float64                `json:"score"`
	MeetsAutoThreshold bool                   `json:"meets_auto_threshold"`
	Target             *matchPreviewTargetDTO `json:"target"`
}

// matchPreviewNone é o resultado neutro: nenhum candidato em nenhuma fonte
// (ou não havia o que buscar -- sem arquivo/título derivável).
var matchPreviewNone = matchPreviewDTO{Found: false, Method: "none", Score: 0, MeetsAutoThreshold: false, Target: nil}

// confirmCandidateBody é o corpo de POST /entities/{id}/confirm-candidate:
// dois formatos conforme `method` -- "local" exige LocalID; "external" exige
// Source+SourceID. Corpo que não bate o method responde 422.
type confirmCandidateBody struct {
	Method   string `json:"method"`
	LocalID  string `json:"local_id"`
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
}

// matchPreview trata GET /api/v1/entities/{id}/match-preview: dry-run,
// read-only, SEM side-effect. 404 se a entity não existe.
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

// montarMatchPreview deriva título/ano do arquivo e busca local-first, depois
// externo se o local não achou nada -- MESMA ordem de tentarAutoMatch
// (automatch.go), mas usando os finders SEM corte de LimiarAutoMatch
// diretamente (nenhuma duplicação de lógica de ranking). err é só erro real
// de banco/infra; "nenhum candidato" nunca é err.
func (s *Server) montarMatchPreview(ctx context.Context, ent sqlcgen.Entity) (matchPreviewDTO, error) {
	titulo, ano, ok, err := s.tituloAnoDoArquivo(ctx, ent)
	if err != nil {
		return matchPreviewDTO{}, err
	}
	if !ok {
		return matchPreviewNone, nil
	}

	// Idioma de busca (briefing 0084): a prévia deve refletir o MESMO idioma
	// que o auto-match de fato usaria -- mesmo resolvedor dos outros dois
	// fluxos (automatch.go/retroativo.go).
	lang, err := s.idiomaDeBusca(ctx, ent.ID)
	if err != nil {
		return matchPreviewDTO{}, err
	}

	// 1) Local-first (0026): mesma busca do auto-match.
	candidatosLocais, err := s.buscarLocal(ctx, titulo, ent.Type, nil)
	if err != nil {
		return matchPreviewDTO{}, err
	}
	if alvo, achou := s.melhorCandidatoLocal(ctx, candidatosLocais, ent.ID, titulo, ano); achou {
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

	// 2) Externo: só quando local não achou nada. Sem enrich ligado (ou sem
	// provider capaz do tipo), a prévia degrada para "none" -- mesma guarda de
	// tentarAutoMatch.
	if s.enrich == nil {
		return matchPreviewNone, nil
	}
	externos, disponivel := s.buscarExterno(ctx, titulo, ent.Type, lang)
	if !disponivel {
		return matchPreviewNone, nil
	}
	cand, score, tituloVencedor, achou := s.melhorCandidatoExterno(ctx, externos, titulo, ano)
	if !achou {
		return matchPreviewNone, nil
	}

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

// confirmCandidate trata POST /api/v1/entities/{id}/confirm-candidate: aplica
// o candidato que a prévia mostrou, SEM checar LimiarAutoMatch (decisão
// humana informada, não corte automático). 404 se a entity não existe; 422 se
// o corpo não bate o `method`. Branch `local`: funde a entity da fila
// (`id`, perdedora) com `local_id` (sobrevivente, o candidato escolhido) via
// fundirEntities -- sem cenário de colisão aqui, os dois lados já foram
// escolhidos explicitamente pelo dono. Branch `external`: reusa
// aplicarIdentidadeExterna (mesma sequência de correctEntity, review.go) com
// o ResponseWriter REAL da requisição -- uma colisão vira 409 de verdade.
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
		// Sobrevivente é sempre o local_id do corpo (o candidato escolhido);
		// perdedora é sempre o item da fila (0060 §4).
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
