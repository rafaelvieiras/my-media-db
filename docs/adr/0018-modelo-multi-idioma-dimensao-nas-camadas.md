# ADR-0018 — Modelo multi-idioma: dimensão de idioma nas camadas

- **Status:** Aceita
- **Data:** 2026-07-09
- **Agenda:** [L1](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md)

## Contexto

O modelo assumia **um valor por campo editorial**. Mas o acervo é multi-idioma: o
gestor define um **idioma principal**, um **fallback** e o conjunto de **idiomas
permitidos**; todo valor obtido no scraping ou editado à mão carrega a **flag do
idioma** em que está, e o dono pode acrescentar valores em outros idiomas (achou só
inglês → grava inglês; depois adiciona o português). Faltava a dimensão de idioma
no modelo das três camadas (`arquivo ⟵ externo ⟵ override`).

## Decisão

**Os campos editoriais traduzíveis passam a ser resolvidos POR IDIOMA, e a dimensão
de idioma é a CHAVE dentro de cada camada — não uma tabela nova.**

- **Traduzíveis** (por idioma): `title`, `description`, `tags` (e afins como
  `tagline`, quando entrarem). **Neutros** (não traduzem): `original_title`,
  `original_language`, `release_date`, `external_ids`, fatos técnicos (mediainfo) e,
  por ora, `extension_data`.
- **Externo** (`media_external`): o `payload_json` guarda as traduções da fonte
  (TMDB `/translations`) já tagueadas por código de idioma; o provider entrega o
  canônico como `idioma → valor` (ADR-0020 para tags).
- **Override** (`media_override`): o `override_json` fica indexado por **campo E
  idioma** — ex.: `{"title": {"pt-BR": "…", "en": null}}`. A autoridade do dono
  (R6) vale **por idioma**.
- **Arquivo/núcleo** (`entity.title`/`description`): é o **fallback neutro** (título
  derivado do arquivo/importação), de idioma indeterminado — o último recurso da
  cadeia (ADR-0019).
- **Nova coluna `entity.original_language`** (código do idioma original da obra,
  vindo da fonte) — âncora do fallback.
- **Config em SETTINGS no banco** (não em env): tabela genérica
  `app_settings(key, value_json, updated_at)` com a chave `languages` =
  `{primary, fallback, allowed[]}`, gerida por endpoint + **UI de admin**. Códigos
  no estilo BCP-47/TMDB (`pt-BR`, `en`, `es`). Os **permitidos** governam o que pode
  ser escrito/scraped; principal e fallback formam a base de leitura.

## Consequências

- A flag de idioma é **intrínseca** (é a chave da camada) — sem explosão de tabelas
  nem coluna de idioma por valor.
- A **resolução por idioma** e a **proveniência** ficam no [ADR-0019](0019-resolucao-por-idioma-fallback-proveniencia.md);
  a **tradução de tags** no [ADR-0020](0020-traducao-de-tags-genres-keywords.md).
- Custo: resolver e provider reformulados; migração de schema (settings +
  `original_language`). **FTS multi-idioma** fica como follow-up (o MVP indexa o
  valor neutro/primário).
- Relacionadas: [R1](0001-resolver-particionamento-por-origem.md),
  [R4](0004-override-como-blob-json-esparso.md),
  [R6](0006-presenca-da-chave-sinaliza-override.md),
  [MD4](0011-external-ids-autoritativo-media-external-cache.md).
