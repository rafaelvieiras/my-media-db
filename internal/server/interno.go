package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
)

// Este arquivo resolve a identidade EXTERNA (source, source_id) para a entity
// INTERNA correspondente (briefing 0026): a base da "centralização interno-first".
// Quando um item externo (uma coleção ou uma de suas partes) já foi importado, a UI
// deve linkar para o destino interno (/entities/{id}) e não oferecer reimportar —
// para isso os DTOs externos ganham um `internal_id`, preenchido por estes helpers.
//
// Só a fonte "tmdb" deduplica hoje (colunas geradas external_id_tmdb / dedup de
// coleção por type='collection'; ver 0015/0025). Para outras fontes o id interno sai
// nil (degrada — sem lookup), consistente com o find-or-create que ainda não dedupa
// fontes sem coluna gerada.

// idInternoItem devolve o id da entity interna de um item externo (filme/tv) já
// importado, ou nil quando não existe, a fonte não é tmdb, ou o source_id é vazio.
// Um erro real de banco degrada para nil (loga) — a leitura da tela nunca cai por
// causa da resolução interno-first, que é um enriquecimento opcional.
func (s *Server) idInternoItem(ctx context.Context, source, sourceID string) *string {
	if source != "tmdb" || sourceID == "" {
		return nil
	}
	ent, err := s.leitura().ObterEntityPorTmdb(ctx, nsDe(sourceID))
	if err != nil {
		logSeErroReal("id interno do item", sourceID, err)
		return nil
	}
	return &ent.ID
}

// idInternoColecao devolve o id da entity COLEÇÃO interna (type='collection') de uma
// coleção externa já importada, ou nil quando não existe, a fonte não é tmdb, ou o
// source_id é vazio. Usa a query dedicada ObterColecaoPorTmdb (restrita a
// type='collection', 0025) para não casar um filme homônimo de mesmo id numérico.
// Mesma resiliência do idInternoItem: erro de banco degrada para nil.
func (s *Server) idInternoColecao(ctx context.Context, source, sourceID string) *string {
	if source != "tmdb" || sourceID == "" {
		return nil
	}
	col, err := s.leitura().ObterColecaoPorTmdb(ctx, nsDe(sourceID))
	if err != nil {
		logSeErroReal("id interno da colecao", sourceID, err)
		return nil
	}
	return &col.ID
}

// logSeErroReal loga apenas erros REAIS de banco — a ausência de linha
// (sql.ErrNoRows) é o caso normal "não importado" e não merece log.
func logSeErroReal(contexto, sourceID string, err error) {
	if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("server: %s (source_id=%s) falhou (segue sem id interno): %v", contexto, sourceID, err)
	}
}
