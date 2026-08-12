package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
)

// TestJornadaTrailerTemaSemYtdlp cobre os briefings 0101/0102/0103
// (ADR-0025) na medida do possível em E2E black-box: o harness (novoHarness)
// NÃO injeta um ytdlp.Gerenciador (só WithSearchProvider/WithAssetsDir, como
// o resto da Camada 1), então NENHUM destes passos toca rede real (nem o
// release do GitHub, nem o download do YouTube) -- exercita a DEGRADAÇÃO EM
// SILÊNCIO documentada no ADR-0025 (yt-dlp ausente = feature indisponível,
// nunca 500) e o roteamento HTTP dos endpoints novos. A lógica PRÓPRIA de
// download (internal/ytdlp) e de match (internal/provider/themerrdb) já tem
// cobertura unitária dedicada nos respectivos pacotes -- fora do orçamento
// desta jornada (sem rede, determinística).
func TestJornadaTrailerTemaSemYtdlp(t *testing.T) {
	c := novoHarness(t)

	// GET /settings/ytdlp: sem Gerenciador injetado, degrada para
	// indisponivel/"nenhum" (montarYtdlpStatusResponse, s.ytdlp==nil).
	status := c.obj(http.MethodGet, "/api/v1/settings/ytdlp", nil, http.StatusOK)
	if d, _ := status["disponivel"].(bool); d {
		t.Errorf("disponivel = %v, quero false (sem Gerenciador injetado no harness)", status["disponivel"])
	}
	if o, _ := status["origem"].(string); o != "nenhum" {
		t.Errorf("origem = %q, quero \"nenhum\"", o)
	}

	// PUT /settings/ytdlp grava o path customizado no banco mesmo sem
	// Gerenciador (a sincronização com s.ytdlp é condicionada a s.ytdlp !=
	// nil) -- confirma que o valor persiste e volta no GET seguinte.
	posto := c.obj(http.MethodPut, "/api/v1/settings/ytdlp",
		map[string]string{"custom_binary_path": "/opt/yt-dlp"}, http.StatusOK)
	if p, _ := posto["custom_binary_path"].(string); p != "/opt/yt-dlp" {
		t.Errorf("custom_binary_path pos-PUT = %v, quero \"/opt/yt-dlp\"", posto["custom_binary_path"])
	}
	relido := c.obj(http.MethodGet, "/api/v1/settings/ytdlp", nil, http.StatusOK)
	if p, _ := relido["custom_binary_path"].(string); p != "/opt/yt-dlp" {
		t.Errorf("custom_binary_path releitura = %v, quero persistido em \"/opt/yt-dlp\"", relido["custom_binary_path"])
	}

	// Importa um filme (mesmo caminho de TestJornadaMidiasLocais) só para
	// ter uma entity com id válido para os endpoints de trailer/tema.
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// GET /entities/{id}/trailer: sem media_file present (sem pasta de
	// destino resolvível), 404.
	statusCode, _ := c.bruto(http.MethodGet, "/api/v1/entities/"+id+"/trailer", nil)
	if statusCode != http.StatusNotFound {
		t.Errorf("GET trailer sem destino: status = %d, quero 404", statusCode)
	}

	// POST /entities/{id}/trailer/download: o guard de yt-dlp roda ANTES de
	// resolver trailer/destino -- 503, nunca 500, mesmo sem media_file.
	statusCode, corpo := c.bruto(http.MethodPost, "/api/v1/entities/"+id+"/trailer/download", nil)
	if statusCode != http.StatusServiceUnavailable {
		t.Errorf("POST trailer/download sem yt-dlp: status = %d, quero 503 (corpo=%s)", statusCode, corpo)
	}

	// GET /entities/{id}/theme/candidato: sem ThemerrDB registrado no modo
	// fake (opcoesBusca do main só injeta fake.Provider; o harness espelha
	// isso com WithSearchProvider(prov)), disponivel=false -- degradação em
	// silêncio, nunca quebra por falta da capacidade.
	candidato := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/theme/candidato", nil, http.StatusOK)
	if d, _ := candidato["disponivel"].(bool); d {
		t.Errorf("theme/candidato disponivel = %v, quero false (sem provider themerrdb no harness)", candidato["disponivel"])
	}
	if l, _ := candidato["ja_tem_local"].(bool); l {
		t.Errorf("theme/candidato ja_tem_local = %v, quero false", candidato["ja_tem_local"])
	}

	// POST /entities/{id}/theme/download: mesmo guard de yt-dlp, ANTES do
	// candidato -- 503.
	statusCode, corpo = c.bruto(http.MethodPost, "/api/v1/entities/"+id+"/theme/download", nil)
	if statusCode != http.StatusServiceUnavailable {
		t.Errorf("POST theme/download sem yt-dlp: status = %d, quero 503 (corpo=%s)", statusCode, corpo)
	}

	// GET /entities/{id}/theme (endpoint pré-existente, briefing 0082): sem
	// theme.* local, 404 -- contrato inalterado por 0103.
	statusCode, _ = c.bruto(http.MethodGet, "/api/v1/entities/"+id+"/theme", nil)
	if statusCode != http.StatusNotFound {
		t.Errorf("GET theme sem arquivo: status = %d, quero 404", statusCode)
	}
}

