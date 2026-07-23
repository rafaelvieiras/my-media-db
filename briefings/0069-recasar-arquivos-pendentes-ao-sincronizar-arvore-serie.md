# 0069 — Recasar arquivos pendentes de episódio ao sincronizar a árvore da série

> O [0067](0067-arvore-de-serie-em-todo-caminho-de-identidade-externa.md) fez
> `Enriquecer` construir a árvore (temporada/episódio) em TODO caminho que dá
> identidade externa a uma série. Mas o vínculo arquivo↔episódio só é tentado
> durante um SCAN — então um arquivo escaneado ANTES da árvore existir fica
> preso na série, sem organização por episódio, até o próximo scan completo.
> Este briefing fecha essa lacuna no mesmo ponto único do 0067.

| | |
|---|---|
| **ID** | 0069 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [ADR-0021](../docs/adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md) |
| **Status** | done |
| **Depende de** | [0034](0034-scanner-de-series-arquivo-episodio.md), [0043](0043-re-enrich-atualiza-arvore-serie.md), [0054](0054-auto-match-no-import-manual-e-job-de-saude.md), [0067](0067-arvore-de-serie-em-todo-caminho-de-identidade-externa.md) |

---

## 1. Objetivo

Investigação do follow-up "auto-match não cobre a árvore de série" (registrado
no §7 do [0054](0054-auto-match-no-import-manual-e-job-de-saude.md#7-fora-deste-briefing--follow-ups))
mostrou que o problema **não** é duplicação de entity de temporada/episódio —
`acharOuCriarTemporada` (`internal/enrich/serie.go:335`) já faz find-or-create
por **número** (season_number, escopado à série-pai), sem depender de
`external_id`; não há bug análogo ao do 0054 nesse nível. E o scanner nunca
cria episódio como draft a partir do nome do arquivo — episódios só nascem via
árvore do provider.

O gap real é outro: o vínculo arquivo↔episódio (`media_file_entity`,
kind=`tv_episode`) só é tentado dentro de um **scan** (`casarEpisodios`,
`internal/scanner/serie.go:360`), contra um índice construído a partir da
árvore **já existente** no banco naquele momento (`carregarIndex`,
`serie.go:270`). Quando o scanner encontra uma série ainda não resolvida
(draft/ambígua), o arquivo é anexado diretamente à entity da série
(`upsertMediaFileSerie`, `EntityID: f.serieID`), com confiança baixa
(`confRevisaoSerie = 40`) e **sem** vínculo de episódio — porque a árvore
ainda não existe para casar contra ela.

Depois, quando o dono resolve a série — Corrigir, Confirmar candidato,
auto-match ou o job de saúde/retroativo — o [0067](0067-arvore-de-serie-em-todo-caminho-de-identidade-externa.md)
já garante que a árvore é construída **naquele momento**, em qualquer um
desses caminhos. Mas nenhum deles re-tenta o vínculo arquivo↔episódio dos
arquivos que já estavam ali, presos na série, esperando a árvore. Eles só
seriam re-processados no **próximo scan completo** daquela fonte — manual
("Escanear" na tela de Bibliotecas) ou o próximo disparo do job noturno
`saude_biblioteca` (podendo levar até ~24h, e mesmo esse job faz o passo de
scan **antes** do auto-match/retroativo na mesma execução — ver `Executar`,
`internal/job/libraryhealth/libraryhealth.go:149-194` — então uma série
resolvida DENTRO de uma execução do job só teria seus arquivos recasados na
execução **seguinte**).

Resultado prático: o dono resolve a série pela fila de Revisão, ela ganha
título/pôster/temporadas certinhas — mas abrir o detalhe mostra os arquivos
ainda soltos na raiz da série, sem separação por episódio, por tempo
indeterminado. É a mesma dor relatada no 0067 (MIB: Homens de Preto), só que
sobrevivendo à correção dele porque afeta uma camada mais fina (arquivo, não
entity/árvore).

## 2. Jornada do usuário

1. O scanner encontra uma pasta de série nova, não consegue resolvê-la com
   confiança (título ambíguo ou auto-import desligado) e cria um draft `tv`
   com os 24 arquivos anexados diretamente à série — sem temporada/episódio
   ainda (comportamento correto e inalterado, 0034).
2. O dono resolve a série pela fila de Revisão (Corrigir) ou o auto-match/job
   de saúde resolve sozinho à noite. A árvore é construída (0067): título,
   pôster, 2 temporadas, 24 episódios aparecem em `GET /entities/{id}/members`.
3. **Hoje**: os 24 arquivos continuam soltos na série — o detalhe mostra a
   árvore completa, mas nenhum episódio tem arquivo associado, até um scan
   completo rodar de novo (o dono pode nem saber que precisa disso).
4. **Depois deste briefing**: no mesmo instante em que a árvore é
   sincronizada (Corrigir, Confirmar candidato, auto-match, retroativo ou
   job), os arquivos que já estavam presos na série são recasados contra a
   árvore recém-criada — cada um vai para o episódio certo, exatamente como
   se um scan tivesse rodado depois da árvore existir. O dono não precisa
   escanear de novo nem esperar o próximo ciclo do job.
5. Arquivo cujo número não bate em nenhuma ordenação da árvore (ambíguo,
   episódio inexistente no provider) continua sem vínculo — mesmo
   comportamento de "revisão" de hoje; este briefing não força casamento
   errado, só evita a espera desnecessária quando o casamento SERIA possível.

## 3. Escopo

**Dentro:**
- Extrair de `internal/scanner/serie.go` a lógica de índice/casamento por
  número (`episodeIndex`, `preencherOrdenacoes`, `indexarEpisodio` — a parte
  que monta o mapa em memória a partir do `extension_data`, e `casarEpisodios`/
  `casarNaOrdem`/`ordensFallback`/`casarAbsoluto`/`mesmaSequencia`) para um
  novo pacote leaf `internal/tvmatch`, sem I/O — mesmo padrão do `internal/matching`
  extraído pelo [0054](0054-auto-match-no-import-manual-e-job-de-saude.md#4-contrato-tecnico):
  `internal/scanner` já depende de `internal/enrich` (não pode inverter), então
  a lógica pura de indexação/casamento vira um pacote neutro que os dois
  consomem.
- `internal/scanner/serie.go` passa a chamar `internal/tvmatch` em vez de
  manter cópia própria — diff mecânico, mesmo comportamento (critério de
  aceite: testes existentes do 0034 continuam verdes sem alteração de
  expectativa).
- Novo passo em `internal/enrich/serie.go`, chamado ao FINAL de
  `aplicarArvoreSerie` (0067, depois do loop de temporadas): para cada
  `media_file` já anexado à série (`ListarMediaFilesPorEntity`) que ainda NÃO
  tem vínculo `tv_episode` (`media_file_entity`), reparse o nome do arquivo
  (`naming.ParseEpisode`), monta o índice da árvore recém-sincronizada via
  `internal/tvmatch` (lendo `ListarRelacoesFilhas` + `extension_data` dos
  episódios, mesmo formato do `carregarIndex` do scanner) e cria o vínculo
  quando casar. Best-effort: falha aqui loga e segue, nunca derruba
  `Enriquecer` (mesma filosofia do 0067 para falha da árvore).
- Testes cobrindo: arquivo pendente é vinculado ao episódio certo assim que a
  árvore é sincronizada via `Enriquecer` (sem novo scan); arquivo sem número
  reconhecível ou sem match na árvore continua sem vínculo; arquivo que já
  tinha vínculo não é reprocessado/duplicado.

**Fora (explicitamente):**
- Mudar `matching.Pontuar`/`LimiarAutoMatch` — o casamento arquivo↔episódio é
  por número exato (mesma regra do 0034), não por similaridade de texto; não
  há "auto-match fuzzy" nesse nível.
- Reordenar os passos do job de saúde (scan → auto-match → retroativo →
  completude) — o fix atua no ponto de sincronização da árvore
  (`aplicarArvoreSerie`), não no scan, então funciona independente da ordem
  do job e de qualquer chamador (Corrigir, botão manual, job, etc.).
- Backfill em massa de séries já afetadas hoje (arquivos presos em séries que
  já têm árvore completa há tempo) — a próxima ação sobre aquela série
  (Corrigir de novo, "baixar informações faltando", ou um scan manual) resolve
  o caso pontual; um script/endpoint de reparo em lote é follow-up, mesmo
  padrão do [0067 §7](0067-arvore-de-serie-em-todo-caminho-de-identidade-externa.md#7-fora-deste-briefing--follow-ups).
- Indicador de UI para "arquivos aguardando vínculo de episódio" — mesmo
  espírito do 0067/0043 para enriquecimento parcial invisível; segue fora,
  fica como follow-up.
- Qualquer mudança em `POST /import` de série nova — nesse caminho não há
  arquivos pendentes (a série acabou de nascer), o passo novo roda mas não
  encontra nada a recasar (no-op idempotente).

## 4. Contrato técnico

### Dados / modelo
Nenhuma migration. Nenhum campo novo.

### API
Nenhuma mudança de contrato. Efeito observável em
`GET /entities/{id}/members`/arquivos: séries resolvidas fora de um scan
passam a mostrar os arquivos já organizados por episódio, sem esperar novo
scan.

### Backend (Go)

**Novo pacote `internal/tvmatch`** (leaf, sem I/O — extraído de
`internal/scanner/serie.go`):
- `type Index struct { ... }` — mesmo shape de `episodeIndex` hoje.
- `func NovoIndex() *Index`
- `func (idx *Index) PreencherOrdenacoes(ns map[string]any)` — corpo de
  `preencherOrdenacoes`.
- `func (idx *Index) IndexarEpisodio(epID string, ns map[string]any)` — corpo
  de `indexarEpisodio` (recebe o `extension_data` já extraído do namespace
  `core.tv@1`, via `tvext.Namespace`, que continua fora deste pacote).
- `func Casar(idx *Index, ep naming.EpisodeInfo) ([]string, bool)` — corpo de
  `casarEpisodios` (+ `casarNaOrdem`/`ordensFallback`/`casarAbsoluto`/
  `mesmaSequencia`, privadas do pacote).
- Testes migrados de `internal/scanner/serie_test.go` (os que cobrem
  `casarEpisodios` puro) para `internal/tvmatch`.

**`internal/scanner/serie.go`**: `carregarIndex`/`indexarTemporada`/
`indexarEpisodio` continuam fazendo a LEITURA (dependem de `sqlcgen.Queries`),
mas delegam a montagem/casamento para `internal/tvmatch.Index`. Diff
mecânico — mesmo comportamento.

**`internal/enrich/serie.go`** — novo passo em `aplicarArvoreSerie`
(`internal/enrich/serie.go:165`, 0067), chamado depois do loop de temporadas:
```go
func (s *Servico) recasarArquivosPendentes(ctx context.Context, serieID string) {
	arquivos, err := s.leitura().ListarMediaFilesPorEntity(ctx, serieID)
	if err != nil {
		log.Printf("enrich: listar arquivos da serie %s falhou (segue): %v", serieID, err)
		return
	}
	var idx *tvmatch.Index // construido sob demanda, so' se houver candidato
	for _, mf := range arquivos {
		if s.temVinculoEpisodio(ctx, mf.ID) {
			continue // ja vinculado (scan anterior ou iteracao anterior deste loop)
		}
		ep, ok := naming.ParseEpisode(semExtensao(filepath.Base(mf.Path)))
		if !ok {
			continue // nao reconhece padrao de episodio -- fica como esta
		}
		if idx == nil {
			idx = s.carregarIndexSerie(ctx, serieID) // ListarRelacoesFilhas + extension_data
		}
		ids, ok := tvmatch.Casar(idx, ep)
		if !ok {
			continue // sem match na arvore -- fica pendente, como hoje
		}
		s.ligarArquivoAosEpisodios(ctx, mf.ID, ids)
	}
}
```
Chamado ao final de `aplicarArvoreSerie`, best-effort (nunca propaga erro —
mesmo padrão do resto da função). `temVinculoEpisodio` e
`ligarArquivoAosEpisodios` usam `ListarVinculosPorArquivo`/
`CriarVinculoEntidade` (`internal/db/queries/media_file_entity.sql`, já
existentes). `carregarIndexSerie` espelha `scanner.carregarIndex`, agora
também disponível em `internal/enrich` via `internal/tvmatch`.

### Arquivos afetados
- Novo: `internal/tvmatch/tvmatch.go` (+ `tvmatch_test.go`).
- Alterar: `internal/scanner/serie.go` (usa `internal/tvmatch`),
  `internal/scanner/serie_test.go` (testes de casamento migram),
  `internal/enrich/serie.go` (`recasarArquivosPendentes` + helpers, chamado
  de `aplicarArvoreSerie`), `internal/enrich/serie_test.go`.
- Sem migration.

## 5. Critérios de aceite

- [x] Reproduzir o cenário: série draft com 2 arquivos (`S01E01`, `S01E02`)
  anexados à série sem vínculo de episódio (simula scan antes da árvore
  existir) + `Enriquecer` (via Corrigir/auto-match/qualquer caminho do 0067)
  → os dois arquivos ganham vínculo `tv_episode` correto, sem novo scan.
- [x] Arquivo cujo padrão de episódio não é reconhecido, ou cujo número não
  existe na árvore sincronizada, continua sem vínculo (nenhum casamento
  forçado/errado).
- [x] Arquivo que já tinha vínculo `tv_episode` de um scan anterior não é
  reprocessado nem duplicado (idempotente).
- [x] `POST /import` de série nova continua idêntico (nenhuma regressão nos
  testes do 0034/0043/0067 já existentes — diff mecânico na extração pro
  `internal/tvmatch`).
- [x] Falha ao listar/recasar arquivos pendentes não derruba `Enriquecer` nem
  o chamador HTTP (best-effort, mesmo padrão do 0067).
- [x] `go build/vet/test -count=1` verdes; catraca local não piora; `tsc -b`
  do web verde (nenhuma mudança de frontend prevista).

## 6. Notas de implementação / decisões

- **Por que no ponto de sync da árvore (`aplicarArvoreSerie`), não no job ou
  no scanner**: mesmo raciocínio do 0067 — um ponto único cobre TODOS os
  chamadores (Corrigir, Confirmar candidato, auto-match, retroativo, job,
  `POST /import`) sem precisar tocar cada um. Resolver reordenando o job
  (scan antes de auto-match/retroativo) só fecharia o caso do job noturno,
  deixando aberto o caso mais comum (ação manual do dono durante o dia).
- **`internal/tvmatch` como pacote leaf**: mesmo motivo do `internal/matching`
  no 0054 — `internal/scanner` já importa `internal/enrich` (não pode
  inverter), então a lógica pura de indexação/casamento (sem I/O) precisa
  viver num pacote neutro que os dois consomem, sem duplicar ~100 linhas de
  lógica de casamento por ordenação/absoluto entre os dois pacotes.
- **Sem alterar `match_confidence`**: o arquivo recasado ganha o vínculo de
  episódio, mas este briefing não recalcula `match_confidence` do
  `media_file` (fica no valor de quando foi escaneado, `confRevisaoSerie`).
  Recalcular isso exigiria decidir uma nova escala fora do escopo do scanner;
  o vínculo em si já resolve a dor relatada (organização por episódio). Se o
  valor de confiança baixo tiver efeito colateral visível (ex.: badge/filtro),
  fica como follow-up.
- **Best-effort, nunca propaga erro**: mesmo raciocínio do 0067 — no momento
  em que este passo rodaria, a árvore e a identidade já foram gravadas; uma
  falha aqui (ex.: `ListarMediaFilesPorEntity` falhou) não deveria fazer o
  `Enriquecer` inteiro (e o chamador HTTP) reportar erro sobre uma operação
  que, na prática, já mudou o estado da entity com sucesso.

## 7. Fora deste briefing / follow-ups

- **Backfill em massa** de séries já afetadas (arquivos presos numa série que
  já tem árvore completa há tempo, de antes deste briefing) — resolvido item
  a item por ora; job/endpoint de reparo em lote é follow-up se o volume
  justificar, mesmo padrão do 0067 §7.
- **Indicador de UI** para "arquivos aguardando vínculo de episódio" —
  mesmo espírito do 0043/0067 para enriquecimento parcial invisível.
- **Recalcular `match_confidence`** do arquivo depois do recasamento —
  hoje fica no valor de quando foi escaneado; ver §6.
