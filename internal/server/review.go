package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa a CURADORIA da Fase 3 (briefing 0018): as filas de
// revisao e de orfas (queries), mais as acoes confirmar/corrigir e a PURGA
// (individual e em lote — a unica porta destrutiva, sempre explicita, ADR-0012).
// As leituras passam pelo pool de leitura; confirmar/corrigir/purgar escrevem pelo
// pool de escrita (ADR-0010). Correcao reusa o serviço de enrich (0015) e o
// resolverEntity, sem nenhum caminho de escrita novo.

// limiarBaixaConfianca e o corte de confianca que separa "palpite alto" de "baixa
// confianca" (escala 0-100 do scanner). ESPELHA o LIMIAR_BAIXA_CONFIANCA=70 do
// front (web/src/lib/entity-badges.ts): os dois DEVEM casar. Acoplamento anotado —
// candidato a virar config/endpoint no futuro (fora deste briefing). Fonte unica no
// backend: a query ListarRevisao o recebe como parametro.
const limiarBaixaConfianca = 70

// confiancaConfirmada e o valor de confianca que a acao "confirmar" grava nos
// media_file da entity: a autoridade do dono (100) tira o item da fila de revisao.
const confiancaConfirmada = 100

// correctBody e o corpo de POST /entities/{id}/correct: a identidade correta que o
// dono informa. Sem source, a fonte-default e resolvida por capacidade (fonteDefault,
// 0047); source_id obrigatorio (422 se vazio).
type correctBody struct {
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
}

// purgeBatchBody e o corpo de POST /entities/purge: os ids a purgar em lote.
type purgeBatchBody struct {
	IDs []string `json:"ids"`
}

// purgaDTO e a resposta da purga individual (DELETE /entities/{id}).
type purgaDTO struct {
	Purged bool `json:"purged"`
}

// purgaLoteDTO e a resposta da purga em lote (POST /entities/purge): quantas
// entities foram de fato removidas (idempotente por id ausente).
type purgaLoteDTO struct {
	Removed int `json:"removed"`
}

// listReview trata GET /api/v1/review: a fila de revisao (draft OU baixa confianca)
// com o mesmo shape de card da lista. 200 com [] quando vazia.
func (s *Server) listReview(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	rows, err := s.leitura().ListarRevisao(r.Context(),
		sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true})
	if err != nil {
		respondErroInterno(w)
		return
	}
	items := make([]entityCardDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, cardDTO(sqlcgen.ListarEntityCardsRow(row)))
	}
	writeJSON(w, http.StatusOK, items)
}

// listOrphans trata GET /api/v1/orphans: a fila de orfas (auto-criadas, todas
// missing, sem investimento do dono). 200 com [] quando vazia.
func (s *Server) listOrphans(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	rows, err := s.leitura().ListarOrfas(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	items := make([]entityCardDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, cardDTO(sqlcgen.ListarEntityCardsRow(row)))
	}
	writeJSON(w, http.StatusOK, items)
}

// confirmEntity trata POST /api/v1/entities/{id}/confirm: o dono aceita o palpite.
// Sobe a confianca dos media_file da entity para 100 e responde a entity resolvida.
// 404 se a entity nao existe.
func (s *Server) confirmEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}
	if err := s.finalizarRevisao(ctx, id); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderResolvida(w, ctx, id, http.StatusOK)
}

