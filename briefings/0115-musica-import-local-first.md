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
| **Status** | done |
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

- [x] Casar 1 arquivo de música contra o MusicBrainz cria **só** artista +
      esse álbum + essa faixa — teste explícito garante que nenhum outro
      release do mesmo artista é criado. (Parte 1)
- [x] Faixa com artist-credit divergente do álbum **não** cria entity/relation
      `music_artista_feat` automaticamente; nome da colaboração fica só como
      metadado legível. (Parte 1)
- [x] Import manual de artista (`POST /import type=artista` via busca)
      continua trazendo a discografia inteira — regressão do 0106 preservada
      por teste. (Parte 1)
- [x] Tela externa de álbum (`type=album`) lista as faixas oficiais do
      release, com `internal_id` correto para as já possuídas e ação de
      importar para as faltando. (Parte 2 — `GET /external/{source}/album/{id}`,
      `internal/server/external_album.go`, testado em
      `internal/server/external_album_test.go` e no frontend em
      `web/src/routes/external-album.test.tsx`/`entity-detail-music.test.tsx`.)
- [x] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes
      (validado nesta Parte 2, repo inteiro).
- [x] `tsc -b`, `npm run lint`, `npm run test:coverage` verdes (frontend
      tocado; validado nesta Parte 2, todos os 65 arquivos/608 testes).
- [x] `make gate-local` limpo antes do commit final. Rodado pelo agente de
      validação/commit/push (18-19/08/2026): passou, com dois ajustes
      necessários (ver seção "Parte 3" abaixo) — jscpd (duplicação nova entre
      testes do 0114/0115) e `e2e_coverage_lines` (regressão real: a vertical
      de música não tinha NENHUMA jornada E2E Camada 1, então ~800 linhas
      novas em `enrich/musica.go`/`provider/musicbrainz`/`scanner/musica.go`
      ficaram 0% cobertas, derrubando o agregado 42.1%→41.7%, BLOQUEANTE).
- [ ] Validação manual em produção: rodar scan/rescan da biblioteca de
      música e confirmar via API que a contagem de artista/álbum/faixa cai
      para perto do 1:1 com os arquivos físicos (referência: LMS mostra
      153/72/411 para os mesmos 411 arquivos). **Ainda pendente**: só é
      possível DEPOIS do deploy em produção confirmado (ver runbook na seção
      6) — fora do alcance do agente de validação/commit, que não faz deploy.

## 6. Notas de implementação / decisões

### Parte 1 implementada (2026-08-19) — núcleo do import escopado

Implementados os itens 1-4 do escopo desta tarefa (capacidade seletiva do
provider, `enrich.ImportarFaixaPossuida`, os 2 call-sites do scanner, e o
metadado de feat sem auto-criar entity/relation). **Fora desta parte**: a tela
externa de "faixas faltando do álbum" (`GET /external/musicbrainz/album/{mbid}`
+ `POST /import type=faixa` + frontend do 0111) — fica para outro agente, que
depende do contrato abaixo. Briefing continua **não marcado como `done`**.

**Contrato exato para quem for construir a Parte 2 (tela externa de álbum):**

- `internal/provider/provider.go`: nova capacidade `MusicaAlbumProvider` com
  dois métodos — um `BuscarAlbum` só-por-MBID-de-release-group **não foi**
  criado (decisão: não foi necessário para os 2 call-sites do scanner); use
  os dois abaixo:
  - `BuscarAlbumDaFaixa(ctx, recordingMBID string, langs []string) (AlbumExterno, bool, error)`
    — resolve o release-group a partir do MBID de RECORDING (via
    `/recording/{id}?inc=releases+release-groups`, prefere release
    `status=Official`) e monta esse álbum via `montarAlbum` (reaproveitado,
    inalterado na semântica de degradar-sem-erro em falha de rede).
  - `BuscarAlbumPorNome(ctx, artistaMBID, nomeAlbum string, langs []string) (AlbumExterno, bool, error)`
    — lista os release-groups do artista (1 chamada barata) e escolhe o de
    título mais próximo de `nomeAlbum` (Jaro-Winkler, limiar
    `matching.LimiarAutoMatch`, sem empate no topo), depois monta só esse via
    `montarAlbum`.
  - **Se a Parte 2 precisar buscar um álbum por MBID de release-group
    DIRETO** (ex.: a tela externa abre `type=album` a partir do
    `external_ids.mbid` já gravado na entity álbum, não de uma faixa nem de
    um nome) — essa capacidade **ainda não existe**. Ou adicionar um 3º
    método (`BuscarAlbumPorMbid(ctx, albumMBID string, langs []string)
    (AlbumExterno, error)`, trivial: é praticamente `c.montarAlbum(ctx,
    albumMBID, "", "")` exposto) ao mesmo padrão, ou reaproveitar
    `BuscarAlbumDaFaixa` se a tela já tiver o MBID de uma faixa candidata.
    Ficou de fora desta parte por não ser exigido pelos 2 call-sites do
    scanner.
  - Ambos os métodos NUNCA enumeram/materializam mais de 1 álbum; erros de
    rede/parsing propagam (não degradam em silêncio) exceto pela semântica
    herdada de `montarAlbum` (que só é usada DEPOIS de já ter resolvido o
    release-group certo).
