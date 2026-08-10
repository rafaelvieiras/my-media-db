# 0091 — Similares: troca `/similar` por `/recommendations`, filtra ruído e abre capacidade `SimilarProvider`

> Hoje a seção "Similares" do detalhe (filme/série) é repasse cru do endpoint
> `/similar` do TMDB, sem nenhum filtro. Reportado ao vivo pelo dono com "A
> Arte da Guerra" (2000): a lista trazia títulos sem relação alguma de tema/
> tom (e ocasionalmente softcore), porque `/similar` do TMDB é um matching
> genérico por gênero/palavra-chave, sem sinal de qualidade. Este briefing
> troca a fonte por `/recommendations` (sinal de comportamento, mais
> relevante), filtra itens claramente fora do padrão aceitável (adulto, nota
> irrelevante) e ordena pelo que sobra por afinidade real com o item de
> origem — abrindo, de quebra, o ponto de plugabilidade para uma fonte de
> similares diferente do TMDB no futuro.

| | |
|---|---|
| **ID** | 0091 |
| **Tipo** | fix/feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md) |
| **Depende de** | 0022, 0023, 0027 |

---

## 1. Objetivo

O TMDB expõe dois endpoints distintos para "itens parecidos": `/similar`
(correlação por gênero/palavra-chave, ruidosa) e `/recommendations` (baseada
em comportamento de quem consumiu os dois títulos, sensivelmente mais
relevante — é o mesmo sinal que alimenta a home do próprio TMDB). O projeto
usa `/similar` desde o briefing original e nunca filtrou o resultado: tudo
que a fonte manda, o card exibe. Na prática isso produz uma lista com
títulos sem relação temática nenhuma com a origem, incluindo eventualmente
conteúdo adulto/softcore — quebra de confiança na seção pro dono, que é a
própria autoridade do catálogo (princípio 1 do `AGENTS.md`).

Este briefing:
1. Troca a fonte de `/similar` para `/recommendations`.
2. Passa a capturar os campos de qualidade que o TMDB já devolve nesse
   payload e hoje são descartados no unmarshal (`vote_average`, `vote_count`,
   `genre_ids`, `adult`).
3. Filtra fora itens adultos e itens com poucos votos (ruído estatístico).
4. Ordena o que sobra por afinidade real com o item de origem (sobreposição
   de gênero, usando os IDs numéricos do próprio TMDB — não os nomes de tag
   já traduzidos/resolvidos, que podem divergir por idioma/fonte).
5. Formaliza isso como uma CAPACIDADE opcional de provider
   (`provider.SimilarProvider`), seguindo o padrão de plugabilidade do
   projeto (`ColecaoProvider`, `ArteProvider`, `TvdbCruzadoProvider`) — hoje
   só o TMDB implementa, mas o ponto de extensão fica pronto para outra
   fonte de recomendação no futuro, sem exigir mudança em quem consome.

## 2. Jornada do usuário

1. O dono abre o detalhe de um filme ou série já catalogado (ou o detalhe
   externo pré-import).
2. Rola até "Similares".
3. Hoje: vê até ~20 títulos na ordem crua que o TMDB `/similar` devolveu,
   incluindo itens sem relação de tema/tom/qualidade com a origem.
4. Depois deste fix: vê uma lista menor (só o que passou no filtro),
   ordenada dos mais afins pros menos afins, sem itens adultos e sem itens
   de nota/volume de voto irrelevantes. Nenhuma mudança visual ou de
   interação é necessária — mesmo componente, mesmo formato de card, só a
   lista por trás muda.
5. Se o filtro zerar a lista (item de nicho, sem recomendações confiáveis),
   a seção "Similares" simplesmente não aparece — mesmo comportamento de
   hoje quando a fonte não traz nada.

## 3. Escopo

**Dentro:**
- Trocar `similar` por `recommendations` no `append_to_response` do TMDB,
  tanto para filme (`internal/provider/tmdb/tmdb.go:212`) quanto para série
  (`internal/provider/tmdb/tmdb_serie_detalhe.go:74`).
- Capturar `genre_ids`, `vote_average`, `vote_count`, `adult` no payload de
  cada item recomendado (hoje só `id/title/name/release_date/
  first_air_date/poster_path` são decodificados).
- Filtrar: descartar item com `adult == true`; descartar item com
  `vote_count < 20`.
