// Package library is the domain service of the datasources (library_source):
// the server folders that the owner points at as the source of the library. It
// handles path validation and normalization, the CRUD through sqlc and the
// derivation of the online/offline state on read. It is the scanner's input
// database (see doc 06) and the first real native /api/v1 surface (see doc 05).
package library

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Possible states of a source (they mirror the CHECK of library_source).
const (
	stateOnline  = "online"
	stateOffline = "offline"
)

// tiposValidos are the types accepted in the MVP (see doc 05). Other values are
// rejected with ErrTipoInvalido (422).
var tiposValidos = map[string]bool{
	"movie": true,
	"tv":    true,
}

// content_type values known by the UI (ADR-0021). There is NO CHECK in the
// schema and no rejection in the service: the field is free/extensible (e.g.:
// 'music' in the future). This map is only a reference; the normalization below
// just fills in the default.
const (
	contentTypeMovie  = "movie"
	contentTypeSeries = "series"
)

// Domain errors. The HTTP handler maps them to a status + code of the standard
// error shape ({error:{code,message}}). They are sentinels so that the caller
// can use errors.Is.
var (
	// ErrPathInexistente: the informed path does not exist. -> 422 invalid_path
	ErrPathInexistente = errors.New("o caminho informado não existe")
	// ErrNaoDiretorio: the path exists but is not a directory. -> 422 not_a_directory
	ErrNaoDiretorio = errors.New("o caminho informado não é um diretório")
	// ErrPathIlegivel: the path exists but cannot be read. -> 422 path_unreadable
	ErrPathIlegivel = errors.New("o caminho informado não pode ser lido (sem permissão)")
	// ErrTipoInvalido: type outside {movie,tv}. -> 422 invalid_type
	ErrTipoInvalido = errors.New("tipo inválido: use movie ou tv")
	// ErrPathVazio: the path field is absent/empty. -> 400 invalid_body
	ErrPathVazio = errors.New("o campo path é obrigatório")
	// ErrPathDuplicado: a source with the same normalized path already exists. -> 409 duplicate_path
	ErrPathDuplicado = errors.New("já existe uma fonte cadastrada para este caminho")
	// ErrNaoEncontrado: no source with the informed id. -> 404 not_found
	ErrNaoEncontrado = errors.New("datasource não encontrado")
)

// Source is the domain representation of a source, with the state already
// derived. LastScan is null while the scanner has not run yet.
type Source struct {
	ID       string
	Path     string
	Type     string
	State    string
	LastScan *string
	// ContentType says whether the library is of movies ("movie") or of series
	// ("series") — the scanner branches the matching flow on this value
	// (ADR-0021). Default "movie".
	ContentType string
	// WatchEnabled turns the fsnotify watcher of this source on/off (see
	// doc 06). Default false on creation; editable only through AtualizarWatch
	// (PATCH /api/v1/library/{id}), applied ON THE FLY by the handler
	// (internal/watch).
	WatchEnabled bool
	// PreferredLanguage is the preferred search/display language of this
	// library (ADR-0018); nil = no preference, it uses the global language of
	// app_settings. The VALIDATION against the allowed languages is done by the
	// CALLER (internal/server), so that this package does not import
	// internal/server nor know about app_settings -- here it only persists what
	// it receives.
	PreferredLanguage *string
	// RetroactiveEnrichmentDays is the OPTIONAL override (see doc 07) of the
	// retry interval of the retroactive enrichment of this library; nil = no
	// override, it uses the global value of app_settings. The VALIDATION of the
	// interval (1-365) is done by the CALLER (internal/server) -- here it only
	// persists what it receives.
	RetroactiveEnrichmentDays *int
}

// Service concentrates the domain operations on library_source. It follows
// ADR-0010: writes through the single connection (db.Write), reads through the
// pool (db.Read).
type Service struct {
	database *db.DB
	// novoID generates the identifier of a new source; injectable for tests.
	novoID func() string
}

// New creates the service from the database pools.
func New(database *db.DB) *Service {
	return &Service{
		database: database,
		novoID:   func() string { return uuid.NewString() },
	}
}

// escrita returns the Queries bound to the write pool (single writer).
func (s *Service) escrita() *sqlcgen.Queries { return sqlcgen.New(s.database.Write) }

// leitura returns the Queries bound to the read pool.
func (s *Service) leitura() *sqlcgen.Queries { return sqlcgen.New(s.database.Read) }