- `internal/enrich/musica.go`: novo tipo exportado `FaixaAlvo{RecordingMBID,
  AlbumNome, FaixaTitulo, FaixaNumero}` e novo método
  `Servico.ImportarFaixaPossuida(ctx, source, artistaMBID string, alvo
  FaixaAlvo, langs []string) (faixaID string, criada bool, err error)`. Cria
  só o artista (identidade via `BuscarDetalhe`/`MapearDetalhe` do Provider
  core — sem tocar release-groups), o álbum daquela faixa e a própria faixa.
  `importarArtista`/`Importar(tipo="artista")` (árvore inteira) **não foram
  alterados no comportamento** — só o helper interno compartilhado
  `acharOuCriarFaixa`/`extensaoFaixa` ganhou um parâmetro
  `artistaPrincipalMBID` (aditivo: grava `feat_artist_name`/`feat_artist_mbid`
  em `extension_data` quando há divergência, em AMBOS os caminhos — árvore
  E escopado — sem mudar nenhuma asserção dos testes já existentes de
  `importarArtista`).
- `internal/musicext/musicext.go`: chaves novas `FeatArtistNameKey =
  "feat_artist_name"` e `FeatArtistMbidKey = "feat_artist_mbid"` dentro do
  namespace `core.music@1` da entity **faixa** (não do álbum). Gravadas
  sempre que `ArtistaCreditoMBID` da faixa diverge do artista principal do
  álbum — inclusive no import de árvore completa (onde a entity/relation
  `music_artista_feat` também continua sendo criada, como antes; o metadado é
  redundante ali, mas não contraditório). No import ESCOPADO
  (`ImportarFaixaPossuida`), é a ÚNICA forma de saber que a faixa tem
  colaboração — não há entity/relation de feat.
- `internal/scanner/musica.go`: `importarEAcharPorMbid` e `matchTextual` agora
  chamam `MusicaImportador.ImportarFaixaPossuida` (interface em
  `internal/scanner/manager.go` ganhou esse 2º método, tipado com
  `enrich.FaixaAlvo` — scanner já importava `internal/enrich` antes, para
  `ErrSerieAmbigua`/`ErrSerieNaoEncontrada`). As funções de busca de faixa
  dentro da árvore JÁ IMPORTADA por entity_relation
  (`acharFaixaNaArvore`/`melhorFilhoPorTitulo`/`faixaPorNumero`) foram
  **removidas** (código morto — a seleção de faixa dentro do álbum agora
  acontece em `enrich.escolherFaixaAlvo`/`melhorFaixaPorTitulo`, operando
  direto sobre `[]provider.FaixaExterna` vindo do provider, sem round-trip ao
  banco). `matchTextual` ganhou uma guarda nova (`tags.Album == ""` também
  cai pro draft) que antes só era checada tarde demais (depois de já ter
  importado a árvore inteira à toa).
- **Testes**: cobertura em 3 camadas — `internal/provider/musicbrainz/
  musicbrainz_test.go` (parsing/seleção HTTP-shape das 2 capacidades novas,
  incl. `errors.Is(ErrNaoEncontrado)`), `internal/enrich/musica_test.go`
  (`TestImportarFaixaPossuidaCriaSoUmAlbum` — critério de aceite central,
  discografia fake com 2 álbuns prova que só 1 é tocado;
  `TestImportarFaixaPossuidaFeatGravaMetadadoSemEntity` — feat vira só
  metadado; `TestImportarFaixaPossuidaViaNomeTextual`; regressão
  `TestImportarArtistaCriaArvore`/`TestReimportarArtistaIdempotente`
  inalteradas e verdes), `internal/scanner/musica_test.go` (reescreveu os 4
  testes que assumiam árvore completa — `TestScanMusicaMbidFaixaEArtista*`
  renomeado para `...ImportaEscopado`, `TestScanMusicaResolucaoReversa*`,
  `TestScanMusicaBuscaTextual`, `TestScanMusicaReescaneio*` — todos agora
  afirmam `arvoreN == 0` e contagens de entity 1:1:1 artista/álbum/faixa).
