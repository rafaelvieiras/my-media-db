package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// episodes_test cobre o SELETOR DE ORDENACAO de episodios (0040): a anotacao
// `orderings` no detalhe da serie e o endpoint GET /entities/{id}/episodes?ordering=X
// que agrupa os episodios projetados na ordem pedida. Prova a uniao das ordens (com o
// default primeiro e o absoluto ao fim), o reagrupamento por DVD, a lista corrida
// absoluta, o fallback ao default numa ordem invalida e o grupo "Sem numeracao".

// detalheDe faz GET /entities/{id} e decodifica a entity RESOLVIDA (exige 200).
func detalheDe(t *testing.T, h http.Handler, id string) resolver.Entidade {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("detalhe %s: status = %d, quero 200 (corpo=%s)", id, rec.Code, rec.Body.String())
	}
	return decodeResolvida(t, rec.Body.Bytes())
}

// episodesDe faz GET /entities/{id}/episodes(?ordering=) e decodifica os grupos.
func episodesDe(t *testing.T, h http.Handler, id, ordering string) episodeGroupsDTO {
	t.Helper()
	target := "/api/v1/entities/" + id + "/episodes"
	if ordering != "" {
		target += "?ordering=" + ordering
	}
	rec := do(t, h, http.MethodGet, target, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("episodes %s (%q): status = %d, quero 200 (corpo=%s)", id, ordering, rec.Code, rec.Body.String())
	}
	var out episodeGroupsDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar episodeGroupsDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return out
}

