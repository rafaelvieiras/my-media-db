// Package historico implementa o BACKFILL retroativo de eventos órfãos
// (briefing 0099, ADR-0022): quando uma entity ganha um id externo
// (tmdb/imdb/tvdb) — via PUT /entities/{id}/external-ids ou pelo
// enriquecimento (internal/enrich) — este pacote vincula os eventos de
// `historico_evento` que ficaram sem entity (entity_id NULL, gravados pelo
// webhook do Jellyfin quando nenhuma entity casava, ver
// internal/server/webhook_jellyfin.go) cujos IDs brutos batem com o id novo.
//
// Pacote pequeno e sem HTTP (mesmo espírito de internal/webhook): só lógica
// sobre sqlcgen.Queries, importável tanto por internal/server quanto por
// internal/enrich sem criar ciclo (nenhum dos dois importa este pacote de
// volta).
package historico

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// fontesReconhecidas são as únicas fontes que o casamento do webhook usa
// hoje (internal/server/webhook_jellyfin.go, casarEntityJellyfin) e,
// portanto, as únicas que um evento órfão pode ter gravado em raw_*_id — o
// backfill cobre exatamente as mesmas 3. Uma fonte fora deste mapa em
// `externalIDs` (ex.: um source de arte/trailer que não participa do
// casamento) é ignorada, não é erro.
var fontesReconhecidas = map[string]bool{"tmdb": true, "imdb": true, "tvdb": true}

// ns embrulha uma string num sql.NullString válido quando não-vazia (NULL
// quando ""). Cópia local do helper equivalente em internal/server (nsDe) —
// este pacote não pode importar internal/server (dependência na direção
// errada: server importaria historico, não o contrário).
func ns(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// VincularOrfaos vincula a entityID todo evento órfão (historico_evento com
// entity_id IS NULL) cujo id bruto de alguma fonte presente em externalIDs
// bate (tmdb/imdb/tvdb — as mesmas 3 fontes que o casamento do webhook usa;
// chaves fora desse conjunto são ignoradas). Roda uma query de backfill por
// fonte presente e com valor não-vazio; soma as linhas afetadas de todas.
//
// Erro de UMA fonte não aborta as demais — acumulado via errors.Join e
// devolvido ao final (o chamador tipicamente só loga, backfill é um bônus
// best-effort sobre uma gravação de identidade que já teve sucesso).
func VincularOrfaos(ctx context.Context, q *sqlcgen.Queries, entityID string, externalIDs map[string]string) (int64, error) {
	var total int64
	var erros error

	for fonte, valor := range externalIDs {
		if valor == "" || !fontesReconhecidas[fonte] {
			continue
		}
		var linhas int64
		var err error
		switch fonte {
		case "tmdb":
			linhas, err = q.VincularHistoricoEventoOrfaoPorTmdb(ctx, sqlcgen.VincularHistoricoEventoOrfaoPorTmdbParams{
				EntityID:  ns(entityID),
				RawTmdbID: ns(valor),
			})
		case "imdb":
			linhas, err = q.VincularHistoricoEventoOrfaoPorImdb(ctx, sqlcgen.VincularHistoricoEventoOrfaoPorImdbParams{
				EntityID:  ns(entityID),
				RawImdbID: ns(valor),
			})
		case "tvdb":
			linhas, err = q.VincularHistoricoEventoOrfaoPorTvdb(ctx, sqlcgen.VincularHistoricoEventoOrfaoPorTvdbParams{
				EntityID:  ns(entityID),
				RawTvdbID: ns(valor),
			})
		}
		if err != nil {
			erros = errors.Join(erros, err)
			continue
		}
		total += linhas
	}
	return total, erros
}
