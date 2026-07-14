// Este arquivo implementa o CASAMENTO POR NOME contra drafts locais no import
// manual (briefing 0054): antes de criar uma entity nova, `importarItemBase`
// tenta achar um draft PURO (sem NENHUM external_id, mesmo `type`) já
// escaneado com o mesmo título/ano — reaproveitando a MESMA pontuação do
// auto-match da fila de Revisão (0050), agora extraída para o pacote leaf
// `internal/matching` (sem ciclo de import com internal/server).
package enrich

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/rafaelvieiras/my-media-db/internal/matching"
)

// limiteCandidatosDraft é o teto de candidatos trazidos pelo FTS5 antes da
// repontuação — pré-filtro generoso o bastante para não perder o candidato
// certo em bibliotecas grandes, sem trazer a tabela inteira.
const limiteCandidatosDraft = 25

// candidatoDraft é um candidato local ao casamento por nome: apenas os campos
// que a pontuação (internal/matching.Pontuar) precisa. Year é 0 quando o
// draft não tem release_date gravada ainda (draft puro, comum).
type candidatoDraft struct {
	ID    string
	Title string
	Year  int
}

// buscarDraftsCandidatosSQL é a query do casamento por nome: FTS5 sobre
// entity_fts, restrita a drafts PUROS (external_ids_json IS NULL) do MESMO
// type. Deliberadamente enxuta (só id/title/release_date) — não é a mesma
// query da busca (internal/server/search.go, buscarCardsFTSSQL), que serve o
// card inteiro; este pacote não tem acesso a entityCardDTO (evita ciclo com
// internal/server, que já importa internal/enrich).
const buscarDraftsCandidatosSQL = `SELECT
    e.id, e.title, e.release_date
FROM entity_fts
JOIN entity e ON e.id = entity_fts.entity_id
WHERE entity_fts MATCH ?
  AND e.type = ?
  AND e.external_ids_json IS NULL
ORDER BY entity_fts.rank
LIMIT ?;`

// buscarDraftsCandidatos roda o MATCH FTS5 sanitizado no pool de LEITURA e
// devolve os candidatos (drafts puros do mesmo type). Título sem nenhum termo
// pesquisável (só pontuação/operadores) devolve lista vazia sem tocar o banco.
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

// anoDoPayload extrai o ano de release_date do detalhe CRU do provider (antes
// de qualquer entity existir — não há coluna gravada para ler ainda, ao
// contrário de internal/server que lê da entity já persistida). Fonte sem
// provider, campo ausente/vazio ou não-string devolvem 0 (degrada para "sem
// ano", igual ao resto do pacote).
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

// tentarCasarComDraftLocal busca candidatos locais (drafts puros do mesmo
// type) e repontua cada um com internal/matching.Pontuar, devolvendo o de
// MAIOR score que bate LimiarAutoMatch. ok=false quando nenhum candidato
// qualifica (ou a busca não achou nada) — sem side-effect algum, quem aplica
// o candidato é o chamador (importarItemBase). Mesmo desenho de
// melhorCandidatoLocal (internal/server/automatch.go), adaptado ao shape
// mínimo deste pacote.
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

// sanitizarMatchFTS transforma o texto do usuário numa expressão MATCH SEGURA
// do FTS5: quebra em termos por espaço, descarta termos sem nenhuma
// letra/dígito e envolve cada termo restante numa FRASE entre aspas com
// prefixo (`"termo"*`), evitando que aspas/parênteses/operadores do input
// quebrem a sintaxe da consulta. Mesma lógica de
// internal/server/search.go:sanitizarMatchFTS (não importável daqui — este
// pacote não pode depender de internal/server, ciclo). Devolve ok=false
// quando não sobra termo pesquisável.
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

// temAlfanumerico diz se a string tem ao menos uma letra ou dígito unicode —
// garante que a frase FTS5 gerada não seja vazia (evita erro de sintaxe).
func temAlfanumerico(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
