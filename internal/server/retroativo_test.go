package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// Tests of the RETROACTIVE EXTERNAL ENRICHMENT: the new query (four quadrants + throttle +
// limit/priority) and the EnriquecerRetroativo/tentarEnriquecerOuSinalizar orchestration (apply
// local/external, flag, only record the attempt, guard without enrich). It covers the criteria
// testable without real HTTP (there is no new endpoint — see the "Out" section of the
// design).

// carimbarTentativa writes last_external_match_attempt_at straight into the database, with a
// CONTROLLED value (fixture), never a real time.Now() -- that is what proves the throttle
// deterministically (the same spirit as definirStatus).
func carimbarTentativa(t *testing.T, d *db.DB, id string, quando time.Time) {
	t.Helper()
	ts := quando.UTC().Format(carimboRetroativoFormato)
	if _, err := d.Write.ExecContext(context.Background(),
		"UPDATE entity SET last_external_match_attempt_at = ? WHERE id = ?", ts, id); err != nil {
		t.Fatalf("carimbarTentativa(%s): %v", id, err)
	}
}

// setupRetroativoServer starts a *Server (not only the http.Handler -- the tests here call
// EnriquecerRetroativo directly, the same pattern as TestAutoMatchElegiveis in
// automatch_test.go) with the automatchProvider (source "tmdb") and assetsDir, so the external
// branch works end-to-end. resultados nil is valid (it only exercises the local branch).
func setupRetroativoServer(t *testing.T, resultados []provider.ResultadoExterno) (*Server, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "retroativo.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	prov := &automatchProvider{
		resultados: resultados,
		raw:        json.RawMessage(`{"title":"Blade Runner 2049"}`),
		campos:     map[string]any{"title": "Blade Runner 2049"},
	}
	s := New("v-teste", database, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))
	return s, database
}

// ── ListarElegiveisEnriquecimentoRetroativo (the query) ────────────────────

// TestListarElegiveisEnriquecimentoRetroativoQuadrantes covers the central criterion §5.1: the
// query is the EXACT complement (De Morgan) of ListarRevisao, proven on the four quadrants of
// status/confidence/identity.
func TestListarElegiveisEnriquecimentoRetroativoQuadrantes(t *testing.T) {
	_, d := setupEntitiesServer(t)
	ctx := context.Background()

	// Quadrant A: draft -- already in the Review queue, out of scope here.
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/draft.mkv", "present", 0, "")

	// Quadrant B: low confidence (0<conf<70), without an id -- already in the Review queue,
	// also out of scope.
	semearEntity(t, d, "e-baixa", "movie", "Baixa")
	semearFile(t, d, "f-baixa", "e-baixa", "/lib/baixa.mkv", "present", 60, "")

	// Quadrant C: confidence=70 (the scanner's confTituloAno), WITHOUT an external id -- the real
	// GAP this feature closes (never verified against any provider).
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/gap.mkv", "present", 70, "")

	// Quadrant D: confidence=70, WITH an external id -- it already has an identity, out of scope
	// (external_ids_json IS NULL anchors the query).
	semearEntity(t, d, "e-comid", "movie", "ComId")
	semearFile(t, d, "f-comid", "e-comid", "/lib/comid.mkv", "present", 70, "")
	if err := escrita(t, d).AtualizarExternalIds(ctx, sqlcgen.AtualizarExternalIdsParams{
		ExternalIdsJson: nsOf(`{"tmdb":"1"}`), ID: "e-comid",
	}); err != nil {
		t.Fatalf("AtualizarExternalIds: %v", err)
	}

	rows := listarElegiveisTeste(t, d, 100)
	got := idsDeRetroativo(rows)
	if got["e-draft"] {
		t.Error("draft não deveria ser elegível (já está na fila de Revisão)")
	}
	if got["e-baixa"] {
		t.Error("baixa confiança não deveria ser elegível (já está na fila de Revisão)")
	}
	if !got["e-gap"] {
		t.Error("confiança=70 sem id deveria ser elegível (o GAP do briefing 0056)")
	}
	if got["e-comid"] {
		t.Error("confiança=70 COM id externo não deveria ser elegível (já tem identidade)")
	}
}

