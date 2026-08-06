package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa o ENRIQUECIMENTO EXTERNO RETROATIVO (briefing 0056),
// extensão do job saude_biblioteca (0053/0054): varre entities `cataloged`
// (ou wanted/unreleased) com confiança 70 do scanner (título+ano parseados do
// arquivo, SEM id externo embutido) -- o complemento EXATO de ListarRevisao
// restrito a itens locais puros com arquivo no disco. Esses itens NUNCA
// passam por nenhum provider externo (não caem na fila de Revisão, então o
// auto-match do 0050/0054 nunca os alcança) -- ficam "invisíveis" para
// sempre, com título/ano bonitinhos mas sem pôster/sinopse/elenco. Este
// arquivo fecha esse gap: busca local-first e depois externo por lote
// pequeno e throttled; aplica sozinho com confiança alta (mesmo limiar do
// 0050), sinaliza para a fila de Revisão quando achou algo plausível mas não
// confiável o bastante, ou só registra a tentativa (throttle) quando não
// achou nada -- reaproveitando INTEGRALMENTE o motor de busca/pontuação e a
// sequência de aplicação externa (aplicarCandidatoExterno, extraído de
// tentarAutoMatch em automatch.go) já existentes.

const (
	// limiteEnriquecimentoRetroativoPorExecucao é o teto de itens VERIFICADOS
	// (não só listados) por execução do job -- throttling de custo/rate-limit
	// do provider externo numa biblioteca que pode ter um backlog grande na
	// primeira vez que este briefing roda.
	limiteEnriquecimentoRetroativoPorExecucao = 15

	// intervaloReTentativaRetroativa é o intervalo mínimo entre duas
	// tentativas do MESMO item -- não re-bate no provider por um item que já
	// foi verificado recentemente e não achou nada (ou não bateu o limiar).
	intervaloReTentativaRetroativa = 30 * 24 * time.Hour

	// limiarSinalizacaoRetroativa é o piso de plausibilidade para gravar o
	// item na fila de Revisão (via AtualizarConfidencePorEntity): abaixo
	// disso, o candidato é ruído (títulos parecidos por coincidência) e não
	// vale interromper o dono -- só a tentativa é registrada (throttle).
	// Decisão NOVA deste briefing (ver §6 do briefing 0056): o 0050 só
	// precisava de UM corte (aplicar ou não, o item já estava visível de
	// qualquer jeito); aqui "não aplicar" sozinho deixaria o item invisível
	// para sempre, daí o piso adicional só para a decisão de "vale sinalizar".
	limiarSinalizacaoRetroativa = 50.0
)

// carimboRetroativoFormato é o layout Go do timestamp gravado por
// AtualizarTentativaMatchExterno (strftime('%Y-%m-%dT%H:%M:%fZ', 'now') no
// SQL -- 3 dígitos de milissegundo), usado para calcular o corte `desde` do
// throttle em Go. Mesmo layout usado por scanner.carimboFormato/
// health_system.go (ComputedAt), sem precisão de nanossegundos (%f do
// SQLite trunca em milissegundos).
const carimboRetroativoFormato = "2006-01-02T15:04:05.000Z"

