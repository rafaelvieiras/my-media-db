# 0041 — i18n de episódio (título/overview por idioma via `media_external`)

> O episódio passa a gravar `media_external` per-fonte (como o filme), com título e
> overview por idioma. O detalhe do episódio então traduz por `?lang=` reusando o
> resolver de 3 camadas — sem mecanismo paralelo, sem migration.

| | |
|---|---|
| **ID** | 0041 |
| **Tipo** | feature |
| **Status** | done |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Docs relacionadas** | [0013](0013-resolver-tres-camadas.md), [0019](0019-fundacao-multi-idioma-settings-resolver.md), [0020](0020-provider-multi-idioma-traducao-tags.md), [0031](0031-series-fundacao-modelo-enriquecimento.md), [0034](0034-scanner-de-series-arquivo-episodio.md) |
| **Depende de** | 0031, 0020 |

---

## 1. Objetivo

O episódio de série grava **só o título/overview escalar do idioma default** no import
(`internal/enrich/serie.go`, `acharOuCriarEpisodio`/`valorPorIdioma` descartam os demais
idiomas; o próprio comentário adia "i18n por-idioma de episódio para a Fase 3"). Por
isso trocar o idioma de leitura (`?lang=`) **não** muda o título/overview do episódio —
diferente de filmes, que traduzem. Este briefing dá ao episódio a **mesma** i18n dos
filmes: persistir os campos por idioma em `media_external` e deixar o resolver compor.

**O provider já entrega os idiomas.** O TheTVDB busca a lista de episódios **uma vez por
idioma da cadeia** no import (`tvdb_serie.go:mesclarTraducoes`; `langs` = idiomas
permitidos passados a `BuscarSerieArvore`). Os tipos do provider já são per-idioma:
`EpisodioExterno.Title`/`.Overview` são `map[string]string` (idioma→valor). Ou seja, os
dados já chegam ao enrich — que hoje os joga fora. **Nenhuma busca nova é adicionada.**

## 2. Jornada do usuário

1. O usuário troca o **idioma de leitura** (seletor global, 0021). Ao abrir um episódio,
   o **título e a sinopse** aparecem no idioma pedido quando há tradução; caem no
   fallback da cadeia (`[lang, fallback, original_language]`) e, por fim, no título
   curado default — igual a filmes.
2. Episódio sem tradução no idioma pedido → mostra o fallback, nunca vazio.

## 3. Escopo

**Dentro:**
- **Persistir `media_external` do episódio** no import, per-fonte (mesma tabela e caminho
  do filme — `UpsertMediaExternal`), com o payload carregando os mapas
  `title`/`overview` por idioma + `original_language`. Idempotente: gravar **também no
  reuse** (re-import de episódio já existente hoje retorna cedo sem reescrever).
- **Mapear o payload de episódio no provider** (`MapearDetalhe`) para emitir
  `{title: map[idioma], description: map[idioma], original_language}`, distinguindo-o do
  payload de série/filme da MESMA fonte (`source="tvdb"`) por um **discriminador** no
  JSON (ex.: `entity_kind:"episode"`), sem quebrar o mapeamento existente de tv/movie.
- **Detalhe do episódio traduz**: `GET /entities/{epId}?lang=X` passa a resolver
  `title`/`description` pela cadeia de idiomas (já são campos `traduzivel` no resolver;
  `resolverEntity` já roda para episódio). **Nenhuma mudança em `resolver.go`.**

**Fora (explicitamente):**
- **Tradução do TÍTULO NO CARD** da lista/membros: cards leem `entity.title` escalar
  (default curado) — **é o comportamento atual dos filmes** (a home não retraduz cards ao
  trocar idioma; só o detalhe). Manter a paridade; card do episódio segue escalar. (A
  numeração E05 do 0039 continua vindo do numbering, independente de idioma.)
- **Migration de schema** — `media_external` já é chaveada por `(entity_id, source)` e
  `entity_id` aceita episódio; nada a criar.
- **i18n de nome de TEMPORADA** — temporadas são "Temporada N" sintéticas; sem valor.
- **Buscar novos idiomas** além dos permitidos configurados (mesma limitação do filme:
  re-import com `allowed` novo re-puxa).
- **Traduzir episódios já importados sem re-import** — a população de `media_external`
  acontece no (re)import; itens antigos precisam de um re-enrich (documentar no smoke).

## 4. Contrato técnico

### Backend (Go)
- **`internal/enrich/serie.go`** (`acharOuCriarEpisodio` e o caminho de reuse):
  - Após criar/achar o episódio, montar o **envelope de episódio** com os mapas
    `Title`/`Overview` per-idioma (crus do `EpisodioExterno`) + `original_language` +
    discriminador `entity_kind:"episode"`, e chamar `UpsertMediaExternal(entity=epID,
    source=<fonte>, source_id=ep.SourceID, payload_json=envelope)` — a MESMA query do
    filme (`internal/db/queries/media_external.sql`). Idempotente no reuse.
  - Manter `entity.title`/`description` escalares (default curado) como base/fallback —
    igual ao filme. `valorPorIdioma` continua definindo o escalar; o per-idioma vai no
    `media_external`.
  - Seguir o precedente do import de filme (`internal/enrich/enrich.go:persistir` →
    `UpsertMediaExternal`) e, se a SÉRIE/temporada já gravar `media_external` por algum
    caminho, espelhá-lo. Confirme na implementação e reuse o mesmo helper (não duplicar o
    marshaling do envelope).
