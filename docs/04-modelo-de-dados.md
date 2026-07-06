# 04 — Modelo de dados

Este é o documento central do projeto. Ele define **como o dado é modelado** para
sustentar, ao mesmo tempo: a autoridade do dono (override), o catálogo maior que
a biblioteca (`status`), e a universalidade (uma entidade para qualquer mídia,
extensível sem tocar no núcleo).

## Princípios do modelo

1. **Entity-first.** Toda mídia é uma **`Entity`** genérica com um conjunto
   mínimo e padronizado de campos. "Filme" e "série" são *tipos* de entidade, não
   schemas diferentes.
2. **Extensão isolada.** Campos específicos de uma vertical vivem em
   **`extension_data`**, um blob JSON *namespaced* — nunca em colunas novas do
   núcleo. Adicionar vertical não altera o schema central.
3. **Composição em camadas.** O valor final de qualquer campo é resolvido como
   `override do usuário ⟵ dado externo ⟵ dado do arquivo`. A verdade servida é a
   composta, não a bruta.
4. **Não-destrutivo.** Nem re-scrape de fonte externa, nem remoção de uma
   extensão apagam dado do dono automaticamente. Purga é sempre explícita.
5. **SQLite-nativo.** O modelo é desenhado para SQLite (JSON1 + FTS5 + colunas
   geradas), não para JSONB/GIN de Postgres.

## A entidade base

Campos do núcleo (comuns a qualquer mídia):

| Campo | Tipo | Nota |
|---|---|---|
| `id` | uuid/text | Identidade estável interna |
| `type` | text | `movie`, `tv`, `season`, `episode`, `collection`, … (extensível) |
| `title` | text | Título canônico (após override) |
| `original_title` | text? | |
| `description` | text? | Sinopse/plot |
| `release_date` | date? | ISO `YYYY-MM-DD` |
| `status` | text | Estado no catálogo — ver abaixo |
| `tags` | json[] | Lista livre |
| `external_ids` | json | `{ "tmdb": "123", "imdb": "tt..." , … }` |
| `extension_data` | json | Dados por vertical, *namespaced* — ver abaixo |
| `created_at` / `updated_at` | timestamp | |

Relações e itens auxiliares (pessoas, empresas, assets, arquivos, relações
entre entidades) são **tabelas próprias**, não campos soltos — ver
[Persistência](#persistência-sqlite).

### O campo `status`

Sustenta o "banco maior que a biblioteca" ([cenário D](01-visao-e-casos-de-uso.md)):

- `owned` — possuído (tem ao menos um arquivo, mas não obriga).
- `wanted` — desejado / wishlist.
- `draft` — rascunho / em curadoria.
- `unreleased` — catalogado, ainda não lançado.

*(Lista inicial; refinar no MVP. Ter arquivo é um atributo de `media_file`, não o
que define `status`.)*

## As três camadas (o resolver)

Cada campo pode vir de três origens. A leitura compõe na ordem de precedência:

```
        ┌─ media_override   (o dono editou → vence sempre)
valor ──┼─ media_external   (puxado da fonte externa, cache local → base)
        └─ media_file        (extraído do arquivo: mediainfo → fatos técnicos)
```

- **Arquivo** (`media_file.mediainfo`): fatos técnicos detectados (codec, HDR,
  faixas, duração real). Não editável — é o que o arquivo *é*.
- **Externo** (`media_external`): payload puxado de cada fonte, guardado **cru e
  identificado pela fonte**, para poder re-compor ou re-puxar. É a base editorial.
- **Override** (`media_override`): registro **esparso** — só os campos que o dono
  mudou. É o que dá a ele a autoridade final.

Re-enriquecer atualiza apenas `media_external`; o `media_override` permanece
intocado. É isso que satisfaz A3/B3/G2.

## `extension_data` — extensão por vertical

Campos que não são universais (ex.: `season_number` de episódio; futuramente
`platform` de um game, `isbn` de um livro) vivem sob uma **chave namespaced**:

```json
{
  "extension_data": {
    "core.tv@1": { "season_number": 2, "episode_number": 5 },
    "core.movie@1": { "collection_ref": "col-uuid" }
  }
}
```

Regras:

- **Namespace obrigatório**: `<owner>.<slug>@<major>` (ex.: `core.tv@1`). No MVP,
  as verticais são **internas** (`core.*`); no futuro, plugins usam o seu próprio
  namespace (`plugin.games@1`).
- **Schema validado**: cada vertical registra um JSON Schema; o núcleo **valida o
  blob antes de persistir** e rejeita o que não conforma.
- **Não-destrutivo**: desabilitar/remover uma vertical **não apaga** o blob;
  purga é ação explícita do admin.
- **Binário fora do JSON**: arquivos pesados (artes, PDFs) **nunca** entram no
  blob — vão para disco/asset e o JSON guarda só referência.

> **Por que isso, e não colunas por tipo:** permite catalogar qualquer mídia do
> servidor (o [cenário I](01-visao-e-casos-de-uso.md)) sem migração de schema a
> cada vertical nova, e é o alicerce sobre o qual os plugins futuros se apoiam —
> sem pagar o custo do maquinário de plugins agora.

## Persistência (SQLite)

Esboço de tabelas (nomes/colunas a fechar no briefing de schema):

```
entity            (id, type, title, original_title, description,
                   release_date, status, tags_json, external_ids_json,
                   extension_data_json, created_at, updated_at)

entity_relation   (id, parent_id, child_id, kind)      -- série↔temporada↔episódio, coleção↔filme
media_external    (id, entity_id, source, source_id, payload_json, fetched_at)
media_override    (id, entity_id, field, value_json, updated_at)   -- esparso
media_file        (id, entity_id, path, content_hash, mediainfo_json, size, mtime)
person            (id, name, external_ids_json)
credit            (id, entity_id, person_id, role, ord)
company           (id, name, kind, external_ids_json)
asset             (id, entity_id, kind, origin, path_or_uri, meta_json)  -- poster/fanart/...
library_source    (id, path, type, last_scan)
-- fase posterior: user, token, permission, plugin_registry
```

### Estratégia de busca (sem GIN)

SQLite não tem índice GIN, então:

- **Busca textual**: tabela **FTS5** (`entity_fts`) sobre título/original/tags.
- **Campos JSON quentes**: expor via **colunas geradas** (`GENERATED ALWAYS AS
  (json_extract(...))`) e indexar essas colunas quando a consulta justificar.
- **`external_ids`**: para lookup por id externo (ex.: achar por `tmdb=123`),
  coluna gerada + índice, ou tabela de índice dedicada.

Isso cobre o uso self-hosted com folga; volume de requests **não é gargalo**
(decisão revista — ver [11](11-decisoes-abertas.md)).

## Contrato de tipos

Como o backend é Go e o front é TS, a fonte de verdade dos tipos será um
**contrato único** (provável OpenAPI a partir da API, gerando tipos TS) — em vez
de um pacote de tipos mantido à mão. Decisão em [05 — API](05-api.md) /
[11](11-decisoes-abertas.md).
