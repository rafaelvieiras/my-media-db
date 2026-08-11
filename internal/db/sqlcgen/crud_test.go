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

// TestCRUDLibrarySourcePathRecasco cobre as queries novas do briefing 0070
// (editar path de um datasource cadastrado): AtualizarLibrarySourcePath,
// ListarMediaFilePathsPorFonte, AtualizarMediaFilePath, ListarEntityIDsPorFonte
// (direto via media_file.entity_id + via a juncao media_file_entity) e
// AtualizarEntityNfoPath.
func TestCRUDLibrarySourcePathRecasco(t *testing.T) {
	q := setup(t)
	ctx := context.Background()

	ls, err := q.CriarLibrarySource(ctx, sqlcgen.CriarLibrarySourceParams{
		ID: "ls-recasco", Path: "/mnt/antigo", Type: "tv", ContentType: "series",
	})
	check(t, err, "CriarLibrarySource")

	// Entity alcancada DIRETO (media_file.entity_id).
	filme, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "filme-recasco", Type: "movie", Title: "Filme"})
	check(t, err, "CriarEntity filme")
	mf, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-recasco", EntityID: filme.ID, Path: "/mnt/antigo/filme.mkv", State: "present",
		LibrarySourceID: ns(ls.ID),
	})
	check(t, err, "CriarMediaFile")

	// Entity alcancada via JUNCAO (media_file_entity), caso do episodio.
	episodio, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "ep-recasco", Type: "episode", Title: "Ep"})
	check(t, err, "CriarEntity episodio")
	check(t, q.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: mf.ID, EntityID: episodio.ID, Kind: "tv_episode", Ordinal: 0,
	}), "CriarVinculoEntidade")

	upd, err := q.AtualizarLibrarySourcePath(ctx, sqlcgen.AtualizarLibrarySourcePathParams{
		Path: "/mnt/novo", ID: ls.ID,
	})
	check(t, err, "AtualizarLibrarySourcePath")
	if upd.Path != "/mnt/novo" {
		t.Errorf("path apos update = %q, quero /mnt/novo", upd.Path)
	}

	arquivos, err := q.ListarMediaFilePathsPorFonte(ctx, ns(ls.ID))
	check(t, err, "ListarMediaFilePathsPorFonte")
	if len(arquivos) != 1 || arquivos[0].ID != mf.ID {
		t.Fatalf("ListarMediaFilePathsPorFonte = %+v, quero 1 linha (%s)", arquivos, mf.ID)
	}

	check(t, q.AtualizarMediaFilePath(ctx, sqlcgen.AtualizarMediaFilePathParams{
		Path: "/mnt/novo/filme.mkv", ID: mf.ID,
	}), "AtualizarMediaFilePath")
	gotMf, err := q.ObterMediaFile(ctx, mf.ID)
	check(t, err, "ObterMediaFile")
	if gotMf.Path != "/mnt/novo/filme.mkv" {
		t.Errorf("path do media_file apos update = %q, quero /mnt/novo/filme.mkv", gotMf.Path)
	}

	entityIDs, err := q.ListarEntityIDsPorFonte(ctx, ns(ls.ID))
	check(t, err, "ListarEntityIDsPorFonte")
	vistos := map[string]bool{}
	for _, id := range entityIDs {
		vistos[id] = true
	}
	if !vistos[filme.ID] || !vistos[episodio.ID] || len(entityIDs) != 2 {
		t.Errorf("ListarEntityIDsPorFonte = %v, quero [%s %s]", entityIDs, filme.ID, episodio.ID)
	}

	check(t, q.AtualizarEntityNfoPath(ctx, sqlcgen.AtualizarEntityNfoPathParams{
		NfoPath: ns("/mnt/novo/tvshow.nfo"), ID: filme.ID,
	}), "AtualizarEntityNfoPath")
	gotEnt, err := q.ObterEntity(ctx, filme.ID)
	check(t, err, "ObterEntity")
	if !gotEnt.NfoPath.Valid || gotEnt.NfoPath.String != "/mnt/novo/tvshow.nfo" {
		t.Errorf("nfo_path apos update = %+v, quero /mnt/novo/tvshow.nfo", gotEnt.NfoPath)
	}
}

