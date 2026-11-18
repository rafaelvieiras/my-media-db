package enrich

// serie.go is the SERIES import branch of enrich (ADR-0021):
// it materializes the series→season→episode TREE from a provider.SerieProvider.
// The series is an ordinary item (reuses importarItem for detail/poster/curarTitulo) plus the
// core.tv@1 namespace (season types); the seasons (only those of the default ordering) and
// the episodes (fine grain, anchored on episode.id) are born with their own extension_data
// and are linked through entity_relation with sort_index. It is idempotent: re-importing
// reuses entities and relations (season dedup by number, episode dedup by external id).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
	"github.com/rafaelvieiras/my-media-db/internal/tvmatch"
)

// Sentinel errors of ImportarSeriePorNome: the series scanner distinguishes them to
// route the file to the review queue WITHOUT guessing (it never imports the wrong series
// nor fabricates an episode).
var (
	// ErrSerieNaoEncontrada: no provider returned a candidate for the name. -> review.
	ErrSerieNaoEncontrada = errors.New("enrich: serie nao encontrada no provider")
	// ErrSerieAmbigua: there were candidates, but no EXACT and UNIQUE title match
	// (0 exact with candidates present, or 2+ exact). -> review.
	ErrSerieAmbigua = errors.New("enrich: serie ambigua (sem match de titulo exato e unico)")
)

const (
	// tipoSerie/tipoTemporada/tipoEpisodio are the entity.type of the tree (ADR-0021).
	tipoSerie     = "tv"
	tipoTemporada = "season"
	tipoEpisodio  = "episode"
	// relacaoTemporada/relacaoEpisodio are the entity_relation.kind of the tree.
	relacaoTemporada = "tv_season"
	relacaoEpisodio  = "tv_episode"
	// nsTV is the extension_data namespace of the series vertical (core.tv@1).
	nsTV = "core.tv@1"
)

// arvoreDaSerie validates the provider capability and fetches the raw tree (seasons +
// episodes) from the source -- without touching any entity. Extracted from importarSerie
// so that it can be reused by Enriquecer: there the entity already exists and only the
// tree needs to be fetched/applied; here, POST /import fetches BEFORE creating the series
// (fail fast without leaving an orphan entity when the source does not support series).
func (s *Servico) arvoreDaSerie(ctx context.Context, source, sourceID string, langs []string) (provider.SerieArvore, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return provider.SerieArvore{}, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	sp, ok := prov.(provider.SerieProvider)
	if !ok {
		return provider.SerieArvore{}, fmt.Errorf("enrich: fonte %q nao suporta serie", source)
	}
	return sp.BuscarSerieArvore(ctx, sourceID, langs)
}

// aplicarArvoreSerie writes the ALREADY FETCHED tree into a series that ALREADY EXISTS
// (known serieID): the series extension_data, the rich cross enrichment (best-effort),
// each season/episode (find-or-create with refresh) and the artwork inventory. Only
// AtualizarExtensionData propagates an error (a real database write); the rest is
// best-effort, exactly as importarSerie already did.
func (s *Servico) aplicarArvoreSerie(ctx context.Context, serieID, source, sourceID string, arvore provider.SerieArvore, langs []string) error {
	if err := s.escrita().AtualizarExtensionData(ctx, sqlcgen.AtualizarExtensionDataParams{
		ExtensionDataJson: nsValid(extensaoSerie(arvore)),
		ID:                serieID,
	}); err != nil {
		return err
	}
	// RICH cross enrichment of the series node (route C; source-agnostic): the OTHER
	// provider available (the one that is not the tree's base source) fills the rich fields
	// (tagline/genres/rating/keywords/similar/status) + downloads the fanart. Best-effort:
	// a failure/absence degrades to tree-only (it never aborts the tree).
	s.enriquecerSerieRico(ctx, serieID, source, sourceID, langs)
	for _, t := range arvore.Temporadas {
		s.importarTemporada(ctx, serieID, source, arvore.DefaultSeasonType, t, langs)
	}
	// Artwork inventory: AFTER the tree, so that the season artworks (Season) find the
	// entity of the matching season. Best-effort (without an ArteProvider or id, it degrades
	// silently).
	s.enriquecerArtes(ctx, serieID, tipoSerie, langs)
	// Theme-music candidate (ADR-0025): same trigger as enriquecerArtes above -- it
	// depends only on the external_ids of the series (already merged by the tree/rich
	// enrichment), without needing the tree itself. Best-effort.
	s.enriquecerTemaMusical(ctx, serieID, tipoSerie, langs)
	// Re-matching of pending files: files that were already stuck on the SERIES
	// (scanned BEFORE the tree existed, with no tv_episode link) get the right link now
	// that the tree has just been synced -- without waiting for a new scan. LAST of all,
	// best-effort (it never propagates an error): the tree/identity have already been
	// written successfully when this step runs.
	s.recasarArquivosPendentes(ctx, serieID)
	return nil
}

