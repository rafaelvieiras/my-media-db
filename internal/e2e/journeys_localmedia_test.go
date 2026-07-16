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

// pngAsset é um PNG mínimo válido, distinto do pngFixture do harness só pelo byte
// marcador (índice 15) — usado para diferenciar dois uploads de poster no mesmo
// teste (o conteúdo em si é irrelevante, só precisa ser um PNG válido).
func pngAsset(marker byte) []byte {
	b := make([]byte, 16)
	copy(b, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	b[15] = marker
	return b
}

// uploadAsset envia um multipart/form-data (campo `file` + `kind`) para POST
// /entities/{id}/assets, espelhando postAsset (internal/server/assets_test.go) —
// pacotes distintos (server vs e2e) exigem uma cópia local do helper multipart,
// já que o harness só fala HTTP com o Server real (nenhum acesso direto aos
// handlers). Exige 201 e devolve o assetDTO decodificado.
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

// TestJornadaMidiasLocais: a jornada ponta-a-ponta do briefing 0062 (materializar
// arte + NFO na pasta REAL da biblioteca, no padrão Kodi/Emby). Cobre: (1) obter um
// media_file state=present ligado a uma entity já importada; (2) upload de um poster
// (fica só no cache interno); (3) POST .../materialize grava o binário fisicamente
// (nome bare — pasta exclusiva de um único vídeo — e vira principal automaticamente,
// por ser o primeiro poster do kind); (4) um segundo poster + PUT .../prefer troca o
// principal, ROTACIONANDO o arquivo físico antigo (poster.<ext> -> poster1.<ext>) sem
// recopiar bytes, refletido na ordenação de GET /artwork (principal primeiro,
// is_local_in_library=true); (5) POST .../nfo grava o .nfo com o título resolvido,
// de forma IDEMPOTENTE (2a chamada sobrescreve o mesmo caminho).
//
// Caminho escolhido para o passo (1) (documentado conforme pedido): importar o
// filme via /api/v1/import (tmdb 603, cria a entity com external_ids.tmdb=603) e
// DEPOIS escanear uma pasta cujo arquivo de vídeo embute o MESMO id
// ([tmdbid-603], convenção Jellyfin que internal/naming já reconhece — ver
// naming_test.go). O scanner casa pelo ID EXTERNO (resolverEntity/
// acharPorIDExterno em internal/scanner/match.go, prioridade máxima sobre
// título+ano, confIDExterno=100) e REUSA a mesma entity em vez de criar uma
// nova — determinístico, sem depender de score de fuzzy-match por título.
func TestJornadaMidiasLocais(t *testing.T) {
	c := novoHarness(t)

	// (1a) Importa o filme: entity com external_ids.tmdb=603.
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// (1b) Pasta EXCLUSIVA (um único vídeo): o nome de arte materializada sai
	// bare (poster.<ext>, extensão real do binário — aqui .png, o mesmo
	// pngFixture do harness), conforme critério de aceite do briefing 0062.
	root := t.TempDir()
	videoPath := filepath.Join(root, "Matrix (1999) [tmdbid-603].mkv")
	escreverVideo(t, videoPath)

	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "movie"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// O scan casou pelo id embutido: a MESMA entity importada (nao uma nova)
	// ganhou o media_file present.
	entPosScan := c.obj(http.MethodGet, "/api/v1/entities/"+id, nil, http.StatusOK)
	if fc, _ := entPosScan["file_count"].(float64); int(fc) != 1 {
		t.Fatalf("file_count pos-scan = %v, quero 1 (scan devia ligar o arquivo a entity importada, nao criar outra)", entPosScan["file_count"])
	}

	// (2) Upload do primeiro poster: fica só no cache interno (local_path nil).
	posterA := c.uploadAsset(id, "poster", pngFixture)
	assetAID := str(t, posterA, "id")
	if posterA["local_path"] != nil {
		t.Fatalf("local_path do upload = %v, quero nil (so no cache ate materializar)", posterA["local_path"])
	}

	// (3) Materializa: grava fisicamente na pasta real com nome bare e vira
	// principal automaticamente (primeiro poster do kind).
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

	// (4) Segundo poster + PUT /prefer: materializa e assume o nome bare; o
	// antigo principal é ROTACIONADO fisicamente (renomeado, nao recopiado).
	posterB := c.uploadAsset(id, "poster", pngAsset(0x42))
	assetBID := str(t, posterB, "id")

	preferido := c.obj(http.MethodPut,
		"/api/v1/entities/"+id+"/assets/"+assetBID+"/prefer", nil, http.StatusOK)
	localB, _ := preferido["local_path"].(string)
	if filepath.Base(localB) != "poster.png" {
		t.Errorf("nome do novo principal = %q, quero \"poster.png\"", filepath.Base(localB))
	}
	// O nome bare é REUSADO pelo novo principal (mesmo diretório/kind): o
	// antigo conteúdo que estava em localA foi RENOMEADO para poster1.png
	// (abaixo) antes de localB assumir o mesmo caminho bare com o conteúdo
	// NOVO — por isso localB == localA (mesmo path, dono trocado).
	if localB != localA {
		t.Errorf("local_path do novo principal = %q, quero igual ao antigo bare %q (nome reusado)", localB, localA)
	}
	if principal, _ := preferido["is_principal"].(bool); !principal {
		t.Errorf("is_principal do novo principal = %v, quero true", preferido["is_principal"])
	}

	// O CONTEÚDO antigo (posterA) foi preservado, só RENOMEADO para o próximo
	// índice livre (poster1.png) — nunca apagado nem recopiado.
	rotacionado := filepath.Join(filepath.Dir(localA), "poster1.png")
	rotacionadoBytes, err := os.ReadFile(rotacionado)
	if err != nil {
		t.Fatalf("antigo principal deveria ter sido renomeado p/ poster1.png (%s): %v", rotacionado, err)
	}
	if !bytes.Equal(rotacionadoBytes, pngFixture) {
		t.Errorf("conteudo de poster1.png nao bate com o do posterA original (rename deveria preservar bytes)")
	}
	// localA/localB (mesmo caminho) agora tem o conteúdo do NOVO principal (posterB).
	novoConteudo, err := os.ReadFile(localB)
	if err != nil {
		t.Fatalf("novo principal nao existe em disco (%s): %v", localB, err)
	}
	if bytes.Equal(novoConteudo, pngFixture) {
		t.Errorf("conteudo de %s ainda e o do posterA: a materializacao do novo principal deveria ter sobrescrito", localB)
	}

	// GET /artwork confirma a ordenacao (principal primeiro) e os selos novos
	// (is_local_in_library/is_principal, briefing 0062 §4).
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

	// (5) NFO: grava o .nfo na pasta real, com o titulo resolvido (pt-BR
	// default: "Matrix", TestJornadaImportarFilme ja prova essa resolucao).
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

	// Idempotente: chamar de novo sobrescreve o MESMO caminho, sem duplicar.
	nfo2 := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/nfo", nil, http.StatusOK)
	if got := str(t, nfo2, "nfo_path"); got != nfoPath {
		t.Errorf("2a chamada de /nfo mudou o caminho: %q != %q (deveria ser idempotente)", got, nfoPath)
	}
}
