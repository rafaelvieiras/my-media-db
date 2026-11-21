package scanner

// serie.go is the core of the SERIES scanner: when the library is
// `content_type='series'`, every video file is one (or more) episode(s). The flow,
// per file:
//
//  1. it derives the IDENTITY of the series from the name of the series FOLDER
//     (the folder above `Season NN/` when there is one);
//  2. it RESOLVES the entity of the series — embedded external id → catalog by
//     title → provider auto-import (enrich). Ambiguous/not found ⇒ review draft;
//  3. it loads the episode tree of the series ONCE (cache per series) and matches
//     the number of the file to the `episode_id` IN MEMORY (default ordering, with
//     a fallback to the others and to the absolute one);
//  4. it persists the media_file pointing to the SERIES and the N:M links per
//     episode (`media_file_entity`, kind='tv_episode'), ANCHORED on the id
//     (ADR-0021): changing the ordering later re-projects numbers without touching
//     any link.
//
// Everything that WRITES outside the junction (the auto-import, the review draft)
// runs in the facts phase, outside the write transaction of the batch — the enrich
// import uses its own write connection, so there is no nested transaction.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
	"github.com/rafaelvieiras/my-media-db/internal/tvmatch"
)

const (
	// contentTypeSeries is the value of library_source.content_type that turns on
	// the series flow (everything else — including the "movie" default — follows
	// the movie path).
	contentTypeSeries = "series"
	// tipoSerieTV is the entity.type of the series (it mirrors enrich.tipoSerie).
	tipoSerieTV = "tv"
	// kindRelTemporada/kindVinculoEpisodio: the kind of entity_relation
	// series→season and season→episode (enrich) and the kind of the
	// file↔episode link in the media_file_entity junction. The same "tv_episode"
	// label in both roles, on purpose.
	kindRelTemporada    = "tv_season"
	kindVinculoEpisodio = "tv_episode"
	// nsTV is the namespace of the extension_data of the series vertical (it
	// mirrors enrich.nsTV).
	nsTV = "core.tv@1"
	// Confidence of the series path (0–100 scale of confianca()): a number matched
	// without ambiguity ≈ 90; a series resolved by an embedded external id =
	// confIDExterno (100). Without a matched episode (review) it falls to
	// confRevisaoSerie, below the front threshold (70), which throws the media_file
	// into the review queue.
	confSerieEp      = 90
	confRevisaoSerie = 40
	// tipoEntTemporada/tipoEntEpisodio/statusRascunho: database values used only by
	// the provisional episode path.
	tipoEntTemporada = "season"
	tipoEntEpisodio  = "episode"
	statusRascunho   = "draft"
)

// serieResolvida is the result (cached per series in the scan) of resolving the
// entity of a series and loading its episode tree. emRevisao marks the series that
// could not be identified (ambiguous/not found) — a draft, without a tree.
type serieResolvida struct {
	serieID   string
	index     *tvmatch.Index
	conf      float64
	emRevisao bool
}

// factsSerie assembles the facts of a series file: it derives the folder/name of
// the series, resolves the entity of the series (cached) and matches the number of
// the file to the episodes. It does not write the media_file/link (that belongs to
// the batch transaction) — it only resolves and matches.
func (r *scanRun) factsSerie(ctx context.Context, path string) fileFacts {
	base := semExtensao(filepath.Base(path))
	f := fileFacts{path: path, isSerie: true, folderDir: filepath.Dir(path)}

	serieFolder, serieNome, folderInfo := derivarSerie(r.source.Path, path)
	f.rawNome = serieNome

	res := r.resolverSerie(ctx, serieFolder, serieNome, folderInfo)
	f.serieID = res.serieID
	f.serieConf = res.conf
	if res.serieID == "" {
		return f // the resolution failed completely (no entity): the writer skips the file
	}

	ep, ok := naming.ParseEpisode(base)
	if !ok {
		// Fallback that Kodi and Jellyfin both accept: a file named only by its
		// episode number inside a season folder takes the SEASON from the folder.
		ep, ok = episodioPelaPasta(path, base)
	}
	if !ok {
		log.Printf("scanner: %q sem padrão de episódio reconhecível; vai para revisão", path)
		if !res.emRevisao && res.index != nil {
			// Without SxxEyy in the name, but the folder may tell the season: the
			// file still becomes a provisional episode.
			f.prov = provisorioDe(path, base)
			f.serieConf = res.conf
		}
		return f
	}
	if res.emRevisao || res.index == nil {
		return f // series under review: the media_file stays pending, without a link
	}
	ids, temporadaResolvida, matched := tvmatch.CasarComPasta(res.index, ep, temporadaDaPasta(path), base)
	if matched && ep.IsAbsolute && temporadaResolvida != 0 {
		// The absolute number was placed by an ORDERING of the series. When the season it lands
		// in is not the folder's, the file stays where it is and the NFO carries the season the
		// ordering gives — Kodi/Jellyfin read the NFO, so the display is right; the disagreement
		// is reported here so it is not mistaken for a silent swap.
		if pasta := temporadaDaPasta(path); pasta != 0 && pasta != temporadaResolvida {
			log.Printf("scanner: %q tem número absoluto %v e está na temporada %d da pasta, mas a ordem da série o coloca na temporada %d", path, ep.Absolute, pasta, temporadaResolvida)
		}
	}
	if !matched {
		log.Printf("scanner: episódio de %q não casou na árvore externa; criando provisório: %+v", path, ep)
		f.prov = provisorioDe(path, base)
		f.serieConf = res.conf
		return f
	}
	f.episodeIDs = ids
	return f
}

