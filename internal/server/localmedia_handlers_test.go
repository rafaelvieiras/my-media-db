package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// decodeInto decodifica o corpo da resposta em v (t.Fatal em erro).
func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decodificar corpo: %v (corpo=%s)", err, rec.Body.String())
	}
}

// Testes de integração do briefing 0062 (mídias locais: NFO + artes na pasta):
// materialização física de arte, rotação ao trocar a principal (PUT /prefer
// estendido), geração de .nfo, ordenação de GET /artwork e as falhas
// esperadas (404 sem media_file, sem .tmp-* órfão em falha de I/O).

// libDirComVideo cria um diretório de biblioteca (distinto do assetsDir de
// cache) e semeia um media_file 'present' apontando para ele — a pasta de
// destino da materialização/NFO.
func libDirComVideo(t *testing.T, d *db.DB, entityID, mfID, nomeVideo string) string {
	t.Helper()
	dir := t.TempDir()
	semearFile(t, d, mfID, entityID, filepath.Join(dir, nomeVideo), "present", 1.0, "")
	return dir
}

// ── POST .../assets/{assetId}/materialize ────────────────────────────────

// TestMaterializePastaExclusivaGravaBare cobre o criterio 5.1: pasta com um
// unico video -> "salvar na pasta" de um poster grava poster.jpg (bare).
func TestMaterializePastaExclusivaGravaBare(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	a := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	if a.LocalPath != nil {
		t.Fatalf("local_path recem-criado = %v, quero nil", a.LocalPath)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/assets/"+a.ID+"/materialize", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAsset(t, rec)
	if got.LocalPath == nil {
		t.Fatal("local_path = nil apos materialize, quero preenchido")
	}
	quero := filepath.Join(libDir, "poster.png")
	if *got.LocalPath != quero {
		t.Errorf("local_path = %q, quero %q (bare, pasta exclusiva)", *got.LocalPath, quero)
	}
	if !got.IsPrincipal {
		t.Error("is_principal = false, quero true (primeira arte do kind, torna-se principal)")
	}
	if _, err := os.Stat(quero); err != nil {
		t.Errorf("arquivo fisico ausente em %s: %v", quero, err)
	}

	// Idempotente: 2a chamada devolve o mesmo local_path sem regravar.
	rec2 := do(t, h, http.MethodPost, "/api/v1/entities/e1/assets/"+a.ID+"/materialize", nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("2a chamada status = %d, quero 200", rec2.Code)
	}
	got2 := decodeAsset(t, rec2)
	if got2.LocalPath == nil || *got2.LocalPath != quero {
		t.Errorf("2a chamada local_path = %v, quero %q (idempotente)", got2.LocalPath, quero)
	}
}

// TestMaterializePastaNaoExclusivaPrefixaVideo cobre o criterio 5.1: pasta
// com dois videos -> nome prefixado <nome-do-video>-poster.jpg.
func TestMaterializePastaNaoExclusivaPrefixaVideo(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	dir := t.TempDir()
	semearFile(t, d, "mf1", "e1", filepath.Join(dir, "Matrix.1999.EXTENDED.mkv"), "present", 1.0, "")
	// Segundo video na MESMA pasta (outra entity) -- pasta deixa de ser exclusiva.
	semearEntity(t, d, "e2", "movie", "Outra Edicao")
	semearFile(t, d, "mf2", "e2", filepath.Join(dir, "Matrix.1999.mkv"), "present", 1.0, "")

	a := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/assets/"+a.ID+"/materialize", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAsset(t, rec)
	quero := filepath.Join(dir, "Matrix.1999.EXTENDED-poster.png")
	if got.LocalPath == nil || *got.LocalPath != quero {
		t.Errorf("local_path = %v, quero %q (prefixado pelo video)", got.LocalPath, quero)
	}
}

// TestMaterializeSemMediaFile404 cobre o criterio 5.5: entity sem media_file
// present -> 404, nada e escrito.
func TestMaterializeSemMediaFile404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Sem Arquivo")

	a := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/assets/"+a.ID+"/materialize", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestMaterializeAssetInexistente404 cobre o 404 de asset/entity inexistente.
func TestMaterializeAssetInexistente404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/assets/naoexiste/materialize", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	rec2 := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/assets/x/materialize", nil)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("entity inexistente: status = %d, quero 404", rec2.Code)
	}
}

