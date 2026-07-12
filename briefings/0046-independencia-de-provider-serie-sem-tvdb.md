# 0046 — Independência de provider: série sem TVDB (TMDB como provedor de árvore)

> Garantir que uma série sobreviva com **qualquer** provider registrado — hoje a árvore de temporadas/episódios está acoplada exclusivamente ao TheTVDB.

| | |
|---|---|
| **ID** | 0046 |
| **Tipo** | feature/fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (séries) |
| **Status** | ready |
| **Docs relacionadas** | [ADR-0021 séries](../docs/adr/0021-modelo-de-series.md), [09-providers](../docs/09-providers.md) |
| **Depende de** | 0031, 0045 |

---

## 1. Objetivo

O princípio do MyMediaDB é **multi-provider**: se o admin cadastrou só a chave
do TMDB, ele deve pesquisar **e importar** filmes **e séries** do TMDB; se
cadastrou só o TheTVDB, idem pelo TVDB. Um provider ter campos que o outro não
tem apenas deixa aqueles campos vazios — **não** pode impedir o acesso à
vertical inteira.

Hoje isso **não** vale para séries. A auditoria confirmou:

- Só **TVDB** implementa `provider.SerieProvider` (a árvore) —
  `internal/provider/tvdb/tvdb_serie.go:27`. O **TMDB não** implementa
  `BuscarSerieArvore` (só `SerieRicoProvider`, enriquecimento).
- Com **só TMDB** registrado: a busca até mostra a série (`/search/multi`
  retorna `tv`), mas o **import falha** — `importarSerie` faz
  `prov.(provider.SerieProvider)` sobre a fonte `"tmdb"`, não implementa, e
  retorna `"enrich: fonte %q nao suporta serie"`
  (`internal/enrich/serie.go:58-60`) → **502** (`internal/server/enrich.go:124`).
  Pelo scanner é pior: `ImportarSeriePorNome` **pula** silenciosamente qualquer
  provider sem `SerieProvider` (`serie.go:174`) → `ErrSerieNaoEncontrada`.
- O cruzamento rico é **hardcoded** na direção TVDB→TMDB: `idTvdbDaSerie`
  assume `source=="tvdb"` (`serie.go:145-150`) e `enriquecerSerieRico` faz
  `providers.Get("tmdb")` fixo (`serie.go:101`).

Resultado: **sem TheTVDB, o usuário não importa séries.** Viola a
provider-independência exatamente na vertical que o usuário apontou.

## 2. Jornada do usuário

1. Admin cadastra **apenas** a chave do TMDB (sem TheTVDB).
2. Pesquisa "The Simpsons"; a série aparece nos resultados externos.
3. Clica em importar. **A série é importada com árvore de temporadas e
   episódios montada a partir do TMDB** (`/tv/{id}` + `/tv/{id}/season/{n}`).
4. Campos que o TMDB não modela ficam vazios, sem erro.
5. Se depois o admin cadastrar o TheTVDB, um re-enrich pode **cruzar** e
   preencher o que faltava — em qualquer direção (a árvore veio do TMDB, o TVDB
   entra como camada de enriquecimento; e vice-versa).

## 3. Escopo

**Dentro:**
- Implementar `provider.SerieProvider` no pacote **tmdb**: `BuscarSerieArvore`
  montando temporadas e episódios a partir de `/tv/{id}?append=...` e
  `/tv/{id}/season/{n}` (numbering official; still via `still_path`; i18n de
  título/overview por idioma como já feito no TVDB).
- **Generalizar o cruzamento rico** para ser bidirecional / agnóstico de fonte:
  a árvore vem de *qualquer* `SerieProvider`; o enriquecimento cruzado escolhe
  *o outro* provider disponível (não `Get("tmdb")` fixo, não `source=="tvdb"`
  fixo). Extrair o id externo da fonte-base de forma genérica.
- Seleção de provider de árvore por **prioridade + disponibilidade** (o primeiro
  `SerieProvider` registrado que responde), não por nome fixo.
- Fallback gracioso: um único provider registrado → só ele; sem 502 por assumir
  o outro.

