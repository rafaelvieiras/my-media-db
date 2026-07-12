package enrich

// creditos.go persiste o ELENCO/EQUIPE/PRODUTORAS (briefing 0029, MD6 Fase A) a partir
// do canonico do provider (chaves cast/crew/companies). E CACHE RE-COMPONIVEL: o dado
// do dono nunca vive aqui (vive no override). Idempotente no re-enriquecimento: apaga os
// creditos de (entity, source) e os repopula, sem duplicar. person/company sao
// deduplicadas por (source, source_id) via upsert -- a mesma pessoa em duas obras tem
// UMA linha. Tudo best-effort: falha de uma parte degrada (loga e segue), nunca aborta
// o enrich.

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

const (
	// kindCast/kindCrew/kindCompany discriminam o papel do credito (coluna credit.kind).
	kindCast    = "cast"
	kindCrew    = "crew"
	kindCompany = "company"
)

// dadosCredito agrega os campos de um credito para criarCredito -- um so ponto de
// insercao (cast/crew/company preenchem os campos que lhes cabem, o resto fica NULL).
type dadosCredito struct {
	entityID     string
	personID     sql.NullString
	companyID    sql.NullString
	kind         string
	department   sql.NullString
	job          sql.NullString
	character    sql.NullString
	ordering     int64
	episodeCount sql.NullInt64
	source       string
	sourceID     sql.NullString
}

// persistirCreditos apaga os creditos de (entityID, source) e repopula elenco/equipe/
// produtoras a partir do canonico. Best-effort: a falha do apagar aborta so este bloco
// (nao o enrich); as demais degradam por item. Chamado no import/enrich de FILME e no no
// SERIE (o elenco agregado e da serie inteira).
func (s *Servico) persistirCreditos(ctx context.Context, entityID, source string, campos map[string]any) {
	if err := s.escrita().ApagarCreditosDaEntityFonte(ctx, sqlcgen.ApagarCreditosDaEntityFonteParams{
		EntityID: entityID,
		Source:   source,
	}); err != nil {
		log.Printf("enrich: apagar creditos de %s (%s) falhou (segue sem creditos): %v", entityID, source, err)
		return
	}
	s.persistirElenco(ctx, entityID, source, listaDeMapas(campos["cast"]))
	s.persistirEquipe(ctx, entityID, source, listaDeMapas(campos["crew"]))
	s.persistirProdutoras(ctx, entityID, source, listaDeMapas(campos["companies"]))
}

// persistirElenco cria os creditos de ELENCO (kind='cast'), cada um ligado a person
// deduplicada. Itens sem person (sem source_id/nome) sao pulados.
func (s *Servico) persistirElenco(ctx context.Context, entityID, source string, itens []map[string]any) {
	for _, it := range itens {
		personID, ok := s.upsertPessoa(ctx, source, it)
		if !ok {
			continue
		}
		s.criarCredito(ctx, dadosCredito{
			entityID:     entityID,
			personID:     nsValid(personID),
			kind:         kindCast,
			character:    nsOrNull(comoString(it["character"])),
			ordering:     comoInt(it["order"]),
			episodeCount: comoNullInt(it["episode_count"]),
			source:       source,
			sourceID:     nsOrNull(comoString(it["source_id"])),
		})
	}
}

// persistirEquipe cria os creditos de EQUIPE (kind='crew'), cada um ligado a person
// deduplicada.
func (s *Servico) persistirEquipe(ctx context.Context, entityID, source string, itens []map[string]any) {
	for _, it := range itens {
		personID, ok := s.upsertPessoa(ctx, source, it)
		if !ok {
			continue
		}
		s.criarCredito(ctx, dadosCredito{
			entityID:     entityID,
			personID:     nsValid(personID),
			kind:         kindCrew,
			department:   nsOrNull(comoString(it["department"])),
			job:          nsOrNull(comoString(it["job"])),
			episodeCount: comoNullInt(it["episode_count"]),
			source:       source,
			sourceID:     nsOrNull(comoString(it["source_id"])),
		})
	}
}

