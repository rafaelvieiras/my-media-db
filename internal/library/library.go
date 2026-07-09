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
func (s *Service) Criar(ctx context.Context, rawPath, tipo string) (Source, error) {
	if !tiposValidos[tipo] {
		return Source{}, ErrTipoInvalido
	}
	if rawPath == "" {
		return Source{}, ErrPathVazio
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
		ID:   s.novoID(),
		Path: abs,
		Type: tipo,
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
// conexão de escrita.
func (s *Service) Apagar(ctx context.Context, id string) error {
	if _, err := s.leitura().ObterLibrarySource(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNaoEncontrado
		}
		return err
	}
	return s.escrita().ApagarLibrarySource(ctx, id)
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
		ID:       row.ID,
		Path:     row.Path,
		Type:     row.Type,
		State:    state,
		LastScan: lastScan,
	}
}
