// Testes de fumaça do acesso gerado pelo sqlc. Ficam NO pacote sqlcgen (como
// pacote de teste externo sqlcgen_test) de propósito: assim a cobertura das
// funções geradas é atribuída ao PRÓPRIO pacote sqlcgen no perfil do `go test`
// (a atribuição cruzada entre pacotes não vale sem -coverpkg). Aqui exercitamos
// cada query ao menos uma vez sobre um banco real, com o schema aplicado pelas
// migrations via o pacote db.
package sqlcgen_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setup abre um banco temporário migrado (schema 4) e devolve os Queries no pool
// de escrita.
func setup(t *testing.T) *sqlcgen.Queries {
	t.Helper()
	d, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "sqlcgen.sqlite")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqlcgen.New(d.Write)
}

func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// check falha o teste se err for não-nulo. Mantém as funções de teste simples.
func check(t *testing.T, err error, ctx string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", ctx, err)
	}
}

// TestCRUDEntity cobre o ciclo completo da entity, incluindo lookups por id
// externo (colunas geradas) e a query de posse derivada.
func TestCRUDEntity(t *testing.T) {
	q := setup(t)
	ctx := context.Background()

	e, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "ent-1", Type: "movie", Title: "Matrix",
		OriginalTitle:   ns("The Matrix"),
		Description:     ns("Neo descobre a verdade."),
		ReleaseDate:     ns("1999-03-31"),
		TagsJson:        ns(`["scifi"]`),
		ExternalIdsJson: ns(`{"tmdb":"603","imdb":"tt0133093"}`),
	})
	check(t, err, "CriarEntity")
	if e.Status != "cataloged" {
		t.Errorf("status default = %q, quero cataloged", e.Status)
	}

	_, err = q.ObterEntity(ctx, e.ID)
	check(t, err, "ObterEntity")
	got, err := q.ObterEntityPorTmdb(ctx, ns("603"))
	check(t, err, "ObterEntityPorTmdb")
	if got.ID != e.ID {
		t.Errorf("ObterEntityPorTmdb id = %q, quero %q", got.ID, e.ID)
	}
	_, err = q.ObterEntityPorImdb(ctx, ns("tt0133093"))
	check(t, err, "ObterEntityPorImdb")

	lista, err := q.ListarEntities(ctx, sqlcgen.ListarEntitiesParams{Limit: 10, Offset: 0})
	check(t, err, "ListarEntities")
	if len(lista) != 1 {
		t.Errorf("ListarEntities = %d, quero 1", len(lista))
	}

	upd, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID: e.ID, Type: "movie", Title: "Matrix (upgrade)", Status: "wanted",
	})
	check(t, err, "AtualizarEntity")
	if upd.Status != "wanted" || upd.Title != "Matrix (upgrade)" {
		t.Errorf("update não refletiu: %+v", upd)
	}

	// posse: falsa sem arquivo, verdadeira com present.
	if tem, _ := q.EntityTemPosse(ctx, e.ID); tem {
		t.Error("posse sem arquivo = true, quero false")
	}
	_, err = q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-1", EntityID: e.ID, Path: "/m.mkv", State: "present",
	})
	check(t, err, "CriarMediaFile")
	if tem, _ := q.EntityTemPosse(ctx, e.ID); !tem {
		t.Error("posse com present = false, quero true")
	}

	check(t, q.ApagarEntity(ctx, e.ID), "ApagarEntity")
}

