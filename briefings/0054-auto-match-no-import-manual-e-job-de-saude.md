# 0054 — Fechar o gap do auto-match: import manual e job de saúde

> O [0050](0050-auto-match-fila-de-revisao.md) construiu o motor de auto-match,
> mas ele só é alcançável por clique manual na fila de Revisão. Este briefing
> conecta o mesmo motor aos dois pontos onde a duplicata realmente nasce: o
> import manual via busca (`POST /import`) e o job de saúde da biblioteca
> ([0053](0053-job-saude-biblioteca.md)).

| | |
|---|---|
| **ID** | 0054 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (extensão direta do [0050](0050-auto-match-fila-de-revisao.md)) |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Status** | draft |
| **Depende de** | [0050](0050-auto-match-fila-de-revisao.md), [0051](0051-fundir-duplicata-conflito-external-id.md), [0053](0053-job-saude-biblioteca.md), [0026](0026-centralizacao-interno-first.md) |

---

## 1. Objetivo

O scanner adiciona arquivo sem metadados como uma entity `draft` (comportamento
esperado, [0009](0009-scanner-fswalk-ffprobe-matching.md)). O dono então busca
o título no provedor externo (busca interna+externa combinada) e clica em
**Importar**. Hoje isso **sempre cria uma entity nova** — `internal/enrich/enrich.go:147`
(`importarItemBase`) só faz find-or-create por `external_id` já gravado
(`acharPorIDExterno`), e o draft recém-escaneado nunca tem um. Resultado
reproduzido: "O Foragido" (1998) vira **duas** entities — o draft com o
arquivo, sem metadados, e a importada com metadados, sem arquivo (uma órfã).

O motor que resolveria isso sozinho **já existe**: `tentarAutoMatch`
(`internal/server/automatch.go:172`, briefing 0050) deriva título/ano, busca
local-first, pontua por similaridade (Jaro-Winkler + ajuste de ano,
`internal/textsim`) e funde automaticamente quando o score bate
`LimiarAutoMatch = 92.0`. Mas ele só é chamado a partir de dois botões
manuais na fila de Revisão (`POST /entities/{id}/auto-match`,
`POST /review/auto-match`) — nunca a partir do import, nem do job de saúde
([0053](0053-job-saude-biblioteca.md)), que hoje só **relata** contagens sem
agir (explícito em seu §3 "Fora").

Este briefing fecha as duas pontas:

1. **Import manual**: antes de criar uma entity nova, tenta casar por nome
   contra drafts locais sem `external_id`. Alta confiança → enriquece o draft
   já existente **no lugar** de criar uma segunda entity (o dono nem percebe
   — o item já aparece enriquecido). Sem candidato forte, o comportamento
   atual é preservado (cria normalmente) — o item criado agora É uma órfã
   "normal" e o draft continua na fila de Revisão, exatamente como hoje.
2. **Job de saúde**: depois de re-escanear e contar revisão/órfãs, roda o
   auto-match em lote (reaproveitando a MESMA lógica de `POST
   /review/auto-match`) sobre os itens elegíveis — inclusive os que ficaram
   órfãos por causa do caso 1 acontecer antes deste briefing, ou por qualquer
   outro motivo (scan que achou o arquivo depois do import, etc.).

Isso aproxima o comportamento do TinyMediaManager (inspiração declarada do
projeto): lá o matching automático acontece em **todo ponto de ingestão**
(scan e scrape manual), com confirmação manual reservada só para os casos
realmente ambíguos — não é uma ferramenta à parte que o dono precisa lembrar
de acionar.

## 2. Jornada do usuário

**Import manual (caso feliz, resolve sozinho):**
1. O dono já tem "O Foragido" na biblioteca como draft (arquivo escaneado,
   sem metadados, visível na fila de Revisão).
2. Ele busca "O Foragido" na tela de busca (interna+externa) e clica em
   **Importar** no resultado do TMDB.
3. O sistema acha o draft local por nome/ano com score ≥ 92 e enriquece
   **essa mesma entity** — o arquivo já tinha, agora ganha pôster/sinopse/etc.
   Nenhuma entity nova aparece. O item some da fila de Revisão (porque ganhou
   metadados) automaticamente, como qualquer enriquecimento normal.

**Import manual (caso incerto, comportamento inalterado):**
1. Mesma busca, mas o título/ano não bate com confiança suficiente contra
   nenhum draft local (ex.: dois filmes homônimos de anos bem diferentes).