**Fora (explicitamente):**
- Fusão campo-a-campo de **árvores divergentes** entre TVDB e TMDB (matching de
  episódios com numbering diferente). Aqui a árvore tem **uma** fonte-base; o
  outro provider entra só como enriquecimento do nó série (como no 0045). O
  merge de árvores cruzadas fica para follow-up.
- Configurar chaves pela UI (continua env-only).

## 4. Contrato técnico

### Backend (Go)
- `internal/provider/tmdb/tmdb_serie.go` (novo): `BuscarSerieArvore(ctx, id,
  langs)` → `provider.SerieArvore` (mesmo shape que o TVDB devolve). Reusar o
  cliente/append já existente em `tmdb_serie_detalhe.go`.
- `internal/provider/provider.go`: nenhuma interface nova — TMDB passa a
  satisfazer `SerieProvider` também. Adicionar asserção de compilação
  `var _ SerieProvider = (*tmdb.Client)(nil)` no teste.
- `internal/enrich/serie.go`:
  - `importarSerie`: escolher o `SerieProvider` pela fonte selecionada; se a
    fonte pedida não é `SerieProvider` mas **outro** provider registrado é,
    decidir política (erro claro vs. cair para o disponível). Documentar.
  - `enriquecerSerieRico`: trocar `Get("tmdb")` e `source=="tvdb"` por uma
    resolução genérica: "o provider da árvore é X; o enriquecedor rico é o
    primeiro *outro* provider que implemente `SerieRicoProvider`". Extrair
    `idExternoDaSerie(source)` genérico em vez de `idTvdbDaSerie`.
- Registro: nenhuma mudança em `cmd/mymediadb/main.go` além de garantir que
  TMDB entra na lista de candidatos a árvore.

### Frontend (Vite/React)
- Nenhuma mudança obrigatória: a UI já é agnóstica de fonte. Validar que o
  detalhe de série renderiza igual vindo do TMDB (still, numbering, i18n).

### Arquivos afetados
- `internal/provider/tmdb/tmdb_serie.go` (novo)
- `internal/provider/tmdb/tmdb.go` (helpers de mapeamento de temporada/episódio)
- `internal/enrich/serie.go` (generalizar seleção e cruzamento)
- `internal/provider/tmdb/tmdb_serie_test.go` (novo)
- `internal/enrich/serie_rico_test.go` (cenário só-TMDB e cruzamento invertido)

## 5. Critérios de aceite

- [ ] Com **só TMDB** registrado, `POST /import` de uma série retorna 2xx e
      persiste árvore com ≥1 temporada e episódios (numbering + still).
- [ ] Com **só TMDB**, o scanner (`ImportarSeriePorNome`) encontra e importa a
      série (não cai em `ErrSerieNaoEncontrada`).
- [ ] Com **só TVDB** registrado, comportamento atual preservado (regressão
      zero: árvore TVDB + sem enriquecimento rico).
- [ ] Com **ambos**, a árvore vem da fonte-base e o **outro** provider enriquece
      o nó série; nenhum `Get("tmdb")`/`source=="tvdb"` hardcoded no caminho.
- [ ] `var _ provider.SerieProvider = (*tmdb.Client)(nil)` compila.
- [ ] `go build ./... && go vet ./... && go test ./... -count=1` verdes; catraca
      local passa.

## 6. Notas de implementação / decisões

- **Uma fonte-base para a árvore**, o resto é enriquecimento (mantém o modelo do
  0045 e evita o problema difícil de casar episódios com numbering divergente).
  A escolha da fonte-base = prioridade do registry entre os `SerieProvider`
  disponíveis.
- O TMDB **tem** episódios e stills por temporada — a árvore é viável sem API
  nova. O que faltava era só **implementar** `SerieProvider` no tmdb; a decisão
  histórica "TVDB é a fonte de séries no MVP" (`tvdb`-only) era de MVP, não
  princípio.

## 7. Fora deste briefing / follow-ups

- Merge de **árvores** entre dois providers (matching de episódios divergentes).
- Escolha de fonte-base de árvore pelo usuário (preferência por série).
- Ver [0047](0047-robustez-de-fonte-default-filme-e-serie.md) (defaults de fonte)
  e a fundação de providers de arte/trailer (fanart.tv) — auditada como pronta.