// TestCRUDMediaFile cobre criar/obter/listar/atualizar-estado/apagar.
func TestCRUDMediaFile(t *testing.T) {
	q := setup(t)
	ctx := context.Background()
	e, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e", Type: "movie", Title: "T"})
	check(t, err, "CriarEntity")

	mf, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-1", EntityID: e.ID, Path: "/a.mkv", State: "present",
		ContentHash:     ns("deadbeef"),
		Size:            sql.NullInt64{Int64: 123, Valid: true},
		Mtime:           ns("2026-07-09T00:00:00Z"),
		Edition:         ns("Director's Cut"),
		PartNo:          sql.NullInt64{Int64: 1, Valid: true},
		MatchConfidence: sql.NullFloat64{Float64: 0.9, Valid: true},
		MediainfoJson:   ns(`{"codec":"h264"}`),
		LastSeen:        ns("2026-07-09T00:00:00Z"),
	})
	check(t, err, "CriarMediaFile")

	_, err = q.ObterMediaFile(ctx, mf.ID)
	check(t, err, "ObterMediaFile")
	fs, err := q.ListarMediaFilesPorEntity(ctx, e.ID)
	check(t, err, "ListarMediaFilesPorEntity")
	if len(fs) != 1 {
		t.Errorf("ListarMediaFilesPorEntity = %d, quero 1", len(fs))
	}
	upd, err := q.AtualizarMediaFileState(ctx, sqlcgen.AtualizarMediaFileStateParams{
		ID: mf.ID, State: "missing", LastSeen: ns("2026-07-10T00:00:00Z"),
	})
	check(t, err, "AtualizarMediaFileState")
	if upd.State != "missing" {
		t.Errorf("state após update = %q, quero missing", upd.State)
	}
	check(t, q.ApagarMediaFile(ctx, mf.ID), "ApagarMediaFile")
}

// TestCRUDAssetRelation cobre asset e entity_relation.
func TestCRUDAssetRelation(t *testing.T) {
	q := setup(t)
	ctx := context.Background()
	e1, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "collection", Title: "Trilogia"})
	check(t, err, "CriarEntity e1")
	e2, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e2", Type: "movie", Title: "Filme"})
	check(t, err, "CriarEntity e2")

	as, err := q.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID: "as-1", EntityID: e2.ID, Kind: "poster", Origin: "local",
		PathOrUri: "/p.jpg", MetaJson: ns(`{"w":600}`),
	})
	check(t, err, "CriarAsset")
	_, err = q.ObterAsset(ctx, as.ID)
	check(t, err, "ObterAsset")
	assets, err := q.ListarAssetsPorEntity(ctx, e2.ID)
	check(t, err, "ListarAssetsPorEntity")
	if len(assets) != 1 {
		t.Errorf("ListarAssetsPorEntity = %d, quero 1", len(assets))
	}
	check(t, q.ApagarAsset(ctx, as.ID), "ApagarAsset")

	rel, err := q.CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID: "rel-1", ParentID: e1.ID, ChildID: e2.ID, Kind: "collection",
	})
	check(t, err, "CriarEntityRelation")
	_, err = q.ObterEntityRelation(ctx, rel.ID)
	check(t, err, "ObterEntityRelation")
	filhas, err := q.ListarRelacoesFilhas(ctx, e1.ID)
	check(t, err, "ListarRelacoesFilhas")
	if len(filhas) != 1 {
		t.Errorf("ListarRelacoesFilhas = %d, quero 1", len(filhas))
	}
	pais, err := q.ListarRelacoesPais(ctx, e2.ID)
	check(t, err, "ListarRelacoesPais")
	if len(pais) != 1 {
		t.Errorf("ListarRelacoesPais = %d, quero 1", len(pais))
	}
	check(t, q.ApagarEntityRelation(ctx, rel.ID), "ApagarEntityRelation")
}

