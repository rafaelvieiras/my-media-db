package scanner

// Testes da CAMADA 2 (briefing 0108, ADR-0028): scanner.Manager passa a
// enfileirar em vez de rodar sua própria goroutine. Este arquivo cobre dois
// ângulos complementares:
//   - TestExecutarReportaProgressoPorArquivo: a LÓGICA de progresso (opção
//     "a" das notas de teste do briefing) -- chama Manager.Executar
//     DIRETO, bypassando a fila por completo, para uma asserção
//     determinística de que o callback é chamado uma vez POR ARQUIVO (não
//     por lote de 25).
//   - TestOrquestracaoViaFilaStartStatusEDedup: o CONTRATO de orquestração
//     (opção "b") -- sobe um execqueue.Manager de verdade, registra o
//     scanner como Runner (ConfigurarFila) e roda o worker de verdade,
//     provando que Start/Status/dedup continuam com o MESMO comportamento
//     observável de antes na superfície pública do scanner.

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// TestExecutarReportaProgressoPorArquivo prova o critério central do
// briefing 0108: o callback de progresso é chamado UMA VEZ POR ARQUIVO
// processado (current crescendo 1, 2, 3...), não uma única vez no fim do
// lote de 25 -- era isso que fazia files_seen parecer travado em scans de
// música lentos (achado real na validação em produção do briefing 0107).
func TestExecutarReportaProgressoPorArquivo(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	const nArquivos = 4
	for i := 0; i < nArquivos; i++ {
		pasta := filepath.Join(root, fmt.Sprintf("Filme %d (2020) [tmdbid-%d]", i, i))
		escrever(t, filepath.Join(pasta, fmt.Sprintf("Filme %d (2020) [tmdbid-%d] - 1080p.mkv", i, i)))
	}

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	m.ConfigurarLibrary(libraryGetterFake{source: src})

	var mu sync.Mutex
	var chamadas [][2]int
	progresso := func(current, total int, _ string) {
		mu.Lock()
		defer mu.Unlock()
		chamadas = append(chamadas, [2]int{current, total})
	}

	if _, err := m.Executar(context.Background(), src.ID, TriggerManual, progresso); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// A primeira chamada é o reporte do total (0/nArquivos), logo após
	// coletarArquivos -- ver executarScan. As nArquivos chamadas seguintes
	// são uma por arquivo processado.
	if len(chamadas) != nArquivos+1 {
		t.Fatalf("progresso chamado %d vez(es), quero %d (1 do total + 1 por arquivo)", len(chamadas), nArquivos+1)
	}
	if chamadas[0] != [2]int{0, nArquivos} {
		t.Errorf("1a chamada = %v, quero [0 %d] (total conhecido, nenhum arquivo visto ainda)", chamadas[0], nArquivos)
	}
	for i := 1; i <= nArquivos; i++ {
		want := [2]int{i, nArquivos}
		if chamadas[i] != want {
			t.Errorf("chamada %d = %v, quero %v", i, chamadas[i], want)
		}
	}
}

// TestOrquestracaoViaFilaStartStatusEDedup sobe um execqueue.Manager de
// verdade (mesmo padrão de internal/execqueue/manager_test.go), liga o
// scanner nele via ConfigurarFila/ConfigurarLibrary e roda o worker de
// verdade -- prova que, por fora do pacote scanner, Start/Status continuam
// se comportando como antes: 202 com um id, 409 (ErrScanEmAndamento) em
// dedup, e Status refletindo o resultado final (running=false, sem erro,
// contagens corretas) depois que o worker processa o item.
func TestOrquestracaoViaFilaStartStatusEDedup(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Filme (2020) [tmdbid-1]")
	escrever(t, filepath.Join(pasta, "Filme (2020) [tmdbid-1] - 1080p.mkv"))

	m := managerFake(t, d)
	lib := library.New(d)
	m.ConfigurarLibrary(lib)
	fila := execqueue.NewManager(d)
	m.ConfigurarFila(fila)

	src := criarSource(t, d, root)

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		fila.Run(runCtx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	jobID, err := m.Start(src, TriggerManual)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if jobID == "" {
		t.Fatal("Start devolveu id vazio")
	}

	// Dedup: um segundo Start para a MESMA fonte, enquanto o item anterior
	// ainda está queued/running na fila, devolve o MESMO erro sentinela
	// público de sempre (ErrScanEmAndamento) -- o contrato HTTP 409 de
	// internal/server/scan.go depende disto.
	if _, err := m.Start(src, TriggerManual); err != ErrScanEmAndamento {
		t.Fatalf("2o Start err = %v, quero ErrScanEmAndamento", err)
	}

	st := aguardarScanConcluido(t, m, src.ID)
	if !st.Existe {
		t.Fatal("Status.Existe = false depois do scan concluído")
	}
	if st.Running {
		t.Fatal("Status.Running = true depois do scan concluído")
	}
	if st.LastError != "" {
		t.Fatalf("LastError = %q, quero vazio (scan bem-sucedido)", st.LastError)
	}
	if st.FilesSeen != 1 || st.FilesTotal != 1 {
		t.Fatalf("FilesSeen/FilesTotal = %d/%d, quero 1/1", st.FilesSeen, st.FilesTotal)
	}
	if st.StartedAt.IsZero() {
		t.Error("StartedAt zero depois do scan concluído")
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entities = %d, quero 1 (scan disparado via fila casou o arquivo)", n)
	}

	// StorageNaoVideo continua acessível pelo caminho novo (worker da fila),
	// sem precisar re-varrer o disco.
	if _, _, ok := m.StorageNaoVideo(src.ID); !ok {
		t.Error("StorageNaoVideo ok=false depois de um scan concluído via fila")
	}
}

// aguardarScanConcluido faz polling de Manager.Status até Existe=true e
// Running=false, ou estoura o prazo -- necessário porque o worker da fila
// roda em goroutine própria.
func aguardarScanConcluido(t *testing.T, m *Manager, sourceID string) Status {
	t.Helper()
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) {
		st := m.Status(sourceID)
		if st.Existe && !st.Running {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout esperando o scan concluir via fila")
	return Status{}
}
