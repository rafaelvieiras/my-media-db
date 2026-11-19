package enrich

// This file holds the MATCH ALERTS: the two reasons that migration 0029 added to the
// suggested match, both discovered from data the product already has (ADR-0038).
//
// It is the same mechanism migration 0028 introduced for a misspelled name (a persisted
// hint, never applied, shown on the item screen with a way out), used twice more:
//
//   MotivoArvoreVazia        the source answered a series with ZERO episodes. Accepting
//                            that match is useless by construction: no file can ever be
//                            placed in a tree that has no episodes.
//   MotivoArvoreIncompativel the tree exists but does not fit the files: the series
//                            accumulated as many provisional episodes as cataloged ones.
//
// Both write the row and RETURN: nothing changes identity, nothing is applied. Refusing
// the match outright was considered and rejected -- see the ADR: revoking the identity
// would make the next scan try the same name again, pick the same broken entry and revoke
// it again, a loop that costs a tree fetch per scan and leaves the series a draft forever.

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strconv"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Motivos do alerta (coluna match_suggestion.reason).
const (
	// MotivoNome is the original hint of migration 0028 (a cheaper variant of the name).
	MotivoNome = "name"
	// MotivoArvoreVazia is a match whose source returned zero episodes.
	MotivoArvoreVazia = "empty_tree"
	// MotivoArvoreIncompativel is a match whose tree exists but does not fit the files.
	MotivoArvoreIncompativel = "tree_mismatch"
)

// SinalizarArvoreInutil persists the alert of a series whose source returned ZERO episodes.
// It looks for ANOTHER candidate of the same name (the phantom entry usually has a real
// sibling: measured, "Aladdin" #14383 has 0 episodes while "Aladdin: A Série Animada"
// #2745 has 86) and stores it as a one-click fix. When no sibling reaches the suggestion
// threshold, the row describes the CURRENT match instead -- the screen then shows the
// warning without an apply button, which is still better than silence.
//
// Best-effort: any failure is a log line. The tree has already failed on its own.
func (s *Servico) SinalizarArvoreInutil(ctx context.Context, entityID, source, sourceID string, langs []string) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		log.Printf("enrich: ler %s para sinalizar arvore vazia falhou (%v)", entityID, err)
		return
	}
	candidato, score, consulta, ok := s.alternativaDeMesmoNome(ctx, ent, source, sourceID, langs)
	if !ok {
		// No usable sibling: describe the match under suspicion. Score 0 and the empty
		// query keep the row honest -- nothing was "found".
		candidato = provider.ResultadoExterno{Source: source, SourceID: sourceID, Type: tipoSerie, Title: ent.Title}
		score, consulta = 0, ""
	}
	params := sqlcgen.UpsertMatchSuggestionParams{
		EntityID:  entityID,
		Source:    candidato.Source,
		SourceID:  candidato.SourceID,
		Title:     candidato.Title,
		Year:      nsDeTexto(candidato.Year),
		PosterUrl: nsDeTexto(candidato.PosterURL),
		Score:     int64(score + 0.5),
		Query:     consulta,
		Reason:    MotivoArvoreVazia,
	}
	if err := s.gravarAlerta(ctx, params); err != nil {
		log.Printf("enrich: gravar alerta de arvore vazia de %s falhou (%v)", entityID, err)
		return
	}
	log.Printf("enrich: serie %q casada com %s:%s que NAO tem episodios -- alerta gravado (candidato %s:%s)",
		ent.Title, source, sourceID, candidato.Source, candidato.SourceID)
}

// SinalizarArvoreIncompativel persists the alert of a series that HAS a tree but does not
// fit the files. The candidate stored is the CURRENT identity: it is the thing under
// suspicion, and the API turns "same identity" into "no apply button".
func (s *Servico) SinalizarArvoreIncompativel(ctx context.Context, entityID string, provisorios, catalogados int) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		log.Printf("enrich: ler %s para sinalizar arvore incompativel falhou (%v)", entityID, err)
		return
	}
	source, sourceID, ok := identidadePrincipal(ent.ExternalIdsJson)
	if !ok {
		return // cannot describe a suspicion without knowing who is suspected
	}
	params := sqlcgen.UpsertMatchSuggestionParams{
		EntityID:       entityID,
		Source:         source,
		SourceID:       sourceID,
		Title:          ent.Title,
		Year:           nsDeTexto(anoDeData(ent.ReleaseDate)),
		Score:          0,
		Query:          "",
		Reason:         MotivoArvoreIncompativel,
		FilesUnmatched: int64(provisorios),
		FilesTotal:     int64(provisorios + catalogados),
	}
	if err := s.gravarAlerta(ctx, params); err != nil {
		log.Printf("enrich: gravar alerta de arvore incompativel de %s falhou (%v)", entityID, err)
		return
	}
	log.Printf("enrich: serie %q (%s:%s) -- %d de %d episodios nao casaram com a arvore; alerta gravado",
		ent.Title, source, sourceID, provisorios, provisorios+catalogados)
}

