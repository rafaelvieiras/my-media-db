package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa a config de IDIOMA em app_settings (briefing 0019):
//   GET  /api/v1/settings/languages  — devolve {primary, fallback, allowed}
//   PUT  /api/v1/settings/languages  — valida e persiste (upsert), invalida o cache
// Além disso deriva a CADEIA de fallback consumida pelo resolver (ADR-0019) e faz o
// parse/validação do ?lang= dos handlers de entities. A leitura tem cache leve em
// memória (mutex, como o cacheExterno da busca), invalidado no PUT.

// appSettingLanguages é a chave da config de idioma em app_settings.
const appSettingLanguages = "languages"

// appSettingEnrichmentRetroativo é a chave da config de throttle do
// enriquecimento retroativo em app_settings (briefing 0085).
const appSettingEnrichmentRetroativo = "enrichment_retroativo"

// appSettingAutoMaterializeArtwork é a chave do toggle de materialização
// automática de artes em app_settings (briefing 0089).
const appSettingAutoMaterializeArtwork = "materializar_artes_automaticamente"

// appSettingYtdlp é a chave do path customizado do binário yt-dlp em
// app_settings (briefing 0101, ADR-0025).
const appSettingYtdlp = "ytdlp"

// retryDaysPadrao/retryDaysMinimo/retryDaysMaximo são o default (quando nunca
// configurado) e o intervalo válido do throttle de reentativa (briefing
// 0085): antes uma constante fixa de 30 dias em retroativo.go, agora
// configurável pelo dono, com default de 15 dias.
const (
	retryDaysPadrao = 15
	retryDaysMinimo = 1
	retryDaysMaximo = 365
)

// languageSettings é a config de idioma canônica (ADR-0018): idioma principal,
// fallback e o conjunto de permitidos. Serializada como value_json da chave
// `languages`. É também o corpo do PUT e a resposta do GET.
type languageSettings struct {
	Primary  string   `json:"primary"`
	Fallback string   `json:"fallback"`
	Allowed  []string `json:"allowed"`
}

// defaultLanguageSettings é o DEFAULT SEGURO usado quando a linha `languages` não
// existe (ou está corrompida): principal pt-BR, fallback en, permitidos [pt-BR, en].
// Assim a resolução funciona de ponta a ponta antes de qualquer configuração.
func defaultLanguageSettings() languageSettings {
	return languageSettings{Primary: "pt-BR", Fallback: "en", Allowed: []string{"pt-BR", "en"}}
}

// retroactiveEnrichmentSettings é a config de throttle do enriquecimento
// retroativo (briefing 0085): intervalo mínimo, em dias, entre duas
// tentativas do MESMO item sem provider. Serializada como value_json da
// chave `enrichment_retroativo`. É também o corpo do PUT e a resposta do GET.
type retroactiveEnrichmentSettings struct {
	RetryDays int `json:"retry_days"`
}

// defaultRetroactiveEnrichmentSettings é o DEFAULT SEGURO usado quando a linha
// `enrichment_retroativo` não existe (ou está corrompida): 15 dias.
func defaultRetroactiveEnrichmentSettings() retroactiveEnrichmentSettings {
	return retroactiveEnrichmentSettings{RetryDays: retryDaysPadrao}
}

// autoMaterializeArtworkSettings é o toggle de materialização automática de
// artes (briefing 0089): quando Enabled, todo auto-import bem-sucedido do
// scan (filme ou série) já materializa na hora as artes do inventário na
// pasta real da biblioteca, sem esperar o job periódico midias_faltantes nem
// clique manual. Serializado como value_json da chave
// `materializar_artes_automaticamente`. É também o corpo do PUT e a resposta
// do GET.
type autoMaterializeArtworkSettings struct {
	Enabled bool `json:"enabled"`
}

// defaultAutoMaterializeArtworkSettings é o DEFAULT SEGURO usado quando a
// linha `materializar_artes_automaticamente` não existe (ou está corrompida):
// desligado -- materialização automática é opt-in (o dono liga quando quiser).
func defaultAutoMaterializeArtworkSettings() autoMaterializeArtworkSettings {
	return autoMaterializeArtworkSettings{Enabled: false}
}

