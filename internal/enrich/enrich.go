// Package enrich is the ENRICHMENT service (explicit import): it pulls a provider's
// detail, persists the external base (media_external, raw payload — ADR-0011), writes
// the authoritative identity (external_ids, MD4) and caches the poster on disk as an
// asset with origin='external'. It is NON-DESTRUCTIVE (ADR-0012): it never touches
// media_override nor artwork with origin='user'. All writes go through the write pool
// (single writer, ADR-0010). An IMAGE failure degrades (logs, continues without a
// poster); a SOURCE failure (BuscarDetalhe) is propagated to the caller.
package enrich

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/externaldetail"
	"github.com/rafaelvieiras/my-media-db/internal/historico"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// timeoutImagem bounds the download of ONE poster; a timeout degrades (no poster).
	timeoutImagem = 15 * time.Second
	// tamanhoMaxImagem is the byte ceiling of a downloaded poster (a guard against a
	// hostile/oversized source). 20 MiB covers a w342 with room to spare.
	tamanhoMaxImagem = 20 << 20
	// kindPoster/kindFanart/originExternal are the tags of the cached assets.
	// fanart = background image (backdrop); cached locally just like the poster so as
	// not to depend on the external source at runtime (same reason as the poster).
	kindPoster     = "poster"
	kindFanart     = "fanart"
	originExternal = "external"
	// tipoColecao/relacaoColecao: a collection is an entity `type='collection'` and its
	// movies are linked through entity_relation with kind='collection'.
	tipoColecao    = "collection"
	relacaoColecao = "collection"
	// tipoFilme is the movie's entity.type — the trigger for artwork enrichment on
	// item import (a series fires its own after the tree, in importarSerie).
	tipoFilme = "movie"
)

// Servico runs the enrichment. It receives the database pools, the multi-provider
// REGISTRY (the source is chosen by name) and the trusted assets directory (where the
// poster binary is written).
type Servico struct {
	database  *db.DB
	providers provider.Registry
	assetsDir string
	// detalhe resolves the external detail CACHE-FIRST (external_detail): the
	// import/enrich now pulls through here, reusing the rich-detail cache when it is
	// fresh (no second trip to the source) and seeding media_external from it.
	detalhe *externaldetail.Servico
	// httpImagem downloads the poster binary (short timeout). Injectable in tests.
	httpImagem *http.Client
	// novoID generates ids for new rows (media_external/asset). Injectable in tests.
	novoID func() string
}

// New creates the service. provider and assetsDir are required to operate; main only
// builds the service when both exist (without a credential, enrich stays off). The
// external-detail cache (external_detail) is built here and shares the table with the
// external-detail GET handler (the state is the database, not the instance).
func New(database *db.DB, provs provider.Registry, assetsDir string) *Servico {
	return &Servico{
		database:   database,
		providers:  provs,
		assetsDir:  assetsDir,
		detalhe:    externaldetail.New(database, provs, 0),
		httpImagem: &http.Client{Timeout: timeoutImagem},
		novoID:     func() string { return uuid.NewString() },
	}
}

// provFonte returns the provider for source `source` from the registry. Missing => (nil,false):
// the caller degrades (the normal flow already guarantees the provider, since PayloadFresco
// fails earlier when the source does not exist).
func (s *Servico) provFonte(source string) (provider.Provider, bool) {
	return s.providers.Get(source)
}

// Enriquecer enriches an ALREADY EXISTING entity: it pulls the detail (in the allowed
// languages `langs`), merges the external identity and persists the external base +
// poster + original_language. A source failure is propagated; an image failure
// degrades. Flow of POST /entities/{id}/enrich (and of every caller that applies an
// external identity to an already existing entity — Corrigir, Confirmar candidato,
// auto-match, retroactive/enrich-missing). Thin wrapper around enriquecerBase that
// always tries to sync the series tree: this is the only point besides importarSerie
// that builds season/episode. `force` (the "Baixar Metadados" action of the Gerenciar
// menu) propagates to ALL the externaldetail.PayloadFresco calls of this pass (base +
// rich enrichment of the series, when there is one), skipping the 24h cache and always
// going to the network — the cache write still happens, so the next call without force
// already serves the fresh result.
func (s *Servico) Enriquecer(ctx context.Context, entityID, source, sourceID, tipo string, langs []string, force bool) error {
	return s.enriquecerBase(ctx, entityID, source, sourceID, tipo, langs, true, force)
}

