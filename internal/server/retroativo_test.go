package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// Testes do ENRIQUECIMENTO EXTERNO RETROATIVO (briefing 0056): a query nova
// (quatro quadrantes + throttle + limite/prioridade) e a orquestração
// EnriquecerRetroativo/tentarEnriquecerOuSinalizar (aplicar local/externo,
// sinalizar, só registrar tentativa, guarda sem enrich). Cobre os critérios
// §5 testáveis sem HTTP real (não há endpoint novo — ver §3 "Fora").

// carimbarTentativa grava last_external_match_attempt_at direto no banco,
// com um valor CONTROLADO (fixture), nunca time.Now() real -- é o que prova
// o throttle de forma determinística (mesmo espírito de definirStatus).
func carimbarTentativa(t *testing.T, d *db.DB, id string, quando time.Time) {
	t.Helper()
	ts := quando.UTC().Format(carimboRetroativoFormato)
	if _, err := d.Write.ExecContext(context.Background(),
		"UPDATE entity SET last_external_match_attempt_at = ? WHERE id = ?", ts, id); err != nil {
		t.Fatalf("carimbarTentativa(%s): %v", id, err)
	}
}

// setupRetroativoServer sobe um *Server (não só o http.Handler -- os testes
// aqui chamam EnriquecerRetroativo diretamente, mesmo padrão de
// TestAutoMatchElegiveis em automatch_test.go) com o automatchProvider
// (fonte "tmdb") e assetsDir, para o ramo externo funcionar de ponta a
// ponta. resultados nil é válido (só exercita o ramo local).
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

// ── ListarElegiveisEnriquecimentoRetroativo (a query) ───────────────────────

