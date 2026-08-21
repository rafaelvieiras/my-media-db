// Package libraryhealth implementa o Handler do job `saude_biblioteca`
// (briefing 0053, follow-up explicito do 0052): a fila de revisao (/review) e
// a de orfas (/orphans) sao QUERY AO VIVO sobre entity/media_file (briefing
// 0018) -- so refletem a realidade do disco depois que ALGUEM roda um scan
// manual daquela library_source. Se um arquivo some do disco sem ninguem
// re-escanear, a entity continua 'present' para sempre. Este job fecha essa
// lacuna: dispara um re-scan de TODA fonte cadastrada, espera cada um
// terminar e resume o estado das filas de curadoria ao final -- prova de
// extensibilidade do sistema de jobs (0052 §5): so um novo Handler + uma
// linha de seed, sem tocar o schema/endpoints de job.
package libraryhealth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// nomeJob e o Handler.Nome() -- casa com o slug seedado em job_definition
// pela migration 0012.
const nomeJob = "saude_biblioteca"

// LibraryLister e o subconjunto de library.Service que o Handler consome:
// listar as fontes cadastradas. Interface para injetar um fake em teste sem
// depender de filesystem real.
type LibraryLister interface {
	Listar(ctx context.Context) ([]library.Source, error)
}

// ScanRunner e o subconjunto de scanner.Manager que o Handler consome: rodar
// o pipeline de scan de UMA fonte, SÍNCRONA E DIRETAMENTE, sem passar pela
// fila unificada (internal/execqueue).
//
// Desde o briefing 0108/ADR-0028 (CAMADA 3): job.Manager também roda sobre o
// MESMO worker sequencial (concorrência 1) que o scanner -- se este Handler
// (que já está executando DENTRO desse worker, chamado por
// job.Manager.executarViaRunner) enfileirasse um scan e esperasse ele
// terminar (o padrão antigo, Start()+polling de Status()), o worker
// deadlockaria contra si mesmo: o único worker estaria ocupado rodando ESTE
// job, então nunca chegaria a pegar o item de scan que o próprio job
// acabou de enfileirar (achado real ao validar esta camada -- os testes de
// storage_snapshot_test.go/TestWithLibraryHealthJobWiring travavam até o
// timeout do teste). Executar (o MESMO método que scanner.Manager expõe
// como execqueue.Runner) roda o pipeline de scan INLINE, no mesmo goroutine
// deste Handler, sem tocar a fila -- exatamente como o worker faria, só que
// chamado diretamente em vez de agendado.
type ScanRunner interface {
	Executar(ctx context.Context, refID, trigger string, progresso execqueue.ProgressoFunc) (mensagem string, err error)
	// StorageNaoVideo devolve os bytes de arquivo NÃO-vídeo classificados
	// como managed/unmanaged (briefing 0063) na última varredura completa da
	// fonte -- ok=false se nunca houve scan. Calculado DENTRO da mesma
	// passada de WalkDir que Executar já dispara (scanner.coletarArquivos);
	// este pacote não varre o disco de novo, só lê o resultado em memória.
	StorageNaoVideo(sourceID string) (managedBytes, unmanagedBytes int64, ok bool)
}

// Contador e o subconjunto de curation.Servico que o Handler consome: as
// contagens das filas de curadoria (briefing 0018), reusando as MESMAS
// queries sqlc do GET /review e GET /orphans -- este pacote nao reimplementa
// nenhum predicado SQL.
type Contador interface {
	ContarRevisao(ctx context.Context) (int, error)
	ContarOrfas(ctx context.Context) (int, error)
}

