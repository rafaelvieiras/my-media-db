package scanner

// reparo_arvore.go rebuilds, ONCE, the provisory tree of the series that were scanned before
// the scanner learned to build it (the 16 draft series holding 785 files flat on the series,
// measured in production on 2026-11-23), and removes the PROVISIONAL episodes that the broken
// dedupe left behind (a duplicate per re-scan of any file whose name carries "&", "<" or ">":
// 213 extras, 182 of them in one season).
//
// It is not part of the scan: it exists because the data that predates the fix has to be
// repaired once, and it is written as product code (not a throwaway script) so the same rules
// of the scan are the ones applied here -- same provisorioDe, same acharOuCriarProvisorios,
// same junction. Dry run by default: without `aplicar` it only measures.

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// RelatorioArvore is what the repair measured (dry run) or did (apply).
type RelatorioArvore struct {
	Series              int
	Arquivos            int
	SemNumero           int
	TemporadasCriadas   int
	EpisodiosCriados    int
	VinculosCriados     int
	DuplicadosRemovidos int
}

// RepararArvoreProvisoria rebuilds the provisory tree of every draft series that holds files
// flat on it and removes the duplicated provisional episodes. Idempotent: a second run finds
// the tree it built and removes nothing.
func (m *Manager) RepararArvoreProvisoria(ctx context.Context, aplicar bool) (RelatorioArvore, error) {
	var rel RelatorioArvore
	series, err := m.seriesDraftSemArvore(ctx)
	if err != nil {
		return rel, err
	}
	run := &scanRun{m: m}
	for _, serieID := range series {
		rel.Series++
		if err := m.repararSerie(ctx, run, serieID, aplicar, &rel); err != nil {
			return rel, err
		}
	}
	if err := m.removerProvisoriosDuplicados(ctx, aplicar, &rel); err != nil {
		return rel, err
	}
	return rel, nil
}

// repararSerie builds the provisory seasons/episodes of one draft series and links the files
// that were flat on it.
func (m *Manager) repararSerie(ctx context.Context, run *scanRun, serieID string, aplicar bool, rel *RelatorioArvore) error {
	caminhos, err := m.caminhosDaEntity(ctx, serieID)
	if err != nil {
		return err
	}
	for _, caminho := range caminhos {
		rel.Arquivos++
		prov := provisorioDe(caminho, semExtensao(filepath.Base(caminho)))
		if prov == nil {
			rel.SemNumero++
			continue
		}
		if !aplicar {
			// The dry run counts FILES and the duplicates to remove, never "seasons/episodes
			// that would be created": per-file arithmetic counts one season per file. The
			// number that matters is measured by applying it to a COPY of the database.
			continue
		}
		if err := m.criarProvisoriosELigar(ctx, run, serieID, caminho, *prov, rel); err != nil {
			return err
		}
	}
	return nil
}

// criarProvisoriosELigar does the find-or-create of the season/episode and re-creates the
// junction of the file with the episode it names -- the same two steps gravarArquivoSerie does
// during a scan.
func (m *Manager) criarProvisoriosELigar(ctx context.Context, run *scanRun, serieID, caminho string, prov provisorioEpisodio, rel *RelatorioArvore) error {
	antes := m.contarEntidades(ctx, serieID)
	q := sqlcgen.New(m.db.Write)
	ids, err := run.acharOuCriarProvisorios(ctx, q, serieID, prov)
	if err != nil {
		return err
	}
	depois := m.contarEntidades(ctx, serieID)
	if depois.temporadas > antes.temporadas {
		rel.TemporadasCriadas += depois.temporadas - antes.temporadas
	}
	if depois.episodios > antes.episodios {
		rel.EpisodiosCriados += depois.episodios - antes.episodios
	}
	arquivoID, err := m.mediaFileDaEntityPorCaminho(ctx, serieID, caminho)
	if err != nil || arquivoID == "" {
		return err
	}
	if err := q.RemoverVinculosDoArquivoPorKind(ctx, sqlcgen.RemoverVinculosDoArquivoPorKindParams{
		MediaFileID: arquivoID,
		Kind:        kindVinculoEpisodio,
	}); err != nil {
		return err
	}
	for i, epID := range ids {
		if err := q.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
			MediaFileID: arquivoID,
			EntityID:    epID,
			Kind:        kindVinculoEpisodio,
			Ordinal:     int64(i),
		}); err != nil {
			return err
		}
		rel.VinculosCriados++
	}
	return nil
}

// contagemDeEntidades is the number of seasons and episodes under a series.
type contagemDeEntidades struct{ temporadas, episodios int }

