// Comando mymediadb: entrypoint do servidor HTTP único que serve a API e o SPA
// embutido no mesmo origin. Lê a porta de env/flag, sobe o servidor e faz
// graceful shutdown ao receber SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// defaultDBPath é o caminho padrão do banco em desenvolvimento: dentro de tmp/,
// ignorado pelo git. Em produção, defina MYMEDIADB_DB_PATH.
const defaultDBPath = "tmp/mymediadb.sqlite"

// version é a versão do binário. Default "dev"; sobrescrita em build com
// -ldflags "-X main.version=<v>".
var version = "dev"

// shutdownTimeout limita quanto tempo o servidor espera para drenar conexões
// em voo durante o encerramento.
const shutdownTimeout = 10 * time.Second

// migrateTimeout limita quanto tempo as migrations podem levar no boot.
const migrateTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatalf("mymediadb: erro fatal: %v", err)
	}
}

func run() error {
	addr := ":" + resolvePort()

	// Abre o banco e aplica as migrations ANTES de aceitar tráfego. Falha de
	// abertura ou migration aborta o boot (fail-fast — ADR-0017).
	database, err := openAndMigrate()
	if err != nil {
		return err
	}
	defer func() {
		if cerr := database.Close(); cerr != nil {
			log.Printf("mymediadb: erro ao fechar o banco: %v", cerr)
		}
	}()

	opts, err := opcoesServer()
	if err != nil {
		return err
	}
	srv := server.New(version, database, opts...)
	handler, err := srv.Handler()
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Canal para propagar erro de ListenAndServe ao fluxo principal.
	serverErr := make(chan error, 1)

	go func() {
		log.Printf("mymediadb: iniciando (versão=%s) escutando em %s", version, addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	// Aguarda sinal de término (SIGINT/SIGTERM) ou falha do servidor.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		stop() // restaura comportamento padrão do sinal
		log.Printf("mymediadb: sinal recebido, encerrando (graceful shutdown, timeout=%s)", shutdownTimeout)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		log.Printf("mymediadb: encerrado com sucesso")
		return nil
	}
}

// openAndMigrate abre o banco no caminho configurado e aplica as migrations
// pendentes. Em sucesso, loga a versão de schema. Erro aborta o boot.
func openAndMigrate() (*db.DB, error) {
	database, err := db.Open(db.Config{Path: resolveDBPath()})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()
	if err := database.Migrate(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	if v, verr := database.SchemaVersion(ctx); verr == nil {
		log.Printf("mymediadb: banco aberto e migrado (schema=%d)", v)
	}
	return database, nil
}

// opcoesServer monta as options do server: a busca/enrich externa (briefing
// 0014/0015) e o diretório de assets. Erro só na criação do diretório de assets.
func opcoesServer() ([]server.Option, error) {
	opts := opcoesBusca()

	dir, err := resolveAssetsDir()
	if err != nil {
		return nil, err
	}
	log.Printf("mymediadb: diretório de assets em %q", dir)
	opts = append(opts, server.WithAssetsDir(dir))
	return opts, nil
}

// opcoesBusca monta a option da busca externa (briefing 0014). Lê a credencial do
// TMDB do ambiente: MYMEDIADB_TMDB_TOKEN (Read Access Token v4, preferido) e, se
// vazio, MYMEDIADB_TMDB_API_KEY (v3). Sem nenhuma das duas, NÃO cria provider
// (busca externa desligada — não é erro; o enrich/import também ficam off). NUNCA
// loga o valor da credencial, só se a busca externa está ligada ou desligada.
func opcoesBusca() []server.Option {
	cred := tmdb.Credencial{
		Token:  os.Getenv("MYMEDIADB_TMDB_TOKEN"),
		APIKey: os.Getenv("MYMEDIADB_TMDB_API_KEY"),
	}
	if !cred.Configurada() {
		log.Printf("mymediadb: busca externa TMDB desabilitada (sem credencial)")
		return nil
	}
	log.Printf("mymediadb: busca externa TMDB habilitada")
	return []server.Option{server.WithSearchProvider(tmdb.New(cred, nil, ""))}
}

// resolveAssetsDir determina o diretório do cache de imagem (briefing 0015): env
// MYMEDIADB_ASSETS_DIR tem prioridade; caso ausente, usa dirname(DB)/assets. Cria
// o diretório se faltar (o cache grava binários lá; a guarda de path do pôster o
// aceita como raiz confiável).
func resolveAssetsDir() (string, error) {
	dir := os.Getenv("MYMEDIADB_ASSETS_DIR")
	if dir == "" {
		dir = filepath.Join(filepath.Dir(resolveDBPath()), "assets")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// resolvePort determina a porta de escuta: env PORT tem prioridade; caso
// ausente, usa o default 8080.
func resolvePort() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}

// resolveDBPath determina o caminho do arquivo SQLite: env MYMEDIADB_DB_PATH
// tem prioridade; caso ausente, usa o default de desenvolvimento em tmp/.
func resolveDBPath() string {
	if p := os.Getenv("MYMEDIADB_DB_PATH"); p != "" {
		return p
	}
	return defaultDBPath
}