// derivarSerie decides the folder and the name of the series of a file. If the
// file is in a season folder ("Season 01"), the series is the folder ABOVE it;
// otherwise it is the immediate folder. The name comes from naming.Parse (clean
// title + embedded external id). A file loose at the root falls back to its own
// file name (a poorly identified series → review).
func derivarSerie(root, path string) (serieFolder, serieNome string, info naming.Info) {
	dir := filepath.Dir(path)
	base := filepath.Base(dir)
	if dir != root && naming.EhPastaTemporada(base) {
		if parent := filepath.Dir(dir); parent != "." && parent != string(filepath.Separator) {
			serieFolder = parent
		} else {
			serieFolder = dir
		}
	} else {
		serieFolder = dir
	}

	nomeBase := filepath.Base(serieFolder)
	if serieFolder == root {
		nomeBase = semExtensao(filepath.Base(path))
	}
	info = naming.Parse(nomeBase)
	serieNome = info.Title
	if serieNome == "" {
		serieNome = nomeBase
	}
	return serieFolder, serieNome, info
}

// resolverSerie returns the resolution of the series, from the scan cache when
// already seen. Key = folder of the series, so that every file of that series
// reuses the same entity and the same index without re-fetching/re-importing.
func (r *scanRun) resolverSerie(ctx context.Context, serieFolder, serieNome string, folderInfo naming.Info) serieResolvida {
	if v, ok := r.serieCache[serieFolder]; ok {
		return v
	}
	res := r.resolverSerieMiss(ctx, serieFolder, serieNome, folderInfo)
	r.serieCache[serieFolder] = res
	return res
}

// resolverSerieMiss resolves a series missing from the cache. Order: embedded
// external id (type=tv) → catalog by title (type=tv) → auto-import by name.
// Success loads the episode index. Ambiguous/not found/import off → review draft
// (find-or-create by title), WITHOUT a tree.
func (r *scanRun) resolverSerieMiss(ctx context.Context, serieFolder, serieNome string, folderInfo naming.Info) serieResolvida {
	q := sqlcgen.New(r.m.db.Read)

	if id, ok := acharSeriePorIDExterno(ctx, q, folderInfo); ok {
		return serieResolvida{serieID: id, index: r.m.carregarIndex(ctx, id), conf: confIDExterno}
	}
	// The folder NAMES the item by id and the catalog has no entity for it yet: import by
	// THAT id. This is the door that makes a typo in the folder name harmless -- the title
	// search is never asked, so "2 Broken Girls [tmdbid-39340]" resolves to the right
	// series even though /search/multi answers zero for the misspelled name.
	if res, ok := r.m.tentarImportarSeriePorID(ctx, folderInfo, r.langsPermitidos(ctx)); ok {
		return res
	}
	if id, ok := acharSerieResolvidaPorTitulo(ctx, q, serieNome); ok {
		return serieResolvida{serieID: id, index: r.m.carregarIndex(ctx, id), conf: confSerieEp}
	}

	alvo := enrich.AlvoSerie{Nome: serieNome, Ano: folderInfo.Year, Arquivos: r.arquivosNaSerie(serieFolder)}
	if res, ok := r.m.tentarImportarSerie(ctx, alvo, r.langsPermitidos(ctx), r.preferredLangDaFonte()); ok {
		return res
	}

	// The name found nothing anywhere. The item goes to review as before; the "did you mean
	// this?" hint is computed LATER, when the owner opens the item (server side), so a scan
	// with hundreds of drafts never fires the cheaper-variant queries in a burst.
	return serieResolvida{serieID: r.acharOuCriarDraftSerie(ctx, serieNome), conf: confRevisaoSerie, emRevisao: true}
}

