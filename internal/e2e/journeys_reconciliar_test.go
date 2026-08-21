package e2e

// journeys_reconciliar_test.go cobre o briefing 0120 (reconciliação de
// filhos órfãos música/série): uma série que ficou presa em RASCUNHO porque
// o match por nome falhou na hora do scan (arquivos presos direto na
// entity-rascunho, sem vínculo de episódio) é resolvida SOZINHA pelo job
// `saude_biblioteca`, sem o dono precisar clicar em nada -- ao contrário de
// TestJornadaCorrigirSerieRecasaArquivosPendentes (journeys_recasar_test.go),
// que prova a MESMA situação resolvida manualmente via
// POST /entities/{id}/correct.
//
// Desafio (mesmo catálogo estático do fake.Provider dos demais testes E2E):
// um nome que o fake reconhece de cara (o título exato de uma fixture, ex.
// "Springfield Tales") resolveria já no PRIMEIRO scan -- nunca viraria
// rascunho; um nome que ele NÃO reconhece (ex. "Mystery Show", usado no
// teste de recasar) nunca resolve, nem na reconciliação (mesmo catálogo).
// Para provar SUCESSO de ponta a ponta, o wrapper abaixo faz o
// fake.Provider FALHAR nas duas primeiras chamadas de Buscar(tipo="tv",
// nome=fixture): a do scan manual (esperarScan) E a do RE-SCAN interno que o
// próprio job saude_biblioteca dispara para TODA library_source cadastrada
// ANTES do passo de reconciliação (ver libraryhealth.Handler.Executar ->
// escanearTodas, que roda ANTES de rodarReconciliacaoFilhosOrfaos) -- falhar
// só a primeira vez deixaria esse RE-SCAN resolver a série pelo caminho
// normal do scan (tentarImportarSerie dentro de resolverSerieMiss), sem
// nunca exercitar ReconciliarSerieDraft/ReconciliarFilhosOrfaos de verdade.
// Só na TERCEIRA chamada em diante (a da reconciliação em si) o wrapper
// deixa passar para o fake real.

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// serieProviderFalhaDuasVezes embute *fake.Provider (herda BuscarSerieArvore/
// BuscarDetalhe/MapearDetalhe/Nome por promoção de método) e sobrescreve só
// Buscar: as duas primeiras chamadas com tipo="tv" e nome==nomeAlvo devolvem
// "sem candidatos" (simulando uma falha transitória de match); da terceira
// chamada em diante para esse nome/tipo -- e SEMPRE para qualquer outro
// nome/tipo -- cai no fake real. Ver o comentário de topo do arquivo sobre o
// motivo de precisar de 2 falhas, não 1.
type serieProviderFalhaDuasVezes struct {
	*fake.Provider
	nomeAlvo string
	chamadas int
}

func (p *serieProviderFalhaDuasVezes) Buscar(ctx context.Context, query, tipo, lang string) ([]provider.ResultadoExterno, error) {
	if tipo == "tv" && strings.EqualFold(query, p.nomeAlvo) {
		p.chamadas++
		if p.chamadas <= 2 {
			return nil, nil
		}
	}
	return p.Provider.Buscar(ctx, query, tipo, lang)
}