// TestListarElegiveisEnriquecimentoRetroativoRestricoesDeEscopo covers the additional
// restrictions of §4: any TOP-LEVEL type (ADR-0030 -- NOT IN sub-nodes, no longer only IN
// (movie, tv)) and only entities with at least one 'present' media_file (direct or through the
// tree).
func TestListarElegiveisEnriquecimentoRetroativoRestricoesDeEscopo(t *testing.T) {
	_, d := setupEntitiesServer(t)

	// collection IS a top-level type (it is not in tiposSubNo) -- it is eligible like any other
	// top-level type with a media_file of its own (the old "only movie/tv" restriction was a
	// scope inherited from the original review-queue work, not a permanent business rule).
	semearEntity(t, d, "e-colecao", "collection", "Coleção")
	semearFile(t, d, "f-colecao", "e-colecao", "/lib/colecao.mkv", "present", 70, "")

	// season is a SUB-NODE (tiposSubNo) -- never eligible, even with a direct 'present'
	// media_file (an artificial scenario only to prove the exclusion by type, since a season
	// usually does not link a media_file directly).
	semearEntity(t, d, "e-temporada", "season", "Temporada 1")
	semearFile(t, d, "f-temporada", "e-temporada", "/lib/temporada.mkv", "present", 70, "")

	// Without ANY present media_file (all missing) -- out of scope.
	semearEntity(t, d, "e-missing", "movie", "Missing")
	semearFile(t, d, "f-missing", "e-missing", "/lib/missing.mkv", "missing", 70, "")

	// tv with confidence 70, present -- it MUST be eligible (the same scope as movie).
	semearEntity(t, d, "e-tv", "tv", "Serie")
	semearFile(t, d, "f-tv", "e-tv", "/lib/serie.mkv", "present", 70, "")

	got := idsDeRetroativo(listarElegiveisTeste(t, d, 100))
	if !got["e-colecao"] {
		t.Error("collection deveria ser elegível (tipo de topo, briefing 0116)")
	}
	if got["e-temporada"] {
		t.Error("season nunca deveria ser elegível (sub-nó, tiposSubNo)")
	}
	if got["e-missing"] {
		t.Error("entity sem nenhum media_file present não deveria ser elegível")
	}
	if !got["e-tv"] {
		t.Error("tv com confiança=70 deveria ser elegível (mesmo escopo de movie)")
	}
}

// TestListarElegiveisEnriquecimentoRetroativoThrottle covers criterion §5.5: an item attempted
// FEWER days ago than the configured throttle (30 days, in this test) does not come back; never
// attempted and attempted LONGER ago do come back. The query resolves the cutoff through
// datetime('now', ...) inside SQLite, so the fixture stamps are anchored on the REAL time
// (time.Now()), never on a fixed date.
func TestListarElegiveisEnriquecimentoRetroativoThrottle(t *testing.T) {
	_, d := setupEntitiesServer(t)
	// The test clock anchors on the REAL time (never a fixed date): the query now resolves the
	// cutoff through datetime('now', ...) inside SQLite itself, no longer a `desde`
	// pre-formatted in Go.
	agora := time.Now()

	semearEntity(t, d, "e-nunca", "movie", "Nunca Tentado")
	semearFile(t, d, "f-nunca", "e-nunca", "/lib/nunca.mkv", "present", 70, "")

	semearEntity(t, d, "e-recente", "movie", "Tentado Recente")
	semearFile(t, d, "f-recente", "e-recente", "/lib/recente.mkv", "present", 70, "")
	carimbarTentativa(t, d, "e-recente", agora.Add(-1*time.Hour))

	semearEntity(t, d, "e-antigo", "movie", "Tentado Antigo")
	semearFile(t, d, "f-antigo", "e-antigo", "/lib/antigo.mkv", "present", 70, "")
	carimbarTentativa(t, d, "e-antigo", agora.Add(-31*24*time.Hour))

	rows, err := escrita(t, d).ListarElegiveisEnriquecimentoRetroativo(context.Background(),
		sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
			Limiar:     sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
			DiasGlobal: 30,
			Limite:     100,
		})
	if err != nil {
		t.Fatalf("ListarElegiveisEnriquecimentoRetroativo: %v", err)
	}
	got := idsDeRetroativo(rows)
	if !got["e-nunca"] {
		t.Error("nunca tentado deveria ser elegível")
	}
	if got["e-recente"] {
		t.Error("tentado há menos de 30 dias NÃO deveria voltar (throttle)")
	}
	if !got["e-antigo"] {
		t.Error("tentado há mais de 30 dias deveria voltar")
	}
}

