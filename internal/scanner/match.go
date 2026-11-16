package scanner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
)

// Confidence thresholds (0–100). They are conservative TUNING, not a model
// (ADR-0008): an embedded id matches 100%; title+year is a high guess; title
// without a year is low (flagged); without a title it becomes an unidentified
// draft.
const (
	// tipoFilme is the entity.type of the MOVIE path (the scanner default: see
	// montarFacts/resolverEntity, which writes Type "movie") -- used by the
	// post-commit dispatch to pick the enrichment hook.
	tipoFilme = "movie"

	confIDExterno   = 100
	confTituloAno   = 70
	confTituloSoNom = 40
	confDraft       = 0
)

// gravarArquivo applies the matching and writes/updates the media_file. Dedup by
// content_hash: if the same content already exists, it UPDATES the path (covers
// rename/move — idempotent) instead of creating another row.
func (r *scanRun) gravarArquivo(ctx context.Context, q *sqlcgen.Queries, f fileFacts) error {
	if f.isSerie {
		return r.gravarArquivoSerie(ctx, q, f)
	}
	if f.isMusica {
		return r.gravarArquivoMusica(ctx, q, f)
	}
	existente, err := q.ObterMediaFilePorContentHash(ctx, nsOpt(f.hash))
	if err == nil {
		return r.reencontrar(ctx, q, existente, f)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// The hash did not match any row, but the PATH may already have one (content
	// swapped in place -- reencode/replacement, or mtime/size of the provider
	// drifted enough to invalidate the short-circuit of
	// mediaFileInalteradoPorPath). Without this check, the INSERT below would hit
	// the UNIQUE(path) and abort the whole batch.
	porPath, err := q.ObterMediaFilePorPath(ctx, f.path)
	if err == nil {
		return r.atualizarConteudoMudou(ctx, q, porPath, f)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	entityID, criada, err := r.resolverEntity(ctx, q, f)
	if err != nil {
		return err
	}
	_, err = q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID:              r.m.novoID(),
		EntityID:        entityID,
		Path:            f.path,
		ContentHash:     nsOpt(f.hash),
		Size:            niInt(f.size),
		Mtime:           nsOpt(f.mtime),
		Edition:         nsOpt(f.arquivo.Edition),
		PartNo:          niOptInt(f.arquivo.PartNo),
		MatchConfidence: nfFloat(confianca(f.entidade)),
		MediainfoJson:   nsBytes(f.mediainfo),
		State:           "present",
		LastSeen:        nsOpt(r.scanTS),
		LibrarySourceID: nsOpt(r.source.ID),
	})
	if err != nil {
		return err
	}
	if criada {
		// It only RECORDS here: the trigger (auto-match + materialization) runs
		// after the batch commit, in despacharNovos (ADR-0031) -- inside the
		// transaction it would read the entity through the READ pool (`sql: no
		// rows in result set`) and, when writing, would block on the single write
		// connection (ADR-0010).
		r.registrarNovo(entidadeNova{id: entityID, tipo: tipoFilme})
	}
	return nil
}

// despacharNovos runs the post-import of the entities created in the scan pass,
// ALREADY OUTSIDE the write transaction (ADR-0031) -- it is the trigger that
// used to live inside gravarArquivo (dispararAutoMatchFilme) and therefore never
// worked in production:
//
//   - movie: it tries to match+enrich (TentarAutoMatchFilme, 0089) and, when it
//     matches, materializes the discovered artwork right away when the toggle is
//     on;
//   - faixa: it materializes the artwork of the newly resolved ALBUM (the cover
//     lives on the album, ADR-0027) when the toggle is on -- the track only
//     enters here when the match went past the draft (conf > confDraft);
//   - series: the trigger already happens in the FACTS phase (tentarImportarSerie,
//     which runs outside any transaction) -- nothing to do here.
//
// Everything is best-effort: a failure never undoes the write of the file nor
// aborts the scan (same semantics as before, now in the right place).
func (r *scanRun) despacharNovos(ctx context.Context) {
	if len(r.novos) == 0 {
		return
	}
	novos := r.novos
	r.novos = nil

	materializados := make(map[string]bool, len(novos))
	for _, n := range novos {
		if err := ctx.Err(); err != nil {
			return
		}
		switch n.tipo {
		case tipoFilme:
			matched := r.dispararAutoMatchFilme(ctx, n.id)
			if matched {
				r.materializar(ctx, n.id, materializados)
			}
		case tipoFaixa:
			if n.conf <= confDraft {
				continue
			}
			for _, albumID := range r.albumsDaFaixa(ctx, n.id) {
				r.materializar(ctx, albumID, materializados)
			}
		}
	}
}