- Ordenar o resultado filtrado por: (1) nº de gêneros em comum com o item de
  ORIGEM (desc — usando os `genre_ids`/`genres` do próprio payload TMDB, IDs
  numéricos estáveis, nunca os nomes de tag já resolvidos/traduzidos do
  item, que podem vir de outra fonte/idioma e não bater); (2) `vote_average`
  (desc) como desempate.
- Criar a capacidade `provider.SimilarProvider` (novo tipo `provider.Similar`
  no canônico) e implementá-la no client TMDB, chamada explicitamente pelo
  ponto que hoje já invoca `MapearDetalhe` por fonte
  (`internal/server/entities.go` e `internal/server/external_detail.go`),
  via type-assert — mesmo padrão de `ColecaoProvider`/`ArteProvider`. O
  `MapearDetalhe` do TMDB deixa de preencher a chave `similar` sozinho (essa
  responsabilidade migra pra `SimilarProvider.Similares`, chamada
  externamente, sem duplicar a lógica em dois lugares).
- Cobrir com teste de tabela: filtro de adulto, filtro de vote_count baixo,
  ordenação por sobreposição de gênero, degradação graciosa quando o
  provider não implementa `SimilarProvider`.
- Aplica-se aos dois caminhos que já usam o mesmo `raw`/`MapearDetalhe`: item
  já catalogado (`GET /entities/{id}`) e detalhe externo pré-import
  (`GET /external/{source}/{type}/{id}`).

**Fora (explicitamente):**
- Qualquer mudança no formato do card/DTO de similar exposto na API
  (`source_id/type/title/year/poster_url`) — os campos novos
  (`vote_average`, `genre_ids`, `adult`, `vote_count`) são usados só
  internamente para filtrar/ordenar, não são expostos ao frontend.
- Qualquer mudança em `web/` — o componente `SimilaresRail`
  (`rich-detail-view.tsx`) continua recebendo a mesma lista no mesmo
  formato, só que já filtrada/ordenada; nada muda na renderização.
- Tornar o limiar de `vote_count`/o critério de ordenação configurável pelo
  dono (setting) — fica hardcoded no provider por ora; vira follow-up se o
  dono achar o limiar errado na prática.
- Cortar/paginar a quantidade de itens exibidos além do que a fonte já
  devolve — sem introduzir truncamento novo (comportamento atual mantido:
  o que sobrar do filtro é exibido inteiro).
- Implementar `SimilarProvider` no TVDB (não modela recomendação; permanece
  sem essa capacidade, degradação graciosa já cobre esse caso) ou em
  qualquer outro provider — só o TMDB implementa nesta entrega.
- Scoring usando os `tags` já resolvidos da entity (nomes traduzidos) — como
  investigado, isso arriscaria mismatch de idioma/taxonomia entre fontes; o
  overlap de gênero usa exclusivamente os IDs crus do TMDB, do MESMO
  payload, sem depender do resolver.

## 4. Contrato técnico

### Backend (Go)

**`internal/provider/provider.go` — nova capacidade:**
```go
// Similar é um item similar/recomendado canônico (briefing 0091), já
// filtrado (sem adulto, sem volume de voto irrelevante) e pronto pra
// exibição — o consumidor não reprocessa nota/gênero/adulto.
type Similar struct {
    SourceID  string
    Type      string
    Title     string
    Year      *string
    PosterURL *string
}

// SimilarProvider é uma CAPACIDADE OPCIONAL de um Provider (briefing 0091):
// extrai os itens SIMILARES/recomendados a partir do payload de detalhe JÁ
// buscado por BuscarDetalhe (sem ida extra à rede — mesmo padrão de
// TvdbCruzadoProvider), já filtrados por qualidade e ORDENADOS por
// afinidade com o item de origem. É opcional porque nem toda fonte modela
// recomendação; o handler faz type-assert do Provider para esta interface e
// degrada para lista vazia (sem erro) quando ausente.
type SimilarProvider interface {
    Similares(raw json.RawMessage) ([]Similar, error)
}
```

**`internal/provider/tmdb/tmdb.go`:**
- `append_to_response` de filme (linha ~212) e de série
  (`tmdb_serie_detalhe.go:74`): trocar o valor `similar` por
  `recommendations` na string concatenada.
- Struct `detalheTMDB` (linha ~415): renomear/trocar o campo `Similar
  similarTMDB \`json:"similar"\`` para `Recommendations
  recommendationsTMDB \`json:"recommendations"\``.

