# 0087 — Corrige o inventário fanart.tv ausente fora do import explícito

> Item novo (filme, série, temporada ou episódio) chega à biblioteca quase
> sempre via scan → auto-match/enriquecimento retroativo, nunca via busca
> manual → `POST /import`. Nesse caminho dominante, o inventário de artes do
> fanart.tv (briefing 0048) nunca é buscado nem persistido — o item fica só
> com poster/fanart do provider de árvore (TMDB/TVDB), mesmo tendo dezenas de
> artes disponíveis na fanart.tv. Investigado ao vivo com dado real de
> produção (`data/mymediadb.sqlite`, entity "A Bela Adormecida",
> `00b21c07-5300-48ac-9c51-9eb449153ceb`): `tmdb_id=10882` presente,
> `MYMEDIADB_FANARTTV_API_KEY` configurada no container, e ainda assim
> `media_external` só tem a linha `source='tmdb'` — nenhuma `source='fanarttv'`.

| | |
|---|---|
| **ID** | 0087 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md) |
| **Depende de** | 0046, 0048 |

---

## 1. Objetivo

O briefing 0048 fechou o fanart.tv como `ArteProvider` lazy (lista o
inventário no import, baixa só o selecionado). Na prática, "no import" só foi
implementado para o caminho `enrich.Importar` (busca manual → botão
"Importar"). Todo o resto do sistema — scanner + auto-match, enriquecimento
retroativo (0085), "Corrigir" e "Confirmar candidato" da fila de Revisão —
passa por `enrich.Enriquecer`, que nunca dispara a busca de artes para
**filme**. Como esse é o caminho pelo qual a esmagadora maioria dos itens
"novos" realmente entra na biblioteca (scan de uma pasta existente, não busca
manual item a item), o efeito prático é: **quase nenhum filme novo ganha
artes do fanart.tv**, contrariando a intenção original do 0048.

Uma segunda causa, independente, bloqueia o mesmo sintoma para **série**
quando só o TMDB está configurado (sem TVDB — o cenário coberto pelo
princípio de independência de provider do 0046, e o caso real desta
instância): o fanart.tv exige `tvdb_id` **exclusivamente** para o endpoint de
TV (`internal/provider/fanarttv/fanarttv.go`, `rotaETabela`, case `"tv"` —
sem fallback para tmdb/imdb, ao contrário do case `"movie"`, que aceita os
dois). O TMDB já devolve `tvdb_id` no mesmo append `external_ids` que o
projeto já busca (usado hoje só para extrair o `imdb_id`), mas esse valor é
descartado — nunca chega a `entity.external_ids_json`. Sem TVDB configurado,
toda série fica permanentemente sem condição de cruzar com o fanart.tv,
mesmo depois de corrigida a causa 1.

Este briefing corrige as duas causas. Sem elas, o princípio nº 6 do
`AGENTS.md` ("a mídia sobrevive a qualquer provider registrado") fica violado
na prática para o fanart.tv: ele só funciona no caminho de import manual e
só quando TVDB também está configurado.

## 2. Jornada do usuário

1. O dono tem `MYMEDIADB_FANARTTV_API_KEY` configurada (e, opcionalmente, sem
   TVDB — só TMDB).
2. Um arquivo novo aparece numa pasta já cadastrada (scan periódico ou
   watch). O scanner cria a entity (draft/cataloged) e a fila de auto-match
   (ou o job de saúde/retroativo) resolve o candidato externo e chama
   `Enriquecer` — **não** `Importar`.
3. Hoje: a entity fica só com poster/fanart do TMDB; a galeria de artes
   (0049) não mostra nada do fanart.tv; o job `midias_faltantes` não tem
   nada extra para materializar.
4. Depois deste fix: o mesmo fluxo (sem nenhuma ação manual adicional do
   dono) grava o inventário `media_external(source='fanarttv')` — filme
   imediatamente; série assim que `tvdb_id` é capturado do append
   `external_ids` do TMDB — e a galeria/job de mídias faltantes passam a
   enxergar as artes extras normalmente, do mesmo jeito que já funcionava
   para import manual.

## 3. Escopo

**Dentro:**
- Causa 1 (filme): dispara `enriquecerArtes` também quando `enrich.Enriquecer`
  é chamado fora do fluxo de import (auto-match, retroativo, Corrigir,
  Confirmar candidato, `POST /entities/{id}/enrich`).
- Causa 2 (série sem TVDB): captura `tvdb_id` do append `external_ids` do
  TMDB (já buscado, sem ida extra à rede) e mescla em
  `entity.external_ids_json["tvdb"]` da entity SÉRIE, para toda série cuja
  árvore veio do TMDB (com ou sem TVDB configurado — não-destrutivo, só
  adiciona um cross-id que hoje nunca é gravado).