// ── PUT .../assets/{assetId}/prefer (rotacao, briefing 0062) ────────────────

// TestPreferRotacao3xSemColisao cobre o criterio 5.3: marcar principal 3x
// produz sufixos crescentes sem colisao.
func TestPreferRotacao3xSemColisao(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	a1 := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	a2 := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	a3 := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	a4 := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))

	prefer := func(id string) assetDTO {
		t.Helper()
		rec := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/"+id+"/prefer", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("prefer(%s) status = %d, quero 200 (corpo=%s)", id, rec.Code, rec.Body.String())
		}
		return decodeAsset(t, rec)
	}

	// a1 vira principal (materializa direto no clique unico -- ainda nao
	// estava salvo na pasta).
	p1 := prefer(a1.ID)
	quero := filepath.Join(libDir, "poster.png")
	if p1.LocalPath == nil || *p1.LocalPath != quero {
		t.Fatalf("a1.local_path = %v, quero %q", p1.LocalPath, quero)
	}
	if !p1.IsPrincipal {
		t.Fatal("a1.is_principal = false, quero true")
	}

	// a2 vira principal: a1 e rotacionado para poster1.jpg.
	p2 := prefer(a2.ID)
	if p2.LocalPath == nil || *p2.LocalPath != quero {
		t.Fatalf("a2.local_path = %v, quero %q (bare)", p2.LocalPath, quero)
	}
	lista := decodeAssets(t, do(t, h, http.MethodGet, "/api/v1/entities/e1/assets", nil))
	a1Depois := acharAsset(t, lista, a1.ID)
	if a1Depois.LocalPath == nil || *a1Depois.LocalPath != filepath.Join(libDir, "poster1.png") {
		t.Errorf("a1.local_path apos rotacao = %v, quero poster1.jpg", a1Depois.LocalPath)
	}
	if a1Depois.IsPrincipal {
		t.Error("a1.is_principal apos rotacao = true, quero false")
	}

	// a3 vira principal: a2 rotaciona para o PROXIMO indice livre (2, nao
	// reaproveita o 1 que a1 ja usa).
	prefer(a3.ID)
	lista = decodeAssets(t, do(t, h, http.MethodGet, "/api/v1/entities/e1/assets", nil))
	a2Depois := acharAsset(t, lista, a2.ID)
	if a2Depois.LocalPath == nil || *a2Depois.LocalPath != filepath.Join(libDir, "poster2.png") {
		t.Errorf("a2.local_path apos 2a rotacao = %v, quero poster2.jpg", a2Depois.LocalPath)
	}

	// a4 vira principal: a3 rotaciona para o indice 3 -- crescente, sem colisao.
	prefer(a4.ID)
	lista = decodeAssets(t, do(t, h, http.MethodGet, "/api/v1/entities/e1/assets", nil))
	a3Depois := acharAsset(t, lista, a3.ID)
	if a3Depois.LocalPath == nil || *a3Depois.LocalPath != filepath.Join(libDir, "poster3.png") {
		t.Errorf("a3.local_path apos 3a rotacao = %v, quero poster3.jpg", a3Depois.LocalPath)
	}
	a4Depois := acharAsset(t, lista, a4.ID)
	if a4Depois.LocalPath == nil || *a4Depois.LocalPath != quero {
		t.Errorf("a4.local_path = %v, quero %q (bare, e o principal atual)", a4Depois.LocalPath, quero)
	}
	if !a4Depois.IsPrincipal {
		t.Error("a4.is_principal = false, quero true (principal atual)")
	}

	// So um principal por kind.
	principais := 0
	for _, x := range lista {
		if x.Kind == "poster" && x.IsPrincipal {
			principais++
		}
	}
	if principais != 1 {
		t.Errorf("assets is_principal=true do kind poster = %d, quero 1", principais)
	}

	// Todos os arquivos fisicos coexistem sem colisao.
	entradas, err := os.ReadDir(libDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	nomes := map[string]bool{}
	for _, e := range entradas {
		nomes[e.Name()] = true
	}
	for _, nome := range []string{"poster.png", "poster1.png", "poster2.png", "poster3.png"} {
		if !nomes[nome] {
			t.Errorf("arquivo %q ausente na pasta apos as rotacoes: %v", nome, nomes)
		}
	}
}