// Criar validates and registers a new source. Steps: validates the type;
// normalizes the path (absolute+clean) BEFORE everything else; validates
// existence/directory/readability; checks for a duplicate by the normalized
// path; inserts through the write connection. The source is born online (it has
// just been validated as readable) and without last_scan.
// contentType is the nature of the library (movie|series, ADR-0021), which the
// scanner uses to branch the matching flow. Free value (no CHECK); empty
// normalizes to "movie" (compatibility with datasources registered before
// content_type existed). preferredLanguage is the optional preferred language
// (ADR-0018); nil = no preference. retroactiveEnrichmentDays is the optional
// override of the retry interval (see doc 07); nil = no override. The
// VALIDATION of both (allowed languages, interval 1-365) has already been done
// by the CALLER (internal/server) before this call -- this service only
// persists the value it receives.
func (s *Service) Criar(ctx context.Context, rawPath, tipo, contentType string, preferredLanguage *string, retroactiveEnrichmentDays *int) (Source, error) {
	if !tiposValidos[tipo] {
		return Source{}, ErrTipoInvalido
	}
	if rawPath == "" {
		return Source{}, ErrPathVazio
	}
	if contentType == "" {
		contentType = contentTypeMovie
	}

	abs, err := normalizarPath(rawPath)
	if err != nil {
		return Source{}, ErrPathInexistente
	}
	if err := validarPath(abs); err != nil {
		return Source{}, err
	}

	// Checks for a duplicate by the ALREADY normalized path (the same one it
	// writes), to return 409 with a clear message before hitting the UNIQUE.
	if _, err := s.leitura().ObterLibrarySourcePorPath(ctx, abs); err == nil {
		return Source{}, ErrPathDuplicado
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Source{}, err
	}

	row, err := s.escrita().CriarLibrarySource(ctx, sqlcgen.CriarLibrarySourceParams{
		ID:                        s.novoID(),
		Path:                      abs,
		Type:                      tipo,
		ContentType:               contentType,
		PreferredLanguage:         ptrParaNullString(preferredLanguage),
		RetroactiveEnrichmentDays: ptrParaNullInt64(retroactiveEnrichmentDays),
	})
	if err != nil {
		return Source{}, err
	}
	// state comes from the DEFAULT 'online'; it is not re-evaluated here (it
	// has just been validated as readable).
	return toSource(row, row.State), nil
}

// Listar returns every source with the state RE-EVALUATED on read (cheap stat
// of the path). It does not persist offline (API decision, §6).
func (s *Service) Listar(ctx context.Context) ([]Source, error) {
	rows, err := s.leitura().ListarLibrarySources(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Source, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSource(row, avaliarState(row.Path)))
	}
	return out, nil
}

// Obter returns a source by id, with the state re-evaluated. ErrNaoEncontrado
// if it does not exist.
func (s *Service) Obter(ctx context.Context, id string) (Source, error) {
	row, err := s.leitura().ObterLibrarySource(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Source{}, ErrNaoEncontrado
		}
		return Source{}, err
	}
	return toSource(row, avaliarState(row.Path)), nil
}

// Apagar removes the source record (it does NOT purge entities -- API decision,
// §6). It confirms the existence first so that it can answer 404; the removal
// goes through the write connection, in a SINGLE transaction (single writer,
// ADR-0010):
//  1. deletes the media_file of the source (WHERE library_source_id = ?), which
//     cascades the media_file_entity links (FK ON DELETE CASCADE, migration
//  0009. and preserves the entities of the catalog;
//  2. deletes the library_source.
//
// All or nothing: if any step fails, the rollback leaves the database intact
// (with no half-deleted state).
func (s *Service) Apagar(ctx context.Context, id string) error {
	if _, err := s.leitura().ObterLibrarySource(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNaoEncontrado
		}
		return err
	}

	tx, err := s.database.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	q := sqlcgen.New(tx)
	// library_source_id is NULLABLE in the schema; the query types the
	// parameter as sql.NullString. Here it is always a concrete id (Valid:
	// true).
	if err := q.ApagarMediaFilesPorFonte(ctx, sql.NullString{String: id, Valid: true}); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := q.ApagarLibrarySource(ctx, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// AtualizarWatch turns watch_enabled of the source on/off (see doc 06, PATCH
// /api/v1/library/{id}). ErrNaoEncontrado if the id does not exist. The CALLER
// (internal/server) is the one that triggers the registration/unregistration
// routine of the fsnotify watcher RIGHT AFTER this call returns successfully —
// this service only persists the column, it does not know internal/watch (it
// would cause an import cycle, since watch depends on library.Source).
func (s *Service) AtualizarWatch(ctx context.Context, id string, enabled bool) (Source, error) {
	if _, err := s.leitura().ObterLibrarySource(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Source{}, ErrNaoEncontrado
		}
		return Source{}, err
	}
	row, err := s.escrita().AtualizarLibrarySourceWatch(ctx, sqlcgen.AtualizarLibrarySourceWatchParams{
		WatchEnabled: watchEnabledInt(enabled),
		ID:           id,
	})
	if err != nil {
		return Source{}, err
	}
	return toSource(row, avaliarState(row.Path)), nil
}