**`internal/provider/tmdb/tmdb_rico.go`:**
- Renomear `similarTMDB`→`recommendationsTMDB` e `itemSimilarTMDB`→
  `itemRecommendationTMDB` (linhas ~116-130), acrescentando:
  ```go
  type itemRecommendationTMDB struct {
      ID           int64         `json:"id"`
      Title        string        `json:"title"`
      Name         string        `json:"name"`
      ReleaseDate  string        `json:"release_date"`
      FirstAirDate string        `json:"first_air_date"`
      PosterPath   string        `json:"poster_path"`
      GenreIds     []int64       `json:"genre_ids"`
      VoteAverage  float64       `json:"vote_average"`
      VoteCount    int           `json:"vote_count"`
      Adult        bool          `json:"adult"`
  }
  ```
- `similaresCanonicos(d detalheTMDB)` (linhas ~309-330) deixa de ser
  chamada de dentro de `enriquecerNeutrosRico` (remover a chamada das linhas
  ~174-175) — essa função migra de "preenche uma chave do mapa canônico" pra
  "implementação de `Similares`", descrita abaixo. Mantém a assinatura
  interna igual (recebe `detalheTMDB` já decodificado), só muda quem chama.
- Nova função de filtro/ordenação (nome sugerido `similaresFiltrados`),
  aplicada sobre `d.Recommendations.Results` ANTES da conversão canônica:
  1. Descarta `Adult == true`.
  2. Descarta `VoteCount < 20`.
  3. Calcula sobreposição de gênero: `len(intersecao(item.GenreIds,
     genrosOrigemIDs(d.Genres)))` — `d.Genres` é o mesmo campo já existente
     (`tmdb.go:400`, `[]generoTMDB` com `ID`/`Name`) usado hoje pra montar
     as tags do item de ORIGEM; usar só os `ID`s.
  4. Ordena por `(overlap desc, VoteAverage desc)`.
  5. Converte o resultado ordenado pro canônico (`provider.Similar`), no
     mesmo formato de hoje (descarta item sem título, mesma regra atual).
- Novo método exportado no client, implementando a interface:
  ```go
  func (c *Client) Similares(raw json.RawMessage) ([]provider.Similar, error) {
      // decodifica raw (mesmo envelope que mapearEnvelope já entende),
      // aplica similaresFiltrados, devolve o canônico.
  }
  ```

**`internal/server/entities.go` / `internal/server/external_detail.go`:**
- No ponto em que cada camada já chama `prov.MapearDetalhe(raw)` por fonte
  (`entities.go` ~999-1037 / `external_detail.go`, montagem do detalhe
  externo), acrescentar: `if sp, ok := prov.(provider.SimilarProvider);
  ok { similares, err := sp.Similares(raw); ... }` — em caso de sucesso,
  grava/sobrescreve a chave `similar` daquela camada com o resultado
  (mesmo formato que `richfields.SimilaresDe` já espera, ver
  `internal/richfields/richfields.go:189-206`); em erro, loga e segue sem
  similares dessa camada (degradação graciosa, nunca derruba o resto do
  detalhe — mesmo padrão de `TvdbCruzadoProvider`/`ArteProvider`). Sem
  `SimilarProvider` implementado pela fonte, a chave `similar` simplesmente
  não é preenchida (comportamento idêntico a hoje quando a fonte não tem o
  campo).

### Arquivos afetados
- `internal/provider/provider.go` (nova capacidade `SimilarProvider` +
  tipo `Similar`).
- `internal/provider/tmdb/tmdb.go` (append_to_response, struct
  `detalheTMDB`).
- `internal/provider/tmdb/tmdb_serie_detalhe.go` (append_to_response).
- `internal/provider/tmdb/tmdb_rico.go` (+ `tmdb_rico_test.go`): filtro,
  ordenação, novo método `Similares`.
- `internal/server/entities.go` (+ teste correspondente): type-assert e
  chamada de `SimilarProvider`.
- `internal/server/external_detail.go` (+ teste correspondente): idem, para
  o detalhe externo pré-import.

## 5. Critérios de aceite

- [x] O `append_to_response` de filme e de série passa a pedir
      `recommendations` em vez de `similar`.
- [x] Um item recomendado com `adult == true` nunca aparece na lista
      canônica de similares. (`TestSimilaresFiltraAdultoEVotoBaixo`,
      `internal/provider/tmdb/tmdb_rico_test.go`.)
- [x] Um item recomendado com `vote_count < 20` nunca aparece na lista
      canônica de similares. (Mesmo teste acima.)
- [x] Dados dois itens recomendados com o mesmo `vote_count` acima do
      limiar, o que tem mais gêneros em comum com o item de origem aparece
      antes na lista. (`TestSimilaresOrdenaPorSobreposicaoDeGenero`,
      `TestSimilaresDesempataPorNota`.)
