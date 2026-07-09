// Package db é a fundação de persistência do MyMediaDB sobre SQLite. Encapsula
// a receita de concorrência ratificada na ADR-0010 (WAL + pragmas, pool de
// leitura com N conexões e escritor único num pool de tamanho 1) e a aplicação
// das migrations embutidas via goose (ADR-0017), tudo com o driver CGO-free
// modernc.org/sqlite. Os próximos briefings só precisam adicionar tabelas e
// queries — o encanamento vive aqui.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registra o driver "sqlite" (SQLite puro em Go, sem CGO)

	"github.com/rafaelvieiras/my-media-db/internal/db/migrations"
)

// driverName é o nome sob o qual o modernc.org/sqlite se registra no
// database/sql. Não confundir com "sqlite3" (esse é o dialeto do goose).
const driverName = "sqlite"

// migrationsDir é o nome do diretório dentro do embed.FS de migrations. O goose
// espera o caminho relativo à raiz do FS embutido.
const migrationsDir = "."

// defaultMaxReadConns é o tamanho padrão do pool de leitura quando o Config não
// especifica um valor. Um punhado de leitores concorrentes é suficiente para o
// uso self-hosted; a escrita é sempre serializada num pool de tamanho 1.
const defaultMaxReadConns = 4

// pragmas são os PRAGMAs da ADR-0010 aplicados a TODA conexão nova. O driver
// modernc executa cada `_pragma=...` do DSN a cada conexão aberta pelo pool —
// por isso valem para o pool inteiro, não uma única vez.
//
//	journal_mode=WAL   → vários leitores + um escritor sem bloqueio mútuo
//	busy_timeout=5000  → espera até 5s por um lock em vez de falhar na hora
//	synchronous=NORMAL → seguro sob WAL; dados de scan são re-deriváveis
//	foreign_keys=ON    → integridade referencial ativa (default do SQLite é OFF)
var pragmas = []string{
	"_pragma=journal_mode(WAL)",
	"_pragma=busy_timeout(5000)",
	"_pragma=synchronous(NORMAL)",
	"_pragma=foreign_keys(ON)",
}

// Config parametriza a abertura do banco.
type Config struct {
	// Path é o caminho do arquivo .sqlite. Obrigatório. O diretório pai é
	// criado se não existir.
	Path string
	// MaxReadConns é o número máximo de conexões do pool de leitura. Se <= 0,
	// usa defaultMaxReadConns.
	MaxReadConns int
}

// DB agrega os dois pools sobre o MESMO arquivo: Read para leituras concorrentes
// e Write para o escritor único (SetMaxOpenConns(1)). Separar os pools é o que
// garante, junto do WAL, que uma escrita em curso não gere `database is locked`
// nas leituras (ADR-0010).
type DB struct {
	// Read é o pool de leitura (N conexões).
	Read *sql.DB
	// Write é o pool de escrita (exatamente 1 conexão = escritor único).
	Write *sql.DB
}

// gooseOnce garante que a configuração global do goose (FS embutido + dialeto)
// seja feita uma única vez por processo, já que a API de biblioteca do goose é
// baseada em estado global.
var gooseOnce sync.Once
var gooseErr error

// setupGoose configura o goose para ler as migrations embutidas com o dialeto
// SQLite. Idempotente e seguro para concorrência.
func setupGoose() error {
	gooseOnce.Do(func() {
		goose.SetBaseFS(migrations.FS)
		gooseErr = goose.SetDialect("sqlite3")
	})
	return gooseErr
}

// dsn monta o DSN do modernc para um caminho de arquivo, anexando os PRAGMAs da
// ADR-0010 como parâmetros `_pragma` — aplicados a cada conexão nova do pool.
func dsn(path string) string {
	return path + "?" + strings.Join(pragmas, "&")
}

// Open abre os dois pools (leitura e escrita) sobre o arquivo de cfg.Path,
// aplicando os PRAGMAs de concorrência a toda conexão. Cria o diretório pai se
// necessário. Não roda migrations — chame Migrate em seguida.
func Open(cfg Config) (*DB, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("db: Config.Path vazio")
	}
	if dir := filepath.Dir(cfg.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("db: não foi possível criar o diretório %q: %w", dir, err)
		}
	}

	maxRead := cfg.MaxReadConns
	if maxRead <= 0 {
		maxRead = defaultMaxReadConns
	}

	connStr := dsn(cfg.Path)

	read, err := sql.Open(driverName, connStr)
	if err != nil {
		return nil, fmt.Errorf("db: abrir pool de leitura: %w", err)
	}
	read.SetMaxOpenConns(maxRead)

	write, err := sql.Open(driverName, connStr)
	if err != nil {
		_ = read.Close()
		return nil, fmt.Errorf("db: abrir pool de escrita: %w", err)
	}
	// Escritor único: no máximo uma conexão de escrita, serializando as
	// escritas em nível de aplicação (ADR-0010).
	write.SetMaxOpenConns(1)

	// Materializa ao menos uma conexão de cada pool para falhar cedo (e para
	// que os PRAGMAs — em especial journal_mode=WAL, que é persistido no
	// arquivo — já valham desde o início).
	if err := write.Ping(); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, fmt.Errorf("db: ping do pool de escrita: %w", err)
	}
	if err := read.Ping(); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, fmt.Errorf("db: ping do pool de leitura: %w", err)
	}

	return &DB{Read: read, Write: write}, nil
}

// Migrate aplica todas as migrations pendentes pela conexão de ESCRITA (o
// escritor único). Fail-fast: qualquer erro é propagado para abortar o boot.
func (d *DB) Migrate(ctx context.Context) error {
	if err := setupGoose(); err != nil {
		return fmt.Errorf("db: configurar goose: %w", err)
	}
	if err := goose.UpContext(ctx, d.Write, migrationsDir); err != nil {
		return fmt.Errorf("db: aplicar migrations: %w", err)
	}
	return nil
}

// MigrateDown reverte a última migration aplicada (uso de desenvolvimento; em
// produção a política é só avançar — ADR-0017).
func (d *DB) MigrateDown(ctx context.Context) error {
	if err := setupGoose(); err != nil {
		return fmt.Errorf("db: configurar goose: %w", err)
	}
	if err := goose.DownContext(ctx, d.Write, migrationsDir); err != nil {
		return fmt.Errorf("db: reverter migration: %w", err)
	}
	return nil
}

// SchemaVersion devolve a versão de schema atual segundo o goose (o maior número
// de migration já aplicada). Útil para o health e o diagnóstico.
func (d *DB) SchemaVersion(ctx context.Context) (int64, error) {
	if err := setupGoose(); err != nil {
		return 0, fmt.Errorf("db: configurar goose: %w", err)
	}
	v, err := goose.GetDBVersionContext(ctx, d.Write)
	if err != nil {
		return 0, fmt.Errorf("db: obter versão de schema: %w", err)
	}
	return v, nil
}

// Ping verifica a conectividade do pool de leitura.
func (d *DB) Ping(ctx context.Context) error {
	return d.Read.PingContext(ctx)
}

// Close fecha os dois pools. Devolve o primeiro erro encontrado, mas tenta
// fechar ambos de qualquer forma.
func (d *DB) Close() error {
	var errs []error
	if d.Read != nil {
		if err := d.Read.Close(); err != nil {
			errs = append(errs, fmt.Errorf("fechar pool de leitura: %w", err))
		}
	}
	if d.Write != nil {
		if err := d.Write.Close(); err != nil {
			errs = append(errs, fmt.Errorf("fechar pool de escrita: %w", err))
		}
	}
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("db: %s", strings.Join(msgs, "; "))
	}
	return nil
}