// Matcher e o subconjunto de *server.Server que o Handler consome para rodar
// o auto-match em lote sobre os itens elegiveis da fila de Revisao (briefing
// 0054, follow-up do 0050/0053): resolve os de alta confianca sozinho, sem
// precisar de clique manual. Satisfeita estruturalmente por *server.Server
// (mesmo padrao de LibraryLister/ScanRunner/Contador) -- este pacote nunca
// importa internal/server (evita ciclo, ja que server.Handler e construido
// DE DENTRO do server, ver server.WithLibraryHealthJob).
type Matcher interface {
	AutoMatchElegiveis(ctx context.Context) (resolved, pending int, err error)
	// EnriquecerRetroativo varre o complemento de ListarRevisao restrito a
	// itens locais puros (sem external_ids, nunca verificados contra
	// provider nenhum) e tenta enriquecer um lote pequeno, throttled por
	// item (briefing 0056, follow-up do 0053/0054): aplica sozinho com alta
	// confiança, ou sinaliza para a fila de Revisão quando achou algo
	// plausível mas não confiável o bastante.
	EnriquecerRetroativo(ctx context.Context) (aplicados, sinalizados int, err error)
	// ReprocessarArtesFaltantes varre entities JÁ identificadas (com
	// external_ids) cujo inventário de arte (fanart.tv etc.) nunca foi
	// verificado -- e tenta de novo, em lote pequeno, throttled por item
	// (briefing 0118, complemento do 0056/0117): fecha o gap de uma entity
	// que ganhou a identidade ANTES de um ArteProvider existir/ser
	// configurado no sistema.
	ReprocessarArtesFaltantes(ctx context.Context) (processados int, err error)
	// DescobrirArteLocalMusica varre artista/album de música com faixa
	// possuída em busca de um pôster local (poster.jpg/folder.jpg) na pasta
	// derivada dos arquivos das faixas -- mesmo mecanismo que filme/série já
	// tinham (briefing 0119, complemento do 0117/0118): a imagem que o dono
	// já tem na pasta passa a valer como fonte de arte, junto de MusicBrainz/
	// fanart.tv.
	DescobrirArteLocalMusica(ctx context.Context) (encontrados int, err error)
}

// CompletenessRecalculator e o subconjunto de *server.Server que o Handler
// consome para recalcular o CACHE de completude de catalogacao (briefing
// 0058, follow-up do 0053/0057): reavalia TODA entity nao-draft contra a
// regua configurada em /settings e grava entity_catalog_completeness, que a
// listagem/`/members` leem (a listagem NAO calcula ao vivo -- ver a nota
// longa em internal/server/completeness.go/completeness_recalc.go sobre por
// que). Interface pequena, mesmo padrao de Matcher/Contador: satisfeita
// estruturalmente por *server.Server, sem este pacote importar
// internal/server (evita ciclo, ja que server.Handler e construido DE DENTRO
// do server, ver server.WithLibraryHealthJob).
type CompletenessRecalculator interface {
	RecalcularCompletudeCatalogacao(ctx context.Context) (avaliados, incompletos int, err error)
}

// resultadoStorage e o agregado managed/unmanaged de arquivo NÃO-vídeo
// (briefing 0063) da ÚLTIMA execução do job que rodou pelo menos uma
// fonte -- espelha o mesmo padrão de "resposta calculada sob demanda, sem
// cache de longa duração no banco" do briefing 0055 (Statfs): fica só em
// memória, recalculado a cada rodada do job saude_biblioteca.
type resultadoStorage struct {
	managedBytes   int64
	unmanagedBytes int64
	computadoEm    time.Time
}

// Handler e o Handler do job saude_biblioteca.
type Handler struct {
	library      LibraryLister
	scanner      ScanRunner
	contador     Contador
	matcher      Matcher
	completeness CompletenessRecalculator

	muStorage sync.Mutex
	storage   resultadoStorage
	storageOK bool
}

// New cria o Handler a partir do servico de datasources, do gerenciador de
// scan, do contador das filas de curadoria, do Matcher de auto-match e do
// CompletenessRecalculator (briefing 0058) -- injetados pelo chamador
// (server.WithLibraryHealthJob), mesmo padrao de enrich.Servico/
// scanner.Manager serem construidos com suas dependencias na hora do
// registro. `completeness` e OPCIONAL: nil degrada para "nao recalcula"
// (mesmo padrao de matcher == nil em rodarAutoMatch/
// rodarEnriquecimentoRetroativo), util para testes que nao precisam
// exercitar este passo.
func New(lib LibraryLister, scn ScanRunner, cont Contador, matcher Matcher, completeness CompletenessRecalculator) *Handler {
	return &Handler{library: lib, scanner: scn, contador: cont, matcher: matcher, completeness: completeness}
}

// Nome devolve o slug estavel do job.
func (h *Handler) Nome() string { return nomeJob }