// tentarImportarSerie tries the auto-import of a series by NAME: extracted from
// resolverSerieMiss so it can be reused by the reconciliation of draft series
// (reconciliar.go), which needs the SAME import attempt outside the context of a
// scanRun. ok=false on any degradation/failure -- no SerieImporter configured,
// an empty name, or the import failed (ambiguous/not found/network error) -- with
// the SAME 3 logs as always. Success: it fires the optional automatic
// materialization (best-effort, it never aborts the resolution) and returns the
// serieResolvida already with the episode index loaded.
func (m *Manager) tentarImportarSerie(ctx context.Context, alvo enrich.AlvoSerie, langs []string, preferredLang string) (serieResolvida, bool) {
	serieNome := alvo.Nome
	if m.serieImporter == nil || serieNome == "" {
		return serieResolvida{}, false
	}
	id, err := m.serieImporter.ImportarSeriePorNome(ctx, alvo, langs, preferredLang)
	if err != nil {
		if errors.Is(err, enrich.ErrSerieIdentidadeIncompativel) {
			log.Printf("scanner: série %q -- a entrada do provider não comporta a pasta (%v) — vai para revisão", serieNome, err)
		} else if errors.Is(err, enrich.ErrSerieAmbigua) {
			log.Printf("scanner: série %q ambígua no provider — vai para revisão", serieNome)
		} else if errors.Is(err, enrich.ErrSerieNaoEncontrada) {
			log.Printf("scanner: série %q não encontrada no provider — vai para revisão", serieNome)
		} else {
			log.Printf("scanner: auto-import da série %q falhou (%v) — vai para revisão", serieNome, err)
		}
		return serieResolvida{}, false
	}
	// Optional automatic materialization (0089): best-effort, it never aborts the
	// resolution -- the series was already imported successfully.
	if m.materializador != nil {
		if merr := m.materializador.MaterializarSeAutoDownloadLigado(ctx, id); merr != nil {
			log.Printf("scanner: materialização automática da série %q falhou (%v)", serieNome, merr)
		}
	}
	return serieResolvida{serieID: id, index: m.carregarIndex(ctx, id), conf: confSerieEp}, true
}

// tentarImportarSeriePorID resolves the series BY the id the folder names, asked of the
// importer directly -- no title search at all (a misspelled folder name is invisible to
// it). It returns ok=false whenever there is nothing to do or the import failed, so the
// caller keeps the normal chain (catalog by title, then import by name, then draft).
//
// The confidence is confIDExterno: the identity came from an id the owner wrote, the same
// trust as an id already present in the catalog.
func (m *Manager) tentarImportarSeriePorID(ctx context.Context, folderInfo naming.Info, langs []string) (serieResolvida, bool) {
	fonte, idExterno, ok := referenciaDaPasta(folderInfo)
	if !ok || m.serieImporter == nil {
		return serieResolvida{}, false
	}
	id, err := m.serieImporter.ImportarSeriePorID(ctx, fonte, idExterno, langs)
	if err != nil {
		log.Printf("scanner: import da série por id (%s:%s) falhou (%v) — segue pelo nome", fonte, idExterno, err)
		return serieResolvida{}, false
	}
	// Optional automatic materialization (0089): best-effort, same as the import by name.
	if m.materializador != nil {
		if merr := m.materializador.MaterializarSeAutoDownloadLigado(ctx, id); merr != nil {
			log.Printf("scanner: materialização automática da série %s:%s falhou (%v)", fonte, idExterno, merr)
		}
	}
	return serieResolvida{serieID: id, index: m.carregarIndex(ctx, id), conf: confIDExterno}, true
}

// langsPermitidos returns the allowed languages (settings) for the auto-import, or
// nil when the scanner runs without that source (the provider falls back to the
// default language).
func (r *scanRun) langsPermitidos(ctx context.Context) []string {
	if r.m.idiomas == nil {
		return nil
	}
	return r.m.idiomas(ctx)
}

// preferredLangDaFonte returns the preferred language of the library_source that
// triggered this scan, or "" when the source has no defined preference -- in that
// case the SerieImporter falls back to the global language
// (langsPermitidos/Manager.idiomas), the same behavior as before.
func (r *scanRun) preferredLangDaFonte() string {
	if r.source.PreferredLanguage == nil {
		return ""
	}
	return *r.source.PreferredLanguage
}

