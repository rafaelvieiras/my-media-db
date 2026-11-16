package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
)

// TestJornadaTrailerTemaSemYtdlp covers ADR-0025 as far as possible in black-box
// E2E: the harness (novoHarness) does NOT inject a ytdlp.Gerenciador (only
// WithSearchProvider/WithAssetsDir, like the rest of Layer 1), so NONE of these steps
// touches the real network (neither the GitHub release nor the YouTube download) --
// it exercises the SILENT DEGRADATION documented in ADR-0025 (yt-dlp missing =
// feature unavailable, never 500) and the HTTP routing of the new endpoints. The
// download logic itself (internal/ytdlp) and the match logic
// (internal/provider/themerrdb) already have dedicated unit coverage in their
// respective packages -- outside the budget of this journey (no network,
// deterministic).
func TestJornadaTrailerTemaSemYtdlp(t *testing.T) {
	c := novoHarness(t)

	// GET /settings/ytdlp: with no Gerenciador injected, it degrades to
	// unavailable/"nenhum" (montarYtdlpStatusResponse, s.ytdlp==nil).
	status := c.obj(http.MethodGet, "/api/v1/settings/ytdlp", nil, http.StatusOK)
	if d, _ := status["disponivel"].(bool); d {
		t.Errorf("disponivel = %v, quero false (sem Gerenciador injetado no harness)", status["disponivel"])
	}
	if o, _ := status["origem"].(string); o != "nenhum" {
		t.Errorf("origem = %q, quero \"nenhum\"", o)
	}

	// PUT /settings/ytdlp writes the custom path to the database even without a
	// Gerenciador (the sync with s.ytdlp is conditional on s.ytdlp != nil) -- it
	// confirms that the value persists and comes back in the next GET.
	posto := c.obj(http.MethodPut, "/api/v1/settings/ytdlp",
		map[string]string{"custom_binary_path": "/opt/yt-dlp"}, http.StatusOK)
	if p, _ := posto["custom_binary_path"].(string); p != "/opt/yt-dlp" {
		t.Errorf("custom_binary_path pos-PUT = %v, quero \"/opt/yt-dlp\"", posto["custom_binary_path"])
	}
	relido := c.obj(http.MethodGet, "/api/v1/settings/ytdlp", nil, http.StatusOK)
	if p, _ := relido["custom_binary_path"].(string); p != "/opt/yt-dlp" {
		t.Errorf("custom_binary_path releitura = %v, quero persistido em \"/opt/yt-dlp\"", relido["custom_binary_path"])
	}

	// Imports a movie (the same path as TestJornadaMidiasLocais) only to have an
	// entity with a valid id for the trailer/theme endpoints.
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// GET /entities/{id}/trailer: with no present media_file (no resolvable
	// destination folder), 404.
	statusCode, _ := c.bruto(http.MethodGet, "/api/v1/entities/"+id+"/trailer", nil)
	if statusCode != http.StatusNotFound {
		t.Errorf("GET trailer sem destino: status = %d, quero 404", statusCode)
	}

	// POST /entities/{id}/trailer/download: the yt-dlp guard runs BEFORE resolving
	// the trailer/destination -- 503, never 500, even without a media_file.
	statusCode, corpo := c.bruto(http.MethodPost, "/api/v1/entities/"+id+"/trailer/download", nil)
	if statusCode != http.StatusServiceUnavailable {
		t.Errorf("POST trailer/download sem yt-dlp: status = %d, quero 503 (corpo=%s)", statusCode, corpo)
	}

	// GET /entities/{id}/theme/candidato: with no ThemerrDB registered in fake mode
	// (opcoesBusca in main only injects fake.Provider; the harness mirrors that with
	// WithSearchProvider(prov)), disponivel=false -- silent degradation, it never
	// breaks for lack of the capability.
	candidato := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/theme/candidato", nil, http.StatusOK)
	if d, _ := candidato["disponivel"].(bool); d {
		t.Errorf("theme/candidato disponivel = %v, quero false (sem provider themerrdb no harness)", candidato["disponivel"])
	}
	if l, _ := candidato["ja_tem_local"].(bool); l {
		t.Errorf("theme/candidato ja_tem_local = %v, quero false", candidato["ja_tem_local"])
	}

	// POST /entities/{id}/theme/download: the same yt-dlp guard, BEFORE the
	// candidate -- 503.
	statusCode, corpo = c.bruto(http.MethodPost, "/api/v1/entities/"+id+"/theme/download", nil)
	if statusCode != http.StatusServiceUnavailable {
		t.Errorf("POST theme/download sem yt-dlp: status = %d, quero 503 (corpo=%s)", statusCode, corpo)
	}

	// GET /entities/{id}/theme (pre-existing endpoint, ADR-0025): with no local
	// theme.*, 404 -- contract unchanged.
	statusCode, _ = c.bruto(http.MethodGet, "/api/v1/entities/"+id+"/theme", nil)
	if statusCode != http.StatusNotFound {
		t.Errorf("GET theme sem arquivo: status = %d, quero 404", statusCode)
	}
}

// TestJornadaTrailerTemaArquivoLocalJaPresente covers the case "the owner already
// placed the file by hand" (trailer OR theme), which GET .../trailer and GET
// .../theme only DETECT and serve -- without depending on yt-dlp/ThemerrDB (the same
// spirit as theme.mp3 in ADR-0025). It needs a REAL resolvable destination folder
// (resolverDestino), so it scans an actual library (the same path as
// TestJornadaMidiasLocais) instead of only importing.
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

	// theme.mp3 placed by hand: GET /theme detects and serves it (200), and
	// .../theme/candidato starts reporting ja_tem_local=true.
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

	// Trailer placed by hand with the CANONICAL NAME (an exclusive folder -- a single
	// video in `root` -- becomes the bare name "trailer.mp4", the same rule
	// baixarTrailerAtomico would use): GET /trailer detects and serves it (200).
	trailerName := localmedia.NomeArquivoArte(localmedia.KindTrailer, true, 0, true, "Matrix (1999) [tmdbid-603]", ".mp4")
	if err := os.WriteFile(filepath.Join(root, trailerName), []byte("fake-video"), 0o644); err != nil {
		t.Fatalf("escrever trailer: %v", err)
	}
	statusCode, corpo = c.bruto(http.MethodGet, "/api/v1/entities/"+id+"/trailer", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("GET trailer com arquivo local: status = %d, quero 200 (corpo=%s)", statusCode, corpo)
	}
}