// AtualizarPreferredLanguage writes (or clears, when lang==nil) the preferred
// language of the source (ADR-0018, PATCH /api/v1/library/{id}).
// ErrNaoEncontrado if the id does not exist. The VALIDATION against the allowed
// languages has already been done by the CALLER (internal/server) before this
// call -- this service only persists the value it receives.
func (s *Service) AtualizarPreferredLanguage(ctx context.Context, id string, lang *string) (Source, error) {
	if _, err := s.leitura().ObterLibrarySource(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Source{}, ErrNaoEncontrado
		}
		return Source{}, err
	}
	row, err := s.escrita().AtualizarLibrarySourcePreferredLanguage(ctx, sqlcgen.AtualizarLibrarySourcePreferredLanguageParams{
		PreferredLanguage: ptrParaNullString(lang),
		ID:                id,
	})
	if err != nil {
		return Source{}, err
	}
	return toSource(row, avaliarState(row.Path)), nil
}

// AtualizarRetroactiveEnrichmentDays writes (or clears, when dias==nil) the
// retry-interval override of the retroactive enrichment of the source (see
// doc 07, PATCH /api/v1/library/{id}). ErrNaoEncontrado if the id does not
// exist. The VALIDATION of the interval (1-365) has already been done by the
// CALLER (internal/server) before this call -- this service only persists the
// value it receives.
func (s *Service) AtualizarRetroactiveEnrichmentDays(ctx context.Context, id string, dias *int) (Source, error) {
	if _, err := s.leitura().ObterLibrarySource(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Source{}, ErrNaoEncontrado
		}
		return Source{}, err
	}
	row, err := s.escrita().AtualizarLibrarySourceRetroactiveEnrichmentDays(ctx, sqlcgen.AtualizarLibrarySourceRetroactiveEnrichmentDaysParams{
		RetroactiveEnrichmentDays: ptrParaNullInt64(dias),
		ID:                        id,
	})
	if err != nil {
		return Source{}, err
	}
	return toSource(row, avaliarState(row.Path)), nil
}

