package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// This file implements the AUTO-MATCH of the review queue: given a
// low-confidence/draft item, it tries to find the right candidate on its own —
// first in the LOCAL catalog (it links through fundirEntities, never duplicates),
// then in the default EXTERNAL source (applying through the same sequence as the
// "Corrigir" action) — and only applies when the score reaches LimiarAutoMatch.
// Without a strong candidate in any source, the item stays untouched in the queue.
// Single and batch reuse the SAME internal logic (tentarAutoMatch); never
// HTTP-to-HTTP.

// LimiarAutoMatch is an alias of matching.LimiarAutoMatch (the scoring was
// extracted into the leaf package internal/matching, also consumed by
// internal/enrich). Kept here so the callers/tests of this package that already
// reference the local symbol do not break.
const LimiarAutoMatch = matching.LimiarAutoMatch

// autoMatchTargetDTO identifies the applied candidate (local or external) in the
// auto-match response. Year is nil when the source does not report a year.
type autoMatchTargetDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Year  *int   `json:"year"`
}

// autoMatchResultDTO is the response of POST /entities/{id}/auto-match — ALWAYS
// 200 (even without a candidate: that is a valid result, not an error).
type autoMatchResultDTO struct {
	Resolved bool                `json:"resolved"`
	Method   string              `json:"method"` // "local" | "external" | "none"
	Score    float64             `json:"score"`
	Target   *autoMatchTargetDTO `json:"target"`
}

// autoMatchNone is the neutral result: no candidate reached the threshold in any
// source (or there was nothing to search — no file / no derivable title).
var autoMatchNone = autoMatchResultDTO{Resolved: false, Method: "none", Score: 0, Target: nil}

// autoMatchBatchBody is the OPTIONAL body of POST /review/auto-match: the ids to
// process. Omitted/empty processes ALL eligible ones (ListarRevisao, the same
// source as GET /review).
type autoMatchBatchBody struct {
	IDs []string `json:"ids"`
}

// autoMatchBatchItemDTO is the result of ONE item of the batch.
type autoMatchBatchItemDTO struct {
	ID       string  `json:"id"`
	Resolved bool    `json:"resolved"`
	Method   string  `json:"method"`
	Score    float64 `json:"score"`
}

// autoMatchBatchResultDTO is the response of POST /review/auto-match: the
// aggregate summary of the batch.
type autoMatchBatchResultDTO struct {
	ResolvedCount int                     `json:"resolved_count"`
	PendingCount  int                     `json:"pending_count"`
	Items         []autoMatchBatchItemDTO `json:"items"`
}

// autoMatchEntity handles POST /api/v1/entities/{id}/auto-match: tries to resolve
// ONE queue item on its own. 404 if the entity does not exist; 500 only on a REAL
// database error — "no candidate found" is 200 {resolved:false}, never an error.
func (s *Server) autoMatchEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	resultado, err := s.tentarAutoMatch(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, resultado)
}

// autoMatchReview handles POST /api/v1/review/auto-match: the BATCH. Optional
// `{"ids":[...]}` body — omitted/empty processes ALL eligible ones through
// ListarRevisao (the same query as GET /review). Each id runs the SAME internal
// logic as the single endpoint (tentarAutoMatch); an id that vanished between the
// listing and the processing (concurrent purge) is skipped, it does not bring the
// batch down.
func (s *Server) autoMatchReview(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()

	var body autoMatchBatchBody
	if ok := decodeOpcional(w, r, &body); !ok {
		return
	}

	ids := body.IDs
	if len(ids) == 0 {
		listados, err := s.idsElegiveisAutoMatch(ctx)
		if err != nil {
			respondErroInterno(w)
			return
		}
		ids = listados
	}

	writeJSON(w, http.StatusOK, s.autoMatchLote(ctx, ids, execqueue.RegistroNulo()))
}