// TestListarElegiveisEnriquecimentoRetroativoLimiteEPrioridade covers criterion §5.6: it never
// processes more than the limit, and never-attempted items come BEFORE long-ago-attempted ones
// (oldest first among those).
func TestListarElegiveisEnriquecimentoRetroativoLimiteEPrioridade(t *testing.T) {
	_, d := setupEntitiesServer(t)
	// The test clock anchors on the REAL time -- see the note in the Throttle test above.
	agora := time.Now()

	semearEntity(t, d, "e-nulo-b", "movie", "Nulo B")
	semearFile(t, d, "f-nulo-b", "e-nulo-b", "/lib/b.mkv", "present", 70, "")
	semearEntity(t, d, "e-nulo-a", "movie", "Nulo A")
	semearFile(t, d, "f-nulo-a", "e-nulo-a", "/lib/a.mkv", "present", 70, "")

	semearEntity(t, d, "e-velho", "movie", "Velho")
	semearFile(t, d, "f-velho", "e-velho", "/lib/velho.mkv", "present", 70, "")
	carimbarTentativa(t, d, "e-velho", agora.Add(-60*24*time.Hour))

	semearEntity(t, d, "e-menos-velho", "movie", "MenosVelho")
	semearFile(t, d, "f-menos-velho", "e-menos-velho", "/lib/menosvelho.mkv", "present", 70, "")
	carimbarTentativa(t, d, "e-menos-velho", agora.Add(-40*24*time.Hour))

	rows, err := escrita(t, d).ListarElegiveisEnriquecimentoRetroativo(context.Background(),
		sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
			Limiar:     sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
			DiasGlobal: 30,
			Limite:     3,
		})
	if err != nil {
		t.Fatalf("ListarElegiveisEnriquecimentoRetroativo: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, quero 3 (LIMIT respeitado, backlog de 4)", len(rows))
	}
	quero := []string{"e-nulo-a", "e-nulo-b", "e-velho"}
	for i, id := range quero {
		if rows[i].ID != id {
			t.Errorf("rows[%d].ID = %q, quero %q (nunca-tentados primeiro, depois o mais antigo)", i, rows[i].ID, id)
		}
	}
}

// TestListarElegiveisEnriquecimentoRetroativoArtistaViaArvore covers criteria §5.1/§5.2: an
// artist WITHOUT a direct media_file (ADR-0027 -- only the track links) is eligible when it has
// a 'present' descendant track in the entity_relation tree; album/track NEVER show up, even
// under the same conditions (sub-nodes, ADR-0030).
func TestListarElegiveisEnriquecimentoRetroativoArtistaViaArvore(t *testing.T) {
	_, d := setupEntitiesServer(t)

	semearEntity(t, d, "art1", "artista", "Duran Duran")
	semearEntity(t, d, "alb1", "album", "Rio")
	semearEntity(t, d, "fx1", "faixa", "Hungry Like the Wolf")
	ligarFilhoOrdenado(t, d, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, d, "f1", "fx1", "/lib/musica/rio/01.flac", "present", 1, "")

	// Artist without ANY owned track in the tree: out of scope (nothing to verify against any
	// provider).
	semearEntity(t, d, "art-sem-posse", "artista", "Sem Posse")

	got := idsDeRetroativo(listarElegiveisTeste(t, d, 100))
	if !got["art1"] {
		t.Error("artista com faixa possuída na árvore deveria ser elegível")
	}
	if got["art-sem-posse"] {
		t.Error("artista sem nenhuma faixa possuída não deveria ser elegível")
	}
	if got["alb1"] {
		t.Error("album nunca deveria ser elegível (sub-nó, ADR-0030)")
	}
	if got["fx1"] {
		t.Error("faixa nunca deveria ser elegível (sub-nó, ADR-0030)")
	}
}

// TestListaSubNosSincronizadaComQuery pins the literal list of excluded types in
// ListarElegiveisEnriquecimentoRetroativo AND in ListarElegiveisReprocessamentoArtes
// (review.sql -- the same literal list, copied from the first one) against tiposSubNo
// (ADR-0030) -- sqlc.slice could not be used here, so the list is literal; this test is the
// safety net against the three of them diverging in the future.
func TestListaSubNosSincronizadaComQuery(t *testing.T) {
	esperado := map[string]bool{}
	for _, tipo := range tiposSubNo {
		esperado[tipo] = true
	}
	naQuery := map[string]bool{"season": true, "episode": true, "album": true, "faixa": true}
	if len(esperado) != len(naQuery) {
		t.Fatalf("tiposSubNo tem %d tipos, a lista literal da query tem %d -- atualize review.sql", len(esperado), len(naQuery))
	}
	for tipo := range esperado {
		if !naQuery[tipo] {
			t.Errorf("tiposSubNo tem %q, mas a lista literal de ListarElegiveisEnriquecimentoRetroativo (review.sql) não -- atualize a query", tipo)
		}
	}
}

// listarElegiveisTeste calls the query with an irrelevant DiasGlobal (the fixtures used by the
// callers of this function NEVER stamp last_external_match_attempt_at, so the IS NULL condition
// is enough -- the throttle value does not come into play) and a high limit -- used by the tests
// that do not exercise the throttle/limit themselves.
func listarElegiveisTeste(t *testing.T, d *db.DB, limite int64) []sqlcgen.ListarElegiveisEnriquecimentoRetroativoRow {
	t.Helper()
	rows, err := escrita(t, d).ListarElegiveisEnriquecimentoRetroativo(context.Background(),
		sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
			Limiar:     sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
			DiasGlobal: retryDaysPadrao,
			Limite:     limite,
		})
	if err != nil {
		t.Fatalf("ListarElegiveisEnriquecimentoRetroativo: %v", err)
	}
	return rows
}

func idsDeRetroativo(rows []sqlcgen.ListarElegiveisEnriquecimentoRetroativoRow) map[string]bool {
	m := make(map[string]bool, len(rows))
	for _, r := range rows {
		m[r.ID] = true
	}
	return m
}

