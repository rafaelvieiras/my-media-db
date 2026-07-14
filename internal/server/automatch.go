package server

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/textsim"
)

// Este arquivo implementa o AUTO-MATCH da fila de revisão (briefing 0050): dado
// um item de baixa confiança/draft, tenta achar sozinho o candidato certo —
// primeiro no catálogo LOCAL (liga via fundirEntities, nunca duplica — 0026),
// depois na fonte EXTERNA default (aplica pela mesma sequência do "Corrigir" do
// 0018) — e só aplica quando o score bate LimiarAutoMatch. Sem candidato forte
// em nenhuma fonte, o item fica intacto na fila. Individual e lote reusam a
// MESMA lógica interna (tentarAutoMatch); nunca HTTP-to-HTTP.

// LimiarAutoMatch é o corte de score (escala 0-100, igual à de match_confidence)
// acima do qual o auto-match aplica o candidato SOZINHO. Deliberadamente bem
// acima de limiarBaixaConfianca (70, review.go): o custo de um falso-positivo
// (contamina o catálogo em silêncio) é muito maior que o de um falso-negativo
// (item que já estava visível na fila continua lá).
const LimiarAutoMatch = 92.0

// autoMatchTargetDTO identifica o candidato aplicado (local ou externo) na
// resposta do auto-match. Year é nil quando a fonte não informa ano.
type autoMatchTargetDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Year  *int   `json:"year"`
}

// autoMatchResultDTO é a resposta de POST /entities/{id}/auto-match — SEMPRE
// 200 (mesmo sem candidato: é um resultado válido, não erro).
type autoMatchResultDTO struct {
	Resolved bool                `json:"resolved"`
	Method   string              `json:"method"` // "local" | "external" | "none"
	Score    float64             `json:"score"`
	Target   *autoMatchTargetDTO `json:"target"`
}

// autoMatchNone é o resultado neutro: nenhum candidato bateu o limiar em
// nenhuma fonte (ou não havia o que buscar — sem arquivo/título derivável).
var autoMatchNone = autoMatchResultDTO{Resolved: false, Method: "none", Score: 0, Target: nil}

// autoMatchBatchBody é o corpo OPCIONAL de POST /review/auto-match: os ids a
// processar. Omitido/vazio processa TODOS os elegíveis (ListarRevisao, mesma
// fonte de GET /review).
type autoMatchBatchBody struct {
	IDs []string `json:"ids"`
}

// autoMatchBatchItemDTO é o resultado de UM item do lote.
type autoMatchBatchItemDTO struct {
	ID       string  `json:"id"`
	Resolved bool    `json:"resolved"`
	Method   string  `json:"method"`
	Score    float64 `json:"score"`
}

// autoMatchBatchResultDTO é a resposta de POST /review/auto-match: o resumo
// agregado do lote.
type autoMatchBatchResultDTO struct {
	ResolvedCount int                     `json:"resolved_count"`
	PendingCount  int                     `json:"pending_count"`
	Items         []autoMatchBatchItemDTO `json:"items"`
}

// autoMatchEntity trata POST /api/v1/entities/{id}/auto-match: tenta resolver
// sozinho UM item da fila. 404 se a entity não existe; 500 só em erro REAL de
// banco — "não achou candidato" é 200 {resolved:false}, nunca erro.
func (s *Server) autoMatchEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	resultado, err := s.tentarAutoMatch(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, resultado)
}

