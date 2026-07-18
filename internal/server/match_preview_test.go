package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// Testes da PRÉVIA de candidato + aplicação supervisionada (briefing 0060):
// GET /entities/{id}/match-preview (dry-run) e POST
// /entities/{id}/confirm-candidate (aplica mesmo abaixo de LimiarAutoMatch).
// As pontuações usadas abaixo (título consulta vs. título candidato, SEM ano
// em nenhum lado para não sofrer o ajuste de +5 de matching.Pontuar) foram
// calculadas com internal/matching.Pontuar diretamente: "The Matrix" vs. "The
// Matrix Reloaded" ~90.5 (< 92, mas > 0) e "Dune" vs. "Dune Part Two" ~86.2 (<
// 92) -- ambos abaixo do LimiarAutoMatch (92) para exercitar exatamente o
// caso que este briefing destrava (prévia/confirmação abaixo do limiar).

// decodeMatchPreview decodifica a resposta de GET .../match-preview.
func decodeMatchPreview(t *testing.T, rec *httptest.ResponseRecorder) matchPreviewDTO {
	t.Helper()
	var out matchPreviewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar matchPreviewDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return out
}

// semearAssetPoster grava um asset kind=poster para a entity, para validar
// que o poster_url da prévia vem de graça (sem chamada de rede extra).
func semearAssetPoster(t *testing.T, d *db.DB, id, entityID string) {
	t.Helper()
	if _, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID: id, EntityID: entityID, Kind: "poster", Origin: "user", PathOrUri: "/tmp/poster.jpg",
	}); err != nil {
		t.Fatalf("CriarAsset(%s): %v", id, err)
	}
}

// ── GET /entities/{id}/match-preview ─────────────────────────────────────────

// TestMatchPreviewSemCandidato cobre found:false quando não há candidato em
// nenhuma fonte (catálogo vazio, sem provider configurado) -- 200, nunca erro.
func TestMatchPreviewSemCandidato(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/Completely Unrelated Thing/x.mkv", "present", 0, "")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e-draft/match-preview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeMatchPreview(t, rec)
	if got.Found || got.Method != "none" || got.Score != 0 || got.MeetsAutoThreshold || got.Target != nil {
		t.Errorf("preview = %+v, quero found:false method:none score:0 target:nil", got)
	}
}

// TestMatchPreviewLocalAbaixoDoLimiar cobre o critério central do briefing: a
// prévia devolve o candidato local mesmo com score < LimiarAutoMatch (92),
// marcando meets_auto_threshold:false -- e o poster_url vem de graça (mesmo
// asset já lido pela busca local, nenhuma chamada de rede extra).
func TestMatchPreviewLocalAbaixoDoLimiar(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e-target", "movie", "The Matrix Reloaded")
	semearAssetPoster(t, d, "asset-poster", "e-target")
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/The Matrix/The Matrix.mkv", "present", 0, "")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e-draft/match-preview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeMatchPreview(t, rec)
	if !got.Found || got.Method != "local" {
		t.Fatalf("preview = %+v, quero found:true method:local", got)
	}
	if got.Score <= 0 || got.Score >= LimiarAutoMatch {
		t.Errorf("score = %v, quero >0 e < %v (abaixo do limiar)", got.Score, LimiarAutoMatch)
	}
	if got.MeetsAutoThreshold {
		t.Errorf("meets_auto_threshold = true, quero false (score abaixo do limiar)")
	}
	if got.Target == nil {
		t.Fatal("target = nil, quero o candidato local")
	}
	if got.Target.LocalID == nil || *got.Target.LocalID != "e-target" {
		t.Errorf("target.local_id = %v, quero e-target", got.Target.LocalID)
	}
	if got.Target.Source != nil || got.Target.SourceID != nil {
		t.Errorf("target.source/source_id = %v/%v, quero nil (candidato local)", got.Target.Source, got.Target.SourceID)
	}
	wantPoster := posterPath("e-target")
	if got.Target.PosterURL == nil || *got.Target.PosterURL != wantPoster {
		t.Errorf("target.poster_url = %v, quero %q (de graça, sem chamada extra)", got.Target.PosterURL, wantPoster)
	}

	// Dry-run: nada foi aplicado -- e-draft continua intacta, e-target não
	// ganhou o arquivo.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-draft"); err != nil {
		t.Errorf("e-draft deveria continuar intacta (dry-run): %v", err)
	}
}