// ── EnriquecerRetroativo / tentarEnriquecerOuSinalizar (the orchestration) ─

// TestEnriquecerRetroativoSemEnrich covers criterion §5.7: without s.enrich (no
// provider/assetsDir), it returns (0,0,nil) WITHOUT touching
// last_external_match_attempt_at -- the item keeps maximum priority.
func TestEnriquecerRetroativoSemEnrich(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	s := New("v-teste", d) // without WithSearchProvider/WithAssetsDir => s.enrich nil
	aplicados, sinalizados, err := s.EnriquecerRetroativo(context.Background())
	if err != nil {
		t.Fatalf("EnriquecerRetroativo: %v", err)
	}
	if aplicados != 0 || sinalizados != 0 {
		t.Errorf("(aplicados, sinalizados) = (%d, %d), quero (0, 0)", aplicados, sinalizados)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-gap")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.LastExternalMatchAttemptAt.Valid {
		t.Error("sem enrich, last_external_match_attempt_at NÃO deveria ser carimbado")
	}
}

// TestEnriquecerRetroativoAplicaLocal covers criterion §5.2 through the LOCAL branch: a local
// candidate with score >= LimiarAutoMatch applies on its own through fundirEntities -- the item
// leaves the eligible population (it gained a local identity, even without external_ids).
func TestEnriquecerRetroativoAplicaLocal(t *testing.T) {
	s, d := setupRetroativoServer(t, nil) // no external candidates
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	aplicados, sinalizados, err := s.EnriquecerRetroativo(context.Background())
	if err != nil {
		t.Fatalf("EnriquecerRetroativo: %v", err)
	}
	if aplicados != 1 {
		t.Errorf("aplicados = %d, quero 1", aplicados)
	}
	if sinalizados != 0 {
		t.Errorf("sinalizados = %d, quero 0", sinalizados)
	}

	// e-gap (loser) was merged into e-target; the file migrated.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-gap"); err == nil {
		t.Error("e-gap ainda existe após aplicar local, quero fundida/apagada")
	}
	arquivo, err := escrita(t, d).ObterMediaFile(context.Background(), "f-gap")
	if err != nil {
		t.Fatalf("ObterMediaFile(f-gap): %v", err)
	}
	if arquivo.EntityID != "e-target" {
		t.Errorf("f-gap.entity_id = %q, quero e-target", arquivo.EntityID)
	}
}

// TestEnriquecerRetroativoAplicaExterno covers criterion §5.2 through the EXTERNAL branch:
// without a local candidate, but with a strong external one, it applies through the "Corrigir"
// sequence (aplicarCandidatoExterno) -- and stamps the attempt.
func TestEnriquecerRetroativoAplicaExterno(t *testing.T) {
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "335984", Type: "movie", Title: "Blade Runner 2049", Year: strp("2017")},
	}
	s, d := setupRetroativoServer(t, resultados)
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/Blade Runner 2049 (2017)/Blade Runner 2049 (2017).mkv", "present", 70, "")

	aplicados, sinalizados, err := s.EnriquecerRetroativo(context.Background())
	if err != nil {
		t.Fatalf("EnriquecerRetroativo: %v", err)
	}
	if aplicados != 1 {
		t.Errorf("aplicados = %d, quero 1", aplicados)
	}
	if sinalizados != 0 {
		t.Errorf("sinalizados = %d, quero 0", sinalizados)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-gap")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.ExternalIDTmdb.Valid || ent.ExternalIDTmdb.String != "335984" {
		t.Errorf("external_id_tmdb = %v, quero 335984", ent.ExternalIDTmdb)
	}
	if !ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at deveria ter sido carimbado")
	}
}

