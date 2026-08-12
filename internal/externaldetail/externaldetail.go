// Package externaldetail é o serviço de DETALHE EXTERNO RICO (briefing 0022): serve
// o envelope aumentado do provider (o mesmo cache re-componível do enrich, ADR-0011)
// com CACHE-FIRST próprio, desacoplado de `entity` — ver o externo NÃO cataloga. A
// primeira consulta puxa da fonte e PERSISTE em external_detail; as próximas servem do
// cache dentro de um TTL, re-puxando quando o TTL expira OU quando o conjunto de
// idiomas permitidos muda (um idioma recém-permitido não está no payload guardado).
//
// É reusado pelo enrich/import (que passa a semear media_external a partir daqui,
// evitando a segunda ida à fonte quando o cache está fresco) e pelo handler GET do
// detalhe externo. A persistência é BEST-EFFORT: se a gravação tropeça, a leitura
// ainda responde o detalhe puxado (a próxima visita re-tenta).
package externaldetail

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// TTLPadrao é o tempo que o detalhe externo é servido do cache antes de re-puxar. O
// detalhe editorial muda pouco; um dia poupa a fonte sem envelhecer demais o conteúdo.
const TTLPadrao = 24 * time.Hour

// Servico resolve o detalhe externo com cache-first. Recebe os pools de banco, o
// REGISTRO multi-provider (a fonte é escolhida por nome, briefing 0031) e o TTL.
// `agora` é injetável em teste (relógio determinístico).
type Servico struct {
	database  *db.DB
	providers provider.Registry
	ttl       time.Duration
	agora     func() time.Time
}

// New cria o serviço. providers é o registro de fontes (o main só o alimenta quando
// há credencial); ttl<=0 cai no TTLPadrao.
func New(database *db.DB, provs provider.Registry, ttl time.Duration) *Servico {
	if ttl <= 0 {
		ttl = TTLPadrao
	}
	return &Servico{database: database, providers: provs, ttl: ttl, agora: time.Now}
}

// PayloadFresco devolve o ENVELOPE AUMENTADO do detalhe externo (o mesmo formato de
// BuscarDetalhe, cache re-componível): do cache quando fresco, senão puxando da fonte
// e persistindo (best-effort). `langs` são os idiomas PERMITIDOS (todas as traduções
// numa só ida, 0020). `force` (briefing 0088) pula a LEITURA do cache (doCacheFresco)
// e vai direto ao provider mesmo com um cache fresco dentro do TTL — a ESCRITA do
// resultado (persistir) continua acontecendo normalmente, então o cache fica
// atualizado com o payload novo para as próximas leituras sem force. Propaga
// provider.ErrNaoEncontrado quando o item não existe na fonte, e o erro cru quando a
// fonte falha (o chamador traduz em 404/502).
//
// Desde o briefing 0100 (ADR-0024): quando não há provider de busca ao vivo
// registrado para a fonte, OU a busca ao vivo falha, cai para o cache
// "qualquer" (doCacheQualquer, ignora TTL/langs -- só checa payload não-vazio)
// antes de propagar erro. Um item já visitado antes nunca perde o payload
// salvo por indisponibilidade momentânea/permanente da fonte (chave ausente,
// revogada, rede fora, provedor descontinuado); só falha de verdade quando
// não há cache algum.
func (s *Servico) PayloadFresco(ctx context.Context, source, sourceID, tipo string, langs []string, force bool) (json.RawMessage, error) {
	if !force {
		if cached, ok := s.doCacheFresco(ctx, source, sourceID, langs); ok {
			return cached, nil
		}
	}

	prov, ok := s.providers.Get(source)
	if !ok {
		if cached, ok := s.doCacheQualquer(ctx, source, sourceID); ok {
			return cached, nil
		}
		return nil, provider.ErrNaoEncontrado
	}

	raw, err := prov.BuscarDetalhe(ctx, sourceID, tipo, langs)
	if err != nil {
		if cached, ok := s.doCacheQualquer(ctx, source, sourceID); ok {
			log.Printf("externaldetail: busca ao vivo de %s/%s falhou, servindo cache stale: %v", source, sourceID, err)
			return cached, nil
		}
		return nil, err
	}
	s.persistir(ctx, source, sourceID, tipo, raw, langs)
	return raw, nil
}

