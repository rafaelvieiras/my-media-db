# 0115 — Música: import local-first (só o que a faixa possuída traz)

> Corrige a distorção achada em produção (478 artistas/236 álbuns/2478 faixas
> para 411 arquivos reais): o scanner deixa de disparar import da discografia
> inteira a partir de 1 faixa casada. Artista/álbum nascem só do que existe em
> disco; faixas faltando do álbum ficam visíveis pra importar sob demanda,
> mesmo padrão de item externo não-possuído que filme/série já usam;
> artista secundário/feat deixa de ser auto-criado.

| | |
|---|---|
| **ID** | 0115 |
| **Tipo** | fix/feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins (música) |
| **Status** | ready |
| **Docs relacionadas** | [ADR-0029](../docs/adr/0029-import-de-musica-local-first-por-faixa-possuida.md), [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md), [ADR-0009](../docs/adr/0009-posse-e-status-eixos-ortogonais.md) |
| **Depende de** | 0105, 0106, 0107 |

---

## 1. Objetivo

Hoje, `internal/enrich/musica.go:37` (`importarArtista`) itera **todos** os
álbuns da árvore MusicBrainz do artista sem filtro, e é disparado a partir de
**1 faixa casada** pelos dois call-sites de `internal/scanner/musica.go`:
`importarEAcharPorMbid` (linha 145) e `matchTextual` (linha 194) — ambos
chamam `musicaImportador.Importar(ctx, source, artistaMBID, tipoArtista,
langs)`, que cai no dispatcher genérico (`internal/enrich/enrich.go:175`) e
sempre traz a árvore inteira. Resultado real em produção: 2478 faixas/236
álbuns/478 artistas catalogados para 411 arquivos físicos (98 faixas
realmente possuídas) — o LMS (`epoupon/lms`, mesma biblioteca) confirma o
padrão correto: 411/72/153, 1:1 com o disco. Ver ADR-0029.

Este briefing muda o gatilho automático do scan para importar **só** o que a
faixa local efetivamente representa (artista + o álbum daquela faixa), expõe
as demais faixas do mesmo álbum como "faltando" (importáveis sob demanda,
mesmo padrão de filme/série não-possuído), e para de auto-criar artista
feat/secundário.

## 2. Jornada do usuário

1. O dono roda um scan da biblioteca de música. Um arquivo casa com uma
   faixa do MusicBrainz. O sistema cria: a entity `artista` (se não existia),
   a entity `album` daquele release específico (se não existia) e a entity
   `faixa` daquela gravação — **nada mais**. Nenhum outro álbum do artista é
   tocado.
2. O dono abre a tela do álbum. Vê as faixas que já tem (as reais, ligadas a
   arquivo) e, na mesma tela ou a partir dela, as faixas do release oficial
   que **não** tem em disco — mesma UX de "item externo ainda não importado"
   que filme/série/coleção já usam (tela externa com botão "Importar" por
   item, marcando `internal_id` quando já existe).
3. Se ele importar uma faixa faltante, ela vira uma entity normal (mesmo
   fluxo de sempre) — ele pode então marcar `status=wanted` como já faz hoje
   com qualquer item catalogado-mas-não-possuído (ADR-0009), sem mecanismo
   novo pra isso.
4. Se uma faixa possuída tem artista de colaboração (feat) divergente do
   artista do álbum, o nome da colaboração aparece como texto (metadado da
   faixa) — **sem** virar entity/artista navegável automaticamente. Se o dono
   quiser aquele artista como item de 1ª classe, ele busca e importa
   manualmente (fluxo já existente, `POST /import type=artista` via busca —
   zero código novo pra isso).
5. Importar um artista inteiro deliberadamente (busca manual → importar)
   continua trazendo a discografia completa, exatamente como hoje — isso é
   pull explícito do dono, não muda.

## 3. Escopo

**Dentro:**
- `internal/provider/provider.go` / `internal/provider/musicbrainz/
  musicbrainz.go`: nova capacidade **seletiva** de álbum — buscar/montar
  **um** release específico (por MBID conhecido, via tag local
  `MusicBrainzAlbumID`/derivado do recording, ou por busca artista+nome de
  álbum quando só há match textual) sem enumerar `release-groups` do artista
  inteiro. Reaproveitar a lógica privada que já existe (`montarAlbum` em
  `musicbrainz.go`), expondo como método público da capacidade.