// TestEnriquecerRetroativoSinaliza covers criterion §5.3: a candidate between
// limiarSinalizacaoRetroativa (50) and LimiarAutoMatch (92) is NEVER applied on its own -- it
// only flags it (AtualizarConfidencePorEntity), with the CEILING
// min(score, limiarBaixaConfianca-1) = 69, making the item show up in GET /review.
func TestEnriquecerRetroativoSinaliza(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)
	// "The Matrix Reloaded" (candidate) vs "The Matrix" (query): it matches partially -- a
	// plausible score (~90) but below 92. No release_date on the candidate (it avoids the year
	// bonus, keeping the score under control); the candidate title MUST contain the same words as
	// the query as a prefix (the local search is FTS5 with an implicit AND between terms).
	semearEntity(t, d, "e-candidato", "movie", "The Matrix Reloaded")
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	aplicados, sinalizados, err := s.EnriquecerRetroativo(context.Background())
	if err != nil {
		t.Fatalf("EnriquecerRetroativo: %v", err)
	}
	if aplicados != 0 {
		t.Errorf("aplicados = %d, quero 0 (candidato abaixo do LimiarAutoMatch nunca aplica sozinho)", aplicados)
	}
	if sinalizados != 1 {
		t.Fatalf("sinalizados = %d, quero 1", sinalizados)
	}

	// external_ids stays NULL -- nothing was applied.
	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-gap")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIdsJson.Valid {
		t.Error("external_ids_json não deveria ter sido gravado (só sinalizado, não aplicado)")
	}
	if !ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at deveria ter sido carimbado")
	}

	arquivo, err := escrita(t, d).ObterMediaFile(context.Background(), "f-gap")
	if err != nil {
		t.Fatalf("ObterMediaFile: %v", err)
	}
	if !arquivo.MatchConfidence.Valid || arquivo.MatchConfidence.Float64 != limiarBaixaConfianca-1 {
		t.Errorf("match_confidence = %v, quero exatamente %v (teto min(score, limiar-1))",
			arquivo.MatchConfidence, limiarBaixaConfianca-1)
	}

	// The item now shows up in the Review queue (the same mechanism as before).
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !idsDe(decodeCards(t, do(t, h, "GET", "/api/v1/review", nil)))["e-gap"] {
		t.Error("e-gap deveria aparecer na fila de Revisão após ser sinalizado")
	}
}

// TestEnriquecerRetroativoSemCandidatoSoCarimba covers criterion §5.4: without a plausible
// candidate in any source (below the flagging floor), the item does NOT change -- only the
// attempt is recorded (throttle).
func TestEnriquecerRetroativoSemCandidatoSoCarimba(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)
	semearEntityComAno(t, d, "e-outro", "movie", "Xyzzy Plugh Quux", "2005-01-01")
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	aplicados, sinalizados, err := s.EnriquecerRetroativo(context.Background())
	if err != nil {
		t.Fatalf("EnriquecerRetroativo: %v", err)
	}
	if aplicados != 0 || sinalizados != 0 {
		t.Errorf("(aplicados, sinalizados) = (%d, %d), quero (0, 0)", aplicados, sinalizados)
	}

	arquivo, err := escrita(t, d).ObterMediaFile(context.Background(), "f-gap")
	if err != nil {
		t.Fatalf("ObterMediaFile: %v", err)
	}
	if !arquivo.MatchConfidence.Valid || arquivo.MatchConfidence.Float64 != 70 {
		t.Errorf("match_confidence = %v, quero inalterado (70)", arquivo.MatchConfidence)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-gap")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at deveria ter sido carimbado (tentativa real ocorreu)")
	}
}

// TestEnriquecerRetroativoLoteMisto covers criterion §5.8 (best-effort): a batch with
// DIFFERENT results per item (applied, flagged, no candidate) processes all of them -- no item
// affects the result of the others, and ALL of them have the attempt stamped.
func TestEnriquecerRetroativoLoteMisto(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)

	// Applies on its own (high score).
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-aplica", "movie", "Aplica")
	semearFile(t, d, "f-aplica", "e-aplica", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	// Flags (medium score; the candidate contains the same words as the query as a prefix, to
	// match in the local FTS5 search, but with a title long enough for the score to stay below
	// LimiarAutoMatch).
	semearEntity(t, d, "e-candidato-fraco", "movie", "Blade Runner Black Lotus Animated Series")
	semearEntity(t, d, "e-sinaliza", "movie", "Sinaliza")
	semearFile(t, d, "f-sinaliza", "e-sinaliza", "/lib/Blade Runner (2017)/Blade Runner (2017).mkv", "present", 70, "")

	// No candidate at all.
	semearEntity(t, d, "e-nada", "movie", "Nada")
	semearFile(t, d, "f-nada", "e-nada", "/lib/Xyzzy Plugh Quux (2010)/Xyzzy Plugh Quux (2010).mkv", "present", 70, "")

	aplicados, sinalizados, err := s.EnriquecerRetroativo(context.Background())
	if err != nil {
		t.Fatalf("EnriquecerRetroativo: %v", err)
	}
	if aplicados != 1 {
		t.Errorf("aplicados = %d, quero 1", aplicados)
	}
	if sinalizados != 1 {
		t.Errorf("sinalizados = %d, quero 1", sinalizados)
	}

	// ALL THREE items (even the one that did not change) have the attempt stamped.
	for _, id := range []string{"e-sinaliza", "e-nada"} {
		ent, err := escrita(t, d).ObterEntity(context.Background(), id)
		if err != nil {
			t.Fatalf("ObterEntity(%s): %v", id, err)
		}
		if !ent.LastExternalMatchAttemptAt.Valid {
			t.Errorf("%s: last_external_match_attempt_at deveria ter sido carimbado", id)
		}
	}
}

// ── variantesBuscaRetroativo / music ────────────────────────────────────────