// enriquecerBase is the body of Enriquecer plus the sincronizarArvore parameter:
// when tipo==tipoSerie and sincronizarArvore, it syncs the season/episode tree as the
// LAST step, best-effort — it logs and degrades, NEVER propagating the error to the
// caller. At that point mesclarExternalIds/persistir/curarTitulo have already written to
// the database (external_ids, base payload, poster, title); a tree-only failure cannot
// make the HTTP caller report "failed" about an operation that already changed the
// state of the entity, and the best-effort callers (aplicarCandidatoExterno in the
// auto-match/retroactive) already treat an Enriquecer error as "not applied, stays
// pending" — propagating would mean an identity that already crossed the confidence
// threshold never gets applied, a regression worse than the missing tree. `force` is
// forwarded to the PayloadFresco call below; the two internal call sites of
// importarItemBase always pass force=false (a new item/reuse in the import already
// fetches fresh by definition, outside the scope of the "Baixar Metadados" button).
func (s *Servico) enriquecerBase(ctx context.Context, entityID, source, sourceID, tipo string, langs []string, sincronizarArvore, force bool) error {
	raw, err := s.detalhe.PayloadFresco(ctx, source, sourceID, tipo, langs, force)
	if err != nil {
		return fmt.Errorf("enrich: buscar detalhe: %w", err)
	}
	if err := s.mesclarExternalIds(ctx, entityID, source, sourceID); err != nil {
		return err
	}
	if err := s.persistir(ctx, entityID, source, sourceID, raw); err != nil {
		return err
	}
	if err := s.curarTitulo(ctx, entityID, source, sourceID, raw, langs); err != nil {
		return err
	}
	// fanart.tv artwork inventory for MOVIE outside the explicit import:
	// Importar already triggers it via importarItem (line ~165); until then, the public
	// path of Enriquecer() (auto-match, retroactive, "Corrigir", "Confirmar candidato",
	// POST /entities/{id}/enrich) NEVER called enriquecerArtes for a movie, leaving the
	// overwhelming majority of new movies (which come in through the scan, not through a
	// manual search) without the extra fanart.tv artwork. sincronizarArvore=false in the
	// two internal call sites of importarItemBase avoids a double call here: those cases
	// are already covered by the explicit importarItem call right after.
	if tipo == tipoFilme && sincronizarArvore {
		s.enriquecerArtes(ctx, entityID, tipo, langs)
		// Theme-music candidate (ADR-0025): same trigger/guard as
		// enriquecerArtes above -- both depend only on the external_ids already
		// merged in this pass, with no step of their own.
		s.enriquecerTemaMusical(ctx, entityID, tipo, langs)
	}
	if tipo == tipoArtista && sincronizarArvore {
		// ARTIST artwork inventory (fanart.tv) outside the explicit tree import (same
		// cause as the movie case above, only in the music vertical): importarArtista/
		// ImportarFaixaPossuida (musica.go) call enriquecerArtes on creation, but they
		// NEVER go through enriquecerBase -- so a pre-existing artist without
		// external_ids (legacy data, from before MusicBrainz) that gains the mbid through
		// the generic Enriquecer() path (auto-match, retroactive, "Corrigir",
		// "Confirmar candidato", POST /entities/{id}/enrich) never triggered the artwork
		// search -- it stayed forever without a photo/background from fanart.tv. No risk
		// of a double call: no call site with tipo=="artista" passes sincronizarArvore=
		// false (importarItemBase, which uses the flag to avoid duplicating, is never
		// called for this type -- Importar dispatches an artist straight to
		// importarArtista).
		s.enriquecerArtes(ctx, entityID, tipo, langs)
	}
	if tipo == tipoSerie {
		// TVDB cross-id captured from the base-source detail, when it supports the
		// optional capability (today only the TMDB client): it closes the gap that left
		// every series whose tree comes ONLY from TMDB (with no TVDB configured, the
		// provider independence) without a usable id for fanart.tv, which on TV requires
		// tvdb_id exclusively (fanarttv.rotaETabela, case "tv"). It runs BEFORE
		// sincronizarArvoreSerie/enriquecerArtes (below) so that idsExternosDe already
		// sees the new tvdb in the SAME pass. Non-destructive (it never overwrites a
		// tvdb already present) and best-effort (it degrades silently without the
		// capability/match).
		s.mesclarTvdbCruzado(ctx, entityID, source, raw)
	}
	if tipo == tipoSerie && sincronizarArvore {
		if err := s.sincronizarArvoreSerie(ctx, entityID, source, sourceID, langs); err != nil {
			log.Printf("enrich: sincronizar arvore da serie %s (fonte %s id %s) falhou (segue so-identidade): %v", entityID, source, sourceID, err)
		}
	}
	return nil
}

