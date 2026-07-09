# 0020 — Provider multi-idioma + tradução de tags

> O provider passa a **puxar traduções** (TMDB `/translations` + gêneros
> localizados) e a gravar o externo **tagueado por idioma** — enchendo os idiomas que
> o resolver ([0019](0019-fundacao-multi-idioma-settings-resolver.md)) já sabe compor.

| | |
|---|---|
| **ID** | 0020 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [ADR-0020](../docs/adr/0020-traducao-de-tags-genres-keywords.md), [ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md) |
| **Status** | draft |
| **Depende de** | [0019](0019-fundacao-multi-idioma-settings-resolver.md), [0015](0015-enriquecimento-import-proxy-imagem.md) |

---

## 1. Objetivo

Com o resolver já resolvendo por idioma ([0019](0019-fundacao-multi-idioma-settings-resolver.md)),
falta a fonte **entregar** os idiomas. O provider TMDB passa a puxar as traduções e
a mapear `raw → canônico` como `idioma → valor`, incluindo **tags traduzíveis**
([ADR-0020](../docs/adr/0020-traducao-de-tags-genres-keywords.md)).

## 2. Escopo (a detalhar quando aterrissar)

**Dentro:**
- **Enriquecimento puxa `/translations`** ([ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md)):
  uma chamada traz todas as traduções (título, sinopse, tagline); `media_external`
  guarda o payload com todas. `MapearDetalhe` passa a entregar `title`/`description`
  como `map[idioma]valor` (limitado aos **permitidos**).
- **Tags por idioma** ([ADR-0020](../docs/adr/0020-traducao-de-tags-genres-keywords.md)):
  gêneros pela **lista localizada** do TMDB por idioma permitido (cache das listas por
  idioma), mapeando ids → nomes; keywords no idioma obtido (sem tradução automática).
- Migração/limpeza do que o [0015](0015-enriquecimento-import-proxy-imagem.md) grava
  como idioma único (compatibilidade — tratar o payload antigo como o idioma principal).
- Testes sem rede (payload `/translations` e listas de gênero fixados).

**Fora:** UI ([0021](0021-ui-idiomas-admin-seletor.md)); override por idioma
([0016](0016-editor-override-ui.md)); tradução automática de keywords.

## 3. Notas

- **Cache das listas de gênero por idioma** evita uma chamada por item; invalida raro.
- O `original_language` (vindo do detalhe) é gravado na entity (âncora do fallback do
  [0019](0019-fundacao-multi-idioma-settings-resolver.md)).
- Volume baixo (self-hosted): puxar por item importado, sem sync em massa.