// TestVariantesBuscaRetroativo covers criterion §5.4 and the per-type dispatch: an artist
// returns its own cataloged title, without a year, WITHOUT requiring any media_file
// (ADR-0027 -- an artist never links directly); a movie keeps the old contract that a
// media_file is required (no file = nothing to search, so manual drafts and orphans are
// left alone); and a movie WITH a file gets TWO attempts, the CATALOG title first.
func TestVariantesBuscaRetroativo(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)
	ctx := context.Background()

	ent := semearEntity(t, d, "art1", "artista", "Duran Duran")
	entRow, err := escrita(t, d).ObterEntity(ctx, ent)
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	variantes, err := s.variantesBuscaRetroativo(ctx, entRow)
	if err != nil {
		t.Fatalf("variantesBuscaRetroativo(artista): %v", err)
	}
	if len(variantes) != 1 || variantes[0].Titulo != "Duran Duran" || variantes[0].Ano != 0 {
		t.Errorf("variantesBuscaRetroativo(artista) = %+v, quero um único {Duran Duran, 0}", variantes)
	}

	semearEntity(t, d, "mv-sem-arquivo", "movie", "Sem Arquivo")
	entMovie, err := escrita(t, d).ObterEntity(ctx, "mv-sem-arquivo")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	variantes, err = s.variantesBuscaRetroativo(ctx, entMovie)
	if err != nil {
		t.Fatalf("variantesBuscaRetroativo(movie): %v", err)
	}
	if len(variantes) != 0 {
		t.Errorf("variantesBuscaRetroativo(movie sem media_file) = %+v, quero vazio (nada a buscar)", variantes)
	}

	// Movie WITH a file: the catalog title goes first, with the year of release_date;
	// the release name (which yields no year at all) comes second.
	semearEntityComAno(t, d, "mv-aligator", "movie", "Alligator 2 A Mutação", "1991-01-01")
	semearFile(t, d, "f-aligator", "mv-aligator",
		"/lib/Filmes/Alligator 2 - A Mutação (1991)/Alligator2.91.BD1080p.MemoriadaTV.Maior.mkv", "present", 70, "")
	entAligator, err := escrita(t, d).ObterEntity(ctx, "mv-aligator")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	variantes, err = s.variantesBuscaRetroativo(ctx, entAligator)
	if err != nil {
		t.Fatalf("variantesBuscaRetroativo(aligator): %v", err)
	}
	// A PASTA vem primeiro (é ela que carrega a obra na convenção Kodi), depois a pasta sem o
	// subtítulo depois do hífen, e só então o nome do arquivo.
	if len(variantes) != 3 {
		t.Fatalf("variantes = %+v, quero 3 (pasta + pasta sem subtítulo + nome do arquivo)", variantes)
	}
	if variantes[0].Titulo != "Alligator 2 A Mutação" || variantes[0].Ano != 1991 {
		t.Errorf("variante[0] = %+v, quero {Alligator 2 A Mutação, 1991} (título da pasta, ano do release_date)", variantes[0])
	}
	if variantes[1].Titulo != "Alligator 2" || variantes[1].Ano != 1991 {
		t.Errorf("variante[1] = %+v, quero {Alligator 2, 1991} (pasta até o hífen)", variantes[1])
	}
	if !strings.Contains(variantes[2].Titulo, "Alligator2") {
		t.Errorf("variante[2] = %+v, quero o nome do arquivo (Alligator2 91 BD1080p ...)", variantes[2])
	}
}

// TestVariantesTituloAnoNaoRepeteBuscaIdêntica covers the dedupe: when the catalog title and
// the file name are the SAME title (the common case of a well-named release), the second
// attempt is dropped -- the provider is not queried twice for the same text.
func TestVariantesTituloAnoNaoRepeteBuscaIdentica(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)
	ctx := context.Background()

	semearEntityComAno(t, d, "mv-dune", "movie", "Dune", "2021-01-01")
	semearFile(t, d, "f-dune", "mv-dune", "/lib/Filmes/Dune (2021)/Dune (2021).mkv", "present", 70, "")
	ent, err := escrita(t, d).ObterEntity(ctx, "mv-dune")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	variantes, err := s.variantesTituloAno(ctx, ent)
	if err != nil {
		t.Fatalf("variantesTituloAno: %v", err)
	}
	if len(variantes) != 1 || variantes[0].Titulo != "Dune" {
		t.Errorf("variantes = %+v, quero uma única {Dune} (título do catálogo e do arquivo são o mesmo)", variantes)
	}
}

// automatchProviderMusica is the same fake as automatchProvider, registered under
// "musicbrainz" -- used by the EnriquecerRetroativo tests for artists
// (buscarLocal/buscarExterno/Enriquecer only need the core provider.Provider, not the
// MusicaProvider/MusicaAlbumProvider capabilities).
type automatchProviderMusica struct {
	resultados []provider.ResultadoExterno
	raw        json.RawMessage
	campos     map[string]any
}

func (p *automatchProviderMusica) Nome() string { return "musicbrainz" }

func (p *automatchProviderMusica) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return p.resultados, nil
}

