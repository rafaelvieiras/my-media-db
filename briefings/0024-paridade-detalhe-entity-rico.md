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

## 5. Proxy de imagem do EXTERNO pré-import (feito)

A tela de detalhe do EXTERNO (pré-import) servia o pôster/fundo direto do CDN da fonte
(TMDB). Como NÃO há entity para anexar um asset (0015 não se aplica), foi criado um
**proxy READ-THROUGH genérico** `GET /api/v1/images?src=<url>` (`internal/server/
image_proxy.go`): baixa a imagem UMA vez, cacheia em disco sob
`{assetsDir}/_proxy/<sha256(src)>` e serve do cache nas próximas — a UI deixa de bater
no CDN em runtime. Diferença do cache da entity: o cache é POR URL, compartilhado entre
itens/visitas (não por entity).

- **Segurança**: `src` é validado contra uma **allowlist de hosts** (`hostsImagem`,
  default `image.tmdb.org`) — a defesa central contra SSRF (jamais buscamos host
  arbitrário). A resposta só é cacheada/servida se for `200 image/*`. O nome do arquivo
  é o sha256 hex da URL: determinístico e imune a path-traversal, então dispensa a
  guarda de raízes do `servirBinarioAsset`. Escrita ATÔMICA (temp + rename) evita servir
  binário parcial sob concorrência.
- **Reescrita**: o DTO do detalhe externo (`urlImagemServida`) troca as artes do HERO
  (pôster/fundo) pela rota do proxy quando há `assetsDir` E o host é proxyável; senão
  devolve a URL direta (degradação: sem `assetsDir` os testes/handlers seguem com a URL
  da fonte). As miniaturas de **similares/coleção** seguem DIRETAS de propósito — evita
  uma rajada de proxy a cada abertura da página.
- **Front**: `deExternalDetail` resolve pôster/fundo por `srcDeImagem` (rota relativa
  recebe a base; URL absoluta passa direto), o mesmo helper do backdrop da entity.

## 6. Coleção como página própria (feito)

A faixa de coleção (`ColecaoBand`) virou um DESTINO navegável. Endpoint novo
`GET /api/v1/external/{source}/collection/{id}?lang=` (`internal/server/external_colecao.go`)
devolve `{ source, source_id, name, overview, poster_url, backdrop_url, parts:[...] }`;
o provider ganhou `BuscarColecao` via **capacidade opcional** `provider.ColecaoProvider`
(type-assert, sem poluir a interface base — fonte sem coleção → 503). As artes do HERO
da coleção passam pelo proxy local (§5); as miniaturas das parts seguem diretas. A rota
estática `collection` tem precedência sobre `{type}` no chi. No front: rota
`/collection/$source/$id` (`web/src/routes/collection-detail.tsx`) que lista as parts
(cada uma linka para `/external/$source/movie/$id`), e a `ColecaoBand` inteira virou um
`Link` para a página. **Mono-idioma pragmático** (busca no `?lang=` ativo) e
**fetch-through sem cache** — o multi-idioma pleno (all-translations do 0022) e o cache
por TTL ficam como follow-ups anotados.

## 7. Player de vídeo embutido (feito)

Os vídeos (`VideosRail`) deixaram de ser só links: um `VideoCard` mostra o thumbnail do
YouTube (`img.youtube.com/vi/{key}/hqdefault.jpg`) com botão de play e, ao clicar, troca
por um `<iframe>` de `www.youtube-nocookie.com/embed/{key}?autoplay=1` (domínio nocookie,
sem rastreio até o play), em 16:9. Vídeos não-YouTube seguem como link externo. Não há
CSP no projeto, então nada a ajustar em `frame-src`/`img-src`.

## 8. Fora deste briefing / follow-ups

- **Elenco/equipe (MD6)** e **nota/avaliação local** — seguem como briefings próprios.
- **Evicção do cache do proxy** (`_proxy` cresce sem limite; dedup por URL torna
  re-visitas grátis, mas não há expurgo) e **proxy das miniaturas** de similares/coleção
  — follow-ups, se o volume justificar.
- **Coleção multi-idioma pleno + cache por TTL** (hoje mono-idioma fetch-through) — o
  cache re-componível all-translations do 0022 aplicado também à coleção.