2. O sistema importa normalmente (cria entity nova) — como hoje. A entity
   nova fica órfã (sem arquivo) e o draft continua na fila de Revisão. O dono
   resolve manualmente mais tarde (correção na fila, ou o botão
   "Auto-sincronizar" já existente do 0050) — nada regride aqui.

**Job de saúde (rede de segurança periódica):**
1. Às 4h, o job `saude_biblioteca` re-escaneia todas as fontes (como hoje).
2. Em vez de só contar revisão/órfãs, ele agora também roda o auto-match em
   lote sobre os itens elegíveis da fila de Revisão.
3. O card na tela de Jobs mostra, além do resumo de fontes: *"5 em revisão (2
   resolvidos automaticamente), 2 órfã(s)"* — o dono acorda e a duplicata do
   dia anterior (a órfã "O Foragido" + o draft) já foi fundida sozinha, sem
   ele precisar abrir a fila.

## 3. Escopo

**Dentro:**
- Novo pacote leaf `internal/matching`: `LimiarAutoMatch` e a função de
  pontuação (Jaro-Winkler + ajuste de ano) hoje privadas em
  `internal/server/automatch.go`, extraídas para serem consumidas também por
  `internal/enrich` (que não pode depender de `internal/server` — ciclo).
  `internal/server/automatch.go` passa a chamar o pacote extraído em vez de
  manter sua própria cópia — mesmo comportamento, zero duplicação.
- `internal/enrich.importarItemBase`: quando `acharPorIDExterno` não encontra
  nada, busca candidatos locais **sem `external_ids`** (drafts puros) do
  mesmo `type`, pontua com `internal/matching`, e se algum bate o limiar,
  enriquece esse draft (`Enriquecer`) em vez de criar uma entity nova.
- `internal/server`: extrai o laço de aplicação em lote de `autoMatchReview`
  (`internal/server/automatch.go:106`) para um método reusável que devolve
  contagem (resolvidos/pendentes), exposto por uma pequena interface que o
  job de saúde consome — sem `internal/job/libraryhealth` importar
  `internal/server` (o `Handler` já é construído DE DENTRO do server,
  mesmo padrão do `WithLibraryHealthJob` do 0053).
- `internal/job/libraryhealth.Handler`: novo campo `Matcher` (interface),
  chamado depois do re-scan e da contagem — roda o auto-match em lote sobre
  os elegíveis da fila de Revisão e inclui o resultado na mensagem final.
- Testes cobrindo: import que enriquece draft existente em vez de duplicar;
  import que não acha candidato preserva o comportamento atual; job que
  aciona o auto-match em lote e reflete o resultado na mensagem.

**Fora (explicitamente):**
- Mudar `LimiarAutoMatch` ou o algoritmo de pontuação (0050 já fixou isso;
  aqui só se reaproveita).
- Badge "já na biblioteca" na tela de busca para itens sem `external_id`
  ainda (levantado na investigação, mas exige lookup textual no caminho de
  leitura da busca — custo de perf a avaliar à parte). Fica como follow-up.
- Auto-match dentro da árvore de série (temporada/episódio) — mesmo fora do
  0050, continua fora aqui.
- Qualquer purga automática — o job continua só relatando/resolvendo
  matches de alta confiança; nunca apaga nada por conta própria fora do que
  `fundirEntities`/`Enriquecer` já fazem hoje (ADR-0012 continua intocado).
- Notificação externa do resultado do job (fora desde o 0053).

## 4. Contrato técnico

### Novo pacote `internal/matching`
- `LimiarAutoMatch = 92.0` (mesmo valor de hoje, só movido).
- `Pontuar(tituloConsulta string, anoConsulta int, tituloCandidato string, anoCandidato int) (score float64, descartado bool)`
  — corpo idêntico ao atual `pontuarCandidato` (`internal/server/automatch.go:329`),
  usando `internal/textsim.Similaridade`.
- Sem dependência de banco/HTTP — pacote puro, testável isoladamente (os
  testes de `pontuarCandidato` migram para cá).
- `internal/server/automatch.go` atualizado para chamar `matching.Pontuar`/
  `matching.LimiarAutoMatch` no lugar das versões privadas — diff mecânico,
  mesmo comportamento (critério de aceite: testes existentes do 0050 continuam
  verdes sem alteração de expectativa).