func (p *automatchProviderMusica) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return p.raw, nil
}

func (p *automatchProviderMusica) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return p.campos, nil
}

// setupRetroativoServerMusica is the same mold as setupRetroativoServer, but it registers
// automatchProviderMusica (source "musicbrainz") instead of "tmdb".
func setupRetroativoServerMusica(t *testing.T, resultados []provider.ResultadoExterno) (*Server, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "retroativo-musica.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	prov := &automatchProviderMusica{
		resultados: resultados,
		raw:        json.RawMessage(`{"name":"Duran Duran"}`),
		campos:     map[string]any{"title": "Duran Duran"},
	}
	s := New("v-teste", database, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))
	return s, database
}

// TestEnriquecerRetroativoArtistaAplicaExterno covers criteria §5.4/§5.5 end-to-end: an artist
// eligible through the tree (owned track, without external_ids) with a strong external
// candidate is applied on its own, the same orchestration as always
// (aplicarCandidatoExterno) -- it proves that termoBuscaRetroativo/the new query fit into the
// real flow without demanding anything special from EnriquecerRetroativo.
func TestEnriquecerRetroativoArtistaAplicaExterno(t *testing.T) {
	resultados := []provider.ResultadoExterno{
		{Source: "musicbrainz", SourceID: "mbid-1", Type: "artista", Title: "Duran Duran"},
	}
	s, d := setupRetroativoServerMusica(t, resultados)

	semearEntity(t, d, "art1", "artista", "Duran Duran")
	semearEntity(t, d, "alb1", "album", "Rio")
	semearEntity(t, d, "fx1", "faixa", "Hungry Like the Wolf")
	ligarFilhoOrdenado(t, d, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, d, "f1", "fx1", "/lib/musica/rio/01.flac", "present", 1, "")

	aplicados, sinalizados, err := s.EnriquecerRetroativo(context.Background())
	if err != nil {
		t.Fatalf("EnriquecerRetroativo: %v", err)
	}
	if aplicados != 1 {
		t.Errorf("aplicados = %d, quero 1", aplicados)
	}
	if sinalizados != 0 {
		t.Errorf("sinalizados = %d, quero 0", sinalizados)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "art1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at deveria ter sido carimbado")
	}
}

// ── POST /entities/{id}/enrich-missing ─────────────────────────────────────

// decodeEnrichMissing decodes the enrich-missing response.
func decodeEnrichMissing(t *testing.T, body []byte) enrichMissingResultDTO {
	t.Helper()
	var out enrichMissingResultDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar enrichMissingResultDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestEnrichMissingAplicaLocal covers the criterion: a local candidate with score >=
// LimiarAutoMatch applies on its own (outcome=applied, method=local) through ONE individual
// POST -- the same assertion as the equivalent criterion of the batch flow
// (TestEnriquecerRetroativoAplicaLocal), only triggered through the HTTP endpoint for ONE
// item.
func TestEnrichMissingAplicaLocal(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-gap/enrich-missing", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeEnrichMissing(t, rec.Body.Bytes())
	if resultado.Outcome != "applied" {
		t.Fatalf("outcome = %q, quero applied (corpo=%s)", resultado.Outcome, rec.Body.String())
	}
	if resultado.Method == nil || *resultado.Method != "local" {
		t.Errorf("method = %v, quero \"local\"", resultado.Method)
	}
	if resultado.Score == nil || *resultado.Score < LimiarAutoMatch {
		t.Errorf("score = %v, quero >= %v", resultado.Score, LimiarAutoMatch)
	}

	// e-gap (loser) was merged into e-target -- the same application proof as the equivalent
	// batch test.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-gap"); err == nil {
		t.Error("e-gap ainda existe após aplicar local, quero fundida/apagada")
	}
}

// TestEnrichMissingAplicaExterno covers the criterion through the EXTERNAL branch: a strong
// external candidate applies through the "Corrigir" sequence (aplicarCandidatoExterno) and the
// body carries method=external.
func TestEnrichMissingAplicaExterno(t *testing.T) {
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "335984", Type: "movie", Title: "Blade Runner 2049", Year: strp("2017")},
	}
	s, d := setupRetroativoServer(t, resultados)
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/Blade Runner 2049 (2017)/Blade Runner 2049 (2017).mkv", "present", 70, "")

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-gap/enrich-missing", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeEnrichMissing(t, rec.Body.Bytes())
	if resultado.Outcome != "applied" || resultado.Method == nil || *resultado.Method != "external" {
		t.Fatalf("resultado = %+v, quero outcome=applied method=external", resultado)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-gap")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at deveria ter sido carimbado")
	}
}

