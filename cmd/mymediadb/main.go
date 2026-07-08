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
	"syscall"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// version é a versão do binário. Default "dev"; sobrescrita em build com
// -ldflags "-X main.version=<v>".
var version = "dev"

// shutdownTimeout limita quanto tempo o servidor espera para drenar conexões
// em voo durante o encerramento.
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatalf("mymediadb: erro fatal: %v", err)
	}
}

func run() error {
	addr := ":" + resolvePort()

	srv := server.New(version)
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

// resolvePort determina a porta de escuta: env PORT tem prioridade; caso
// ausente, usa o default 8080.
func resolvePort() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