- [x] Um provider que NÃO implementa `SimilarProvider` (ex.: um fake de
      teste) resulta em detalhe sem a chave `similar` — sem erro, sem 500.
      (`TestEntitySimilaresSemSimilarProvider`,
      `TestExternalDetailSimilaresSemSimilarProvider`,
      `internal/server/similar_test.go`.)
- [x] `GET /entities/{id}` e `GET /external/tmdb/{type}/{id}` continuam
      devolvendo o campo `similar` no MESMO formato de hoje
      (`source_id/type/title/year/poster_url`) — sem quebra de contrato de
      API, sem mudança necessária no frontend.
      (`TestEntitySimilaresComSimilarProvider`,
      `TestExternalDetailSimilaresComSimilarProvider`.)
- [x] `go build ./... && go vet ./... && go test ./... -count=1` verde.
- [x] `make gate-local` passa.

## 6. Notas de implementação / decisões

- **Por que `/recommendations` em vez de só filtrar `/similar`?** `/similar`
  do TMDB é documentadamente um matching por gênero/palavra-chave, sem
  sinal de comportamento; mesmo filtrando adulto/nota, a base já é fraca.
  `/recommendations` é o mesmo sinal que o próprio TMDB usa pra "quem viu
  isso também viu" — ataca a causa, não só o sintoma.
- **Por que `vote_count >= 20` e não um piso de `vote_average`?** Nota
  média com poucos votos é estatisticamente não-confiável (um título com 2
  votos e nota 8 não é "melhor" que um com 500 votos e nota 6.5); volume de
  voto é um proxy melhor de "a recomendação é de um título real, conhecido,
  não ruído" do que a nota em si. Não há piso de nota — mesmo um filme com
  nota mediana é uma recomendação válida se for do gênero certo e
  amplamente assistido.
- **Por que overlap de gênero via ID numérico do TMDB, não via
  `Entidade.Tags` resolvida?** Investigado: `Entidade.Tags` é resolvida
  pela cadeia de idioma do PEDIDO (`resolver.go:846-869`) e pode vir de
  fonte diferente do TMDB (TVDB tem taxonomia própria) ou em idioma
  diferente do que o payload de recomendação está sendo processado — nomes
  de string arriscam mismatch silencioso. Os `genre_ids` do item
  recomendado e os `d.Genres[].ID` do item de origem vêm do MESMO payload,
  mesma fonte, mesmo espaço de IDs — comparação exata, sem ambiguidade.
- **Por que `SimilarProvider` é chamado de fora (`entities.go`/
  `external_detail.go`) em vez de o próprio `MapearDetalhe` do TMDB chamar
  `c.Similares` internamente?** Pra a capacidade ser um ponto de extensão
  DE VERDADE (não só uma interface decorativa): seguindo o mesmo padrão de
  `ColecaoProvider`/`ArteProvider`/`TvdbCruzadoProvider`, é o chamador
  externo que faz o type-assert — assim, se um dia OUTRO provider
  implementar `SimilarProvider`, ele é escolhido automaticamente sem tocar
  no TMDB nem no chamador. Custo: dois pontos de chamada a mais (
  `entities.go`, `external_detail.go`) em vez de um só dentro do TMDB —
  aceitável, é exatamente o padrão que as outras capacidades já pagam.
- **Por que não expor `vote_average`/`genre_ids`/`adult` na API?** Não há
  necessidade de UI hoje (o pedido do dono era "tirar o lixo da lista", não
  "mostrar nota"); manter o DTO igual evita qualquer mudança de frontend
  neste briefing. Se o dono quiser mostrar nota/gênero no card de similar
  depois, é um follow-up pequeno e isolado (o dado já vai estar disponível
  no backend).
- **Por que não tornar o limiar configurável?** Não há indício de que o
  dono precise ajustar isso por biblioteca/gosto — é uma correção de
  qualidade objetiva (excluir ruído estatístico e conteúdo adulto), não uma
  preferência. Vira setting só se a prática mostrar necessidade real.

## 7. Fora deste briefing / follow-ups

- Expor nota/gênero em comum no card de "Similares" da UI (dado já
  disponível no backend após este briefing, se quiser puxar depois).
- Tornar o limiar de `vote_count` configurável via painel.
- Um segundo `SimilarProvider` (ex.: uma fonte de recomendação própria
  baseada no que o dono já tem catalogado) — sem caso de uso concreto
  hoje; a capacidade fica pronta para quando/se aparecer.
