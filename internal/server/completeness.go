package server

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// This file implements the CATALOG COMPLETENESS ASSESSMENT per item:
// it consumes the ruler configured in catalog_completeness.go and decides, for ONE item,
// what is missing. Distinct from OWNERSHIP completeness
// (web/src/lib/entity-badges.ts) -- see the note in catalog_completeness.go,
// repeated there.
//
// avaliarCompletude is PURPOSEFULLY pure (no I/O): the SAME test covers the
// two paths that consume it -- live in the detail
// (anotarCompletudeCatalogacao, entities.go) and in batch in the job
// (RecalcularCompletudeCatalogacao, completeness_recalc.go). Only the FILL
// mode of dadosCompletude changes between the two; the rule itself lives only here.

// statusDraft is the entity.status value of an item never matched:
// the review queue and the ListarRevisao query already use this same
// raw string (internal/db/queries/review.sql); centralized here only so the
// call-sites (ehDraft) do not spread the literal.
const statusDraft = "draft"

// dadosCompletude is the shape DECOUPLED from the data origin, evaluated against
// the ruler of ONE type (tipoCompletenessConfig): filled both from an
// already resolved resolver.Entidade (live detail path, via
// dadosCompletudeDeEntidade) and by a lighter read inside the batch job (which
// today reuses the SAME resolverEntity -- see completeness_recalc.go).
type dadosCompletude struct {
	Description    *string
	Runtime        *int
	Certification  *string
	Keywords       []string
	TemElenco      bool
	TemArquivo     bool
	ArtesPresentes map[string]bool
}

// resultadoCompletude is an ALIAS of resolver.CatalogCompleteness (not a new
// type): it has to be the SAME type that resolver.Entidade.CatalogCompleteness
// carries, so anotarCompletudeCatalogacao (entities.go) can fill the detail field
// without a conversion — and so the card DTO (entityCardDTO,
// completeness_recalc.go) can reuse the same shape when reading/writing the
// entity_catalog_completeness cache. The JSON tags (complete/missing) live in
// the canonical definition in internal/resolver (it is what the detail serializes
// directly through GET /entities/{id}).
type resultadoCompletude = resolver.CatalogCompleteness

// Stable keys of `missing`: they mirror 1:1 the booleans of
// tipoCompletenessConfig; prefixoArtwork builds `artwork:<kind>` (one item per
// kind ABSENT, not an aggregated "artwork" -- the owner wants to know EXACTLY
// what is missing).
const (
	chaveDescription   = "description"
	chaveRuntime       = "runtime"
	chaveCertification = "certification"
	chaveKeywords      = "keywords"
	chaveCast          = "cast"
	chaveMediaFile     = "media_file"
	prefixoArtwork     = "artwork:"
)

// avaliarCompletude decides whether ONE item meets the `cfg` catalog completeness
// ruler. PURE (no I/O): testable in isolation, without a database.
// `achou=false` (the entity type has no entry in the config -- "absent type =
// nothing required") returns {true, []} straight away, without looking at
// `dados`. Otherwise, it tests each configured boolean against `dados` and each
// kind of `cfg.Artwork` against `dados.ArtesPresentes`, accumulating the missing
// keys in the fixed order description, runtime, certification, keywords,
// cast, media_file, artwork:<kind...> (the order of cfg.Artwork -- the one the
// owner configured in /settings, not alphabetical) -- stable order = predictable
// tooltip/list, useful for tests and for not reordering the UI on every render.
func avaliarCompletude(cfg tipoCompletenessConfig, achou bool, dados dadosCompletude) resultadoCompletude {
	if !achou {
		return resultadoCompletude{Complete: true, Missing: []string{}}
	}
	faltando := make([]string, 0)
	if cfg.Description && campoTextoVazio(dados.Description) {
		faltando = append(faltando, chaveDescription)
	}
	if cfg.Runtime && dados.Runtime == nil {
		faltando = append(faltando, chaveRuntime)
	}
	if cfg.Certification && campoTextoVazio(dados.Certification) {
		faltando = append(faltando, chaveCertification)
	}
	if cfg.Keywords && len(dados.Keywords) == 0 {
		faltando = append(faltando, chaveKeywords)
	}
	if cfg.Cast && !dados.TemElenco {
		faltando = append(faltando, chaveCast)
	}
	if cfg.MediaFile && !dados.TemArquivo {
		faltando = append(faltando, chaveMediaFile)
	}
	for _, kind := range cfg.Artwork {
		if !dados.ArtesPresentes[kind] {
			faltando = append(faltando, prefixoArtwork+kind)
		}
	}
	return resultadoCompletude{Complete: len(faltando) == 0, Missing: faltando}
}

