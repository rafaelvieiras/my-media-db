// Package library é o serviço de domínio dos datasources (library_source): as
// pastas do servidor que o dono aponta como fonte da biblioteca. Cuida da
// validação e normalização de path, do CRUD via sqlc e da derivação do estado
// online/offline na leitura. É a base de dados-de-entrada do scanner (0009) e a
// primeira superfície nativa /api/v1 de verdade (briefing 0008).
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

// Estados possíveis de uma fonte (espelham o CHECK de library_source).
const (
	stateOnline  = "online"
	stateOffline = "offline"
)

// tiposValidos são os tipos aceitos no MVP (briefing 0008). Outros valores são
// rejeitados com ErrTipoInvalido (422).
var tiposValidos = map[string]bool{
	"movie": true,
	"tv":    true,
}

// Valores de content_type conhecidos pela UI (0034). NAO ha CHECK no schema nem
// rejeicao no servico: o campo e livre/extensivel (ex.: 'music' no futuro). Este
// mapa serve so de referencia; a normalizacao abaixo apenas preenche o default.
const (
	contentTypeMovie  = "movie"
	contentTypeSeries = "series"
)

// Erros de domínio. O handler HTTP os mapeia para status + code do shape de
// erro padrão ({error:{code,message}}). São sentinelas para permitir
// errors.Is no chamador.
var (
	// ErrPathInexistente: o caminho não existe. -> 422 invalid_path
	ErrPathInexistente = errors.New("o caminho informado não existe")
	// ErrNaoDiretorio: o caminho existe mas não é um diretório. -> 422 not_a_directory
	ErrNaoDiretorio = errors.New("o caminho informado não é um diretório")
	// ErrPathIlegivel: o caminho existe mas não pode ser lido. -> 422 path_unreadable
	ErrPathIlegivel = errors.New("o caminho informado não pode ser lido (sem permissão)")
	// ErrTipoInvalido: type fora de {movie,tv}. -> 422 invalid_type
	ErrTipoInvalido = errors.New("tipo inválido: use movie ou tv")
	// ErrPathVazio: campo path ausente/vazio. -> 400 invalid_body
	ErrPathVazio = errors.New("o campo path é obrigatório")
	// ErrPathDuplicado: já existe uma fonte com o mesmo path normalizado. -> 409 duplicate_path
	ErrPathDuplicado = errors.New("já existe uma fonte cadastrada para este caminho")
	// ErrNaoEncontrado: nenhuma fonte com o id informado. -> 404 not_found
	ErrNaoEncontrado = errors.New("datasource não encontrado")
)

// Source é a representação de domínio de uma fonte, com o estado já derivado.
// LastScan é nulo enquanto o scanner (0009) não rodar.
type Source struct {
	ID       string
	Path     string
	Type     string
	State    string
	LastScan *string
	// ContentType diz se a biblioteca e de filmes ("movie") ou de series ("series")
	// — o scanner (0034) ramifica o fluxo de match por este valor. Default "movie".
	ContentType string
	// WatchEnabled liga/desliga o watcher fsnotify desta fonte (briefing 0064).
	// Default false no cadastro; editavel so via AtualizarWatch (PATCH
	// /api/v1/library/{id}), aplicado A QUENTE pelo handler (internal/watch).
	WatchEnabled bool
}

// Service concentra as operações de domínio sobre library_source. Segue a
// ADR-0010: escritas pela conexão única (db.Write), leituras pelo pool
// (db.Read).
type Service struct {
	database *db.DB
	// novoID gera o identificador de uma fonte nova; injetável para testes.
	novoID func() string
}

// New cria o serviço a partir dos pools de banco.
func New(database *db.DB) *Service {
	return &Service{
		database: database,
		novoID:   func() string { return uuid.NewString() },
	}
}

// escrita devolve os Queries ligados ao pool de escrita (escritor único).
func (s *Service) escrita() *sqlcgen.Queries { return sqlcgen.New(s.database.Write) }

