package server

// Testes de POST /entities/{id}/force-reconcile (briefing 0122): dispara a
// MESMA reconciliação de filhos órfãos do job em lote (ReconciliarFilhosOrfaos,
// 0120, testado em reconciliar_orfaos_test.go), mas para UMA entity e
// IGNORANDO o throttle de last_external_match_attempt_at. Reusa os fakes/
// helpers de reconciliar_orfaos_test.go (mesmo pacote): setupReconciliarOrfaosServer,
// semearFaixaDraftOrfa, semearSerieDraftComArquivo, fakeMusicaProviderOrfaos,
// reconciliarProberFake, mediaFileEntityID.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// decodeForceReconcile decodifica a resposta do endpoint.
func decodeForceReconcile(t *testing.T, body []byte) forcarReconciliacaoResultDTO {
	t.Helper()
	var out forcarReconciliacaoResultDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar forcarReconciliacaoResultDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestForceReconcileFaixaIgnoraThrottle cobre o critério central do briefing
// 0122: uma faixa draft órfã com last_external_match_attempt_at BEM recente
// (dentro do throttle de ReconciliarFilhosOrfaos) ainda assim é tentada pelo
// endpoint individual -- e religa com sucesso, provando que o throttle é
// ignorado (diferente do job em lote, que nem chamaria o provider para este
// item -- ver TestReconciliarFilhosOrfaosRespeitaThrottle).
func TestForceReconcileFaixaIgnoraThrottle(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/musica/aha/01 - Track One.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		path: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	})
	semearFaixaDraftOrfa(t, d, "fx-draft", "Track One", path)
	// Carimba uma tentativa BEM recente -- um throttle de lote bloquearia.
	carimbarTentativa(t, d, "fx-draft", time.Now().Add(-1*time.Hour))

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/fx-draft/force-reconcile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeForceReconcile(t, rec.Body.Bytes())
	if !resultado.Linked {
		t.Errorf("linked = %v, quero true (throttle recente deveria ter sido IGNORADO)", resultado.Linked)
	}
	if musica.chamadasBuscar == 0 {
		t.Error("provider de música não foi chamado -- throttle não foi ignorado")
	}

	entityID := mediaFileEntityID(t, d, path)
	if entityID == "fx-draft" {
		t.Error("media_file ainda aponta pro draft, deveria estar repontado pra faixa resolvida")
	}
}

// TestForceReconcileSerieIgnoraThrottle cobre o mesmo critério para série: uma
// série draft com arquivo cuja árvore só resolve via provider (fake.New),
// carimbada recentemente, ainda assim é retentada e religada pelo endpoint
// individual.
func TestForceReconcileSerieIgnoraThrottle(t *testing.T) {
	img := servidorImagemArvoreSerie(t)
	h, d := setupSearchServer(t, WithSearchProvider(fake.New(img.URL)), WithAssetsDir(t.TempDir()))
	path := "/lib/series/springfield/Springfield Tales S01E01.mkv"
	semearSerieDraftComArquivo(t, d, "serie-draft", "Springfield Tales", path)
	carimbarTentativa(t, d, "serie-draft", time.Now().Add(-1*time.Hour))

	rec := do(t, h, http.MethodPost, "/api/v1/entities/serie-draft/force-reconcile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeForceReconcile(t, rec.Body.Bytes())
	if !resultado.Linked {
		t.Errorf("linked = %v, quero true (throttle recente deveria ter sido IGNORADO)", resultado.Linked)
	}

	entityID := mediaFileEntityID(t, d, path)
	if entityID == "serie-draft" {
		t.Error("media_file ainda aponta pro draft, deveria estar repontado pra série resolvida")
	}
}

// TestForceReconcileTipoNaoAplicavelNaoCarimba cobre o critério "tipo não
// aplicável" do §4: uma entity type=movie responde 200/linked=false SEM
// chamar nenhuma reconciliação e SEM carimbar a tentativa (diferente de
// faixa/série, que sempre carimbam).
func TestForceReconcileTipoNaoAplicavelNaoCarimba(t *testing.T) {
	s, d := setupReconciliarOrfaosServer(t, &fakeMusicaProviderOrfaos{}, nil)
	semearEntity(t, d, "e-movie", "movie", "Um Filme Qualquer")

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-movie/force-reconcile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeForceReconcile(t, rec.Body.Bytes())
	if resultado.Linked {
		t.Errorf("linked = %v, quero false (tipo não aplicável)", resultado.Linked)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-movie")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at NÃO deveria ter sido carimbado (tipo não aplicável, sem tentativa real)")
	}
}

// TestForceReconcileEntidadeInexistente404 cobre o critério §4: entity
// inexistente responde 404, mesmo padrão de enrich-missing/auto-match.
func TestForceReconcileEntidadeInexistente404(t *testing.T) {
	s, _ := setupReconciliarOrfaosServer(t, &fakeMusicaProviderOrfaos{}, nil)
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/force-reconcile", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestForceReconcileSemScannerDegradaSemErro cobre a guarda s.scanner==nil:
// sem scanner, responde 200/linked=false direto, sem tentar nada -- mesma
// guarda de ReconciliarFilhosOrfaos.
func TestForceReconcileSemScannerDegradaSemErro(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "fx-sem-scanner", "faixa", "Sem Scanner")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/fx-sem-scanner/force-reconcile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeForceReconcile(t, rec.Body.Bytes())
	if resultado.Linked {
		t.Errorf("linked = %v, quero false (sem scanner)", resultado.Linked)
	}
}

// TestForceReconcileErroDeInfraPropaga500 cobre o critério §4: um erro REAL
// de banco (pool de escrita fechado) propaga como 500 -- nunca degrada
// silenciosamente. Usa um cenário SEM candidato algum (faixa cujo artista/
// álbum não batem em nenhuma fixture do provider, mesmo caminho de
// TestReconciliarFilhosOrfaosSerieSemMatchAindaCarimba): a reconciliação em
// si só usa o pool de LEITURA e o Prober (nenhuma escrita é tentada, já que
// nenhum candidato é encontrado para religar) -- e o carimbo, que é a
// ÚNICA escrita alcançada neste caminho, é o que falha com o pool de
// escrita fechado, provando o "OU" do critério (tentarErr==nil mas
// carimbarErr!=nil ainda vira 500). Forçar o erro DENTRO da própria
// reconciliação exigiria pré-semear artista/álbum/faixa já catalogados
// batendo exatamente as fixtures do provider fake (para o match textual não
// precisar escrever nada além do carimbo final) -- não vale o custo extra
// dado que o "OU" já fica provado por este lado.
func TestForceReconcileErroDeInfraPropaga500(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/musica/desconhecida/01 - Faixa Desconhecida.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		path: {AlbumArtist: "Banda Sem Fixture", Album: "Album Sem Fixture", Title: "Faixa Desconhecida", TrackNumber: 1},
	})
	semearFaixaDraftOrfa(t, d, "fx-draft", "Faixa Desconhecida", path)

	if err := d.Write.Close(); err != nil {
		t.Fatalf("fechar Write: %v", err)
	}

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/fx-draft/force-reconcile", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, quero 500 (pool de escrita fechado, corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "internal_error")
}