// TestCRUDMediaExternalOverride cobre o CRUD das camadas externo/override e prova
// o round-trip: payload_json/override_json voltam intactos, inclusive um JSON com
// acento no VALOR (o valor e dado, nao SQL -- passa byte a byte).
func TestCRUDMediaExternalOverride(t *testing.T) {
	q := setup(t)
	ctx := context.Background()
	e, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e", Type: "movie", Title: "T"})
	check(t, err, "CriarEntity")

	// media_external: upsert com payload contendo acento no valor.
	const payload = `{"titulo":"Coração Selvagem","tagline":"Amor à primeira vista"}`
	ext, err := q.UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID: "ext-1", EntityID: e.ID, Source: "tmdb",
		SourceID: ns("603"), PayloadJson: ns(payload),
	})
	check(t, err, "UpsertMediaExternal")
	if ext.PayloadJson.String != payload {
		t.Errorf("payload_json apos upsert = %q, quero %q", ext.PayloadJson.String, payload)
	}

	got, err := q.ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: e.ID, Source: "tmdb",
	})
	check(t, err, "ObterMediaExternalPorFonte")
	if got.PayloadJson.String != payload {
		t.Errorf("payload_json apos leitura = %q, quero %q (round-trip)", got.PayloadJson.String, payload)
	}

	lista, err := q.ListarMediaExternalPorEntity(ctx, e.ID)
	check(t, err, "ListarMediaExternalPorEntity")
	if len(lista) != 1 {
		t.Errorf("ListarMediaExternalPorEntity = %d, quero 1", len(lista))
	}
	check(t, q.ApagarMediaExternal(ctx, ext.ID), "ApagarMediaExternal")

	// media_override: upsert com override contendo acento no valor.
	const override = `{"title":"Coração","description":"Sinopse com acentuação"}`
	ov, err := q.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID: e.ID, OverrideJson: ns(override),
	})
	check(t, err, "UpsertMediaOverride")
	if ov.OverrideJson.String != override {
		t.Errorf("override_json apos upsert = %q, quero %q", ov.OverrideJson.String, override)
	}
	gotOv, err := q.ObterMediaOverride(ctx, e.ID)
	check(t, err, "ObterMediaOverride")
	if gotOv.OverrideJson.String != override {
		t.Errorf("override_json apos leitura = %q, quero %q (round-trip)", gotOv.OverrideJson.String, override)
	}
	check(t, q.ApagarMediaOverride(ctx, e.ID), "ApagarMediaOverride")
}

// TestCRUDLibrarySourceEMeta cobre library_source e a query-semente ValorMeta.
func TestCRUDLibrarySourceEMeta(t *testing.T) {
	q := setup(t)
	ctx := context.Background()

	ls, err := q.CriarLibrarySource(ctx, sqlcgen.CriarLibrarySourceParams{
		ID: "ls-1", Path: "/mnt/filmes", Type: "movies",
	})
	check(t, err, "CriarLibrarySource")
	if ls.State != "online" {
		t.Errorf("state default = %q, quero online", ls.State)
	}
	_, err = q.ObterLibrarySource(ctx, ls.ID)
	check(t, err, "ObterLibrarySource")
	fontes, err := q.ListarLibrarySources(ctx)
	check(t, err, "ListarLibrarySources")
	if len(fontes) != 1 {
		t.Errorf("ListarLibrarySources = %d, quero 1", len(fontes))
	}
	upd, err := q.AtualizarLibrarySource(ctx, sqlcgen.AtualizarLibrarySourceParams{
		ID: ls.ID, Type: "movies", State: "offline", LastScan: ns("2026-07-09T00:00:00Z"),
	})
	check(t, err, "AtualizarLibrarySource")
	if upd.State != "offline" {
		t.Errorf("state após update = %q, quero offline", upd.State)
	}
	check(t, q.ApagarLibrarySource(ctx, ls.ID), "ApagarLibrarySource")

	// query-semente do 0006.
	v, err := q.ValorMeta(ctx, "fundacao")
	check(t, err, "ValorMeta")
	if v != "0006" {
		t.Errorf("ValorMeta(fundacao) = %q, quero 0006", v)
	}
}