// esperarJob dispara POST /jobs/{id}/run (202) e faz polling em
// GET /jobs/{id}/executions?limit=1 até a execução mais recente sair de
// queued/running. Devolve o status final ("success"/"error"). Mesmo
// espírito/prazo de esperarScan (journeys_test.go).
func (c *cliente) esperarJob(jobID string) string {
	c.t.Helper()
	c.obj(http.MethodPost, "/api/v1/jobs/"+jobID+"/run", nil, http.StatusAccepted)
	prazo := time.Now().Add(30 * time.Second)
	for time.Now().Before(prazo) {
		resp := c.obj(http.MethodGet, "/api/v1/jobs/"+jobID+"/executions?limit=1", nil, http.StatusOK)
		itens, _ := resp["items"].([]any)
		if len(itens) > 0 {
			exec, _ := itens[0].(map[string]any)
			status, _ := exec["status"].(string)
			if status != "" && status != "queued" && status != "running" {
				return status
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	c.t.Fatalf("job %s nao concluiu no prazo", jobID)
	return ""
}

// TestJornadaReconciliacaoAutomaticaSerieDraft prova o briefing 0120 de
// ponta a ponta: uma pasta de série com o título EXATO da fixture do fake
// ("Springfield Tales", fake.SerieFixtureID) escaneada quando o provider
// ainda "não sabe resolver" (as 2 primeiras tentativas falham, ver o wrapper
// acima) fica presa em rascunho -- e o job `saude_biblioteca` a resolve
// sozinho, sem nenhum POST /correct.
func TestJornadaReconciliacaoAutomaticaSerieDraft(t *testing.T) {
	fakeReal := fake.New("")
	wrapper := &serieProviderFalhaDuasVezes{Provider: fakeReal, nomeAlvo: "Springfield Tales"}
	c := novoHarnessComOpcoes(t, server.WithLibraryHealthJob(), server.WithSearchProvider(wrapper))

	root := t.TempDir()
	base := filepath.Join(root, "Springfield Tales")
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E01.mkv"))
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E02.mkv"))

	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "tv", "content_type": "series"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// (1) A 1a chamada de Buscar (dentro do scan manual) falha de propósito:
	// a série cai na fila de revisão, um rascunho com os 2 arquivos presos
	// direto nela, sem vínculo de episódio (mesmo estado inicial do teste de
	// recasar).
	serieID := acharSerieEmRevisao(t, c)
	if arqs := c.arquivosDaEntity(serieID); len(arqs) != 2 {
		t.Fatalf("arquivos presos na serie rascunho = %d, quero 2 (S01E01/S01E02 sem vinculo)", len(arqs))
	}

	// (2) Dispara o job saude_biblioteca: internamente ele re-escaneia a
	// fonte cadastrada (2a chamada de Buscar, ainda falha) e, no passo de
	// reconciliação de filhos órfãos (0120, depois do re-scan/auto-match/
	// enriquecimento retroativo), tenta de novo (3a chamada, agora sucede)
	// via Manager.ReconciliarSerieDraft -- sem o dono clicar em nada.
	status := c.esperarJob("saude_biblioteca")
	if status != "success" {
		t.Fatalf("status da execucao do job saude_biblioteca = %q, quero \"success\"", status)
	}

	// (3) DIFERENTE do teste de recasar (onde /correct aplica a identidade IN
	// PLACE na mesma entity draft): ReconciliarSerieDraft importa a série via
	// ImportarSeriePorNome, que cria/acha a série pela IDENTIDADE do
	// provider -- uma entity NOVA, distinta do rascunho original -- e só
	// REPONTEIA media_file.entity_id para ela (religarArquivoSerie). O
	// rascunho original (serieID) fica órfão, sem media_file algum (não é
	// apagado -- principio 5, não-destrutivo). Para achar a série resolvida
	// sem adivinhar o id, reusa o mesmo caminho INTERNO-FIRST de sempre:
	// POST /import com a MESMA fonte/source_id é idempotente (find-or-create)
	// e devolve 200 com o id que a reconciliação já criou.
	resolvida := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": fake.SerieFixtureID, "type": "tv"}, http.StatusOK)
	serieResolvidaID := str(t, resolvida, "id")
	if serieResolvidaID == serieID {
		t.Fatalf("serie resolvida = rascunho original (%s); reconciliacao deveria ter apontado para uma entity nova", serieID)
	}

	// (3b) O rascunho original ficou sem nenhum media_file (os 2 arquivos
	// foram repontados para a série resolvida) -- órfão, não apagado.
	if arqs := c.arquivosDaEntity(serieID); len(arqs) != 0 {
		t.Fatalf("arquivos ainda presos no rascunho original = %d, quero 0 (repontados pela reconciliacao)", len(arqs))
	}

	// (3c) Mesmas asserções de TestJornadaCorrigirSerieRecasaArquivosPendentes:
	// a árvore nasceu (2 temporadas) e os 2 arquivos que já estavam presos
	// ganharam vínculo aos episódios certos pelo NÚMERO -- só que chegando lá
	// pelo job automático, não por /correct.
	temporadas := c.idsDosMembros(serieResolvidaID)
	if len(temporadas) != 2 {
		t.Fatalf("temporadas apos reconciliar = %d, quero 2 (arvore sincronizada)", len(temporadas))
	}
	epsT1 := c.idsDosMembros(temporadas[0])
	if len(epsT1) != 3 {
		t.Fatalf("episodios T1 = %d, quero 3", len(epsT1))
	}
	assertFileCount(t, c, epsT1[0], 1) // S01E01 -> ep 101, religado pela reconciliacao
	assertFileCount(t, c, epsT1[1], 1) // S01E02 -> ep 102, religado pela reconciliacao
	assertFileCount(t, c, epsT1[2], 0) // S01E03: nenhum arquivo cobria esse numero
	assertPosse(t, c, temporadas[0], 2, 3)

	// (4) Achado ao rodar esta jornada: DIFERENTE de /correct (que resolve a
	// MESMA entity in-place e por isso ela some de /review), a reconciliação
	// deixa o rascunho ORIGINAL (agora órfão, sem media_file) na fila de
	// revisão para sempre -- ListarRevisao (review.sql) lista TODO
	// status='draft' incondicionalmente, sem olhar file_count; e
	// ReconciliarSerieDraft nunca toca o status/existência da entity antiga
	// (só repontea media_file.entity_id -- não-destrutivo, princípio 5, mas
	// também não promove/apaga o rascunho vazio). O que a reconciliação
	// GARANTE -- e o que esta asserção prova -- é que NENHUMA série tv na
	// fila de revisão ainda tem arquivo preso (owned=true): o problema real
	// (arquivo grudado numa entity errada, sem vínculo de episódio) está
	// mesmo resolvido; o rascunho remanescente é só um registro vazio,
	// esperando o dono apagar/mesclar manualmente se quiser.
	fila := c.arr(http.MethodGet, "/api/v1/review", nil, http.StatusOK)
	for _, item := range fila {
		m, _ := item.(map[string]any)
		if str(t, m, "type") != "tv" {
			continue
		}
		if owned, _ := m["owned"].(bool); owned {
			t.Fatalf("serie tv na fila de revisao ainda com arquivo preso (owned=true) apos a reconciliacao automatica: %v", m)
		}
	}
}
