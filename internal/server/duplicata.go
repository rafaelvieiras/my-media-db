package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
)

// This file implements the SAFETY NET against duplicates caused by an external_id
// conflict: the two write points of the external identity on an EXISTING entity — PUT
// /entities/{id}/external-ids and POST /entities/{id}/correct — would otherwise only
// write the map, without checking whether that id already belongs to ANOTHER entity.
// When it does, resolverColisaoExterna decides: without the owner's investment in the
// other entity → merge automatically (the duplicate goes away); with investment → answer
// 409 without writing anything.
//
// The collision is only detectable for sources with an indexed generated column
// (tmdb/imdb/tvdb, ADR-0011/ADR-0021; mbid too, ADR-0027 item 6) — the same scope as the
// import's find-or-create (enrich.acharPorIDExterno for tmdb/imdb/tvdb;
// ObterEntityPorMbid for the music vertical). Sources without a column of their own
// cannot collide today (the import's find-or-create does not dedupe on them either).

// headerDuplicataFundida is the response header that signals to the frontend that a
// duplicate was merged (removed) during this write — the body remains the usual resolved
// entity (responderResolvida), with no new field in the DTO.
const headerDuplicataFundida = "X-Duplicate-Merged-Id"

// resolverColisaoExterna checks whether `source`+`sourceID` already belongs to another
// entity of the same `tipo` (different from `alvoID`). No collision: (false, true) — the
// caller goes on normally. Collision without the owner's investment in the other entity:
// it merges (the other one disappears) and returns (true, true) — the caller must set the
// headerDuplicataFundida header before answering. With investment: it writes the 409 and
// returns (false, false) — the caller aborts without writing anything.
func (s *Server) resolverColisaoExterna(w http.ResponseWriter, ctx context.Context, alvoID, tipo, source, sourceID string) (fundida bool, ok bool) {
	outroID, outroTitulo, achou, err := s.acharColisaoExterna(ctx, alvoID, tipo, source, sourceID)
	if err != nil {
		respondErroInterno(w)
		return false, false
	}
	if !achou {
		return false, true
	}

	temInvestimento, err := sqlcgen.New(s.db.Read).EntityTemInvestimentoDono(ctx, sqlcgen.EntityTemInvestimentoDonoParams{
		EntityID:   outroID,
		EntityID_2: outroID,
	})
	if err != nil {
		respondErroInterno(w)
		return false, false
	}
	if temInvestimento.Bool {
		writeError(w, http.StatusConflict, "conflict_duplicate_identity", fmt.Sprintf(
			"este id já pertence a %q (id %s), que tem edição ou arte própria do dono — funda manualmente antes de aplicar aqui",
			outroTitulo, outroID))
		return false, false
	}

	if err := s.fundirEntities(ctx, alvoID, outroID); err != nil {
		respondErroInterno(w)
		return false, false
	}
	w.Header().Set(headerDuplicataFundida, outroID)
	return true, true
}

