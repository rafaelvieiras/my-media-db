# 0051 — Fundir duplicata por conflito de external_id (import + corrigir)

> Quando o dono grava um id externo que **já pertence a outra entity** (via
> corrigir/editar identidade), o sistema deixa de criar silenciosamente duas
> entities para a mesma obra — funde a duplicata "vazia" automaticamente, ou
> avisa com clareza quando a fusão não é segura.

| | |
|---|---|
| **ID** | 0051 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Depende de** | [0015](0015-enriquecimento-import-proxy-imagem.md), [0018](0018-fila-revisao-orfas-correcao-match.md), [0027](0027-similar-search-ids-item-manual.md) |

---

## 1. Objetivo

Bug relatado ao vivo pelo dono: um arquivo escaneado virou uma entity `draft`
(sem `external_ids`) na biblioteca. O dono buscou o título manualmente no TMDB e
clicou **"Importar"** — como a entity `draft` ainda não tinha `external_ids`, o
find-or-create do import ([0015](0015-enriquecimento-import-proxy-imagem.md))
não a encontrou e **criou uma entity nova, sem arquivo**. Em seguida o dono foi
até a entity **com o arquivo** e gravou o mesmo id do TMDB que acabou de
importar (fluxo de corrigir/editar identidade, [0018](0018-fila-revisao-orfas-correcao-match.md)/
[0027](0027-similar-search-ids-item-manual.md)) — o sistema **apenas gravou o
mapa**, sem checar que aquele id já pertencia a outra entity. Resultado: **duas
entities na biblioteca para o mesmo filme**, uma com o arquivo (agora
identificada) e outra vazia (a importada por engano), ambas com o mesmo
`external_id_tmdb`.

A causa raiz é estrutural, não um typo: **nenhum dos dois pontos de escrita da
identidade externa** (`PUT /entities/{id}/external-ids` e
`POST /entities/{id}/correct`) **verifica se o id já está em uso por outra
entity**. O único lugar que faz dedup por id externo é o find-or-create do
`POST /import` (`enrich.Importar`/`acharPorIDExterno`) — que só ajuda quando o
import acontece DEPOIS da identidade já estar certa, não quando o dono corrige
a identidade de uma entity que já tinha sido criada solta.

Este briefing fecha esse buraco: ao gravar uma identidade externa que já
pertence a outra entity, o sistema **funde** a duplicata (quando seguro) em vez
de deixar as duas vivas.

## 2. Jornada do usuário (bug reproduzido)

1. Scan encontra `arquivo.mkv`, não acha id/título parseável → cria entity
   `movie` `draft`, com o `media_file` ligado. Aparece na Home/fila de Revisão.
2. Dono busca "o nome do filme" na busca manual, acha o resultado certo no
   TMDB e clica **Importar**. Como a entity `draft` não tem `external_ids`
   ainda, o import **cria uma entity nova** (sem arquivo) — o dono agora vê o
   filme **duas vezes** na Home: uma sem capa/arquivo (a importada) e uma com
   o arquivo (ainda `draft`).
3. Dono percebe, abre a entity **com o arquivo** e cola o mesmo id do TMDB no
   editor de identidade (`external_ids`) para "corrigir" o match.
4. **Hoje**: o sistema grava o id e re-enriquece a entity do arquivo — mas
   **não remove nem funde** a entity importada no passo 2. Ficam duas
   entities `movie` com `external_id_tmdb` idêntico: uma completa (arquivo +
   identidade), outra fantasma (identidade + capa, sem arquivo).
5. **Depois deste briefing**: no passo 4, o sistema detecta que aquele
   `external_id_tmdb` já pertence à entity criada no passo 2, verifica que ela
   não tem investimento do dono (sem override, sem arte custom) e a **funde**
   na entity do arquivo — a duplicata desaparece da Home automaticamente, sem
   o dono precisar caçar e apagar manualmente.

## 3. Escopo

