package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngAsset is a minimal valid PNG, different from the harness pngFixture only by the
// marker byte (index 15) — used to tell two poster uploads apart in the same test
// (the content itself is irrelevant, it only has to be a valid PNG).
func pngAsset(marker byte) []byte {
	b := make([]byte, 16)
	copy(b, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	b[15] = marker
	return b
}

// uploadAsset sends a multipart/form-data (`file` field + `kind`) to POST
// /entities/{id}/assets, mirroring postAsset (internal/server/assets_test.go) —
// distinct packages (server vs e2e) require a local copy of the multipart helper,
// since the harness only speaks HTTP with the real Server (no direct access to the
// handlers). It requires 201 and returns the decoded assetDTO.
func (c *cliente) uploadAsset(entityID, kind string, content []byte) map[string]any {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if kind != "" {
		if err := mw.WriteField("kind", kind); err != nil {
			c.t.Fatalf("WriteField kind: %v", err)
		}
	}
	fw, err := mw.CreateFormFile("file", "art.png")
	if err != nil {
		c.t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		c.t.Fatalf("escrever conteudo do upload: %v", err)
	}
	if err := mw.Close(); err != nil {
		c.t.Fatalf("multipart Close: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.base+"/api/v1/entities/"+entityID+"/assets", &buf)
	if err != nil {
		c.t.Fatalf("montar request de upload: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatalf("request de upload: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("ler corpo do upload: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		c.t.Fatalf("upload de asset: status = %d, quero %d (corpo=%s)", resp.StatusCode, http.StatusCreated, raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		c.t.Fatalf("upload de asset: corpo nao e objeto: %v (corpo=%s)", err, raw)
	}
	return m
}

// TestJornadaMidiasLocais: the end-to-end journey of materializing artwork + NFO
// into the REAL library folder, following the Kodi/Emby standard. Covers: (1)
// getting a media_file state=present linked to an already imported entity; (2) the
// upload of a poster (it stays only in the internal cache); (3) POST .../materialize
// writes the binary physically (bare name — folder exclusive to a single video — and
// it becomes principal automatically, being the first poster of the kind); (4) a
// second poster + PUT .../prefer swaps the principal, ROTATING the old physical file
// (poster.<ext> -> poster1.<ext>) without recopying bytes, reflected in the ordering
// of GET /artwork (principal first, is_local_in_library=true); (5) POST .../nfo writes
// the .nfo with the resolved title, IDEMPOTENTLY (the 2nd call overwrites the same
// path).
//
// Path chosen for step (1) (documented as requested): import the movie through
// /api/v1/import (tmdb 603, creating the entity with external_ids.tmdb=603) and THEN
// scan a folder whose video file embeds the SAME id ([tmdbid-603], the Jellyfin
// convention that internal/naming already recognizes — see naming_test.go). The
// scanner matches by the EXTERNAL ID (resolverEntity/acharPorIDExterno in
// internal/scanner/match.go, maximum priority over title+year, confIDExterno=100) and
// REUSES the same entity instead of creating a new one — deterministic, without
// depending on a fuzzy-match score by title.
func TestJornadaMidiasLocais(t *testing.T) {
	c := novoHarness(t)

	// (1a) Imports the movie: entity with external_ids.tmdb=603.
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// (1b) EXCLUSIVE folder (a single video): the materialized artwork name comes
	// out bare (poster.<ext>, the real extension of the binary — here .png, the same
	// pngFixture of the harness), as the acceptance criterion requires.
	root := t.TempDir()
	videoPath := filepath.Join(root, "Matrix (1999) [tmdbid-603].mkv")
	escreverVideo(t, videoPath)

	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "movie"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// The scan matched by the embedded id: the SAME imported entity (not a new one)
	// got the present media_file.
	entPosScan := c.obj(http.MethodGet, "/api/v1/entities/"+id, nil, http.StatusOK)
	if fc, _ := entPosScan["file_count"].(float64); int(fc) != 1 {
		t.Fatalf("file_count pos-scan = %v, quero 1 (scan devia ligar o arquivo a entity importada, nao criar outra)", entPosScan["file_count"])
	}

	// (2) Upload of the first poster: it stays only in the internal cache (local_path nil).
	posterA := c.uploadAsset(id, "poster", pngFixture)
	assetAID := str(t, posterA, "id")
	if posterA["local_path"] != nil {
		t.Fatalf("local_path do upload = %v, quero nil (so no cache ate materializar)", posterA["local_path"])
	}

	// (3) Materializes: it writes physically into the real folder with a bare name and
	// becomes principal automatically (first poster of the kind).
	materializado := c.obj(http.MethodPost,
		"/api/v1/entities/"+id+"/assets/"+assetAID+"/materialize", nil, http.StatusOK)
	localA, ok := materializado["local_path"].(string)
	if !ok || localA == "" {
		t.Fatalf("local_path pos-materialize = %v, quero caminho preenchido", materializado["local_path"])
	}
	if filepath.Base(localA) != "poster.png" {
		t.Errorf("nome do poster materializado = %q, quero \"poster.png\" (pasta exclusiva, bare)", filepath.Base(localA))
	}
	if _, err := os.Stat(localA); err != nil {
		t.Fatalf("poster materializado nao existe em disco (%s): %v", localA, err)
	}
	if principal, _ := materializado["is_principal"].(bool); !principal {
		t.Errorf("is_principal pos-materialize = %v, quero true (primeiro poster do kind)", materializado["is_principal"])
	}

	// (4) Second poster + PUT /prefer: it materializes and takes the bare name; the
	// old principal is ROTATED physically (renamed, not recopied).
	posterB := c.uploadAsset(id, "poster", pngAsset(0x42))
	assetBID := str(t, posterB, "id")

	preferido := c.obj(http.MethodPut,
		"/api/v1/entities/"+id+"/assets/"+assetBID+"/prefer", nil, http.StatusOK)
	localB, _ := preferido["local_path"].(string)
	if filepath.Base(localB) != "poster.png" {
		t.Errorf("nome do novo principal = %q, quero \"poster.png\"", filepath.Base(localB))
	}
	// The bare name is REUSED by the new principal (same directory/kind): the old
	// content that was in localA was RENAMED to poster1.png (below) before localB
	// took over the same bare path with the NEW content — that is why
	// localB == localA (same path, different owner).
	if localB != localA {
		t.Errorf("local_path do novo principal = %q, quero igual ao antigo bare %q (nome reusado)", localB, localA)
	}
	if principal, _ := preferido["is_principal"].(bool); !principal {
		t.Errorf("is_principal do novo principal = %v, quero true", preferido["is_principal"])
	}

	// The old CONTENT (posterA) was preserved, only RENAMED to the next free index
	// (poster1.png) — never deleted nor recopied.
	rotacionado := filepath.Join(filepath.Dir(localA), "poster1.png")
	rotacionadoBytes, err := os.ReadFile(rotacionado)
	if err != nil {
		t.Fatalf("antigo principal deveria ter sido renomeado p/ poster1.png (%s): %v", rotacionado, err)
	}
	if !bytes.Equal(rotacionadoBytes, pngFixture) {
		t.Errorf("conteudo de poster1.png nao bate com o do posterA original (rename deveria preservar bytes)")
	}
	// localA/localB (same path) now hold the content of the NEW principal (posterB).
	novoConteudo, err := os.ReadFile(localB)
	if err != nil {
		t.Fatalf("novo principal nao existe em disco (%s): %v", localB, err)
	}
	if bytes.Equal(novoConteudo, pngFixture) {
		t.Errorf("conteudo de %s ainda e o do posterA: a materializacao do novo principal deveria ter sobrescrito", localB)
	}

	// GET /artwork confirms the ordering (principal first) and the new flags
	// (is_local_in_library/is_principal).
	artwork := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/artwork", nil, http.StatusOK)
	posters, ok := artwork["poster"].([]any)
	if !ok || len(posters) < 2 {
		t.Fatalf("artwork[poster] = %v, quero >=2 itens (os 2 posters locais)", artwork["poster"])
	}
	primeiro, _ := posters[0].(map[string]any)
	if assetID, _ := primeiro["asset_id"].(string); assetID != assetBID {
		t.Errorf("1o poster de /artwork = %q, quero %q (o novo principal vem primeiro)", assetID, assetBID)
	}
	if p, _ := primeiro["is_principal"].(bool); !p {
		t.Errorf("is_principal do 1o poster de /artwork = %v, quero true", primeiro["is_principal"])
	}
	if p, _ := primeiro["is_local_in_library"].(bool); !p {
		t.Errorf("is_local_in_library do 1o poster de /artwork = %v, quero true", primeiro["is_local_in_library"])
	}

	// (5) NFO: it writes the .nfo into the real folder, with the resolved title (pt-BR
	// default: "Matrix", TestJornadaImportarFilme already proves that resolution).
	nfo := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/nfo", nil, http.StatusOK)
	nfoPath := str(t, nfo, "nfo_path")
	if filepath.Base(nfoPath) != "Matrix (1999) [tmdbid-603].nfo" {
		t.Errorf("nome do nfo = %q, quero baseado no nome do video (filme != serie)", filepath.Base(nfoPath))
	}
	corpo, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("nfo nao existe em disco (%s): %v", nfoPath, err)
	}
	if !strings.Contains(string(corpo), "<title>Matrix</title>") {
		t.Errorf("conteudo do nfo nao contem <title>Matrix</title>: %s", corpo)
	}

	// Idempotent: calling it again overwrites the SAME path, without duplicating.
	nfo2 := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/nfo", nil, http.StatusOK)
	if got := str(t, nfo2, "nfo_path"); got != nfoPath {
		t.Errorf("2a chamada de /nfo mudou o caminho: %q != %q (deveria ser idempotente)", got, nfoPath)
	}

	// (6) The path the nightly JOB takes (MaterializarMidiasFaltantes), over a library that
	// is already in place: the NFO above is convergent at the canonical destination and the
	// poster is already materialized in the folder. It must answer "nothing to do" and
	// write nothing -- that answer is where a run over 19.018 entities spends its time, and
	// a single false positive there rewrites the owner's whole library every night.
	job := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/materialize-missing-media", nil, http.StatusOK)
	if gravado, _ := job["nfo_gravado"].(bool); gravado {
		t.Errorf("materialize-missing-media regravou um NFO ja convergente (%s)", nfoPath)
	}
	if artes, _ := job["artes_baixadas"].([]any); len(artes) != 0 {
		t.Errorf("materialize-missing-media baixou artes com tudo em disco: %v", job["artes_baixadas"])
	}
	if _, err := os.Stat(nfoPath); err != nil {
		t.Errorf("o NFO convergente sumiu depois da passada do job: %v", err)
	}
}