- Cobrir as duas causas com teste de regressão (unit, sem depender de rede
  real).
- Reaproveitar a UPSERT idempotente já existente (`persistirInventarioArtes`,
  `UNIQUE(entity_id, source)`) — sem mudar o formato do inventário nem a
  lógica de agregação da galeria/job de mídias faltantes.

**Fora (explicitamente):**
- Qualquer mudança em `internal/server/artwork.go` (agregador
  `montarGruposArtwork`) ou no job `midias_faltantes` — ambos já leem
  corretamente o inventário fanart.tv quando ele existe; o gap é só na
  ESCRITA do inventário.
- Backfill em massa dos itens já presos hoje sem inventário — o
  enriquecimento retroativo (0085) já periodicamente re-chama `Enriquecer`
  para itens elegíveis, então eles se auto-corrigem com o tempo; um
  reprocessamento manual (script/endpoint dedicado) fica como follow-up se o
  dono quiser acelerar.
- Suportar OUTRO cross-id além de `tvdb_id` (ex.: TMDB→TVDB para filme, que
  nem existe — filme já aceita tmdb/imdb direto no fanart.tv) — fora de
  escopo, sem caso de uso hoje.
- Mudar o comportamento "degrada em silêncio, sem log" quando falta id
  utilizável (`rotaETabela` retornando `ok=false`) — isso é intencional
  (princípio 6) e correto; só o CONTEÚDO dos ids disponíveis está sendo
  corrigido aqui, não o comportamento de degradação.

## 4. Contrato técnico

### Backend (Go)

**Causa 1 — `internal/enrich/enrich.go`:**
- `enriquecerBase` (linha ~100-134): adicionar, após `curarTitulo` (e fora do
  branch de série, que já cobre a si mesma via `sincronizarArvoreSerie` →
  `aplicarArvoreSerie` → `enriquecerArtes`), o disparo para filme:
  ```go
  if tipo == tipoFilme && sincronizarArvore {
      s.enriquecerArtes(ctx, entityID, tipo, langs)
  }
  ```
  Reusa o parâmetro `sincronizarArvore` já existente como sinal "esta chamada
  veio do `Enriquecer()` público", não de dentro de `importarItemBase`
  (que passa `false` nos dois call sites internos e já é coberto pela
  chamada existente em `importarItem`, linha ~164-166) — evita
  dupla-chamada sem introduzir parâmetro novo. Atualizar o comentário da
  linha ~190 (hoje diz que o parâmetro é "inofensivo" para filme — deixa de
  ser verdade).
- Nenhuma mudança em `importarItem`/`importarItemBase` — o caminho de
  import explícito continua idêntico.

**Causa 2 — `internal/provider/tmdb/tmdb_rico.go` + `internal/enrich/serie.go`:**
- `externalIdsTMDB` (linha ~86-88): acrescentar `TvdbID string
  \`json:"tvdb_id"\`` (o append `external_ids` do TMDB já devolve esse campo
  para `/tv/{id}`; para `/movie/{id}` ele vem vazio, inofensivo).
- Expor esse valor ao chamador do enrich sem inventar um canal novo:
  adicionar um método simples ao client TMDB, ex.
  `func (c *Client) TvdbIDDoDetalheSerie(raw json.RawMessage) (string, bool)`
  (unmarshal mínimo, só do campo `external_ids.tvdb_id`) — ou, se for mais
  direto, expor via um pequeno método em cima do `detalheTMDB` já decodificado
  dentro do próprio pacote tmdb e devolver o valor batendo pelo canônico
  (ver nota abaixo sobre onde plugar).
- `internal/enrich/serie.go`: no ponto em que a série tem sua árvore
  sincronizada a partir da fonte-base (`sincronizarArvoreSerie`/
  `aplicarArvoreSerie`, ou diretamente em `enriquecerBase` antes de
  `sincronizarArvoreSerie` — o `raw` do `PayloadFresco` já está disponível
  em `enriquecerBase` linha ~115), quando `source == "tmdb"` e o `raw` traz
  `tvdb_id`, mesclar em `entity.external_ids_json["tvdb"]` via uma pequena
  variante de `mesclarExternalIds` (mesmo padrão: lê o JSON atual, só
  ACRESCENTA/atualiza a chave `tvdb`, nunca mexe nas demais). Chamar isso
  **antes** de `enriquecerArtes` da série rodar (para que `idsExternosDe`
  já enxergue o `tvdb` recém-gravado na mesma passada).
- Sem migration — `external_ids_json` já é um mapa livre (MD4).