// recasarArquivosPendentes closes the gap: the file->episode link
// (media_file_entity, kind=tv_episode) was only attempted INSIDE a scan, against the
// tree that already existed at that moment (internal/scanner/serie.go). When the
// scanner finds the series BEFORE it has an external identity, the file stays stuck
// directly on the series entity, with no link, until the NEXT full scan. This step
// attempts the same matching by NUMBER (internal/tvmatch, the scanner rule) against the
// freshly synced tree, at the SAME instant it comes into existence -- it is
// idempotent (it skips an already linked file) and never matches by force (without a
// match, the file stays pending, exactly as today).
func (s *Servico) recasarArquivosPendentes(ctx context.Context, serieID string) {
	arquivos, err := s.leitura().ListarMediaFilesPorEntity(ctx, serieID)
	if err != nil {
		log.Printf("enrich: listar arquivos da serie %s p/ recasamento falhou (segue): %v", serieID, err)
		return
	}
	var idx *tvmatch.Index // built on demand, only if there is a candidate to re-match
	for _, mf := range arquivos {
		if s.temVinculoEpisodio(ctx, mf.ID) {
			continue // already linked (previous scan or previous iteration of this loop)
		}
		ep, ok := naming.ParseEpisode(semExtensaoArquivo(filepath.Base(mf.Path)))
		if !ok {
			continue // episode pattern not recognized -- stays pending
		}
		if idx == nil {
			idx = s.carregarIndexSerie(ctx, serieID)
		}
		ids, ok := tvmatch.Casar(idx, ep)
		if !ok {
			continue // no match in the tree -- stays pending, as today
		}
		s.ligarArquivoAosEpisodios(ctx, mf.ID, ids)
	}
}

// temVinculoEpisodio tells whether the media_file already has some kind=tv_episode link
// in media_file_entity -- it guarantees the idempotence of the re-matching (it never
// reprocesses nor duplicates a file that a previous scan, or a previous iteration of
// this same loop, already linked). A read error degrades to "no link" (the worst case is
// trying to re-match again, which is idempotent because of the upsert in
// CriarVinculoEntidade).
func (s *Servico) temVinculoEpisodio(ctx context.Context, mediaFileID string) bool {
	vinculos, err := s.leitura().ListarVinculosPorArquivo(ctx, mediaFileID)
	if err != nil {
		log.Printf("enrich: listar vinculos do arquivo %s falhou (segue): %v", mediaFileID, err)
		return false
	}
	for _, v := range vinculos {
		if v.Kind == relacaoEpisodio {
			return true
		}
	}
	return false
}

// ligarArquivoAosEpisodios creates the media_file_entity link (kind=tv_episode) for each
// matched episode, with ordinal = position in the list -- the same layout as the scanner
// (an S01E01-E03 range becomes 3 rows, ordinal 0/1/2, under the same media_file). A
// failure degrades (logs and continues, one episode of the list does not bring down the
// others).
func (s *Servico) ligarArquivoAosEpisodios(ctx context.Context, mediaFileID string, episodioIDs []string) {
	for i, epID := range episodioIDs {
		if err := s.escrita().CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
			MediaFileID: mediaFileID,
			EntityID:    epID,
			Kind:        relacaoEpisodio,
			Ordinal:     int64(i),
		}); err != nil {
			log.Printf("enrich: vincular arquivo %s ao episodio %s falhou (segue): %v", mediaFileID, epID, err)
		}
	}
}

// carregarIndexSerie builds the number->episode_id index of the tree of a series
// (mirrors scanner.carregarIndex, the SAME read: ListarRelacoesFilhas + extension_data of
// each episode), now available in internal/enrich through internal/tvmatch. A read failure
// degrades to an empty index -- Casar simply finds nothing, the file stays pending (no
// forced matching).
func (s *Servico) carregarIndexSerie(ctx context.Context, serieID string) *tvmatch.Index {
	idx := tvmatch.NovoIndex()
	q := s.leitura()

	serie, err := q.ObterEntity(ctx, serieID)
	if err != nil {
		log.Printf("enrich: serie %s ilegivel ao montar indice de recasamento: %v", serieID, err)
		return idx
	}
	idx.PreencherOrdenacoes(tvext.Namespace(serie.ExtensionDataJson, nsTV))

	temps, err := q.ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		log.Printf("enrich: temporadas da serie %s ilegiveis ao montar indice de recasamento: %v", serieID, err)
		return idx
	}
	for _, t := range temps {
		if t.Kind != relacaoTemporada {
			continue
		}
		eps, err := q.ListarRelacoesFilhas(ctx, t.ChildID)
		if err != nil {
			continue // unreadable season: degrades without bringing down the rest of the index
		}
		for _, e := range eps {
			if e.Kind != relacaoEpisodio {
				continue
			}
			ep, err := q.ObterEntity(ctx, e.ChildID)
			if err != nil {
				continue
			}
			idx.IndexarEpisodio(ep.ID, tvext.Namespace(ep.ExtensionDataJson, nsTV))
		}
	}
	return idx
}

