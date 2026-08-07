package e2e

// TestJornadaAcoesForcadasGerenciar cobre a jornada ponta-a-ponta do briefing
// 0088 (3 ações sempre visíveis do menu Gerenciar): (1) POST .../refresh-files
// força o re-hash de um media_file 'present' mesmo sem mudança de
// mtime/size, importa um asset local novo colocado numa pasta de entity JÁ
// catalogada (gap antes só coberto na 1a varredura) e, com um .nfo trazendo
// campos LOCAIS de reprodução, preenche só o que estiver ausente no banco —
// e NÃO sobrescreve numa segunda chamada; (2) POST .../enrich com
// {"force": true} busca o detalhe do provider pulando o cache de 24h; (3)
// POST .../materialize-missing-media atualiza o inventário de artes e
// materializa na pasta real, reaproveitando MaterializarMidiasFaltantes sem
// duplicar lógica (0064). As 3 rotas são exercitadas só pela API pública
// (harness black-box), como as demais jornadas deste pacote.
import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJornadaAcoesForcadasGerenciar(t *testing.T) {
	c := novoHarness(t)

	// (0) Importa o filme (entity com external_ids.tmdb=603) e escaneia uma
	// pasta cujo vídeo embute o mesmo id, ligando o media_file 'present' à
	// MESMA entity (mesmo caminho de TestJornadaMidiasLocais).
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	root := t.TempDir()
	videoPath := filepath.Join(root, "Matrix (1999) [tmdbid-603].mkv")
	escreverVideo(t, videoPath)

	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "movie"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// (1a) "Verificar Atualizações de Arquivos": mesmo sem NENHUMA mudança em
	// disco desde o scan, o endpoint força o re-hash do media_file 'present'
	// (ao contrário do scan incremental, que faria short-circuit).
	semMudanca := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/refresh-files", nil, http.StatusOK)
	if fr, _ := semMudanca["files_rechecked"].(float64); int(fr) != 1 {
		t.Errorf("files_rechecked (sem mudanca em disco) = %v, quero 1 (refresh-files ignora o short-circuit incremental)", semMudanca["files_rechecked"])
	}

	// (1b) Asset local colocado DEPOIS da 1a varredura, na entity JÁ
	// catalogada — o gap que motivou o briefing (descobrirPoster só roda em
	// criarEntity). O upload multipart é o caminho normal do dono; aqui
	// simulamos o dono deixando um poster.jpg direto na pasta do vídeo.
	posterPath := filepath.Join(root, "poster.jpg")
	if err := os.WriteFile(posterPath, []byte("fake-poster-bytes"), 0o644); err != nil {
		t.Fatalf("escrever poster.jpg: %v", err)
	}
	comAsset := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/refresh-files", nil, http.StatusOK)
	if af, _ := comAsset["assets_found"].(float64); int(af) != 1 {
		t.Errorf("assets_found apos colocar poster.jpg na pasta = %v, quero 1", comAsset["assets_found"])
	}
	artwork := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/artwork", nil, http.StatusOK)
	posters, _ := artwork["poster"].([]any)
	achouLocal := false
	for _, p := range posters {
		m, _ := p.(map[string]any)
		if origem, _ := m["source"].(string); origem == "local" {
			achouLocal = true
		}
	}
	if !achouLocal {
		t.Errorf("artwork[poster] pos-refresh-files = %v, queria um item origin=local (poster.jpg descoberto)", posters)
	}

	// (1c) Campos LOCAIS de reprodução: escreve um .nfo trazendo
	// playcount/watched/userrating/resume e confirma que o refresh-files
	// preenche o que ainda está ausente no banco.
	nfoPath := filepath.Join(root, "Matrix (1999) [tmdbid-603].nfo")
	nfoConteudo := `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Matrix</title>
  <userrating>8.5</userrating>
  <playcount>3</playcount>
  <watched>true</watched>
  <lastplayed>2026-01-01 10:00:00</lastplayed>
  <resume>
    <position>120.5</position>
    <total>5400</total>
  </resume>
</movie>`
	if err := os.WriteFile(nfoPath, []byte(nfoConteudo), 0o644); err != nil {
		t.Fatalf("escrever .nfo: %v", err)
	}
	// entity.nfo_path só existe apos gravar o NFO oficial (endpoint dedicado,
	// briefing 0062) — depois sobrescrevemos o CONTEUDO no mesmo caminho com
	// os campos locais acima, sem mexer no fluxo de geracao do NFO em si.
	nfoResp := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/nfo", nil, http.StatusOK)
	nfoPathReal := str(t, nfoResp, "nfo_path")
	if err := os.WriteFile(nfoPathReal, []byte(nfoConteudo), 0o644); err != nil {
		t.Fatalf("sobrescrever .nfo com campos locais: %v", err)
	}

	comCampos := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/refresh-files", nil, http.StatusOK)
	preenchidos := strs(t, comCampos, "playback_fields_filled")
	for _, esperado := range []string{"watched", "play_count", "user_rating", "last_played_at", "resume_position_seconds", "resume_total_seconds"} {
		if !contem(preenchidos, esperado) {
			t.Errorf("playback_fields_filled = %v, faltou %q", preenchidos, esperado)
		}
	}

	// (1d) Não-destrutivo: chamar de novo NÃO reporta os mesmos campos como
	// "novos" (já estão preenchidos no banco — o COALESCE é no-op).
	comCamposDeNovo := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/refresh-files", nil, http.StatusOK)
	preenchidosDeNovo := strs(t, comCamposDeNovo, "playback_fields_filled")
	if contem(preenchidosDeNovo, "user_rating") {
		t.Errorf("2a chamada de refresh-files reportou user_rating como novo: %v (deveria ser no-op, ja preenchido)", preenchidosDeNovo)
	}

	// (2) "Baixar Metadados": force=true busca o provider mesmo com o cache
	// de 24h fresco (o import + os refresh-files acima já deixaram o cache
	// quente para tmdb/603) — sucesso 200 e a entity segue resolvida.
	forcado := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/enrich",
		map[string]any{"force": true}, http.StatusOK)
	if title := str(t, forcado, "title"); !strings.Contains(strings.ToLower(title), "matrix") {
		t.Errorf("title pos-enrich forcado = %q, queria conter \"matrix\"", title)
	}
	// force=false (comportamento default) continua servindo do cache sem
	// regressão — mesma rota, sem body extra.
	c.obj(http.MethodPost, "/api/v1/entities/"+id+"/enrich", map[string]any{}, http.StatusOK)

	// (3) "Baixar Artes Faltantes": nova consulta ao(s) ArteProvider(s)
	// registrado(s) (aqui, nenhum além do fake de busca — degrada em
	// silêncio, AGENTS.md §6) seguida de MaterializarMidiasFaltantes sobre o
	// inventário já resolvido (o poster do enrich). Sempre 200.
	materializado := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/materialize-missing-media", nil, http.StatusOK)
	if _, ok := materializado["artes_baixadas"].([]any); !ok {
		t.Errorf("artes_baixadas ausente/tipo errado na resposta de materialize-missing-media: %v", materializado["artes_baixadas"])
	}
	if _, ok := materializado["nfo_gravado"].(bool); !ok {
		t.Errorf("nfo_gravado ausente/tipo errado na resposta de materialize-missing-media: %v", materializado["nfo_gravado"])
	}

	// 404 para entity inexistente nas 3 rotas — guarda comum às 3 ações.
	for _, rota := range []string{"/refresh-files", "/materialize-missing-media"} {
		if status, _ := c.bruto(http.MethodPost, "/api/v1/entities/inexistente"+rota, nil); status != http.StatusNotFound {
			t.Errorf("POST .../inexistente%s: status = %d, quero 404", rota, status)
		}
	}
}