// materializar calls the optional materialization hook (0089) a single time per
// entity in this pass -- several tracks of the same album do not trigger the
// same download/write N times.
func (r *scanRun) materializar(ctx context.Context, entityID string, feitos map[string]bool) {
	if r.m.materializador == nil || feitos[entityID] {
		return
	}
	feitos[entityID] = true
	if err := r.m.materializador.MaterializarSeAutoDownloadLigado(ctx, entityID); err != nil {
		log.Printf("scanner: materialização automática da entity %q falhou (%v)", entityID, err)
	}
}

// albumsDaFaixa returns the albums (parents, kind of the music link) of a newly
// resolved track -- read through the READ pool, now safe (post-commit). Empty
// when the track does not have an album linked yet (partial match).
func (r *scanRun) albumsDaFaixa(ctx context.Context, faixaID string) []string {
	relacoes, err := sqlcgen.New(r.m.db.Read).ListarRelacoesPais(ctx, faixaID)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(relacoes))
	for _, rel := range relacoes {
		if rel.ParentID != "" {
			ids = append(ids, rel.ParentID)
		}
	}
	return ids
}

// dispararAutoMatchFilme is the best-effort trigger of the immediate movie
// auto-match (0089): called AFTER the batch commit (despacharNovos), never again
// from inside the transaction. It returns matched=true only when a strong
// candidate was actually applied.
func (r *scanRun) dispararAutoMatchFilme(ctx context.Context, entityID string) bool {
	if r.m.filmeImportador == nil {
		return false
	}
	matched, err := r.m.filmeImportador.TentarAutoMatchFilme(ctx, entityID)
	if err != nil {
		log.Printf("scanner: auto-match imediato do filme %q falhou (%v) -- entity permanece pendente na fila de Revisão", entityID, err)
		return false
	}
	return matched
}

// reencontrar handles a file already known by content_hash: it updates the path
// (rename/move), size/mtime, sets it back to present and stamps last_seen. It
// reuses the existing entity (keeps the cache coherent for the other versions of
// the work).
func (r *scanRun) reencontrar(ctx context.Context, q *sqlcgen.Queries, existente sqlcgen.MediaFile, f fileFacts) error {
	_, err := q.AtualizarMediaFileVisto(ctx, sqlcgen.AtualizarMediaFileVistoParams{
		Path:            f.path,
		Size:            niInt(f.size),
		Mtime:           nsOpt(f.mtime),
		LastSeen:        nsOpt(r.scanTS),
		LibrarySourceID: nsOpt(r.source.ID),
		ID:              existente.ID,
	})
	if err == nil {
		r.cache[f.folderKey] = existente.EntityID
	}
	return err
}

// atualizarConteudoMudou handles a file whose PATH was already known but whose
// newly computed content_hash differs from the existing row: the content was
// replaced in place (reencode, swapped file). It updates every field derived
// from the content, preserving id/entity_id (same work, same path).
func (r *scanRun) atualizarConteudoMudou(ctx context.Context, q *sqlcgen.Queries, existente sqlcgen.MediaFile, f fileFacts) error {
	_, err := q.AtualizarMediaFileConteudoMudou(ctx, sqlcgen.AtualizarMediaFileConteudoMudouParams{
		ContentHash:     nsOpt(f.hash),
		Size:            niInt(f.size),
		Mtime:           nsOpt(f.mtime),
		Edition:         nsOpt(f.arquivo.Edition),
		PartNo:          niOptInt(f.arquivo.PartNo),
		MatchConfidence: nfFloat(confianca(f.entidade)),
		MediainfoJson:   nsBytes(f.mediainfo),
		LastSeen:        nsOpt(r.scanTS),
		LibrarySourceID: nsOpt(r.source.ID),
		ID:              existente.ID,
	})
	if err == nil {
		r.cache[f.folderKey] = existente.EntityID
	}
	return err
}