// TestPreferSemMediaFile404 cobre a extensao do 0062: prefer numa entity sem
// media_file present -> 404 (sem pasta de destino).
func TestPreferSemMediaFile404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Sem Arquivo")
	a := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))

	rec := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/"+a.ID+"/prefer", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// ── POST /entities/{id}/nfo ──────────────────────────────────────────────

// TestWriteEntityNfoMovieHappyPath: POST /nfo grava movie.nfo no diretorio do
// video, e a 2a chamada e idempotente (sobrescreve, nao duplica).
func TestWriteEntityNfoMovieHappyPath(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/nfo", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var resultado nfoWriteResult
	decodeInto(t, rec, &resultado)
	queroPath := filepath.Join(libDir, "Matrix.1999.nfo")
	if resultado.NfoPath != queroPath {
		t.Errorf("nfo_path = %q, quero %q", resultado.NfoPath, queroPath)
	}
	if resultado.NfoWrittenAt == "" {
		t.Error("nfo_written_at vazio")
	}
	conteudo1, err := os.ReadFile(queroPath)
	if err != nil {
		t.Fatalf("ler nfo: %v", err)
	}
	if !strings.Contains(string(conteudo1), "<movie>") || !strings.Contains(string(conteudo1), "<title>Matrix</title>") {
		t.Errorf("conteudo do nfo nao bate: %s", conteudo1)
	}

	// entity.nfo_path/nfo_written_at persistidos.
	ent, err := escrita(t, d).ObterEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.NfoPath.Valid || ent.NfoPath.String != queroPath {
		t.Errorf("entity.nfo_path = %v, quero %q", ent.NfoPath, queroPath)
	}

	// 2a chamada: idempotente -- sobrescreve por completo, mesmo arquivo,
	// timestamp atualizado.
	rec2 := do(t, h, http.MethodPost, "/api/v1/entities/e1/nfo", nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("2a chamada status = %d, quero 200", rec2.Code)
	}
	var resultado2 nfoWriteResult
	decodeInto(t, rec2, &resultado2)
	if resultado2.NfoPath != queroPath {
		t.Errorf("2a chamada nfo_path = %q, quero %q (mesmo arquivo)", resultado2.NfoPath, queroPath)
	}
	conteudo2, err := os.ReadFile(queroPath)
	if err != nil {
		t.Fatalf("ler nfo (2a vez): %v", err)
	}
	// Sem duplicacao de conteudo -- o title aparece so uma vez.
	if strings.Count(string(conteudo2), "<title>Matrix</title>") != 1 {
		t.Errorf("conteudo duplicado apos 2a escrita: %s", conteudo2)
	}
}