**Dentro:**
- Checagem de colisão de identidade nos DOIS pontos de escrita que hoje não a
  fazem: `PUT /entities/{id}/external-ids` ([0027](0027-similar-search-ids-item-manual.md))
  e `POST /entities/{id}/correct` ([0018](0018-fila-revisao-orfas-correcao-match.md)).
  Cobre `tmdb`/`imdb`/`tvdb` (as únicas fontes com coluna gerada indexada hoje).
- **Fusão automática** quando a entity "perdedora" (a que já tinha o id) não
  tem investimento do dono — mesmo predicado de "órfã" do
  [0018](0018-fila-revisao-orfas-correcao-match.md): sem `media_override`, sem
  `asset` `origin='user'`. A entity "sobrevivente" é sempre a que está sendo
  escrita (a tela em que o dono está).
- Fusão move para a sobrevivente: `media_file` (e `media_file_entity` de
  série), `media_external` (sem duplicar por fonte), `asset` (kinds que a
  sobrevivente não tem), `credit`, `entity_relation` — depois apaga a
  perdedora (`ApagarEntity`, CASCADE limpa o resto).
- Quando a perdedora TEM investimento do dono, a fusão automática **não**
  acontece — a escrita responde um conflito claro (identifica a outra
  entity) em vez de silenciosamente deixar duas identidades iguais.
- Regressão: `internal/server/external_ids_dup_test.go` (já escrito nesta
  sessão, hoje com `t.Skip` documentando o bug) — remover o skip faz parte do
  critério de aceite.

**Fora (explicitamente):**
- Mudar o find-or-create do `POST /import` em si — ele já dedupe corretamente
  quando o id já está na entity certa; o buraco é só nos outros dois
  endpoints.
- Sugerir candidato local **antes** de importar (evitar a colisão nascer) —
  isso é o [0050](0050-auto-match-fila-de-revisao.md) (auto-match), que
  reduz a *chance* do cenário acontecer mas não substitui esta rede de
  segurança.
- Fusão manual/UI para quando **as duas** entities têm investimento do dono
  (duplicata "de verdade", ambas com dados reais) — fica como conflito
  reportado (409), resolução manual segue de fora (follow-up já apontado em
  0018 §7 e 0050 §7).
- Dedup para fontes sem coluna gerada (providers de identidade futuros além
  de tmdb/imdb/tvdb).
- Merge dentro da árvore de série (temporada/episódio) — só nível obra/topo.

## 4. Contrato técnico

### Dados / modelo
- `internal/db/queries/duplicata.sql` (nova): `EntityTemInvestimentoDono`
  (mesmo predicado do órfã, 0018) + queries de movimentação ponto-a-ponto
  (`MoverMediaFileParaEntity` em massa; `MoverMediaFileEntityLinha`/
  `MoverMediaExternalLinha`/`MoverAssetLinha`/`MoverEntityRelationParentLinha`/
  `MoverEntityRelationChildLinha` por linha; `MoverTodosCreditsParaEntity` em
  massa). **Decisão tomada**: o parser SQL do sqlc (engine sqlite) não aceita
  alias na tabela-alvo de um `UPDATE` nem correlated subquery contra ela — a
  decisão "a sobrevivente já tem uma linha equivalente?" é feita em **Go**
  (lista os dois lados com `ListarXPorEntity` e só move o que não colide),
  não em SQL, no mesmo espírito do resto do projeto (ex.: `purgeBatch` itera
  ids em vez de SQL em massa).
- Colisão detectada via `ListarEntitiesPorTmdbExcetoId`/`ListarEntitiesPorImdbExcetoId`/
  `ListarEntitiesPorTvdbExcetoId` (`:many`, excluem o alvo na própria SQL),
  filtrando `type == tipo` em Go — ver nota de bug residual abaixo sobre por
  que **não** são as queries `ObterEntityPor*` (`:one`) + filtro em Go.

### API
- `PUT /entities/{id}/external-ids` e `POST /entities/{id}/correct`: ao
  detectar colisão,
  - perdedora **sem** investimento → funde e responde `200` com a entity
    resolvida (sobrevivente) + **header** `X-Duplicate-Merged-Id: <id-da-
    entity-removida>` (decisão: header em vez de campo novo no DTO —
    `resolver.Entidade` é um contrato compartilhado por vários endpoints,
    o header é aditivo e não exige tocá-lo).
  - perdedora **com** investimento → `409 conflict_duplicate_identity`,
    mensagem já pronta em português citando a outra entity (id + título), sem
    gravar nada.