// gravarAlerta writes the row. Shared by both reasons so the two signals cannot drift.
func (s *Servico) gravarAlerta(ctx context.Context, params sqlcgen.UpsertMatchSuggestionParams) error {
	return s.escrita().UpsertMatchSuggestion(ctx, params)
}

// alternativaDeMesmoNome searches the SOURCE of the current match for the same name and
// returns the best OTHER candidate that reaches matching.LimiarSugestao. ok=false when the
// name has nothing better to offer (a single-entry name, a source error, a weak sibling).
//
// One query, at most: this runs when a match is being applied (once per item), never in a
// loop over the review queue.
func (s *Servico) alternativaDeMesmoNome(ctx context.Context, ent sqlcgen.Entity, source, sourceID string, langs []string) (provider.ResultadoExterno, float64, string, bool) {
	prov, ok := s.provFonte(source)
	if !ok {
		return provider.ResultadoExterno{}, 0, "", false
	}
	nome := strings.TrimSpace(ent.Title)
	if nome == "" {
		return provider.ResultadoExterno{}, 0, "", false
	}
	res, err := prov.Buscar(ctx, nome, tipoSerie, langPrincipal(langs))
	if err != nil {
		log.Printf("enrich: buscar alternativa de %q na fonte %q falhou (segue): %v", nome, source, err)
		return provider.ResultadoExterno{}, 0, "", false
	}
	ano := matching.AnoDeReleaseDate(ent.ReleaseDate.String)
	melhor := provider.ResultadoExterno{}
	scoreMelhor := 0.0
	for _, r := range res {
		if r.Type != tipoSerie || r.SourceID == sourceID {
			continue
		}
		anoCand, _ := strconv.Atoi(strings.TrimSpace(deref(r.Year)))
		// The original title counts too: the sibling of a phantom entry is often titled
		// "Aladdin: A Série Animada" in the library language while the name on disk is
		// "Aladdin" -- scoring only the localized title rejects the right item.
		score, _, descartado := matching.PontuarComAlternativos(nome, ano, r.Title, []string{r.TituloOriginal}, anoCand)
		if descartado || score <= scoreMelhor {
			continue
		}
		melhor, scoreMelhor = r, score
	}
	if scoreMelhor < matching.LimiarSugestao {
		return provider.ResultadoExterno{}, 0, "", false
	}
	return melhor, scoreMelhor, nome, true
}

// identidadePrincipal returns the source:id of the CURRENT match, by the same priority the
// rest of the code uses (tmdb first: it is the tree source of this project). ok=false when
// the entity carries no id at all -- there is nothing to suspect.
func identidadePrincipal(externos sql.NullString) (string, string, bool) {
	ids := jsonIDs(externos)
	for _, fonte := range []string{"tmdb", "tvdb", "imdb", "mbid"} {
		if v := comoString(ids[fonte]); v != "" {
			return fonte, v, true
		}
	}
	return "", "", false
}

// anoDeData extracts the year of an ISO date ("1994-02-06" -> "1994"). nil when empty or
// unparseable -- every caller treats the year as optional.
func anoDeData(data sql.NullString) *string {
	if !data.Valid || len(data.String) < 4 {
		return nil
	}
	ano := data.String[:4]
	if _, err := strconv.Atoi(ano); err != nil {
		return nil
	}
	return &ano
}

// deref dereferences an optional string, returning "" when absent.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// nsDeTexto converts an optional string to a nullable column value.
func nsDeTexto(p *string) sql.NullString {
	if p == nil || *p == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

// jsonIDs unmarshals external_ids_json into a map, tolerating garbage.
func jsonIDs(bruto sql.NullString) map[string]any {
	if !bruto.Valid || bruto.String == "" {
		return nil
	}
	m := map[string]any{}
	if json.Unmarshal([]byte(bruto.String), &m) != nil {
		return nil
	}
	return m
}
