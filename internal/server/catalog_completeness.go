package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa a config de COMPLETUDE DE CATALOGAÇÃO em app_settings
// (briefing 0057): GET/PUT /api/v1/settings/catalog-completeness. Espelha
// exatamente a estrutura de settings.go (config de idioma, briefing 0019): cache
// leve em memória (mutex + flag `carregado`), GET não-destrutivo (default de
// fábrica quando a chave não existe, sem gravar nada) e PUT como substituição
// TOTAL (não é PATCH parcial).
//
// Distinção crítica (ver briefing 0057 §6): isto é "completude de CATALOGAÇÃO"
// (preenchimento de metadados/artes de UM item) — DIFERENTE de "completude de
// POSSE" (quantos episódios/temporadas o dono TEM no disco, `web/src/lib/
// entity-badges.ts`, briefings 0036/0037). Não confundir os dois conceitos.

// appSettingCatalogCompleteness é a chave da config de completude de
// catalogação em app_settings.
const appSettingCatalogCompleteness = "catalog_completeness"

// tipoCompletenessConfig é o conjunto de campos/artes desejáveis para UM
// entity.type (briefing 0057 §4). Todos os campos são obrigatórios no PUT — o
// formulário sempre manda o objeto completo (substituição total).
type tipoCompletenessConfig struct {
	Description   bool     `json:"description"`
	Runtime       bool     `json:"runtime"`
	Certification bool     `json:"certification"`
	Keywords      bool     `json:"keywords"`
	Cast          bool     `json:"cast"`
	MediaFile     bool     `json:"media_file"`
	Artwork       []string `json:"artwork"`
}

// catalogCompletenessSettings é a config canônica, chave de topo = entity.type
// (ABERTA — qualquer string de tipo é aceita, não só movie/tv; ver briefing
// 0057 §4 e ADR de entity.type como TEXT livre). Um tipo ausente do mapa
// significa "nada exigido" para aquele tipo. Serializada como value_json da
// chave `catalog_completeness`; também é o corpo do PUT e a resposta do GET.
type catalogCompletenessSettings map[string]tipoCompletenessConfig

// defaultCatalogCompletenessSettings é o DEFAULT DE FÁBRICA deliberadamente
// mínimo (briefing 0057 §6): sinopse + arquivo de mídia + poster marcados para
// `movie` e `tv`, resto desmarcado. Um default "tudo marcado" puniria
// retroativamente acervos já catalogados sem o dono ter pedido nada.
func defaultCatalogCompletenessSettings() catalogCompletenessSettings {
	minimo := tipoCompletenessConfig{
		Description: true,
		MediaFile:   true,
		Artwork:     []string{kindPoster},
	}
	return catalogCompletenessSettings{
		"movie": minimo,
		"tv":    minimo,
	}
}

// catalogCompletenessCache guarda em memória a config já decodificada,
// protegido por mutex — mesmo padrão de settingsCache (briefing 0019).
// `carregado` distingue "ainda não lido" de "lido" (o valor pode ser o
// default). Invalidado no PUT.
type catalogCompletenessCache struct {
	mu        sync.Mutex
	carregado bool
	cfg       catalogCompletenessSettings
}

// obterCatalogCompletenessSettings devolve a config de completude de
// catalogação, do cache quando já carregada, senão lendo app_settings pela
// conexão de LEITURA. Linha ausente ou JSON inválido ⇒ default de fábrica
// (nunca erro por isso). Só um erro real de banco é propagado. Exportável do
// pacote server para outros handlers/pacotes (0056/0058) consumirem sem golpe
// de I/O por request.
func (s *Server) obterCatalogCompletenessSettings(ctx context.Context) (catalogCompletenessSettings, error) {
	s.catalogCompleteness.mu.Lock()
	defer s.catalogCompleteness.mu.Unlock()
	if s.catalogCompleteness.carregado {
		return s.catalogCompleteness.cfg, nil
	}
	row, err := s.leitura().ObterAppSetting(ctx, appSettingCatalogCompleteness)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.catalogCompleteness.cfg = defaultCatalogCompletenessSettings()
			s.catalogCompleteness.carregado = true
			return s.catalogCompleteness.cfg, nil
		}
		return nil, err
	}
	cfg := defaultCatalogCompletenessSettings()
	if row.ValueJson.Valid && row.ValueJson.String != "" {
		var parsed catalogCompletenessSettings
		if json.Unmarshal([]byte(row.ValueJson.String), &parsed) == nil && parsed.valida() == nil {
			cfg = parsed
		}
	}
	s.catalogCompleteness.cfg = cfg
	s.catalogCompleteness.carregado = true
	return cfg, nil
}

// invalidarCatalogCompleteness zera o cache (chamado após o PUT gravar).
func (s *Server) invalidarCatalogCompleteness() {
	s.catalogCompleteness.mu.Lock()
	s.catalogCompleteness.carregado = false
	s.catalogCompleteness.mu.Unlock()
}

// valida diz se a config é internamente consistente (briefing 0057 §4): toda
// chave de tipo é não-vazia e todo item de artwork pertence ao registro
// kindsValidos (internal/server/assets.go). Tipos com objeto vazio ({}) são
// aceitos (equivalem a "nada exigido"). Devolve nil quando válido, ou um erro
// descritivo (para a mensagem de 422) caso contrário.
func (c catalogCompletenessSettings) valida() error {
	for tipo, cfg := range c {
		if tipo == "" {
			return errors.New("chave de tipo vazia não é permitida")
		}
		for _, kind := range cfg.Artwork {
			if !kindsValidos[kind] {
				return errors.New("kind de arte desconhecido \"" + kind +
					"\" (use " + kindsValidosListaOrdenada() + ")")
			}
		}
	}
	return nil
}

// kindsValidosListaOrdenada devolve os kinds do registro kindsValidos
// (internal/server/assets.go) numa lista ordenada e separada por vírgula, para
// compor a mensagem de erro de validação.
func kindsValidosListaOrdenada() string {
	kinds := make([]string, 0, len(kindsValidos))
	for k := range kindsValidos {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return strings.Join(kinds, ", ")
}

// getCatalogCompletenessSettings trata GET /api/v1/settings/catalog-completeness:
// devolve a config atual (ou o default de fábrica quando ausente, sem gravar
// nada).
func (s *Server) getCatalogCompletenessSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	cfg, err := s.obterCatalogCompletenessSettings(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putCatalogCompletenessSettings trata PUT /api/v1/settings/catalog-completeness:
// valida (chaves de tipo não-vazias, artwork ⊆ kindsValidos) e faz upsert,
// invalidando o cache. Substituição TOTAL (não é PATCH parcial — mesmo padrão
// do PUT de idiomas). Corpo malformado ⇒ 400; inválido ⇒ 422; sucesso ⇒ 200 com
// a config gravada (eco).
func (s *Server) putCatalogCompletenessSettings(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	var body catalogCompletenessSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if err := body.valida(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"config de completude de catalogação inválida: "+err.Error())
		return
	}

	valor, err := json.Marshal(body)
	if err != nil {
		respondErroInterno(w)
		return
	}
	_, err = sqlcgen.New(s.db.Write).UpsertAppSetting(r.Context(), sqlcgen.UpsertAppSettingParams{
		Key:       appSettingCatalogCompleteness,
		ValueJson: sql.NullString{String: string(valor), Valid: true},
	})
	if err != nil {
		respondErroInterno(w)
		return
	}
	s.invalidarCatalogCompleteness()
	writeJSON(w, http.StatusOK, body)
}