// TestMatchPreviewExterno cobre o ramo externo da prévia (sem candidato
// local): score abaixo do limiar, poster_url cru do provider, source/
// source_id preenchidos e local_id nil.
func TestMatchPreviewExterno(t *testing.T) {
	posterURL := "https://image.tmdb.org/dune-part-two.jpg"
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "438631", Type: "movie", Title: "Dune Part Two", PosterURL: &posterURL},
	}
	h, d := setupAutoMatchServer(t, resultados)
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/Dune/Dune.mkv", "present", 0, "")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e-draft/match-preview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeMatchPreview(t, rec)
	if !got.Found || got.Method != "external" {
		t.Fatalf("preview = %+v, quero found:true method:external", got)
	}
	if got.Score <= 0 || got.Score >= LimiarAutoMatch {
		t.Errorf("score = %v, quero >0 e < %v", got.Score, LimiarAutoMatch)
	}
	if got.MeetsAutoThreshold {
		t.Error("meets_auto_threshold = true, quero false")
	}
	if got.Target == nil {
		t.Fatal("target = nil, quero o candidato externo")
	}
	if got.Target.LocalID != nil {
		t.Errorf("target.local_id = %v, quero nil (candidato externo)", got.Target.LocalID)
	}
	if got.Target.Source == nil || *got.Target.Source != "tmdb" {
		t.Errorf("target.source = %v, quero tmdb", got.Target.Source)
	}
	if got.Target.SourceID == nil || *got.Target.SourceID != "438631" {
		t.Errorf("target.source_id = %v, quero 438631", got.Target.SourceID)
	}
	if got.Target.PosterURL == nil || *got.Target.PosterURL != posterURL {
		t.Errorf("target.poster_url = %v, quero %q (URL crua do provider)", got.Target.PosterURL, posterURL)
	}

	// Dry-run: nenhuma identidade foi gravada.
	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-draft")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTmdb.Valid {
		t.Errorf("external_id_tmdb = %v, quero NULL (dry-run, nada aplicado)", ent.ExternalIDTmdb)
	}
}

// TestMatchPreviewInexistente404 cobre o 404 quando a entity não existe.
func TestMatchPreviewInexistente404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/match-preview", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// ── POST /entities/{id}/confirm-candidate ────────────────────────────────────

// TestConfirmCandidateLocalAbaixoDoLimiar cobre o critério central: aplica o
// candidato LOCAL (funde) mesmo com score abaixo de LimiarAutoMatch -- sem
// checagem de limiar neste endpoint (decisão humana informada).
func TestConfirmCandidateLocalAbaixoDoLimiar(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e-target", "movie", "The Matrix Reloaded")
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/The Matrix/The Matrix.mkv", "present", 0, "")

	// Confirma que a prévia, de fato, mostraria esse candidato ABAIXO do
	// limiar antes de aplicar (ancora o cenário do teste no comportamento real).
	previewRec := do(t, h, http.MethodGet, "/api/v1/entities/e-draft/match-preview", nil)
	preview := decodeMatchPreview(t, previewRec)
	if !preview.Found || preview.MeetsAutoThreshold {
		t.Fatalf("preview = %+v, quero found:true meets_auto_threshold:false (pré-condição do teste)", preview)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/confirm-candidate",
		map[string]string{"method": "local", "local_id": "e-target"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeEntidade(t, rec)
	if got.ID != "e-target" {
		t.Errorf("entity resolvida id = %q, quero e-target (sobrevivente da fusão)", got.ID)
	}

	// e-draft (perdedora) foi apagada; o arquivo migrou para e-target.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-draft"); err == nil {
		t.Error("e-draft ainda existe após confirm-candidate local, quero fundida/apagada")
	}
	arquivo, err := escrita(t, d).ObterMediaFile(context.Background(), "f-draft")
	if err != nil {
		t.Fatalf("ObterMediaFile(f-draft): %v", err)
	}
	if arquivo.EntityID != "e-target" {
		t.Errorf("f-draft.entity_id = %q, quero e-target", arquivo.EntityID)
	}
}

// TestConfirmCandidateExternoAbaixoDoLimiar cobre o ramo externo: aplica
// (grava identidade + re-enriquece + sobe confiança) mesmo com score abaixo
// do limiar -- mesma sequência de correctEntity, via aplicarIdentidadeExterna.
func TestConfirmCandidateExternoAbaixoDoLimiar(t *testing.T) {
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "438631", Type: "movie", Title: "Dune Part Two"},
	}
	h, d := setupAutoMatchServer(t, resultados)
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/Dune/Dune.mkv", "present", 0, "")

	previewRec := do(t, h, http.MethodGet, "/api/v1/entities/e-draft/match-preview", nil)
	preview := decodeMatchPreview(t, previewRec)
	if !preview.Found || preview.MeetsAutoThreshold || preview.Method != "external" {
		t.Fatalf("preview = %+v, quero found:true method:external meets_auto_threshold:false", preview)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/confirm-candidate",
		map[string]string{"method": "external", "source": "tmdb", "source_id": "438631"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeEntidade(t, rec)
	if got.ID != "e-draft" {
		t.Errorf("entity resolvida id = %q, quero e-draft (identidade aplicada na mesma entity)", got.ID)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-draft")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.ExternalIDTmdb.Valid || ent.ExternalIDTmdb.String != "438631" {
		t.Errorf("external_id_tmdb = %v, quero 438631", ent.ExternalIDTmdb)
	}
	if idsDe(decodeCards(t, do(t, h, http.MethodGet, "/api/v1/review", nil)))["e-draft"] {
		t.Error("e-draft ainda na fila de revisão após confirm-candidate externo")
	}
}

// TestConfirmCandidateExternoSerieConstroiArvore cobre o briefing 0067: o branch
// EXTERNO de confirm-candidate (aplicarIdentidadeExterna -> s.enrich.Enriquecer) sobre
// uma entity type='tv' constroi temporada/episodio, exatamente como POST /import ja
// construia -- GET /entities/{id}/members deixa de vir vazio. Usa o fixture canned de
// fake.Provider (mesma arvore de internal/enrich/enrich_arvore_test.go: 2 temporadas
// de 3 episodios).
func TestConfirmCandidateExternoSerieConstroiArvore(t *testing.T) {
	img := servidorImagemArvoreSerie(t)
	h, d := setupSearchServer(t, WithSearchProvider(fake.New(img.URL)), WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "e-draft", "tv", "Draft")
	definirStatus(t, d, "e-draft", "draft")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/confirm-candidate",
		map[string]string{"method": "external", "source": "tmdb", "source_id": fake.SerieFixtureID})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}

	temporadas := membrosDe(t, h, "e-draft")
	if len(temporadas) != 2 {
		t.Fatalf("members(e-draft) = %d, quero 2 temporadas (confirm-candidate externo sincronizou a arvore, 0067)", len(temporadas))
	}
	totalEpisodios := 0
	for _, temp := range temporadas {
		totalEpisodios += len(membrosDe(t, h, temp.ID))
	}
	if totalEpisodios != 6 {
		t.Errorf("episodios = %d, quero 6", totalEpisodios)
	}
}

