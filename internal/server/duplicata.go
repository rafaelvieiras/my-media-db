package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa a REDE DE SEGURANÇA contra duplicata por conflito de
// external_id (briefing 0051): os dois pontos de escrita da identidade externa
// numa entity JÁ EXISTENTE — PUT /entities/{id}/external-ids (0027) e
// POST /entities/{id}/correct (0018) — hoje só gravam o mapa, sem checar se
// aquele id já pertence a OUTRA entity. Quando pertence, resolverColisaoExterna
// decide: sem investimento do dono na outra entity → funde automaticamente
// (some a duplicata); com investimento → responde 409 sem gravar nada.
//
// A colisão só é detectável para fontes com coluna gerada indexada
// (tmdb/imdb/tvdb, ADR-0011/0021) — mesmo escopo do find-or-create do import
// (enrich.acharPorIDExterno). Fontes sem coluna própria não têm como colidir
// hoje (o find-or-create do import também não dedupe nelas).

// headerDuplicataFundida é o header de resposta que sinaliza ao frontend que uma
// duplicata foi fundida (removida) durante esta escrita — o corpo segue sendo a
// entity resolvida normal (responderResolvida), sem novo campo no DTO.
const headerDuplicataFundida = "X-Duplicate-Merged-Id"

// resolverColisaoExterna verifica se `source`+`sourceID` já pertence a outra
// entity do mesmo `tipo` (diferente de `alvoID`). Sem colisão: (false, true) —
// o chamador segue normalmente. Com colisão sem investimento do dono na outra
// entity: funde (a outra desaparece) e devolve (true, true) — o chamador deve
// setar o header headerDuplicataFundida antes de responder. Com investimento:
// escreve o 409 e devolve (false, false) — o chamador aborta sem gravar nada.
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

// acharColisaoExterna procura uma entity do mesmo `tipo`, DIFERENTE de `alvoID`,
// que já tenha `source`=`sourceID` na identidade. achou=false quando a fonte não
// tem coluna gerada (dedup indisponível) ou ninguém mais tem aquele id.
func (s *Server) acharColisaoExterna(ctx context.Context, alvoID, tipo, source, sourceID string) (id, titulo string, achou bool, err error) {
	r := s.leitura()
	sid := nsDe(sourceID)
	var ent sqlcgen.Entity
	switch source {
	case "tmdb":
		ent, err = r.ObterEntityPorTmdb(ctx, sid)
	case "imdb":
		ent, err = r.ObterEntityPorImdb(ctx, sid)
	case "tvdb":
		ent, err = r.ObterEntityPorTvdb(ctx, sid)
	default:
		return "", "", false, nil
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	if ent.ID == alvoID || ent.Type != tipo {
		return "", "", false, nil
	}
	return ent.ID, ent.Title, true, nil
}

// fundirEntities funde `perdedoraID` em `sobreviventeID`: reatribui media_file
// (sempre), media_file_entity/media_external/asset/credit/entity_relation
// (só a parte que a sobrevivente ainda não tem, para não violar UNIQUE/PK — ver
// o comentário no topo de duplicata.sql) e por fim apaga a perdedora (o
// ON DELETE CASCADE limpa o que ficou para trás). A perdedora NUNCA tem
// media_override (resolverColisaoExterna só chama isto sem investimento do
// dono), então media_override não entra aqui.
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

// moverVinculosSemColisao move os vinculos media_file_entity (N:M de serie,
// 0034) da perdedora para a sobrevivente, um a um, pulando qualquer
// (media_file_id, kind) que a sobrevivente ja tenha (evitaria violar a PK
// composta) -- essas linhas ficam para o CASCADE quando a perdedora for apagada.
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

// moverMediaExternalSemColisao move o cache externo (media_external) da
// perdedora para a sobrevivente por fonte, pulando fontes que a sobrevivente ja
// tem cacheadas (evitaria violar UNIQUE(entity_id, source)) -- a duplicada fica
// para o CASCADE.
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

// moverAssetsSemColisao move os assets (poster/fanart/…) da perdedora para a
// sobrevivente por kind, pulando kinds que a sobrevivente ja tem (evita
// empilhar duplicata, ex.: dois posteres) -- o resto fica para o CASCADE.
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

// moverRelacoesSemColisao move as entity_relation (colecao<->filme, serie<->
// temporada<->episodio) em que a perdedora aparece como pai OU filho, pulando
// qualquer (parent,child,kind) que a sobrevivente ja tenha (evitaria violar a
// UNIQUE) -- o resto fica para o CASCADE.
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
