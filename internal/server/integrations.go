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

// This file implements the API of INTEGRATIONS configurable through the UI/database
// (ADR-0023):
//
//	GET /api/v1/integrations          — list {slug,nome,descricao,configurada}
//	GET /api/v1/integrations/{slug}   — field schema + effective value per field
//	PUT /api/v1/integrations/{slug}   — PARTIAL write (null removes the key)
//
// Foundation for the /dashboard/integrations screen; this delivery has no UI of its own.
// The effective value of each field is always database → env var →
// absent (s.integrationsResolver, internal/integration); a `tipo:"secret"` field NEVER
// returns a raw `valor`, only `definido`/`origem`.

// integrationSummaryDTO is ONE item of the GET /integrations listing.
type integrationSummaryDTO struct {
	Slug        string `json:"slug"`
	Nome        string `json:"nome"`
	Descricao   string `json:"descricao"`
	Configurada bool   `json:"configurada"`
}

// integrationsListDTO is the body of GET /integrations.
type integrationsListDTO struct {
	Integrations []integrationSummaryDTO `json:"integrations"`
}

// integrationOpcaoDTO is one option of a TipoSelect field.
type integrationOpcaoDTO struct {
	Valor string `json:"valor"`
	Label string `json:"label"`
}

// integrationCampoDTO is ONE field in the body of GET/PUT /integrations/{slug}. `valor`
// only shows up when the field is NOT secret AND there is an effective value (omitempty +
// pointer: absent from the JSON when nil).
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

// integrationDetailDTO is the body of GET/PUT /integrations/{slug}.
type integrationDetailDTO struct {
	Slug      string                `json:"slug"`
	Nome      string                `json:"nome"`
	Descricao string                `json:"descricao"`
	Campos    []integrationCampoDTO `json:"campos"`
}

// putIntegrationRequestDTO is the body of PUT /integrations/{slug}. A `null` value in the
// JSON becomes a nil pointer here — it removes the key (which goes back to inheriting the
// env var); a key ABSENT from the object is not touched.
type putIntegrationRequestDTO struct {
	Valores map[string]*string `json:"valores"`
}

// listIntegrations handles GET /api/v1/integrations: it iterates the Registry, resolves
// the effective values of each Descriptor and calls Configurada(). Always 200 (even with an
// empty Registry — an empty list, not an error).
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

// getIntegration handles GET /api/v1/integrations/{slug}: it returns the field schema plus
// the effective value (database→env var→absent) of each one. 404 if the slug is not in the
// Registry.
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

// montarIntegrationDetail builds the common shape of GET and PUT (post-write echo) from
// the Descriptor + the Resolver: for each Campo, it computes the effective value and only
// includes a raw `valor` when the type is NOT secret and there is an effective value — a
// secret field never leaks the value, not even in the PUT echo.
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

// putIntegration handles PUT /api/v1/integrations/{slug}: PARTIAL write into the
// values_json of the slug — only the keys present in `valores` change; a `null` value
// REMOVES the key (which goes back to inheriting the env var). 404 if the slug is unknown;
// 422 if `valores` comes empty or names a key that is not a Campo of the Descriptor. It
// does not block on a missing required value — the owner may save partially (it only
// reflects as `configurada:false` in the listing).
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

// obterIntegrationValores reads the current values_json of a slug (an empty map when the
// slug was never written or the JSON is corrupted — the same defensive stance as the
// Resolver, which never propagates an error for that). Only a real database error (not
// sql.ErrNoRows) is propagated.
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