// semExtensaoArquivo removes the extension from the base name of a file before
// naming.ParseEpisode (which expects the name without an extension) -- it mirrors
// scanner.semExtensao, without depending on the scanner package (internal/enrich cannot
// import it, only the other way around).
func semExtensaoArquivo(nome string) string {
	return strings.TrimSuffix(nome, filepath.Ext(nome))
}

// mesclarTvdbCruzado tries to capture the tvdb_id from the ALREADY FETCHED series detail
// (raw, with no extra trip to the network) and merges it into entity.external_ids_json
// when the series does not have a tvdb of its own yet: it closes the gap that left every
// series whose tree comes ONLY from TMDB (with no TVDB configured, the provider
// independence) without a usable id for fanart.tv, which on TV requires tvdb_id
// exclusively (fanarttv.rotaETabela, case "tv"). It only runs when the base source
// `source` implements the optional capability provider.TvdbCruzadoProvider (today only the
// TMDB client, whose external_ids append already brings tvdb_id for free); with NO
// hardcode of "tmdb" -- a generic type-assert, like enriquecedorRico. The absence of the
// capability or no match in the payload degrades silently (best-effort). It never
// overwrites a tvdb already present (mesclarExternalIdSeAusente): when the tree comes from
// TVDB itself, mesclarExternalIds has already written the authoritative tvdb before this
// point, and this method never runs anyway (the TVDB client does not implement the
// capability).
func (s *Servico) mesclarTvdbCruzado(ctx context.Context, entityID, source string, raw json.RawMessage) {
	prov, ok := s.provFonte(source)
	if !ok {
		return
	}
	cruzado, ok := prov.(provider.TvdbCruzadoProvider)
	if !ok {
		return // base source does not expose the cross-id: silent degradation
	}
	tvdbID, ok := cruzado.TvdbIDDoDetalheSerie(raw)
	if !ok {
		return // no tvdb_id in the detail (TMDB-exclusive series, no match on TVDB)
	}
	if err := s.mesclarExternalIdSeAusente(ctx, entityID, "tvdb", tvdbID); err != nil {
		log.Printf("enrich: mesclar tvdb cruzado da serie %s (fonte %s) falhou (segue): %v", entityID, source, err)
	}
}

// DescartarArvoreSerie removes the WHOLE season/episode tree of a series (the relations
// and the child entities), keeping the series itself and its files.
//
// It exists for a manual identity change: the tree describes the PREVIOUS show, so
// re-importing over it leaves two episode sets inside the same seasons (measured: after
// correcting "White Collar" from "White Collar Blue", Temporada 1 held both the 22
// episodes of the wrong show -- titled "Episódio N", and holding every file -- and the 14
// real ones). Dedup by external id (ADR-0021) cannot merge them: they are different
// episodes of different shows.
//
// The FILES survive: media_file.entity_id points at the SERIES, not at the episode, and
// the episode link lives in media_file_entity, which goes away with the episode. The
// re-import that follows calls recasarArquivosPendentes, which re-links them by number
// against the new tree.
func (s *Servico) DescartarArvoreSerie(ctx context.Context, serieID string) error {
	q := s.escrita()
	temporadas, err := q.ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		return err
	}
	for _, temp := range temporadas {
		if temp.Kind != relacaoTemporada {
			continue
		}
		episodios, err := q.ListarRelacoesFilhas(ctx, temp.ChildID)
		if err != nil {
			return err
		}
		for _, ep := range episodios {
			if ep.Kind != relacaoEpisodio {
				continue
			}
			if err := q.ApagarEntityRelation(ctx, ep.ID); err != nil {
				return err
			}
			if err := q.ApagarEntity(ctx, ep.ChildID); err != nil {
				return err
			}
		}
		if err := q.ApagarEntityRelation(ctx, temp.ID); err != nil {
			return err
		}
		if err := q.ApagarEntity(ctx, temp.ChildID); err != nil {
			return err
		}
	}
	return nil
}

// sincronizarArvoreSerie combines the two steps above for a caller that only has
// (serieID, source, sourceID) -- the case of Enriquecer, which does not have a pre-fetched
// tree the way importarSerie does. The error covers both capability/network
// (arvoreDaSerie) and the extension_data write (aplicarArvoreSerie); the caller
// (Enriquecer) treats it as best-effort.
func (s *Servico) sincronizarArvoreSerie(ctx context.Context, serieID, source, sourceID string, langs []string) error {
	arvore, err := s.arvoreDaSerie(ctx, source, sourceID, langs)
	if err != nil {
		return err
	}
	return s.aplicarArvoreSerie(ctx, serieID, source, sourceID, arvore, langs)
}

