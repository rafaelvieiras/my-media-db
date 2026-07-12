package server

import (
	"context"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// Este arquivo resolve o interno-first dos CRÉDITOS (briefing 0029 Fase D): quando a
// pessoa de um crédito de elenco/equipe já foi catalogada por ALGUM import (person
// deduplicada por source+source_id, ver internal/enrich/creditos.go), o crédito ganha
// `person_id` — o sinal que a UI usa para linkar à página da pessoa (GET /person/{id})
// em vez de deixar o nome não-clicável. Espelha idInternoItem/idInternoColecao
// (interno.go): mesma resiliência (erro real de banco degrada para nil, sql.ErrNoRows
// é o caso normal "pessoa não catalogada" e não loga).

// anotarCreditosInternos preenche PersonID de cada crédito de `cast`/`crew` cuja pessoa
// já está catalogada sob `source`. `source` vazio (fonte não identificada) é no-op —
// sem fonte não há como montar a chave (source, source_id) do lookup. Muta as fatias em
// memória (in place), como proxyarCreditos.
func (s *Server) anotarCreditosInternos(ctx context.Context, source string, cast []richfields.Cast, crew []richfields.Crew) {
	if source == "" {
		return
	}
	for i := range cast {
		cast[i].PersonID = s.idPessoaPorFonte(ctx, source, cast[i].SourceID)
	}
	for i := range crew {
		crew[i].PersonID = s.idPessoaPorFonte(ctx, source, crew[i].SourceID)
	}
}

// idPessoaPorFonte devolve o id local da pessoa catalogada sob (source, sourceID), ou
// nil quando não existe, sourceID é vazio, ou um erro real de banco degrada a busca
// (loga; a leitura da tela nunca cai por causa deste enriquecimento opcional).
func (s *Server) idPessoaPorFonte(ctx context.Context, source, sourceID string) *string {
	if sourceID == "" {
		return nil
	}
	p, err := s.leitura().ObterPersonPorFonte(ctx, sqlcgen.ObterPersonPorFonteParams{
		Source:   source,
		SourceID: sourceID,
	})
	if err != nil {
		logSeErroReal("id interno da pessoa do credito", sourceID, err)
		return nil
	}
	return &p.ID
}

// fonteDoCampoExterno descobre qual fonte (CamadaExterna) forneceu o valor NÃO-NIL de
// `campo` na fusão por prioridade que o resolver faz (mesma regra de
// camposExternosMesclados em internal/resolver/resolver.go: a PRIMEIRA da lista —
// índice 0 = maior prioridade — com valor não-nil vence). "" quando nenhuma camada tem
// o campo. Necessário porque resolver.Resolver já devolve os créditos ACHATADOS (sem
// proveniência) — mas person.source precisa casar com o `source` que alimentou aquele
// crédito no enrich (ver internal/enrich/creditos.go: persistirCreditos grava
// source=fonte do media_external que trouxe o cast/crew daquela chamada).
func fonteDoCampoExterno(externo []resolver.CamadaExterna, campo string) string {
	for _, c := range externo {
		if v, ok := c.Campos[campo]; ok && v != nil {
			return c.Fonte
		}
	}
	return ""
}