// acharSeriePorIDExterno looks the series up by the id embedded in the folder name
// (tmdb has priority over imdb), FILTERING type='tv' — a numeric id equal to that
// of a movie must not match the series.
// acharSeriePorIDExterno looks the series up by the id embedded in the folder name
// (tmdb has priority over imdb), TYPE-SCOPED to 'tv': the external id is only unique
// within a type, and a numeric tmdb id equal to that of a movie or an episode must not
// match the series.
func acharSeriePorIDExterno(ctx context.Context, q *sqlcgen.Queries, info naming.Info) (string, bool) {
	if info.TMDBID != "" {
		e, err := q.ObterEntityPorTmdbTipo(ctx, sqlcgen.ObterEntityPorTmdbTipoParams{
			ExternalIDTmdb: nsOpt(info.TMDBID), Type: tipoSerieTV,
		})
		if err == nil {
			return e.ID, true
		}
	}
	if info.TVDBID != "" {
		e, err := q.ObterEntityPorTvdbTipo(ctx, sqlcgen.ObterEntityPorTvdbTipoParams{
			ExternalIDTvdb: nsOpt(info.TVDBID), Type: tipoSerieTV,
		})
		if err == nil {
			return e.ID, true
		}
	}
	if info.IMDBID != "" {
		e, err := q.ObterEntityPorImdbTipo(ctx, sqlcgen.ObterEntityPorImdbTipoParams{
			ExternalIDImdb: nsOpt(info.IMDBID), Type: tipoSerieTV,
		})
		if err == nil {
			return e.ID, true
		}
	}
	return "", false
}

// referenciaDaPasta returns the id the FOLDER NAMES, in priority order: tmdb, tvdb, imdb.
// TMDb comes first because it is the source the import goes through (the tree of a TVDB
// id is crossed to TMDb anyway), so the id the user pastes most is also the cheapest.
func referenciaDaPasta(info naming.Info) (fonte, id string, ok bool) {
	switch {
	case info.TMDBID != "":
		return "tmdb", info.TMDBID, true
	case info.TVDBID != "":
		return "tvdb", info.TVDBID, true
	case info.IMDBID != "":
		return "imdb", info.IMDBID, true
	}
	return "", "", false
}

// acharSeriePorTitulo looks the series up in the catalog by the pair (type='tv',
// title), to reuse a series imported in a previous scan without a new round-trip to
// the provider. WITHOUT a status filter, on purpose: it is used EXCLUSIVELY by
// acharOuCriarDraftSerie (find-or-create of the review draft), which needs to keep
// finding the SAME draft already created so as not to duplicate it on every scan.
// The RESOLUTION path (resolverSerieMiss) uses acharSerieResolvidaPorTitulo below,
// which filters drafts out -- do NOT reuse this function there.
func acharSeriePorTitulo(ctx context.Context, q *sqlcgen.Queries, nome string) (string, bool) {
	if nome == "" {
		return "", false
	}
	e, err := q.ObterEntityPorTituloTipo(ctx, sqlcgen.ObterEntityPorTituloTipoParams{Type: tipoSerieTV, Title: nome})
	if err == nil {
		return e.ID, true
	}
	return "", false
}

// acharSerieResolvidaPorTitulo looks up the series that is really RESOLVED
// (type='tv', title, any status DIFFERENT from 'draft') -- used ONLY by the
// RESOLUTION path (resolverSerieMiss). Bug fixed: a review draft (status='draft',
// created by acharOuCriarDraftSerie) must never be found here as if it were a
// resolved identity -- before this fix, that PERMANENTLY blocked the real
// auto-import attempt (tentarImportarSerie was never reached again once the draft
// existed). "", false both for "did not find it" and for "found it, but it is a
// draft".
func acharSerieResolvidaPorTitulo(ctx context.Context, q *sqlcgen.Queries, nome string) (string, bool) {
	if nome == "" {
		return "", false
	}
	e, err := q.ObterEntityPorTituloTipo(ctx, sqlcgen.ObterEntityPorTituloTipoParams{Type: tipoSerieTV, Title: nome})
	if err != nil || e.Status == "draft" {
		return "", false
	}
	return e.ID, true
}