// importarSerie imports the TREE of a series. It requires the optional SerieProvider
// capability (otherwise error → 502, like the collection). The series (type='tv') is
// born/updated through the item template (detail/poster/curarTitulo) and receives the
// core.tv@1 namespace; then each season of the default ordering and each fine-grained
// episode are imported. The failure of ONE season/episode degrades (logs and continues) —
// the partial tree is valid.
//
// The capability/tree fetch runs BEFORE resolving the series entity (the same order as
// always: fail fast without leaving an orphan entity when the source does not support
// series).
func (s *Servico) importarSerie(ctx context.Context, source, sourceID string, langs []string) (string, bool, error) {
	arvore, err := s.arvoreDaSerie(ctx, source, sourceID, langs)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar arvore da serie: %w", err)
	}
	serieID, criada, err := s.importarItemSemArvore(ctx, source, sourceID, tipoSerie, langs)
	if err != nil {
		return serieID, criada, err
	}
	if err := s.aplicarArvoreSerie(ctx, serieID, source, sourceID, arvore, langs); err != nil {
		return serieID, criada, err
	}
	return serieID, criada, nil
}

// enriquecerSerieRico ENRICHES the series node with the rich fields that the tree's
// base source does not model (route C; SOURCE-AGNOSTIC): it picks the FIRST OTHER
// provider registered with the SerieRicoProvider capability, crosses the series external
// id (the sourceID of the base source) into the rich source, fetches the rich canonical
// (only the rich keys) and PERSISTS it as a media_external layer of the rich source on the
// series node — which the layer composition (montarExterno) already composes for free on
// top of the base, reusing the movie machinery WITHOUT clobbering title/language. Then it
// caches the FANART (backdrop_url). Idempotent (the reuse REWRITES by (entity_id, source),
// matching the refresh behaviour of the reuse path). Everything is best-effort: with no
// other rich provider, no match or a network error it degrades silently to tree-only — it
// never aborts. With no hardcode of "tmdb"/"tvdb": the base and the rich sources are
// resolved by availability/capability in the registry.
func (s *Servico) enriquecerSerieRico(ctx context.Context, serieID, source, sourceID string, langs []string) {
	rico, ricoSource, ok := s.enriquecedorRico(source)
	if !ok {
		return // no other provider exposes the rich capability: only the tree of the base source
	}
	idExterno := idExternoDaSerie(source, sourceID)
	if idExterno == "" {
		return // no external id to cross
	}
	campos, ok, err := rico.BuscarSerieDetalheRico(ctx, idExterno, langs)
	if err != nil {
		log.Printf("enrich: serie-rico %s da serie %s (fonte-base %s id %s) falhou (segue so-arvore): %v", ricoSource, serieID, source, idExterno, err)
		return
	}
	if !ok || len(campos) == 0 {
		return // no match in the rich source: degrades (tree-only)
	}
	if err := s.persistirSerieRico(ctx, serieID, ricoSource, campos); err != nil {
		log.Printf("enrich: persistir serie-rico da serie %s falhou (segue): %v", serieID, err)
		return
	}
	// backdrop_url now exists in the series canonical: download the fanart (poster_url does
	// not come in the rich canonical — the poster still comes from the base source, already
	// cached in importarItem).
	s.cachearArtes(ctx, serieID, campos)
	// Cast/crew/studios (MD6) of the series node come from the RICH SOURCE (the TMDB
	// aggregate), under the rich source -- idempotent by (series, ricoSource). In the
	// TMDB-base case (no other provider) this branch does not run; the credits then come
	// from the persistir of the series node (base source), which already mapped the
	// aggregate_credits of /tv.
	s.persistirCreditos(ctx, serieID, ricoSource, campos)
}

// enriquecedorRico picks the provider for the RICH enrichment of the series node: the
// FIRST registered provider, DIFFERENT from the base source `base`, that implements
// SerieRicoProvider (respecting the order = registry priority). It replaces the old fixed
// Get("tmdb") — the tree can come from any source and the enricher is the other capable
// provider. (nil,"",false) when no other one has the capability: the series stays with the
// tree only (degrades).
func (s *Servico) enriquecedorRico(base string) (provider.SerieRicoProvider, string, bool) {
	for _, prov := range s.providers.All() {
		if prov.Nome() == base {
			continue // the base source does not enrich itself
		}
		if rico, ok := prov.(provider.SerieRicoProvider); ok {
			return rico, prov.Nome(), true
		}
	}
	return nil, "", false
}

// idExternoDaSerie returns the external id of the series at the BASE SOURCE of the tree
// (the sourceID of the import), which the rich-enrichment provider crosses into its own
// source. Generic (previously idTvdbDaSerie assumed source=="tvdb"): it does not presume
// which source is the base — any SerieProvider anchoring the tree exposes its id here, and
// the rich enricher resolves it.
func idExternoDaSerie(source, sourceID string) string {
	_ = source // the base source is generic; the id to cross is always the sourceID of the import
	return sourceID
}