// resolverEntity finds or creates the Entity of the work. Order: scan cache →
// external id (tmdb/imdb) → title+year → create a new one. Without an id and
// without a title it becomes a draft. criada is true ONLY when the branch that
// effectively created the entity (criarEntity) was taken -- the other branches
// (scan cache, external id/title+year that already exist) return false. Used by
// gravarArquivo (0089) to fire the immediate auto-match only for an entity NEW
// in this scan pass.
func (r *scanRun) resolverEntity(ctx context.Context, q *sqlcgen.Queries, f fileFacts) (id string, criada bool, err error) {
	if id, ok := r.cache[f.folderKey]; ok {
		return id, false, nil
	}
	if id, achou, err := acharPorIDExterno(ctx, q, f.entidade); err != nil {
		return "", false, err
	} else if achou {
		r.cache[f.folderKey] = id
		return id, false, nil
	}

	titulo, releaseDate, status := classificar(f.entidade, f.rawNome)

	if id, achou, err := r.acharPorTituloAno(ctx, q, f.entidade, titulo, releaseDate); err != nil {
		return "", false, err
	} else if achou {
		r.cache[f.folderKey] = id
		return id, false, nil
	}

	id, err = r.criarEntity(ctx, q, f, titulo, releaseDate, status)
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// acharPorIDExterno looks the entity up by the id embedded in the name (tmdb has
// priority over imdb). achou=false when there is no id or it does not exist yet.
func acharPorIDExterno(ctx context.Context, q *sqlcgen.Queries, p naming.Info) (string, bool, error) {
	if p.TMDBID != "" {
		e, err := q.ObterEntityPorTmdb(ctx, nsOpt(p.TMDBID))
		if err == nil {
			return e.ID, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
	}
	if p.IMDBID != "" {
		e, err := q.ObterEntityPorImdb(ctx, nsOpt(p.IMDBID))
		if err == nil {
			return e.ID, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
	}
	return "", false, nil
}

// acharPorTituloAno dedup without an id: it reuses the same entity for (movie,
// title, release_date). Only when there is NO external id (with an id, the lookup
// above wins).
func (r *scanRun) acharPorTituloAno(ctx context.Context, q *sqlcgen.Queries, p naming.Info, titulo string, releaseDate sql.NullString) (string, bool, error) {
	if titulo == "" || p.TMDBID != "" || p.IMDBID != "" {
		return "", false, nil
	}
	e, err := q.ObterEntityPorTituloAno(ctx, sqlcgen.ObterEntityPorTituloAnoParams{
		Type:        "movie",
		Title:       titulo,
		ReleaseDate: releaseDate,
	})
	if err == nil {
		return e.ID, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	return "", false, nil
}

// criarEntity inserts the new entity (a draft when unidentified) and, being a new
// work, records the local artwork as an asset. It returns the created id.
func (r *scanRun) criarEntity(ctx context.Context, q *sqlcgen.Queries, f fileFacts, titulo string, releaseDate sql.NullString, status string) (string, error) {
	id := r.m.novoID()
	extJSON := externalIDsJSON(f.entidade)
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              id,
		Type:            "movie",
		Title:           titulo,
		ReleaseDate:     releaseDate,
		ExternalIdsJson: nsOpt(extJSON),
	}); err != nil {
		return "", err
	}
	// CriarEntity is born 'cataloged' (DEFAULT). It is lowered to 'draft' when
	// the work was not identified (visible, not lost — ADR-0012).
	if status == "draft" {
		if _, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
			ID:              id,
			Type:            "movie",
			Title:           titulo,
			Status:          "draft",
			ReleaseDate:     releaseDate,
			ExternalIdsJson: nsOpt(extJSON),
		}); err != nil {
			return "", err
		}
	}
	r.cache[f.folderKey] = id
	if f.poster != "" {
		if _, err := q.CriarAsset(ctx, sqlcgen.CriarAssetParams{
			ID:        r.m.novoID(),
			EntityID:  id,
			Kind:      "poster",
			Origin:    "local",
			PathOrUri: f.poster,
		}); err != nil {
			return "", err
		}
	}
	return id, nil
}

// classificar decides the title to store, the release_date and the status.
// Without a parseable title and without an id, the work is a draft (it uses the
// raw name as the visible title).
func classificar(p naming.Info, rawNome string) (titulo string, releaseDate sql.NullString, status string) {
	titulo = p.Title
	status = "cataloged"
	if titulo == "" {
		titulo = rawNome
		if p.TMDBID == "" && p.IMDBID == "" {
			status = "draft"
		}
	}
	if p.Year > 0 {
		releaseDate = sql.NullString{String: fmt.Sprintf("%04d-01-01", p.Year), Valid: true}
	}
	return titulo, releaseDate, status
}

// confianca scores the match (0–100) according to the signals in the name
// (ADR-0008).
func confianca(p naming.Info) float64 {
	switch {
	case p.TMDBID != "" || p.IMDBID != "":
		return confIDExterno
	case p.Title == "":
		return confDraft
	case p.Year > 0:
		return confTituloAno
	default:
		return confTituloSoNom
	}
}

// externalIDsJSON builds the external_ids map ({"tmdb":"..","imdb":".."}); "" if
// there is no embedded id. Built by hand (few fields) so as not to pull in json.
func externalIDsJSON(p naming.Info) string {
	if p.TMDBID == "" && p.IMDBID == "" {
		return ""
	}
	partes := make([]string, 0, 2)
	if p.TMDBID != "" {
		partes = append(partes, fmt.Sprintf(`"tmdb":%q`, p.TMDBID))
	}
	if p.IMDBID != "" {
		partes = append(partes, fmt.Sprintf(`"imdb":%q`, p.IMDBID))
	}
	out := "{"
	for i, p := range partes {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out + "}"
}
