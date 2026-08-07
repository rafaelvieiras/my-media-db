package server

// refresh_files.go implementa POST /api/v1/entities/{id}/refresh-files
// (briefing 0088, menu Gerenciar > "Verificar Atualizações de Arquivos"):
// ação SEMPRE visível (não depende de catalog_completeness, ao contrário de
// /download-missing-artwork) que reconfere, para a entity aberta no
// detalhe: (1) força re-hash/mediainfo dos media_file 'present' da entity,
// ignorando o short-circuit do scan incremental
// (scanner.Manager.RecheckEntityFiles); (2) reconfere a pasta de cada
// media_file em busca de um asset local novo/atualizado
// (scanner.Manager.RefreshLocalAsset — cobre o caso hoje quebrado de a
// entity já existir no catálogo, já que descobrirPoster só roda dentro de
// criarEntity na 1ª varredura); (3) se houver `.nfo` em entity.nfo_path, lê
// os campos LOCAIS de reprodução (internal/localmedia.ExtrairCamposUsuario,
// MESMO parsing já reusado por gravarNFO/localmedia_handlers.go) e preenche
// no banco só os que ainda estiverem NULL
// (PreencherCamposLocaisReproducaoSeAusentes — COALESCE, nunca sobrescreve).
// Best-effort por natureza (mesmo espírito do resto do sistema de jobs):
// falha pontual numa subetapa é logada, nunca aborta a chamada — sempre 200
// depois do 404 de entity inexistente.

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
)

// refreshFilesResultDTO é a resposta de POST /entities/{id}/refresh-files
// (RefreshFilesResult no contrato, openapi.yaml) — resumo informativo, não é
// um contrato rígido para outra integração consumir.
type refreshFilesResultDTO struct {
	FilesRechecked       int      `json:"files_rechecked"`
	AssetsFound          int      `json:"assets_found"`
	PlaybackFieldsFilled []string `json:"playback_fields_filled"`
}

// refreshEntityFiles trata POST /api/v1/entities/{id}/refresh-files. 404 se
// a entity não existe; 200 sempre depois disso — cada subetapa é
// best-effort (mesmo padrão de enrichMissing/downloadMissingArtwork), nunca
// 5xx só por "não achou nada para reconferir".
func (s *Server) refreshEntityFiles(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	files, err := s.leitura().ListarMediaFilesPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		if f.State == "present" {
			paths = append(paths, f.Path)
		}
	}

	dto := refreshFilesResultDTO{PlaybackFieldsFilled: []string{}}
	dto.FilesRechecked = s.recheckEntityFilesPorFonte(ctx, files)

	if len(paths) > 0 {
		found, err := s.scanner.RefreshLocalAsset(ctx, id, paths)
		if err != nil {
			log.Printf("refresh-files: descoberta de asset local da entity %q falhou (best-effort, ignorado): %v", id, err)
		} else if found {
			dto.AssetsFound = 1
		}
	}

	dto.PlaybackFieldsFilled = s.preencherCamposLocaisDoNFO(ctx, ent)

	writeJSON(w, http.StatusOK, dto)
}

// recheckEntityFilesPorFonte agrupa os media_file 'present' por
// library_source (uma entity pode, em teoria, ter arquivos de fontes
// diferentes) e chama scanner.RecheckEntityFiles uma vez por grupo — mesmo
// padrão de resolução de library_source de um media_file usado em
// internal/server/retroativo.go (idiomaDeBusca/diasReentativaRetroativa).
// Best-effort: fonte que sumiu ou falha de recheck é logada e pulada, nunca
// aborta a chamada (a ação é sempre 200). Devolve o total de arquivos
// efetivamente re-hasheados nesta chamada.
func (s *Server) recheckEntityFilesPorFonte(ctx context.Context, files []sqlcgen.MediaFile) int {
	porFonte := make(map[string][]string)
	for _, f := range files {
		if f.State != "present" || !f.LibrarySourceID.Valid || f.LibrarySourceID.String == "" {
			continue
		}
		porFonte[f.LibrarySourceID.String] = append(porFonte[f.LibrarySourceID.String], f.Path)
	}

	total := 0
	for srcID, paths := range porFonte {
		src, err := s.library.Obter(ctx, srcID)
		if err != nil {
			log.Printf("refresh-files: library_source %q indisponível (best-effort, ignorado): %v", srcID, err)
			continue
		}
		n, err := s.scanner.RecheckEntityFiles(ctx, src, paths)
		if err != nil {
			log.Printf("refresh-files: recheck de %d arquivo(s) da fonte %q falhou (best-effort, ignorado): %v", len(paths), srcID, err)
		}
		total += n
	}
	return total
}

