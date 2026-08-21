# 0116 — Enriquecimento retroativo cobre `artista` (música)

> O passo retroativo do job `saude_biblioteca` (0056) só varre `movie`/`tv` —
> `artista` catalogado localmente (sem `external_ids`) nunca é verificado
> contra o MusicBrainz sozinho, mesmo sendo um tipo de topo (ADR-0030) tão
> elegível quanto filme/série. Este briefing generaliza o filtro para
> reaproveitar o registro único de tipos-de-topo (`tiposSubNo`, 0112) em vez
> de uma segunda lista hardcoded — assim uma vertical de topo futura herda a
> cobertura de graça, sem tocar esta query de novo.

| | |
|---|---|
| **ID** | 0116 |
| **Tipo** | feature |
| **Fase (roadmap)** | Transversal / infraestrutura operacional — extensão do job `saude_biblioteca` ([0056](0056-enriquecimento-externo-retroativo-job-saude.md)), agora abrindo pra música. |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md), [ADR-0030](../docs/adr/0030-tipos-de-topo-biblioteca-e-busca-genericas.md) |
| **Status** | done |
| **Depende de** | [0056](0056-enriquecimento-externo-retroativo-job-saude.md), [0106](0106-provider-musicbrainz-cover-art-archive.md), [0112](0112-registro-tipos-de-topo.md), [0115](0115-musica-import-local-first.md) |

---

## 1. Objetivo

`ListarElegiveisEnriquecimentoRetroativo` (`internal/db/queries/review.sql`,
0056) restringe o universo do enriquecimento retroativo em lote a
`e.type IN ('movie', 'tv')` — decisão de **escopo herdada** do briefing 0050
(música ainda nem existia), reafirmada como adiamento deliberado pelo
briefing 0107 mesmo depois de música existir no sistema:

> "Job de saúde/retroativo para música — avaliar depois de validar o scan em
> produção." (0107, §7)

Hoje, um `artista` catalogado só pelo scanner de música (nome de pasta
reconhecido, sem `external_ids` ainda) **nunca** é alcançado pelo passo
retroativo — fica "invisível" pra sempre (sem bio/foto/logo do MusicBrainz),
a não ser que o dono clique manualmente em "Baixar Metadados" por artista.
Isso viola o princípio #6 do AGENTS.md (mídia sobrevive a qualquer provider
registrado, enriquecimento por capacidade — nenhuma vertical deveria
precisar de tratamento especial pra ser alcançada por um mecanismo já
genérico).

`album`/`faixa` são **sub-nós** da árvore de música (ADR-0027/ADR-0030) —
nunca fazem sentido como candidatos de "nível de topo" desta query (não têm
`status`/fila de Revisão própria no sentido em que `ListarRevisao` assume) e
**continuam de fora**, agora e no futuro.

Há também um obstáculo técnico real (não só o filtro de tipo): a derivação
do termo de busca (`tituloAnoDoArquivo`) e a checagem de elegibilidade
(`EXISTS media_file`) pressupõem que a entity tem um **arquivo próprio**
(`naming.Parse` do nome do arquivo) — verdadeiro pra movie/tv/faixa, **falso**
pra `artista`, que por ADR-0027 nunca liga direto a `media_file` (só a faixa
liga). Este briefing resolve os dois problemas juntos.

## 2. Jornada do usuário

1. O dono aponta uma fonte de música nova; o scanner reconhece a pasta do
   artista (nome da pasta ≈ nome do artista, briefing 0106/0115) e cria a
   entity `artista` já com título legível, mas sem `external_ids` — a
   biblioteca mostra o artista sem foto, bio ou logo.
2. Às 4h, o job `saude_biblioteca` roda (mesmo agendamento de sempre). Depois
   do re-scan, do auto-match em lote (0054) e do passo retroativo movie/tv já
   existente (0056), o mesmo passo agora **também** considera artistas
   catalogados sem `external_ids` e com ao menos uma faixa possuída na árvore.
3. Busca local-first e depois no MusicBrainz (mesmo mecanismo do 0056, sem
   nenhuma UI nova). Achou com confiança alta → aplica sozinho (liga a um
   artista local existente, ou importa/enriquece via candidato externo) — o
   artista ganha foto/bio na hora, sem o dono perceber.
4. Achou algo plausível mas não confiável (dois artistas homônimos) → o
   artista aparece na fila de Revisão, como qualquer outro item de baixa
   confiança — o dono resolve manualmente como já faz para filme/série.