// acharOuCriarDraftSerie is the review FALLBACK: it reuses (find) the draft series
// by title or creates an entity type='tv' status='draft' (unidentified, visible in
// the review/orphan queue — 0018). It is the NOT NULL anchor of the media_file when
// the series could not be resolved; it is NOT a "ghost" series with invented
// episodes. "" only when not even that could be created (the writer then skips the
// file).
func (r *scanRun) acharOuCriarDraftSerie(ctx context.Context, nome string) string {
	if id, ok := acharSeriePorTitulo(ctx, sqlcgen.New(r.m.db.Read), nome); ok {
		return id
	}
	titulo := nome
	if titulo == "" {
		titulo = "Série não identificada"
	}
	q := sqlcgen.New(r.m.db.Write)
	id := r.m.novoID()
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: id, Type: tipoSerieTV, Title: titulo}); err != nil {
		log.Printf("scanner: falha ao criar série rascunho %q: %v", titulo, err)
		return ""
	}
	if _, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID: id, Type: tipoSerieTV, Title: titulo, Status: "draft",
	}); err != nil {
		log.Printf("scanner: falha ao rebaixar série rascunho %q para draft: %v", titulo, err)
	}
	return id
}

// carregarIndex assembles the number→episode_id index of a series, reading the
// series (for the orderings) and each episode (numbering + absolute_number in the
// extension_data). It is the SINGLE read per series of the scan; the in-memory
// matching (internal/tvmatch) comes after, without SQL. A Manager method (not
// scanRun) so it can be reused outside a scan (reconciliar.go,
// tentarImportarSerie) -- it uses no scanRun-specific state.
func (m *Manager) carregarIndex(ctx context.Context, serieID string) *tvmatch.Index {
	q := sqlcgen.New(m.db.Read)
	idx := tvmatch.NovoIndex()

	serie, err := q.ObterEntity(ctx, serieID)
	if err != nil {
		log.Printf("scanner: série %s ilegível ao montar índice: %v", serieID, err)
		return idx
	}
	idx.PreencherOrdenacoes(extrairTVNS(serie.ExtensionDataJson))
	// The owner order lives in the OVERRIDE layer (ADR-0033): a re-enrich rewrites
	// extension_data, the owner's numbering must survive it.
	if ov, err := q.ObterMediaOverride(ctx, serieID); err == nil {
		idx.AplicarOrdemDoDono(tvext.OrdemDoDonoDoOverride(ov.OverrideJson))
	}

	temps, err := q.ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		log.Printf("scanner: temporadas da série %s ilegíveis: %v", serieID, err)
		return idx
	}
	for _, t := range temps {
		if t.Kind != kindRelTemporada {
			continue
		}
		m.indexarTemporada(ctx, q, idx, t.ChildID)
	}
	return idx
}

// indexarTemporada walks the episodes of ONE season and inserts them into the
// index. An unreadable season is skipped (it degrades without taking down the rest
// of the index).
func (m *Manager) indexarTemporada(ctx context.Context, q *sqlcgen.Queries, idx *tvmatch.Index, tempID string) {
	eps, err := q.ListarRelacoesFilhas(ctx, tempID)
	if err != nil {
		return
	}
	for _, e := range eps {
		if e.Kind != kindVinculoEpisodio {
			continue
		}
		m.indexarEpisodio(ctx, q, idx, e.ChildID)
	}
}

// indexarEpisodio reads the extension_data of an episode and delegates the in-memory
// assembly (numbering + absolute_number) to internal/tvmatch. An unreadable episode
// is skipped.
func (m *Manager) indexarEpisodio(ctx context.Context, q *sqlcgen.Queries, idx *tvmatch.Index, epID string) {
	ent, err := q.ObterEntity(ctx, epID)
	if err != nil {
		return
	}
	// The title goes into the index: it is the only evidence the FILE carries about which
	// episode an absolute number points at (#105).
	idx.IndexarEpisodioComTitulo(epID, ent.Title, extrairTVNS(ent.ExtensionDataJson))
}