// TestCRUDJob cobre o schema de jobs (briefing 0052): as definicoes seed
// (backup_vacuum e saude_biblioteca da migration 0011, midias_faltantes da
// migration 0016/briefing 0064), a consulta de agendaveis, o ciclo de uma
// execucao e a poda do historico por contagem.
func TestCRUDJob(t *testing.T) {
	q := setup(t)
	ctx := context.Background()

	defs, err := q.ListarJobDefinitions(ctx)
	check(t, err, "ListarJobDefinitions")
	if len(defs) != 3 || defs[0].ID != "backup_vacuum" || defs[1].ID != "saude_biblioteca" || defs[2].ID != "midias_faltantes" {
		t.Fatalf("ListarJobDefinitions = %+v, quero 3 linhas (backup_vacuum, saude_biblioteca, midias_faltantes)", defs)
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

// TestCRUDHistoricoEvento cobre o ciclo de historico_evento (briefing 0092)
// e a extensao de eventos orfaos + backfill (briefing 0099): criacao normal
// (com entity), criacao orfa (sem entity), listagens, busca por id, contagem
// de orfaos e vinculo retroativo por tmdb/imdb/tvdb.
func TestCRUDHistoricoEvento(t *testing.T) {
	q := setup(t)
	ctx := context.Background()

	e, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: "ent-hist-1", Type: "movie", Title: "O Foragido",
		ExternalIdsJson: ns(`{"tmdb":"9417"}`),
	})
	check(t, err, "CriarEntity")

	// Evento normal, ja com entity casada.
	evNormal, err := q.CriarHistoricoEvento(ctx, sqlcgen.CriarHistoricoEventoParams{
		ID: "hist-1", EntityID: ns(e.ID), Kind: "watch", Status: "finished",
		OccurredAt: "2026-08-01T12:00:00Z", Source: "jellyfin",
	})
	check(t, err, "CriarHistoricoEvento")
	if evNormal.EntityID.String != e.ID {
		t.Errorf("CriarHistoricoEvento.EntityID = %+v, quero %s", evNormal.EntityID, e.ID)
	}

	achado, err := q.BuscarHistoricoEventoPorID(ctx, "hist-1")
	check(t, err, "BuscarHistoricoEventoPorID")
	if achado.ID != "hist-1" || achado.Kind != "watch" {
		t.Errorf("BuscarHistoricoEventoPorID = %+v, quero hist-1/watch", achado)
	}

	porEntity, err := q.ListarHistoricoEventoPorEntity(ctx, sqlcgen.ListarHistoricoEventoPorEntityParams{
		EntityID: ns(e.ID), Kind: nil,
	})
	check(t, err, "ListarHistoricoEventoPorEntity")
	if len(porEntity) != 1 {
		t.Fatalf("ListarHistoricoEventoPorEntity = %d, quero 1", len(porEntity))
	}

	ultimoPorKind, err := q.ListarUltimoHistoricoEventoPorKind(ctx, ns(e.ID))
	check(t, err, "ListarUltimoHistoricoEventoPorKind")
	if len(ultimoPorKind) != 1 {
		t.Fatalf("ListarUltimoHistoricoEventoPorKind = %d, quero 1", len(ultimoPorKind))
	}

	// Eventos orfaos: sem entity, com IDs brutos para eventual backfill.
	orfaoTmdb, err := q.CriarHistoricoEventoOrfao(ctx, sqlcgen.CriarHistoricoEventoOrfaoParams{
		ID: "hist-orfao-tmdb", Kind: "watch", Status: "finished",
		OccurredAt: "2026-08-02T12:00:00Z", Source: "jellyfin",
		RawTmdbID: ns("9417"), RawTitle: ns("O Foragido"), RawYear: ns("1993"),
	})
	check(t, err, "CriarHistoricoEventoOrfao (tmdb)")
	if orfaoTmdb.EntityID.Valid {
		t.Errorf("CriarHistoricoEventoOrfao.EntityID = %+v, quero invalido (NULL)", orfaoTmdb.EntityID)
	}

	orfaoImdb, err := q.CriarHistoricoEventoOrfao(ctx, sqlcgen.CriarHistoricoEventoOrfaoParams{
		ID: "hist-orfao-imdb", Kind: "watch", Status: "finished",
		OccurredAt: "2026-08-03T12:00:00Z", Source: "jellyfin",
		RawImdbID: ns("tt0106977"),
	})
	check(t, err, "CriarHistoricoEventoOrfao (imdb)")

	orfaoTvdb, err := q.CriarHistoricoEventoOrfao(ctx, sqlcgen.CriarHistoricoEventoOrfaoParams{
		ID: "hist-orfao-tvdb", Kind: "watch", Status: "finished",
		OccurredAt: "2026-08-04T12:00:00Z", Source: "jellyfin",
		RawTvdbID: ns("70983"),
	})
	check(t, err, "CriarHistoricoEventoOrfao (tvdb)")

	total, err := q.ContarHistoricoEventoOrfaos(ctx)
	check(t, err, "ContarHistoricoEventoOrfaos")
	if total != 3 {
		t.Fatalf("ContarHistoricoEventoOrfaos = %d, quero 3", total)
	}

	orfaos, err := q.ListarHistoricoEventoOrfaos(ctx, sqlcgen.ListarHistoricoEventoOrfaosParams{Limit: 10, Offset: 0})
	check(t, err, "ListarHistoricoEventoOrfaos")
	if len(orfaos) != 3 {
		t.Fatalf("ListarHistoricoEventoOrfaos = %d, quero 3", len(orfaos))
	}

	// Backfill: entity ganhou o tmdb -- vincula o orfao correspondente.
	linhasTmdb, err := q.VincularHistoricoEventoOrfaoPorTmdb(ctx, sqlcgen.VincularHistoricoEventoOrfaoPorTmdbParams{
		EntityID: ns(e.ID), RawTmdbID: ns("9417"),
	})
	check(t, err, "VincularHistoricoEventoOrfaoPorTmdb")
	if linhasTmdb != 1 {
		t.Errorf("VincularHistoricoEventoOrfaoPorTmdb = %d, quero 1", linhasTmdb)
	}

	linhasImdb, err := q.VincularHistoricoEventoOrfaoPorImdb(ctx, sqlcgen.VincularHistoricoEventoOrfaoPorImdbParams{
		EntityID: ns(e.ID), RawImdbID: ns("tt0106977"),
	})
	check(t, err, "VincularHistoricoEventoOrfaoPorImdb")
	if linhasImdb != 1 {
		t.Errorf("VincularHistoricoEventoOrfaoPorImdb = %d, quero 1", linhasImdb)
	}

	linhasTvdb, err := q.VincularHistoricoEventoOrfaoPorTvdb(ctx, sqlcgen.VincularHistoricoEventoOrfaoPorTvdbParams{
		EntityID: ns(e.ID), RawTvdbID: ns("70983"),
	})
	check(t, err, "VincularHistoricoEventoOrfaoPorTvdb")
	if linhasTvdb != 1 {
		t.Errorf("VincularHistoricoEventoOrfaoPorTvdb = %d, quero 1", linhasTvdb)
	}

	totalPos, err := q.ContarHistoricoEventoOrfaos(ctx)
	check(t, err, "ContarHistoricoEventoOrfaos (pos-backfill)")
	if totalPos != 0 {
		t.Errorf("ContarHistoricoEventoOrfaos pos-backfill = %d, quero 0", totalPos)
	}

	vinculado, err := q.BuscarHistoricoEventoPorID(ctx, orfaoTmdb.ID)
	check(t, err, "BuscarHistoricoEventoPorID (orfao vinculado)")
	if vinculado.EntityID.String != e.ID {
		t.Errorf("orfao vinculado EntityID = %+v, quero %s", vinculado.EntityID, e.ID)
	}

	// Apagar um evento (usado por rotinas de limpeza/testes).
	check(t, q.ApagarHistoricoEvento(ctx, orfaoImdb.ID), "ApagarHistoricoEvento")
	check(t, q.ApagarHistoricoEvento(ctx, orfaoTvdb.ID), "ApagarHistoricoEvento")

	restantesPorEntity, err := q.ListarHistoricoEventoPorEntity(ctx, sqlcgen.ListarHistoricoEventoPorEntityParams{
		EntityID: ns(e.ID), Kind: nil,
	})
	check(t, err, "ListarHistoricoEventoPorEntity (pos-apagar/backfill)")
	if len(restantesPorEntity) != 2 {
		t.Errorf("ListarHistoricoEventoPorEntity pos-backfill = %d, quero 2 (normal + tmdb vinculado -- imdb/tvdb vinculados foram apagados)", len(restantesPorEntity))
	}
}
