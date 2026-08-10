package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/webhook"
)

// Este arquivo implementa o RECEPTOR do webhook de reprodução do Jellyfin
// (briefing 0093, ADR-0022, depende do 0092): normaliza cada notificação de
// play/pause/stop do plugin `jellyfin-plugin-webhook` num `historico_evento`,
// casando o item pelos ids externos que o próprio payload já carrega. O
// parser tolerante (NotificationType/ItemType/Provider_*/ticks) vive em
// internal/webhook (sem banco/HTTP); aqui só a fiação HTTP + o casamento por
// id externo + a config do token via app_settings.

// appSettingWebhookJellyfinToken é a chave da config do token do webhook do
// Jellyfin em app_settings (briefing 0093), valor {"token": "..."}.
const appSettingWebhookJellyfinToken = "webhook_jellyfin_token"

// tamanhoTokenBytes é o tamanho (em bytes, antes de hex-encode) do token
// gerado -- 32 bytes = 64 chars hex, mesmo espírito de uma chave de API.
const tamanhoTokenBytes = 32

// jellyfinWebhookTokenSettings é a config do token, serializada como
// value_json da chave webhook_jellyfin_token. É também o corpo de
// POST/GET .../webhooks/jellyfin/token.
type jellyfinWebhookTokenSettings struct {
	Token string `json:"token"`
}

// obterJellyfinWebhookToken lê o token atual de app_settings pela conexão de
// LEITURA. sql.ErrNoRows (nenhum token gerado ainda) é propagado tal como
// veio -- o GET/o handler do webhook decidem o que fazer (404/401).
func (s *Server) obterJellyfinWebhookToken(ctx context.Context) (string, error) {
	row, err := s.leitura().ObterAppSetting(ctx, appSettingWebhookJellyfinToken)
	if err != nil {
		return "", err
	}
	if !row.ValueJson.Valid || row.ValueJson.String == "" {
		return "", sql.ErrNoRows
	}
	var cfg jellyfinWebhookTokenSettings
	if err := json.Unmarshal([]byte(row.ValueJson.String), &cfg); err != nil || cfg.Token == "" {
		return "", sql.ErrNoRows
	}
	return cfg.Token, nil
}