// EnriquecerRetroativo varre o lote de itens elegíveis (locais puros, nunca
// verificados contra provider nenhum, respeitando o throttle por item) e
// tenta enriquecer cada um -- aplicando sozinho, sinalizando para Revisão ou
// só registrando a tentativa. Satisfaz libraryhealth.Matcher (chamado pelo
// job saude_biblioteca, briefing 0056, DEPOIS do auto-match em lote
// existente 0054). err é SÓ erro real de banco na LISTAGEM; falha num item
// individual é absorvida (best-effort, loga e segue).
func (s *Server) EnriquecerRetroativo(ctx context.Context) (aplicados, sinalizados int, err error) {
	// Sem provider configurado (ou sem assetsDir -- mesma guarda de
	// tentarAutoMatch), NENHUMA tentativa real pode acontecer: devolve sem
	// tocar o banco, para não carimbar last_external_match_attempt_at de
	// itens que não foram de fato verificados -- eles mantêm prioridade
	// MÁXIMA (nunca tentado) assim que um provider voltar a existir, em vez
	// de esperar 30 dias de throttle por um período em que não havia
	// provider nenhum (ver §6 do briefing 0056).
	if s.enrich == nil {
		return 0, 0, nil
	}

	desde := time.Now().Add(-intervaloReTentativaRetroativa).UTC().Format(carimboRetroativoFormato)
	rows, err := s.leitura().ListarElegiveisEnriquecimentoRetroativo(ctx, sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
		Limiar: sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
		Desde:  sql.NullString{String: desde, Valid: true},
		Limite: limiteEnriquecimentoRetroativoPorExecucao,
	})
	if err != nil {
		return 0, 0, err
	}

	for _, row := range rows {
		ent, err := s.leitura().ObterEntity(ctx, row.ID)
		if err != nil {
			// Sumiu entre a listagem e o processamento (purga concorrente) --
			// pula, não derruba o lote (mesmo padrão de autoMatchLote).
			continue
		}

		resultado, _, _, tentarErr := s.tentarEnriquecerOuSinalizar(ctx, ent)
		if tentarErr != nil {
			log.Printf("saude_biblioteca: enriquecimento retroativo falhou para %s (best-effort, ignorado): %v", ent.ID, tentarErr)
		}

		// Carimba a tentativa SEMPRE ao final (mesmo se tentarErr != nil): o
		// item foi de fato verificado nesta execução -- best-effort, erro
		// aqui só loga, não aborta o item nem o lote (briefing 0056 §4).
		// carimbarTentativaExterna (extraído no briefing 0059) é o MESMO
		// helper reusado pelo endpoint individual enrichMissing, sem duplicar
		// a chamada sqlc.
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

// tentarEnriquecerOuSinalizar tenta enriquecer UMA entity elegível: deriva
// título/ano do arquivo, busca local-first e depois externo, usando o MELHOR
// score mesmo abaixo do limiar (precisa do valor para decidir "sinalizar").
// Desde o briefing 0060, melhorCandidatoLocal/melhorCandidatoExterno
// (automatch.go) já devolvem o maior-pontuado SEM o corte de LimiarAutoMatch
// embutido -- quem decide aplicar é o chamador -- então este arquivo reusa os
// dois diretamente, sem duplicar a lógica de ranking. resultado é
// "applied"/"flagged"/"none"; method (só em "applied", "local"/"external") e
// score (em "applied"/"flagged") alimentam o DTO público de enrichMissing
// (briefing 0059) -- EnriquecerRetroativo (o job em lote) ignora os dois,
// interessado só em resultado para as contagens agregadas. err é SÓ erro real
// de banco/infra.
func (s *Server) tentarEnriquecerOuSinalizar(ctx context.Context, ent sqlcgen.Entity) (resultado, method string, score float64, err error) {
	titulo, ano, ok, err := s.tituloAnoDoArquivo(ctx, ent.ID)
	if err != nil {
		return "", "", 0, err
	}
	if !ok {
		return "none", "", 0, nil
	}

	// Idioma de busca (briefing 0084): mesmo resolvedor do auto-match -- a
	// biblioteca de origem pode ter um idioma preferido; sem um, cai no
	// idioma PRINCIPAL global (nunca mais o "" cego de antes deste briefing).
	lang, err := s.idiomaDeBusca(ctx, ent.ID)
	if err != nil {
		return "", "", 0, err
	}

	// 1) Local-first (mesma ordem de tentarAutoMatch): nunca duplica entity
	// já catalogada.
	candidatosLocais, err := s.buscarLocal(ctx, titulo, ent.Type, "", "")
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

	// 2) Externo: só quando local não bateu o limiar de aplicação.
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
					// aplicarCandidatoExterno degradou (colisão com
					// investimento do dono na outra entity, ou a fonte
					// falhou ao entregar o detalhe): não há como aplicar às
					// cegas -- cai para a decisão de sinalizar abaixo, com o
					// mesmo score já calculado (o candidato continua
					// plausível, só não pôde ser aplicado sozinho).
				}
			}
		}
	}

	// 3) Sem candidato forte em nenhuma fonte (ou aplicação externa
	// degradada acima): decide sinalizar pelo MAIOR score entre local e
	// externo, já calculados.
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

	// Teto do valor GRAVADO no banco: sem ele, um candidato pontuado entre
	// limiarBaixaConfianca e LimiarAutoMatch (achou algo bom, mas não bom o
	// bastante para os 92 do auto-match) seria gravado com um
	// match_confidence que NÃO bate `< limiar` -- o item continuaria
	// invisível, o oposto do pretendido (ver §6 do briefing 0056). O score
	// DEVOLVIDO ao chamador (para o DTO de enrichMissing) é o melhorGeral
	// real, não o teto -- o dono quer ver a pontuação de fato do candidato,
	// não o valor artificialmente rebaixado só para a fila de Revisão.
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

// carimbarTentativaExterna grava last_external_match_attempt_at para `id` via
// AtualizarTentativaMatchExterno -- extraído do corpo antes inline de
// EnriquecerRetroativo (briefing 0059) para ser reusado SEM duplicar a
// chamada sqlc pelo endpoint individual enrichMissing (que carimba
// incondicionalmente, mesmo ignorando o throttle do job em lote).
func (s *Server) carimbarTentativaExterna(ctx context.Context, id string) error {
	return sqlcgen.New(s.db.Write).AtualizarTentativaMatchExterno(ctx, id)
}

// enrichMissingResultDTO é a resposta de POST /entities/{id}/enrich-missing
// (briefing 0059, openapi.yaml EnrichMissingResult) -- espelha o `resultado`
// de tentarEnriquecerOuSinalizar, reaproveitado como o DTO público deste
// endpoint (sem vocabulário novo). Method só presente quando
// outcome=="applied"; Score presente quando outcome é "applied" ou
// "flagged"; nenhum dos dois em "none".
type enrichMissingResultDTO struct {
	Outcome string   `json:"outcome"`
	Method  *string  `json:"method,omitempty"`
	Score   *float64 `json:"score,omitempty"`
}

// enrichMissing trata POST /api/v1/entities/{id}/enrich-missing (briefing
// 0059): dispara SOB DEMANDA, para UM item aberto no detalhe, a MESMA
// orquestração de busca/aplicação/sinalização do job de saúde
// (tentarEnriquecerOuSinalizar, 0056) -- SEM nenhuma guarda de elegibilidade
// por status/throttle (ação explícita e pontual do dono, diferente da
// varredura em lote; ver briefing 0059 §6 para a justificativa de não
// replicar o filtro do job aqui). Sempre carimba
// last_external_match_attempt_at ao final (mesmo em outcome=="none"), para o
// PRÓXIMO lote do job não competir pelo item que o dono acabou de resolver
// manualmente. 404 se a entity não existe; 200 sempre depois disso (nunca
// 5xx só por "não achou candidato" -- mesmo padrão de /auto-match); 500 só em
// erro real de banco/infra (na busca OU ao carimbar).
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
	// Carimba SEMPRE, mesmo se tentarErr != nil -- o item foi de fato
	// verificado nesta chamada (mesma semântica de EnriquecerRetroativo, só
	// que aqui um erro de banco em QUALQUER um dos dois passos vira 500 (não
	// é best-effort de lote -- é uma chamada HTTP individual).
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