// AtualizarPath repoints the datasource to a new path (the disk/mount moved
// elsewhere, see doc 06) and cascades the re-prefixing to every absolute path
// derived from the old prefix -- media_file.path of the source,
// asset.local_path and entity.nfo_path of the entities reached through those
// files -- preserving id/content_hash/matches (it does NOT trigger a re-scan).
// Steps:
//  1. Gets the current source (404 if it does not exist).
//  2. Empty rawNovoPath -> ErrPathVazio (400).
//  3. Normalizes+validates the new path (the same rules as Criar: it exists, is
//     a directory, is readable -> 422).
//  4. New path EQUAL to the current one -> no-op, returns the source as it is,
//     without opening a transaction.
//  5. Duplicate against ANY OTHER source (ignoring itself) -> 409
//     ErrPathDuplicado.
//  6. Single write transaction (the same pattern as Apagar): updates
//     library_source.path, re-prefixes each media_file.path through TrimPrefix
//     in Go (never SUBSTR/REPLACE in SQL -- see the note about codepoint vs
//     byte in accented paths), discovers the entities reached (direct ones
//     through media_file.entity_id + indirect ones through media_file_entity)
//     and re-prefixes asset.local_path/entity.nfo_path of each one, IF AND ONLY
//     IF the stored value starts with the exact old prefix (an entity with a
//     file in more than one source, a rare case, is left untouched -- it does
//     not make the current state worse).
func (s *Service) AtualizarPath(ctx context.Context, id, rawNovoPath string) (Source, error) {
	atual, err := s.leitura().ObterLibrarySource(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Source{}, ErrNaoEncontrado
		}
		return Source{}, err
	}

	novoAbs, err := s.validarNovoPathDeFonte(ctx, id, atual.Path, rawNovoPath)
	if err != nil {
		return Source{}, err
	}
	if novoAbs == "" {
		// No-op sentinel (new path equal to the current one): avoids an
		// unnecessary transaction and stat calls (see doc 06, §4).
		return toSource(atual, avaliarState(atual.Path)), nil
	}

	prefixoAntigo := atual.Path

	tx, err := s.database.Write.BeginTx(ctx, nil)
	if err != nil {
		return Source{}, err
	}
	q := sqlcgen.New(tx)

	novaRow, err := q.AtualizarLibrarySourcePath(ctx, sqlcgen.AtualizarLibrarySourcePathParams{
		Path: novoAbs,
		ID:   id,
	})
	if err != nil {
		_ = tx.Rollback()
		return Source{}, err
	}

	fonteID := sql.NullString{String: id, Valid: true}
	if err := recascarMediaFilesDaFonte(ctx, q, fonteID, prefixoAntigo, novoAbs); err != nil {
		_ = tx.Rollback()
		return Source{}, err
	}
	if err := recascarEntidadesAlcancadas(ctx, q, fonteID, prefixoAntigo, novoAbs); err != nil {
		_ = tx.Rollback()
		return Source{}, err
	}

	if err := tx.Commit(); err != nil {
		return Source{}, err
	}
	return toSource(novaRow, avaliarState(novaRow.Path)), nil
}