// autoMatchReview trata POST /api/v1/review/auto-match: o LOTE. Corpo
// `{"ids":[...]}` opcional — omitido/vazio processa TODOS os elegíveis via
// ListarRevisao (mesma query de GET /review). Cada id roda a MESMA lógica
// interna do endpoint individual (tentarAutoMatch); um id que sumiu entre a
// listagem e o processamento (purga concorrente) é pulado, não derruba o lote.
func (s *Server) autoMatchReview(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()

	var body autoMatchBatchBody
	if ok := decodeOpcional(w, r, &body); !ok {
		return
	}

	ids := body.IDs
	if len(ids) == 0 {
		rows, err := s.leitura().ListarRevisao(ctx, sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true})
		if err != nil {
			respondErroInterno(w)
			return
		}
		ids = make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
	}

	items := make([]autoMatchBatchItemDTO, 0, len(ids))
	resolvedCount := 0
	for _, id := range ids {
		ent, err := s.leitura().ObterEntity(ctx, id)
		if err != nil {
			continue // sumiu ou id inválido no corpo -- idempotente, pula
		}
		resultado, err := s.tentarAutoMatch(ctx, ent)
		if err != nil {
			// erro real de banco NESTE item: registra como pendente e segue o
			// lote -- um item ruim não deveria derrubar os outros 29.
			items = append(items, autoMatchBatchItemDTO{ID: id, Resolved: false, Method: "none", Score: 0})
			continue
		}
		if resultado.Resolved {
			resolvedCount++
		}
		items = append(items, autoMatchBatchItemDTO{
			ID: id, Resolved: resultado.Resolved, Method: resultado.Method, Score: resultado.Score,
		})
	}

	writeJSON(w, http.StatusOK, autoMatchBatchResultDTO{
		ResolvedCount: resolvedCount,
		PendingCount:  len(items) - resolvedCount,
		Items:         items,
	})
}

// ── Orquestração compartilhada ───────────────────────────────────────────────

// tentarAutoMatch tenta resolver `ent` sozinho: deriva título/ano do arquivo,
// busca local-first, repontua e aplica (fundirEntities) se bater o limiar;
// senão busca externo, repontua e aplica pela sequência do "Corrigir" (0018)
// se bater o limiar; senão devolve autoMatchNone. O err de retorno é SÓ erro
// real de banco/infra — "nenhum candidato" nunca é err.
//
// Os helpers acoplados a escrita HTTP (resolverColisaoExterna, idiomasPermitidos)
// são chamados com um capturaResposta descartável, NUNCA com o ResponseWriter
// real da requisição: uma colisão que exige decisão do dono (409, rede de
// segurança do 0051) não deve ser aplicada às cegas pelo auto-match — o item
// fica pendente na fila, como qualquer candidato que não bateu o limiar.
func (s *Server) tentarAutoMatch(ctx context.Context, ent sqlcgen.Entity) (autoMatchResultDTO, error) {
	titulo, ano, ok, err := s.tituloAnoDoArquivo(ctx, ent.ID)
	if err != nil {
		return autoMatchResultDTO{}, err
	}
	if !ok {
		return autoMatchNone, nil
	}

	// 1) Local-first (0026): nunca duplica entity já catalogada.
	candidatosLocais, err := s.buscarLocal(ctx, titulo, ent.Type, "", "")
	if err != nil {
		return autoMatchResultDTO{}, err
	}
	if alvo, achou := s.melhorCandidatoLocal(ctx, candidatosLocais, ent.ID, titulo, ano); achou {
		if err := s.fundirEntities(ctx, alvo.ID, ent.ID); err != nil {
			return autoMatchResultDTO{}, err
		}
		return autoMatchResultDTO{
			Resolved: true, Method: "local", Score: alvo.Score,
			Target: &autoMatchTargetDTO{ID: alvo.ID, Title: alvo.Title, Year: alvo.Year},
		}, nil
	}

	// 2) Externo: só quando local não bateu. Sem provider capaz do tipo, ou sem
	// enrich ligado (assetsDir ausente), não há como aplicar -- degrada a "none".
	if s.enrich == nil {
		return autoMatchNone, nil
	}
	externos, disponivel := s.buscarExterno(ctx, titulo, ent.Type, "")
	if !disponivel {
		return autoMatchNone, nil
	}
	cand, score, achou := melhorCandidatoExterno(externos, titulo, ano)
	if !achou {
		return autoMatchNone, nil
	}

	captura := &capturaResposta{}
	if _, ok := s.resolverColisaoExterna(captura, ctx, ent.ID, ent.Type, cand.Source, cand.SourceID); !ok {
		if captura.status != http.StatusConflict {
			return autoMatchResultDTO{}, errStatus(captura.status)
		}
		// Colisão com investimento do dono na outra entity: não aplica às
		// cegas, item fica pendente para resolução manual.
		return autoMatchNone, nil
	}

	langs, err := s.idiomasPermitidos(ctx, captura)
	if err != nil {
		return autoMatchResultDTO{}, err
	}
	if err := s.enrich.Enriquecer(ctx, ent.ID, cand.Source, cand.SourceID, ent.Type, langs); err != nil {
		// Fonte falhou ao entregar o detalhe -- não é erro do auto-match em si
		// (a fonte pode se recuperar depois); o item fica pendente.
		return autoMatchNone, nil
	}
	if err := s.finalizarRevisao(ctx, ent.ID); err != nil {
		return autoMatchResultDTO{}, err
	}

	var anoAlvo *int
	if a := anoDeStringPtr(cand.Year); a > 0 {
		anoAlvo = &a
	}
	return autoMatchResultDTO{
		Resolved: true, Method: "external", Score: score,
		Target: &autoMatchTargetDTO{ID: ent.ID, Title: cand.Title, Year: anoAlvo},
	}, nil
}