// doCacheFresco devolve o payload cacheado quando existe e está FRESCO: dentro do TTL
// E com o conjunto de idiomas pedido contido no guardado. Qualquer tropeço (linha
// ausente, timestamp/langs ilegível, payload vazio) devolve (nil,false) — o chamador
// re-puxa. Nunca propaga erro: o cache é otimização, não barreira.
func (s *Servico) doCacheFresco(ctx context.Context, source, sourceID string, langs []string) (json.RawMessage, bool) {
	row, ok := s.obterLinhaCache(ctx, source, sourceID)
	if !ok {
		return nil, false
	}
	if !dentroDoTTL(row.FetchedAt, s.ttl, s.agora()) {
		return nil, false
	}
	if !idiomasContidos(row.LangsJson, langs) {
		return nil, false
	}
	return json.RawMessage(row.PayloadJson.String), true
}

// doCacheQualquer devolve o payload cacheado quando existe, IGNORANDO
// frescor (TTL/langs) — usado como último recurso quando a busca ao vivo não
// é possível (sem provider) ou falhou, para nunca perder dado já salvo por
// indisponibilidade momentânea/permanente da fonte (ADR-0024/briefing 0100).
func (s *Servico) doCacheQualquer(ctx context.Context, source, sourceID string) (json.RawMessage, bool) {
	row, ok := s.obterLinhaCache(ctx, source, sourceID)
	if !ok {
		return nil, false
	}
	return json.RawMessage(row.PayloadJson.String), true
}

// obterLinhaCache lê a linha de external_detail e devolve (linha,true) só
// quando existe e tem payload não-vazio -- a checagem de frescor (TTL/langs)
// fica por conta do chamador (doCacheFresco); doCacheQualquer usa a linha
// crua. Qualquer tropeço (linha ausente, erro de leitura) devolve
// (zero,false) e NUNCA propaga erro: o cache é otimização, não barreira.
func (s *Servico) obterLinhaCache(ctx context.Context, source, sourceID string) (sqlcgen.ExternalDetail, bool) {
	row, err := sqlcgen.New(s.database.Read).ObterExternalDetail(ctx, sqlcgen.ObterExternalDetailParams{
		Source:   source,
		SourceID: sourceID,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("externaldetail: ler cache de %s/%s falhou: %v", source, sourceID, err)
		}
		return sqlcgen.ExternalDetail{}, false
	}
	if !row.PayloadJson.Valid || row.PayloadJson.String == "" {
		return sqlcgen.ExternalDetail{}, false
	}
	return row, true
}

// persistir grava (upsert) o payload no cache external_detail, best-effort: uma falha
// é logada e NÃO aborta a leitura (a próxima visita re-tenta). `langs_json` guarda os
// idiomas contidos, base do gatilho de refresh por conjunto de permitidos.
func (s *Servico) persistir(ctx context.Context, source, sourceID, tipo string, raw json.RawMessage, langs []string) {
	langsJSON, err := json.Marshal(langs)
	if err != nil {
		log.Printf("externaldetail: serializar langs de %s/%s falhou (segue sem cachear): %v", source, sourceID, err)
		return
	}
	if err := sqlcgen.New(s.database.Write).UpsertExternalDetail(ctx, sqlcgen.UpsertExternalDetailParams{
		Source:      source,
		SourceID:    sourceID,
		Type:        tipo,
		PayloadJson: nsValid(string(raw)),
		LangsJson:   nsValid(string(langsJSON)),
	}); err != nil {
		log.Printf("externaldetail: cachear %s/%s falhou (segue sem cachear): %v", source, sourceID, err)
	}
}

// dentroDoTTL diz se o carimbo `fetched_at` (formato do strftime das migrations) está
// a menos de `ttl` de `agora`. Carimbo ilegível conta como EXPIRADO (re-puxa) — o
// seguro é preferir a fonte a servir algo com data desconhecida.
func dentroDoTTL(fetchedAt string, ttl time.Duration, agora time.Time) bool {
	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return false
	}
	return agora.Sub(t) < ttl
}

// idiomasContidos diz se TODOS os `pedidos` estão no conjunto guardado em `langsJSON`.
// Um idioma pedido ausente (ex.: recém-permitido) força o refresh (ADR-0011/0020): o
// payload guardado não tem aquela tradução. langs_json ilegível conta como "não
// contém" (re-puxa, seguro).
func idiomasContidos(langsJSON sql.NullString, pedidos []string) bool {
	if !langsJSON.Valid || langsJSON.String == "" {
		return len(pedidos) == 0
	}
	var guardados []string
	if json.Unmarshal([]byte(langsJSON.String), &guardados) != nil {
		return false
	}
	conjunto := make(map[string]struct{}, len(guardados))
	for _, l := range guardados {
		conjunto[l] = struct{}{}
	}
	for _, p := range pedidos {
		if _, ok := conjunto[p]; !ok {
			return false
		}
	}
	return true
}

// nsValid embrulha uma string num sql.NullString válido (não-NULL).
func nsValid(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