// gerarTokenAleatorio gera um token hex de tamanhoTokenBytes bytes via
// crypto/rand (nunca math/rand -- token de autenticação).
func gerarTokenAleatorio() (string, error) {
	b := make([]byte, tamanhoTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// regenerateJellyfinWebhookToken trata POST /api/v1/webhooks/jellyfin/token
// (briefing 0093): gera um token novo e SUBSTITUI o anterior (upsert em
// app_settings) -- chamadas subsequentes ao webhook com o token antigo
// deixam de funcionar. 200 com o token novo; 500 em erro real (geração ou
// gravação).
func (s *Server) regenerateJellyfinWebhookToken(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	token, err := gerarTokenAleatorio()
	if err != nil {
		respondErroInterno(w)
		return
	}
	cfg := jellyfinWebhookTokenSettings{Token: token}
	valor, err := json.Marshal(cfg)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingWebhookJellyfinToken,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// getJellyfinWebhookToken trata GET /api/v1/webhooks/jellyfin/token
// (briefing 0093): devolve o token vigente. 404 se nenhum token foi gerado
// ainda (o dono deve chamar POST primeiro).
func (s *Server) getJellyfinWebhookToken(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	token, err := s.obterJellyfinWebhookToken(r.Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found",
				"nenhum token de webhook do Jellyfin foi gerado ainda")
			return
		}
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, jellyfinWebhookTokenSettings{Token: token})
}

// receiveJellyfinWebhook trata POST /api/v1/webhooks/jellyfin?token=
// (briefing 0093): autentica por token (comparação em TEMPO CONSTANTE),
// parseia o payload best-effort, casa o item por id externo e persiste um
// historico_evento. SEMPRE 204 quando o token bate -- inclusive payload não
// reconhecível/sem match/ItemType não suportado (para não fazer o Jellyfin
// retentar, ver briefing 0093 §3/§6); 401 quando o token falta ou não bate.
// Nunca 500 por causa do CORPO do payload (parse/match degradam para
// "descartado e logado").
func (s *Server) receiveJellyfinWebhook(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if !s.autenticarJellyfinWebhook(w, r) {
		return
	}
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("webhook jellyfin: falha ao ler o corpo (descartado): %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ev, err := webhook.ParsePayload(body)
	if err != nil {
		log.Printf("webhook jellyfin: payload nao e JSON valido (descartado): %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !ev.ItemTypeSuportado() {
		log.Printf("webhook jellyfin: ItemType %q nao suportado (descartado)", ev.ItemType)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	status := webhook.MapStatus(ev.NotificationType, ev.ProgressPercent)
	if status == "" {
		log.Printf("webhook jellyfin: NotificationType %q desconhecido (descartado)", ev.NotificationType)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	entityID, ok := s.casarEntityJellyfin(ctx, ev)
	if !ok {
		log.Printf("webhook jellyfin: nenhuma entity casou (tmdb=%q imdb=%q tvdb=%q) -- descartado",
			ev.TmdbID, ev.ImdbID, ev.TvdbID)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	_, err = sqlcgen.New(s.db.Write).CriarHistoricoEvento(ctx, sqlcgen.CriarHistoricoEventoParams{
		ID:              uuid.NewString(),
		EntityID:        entityID,
		Kind:            webhook.Kind,
		Status:          status,
		ProgressPercent: f64ToNull(ev.ProgressPercent),
		PositionSeconds: f64ToNull(ev.PositionSeconds),
		OccurredAt:      time.Now().UTC().Format(time.RFC3339),
		Source:          webhook.Source,
		PayloadJson:     sql.NullString{String: string(body), Valid: true},
	})
	if err != nil {
		// Erro real de banco ao gravar: loga mas ainda responde 204 -- o
		// Jellyfin não deve retentar por causa de uma falha interna nossa
		// (o dono investiga pelo log; retry não resolveria).
		log.Printf("webhook jellyfin: falha ao gravar historico_evento (entity=%s): %v", entityID, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// autenticarJellyfinWebhook valida o `?token=` da query contra o configurado
// em app_settings, já escrevendo a resposta de erro (401/500) quando falha.
// Extraído do handler principal só pra manter a complexidade ciclomática
// baixa (mesmo padrão de exigirEntity) — `false` significa "já respondido,
// pare aqui".
func (s *Server) autenticarJellyfinWebhook(w http.ResponseWriter, r *http.Request) bool {
	tokenEsperado, err := s.obterJellyfinWebhookToken(r.Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "unauthorized",
				"nenhum token de webhook do Jellyfin foi gerado ainda")
			return false
		}
		respondErroInterno(w)
		return false
	}
	if !tokenValido(r.URL.Query().Get("token"), tokenEsperado) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "token invalido ou ausente")
		return false
	}
	return true
}

// tokenValido compara o token recebido com o esperado em TEMPO CONSTANTE
// (crypto/subtle -- evita timing attack, mesmo sendo um endpoint de baixo
// risco, briefing 0093 §4). "" nunca bate (evita comparar dois vazios).
func tokenValido(recebido, esperado string) bool {
	if recebido == "" || esperado == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(recebido), []byte(esperado)) == 1
}

// casarEntityJellyfin tenta casar o item do Jellyfin com uma entity
// catalogada pelos ids externos presentes no payload, na ordem
// tmdb -> imdb -> tvdb (briefing 0093 §3), parando no primeiro que bater.
// Devolve (id, false) sem nenhum id presente ou nenhum bater -- nunca cria
// entity nova a partir de um webhook.
func (s *Server) casarEntityJellyfin(ctx context.Context, ev webhook.Evento) (string, bool) {
	if ev.TmdbID != "" {
		if ent, err := s.leitura().ObterEntityPorTmdb(ctx, sql.NullString{String: ev.TmdbID, Valid: true}); err == nil {
			return ent.ID, true
		}
	}
	if ev.ImdbID != "" {
		if ent, err := s.leitura().ObterEntityPorImdb(ctx, sql.NullString{String: ev.ImdbID, Valid: true}); err == nil {
			return ent.ID, true
		}
	}
	if ev.TvdbID != "" {
		if ent, err := s.leitura().ObterEntityPorTvdb(ctx, sql.NullString{String: ev.TvdbID, Valid: true}); err == nil {
			return ent.ID, true
		}
	}
	return "", false
}