// idsElegiveisAutoMatch lists the ids of the Review queue eligible for batch
// auto-match (the same query as GET /review, limiarBaixaConfianca) -- reused by
// autoMatchReview (empty body) and by AutoMatchElegiveis (health job), so the
// query is not duplicated in two places.
func (s *Server) idsElegiveisAutoMatch(ctx context.Context) ([]string, error) {
	rows, err := s.leitura().ListarRevisao(ctx, sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// autoMatchLote runs tentarAutoMatch for each id of `ids`, aggregating the result --
// the SAME logic used by autoMatchReview (HTTP) and by AutoMatchElegiveis (health
// job). An id that vanished between the listing and the processing (concurrent purge)
// is skipped, it does not bring the batch down; a real database error on THIS item
// becomes "pending" in the result, without aborting the others.
// registro is where each item's outcome is narrated (ADR-0040): the health job
// passes the registrar of the queue item the owner is watching, the HTTP batch
// endpoint passes a no-op one (the response already carries the per-item result).
func (s *Server) autoMatchLote(ctx context.Context, ids []string, registro execqueue.Registro) autoMatchBatchResultDTO {
	items := make([]autoMatchBatchItemDTO, 0, len(ids))
	resolvedCount := 0
	for i, id := range ids {
		registro.Progresso(execqueue.Progresso{Atual: i, Total: len(ids)})
		ent, err := s.leitura().ObterEntity(ctx, id)
		if err != nil {
			continue // vanished or invalid id in the body -- idempotent, skip it
		}
		registro.Progresso(execqueue.Progresso{Item: ent.Title})
		resultado, err := s.tentarAutoMatch(ctx, ent)
		if err != nil {
			// real database error on THIS item: record it as pending and go on with
			// the batch -- one bad item should not bring down the other 29.
			items = append(items, autoMatchBatchItemDTO{ID: id, Resolved: false, Method: "none", Score: 0})
			registro.Evento(execqueue.Evento{Status: execqueue.EventoErro, Label: ent.Title, Detail: err.Error()})
			continue
		}
		if resultado.Resolved {
			resolvedCount++
			registro.Evento(execqueue.Evento{Status: execqueue.EventoOK, Label: ent.Title, Detail: resultado.Method})
		} else {
			// Not resolved: stays in the queue for the owner. Recorded as a skip
			// so the trail answers "what did it leave behind" without counting
			// it as a failure.
			registro.Evento(execqueue.Evento{Status: execqueue.EventoSkip, Label: ent.Title, Detail: "sem candidato acima do limiar"})
		}
		items = append(items, autoMatchBatchItemDTO{
			ID: id, Resolved: resultado.Resolved, Method: resultado.Method, Score: resultado.Score,
		})
	}

	return autoMatchBatchResultDTO{
		ResolvedCount: resolvedCount,
		PendingCount:  len(items) - resolvedCount,
		Items:         items,
	}
}

// AutoMatchElegiveis runs the batch auto-match over ALL the eligible items of the
// Review queue (the same query as POST /review/auto-match without a body) --
// consumed by the saude_biblioteca job through the libraryhealth.Matcher interface,
// which *Server satisfies structurally (without libraryhealth importing
// internal/server). err is only a real database error in the LISTING; a failure on
// an individual item is already absorbed by autoMatchLote.
func (s *Server) AutoMatchElegiveis(ctx context.Context, registro execqueue.Registro) (resolved, pending int, err error) {
	ids, err := s.idsElegiveisAutoMatch(ctx)
	if err != nil {
		return 0, 0, err
	}
	resultado := s.autoMatchLote(ctx, ids, registro)
	return resultado.ResolvedCount, resultado.PendingCount, nil
}

// ── Shared orchestration ─────────────────────────────────────────────────────

// tentarAutoMatch tries to resolve `ent` on its own: derives title/year from the file,
// searches local-first, rescores and applies (fundirEntities) if it reaches the
// threshold; otherwise searches externally, rescores and applies through the
// "Corrigir" sequence if it reaches the threshold; otherwise returns autoMatchNone.
// The returned err is ONLY a real database/infra error — "no candidate" is never err.
//
// The helpers coupled to HTTP writing (resolverColisaoExterna, idiomasPermitidos) are
// called with a throwaway capturaResposta, NEVER with the request's real
// ResponseWriter: a collision that requires a decision from the owner (409, the safety
// net for merges) must not be applied blindly by the auto-match — the item stays
// pending in the queue, like any candidate that did not reach the threshold.
func (s *Server) tentarAutoMatch(ctx context.Context, ent sqlcgen.Entity) (autoMatchResultDTO, error) {
	variantes, err := s.variantesTituloAno(ctx, ent)
	if err != nil {
		return autoMatchResultDTO{}, err
	}
	if len(variantes) == 0 {
		return autoMatchNone, nil
	}

	// Search language: the entity's origin library may have a preferred language;
	// without one, it falls back to the global MAIN language (never the blind ""
	// used before).
	lang, err := s.idiomaDeBusca(ctx, ent.ID)
	if err != nil {
		return autoMatchResultDTO{}, err
	}

	// 1) Local-first: never duplicates an entity already in the catalog. The finder
	// returns the HIGHEST-scored non-discarded one regardless of threshold (to also
	// serve the preview without a cut) -- the >= LimiarAutoMatch cut is decided HERE,
	// by the caller.
	alvo, achou, err := s.melhorCandidatoLocalEntre(ctx, ent, variantes)
	if err != nil {
		return autoMatchResultDTO{}, err
	}
	if achou && alvo.Score >= LimiarAutoMatch {
		if err := s.fundirEntities(ctx, alvo.ID, ent.ID); err != nil {
			return autoMatchResultDTO{}, err
		}
		return autoMatchResultDTO{
			Resolved: true, Method: "local", Score: alvo.Score,
			Target: &autoMatchTargetDTO{ID: alvo.ID, Title: alvo.Title, Year: alvo.Year},
		}, nil
	}

	// 2) External: only when local did not reach it. With no provider capable of the
	// type, or with enrich off (no assetsDir), there is no way to apply -- degrades to
	// "none".
	if s.enrich == nil {
		return autoMatchNone, nil
	}
	buscado, err := s.melhorCandidatoExternoEntre(ctx, ent, variantes, lang)
	if err != nil {
		return autoMatchResultDTO{}, err
	}
	// Same reasoning as the local one: the finder returns the highest-scored
	// non-discarded candidate without a cut; the >= LimiarAutoMatch cut is decided
	// here. TituloVencedor does not interest the auto-match (only the preview,
	// match_preview.go).
	if !buscado.Disponivel || !buscado.Achou || buscado.Score < LimiarAutoMatch {
		return autoMatchNone, nil
	}

	return s.aplicarCandidatoExterno(ctx, ent, buscado.Candidato, buscado.Score)
}

// aplicarCandidatoExterno applies ONE already chosen external candidate (score >=
// LimiarAutoMatch in tentarAutoMatch, or >= LimiarAutoMatch in
// tentarEnriquecerOuSinalizar) through the SAME sequence as the "Corrigir" action:
// resolverColisaoExterna → s.enrich.Enriquecer → finalizarRevisao. Extracted from the
// body that used to be inline in tentarAutoMatch so both flows (review queue and
// retroactive enrichment) can reuse it without duplicating the application sequence
// in two files — the single external application point of the package. The returned
// err is ONLY a real database/infra error; a collision with the owner's investment
// degrades to autoMatchNone (pending), never applies blindly.
func (s *Server) aplicarCandidatoExterno(ctx context.Context, ent sqlcgen.Entity, cand provider.ResultadoExterno, score float64) (autoMatchResultDTO, error) {
	captura := &capturaResposta{}
	if _, ok := s.resolverColisaoExterna(captura, ctx, ent.ID, ent.Type, cand.Source, cand.SourceID); !ok {
		if captura.status != http.StatusConflict {
			return autoMatchResultDTO{}, errStatus(captura.status)
		}
		// Collision with the owner's investment on the other entity: do not apply
		// blindly, the item stays pending for manual resolution.
		return autoMatchNone, nil
	}

	langs, err := s.idiomasPermitidos(ctx, captura)
	if err != nil {
		return autoMatchResultDTO{}, err
	}
	if err := s.enrich.Enriquecer(ctx, ent.ID, cand.Source, cand.SourceID, ent.Type, langs, false); err != nil {
		// The source failed to deliver the detail -- that is not an error of the
		// auto-match itself (the source may recover later); the item stays pending.
		return autoMatchNone, nil
	}
	if err := s.finalizarRevisao(ctx, ent.ID); err != nil {
		return autoMatchResultDTO{}, err
	}

	var anoAlvo *int
	if a := anoDeStringPtr(cand.Year); a > 0 {
		anoAlvo = &a
	}
	return autoMatchResultDTO{
		Resolved: true, Method: "external", Score: score,
		Target: &autoMatchTargetDTO{ID: ent.ID, Title: cand.Title, Year: anoAlvo},
	}, nil
}

// consultaTituloAno is ONE search attempt: the text to send to the finders and the year
// to score/qualify it with (0 = unknown year, matching.ajustarPorAno already degrades
// without a bonus and without a veto).
type consultaTituloAno struct {
	Titulo string
	Ano    int
}

// variantesTituloAno lists the search attempts for `ent`, in PREFERENCE order and
// without duplicate titles.
//
// A movie is searched TWICE: first with the CATALOG title (entity.title, the folder
// name the owner curated), then with the one derived from the file name (the release
// name). The scan gives a movie two very different strings -- the folder
// ("Alligator 2 A Mutação") and the file ("Alligator2.91.BD1080p.MemoriadaTV.Maior") --
// and the provider only recognizes the first one. Measured on a production library:
// of 25 random movies without any external id, 21 reach LimiarAutoMatch when searched by
// the catalog title, against ~0 when searched by the release name (the release name
// "Alligator2 91 BD1080p MemoriadaTV Maior" returns ZERO candidates, while "Alligator 2
// A Mutação" returns the right movie as the first result with score 100).
//
// The other types keep the single term they always had: a series already prefers its
// FOLDER (tituloAnoDaPastaDaSerie, with the entity title as fallback) and never had the
// movie's problem.
//
// An entity with NO file at all has nothing to search (empty result, not an error): that
// is the old contract of tituloAnoDoArquivo and it keeps the auto-match away from manual
// drafts and orphans.
func (s *Server) variantesTituloAno(ctx context.Context, ent sqlcgen.Entity) ([]consultaTituloAno, error) {
	files, err := s.leitura().ListarMediaFilesPorEntity(ctx, ent.ID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	var out []consultaTituloAno
	if ent.Type == tipoEntFilme {
		// The FOLDER comes first, because in the Kodi convention -- the one this project
		// follows and every player reads -- the folder IS the work and its name is more
		// trustworthy than the file name inside it. The entity title is only a cleaned copy
		// of it, so the raw name still carries information the copy lost (the subtitle after
		// the dash, the brackets).
		out = append(out, s.variantesDaPastaDoFilme(ctx, ent, files[0])...)
		if t := strings.TrimSpace(ent.Title); t != "" {
			out = append(out, consultaTituloAno{Titulo: t, Ano: anoDeReleaseDate(ent.ReleaseDate)})
		}
	}
	if t, a, ok := s.tituloAnoDoArquivo(ctx, ent, files); ok {
		out = append(out, consultaTituloAno{Titulo: t, Ano: a})
	}
	return dedupeConsultas(comVariantesSemEnfeite(out)), nil
}

// variantesDaPastaDoFilme derives the search attempts from the NAME OF THE FOLDER the movie
// sits in -- the authoritative name of the work, ahead of the release name of any file.
//
// It gives two attempts: the folder as the convention writes it ("101 Dálmatas - O Filme",
// year from "(1996)") and, when there is a subtitle after the dash, the title alone
// ("101 Dálmatas"). Sources index the title, so the second one is the attempt that answers
// most of the time; the first is kept because a folder WITHOUT a subtitle is exactly the
// same string as the title, and because the provider may know the longer form.
//
// Nothing is derived when the file sits loose at the root of the library: the folder there is
// the library itself, and searching a provider for "Filmes" would be a wildcard.
func (s *Server) variantesDaPastaDoFilme(ctx context.Context, ent sqlcgen.Entity, arquivo sqlcgen.MediaFile) []consultaTituloAno {
	pasta := filepath.Dir(arquivo.Path)
	if s.ehRaizDaFonte(ctx, arquivo, pasta) {
		return nil
	}
	base := filepath.Base(pasta)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return nil
	}
	info := naming.Parse(base)
	ano := info.Year
	if ano == 0 {
		ano = anoDeReleaseDate(ent.ReleaseDate)
	}
	var out []consultaTituloAno
	if info.Title != "" {
		out = append(out, consultaTituloAno{Titulo: info.Title, Ano: ano})
	}
	if semSub := naming.TituloSemSubtitulo(base); semSub != "" {
		out = append(out, consultaTituloAno{Titulo: semSub, Ano: ano})
	}
	return out
}

// comVariantesSemEnfeite puts, right after each attempt, the SAME attempt without the trailing
// filler of the title ("101 Dálmatas O Filme" -> "101 Dálmatas", "Mortal Kombat O Filme" ->
// "Mortal Kombat"). It never comes BEFORE its literal: the title as it is on disk is always
// tried first, and the variant only costs a provider call when that one failed.
//
// MEASURED against TMDb, the case that motivated it: "101 Dálmatas O Filme" returns ZERO
// results while "101 Dálmatas" returns the right movie as its first result. It is the same
// folder-naming habit as the typo of ADR-0035, only with a genre tag instead of a misspelled
// word -- and, unlike the typo, the variant is cheap and deterministic, so it can run in the
// automatic path instead of waiting for the owner to open the item.
func comVariantesSemEnfeite(consultas []consultaTituloAno) []consultaTituloAno {
	out := make([]consultaTituloAno, 0, 2*len(consultas))
	for _, c := range consultas {
		out = append(out, c)
		if sem := naming.SemEnfeite(c.Titulo); sem != c.Titulo {
			out = append(out, consultaTituloAno{Titulo: sem, Ano: c.Ano})
		}
	}
	return out
}

// dedupeConsultas drops the attempts whose title repeats a previous one once normalized
// ("Alligator 2: A Mutação" and "Alligator 2 A Mutação" are the SAME search): the
// second attempt would spend a provider call to score the same candidate list against
// the same title. An attempt with an empty normalized title is dropped as well -- there
// is nothing to search.
func dedupeConsultas(in []consultaTituloAno) []consultaTituloAno {
	vistos := make(map[string]bool, len(in))
	out := make([]consultaTituloAno, 0, len(in))
	for _, c := range in {
		chave := matching.NormalizarNome(c.Titulo)
		if chave == "" || vistos[chave] {
			continue
		}
		vistos[chave] = true
		out = append(out, c)
	}
	return out
}

// tituloAnoDoArquivo derives title/year from the FIRST file linked to the entity via
// naming.Parse (the SAME parser used by the scan) — it does not trust the raw
// entity.title of a draft. ok=false when the name yields no title (e.g. "2160p.mkv"):
// in that case there is nothing to search, and that is NOT a failure of the auto-match.
// The caller has already listed the files (variantesTituloAno), so this function does no
// I/O beyond the folder check of the series branch.
//
// SERIES is the special case: the first file of a series is an EPISODE, and its name
// carries SxxEyy and the episode title -- "Os gárgulas - S03E08 - Genesis Undone"
// becomes similarity query 58 against "Gárgulas", far below LimiarAutoMatch, and the
// provider does not even return a candidate. The title the search needs is in the
// series FOLDER ("Os Gárgulas (1994)"), which is the same string the scan used to name
// the entity, only with the year.
func (s *Server) tituloAnoDoArquivo(ctx context.Context, ent sqlcgen.Entity, files []sqlcgen.MediaFile) (titulo string, ano int, ok bool) {
	if ent.Type == tipoEntSerie {
		if t, a, achou := s.tituloAnoDaPastaDaSerie(ctx, ent, files[0]); achou {
			return t, a, true
		}
	}
	nome := strings.TrimSuffix(filepath.Base(files[0].Path), filepath.Ext(files[0].Path))
	info := naming.Parse(nome)
	if info.Title == "" {
		return "", 0, false
	}
	return info.Title, info.Year, true
}

// tituloAnoDaPastaDaSerie derives title/year from the series FOLDER name, going one
// level up when the file sits in a season folder (naming.PastaDaSerie). achou=false when
// the derived folder is the source ROOT (a file loose in the library: querying the
// provider with the source name, like "Series", would match the wrong title) and the
// entity does not give a title either -- in that case the caller moves on to the
// derivation from the file name, as it always did.
func (s *Server) tituloAnoDaPastaDaSerie(ctx context.Context, ent sqlcgen.Entity, arquivo sqlcgen.MediaFile) (titulo string, ano int, achou bool) {
	pasta := naming.PastaDaSerie(arquivo.Path)
	if !s.ehRaizDaFonte(ctx, arquivo, pasta) {
		if info := naming.Parse(filepath.Base(pasta)); info.Title != "" {
			return info.Title, info.Year, true
		}
	}
	// No usable folder: the title the scan gave the series still works (without the
	// year), and it is better than querying the provider with the episode name.
	if t := strings.TrimSpace(ent.Title); t != "" {
		return t, anoDeReleaseDate(ent.ReleaseDate), true
	}
	return "", 0, false
}

// ehRaizDaFonte tells whether `pasta` is the root of the file's library_source -- the
// "file loose in the library" case, where the folder identifies no series at all.
// Without a library_source_id (legacy file) or on a read failure it degrades to "not a
// root": the worst case is querying the immediate folder, as was already done.
func (s *Server) ehRaizDaFonte(ctx context.Context, arquivo sqlcgen.MediaFile, pasta string) bool {
	if !arquivo.LibrarySourceID.Valid {
		return false
	}
	src, err := s.leitura().ObterLibrarySource(ctx, arquivo.LibrarySourceID.String)
	if err != nil {
		return false
	}
	return filepath.Clean(src.Path) == filepath.Clean(pasta)
}

// candidatoPontuado is the best local candidate found, already with the final score
// (0-100) and the fields to compose autoMatchTargetDTO/matchPreviewDTO.
// PosterURL comes for free from the local search card (entityCardDTO.PosterURL) -- no
// extra call.
type candidatoPontuado struct {
	ID        string
	Title     string
	Year      *int
	Score     float64
	PosterURL *string
}

// melhorCandidatoLocal rescores the shortlist of the LOCAL search (buscarLocal — the
// FTS5 BM25 is only a pre-filter, never the final score) and ALWAYS returns the
// candidate with the HIGHEST score among the non-discarded ones -- the
// LimiarAutoMatch cut is no longer made here, but by the CALLER (tentarAutoMatch
// applies the cut; the match preview, matchPreviewDTO, applies no cut at all and only
// flags meets_auto_threshold). It ignores `selfID` (the queue item itself may show up
// in the shortlist when the title already matches in the FTS). A candidate whose year
// diverges by more than 1 year (matching.Pontuar descartado=true) never enters the
// ranking -- that structural disqualification still always applies. ok=false when no
// non-discarded candidate exists.
func (s *Server) melhorCandidatoLocal(ctx context.Context, candidatos []entityCardDTO, selfID, titulo string, ano int) (candidatoPontuado, bool) {
	var melhor candidatoPontuado
	achou := false
	for _, c := range candidatos {
		if c.ID == selfID {
			continue
		}
		anoCand := s.anoDaEntity(ctx, c.ID)
		pontos, descartado := matching.Pontuar(titulo, ano, c.Title, anoCand)
		if descartado {
			continue
		}
		if !achou || pontos > melhor.Score {
			melhor = candidatoPontuado{ID: c.ID, Title: c.Title, Score: pontos, PosterURL: c.PosterURL}
			if anoCand > 0 {
				a := anoCand
				melhor.Year = &a
			}
			achou = true
		}
	}
	return melhor, achou
}

// candidatoExternoPontuado is an external candidate already scored by main
// title (matching.Pontuar) -- step 1 of melhorCandidatoExterno, before the rescoring by
// alternative titles of the top-3.
type candidatoExternoPontuado struct {
	cand  provider.ResultadoExterno
	score float64
}

// candidatosAlternativosTopN is the ceiling of candidates that trigger an alternative
// titles lookup: controlled cost, 1 extra network call per candidate -- the right
// candidate is almost always among the top-3 by main-title score.
const candidatosAlternativosTopN = 3

// titulosDoCandidato returns the extra titles the search itself already delivered for a
// candidate (today only the original-language title). Kept as a function so the callers
// have ONE place to grow the set without touching the scoring call.
func titulosDoCandidato(c provider.ResultadoExterno) []string {
	if c.TituloOriginal == "" {
		return nil
	}
	return []string{c.TituloOriginal}
}

// melhorCandidatoExterno rescores the results of the EXTERNAL search (buscarExterno,
// already merged/deduplicated by source) and ALWAYS returns the one with the HIGHEST
// score among the non-discarded ones -- the same reasoning as melhorCandidatoLocal: the
// LimiarAutoMatch cut is the CALLER's decision, not this finder's. achou=false when no
// non-discarded candidate exists.
//
// The scoring goes through TWO stages: (1) single title (matching.Pontuar) for ALL the
// candidates, ordered by score desc; (2) the TOP-3 (candidatosAlternativosTopN) are
// rescored with matching.PontuarComAlternativos against the alternative/translated
// titles the provider knows (OPTIONAL capability provider.TituloAlternativoProvider,
// best-effort — a failure/absence degrades to that candidate's single-title score,
// without aborting the function). The final winner is the highest score among the N
// candidates of stage 1 and the 3 rescored ones of stage 2. tituloVencedor is the
// ALTERNATIVE title that produced the winner's score, when it beat the candidate's own
// main title; "" when the winner did not go through the rescoring (outside the top-3)
// or when the main title was already the best.
func (s *Server) melhorCandidatoExterno(ctx context.Context, candidatos []provider.ResultadoExterno, titulo string, ano int) (provider.ResultadoExterno, float64, string, bool) {
	// The year slack comes from the TYPE of the candidates: a series folder states a year more
	// loosely than a movie folder does ("Clube do Terror (1990)" is the 1992 show), so a 1-year
	// gate would discard the only correct candidate and let the remake win -- see ADR-0046.
	toleranciaAno := matching.ToleranciaAnoPadrao
	if len(candidatos) > 0 && candidatos[0].Type == tipoEntSerie {
		toleranciaAno = matching.ToleranciaAnoSerie
	}
	pontuados := make([]candidatoExternoPontuado, 0, len(candidatos))
	for _, c := range candidatos {
		anoCand := anoDeStringPtr(c.Year)
		// Stage 1 also compares against the ORIGINAL title the source reports for free in
		// the search payload. Without it the candidate whose displayed title is the
		// translation ("Crimes do Colarinho Branco" for "White Collar") scores ~0, never
		// reaches the top-3 of stage 2, and loses to a longer title that merely starts
		// with the query ("White Collar Blue", 94.1) -- a wrong match ABOVE
		// LimiarAutoMatch, i.e. silently cataloged without review.
		alternativos := titulosDoCandidato(c)
		pontos, _, descartado := matching.PontuarComAlternativosTolerancia(titulo, ano, c.Title, alternativos, anoCand, toleranciaAno)
		if descartado {
			continue
		}
		pontuados = append(pontuados, candidatoExternoPontuado{cand: c, score: pontos})
	}
	if len(pontuados) == 0 {
		return provider.ResultadoExterno{}, 0, "", false
	}
	sort.SliceStable(pontuados, func(i, j int) bool { return pontuados[i].score > pontuados[j].score })

	melhor := pontuados[0]
	tituloVencedor := ""

	topN := candidatosAlternativosTopN
	if topN > len(pontuados) {
		topN = len(pontuados)
	}
	for i := 0; i < topN; i++ {
		p := pontuados[i]
		alternativos := s.titulosAlternativosDoCandidato(ctx, p.cand)
		if len(alternativos) == 0 {
			continue
		}
		anoCand := anoDeStringPtr(p.cand.Year)
		repontuado, vencedor, descartado := matching.PontuarComAlternativosTolerancia(titulo, ano, p.cand.Title, alternativos, anoCand, toleranciaAno)
		if descartado || repontuado <= melhor.score {
			continue
		}
		melhor = candidatoExternoPontuado{cand: p.cand, score: repontuado}
		if vencedor != p.cand.Title {
			tituloVencedor = vencedor
		} else {
			tituloVencedor = ""
		}
	}
	return melhor.cand, melhor.score, tituloVencedor, true
}

// melhorCandidatoLocalEntre walks the search variants IN ORDER and STOPS at the first one
// that reaches LimiarAutoMatch -- the second search then costs nothing. Below the
// threshold it returns the BEST candidate among ALL variants, because the callers also
// need the real score to decide "flag" (retroativo.go) and to display it
// (match_preview.go).
//
// Wrapping the single-term finder (melhorCandidatoLocal) instead of duplicating it keeps
// the ranking/threshold logic in exactly one place.
func (s *Server) melhorCandidatoLocalEntre(ctx context.Context, ent sqlcgen.Entity, variantes []consultaTituloAno) (candidatoPontuado, bool, error) {
	var melhor candidatoPontuado
	achou := false
	for _, v := range variantes {
		candidatos, err := s.buscarLocal(ctx, v.Titulo, ent.Type, nil)
		if err != nil {
			return candidatoPontuado{}, false, err
		}
		alvo, ok := s.melhorCandidatoLocal(ctx, candidatos, ent.ID, v.Titulo, v.Ano)
		if ok && (!achou || alvo.Score > melhor.Score) {
			melhor, achou = alvo, true
		}
		if achou && melhor.Score >= LimiarAutoMatch {
			return melhor, true, nil
		}
	}
	return melhor, achou, nil
}

// candidatoExternoBusca is the outcome of the EXTERNAL search over every variant. Achou
// says whether any variant produced a non-discarded candidate; Disponivel whether at
// least one source answered at all (the caller's "external:[]/available:false"
// degradation depends on that distinction, and with several variants "all sources failed
// for one variant" is not the same as "all sources failed").
type candidatoExternoBusca struct {
	Candidato      provider.ResultadoExterno
	TituloVencedor string
	Score          float64
	Achou          bool
	Disponivel     bool
}

// melhorCandidatoExternoEntre is the external counterpart of melhorCandidatoLocalEntre:
// it walks the variants IN ORDER, stops at the first one that reaches LimiarAutoMatch and
// otherwise returns the best-scored candidate among all of them. A variant whose sources
// ALL failed (not merely "returned nothing") does not abort the walk -- the next variant
// still gets its chance, and Disponivel records that some source did answer.
func (s *Server) melhorCandidatoExternoEntre(ctx context.Context, ent sqlcgen.Entity, variantes []consultaTituloAno, lang string) (candidatoExternoBusca, error) {
	var melhor candidatoExternoBusca
	for _, v := range variantes {
		externos, disponivel := s.buscarExterno(ctx, v.Titulo, ent.Type, lang)
		if !disponivel {
			continue
		}
		melhor.Disponivel = true
		cand, score, tituloVencedor, achou := s.melhorCandidatoExterno(ctx, externos, v.Titulo, v.Ano)
		if !achou {
			continue
		}
		if !melhor.Achou || score > melhor.Score {
			melhor = candidatoExternoBusca{
				Candidato:      cand,
				TituloVencedor: tituloVencedor,
				Score:          score,
				Achou:          true,
				Disponivel:     true,
			}
		}
		if melhor.Score >= LimiarAutoMatch {
			return melhor, nil
		}
	}
	return melhor, nil
}

// titulosAlternativosDoCandidato fetches the alternative/translated titles that the
// provider of `cand`'s source knows (OPTIONAL capability
// provider.TituloAlternativoProvider, type-assert -- sources without that capability,
// or without a provider registered under that name, silently return nil). The result is
// cached in memory by (source,source_id), with the same TTL as cacheExterno
// (ttlCacheExterno) -- two lookups of the SAME candidate within the TTL do not trigger
// a new call. A real network error is BEST-EFFORT: it logs and returns nil (the caller
// rescores with the main title only).
func (s *Server) titulosAlternativosDoCandidato(ctx context.Context, cand provider.ResultadoExterno) []string {
	if s.cacheTitulosAlt == nil {
		return nil
	}
	chave := cand.Source + "\x00" + cand.SourceID
	if v, ok := s.cacheTitulosAlt.obter(chave); ok {
		return v
	}
	prov, ok := s.providers.Get(cand.Source)
	if !ok {
		return nil
	}
	tap, ok := prov.(provider.TituloAlternativoProvider)
	if !ok {
		return nil
	}
	titulos, err := tap.BuscarTitulosAlternativos(ctx, cand.Type, cand.SourceID)
	if err != nil {
		log.Printf("automatch: titulos alternativos de %s %s/%s falharam (best-effort, segue so titulo principal): %v",
			cand.Type, cand.Source, cand.SourceID, err)
		return nil
	}
	s.cacheTitulosAlt.guardar(chave, titulos)
	return titulos
}

// ── alternative titles cache ─────────────────────────────────────────────────

// cacheTitulosAlternativos is a mutex-protected in-memory cache of the alternative
// titles of an external candidate by key (source+"\x00"+source_id), with TTL
// expiration -- the same design as cacheExterno (search.go), reused here so a second
// cache pattern is not invented inside the same package.
type cacheTitulosAlternativos struct {
	mu    sync.Mutex
	ttl   time.Duration
	itens map[string]entradaCacheTitulosAlternativos
	agora func() time.Time
}

// entradaCacheTitulosAlternativos is one cache entry: the titles and the expiration
// instant.
type entradaCacheTitulosAlternativos struct {
	titulos []string
	expira  time.Time
}

// novoCacheTitulosAlternativos creates a cache with the given TTL.
func novoCacheTitulosAlternativos(ttl time.Duration) *cacheTitulosAlternativos {
	return &cacheTitulosAlternativos{ttl: ttl, itens: make(map[string]entradaCacheTitulosAlternativos), agora: time.Now}
}

// obter returns the cached titles and true when the key exists and has not expired;
// otherwise (nil, false).
func (c *cacheTitulosAlternativos) obter(chave string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.itens[chave]
	if !ok || c.agora().After(e.expira) {
		return nil, false
	}
	return e.titulos, true
}

// guardar writes (or overwrites) the titles of the key with a new expiration.
func (c *cacheTitulosAlternativos) guardar(chave string, titulos []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.itens[chave] = entradaCacheTitulosAlternativos{titulos: titulos, expira: c.agora().Add(c.ttl)}
}

// anoDaEntity reads the entity's release_date (core column, not the resolved override
// — the auto-match runs BEFORE any editing, so the base already reflects the best the
// catalog knows) and returns the first 4 digits as the year; 0 when absent/invalid/on
// error (it degrades to "no year", never failing the auto-match because of a date-less
// neighbor).
func (s *Server) anoDaEntity(ctx context.Context, id string) int {
	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		return 0
	}
	return anoDeReleaseDate(ent.ReleaseDate)
}

// anoDeReleaseDate extracts the year from the first 4 digits of a "YYYY-MM-DD" (or
// "YYYY") release_date; 0 when absent/short/invalid.
func anoDeReleaseDate(rd sql.NullString) int {
	if !rd.Valid || len(rd.String) < 4 {
		return 0
	}
	ano, err := strconv.Atoi(rd.String[:4])
	if err != nil {
		return 0
	}
	return ano
}

// anoDeStringPtr extracts the year from provider.ResultadoExterno.Year ("YYYY" or nil);
// 0 when absent/invalid.
func anoDeStringPtr(s *string) int {
	if s == nil {
		return 0
	}
	ano, err := strconv.Atoi(*s)
	if err != nil {
		return 0
	}
	return ano
}

// ── capturaResposta ──────────────────────────────────────────────────────────

// capturaResposta is a minimal http.ResponseWriter that only CAPTURES the status
// (never writes to the real response): used to run helpers coupled to HTTP writing
// (resolverColisaoExterna, idiomasPermitidos) without an internal ERROR write of theirs
// leaking into the auto-match's final response — the auto-match always decides on its
// own what to answer (200 with resolved:false in the single endpoint; a pending item in
// the batch), never forwarding somebody else's error body.
type capturaResposta struct {
	header http.Header
	status int
}

func (c *capturaResposta) Header() http.Header {
	if c.header == nil {
		c.header = http.Header{}
	}
	return c.header
}

func (c *capturaResposta) Write(b []byte) (int, error) { return len(b), nil }

func (c *capturaResposta) WriteHeader(status int) { c.status = status }

// errStatus is a minimal error carrying the HTTP status captured by capturaResposta,
// only for the log/internal error — the caller always degrades to respondErroInterno
// (500), the status here is only diagnostic context.
type errStatus int

func (e errStatus) Error() string {
	return "auto-match: helper interno escreveu status " + strconv.Itoa(int(e))
}

// ── Immediate movie auto-match in the scan ───────────────────────────────────

// TentarAutoMatchFilme is the adapter that satisfies scanner.FilmeImportador: called by
// the scanner right after creating a NEW movie entity, it tries to match+enrich against
// the configured providers reusing the SAME orchestration (tentarAutoMatch) and the SAME
// threshold (LimiarAutoMatch) as the batch auto-match/saude_biblioteca job -- never a
// parallel heuristic. matched=true only when a candidate was in fact applied (Resolved).
// err only propagates on a real infra failure (unrecoverable database/network); "no
// strong enough candidate" is NOT an error -- the same silent-degradation semantics
// resolverSerieMiss already applies to the series path (the entity stays
// cataloged/draft, pending in the Review queue).
func (s *Server) TentarAutoMatchFilme(ctx context.Context, entityID string) (bool, error) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return false, err
	}
	resultado, err := s.tentarAutoMatch(ctx, ent)
	if err != nil {
		return false, err
	}
	return resultado.Resolved, nil
}
