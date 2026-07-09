// Package resolver compõe a visão RESOLVIDA de uma entity a partir das camadas
// disponíveis. Na Fase 2 existe uma única camada — a do ARQUIVO/núcleo (a Entity
// do núcleo mais os seus media_file) —, mas a forma da resposta já é a
// DEFINITIVA (doc 04/03): a Fase 3 vai adicionar as camadas `externo`
// (media_external) e `override` (media_override) POR BAIXO, compondo a
// precedência `override ⟵ externo ⟵ arquivo` campo a campo, SEM mudar a
// assinatura pública nem o shape servido. Assim a UI (briefing 0011) não muda
// quando o enriquecimento chegar.
//
// A costura para a Fase 3 está pronta (campos comentados em Camadas), mas nada
// que ainda não exista é criado aqui — evita YAGNI sem pintar a API num canto.
package resolver

import (
	"database/sql"
	"encoding/json"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Camadas agrega as CAMADAS que o resolver compõe. Hoje só a camada
// arquivo/núcleo: a Entity do núcleo, os seus arquivos e o link do pôster local
// (quando há um asset de pôster). A Fase 3 acrescenta AQUI os campos Externo e
// Override — quem chama passa o que tiver e o resolver compõe; a assinatura de
// Resolver não muda.
type Camadas struct {
	// Entity é a linha de núcleo (camada base, sempre presente).
	Entity sqlcgen.Entity
	// Files são os media_file da entity (posse derivada e contagem saem daqui).
	Files []sqlcgen.MediaFile
	// PosterURL é a referência (relativa) do endpoint que serve o pôster local,
	// ou nil quando a entity não tem pôster. O chamador a monta (é um link de
	// API, não um dado de domínio), o resolver apenas a coloca no shape final.
	PosterURL *string

	// Fase 3 (ainda NÃO existem no schema — a costura fica pronta, o campo não):
	//   Externo  *sqlcgen.MediaExternal  // payload externo cru por fonte
	//   Override *sqlcgen.MediaOverride  // patch esparso do dono (vence sempre)
}

// Entidade é a visão RESOLVIDA — o shape final servido em
// GET /api/v1/entities/{id}. Os ponteiros saem como `null` no JSON quando
// ausentes; `tags`/`external_ids` saem sempre como lista/objeto (nunca null)
// para simplificar o consumo no front.
type Entidade struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Title         string            `json:"title"`
	OriginalTitle *string           `json:"original_title"`
	Description   *string           `json:"description"`
	ReleaseDate   *string           `json:"release_date"`
	Status        string            `json:"status"`
	Tags          []string          `json:"tags"`
	ExternalIDs   map[string]string `json:"external_ids"`
	Owned         bool              `json:"owned"`
	FileCount     int               `json:"file_count"`
	PosterURL     *string           `json:"poster_url"`
}

// Resolver compõe a Entidade resolvida a partir das camadas. É o PONTO ÚNICO
// onde a precedência `override ⟵ externo ⟵ arquivo` será aplicada na Fase 3;
// hoje há só a base arquivo/núcleo, então os campos vêm diretos da Entity, e a
// posse/contagem, dos arquivos.
func Resolver(c Camadas) Entidade {
	ent := Entidade{
		ID:            c.Entity.ID,
		Type:          c.Entity.Type,
		Title:         c.Entity.Title,
		OriginalTitle: nsPtr(c.Entity.OriginalTitle),
		Description:   nsPtr(c.Entity.Description),
		ReleaseDate:   nsPtr(c.Entity.ReleaseDate),
		Status:        c.Entity.Status,
		Tags:          parseTags(c.Entity.TagsJson),
		ExternalIDs:   parseExternalIDs(c.Entity.ExternalIdsJson),
		Owned:         temPosse(c.Files),
		FileCount:     len(c.Files),
		PosterURL:     c.PosterURL,
	}
	return ent
}

// temPosse aplica a POSSE DERIVADA (ADR-0009) sobre os arquivos já carregados:
// verdadeiro se, e só se, existe ao menos um media_file com state='present'.
func temPosse(files []sqlcgen.MediaFile) bool {
	for _, f := range files {
		if f.State == "present" {
			return true
		}
	}
	return false
}

// parseTags decodifica tags_json (um array JSON de strings) numa fatia. Sempre
// devolve uma fatia não-nil (vazia quando ausente/inválido) para o JSON sair
// como `[]` em vez de `null`.
func parseTags(raw sql.NullString) []string {
	out := []string{}
	if !raw.Valid || raw.String == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw.String), &out)
	if out == nil {
		return []string{}
	}
	return out
}

// parseExternalIDs decodifica external_ids_json (objeto JSON string->string).
// Valores não-string são ignorados. Sempre devolve um mapa não-nil.
func parseExternalIDs(raw sql.NullString) map[string]string {
	out := map[string]string{}
	if !raw.Valid || raw.String == "" {
		return out
	}
	bruto := map[string]any{}
	if err := json.Unmarshal([]byte(raw.String), &bruto); err != nil {
		return out
	}
	for k, v := range bruto {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// nsPtr converte um sql.NullString em *string (nil quando NULL) para o campo
// sair como `null` no JSON.
func nsPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}