// correctEntity trata POST /api/v1/entities/{id}/correct: o dono informa a
// identidade correta. Grava external_ids[source]=source_id e RE-ENRIQUECE pelo
// serviço do 0015 (identidade vence, MD4; multi-idioma de graça), depois sobe a
// confianca para 100. Nao toca override/arte (o enrich e nao-destrutivo). 404 se a
// entity nao existe; 422 se source_id vazio; 502 se a fonte falhar; 503 se o enrich
// esta desligado.
func (s *Server) correctEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if s.enrichIndisponivel(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	var body correctBody
	if ok := decodeOpcional(w, r, &body); !ok {
		return
	}
	source, ok := s.resolverFonte(w, ent.Type, body.Source)
	if !ok {
		return
	}
	if body.SourceID == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "source_id é obrigatório")
		return
	}

	// Rede de seguranca contra duplicata (briefing 0051): a identidade corrigida
	// pode ja pertencer a outra entity (ex.: importada por engano antes desta
	// correcao) -- funde sem investimento do dono, ou aborta com 409 com.
	if _, ok := s.resolverColisaoExterna(w, ctx, id, ent.Type, source, body.SourceID); !ok {
		return
	}

	langs, err := s.idiomasPermitidos(ctx, w)
	if err != nil {
		return
	}
	if err := s.enrich.Enriquecer(ctx, id, source, body.SourceID, ent.Type, langs); err != nil {
		respondFonteIndisponivel(w)
		return
	}
	if err := s.finalizarRevisao(ctx, id); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderResolvida(w, ctx, id, http.StatusOK)
}

// deleteEntity trata DELETE /api/v1/entities/{id}: PURGA individual. Apaga a entity
// (ApagarEntity) — o ON DELETE CASCADE leva media_file/media_external/media_override/
// asset junto — e limpa best-effort o diretorio de binarios de assets da entity.
// 200 com {purged:true}; 404 se a entity nao existe.
func (s *Server) deleteEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}
	if err := sqlcgen.New(s.db.Write).ApagarEntity(ctx, id); err != nil {
		respondErroInterno(w)
		return
	}
	s.limparBinariosAsset(id)
	writeJSON(w, http.StatusOK, purgaDTO{Purged: true})
}

// purgeBatch trata POST /api/v1/entities/purge: PURGA em lote. Purga cada id do
// corpo, ignorando ids ausentes (idempotente), e responde quantas foram removidas.
// 422 corpo invalido.
func (s *Server) purgeBatch(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()

	var body purgeBatchBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "corpo JSON inválido")
		return
	}

	removed := 0
	for _, id := range body.IDs {
		ok, err := s.purgarSeExiste(ctx, id)
		if err != nil {
			respondErroInterno(w)
			return
		}
		if ok {
			removed++
		}
	}
	writeJSON(w, http.StatusOK, purgaLoteDTO{Removed: removed})
}

// ── Helpers da curadoria ─────────────────────────────────────────────────────

// finalizarRevisao e o passo final compartilhado por confirmar e corrigir: tira o
// item da fila de revisao pelos DOIS ramos do predicado. (1) sobe a confianca dos
// media_file para 100 (mata o ramo "baixa confianca"); (2) promove um draft a
// 'cataloged' (mata o ramo "status='draft'"). Sem o (2), um draft confirmado/
// corrigido subia a confianca mas ficava preso na fila como nao-identificado. As
// duas escritas vao pelo pool de escrita; PromoverDeDraft e no-op fora de draft.
func (s *Server) finalizarRevisao(ctx context.Context, id string) error {
	q := sqlcgen.New(s.db.Write)
	if err := q.AtualizarConfidencePorEntity(ctx, sqlcgen.AtualizarConfidencePorEntityParams{
		MatchConfidence: sql.NullFloat64{Float64: confiancaConfirmada, Valid: true},
		EntityID:        id,
	}); err != nil {
		return err
	}
	return q.PromoverDeDraft(ctx, id)
}

// purgarSeExiste apaga a entity e limpa seus binarios SE ela existir; devolve
// (true, nil) quando removeu, (false, nil) quando ja nao existia (idempotente). Um
// erro real de banco e propagado.
func (s *Server) purgarSeExiste(ctx context.Context, id string) (bool, error) {
	if _, err := s.leitura().ObterEntity(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if err := sqlcgen.New(s.db.Write).ApagarEntity(ctx, id); err != nil {
		return false, err
	}
	s.limparBinariosAsset(id)
	return true, nil
}

// limparBinariosAsset remove best-effort o diretorio de binarios de assets da
// entity ({assetsDir}/{id}). Sem assetsDir configurado e no-op; erros sao
// ignorados (o CASCADE ja removeu as linhas — o disco e so cache).
func (s *Server) limparBinariosAsset(id string) {
	if s.assetsDir == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(s.assetsDir, id))
}