// Executar re-escaneia TODA library_source cadastrada (sequencialmente,
// aguardando cada scan terminar antes de seguir para o proximo -- nao ha
// config de paralelismo, ver notas do briefing 0053) e resume o resultado:
// quantas fontes escaneadas com sucesso, quantas falharam, quantos itens em
// revisao e quantas orfas ao final. cfg nao e usado (sem parametros hoje).
//
// Erro de UMA fonte nao aborta as outras (mesmo espirito best-effort do
// RunScheduler, briefing 0052); erro e devolvido apenas se TODAS as fontes
// falharem.
func (h *Handler) Executar(ctx context.Context, _ json.RawMessage) (string, error) {
	sources, err := h.library.Listar(ctx)
	if err != nil {
		return "", fmt.Errorf("listar fontes da biblioteca: %w", err)
	}

	escaneadas, falhas := h.escanearTodas(ctx, sources)
	h.atualizarStorage(sources)
	if len(sources) > 0 && escaneadas == 0 {
		return "", fmt.Errorf("todas as %d fonte(s) falharam ao escanear: %s",
			len(sources), strings.Join(falhas, "; "))
	}

	revisao, err := h.contador.ContarRevisao(ctx)
	if err != nil {
		return montarMensagemSemContagem(len(sources), escaneadas, falhas, err), nil
	}
	orfas, err := h.contador.ContarOrfas(ctx)
	if err != nil {
		return montarMensagemSemContagem(len(sources), escaneadas, falhas, err), nil
	}

	resolvidos := h.rodarAutoMatch(ctx)
	if resolvidos > 0 {
		// Recontagem: o auto-match pode ter tirado itens da fila -- reflete o
		// estado FINAL, nao o de antes de rodar. Erro aqui degrada para a
		// contagem ja obtida acima (best-effort, nao esconde o resto do resumo).
		if recontado, err := h.contador.ContarRevisao(ctx); err == nil {
			revisao = recontado
		}
	}

	aplicados, sinalizados := h.rodarEnriquecimentoRetroativo(ctx)
	if sinalizados > 0 {
		// Recontagem de novo (briefing 0056): itens sinalizados por este
		// passo passam a aparecer na fila de Revisao -- o resumo final
		// precisa refletir o estado depois DESTE passo tambem, nao so do
		// auto-match acima.
		if recontado, err := h.contador.ContarRevisao(ctx); err == nil {
			revisao = recontado
		}
	}

	artesReprocessadas := h.rodarReprocessamentoArtes(ctx)
	arteLocalMusica := h.rodarDescobertaArteLocalMusica(ctx)

	avaliados, incompletos := h.rodarRecalculoCompletude(ctx)

	return montarMensagem(len(sources), escaneadas, falhas, revisao, orfas, resolvidos, aplicados, sinalizados, artesReprocessadas, arteLocalMusica, avaliados, incompletos), nil
}

// rodarAutoMatch chama o Matcher (auto-match em lote sobre os elegiveis da
// fila de Revisao, briefing 0054) e devolve quantos itens foram resolvidos
// automaticamente. Best-effort: uma falha aqui NAO aborta o job -- so loga e
// devolve 0, para o resumo original de scan/contagem ainda sair.
func (h *Handler) rodarAutoMatch(ctx context.Context) int {
	if h.matcher == nil {
		return 0
	}
	resolved, _, err := h.matcher.AutoMatchElegiveis(ctx)
	if err != nil {
		log.Printf("saude_biblioteca: auto-match em lote falhou (best-effort, ignorado): %v", err)
		return 0
	}
	return resolved
}

// rodarEnriquecimentoRetroativo chama o Matcher (enriquecimento externo
// retroativo, briefing 0056) e devolve quantos itens foram aplicados
// sozinhos e quantos foram sinalizados para a fila de Revisao. Best-effort:
// uma falha aqui NAO aborta o job -- so loga e devolve (0, 0), para o resumo
// original ainda sair. Chamado DEPOIS de rodarAutoMatch (mesma ordem do
// briefing: re-scan -> auto-match em lote -> enriquecimento retroativo).
func (h *Handler) rodarEnriquecimentoRetroativo(ctx context.Context) (aplicados, sinalizados int) {
	if h.matcher == nil {
		return 0, 0
	}
	aplicados, sinalizados, err := h.matcher.EnriquecerRetroativo(ctx)
	if err != nil {
		log.Printf("saude_biblioteca: enriquecimento retroativo em lote falhou (best-effort, ignorado): %v", err)
		return 0, 0
	}
	return aplicados, sinalizados
}

// rodarReprocessamentoArtes chama o Matcher (reprocessamento de artes
// faltantes, briefing 0118) e devolve quantos itens foram verificados de
// novo. Best-effort: uma falha aqui NAO aborta o job -- so loga e devolve 0,
// para o resumo original ainda sair. Chamado DEPOIS de
// rodarEnriquecimentoRetroativo (mesma familia de passo best-effort de
// enriquecimento) e ANTES do recalculo de completude.
func (h *Handler) rodarReprocessamentoArtes(ctx context.Context) int {
	if h.matcher == nil {
		return 0
	}
	processados, err := h.matcher.ReprocessarArtesFaltantes(ctx)
	if err != nil {
		log.Printf("saude_biblioteca: reprocessamento de artes em lote falhou (best-effort, ignorado): %v", err)
		return 0
	}
	return processados
}

