# 0118 — Reprocessar artes faltantes de entities já identificadas

> Uma entity que já tem identidade externa (mbid/tmdb/imdb/tvdb) pode nunca ter
> ganhado o inventário de arte de nenhum `ArteProvider` — porque o provider
> (ex.: fanart.tv) foi configurado DEPOIS que ela foi importada. Sem nenhum
> mecanismo automático de retry, ela ficava sem foto/fundo/logo para sempre, só
> alcançável pelo botão manual "Baixar Artes Faltantes", um item de cada vez.

| | |
|---|---|
| **ID** | 0118 |
| **Tipo** | feature |
| **Fase (roadmap)** | Transversal / infraestrutura operacional — extensão do job `saude_biblioteca`, complemento do enriquecimento retroativo ([0056](0056-enriquecimento-externo-retroativo-job-saude.md)/[0116](0116-enriquecimento-retroativo-musica-artista.md)) e do [0117](0117-fix-artes-e-identidade-mbid-fora-do-import.md). |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Status** | done |
| **Depende de** | [0048](0048-fundacao-providers-de-enriquecimento-arte-trailer-tema.md), [0056](0056-enriquecimento-externo-retroativo-job-saude.md), [0085](0085-throttle-configuravel-enriquecimento-retroativo.md), [0088](0088-acoes-forcadas-menu-gerenciar.md), [0117](0117-fix-artes-e-identidade-mbid-fora-do-import.md) |

---

## 1. Objetivo

Depois de deployar o [0117](0117-fix-artes-e-identidade-mbid-fora-do-import.md)
(que fecha o gap de artista SEM identidade), o dono validou em produção e
reportou: rodou o job de saúde duas vezes e a maioria dos artistas continua
sem imagem — só um (já identificado por outra via) tem inventário completo.
Investigação ao vivo (API de produção) confirmou: **todos os 24 artistas já
têm `external_ids["mbid"]`** — nenhum é elegível para o 0116/0117 (que exige
`external_ids_json IS NULL`). O `EnriquecerRetroativo` rodou e aplicou 0/0,
corretamente — não havia nada para ele fazer.

O problema real é outro: `GET /entities/{id}/artwork` mostra `available: {}`
(inventário vazio) para 23 dos 24 artistas — inclusive nomes mainstream que
certamente existem no fanart.tv (a-ha, Jamiroquai, Tears for Fears, Fatboy
Slim). A única explicação plausível é que o inventário de arte foi buscado
(no import original, `importarArtista`/`ImportarFaixaPossuida`, que sempre
chamam `enriquecerArtes`) **antes** de o fanart.tv estar configurado no
sistema — a chamada rodou com `s.providers.All()` sem nenhum `ArteProvider`,
achou nada, e nunca mais foi tentada. Não existe hoje NENHUM mecanismo
automático que retente a busca de arte para uma entity que já tem identidade
— só o botão manual "Baixar Artes Faltantes" (briefing 0088), um item por vez.
Isso vale para qualquer tipo (movie/tv/artista), não só música — é só mais
visível em música porque o fanart.tv foi configurado recentemente.

## 2. Jornada do usuário

1. O dono cadastra a chave da fanart.tv MESES depois de já ter uma biblioteca
   grande de música/filmes/séries importada.
2. Sem este briefing: nada acontece automaticamente — os itens antigos ficam
   sem arte para sempre, a não ser que o dono clique "Baixar Artes Faltantes"
   item por item.
3. Com este briefing: na próxima execução do job `saude_biblioteca` (agendada
   ou manual), um lote pequeno de entities já identificadas mas sem inventário
   verificado é reprocessado automaticamente; a mensagem final do job ganha
   "N entity(ies) com inventário de arte reprocessado". Nas execuções
   seguintes (respeitando o throttle de dias), o restante da biblioteca é
   coberto aos poucos, sem hostilizar a API do provider.

## 3. Escopo

**Dentro:**
- Novo método `Server.ReprocessarArtesFaltantes` + query
  `ListarElegiveisReprocessamentoArtes` (mirror de
  `ListarElegiveisEnriquecimentoRetroativo`, mas com
  `external_ids_json IS NOT NULL`): tipos de topo, não-draft, com posse
  (direta ou via árvore), respeitando o MESMO throttle
  (`retroactive_enrichment_days`/global) e o MESMO teto por execução.