// TestWriteEntityNfoPreservaCamposDoUsuario: uma regravacao (2a chamada de
// POST .../nfo, ou o job midias_faltantes reescrevendo por qualquer motivo)
// NUNCA pode apagar avaliacao/assistido/contagem/ultima-reproducao que o
// usuario (ou o proprio Kodi/Emby/Jellyfin) tenha gravado no arquivo entre a
// 1a e a 2a chamada -- so os campos que vem do resolver.Entidade (titulo,
// plot, etc.) devem ser atualizados.
func TestWriteEntityNfoPreservaCamposDoUsuario(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/nfo", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("1a chamada status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	queroPath := filepath.Join(libDir, "Matrix.1999.nfo")

	// Simula o Kodi (ou o usuario a mao) editando o arquivo que acabamos de
	// gravar: avalia, marca como assistido 2x e grava um ponto de retomada.
	editado := strings.Replace(
		string(mustReadFile(t, queroPath)),
		"</movie>",
		"  <userrating>9</userrating>\n  <playcount>2</playcount>\n  <watched>true</watched>\n  <lastplayed>2026-07-01 21:00:00</lastplayed>\n  <resume><position>120</position><total>8160</total></resume>\n</movie>",
		1,
	)
	if err := os.WriteFile(queroPath, []byte(editado), 0o644); err != nil {
		t.Fatalf("simular edicao do usuario: %v", err)
	}

	// 2a chamada: regrava (ex.: metadado foi atualizado no resolver).
	rec2 := do(t, h, http.MethodPost, "/api/v1/entities/e1/nfo", nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("2a chamada status = %d, quero 200", rec2.Code)
	}
	conteudo2 := string(mustReadFile(t, queroPath))
	for _, esperado := range []string{
		"<userrating>9</userrating>",
		"<playcount>2</playcount>",
		"<watched>true</watched>",
		"<lastplayed>2026-07-01 21:00:00</lastplayed>",
		"<position>120</position>",
		"<total>8160</total>",
	} {
		if !strings.Contains(conteudo2, esperado) {
			t.Errorf("2a gravacao apagou campo do usuario, nao encontrei %q:\n%s", esperado, conteudo2)
		}
	}
	// O title, por outro lado, segue vindo do resolver (nao e um campo local).
	if strings.Count(conteudo2, "<title>Matrix</title>") != 1 {
		t.Errorf("title nao regravado corretamente: %s", conteudo2)
	}
}

// mustReadFile le um arquivo ou falha o teste -- helper local so para deixar
// TestWriteEntityNfoPreservaCamposDoUsuario legivel.
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ler %s: %v", path, err)
	}
	return b
}

// TestWriteEntityNfoTVShow: serie grava tvshow.nfo (nao <video>.nfo).
func TestWriteEntityNfoTVShow(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "s1", "tv", "Show")
	libDir := libDirComVideo(t, d, "s1", "mf1", "Show.S01E01.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/s1/nfo", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var resultado nfoWriteResult
	decodeInto(t, rec, &resultado)
	queroPath := filepath.Join(libDir, "tvshow.nfo")
	if resultado.NfoPath != queroPath {
		t.Errorf("nfo_path = %q, quero %q", resultado.NfoPath, queroPath)
	}
}