// persistirSerieRico writes (or rewrites) the RICH canonical of the series as a
// media_external layer of the rich source `ricoSource`, wrapped in the
// ChaveEnvelopeSerieRico envelope that the MapearDetalhe of that source recognizes
// (passthrough). SourceID stays NULL: it is a crossed ENRICHMENT layer, not a primary
// import of the rich source (the series node is not deduplicated by its id — the anchor is
// the base source of the tree).
func (s *Servico) persistirSerieRico(ctx context.Context, serieID, ricoSource string, campos map[string]any) error {
	payload, err := json.Marshal(map[string]any{provider.ChaveEnvelopeSerieRico: campos})
	if err != nil {
		return err
	}
	_, err = s.escrita().UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID:          s.novoID(),
		EntityID:    serieID,
		Source:      ricoSource,
		SourceID:    sql.NullString{},
		PayloadJson: nsValid(string(payload)),
	})
	return err
}

// ImportarSeriePorNome fetches the series by NAME from the providers that support
// series, picks the candidate with an EXACT and UNIQUE TITLE (case-insensitive) and imports
// its tree (reusing importarSerie, idempotent). It is the reusable point that the series
// scanner calls when the series is not in the catalog yet.
//
// Choice (respects the source priority = Registry order, e.g.: [tmdb, tvdb]): it walks the
// providers in order; the FIRST one that returns exactly ONE exact title match wins and is
// imported. A provider that returns 2+ exact matches is ambiguous. If no provider gave an
// exact-unique match but there were candidates, it is ErrSerieAmbigua; if there was no
// candidate at all, it is ErrSerieNaoEncontrada. It NEVER guesses by partial title.
//
// preferredLang is the preferred language of the library_source that triggered the import
// (library.Source.PreferredLanguage), when defined -- used instead of the FIRST language of
// `langs` (langPrincipal) only for the name SEARCH below; `langs` remains the full list of
// allowed languages used in the import of the tree/detail (importarSeriePorFonte). Empty
// keeps the usual behaviour (langPrincipal(langs), the global language).
func (s *Servico) ImportarSeriePorNome(ctx context.Context, nome string, langs []string, preferredLang string) (string, error) {
	alvo := strings.TrimSpace(nome)
	if alvo == "" {
		return "", ErrSerieNaoEncontrada
	}
	lang := preferredLang
	if lang == "" {
		lang = langPrincipal(langs)
	}
	houveCandidato := false
	for _, prov := range s.providers.All() {
		if _, ok := prov.(provider.SerieProvider); !ok {
			continue
		}
		res, err := prov.Buscar(ctx, alvo, tipoSerie, lang)
		if err != nil {
			log.Printf("enrich: busca de serie %q na fonte %q falhou (segue): %v", alvo, prov.Nome(), err)
			continue
		}
		if len(res) > 0 {
			houveCandidato = true
		}
		exatos := exatosPorTitulo(res, alvo)
		switch len(exatos) {
		case 0:
			continue // this source has no exact match; try the next one (priority)
		case 1:
			return s.importarSeriePorFonte(ctx, prov.Nome(), exatos[0].SourceID, langs)
		default:
			return "", ErrSerieAmbigua // 2+ exact matches in the same source: do not guess
		}
	}
	if houveCandidato {
		return "", ErrSerieAmbigua // candidates existed, none exact-unique
	}
	return "", ErrSerieNaoEncontrada
}

// importarSeriePorFonte imports the tree of the chosen series (source, sourceID), reusing
// the importarSerie from Phase 1. It returns only the id (the `criada` flag is of no
// interest to the scanner: an idempotent call reuses the existing series).
func (s *Servico) importarSeriePorFonte(ctx context.Context, source, sourceID string, langs []string) (string, error) {
	id, _, err := s.importarSerie(ctx, source, sourceID, langs)
	return id, err
}

// exatosPorTitulo filters the results whose title matches EXACTLY (case-insensitive) the
// target name, deduplicated by SourceID (the same series repeated at the source counts
// once).
func exatosPorTitulo(res []provider.ResultadoExterno, alvo string) []provider.ResultadoExterno {
	vistos := map[string]bool{}
	var out []provider.ResultadoExterno
	for _, r := range res {
		if !strings.EqualFold(strings.TrimSpace(r.Title), alvo) {
			continue
		}
		if vistos[r.SourceID] {
			continue
		}
		vistos[r.SourceID] = true
		out = append(out, r)
	}
	return out
}

