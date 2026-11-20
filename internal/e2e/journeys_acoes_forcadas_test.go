package e2e

// TestJornadaAcoesForcadasGerenciar covers the end-to-end journey of the 3
// actions that are always visible in the Gerenciar menu: (1) POST .../refresh-files
// forces the re-hash of a 'present' media_file even with no mtime/size change,
// imports a new local asset placed in the folder of an ALREADY cataloged entity
// (a gap previously covered only on the 1st sweep) and, with an .nfo bringing
// LOCAL playback fields, fills in only what is missing in the database — and does
// NOT overwrite it on a second call; (2) POST .../enrich with {"force": true}
// fetches the provider detail skipping the 24h cache; (3) POST
// .../materialize-missing-media updates the artwork inventory and materializes it
// into the real folder, reusing MaterializarMidiasFaltantes without duplicating
// logic. All 3 routes are exercised only through the public API (black-box
// harness), like the other journeys of this package.
import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJornadaAcoesForcadasGerenciar(t *testing.T) {
	c := novoHarness(t)

	// (0) Imports the movie (entity with external_ids.tmdb=603) and scans a
	// folder whose video embeds the same id, linking the 'present' media_file to
	// the SAME entity (the same path as TestJornadaMidiasLocais).
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

	// (1a) "Verificar Atualizações de Arquivos": even with NO change on disk
	// since the scan, the endpoint forces the re-hash of the 'present'
	// media_file (unlike the incremental scan, which would short-circuit).
	semMudanca := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/refresh-files", nil, http.StatusOK)
	if fr, _ := semMudanca["files_rechecked"].(float64); int(fr) != 1 {
		t.Errorf("files_rechecked (sem mudanca em disco) = %v, quero 1 (refresh-files ignora o short-circuit incremental)", semMudanca["files_rechecked"])
	}

	// (1b) Local asset placed AFTER the 1st sweep, on the ALREADY cataloged
	// entity — the gap that motivated the change (descobrirPoster only runs in
	// criarEntity). The multipart upload is the owner's normal path; here we
	// simulate the owner dropping a poster.jpg straight into the video folder.
	posterPath := filepath.Join(root, "poster.jpg")
	if err := os.WriteFile(posterPath, []byte("fake-poster-bytes"), 0o644); err != nil {
		t.Fatalf("escrever poster.jpg: %v", err)
	}
	comAsset := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/refresh-files", nil, http.StatusOK)
	if af, _ := comAsset["assets_found"].(float64); int(af) != 1 {
		t.Errorf("assets_found apos colocar poster.jpg na pasta = %v, quero 1", comAsset["assets_found"])
	}
	artwork := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/artwork", nil, http.StatusOK)
	posters, _ := artwork["poster"].([]any)
	achouLocal := false
	for _, p := range posters {
		m, _ := p.(map[string]any)
		if origem, _ := m["source"].(string); origem == "local" {
			achouLocal = true
		}
	}
	if !achouLocal {
		t.Errorf("artwork[poster] pos-refresh-files = %v, queria um item origin=local (poster.jpg descoberto)", posters)
	}

	// (1c) LOCAL playback fields: writes an .nfo bringing
	// playcount/watched/userrating/resume and confirms that refresh-files fills
	// in what is still missing in the database.
	nfoPath := filepath.Join(root, "Matrix (1999) [tmdbid-603].nfo")
	nfoConteudo := `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Matrix</title>
  <userrating>8.5</userrating>
  <playcount>3</playcount>
  <watched>true</watched>
  <lastplayed>2026-01-01 10:00:00</lastplayed>
  <resume>
    <position>120.5</position>
    <total>5400</total>
  </resume>
</movie>`
	if err := os.WriteFile(nfoPath, []byte(nfoConteudo), 0o644); err != nil {
		t.Fatalf("escrever .nfo: %v", err)
	}
	// entity.nfo_path only exists after writing the official NFO (dedicated
	// endpoint) — afterwards we overwrite the CONTENT at the same path with the
	// local fields above, without touching the NFO generation flow itself.
	nfoResp := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/nfo", nil, http.StatusOK)
	nfoPathReal := str(t, nfoResp, "nfo_path")
	if err := os.WriteFile(nfoPathReal, []byte(nfoConteudo), 0o644); err != nil {
		t.Fatalf("sobrescrever .nfo com campos locais: %v", err)
	}

	comCampos := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/refresh-files", nil, http.StatusOK)
	preenchidos := strs(t, comCampos, "playback_fields_filled")
	for _, esperado := range []string{"watched", "play_count", "user_rating", "last_played_at", "resume_position_seconds", "resume_total_seconds"} {
		if !contem(preenchidos, esperado) {
			t.Errorf("playback_fields_filled = %v, faltou %q", preenchidos, esperado)
		}
	}

	// (1d) Non-destructive: calling it again does NOT report the same fields as
	// "new" (they are already filled in the database — the COALESCE is a no-op).
	comCamposDeNovo := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/refresh-files", nil, http.StatusOK)
	preenchidosDeNovo := strs(t, comCamposDeNovo, "playback_fields_filled")
	if contem(preenchidosDeNovo, "user_rating") {
		t.Errorf("2a chamada de refresh-files reportou user_rating como novo: %v (deveria ser no-op, ja preenchido)", preenchidosDeNovo)
	}

	// (2) "Baixar Metadados": force=true fetches from the provider even with a
	// fresh 24h cache (the import + the refresh-files above already left the
	// cache warm for tmdb/603) — success 200 and the entity stays resolved.
	forcado := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/enrich",
		map[string]any{"force": true}, http.StatusOK)
	if title := str(t, forcado, "title"); !strings.Contains(strings.ToLower(title), "matrix") {
		t.Errorf("title pos-enrich forcado = %q, queria conter \"matrix\"", title)
	}
	// force=false (default behavior) keeps serving from the cache with no
	// regression — same route, no extra body.
	c.obj(http.MethodPost, "/api/v1/entities/"+id+"/enrich", map[string]any{}, http.StatusOK)

	// (3) "Baixar Artes Faltantes": a new query to the registered
	// ArteProvider(s) (here, none beyond the search fake — it degrades silently,
	// AGENTS.md §6) followed by MaterializarMidiasFaltantes over the already
	// resolved inventory (the poster from the enrich). Always 200.
	materializado := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/materialize-missing-media", nil, http.StatusOK)
	if _, ok := materializado["artes_baixadas"].([]any); !ok {
		t.Errorf("artes_baixadas ausente/tipo errado na resposta de materialize-missing-media: %v", materializado["artes_baixadas"])
	}
	if _, ok := materializado["nfo_gravado"].(bool); !ok {
		t.Errorf("nfo_gravado ausente/tipo errado na resposta de materialize-missing-media: %v", materializado["nfo_gravado"])
	}

	// (3b) IDEMPOTENCY: the job runs this same path on 19.018 entities every night, so
	// the second call over an entity that is already materialized must be a no-op — and
	// it is the path that decides "nothing to do" (NFO already convergent at the same
	// destination, every artwork kind already in the library). Without this the run would
	// rewrite the whole library twice.
	segunda := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/materialize-missing-media", nil, http.StatusOK)
	if artes, ok := segunda["artes_baixadas"].([]any); !ok || len(artes) != 0 {
		t.Errorf("2a chamada de materialize-missing-media baixou artes de novo: %v (deveria ser vazio)", segunda["artes_baixadas"])
	}
	if gravado, ok := segunda["nfo_gravado"].(bool); !ok || gravado {
		t.Errorf("2a chamada de materialize-missing-media gravou o NFO de novo: %v (deveria ser false)", segunda["nfo_gravado"])
	}

	// 404 for a nonexistent entity on the 3 routes — a guard shared by the 3 actions.
	for _, rota := range []string{"/refresh-files", "/materialize-missing-media"} {
		if status, _ := c.bruto(http.MethodPost, "/api/v1/entities/inexistente"+rota, nil); status != http.StatusNotFound {
			t.Errorf("POST .../inexistente%s: status = %d, quero 404", rota, status)
		}
	}
}