// ytdlpPathSettings é o path customizado do binário yt-dlp configurado pelo
// dono (briefing 0101, ADR-0025): {"custom_binary_path": string|null}.
// Serializado como value_json da chave `ytdlp`. É o corpo do PUT (o GET devolve
// o shape mais rico ytdlpStatusResponse, abaixo). Tem PRECEDÊNCIA sobre a env
// var MYMEDIADB_YTDLP_PATH (mesmo espírito do ADR-0023) -- aplicada em runtime
// via ytdlp.Gerenciador.SetCustomPath, sem reiniciar o processo.
type ytdlpPathSettings struct {
	CustomBinaryPath *string `json:"custom_binary_path"`
}

// defaultYtdlpPathSettings é o DEFAULT SEGURO usado quando a linha `ytdlp`
// não existe (ou está corrompida): nenhum path customizado -- o Gerenciador
// cai para a env var (se houver) ou o download automático.
func defaultYtdlpPathSettings() ytdlpPathSettings {
	return ytdlpPathSettings{CustomBinaryPath: nil}
}

// ytdlpStatusResponse é o corpo devolvido por GET/PUT /settings/ytdlp: o
// status EFETIVO do Gerenciador (disponível/origem/detalhe, calculado por
// ytdlp.Gerenciador.Status) mais o path customizado atualmente gravado no
// banco. `origem` não distingue se um path "custom" veio do banco ou da env
// var (o contrato só expõe custom_binary_path do BANCO -- a env var nunca é
// ecoada por API, mesmo espírito de nunca expor segredo/config de boot).
type ytdlpStatusResponse struct {
	Disponivel       bool    `json:"disponivel"`
	Origem           string  `json:"origem"`
	CustomBinaryPath *string `json:"custom_binary_path"`
	Detalhe          *string `json:"detalhe"`
}

// settingsCache guarda em memória a config de idioma e a de throttle do
// enriquecimento retroativo já decodificadas, protegidas pelo MESMO mutex.
// `carregado`/`retroCarregado` distinguem "ainda não lido" de "lido" (o valor
// pode ser o default). Invalidados no PUT correspondente.
type settingsCache struct {
	mu        sync.Mutex
	carregado bool
	langs     languageSettings

	retroCarregado bool
	retro          retroactiveEnrichmentSettings

	autoMaterializeCarregado bool
	autoMaterialize          autoMaterializeArtworkSettings

	ytdlpCarregado bool
	ytdlpPath      ytdlpPathSettings
}

// obterLanguageSettings devolve a config de idioma, do cache quando já carregada,
// senão lendo app_settings pela conexão de LEITURA. Linha ausente ou JSON inválido
// ⇒ default seguro (nunca erro por isso). Só um erro real de banco é propagado.
func (s *Server) obterLanguageSettings(ctx context.Context) (languageSettings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.carregado {
		return s.settings.langs, nil
	}
	row, err := s.leitura().ObterAppSetting(ctx, appSettingLanguages)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.settings.langs = defaultLanguageSettings()
			s.settings.carregado = true
			return s.settings.langs, nil
		}
		return languageSettings{}, err
	}
	langs := defaultLanguageSettings()
	if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed languageSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil && parsed.valida() {
			langs = parsed
		}
	}
	s.settings.langs = langs
	s.settings.carregado = true
	return langs, nil
}

// obterRetroactiveEnrichmentSettings devolve a config de throttle do
// enriquecimento retroativo, do cache quando já carregada, senão lendo
// app_settings pela conexão de LEITURA. Linha ausente ou JSON inválido ⇒
// default seguro (nunca erro por isso). Só um erro real de banco é propagado.
// Espelha obterLanguageSettings linha a linha (briefing 0085).
func (s *Server) obterRetroactiveEnrichmentSettings(ctx context.Context) (retroactiveEnrichmentSettings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.retroCarregado {
		return s.settings.retro, nil
	}
	row, err := s.leitura().ObterAppSetting(ctx, appSettingEnrichmentRetroativo)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.settings.retro = defaultRetroactiveEnrichmentSettings()
			s.settings.retroCarregado = true
			return s.settings.retro, nil
		}
		return retroactiveEnrichmentSettings{}, err
	}
	retro := defaultRetroactiveEnrichmentSettings()
	if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed retroactiveEnrichmentSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil && parsed.valida() {
			retro = parsed
		}
	}
	s.settings.retro = retro
	s.settings.retroCarregado = true
	return retro, nil
}