// TestEnrichMissingSerieConstroiArvore covers the series tree: the retroactive/individual
// enrich-missing (tentarEnriquecerOuSinalizar -> aplicarCandidatoExterno -> s.enrich.Enriquecer)
// on a type='tv' entity builds season/episode, exactly as POST /import already did --
// GET /entities/{id}/members stops coming out empty. It uses the canned fake.Provider fixture
// (the same tree as internal/enrich/enrich_arvore_test.go: 2 seasons of 3 episodes).
func TestEnrichMissingSerieConstroiArvore(t *testing.T) {
	img := servidorImagemArvoreSerie(t)
	h, d := setupSearchServer(t, WithSearchProvider(fake.New(img.URL)), WithAssetsDir(t.TempDir()))
	semearEntity(t, d, "e-gap", "tv", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/Springfield Tales (1989)/Springfield Tales (1989).mkv", "present", 70, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-gap/enrich-missing", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeEnrichMissing(t, rec.Body.Bytes())
	if resultado.Outcome != "applied" || resultado.Method == nil || *resultado.Method != "external" {
		t.Fatalf("resultado = %+v, quero outcome=applied method=external", resultado)
	}

	temporadas := membrosDe(t, h, "e-gap")
	if len(temporadas) != 2 {
		t.Fatalf("members(e-gap) = %d, quero 2 temporadas (enrich-missing sincronizou a arvore, 0067)", len(temporadas))
	}
	totalEpisodios := 0
	for _, temp := range temporadas {
		totalEpisodios += len(membrosDe(t, h, temp.ID))
	}
	if totalEpisodios != 6 {
		t.Errorf("episodios = %d, quero 6", totalEpisodios)
	}
}

// TestEnrichMissingSinaliza covers the criterion: a medium candidate (score between
// limiarSinalizacaoRetroativa and LimiarAutoMatch) flags (outcome=flagged, with a score but
// WITHOUT method) -- and the item starts showing up in GET /review.
func TestEnrichMissingSinaliza(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)
	semearEntity(t, d, "e-candidato", "movie", "The Matrix Reloaded")
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-gap/enrich-missing", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeEnrichMissing(t, rec.Body.Bytes())
	if resultado.Outcome != "flagged" {
		t.Fatalf("outcome = %q, quero flagged (corpo=%s)", resultado.Outcome, rec.Body.String())
	}
	if resultado.Method != nil {
		t.Errorf("method = %v, quero nil (só presente em outcome=applied)", *resultado.Method)
	}
	if resultado.Score == nil || *resultado.Score < limiarSinalizacaoRetroativa || *resultado.Score >= LimiarAutoMatch {
		t.Errorf("score = %v, quero entre %v e %v", resultado.Score, limiarSinalizacaoRetroativa, LimiarAutoMatch)
	}

	if !idsDe(decodeCards(t, do(t, h, "GET", "/api/v1/review", nil)))["e-gap"] {
		t.Error("e-gap deveria aparecer na fila de Revisão após ser sinalizado")
	}
}

// TestEnrichMissingSemCandidato covers the criterion: without a plausible candidate in any
// source, outcome=none (no method/score), 200, and the attempt is STILL stamped.
func TestEnrichMissingSemCandidato(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)
	semearEntityComAno(t, d, "e-outro", "movie", "Xyzzy Plugh Quux", "2005-01-01")
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-gap/enrich-missing", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeEnrichMissing(t, rec.Body.Bytes())
	if resultado.Outcome != "none" || resultado.Method != nil || resultado.Score != nil {
		t.Errorf("resultado = %+v, quero {outcome:none, method:nil, score:nil}", resultado)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-gap")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at deveria ter sido carimbado mesmo sem candidato")
	}
}

// TestEnrichMissingIgnoraThrottle covers the criterion: the individual endpoint WORKS even when
// last_external_match_attempt_at was stamped FEWER days ago than the configured throttle
// interval (30 days, in this test) -- unlike the batch job (EnriquecerRetroativo), which would
// never get to call tentarEnriquecerOuSinalizar for this item (filtered out by the query). The
// endpoint does NO throttle check at all before calling the orchestration.
func TestEnrichMissingIgnoraThrottle(t *testing.T) {
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "335984", Type: "movie", Title: "Blade Runner 2049", Year: strp("2017")},
	}
	s, d := setupRetroativoServer(t, resultados)
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/Blade Runner 2049 (2017)/Blade Runner 2049 (2017).mkv", "present", 70, "")
	// Stamps a VERY recent attempt -- a batch throttle would block it.
	carimbarTentativa(t, d, "e-gap", time.Now().Add(-1*time.Hour))

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-gap/enrich-missing", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeEnrichMissing(t, rec.Body.Bytes())
	if resultado.Outcome != "applied" {
		t.Errorf("outcome = %q, quero applied (throttle recente deveria ter sido IGNORADO)", resultado.Outcome)
	}
}

// TestEnrichMissingEntidadeInexistente: 404 for an entity that does not exist.
func TestEnrichMissingEntidadeInexistente(t *testing.T) {
	s, _ := setupRetroativoServer(t, nil)
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/enrich-missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}