// tituloAnoDoArquivo deriva título/ano do PRIMEIRO arquivo ligado à entity via
// naming.Parse (o MESMO parser do scan, briefing 0009) — não confia no
// entity.title bruto de um draft. ok=false (sem err) quando a entity não tem
// nenhum arquivo, ou o nome não rende título nenhum (ex.: "2160p.mkv"): nesses
// casos não há o que buscar, e isso NÃO é uma falha do auto-match.
func (s *Server) tituloAnoDoArquivo(ctx context.Context, entityID string) (titulo string, ano int, ok bool, err error) {
	files, err := s.leitura().ListarMediaFilesPorEntity(ctx, entityID)
	if err != nil {
		return "", 0, false, err
	}
	if len(files) == 0 {
		return "", 0, false, nil
	}
	nome := strings.TrimSuffix(filepath.Base(files[0].Path), filepath.Ext(files[0].Path))
	info := naming.Parse(nome)
	if info.Title == "" {
		return "", 0, false, nil
	}
	return info.Title, info.Year, true, nil
}

// candidatoPontuado é o melhor candidato local encontrado, já com o score
// final (0-100) e os campos para compor autoMatchTargetDTO.
type candidatoPontuado struct {
	ID    string
	Title string
	Year  *int
	Score float64
}

// melhorCandidatoLocal repontua a shortlist da busca LOCAL (buscarLocal — o
// BM25 do FTS5 é só um pré-filtro, nunca o score final) e devolve o de MAIOR
// score que bate LimiarAutoMatch. Ignora `selfID` (o próprio item da fila pode
// aparecer na shortlist quando o título já bate no FTS). ok=false quando
// nenhum candidato qualifica.
func (s *Server) melhorCandidatoLocal(ctx context.Context, candidatos []entityCardDTO, selfID, titulo string, ano int) (candidatoPontuado, bool) {
	var melhor candidatoPontuado
	achou := false
	for _, c := range candidatos {
		if c.ID == selfID {
			continue
		}
		anoCand := s.anoDaEntity(ctx, c.ID)
		pontos, descartado := pontuarCandidato(titulo, ano, c.Title, anoCand)
		if descartado || pontos < LimiarAutoMatch {
			continue
		}
		if !achou || pontos > melhor.Score {
			melhor = candidatoPontuado{ID: c.ID, Title: c.Title, Score: pontos}
			if anoCand > 0 {
				a := anoCand
				melhor.Year = &a
			}
			achou = true
		}
	}
	return melhor, achou
}

// melhorCandidatoExterno repontua os resultados da busca EXTERNA (buscarExterno,
// já mesclados/deduplicados por fonte) e devolve o de MAIOR score que bate
// LimiarAutoMatch. ok=false quando nenhum candidato qualifica.
func melhorCandidatoExterno(candidatos []provider.ResultadoExterno, titulo string, ano int) (provider.ResultadoExterno, float64, bool) {
	var melhor provider.ResultadoExterno
	melhorScore := -1.0
	achou := false
	for _, c := range candidatos {
		anoCand := anoDeStringPtr(c.Year)
		pontos, descartado := pontuarCandidato(titulo, ano, c.Title, anoCand)
		if descartado || pontos < LimiarAutoMatch {
			continue
		}
		if pontos > melhorScore {
			melhor, melhorScore, achou = c, pontos, true
		}
	}
	return melhor, melhorScore, achou
}

