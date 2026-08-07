package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo implementa o AUTO-MATCH da fila de revisão (briefing 0050): dado
// um item de baixa confiança/draft, tenta achar sozinho o candidato certo —
// primeiro no catálogo LOCAL (liga via fundirEntities, nunca duplica — 0026),
// depois na fonte EXTERNA default (aplica pela mesma sequência do "Corrigir" do
// 0018) — e só aplica quando o score bate LimiarAutoMatch. Sem candidato forte
// em nenhuma fonte, o item fica intacto na fila. Individual e lote reusam a
// MESMA lógica interna (tentarAutoMatch); nunca HTTP-to-HTTP.

// LimiarAutoMatch é alias de matching.LimiarAutoMatch (briefing 0054 extraiu a
// pontuação para o pacote leaf internal/matching, consumido também por
// internal/enrich). Mantido aqui para não quebrar chamadores/testes deste
// pacote que já referenciam o símbolo local.
const LimiarAutoMatch = matching.LimiarAutoMatch

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
		listados, err := s.idsElegiveisAutoMatch(ctx)
		if err != nil {
			respondErroInterno(w)
			return
		}
		ids = listados
	}

	writeJSON(w, http.StatusOK, s.autoMatchLote(ctx, ids))
}

// idsElegiveisAutoMatch lista os ids da fila de Revisão elegíveis ao
// auto-match em lote (mesma query de GET /review, limiarBaixaConfianca) --
// reusado por autoMatchReview (corpo vazio) e por AutoMatchElegiveis (job de
// saúde, briefing 0054), para não duplicar a query em dois lugares.
func (s *Server) idsElegiveisAutoMatch(ctx context.Context) ([]string, error) {
	rows, err := s.leitura().ListarRevisao(ctx, sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// autoMatchLote roda tentarAutoMatch para cada id de `ids`, agregando o
// resultado -- MESMA lógica usada por autoMatchReview (HTTP) e por
// AutoMatchElegiveis (job de saúde, briefing 0054). Um id que sumiu entre a
// listagem e o processamento (purga concorrente) é pulado, não derruba o
// lote; erro real de banco NESTE item vira "pendente" no resultado, sem
// abortar os demais.
func (s *Server) autoMatchLote(ctx context.Context, ids []string) autoMatchBatchResultDTO {
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

	return autoMatchBatchResultDTO{
		ResolvedCount: resolvedCount,
		PendingCount:  len(items) - resolvedCount,
		Items:         items,
	}
}

// AutoMatchElegiveis roda o auto-match em lote sobre TODOS os itens
// elegíveis da fila de Revisão (mesma query de POST /review/auto-match sem
// corpo) -- consumido pelo job saude_biblioteca (briefing 0053/0054) através
// da interface libraryhealth.Matcher, que *Server satisfaz estruturalmente
// (sem libraryhealth importar internal/server). err é só erro real de banco
// na LISTAGEM; falha em um item individual já é absorvida por autoMatchLote.
func (s *Server) AutoMatchElegiveis(ctx context.Context) (resolved, pending int, err error) {
	ids, err := s.idsElegiveisAutoMatch(ctx)
	if err != nil {
		return 0, 0, err
	}
	resultado := s.autoMatchLote(ctx, ids)
	return resultado.ResolvedCount, resultado.PendingCount, nil
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

	// Idioma de busca (briefing 0084): a biblioteca de origem da entity pode
	// ter um idioma preferido; sem um, cai no idioma PRINCIPAL global (nunca
	// mais o "" cego de antes deste briefing).
	lang, err := s.idiomaDeBusca(ctx, ent.ID)
	if err != nil {
		return autoMatchResultDTO{}, err
	}

	// 1) Local-first (0026): nunca duplica entity já catalogada. O finder devolve
	// o MAIOR-pontuado não-descartado independente de limiar (briefing 0060, para
	// servir também a prévia sem corte) -- o corte >= LimiarAutoMatch é decidido
	// AQUI, pelo chamador.
	candidatosLocais, err := s.buscarLocal(ctx, titulo, ent.Type, "", "")
	if err != nil {
		return autoMatchResultDTO{}, err
	}
	if alvo, achou := s.melhorCandidatoLocal(ctx, candidatosLocais, ent.ID, titulo, ano); achou && alvo.Score >= LimiarAutoMatch {
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
	externos, disponivel := s.buscarExterno(ctx, titulo, ent.Type, lang)
	if !disponivel {
		return autoMatchNone, nil
	}
	// Mesmo raciocínio do local: o finder devolve o maior-pontuado não-descartado
	// sem corte; o corte >= LimiarAutoMatch é decidido aqui. tituloVencedor não
	// interessa ao auto-match (só à prévia, match_preview.go).
	cand, score, _, achou := s.melhorCandidatoExterno(ctx, externos, titulo, ano)
	if !achou || score < LimiarAutoMatch {
		return autoMatchNone, nil
	}

	return s.aplicarCandidatoExterno(ctx, ent, cand, score)
}

// aplicarCandidatoExterno aplica UM candidato externo já escolhido (score ≥
// LimiarAutoMatch em tentarAutoMatch, ou ≥ LimiarAutoMatch em
// tentarEnriquecerOuSinalizar, briefing 0056) pela MESMA sequência do
// "Corrigir" (0018): resolverColisaoExterna → s.enrich.Enriquecer →
// finalizarRevisao. Extraído do corpo antes inline de tentarAutoMatch para
// ser reusado pelos DOIS fluxos (fila de Revisão e enriquecimento
// retroativo) sem duplicar a sequência de aplicação em dois arquivos —
// único ponto de aplicação externa do pacote. O err de retorno é SÓ erro
// real de banco/infra; colisão com investimento do dono degrada para
// autoMatchNone (pendente), nunca aplica às cegas.
func (s *Server) aplicarCandidatoExterno(ctx context.Context, ent sqlcgen.Entity, cand provider.ResultadoExterno, score float64) (autoMatchResultDTO, error) {
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
	if err := s.enrich.Enriquecer(ctx, ent.ID, cand.Source, cand.SourceID, ent.Type, langs, false); err != nil {
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
// final (0-100) e os campos para compor autoMatchTargetDTO/matchPreviewDTO
// (briefing 0060). PosterURL vem de graça do card da busca local
// (entityCardDTO.PosterURL) -- nenhuma chamada extra.
type candidatoPontuado struct {
	ID        string
	Title     string
	Year      *int
	Score     float64
	PosterURL *string
}

// melhorCandidatoLocal repontua a shortlist da busca LOCAL (buscarLocal — o
// BM25 do FTS5 é só um pré-filtro, nunca o score final) e devolve SEMPRE o
// candidato de MAIOR score entre os não-descartados -- desde o briefing 0060
// o corte por LimiarAutoMatch NÃO é mais feito aqui, e sim pelo CHAMADOR
// (tentarAutoMatch aplica o corte; a prévia de match, matchPreviewDTO, não
// aplica corte nenhum e só marca meets_auto_threshold). Ignora `selfID` (o
// próprio item da fila pode aparecer na shortlist quando o título já bate no
// FTS). Um candidato com ano divergindo mais de 1 ano (matching.Pontuar
// descartado=true) nunca entra no ranking -- essa desqualificação estrutural
// continua valendo sempre. ok=false quando nenhum candidato não-descartado
// existe.
func (s *Server) melhorCandidatoLocal(ctx context.Context, candidatos []entityCardDTO, selfID, titulo string, ano int) (candidatoPontuado, bool) {
	var melhor candidatoPontuado
	achou := false
	for _, c := range candidatos {
		if c.ID == selfID {
			continue
		}
		anoCand := s.anoDaEntity(ctx, c.ID)
		pontos, descartado := matching.Pontuar(titulo, ano, c.Title, anoCand)
		if descartado {
			continue
		}
		if !achou || pontos > melhor.Score {
			melhor = candidatoPontuado{ID: c.ID, Title: c.Title, Score: pontos, PosterURL: c.PosterURL}
			if anoCand > 0 {
				a := anoCand
				melhor.Year = &a
			}
			achou = true
		}
	}
	return melhor, achou
}

// candidatoExternoPontuado é um candidato externo já pontuado por título
// único (matching.Pontuar) -- passo 1 de melhorCandidatoExterno, antes da
// repontuação por títulos alternativos dos top-3.
type candidatoExternoPontuado struct {
	cand  provider.ResultadoExterno
	score float64
}

// candidatosAlternativosTopN é o teto de candidatos que disparam busca de
// títulos alternativos (briefing 0084 §3): custo controlado, 1 chamada de
// rede extra por candidato -- o candidato certo quase sempre está entre os
// top-3 por score de título principal.
const candidatosAlternativosTopN = 3

// melhorCandidatoExterno repontua os resultados da busca EXTERNA (buscarExterno,
// já mesclados/deduplicados por fonte) e devolve SEMPRE o de MAIOR score entre
// os não-descartados -- mesmo raciocínio de melhorCandidatoLocal (0060): o
// corte por LimiarAutoMatch é decisão do CHAMADOR, não deste finder. achou=false
// quando nenhum candidato não-descartado existe.
//
// Desde o briefing 0084, a pontuação passa por DOIS estágios: (1) título único
// (matching.Pontuar) para TODOS os candidatos, ordenados por score desc; (2) os
// TOP-3 (candidatosAlternativosTopN) são repontuados com
// matching.PontuarComAlternativos contra os títulos alternativos/traduzidos que
// o provider conhecer (capacidade OPCIONAL provider.TituloAlternativoProvider,
// best-effort — falha/ausência degrada para o score de título único desse
// candidato, sem abortar a função). O vencedor final é o maior score entre os
// N candidatos do estágio 1 e os 3 repontuados do estágio 2. tituloVencedor é o
// título ALTERNATIVO que produziu o score do vencedor, quando ele bateu o
// título principal do próprio candidato; "" quando o vencedor não passou pela
// repontuação (fora do top-3) ou quando o título principal já era o melhor.
func (s *Server) melhorCandidatoExterno(ctx context.Context, candidatos []provider.ResultadoExterno, titulo string, ano int) (provider.ResultadoExterno, float64, string, bool) {
	pontuados := make([]candidatoExternoPontuado, 0, len(candidatos))
	for _, c := range candidatos {
		anoCand := anoDeStringPtr(c.Year)
		pontos, descartado := matching.Pontuar(titulo, ano, c.Title, anoCand)
		if descartado {
			continue
		}
		pontuados = append(pontuados, candidatoExternoPontuado{cand: c, score: pontos})
	}
	if len(pontuados) == 0 {
		return provider.ResultadoExterno{}, 0, "", false
	}
	sort.SliceStable(pontuados, func(i, j int) bool { return pontuados[i].score > pontuados[j].score })

	melhor := pontuados[0]
	tituloVencedor := ""

	topN := candidatosAlternativosTopN
	if topN > len(pontuados) {
		topN = len(pontuados)
	}
	for i := 0; i < topN; i++ {
		p := pontuados[i]
		alternativos := s.titulosAlternativosDoCandidato(ctx, p.cand)
		if len(alternativos) == 0 {
			continue
		}
		anoCand := anoDeStringPtr(p.cand.Year)
		repontuado, vencedor, descartado := matching.PontuarComAlternativos(titulo, ano, p.cand.Title, alternativos, anoCand)
		if descartado || repontuado <= melhor.score {
			continue
		}
		melhor = candidatoExternoPontuado{cand: p.cand, score: repontuado}
		if vencedor != p.cand.Title {
			tituloVencedor = vencedor
		} else {
			tituloVencedor = ""
		}
	}
	return melhor.cand, melhor.score, tituloVencedor, true
}

// titulosAlternativosDoCandidato busca os títulos alternativos/traduzidos que
// o provider da fonte de `cand` conhecer (capacidade OPCIONAL
// provider.TituloAlternativoProvider, type-assert -- fontes sem essa
// capacidade, ou sem provider registrado sob aquele nome, devolvem nil em
// silêncio). Resultado cacheado em memória por (source,source_id), mesmo TTL
// do cacheExterno (ttlCacheExterno) -- duas buscas pelo MESMO candidato
// dentro do TTL não disparam nova chamada. Erro real de rede é BEST-EFFORT:
// loga e devolve nil (o chamador repontua só com o título principal).
func (s *Server) titulosAlternativosDoCandidato(ctx context.Context, cand provider.ResultadoExterno) []string {
	if s.cacheTitulosAlt == nil {
		return nil
	}
	chave := cand.Source + "\x00" + cand.SourceID
	if v, ok := s.cacheTitulosAlt.obter(chave); ok {
		return v
	}
	prov, ok := s.providers.Get(cand.Source)
	if !ok {
		return nil
	}
	tap, ok := prov.(provider.TituloAlternativoProvider)
	if !ok {
		return nil
	}
	titulos, err := tap.BuscarTitulosAlternativos(ctx, cand.Type, cand.SourceID)
	if err != nil {
		log.Printf("automatch: titulos alternativos de %s %s/%s falharam (best-effort, segue so titulo principal): %v",
			cand.Type, cand.Source, cand.SourceID, err)
		return nil
	}
	s.cacheTitulosAlt.guardar(chave, titulos)
	return titulos
}

// ── cache de titulos alternativos ────────────────────────────────────────────

// cacheTitulosAlternativos é um cache em memória, protegido por mutex, dos
// títulos alternativos de um candidato externo por chave (source+"\x00"+
// source_id), com expiração por TTL -- mesmo desenho de cacheExterno
// (search.go), reaproveitado aqui para não inventar um segundo padrão de
// cache no mesmo pacote.
type cacheTitulosAlternativos struct {
	mu    sync.Mutex
	ttl   time.Duration
	itens map[string]entradaCacheTitulosAlternativos
	agora func() time.Time
}

// entradaCacheTitulosAlternativos é uma entrada do cache: os títulos e o
// instante de expiração.
type entradaCacheTitulosAlternativos struct {
	titulos []string
	expira  time.Time
}

// novoCacheTitulosAlternativos cria um cache com o TTL dado.
func novoCacheTitulosAlternativos(ttl time.Duration) *cacheTitulosAlternativos {
	return &cacheTitulosAlternativos{ttl: ttl, itens: make(map[string]entradaCacheTitulosAlternativos), agora: time.Now}
}

// obter devolve os títulos cacheados e true quando a chave existe e não
// expirou; caso contrário (nil, false).
func (c *cacheTitulosAlternativos) obter(chave string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.itens[chave]
	if !ok || c.agora().After(e.expira) {
		return nil, false
	}
	return e.titulos, true
}

// guardar grava (ou sobrescreve) os títulos da chave com nova expiração.
func (c *cacheTitulosAlternativos) guardar(chave string, titulos []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.itens[chave] = entradaCacheTitulosAlternativos{titulos: titulos, expira: c.agora().Add(c.ttl)}
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