// Importar is the find-or-create by external id (MD4) of POST /import: if an entity
// with that id already exists at the source, it reuses and re-enriches it
// (criada=false); otherwise it creates one (DEFAULT status 'cataloged', initial title
// from the detail) and enriches it (criada=true). It returns the entity id resolvable
// by the caller.
func (s *Servico) Importar(ctx context.Context, source, sourceID, tipo string, langs []string) (id string, criada bool, err error) {
	switch tipo {
	case tipoColecao:
		return s.importarColecao(ctx, source, sourceID, langs)
	case tipoSerie:
		return s.importarSerie(ctx, source, sourceID, langs)
	case tipoArtista:
		return s.importarArtista(ctx, source, sourceID, langs)
	default:
		return s.importarItem(ctx, source, sourceID, tipo, langs)
	}
}

// importarItem is the find-or-create of a single ITEM (movie, or the base row of a
// series/season/episode): it reuses the existing entity (dedup by external id) and
// re-enriches it, or creates a new one (initial title from the detail) and persists the
// external base + poster. It is the path shared by Importar (movie) and importarSerie
// (the series itself uses this template for detail/poster/curarTitulo before the tree).
func (s *Servico) importarItem(ctx context.Context, source, sourceID, tipo string, langs []string) (string, bool, error) {
	id, criada, err := s.importarItemBase(ctx, source, sourceID, tipo, langs)
	if err != nil {
		return id, criada, err
	}
	// Artwork inventory: only the MOVIE triggers here — the series node also goes
	// through importarItem (inside importarSerie), but its artwork waits for the tree
	// (importarSerie calls enriquecerArtes later, to route the season artwork).
	// Best-effort.
	if tipo == tipoFilme {
		s.enriquecerArtes(ctx, id, tipo, langs)
		// Theme-music candidate (ADR-0025): same trigger as enriquecerArtes
		// above -- the MOVIE import path fires both together.
		s.enriquecerTemaMusical(ctx, id, tipo, langs)
	}
	return id, criada, nil
}

// importarItemSemArvore is the variant of importarItemBase used by importarSerie to
// resolve the series entity WITHOUT firing anything tree-related on its own: today it
// is importarItemBase directly, because this path already does not fire the artwork
// trigger (it stays outside tipoSerie) nor sync the tree by itself (the two internal
// call sites of Enriquecer inside importarItemBase pass sincronizarArvore=false) —
// importarSerie syncs the tree explicitly right after, exactly once.
func (s *Servico) importarItemSemArvore(ctx context.Context, source, sourceID, tipo string, langs []string) (string, bool, error) {
	return s.importarItemBase(ctx, source, sourceID, tipo, langs)
}

