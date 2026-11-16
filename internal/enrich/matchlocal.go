// This file implements the NAME MATCHING against local drafts on manual import:
// before creating a new entity, `importarItemBase` tries to find a PURE draft (with NO
// external_id, same `type`) already scanned with the same title/year — reusing the SAME
// scoring of the auto-match of the Revisão queue, now extracted into the leaf package
// `internal/matching` (no import cycle with internal/server).
package enrich

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/rafaelvieiras/my-media-db/internal/matching"
)

// limiteCandidatosDraft is the ceiling of candidates brought by FTS5 before the
// re-scoring — a pre-filter generous enough not to lose the right candidate in large
// libraries, without pulling the whole table.
const limiteCandidatosDraft = 25

// candidatoDraft is a local candidate for the name matching: only the fields that the
// scoring (internal/matching.Pontuar) needs. Year is 0 when the draft has no release_date
// recorded yet (a pure draft, the common case).
type candidatoDraft struct {
	ID    string
	Title string
	Year  int
}

// buscarDraftsCandidatosSQL is the name-matching query: FTS5 over entity_fts, restricted
// to PURE drafts (external_ids_json IS NULL) of the SAME type. Deliberately lean (only
// id/title/release_date) — it is not the same query as the search
// (internal/server/search.go, buscarCardsFTSSQL), which serves the whole card; this
// package has no access to entityCardDTO (avoids a cycle with internal/server, which
// already imports internal/enrich).
const buscarDraftsCandidatosSQL = `SELECT
    e.id, e.title, e.release_date
FROM entity_fts
JOIN entity e ON e.id = entity_fts.entity_id
WHERE entity_fts MATCH ?
  AND e.type = ?
  AND e.external_ids_json IS NULL
ORDER BY entity_fts.rank
LIMIT ?;`

// buscarDraftsCandidatos runs the sanitized FTS5 MATCH on the READ pool and returns the
// candidates (pure drafts of the same type). A title with no searchable term (only
// punctuation/operators) returns an empty list without touching the database.
func (s *Servico) buscarDraftsCandidatos(ctx context.Context, titulo, tipo string) ([]candidatoDraft, error) {
	match, ok := sanitizarMatchFTS(titulo)
	if !ok {
		return nil, nil
	}
	rows, err := s.database.Read.QueryContext(ctx, buscarDraftsCandidatosSQL, match, tipo, int64(limiteCandidatosDraft))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []candidatoDraft
	for rows.Next() {
		var id, title string
		var releaseDate sql.NullString
		if err := rows.Scan(&id, &title, &releaseDate); err != nil {
			return nil, err
		}
		ano := 0
		if releaseDate.Valid {
			ano = matching.AnoDeReleaseDate(releaseDate.String)
		}
		out = append(out, candidatoDraft{ID: id, Title: title, Year: ano})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// anoDoPayload extracts the release_date year from the RAW detail of the provider (before
// any entity exists — there is no written column to read yet, unlike internal/server, which
// reads from the entity already persisted). A source without a provider, a missing/empty or
// non-string field return 0 (degrading to "no year", like the rest of the package).
func (s *Servico) anoDoPayload(source string, raw json.RawMessage) int {
	prov, ok := s.provFonte(source)
	if !ok {
		return 0
	}
	campos, err := prov.MapearDetalhe(raw)
	if err != nil {
		return 0
	}
	rd, ok := campos["release_date"].(string)
	if !ok || rd == "" {
		return 0
	}
	return matching.AnoDeReleaseDate(rd)
}

// tentarCasarComDraftLocal looks for local candidates (pure drafts of the same type) and
// re-scores each one with internal/matching.Pontuar, returning the one with the HIGHEST
// score that meets LimiarAutoMatch. ok=false when no candidate qualifies (or the search
// found nothing) — with no side-effect at all, the one that applies the candidate is the
// caller (importarItemBase). Same design as melhorCandidatoLocal
// (internal/server/automatch.go), adapted to the minimal shape of this package.
func (s *Servico) tentarCasarComDraftLocal(ctx context.Context, titulo string, ano int, tipo string) (id string, achou bool, err error) {
	candidatos, err := s.buscarDraftsCandidatos(ctx, titulo, tipo)
	if err != nil {
		return "", false, err
	}
	melhorScore := -1.0
	for _, c := range candidatos {
		pontos, descartado := matching.Pontuar(titulo, ano, c.Title, c.Year)
		if descartado || pontos < matching.LimiarAutoMatch {
			continue
		}
		if pontos > melhorScore {
			id, melhorScore, achou = c.ID, pontos, true
		}
	}
	return id, achou, nil
}

// sanitizarMatchFTS turns the user text into a SAFE FTS5 MATCH expression: it splits it
// into terms by whitespace, discards terms with no letter/digit and wraps each remaining
// term in a quoted PHRASE with a prefix (`"term"*`), preventing quotes/parentheses/operators
// from the input from breaking the query syntax. Same logic as
// internal/server/search.go:sanitizarMatchFTS (not importable from here — this package
// cannot depend on internal/server, cycle). It returns ok=false when no searchable term is
// left.
func sanitizarMatchFTS(termo string) (string, bool) {
	limpo := strings.ReplaceAll(termo, `"`, " ")
	campos := strings.Fields(limpo)
	frases := make([]string, 0, len(campos))
	for _, c := range campos {
		if !temAlfanumerico(c) {
			continue
		}
		frases = append(frases, `"`+c+`"*`)
	}
	if len(frases) == 0 {
		return "", false
	}
	return strings.Join(frases, " "), true
}

// temAlfanumerico tells whether the string has at least one unicode letter or digit — it
// ensures the generated FTS5 phrase is not empty (avoids a syntax error).
func temAlfanumerico(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