// importarTemporada creates (or reuses) the season, caches its poster, links it to the
// series and imports its episodes. Isolated from importarSerie to keep the gocyclo low.
// Failures degrade (logs and continues).
func (s *Servico) importarTemporada(ctx context.Context, serieID, source, defaultType string, t provider.TemporadaExterna, langs []string) {
	tempID, err := s.acharOuCriarTemporada(ctx, serieID, t, langs)
	if err != nil {
		log.Printf("enrich: temporada %d da serie %s falhou (segue): %v", t.SeasonNumber, serieID, err)
		return
	}
	s.cachearPosterTemporadaSeAusente(ctx, tempID, t.PosterURL)
	s.ligarFilho(ctx, serieID, tempID, relacaoTemporada, t.SeasonNumber)
	for _, ep := range t.Episodios {
		s.importarEpisodio(ctx, tempID, source, defaultType, ep, langs)
	}
}

// cachearPosterTemporadaSeAusente downloads the season poster ONLY when there is no
// external asset yet: on a re-import the whole tree is refreshed, but re-downloading
// artwork already present on every import would hammer the CDN with no gain (the poster
// rarely changes). Only-if-absent gives cheap idempotence; refreshing artwork that CHANGED
// is a follow-up (hash/force). A read error degrades (logs and continues); a download error
// likewise — the partial tree is still valid.
func (s *Servico) cachearPosterTemporadaSeAusente(ctx context.Context, tempID, posterURL string) {
	if posterURL == "" {
		return
	}
	_, err := s.leitura().ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: tempID,
		Kind:     kindPoster,
		Origin:   originExternal,
	})
	if err == nil {
		return // it already exists: do not re-download (only-if-absent)
	}
	if !isNoRows(err) {
		log.Printf("enrich: checar poster da temporada %s falhou (segue): %v", tempID, err)
		return
	}
	if err := s.cachearArte(ctx, tempID, posterURL, kindPoster, "poster"); err != nil {
		log.Printf("enrich: poster da temporada %s falhou (segue): %v", tempID, err)
	}
}

// acharOuCriarTemporada performs the IDEMPOTENT find-or-create of the season by
// (parent series, season_number): there is no source id in the struct, so the dedup is by
// number — it reuses an already linked tv_season child with the same sort_index. On reuse
// it REWRITES the base (title/year/extension_data) with the current provider data (the
// owner's override still wins in the resolution); otherwise it creates a type='season'
// entity. The fields come from camposTemporada, shared between create and update (so they
// cannot diverge).
func (s *Servico) acharOuCriarTemporada(ctx context.Context, serieID string, t provider.TemporadaExterna, langs []string) (string, error) {
	title, releaseDate, extData := camposTemporada(t, langs)
	if rels, err := s.leitura().ListarRelacoesFilhas(ctx, serieID); err == nil {
		for _, r := range rels {
			if r.Kind == relacaoTemporada && r.SortIndex.Valid && int(r.SortIndex.Int64) == t.SeasonNumber {
				if err := s.escrita().AtualizarTemporadaBase(ctx, sqlcgen.AtualizarTemporadaBaseParams{
					Title:             title,
					ReleaseDate:       releaseDate,
					ExtensionDataJson: extData,
					ID:                r.ChildID,
				}); err != nil {
					return "", err
				}
				return r.ChildID, nil
			}
		}
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoTemporada,
		Title:             title,
		ReleaseDate:       releaseDate,
		ExtensionDataJson: extData,
	}); err != nil {
		return "", err
	}
	return novo, nil
}

// camposTemporada builds the base fields of the season shared between create and the
// refresh of reuse: title (Name[default] or "Temporada N"), date (year) and the
// core.tv@1 extension_data. Centralizing avoids the refresh forgetting a field in the
// future.
func camposTemporada(t provider.TemporadaExterna, langs []string) (title string, releaseDate, extData sql.NullString) {
	return tituloTemporada(t, langs), nsOrNull(t.Year), nsValid(extensaoTemporada(t))
}

// importarEpisodio creates (or reuses) the episode and links it to the season with
// sort_index = episode number IN THE DEFAULT ORDERING. Isolated to keep the gocyclo low.
//
// The episode still is NOT downloaded on import: a series has hundreds/thousands of
// episodes (Looney Tunes ~1195), and downloading one still per episode synchronously in
// POST /import is slow and hostile to the CDN. The episode artwork (still) is left for
// Phase 3 (proxy/cache on demand), together with the season artwork. Here we only
// persist the episode and the ordered relation.
func (s *Servico) importarEpisodio(ctx context.Context, tempID, source, defaultType string, ep provider.EpisodioExterno, langs []string) {
	epID, err := s.acharOuCriarEpisodio(ctx, source, ep, langs)
	if err != nil {
		log.Printf("enrich: episodio %s falhou (segue): %v", ep.SourceID, err)
		return
	}
	s.ligarFilho(ctx, tempID, epID, relacaoEpisodio, ep.Numbering[defaultType].Number)
}