// validarNovoPathDeFonte normalizes+validates the new path of a source (the
// same rules as Criar: it exists, is a directory, is readable) and checks for a
// duplicate against ANY OTHER source (ignoring itself). Extracted from
// AtualizarPath to keep the cyclomatic complexity low. It returns "" (no error)
// when the new path, once normalized, is EQUAL to the current path -- a no-op
// sentinel that the caller interprets (it never happens in an error case, since
// a valid absolute path is never empty).
func (s *Service) validarNovoPathDeFonte(ctx context.Context, id, pathAtual, rawNovoPath string) (string, error) {
	if rawNovoPath == "" {
		return "", ErrPathVazio
	}
	novoAbs, err := normalizarPath(rawNovoPath)
	if err != nil {
		return "", ErrPathInexistente
	}
	if err := validarPath(novoAbs); err != nil {
		return "", err
	}
	if novoAbs == pathAtual {
		return "", nil
	}

	// Duplicate by the ALREADY normalized path, ignoring the source ITSELF (it
	// would match through the old path before the UPDATE, which is not a
	// duplicate).
	if outro, err := s.leitura().ObterLibrarySourcePorPath(ctx, novoAbs); err == nil {
		if outro.ID != id {
			return "", ErrPathDuplicado
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return novoAbs, nil
}

// recascarMediaFilesDaFonte swaps the path prefix of every media_file of the
// source (extracted from AtualizarPath to keep the cyclomatic complexity low).
// IF AND ONLY IF the stored path starts with the exact old prefix (see the
// defensive note in doc 06, §6) -- there should never be a media_file with
// library_source_id of this source whose path does not match, but the code does
// not risk corrupting an unexpected value.
func recascarMediaFilesDaFonte(ctx context.Context, q *sqlcgen.Queries, fonteID sql.NullString, prefixoAntigo, novoPrefixo string) error {
	arquivos, err := q.ListarMediaFilePathsPorFonte(ctx, fonteID)
	if err != nil {
		return err
	}
	for _, mf := range arquivos {
		if !strings.HasPrefix(mf.Path, prefixoAntigo) {
			continue
		}
		novoPath := novoPrefixo + strings.TrimPrefix(mf.Path, prefixoAntigo)
		if err := q.AtualizarMediaFilePath(ctx, sqlcgen.AtualizarMediaFilePathParams{
			Path: novoPath,
			ID:   mf.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// recascarEntidadesAlcancadas swaps the prefix of asset.local_path and
// entity.nfo_path of every entity reached by the media_file of the source
// (directly through media_file.entity_id + indirectly through
// media_file_entity -- see ListarEntityIDsPorFonte).
func recascarEntidadesAlcancadas(ctx context.Context, q *sqlcgen.Queries, fonteID sql.NullString, prefixoAntigo, novoPrefixo string) error {
	entityIDs, err := q.ListarEntityIDsPorFonte(ctx, fonteID)
	if err != nil {
		return err
	}
	for _, entityID := range entityIDs {
		if err := recascarAssetsDaEntidade(ctx, q, entityID, prefixoAntigo, novoPrefixo); err != nil {
			return err
		}
		if err := recascarNfoDaEntidade(ctx, q, entityID, prefixoAntigo, novoPrefixo); err != nil {
			return err
		}
	}
	return nil
}

// recascarAssetsDaEntidade swaps the prefix of asset.local_path of every
// artwork of the entity WHOSE stored value starts with the exact old prefix --
// assets without local_path (never materialized on disk) or with a different
// prefix (an entity with a file in more than one source, the rare case of §6)
// are left untouched.
func recascarAssetsDaEntidade(ctx context.Context, q *sqlcgen.Queries, entityID, prefixoAntigo, novoPrefixo string) error {
	assets, err := q.ListarAssetsPorEntity(ctx, entityID)
	if err != nil {
		return err
	}
	for _, a := range assets {
		if !a.LocalPath.Valid || !strings.HasPrefix(a.LocalPath.String, prefixoAntigo) {
			continue
		}
		novoLocalPath := novoPrefixo + strings.TrimPrefix(a.LocalPath.String, prefixoAntigo)
		if err := q.AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
			LocalPath: sql.NullString{String: novoLocalPath, Valid: true},
			ID:        a.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// recascarNfoDaEntidade swaps the prefix of entity.nfo_path IF the stored value
// starts with the exact old prefix; an entity without an exported .nfo (or with
// a different prefix) is left untouched. It does NOT touch nfo_written_at (see
// AtualizarEntityNfoPath): this is not a new export.
func recascarNfoDaEntidade(ctx context.Context, q *sqlcgen.Queries, entityID, prefixoAntigo, novoPrefixo string) error {
	ent, err := q.ObterEntity(ctx, entityID)
	if err != nil {
		return err
	}
	if !ent.NfoPath.Valid || !strings.HasPrefix(ent.NfoPath.String, prefixoAntigo) {
		return nil
	}
	novoNfoPath := novoPrefixo + strings.TrimPrefix(ent.NfoPath.String, prefixoAntigo)
	return q.AtualizarEntityNfoPath(ctx, sqlcgen.AtualizarEntityNfoPathParams{
		NfoPath: sql.NullString{String: novoNfoPath, Valid: true},
		ID:      entityID,
	})
}

// watchEnabledInt converts the domain bool into the INTEGER column (CHECK 0/1).
func watchEnabledInt(enabled bool) int64 {
	if enabled {
		return 1
	}
	return 0
}

// toSource converts the row generated by sqlc into the domain representation,
// applying the state already derived by the caller.
func toSource(row sqlcgen.LibrarySource, state string) Source {
	var lastScan *string
	if row.LastScan.Valid {
		v := row.LastScan.String
		lastScan = &v
	}
	return Source{
		ID:                        row.ID,
		Path:                      row.Path,
		Type:                      row.Type,
		State:                     state,
		LastScan:                  lastScan,
		ContentType:               row.ContentType,
		WatchEnabled:              row.WatchEnabled != 0,
		PreferredLanguage:         nullStringParaPtr(row.PreferredLanguage),
		RetroactiveEnrichmentDays: nullInt64ParaPtr(row.RetroactiveEnrichmentDays),
	}
}

// ptrParaNullString converts a domain *string (nil = absent) into the
// sql.NullString writable by the NULLABLE preferred_language column.
func ptrParaNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// nullStringParaPtr converts the NULLABLE column read back into a domain
// *string (nil when NULL).
func nullStringParaPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// ptrParaNullInt64 converts a domain *int (nil = absent) into the sql.NullInt64
// writable by the NULLABLE retroactive_enrichment_days column.
func ptrParaNullInt64(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

// nullInt64ParaPtr converts the NULLABLE column read back into a domain *int
// (nil when NULL).
func nullInt64ParaPtr(ni sql.NullInt64) *int {
	if !ni.Valid {
		return nil
	}
	v := int(ni.Int64)
	return &v
}