// obterAutoMaterializeArtworkSettings devolve o toggle de materialização
// automática de artes, do cache quando já carregado, senão lendo
// app_settings pela conexão de LEITURA. Linha ausente ou JSON inválido ⇒
// default seguro (desligado, nunca erro por isso). Só um erro real de banco
// é propagado. Espelha obterLanguageSettings/obterRetroactiveEnrichmentSettings
// linha a linha (briefing 0089).
func (s *Server) obterAutoMaterializeArtworkSettings(ctx context.Context) (autoMaterializeArtworkSettings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.autoMaterializeCarregado {
		return s.settings.autoMaterialize, nil
	}
	row, err := s.leitura().ObterAppSetting(ctx, appSettingAutoMaterializeArtwork)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.settings.autoMaterialize = defaultAutoMaterializeArtworkSettings()
			s.settings.autoMaterializeCarregado = true
			return s.settings.autoMaterialize, nil
		}
		return autoMaterializeArtworkSettings{}, err
	}
	cfg := defaultAutoMaterializeArtworkSettings()
	if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed autoMaterializeArtworkSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil {
			cfg = parsed
		}
	}
	s.settings.autoMaterialize = cfg
	s.settings.autoMaterializeCarregado = true
	return cfg, nil
}

// obterYtdlpPathSettings devolve o path customizado do yt-dlp gravado no
// banco, do cache quando já carregado, senão lendo app_settings pela conexão
// de LEITURA. Linha ausente ou JSON inválido ⇒ default seguro (nenhum path
// customizado). Só um erro real de banco é propagado. Ao (re)carregar,
// sincroniza o valor no ytdlp.Gerenciador (s.ytdlp.SetCustomPath) -- é assim
// que o override persistido passa a valer, tanto no boot (chamado uma vez em
// montarServicos) quanto depois de um PUT (invalidarSettings zera o cache,
// forçando esta releitura na próxima chamada). Sem s.ytdlp injetado, só
// devolve a config (sem sincronizar nada) -- cenário defensivo de teste de
// rota sem Gerenciador.
func (s *Server) obterYtdlpPathSettings(ctx context.Context) (ytdlpPathSettings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.ytdlpCarregado {
		return s.settings.ytdlpPath, nil
	}
	cfg := defaultYtdlpPathSettings()
	row, err := s.leitura().ObterAppSetting(ctx, appSettingYtdlp)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return ytdlpPathSettings{}, err
		}
	} else if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed ytdlpPathSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil {
			cfg = parsed
		}
	}
	s.settings.ytdlpPath = cfg
	s.settings.ytdlpCarregado = true
	if s.ytdlp != nil {
		if cfg.CustomBinaryPath != nil {
			s.ytdlp.SetCustomPath(*cfg.CustomBinaryPath)
		} else {
			s.ytdlp.SetCustomPath("")
		}
	}
	return cfg, nil
}

// montarYtdlpStatusResponse monta o corpo de GET/PUT /settings/ytdlp: o
// status efetivo (via s.ytdlp.Status) mais o path customizado gravado no
// banco. Sem s.ytdlp injetado, degrada para indisponível/"nenhum" em vez de
// consultar um Gerenciador inexistente (cenário defensivo, não deveria
// ocorrer em produção -- main.go sempre injeta via WithYtdlp).
func (s *Server) montarYtdlpStatusResponse(ctx context.Context, pathCfg ytdlpPathSettings) ytdlpStatusResponse {
	if s.ytdlp == nil {
		detalhe := "yt-dlp nao configurado neste servidor"
		return ytdlpStatusResponse{
			Disponivel:       false,
			Origem:           "nenhum",
			CustomBinaryPath: pathCfg.CustomBinaryPath,
			Detalhe:          &detalhe,
		}
	}
	disponivel, origem, detalhe := s.ytdlp.Status(ctx)
	resp := ytdlpStatusResponse{
		Disponivel:       disponivel,
		Origem:           origem,
		CustomBinaryPath: pathCfg.CustomBinaryPath,
	}
	if detalhe != "" {
		resp.Detalhe = &detalhe
	}
	return resp
}