- Validado: `go build ./...`, `go vet ./...`, `go test ./... -count=1` — 100%
  verde em TODO o repo (não só os pacotes tocados) nesta parte.
- **Pendente para a Parte 2** (outro agente): endpoint de tela externa
  `type=album`, `POST /import type=faixa` (`tipoImportavel` em
  `internal/server/enrich.go`), e o link no frontend da tela de álbum
  (0111). Nenhum desses foi tocado aqui.

### Parte 2 implementada (2026-08-19) — tela externa de álbum + import de faixa avulsa

Fecha o que a Parte 1 deixou pendente: endpoint de "faixas faltando" do álbum,
import de faixa avulsa e o link no frontend. Decisões que não estavam 100%
especificadas no briefing:

- **3º método do provider** (`internal/provider/provider.go`,
  `MusicaAlbumProvider.BuscarAlbumPorMbid(ctx, albumMBID string, langs
  []string) (AlbumExterno, error)`): implementado em
  `internal/provider/musicbrainz/musicbrainz.go` como a nota da Parte 1
  sugeriu — mas NÃO é um wrapper trivial de `montarAlbum` (que nunca erra,
  degrada em silêncio pra shell). `montarAlbum` foi refatorado em dois:
  `montarAlbumOuErro` (novo, propaga o erro da 1ª chamada — inclusive
  `provider.ErrNaoEncontrado` em 404) e `montarAlbum` (o de sempre, wrapper
  que degrada o erro pra shell, comportamento IDÊNTICO ao anterior pros 2
  call-sites existentes). Motivo: ao contrário de `BuscarAlbumDaFaixa`/
  `BuscarAlbumPorNome` (que resolvem o release-group por um caminho separado
  ANTES de chamar `montarAlbum`, então "álbum não existe" já foi descartado),
  `BuscarAlbumPorMbid` não tem resolução prévia — o MBID passado pode
  simplesmente não existir na fonte, e o chamador (o handler HTTP) precisa
  saber disso pra responder 404 em vez de "200 com álbum vazio".
- **Path do endpoint**: `GET /api/v1/external/{source}/album/{id}` (não
  `/external/musicbrainz/album/{mbid}` fixo por fonte) — mesmo padrão de
  `/external/{source}/collection/{id}`, o segmento estático `album` tem
  precedência automática sobre `{type}` de `/external/{source}/{type}/{id}`
  no chi (mesmo mecanismo, sem novo cuidado de ordem de registro). Handler
  novo `internal/server/external_album.go`
  (`getExternalAlbum`/`montarAlbumExterno`), molde estrutural de
  `external_colecao.go`. `internal_id` do álbum e de cada faixa resolvido por
  MBID via uma generalização nova de `idInternoItem`/`idInternoColecao`
  (`idInternoPorMbid`, `internal/server/interno.go`) — a música usa sempre o
  MESMO id externo (mbid) pra qualquer `entity.type`, ao contrário de
  filme/tv que despacham por `source`.
- **Import de faixa avulsa**: `tipoImportavel` (`internal/server/enrich.go`)
  JÁ NÃO aceitava `faixa` — adicionado. O dispatcher genérico
  `enrich.Servico.Importar(ctx, source, sourceID, tipo, langs)` não carrega
  contexto de PAI, então `type=faixa` NÃO passa por ele: `importar()` (server)
  ganhou um branch dedicado que chama um método novo,
  `enrich.Servico.ImportarFaixaAvulsa(ctx, source, recordingMBID, albumID,
  langs)` (`internal/enrich/musica.go`). O "pai" chega por um campo NOVO no
  corpo do POST, `parent_id` (id INTERNO do álbum) — `importBody.ParentID`,
  422 quando `type=faixa` e `parent_id` ausente. `ImportarFaixaAvulsa`
  resolve o mbid do álbum pai via `idsExternosDe` (já existia em
  `artes.go`), re-busca o álbum por `BuscarAlbumPorMbid` (reaproveita — nunca
  enumera a discografia), localiza a faixa por MBID exato
  (`escolherFaixaAlvo`, já existia) e reaproveita
  `acharOuCriarFaixa`/`ligarFilho` (idempotentes, já existiam) — nenhuma
  duplicação em reimport. O artista PRINCIPAL do álbum (só para o metadado de
  feat) é resolvido subindo 1 nível em `entity_relation` (`ListarRelacoesPais`,
  novo helper `artistaPrincipalDoAlbum`) — degrada em silêncio (sem feat) se
  ausente, nunca falha o import por isso.