- `internal/enrich/musica.go`: nova função (substituindo o disparo direto de
  `importarArtista` pelos call-sites do scanner) que:
  1. Acha-ou-cria o artista **sem** trazer álbuns (só identidade —
     nome/MBID/bio; reaproveitar `acharOuCriarArtista` já existente, que já é
     idempotente e não depende do loop de álbuns).
  2. Acha-ou-cria **o álbum daquela faixa** (só esse release) via a nova
     capacidade seletiva do provider.
  3. Acha-ou-cria **a faixa casada** dentro desse álbum (reaproveitar
     `acharOuCriarFaixa`/`importarFaixa` já existentes, escopados a 1 faixa).
  4. **Não** cria entity/relation de artista feat automaticamente — se o
     artist-credit da faixa divergir do artista do álbum, grava o nome bruto
     da colaboração em `extension_data core.music@1` (campo novo, ex.
     `feat_artist_name`/`feat_artist_mbid`) só como metadado de leitura, sem
     `entity_relation(kind=music_artista_feat)`.
- `internal/scanner/musica.go`: `importarEAcharPorMbid` (linha 145) e
  `matchTextual` (linha 194) passam a chamar a nova função escopada em vez de
  `Importar(..., tipoArtista, ...)` (árvore inteira). Para o caminho textual
  (sem MBID embutido na tag), a busca de álbum precisa ser por
  artista+nome-do-álbum (nova capacidade do provider), não por enumeração.
- **Faixas faltando do álbum** — reaproveitar o padrão já existente de tela
  externa (épico "tela rica do externo", `GET /external/{source}/{type}/{id}`
  + `ResolvedEntity`/`internal_id`, hoje usado por coleção/série): estender
  esse fluxo genérico para `type=album`, fonte `musicbrainz`, usando a nova
  capacidade seletiva de álbum do provider — lista as faixas oficiais do
  release, marcando `internal_id` para as já catalogadas (ligadas via
  `entity_relation kind=music_album`) e permitindo `POST /import type=faixa`
  (reaproveitar o dispatcher genérico já existente) para as demais. Verificar
  se `tipoImportavel` (`internal/server/enrich.go:233`) já aceita `faixa`;
  se não aceitar, adicionar (reaproveitando `importarItem` genérico se já
  servir, dado que faixa já tem `external_id_mbid`).
- Frontend: tela de álbum (`web/src/routes/...`, componente `TracklistAlbum`
  do briefing 0111) ganha link/ação para a tela externa de faixas faltando
  quando o álbum tiver `external_ids.mbid` — mesmo padrão visual/de navegação
  que a tela externa de coleção/série já usa (reaproveitar componente
  existente, não recriar).
- Testes cobrindo: 1 faixa casada não traz outros álbuns do artista; feat não
  auto-cria entity/relation (só grava metadado); import manual de artista
  continua trazendo a árvore inteira (regressão do 0106 preservada);
  faixas faltando do álbum aparecem na tela externa com `internal_id` correto
  para as já possuídas.

**Fora (explicitamente):**
- Mudar o comportamento equivalente de série (`importarSerie` também traz a
  árvore inteira) — tensão análoga registrada no ADR-0029, não resolvida
  aqui.
- UI dedicada para importar o artista feat direto da tela da faixa — o fluxo
  de busca/import manual já existente cobre o caso, sem atalho extra.
- Registro de tipos de topo / `/search` genérico / home / rota `/library` —
  briefings 0112-0114 (independentes deste).
- **Limpeza dos dados de produção** (excluir artista/álbum/faixa não-possuídos
  criados pelo comportamento antigo) — ação operacional separada, feita
  **depois** do deploy deste briefing (ver seção 6).

## 4. Contrato técnico

### Dados / modelo
- Sem migration nova de schema (`entity`/`entity_relation` já cobrem tudo,
  ADR-0027). Campo novo em `extension_data core.music@1` para o nome bruto de
  colaboração (`feat_artist_name`, opcional) — dentro do JSON existente, sem
  coluna nova.

### API
- `GET /external/musicbrainz/album/{mbid}` (ou o path genérico equivalente já
  usado por `type=collection`/`type=tv` — confirmar o padrão exato do roteador
  existente): novo suporte a `type=album`.
- `POST /api/v1/import`: confirmar/adicionar `type=faixa` em `tipoImportavel`.

### Backend (Go)
- `internal/provider/provider.go`, `internal/provider/musicbrainz/
  musicbrainz.go` (+ `_test.go`): capacidade seletiva de álbum.