// invalidarSettings zera o cache (chamado após o PUT gravar).
func (s *Server) invalidarSettings() {
	s.settings.mu.Lock()
	s.settings.carregado = false
	s.settings.retroCarregado = false
	s.settings.autoMaterializeCarregado = false
	s.settings.ytdlpCarregado = false
	s.settings.mu.Unlock()
}

// valida diz se a config é internamente consistente (ADR-0018): principal e fallback
// não-vazios e ∈ allowed; ao menos um permitido, todos não-vazios. É a regra do PUT
// e o guarda contra um value_json corrompido no banco.
func (l languageSettings) valida() bool {
	if l.Primary == "" || l.Fallback == "" || len(l.Allowed) == 0 {
		return false
	}
	for _, a := range l.Allowed {
		if a == "" {
			return false
		}
	}
	return l.permite(l.Primary) && l.permite(l.Fallback)
}

// valida diz se a config de throttle é internamente consistente (briefing
// 0085): retry_days dentro de [retryDaysMinimo, retryDaysMaximo]. É a regra
// do PUT e o guarda contra um value_json corrompido no banco.
func (r retroactiveEnrichmentSettings) valida() bool {
	return r.RetryDays >= retryDaysMinimo && r.RetryDays <= retryDaysMaximo
}

// permite diz se o código está no conjunto de permitidos.
func (l languageSettings) permite(codigo string) bool {
	for _, a := range l.Allowed {
		if a == codigo {
			return true
		}
	}
	return false
}

// cadeiaDeIdiomas deriva a cadeia de fallback para um `lang` pedido (ADR-0019):
// `[lang, fallback, original_language?]` DEDUPLICADA, terminando implicitamente na
// base neutra (arquivo — o resolver cai nela quando a cadeia se esgota). Entradas
// vazias são descartadas.
func (l languageSettings) cadeiaDeIdiomas(lang string, originalLanguage sql.NullString) []string {
	bruta := []string{lang, l.Fallback}
	if originalLanguage.Valid && originalLanguage.String != "" {
		bruta = append(bruta, originalLanguage.String)
	}
	vista := make(map[string]struct{}, len(bruta))
	cadeia := make([]string, 0, len(bruta))
	for _, c := range bruta {
		if c == "" {
			continue
		}
		if _, ok := vista[c]; ok {
			continue
		}
		vista[c] = struct{}{}
		cadeia = append(cadeia, c)
	}
	return cadeia
}

// parseLang lê o ?lang= validando contra os permitidos. Ausente ⇒ principal
// (default). Presente mas fora dos permitidos ⇒ 400 invalid_query (ok=false).
func (s *Server) parseLang(w http.ResponseWriter, cfg languageSettings, lang string) (string, bool) {
	if lang == "" {
		return cfg.Primary, true
	}
	if !cfg.permite(lang) {
		writeError(w, http.StatusBadRequest, "invalid_query",
			"lang fora dos idiomas permitidos")
		return "", false
	}
	return lang, true
}

// getLanguageSettings trata GET /api/v1/settings/languages: devolve a config atual
// (ou o default seguro quando ausente).
func (s *Server) getLanguageSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cfg, err := s.obterLanguageSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putLanguageSettings trata PUT /api/v1/settings/languages: valida (principal e
// fallback ⊆ permitidos, códigos não-vazios) e faz upsert, invalidando o cache.
// Corpo malformado ⇒ 400; inválido ⇒ 422; sucesso ⇒ 200 com a config gravada.
func (s *Server) putLanguageSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body languageSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if !body.valida() {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"config de idioma inválida: principal e fallback devem estar em allowed, e os códigos não podem ser vazios")
		return
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingLanguages,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarSettings()
	writeJSON(w, http.StatusOK, body)
}