// gravarArquivoSerie persists the series file: the media_file points to the SERIES
// (the work), and the per-episode links live in the N:M junction. Idempotent: dedup
// by content_hash (rename/move) and a clean re-scan of the links by kind before
// recreating — the ids re-projected in the current scan, never numbers. Without a
// matched episode, the media_file enters with review confidence (it shows up as
// pending in the queue).
func (r *scanRun) gravarArquivoSerie(ctx context.Context, q *sqlcgen.Queries, f fileFacts) error {
	if f.serieID == "" {
		log.Printf("scanner: pulando %q: série não resolvível e sem rascunho", f.path)
		return nil
	}

	// The external tree did not have the episode, but the file declares the
	// numbering: it creates the provisional one and continues as a normal link.
	if len(f.episodeIDs) == 0 && f.prov != nil {
		ids, perr := r.acharOuCriarProvisorios(ctx, q, f.serieID, *f.prov)
		if perr != nil {
			log.Printf("scanner: falha ao criar o episódio provisório de %q (fica em revisão): %v", f.path, perr)
		} else {
			f.episodeIDs = ids
		}
	}

	matchConf := float64(confRevisaoSerie)
	if len(f.episodeIDs) > 0 {
		matchConf = f.serieConf
	}

	mediaFileID, err := r.upsertMediaFileSerie(ctx, q, f, matchConf)
	if err != nil {
		return err
	}

	// Clean re-scan of the episode links of this file (it does not touch other
	// kinds) and recreation from the ids matched now — the ADR-0021 anchor.
	if err := q.RemoverVinculosDoArquivoPorKind(ctx, sqlcgen.RemoverVinculosDoArquivoPorKindParams{
		MediaFileID: mediaFileID,
		Kind:        kindVinculoEpisodio,
	}); err != nil {
		return err
	}
	for i, epID := range f.episodeIDs {
		if err := q.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
			MediaFileID: mediaFileID,
			EntityID:    epID,
			Kind:        kindVinculoEpisodio,
			Ordinal:     int64(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

// upsertMediaFileSerie creates the media_file (or finds it again by content_hash
// and updates the path/state). It returns the media_file id for the links.
func (r *scanRun) upsertMediaFileSerie(ctx context.Context, q *sqlcgen.Queries, f fileFacts, matchConf float64) (string, error) {
	existente, err := q.ObterMediaFilePorContentHash(ctx, nsOpt(f.hash))
	if err == nil {
		_, uerr := q.AtualizarMediaFileVisto(ctx, sqlcgen.AtualizarMediaFileVistoParams{
			Path:            f.path,
			Size:            niInt(f.size),
			Mtime:           nsOpt(f.mtime),
			LastSeen:        nsOpt(r.scanTS),
			LibrarySourceID: nsOpt(r.source.ID),
			ID:              existente.ID,
		})
		return existente.ID, uerr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id := r.m.novoID()
	_, cerr := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID:              id,
		EntityID:        f.serieID,
		Path:            f.path,
		ContentHash:     nsOpt(f.hash),
		Size:            niInt(f.size),
		Mtime:           nsOpt(f.mtime),
		MatchConfidence: nfFloat(matchConf),
		MediainfoJson:   nsBytes(f.mediainfo),
		State:           "present",
		LastSeen:        nsOpt(r.scanTS),
		LibrarySourceID: nsOpt(r.source.ID),
	})
	return id, cerr
}

// extrairTVNS extracts the object of the core.tv@1 namespace from the
// extension_data. nil when absent/unreadable (the caller degrades — a series/episode
// without numbering is not indexed). It delegates to the single parser of tvext (the
// same namespace→object envelope reused by the server); kept as a local name for the
// clarity of the scanner callers.
func extrairTVNS(raw sql.NullString) map[string]any {
	return tvext.Namespace(raw, nsTV)
}

// provisorioEpisodio is the numbering that the FILE declares and the external tree
// does not have: season + numbers read from the name, or only the season of the
// folder when the name does not carry SxxEyy. titulo is the file name without
// extension, used as the title of the provisional episode.
type provisorioEpisodio struct {
	season  int
	numeros []int
	titulo  string
	arquivo string // file name without extension: dedup key when there is no number
}

// episodioPelaPasta is the fallback both Kodi and Jellyfin accept: a file named
// only by its episode number ("01 - O Doutor Fanático.mkv", "Episódio 16 - O Trem
// do Terror.mkv", "14.mkv") inside a season folder takes the SEASON from the
// folder. Without a season folder there is nothing reliable to anchor it on, so it
// stays unrecognized and goes to review.
func episodioPelaPasta(path, base string) (naming.EpisodeInfo, bool) {
	season, ok := naming.NumeroDaPastaTemporada(filepath.Base(filepath.Dir(path)))
	if !ok {
		return naming.EpisodeInfo{}, false
	}
	n, ok := naming.NumeroEpisodioInicial(base)
	if !ok {
		return naming.EpisodeInfo{}, false
	}
	return naming.EpisodeInfo{Season: season, Episodes: []int{n}}, true
}

// temporadaDaPasta returns the season the FOLDER declares, or 0 when the folder is not a
// season one. It is the hint an absolute number needs: the number alone has no season.
func temporadaDaPasta(path string) int {
	n, ok := naming.NumeroDaPastaTemporada(filepath.Base(filepath.Dir(path)))
	if !ok {
		return 0
	}
	return n
}

// provisorioDe assembles the provisional one from the parse of the file name or,
// without SxxEyy, from the season folder it lives in. It returns nil when there is
// no reliable season (a file loose at the root, a folder that is not a season one).
func provisorioDe(path, base string) *provisorioEpisodio {
	if ep, ok := naming.ParseEpisode(base); ok && len(ep.Episodes) > 0 && !ep.IsAbsolute {
		return &provisorioEpisodio{season: ep.Season, numeros: ep.Episodes, titulo: tituloDoArquivo(base), arquivo: base}
	}
	if pareceExtra(base) {
		// Extras (featurette, sample, trailer...) live in the season folders and
		// are not episodes: they only become provisional when the name states the
		// number.
		return nil
	}
	if n, ok := naming.NumeroDaPastaTemporada(filepath.Base(filepath.Dir(path))); ok {
		return &provisorioEpisodio{season: n, titulo: tituloDoArquivo(base), arquivo: base}
	}
	return nil
}

// tituloDoArquivo cleans the file name so it can serve as the title of the
// provisional episode (it removes technical tokens such as resolution and source).
// The episode number, when it exists, enters the title through the caller
// ("Episódio N"), the same pattern the enrichment uses when the source gives no
// title — the raw file name stays in media_file.path anyway.
func tituloDoArquivo(base string) string {
	if t := naming.Parse(base).Title; t != "" {
		return t
	}
	return base
}

// pareceExtra tells whether the file name marks extra material inside a season
// folder (a "Season 01/featurette bonus.mkv" must not become a provisional
// episode — that would be a lie about the library). The word list lives in
// localmedia.ExtraWords, ONE list for the whole codebase: the same marks
// decide which file NAMES an entity when it exports its NFO and artwork
// (localmedia.ArquivoMaisAntigo, issue #96), and two lists would drift.
func pareceExtra(base string) bool {
	return localmedia.IsExtraFileName(base)
}

// acharOuCriarProvisorios returns the ids of the provisional episodes for the file:
// one per number read from the name, or a single one without numbering (dedup by
// title inside the season). It creates the season when it does not exist in the
// series either. Idempotent: a re-scan finds what was already created.
func (r *scanRun) acharOuCriarProvisorios(ctx context.Context, q *sqlcgen.Queries, serieID string, prov provisorioEpisodio) ([]string, error) {
	tempID, err := r.acharOuCriarTemporadaProvisoria(ctx, q, serieID, prov.season)
	if err != nil {
		return nil, err
	}
	if len(prov.numeros) == 0 {
		id, err := r.acharOuCriarEpisodioProvisorio(ctx, q, tempID, prov.season, 0, false, prov.titulo, prov.arquivo)
		if err != nil {
			return nil, err
		}
		return []string{id}, nil
	}
	ids := make([]string, 0, len(prov.numeros))
	for _, n := range prov.numeros {
		id, err := r.acharOuCriarEpisodioProvisorio(ctx, q, tempID, prov.season, n, true, fmt.Sprintf("Episódio %d", n), prov.arquivo)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// acharOuCriarTemporadaProvisoria does the find-or-create of the season by
// sort_index inside the series (the same key the enrich uses in
// acharOuCriarTemporada, without a source id).
func (r *scanRun) acharOuCriarTemporadaProvisoria(ctx context.Context, q *sqlcgen.Queries, serieID string, season int) (string, error) {
	rels, err := q.ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		return "", err
	}
	for _, rel := range rels {
		if rel.Kind == kindRelTemporada && rel.SortIndex.Valid && int(rel.SortIndex.Int64) == season {
			return rel.ChildID, nil
		}
	}
	id := r.m.novoID()
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:    id,
		Type:  tipoEntTemporada,
		Title: tituloTemporadaProvisoria(season),
	}); err != nil {
		return "", err
	}
	if _, err := q.CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID:        r.m.novoID(),
		ParentID:  serieID,
		ChildID:   id,
		Kind:      kindRelTemporada,
		SortIndex: sql.NullInt64{Int64: int64(season), Valid: true},
	}); err != nil {
		return "", err
	}
	return id, nil
}