5. Álbuns e faixas continuam de fora deste passo (não fazem sentido como
   "nó de topo" a verificar sozinho) — cobertos por outros mecanismos já
   existentes (import ao catalogar o artista, botão manual "Baixar
   Metadados" por item, ambos já funcionando).

## 3. Escopo

**Dentro:**
- Generalizar `ListarElegiveisEnriquecimentoRetroativo`: trocar
  `e.type IN ('movie', 'tv')` por `e.type NOT IN (sqlc.slice('sub_nos'))`,
  reaproveitando o registro único `tiposSubNo` (ADR-0030/0112,
  `internal/server/entities.go`) em vez de uma segunda lista hardcoded — uma
  vertical de topo futura (ex.: videoclipe) herda a cobertura automaticamente,
  sem precisar tocar esta query de novo.
- Estender a checagem `EXISTS media_file 'present'` (hoje só direta,
  `m.entity_id = e.id`) com um `OR EXISTS` via árvore de `entity_relation`
  (mesmo padrão `WITH RECURSIVE arvore(ancestor_id, descendant_id)` já usado
  por `ListarEntitiesMusicaComFaixaPossuida`, `internal/db/queries/media_file.sql`,
  briefing 0116-anterior/owned) — necessário porque `artista` nunca tem
  `media_file` direto (ADR-0027, só a faixa liga). `movie`/`tv` continuam
  cobertos pela cláusula direta de sempre (episódio de série já liga direto
  na própria série, ADR-0021 — a cláusula nova é aditiva, nunca reduz o
  universo atual).
- Nova função `s.termoBuscaRetroativo(ctx, ent) (titulo string, ano int, ok bool, err error)`
  em `internal/server/retroativo.go`, despachada por `ent.Type`: `artista` usa
  o próprio `ent.Title` já catalogado (sem arquivo, sem ano — ano=0 já
  degrada sem bônus nem descarte em `matching.ajustarPorAno`, comportamento
  existente, só precisa ser exercitado); qualquer outro tipo reusa
  `tituloAnoDoArquivo` como hoje. `tentarEnriquecerOuSinalizar` passa a
  chamar esta função em vez de `tituloAnoDoArquivo` diretamente.
- `enrichMissing` (`POST /entities/{id}/enrich-missing`, botão manual "Baixar
  informações faltando" do 0059) herda o comportamento de graça — já chama
  `tentarEnriquecerOuSinalizar`, sem mudança de contrato.
- Testes: query cobre artista elegível via árvore (faixa possuída, sem
  `external_ids`) e confirma que `album`/`faixa` continuam de fora mesmo
  possuindo `media_file`/sem `external_ids`; `termoBuscaRetroativo` devolve o
  título do artista sem exigir `media_file`; `EnriquecerRetroativo`
  fim-a-fim aplica/sinaliza artista via `fakeMusicaProvider`.

**Fora (explicitamente):**
- `album`/`faixa` neste passo — permanecem sub-nós (ADR-0030), nunca
  candidatos de topo aqui. Faixa já é alcançada de outra forma (import ao
  catalogar o artista, botão manual por item).
- Mudar `limiarSinalizacaoRetroativa`/`LimiarAutoMatch`/
  `limiteEnriquecimentoRetroativoPorExecucao` ou o algoritmo de pontuação —
  reaproveitados tal como estão.
- Qualquer vertical de topo além de música — a generalização via `tiposSubNo`
  já cobre isso de graça (é o ponto do briefing), sem trabalho adicional
  específico aqui.
- Override de `retroactive_enrichment_days` por biblioteca para `artista`
  (`diasReentativaRetroativa`/subquery de throttle): a subquery correlacionada
  hoje só olha `media_file` DIRETO da entity — para `artista` isso não
  encontra nenhuma linha e cai no fallback global (`dias_global`), mesmo
  comportamento de qualquer entity sem override específico. Aceitável (não é
  regressão: hoje artista nem passa pelo throttle, por estar fora do
  universo inteiro); estender o override por árvore fica para quando houver
  necessidade real de configurar isso por biblioteca de música.
- Trailer/tema musical — fora de escopo deste job (já tratado como benigno
  quando não há destino, briefing 0104).

## 4. Contrato técnico

### Dados / modelo

Nenhuma migration. Só a query sqlc muda de forma (`internal/db/queries/review.sql`):

```sql
-- name: ListarElegiveisEnriquecimentoRetroativo :many
-- Complemento do WHERE de ListarRevisao, restrito a tipos DE TOPO (ADR-0030):
-- entities SEM nenhum external_id (external_ids_json IS NULL) que NAO estao
-- na fila de Revisao (status != 'draft', sem media_file de baixa confianca)
-- E que tem ao menos um media_file 'present' -- direto (movie/tv/collection/
-- qualquer tipo com arquivo proprio) OU via arvore de entity_relation
-- (musica: artista nunca liga media_file direto, ADR-0027 -- so a faixa
-- liga; mesmo padrao WITH RECURSIVE ja usado por
-- ListarEntitiesMusicaComFaixaPossuida, media_file.sql). O type NOT IN
-- abaixo e a lista LITERAL dos mesmos valores de tiposSubNo (ADR-0030/
-- briefing 0112, internal/server/entities.go) -- sqlc.slice('sub_nos') foi
-- tentado e descartado (ver nota de implementacao do briefing 0116): sqlc,
-- no dialeto sqlite, nao combina sqlc.slice com outros sqlc.arg no MESMO
-- statement. TestListaSubNosSincronizadaComQuery (retroativo_test.go) trava
-- a lista literal contra tiposSubNo -- se um tipo novo entrar ali, o teste
-- falha ate esta query ser atualizada a mao. Throttle: nunca tentado (NULL)
-- tem prioridade, depois o mais antigo tentado primeiro; sqlc.arg('dias_global')
-- e o intervalo GLOBAL em dias (briefing 0085), usado como fallback quando
-- nenhum media_file 'present' da entity vem de uma biblioteca com override
-- (a subquery de override so olha media_file DIRETO -- para artista isso
-- sempre cai no fallback global, ver secao 3 "Fora" do briefing 0116);
-- sqlc.arg('limite') poda o lote por execucao.
WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT e.id, e.type, e.title, e.release_date, e.last_external_match_attempt_at
FROM entity e
WHERE e.external_ids_json IS NULL
  AND e.type NOT IN ('season', 'episode', 'album', 'faixa')
  AND e.status != 'draft'
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
        UNION
        SELECT 1 FROM arvore t
        JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
        WHERE t.ancestor_id = e.id
      )
  AND NOT EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id
          AND m.state = 'present'
          AND m.match_confidence > 0
          AND m.match_confidence < sqlc.arg('limiar')
      )
  AND (
        e.last_external_match_attempt_at IS NULL
        OR e.last_external_match_attempt_at < COALESCE(
            (SELECT datetime('now', '-' || MIN(COALESCE(ls.retroactive_enrichment_days, CAST(sqlc.arg('dias_global') AS INTEGER))) || ' days')
             FROM media_file mf
             JOIN library_source ls ON ls.id = mf.library_source_id
             WHERE mf.entity_id = e.id AND mf.state = 'present'),
            datetime('now', '-' || CAST(sqlc.arg('dias_global') AS INTEGER) || ' days')
        )
      )
ORDER BY
    (e.last_external_match_attempt_at IS NULL) DESC,
    e.last_external_match_attempt_at ASC,
    e.title COLLATE NOCASE ASC,
    e.id
LIMIT sqlc.arg('limite');
```

`ListarElegiveisEnriquecimentoRetroativoParams` **não** ganha campo novo —
ver nota abaixo sobre por que `sqlc.slice` foi descartado.

**Ajustes feitos na implementação (21/08/2026), divergindo do desenho inicial
deste §4:**
1. `sqlc.slice('sub_nos')` **não pôde ser usado**: sqlc, no dialeto sqlite
   deste projeto, não combina `sqlc.slice` com outros `sqlc.arg` no MESMO
   statement — a expansão dinâmica de `?` do slice desalinha a numeração dos
   placeholders nomeados (`?N`) dos demais args, causando erro de bind em
   runtime (`missing named argument`/`datatype mismatch`), mesmo depois de
   eliminar o reuso de `dias_global` (tentativa intermediária com uma
   sub-tabela `cfg` cross-joined, também descartada). Resolvido com a lista
   LITERAL (`'season', 'episode', 'album', 'faixa'`), guardada por um teste
   novo (`TestListaSubNosSincronizadaComQuery`) que compara essa lista contra
   `tiposSubNo` em Go — se divergirem no futuro, o teste falha e sinaliza que
   a query precisa ser atualizada à mão. Não é o ideal (ainda é uma 2ª lista,
   só que agora com um teste-trava em vez de auto-sincronizada), mas é seguro
   dado o limite real do sqlc encontrado.
2. `OR EXISTS(...)` combinando duas cláusulas `EXISTS` como predicado direto
   também não parseou (mesmo sem slice) — resolvido reescrevendo como um
   único `EXISTS (SELECT ... UNION SELECT ...)`, equivalente (verdadeiro se
   qualquer um dos dois lados tiver linha), mais simples de parsear.
3. **Consequência aceita**: como a lista virou "excluir sub-nós" em vez de
   "incluir só movie/tv", `collection` (tipo de topo existente, não é
   sub-nó) passa a ser elegível também, quando tiver `media_file` próprio —
   não era o objetivo direto deste briefing, mas é a consequência correta e
   esperada de tratar "de topo" uniformemente (a restrição antiga "só
   movie/tv" era escopo herdado do 0050, não regra de negócio permanente).
   `TestListarElegiveisEnriquecimentoRetroativoRestricoesDeEscopo` foi
   atualizado para refletir isso.

### API

Nenhuma mudança de contrato. `POST /entities/{id}/enrich-missing` (0059)
continua com o mesmo shape de resposta — passa a poder devolver
`outcome:"applied"/"flagged"` também para `artista` sem exigir nada novo do
chamador.

### Backend (Go)

- **`internal/server/retroativo.go`**:
  - Nova função `termoBuscaRetroativo(ctx, ent sqlcgen.Entity) (titulo string, ano int, ok bool, err error)`:
    `if ent.Type == tipoEntArtista { return ent.Title, 0, ent.Title != "", nil }`;
    senão, `return s.tituloAnoDoArquivo(ctx, ent.ID)` (comportamento atual,
    inalterado).
  - `tentarEnriquecerOuSinalizar`: troca a chamada direta a
    `s.tituloAnoDoArquivo(ctx, ent.ID)` por `s.termoBuscaRetroativo(ctx, ent)`.
  - `EnriquecerRetroativo`: chamada a `ListarElegiveisEnriquecimentoRetroativo`
    sem mudança de assinatura (a lista de tipos passou a ser literal na
    query, não um parâmetro Go — ver nota de implementação acima).
- Nenhuma mudança em `internal/job/libraryhealth` (interface `Matcher`
  inalterada — só o comportamento por trás de `EnriquecerRetroativo` muda).
- Nenhuma mudança em `internal/server/automatch.go` (`melhorCandidatoLocal`/
  `melhorCandidatoExterno`/`buscarLocal`/`buscarExterno` já são agnósticos de
  tipo, confirmado por leitura de código — reusados sem alteração).

### Frontend (Vite/React)

Nenhuma mudança — `/jobs` e `/review` já são genéricos (briefing 0056).

### Arquivos afetados

- `internal/db/queries/review.sql` (query editada).
- `internal/db/sqlcgen/review.sql.go` (gerado via `sqlc generate`).
- `internal/server/retroativo.go` (nova função `termoBuscaRetroativo`,
  ajuste em `tentarEnriquecerOuSinalizar`/`EnriquecerRetroativo`).
- `internal/server/retroativo_test.go` (testes novos + ajuste do teste de
  escopo pré-existente, que assumia a restrição antiga a movie/tv).

## 5. Critérios de aceite

- [ ] Um `artista` catalogado (`status` != `draft`), sem `external_ids_json`,
  com uma faixa descendente `present` na árvore, é elegível a
  `ListarElegiveisEnriquecimentoRetroativo` — provado por teste de query.
- [ ] Um `album`/`faixa` nas mesmas condições (sem `external_ids`, com
  `media_file` possuído) **nunca** aparece nessa listagem, mesmo depois desta
  mudança (regressão anti-vazamento de sub-nó, ADR-0030).
- [ ] `movie`/`tv` elegíveis antes desta mudança continuam elegíveis
  (nenhuma regressão no universo já coberto pelo 0056).
- [ ] `termoBuscaRetroativo` devolve `(título-do-artista, 0, true, nil)` para
  `tipoEntArtista` sem chamar `ListarMediaFilesPorEntity`/exigir arquivo
  algum; para `movie`/`tv` continua delegando a `tituloAnoDoArquivo` sem
  mudança de comportamento.
- [ ] `EnriquecerRetroativo` fim-a-fim, com `fakeMusicaProvider` (mesmo fake
  de `internal/enrich/musica_test.go`, ou equivalente local a
  `internal/server`) devolvendo candidato forte para um artista elegível,
  aplica sozinho (`fundirEntities` ou `aplicarCandidatoExterno`) e incrementa
  `aplicados`.
- [ ] Candidato de artista com score entre `limiarSinalizacaoRetroativa` (50)
  e `LimiarAutoMatch` (92) sinaliza (`match_confidence` atualizado), item
  aparece em `GET /review`.
- [ ] `TestListaSubNosSincronizadaComQuery` trava a lista literal de tipos
  excluídos contra `tiposSubNo` — falha se um divergir do outro no futuro.
- [ ] `go build/vet/test -count=1` verdes; `make gate-local` não piora
  (gocyclo de funções novas ≤ 10).

## 6. Notas de implementação / decisões

- **Por que `NOT IN (sub-nós)` em vez de adicionar `'artista'` à lista
  `IN`**: uma lista de inclusão (`IN ('movie','tv','artista')`) resolveria
  música, mas repetiria o mesmo erro que motivou o 0112 — uma vertical de
  topo futura (videoclipe, por exemplo) ficaria fora até alguém lembrar de
  editar esta query de novo. Inverter para "excluir sub-nós" (o MESMO
  conjunto de valores de `tiposSubNo`, ADR-0030) garante que todo tipo de
  topo, presente ou futuro, herda a cobertura automaticamente. O ideal seria
  esta lista vir do próprio `tiposSubNo` em runtime (via `sqlc.slice`), mas
  isso esbarrou num limite real do sqlc (ver nota de implementação do §4) —
  a lista ficou literal, com um teste (`TestListaSubNosSincronizadaComQuery`)
  travando as duas contra divergência, em vez da propagação automática
  originalmente desenhada.
- **Por que árvore via `entity_relation` e não uma coluna nova**: mesmo
  raciocínio do briefing 0116-anterior (owned de artista/album) e do
  `ListarEntitiesMusicaComFaixaPossuida` já existente — ADR-0027 é categórico
  que só a faixa liga a `media_file`; duplicar essa informação numa coluna
  desnormalizada em `artista` reintroduziria uma sincronização manual que o
  modelo genérico de `entity_relation` já resolve sem custo de schema.
  Reusar o MESMO padrão `WITH RECURSIVE` já testado em produção (0116
  anterior, materialização de capa/foto) em vez de inventar uma variação.
- **Por que `termoBuscaRetroativo` e não estender `tituloAnoDoArquivo`**: a
  função atual é fundamentalmente "derive de um arquivo no disco" — artista
  não tem arquivo próprio, então não é uma variação da mesma função, é uma
  fonte de termo de busca INTEIRAMENTE diferente (o título já catalogado).
  Uma função de despacho fina, no mesmo espírito de `montarGruposArtwork`/
  outras dispatch-by-type já existentes no pacote, deixa a intenção explícita
  sem forçar `tituloAnoDoArquivo` a saber sobre música.
- **Ano=0 para artista não é um caso especial a tratar**: `ajustarPorAno`
  (`internal/matching/matching.go:70`) já devolve `(scoreBase, false)` sem
  ajuste nem descarte quando qualquer um dos dois anos é `<= 0` — comportamento
  existente, coberto por `TestPontuar_AnoAusenteNaoAjustaNemDescarta`; este
  briefing só passa a EXERCITAR esse caminho pela primeira vez com dado real
  de música, não precisa de mudança na função de pontuação.

## 7. Fora deste briefing / follow-ups

- **Override de `retroactive_enrichment_days` por biblioteca aplicável a
  `artista` via árvore** — hoje cai no fallback global; refinar quando houver
  necessidade real de configurar throttle diferenciado por biblioteca de
  música.
- **`album`/`faixa` como candidatos de enriquecimento retroativo próprio** —
  deliberadamente fora (ADR-0030, sub-nós); se surgir necessidade real, é
  briefing novo, não extensão deste.
- **Vertical de topo além de música** (videoclipe etc.) — um tipo de topo
  NOVO já nasce elegível automaticamente (não entra na lista de exclusão a
  menos que seja um SUB-nó, mesmo caso de `season`/`episode`/`album`/`faixa`
  hoje) — `TestListaSubNosSincronizadaComQuery` só pega quando `tiposSubNo`
  ganha um tipo novo que a lista literal desta query não reflete ainda. A
  busca de termo (`termoBuscaRetroativo`) ainda precisaria de um `case` novo
  se a vertical não tiver arquivo próprio como movie/tv, mesmo padrão deste
  briefing.