// TestWriteEntityNfoSeasonNao404: uma TEMPORADA não tem NFO próprio neste
// briefing (evita colidir com o tvshow.nfo/NFO de episódio na raiz da
// série) — POST /nfo numa season devolve 404, tratado como "sem pasta de
// destino".
func TestWriteEntityNfoSeasonNao404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "s1", "tv", "Show")
	libDirComVideo(t, d, "s1", "mf1", "Show.S01E01.mkv")
	semearEntity(t, d, "temp1", "season", "Temporada 1")
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: "rel1", ParentID: "s1", ChildID: "temp1", Kind: "tv_season",
	}); err != nil {
		t.Fatalf("CriarEntityRelation: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/temp1/nfo", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestWriteEntityNfoSemMediaFile404 cobre o criterio 5.5: entity sem
// media_file present -> 404, nada e escrito.
func TestWriteEntityNfoSemMediaFile404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Sem Arquivo")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/nfo", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.NfoPath.Valid {
		t.Errorf("entity.nfo_path = %v, quero NULL (nada deveria ter sido escrito)", ent.NfoPath)
	}
}

// TestWriteEntityNfoEntidadeInexistente404 cobre o 404 de entity inexistente.
func TestWriteEntityNfoEntidadeInexistente404(t *testing.T) {
	h, _, _ := setupAssetsServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/nfo", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
}

// ── GET /artwork: ordenacao (briefing 0062) ──────────────────────────────

// TestArtworkOrdenaPrincipalLocalExterno cobre o criterio 5.4: a lista de um
// kind sai ordenada principal -> local -> externo.
func TestArtworkOrdenaPrincipalLocalExterno(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	// external cacheado (is_local=true, mas nao materializado na pasta real).
	extID := "ext1"
	if _, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID: extID, EntityID: "e1", Kind: "poster", Origin: "external", PathOrUri: "/tmp/nao-importa.png",
	}); err != nil {
		t.Fatalf("CriarAsset external: %v", err)
	}

	// user, materializado na pasta real e marcado principal via /prefer.
	userAsset := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	if rec := do(t, h, http.MethodPut, "/api/v1/entities/e1/assets/"+userAsset.ID+"/prefer", nil); rec.Code != http.StatusOK {
		t.Fatalf("prefer status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	decodeInto(t, rec, &body)
	var posters []artworkItem
	if err := json.Unmarshal(body["poster"], &posters); err != nil {
		t.Fatalf("decodificar posters: %v (corpo=%s)", err, rec.Body.String())
	}
	if len(posters) != 2 {
		t.Fatalf("posters = %d, quero 2", len(posters))
	}
	if !posters[0].IsPrincipal || posters[0].AssetID != userAsset.ID {
		t.Errorf("posters[0] = %+v, quero o principal (user) primeiro", posters[0])
	}
	if posters[1].AssetID != extID {
		t.Errorf("posters[1] = %+v, quero o external por ultimo", posters[1])
	}
}

// ── Falha de I/O: sem .tmp-* orfao (briefing 0062) ───────────────────────

// TestMaterializeFalhaIOSemOrfaoELocalPath cobre o criterio 5.5: falha de
// I/O ao gravar (diretorio sem permissao de escrita) nao deixa .tmp-* orfao
// nem local_path inconsistente com o disco.
func TestMaterializeFalhaIOSemOrfaoELocalPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignora permissao de diretorio (CI/containers como root)")
	}
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "sub", "Matrix.1999.mkv")
	semearFile(t, d, "mf1", "e1", videoPath, "present", 1.0, "")

	// A pasta de destino (dir/sub) nao existe ainda -- cria-a SEM permissao
	// de escrita, forcando EscreverArquivoAtomico a falhar no CreateTemp.
	destDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(destDir, 0o555); err != nil {
		t.Fatalf("MkdirAll (somente leitura): %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(destDir, 0o755) })

	a := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/assets/"+a.ID+"/materialize", nil)
	if rec.Code == http.StatusOK {
		t.Fatal("status = 200, queria falha (diretorio sem permissao de escrita)")
	}

	entradas, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entradas {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("arquivo temporario orfao encontrado: %s", e.Name())
		}
	}

	got, err := escrita(t, d).ObterAsset(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("ObterAsset: %v", err)
	}
	if got.LocalPath.Valid {
		t.Errorf("asset.local_path = %v apos falha, quero NULL (nada persistido)", got.LocalPath)
	}
}

// ── GET /entities/{id}/theme (briefing 0082) ─────────────────────────────

// TestGetEntityThemeMp3Encontrado cobre o caso feliz: theme.mp3 na raiz
// resolvida por DiretorioDestino e servido com o content-type correto.
func TestGetEntityThemeMp3Encontrado(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	conteudo := []byte("id3-fake-mp3-bytes")
	if err := os.WriteFile(filepath.Join(dir, "theme.mp3"), conteudo, 0o644); err != nil {
		t.Fatalf("escrever theme.mp3: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/theme", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type = %q, quero audio/mpeg", ct)
	}
	if rec.Body.String() != string(conteudo) {
		t.Errorf("corpo = %q, quero %q", rec.Body.String(), conteudo)
	}
}

// TestGetEntityThemeOggEFlac cobre as extensoes alternativas aceitas.
func TestGetEntityThemeOggEFlac(t *testing.T) {
	for _, tc := range []struct {
		arquivo   string
		contentCT string
	}{
		{"theme.ogg", "audio/ogg"},
		{"theme.flac", "audio/flac"},
	} {
		t.Run(tc.arquivo, func(t *testing.T) {
			h, d, _ := setupAssetsServer(t)
			semearEntity(t, d, "e1", "movie", "Matrix")
			dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")
			if err := os.WriteFile(filepath.Join(dir, tc.arquivo), []byte("x"), 0o644); err != nil {
				t.Fatalf("escrever %s: %v", tc.arquivo, err)
			}
			rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/theme", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != tc.contentCT {
				t.Errorf("Content-Type = %q, quero %q", ct, tc.contentCT)
			}
		})
	}
}

