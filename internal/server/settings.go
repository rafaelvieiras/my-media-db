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

// settingsCache guarda em memória a config de idioma já decodificada, protegido por
// mutex. `carregado` distingue "ainda não lido" de "lido" (o valor pode ser o
// default). Invalidado no PUT.
type settingsCache struct {
	mu        sync.Mutex
	carregado bool
	langs     languageSettings
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

// invalidarSettings zera o cache (chamado após o PUT gravar).
func (s *Server) invalidarSettings() {
	s.settings.mu.Lock()
	s.settings.carregado = false
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