- `internal/enrich/musica.go` (+ `_test.go`): nova função de import escopado;
  `importarArtista` (árvore inteira) **permanece intacta** para o caminho de
  import manual (busca → importar artista).
- `internal/scanner/musica.go` (+ `_test.go`): os dois call-sites trocam de
  função.
- `internal/server/enrich.go` (se `type=faixa` precisar entrar em
  `tipoImportavel`) e o handler de external-detail (se precisar de ajuste
  para `type=album`).
- `internal/musicext/musicext.go`: eventual constante nova de chave de
  `extension_data` para `feat_artist_name`.

### Frontend (Vite/React)
- Tela de álbum (0111): link para faixas faltando, reaproveitando o
  componente de tela externa já existente (coleção/série).

### Arquivos afetados
- `internal/provider/provider.go`, `internal/provider/musicbrainz/*`
- `internal/enrich/musica.go`, `_test.go`
- `internal/scanner/musica.go`, `_test.go`
- `internal/server/enrich.go` (se necessário), `internal/server/external.go`
  (ou onde estiver o handler de external-detail; localizar exato durante a
  implementação)
- `internal/musicext/musicext.go`
- Componente de tela de álbum no frontend (`web/src/routes/entity-detail*` ou
  componente dedicado do 0111)

## 5. Critérios de aceite

- [ ] Casar 1 arquivo de música contra o MusicBrainz cria **só** artista +
      esse álbum + essa faixa — teste explícito garante que nenhum outro
      release do mesmo artista é criado.
- [ ] Faixa com artist-credit divergente do álbum **não** cria entity/relation
      `music_artista_feat` automaticamente; nome da colaboração fica só como
      metadado legível.
- [ ] Import manual de artista (`POST /import type=artista` via busca)
      continua trazendo a discografia inteira — regressão do 0106 preservada
      por teste.
- [ ] Tela externa de álbum (`type=album`) lista as faixas oficiais do
      release, com `internal_id` correto para as já possuídas e ação de
      importar para as faltando.
- [ ] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes.
- [ ] `tsc -b`, `npm run lint`, `npm run test:coverage` verdes (frontend
      tocado).
- [ ] `make gate-local` limpo antes do commit final.
- [ ] Validação manual em produção: rodar scan/rescan da biblioteca de
      música e confirmar via API que a contagem de artista/álbum/faixa cai
      para perto do 1:1 com os arquivos físicos (referência: LMS mostra
      153/72/411 para os mesmos 411 arquivos).

## 6. Notas de implementação / decisões

- **Ordem de execução recomendada**: este briefing pode rodar em paralelo com
  0112-0114 (times/schemas diferentes), mas a **limpeza de dado em produção**
  (pedida pelo dono) só deve ser executada **depois** deste briefing estar
  deployado — senão um scan/rescan subsequente recria a mesma distorção antes
  da correção estar no ar. Ver runbook de limpeza abaixo.
- **Runbook de limpeza de produção** (executar manualmente, fora do escopo de
  código deste briefing, depois do deploy confirmado):
  1. Identificar todo `entity type IN ('artista','album','faixa')` sem posse
     (`NOT EXISTS media_file present`) e sem `status='wanted'` (preservar o
     que o dono já marcou como desejado deliberadamente, se houver).
  2. Confirmar a contagem antes de apagar (comparar com a lista de exceções).
  3. Apagar em cascata respeitando FK (`entity_relation` antes de `entity`,
     ou usar `ON DELETE CASCADE` se já configurado — confirmar no schema).
  4. Confirmar pós-limpeza via `GET /health/system` (`by_type`) e comparar
     com a contagem de arquivos físicos.
  5. Não apagar nada com `media_file` vinculado (posse real) nem qualquer
     item que o dono tenha explicitamente marcado `wanted`.
- Se durante a implementação um caso de hierarquia não coberto pelo ADR-0027/
  ADR-0029 aparecer (ex. faixa solta sem álbum, "Various Artists"), **parar e
  voltar para a decisão** em vez de inventar solução ad-hoc — mesma regra já
  usada no 0105/0106.

## 7. Fora deste briefing / follow-ups

- Estender o mesmo racional local-first para série/coleção (`importarSerie`)
  — achado registrado no ADR-0029, não decidido aqui.
- UI de import de artista feat a partir da tela da faixa — não pedida agora.
