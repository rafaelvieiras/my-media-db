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
	"github.com/rafaelvieiras/my-media-db/internal/job/backup"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fanarttv"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tvdb"
	"github.com/rafaelvieiras/my-media-db/internal/server"
	"github.com/rafaelvieiras/my-media-db/internal/ytdlp"
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

	opts, err := opcoesServer(database)
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

	// Sinal de término (SIGINT/SIGTERM) — o MESMO ctx encerra tanto o
	// agendador de jobs (RunScheduler, abaixo) quanto o shutdown do HTTP.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Agendador embutido de jobs (briefing 0052): dispara ANTES do
	// ListenAndServe, para o boot do HTTP não atrasar o primeiro tick. Sai
	// sozinho quando ctx é cancelado (mesmo sinal do shutdown do HTTP).
	// srv.Jobs() nunca é nil aqui — database (acima) é sempre não-nil quando
	// o boot chega a este ponto, e montarServicos constrói o Manager sempre
	// que há banco.
	go srv.Jobs().RunScheduler(ctx)

	// Watch de filesystem opcional por fonte (briefing 0064): registra o
	// watcher fsnotify de cada library_source com watch_enabled=1 ANTES do
	// ListenAndServe (mesmo espírito do RunScheduler acima — não atrasa o
	// boot do HTTP; best-effort, nunca aborta por causa de UMA fonte).
	// FecharWatchers no shutdown gracioso, abaixo.
	srv.IniciarWatchers(context.Background())
	defer srv.FecharWatchers()

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
// 0014/0015), o diretório de assets, os jobs semente backup_vacuum
// (briefing 0052), saude_biblioteca (briefing 0053) e midias_faltantes +
// watch de filesystem (briefing 0064). Erro só na criação dos diretórios
// (assets/backups).
func opcoesServer(database *db.DB) ([]server.Option, error) {
	opts := opcoesBusca()

	// Registro de CODECS (briefing 0100, ADR-0024): sempre populado,
	// independente de credencial, só habilita a capacidade de INTERPRETAR
	// payload já cacheado (nunca busca ao vivo), para dado catalogado
	// sobreviver à ausência/revogação de chave. FORA do modo fake (E2E):
	// lá o registro de busca (fake.New, em opcoesBusca) já responde por
	// "tmdb" sozinho -- registrar também o codec real de tmdb aqui faria
	// codecPara priorizá-lo sobre o fake e quebrar o parse das fixtures.
	if os.Getenv("MYMEDIADB_FAKE_PROVIDER") != "1" {
		opts = append(opts, server.WithCodecRegistry(provedoresConhecidos()...))
	}

	dir, err := resolveAssetsDir()
	if err != nil {
		return nil, err
	}
	log.Printf("mymediadb: diretório de assets em %q", dir)
	opts = append(opts, server.WithAssetsDir(dir))

	backupDir, err := resolveBackupDir()
	if err != nil {
		return nil, err
	}
	log.Printf("mymediadb: diretório de backups em %q (job backup_vacuum)", backupDir)
	opts = append(opts, server.WithJobHandlers(backup.New(database.Write, backupDir)))

	// Gerenciador do binário yt-dlp (briefing 0101, ADR-0025): fundação para
	// trailer (0102) e música-tema (0103). Diretório de dados no mesmo padrão
	// de resolveAssetsDir/resolveBackupDir; o path customizado do admin (env
	// MYMEDIADB_YTDLP_PATH) é resolvido AQUI e passado pronto -- o pacote
	// internal/ytdlp nunca lê os.Getenv sozinho (mantém-se testável). O
	// override via banco (app_settings, UI de Integrações) tem precedência e
	// é sincronizado depois, no boot do Server (montarServicos).
	ytdlpDir, err := resolveYtdlpDir()
	if err != nil {
		return nil, err
	}
	log.Printf("mymediadb: diretório do yt-dlp em %q", ytdlpDir)
	opts = append(opts, server.WithYtdlp(ytdlp.Novo(ytdlpDir, os.Getenv("MYMEDIADB_YTDLP_PATH"))))

	// Job saude_biblioteca (briefing 0053): registrado via option DEDICADA
	// (não WithJobHandlers) porque o Handler precisa do scanner.Manager e do
	// library.Service, internos ao Server — ver o comentário de
	// WithLibraryHealthJob em internal/server/server.go.
	opts = append(opts, server.WithLibraryHealthJob())

	// Job midias_faltantes + watch de filesystem opcional por fonte
	// (briefing 0064): mesmo motivo de option DEDICADA do saude_biblioteca
	// acima — os dois precisam de dependências internas ao Server. O boot
	// efetivo do watch (listar fontes com watch_enabled=1 e registrar cada
	// uma) acontece em srv.IniciarWatchers, chamado em run() depois de
	// server.New (ver comentário lá).
	opts = append(opts, server.WithMidiasFaltantesJob(), server.WithWatch())

	return opts, nil
}