// campoTextoVazio says whether a translatable text field (synopsis/classification)
// counts as MISSING for the ruler: nil pointer (never resolved) OR a blank
// string (resolved, but empty -- the same "nothing to show" UX that the
// detail already treats as absence).
func campoTextoVazio(s *string) bool {
	return s == nil || strings.TrimSpace(*s) == ""
}

// ehDraft says whether an entity status is 'draft' (never matched): a draft
// item has no resolved metadata to assess catalog completeness
// (explicitly out of scope). Named for clarity at the call-sites (entities.go/
// completeness_recalc.go).
func ehDraft(status string) bool {
	return status == statusDraft
}

// dadosCompletudeDeEntidade adapts the already resolved detail shape
// to dadosCompletude, without any extra query: Description/Runtime/
// Certification/Keywords/Cast/Owned already come ready from resolver.Entidade
// (resolverEntity builds them anyway, with or without completeness).
// `assets` is the SAME list resolverEntity already loaded
// (ListarAssetsPorEntity) to build poster_url/backdrop_url/still_url --
// reused here, never re-queried (resolver.Entidade does not keep the raw
// asset list, only the already resolved URLs, which is why it comes in as a
// separate parameter instead of a field of the shape).
func dadosCompletudeDeEntidade(e resolver.Entidade, assets []sqlcgen.Asset) dadosCompletude {
	return dadosCompletude{
		Description:    e.Description,
		Runtime:        e.Runtime,
		Certification:  e.Certification,
		Keywords:       e.Keywords,
		TemElenco:      len(e.Cast) > 0,
		TemArquivo:     e.Owned,
		ArtesPresentes: artesPresentesDeAssets(assets),
	}
}

// completudeDoCache decodes the completeness_complete/
// completeness_missing_json columns (LEFT JOIN entity_catalog_completeness) read
// by the card queries (ListarEntityCards/ListarEntityCardsDesc/
// ObterEntityCard): nil when the cache row does not exist
// (NULL, item not evaluated yet -- new or draft) or the missing_json is
// corrupted (degrades to nil -- never breaks the listing because of an invalid
// cache).
func completudeDoCache(complete sql.NullInt64, missingJSON sql.NullString) *resultadoCompletude {
	if !complete.Valid || !missingJSON.Valid {
		return nil
	}
	var missing []string
	if err := json.Unmarshal([]byte(missingJSON.String), &missing); err != nil {
		return nil
	}
	if missing == nil {
		missing = []string{}
	}
	return &resultadoCompletude{Complete: complete.Int64 != 0, Missing: missing}
}

// artesPresentesDeAssets summarizes `assets` (ListarAssetsPorEntity) into a map
// kind -> has a SERVABLE asset, by the SAME precedence of escolherAssetServido
// (assets.go): preferred -> user -> external -> base. Used only
// for the completeness assessment; not to be confused with the
// choice of ONE asset to serve (poster_url/backdrop_url), made separately
// in resolverEntity/artesLocais.
func artesPresentesDeAssets(assets []sqlcgen.Asset) map[string]bool {
	presentes := make(map[string]bool)
	for _, a := range assets {
		if presentes[a.Kind] {
			continue
		}
		if _, ok := escolherAssetServido(assets, a.Kind); ok {
			presentes[a.Kind] = true
		}
	}
	return presentes
}
