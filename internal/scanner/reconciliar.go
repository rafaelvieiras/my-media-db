package scanner

// reconciliar.go implements the RECONCILIATION of orphan children: a track/series
// that got stuck in draft because the match failed at scan time (or the parent
// tree did not exist yet) is never retried by the scan itself -- the match
// pipeline runs a SINGLE time, per file, at scan time (see the package comment of
// musica.go/serie.go). The two methods exported here are called from OUTSIDE the
// scanner package (server/job layer, analogous to RefreshLocalAsset in
// recheck.go) to retry that resolution later, without re-walking the whole
// datasource: they reuse the SAME matching logic of the scan
// (Manager.matchTextualFaixa, Manager.tentarImportarSerie) outside the context of
// a scanRun.
//
// Neither of them deletes the draft entity (non-destructive principle): it simply
// ends up with no media_file (derived ownership drops to zero) and leaves the
// review queue through the mechanism that already exists.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/tvmatch"
)

// ReconciliarFaixaDraft tries to relink an orphan DRAFT track (without an
// entity_relation as the child of any album) against the identity tree that is
// already resolved, re-extracting the tags of the file (its media_file) and
// running the SAME matchTextualFaixa of the scan. Success: it repoints
// media_file.entity_id of the draft track to the newly resolved track.
// linked=false without an error is normal degradation (no provider/importer, no
// media_file, tags not extractable, or no match -- never a business error, only a
// real database/io error rises in err).
func (m *Manager) ReconciliarFaixaDraft(ctx context.Context, entityID string, langs []string) (linked bool, err error) {
	mf, ok, err := m.mediaFilePresentePorEntity(ctx, entityID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	tags, duracaoSegundos, ok := m.probarTags(ctx, mf.Path)
	if !ok {
		return false, nil
	}

	faixaID, _, ok := m.matchTextualFaixa(ctx, tags, duracaoSegundos, langs, nil)
	if !ok {
		return false, nil
	}

	if err := sqlcgen.New(m.db.Write).AtualizarMediaFileEntityID(ctx, sqlcgen.AtualizarMediaFileEntityIDParams{
		EntityID:        faixaID,
		MatchConfidence: nfFloat(confMusicaTextual),
		ID:              mf.ID,
	}); err != nil {
		return false, err
	}
	m.purgarRascunhoSubstituido(ctx, entityID)
	return true, nil
}

// purgarRascunhoSubstituido discards the draft that has just lost the file to the
// definitive entity. Without this the draft stayed in the review queue forever as
// "sem arquivo": 114 cases measured in production on Nov 8, 2026, and the queue
// never shrank even with the reconciliation working. Best-effort (the new link is
// already written): a failure only logs.
func (m *Manager) purgarRascunhoSubstituido(ctx context.Context, entityID string) {
	if entityID == "" {
		return
	}
	if _, err := sqlcgen.New(m.db.Write).ApagarRascunhoVazio(ctx, entityID); err != nil {
		log.Printf("scanner: purga do rascunho substituido %q falhou (ignorado): %v", entityID, err)
	}
}

// ReconciliarSerieDraft tries to reimport a DRAFT series using the title of the
// entity via tentarImportarSerie; success: for each media_file currently pointing
// to that draft series, it re-derives the episode number (naming.ParseEpisode on
// the basename of the path) and matches via tvmatch.Casar against the index of
// the newly imported series, repointing media_file.entity_id to the real series
// and creating the media_file_entity links (same pattern as gravarArquivoSerie:
// RemoverVinculosDoArquivoPorKind + CriarVinculoEntidade,
// kind=kindVinculoEpisodio). A media_file with no matchable episode keeps pointing
// to the real series anyway (just without an episode link -- it falls into the
// review queue of the series, no longer into the queue of the whole series).
// linked=true only when at least 1 media_file was relinked. preferredLang is
// always "" here (no preference for a specific source -- the reconciliation has no
// clear originating library_source per file; the importer falls back to the global
// language, the same behavior of preferredLangDaFonte when there is no
// preference).
func (m *Manager) ReconciliarSerieDraft(ctx context.Context, entityID string, langs []string) (linked bool, err error) {
	entity, err := sqlcgen.New(m.db.Read).ObterEntity(ctx, entityID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	res, ok := m.tentarImportarSerie(ctx, entity.Title, langs, "")
	if !ok {
		return false, nil
	}

	arquivos, err := sqlcgen.New(m.db.Read).ListarMediaFilesPorEntity(ctx, entityID)
	if err != nil {
		return false, err
	}

	religados := 0
	for _, mf := range arquivos {
		if mf.State != "present" {
			continue
		}
		if err := m.religarArquivoSerie(ctx, mf, res); err != nil {
			return religados > 0, err
		}
		religados++
	}
	if religados > 0 {
		// Same purge as the track case: the draft series that lost every file
		// leaves the queue. The query does not delete a series with a linked
		// episode (it checks the link as parent), so what leaves here is the truly
		// empty shell.
		m.purgarRascunhoSubstituido(ctx, entityID)
	}
	return religados > 0, nil
}

// religarArquivoSerie repoints ONE media_file of the draft series to the real
// series (res.serieID), matching the episode when possible (same pattern as
// gravarArquivoSerie in serie.go): it re-derives the episode number from the
// basename of the path and matches against the index already loaded in `res`.
// Without a matchable episode, the file is still repointed to the real series,
// only without a link (it falls into the review queue of the real series, no
// longer into the draft one).
func (m *Manager) religarArquivoSerie(ctx context.Context, mf sqlcgen.MediaFile, res serieResolvida) error {
	confianca := float64(confRevisaoSerie)
	var episodeIDs []string
	if res.index != nil {
		if ep, ok := naming.ParseEpisodeNoCaminho(mf.Path); ok {
			if ids, _, matched := tvmatch.CasarComPasta(res.index, ep, temporadaDaPasta(mf.Path)); matched {
				episodeIDs = ids
				confianca = res.conf
			}
		}
	}

	escrita := sqlcgen.New(m.db.Write)
	if err := escrita.AtualizarMediaFileEntityID(ctx, sqlcgen.AtualizarMediaFileEntityIDParams{
		EntityID:        res.serieID,
		MatchConfidence: nfFloat(confianca),
		ID:              mf.ID,
	}); err != nil {
		return err
	}

	if len(episodeIDs) == 0 {
		return nil
	}

	if err := escrita.RemoverVinculosDoArquivoPorKind(ctx, sqlcgen.RemoverVinculosDoArquivoPorKindParams{
		MediaFileID: mf.ID,
		Kind:        kindVinculoEpisodio,
	}); err != nil {
		return err
	}
	for i, epID := range episodeIDs {
		if err := escrita.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
			MediaFileID: mf.ID,
			EntityID:    epID,
			Kind:        kindVinculoEpisodio,
			Ordinal:     int64(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

// mediaFilePresentePorEntity returns the FIRST 'present' media_file of an entity
// (ADR-0027: 1:1 track-file, but defensive against more than one). ok=false
// without an error when the entity has no present media_file.
func (m *Manager) mediaFilePresentePorEntity(ctx context.Context, entityID string) (sqlcgen.MediaFile, bool, error) {
	arquivos, err := sqlcgen.New(m.db.Read).ListarMediaFilesPorEntity(ctx, entityID)
	if err != nil {
		return sqlcgen.MediaFile{}, false, err
	}
	for _, mf := range arquivos {
		if mf.State == "present" {
			return mf, true, nil
		}
	}
	return sqlcgen.MediaFile{}, false, nil
}

// probarTags re-extracts the audio tags (and the container duration,
// info.General.Duration -- 0 when unavailable) of a path through the Prober
// already injected in the Manager (same path as factsMusica, musica.go). ok=false
// without an error when the probe fails, returns empty, or the JSON does not
// unmarshal -- never a business error (the reconciliation simply cannot try again
// right now); duration 0 in that case, treated as "unknown" by the
// enrich/provider, with no regression.
func (m *Manager) probarTags(ctx context.Context, path string) (mediainfo.AudioTags, float64, bool) {
	mi, err := m.prober.Probe(ctx, path)
	if err != nil || len(mi) == 0 {
		if err != nil {
			log.Printf("scanner: reconciliação: probe de %q falhou: %v", path, err)
		}
		return mediainfo.AudioTags{}, 0, false
	}
	var info mediainfo.Info
	if err := json.Unmarshal(mi, &info); err != nil || info.Tags == nil {
		return mediainfo.AudioTags{}, 0, false
	}
	var duracaoSegundos float64
	if info.General.Duration > 0 {
		duracaoSegundos = info.General.Duration
	}
	return *info.Tags, duracaoSegundos, true
}