// pontuarCandidato calcula o score final [0,100] do briefing 0050 §4:
// similaridade Jaro-Winkler do título (textsim) em escala 0-100, ajustada pelo
// ano. descartado=true quando os DOIS anos existem e divergem em MAIS de 1 ano
// (remake/refilmagem não deve colar, mesmo com título idêntico) — o candidato
// nunca entra no ranking nesse caso. Anos iguais somam +5 (cap em 100);
// diferença de exatamente 1 ano não ajusta (tolerância a erro comum de
// catalogação); qualquer ano ausente também não ajusta (nada para comparar).
func pontuarCandidato(tituloConsulta string, anoConsulta int, tituloCandidato string, anoCandidato int) (score float64, descartado bool) {
	score = textsim.Similaridade(tituloConsulta, tituloCandidato) * 100
	if anoConsulta > 0 && anoCandidato > 0 {
		diff := anoConsulta - anoCandidato
		if diff < 0 {
			diff = -diff
		}
		switch {
		case diff > 1:
			return 0, true
		case diff == 0:
			score += 5
			if score > 100 {
				score = 100
			}
		}
	}
	return score, false
}

// anoDaEntity lê o release_date da entity (coluna núcleo, não o override
// resolvido — o auto-match roda ANTES de qualquer edição, então a base já
// reflete o melhor que o catálogo sabe) e devolve os 4 primeiros dígitos como
// ano; 0 quando ausente/inválido/erro (degrada para "sem ano", nunca falha o
// auto-match por um vizinho sem data).
func (s *Server) anoDaEntity(ctx context.Context, id string) int {
	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		return 0
	}
	return anoDeReleaseDate(ent.ReleaseDate)
}

// anoDeReleaseDate extrai o ano dos 4 primeiros dígitos de um release_date
// "YYYY-MM-DD" (ou "YYYY"); 0 quando ausente/curto/inválido.
func anoDeReleaseDate(rd sql.NullString) int {
	if !rd.Valid || len(rd.String) < 4 {
		return 0
	}
	ano, err := strconv.Atoi(rd.String[:4])
	if err != nil {
		return 0
	}
	return ano
}

// anoDeStringPtr extrai o ano de provider.ResultadoExterno.Year ("YYYY" ou
// nil); 0 quando ausente/inválido.
func anoDeStringPtr(s *string) int {
	if s == nil {
		return 0
	}
	ano, err := strconv.Atoi(*s)
	if err != nil {
		return 0
	}
	return ano
}

// ── capturaResposta ──────────────────────────────────────────────────────────

// capturaResposta é um http.ResponseWriter mínimo que só CAPTURA o status
// (nunca escreve na resposta real): usado para rodar helpers acoplados a
// escrita HTTP (resolverColisaoExterna, idiomasPermitidos) sem que uma escrita
// de ERRO interna deles vaze para a resposta final do auto-match — o auto-match
// sempre decide sozinho o que responder (200 com resolved:false no individual;
// item pendente no lote), nunca repassando o corpo de erro alheio.
type capturaResposta struct {
	header http.Header
	status int
}

func (c *capturaResposta) Header() http.Header {
	if c.header == nil {
		c.header = http.Header{}
	}
	return c.header
}

func (c *capturaResposta) Write(b []byte) (int, error) { return len(b), nil }

func (c *capturaResposta) WriteHeader(status int) { c.status = status }

// errStatus é um erro mínimo que carrega o status HTTP capturado por
// capturaResposta, só para o log/erro interno — o chamador sempre degrada para
// respondErroInterno (500), o status aqui é só contexto de diagnóstico.
type errStatus int

func (e errStatus) Error() string {
	return "auto-match: helper interno escreveu status " + strconv.Itoa(int(e))
}