// TestGetEntityThemeSemArquivo404 cobre a degradacao graciosa: pasta
// resolvida existe (media_file present), mas sem nenhum theme.* -> 404, sem
// erro 500.
func TestGetEntityThemeSemArquivo404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/theme", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestGetEntityThemeSemMediaFile404 cobre o criterio "sem storage local
// associado": entity sem nenhum media_file present -> 404, mesma condicao
// de sem pasta de destino que materialize/nfo ja tratam.
func TestGetEntityThemeSemMediaFile404(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Sem Arquivo")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/theme", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestGetEntityThemeHead cobre o HEAD (o front confere existência sem baixar
// o binário inteiro antes de renderizar o player): 200 sem corpo quando há
// tema, 404 quando não há.
func TestGetEntityThemeHead(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	recAusente := do(t, h, http.MethodHead, "/api/v1/entities/e1/theme", nil)
	if recAusente.Code != http.StatusNotFound {
		t.Fatalf("HEAD sem tema: status = %d, quero 404", recAusente.Code)
	}

	if err := os.WriteFile(filepath.Join(dir, "theme.mp3"), []byte("xyz"), 0o644); err != nil {
		t.Fatalf("escrever theme.mp3: %v", err)
	}
	recPresente := do(t, h, http.MethodHead, "/api/v1/entities/e1/theme", nil)
	if recPresente.Code != http.StatusOK {
		t.Fatalf("HEAD com tema: status = %d, quero 200", recPresente.Code)
	}
	if recPresente.Body.Len() != 0 {
		t.Errorf("HEAD com tema: corpo = %d bytes, quero 0 (sem corpo)", recPresente.Body.Len())
	}
}

// TestGetEntityThemeEntidadeInexistente404 cobre o 404 de entity inexistente.
func TestGetEntityThemeEntidadeInexistente404(t *testing.T) {
	h, _, _ := setupAssetsServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/theme", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestGetEntityThemeSerieUsaRaizDaSerie cobre o item "raiz da série": tema
// na raiz da SERIE (nao numa subpasta de temporada) e encontrado quando o
// endpoint e chamado com o id da serie -- mesma resolucao de DiretorioDestino
// usada por materialize/nfo (briefing 0081).
func TestGetEntityThemeSerieUsaRaizDaSerie(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "serie1", "tv", "Serie")
	semearEntity(t, d, "temp1", "season", "Temporada 1")
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: "rel1", ParentID: "serie1", ChildID: "temp1", Kind: "tv_season",
	}); err != nil {
		t.Fatalf("CriarEntityRelation: %v", err)
	}

	raiz := t.TempDir()
	// Duas pastas de temporada IRMAS sob a raiz -- o prefixo comum cai na
	// raiz da serie (briefing 0081), sem precisar do fallback de subir nivel.
	semearFile(t, d, "mf1", "serie1", filepath.Join(raiz, "Season 01", "s01e01.mkv"), "present", 1.0, "")
	semearFile(t, d, "mf2", "serie1", filepath.Join(raiz, "Season 02", "s02e01.mkv"), "present", 1.0, "")

	if err := os.WriteFile(filepath.Join(raiz, "theme.mp3"), []byte("tema-serie"), 0o644); err != nil {
		t.Fatalf("escrever theme.mp3: %v", err)
	}

	recSerie := do(t, h, http.MethodGet, "/api/v1/entities/serie1/theme", nil)
	if recSerie.Code != http.StatusOK {
		t.Fatalf("serie: status = %d, quero 200 (corpo=%s)", recSerie.Code, recSerie.Body.String())
	}

	recTemp := do(t, h, http.MethodGet, "/api/v1/entities/temp1/theme", nil)
	if recTemp.Code != http.StatusOK {
		t.Fatalf("temporada: status = %d, quero 200 (corpo=%s)", recTemp.Code, recTemp.Body.String())
	}
}