### `internal/enrich` — casar por nome antes de criar
- Novo arquivo `internal/enrich/matchlocal.go`:
  - `buscarDraftsCandidatos(ctx, titulo, tipo string) ([]candidatoDraft, error)`
    — query própria (FTS5 sobre `entity_fts`, restrita a
    `external_ids_json IS NULL AND type = ?`) no pool de leitura
    (`s.database.Read`); devolve `{ID, Title string; Year *int}`. Deliberadamente
    restrito a drafts **puros** (sem NENHUM external_id ainda) — nunca casa
    contra uma entity já catalogada por outra fonte, evitando fusão indevida.
  - `anoDoPayload(source string, raw json.RawMessage) int` — via
    `prov.MapearDetalhe(raw)["release_date"]` (mesmo campo já denormalizado em
    `persistir`, `enrich.go:346`), parse dos 4 primeiros dígitos (mesma lógica
    de `anoDeReleaseDate` do automatch, mas sobre o mapa canônico em vez da
    coluna já gravada — aqui a entity ainda nem existe).
  - `tentarCasarComDraftLocal(ctx, titulo string, ano int, tipo string) (id string, achou bool, err error)`
    — busca + pontua (`matching.Pontuar`/`LimiarAutoMatch`) + devolve o melhor
    candidato acima do limiar, mesmo desenho de `melhorCandidatoLocal`
    (0050), mas sem side-effect (quem aplica é o chamador).
- `importarItemBase` (`enrich.go:147`): depois de `acharPorIDExterno` não
  achar nada e ANTES do bloco de criação, busca `raw`/`titulo` (já eram
  buscados nessa ordem) e chama `tentarCasarComDraftLocal`. Se achar: devolve
  `(id, false, s.Enriquecer(ctx, id, source, sourceID, tipo, langs))` — o
  draft vira a entity enriquecida, sem criar nada novo. Sem candidato: segue
  o fluxo de criação exatamente como está hoje.
- **Sem merge/`fundirEntities` neste caminho**: como a entity nova ainda não
  foi criada, "aplicar o candidato" é simplesmente enriquecer o draft
  encontrado — não há duas entities para fundir.

### `internal/server` — reuso pelo job
- Extrai o corpo do laço de `autoMatchReview` (`automatch.go:117-150`) para
  `func (s *Server) autoMatchLote(ctx context.Context, ids []string) autoMatchBatchResultDTO`
  — `autoMatchReview` (HTTP) e o novo consumidor do job chamam o mesmo método.
- Novo método `func (s *Server) AutoMatchElegiveis(ctx context.Context) (resolved, pending int, err error)`:
  resolve `ids` via `ListarRevisao` (mesma query de `GET /review`, igual ao
  que `autoMatchReview` já faz quando o corpo vem vazio) e chama
  `autoMatchLote`. Assinatura pensada para satisfazer estruturalmente a
  interface que `internal/job/libraryhealth` vai declarar (sem
  `libraryhealth` importar `internal/server`).
- `server.WithLibraryHealthJob()` (`server.go`, 0053) passa `s` como quarto
  argumento de `libraryhealth.New(...)`.

### `internal/job/libraryhealth`
- Nova interface pequena `Matcher interface { AutoMatchElegiveis(ctx context.Context) (resolved, pending int, err error) }`,
  satisfeita estruturalmente por `*server.Server` (mesmo padrão de
  `LibraryLister`/`ScanRunner`/`Contador` do 0053).
- `Handler` ganha o campo `matcher Matcher`; `New(...)` recebe o quarto
  argumento.
- `Executar`: depois de contar revisão/órfãs (como hoje), chama
  `matcher.AutoMatchElegiveis(ctx)`. Falha aqui é **best-effort** — loga e
  segue com o resumo original sem o auto-match (o job não deveria virar erro
  só porque o auto-match tropeçou; o re-scan já rodou e isso é o que importa
  reportar como sucesso/falha).
- Mensagem final ganha o resolvido: `"<N> fonte(s) escaneada(s)...; <R> em
  revisão (<M> resolvidos automaticamente), <O> órfã(s)"` — `<R>` recontado
  DEPOIS do auto-match (reflete o estado real ao final, não o de antes).

### Arquivos afetados
- Novo: `internal/matching/matching.go` (+ `matching_test.go`).
- Novo: `internal/enrich/matchlocal.go` (+ `matchlocal_test.go`).
- Alterar: `internal/server/automatch.go` (usa `internal/matching`, extrai
  `autoMatchLote`, adiciona `AutoMatchElegiveis`), `internal/server/server.go`
  (`WithLibraryHealthJob` passa `s`), `internal/enrich/enrich.go`
  (`importarItemBase`), `internal/job/libraryhealth/libraryhealth.go`
  (`Matcher`, mensagem final), respectivos `_test.go`.
- Sem migration (nenhuma mudança de schema).

