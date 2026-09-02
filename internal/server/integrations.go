package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/integration"
)

// Este arquivo implementa a API de INTEGRAÇÕES configuráveis via UI/banco
// (briefing 0095, ADR-0023):
//
//	GET /api/v1/integrations          — lista {slug,nome,descricao,configurada}
//	GET /api/v1/integrations/{slug}   — schema de campos + valor efetivo por campo
//	PUT /api/v1/integrations/{slug}   — grava PARCIAL (null remove a chave)
//
// Fundação para o 0096 (tela /dashboard/integrations); esta entrega não tem
// UI própria. O valor efetivo de cada campo é sempre banco → env var →
// ausente (s.integrationsResolver, internal/integration); um campo
// `tipo:"secret"` NUNCA devolve `valor` cru, só `definido`/`origem`.

// integrationSummaryDTO é UM item da listagem GET /integrations.
type integrationSummaryDTO struct {
	Slug        string `json:"slug"`
	Nome        string `json:"nome"`
	Descricao   string `json:"descricao"`
	Configurada bool   `json:"configurada"`
}

// integrationsListDTO é o corpo de GET /integrations.
type integrationsListDTO struct {
	Integrations []integrationSummaryDTO `json:"integrations"`
}

// integrationOpcaoDTO é uma alternativa de um campo TipoSelect.
type integrationOpcaoDTO struct {
	Valor string `json:"valor"`
	Label string `json:"label"`
}

// integrationCampoDTO é UM campo no corpo de GET/PUT /integrations/{slug}.
// `valor` só aparece quando o campo NÃO é secreto E há um valor efetivo
// (omitempty + ponteiro: ausente no JSON quando nil).
type integrationCampoDTO struct {
	Nome        string                `json:"nome"`
	Label       string                `json:"label"`
	Tipo        string                `json:"tipo"`
	Obrigatorio bool                  `json:"obrigatorio"`
	Ajuda       string                `json:"ajuda,omitempty"`
	Placeholder string                `json:"placeholder,omitempty"`
	Opcoes      []integrationOpcaoDTO `json:"opcoes,omitempty"`
	Definido    bool                  `json:"definido"`
	Origem      string                `json:"origem"`
	Valor       *string               `json:"valor,omitempty"`
}

// integrationDetailDTO é o corpo de GET/PUT /integrations/{slug}.
type integrationDetailDTO struct {
	Slug      string                `json:"slug"`
	Nome      string                `json:"nome"`
	Descricao string                `json:"descricao"`
	Campos    []integrationCampoDTO `json:"campos"`
}

// putIntegrationRequestDTO é o corpo do PUT /integrations/{slug}. Um valor
// `null` no JSON vira ponteiro nil aqui — remove a chave (volta a herdar a
// env var); uma chave AUSENTE do objeto não é tocada.
type putIntegrationRequestDTO struct {
	Valores map[string]*string `json:"valores"`
}

// listIntegrations trata GET /api/v1/integrations: itera o Registry, resolve
// os valores efetivos de cada Descriptor e chama Configurada() (briefing
// 0095). Sempre 200 (mesmo com o Registry vazio — lista vazia, não erro).
func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	descs := s.integrations.All()
	out := make([]integrationSummaryDTO, 0, len(descs))
	for _, d := range descs {
		efetivos := s.integrationsResolver.Valores(r.Context(), d.Slug(), d.Campos())
		out = append(out, integrationSummaryDTO{
			Slug:        d.Slug(),
			Nome:        d.Nome(),
			Descricao:   d.Descricao(),
			Configurada: d.Configurada(efetivos),
		})
	}
	writeJSON(w, http.StatusOK, integrationsListDTO{Integrations: out})
}

// getIntegration trata GET /api/v1/integrations/{slug}: devolve o schema de
// campos + o valor efetivo (banco→env var→ausente) de cada um. 404 se o slug
// não está no Registry.
func (s *Server) getIntegration(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	slug := chi.URLParam(r, "slug")
	d, ok := s.integrations.Get(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "integração desconhecida")
		return
	}
	writeJSON(w, http.StatusOK, s.montarIntegrationDetail(r.Context(), d))
}

