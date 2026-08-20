package server

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo resolve a identidade EXTERNA (source, source_id) para a entity
// INTERNA correspondente (briefing 0026): a base da "centralização interno-first".
// Quando um item externo (uma coleção ou uma de suas partes) já foi importado, a UI
// deve linkar para o destino interno (/entities/{id}) e não oferecer reimportar —
// para isso os DTOs externos ganham um `internal_id`, preenchido por estes helpers.
//
// A resolução casa por (source, source_id) contra a identidade AUTORITATIVA da entity
// (colunas geradas external_id_tmdb/tvdb; ver 0015/0025/0007). Generalizado no 0047: QUALQUER
// fonte com coluna gerada (tmdb E tvdb hoje) resolve o interno-first, não só tmdb — assim um
// item importado do TheTVDB também aparece "na biblioteca". Fontes sem coluna própria saem nil
// (degradam — sem lookup), coerente com o find-or-create (acharPorIDExterno) de mesma cobertura.

// idInternoItem devolve o id da entity interna de um item externo (filme/tv) já
// importado, ou nil quando não existe, a fonte não tem lookup, ou o source_id é vazio.
// Despacha pela FONTE (espelha enrich.acharPorIDExterno): "tmdb"→external_id_tmdb,
// "tvdb"→external_id_tvdb (colunas geradas indexadas). Um erro real de banco degrada para
// nil (loga) — a leitura da tela nunca cai por causa da resolução interno-first, que é um
// enriquecimento opcional.
func (s *Server) idInternoItem(ctx context.Context, source, sourceID string) *string {
	if sourceID == "" {
		return nil
	}
	var ent sqlcgen.Entity
	var err error
	switch source {
	case "tmdb":
		ent, err = s.leitura().ObterEntityPorTmdb(ctx, nsDe(sourceID))
	case "tvdb":
		ent, err = s.leitura().ObterEntityPorTvdb(ctx, nsDe(sourceID))
	default:
		return nil // fonte sem coluna gerada: sem lookup (degrada)
	}
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

// idInternoPorMbid devolve o id da entity interna (artista/álbum/faixa) já
// catalogada com este MBID de MusicBrainz, ou nil quando não existe ou `sourceID`
// é vazio. Generaliza idInternoItem/idInternoColecao (que despacham por `source` de
// filme/tv) para a vertical de música (briefing 0115 Parte 2): o id externo
// universal da música é sempre o MBID (coluna gerada external_id_mbid),
// independente do entity.type. Usado pela tela externa de álbum
// (external_album.go) para marcar internal_id tanto no álbum quanto em cada faixa
// oficial do release. Mesma resiliência de idInternoItem: erro real de banco
// degrada para nil (loga).
func (s *Server) idInternoPorMbid(ctx context.Context, sourceID string) *string {
	if sourceID == "" {
		return nil
	}
	ent, err := s.leitura().ObterEntityPorMbid(ctx, nsDe(sourceID))
	if err != nil {
		logSeErroReal("id interno por mbid", sourceID, err)
		return nil
	}
	return &ent.ID
}

// logSeErroReal loga apenas erros REAIS de banco — a ausência de linha
// (sql.ErrNoRows) é o caso normal "não importado" e não merece log.
func logSeErroReal(contexto, sourceID string, err error) {
	if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("server: %s (source_id=%s) falhou (segue sem id interno): %v", contexto, sourceID, err)
	}
}