// TestListarElegiveisEnriquecimentoRetroativoQuadrantes cobre o critério
// central §5.1: a query é o complemento EXATO (De Morgan) de ListarRevisao,
// provado nos quatro quadrantes de status/confiança/identidade.
func TestListarElegiveisEnriquecimentoRetroativoQuadrantes(t *testing.T) {
	_, d := setupEntitiesServer(t)
	ctx := context.Background()

	// Quadrante A: draft -- já está na fila de Revisão, fora de escopo aqui.
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/draft.mkv", "present", 0, "")

	// Quadrante B: baixa confiança (0<conf<70), sem id -- já está na fila de
	// Revisão, também fora de escopo.
	semearEntity(t, d, "e-baixa", "movie", "Baixa")
	semearFile(t, d, "f-baixa", "e-baixa", "/lib/baixa.mkv", "present", 60, "")

	// Quadrante C: confiança=70 (confTituloAno do scanner), SEM id externo --
	// o GAP real deste briefing (nunca verificado contra provider nenhum).
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/gap.mkv", "present", 70, "")

	// Quadrante D: confiança=70, COM id externo -- já tem identidade, fora do
	// escopo (external_ids_json IS NULL ancora a query).
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

// TestListarElegiveisEnriquecimentoRetroativoRestricoesDeEscopo cobre as
// restrições adicionais do §4: só type IN (movie, tv) e só entities com ao
// menos um media_file 'present'.
func TestListarElegiveisEnriquecimentoRetroativoRestricoesDeEscopo(t *testing.T) {
	_, d := setupEntitiesServer(t)

	// collection não é alvo (nível obra/série-topo só, mesmo escopo do 0050).
	semearEntity(t, d, "e-colecao", "collection", "Coleção")
	semearFile(t, d, "f-colecao", "e-colecao", "/lib/colecao.mkv", "present", 70, "")

	// Sem NENHUM media_file present (todos missing) -- fora de escopo.
	semearEntity(t, d, "e-missing", "movie", "Missing")
	semearFile(t, d, "f-missing", "e-missing", "/lib/missing.mkv", "missing", 70, "")

	// tv com confiança 70, present -- DEVE ser elegível (mesmo escopo de movie).
	semearEntity(t, d, "e-tv", "tv", "Serie")
	semearFile(t, d, "f-tv", "e-tv", "/lib/serie.mkv", "present", 70, "")

	got := idsDeRetroativo(listarElegiveisTeste(t, d, 100))
	if got["e-colecao"] {
		t.Error("collection não deveria ser elegível (fora do escopo movie/tv)")
	}
	if got["e-missing"] {
		t.Error("entity sem nenhum media_file present não deveria ser elegível")
	}
	if !got["e-tv"] {
		t.Error("tv com confiança=70 deveria ser elegível (mesmo escopo de movie)")
	}
}

// TestListarElegiveisEnriquecimentoRetroativoThrottle cobre o critério §5.5:
// item tentado há MENOS de intervaloReTentativaRetroativa não volta;
// nunca tentado e tentado há MAIS tempo voltam. Fixture de tempo CONTROLADO
// (não time.Now() real).
func TestListarElegiveisEnriquecimentoRetroativoThrottle(t *testing.T) {
	_, d := setupEntitiesServer(t)
	agora := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	semearEntity(t, d, "e-nunca", "movie", "Nunca Tentado")
	semearFile(t, d, "f-nunca", "e-nunca", "/lib/nunca.mkv", "present", 70, "")

	semearEntity(t, d, "e-recente", "movie", "Tentado Recente")
	semearFile(t, d, "f-recente", "e-recente", "/lib/recente.mkv", "present", 70, "")
	carimbarTentativa(t, d, "e-recente", agora.Add(-1*time.Hour))

	semearEntity(t, d, "e-antigo", "movie", "Tentado Antigo")
	semearFile(t, d, "f-antigo", "e-antigo", "/lib/antigo.mkv", "present", 70, "")
	carimbarTentativa(t, d, "e-antigo", agora.Add(-31*24*time.Hour))

	desde := agora.Add(-intervaloReTentativaRetroativa).UTC().Format(carimboRetroativoFormato)
	rows, err := escrita(t, d).ListarElegiveisEnriquecimentoRetroativo(context.Background(),
		sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
			Limiar: sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
			Desde:  sql.NullString{String: desde, Valid: true},
			Limite: 100,
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

// TestListarElegiveisEnriquecimentoRetroativoLimiteEPrioridade cobre o
// critério §5.6: nunca processa mais que o limite, e nunca-tentados vêm
// ANTES de tentados-há-muito-tempo (mais antigo primeiro entre estes).
func TestListarElegiveisEnriquecimentoRetroativoLimiteEPrioridade(t *testing.T) {
	_, d := setupEntitiesServer(t)
	agora := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

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

	desde := agora.Add(-intervaloReTentativaRetroativa).UTC().Format(carimboRetroativoFormato)
	rows, err := escrita(t, d).ListarElegiveisEnriquecimentoRetroativo(context.Background(),
		sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
			Limiar: sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
			Desde:  sql.NullString{String: desde, Valid: true},
			Limite: 3,
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

// listarElegiveisTeste chama a query com um `desde` no futuro (qualquer item
// nunca tentado ou tentado no passado volta) e um limite alto -- usado pelos
// testes que não exercitam o throttle/limite em si.
func listarElegiveisTeste(t *testing.T, d *db.DB, limite int64) []sqlcgen.ListarElegiveisEnriquecimentoRetroativoRow {
	t.Helper()
	desde := time.Now().Add(24 * time.Hour).UTC().Format(carimboRetroativoFormato)
	rows, err := escrita(t, d).ListarElegiveisEnriquecimentoRetroativo(context.Background(),
		sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
			Limiar: sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
			Desde:  sql.NullString{String: desde, Valid: true},
			Limite: limite,
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

// ── EnriquecerRetroativo / tentarEnriquecerOuSinalizar (a orquestração) ─────

// TestEnriquecerRetroativoSemEnrich cobre o critério §5.7: sem s.enrich
// (sem provider/assetsDir), devolve (0,0,nil) SEM tocar
// last_external_match_attempt_at -- o item mantém prioridade máxima.
func TestEnriquecerRetroativoSemEnrich(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	s := New("v-teste", d) // sem WithSearchProvider/WithAssetsDir => s.enrich nil
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

// TestEnriquecerRetroativoAplicaLocal cobre o critério §5.2 pelo ramo LOCAL:
// candidato local com score >= LimiarAutoMatch aplica sozinho via
// fundirEntities -- o item some da população elegível (ganhou identidade
// local, ainda que sem external_ids).
func TestEnriquecerRetroativoAplicaLocal(t *testing.T) {
	s, d := setupRetroativoServer(t, nil) // sem candidatos externos
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

	// e-gap (perdedora) foi fundida em e-target; o arquivo migrou.
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

// TestEnriquecerRetroativoAplicaExterno cobre o critério §5.2 pelo ramo
// EXTERNO: sem candidato local, mas com um candidato externo forte, aplica
// pela sequência do "Corrigir" (aplicarCandidatoExterno) -- e carimba a
// tentativa.
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

// TestEnriquecerRetroativoSinaliza cobre o critério §5.3: candidato entre
// limiarSinalizacaoRetroativa (50) e LimiarAutoMatch (92) NUNCA é aplicado
// sozinho -- só sinaliza (AtualizarConfidencePorEntity), com o TETO
// min(score, limiarBaixaConfianca-1) = 69, fazendo o item aparecer em
// GET /review.
func TestEnriquecerRetroativoSinaliza(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)
	// "The Matrix Reloaded" (candidato) vs "The Matrix" (consulta): casa
	// parcialmente -- score plausível (~90) mas abaixo de 92. Sem release_date
	// no candidato (evita o bônus de ano, mantendo o score sob controle); o
	// título do candidato PRECISA conter as mesmas palavras da consulta como
	// prefixo (busca local é FTS5 com AND implícito entre termos).
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

	// external_ids continua NULO -- nada foi aplicado.
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

	// O item agora aparece na fila de Revisão (mesmo mecanismo já existente).
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !idsDe(decodeCards(t, do(t, h, "GET", "/api/v1/review", nil)))["e-gap"] {
		t.Error("e-gap deveria aparecer na fila de Revisão após ser sinalizado")
	}
}

// TestEnriquecerRetroativoSemCandidatoSoCarimba cobre o critério §5.4: sem
// candidato plausível em nenhuma fonte (abaixo do piso de sinalização), o
// item NÃO muda -- só a tentativa é registrada (throttle).
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

// TestEnriquecerRetroativoLoteMisto cobre o critério §5.8 (best-effort): um
// lote com resultados DIFERENTES por item (aplicado, sinalizado, sem
// candidato) processa todos -- nenhum item afeta o resultado dos outros, e
// TODOS têm a tentativa carimbada.
func TestEnriquecerRetroativoLoteMisto(t *testing.T) {
	s, d := setupRetroativoServer(t, nil)

	// Aplica sozinho (score alto).
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-aplica", "movie", "Aplica")
	semearFile(t, d, "f-aplica", "e-aplica", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 70, "")

	// Sinaliza (score médio; candidato contém as mesmas palavras da consulta
	// como prefixo, para casar na busca local FTS5, mas com título longo o
	// bastante para o score ficar abaixo de LimiarAutoMatch).
	semearEntity(t, d, "e-candidato-fraco", "movie", "Blade Runner Black Lotus Animated Series")
	semearEntity(t, d, "e-sinaliza", "movie", "Sinaliza")
	semearFile(t, d, "f-sinaliza", "e-sinaliza", "/lib/Blade Runner (2017)/Blade Runner (2017).mkv", "present", 70, "")

	// Sem candidato nenhum.
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

	// Os TRÊS itens (mesmo o que não mudou) têm a tentativa carimbada.
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

// ── POST /entities/{id}/enrich-missing (briefing 0059) ─────────────────────

// decodeEnrichMissing decodifica a resposta do enrich-missing.
func decodeEnrichMissing(t *testing.T, body []byte) enrichMissingResultDTO {
	t.Helper()
	var out enrichMissingResultDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar enrichMissingResultDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestEnrichMissingAplicaLocal cobre o critério §5: candidato local com score
// >= LimiarAutoMatch aplica sozinho (outcome=applied, method=local) via UM
// POST individual -- mesma asserção do critério equivalente do 0056
// (TestEnriquecerRetroativoAplicaLocal), só que disparado pelo endpoint HTTP
// para UM item.
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

	// e-gap (perdedora) foi fundida em e-target -- mesma prova de aplicação
	// do teste em lote equivalente.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "e-gap"); err == nil {
		t.Error("e-gap ainda existe após aplicar local, quero fundida/apagada")
	}
}

// TestEnrichMissingAplicaExterno cobre o critério §5 pelo ramo EXTERNO:
// candidato externo forte aplica via a sequência do "Corrigir"
// (aplicarCandidatoExterno) e o corpo traz method=external.
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

// TestEnrichMissingSerieConstroiArvore cobre o briefing 0067: o retroativo/
// enrich-missing individual (tentarEnriquecerOuSinalizar -> aplicarCandidatoExterno ->
// s.enrich.Enriquecer) sobre uma entity type='tv' constroi temporada/episodio,
// exatamente como POST /import ja construia -- GET /entities/{id}/members deixa de vir
// vazio. Usa o fixture canned de fake.Provider (mesma arvore de internal/enrich/
// enrich_arvore_test.go: 2 temporadas de 3 episodios).
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

// TestEnrichMissingSinaliza cobre o critério §5: candidato médio (score
// entre limiarSinalizacaoRetroativa e LimiarAutoMatch) sinaliza
// (outcome=flagged, com score mas SEM method) -- e o item passa a aparecer
// em GET /review.
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

// TestEnrichMissingSemCandidato cobre o critério §5: sem candidato plausível
// em nenhuma fonte, outcome=none (sem method/score), 200, e a tentativa AINDA
// é carimbada.
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

// TestEnrichMissingIgnoraThrottle cobre o critério §5: o endpoint individual
// FUNCIONA mesmo quando last_external_match_attempt_at foi carimbado há
// MENOS de intervaloReTentativaRetroativa (30 dias) -- diferente do job em
// lote (EnriquecerRetroativo), que nunca chegaria a chamar
// tentarEnriquecerOuSinalizar para este item (filtrado pela query). O
// endpoint não faz NENHUMA checagem de throttle antes de chamar a
// orquestração.
func TestEnrichMissingIgnoraThrottle(t *testing.T) {
	resultados := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "335984", Type: "movie", Title: "Blade Runner 2049", Year: strp("2017")},
	}
	s, d := setupRetroativoServer(t, resultados)
	semearEntity(t, d, "e-gap", "movie", "Gap")
	semearFile(t, d, "f-gap", "e-gap", "/lib/Blade Runner 2049 (2017)/Blade Runner 2049 (2017).mkv", "present", 70, "")
	// Carimba uma tentativa BEM recente -- um throttle de lote bloquearia.
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

// TestEnrichMissingEntidadeInexistente: 404 para entity que não existe.
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