// acharColisaoExterna looks for an entity of the same `tipo`, DIFFERENT from `alvoID`,
// that already has `source`=`sourceID` in its identity. achou=false when the source has
// no generated column (dedup unavailable) or nobody else has that id.
//
// It uses :many queries that ALREADY exclude alvoID in the SQL itself
// (ListarEntitiesPor*ExcetoId), instead of a :one followed by a filter in Go. That
// matters when the data is already corrupted by a PRE-EXISTING duplicate (e.g. a
// conflict written before this fix existed, when the generated column external_id_* is
// not UNIQUE — only indexed): if TWO entities already share the same id and the owner
// submits that id again on the entity that already had it, a :one without ORDER BY may
// return the TARGET entity itself, and an `ent.ID == alvoID` filter in Go would discard
// that single result without ever finding the other one — the merge would never fire.
func (s *Server) acharColisaoExterna(ctx context.Context, alvoID, tipo, source, sourceID string) (id, titulo string, achou bool, err error) {
	r := s.leitura()
	sid := nsDe(sourceID)
	var candidatas []sqlcgen.Entity
	switch source {
	case "tmdb":
		candidatas, err = r.ListarEntitiesPorTmdbExcetoId(ctx, sqlcgen.ListarEntitiesPorTmdbExcetoIdParams{
			TmdbID: sid, AlvoID: alvoID,
		})
	case "imdb":
		candidatas, err = r.ListarEntitiesPorImdbExcetoId(ctx, sqlcgen.ListarEntitiesPorImdbExcetoIdParams{
			ImdbID: sid, AlvoID: alvoID,
		})
	case "tvdb":
		candidatas, err = r.ListarEntitiesPorTvdbExcetoId(ctx, sqlcgen.ListarEntitiesPorTvdbExcetoIdParams{
			TvdbID: sid, AlvoID: alvoID,
		})
	case musicext.FonteMusicBrainz, musicext.ChaveExternalIDMbid:
		// Two `source` values reach here for the SAME id: aplicarCandidatoExterno/
		// aplicarIdentidadeExterna pass the Provider.Nome() ("musicbrainz", the convention
		// of ALL the rest of the file), while PUT /entities/{id}/external-ids
		// (external_ids.go) iterates the RAW key the client writes in the JSON ("mbid",
		// the storage convention -- musicext.ChaveExternalIDMbid). Covering only one of the
		// two would leave the other path without collision detection.
		candidatas, err = r.ListarEntitiesPorMbidExcetoId(ctx, sqlcgen.ListarEntitiesPorMbidExcetoIdParams{
			MbidID: sid, AlvoID: alvoID,
		})
	default:
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	for _, ent := range candidatas {
		if ent.Type == tipo {
			return ent.ID, ent.Title, true, nil
		}
	}
	return "", "", false, nil
}

// fundirEntities merges `perdedoraID` into `sobreviventeID`: it reassigns media_file
// (always), media_file_entity/media_external/asset/credit/entity_relation (only the part
// the survivor does not have yet, so UNIQUE/PK is not violated — see the comment at the
// top of duplicata.sql) and finally deletes the loser (the ON DELETE CASCADE cleans up
// what was left behind). The loser NEVER has a media_override (resolverColisaoExterna
// only calls this without the owner's investment), so media_override does not show up
// here.
func (s *Server) fundirEntities(ctx context.Context, sobreviventeID, perdedoraID string) error {
	w := sqlcgen.New(s.db.Write)
	r := s.leitura()

	if err := w.MoverMediaFileParaEntity(ctx, sqlcgen.MoverMediaFileParaEntityParams{
		Sobrevivente: sobreviventeID, Perdedora: perdedoraID,
	}); err != nil {
		return err
	}

	if err := moverVinculosSemColisao(ctx, r, w, sobreviventeID, perdedoraID); err != nil {
		return err
	}
	if err := moverMediaExternalSemColisao(ctx, r, w, sobreviventeID, perdedoraID); err != nil {
		return err
	}
	if err := moverAssetsSemColisao(ctx, r, w, sobreviventeID, perdedoraID); err != nil {
		return err
	}

	creditosSobrevivente, err := r.ListarCreditosDaEntity(ctx, sobreviventeID)
	if err != nil {
		return err
	}
	if len(creditosSobrevivente) == 0 {
		if err := w.MoverTodosCreditsParaEntity(ctx, sqlcgen.MoverTodosCreditsParaEntityParams{
			Sobrevivente: sobreviventeID, Perdedora: perdedoraID,
		}); err != nil {
			return err
		}
	}

	if err := moverRelacoesSemColisao(ctx, r, w, sobreviventeID, perdedoraID); err != nil {
		return err
	}

	return w.ApagarEntity(ctx, perdedoraID)
}

// moverVinculosSemColisao moves the media_file_entity links (the series N:M) from the
// loser to the survivor, one by one, skipping any (media_file_id, kind) the survivor
// already has (it would violate the composite PK) -- those rows are left for the CASCADE
// when the loser is deleted.
func moverVinculosSemColisao(ctx context.Context, r, w *sqlcgen.Queries, sobreviventeID, perdedoraID string) error {
	daPerdedora, err := r.ListarVinculosPorEntidade(ctx, perdedoraID)
	if err != nil || len(daPerdedora) == 0 {
		return err
	}
	daSobrevivente, err := r.ListarVinculosPorEntidade(ctx, sobreviventeID)
	if err != nil {
		return err
	}
	ja := make(map[[2]string]bool, len(daSobrevivente))
	for _, v := range daSobrevivente {
		ja[[2]string{v.MediaFileID, v.Kind}] = true
	}
	for _, v := range daPerdedora {
		if ja[[2]string{v.MediaFileID, v.Kind}] {
			continue
		}
		if err := w.MoverMediaFileEntityLinha(ctx, sqlcgen.MoverMediaFileEntityLinhaParams{
			Sobrevivente: sobreviventeID, MediaFileID: v.MediaFileID, Perdedora: perdedoraID, Kind: v.Kind,
		}); err != nil {
			return err
		}
	}
	return nil
}

// moverMediaExternalSemColisao moves the external cache (media_external) from the loser
// to the survivor per source, skipping sources the survivor already has cached (it would
// violate UNIQUE(entity_id, source)) -- the duplicate is left for the CASCADE.
func moverMediaExternalSemColisao(ctx context.Context, r, w *sqlcgen.Queries, sobreviventeID, perdedoraID string) error {
	daPerdedora, err := r.ListarMediaExternalPorEntity(ctx, perdedoraID)
	if err != nil || len(daPerdedora) == 0 {
		return err
	}
	daSobrevivente, err := r.ListarMediaExternalPorEntity(ctx, sobreviventeID)
	if err != nil {
		return err
	}
	ja := make(map[string]bool, len(daSobrevivente))
	for _, m := range daSobrevivente {
		ja[m.Source] = true
	}
	for _, m := range daPerdedora {
		if ja[m.Source] {
			continue
		}
		if err := w.MoverMediaExternalLinha(ctx, sqlcgen.MoverMediaExternalLinhaParams{
			Sobrevivente: sobreviventeID, ID: m.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// moverAssetsSemColisao moves the assets (poster/fanart/…) from the loser to the
// survivor per kind, skipping kinds the survivor already has (it avoids stacking a
// duplicate, e.g. two posters) -- the rest is left for the CASCADE.
func moverAssetsSemColisao(ctx context.Context, r, w *sqlcgen.Queries, sobreviventeID, perdedoraID string) error {
	daPerdedora, err := r.ListarAssetsPorEntity(ctx, perdedoraID)
	if err != nil || len(daPerdedora) == 0 {
		return err
	}
	daSobrevivente, err := r.ListarAssetsPorEntity(ctx, sobreviventeID)
	if err != nil {
		return err
	}
	ja := make(map[string]bool, len(daSobrevivente))
	for _, a := range daSobrevivente {
		ja[a.Kind] = true
	}
	for _, a := range daPerdedora {
		if ja[a.Kind] {
			continue
		}
		if err := w.MoverAssetLinha(ctx, sqlcgen.MoverAssetLinhaParams{
			Sobrevivente: sobreviventeID, ID: a.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// moverRelacoesSemColisao moves the entity_relation rows (collection<->movie,
// series<->season<->episode) in which the loser appears as parent OR child, skipping any
// (parent,child,kind) the survivor already has (it would violate the UNIQUE) -- the rest
// is left for the CASCADE.
func moverRelacoesSemColisao(ctx context.Context, r, w *sqlcgen.Queries, sobreviventeID, perdedoraID string) error {
	comoPaiPerdedora, err := r.ListarRelacoesFilhas(ctx, perdedoraID)
	if err != nil {
		return err
	}
	comoPaiSobrevivente, err := r.ListarRelacoesFilhas(ctx, sobreviventeID)
	if err != nil {
		return err
	}
	jaPai := make(map[[2]string]bool, len(comoPaiSobrevivente))
	for _, rel := range comoPaiSobrevivente {
		jaPai[[2]string{rel.ChildID, rel.Kind}] = true
	}
	for _, rel := range comoPaiPerdedora {
		if jaPai[[2]string{rel.ChildID, rel.Kind}] {
			continue
		}
		if err := w.MoverEntityRelationParentLinha(ctx, sqlcgen.MoverEntityRelationParentLinhaParams{
			Sobrevivente: sobreviventeID, ID: rel.ID,
		}); err != nil {
			return err
		}
	}

	comoFilhoPerdedora, err := r.ListarRelacoesPais(ctx, perdedoraID)
	if err != nil {
		return err
	}
	comoFilhoSobrevivente, err := r.ListarRelacoesPais(ctx, sobreviventeID)
	if err != nil {
		return err
	}
	jaFilho := make(map[[2]string]bool, len(comoFilhoSobrevivente))
	for _, rel := range comoFilhoSobrevivente {
		jaFilho[[2]string{rel.ParentID, rel.Kind}] = true
	}
	for _, rel := range comoFilhoPerdedora {
		if jaFilho[[2]string{rel.ParentID, rel.Kind}] {
			continue
		}
		if err := w.MoverEntityRelationChildLinha(ctx, sqlcgen.MoverEntityRelationChildLinhaParams{
			Sobrevivente: sobreviventeID, ID: rel.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}