// rodarDescobertaArteLocalMusica chama o Matcher (descoberta de arte local de
// música, briefing 0119) e devolve quantas entities ganharam/atualizaram um
// pôster local. Best-effort: uma falha aqui NAO aborta o job -- so loga e
// devolve 0. Chamado logo depois de rodarReprocessamentoArtes (mesma familia
// de passo de arte) e ANTES do recalculo de completude.
func (h *Handler) rodarDescobertaArteLocalMusica(ctx context.Context) int {
	if h.matcher == nil {
		return 0
	}
	encontrados, err := h.matcher.DescobrirArteLocalMusica(ctx)
	if err != nil {
		log.Printf("saude_biblioteca: descoberta de arte local de musica falhou (best-effort, ignorado): %v", err)
		return 0
	}
	return encontrados
}

// rodarRecalculoCompletude chama o CompletenessRecalculator (recalculo em
// lote do cache de completude de catalogacao, briefing 0058) e devolve
// quantos itens foram avaliados e quantos ficaram incompletos. Best-effort:
// uma falha aqui NAO aborta o job -- so loga e devolve (0, 0), para o resumo
// original ainda sair. Chamado por ULTIMO (depois do re-scan, auto-match e
// enriquecimento retroativo): a completude reflete o estado FINAL da
// execucao, apos os passos anteriores poderem ter mudado dados resolvidos.
func (h *Handler) rodarRecalculoCompletude(ctx context.Context) (avaliados, incompletos int) {
	if h.completeness == nil {
		return 0, 0
	}
	avaliados, incompletos, err := h.completeness.RecalcularCompletudeCatalogacao(ctx)
	if err != nil {
		log.Printf("saude_biblioteca: recalculo de completude de catalogacao falhou (best-effort, ignorado): %v", err)
		return 0, 0
	}
	return avaliados, incompletos
}

// atualizarStorage soma, por TODA fonte cadastrada (não só as escaneadas com
// sucesso NESTA rodada), os buckets managed/unmanaged de arquivo NÃO-vídeo
// que o scanner.Manager guarda em memória (briefing 0063), e grava o
// agregado para internal/server/health_system.go ler via
// UltimoResultadoStorage. Uma fonte offline nesta rodada simplesmente NÃO
// atualiza seu bucket no scanner.Manager (Executar falha antes de tocar o
// estado de storage) -- então o valor somado aqui automaticamente reflete a
// política "mantém o último valor calculado" do briefing 0063 §3/§6, sem
// nenhuma lógica extra de offline neste pacote (mesma política de
// UsoDisco/Statfs do briefing 0055). Best-effort: nunca falha o job.
func (h *Handler) atualizarStorage(sources []library.Source) {
	var managed, unmanaged int64
	for _, src := range sources {
		m, u, ok := h.scanner.StorageNaoVideo(src.ID)
		if !ok {
			continue
		}
		managed += m
		unmanaged += u
	}
	h.muStorage.Lock()
	h.storage = resultadoStorage{managedBytes: managed, unmanagedBytes: unmanaged, computadoEm: time.Now().UTC()}
	h.storageOK = true
	h.muStorage.Unlock()
}

// UltimoResultadoStorage devolve o agregado managed/unmanaged de arquivo
// NÃO-vídeo (briefing 0063) da ÚLTIMA execução do job saude_biblioteca --
// lido por internal/server/health_system.go para montar
// storage.managed_media_bytes (somado ao vídeo indexado)/
// storage.unmanaged_bytes. ok=false antes da primeira execução (processo
// recém-iniciado, job ainda não rodou nenhuma vez).
func (h *Handler) UltimoResultadoStorage() (managedBytes, unmanagedBytes int64, computadoEm time.Time, ok bool) {
	h.muStorage.Lock()
	defer h.muStorage.Unlock()
	return h.storage.managedBytes, h.storage.unmanagedBytes, h.storage.computadoEm, h.storageOK
}

// escanearTodas dispara e aguarda o scan de cada fonte, sequencialmente.
// Devolve quantas terminaram com sucesso e as mensagens de falha das demais
// (fonte + motivo). Best-effort: uma falha nao interrompe as seguintes.
func (h *Handler) escanearTodas(ctx context.Context, sources []library.Source) (int, []string) {
	escaneadas := 0
	var falhas []string
	for _, src := range sources {
		if err := h.escanearEAguardar(ctx, src); err != nil {
			falhas = append(falhas, fmt.Sprintf("%s: %v", src.Path, err))
			continue
		}
		escaneadas++
	}
	return escaneadas, falhas
}