// importarItemBase is the pure find-or-create of the item (without the artwork
// enrichment, which importarItem fires on top). Keeping it separate keeps the artwork
// trigger out of the series node path (which reuses this template) and the gocyclo low.
//
// The two internal call sites of enriquecerBase below pass sincronizarArvore=false:
// when tipo==tipoSerie, the only caller of these branches is importarSerie (through
// importarItemSemArvore), which already syncs the tree explicitly right after — without
// the flag, the reuse branch would make TWO trips to the provider (BuscarSerieArvore
// here inside the internal Enriquecer, and again in importarSerie). For MOVIE the flag
// ALSO avoids the double call to enriquecerArtes: with sincronizarArvore=false here, the
// new trigger inside enriquecerBase does not fire — the one that fires is importarItem
// itself, right below (see its comment). It is no longer "harmless" for
// movie/collection: the parameter now has a real effect for movies too.
func (s *Servico) importarItemBase(ctx context.Context, source, sourceID, tipo string, langs []string) (string, bool, error) {
	if existente, ok, err := s.acharPorIDExterno(ctx, source, sourceID); err != nil {
		return "", false, err
	} else if ok {
		return existente, false, s.enriquecerBase(ctx, existente, source, sourceID, tipo, langs, false, false)
	}

	// It does not exist: pull the detail once (for the initial title) and create the
	// entity already with the external identity; then persist the external base and the
	// poster. The detail comes from cache-first (external_detail): the first trip writes,
	// the next ones reuse.
	raw, err := s.detalhe.PayloadFresco(ctx, source, sourceID, tipo, langs, false)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar detalhe: %w", err)
	}
	titulo := s.tituloInicial(source, raw, sourceID, langs)

	// Name matching against local drafts: before creating a new entity, try to find a
	// PURE draft (with NO external_id) of the same type and same title/year. High
	// confidence -> enrich THAT draft instead of creating a second entity (the orphan
	// "draft with a file, without metadata" + "imported with metadata, without a file"
	// that motivated this work). No candidate: follow the normal creation flow,
	// unchanged.
	if idDraft, achou, err := s.tentarCasarComDraftLocal(ctx, titulo, s.anoDoPayload(source, raw), tipo); err != nil {
		return "", false, err
	} else if achou {
		return idDraft, false, s.enriquecerBase(ctx, idDraft, source, sourceID, tipo, langs, false, false)
	}

	extIDs, err := json.Marshal(map[string]string{source: sourceID})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              novo,
		Type:            tipo,
		Title:           titulo,
		ExternalIdsJson: nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	if err := s.persistir(ctx, novo, source, sourceID, raw); err != nil {
		return novo, true, err
	}
	return novo, true, nil
}

// importarColecao imports a COLLECTION as an entity `type='collection'`:
// find-or-create of the collection (dedup by tmdb id, restricted to type='collection'),
// caches the artwork of the set (poster/background) and imports EACH member movie
// (reusing the Importar of movie, which does find-or-create + enrich), linking it through
// entity_relation (kind='collection'). It is idempotent: re-importing reuses the
// collection and only links members not linked yet. The failure of ONE part degrades
// (logs and continues) — the partial collection is valid. It requires the optional
// capability provider.ColecaoProvider (otherwise error → 502).
func (s *Servico) importarColecao(ctx context.Context, source, sourceID string, langs []string) (string, bool, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	cp, ok := prov.(provider.ColecaoProvider)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q nao suporta colecao", source)
	}
	col, err := cp.BuscarColecao(ctx, sourceID, langPrincipal(langs))
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar colecao: %w", err)
	}
	colID, criada, err := s.acharOuCriarColecao(ctx, source, sourceID, col.Name)
	if err != nil {
		return "", false, err
	}
	s.cachearArtes(ctx, colID, map[string]any{
		"poster_url":   derefStr(col.PosterURL),
		"backdrop_url": derefStr(col.BackdropURL),
	})
	s.ligarMembros(ctx, colID, source, col.Parts, langs)
	return colID, criada, nil
}

// ligarMembros imports each member movie (find-or-create + enrich) and links it to the
// collection through entity_relation(kind='collection'). It is idempotent: it loads the
// already-linked ones and skips those that exist (the UNIQUE still protects against a
// race). The failure of ONE part degrades (logs and continues) — the partial collection
// is valid.
func (s *Servico) ligarMembros(ctx context.Context, colID, source string, parts []provider.ResultadoExterno, langs []string) {
	ligados := map[string]bool{}
	if rels, err := s.leitura().ListarRelacoesFilhas(ctx, colID); err == nil {
		for _, r := range rels {
			if r.Kind == relacaoColecao {
				ligados[r.ChildID] = true
			}
		}
	}
	for _, p := range parts {
		filmeID, _, err := s.Importar(ctx, source, p.SourceID, p.Type, langs)
		if err != nil {
			log.Printf("enrich: importar parte %s da colecao falhou (segue): %v", p.SourceID, err)
			continue
		}
		if ligados[filmeID] {
			continue
		}
		if _, err := s.escrita().CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
			ID: s.novoID(), ParentID: colID, ChildID: filmeID, Kind: relacaoColecao,
		}); err != nil {
			log.Printf("enrich: ligar parte %s a colecao %s falhou (segue): %v", filmeID, colID, err)
			continue
		}
		ligados[filmeID] = true
	}
}

// langPrincipal returns the first (primary) language of the list, or "" when it is
// empty — the collection is pragmatically single-language, so it is searched only in the
// primary language.
func langPrincipal(langs []string) string {
	if len(langs) > 0 {
		return langs[0]
	}
	return ""
}

