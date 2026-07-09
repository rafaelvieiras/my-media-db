# 0020 — Provider multi-idioma + tradução de tags

> O provider passa a **puxar traduções** (TMDB `append_to_response=translations` +
> listas de gênero localizadas) e a gravar o externo **tagueado por idioma** —
> enchendo os idiomas que o resolver ([0019](0019-fundacao-multi-idioma-settings-resolver.md))
> já sabe compor.

| | |
|---|---|
| **ID** | 0020 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [ADR-0020](../docs/adr/0020-traducao-de-tags-genres-keywords.md), [ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md), [ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md) |
| **Status** | done |
| **Depende de** | [0019](0019-fundacao-multi-idioma-settings-resolver.md), [0015](0015-enriquecimento-import-proxy-imagem.md) |

---

## 1. Objetivo

Com o resolver já resolvendo por idioma ([0019](0019-fundacao-multi-idioma-settings-resolver.md)),
falta a **fonte entregar** os idiomas. O provider TMDB passa a puxar as traduções e
a mapear `raw → canônico` como `idioma → valor`, incluindo **tags traduzíveis**
([ADR-0020](../docs/adr/0020-traducao-de-tags-genres-keywords.md)). É o que faz o
`?lang=pt-BR` servir a sinopse em português e cair no inglês quando falta.

## 2. Jornada do usuário

1. O dono importa um filme; o sistema puxa **todas** as traduções permitidas numa só
   ida à fonte.
2. No detalhe, `?lang=pt-BR` mostra título/sinopse/gêneros em português; um item sem
   tradução pt-BR mostra o inglês (com a proveniência sinalizando).
3. As **tags/gêneros** aparecem traduzidas — o gap do TMDB (que não traduz keywords)
   é coberto para gêneros; keywords ficam no idioma obtido até tradução manual.

## 3. A decisão de design que sustenta tudo

**`MapearDetalhe` roda em TEMPO DE LEITURA e é PURO (sem rede)** — é chamado no
`getEntity` para mapear o payload guardado. Mas os **nomes de gênero traduzidos NÃO
vêm** no detalhe do TMDB (só os ids + nomes num idioma). Logo, a tradução dos gêneros
tem de ser **resolvida no ENRICH** (que pode fazer rede) e **gravada no payload**.

Decisão: o `media_external.payload_json` para o TMDB passa a ser um **payload
aumentado** (ainda um cache re-componível, coerente com [ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md)):

```json
{
  "detail": { …resposta crua de /{movie|tv}/{id}?append_to_response=translations… },
  "tags_by_lang": { "pt-BR": ["Ficção científica","Drama"], "en": ["Science Fiction","Drama"] }
}
```

- `detail` traz as **traduções de título/sinopse/tagline** (via `append_to_response`)
  — todas de uma **só chamada**.
- `tags_by_lang` é **computado no enrich** a partir das listas de gênero localizadas
  (uma por idioma permitido, cacheadas), mapeando os ids de gênero do item.
- `MapearDetalhe` lê esse envelope **sem rede**. **Compatibilidade:** um payload
  ANTIGO (detalhe cru, sem envelope, gravado pelo 0015) é detectado e tratado como
  idioma **principal** (a tolerância que o [0019](0019-fundacao-multi-idioma-settings-resolver.md)
  já implementou no resolver — aqui o mapper faz o equivalente).

## 4. Contrato técnico

### Provider (`internal/provider` + `tmdb`)
- **`BuscarDetalhe(ctx, sourceID, tipo string, langs []string) (json.RawMessage, error)`**
  (assinatura estendida com os idiomas permitidos): faz `GET /{movie|tv}/{id}?append_to_response=translations`
  (idioma base = primeiro de `langs`), busca/reusa as **listas de gênero por idioma**
  (`GET /genre/{movie|tv}/list?language=`, cache em memória com mutex), monta e
  devolve o **payload aumentado** (`{detail, tags_by_lang}`). Sem `langs` → degrada
  ao comportamento do 0015 (só o principal, envelope com um idioma).
- **`MapearDetalhe(raw) (map[string]any, error)`** (assinatura estável, PURA): lê o
  envelope e entrega o canônico:
  - `title`, `description` como **`map[idioma]valor`** (das `detail.translations` +
    o topo do detalhe sob o `original_language`/principal);
  - `tags` como **`map[idioma][]string`** (de `tags_by_lang`);
  - `original_title`, `release_date`, `poster_url` **neutros** (escalares);
  - `original_language` (do detalhe) para o enrich gravar em `entity.original_language`.
  - Payload legado (sem envelope) → escalares (idioma principal), como o 0015.
- **Códigos de idioma:** o TMDB usa `iso_639_1`(+`iso_3166_1`) — combinar para casar
  `pt-BR`/`en`. Documentar o mapeamento; verificar shapes do TMDB por ctx7 se preciso.

### Enrich (`internal/enrich`) + handlers
- `Enriquecer`/`Importar` ganham `langs []string` (os **permitidos** das settings); o
  handler os lê (helper de settings do [0019](0019-fundacao-multi-idioma-settings-resolver.md))
  e passa. O enrich grava o payload aumentado e **`entity.original_language`** (nova
  coluna do 0019) a partir do detalhe.
- Não-destrutivo ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):
  re-enriquecer atualiza `media_external`; nunca toca override/arte `origin=user`.

### API / tipos
- Sem rota nova. `openapi.yaml`: nota de que o externo agora é multi-idioma (o shape
  resolvido já existe do 0019). Regenerar tipos se algo mudar; `tsc -b` verde.

### Arquivos afetados
- Alterar: `internal/provider/provider.go` (interface), `internal/provider/tmdb/*`
  (append_to_response, listas de gênero, cache, payload aumentado, MapearDetalhe),
  `internal/enrich/*` (langs + original_language), `internal/server/enrich.go` +
  `search_test.go` (fake do provider), testes.

## 5. Critérios de aceite

- [x] Importar um item puxa as traduções numa só chamada de detalhe (`append_to_response=translations`) e grava o payload aumentado; `?lang=pt-BR` serve pt-BR, `?lang=en` serve en.
- [x] Faltando o idioma pedido, a leitura cai no fallback (comportamento do 0019 vivo com dados reais).
- [x] **Gêneros traduzidos** por idioma (via listas localizadas, cacheadas); keywords no idioma obtido.
- [x] `entity.original_language` é gravado no import/enrich (âncora do fallback).
- [x] Payload antigo (0015) segue resolvendo sob o principal (compat). Testes sem rede (httptest + fixtures). Catraca verde.

## 6. Notas de implementação / decisões

- **Uma chamada de detalhe** (`append_to_response=translations`) em vez de N por
  idioma — decisão do dono (L1/Q3). As **listas de gênero** por idioma são poucas e
  cacheadas (invalida raro), então não multiplicam por item.
- **Payload aumentado ≠ furar o ADR-0011:** continua sendo cache re-componível sem
  re-puxar; só embrulha o cru com o `tags_by_lang` derivado (o que o read-time puro
  exige). Documentar o envelope.
- **Keywords** (não traduzidas pelo TMDB) ficam fora do MVP de tags traduzidas ou
  entram sob o idioma obtido — decidir na implementação sem bloquear os gêneros.
- Volume baixo (self-hosted): puxar por item importado, sem sync em massa.

## 7. Fora deste briefing / follow-ups

- UI de idiomas ([0021](0021-ui-idiomas-admin-seletor.md)); override por idioma
  ([0016](0016-editor-override-ui.md)); tradução automática de keywords; fanart/
  backdrop por idioma; FTS por idioma.