- **`provedorCapaz`** (`internal/server/enrich.go`) ganhou um `case "faixa"`
  exigindo `MusicaAlbumProvider` explicitamente — `"artista"` continua caindo
  no `default` (sempre capaz, gap pré-existente do 0106 não tocado aqui,
  fora de escopo).
- **openapi.yaml**: novo path/schemas (`ExternalAlbum`, `ExternalAlbumTrack`)
  + `ImportRequest.type` ganhou `artista`/`faixa` (o enum estava desatualizado
  desde o 0106 — `artista` já era aceito no backend, mas nunca declarado no
  spec) e campo novo opcional `parent_id`. `api-types.ts` regenerado via
  `npm run gen:types`.
- **Frontend**: `type==='album'` é tratado dentro da MESMA rota
  `/external/$source/$type/$id` (`web/src/routes/external-detail.tsx`) — sem
  rota nova (0113/0114 estavam mexendo em `router.tsx` concorrentemente,
  fora dos limites desta tarefa). `ExternalDetailScreen` virou um dispatcher
  fino: delega a `ExternalAlbumView`
  (`web/src/routes/external-album.tsx`, componente novo, molde visual de
  `collection-detail.tsx`) quando `type==='album'`, senão renderiza o
  `ExternalItemView` de sempre (extraído do corpo antigo, sem mudança de
  comportamento pros demais tipos). O backend já roteava a mesma URL pro
  handler certo (precedência de segmento estático), então a paridade
  frontend/backend saiu de graça. `ImportarButton`
  (`web/src/components/rich-detail-view.tsx`) ganhou a prop opcional
  `parentId`, repassada como `parent_id` no corpo do import — reaproveitada
  tanto pela nova tela de álbum quanto por qualquer chamador futuro. Link "Ver
  faixas faltando" em `TracklistAlbum`
  (`web/src/components/tracklist-album.tsx`), condicionado a `mbid` (prop
  nova, `entity-detail.tsx` passa `data.external_ids.mbid`) — fonte
  hardcoded `"musicbrainz"` (única que modela a vertical hoje, mesmo hardcode
  que o backend usa em `provider.Nome()`).
- **Testes**: `internal/provider/musicbrainz/musicbrainz_test.go`
  (`TestBuscarAlbumPorMbid`/`...NaoEncontrado`/`...Vazio`);
  `internal/enrich/musica_test.go` (fake provider ganhou
  `BuscarAlbumPorMbid`, exigido pela interface ampliada — sem isso os testes
  da Parte 1 quebravam, corrigido aqui);
  `internal/scanner/musica_test.go` (idem, stub só pra satisfazer a
  interface — o scanner nunca chama este método);
  `internal/server/external_album_test.go` (endpoint novo: `internal_id`
  correto pras faixas já catalogadas vs. ausente pras faltando, 404, 503 sem
  provider; import de faixa avulsa liga ao álbum pai com `sort_index`
  correto e é idempotente em reimport; 422 sem `parent_id`; 502 quando o
  álbum pai não tem mbid); `web/src/routes/external-album.test.tsx` (tela
  nova: header, faixa já interna vs. faltando, clique em Importar envia
  `type=faixa`+`parent_id`, 404/503, álbum sem faixas);
  `web/src/routes/external-detail.test.tsx` (`type=album` delega à
  `ExternalAlbumView`, nunca ao `RichDetailView`);
  `web/src/routes/entity-detail-music.test.tsx` (link "Ver faixas faltando"
  aparece só com `mbid`).
- Validado: `go build ./...`, `go vet ./...`, `go test ./... -count=1` (repo
  inteiro, 100% verde) + `tsc -b`, `npm run lint`, `npm run test:coverage`
  (65 arquivos/608 testes, 100% verde). `make gate-local` NÃO rodado (
  reservado ao agente final, AGENTS.md); nenhum commit/push feito.