// acharOuCriarEpisodio performs the find-or-create of the episode by external id (dedup by
// external_id_<source>, the ANCHOR of ADR-0021 — never the number) and, THEN, writes the
// external base (media_external) with the titles/synopses PER LANGUAGE — the same i18n of
// the movie applied to the episode. It is idempotent: on reuse the base row is reused and
// the media_external is REWRITTEN (upsert by (entity_id, source)).
func (s *Servico) acharOuCriarEpisodio(ctx context.Context, source string, ep provider.EpisodioExterno, langs []string) (string, error) {
	epID, err := s.idDoEpisodio(ctx, source, ep, langs)
	if err != nil {
		return "", err
	}
	if err := s.persistirEpisodioExterno(ctx, epID, source, ep); err != nil {
		return "", err
	}
	return epID, nil
}

// idDoEpisodio returns the id of the episode entity: it reuses the existing one (dedup by
// external id) or creates a new one. SCALAR fields on the ROW (base/curated fallback,
// default language only; the per-language data lives in media_external): title,
// description, air date, external_ids ({source: episode.id} + the ones the source brought)
// and the core.tv@1 namespace (absolute_number + multi-order numbering).
func (s *Servico) idDoEpisodio(ctx context.Context, source string, ep provider.EpisodioExterno, langs []string) (string, error) {
	campos, err := montarCamposEpisodio(source, ep, langs)
	if err != nil {
		return "", err
	}
	if existente, ok, err := s.acharPorIDExterno(ctx, source, ep.SourceID, tipoEpisodio); err != nil {
		return "", err
	} else if ok {
		// Reuse: REWRITES the base with the current provider data — in
		// particular still_url/numbering (extension_data), which only the create used to write.
		// It does not touch status/tags/original_title/media_override: the owner's override
		// still wins.
		if err := s.escrita().AtualizarEpisodioBase(ctx, sqlcgen.AtualizarEpisodioBaseParams{
			Title:             campos.title,
			Description:       campos.description,
			ReleaseDate:       campos.releaseDate,
			ExternalIdsJson:   campos.externalIDs,
			ExtensionDataJson: campos.extensionData,
			ID:                existente,
		}); err != nil {
			return "", err
		}
		return existente, nil
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoEpisodio,
		Title:             campos.title,
		Description:       campos.description,
		ReleaseDate:       campos.releaseDate,
		ExternalIdsJson:   campos.externalIDs,
		ExtensionDataJson: campos.extensionData,
	}); err != nil {
		return "", err
	}
	return novo, nil
}

// camposEpisodio aggregates the base fields of the episode (row scalars) shared between the
// create and the refresh of reuse: title, synopsis (default language), air date,
// external_ids ({source: episode.id} + the ones of the source) and the core.tv@1
// extension_data (numbering + absolute_number + still_url). A single assembly point avoids
// the refresh diverging from the create (forgetting a new field in the future).
type camposEpisodio struct {
	title         string
	description   sql.NullString
	releaseDate   sql.NullString
	externalIDs   sql.NullString
	extensionData sql.NullString
}

// montarCamposEpisodio computes the camposEpisodio; it fails only on the external_ids marshal.
func montarCamposEpisodio(source string, ep provider.EpisodioExterno, langs []string) (camposEpisodio, error) {
	extIDs, err := json.Marshal(idsDoEpisodio(source, ep))
	if err != nil {
		return camposEpisodio{}, err
	}
	return camposEpisodio{
		title:         tituloEpisodio(ep, langs),
		description:   nsOrNull(valorPorIdioma(ep.Overview, langs)),
		releaseDate:   nsOrNull(ep.AirDate),
		externalIDs:   nsValid(string(extIDs)),
		extensionData: nsValid(extensaoEpisodio(ep)),
	}, nil
}

// persistirEpisodioExterno writes (or rewrites) the external base of the episode: a
// per-source media_external whose payload carries the raw PER-LANGUAGE title/overview maps
// (without discarding languages, unlike the row scalar). It mirrors the movie import
// (enrich.persistir → UpsertMediaExternal), but with the episode envelope (entity_kind
// discriminator) — that is what makes the episode detail translate through the resolver.
// Idempotent by (entity_id, source): the reuse REWRITES. original_language is not
// denormalized (the tree does not propagate it per episode; the chain works as
// [lang, fallback]).
func (s *Servico) persistirEpisodioExterno(ctx context.Context, epID, source string, ep provider.EpisodioExterno) error {
	payload, err := provider.MontarEnvelopeEpisodio(ep, "")
	if err != nil {
		return err
	}
	_, err = s.escrita().UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID:          s.novoID(),
		EntityID:    epID,
		Source:      source,
		SourceID:    nsValid(ep.SourceID),
		PayloadJson: nsValid(string(payload)),
	})
	return err
}

