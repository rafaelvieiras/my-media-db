# 0048 — Fundação de providers de enriquecimento por capacidade (arte, trailer, tema)

> Preparar os poucos pontos de extensão que faltam para plugar providers
> especializados — fanart.tv (arte), trailer e tema musical — no mesmo padrão
> de capacidade que TVDB/TMDB já usam.

| | |
|---|---|
| **ID** | 0048 |
| **Tipo** | feature (fundação) |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | draft |
| **Docs relacionadas** | [09-providers](../docs/09-providers.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md) |
| **Depende de** | 0044 (artes), 0045 (enriquecimento híbrido) |

---

## 1. Objetivo

A auditoria de arquitetura confirmou que a fundação **já aguenta** múltiplos
providers que se enriquecem, sem reforma:

- Interface **orientada a capacidade** (`Provider` core +
  `ColecaoProvider`/`SerieProvider`/`SerieRicoProvider` opcionais via
  type-assert) — `internal/provider/provider.go:26-121`.
- **Registry N-providers** com ordem = prioridade — `registry.go:12-69`.
- **Camada de merge** (resolver) que compõe por classe de campo e por namespace
  — `internal/resolver/resolver.go:445,457-470,759-774`.
- **Precedente de camada cruzada**: `enriquecerSerieRico` grava uma linha
  `media_external source="tmdb"` num nó que veio do TVDB (`serie.go:141-154`) —
  é exatamente o shape que fanart.tv usaria (`source="fanarttv"`, só artes).
- Tabela `asset` é **kind-agnóstica** no banco (`kind TEXT` sem enum,
  `0002_schema_nucleo.sql:94-102`) → um provider de arte pode gravar
  `clearlogo`, `cdart`, `banner`, etc. sem migration.
- Chave de API de novo provider = env var + `Registrar` (`main.go:173-187`),
  sem mudança estrutural.

Faltam só **preparos pontuais**. Este briefing os entrega para desbloquear os
providers concretos (que virão em briefings próprios).

## 2. Escopo

**Dentro:**
1. **Interface `ArteProvider`** em `provider.go` (análoga a `SerieRicoProvider`):
   `BuscarArtes(ctx, ids externos, langs) → []Arte{kind, url, lang?, meta}`.
   Enriquecimento cruzado por id externo, degradação silenciosa quando ausente.
2. **`cachearArtes` generalizado**: `internal/enrich/enrich.go:345` hoje só lê
   `poster_url`/`backdrop_url` fixos; passar a iterar sobre os kinds vindos de um
   `ArteProvider` e baixar cada um via `cachearArte` (`enrich.go:394-430`, já
   existe, `origin="external"`).
3. **Abrir a validação de kind na entrada HTTP**: `internal/server/assets.go:37-39`
   e `parseKind` (`:139`) rejeitam com 422 tudo fora de poster/fanart/still. Para
   o dono **gerenciar** arte estendida (subir/deletar clearlogo, banner, cdart…),
   transformar a lista fechada em um **registro de kinds** extensível.
4. **Decisão de modelagem de binário de trailer/tema** (única lacuna estrutural):
   trailer/tema hoje existem só como **metadado/link**
   (`tmdb_rico.go:21-27`, `richfields.go:24`), não como arquivo baixado. Escolher:
   - **Opção A** (sem migration): reusar `asset` com `kind="trailer"`/`"theme"`,
     `path_or_uri` apontando o arquivo baixado.
     - **Opção B**: tratar como `media_file` (arquivo de mídia de 1ª classe).
   Registrar a decisão aqui e (se estrutural) subir para `docs/04`.

**Fora (explicitamente):**
- Os providers concretos em si (fanart.tv, provider de trailer, provider de
  tema) — cada um vira um briefing próprio. Em especial, **fanart.tv exige
  levantar antes a lista de tipos de arte** que ele expõe (poster, HD clearart,
  cdart, background, banner, thumb…) para mapear aos kinds — o usuário conduzirá
  esse levantamento.
- UI de gestão dos novos kinds de arte (galeria já existe; estender depois).

## 3. Critérios de aceite

- [ ] `provider.ArteProvider` definido; um provider fake que o implemente é
      exercitado em teste, e `cachearArtes` baixa os kinds retornados.
- [ ] `parseKind` aceita os kinds registrados (não mais 3 fixos) e ainda rejeita
      kind desconhecido com 422.
- [ ] Decisão trailer/tema documentada (A ou B) com justificativa.
- [ ] `go build/vet/test -count=1` verdes; catraca local passa.

## 4. Notas / decisões

- Nada aqui é reforma: são extensões dentro dos padrões existentes. O maior
  ponto de decisão é a modelagem do **binário** de trailer/tema — o resto é
  seguir o precedente de `SerieRicoProvider` + camada `media_external` cruzada.

## 5. Follow-ups (briefings próprios)

- **fanart.tv** (provider de arte): levantar tipos de arte → mapear kinds →
  implementar `ArteProvider`.
- **Provider de trailer** (download para o disco).
- **Provider de tema musical** (download para o disco).
- **Novos esquemas via plugin** (videoclipe→artista, música→álbum): a fundação
  genérica (`entity.type` livre, `entity_relation` com `kind`, `extension_data`
  namespaced) já existe — cada esquema traz seus providers pelo mesmo Registry.