// leitura devolve os Queries ligados ao pool de leitura.
func (s *Service) leitura() *sqlcgen.Queries { return sqlcgen.New(s.database.Read) }

// Criar valida e cadastra uma nova fonte. Passos: valida o tipo; normaliza o
// path (absoluto+limpo) ANTES de tudo; valida existência/diretório/legibilidade;
// checa duplicata pelo path normalizado; insere pela conexão de escrita. A fonte
// nasce online (acabou de ser validada como legível) e sem last_scan.
// contentType é a natureza da biblioteca (movie|series, 0034), que o scanner usa
// para ramificar o fluxo de match. Valor livre (sem CHECK); vazio normaliza para
// "movie" (compat com o cadastro pré-0034).
func (s *Service) Criar(ctx context.Context, rawPath, tipo, contentType string) (Source, error) {
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

	// Checa duplicata pelo path JÁ normalizado (o mesmo que grava), para
	// devolver 409 com mensagem clara antes de esbarrar no UNIQUE.
	if _, err := s.leitura().ObterLibrarySourcePorPath(ctx, abs); err == nil {
		return Source{}, ErrPathDuplicado
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Source{}, err
	}

	row, err := s.escrita().CriarLibrarySource(ctx, sqlcgen.CriarLibrarySourceParams{
		ID:          s.novoID(),
		Path:        abs,
		Type:        tipo,
		ContentType: contentType,
	})
	if err != nil {
		return Source{}, err
	}
	// state vem do DEFAULT 'online'; não reavaliamos aqui (acabou de ser
	// validado como legível).
	return toSource(row, row.State), nil
}

// Listar devolve todas as fontes com o state REAVALIADO na leitura (stat barato
// do path). Não persiste offline (decisão 0008 sec.6).
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

// Obter devolve uma fonte pelo id, com o state reavaliado. ErrNaoEncontrado se
// não existir.
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

// Apagar remove o registro da fonte (NÃO purga entities -- decisão 0008 sec.6).
// Confirma a existência antes para poder responder 404; a remoção passa pela
// conexão de escrita, numa ÚNICA transação (escritor único, ADR-0010):
//  1. apaga os media_file da fonte (WHERE library_source_id = ?), o que cascateia
//     os vínculos media_file_entity (FK ON DELETE CASCADE, migration 0009) e
//     preserva as entities do catálogo;
//  2. apaga a library_source.
//
// Tudo ou nada: se qualquer passo falhar, o rollback deixa o banco intacto (sem
// estado meio-apagado).
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
	// library_source_id é NULLABLE no schema; a query tipa o parâmetro como
	// sql.NullString. Aqui é sempre um id concreto (Valid: true).
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

// AtualizarWatch liga/desliga watch_enabled da fonte (briefing 0064, PATCH
// /api/v1/library/{id}). ErrNaoEncontrado se o id não existir. O CHAMADOR
// (internal/server) é quem aciona a rotina de registro/desregistro do
// watcher fsnotify LOGO APÓS esta chamada retornar com sucesso — este
// serviço só persiste a coluna, não conhece internal/watch (evitaria um
// ciclo de import, já que watch depende de library.Source).
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