- Envelope de erro padrão (`{"error":{"code","message"}}`) para o `409`.

### Backend (Go)
- `internal/server/duplicata.go` (novo): `resolverColisaoExterna` (decide
  fundir vs. conflito, escreve o header/409), `acharColisaoExterna` (lookup
  por fonte), `fundirEntities` + as 4 funções `mover*SemColisao` (aplicam o
  "só move o que não colide" por tabela).
- `internal/server/external_ids.go` (`updateExternalIds`): itera cada par do
  mapa normalizado e chama `resolverColisaoExterna` antes de gravar.
- `internal/server/review.go` (`correctEntity`, 0018): chama
  `resolverColisaoExterna` antes do `enrich.Enriquecer` (a fusão traz de
  graça o `media_external`/`asset`/`credit` que a perdedora já tinha,
  cache-first).

### Frontend (Vite/React)
- **Achado durante a implementação**: o projeto não tem lib de toast/snackbar
  (sem `sonner` nem componente próprio) — o padrão já usado é um `<p role=
  "status">`/`<p role="alert">` inline no card/form (ex.: `settings.tsx`).
  Seguido esse padrão em vez de introduzir uma lib nova.
- `web/src/lib/api.ts`: `updateExternalIds`/`correctMatch` passam a devolver
  `{entity, duplicataMesclada}` (lê o header `X-Duplicate-Merged-Id` da
  `Response` crua).
- `web/src/components/external-ids-editor.tsx` e `web/src/routes/review.tsx`:
  banner "Duplicata mesclada automaticamente…" quando `duplicataMesclada`; o
  `409` já cai no tratamento de erro existente (`ApiError.message` exibida
  inline), sem mudança adicional.

### Arquivos afetados
- Novos: `internal/db/queries/duplicata.sql` (+ `internal/db/sqlcgen/duplicata.sql.go`
  gerado), `internal/server/duplicata.go`, `internal/server/external_ids_dup_test.go`.
- Alterados: `internal/server/external_ids.go`, `internal/server/review.go`,
  `web/src/lib/api.ts`, `web/src/components/external-ids-editor.tsx`,
  `web/src/routes/review.tsx`, `internal/db/queries/entity.sql` (+ gerado) —
  ver bug residual abaixo.

### Bug residual encontrado em produção (pós-deploy) e correção

Após o primeiro deploy, o dono relatou que uma duplicata **já existente antes
deste fix** (criada pelo bug original, quando a coluna gerada `external_id_*`
não é `UNIQUE` — só indexada) continuava sem fundir mesmo reenviando o mesmo
id pela tela de edição ("Salvar e re-enriquecer"), e os assets exclusivos da
duplicata (ex.: fanart do fanart.tv) nunca chegavam à entity com o arquivo.

**Causa**: `acharColisaoExterna` usava `ObterEntityPorTmdb`/`ObterEntityPorImdb`/
`ObterEntityPorTvdb` (queries `:one`, sem `ORDER BY`) e depois filtrava
`ent.ID == alvoID` em **Go** para descartar "a própria entity". Isso só
funciona quando existe **no máximo uma outra** entity com aquele id. Quando o
dado já estava corrompido (duas entities com o mesmo `external_id_tmdb`), a
query `:one` podia devolver a **própria** entity-alvo (sem garantia de ordem);
o filtro descartava esse único resultado e o código concluía "achou=false" —
nunca descobria a outra entity, e a fusão nunca disparava.

**Correção**: três novas queries `:many` — `ListarEntitiesPorTmdbExcetoId`/
`ListarEntitiesPorImdbExcetoId`/`ListarEntitiesPorTvdbExcetoId` — que já
excluem `alvoID` no `WHERE ... AND id != sqlc.arg('alvo_id')`, então o
resultado nunca inclui a própria entity-alvo, não importa quantas outras
compartilhem o id. `acharColisaoExterna` percorre os resultados procurando o
primeiro com `type == tipo`.