// acharOuCriarColecao returns the id of the collection entity: it reuses the existing
// one (dedup by tmdb id + type='collection') or creates a new `type='collection'` with
// the name of the set and the external identity {source: sourceID}. `criada` says whether
// it was born now (for the 201).
func (s *Servico) acharOuCriarColecao(ctx context.Context, source, sourceID, nome string) (string, bool, error) {
	if source == "tmdb" {
		existente, err := s.leitura().ObterColecaoPorTmdb(ctx, nsValid(sourceID))
		if err == nil {
			return existente.ID, false, nil
		}
		if !isNoRows(err) {
			return "", false, err
		}
	}
	titulo := nome
	if titulo == "" {
		titulo = sourceID
	}
	extIDs, err := json.Marshal(map[string]string{source: sourceID})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              novo,
		Type:            tipoColecao,
		Title:           titulo,
		ExternalIdsJson: nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	return novo, true, nil
}

// acharPorIDExterno looks for an entity by its id at the source (MD4 dedup), choosing
// the query by SOURCE: "tmdb"→external_id_tmdb, "tvdb"→external_id_tvdb (indexed
// generated columns). Sources without a column of their own do not deduplicate yet
// (find-or-create always creates) — that is the current scope. NoRows is not an error (it
// does not exist yet).
func (s *Servico) acharPorIDExterno(ctx context.Context, source, sourceID string) (string, bool, error) {
	var ent sqlcgen.Entity
	var err error
	switch source {
	case "tmdb":
		ent, err = s.leitura().ObterEntityPorTmdb(ctx, nsValid(sourceID))
	case "tvdb":
		ent, err = s.leitura().ObterEntityPorTvdb(ctx, nsValid(sourceID))
	default:
		return "", false, nil
	}
	if err != nil {
		if isNoRows(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return ent.ID, true, nil
}

// persistir writes the external base (UPSERT of the raw payload by entity_id+source) and
// triggers the poster cache. The image is best-effort: its failure is logged and does NOT
// abort the enrich (the item stays without a poster, the import is partial but valid).
func (s *Servico) persistir(ctx context.Context, entityID, source, sourceID string, raw json.RawMessage) error {
	if _, err := s.escrita().UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID:          s.novoID(),
		EntityID:    entityID,
		Source:      source,
		SourceID:    nsValid(sourceID),
		PayloadJson: nsValid(string(raw)),
	}); err != nil {
		return err
	}

	prov, ok := s.provFonte(source)
	if !ok {
		log.Printf("enrich: fonte %q sem provider ao persistir %s (segue sem poster)", source, entityID)
		return nil
	}
	campos, err := prov.MapearDetalhe(raw)
	if err != nil {
		log.Printf("enrich: mapear detalhe de %s falhou (segue sem poster): %v", entityID, err)
		return nil
	}
	// Writes the anchor of the fallback: original_language from the detail. It is a
	// fact of the source (not data from the owner), so re-enriching only reaffirms it.
	if ol, ok := campos["original_language"].(string); ok && ol != "" {
		if err := s.escrita().AtualizarOriginalLanguage(ctx, sqlcgen.AtualizarOriginalLanguageParams{
			OriginalLanguage: nsValid(ol),
			ID:               entityID,
		}); err != nil {
			return err
		}
	}
	// Denormalizes the neutral release_date into the base column (same treatment as
	// original_language): a fact of the source, it makes sorting/listing by year cheap (e.g.:
	// members of a collection) without resolving every item. It does not change the
	// resolution (base=external).
	if rd, ok := campos["release_date"].(string); ok && rd != "" {
		if err := s.escrita().AtualizarReleaseDate(ctx, sqlcgen.AtualizarReleaseDateParams{
			ReleaseDate: nsValid(rd),
			ID:          entityID,
		}); err != nil {
			return err
		}
	}
	s.cachearArtes(ctx, entityID, campos)
	// Cast/crew/studios (MD6): a re-composable cache populated from the canonical,
	// idempotent by (entity, source). Only the MOVIE and the SERIES node reach here
	// (season/episode do not go through persistir); the series cast is the aggregate of the
	// whole series.
	s.persistirCreditos(ctx, entityID, source, campos)
	return nil
}