- Novo passo no job `saude_biblioteca`, depois do enriquecimento retroativo:
  chama `AtualizarInventarioArtes` (já existente, briefing 0088) para cada
  candidato, best-effort, e carimba `last_external_match_attempt_at`.
- Reaproveita `last_external_match_attempt_at` com uma semântica NOVA para
  entities já identificadas ("última vez que o inventário de arte foi
  verificado") — livre para esse universo, já que a query irmã só olha
  `external_ids_json IS NULL`.

**Fora (explicitamente):**
- Um backfill imediato/forçado (sem esperar o próximo ciclo do job) — o dono
  pode rodar `saude_biblioteca` manualmente (`POST /jobs/saude_biblioteca/run`)
  se quiser antecipar.
- Diferenciar "provider respondeu vazio de propósito" de "provider nunca foi
  consultado" — os dois casos são tratados igual (throttle por
  `last_external_match_attempt_at`, retry periódico); não há nova coluna/
  marcador por fonte.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migração nova — reusa `entity.last_external_match_attempt_at`
  (migration 0013) com a semântica dupla descrita acima.

### Backend (Go)
- `internal/db/queries/review.sql` (+ sqlc): `ListarElegiveisReprocessamentoArtes`.
- `internal/server/reprocessar_artes.go` (novo): `Server.ReprocessarArtesFaltantes`.
- `internal/job/libraryhealth/libraryhealth.go`: `Matcher.ReprocessarArtesFaltantes`,
  `rodarReprocessamentoArtes`, wiring em `Executar`, cláusula nova em
  `montarMensagem`.

### Arquivos afetados
- `internal/db/queries/review.sql`, `internal/db/sqlcgen/review.sql.go` (gerado).
- `internal/server/reprocessar_artes.go`, `internal/server/reprocessar_artes_test.go`.
- `internal/job/libraryhealth/libraryhealth.go`, `internal/job/libraryhealth/libraryhealth_test.go`.
- `internal/server/retroativo_test.go` (comentário de
  `TestListaSubNosSincronizadaComQuery` atualizado — mesma lista literal).
- `internal/db/sqlcgen/crud_test.go` (cobertura direta da query nova, mesmo
  motivo do 0117: cross-package não conta sem `-coverpkg`).

## 5. Critérios de aceite

- [x] Uma entity com identidade (`external_ids` não-nulo), tipo de topo,
      não-draft, com posse, e SEM `media_external(source=<algum ArteProvider
      registrado>)` é reprocessada automaticamente na próxima execução do job
      `saude_biblioteca` (ou chamada direta de `ReprocessarArtesFaltantes`).
- [x] O throttle é respeitado: uma entity já verificada dentro da janela de
      `retroactive_enrichment_days` NÃO é reprocessada de novo.
- [x] Sem `s.enrich` configurado, degrada para `(0, nil)` sem tocar o banco.
- [x] A mensagem final do job ganha "N entity(ies) com inventário de arte
      reprocessado" só quando N > 0.
- [x] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes;
      `make gate-local` sem regressão de cobertura.

## 6. Notas de implementação / decisões

- Reusar `last_external_match_attempt_at` (em vez de uma coluna nova) foi
  possível porque as duas queries são estruturalmente EXCLUSIVAS
  (`external_ids_json IS NULL` vs. `IS NOT NULL`) — o mesmo campo nunca é lido
  com os dois significados para a MESMA linha ao mesmo tempo. Uma entity que
  transita de "sem identidade" para "identificada" via 0116/0117 já ganha o
  inventário de arte NA HORA (enriquecerArtes chamado dentro de
  `aplicarCandidatoExterno`/`enriquecerBase`), então na prática ela nunca
  chega "identificada mas sem arte E com o timestamp antigo" — o caso real
  (import direto ANTES do provider existir) sempre parte de
  `last_external_match_attempt_at IS NULL`.
- `AtualizarInventarioArtes` (não `Enriquecer`) foi escolhido de propósito:
  não refaz busca de metadado/título/árvore, só o inventário de arte — mais
  barato e sem efeito colateral em campos que o dono pode já ter corrigido.

## 7. Fora deste briefing / follow-ups

- Se, depois de alguns ciclos do job, o dono perceber que artistas específicos
  ainda ficam sem arte, é porque o fanart.tv genuinamente não tem dados para
  esse artista (comportamento esperado da fonte, não um bug) — o botão manual
  "Baixar Artes Faltantes" continua disponível para forçar uma nova tentativa
  pontual a qualquer momento.