// montarIntegrationDetail monta o shape comum de GET e PUT (eco pós-gravação)
// a partir do Descriptor + o Resolver: para cada Campo, calcula o efetivo e
// só inclui `valor` cru quando o tipo NÃO é secreto e há valor efetivo —
// campo secreto nunca vaza o valor, nem no eco do PUT.
func (s *Server) montarIntegrationDetail(ctx context.Context, d integration.Descriptor) integrationDetailDTO {
	campos := d.Campos()
	out := make([]integrationCampoDTO, 0, len(campos))
	for _, c := range campos {
		valor, origem, ok := s.integrationsResolver.Efetivo(ctx, d.Slug(), c)
		dto := integrationCampoDTO{
			Nome:        c.Nome,
			Label:       c.Label,
			Tipo:        string(c.Tipo),
			Obrigatorio: c.Obrigatorio,
			Ajuda:       c.Ajuda,
			Placeholder: c.Placeholder,
			Definido:    ok,
			Origem:      origem,
		}
		for _, o := range c.Opcoes {
			dto.Opcoes = append(dto.Opcoes, integrationOpcaoDTO{Valor: o.Valor, Label: o.Label})
		}
		if c.Tipo != integration.TipoSegredo && ok {
			v := valor
			dto.Valor = &v
		}
		out = append(out, dto)
	}
	return integrationDetailDTO{Slug: d.Slug(), Nome: d.Nome(), Descricao: d.Descricao(), Campos: out}
}

// putIntegration trata PUT /api/v1/integrations/{slug}: grava PARCIAL no
// values_json do slug — só as chaves presentes em `valores` mudam; um valor
// `null` REMOVE a chave (volta a herdar a env var). 404 se o slug é
// desconhecido; 422 se `valores` vier vazio ou citar uma chave que não é um
// Campo do Descriptor. Não bloqueia por obrigatoriedade ausente — o dono pode
// salvar parcialmente (reflete só em `configurada:false` na listagem).
func (s *Server) putIntegration(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	slug := chi.URLParam(r, "slug")
	d, ok := s.integrations.Get(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "integração desconhecida")
		return
	}

	var body putIntegrationRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if len(body.Valores) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "valores não pode ser vazio")
		return
	}
	camposValidos := make(map[string]bool, len(d.Campos()))
	for _, c := range d.Campos() {
		camposValidos[c.Nome] = true
	}
	for nome := range body.Valores {
		if !camposValidos[nome] {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body",
				fmt.Sprintf("campo desconhecido para %q: %s", slug, nome))
			return
		}
	}

	atuais, err := s.obterIntegrationValores(r.Context(), slug)
	if err != nil {
		respondErroInterno(w)
		return
	}
	for nome, valor := range body.Valores {
		if valor == nil {
			delete(atuais, nome)
		} else {
			atuais[nome] = *valor
		}
	}
	novoJSON, err := json.Marshal(atuais)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if _, err := sqlcgen.New(s.db.Write).UpsertIntegrationConfig(r.Context(), sqlcgen.UpsertIntegrationConfigParams{
		Slug:       slug,
		ValuesJson: string(novoJSON),
	}); err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, s.montarIntegrationDetail(r.Context(), d))
}

// obterIntegrationValores lê o values_json atual de um slug (map vazio
// quando o slug nunca foi gravado ou o JSON está corrompido — mesma postura
// defensiva do Resolver, nunca propaga erro por isso). Só um erro real de
// banco (não sql.ErrNoRows) é propagado.
func (s *Server) obterIntegrationValores(ctx context.Context, slug string) (map[string]string, error) {
	row, err := s.leitura().ObterIntegrationConfig(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	valores := map[string]string{}
	if row.ValuesJson != "" {
		if err := json.Unmarshal([]byte(row.ValuesJson), &valores); err != nil {
			return map[string]string{}, nil
		}
	}
	return valores, nil
}
