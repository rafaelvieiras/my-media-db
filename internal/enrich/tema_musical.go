package enrich

// tema_musical.go é o ramo do enrich que busca e persiste o CANDIDATO de
// música-tema (briefing 0103, ADR-0025): depois do import/enrich primário,
// itera os providers que implementam provider.TemaMusicalProvider (a
// ThemerrDB), busca o candidato do item pelos ids externos e PERSISTE (ou a
// ausência de match) como um envelope media_external por fonte de tema —
// mesma técnica do inventário de artes (artes.go), mas mais simples: um
// candidato só, não um mapa por kind. É idempotente (UPSERT por
// UNIQUE(entity_id, source)) e best-effort: sem a capacidade, sem id ou sem
// match, degrada em silêncio (regressão zero). O download real do binário é
// lazy, disparado só pelo POST .../theme/download (internal/server).

import (
	"context"
	"encoding/json"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// AtualizarTemaMusical é o wrapper PÚBLICO de enriquecerTemaMusical (mesmo
// espírito de AtualizarInventarioArtes, briefing 0088): permite a um futuro
// chamador HTTP disparar uma nova consulta ao(s) TemaMusicalProvider(s)
// registrado(s) sem passar pelo Enriquecer inteiro. Best-effort por natureza
// (mesmo contrato de AtualizarInventarioArtes): nunca retorna erro real hoje;
// o `error` existe só para uma assinatura estável.
func (s *Servico) AtualizarTemaMusical(ctx context.Context, entityID, tipo string, langs []string) error {
	s.enriquecerTemaMusical(ctx, entityID, tipo, langs)
	return nil
}

// enriquecerTemaMusical busca e persiste o candidato de tema musical de
// `entityID` (tipo movie|tv) em cada provider com a capacidade
// TemaMusicalProvider. Best-effort: erro/ausência degrada em silêncio.
// Chamado nos MESMOS pontos que enriquecerArtes (enrich.go/serie.go): o
// candidato acompanha o mesmo ciclo de vida do inventário de artes, já que
// os dois dependem só dos external_ids do item, sem passo próprio.
func (s *Servico) enriquecerTemaMusical(ctx context.Context, entityID, tipo string, langs []string) {
	_ = langs // reservado (mesma assinatura de enriquecerArtes); a ThemerrDB nao usa idioma
	ids, ok := s.idsExternosDe(ctx, entityID)
	if !ok {
		return // sem nenhum id externo: nada a cruzar
	}
	for _, prov := range s.providers.All() {
		tp, ok := prov.(provider.TemaMusicalProvider)
		if !ok {
			continue
		}
		tema, achou, err := tp.BuscarTemaMusical(ctx, ids, tipo)
		if err != nil {
			log.Printf("enrich: tema musical de %s na fonte %q falhou (segue): %v", entityID, prov.Nome(), err)
			continue
		}
		if err := s.persistirCandidatoTema(ctx, entityID, prov.Nome(), tema, achou); err != nil {
			log.Printf("enrich: persistir candidato de tema musical de %s (fonte %q) falhou (segue): %v", entityID, prov.Nome(), err)
		}
	}
}

// persistirCandidatoTema grava o envelope do candidato (ou a ausência de
// match) em media_external, sob a fonte `source`. Grava também quando
// achou=false (envelope vazio, sem a chave) -- assim GET .../theme/candidato
// sabe que a consulta já aconteceu, sem re-bater na API a cada carga da
// tela; o refresh só ocorre no próximo ciclo normal de enriquecimento
// (briefing §3 "fora"). Idempotente por (entity_id, source).
func (s *Servico) persistirCandidatoTema(ctx context.Context, entityID, source string, tema provider.TemaMusical, achou bool) error {
	corpo := map[string]any{}
	if achou && tema.URL != "" {
		corpo[provider.ChaveEnvelopeTemaMusical] = tema.URL
	}
	payload, err := json.Marshal(corpo)
	if err != nil {
		return err
	}
	_, err = s.escrita().UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID:          s.novoID(),
		EntityID:    entityID,
		Source:      source,
		SourceID:    nsValid(""),
		PayloadJson: nsValid(string(payload)),
	})
	return err
}