// cachearArtes caches locally the poster and the BACKGROUND (fanart/backdrop) of the
// canonical, each one best-effort: the rich detail screen serves both from disk, without
// depending on the source at runtime. A failure in one only degrades (without that
// artwork), it never aborts the enrich.
func (s *Servico) cachearArtes(ctx context.Context, entityID string, campos map[string]any) {
	if u, ok := campos["poster_url"].(string); ok && u != "" {
		if err := s.cachearArte(ctx, entityID, u, kindPoster, "poster"); err != nil {
			log.Printf("enrich: cache do poster de %s falhou (segue sem poster): %v", entityID, err)
		}
	}
	if u, ok := campos["backdrop_url"].(string); ok && u != "" {
		if err := s.cachearArte(ctx, entityID, u, kindFanart, "fanart"); err != nil {
			log.Printf("enrich: cache do backdrop de %s falhou (segue sem fundo): %v", entityID, err)
		}
	}
}

// mesclarExternalIds reads the current external_ids, joins {key: sourceID} and writes it
// — the identity is authoritative (MD4). It preserves ids from other sources; it
// overwrites the one of its own source (re-import under a corrected id). Non-string values
// of the legacy JSON are discarded (the model is string→string). The key is normally the
// `source` itself (tmdb/imdb/tvdb); chaveExternalID diverts to "mbid" when
// source=="musicbrainz" (see musicext.ChaveExternalIDMbid).
func (s *Servico) mesclarExternalIds(ctx context.Context, entityID, source, sourceID string) error {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return err
	}
	ids := map[string]string{}
	if ent.ExternalIdsJson.Valid && ent.ExternalIdsJson.String != "" {
		bruto := map[string]any{}
		if json.Unmarshal([]byte(ent.ExternalIdsJson.String), &bruto) == nil {
			for k, v := range bruto {
				if str, ok := v.(string); ok {
					ids[k] = str
				}
			}
		}
	}
	ids[chaveExternalID(source)] = sourceID
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if err := s.escrita().AtualizarExternalIds(ctx, sqlcgen.AtualizarExternalIdsParams{
		ExternalIdsJson: nsValid(string(b)),
		ID:              entityID,
	}); err != nil {
		return err
	}
	// Retroactive backfill: the identity has already been written successfully --
	// linking orphan events is best-effort, the same degradation treatment already used
	// in cachearArte/ligarMembros of this file.
	if _, err := historico.VincularOrfaos(ctx, s.escrita(), entityID, ids); err != nil {
		log.Printf("enrich: backfill de eventos orfaos para %s falhou (segue): %v", entityID, err)
	}
	return nil
}

// chaveExternalID maps the name of the SOURCE (Provider.Nome(), e.g.: "tmdb"/"imdb"/
// "tvdb"/"musicbrainz") to the key written in external_ids_json. Identity for every
// provider, EXCEPT musicbrainz — whose id always lives under "mbid"
// (musicext.ChaveExternalIDMbid), the only convention the rest of the code
// (idsExternosDe, ObterEntityPorMbid/generated column external_id_mbid) understands.
// Without this diversion, the generic external-application path (auto-match/
// retroactive, "Corrigir", "Confirmar candidato") would write under "musicbrainz" —
// a key that is never read anywhere in the system.
func chaveExternalID(source string) string {
	if source == musicext.FonteMusicBrainz {
		return musicext.ChaveExternalIDMbid
	}
	return source
}

// AplicarIdentidadeExclusiva writes {source: sourceID} as the entity's ONLY external
// identity, dropping the ids of every other source.
//
// It exists for a CORRECTION ("Corrigir"/"Confirmar candidato"/auto-match applying a new
// identity): mesclarExternalIds preserves the other keys, which is right for an
// enrichment that COMPLETES an identity and wrong for one that REPLACES it. Measured: a
// series wrongly matched to "White Collar Blue" (2002) kept `tvdb: 82010` -- the id of
// the OTHER show -- after being corrected to the right TMDB id, so the NFO export would
// point Jellyfin at a different series. Any cross-id the enrichment can resolve
// authoritatively is written back right after this call (mesclarExternalIdSeAusente).
func (s *Servico) AplicarIdentidadeExclusiva(ctx context.Context, entityID, source, sourceID string) error {
	ids := map[string]string{chaveExternalID(source): sourceID}
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.escrita().AtualizarExternalIds(ctx, sqlcgen.AtualizarExternalIdsParams{
		ExternalIdsJson: nsValid(string(b)),
		ID:              entityID,
	})
}