- **`internal/provider/tvdb/tvdb_detalhe.go`** (`MapearDetalhe`, e o tipo de retorno
  consumido por `camadasDaFonte`): reconhecer o envelope com `entity_kind:"episode"` e
  devolver `title`/`description` como `map[idioma]any` + `original_language`. O ramo de
  série/filme atual permanece intocado (o discriminador decide o ramo). Cobrir o payload
  sem discriminador (episódios legados, se houver) degradando ao escalar — sem panic.
- **Resolver/handlers**: nenhuma mudança. `resolverEntity(ctx, id, lang)` já monta a
  cadeia (`cadeiaDeIdiomas(lang, ent.OriginalLanguage)`) e `camadasDaFonte` já chama
  `MapearDetalhe`. Verificar que o episódio, uma vez com `media_external`, aparece com
  `Externo` não-vazio nas camadas e traduz. **Denormalizar `entity.original_language` do
  episódio** (a série já o conhece) é **opcional** — sem isso a cadeia funciona como
  `[lang, fallback]`; fazer só se for barato no import.

### Frontend (Vite/React)
- **Nada novo esperado**: a tela de detalhe do episódio já usa `resolverEntity` com o
  `lang` do seletor (0021) e a query já inclui `lang` na chave. Ao trocar o idioma, o
  título/sinopse do episódio devem atualizar. **Confirmar** com teste/smoke; só ajustar
  se algo específico do episódio não repassar o `lang` (não previsto).

### OpenAPI
- Sem mudança de contrato (title/description já existem no `ResolvedEntity`; a origem
  passa a ter tradução). Se algum teste de contrato exigir, ajustar mínimo.

### Arquivos afetados (previsão)
- `internal/enrich/serie.go` (upsert media_external do episódio, idempotente),
  `internal/provider/tvdb/tvdb_detalhe.go` (ramo de episódio em MapearDetalhe),
  possível helper de envelope compartilhado. **Sem migration, sem resolver.go.**
- Testes: Go do enrich (episódio grava media_external per-idioma; reuse reescreve),
  do provider (MapearDetalhe de episódio → maps + original_language; payload sem
  discriminador degrada), e do server (detalhe do episódio traduz por lang, fallback).

## 5. Critérios de aceite

- [ ] Importar/enriquecer uma série grava uma linha `media_external` por episódio, com
      `title`/`overview` por idioma no `payload_json` (idiomas = permitidos buscados).
- [ ] `GET /entities/{epId}?lang=<principal>` e `?lang=<fallback>` retornam
      título/sinopse **traduzidos** quando há tradução; caem no fallback/curado quando
      não há (nunca vazio).
- [ ] Re-import do mesmo episódio **reescreve** o `media_external` (idempotente).
- [ ] O ramo de tv/filme de `MapearDetalhe` segue intacto (discriminador isola o
      episódio); payload legado sem discriminador não quebra.
- [ ] Card de episódio no /members segue com o título escalar default (paridade com
      filme) — sem regressão do número E05 (0039).
- [ ] Catraca completa verde (Go cov, e2e, tsc 0, web cov, jscpd, gocyclo ≤10).
- [ ] Smoke real: re-enrich de uma série; abrir um episódio e trocar o idioma de leitura
      → título/sinopse mudam; idioma sem tradução → fallback.

## 6. Notas de implementação / decisões

- **Reusar `media_external` + resolver, sem mecanismo paralelo** (decisão da pesquisa):
  o per-idioma de filme já vive no `payload_json` de `media_external` e o resolver já
  compõe `title`/`description` como `traduzivel`. O episódio entra nesse mesmo trilho —
  custo mínimo, zero duplicação de lógica de idioma. (Guardar os mapas em
  `extension_data core.tv` foi **rejeitado**: o resolver não leria de lá.)
- **Discriminador no payload** resolve a colisão de shape série×episódio na mesma fonte
  (`tvdb`) dentro de `MapearDetalhe`.
- **Card escalar, detalhe traduzido**: paridade deliberada com filmes; evita retraduzir
  listas inteiras (custo/*N*) e mantém o comportamento coerente em todo o app.
- **Sem buscas novas**: o provider já puxa por idioma no import; só paramos de descartar.

## 7. Fora deste briefing / follow-ups

- Tradução de título no **card** de listas (se algum dia se quiser retraduzir grids) —
  vale para filme e episódio juntos, é outra decisão de produto.
- **0042** — still de episódio (arte), o último item da Fase 3.
- Denormalização de `original_language` por episódio, se a cadeia precisar do idioma
  original com frequência.
- Re-enrich em massa de episódios legados (migração de dados, se desejado) para popular
  `media_external` sem re-import manual.