// contarEntidades counts the seasons and episodes of the series (read pool).
func (m *Manager) contarEntidades(ctx context.Context, serieID string) contagemDeEntidades {
	var c contagemDeEntidades
	_ = m.db.Read.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM entity_relation r WHERE r.parent_id = ? AND r.kind = 'tv_season'),
		  (SELECT COUNT(*) FROM entity_relation r
		     JOIN entity_relation t ON t.child_id = r.parent_id AND t.kind = 'tv_season'
		    WHERE t.parent_id = ? AND r.kind = 'tv_episode')`, serieID, serieID).
		Scan(&c.temporadas, &c.episodios)
	return c
}

// seriesDraftSemArvore lists the draft series that hold files and have NO season: the ones the
// old scanner left flat.
func (m *Manager) seriesDraftSemArvore(ctx context.Context) ([]string, error) {
	rows, err := m.db.Read.QueryContext(ctx, `
		SELECT e.id FROM entity e
		 WHERE e.type = 'tv' AND e.status = 'draft'
		   AND EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id)
		   AND NOT EXISTS (SELECT 1 FROM entity_relation r WHERE r.parent_id = e.id AND r.kind = 'tv_season')
		 ORDER BY e.title`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// caminhosDaEntity returns the paths of the files pointing at the entity.
func (m *Manager) caminhosDaEntity(ctx context.Context, entityID string) ([]string, error) {
	rows, err := m.db.Read.QueryContext(ctx,
		`SELECT path FROM media_file WHERE entity_id = ? ORDER BY path`, entityID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var caminhos []string
	for rows.Next() {
		var caminho string
		if err := rows.Scan(&caminho); err != nil {
			return nil, err
		}
		caminhos = append(caminhos, caminho)
	}
	return caminhos, rows.Err()
}

// mediaFileDaEntityPorCaminho returns the media_file id of that path inside the entity.
func (m *Manager) mediaFileDaEntityPorCaminho(ctx context.Context, entityID, caminho string) (string, error) {
	var id string
	err := m.db.Read.QueryRowContext(ctx,
		`SELECT id FROM media_file WHERE entity_id = ? AND path = ?`, entityID, caminho).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// removerProvisoriosDuplicados removes the PROVISIONAL episodes that duplicate another one of
// the same season (same file name in the extension_data) and hold no file at all. It keeps the
// OLDEST of each group -- the one a link, if any ever existed, points at.
func (m *Manager) removerProvisoriosDuplicados(ctx context.Context, aplicar bool, rel *RelatorioArvore) error {
	grupos, err := m.provisoriosDuplicados(ctx)
	if err != nil {
		return err
	}
	for _, ids := range grupos {
		// ids is ordered by created_at: the first one stays.
		sobras := ids[1:]
		rel.DuplicadosRemovidos += len(sobras)
		if !aplicar {
			continue
		}
		q := sqlcgen.New(m.db.Write)
		for _, id := range sobras {
			if err := q.ApagarEntity(ctx, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// provisoriosDuplicados groups, per season, the provisional episodes that carry the same file
// name and no file. Each group comes ordered by creation (oldest first).
func (m *Manager) provisoriosDuplicados(ctx context.Context) (map[string][]string, error) {
	rows, err := m.db.Read.QueryContext(ctx, `
		SELECT r.parent_id, e.id, e.created_at, e.extension_data_json
		  FROM entity e
		  JOIN entity_relation r ON r.child_id = e.id AND r.kind = 'tv_episode'
		 WHERE e.type = 'episode' AND e.status = 'draft'
		   AND NOT EXISTS (SELECT 1 FROM media_file_entity j WHERE j.entity_id = e.id)
		   AND NOT EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id)
		 ORDER BY r.parent_id, e.created_at, e.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	type chave struct{ temporada, arquivo string }
	grupos := make(map[chave][]string)
	for rows.Next() {
		var temporada, id, criado string
		var extensao sql.NullString
		if err := rows.Scan(&temporada, &id, &criado, &extensao); err != nil {
			return nil, err
		}
		arquivo := arquivoDoProvisorio(extensao.String)
		if arquivo == "" {
			continue
		}
		k := chave{temporada: temporada, arquivo: arquivo}
		grupos[k] = append(grupos[k], id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Only the groups with more than one member are duplicates.
	duplicados := make(map[string][]string, len(grupos))
	for k, ids := range grupos {
		if len(ids) < 2 {
			continue
		}
		sort.SliceStable(ids, func(i, j int) bool { return i < j })
		duplicados[k.temporada+"|"+k.arquivo] = ids
	}
	return duplicados, nil
}