// TestJornadaTrailerTemaArquivoLocalJaPresente cobre o caso "o dono já
// colocou o arquivo à mão" (trailer OU tema), que GET .../trailer e GET
// .../theme só DETECTAM e servem -- sem depender de yt-dlp/ThemerrDB (mesmo
// espírito de theme.mp3 desde o briefing 0082). Precisa de uma pasta de
// destino REAL resolvível (resolverDestino), por isso escaneia uma
// biblioteca de verdade (mesmo caminho de TestJornadaMidiasLocais) em vez de
// só importar.
func TestJornadaTrailerTemaArquivoLocalJaPresente(t *testing.T) {
	c := novoHarness(t)

	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	root := t.TempDir()
	videoPath := filepath.Join(root, "Matrix (1999) [tmdbid-603].mkv")
	escreverVideo(t, videoPath)

	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "movie"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// theme.mp3 colocado à mão: GET /theme detecta e serve (200), e
	// .../theme/candidato passa a reportar ja_tem_local=true.
	if err := os.WriteFile(filepath.Join(root, "theme.mp3"), []byte("fake-audio"), 0o644); err != nil {
		t.Fatalf("escrever theme.mp3: %v", err)
	}
	statusCode, corpo := c.bruto(http.MethodGet, "/api/v1/entities/"+id+"/theme", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("GET theme com arquivo local: status = %d, quero 200 (corpo=%s)", statusCode, corpo)
	}
	candidato := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/theme/candidato", nil, http.StatusOK)
	if l, _ := candidato["ja_tem_local"].(bool); !l {
		t.Errorf("theme/candidato ja_tem_local = %v, quero true (theme.mp3 já presente)", candidato["ja_tem_local"])
	}

	// Trailer colocado à mão com o NOME CANÔNICO (pasta exclusiva -- um
	// único vídeo em `root` -- vira nome bare "trailer.mp4", mesma regra que
	// baixarTrailerAtomico usaria): GET /trailer detecta e serve (200).
	trailerName := localmedia.NomeArquivoArte(localmedia.KindTrailer, true, 0, true, "Matrix (1999) [tmdbid-603]", ".mp4")
	if err := os.WriteFile(filepath.Join(root, trailerName), []byte("fake-video"), 0o644); err != nil {
		t.Fatalf("escrever trailer: %v", err)
	}
	statusCode, corpo = c.bruto(http.MethodGet, "/api/v1/entities/"+id+"/trailer", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("GET trailer com arquivo local: status = %d, quero 200 (corpo=%s)", statusCode, corpo)
	}
}