// mesclarExternalIdSeAusente merges {key: value} into entity.external_ids_json ONLY when
// the key is not present yet (or is empty) — unlike mesclarExternalIds (which ALWAYS
// overwrites its own source, authoritative), this one is for an auxiliary CROSS-ID that
// must never clobber an authoritative value already written by another route (e.g.: the
// TVDB tree itself, which writes "tvdb" through mesclarExternalIds when IT is the
// base source). It preserves the other keys. value=="" is a no-op. A read/write error is
// propagated for the caller to degrade.
func (s *Servico) mesclarExternalIdSeAusente(ctx context.Context, entityID, chave, valor string) error {
	if valor == "" {
		return nil
	}
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return err
	}
	ids := map[string]string{}
	if ent.ExternalIdsJson.Valid && ent.ExternalIdsJson.String != "" {
		bruto := map[string]any{}
		if json.Unmarshal([]byte(ent.ExternalIdsJson.String), &bruto) == nil {
			for k, v := range bruto {
				if str, ok := v.(string); ok {
					ids[k] = str
				}
			}
		}
	}
	if ids[chave] != "" {
		return nil // it already has an authoritative value -- does not overwrite (non-destructive)
	}
	ids[chave] = valor
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if err := s.escrita().AtualizarExternalIds(ctx, sqlcgen.AtualizarExternalIdsParams{
		ExternalIdsJson: nsValid(string(b)),
		ID:              entityID,
	}); err != nil {
		return err
	}
	// Retroactive backfill: only the source that actually changed here (not the whole
	// map -- avoids redundant work already covered by another route when the other
	// sources did not change). Best-effort, same pattern as mesclarExternalIds.
	if _, err := historico.VincularOrfaos(ctx, s.escrita(), entityID, map[string]string{chave: valor}); err != nil {
		log.Printf("enrich: backfill de eventos orfaos (chave %s) para %s falhou (segue): %v", chave, entityID, err)
	}
	return nil
}

// cachearArte downloads the binary of an artwork (poster or fanart/backdrop), writes it
// to {assetsDir}/{entityID}/{nomeBase}.<ext> and creates (or reuses, dedup) the asset with
// origin='external' and the given `kind`. It never matches/deletes artwork with
// origin='user' (the lookup filters by origin). The error is returned to the caller only
// to LOG (degrade), never to abort the enrich.
func (s *Servico) cachearArte(ctx context.Context, entityID, url, kind, nomeBase string) error {
	dados, contentType, err := s.baixar(ctx, url)
	if err != nil {
		return err
	}

	dir := filepath.Join(s.assetsDir, entityID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	destino := filepath.Join(dir, nomeBase+extensaoImagem(contentType, url))
	if err := os.WriteFile(destino, dados, 0o644); err != nil {
		return err
	}
	return s.registrarAsset(ctx, entityID, destino, kind)
}

// registrarAsset does the DEDUP of the external asset of that `kind`: if one already
// exists (same entity, kind, origin='external'), it updates its path (removing the old
// binary when the extension changed) instead of duplicating; otherwise it creates a new one.
func (s *Servico) registrarAsset(ctx context.Context, entityID, destino, kind string) error {
	existente, err := s.leitura().ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: entityID,
		Kind:     kind,
		Origin:   originExternal,
	})
	if err != nil {
		if isNoRows(err) {
			_, cerr := s.escrita().CriarAsset(ctx, sqlcgen.CriarAssetParams{
				ID:        s.novoID(),
				EntityID:  entityID,
				Kind:      kind,
				Origin:    originExternal,
				PathOrUri: destino,
			})
			return cerr
		}
		return err
	}
	if existente.PathOrUri != destino {
		_ = os.Remove(existente.PathOrUri) // best-effort: the old binary goes away (different ext)
	}
	return s.escrita().AtualizarAssetPath(ctx, sqlcgen.AtualizarAssetPathParams{
		PathOrUri: destino,
		MetaJson:  existente.MetaJson,
		ID:        existente.ID,
	})
}

// baixar does the GET of the poster with a timeout and a size ceiling. It returns the
// bytes and the content-type. Status != 200 or I/O is an error (the caller degrades).
func (s *Servico) baixar(ctx context.Context, posterURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, posterURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.httpImagem.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("enrich: status %d ao baixar poster", resp.StatusCode)
	}
	dados, err := io.ReadAll(io.LimitReader(resp.Body, tamanhoMaxImagem))
	if err != nil {
		return nil, "", err
	}
	return dados, resp.Header.Get("Content-Type"), nil
}

