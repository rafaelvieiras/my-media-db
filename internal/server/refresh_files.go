package server

// refresh_files.go implements POST /api/v1/entities/{id}/refresh-files
// (Manage menu > "Check for File Updates"): an ALWAYS visible action (it does
// not depend on catalog_completeness, unlike /download-missing-artwork) that
// rechecks, for the entity open in the detail: (1) it forces a re-hash/mediainfo of
// the entity's 'present' media_file rows, ignoring the short-circuit of the incremental
// scan (scanner.Manager.RecheckEntityFiles); (2) it rechecks the folder of each
// media_file looking for a new/updated local asset
// (scanner.Manager.RefreshLocalAsset — it covers the case that is broken today where
// the entity already exists in the catalog, since descobrirPoster only runs inside
// criarEntity on the 1st sweep); (3) if there is an `.nfo` in entity.nfo_path, it reads
// the LOCAL playback fields (internal/localmedia.ExtrairCamposUsuario,
// the SAME parsing already reused by gravarNFO/localmedia_handlers.go) and fills in
// the database only the ones still NULL
// (PreencherCamposLocaisReproducaoSeAusentes — COALESCE, never overwrites).
// Best-effort by nature (same spirit as the rest of the job system):
// a punctual failure in a substep is logged, it never aborts the call — always
// 200 after the 404 of a nonexistent entity.

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

// refreshFilesResultDTO is the response of POST /entities/{id}/refresh-files
// (RefreshFilesResult in the contract, openapi.yaml) — an informational summary, not
// a rigid contract for another integration to consume.
type refreshFilesResultDTO struct {
	FilesRechecked       int      `json:"files_rechecked"`
	AssetsFound          int      `json:"assets_found"`
	PlaybackFieldsFilled []string `json:"playback_fields_filled"`
}

// refreshEntityFiles handles POST /api/v1/entities/{id}/refresh-files. 404 if
// the entity does not exist; 200 always after that — each substep is
// best-effort (same pattern as enrichMissing/downloadMissingArtwork), never
// 5xx just because "nothing was found to recheck".
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

// recheckEntityFilesPorFonte groups the 'present' media_file rows by
// library_source (an entity may, in theory, have files from different sources) and
// calls scanner.RecheckEntityFiles once per group — same library_source resolution
// pattern of a media_file used in internal/server/retroativo.go
// (idiomaDeBusca/diasReentativaRetroativa). Best-effort: a source that vanished or a
// recheck failure is logged and skipped, it never aborts the call (the action is
// always 200). It returns the total number of files actually re-hashed in this call.
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

// preencherCamposLocaisDoNFO reads the LOCAL playback fields from the `.nfo` already
// on disk (entity.nfo_path) and fills in the database only the ones still empty
// (PreencherCamposLocaisReproducaoSeAusentes — COALESCE, never overwrites). It returns
// the names of the fields that were empty in the database (`ent`, captured BEFORE this
// call) AND that the `.nfo` had a value for — the response reflects what CHANGED, not
// what the `.nfo` contained. Best-effort: no nfo_path, a missing/unreadable file or XML
// that does not parse (ExtrairCamposUsuario already degrades to a zero value) return an
// empty list, they never fail the call.
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

	// Called whenever the .nfo brought SOMETHING (even if everything was already
	// filled in the database -- COALESCE is idempotent/no-op in that case; simpler than
	// deciding here whether it is worth persisting).
	if err := sqlcgen.New(s.db.Write).PreencherCamposLocaisReproducaoSeAusentes(ctx, params); err != nil {
		log.Printf("refresh-files: preencher campos locais de reproducao da entity %q falhou (best-effort, ignorado): %v", ent.ID, err)
		return []string{}
	}
	return preenchidos
}
