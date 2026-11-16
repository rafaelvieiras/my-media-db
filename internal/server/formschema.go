package server

import (
	"net/http"
	"sort"

	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// This file serves the editorial FORM DESCRIPTOR: the surface that tells the frontend
// HOW to render each editable field — without it having to retype the list of
// fields.
//
//	GET /api/v1/schema/entity-fields  → { "fields": [FieldDescriptor, ...] }
//
// The LIST of fields is derived from the single source `resolver.CamposEditaveis()`
// (ADR-0001); the PRESENTATION (label/group/widget/order/range) lives here, in the
// server, in a separate declarative registry. They are TWO registries on purpose: the
// resolver handles the composition (pure, with no UI concern); the server handles the
// presentation (UI). A test ties them together — every editable field (except
// extension_data/credits, see below) MUST have a descriptor —, ensuring that adding an
// editable field on the backend never leaves the frontend orphaned.

// apresentacaoCampo is the UI METADATA of an editorial field: how to label it, in which
// section to show it, with which widget to render it and in which order. It is a
// declarative registry (data), not a `switch` by name — a future plugin would contribute
// here the presentation of the field it contributes to the resolver.
type apresentacaoCampo struct {
	label     string
	group     string // "traduzivel" | "neutro" (form section)
	widget    string // "text" | "textarea" | "number" | "date" | "tags"
	order     int    // stable rendering order
	creatable bool   // does it show up in the CREATE item form?

	// min/max/step only make sense for the "number" widget (they mirror the range the
	// backend validates in formaNumero); they stay nil on the others.
	min  *float64
	max  *float64
	step *float64
	// placeholder is optional; nil = no placeholder.
	placeholder *string
}

// registroApresentacao is the UI metadata per editorial field (the "presentation
// registry" section of the design). The keys match the ones of `registroOrigem` in the
// resolver. The 0–10 range of local_rating only MIRRORS the authoritative validation of
// the backend (formaNumero) — the descriptor is not the source of the rule, it only
// reflects it to the input.
var registroApresentacao = map[string]apresentacaoCampo{
	"title":          {label: "Título", group: "traduzivel", widget: "text", order: 10, creatable: true},
	"description":    {label: "Sinopse", group: "traduzivel", widget: "textarea", order: 20, creatable: true},
	"tags":           {label: "Tags", group: "traduzivel", widget: "tags", order: 30, creatable: false},
	"original_title": {label: "Título original", group: "neutro", widget: "text", order: 40, creatable: true},
	"release_date":   {label: "Data de lançamento", group: "neutro", widget: "date", order: 50, creatable: true},
	"local_rating": {label: "Sua nota", group: "neutro", widget: "number", order: 60, creatable: false,
		min: fptr(0), max: fptr(10), step: fptr(0.1)},
	// extension_data does NOT go in here on purpose: although it is in
	// CamposEditaveis() (kind=extensao), it is NOT a simple form field — it is the map
	// of verticals per namespace/plugin, with its own editor. That is why it is EXCLUDED
	// from the descriptors (see descritoresDeCampo). The coverage ratchet rule
	// (formschema_test.go) requires a descriptor for EVERY editable field with
	// kind != extensao.
}

// fieldDescriptor is the served descriptor of an editable field: it combines the
// presentation (registroApresentacao) with the resolver's public label
// (translatable/kind). It is what the frontend consumes to build the input and the
// client-side validation rule.
type fieldDescriptor struct {
	Name         string `json:"name"`         // field key (= key of the override/registry)
	Label        string `json:"label"`        // pt-BR label (single-language UI today)
	Group        string `json:"group"`        // "traduzivel" | "neutro"
	Widget       string `json:"widget"`       // "text" | "textarea" | "number" | "date" | "tags"
	Translatable bool   `json:"translatable"` // = registroOrigem[campo].traduzivel
	Kind         string `json:"kind"`         // = public Kind of the resolver (escalar|lista|numero)
	Order        int    `json:"order"`        // stable rendering order
	Creatable    bool   `json:"creatable"`    // does it show up in the CREATE item form?

	// Only for the "number" widget; omitted (omitempty) on the others.
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Step *float64 `json:"step,omitempty"`
	// Optional; omitted when absent.
	Placeholder *string `json:"placeholder,omitempty"`
}

// fieldSchemaResponse is the response envelope of the endpoint: the list of descriptors,
// already SORTED by ascending `order`.
type fieldSchemaResponse struct {
	Fields []fieldDescriptor `json:"fields"`
}

// descritoresDeCampo combines the single source `resolver.CamposEditaveis()` with the
// presentation registry, generating the list of descriptors SORTED by `order`. It
// iterates the EDITABLE fields of the resolver (never a `switch` by name): translatable
// and kind come from the resolver; label/group/widget/order/range come from the
// presentation.
//
// The extension_data (kind=extensao) and credits (kind=creditos) are EXCLUDED: they are
// not simple form fields (extension_data is the map of verticals per namespace/plugin;
// credits is the patch segregated by cast/crew/companies) — both get a DEDICATED editor
// in the frontend, outside the SchemaForm. An editable field of kind !=
// extensao/creditos WITHOUT presentation is a programming error — the coverage test
// fails, forcing the pair "new field ⇒ descriptor".
func descritoresDeCampo() []fieldDescriptor {
	editaveis := resolver.CamposEditaveis()
	out := make([]fieldDescriptor, 0, len(editaveis))
	for campo, info := range editaveis {
		if info.Kind == resolver.KindExtensao || info.Kind == resolver.KindCreditos {
			continue // extension_data/credits: editable, but with a dedicated editor (not a simple form).
		}
		apr, ok := registroApresentacao[campo]
		if !ok {
			// Editable field without presentation: a programming error (the coverage
			// ratchet catches it in a test). We do not serve it without a descriptor.
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

// getEntityFieldsSchema handles GET /api/v1/schema/entity-fields: returns the field
// descriptors of the editorial form. Static metadata (immutable between deploys), with no
// authentication and cacheable — it does not touch the database.
func (s *Server) getEntityFieldsSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, fieldSchemaResponse{Fields: descritoresDeCampo()})
}

// fptr wraps a float64 in a *float64 (the presentation carries min/max/step as pointers
// so they are omitted, through omitempty, on the non-numeric fields).
func fptr(v float64) *float64 { return &v }