// tituloInicial extracts the title from the raw detail for the newly created item
// (mapped by the provider of the SOURCE); it falls back to the sourceID when the detail
// carries no title (poor item) or the source has no provider — never empty (title is NOT
// NULL).
func (s *Servico) tituloInicial(source string, raw json.RawMessage, sourceID string, langs []string) string {
	if prov, ok := s.provFonte(source); ok {
		if campos, err := prov.MapearDetalhe(raw); err == nil {
			if t := tituloLegivel(campos, langs); t != "" {
				return t
			}
		}
	}
	return sourceID
}

// curarTitulo AUTO-HEALS the "number instead of the name" in items imported BEFORE the
// tituloInicial fix: if the base title is still exactly the numeric fallback (== the
// sourceID written at creation), it recomputes it from the detail (primary language). The
// condition is STRICT (== sourceID) on purpose -- a readable title, even if genuinely
// numeric ("1917"), never matches the source id, so it is never overwritten. It does not
// touch override/external (the resolution still gives authority to the owner). It degrades
// silently: any stumble here keeps the current title (the healing is best-effort).
func (s *Servico) curarTitulo(ctx context.Context, entityID, source, sourceID string, raw json.RawMessage, langs []string) error {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return err
	}
	if ent.Title != sourceID {
		return nil // title is already readable -- nothing to heal
	}
	prov, ok := s.provFonte(source)
	if !ok {
		return nil
	}
	campos, err := prov.MapearDetalhe(raw)
	if err != nil {
		return nil
	}
	novo := tituloLegivel(campos, langs)
	if novo == "" || novo == ent.Title {
		return nil
	}
	return s.escrita().AtualizarTitulo(ctx, sqlcgen.AtualizarTituloParams{Title: novo, ID: entityID})
}

// tituloLegivel extracts a displayable title from the provider's canonical map, tolerating
// the "title" field both as a SCALAR (legacy payload, single-language) and as a
// map[language]value (the multi-language envelope). It prefers the title in one of the
// allowed languages (in order -- langs[0] is the primary one), falls back to
// original_title and, finally, to any title present. It returns "" only when there is
// no title at all -- and then the caller uses the sourceID.
//
// Without this tolerance, the old assertion `campos["title"].(string)` failed for
// EVERY item imported after the multi-language model (title became a map), writing the
// numeric source id as entity.title -- the "number instead of the name" in the list.
func tituloLegivel(campos map[string]any, langs []string) string {
	switch t := campos["title"].(type) {
	case string:
		return t
	case map[string]any:
		return tituloDoMapa(t, comoString(campos["original_title"]), langs)
	}
	return ""
}

// tituloDoMapa picks a title from the language->value map: the first requested language
// (in order) that has a value, then the original_title, then any title present. "" if the
// map has none.
func tituloDoMapa(porIdioma map[string]any, original string, langs []string) string {
	for _, l := range langs {
		if v := comoString(porIdioma[l]); v != "" {
			return v
		}
	}
	if original != "" {
		return original
	}
	for _, v := range porIdioma {
		if s := comoString(v); s != "" {
			return s
		}
	}
	return ""
}

// comoString returns the value as a string when it is a string; "" otherwise (including
// nil). It trims the type assertions of the canonical titles.
func comoString(v any) string {
	s, _ := v.(string)
	return s
}

// escrita/leitura return the Queries bound to the write/read pools (ADR-0010).
func (s *Servico) escrita() *sqlcgen.Queries { return sqlcgen.New(s.database.Write) }
func (s *Servico) leitura() *sqlcgen.Queries { return sqlcgen.New(s.database.Read) }

// extensaoImagem chooses the file extension of the poster: first by the content-type
// (what the source declared), then by the extension of the URL; default .jpg.
func extensaoImagem(contentType, u string) string {
	switch {
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return ".jpg"
	}
	switch strings.ToLower(filepath.Ext(u)) {
	case ".png":
		return ".png"
	case ".webp":
		return ".webp"
	case ".jpg", ".jpeg":
		return ".jpg"
	}
	return ".jpg"
}

// nsValid wraps a string in a valid (non-NULL) sql.NullString.
func nsValid(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// derefStr dereferences a *string, returning "" when nil — to build the canonical map
// that cachearArtes consumes (it ignores an empty key).
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// isNoRows tells whether the error is sql.ErrNoRows (missing row — not a real failure).
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