### Arquivos afetados
- `internal/enrich/enrich.go` (+ `enrich_test.go`).
- `internal/enrich/serie.go` (+ `serie_test.go`).
- `internal/provider/tmdb/tmdb_rico.go` (+ `tmdb_test.go` ou
  `fanarttv`-adjacent, conforme onde já existe cobertura do append
  `external_ids`).

## 5. Critérios de aceite

- [x] Um filme resolvido via auto-match (ou enriquecimento retroativo, ou
      "Corrigir"/"Confirmar candidato" — qualquer caminho que NÃO seja
      `POST /import`) passa a ter uma linha `media_external(source='fanarttv')`
      após o enriquecimento, idêntica ao que já acontecia via import manual.
      (`TestEnriquecerFilmeForaDoImportPersisteInventarioArtes`,
      `internal/enrich/artes_test.go`.)
- [x] O mesmo teste, repetido, não duplica a linha (idempotência preservada,
      UPSERT por `entity_id+source`). (Mesmo teste, loop de 2 chamadas +
      `lerInventario` `:one`.)
- [x] Import manual (`POST /import`) continua dessa consulta ao fanart.tv
      **exatamente uma vez** (sem dupla-chamada introduzida pelo fix da
      causa 1). (`TestImportarFilmeNaoDuplicaChamadaDeArtes`,
      `internal/enrich/artes_test.go`.)
- [x] Uma série cuja árvore vem só do TMDB (sem TVDB configurado) passa a
      ter `tvdb` em `entity.external_ids_json` após o enriquecimento, com o
      valor batendo o `tvdb_id` do append `external_ids` do TMDB.
      (`TestEnriquecerSerieSemTvdbCapturaCrossId`,
      `internal/enrich/serie_tvdb_cruzado_test.go`; extração pura coberta por
      `TestTvdbIDDoDetalheSerieDoEnvelope`/`...PayloadLegado`/`...SemMatch` em
      `internal/provider/tmdb/tmdb_rico_test.go`.)
- [x] Com o `tvdb` presente, o inventário `media_external(source='fanarttv')`
      da série passa a ser gravado (hoje `rotaETabela` degrada em silêncio
      por falta de id; com o `tvdb` presente, o fluxo segue normal). (Mesmo
      `TestEnriquecerSerieSemTvdbCapturaCrossId`, mais
      `TestEnriquecerSerieSemTvdbIdempotente` para a repetição.)
- [x] Série com TVDB já configurado (fonte-base TVDB) continua funcionando
      sem regressão — o `tvdb` já vinha de `mesclarExternalIds` nesse caso;
      a mesclagem nova não deve sobrescrever com valor vazio/diferente.
      (`TestSerieComTvdbConfiguradoNaoRegride`,
      `internal/enrich/serie_tvdb_cruzado_test.go`.)
- [x] `go build ./... && go vet ./... && go test ./... -count=1` verde.
- [x] `make gate-local` passa.

## 6. Notas de implementação / decisões

- **Por que reusar `sincronizarArvore` como sinal em vez de um parâmetro
  novo?** Ele já carrega exatamente a semântica necessária ("esta chamada é
  o `Enriquecer()` público, não uma chamada interna do import") e já é
  passado em toda call site relevante — introduzir outro parâmetro booleano
  para a mesma distinção seria redundante.
- **Por que não fazer o backfill dos itens já presos neste briefing?** O
  enriquecimento retroativo (0085) já teria feito isso sozinho depois do
  deploy do fix, dentro da janela configurada
  (`retroactive_enrichment_days`, default 15) — cobrir manualmente aqui
  duplicaria esforço com um mecanismo que já existe e já é testado.
- **Por que capturar `tvdb_id` do TMDB em vez de exigir TVDB configurado?**
  Contraria diretamente o princípio 6 do `AGENTS.md`
  ("independência de provider... um provider não modelar um campo apenas o
  deixa vazio — nunca bloqueia a vertical inteira"): o dono pode ter só a
  chave TMDB e ainda assim querer artes do fanart.tv nas séries. O TMDB já
  devolve o dado de graça (mesmo append já buscado); descartá-lo era o bug.
- **Por que não estender `mesclarExternalIds` para aceitar cross-ids
  arbitrários de qualquer provider?** Seria generalizar uma necessidade que
  hoje só existe para TMDB→tvdb; abstrair antes de um segundo caso real
  concreto é especulação. Se aparecer um segundo cross-id necessário
  (ex. outro provider de arte exigindo outro id), generalizar então.

## 7. Fora deste briefing / follow-ups

- Endpoint/botão manual de "reprocessar artes agora" para não depender da
  janela do enriquecimento retroativo — só se o dono pedir depois de ver o
  fix rodando organicamente.
- Fallback de fanart.tv para TV aceitar tmdb/imdb diretamente (caso a API
  deles um dia suporte) — hoje é limitação real da API deles, não do nosso
  código.