// ligarFilho creates the parent→child relation IDEMPOTENTLY: it loads the already linked
// ones and skips when the pair (child, kind) already exists (the
// UNIQUE(parent,child,kind) still protects against a race). sort_index carries the stable
// number of the child (season within the series, episode within the season) so that
// ListarRelacoesFilhas can order. A failure degrades (logs).
func (s *Servico) ligarFilho(ctx context.Context, parentID, childID, kind string, sortIndex int) {
	if rels, err := s.leitura().ListarRelacoesFilhas(ctx, parentID); err == nil {
		for _, r := range rels {
			if r.Kind == kind && r.ChildID == childID {
				return
			}
		}
	}
	if _, err := s.escrita().CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID:        s.novoID(),
		ParentID:  parentID,
		ChildID:   childID,
		Kind:      kind,
		SortIndex: sql.NullInt64{Int64: int64(sortIndex), Valid: true},
	}); err != nil {
		log.Printf("enrich: ligar %s->%s (%s) falhou (segue): %v", parentID, childID, kind, err)
	}
}

// --- assembly of extension_data (core.tv@1 namespace) ---

// extensaoSerie builds the series extension_data: default_season_type + season_types.
func extensaoSerie(a provider.SerieArvore) string {
	return envelopeTV(map[string]any{
		"default_season_type": a.DefaultSeasonType,
		"season_types":        a.SeasonTypes,
	})
}

// extensaoTemporada builds the season extension_data: ordering + season_number
// (+ year when present).
func extensaoTemporada(t provider.TemporadaExterna) string {
	payload := map[string]any{
		"ordering":      t.Ordering,
		"season_number": t.SeasonNumber,
	}
	if t.Year != "" {
		payload["year"] = t.Year
	}
	return envelopeTV(payload)
}

// extensaoEpisodio builds the episode extension_data: numbering (ALL the orders the
// series uses) + absolute_number when the source reported it + still_url when the provider
// brought a frame. The still is language-NEUTRAL artwork (like the numbering): only the raw
// URL is persisted — the image is fetched/cached ON DEMAND by the image proxy on the first
// view, never downloaded on import (a series has hundreds/thousands of eps).
func extensaoEpisodio(ep provider.EpisodioExterno) string {
	payload := map[string]any{
		"numbering": numberingMap(ep.Numbering),
	}
	if ep.AbsoluteNumber != nil {
		payload["absolute_number"] = *ep.AbsoluteNumber
	}
	if ep.StillURL != "" {
		payload["still_url"] = ep.StillURL
	}
	return envelopeTV(payload)
}

// numberingMap converts the canonical Numbering (order→Numero) into the JSON object
// order→{season,number} that the extension_data stores.
func numberingMap(n map[string]provider.Numero) map[string]any {
	out := make(map[string]any, len(n))
	for ordem, num := range n {
		out[ordem] = map[string]any{"season": num.Season, "number": num.Number}
	}
	return out
}

// envelopeTV wraps the payload in the core.tv@1 namespace and serializes it. A marshal
// error (unreachable for these simple maps) degrades to "{}".
func envelopeTV(payload map[string]any) string {
	b, err := json.Marshal(map[string]any{nsTV: payload})
	if err != nil {
		return "{}"
	}
	return string(b)
}

// --- pure field helpers ---

// tituloTemporada is Name[default language] or the "Temporada N" fallback (seasons rarely
// have a name). N is the season_number (even when it is a calendar year, "Temporada N"
// still works as a label).
func tituloTemporada(t provider.TemporadaExterna, langs []string) string {
	if nome := valorPorIdioma(t.Name, langs); nome != "" {
		return nome
	}
	return fmt.Sprintf("Temporada %d", t.SeasonNumber)
}

// tituloEpisodio is Title[default language]; it falls back to the SourceID when there is no
// title (title is NOT NULL — never empty), mirroring the tituloInicial of an item.
func tituloEpisodio(ep provider.EpisodioExterno, langs []string) string {
	if t := valorPorIdioma(ep.Title, langs); t != "" {
		return t
	}
	return ep.SourceID
}

// idsDoEpisodio builds the episode external_ids: {source: episode.id} (the dedup anchor by
// external_id_<source>) PLUS the ids the source brought (e.g.: tvdb). The source wins in a
// collision (it is the authoritative id of the import).
func idsDoEpisodio(source string, ep provider.EpisodioExterno) map[string]string {
	ids := map[string]string{}
	for k, v := range ep.ExternalIDs {
		if v != "" {
			ids[k] = v
		}
	}
	ids[source] = ep.SourceID
	return ids
}

// valorPorIdioma picks the value of the "default language": the first of `langs` with a
// value, otherwise any value present (in alphabetical order of language, for
// determinism). "" when the map has none.
func valorPorIdioma(m map[string]string, langs []string) string {
	for _, l := range langs {
		if v := m[l]; v != "" {
			return v
		}
	}
	chaves := make([]string, 0, len(m))
	for k := range m {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	for _, k := range chaves {
		if m[k] != "" {
			return m[k]
		}
	}
	return ""
}

// nsOrNull wraps a string in a valid sql.NullString, or NULL when empty (for optional
// columns: description/release_date of season/episode).
func nsOrNull(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
