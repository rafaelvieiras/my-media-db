# 0024 — Paridade rica entity↔externo: `ResolvedEntity` rico + `RichDetailView` compartilhada

> A tela de detalhe da ENTITY importada passa a ter o MESMO padrão de informação e
> visual do detalhe do EXTERNO ([0023](0023-tela-detalhe-externo-ui.md)),
> compartilhando o mesmo código — com diferenças sutis (a entity tem Editar/arte/
> arquivos; o externo tem Importar). Decisão do dono na revisão do épico.

| | |
|---|---|
| **ID** | 0024 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (extensão pós-revisão) |
| **Docs relacionadas** | [03-modelo-dados](../docs/03-modelo-dados.md), [04-resolver](../docs/04-resolver.md), [05-api](../docs/05-api.md) |
| **Status** | done |
| **Depende de** | [0022](0022-detalhe-externo-rico-persistente.md), [0023](0023-tela-detalhe-externo-ui.md) |

---

## 1. Objetivo

Fechar a assimetria: o detalhe do EXTERNO (antes de importar) mostrava o conjunto
rico (backdrop, slogan, gêneros, palavras-chave, duração, classificação, nota,
vídeos, coleção, similares, links), mas o detalhe da ENTITY importada era raso
(título/tipo/status/sinopse/tags/arquivos). O dono decidiu que as **duas telas devem
seguir o mesmo padrão** de informação e visual, **dividindo o mesmo código**, com
apenas diferenças sutis. Era follow-up listado no §7 do [0023](0023-tela-detalhe-externo-ui.md);
puxado para agora por decisão explícita.

## 2. Decisão de arquitetura

O dado rico JÁ está no banco: o enriquecimento ([0022](0022-detalhe-externo-rico-persistente.md))
grava o payload cru rico em `media_external`, e o resolver da entity já roda o
`MapearDetalhe` do provider sobre ele — os campos ricos canônicos já chegavam à
composição, apenas eram IGNORADOS por não estarem registrados. Escolha (entre
"frontend puxa o /external" × "backend expõe no ResolvedEntity"): **expor no backend**
— fonte única, sem 2ª chamada, e o override pode incidir no futuro.

## 3. Escopo / o que foi feito

**Backend**
- **`internal/richfields`** (novo): pacote COMUM com os tipos `Video/Collection/Similar`
  e os resolvedores/conversores PUROS sobre o canônico (traduzível por cadeia,
  certificação por região com fallback US, vídeos/coleção/similares, links). Consumido
  pelo handler `/external` ([0022](0022-detalhe-externo-rico-persistente.md)) E pelo
  resolver da entity — os dois JSONs saem com o MESMO shape, sem duplicar lógica.
- **Resolver** ([04](../docs/04-resolver.md)): `tagline` e `keywords` entram no
  `registroOrigem` como traduzíveis READ-ONLY (nova etiqueta `soLeitura` — compõem por
  idioma como title/tags, mas ficam FORA de `CamposEditaveis`, logo não são editáveis
  por override, 0016). Os neutros ricos (runtime, vote_average, backdrop_url,
  certification, videos, collection, similar, external_links) são extraídos direto da
  camada externa (prioridade R2) via `richfields`. Gêneros = as `tags` (que já os
  carregam). `ResolvedEntity` ganha esses campos; o shape do resto não muda.
- **openapi.yaml**: `ResolvedEntity` reusa os componentes `ExternalVideo/Collection/
  Similar`; tipos web regerados (`gen:types`).
- Sem migração/sqlc: o payload cru já está gravado.

**Frontend**
- **`RichDetailView`** (novo, compartilhado): hero (backdrop como BANNER no topo com
  degradê para o card + pôster/título/slogan/metadados sobre card sólido), sinopse,
  gêneros, palavras-chave, vídeos, coleção, similares navegáveis, links. Slots para o
  específico de cada tela.
- **`lib/rich-detail.ts`**: modelo normalizado `RichDetail` + adapters
  `deExternalDetail` / `deResolvedEntity` (este resolve pôster local via `posterSrc` e
  usa `tags` como gêneros).
- `/external` e `/entities/{id}` passam a RENDERIZAR pela mesma `RichDetailView`; a
  entity injeta por slot os selos de override (herdado × editado × idioma), o botão/
  editor de override, a galeria de arte e os arquivos. O `ExternalResultCard` reusa o
  `PosterImg`/`ImportarButton` compartilhados (mata a duplicação medida pelo jscpd).

**Contraste (card superior)**: o hero antigo do externo punha o backdrop atrás do
texto a `opacity-30`, com contraste baixo e irregular em tema claro. Trocado por
BANNER com scrim degradê para o `bg-card`; o texto fica sobre o card sólido — contraste
= baseline do tema (AA), em claro e escuro.

## 4. Backdrop cacheado localmente (feito)

O FUNDO (backdrop) da entity passou a ser cacheado em disco no enriquecimento, como o
pôster (0015): `enrich.cachearArtes` grava poster+fanart (origin='external'), a rota
`GET /entities/{id}/assets/fanart` serve o binário (precedência preferida→user→external
+ guarda de path-traversal), e o resolver expõe `backdrop_url` como a rota LOCAL quando
há cache (via `Camadas.BackdropURL`), caindo na URL direta da fonte quando não há. O
front resolve absoluta×relativa (`srcDeImagem`). Entities enriquecidas ANTES do 0022
têm payload parcial (sem keywords/vídeos/similares/certificação e sem fanart cacheado):
um **re-enriquecimento** re-puxa o payload rico e cacheia o fundo.

## 5. Fora deste briefing / follow-ups

- **Elenco/equipe (MD6)** e **nota/avaliação local** — seguem como briefings próprios.
- **Backdrop do detalhe do EXTERNO (pré-import)** ainda usa a URL direta (não há entity
  para anexar o asset; caching pré-import exigiria um proxy genérico) — follow-up.
- **Coleção como página própria** e **player embutido** — como no §7 do [0023](0023-tela-detalhe-externo-ui.md).