// semearSerieOfficialDvd monta uma serie com 2 temporadas (official) e 3 episodios cujo
// numbering DIVERGE entre official e dvd, todos com absoluto. Reusada pelos testes de
// agrupamento. official: T1[e1,e2], T2[e3]; dvd: T1[e1], T2[e2,e3]; absoluto: 1,2,3.
func semearSerieOfficialDvd(t *testing.T, d *db.DB) {
	t.Helper()
	semearEntity(t, d, "s1", "tv", "Serie")
	semearExtension(t, d, "s1", `{"core.tv@1":{"default_season_type":"official"}}`)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	semearEntity(t, d, "se2", "season", "Temporada 2")
	semearExtension(t, d, "se2", `{"core.tv@1":{"ordering":"official","season_number":2}}`)
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-se2", "s1", "se2", "tv_season", 2)

	semearEntity(t, d, "e1", "episode", "Ep 1")
	semearExtension(t, d, "e1", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":1},"dvd":{"season":1,"number":1}},"absolute_number":1}}`)
	semearEntity(t, d, "e2", "episode", "Ep 2")
	semearExtension(t, d, "e2", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":2},"dvd":{"season":2,"number":1}},"absolute_number":2}}`)
	semearEntity(t, d, "e3", "episode", "Ep 3")
	semearExtension(t, d, "e3", `{"core.tv@1":{"numbering":{"official":{"season":2,"number":1},"dvd":{"season":2,"number":2}},"absolute_number":3}}`)
	ligarFilhoOrdenado(t, d, "r-e1", "se1", "e1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-e2", "se1", "e2", "tv_episode", 2)
	ligarFilhoOrdenado(t, d, "r-e3", "se2", "e3", "tv_episode", 1)
}

// idsDosGrupos achata os grupos em (season -> ids dos episodios na ordem), para asserir
// o agrupamento sem repetir o walk nos testes.
func idsDosGrupos(g episodeGroupsDTO) map[int][]string {
	out := map[int][]string{}
	for _, grp := range g.Groups {
		ids := make([]string, 0, len(grp.Episodes))
		for _, ep := range grp.Episodes {
			ids = append(ids, ep.ID)
		}
		out[grp.Season] = ids
	}
	return out
}

// TestOrderingsAnotadoNaSerie: o detalhe da serie traz orderings{default,available} com
// o default (default_season_type) primeiro, as ordens de numbering e o absoluto ao fim.
func TestOrderingsAnotadoNaSerie(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearSerieOfficialDvd(t, d)

	det := detalheDe(t, h, "s1")
	if det.Orderings == nil {
		t.Fatalf("serie sem orderings; quero {default:official, available:[official,dvd,absolute]}")
	}
	eq(t, "default", det.Orderings.Default, "official")
	quero := []string{"official", "dvd", "absolute"}
	if len(det.Orderings.Available) != len(quero) {
		t.Fatalf("available = %v, quero %v", det.Orderings.Available, quero)
	}
	for i, o := range quero {
		eq(t, "available["+o+"]", det.Orderings.Available[i], o)
	}
}

// TestOrderingsAusenteParaNaoSerie: um filme nao carrega orderings (so a serie).
func TestOrderingsAusenteParaNaoSerie(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "m1", "movie", "Filme")

	det := detalheDe(t, h, "m1")
	if det.Orderings != nil {
		t.Errorf("filme com orderings %+v; quero nil", det.Orderings)
	}
}

// TestOrderingsUmaSoOrdem: serie cujos episodios so tem "official" (sem dvd, sem
// absoluto) tem available de 1 item -- a UI entao esconde o seletor.
func TestOrderingsUmaSoOrdem(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearExtension(t, d, "s1", `{"core.tv@1":{"default_season_type":"official"}}`)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	semearEntity(t, d, "e1", "episode", "Ep 1")
	semearExtension(t, d, "e1", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":1}}}}`)
	ligarFilhoOrdenado(t, d, "r-e1", "se1", "e1", "tv_episode", 1)

	det := detalheDe(t, h, "s1")
	if det.Orderings == nil || len(det.Orderings.Available) != 1 {
		t.Fatalf("orderings = %+v; quero available de 1 item (official)", det.Orderings)
	}
	eq(t, "unica ordem", det.Orderings.Available[0], "official")
}

// TestEpisodesDefaultAgrupaOficial: sem ?ordering (cai no default official), os
// episodios saem agrupados nas temporadas oficiais (T1[e1,e2], T2[e3]).
func TestEpisodesDefaultAgrupaOficial(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearSerieOfficialDvd(t, d)

	g := episodesDe(t, h, "s1", "")
	eq(t, "ordering aplicada", g.Ordering, "official")
	grupos := idsDosGrupos(g)
	eqSlice(t, "T1 oficial", grupos[1], []string{"e1", "e2"})
	eqSlice(t, "T2 oficial", grupos[2], []string{"e3"})
}

// TestEpisodesAgrupaDvd: ?ordering=dvd reagrupa/renumera pela ordem DVD (T1[e1],
// T2[e2,e3]) -- prova que a ordem alternativa e projetada na leitura, nao a oficial.
func TestEpisodesAgrupaDvd(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearSerieOfficialDvd(t, d)

	g := episodesDe(t, h, "s1", "dvd")
	eq(t, "ordering aplicada", g.Ordering, "dvd")
	grupos := idsDosGrupos(g)
	eqSlice(t, "T1 dvd", grupos[1], []string{"e1"})
	eqSlice(t, "T2 dvd", grupos[2], []string{"e2", "e3"})
	// O card do e2 carrega o numbering JA em dvd (T2E01, nao a posicao oficial).
	for _, grp := range g.Groups {
		for _, ep := range grp.Episodes {
			if ep.ID == "e2" {
				eqPtrField(t, "e2 dvd season", ep.Numbering, func(n *episodeNumberingDTO) int { return n.Season }, 2)
				eqPtrField(t, "e2 dvd number", ep.Numbering, func(n *episodeNumberingDTO) int { return n.Number }, 1)
			}
		}
	}
}

// TestEpisodesAbsolutoCorrido: ?ordering=absolute devolve UM grupo corrido ("Episodios",
// season 0) ordenado pelo absoluto (e1,e2,e3), sem temporadas.
func TestEpisodesAbsolutoCorrido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearSerieOfficialDvd(t, d)

	g := episodesDe(t, h, "s1", "absolute")
	eq(t, "ordering aplicada", g.Ordering, "absolute")
	if len(g.Groups) != 1 {
		t.Fatalf("grupos = %d, quero 1 (lista corrida absoluta)", len(g.Groups))
	}
	eq(t, "label do grupo absoluto", g.Groups[0].Label, "Episódios")
	eq(t, "season do grupo absoluto", g.Groups[0].Season, 0)
	eqSlice(t, "absoluto corrido", idsDosGrupos(g)[0], []string{"e1", "e2", "e3"})
}

// TestEpisodesOrderingInvalidoCaiNoDefault: ?ordering=xyz (inexistente) cai no default.
func TestEpisodesOrderingInvalidoCaiNoDefault(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearSerieOfficialDvd(t, d)

	g := episodesDe(t, h, "s1", "xyz")
	eq(t, "ordering invalida cai no default", g.Ordering, "official")
}

// TestEpisodesSemNumeracaoNaOrdem: episodio SEM entrada para a ordem pedida cai num
// grupo "Sem numeracao" ao fim (nao e omitido nem inventa posicao).
func TestEpisodesSemNumeracaoNaOrdem(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearExtension(t, d, "s1", `{"core.tv@1":{"default_season_type":"official"}}`)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	// e1 tem official + dvd; e2 so tem official (sem dvd).
	semearEntity(t, d, "e1", "episode", "Ep 1")
	semearExtension(t, d, "e1", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":1},"dvd":{"season":1,"number":1}}}}`)
	semearEntity(t, d, "e2", "episode", "Ep 2")
	semearExtension(t, d, "e2", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":2}}}}`)
	ligarFilhoOrdenado(t, d, "r-e1", "se1", "e1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-e2", "se1", "e2", "tv_episode", 2)

	g := episodesDe(t, h, "s1", "dvd")
	ultimo := g.Groups[len(g.Groups)-1]
	eq(t, "grupo sem numeracao", ultimo.Label, "Sem numeração")
	eqSlice(t, "e2 cai no sem-numeracao", idsDosGrupos(g)[0], []string{"e2"})
	// e1 (com dvd) fica no grupo da temporada, fora do sem-numeracao.
	eqSlice(t, "e1 na T1 dvd", idsDosGrupos(g)[1], []string{"e1"})
}

// TestEpisodesSerieInexistente404: GET /episodes de uma entity inexistente e 404.
func TestEpisodesSerieInexistente404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/episodes", nil)
	eq(t, "status inexistente", rec.Code, http.StatusNotFound)
}

// TestOrdensDisponiveisDedupAbsoluto: quando o numbering ja traz a chave `absolute`
// (TheTVDB) E o episodio tem `absolute_number`, `absolute` NAO pode aparecer duas vezes
// em Available (regressao do Looney Tunes). Fica ao fim, uma unica vez, default primeiro.
func TestOrdensDisponiveisDedupAbsoluto(t *testing.T) {
	chaves := map[string]bool{"official": true, "dvd": true, "absolute": true}
	got := ordensDisponiveis(chaves, true, "official")
	eqSlice(t, "available dedup", got, []string{"official", "dvd", "absolute"})
}

// eqSlice compara duas fatias de strings (ordem inclusa).
func eqSlice(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, quero %v", name, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s = %v, quero %v", name, got, want)
			return
		}
	}
}

// eqPtrField extrai um campo do numbering (nao-nil) e compara; nil falha.
func eqPtrField(t *testing.T, name string, n *episodeNumberingDTO, get func(*episodeNumberingDTO) int, want int) {
	t.Helper()
	if n == nil {
		t.Errorf("%s: numbering nil, quero %d", name, want)
		return
	}
	eq(t, name, get(n), want)
}