// preencherCamposLocaisDoNFO lê os campos LOCAIS de reprodução do `.nfo` já
// em disco (entity.nfo_path) e preenche no banco só os que ainda estiverem
// vazios (PreencherCamposLocaisReproducaoSeAusentes — COALESCE, nunca
// sobrescreve, briefing 0088). Devolve os nomes dos campos que estavam
// vazios no banco (`ent`, capturado ANTES desta chamada) E o `.nfo` trazia
// valor — a resposta reflete o que MUDOU, não o que o `.nfo` continha.
// Best-effort: sem nfo_path, arquivo ausente/ilegível ou XML que não
// parseia (ExtrairCamposUsuario já degrada para zero-value) devolvem lista
// vazia, nunca falham a chamada.
func (s *Server) preencherCamposLocaisDoNFO(ctx context.Context, ent sqlcgen.Entity) []string {
	preenchidos := []string{}
	if !ent.NfoPath.Valid || ent.NfoPath.String == "" {
		return preenchidos
	}
	bytes, err := os.ReadFile(ent.NfoPath.String)
	if err != nil {
		return preenchidos
	}
	campos := localmedia.ExtrairCamposUsuario(bytes)
	if campos.Watched == nil && campos.PlayCount == nil && campos.UserRating == nil &&
		campos.LastPlayed == "" && campos.Resume == nil {
		return preenchidos
	}

	params := sqlcgen.PreencherCamposLocaisReproducaoSeAusentesParams{ID: ent.ID}
	if campos.Watched != nil {
		v := int64(0)
		if *campos.Watched {
			v = 1
		}
		params.Watched = sql.NullInt64{Int64: v, Valid: true}
		if !ent.Watched.Valid {
			preenchidos = append(preenchidos, "watched")
		}
	}
	if campos.PlayCount != nil {
		params.PlayCount = sql.NullInt64{Int64: int64(*campos.PlayCount), Valid: true}
		if !ent.PlayCount.Valid {
			preenchidos = append(preenchidos, "play_count")
		}
	}
	if campos.UserRating != nil {
		params.UserRating = sql.NullFloat64{Float64: *campos.UserRating, Valid: true}
		if !ent.UserRating.Valid {
			preenchidos = append(preenchidos, "user_rating")
		}
	}
	if campos.LastPlayed != "" {
		params.LastPlayedAt = sql.NullString{String: campos.LastPlayed, Valid: true}
		if !ent.LastPlayedAt.Valid {
			preenchidos = append(preenchidos, "last_played_at")
		}
	}
	if campos.Resume != nil {
		params.ResumePositionSeconds = sql.NullFloat64{Float64: campos.Resume.Position, Valid: true}
		params.ResumeTotalSeconds = sql.NullFloat64{Float64: campos.Resume.Total, Valid: true}
		if !ent.ResumePositionSeconds.Valid {
			preenchidos = append(preenchidos, "resume_position_seconds")
		}
		if !ent.ResumeTotalSeconds.Valid {
			preenchidos = append(preenchidos, "resume_total_seconds")
		}
	}

	// Chama sempre que o .nfo trouxe ALGO (mesmo que já estivesse tudo
	// preenchido no banco -- COALESCE é idempotente/no-op nesse caso; mais
	// simples que decidir aqui se vale a pena persistir).
	if err := sqlcgen.New(s.db.Write).PreencherCamposLocaisReproducaoSeAusentes(ctx, params); err != nil {
		log.Printf("refresh-files: preencher campos locais de reproducao da entity %q falhou (best-effort, ignorado): %v", ent.ID, err)
		return []string{}
	}
	return preenchidos
}