**Teste de regressão**: `TestCorrigirExternalIdComDuplicataJaCorrompidaNoBanco`
(`internal/server/external_ids_dup_test.go`) cria as DUAS entities já com
`external_id_tmdb=777` (simulando o dado corrompido, sem passar por nenhum
endpoint), dá um asset `fanart` só à duplicata, e confirma que reenviar o
mesmo id na entity com arquivo funde a duplicata e transfere o asset.
Confirmado que falha sem o fix (roda com o código antigo antes de aplicar a
correção) e passa com ele.

## 5. Critérios de aceite

- [x] Gravar (`PUT external-ids` ou `POST correct`) um id externo que já
  pertence a outra entity do mesmo `type`, quando a outra **não** tem
  investimento do dono, funde as duas automaticamente: arquivo(s) migram para
  a entity editada, a duplicata some da biblioteca. Nunca sobram duas
  entities com o mesmo `external_id_<fonte>`.
- [x] Quando a outra entity **tem** investimento do dono, a escrita responde
  `409` identificando a outra entity, em vez de duplicar a identidade
  silenciosamente.
- [x] `TestCorrigirExternalIdParaIdJaImportadoDuplicaEntity` (`internal/server/external_ids_dup_test.go`)
  passa **sem** `t.Skip`; `TestCorrigirExternalIdParaIdComInvestimentoDoDonoNaoFunde`
  cobre o lado do conflito (409, nada gravado).
- [x] `POST /import` não mudou de comportamento (segue find-or-create simples,
  como antes) — só os pontos de escrita de identidade em entity já existente
  foram tocados.
- [x] Catraca verde (Go + type-check web) — `make gate-local` passou: cobertura
  Go 74.9% (+0.5), cobertura web 91.35% (+0.85), 0 issues lint/vuln, 0 erros
  de type-check.

## 6. Notas de implementação / decisões

- **Motivação real, não hipotética**: reproduzido ao vivo pelo dono durante o
  uso normal (busca manual + import + correção de identidade), não um cenário
  hipotético. O teste de regressão foi escrito ANTES do fix (confirmado que
  falhava — reproduzia o bug — com `t.Skip` documentado) e depois teve o skip
  removido, virando a prova de que a correção funciona.
- **Direção do merge é sempre "quem está sendo editado sobrevive"**: o dono
  está olhando a tela da entity que ele está corrigindo — é ela que deve
  continuar existindo com o arquivo e (agora) a identidade certa. A outra
  (que só tinha a identidade, sem arquivo) é a que desaparece.
- **Reusa o predicado de "órfã" do 0018** para decidir fusão automática vs.
  conflito — não inventa um segundo critério de "investimento do dono".
- **Complementar ao 0050, não sobreposto**: o auto-match (0050) tenta achar o
  candidato local ANTES de importar, reduzindo a chance da colisão nascer;
  este briefing garante que, se ela nascer (por qualquer caminho, inclusive
  manual), o sistema nunca fica com identidade duplicada sem avisar ou sem
  resolver.
- **Fusão parcial (ambos os lados com dado real) fica de fora** deliberadamente
  — é o merge/split geral de duplicatas já apontado como aberto em 0018/0050,
  um problema mais amplo (UI de escolha de qual dado vence campo a campo) que
  não deve travar o fix do caso comum (duplicata "vazia" criada por engano).

## 7. Fora deste briefing / follow-ups

- Merge/split completo de duplicatas quando ambas as entities têm investimento
  real do dono (já apontado em [0018](0018-fila-revisao-orfas-correcao-match.md#7-fora-deste-briefing--follow-ups)
  e [0050](0050-auto-match-fila-de-revisao.md#7-fora-deste-briefing--follow-ups)).
- Auto-match/local-first no import ([0050](0050-auto-match-fila-de-revisao.md)).
- Dedup de identidade para fontes sem coluna gerada indexada.
