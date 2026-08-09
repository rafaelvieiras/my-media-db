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

// Limiares de confiança (0–100). São TUNING conservador, não modelo (ADR-0008):
// id embutido casa 100%; título+ano é palpite alto; título sem ano é baixo
// (sinalizado); sem título vira draft não-identificado.
const (
	confIDExterno   = 100
	confTituloAno   = 70
	confTituloSoNom = 40
	confDraft       = 0
)

// gravarArquivo aplica o matching e grava/atualiza o media_file. Dedup por
// content_hash: se o mesmo conteúdo já existe, ATUALIZA o path (cobre
// rename/move — idempotente) em vez de criar outra linha.
func (r *scanRun) gravarArquivo(ctx context.Context, q *sqlcgen.Queries, f fileFacts) error {
	if f.isSerie {
		return r.gravarArquivoSerie(ctx, q, f)
	}
	existente, err := q.ObterMediaFilePorContentHash(ctx, nsOpt(f.hash))
	if err == nil {
		return r.reencontrar(ctx, q, existente, f)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// O hash não bateu com nenhuma linha, mas o PATH pode já ter uma (conteúdo
	// trocado no lugar -- reencode/substituição, ou mtime/size do provedor
	// sofreram drift o bastante pra invalidar o short-circuit de
	// mediaFileInalteradoPorPath). Sem esta checagem, o INSERT abaixo bateria no
	// UNIQUE(path) e abortaria o lote inteiro.
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
		r.dispararAutoMatchFilme(ctx, entityID)
	}
	return nil
}

// dispararAutoMatchFilme é o gatilho best-effort do auto-match imediato de
// filme (0089): roda só para entity NOVA nesta passada de scan, DEPOIS do
// media_file já gravado (o auto-match lê título/ano via media_file) -- uma
// falha aqui nunca desfaz a gravação do arquivo. Achando candidato forte o
// bastante e com "materializar artes automaticamente" ligado, materializa na
// hora; sem candidato ou toggle desligado, a entity segue pendente na fila de
// Revisão (ou só enriquecida, sem materializar), como hoje.
func (r *scanRun) dispararAutoMatchFilme(ctx context.Context, entityID string) {
	if r.m.filmeImportador == nil {
		return
	}
	matched, err := r.m.filmeImportador.TentarAutoMatchFilme(ctx, entityID)
	if err != nil {
		log.Printf("scanner: auto-match imediato do filme %q falhou (%v) -- entity permanece pendente na fila de Revisão", entityID, err)
		return
	}
	if !matched || r.m.materializador == nil {
		return
	}
	if err := r.m.materializador.MaterializarSeAutoDownloadLigado(ctx, entityID); err != nil {
		log.Printf("scanner: materialização automática do filme %q falhou (%v)", entityID, err)
	}
}

// reencontrar trata um arquivo já conhecido pelo content_hash: atualiza o path
// (rename/move), size/mtime, volta a present e carimba last_seen. Reaproveita a
// entity existente (mantém o cache coerente para as demais versões da obra).
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

// atualizarConteudoMudou trata um arquivo cujo PATH já era conhecido mas o
// content_hash recém-calculado diverge da linha existente: o conteúdo foi
// substituído no lugar (reencode, arquivo trocado). Atualiza todos os campos
// derivados do conteúdo, preservando id/entity_id (mesma obra, mesmo path).
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

// resolverEntity acha ou cria a Entity da obra. Ordem: cache do scan → id externo
// (tmdb/imdb) → título+ano → cria nova. Sem id e sem título vira draft.
// criada é true SÓ quando o branch que efetivamente criou a entity (criarEntity)
// foi percorrido -- os demais branches (cache do scan, id externo/título+ano já
// existentes) devolvem false. Usado por gravarArquivo (0089) para disparar o
// auto-match imediato só em entity NOVA desta passada de scan.
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

// acharPorIDExterno procura a entity pelo id embutido no nome (tmdb tem
// prioridade sobre imdb). achou=false quando não há id ou não existe ainda.
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

// acharPorTituloAno dedup sem id: reusa a mesma entity para (movie, título,
// release_date). Só quando NÃO há id externo (com id, o lookup acima manda).
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

// criarEntity insere a entity nova (draft quando não-identificada) e, sendo obra
// nova, registra a arte local como asset. Devolve o id criado.
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
	// CriarEntity nasce 'cataloged' (DEFAULT). Rebaixa para 'draft' quando a obra
	// não foi identificada (visível, não perdido — ADR-0012).
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

// classificar decide o título de gravação, o release_date e o status. Sem
// título parseável e sem id, a obra é draft (usa o nome cru como título visível).
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

// confianca pontua o match (0–100) conforme os sinais do nome (ADR-0008).
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

// externalIDsJSON monta o mapa external_ids ({"tmdb":"..","imdb":".."}); "" se
// não há id embutido. Construído à mão (poucos campos) para não puxar json.
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