// acharOuCriarEpisodioProvisorio does the find-or-create of the episode in the
// season: with a number, the key is the sort_index of the relation (the way the
// enrich links); without a number, it is the exact title inside the season. The
// provisional one is born status='draft' and marks the origin in the extension_data,
// so the UI and the reconciliation know that the number came from the file.
func (r *scanRun) acharOuCriarEpisodioProvisorio(ctx context.Context, q *sqlcgen.Queries, tempID string, season, number int, comNumero bool, titulo, arquivo string) (string, error) {
	rels, err := q.ListarRelacoesFilhas(ctx, tempID)
	if err != nil {
		return "", err
	}
	for _, rel := range rels {
		if rel.Kind != kindVinculoEpisodio {
			continue
		}
		if comNumero {
			if rel.SortIndex.Valid && int(rel.SortIndex.Int64) == number {
				r.atualizarTituloDoProvisorio(ctx, q, rel.ChildID, titulo)
				return rel.ChildID, nil
			}
			continue
		}
		// Without a number the key is the FILE NAME stored in the extension_data,
		// not the title: the title is a presentation rule and may change from one
		// version to another (that was what created duplicate provisional episodes
		// in the first round).
		existente, oerr := q.ObterEntity(ctx, rel.ChildID)
		if oerr == nil && strings.Contains(existente.ExtensionDataJson.String, `"arquivo":"`+arquivo+`"`) {
			return rel.ChildID, nil
		}
	}
	id := r.m.novoID()
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                id,
		Type:              tipoEntEpisodio,
		Title:             titulo,
		ExtensionDataJson: nsBytes(extensionProvisorio(season, number, comNumero, arquivo)),
	}); err != nil {
		return "", err
	}
	// CriarEntity does not take a status; the provisional one is born 'draft' in an
	// UPDATE, the same path the music draft track uses (musica.go).
	if _, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID:                id,
		Type:              tipoEntEpisodio,
		Title:             titulo,
		Status:            statusRascunho,
		ExtensionDataJson: nsBytes(extensionProvisorio(season, number, comNumero, arquivo)),
	}); err != nil {
		return "", err
	}
	if _, err := q.CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID:        r.m.novoID(),
		ParentID:  tempID,
		ChildID:   id,
		Kind:      kindVinculoEpisodio,
		SortIndex: sql.NullInt64{Int64: int64(number), Valid: true},
	}); err != nil {
		return "", err
	}
	log.Printf("scanner: episódio provisório %q -> S%02dE%02d (a árvore externa não tem esse episódio)", titulo, season, number)
	return id, nil
}

