package server

import (
	"net/http"
	"sort"

	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// Este arquivo serve o DESCRITOR DE FORMULÁRIO editorial (briefing 0030): a
// superfície que diz ao front COMO renderizar cada campo editável — sem que ele
// precise redigitar a lista de campos.
//
//	GET /api/v1/schema/entity-fields  → { "fields": [FieldDescriptor, ...] }
//
// A LISTA de campos é derivada da fonte única `resolver.CamposEditaveis()`
// (R1/ADR-0001); a APRESENTAÇÃO (label/grupo/widget/ordem/faixa) vive aqui, no
// server, num registro declarativo separado. São DOIS registros de propósito: o
// resolver cuida da composição (puro, sem concern de UI); o server cuida da
// apresentação (UI). Um teste os amarra — todo campo editável (exceto o
// extension_data, ver abaixo) TEM de ter descritor —, garantindo que adicionar
// um campo editável no backend nunca deixe o front órfão.

// apresentacaoCampo é o METADADO de UI de um campo editorial: como rotulá-lo,
// em que seção mostrá-lo, com que widget renderizá-lo e em que ordem. É um
// registro declarativo (dado), não um `switch` por nome — um plugin futuro
// contribuiria aqui a apresentação do campo que contribui ao resolver.
type apresentacaoCampo struct {
	label     string
	group     string // "traduzivel" | "neutro" (seção do formulário)
	widget    string // "text" | "textarea" | "number" | "date" | "tags"
	order     int    // ordem estável de renderização
	creatable bool   // aparece no formulário de CRIAR item?

	// min/max/step só fazem sentido para o widget "number" (espelham a faixa que
	// o backend valida em formaNumero); ficam nil nos demais.
	min  *float64
	max  *float64
	step *float64
	// placeholder é opcional; nil = sem placeholder.
	placeholder *string
}

// registroApresentacao é o metadado de UI por campo editorial (briefing 0030,
// §"Registro de apresentação"). As chaves casam com as de `registroOrigem` no
// resolver. A faixa 0–10 do local_rating só ESPELHA a validação autoritativa do
// backend (formaNumero) — o descritor não é a fonte da regra, só a reflete para
// o input.
var registroApresentacao = map[string]apresentacaoCampo{
	"title":          {label: "Título", group: "traduzivel", widget: "text", order: 10, creatable: true},
	"description":    {label: "Sinopse", group: "traduzivel", widget: "textarea", order: 20, creatable: true},
	"tags":           {label: "Tags", group: "traduzivel", widget: "tags", order: 30, creatable: false},
	"original_title": {label: "Título original", group: "neutro", widget: "text", order: 40, creatable: true},
	"release_date":   {label: "Data de lançamento", group: "neutro", widget: "date", order: 50, creatable: true},
	"local_rating": {label: "Sua nota", group: "neutro", widget: "number", order: 60, creatable: false,
		min: fptr(0), max: fptr(10), step: fptr(0.1)},
	// extension_data NÃO entra aqui de propósito: embora esteja em
	// CamposEditaveis() (kind=extensao), NÃO é um campo de formulário simples — é
	// o mapa de verticais por namespace/plugin, com edição própria. Por isso é
	// EXCLUÍDO dos descritores (ver descritoresDeCampo). A regra da catraca de
	// cobertura (formschema_test.go) exige descritor de TODO campo editável com
	// kind != extensao.
}

// fieldDescriptor é o descritor servido de um campo editável (briefing 0030 §4):
// combina a apresentação (registroApresentacao) com a etiqueta pública do
// resolver (translatable/kind). É o que o front consome para montar o input e a
// regra de validação de cliente.
type fieldDescriptor struct {
	Name         string `json:"name"`         // chave do campo (= chave do override/registro)
	Label        string `json:"label"`        // rótulo pt-BR (UI mono-idioma hoje)
	Group        string `json:"group"`        // "traduzivel" | "neutro"
	Widget       string `json:"widget"`       // "text" | "textarea" | "number" | "date" | "tags"
	Translatable bool   `json:"translatable"` // = registroOrigem[campo].traduzivel
	Kind         string `json:"kind"`         // = Kind público do resolver (escalar|lista|numero)
	Order        int    `json:"order"`        // ordem estável de renderização
	Creatable    bool   `json:"creatable"`    // aparece no formulário de CRIAR item?

	// Só para o widget "number"; omitidos (omitempty) nos demais.
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Step *float64 `json:"step,omitempty"`
	// Opcional; omitido quando ausente.
	Placeholder *string `json:"placeholder,omitempty"`
}

// fieldSchemaResponse é o envelope de resposta do endpoint: a lista de
// descritores, já ORDENADA por `order` crescente.
type fieldSchemaResponse struct {
	Fields []fieldDescriptor `json:"fields"`
}

// descritoresDeCampo combina a fonte única `resolver.CamposEditaveis()` com o
// registro de apresentação, gerando a lista de descritores ORDENADA por `order`.
// Itera os campos EDITÁVEIS do resolver (nunca um `switch` por nome): translatable
// e kind vêm do resolver; label/grupo/widget/ordem/faixa vêm da apresentação.
//
// O extension_data (kind=extensao) é EXCLUÍDO: não é campo de formulário simples
// (é o mapa de verticais por namespace/plugin). Um campo editável de kind !=
// extensao SEM apresentação é erro de programação — o teste de cobertura falha,
// forçando o par "campo novo ⇒ descritor".
func descritoresDeCampo() []fieldDescriptor {
	editaveis := resolver.CamposEditaveis()
	out := make([]fieldDescriptor, 0, len(editaveis))
	for campo, info := range editaveis {
		if info.Kind == resolver.KindExtensao {
			continue // extension_data: editável, mas não é campo de formulário simples.
		}
		apr, ok := registroApresentacao[campo]
		if !ok {
			// Campo editável sem apresentação: erro de programação (a catraca de
			// cobertura pega em teste). Não o servimos sem descritor.
			continue
		}
		out = append(out, fieldDescriptor{
			Name:         campo,
			Label:        apr.label,
			Group:        apr.group,
			Widget:       apr.widget,
			Translatable: info.Traduzivel,
			Kind:         string(info.Kind),
			Order:        apr.order,
			Creatable:    apr.creatable,
			Min:          apr.min,
			Max:          apr.max,
			Step:         apr.step,
			Placeholder:  apr.placeholder,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

// getEntityFieldsSchema trata GET /api/v1/schema/entity-fields: devolve os
// descritores de campo do formulário editorial (briefing 0030). Metadado estático
// (imutável entre deploys), sem autenticação e cacheável — não toca o banco.
func (s *Server) getEntityFieldsSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, fieldSchemaResponse{Fields: descritoresDeCampo()})
}

// fptr embrulha um float64 num *float64 (a apresentação carrega min/max/step como
// ponteiros para omiti-los, via omitempty, nos campos não-numéricos).
func fptr(v float64) *float64 { return &v }