// opcoesBusca monta a option das fontes externas (briefing 0014/0031): REGISTRA cada
// provider configurado no registro multi-provider. TMDB e TheTVDB podem COEXISTIR (a
// busca consulta ambos e mescla; o resto escolhe a fonte por nome).
//
// - TMDB: MYMEDIADB_TMDB_TOKEN (Read Access Token v4, preferido) ou MYMEDIADB_TMDB_API_KEY (v3).
// - TheTVDB: MYMEDIADB_TTDB_API_KEY (apikey do projeto, login v4).
//
// Sem NENHUMA credencial, não registra provider (busca externa desligada — não é erro;
// enrich/import também ficam off). NUNCA loga o valor da credencial, só ligado/desligado.
//
// MYMEDIADB_FAKE_PROVIDER=1 tem PRECEDÊNCIA e injeta o provider de fixtures
// determinístico (briefing 0028), EXCLUSIVO: o binário roda o fluxo inteiro sem fonte
// real nem segredo, para os testes E2E (Camada 2) e inspeção manual. As imagens vêm de
// MYMEDIADB_FAKE_IMAGE_BASE (um servidor de fixtures); vazio => sem pôster.
func opcoesBusca() []server.Option {
	if os.Getenv("MYMEDIADB_FAKE_PROVIDER") == "1" {
		log.Printf("mymediadb: PROVIDER FAKE de fixtures habilitado (E2E; sem fonte real)")
		return []server.Option{server.WithSearchProvider(fake.New(os.Getenv("MYMEDIADB_FAKE_IMAGE_BASE")))}
	}
	provs := provedoresReais()
	if len(provs) == 0 {
		log.Printf("mymediadb: busca externa desabilitada (sem credencial)")
		return nil
	}
	return []server.Option{server.WithSearchProvider(provs...)}
}

// provedoresReais constrói os providers reais cujas credenciais estão no ambiente
// (TMDB e/ou TheTVDB), na ordem de prioridade padrão. Vazio quando nenhuma existe.
func provedoresReais() []provider.Provider {
	var provs []provider.Provider

	credTmdb := tmdb.Credencial{
		Token:  os.Getenv("MYMEDIADB_TMDB_TOKEN"),
		APIKey: os.Getenv("MYMEDIADB_TMDB_API_KEY"),
	}
	if credTmdb.Configurada() {
		log.Printf("mymediadb: busca externa TMDB habilitada")
		provs = append(provs, tmdb.New(credTmdb, nil, ""))
	}

	credTvdb := tvdb.Credencial{APIKey: os.Getenv("MYMEDIADB_TTDB_API_KEY")}
	if credTvdb.Configurada() {
		log.Printf("mymediadb: busca externa TheTVDB habilitada")
		provs = append(provs, tvdb.New(credTvdb, nil, ""))
	}

	// fanart.tv (briefing 0048): provider DEDICADO A ARTE (nunca fonte de busca). Entra no
	// registry por capacidade (ArteProvider) quando a chave PESSOAL esta no ambiente; sem
	// ela, e omitido em silencio (regressao zero). A chave so vem do env, nunca hardcoded.
	credFanart := fanarttv.Credencial{APIKey: os.Getenv("MYMEDIADB_FANARTTV_API_KEY")}
	if credFanart.Configurada() {
		log.Printf("mymediadb: enriquecimento de arte fanart.tv habilitado")
		provs = append(provs, fanarttv.New(credFanart, nil, ""))
	}

	return provs
}

// provedoresConhecidos constrói TODOS os providers que o binário sabe
// interpretar (tmdb/tvdb/fanarttv), com credencial VAZIA, INDEPENDENTE de
// qualquer chave configurada -- usados só para a capacidade de PARSE de
// payload já cacheado (MapearDetalhe/Similares/TvdbCruzadoProvider), nunca
// para busca ao vivo. Registrar sempre garante que dado já catalogado
// sobrevive à ausência/revogação de credencial (ADR-0024/briefing 0100).
func provedoresConhecidos() []provider.Provider {
	return []provider.Provider{
		tmdb.New(tmdb.Credencial{}, nil, ""),
		tvdb.New(tvdb.Credencial{}, nil, ""),
		fanarttv.New(fanarttv.Credencial{}, nil, ""),
	}
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

// resolveBackupDir determina o diretório dos snapshots do job backup_vacuum
// (briefing 0052): env MYMEDIADB_BACKUP_DIR tem prioridade; caso ausente, usa
// dirname(DB)/backups — mesma convenção de resolveAssetsDir. Cria o diretório
// se faltar.
func resolveBackupDir() (string, error) {
	dir := os.Getenv("MYMEDIADB_BACKUP_DIR")
	if dir == "" {
		dir = filepath.Join(filepath.Dir(resolveDBPath()), "backups")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// resolveYtdlpDir determina o diretório de dados do binário yt-dlp baixado
// dinamicamente (briefing 0101, ADR-0025): env MYMEDIADB_YTDLP_DIR tem
// prioridade; caso ausente, usa dirname(DB)/bin -- mesma convenção de
// resolveAssetsDir/resolveBackupDir, reaproveitando o volume que já existe
// hoje (nenhum volume novo a provisionar). Cria o diretório se faltar.
func resolveYtdlpDir() (string, error) {
	dir := os.Getenv("MYMEDIADB_YTDLP_DIR")
	if dir == "" {
		dir = filepath.Join(filepath.Dir(resolveDBPath()), "bin")
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