// TestCRUDJob cobre o schema de jobs (briefing 0052): a definicao seed
// (backup_vacuum, migration 0011), a consulta de agendaveis, o ciclo de uma
// execucao e a poda do historico por contagem.
func TestCRUDJob(t *testing.T) {
	q := setup(t)
	ctx := context.Background()

	defs, err := q.ListarJobDefinitions(ctx)
	check(t, err, "ListarJobDefinitions")
	if len(defs) != 1 || defs[0].ID != "backup_vacuum" {
		t.Fatalf("ListarJobDefinitions = %+v, quero 1 linha (backup_vacuum)", defs)
	}
	if defs[0].Enabled != 0 || !defs[0].ScheduleCron.Valid || defs[0].ScheduleCron.String != "0 3 * * *" {
		t.Errorf("seed backup_vacuum = %+v, quero enabled=0 schedule='0 3 * * *'", defs[0])
	}

	obtido, err := q.ObterJobDefinition(ctx, "backup_vacuum")
	check(t, err, "ObterJobDefinition")
	if obtido.ID != "backup_vacuum" {
		t.Errorf("ObterJobDefinition.ID = %q, quero backup_vacuum", obtido.ID)
	}

	atualizado, err := q.AtualizarJobConfig(ctx, sqlcgen.AtualizarJobConfigParams{
		Enabled: 1, ScheduleCron: ns("0 4 * * *"), NextRunAt: ns("2026-07-14T04:00:00Z"), ID: "backup_vacuum",
	})
	check(t, err, "AtualizarJobConfig")
	if atualizado.Enabled != 1 || atualizado.ScheduleCron.String != "0 4 * * *" {
		t.Errorf("AtualizarJobConfig = %+v, quero enabled=1 schedule='0 4 * * *'", atualizado)
	}

	agendaveisCedo, err := q.ListarJobDefinitionsAgendaveis(ctx, ns("2026-07-14T03:00:00Z"))
	check(t, err, "ListarJobDefinitionsAgendaveis (cedo)")
	if len(agendaveisCedo) != 0 {
		t.Errorf("agendaveis antes do next_run_at = %d, quero 0", len(agendaveisCedo))
	}
	agendaveisNaHora, err := q.ListarJobDefinitionsAgendaveis(ctx, ns("2026-07-14T05:00:00Z"))
	check(t, err, "ListarJobDefinitionsAgendaveis (na hora)")
	if len(agendaveisNaHora) != 1 {
		t.Errorf("agendaveis apos o next_run_at = %d, quero 1", len(agendaveisNaHora))
	}

	for i, trigger := range []string{"manual", "schedule", "schedule"} {
		id := "exec-" + string(rune('1'+i))
		exec, err := q.CriarJobExecucao(ctx, sqlcgen.CriarJobExecucaoParams{ID: id, JobID: "backup_vacuum", Trigger: trigger})
		check(t, err, "CriarJobExecucao")
		if exec.Status != "running" {
			t.Errorf("CriarJobExecucao.Status = %q, quero running", exec.Status)
		}
		finalizado, err := q.FinalizarJobExecucao(ctx, sqlcgen.FinalizarJobExecucaoParams{
			Status: "success", Message: ns("backup gravado (7 mantidos)"), ID: id,
		})
		check(t, err, "FinalizarJobExecucao")
		if finalizado.Status != "success" || !finalizado.FinishedAt.Valid {
			t.Errorf("FinalizarJobExecucao = %+v, quero status=success com finished_at", finalizado)
		}
	}

	defFinal, err := q.FinalizarJobDefinicao(ctx, sqlcgen.FinalizarJobDefinicaoParams{
		LastRunAt: ns("2026-07-14T04:00:00Z"), LastStatus: ns("success"),
		LastMessage: ns("backup gravado (7 mantidos)"), NextRunAt: ns("2026-07-15T04:00:00Z"), ID: "backup_vacuum",
	})
	check(t, err, "FinalizarJobDefinicao")
	if defFinal.LastStatus.String != "success" {
		t.Errorf("FinalizarJobDefinicao.LastStatus = %+v, quero success", defFinal.LastStatus)
	}

	execs, err := q.ListarJobExecucoes(ctx, sqlcgen.ListarJobExecucoesParams{JobID: "backup_vacuum", Limit: 10})
	check(t, err, "ListarJobExecucoes")
	if len(execs) != 3 {
		t.Fatalf("ListarJobExecucoes = %d, quero 3", len(execs))
	}

	// Retencao: manter só a mais recente (offset=1), as outras duas são excedente.
	excedentes, err := q.ListarJobExecucaoIdsExcedentes(ctx, sqlcgen.ListarJobExecucaoIdsExcedentesParams{
		JobID: "backup_vacuum", Offset: 1,
	})
	check(t, err, "ListarJobExecucaoIdsExcedentes")
	if len(excedentes) != 2 {
		t.Fatalf("ListarJobExecucaoIdsExcedentes = %d, quero 2", len(excedentes))
	}
	for _, id := range excedentes {
		check(t, q.ApagarJobExecucao(ctx, id), "ApagarJobExecucao")
	}

	restantes, err := q.ListarJobExecucoes(ctx, sqlcgen.ListarJobExecucoesParams{JobID: "backup_vacuum", Limit: 10})
	check(t, err, "ListarJobExecucoes (pos-poda)")
	if len(restantes) != 1 {
		t.Errorf("ListarJobExecucoes pos-poda = %d, quero 1", len(restantes))
	}
}