// persistirProdutoras cria os creditos de PRODUTORA (kind='company'), cada um ligado a
// company deduplicada.
func (s *Servico) persistirProdutoras(ctx context.Context, entityID, source string, itens []map[string]any) {
	for _, it := range itens {
		companyID, ok := s.upsertProdutora(ctx, source, it)
		if !ok {
			continue
		}
		s.criarCredito(ctx, dadosCredito{
			entityID:  entityID,
			companyID: nsValid(companyID),
			kind:      kindCompany,
			source:    source,
			sourceID:  nsOrNull(comoString(it["source_id"])),
		})
	}
}

// upsertPessoa deduplica a pessoa por (source, source_id) e devolve o id da linha
// (existente ou nova). ok=false quando o item nao tem source_id/nome (nada a ligar) ou o
// upsert falha (degrada, loga).
func (s *Servico) upsertPessoa(ctx context.Context, source string, it map[string]any) (string, bool) {
	sourceID := comoString(it["source_id"])
	nome := comoString(it["name"])
	if sourceID == "" || nome == "" {
		return "", false
	}
	payload, _ := json.Marshal(it)
	id, err := s.escrita().UpsertPerson(ctx, sqlcgen.UpsertPersonParams{
		ID:          s.novoID(),
		Source:      source,
		SourceID:    sourceID,
		Name:        nome,
		ProfilePath: nsOrNull(comoString(it["profile_path"])),
		PayloadJson: nsValid(string(payload)),
	})
	if err != nil {
		log.Printf("enrich: upsert person %s (%s) falhou (segue): %v", sourceID, source, err)
		return "", false
	}
	return id, true
}

// upsertProdutora deduplica a produtora por (source, source_id) e devolve o id da linha.
// ok=false quando falta source_id/nome ou o upsert falha.
func (s *Servico) upsertProdutora(ctx context.Context, source string, it map[string]any) (string, bool) {
	sourceID := comoString(it["source_id"])
	nome := comoString(it["name"])
	if sourceID == "" || nome == "" {
		return "", false
	}
	payload, _ := json.Marshal(it)
	id, err := s.escrita().UpsertCompany(ctx, sqlcgen.UpsertCompanyParams{
		ID:          s.novoID(),
		Source:      source,
		SourceID:    sourceID,
		Name:        nome,
		LogoPath:    nsOrNull(comoString(it["logo_path"])),
		PayloadJson: nsValid(string(payload)),
	})
	if err != nil {
		log.Printf("enrich: upsert company %s (%s) falhou (segue): %v", sourceID, source, err)
		return "", false
	}
	return id, true
}

// criarCredito insere UM credito. Best-effort: falha degrada (loga e segue) -- um
// credito perdido nao derruba os outros nem o enrich.
func (s *Servico) criarCredito(ctx context.Context, d dadosCredito) {
	if err := s.escrita().CriarCredito(ctx, sqlcgen.CriarCreditoParams{
		ID:           s.novoID(),
		EntityID:     d.entityID,
		PersonID:     d.personID,
		CompanyID:    d.companyID,
		Kind:         d.kind,
		Department:   d.department,
		Job:          d.job,
		Character:    d.character,
		Ordering:     d.ordering,
		EpisodeCount: d.episodeCount,
		Source:       d.source,
		SourceID:     d.sourceID,
	}); err != nil {
		log.Printf("enrich: criar credito %s de %s falhou (segue): %v", d.kind, d.entityID, err)
	}
}

// listaDeMapas converte um valor do canonico ([]any de map[string]any) na fatia de
// mapas. Tolera o valor ausente/de outro tipo devolvendo nil (o consumidor itera vazio).
func listaDeMapas(v any) []map[string]any {
	lista, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(lista))
	for _, it := range lista {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// comoInt devolve o valor como int64, tolerando int/int64/float64 (o `order` vem int do
// mapeamento em processo, mas float64 quando o canonico passou por JSON). 0 nos demais.
func comoInt(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

// comoNullInt devolve o valor como sql.NullInt64 valido (int/int64/float64) ou NULL
// (ausente/outro tipo) -- para o episode_count opcional (so serie).
func comoNullInt(v any) sql.NullInt64 {
	switch n := v.(type) {
	case int:
		return sql.NullInt64{Int64: int64(n), Valid: true}
	case int64:
		return sql.NullInt64{Int64: n, Valid: true}
	case float64:
		return sql.NullInt64{Int64: int64(n), Valid: true}
	}
	return sql.NullInt64{}
}