// atualizarTituloDoProvisorio keeps the title of an already created provisional
// episode up to date with the current rule, without touching an episode that came
// from the source (the `provisorio` mark in the extension_data is what distinguishes
// the two). An error is logged and ignored: a stale title cannot take the scan down.
func (r *scanRun) atualizarTituloDoProvisorio(ctx context.Context, q *sqlcgen.Queries, epID, titulo string) {
	ep, err := q.ObterEntity(ctx, epID)
	if err != nil || ep.Title == titulo {
		return
	}
	if !strings.Contains(ep.ExtensionDataJson.String, `"provisorio"`) {
		return
	}
	if _, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID:                epID,
		Type:              ep.Type,
		Title:             titulo,
		Status:            ep.Status,
		ExtensionDataJson: ep.ExtensionDataJson,
	}); err != nil {
		log.Printf("scanner: falha ao atualizar o título do episódio provisório %q: %v", epID, err)
	}
}

// tituloTemporadaProvisoria names the season created by the scanner with the same
// convention as the enrich ("Especiais" for 0).
func tituloTemporadaProvisoria(season int) string {
	if season == 0 {
		return "Especiais"
	}
	return fmt.Sprintf("Temporada %d", season)
}

// extensionProvisorio assembles the core.tv@1 of the provisional episode: the
// official numbering and the mark that it came from the file, not from the external
// source.
func extensionProvisorio(season, number int, comNumero bool, arquivo string) []byte {
	numero := map[string]any{"season": season}
	if comNumero {
		numero["number"] = number
	}
	doc := map[string]any{
		nsTV: map[string]any{
			"numbering":  map[string]any{"official": numero},
			"provisorio": map[string]any{"origem": "arquivo", "arquivo": arquivo},
		},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	return b
}

// arquivosNaSerie counts the video files under the series folder -- the SIZE EVIDENCE the
// identity check needs (a folder with 26 files cannot be a show with 3 episodes).
//
// Cached per scan: the count is asked once per series even when the folder holds hundreds of
// files, and a failure to read degrades to 0 (no size evidence, never a failed scan). The
// extension filter is the SAME one the scan uses, so the count and the scanned files agree.
func (r *scanRun) arquivosNaSerie(serieFolder string) int {
	if n, ok := r.arquivosPorSerie[serieFolder]; ok {
		return n
	}
	total := 0
	err := filepath.WalkDir(serieFolder, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // a subfolder nobody can read is not a reason to abort the count
		}
		if d.IsDir() {
			return nil
		}
		if extensoesVideo[strings.ToLower(filepath.Ext(d.Name()))] {
			total++
		}
		return nil
	})
	if err != nil {
		log.Printf("scanner: contar arquivos de %q falhou (%v) -- sem evidencia de tamanho para a identidade", serieFolder, err)
		total = 0
	}
	r.arquivosPorSerie[serieFolder] = total
	return total
}