// AtualizarPath reaponta o datasource para um novo caminho (briefing 0070:
// disco/mount mudou de lugar) e recasca em cascata todos os caminhos
// absolutos derivados do prefixo antigo -- media_file.path da fonte,
// asset.local_path e entity.nfo_path das entities alcançadas por esses
// arquivos -- preservando id/content_hash/matches (NÃO dispara re-scan).
// Passos:
//  1. Obtém a fonte atual (404 se não existir).
//  2. rawNovoPath vazio -> ErrPathVazio (400).
//  3. Normaliza+valida o novo path (mesmas regras de Criar: existe, é
//     diretório, é legível -> 422).
//  4. Path novo IGUAL ao atual -> no-op, devolve a fonte como está, sem abrir
//     transação.
//  5. Duplicata contra QUALQUER OUTRA fonte (ignorando a própria) -> 409
//     ErrPathDuplicado.
//  6. Transação única de escrita (mesmo padrão de Apagar): atualiza
//     library_source.path, recasca cada media_file.path por TrimPrefix em Go
//     (nunca SUBSTR/REPLACE no SQL -- ver nota do briefing sobre codepoint vs
//     byte em paths acentuados), descobre as entities alcançadas (diretas via
//     media_file.entity_id + indiretas via media_file_entity) e recasca
//     asset.local_path/entity.nfo_path de cada uma, SE E SOMENTE SE o valor
//     gravado começa com o prefixo antigo exato (entity com arquivo em mais
//     de uma fonte, caso raro, fica sem tocar -- não piora o estado atual).
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
		// Sentinela de no-op (path novo igual ao atual): evita transação e
		// stat desnecessários (briefing 0070 sec.4).
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

// validarNovoPathDeFonte normaliza+valida o novo path de uma fonte (mesmas
// regras de Criar: existe, é diretório, é legível) e checa duplicata contra
// QUALQUER OUTRA fonte (ignorando a própria). Extraído de AtualizarPath para
// manter a complexidade ciclomática baixa. Devolve "" (sem erro) quando o
// path novo, já normalizado, é IGUAL ao path atual -- sentinela de no-op que
// o chamador interpreta (nunca ocorre em caso de erro, já que um path
// absoluto válido nunca é vazio).
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

	// Duplicata pelo path JÁ normalizado, ignorando a PRÓPRIA fonte (ela
	// mesma casaria pelo path antigo antes do UPDATE, o que não é duplicata).
	if outro, err := s.leitura().ObterLibrarySourcePorPath(ctx, novoAbs); err == nil {
		if outro.ID != id {
			return "", ErrPathDuplicado
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return novoAbs, nil
}

// recascarMediaFilesDaFonte troca o prefixo de path de todo media_file da
// fonte (extraído de AtualizarPath para manter a complexidade ciclomática
// baixa). SE E SOMENTE SE o path gravado começa com o prefixo antigo exato
// (ver nota defensiva do briefing 0070 §6) -- nunca deveria haver um
// media_file com library_source_id desta fonte cujo path não bata, mas o
// código não arrisca corromper um valor inesperado.
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

// recascarEntidadesAlcancadas troca o prefixo de asset.local_path e
// entity.nfo_path de cada entity alcançada pelos media_file da fonte (direto
// via media_file.entity_id + indireto via media_file_entity -- ver
// ListarEntityIDsPorFonte).
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

// recascarAssetsDaEntidade troca o prefixo de asset.local_path de toda arte
// da entity CUJO valor gravado começa com o prefixo antigo exato -- assets
// sem local_path (nunca materializados em disco) ou com um prefixo diferente
// (entity com arquivo em mais de uma fonte, caso raro do §6) ficam intocados.
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

// recascarNfoDaEntidade troca o prefixo de entity.nfo_path SE o valor gravado
// começa com o prefixo antigo exato; entity sem .nfo exportado (ou com um
// prefixo diferente) fica intocada. NÃO toca nfo_written_at (ver
// AtualizarEntityNfoPath): isto não é uma nova exportação.
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

// watchEnabledInt converte o bool de domínio para a coluna INTEGER (CHECK 0/1).
func watchEnabledInt(enabled bool) int64 {
	if enabled {
		return 1
	}
	return 0
}

// toSource converte a linha gerada pelo sqlc na representação de domínio,
// aplicando o state já derivado pelo chamador.
func toSource(row sqlcgen.LibrarySource, state string) Source {
	var lastScan *string
	if row.LastScan.Valid {
		v := row.LastScan.String
		lastScan = &v
	}
	return Source{
		ID:           row.ID,
		Path:         row.Path,
		Type:         row.Type,
		State:        state,
		LastScan:     lastScan,
		ContentType:  row.ContentType,
		WatchEnabled: row.WatchEnabled != 0,
	}
}