// getAutoMaterializeArtworkSettings trata GET
// /api/v1/settings/auto-materialize-artwork (briefing 0089): devolve o
// toggle atual (ou o default seguro {enabled:false} quando ausente). Espelha
// getLanguageSettings/getEnrichmentRetroativoSettings.
func (s *Server) getAutoMaterializeArtworkSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cfg, err := s.obterAutoMaterializeArtworkSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putAutoMaterializeArtworkSettings trata PUT
// /api/v1/settings/auto-materialize-artwork (briefing 0089): faz upsert do
// toggle, invalidando o cache. Corpo malformado ⇒ 400 invalid_body; sucesso
// ⇒ 200 com o valor gravado. Espelha putLanguageSettings/
// putEnrichmentRetroativoSettings (sem etapa de validação semântica: um bool
// não tem estado inválido além de "não decodificou").
func (s *Server) putAutoMaterializeArtworkSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body autoMaterializeArtworkSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingAutoMaterializeArtwork,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarSettings()
	writeJSON(w, http.StatusOK, body)
}

// getEnrichmentRetroativoSettings trata GET /api/v1/settings/enrichment-retroativo
// (briefing 0085): devolve a config atual (ou o default seguro {retry_days:15}
// quando ausente). Espelha getLanguageSettings.
func (s *Server) getEnrichmentRetroativoSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cfg, err := s.obterRetroactiveEnrichmentSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putEnrichmentRetroativoSettings trata PUT /api/v1/settings/enrichment-retroativo
// (briefing 0085): valida (retry_days ∈ [1, 365]) e faz upsert, invalidando o
// cache. Corpo malformado ⇒ 400; inválido ⇒ 422; sucesso ⇒ 200 com a config
// gravada. Espelha putLanguageSettings.
func (s *Server) putEnrichmentRetroativoSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body retroactiveEnrichmentSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if !body.valida() {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"config de enriquecimento retroativo inválida: retry_days deve estar entre 1 e 365")
		return
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingEnrichmentRetroativo,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarSettings()
	writeJSON(w, http.StatusOK, body)
}

// getYtdlpSettings trata GET /api/v1/settings/ytdlp (briefing 0101,
// ADR-0025): devolve o status EFETIVO do binário yt-dlp (path customizado do
// banco vence sobre a env var MYMEDIADB_YTDLP_PATH, que vence sobre o
// binário baixado dinamicamente) mais o path customizado gravado no banco.
// Nunca 500 por falta de yt-dlp -- indisponibilidade é um ESTADO normal do
// corpo (disponivel=false + detalhe), não um erro HTTP.
func (s *Server) getYtdlpSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	pathCfg, err := s.obterYtdlpPathSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, s.montarYtdlpStatusResponse(r.Context(), pathCfg))
}

// putYtdlpSettings trata PUT /api/v1/settings/ytdlp (briefing 0101,
// ADR-0025): grava (ou limpa, quando null/string vazia) o path customizado
// no banco e sincroniza IMEDIATAMENTE o ytdlp.Gerenciador (SetCustomPath) --
// efeito em runtime, sem reiniciar o processo (diferente do TMDB do 0095:
// aqui não há credencial de provider envolvida no boot, só um path de
// binário). Corpo malformado ⇒ 400 invalid_body; sucesso ⇒ 200 com o status
// efetivo recalculado (mesmo shape do GET).
func (s *Server) putYtdlpSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body ytdlpPathSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	// string vazia == null: ambos limpam o override (voltam a herdar a env
	// var/download automático).
	if body.CustomBinaryPath != nil && *body.CustomBinaryPath == "" {
		body.CustomBinaryPath = nil
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingYtdlp,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarSettings()
	if s.ytdlp != nil {
		if body.CustomBinaryPath != nil {
			s.ytdlp.SetCustomPath(*body.CustomBinaryPath)
		} else {
			s.ytdlp.SetCustomPath("")
		}
	}
	writeJSON(w, http.StatusOK, s.montarYtdlpStatusResponse(r.Context(), body))
}