// TestConfirmCandidate422CorpoIncompleto cobre o 422 quando o corpo não bate
// o `method` (local sem local_id, external sem source/source_id, method
// inválido).
func TestConfirmCandidate422CorpoIncompleto(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")

	casos := []struct {
		nome string
		body map[string]string
	}{
		{"local sem local_id", map[string]string{"method": "local"}},
		{"external sem source", map[string]string{"method": "external", "source_id": "1"}},
		{"external sem source_id", map[string]string{"method": "external", "source": "tmdb"}},
		{"method inválido", map[string]string{"method": "outro"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/confirm-candidate", c.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
			}
			checkErrorShape(t, rec, "invalid_body")
		})
	}
}

// TestConfirmCandidateExterno409ColisaoReal cobre o critério mais importante
// do branch externo: uma colisão de identidade com investimento do dono
// responde 409 DE VERDADE na requisição (não degrada silenciosamente como o
// auto-match em lote faz via capturaResposta).
func TestConfirmCandidateExterno409ColisaoReal(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	ctx := context.Background()
	w := sqlcgen.New(d.Write)

	// Entity já importada com o id externo, COM investimento do dono (override).
	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{
		"source": "tmdb", "source_id": "999", "type": "movie",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	importada := decodeEntidade(t, rec)
	if _, err := w.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID:     importada.ID,
		OverrideJson: nsOf(`{"title":"Titulo Curado"}`),
	}); err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}

	// Item da fila, diferente da entity importada, tentando confirmar o MESMO
	// id externo que já pertence à entity com investimento do dono.
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "e-draft", Type: "movie", Title: "Draft",
	}); err != nil {
		t.Fatalf("CriarEntity e-draft: %v", err)
	}
	definirStatus(t, d, "e-draft", "draft")

	rec2 := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/confirm-candidate",
		map[string]string{"method": "external", "source": "tmdb", "source_id": "999"})
	if rec2.Code != http.StatusConflict {
		t.Fatalf("status = %d, quero 409 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	checkErrorShape(t, rec2, "conflict_duplicate_identity")

	// Nada foi gravado: e-draft continua sem identidade, a importada continua
	// existindo intacta.
	entDraft, err := w.ObterEntity(ctx, "e-draft")
	if err != nil {
		t.Fatalf("ObterEntity e-draft: %v", err)
	}
	if entDraft.ExternalIDTmdb.Valid {
		t.Errorf("e-draft.external_id_tmdb = %v, quero NULL (nada deveria ter sido gravado)", entDraft.ExternalIDTmdb)
	}
	if _, err := w.ObterEntity(ctx, importada.ID); err != nil {
		t.Errorf("entity importada (%q) não deveria ter sido apagada, err=%v", importada.ID, err)
	}
}

// TestConfirmCandidateInexistente404 cobre o 404 quando a entity não existe.
func TestConfirmCandidateInexistente404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/confirm-candidate",
		map[string]string{"method": "local", "local_id": "e-target"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}