// escanearEAguardar roda o scan da fonte SÍNCRONA E DIRETAMENTE (via
// ScanRunner.Executar, que bypassa a fila unificada -- ver o comentário
// longo em ScanRunner sobre por que: enfileirar+esperar aqui deadlockaria
// contra o próprio worker que está executando este job). Não há mais
// polling: Executar só devolve quando o scan de fato terminou.
func (h *Handler) escanearEAguardar(ctx context.Context, src library.Source) error {
	// TriggerSchedule é a aproximação razoável (briefing 0108): o
	// job.Handler.Executar não recebe hoje se foi disparado manual ou por
	// schedule (job.Handler{Nome, Executar} não carrega esse dado, e mudar
	// essa interface é fora do escopo desta camada) -- "schedule" é sempre
	// verdade no sentido de ser um disparo SISTÊMICO, nunca literalmente "o
	// dono clicou em escanear ESTA fonte".
	_, err := h.scanner.Executar(ctx, src.ID, scanner.TriggerSchedule, nil)
	return err
}

// montarMensagem compoe o resumo final de sucesso: quantas fontes
// escaneadas (com o detalhe das falhas parciais, se houver) + as contagens
// atuais das filas de curadoria. resolvidos (briefing 0054) e o resultado do
// auto-match em lote; 0 omite o parenteses para nao poluir a mensagem em
// operacao normal, quando nao havia nada a resolver. aplicados/sinalizados
// (briefing 0056) sao o resultado do enriquecimento retroativo; a clausula
// extra e omitida quando os dois sao zero (mesmo espirito da omissao de
// resolvidos==0 acima). artesReprocessadas (briefing 0118) e quantas entities
// JA identificadas tiveram o inventario de arte reverificado; clausula
// omitida quando zero (mesmo espirito). arteLocalMusica (briefing 0119) e
// quantas entities artista/album ganharam/atualizaram um poster local;
// clausula omitida quando zero. avaliados/incompletos (briefing 0058)
// sao o resultado do recalculo de completude de catalogacao; a clausula
// extra e omitida quando nao houve NENHUM item avaliado (h.completeness ==
// nil ou a biblioteca vazia) -- com pelo menos um avaliado, a clausula
// aparece mesmo com incompletos==0 (0 incompletos e uma informacao positiva
// relevante, diferente de aplicados/sinalizados==0 que so significa "nada a
// fazer").
func montarMensagem(total, escaneadas int, falhas []string, revisao, orfas, resolvidos, aplicados, sinalizados, artesReprocessadas, arteLocalMusica, avaliados, incompletos int) string {
	base := fmt.Sprintf("%s; %d em revisão, %d órfã(s)", resumoScans(total, escaneadas, falhas), revisao, orfas)
	if resolvidos > 0 {
		base = fmt.Sprintf("%s; %d em revisão (%d resolvidos automaticamente), %d órfã(s)",
			resumoScans(total, escaneadas, falhas), revisao, resolvidos, orfas)
	}
	if aplicados > 0 || sinalizados > 0 {
		base = fmt.Sprintf("%s; enriquecimento retroativo: %d aplicado(s), %d sinalizado(s) para revisão",
			base, aplicados, sinalizados)
	}
	if artesReprocessadas > 0 {
		base = fmt.Sprintf("%s; %d entity(ies) com inventário de arte reprocessado", base, artesReprocessadas)
	}
	if arteLocalMusica > 0 {
		base = fmt.Sprintf("%s; %d entity(ies) de música com pôster local encontrado", base, arteLocalMusica)
	}
	if avaliados > 0 {
		base = fmt.Sprintf("%s; %d itens avaliados, %d incompletos em catalogação", base, avaliados, incompletos)
	}
	return base
}

// montarMensagemSemContagem e usada quando os scans terminaram mas a
// contagem das filas falhou -- ainda assim reflete o que rodou de fato (o
// scan em si nao falhou por causa disto).
func montarMensagemSemContagem(total, escaneadas int, falhas []string, contErr error) string {
	return fmt.Sprintf("%s; erro ao contar filas de curadoria: %v", resumoScans(total, escaneadas, falhas), contErr)
}

// resumoScans descreve quantas fontes foram escaneadas com sucesso.
func resumoScans(total, escaneadas int, falhas []string) string {
	switch {
	case total == 0:
		return "nenhuma fonte de biblioteca cadastrada"
	case len(falhas) == 0:
		return fmt.Sprintf("%d fonte(s) escaneada(s) com sucesso", escaneadas)
	default:
		return fmt.Sprintf("%d de %d fonte(s) escaneada(s) com sucesso (%d falharam: %s)",
			escaneadas, total, len(falhas), strings.Join(falhas, "; "))
	}
}