## 5. Critérios de aceite

- [ ] Reproduzir o bug relatado vira teste: draft "O Foragido" (1998, arquivo
  presente, sem `external_ids`) + `POST /import` do mesmo filme no TMDB →
  UMA entity só, com arquivo E metadados; nenhuma entity órfã criada.
- [ ] Import de um item **sem** candidato local forte continua criando uma
  entity nova, exatamente como hoje (nenhuma regressão no caminho comum).
- [ ] Candidato local com ano divergindo em mais de 1 ano nunca é casado no
  import, mesmo com título idêntico (mesma regra do 0050).
- [ ] Draft que já tem QUALQUER `external_id` (de outra fonte) nunca é
  candidato do casamento por nome no import — só drafts puros.
- [ ] `internal/matching.Pontuar`/`LimiarAutoMatch` produzem exatamente os
  mesmos resultados que a versão anterior privada de
  `internal/server/automatch.go` (testes migrados, mesmas expectativas).
- [ ] `POST /review/auto-match` e `POST /entities/{id}/auto-match` continuam
  funcionando sem mudança de contrato (mesma resposta, mesmo comportamento)
  depois da extração de `autoMatchLote`.
- [ ] Job `saude_biblioteca`: rodar com itens elegíveis na fila resolve os de
  alta confiança e a mensagem final reflete `<M> resolvidos automaticamente`
  e a contagem de revisão RECONTADA (pós auto-match, não a de antes).
- [ ] Falha do auto-match dentro do job não derruba o job — o resumo de
  scan/contagem original ainda é reportado.
- [ ] `go build/vet/test -count=1` verdes; `tsc -b` do web verde (nenhuma
  mudança de frontend prevista — nenhum contrato de API muda); catraca local
  não piora.

## 6. Notas de implementação / decisões

- **Por que enriquecer o draft em vez de criar+fundir no import**: no
  caminho do import, a entity nova ainda **não existe** no momento da
  decisão — diferente do auto-match da fila (0050), onde o item incerto JÁ é
  uma entity e por isso `fundirEntities` faz sentido (duas entities reais
  para reconciliar). Aqui basta redirecionar o enrich para o id do draft
  encontrado. Mais simples, sem side-effects de merge (mover
  `media_file_entity`/`asset`/etc.) para reverter se algo der errado no meio.
- **Restrição a drafts SEM NENHUM `external_id`** (não só sem o da fonte
  sendo importada): matching por nome contra uma entity que já tem
  identidade de OUTRA fonte é o cenário de colisão que o
  [0051](0051-fundir-duplicata-conflito-external-id.md) trata por id, não por
  nome — misturar os dois critérios aqui aumentaria o risco de falso-positivo
  sem necessidade (o caso relatado pelo usuário é sempre draft puro, vindo do
  scanner).
- **`internal/matching` como pacote leaf**: a alternativa seria
  `internal/enrich` importar `internal/server` diretamente, mas
  `internal/server` já importa `internal/enrich` (campo `s.enrich`) — ciclo.
  Extrair só a pontuação (pura, sem I/O) é o corte mínimo que resolve sem
  reestruturar `automatch.go` inteiro.
- **Job aciona via interface, não importa `internal/server`**: mesmo padrão
  já estabelecido pelo 0053 para `LibraryLister`/`ScanRunner`/`Contador` — o
  `Handler` declara o que precisa, quem constrói (`WithLibraryHealthJob`,
  dentro de `internal/server`) sabe como satisfazer.
- **Falha do auto-match no job é best-effort**: espelha a mesma filosofia do
  0053 para falha de uma fonte individual — um pedaço ruim não deveria
  esconder o resultado do resto do job.
- **Inspiração TinyMediaManager**: o TMM aciona o scraper/matcher em TODO
  ponto de entrada (scan automático E scrape manual), com o mesmo motor de
  confiança — nunca uma ação isolada que o dono precisa lembrar de clicar.
  Este briefing é o passo que faltava para o projeto replicar esse
  comportamento, sem inventar um segundo algoritmo de matching (reaproveita
  o do 0050 nos dois novos pontos de entrada).

## 7. Fora deste briefing / follow-ups

- Badge "já na biblioteca" na busca para itens sem `external_id` (precisa de
  lookup textual barato no caminho de leitura — investigar custo antes).
- Auto-match na árvore de série (temporada/episódio individual).
- Expor no frontend (tela de Jobs) o detalhe de QUAIS itens o job resolveu
  automaticamente (hoje só o resumo agregado) — se o volume real justificar.