- **Achado do ambiente, sem relação com o código**: `web/coverage/` estava
  com o dono trocado pra `root` (provavelmente por causa do container de dev,
  `compose.dev.yaml`, rodando como root e escrevendo no bind mount) — o
  `npm run test:coverage` normal falhou com `EACCES` ao tentar sobrescrever
  `coverage-summary.json`. Contornado rodando `vitest run --coverage
  --coverage.reportsDirectory=<dir alternativo>` só pra confirmar 100% verde;
  o dono do projeto deve limpar/corrigir a permissão de `web/coverage/`
  antes do próximo `npm run test:coverage` normal.

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

### Parte 3 (agente de validação/commit/push, 18-19/08/2026) — gate + jornada E2E

Fechou o ciclo dos 4 briefings implementados em paralelo (0112-0115): conferiu
o diff contra cada briefing, rodou `make gate-local` e corrigiu as duas
regressões bloqueantes que apareceram, sem tocar nada do ESCOPO funcional das
Partes 1/2 (nenhuma mudança de comportamento do import/scanner/endpoint).

- **jscpd (duplicação web)**: o diff combinado dos 4 briefings introduziu
  duplicação nova entre arquivos de TESTE de mais de um briefing (ex.:
  `poster-grid.test.tsx`↔`library-browse.test.tsx`, ambos do 0114;
  `entity-detail-music.test.tsx` ganhou um teste quase idêntico a um já
  existente). Corrigido extraindo helpers compartilhados
  (`paginaResposta`/`urlEnviada` em `web/src/test/stub-fetch.ts`, reuso de
  `erroJson`/`fakeLanguage`/`LinkMock` já existentes) e mesclando 2 testes
  quase-idênticos em `entity-detail-music.test.tsx` — mesmo padrão já usado em
  sessões anteriores (AGENTS.md). Resultado: 0,36% (< baseline 0,37%).
- **`e2e_coverage_lines` (Camada 1 Go)**: regressão REAL, não boilerplate — a
  vertical de música nunca ganhou uma jornada E2E (o harness da Camada 1 não
  sobe o scanner nem tem um provider musicbrainz fake), então TODAS as ~800
  linhas novas de `internal/enrich/musica.go`,
  `internal/provider/musicbrainz/musicbrainz.go`,
  `internal/scanner/musica.go` e `internal/server/external_album.go` ficaram
  0% cobertas por essa métrica, derrubando o agregado 42,1%→41,7%
  (bloqueante). Corrigido com uma jornada nova,
  `internal/e2e/journeys_musica_test.go`
  (`TestJornadaMusicaImportArtistaEFaixaFaltando`), e um provider fake dedicado
  `internal/e2e/fake_musicbrainz_test.go` (`Nome()="musicbrainz"`,
  implementa `provider.MusicaProvider`+`provider.MusicaAlbumProvider` com
  dados fixos): importa um artista inteiro via busca manual (POST /import
  type=artista, 0106) com uma árvore fake que só traz 1 das 2 faixas do
  álbum oficial, confirma a tela externa "faixas faltando" (GET
  /external/musicbrainz/album/{mbid}) e importa a 2ª faixa avulsa (POST
  /import type=faixa+parent_id, 0115 Parte 2), com reimport idempotente. O
  gatilho automático do SCAN (0115 Parte 1) continua sem jornada E2E — cobrir
  isso exigiria subir o scanner real dentro do harness com arquivos de áudio
  com tags MBID embutidas, escopo maior que esta correção pontual; fica como
  follow-up (§7). Resultado: 42,9% (> baseline 42,1%). Helper genérico `objs`
  (`internal/e2e/harness_test.go`) extraído para a jornada nova não estourar
  gocyclo 10 (AGENTS.md pede ≤10 em função nova).
- `go build ./...`, `go vet ./...`, `go test ./... -count=1` (repo inteiro) e
  `tsc -b`/`npm run lint`/`npx vitest run` (web) revalidados 100% verdes após
  os dois ajustes acima. `make gate-local` completo: PASSOU.

## 7. Fora deste briefing / follow-ups

- Estender o mesmo racional local-first para série/coleção (`importarSerie`)
  — achado registrado no ADR-0029, não decidido aqui.
- UI de import de artista feat a partir da tela da faixa — não pedida agora.
- Jornada E2E Camada 1 para o gatilho AUTOMÁTICO do scan de música (0115
  Parte 1 — `importarEAcharPorMbid`/`matchTextual`) — a jornada nova (Parte 3)
  cobre só o caminho de import HTTP direto (busca manual + tela externa); o
  scan em si exigiria subir o scanner real no harness com arquivos de áudio
  com tags MBID embutidas, escopo maior que a correção pontual do gate.
