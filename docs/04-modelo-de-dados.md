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
| `status` | text | **Intenção** do dono (`cataloged` default / `wanted` / `unreleased` / `draft`) — ver abaixo. Posse **não** é status |
| `tags` | json[] | Lista livre |
| `external_ids` | json | `{ "tmdb": "123", "imdb": "tt..." , … }` — **mapa de identidade autoritativo** (ver [Identidade externa](#identidade-externa-external_ids-autoritativo-vs-media_external-cache)) |
| `extension_data` | json | Dados por vertical, *namespaced* — ver abaixo |
| `created_at` / `updated_at` | timestamp | |

Relações e itens auxiliares (pessoas, empresas, assets, arquivos, relações
entre entidades) são **tabelas próprias**, não campos soltos — ver
[Persistência](#persistência-sqlite).

### Posse e `status`: dois eixos ortogonais

*"Tenho o arquivo?"* e *"o que pretendo com o item?"* são perguntas **independentes**.
Modelá-las em eixos separados é o que sustenta o "banco maior que a biblioteca"
([cenário D](01-visao-e-casos-de-uso.md)):

- **Posse — fato derivado.** Não é campo editável: é computado da existência de ao
  menos um `media_file` para a entidade (*"na biblioteca"* vs *"só no catálogo"*).
  Por ser derivada, não mente nem diverge do disco. Pode ser materializada como
  coluna gerada para filtrar rápido.
- **`status` — intenção do dono.** Um estado por vez:
  - `cataloged` — item normal e curado (**default**).
  - `wanted` — desejado / wishlist.
  - `unreleased` — catalogado, ainda não lançado.
  - `draft` — rascunho / em curadoria.

Os eixos combinam livremente:

| `status` | posse | Significado |
|---|---|---|
| `cataloged` | sim | o item comum da estante |
| `wanted` | não | wishlist pura |
| `wanted` | sim | tenho uma cópia, mas quero upgrade (cam rip → Blu-ray) |
| `unreleased` | não | catalogado, ainda vai lançar |

> **Não existe coluna `owned`.** O antigo valor `owned` era posse disfarçada de
> intenção — agora posse é derivada e `status` é só intenção. As **transições** de
> `status` quando um arquivo aparece/some ficam no
> [00 — agenda, MD5](00-agenda-de-decisoes.md).

## O resolver (composição em camadas)

O resolver compõe o valor final de cada entidade a partir de três camadas, na
ordem de precedência abaixo. Ele é um **motor genérico**: não conhece nenhum
campo pelo nome — nem do núcleo, nem de extensão. Guia-se por metadado (ver
"Origem por campo").

```
        ┌─ media_override   (o dono editou → vence sempre)
valor ──┼─ media_external   (puxado da(s) fonte(s) externa(s) → base editorial)
        └─ media_file        (extraído do arquivo: mediainfo → fatos técnicos)
```

- **Arquivo** (`media_file.mediainfo`): fatos técnicos detectados (codec, HDR,
  faixas, duração real). É o que o arquivo *é* — mas **pode ser corrigido por
  override** (o detectado original permanece preservado; ver "Override sobre o
  técnico").
- **Externo** (`media_external`): payload puxado de cada fonte, guardado **cru e
  identificado pela fonte**, para re-compor ou re-puxar. É a base editorial.
- **Override** (`media_override`): patch **esparso** do dono — só os campos que
  ele mudou. É o que dá a ele a autoridade final. **Override é universal**: alcança
  qualquer campo, sem exceção (inclusive técnico e de extensão).

Re-enriquecer atualiza apenas `media_external`; o `media_override` permanece
intocado. É isso que satisfaz A3/B3/G2.

### Origem por campo (particionamento)

As três camadas **não** competem indiscriminadamente por todo campo: cada campo
declara de quais **fontes-base** pode vir (`arquivo`, `externo`, ou `nenhuma` —
esta última para itens que só existem se o dono os criar, como a série "caseira"
do [cenário C](01-visao-e-casos-de-uso.md)). O **override não é declarado** —
por ser universal, aplica-se a todos.

Essa declaração é **metadado do campo**, não uma tabela fixa no código:

- **Campos do núcleo** declaram a origem no schema do núcleo (conjunto pequeno e
  estável).
- **Campos de extensão** declaram a origem como anotação no **JSON Schema do
  próprio namespace** (ver `extension_data`). Adicionar uma vertical/plugin não
  toca o núcleo nem o resolver.

Assim, `title`/`description` têm base `externo`; `video_codec`/`hdr` têm base
`arquivo`; um `season_number` de `core.tv@1` declara a sua própria base no schema
da vertical. O resolver apenas lê a etiqueta e compõe.

Campos de **sistema** (`id`, `created_at`, `updated_at`, `content_hash`) ficam
**fora** do resolver: são identidade/auditoria, não valor editorial.

### Prioridade entre fontes externas

Quando há mais de uma fonte externa (visão multi-provider) com valor para o mesmo
campo, o resolver usa uma **lista de prioridade global e configurável** de fontes
(ex.: `[tmdb, tvdb, musicbrainz]`). Para cada campo, vence a primeira fonte da
lista que tiver valor; o resultado forma a base externa, sobre a qual o override
entra. **Default do MVP:** `[tmdb]`. (Prioridade por campo ou fonte primária por
entidade ficam como evolução pós-MVP, sem quebrar o modelo.)

### Campos-lista: merge por operações

Campos que são **listas de valores simples** (`tags`, `genres`) não são
substituídos em bloco pelo override: este guarda **adições** e **remoções por
valor**, e a base externa continua viva por baixo (um re-scrape que traga um
gênero novo o inclui, sem desfazer as edições do dono). A ordem exibida segue a
externa, com remoções aplicadas e adições ao final; itens duplicados são
deduplicados na composição.

> **Listas de relações** (elenco, equipe) **não** são campos-lista deste tipo:
> vivem em tabelas próprias (`person`/`credit`) e o override delas é tratado à
> parte (ver [00 — agenda, MD6](00-agenda-de-decisoes.md)).

### Composição uniforme (núcleo e extensão)

O resolver **não distingue** campo de núcleo de campo de `extension_data`: a mesma
precedência `override ⟵ externo ⟵ arquivo` vale campo a campo, **inclusive dentro
dos namespaces de extensão**. O patch de override espelha a entity inteira,
extensão incluída.

### "Sem override" vs. "override para vazio"

Como o override é um patch esparso, a **presença da chave** é o sinal de autoridade:

- **Chave presente** no patch (mesmo com valor `null`/vazio) → o dono decidiu;
  vence a base externa, **inclusive para apagar** um valor que a fonte insiste em
  trazer.
- **Chave ausente** → não houve override; herda da base.

A UI de edição deve distinguir **"limpar override"** (remover a chave, voltando a
herdar) de **"definir vazio"** (chave presente com `null`). A **proveniência por
campo** (de qual camada o valor veio) **não é exposta na API** por ora — a UI
consulta o override separadamente quando precisa.

### Override sobre o técnico

Por o override ser universal, o dono pode **corrigir um fato técnico mal-detectado**
(ex.: HDR falso-positivo do `ffprobe`) pelo mesmo mecanismo. O dado detectado
original permanece em `media_file` (não-destrutivo); o override apenas entra por
cima na composição. É uso de exceção, não de rotina.

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

## Ciclo de vida e deleção

O princípio **não-destrutivo** governa o que acontece quando um arquivo some. Regra
central: **eventos de arquivo mexem só na posse (derivada); nunca no `status`
(intenção) nem no trabalho do dono.**

- **Arquivo some.** O `media_file` vira `missing` no scan (ver
  [06](06-biblioteca-e-scan.md)); a **entity é preservada**. Como posse deriva de
  haver `media_file` **presente**, ela passa a *não* — mas o `status` fica
  **intacto** (um `cataloged` sem arquivo segue `cataloged`; o dono decide se agora
  "quer" de volta).
- **Arquivo aparece** para um item `wanted` → posse=sim e o `status` **segue**
  `wanted` (preserva o "tenho cópia, quero upgrade"). O sistema nunca reescreve
  intenção.
- **Datasource offline** (HD desmontado) não marca nada como `missing` — o disco
  ausente não é deleção (ver [06](06-biblioteca-e-scan.md)).
- **Órfãs.** Uma entity **auto-criada** cujo arquivo sumiu e que **não tem nenhum
  investimento do dono** — sem `media_override`, sem asset custom, sem coleção
  manual, `status` ainda no default `cataloged` — é marcada **órfã** e vai para uma
  fila de limpeza. **Nunca é apagada sozinha.**
- **Purga** é a única operação destrutiva, e é sempre **explícita** do dono
  (individual ou em lote): remove a entity e seus derivados (`media_external`,
  `media_override`, assets). Um `media_file` de arquivo que **ainda existe** no disco
  jamais é purgado pelo sistema.

Esboço de tabelas (nomes/colunas a fechar no briefing de schema):

```
entity            (id, type, title, original_title, description,
                   release_date, status, tags_json, external_ids_json,
                   extension_data_json, created_at, updated_at)

entity_relation   (id, parent_id, child_id, kind)      -- série↔temporada↔episódio, coleção↔filme
media_external    (id, entity_id, source, source_id, payload_json, fetched_at)
                   -- cache de payload; source_id subordinado ao external_ids (ver Identidade externa)
media_override    (entity_id, override_json, updated_at)   -- patch esparso (1 linha/entity), espelha a entity
media_file        (id, entity_id, path, content_hash, edition, part_no,
                   match_confidence, mediainfo_json, size, mtime)
                   -- 1:N com entity (versões/qualidades/edições); edition=rótulo,
                   -- part_no p/ stacking (cd1/cd2); ver 06 — Identidade e matching
person            (id, name, external_ids_json)
credit            (id, entity_id, person_id, role, ord)
company           (id, name, kind, external_ids_json)
asset             (id, entity_id, kind, origin, path_or_uri, meta_json)  -- poster/fanart/...
library_source    (id, path, type, last_scan)
-- fase posterior: user, token, permission, plugin_registry
```

### Identidade externa: `external_ids` (autoritativo) vs `media_external` (cache)

O mesmo id externo aparece nos dois lugares; a regra de autoridade evita divergência:

- **`entity.external_ids` é o mapa de identidade autoritativo.** Guarda **todos os
  ids conhecidos** da entidade (buscados ou não) — inclusive um `imdb` lido do nome
  do arquivo (ver [06 — matching](06-biblioteca-e-scan.md)) mesmo sem a ficha do
  IMDb ter sido baixada. É o que o **matching grava** e o que o **lookup lê** (coluna
  gerada + índice; ver Estratégia de busca).
- **`media_external` é cache de payload** — uma linha por fonte efetivamente buscada.
  Seu `source_id` é **subordinado**: para uma fonte presente em `external_ids`, vale
  o invariante `media_external.source_id == external_ids[source]`.
- **Conflito → a identidade vence.** Uma linha de `media_external` cujo `source_id`
  não bate com `external_ids` é **cache obsoleto**: re-busca sob o id correto e
  descarta a antiga. O dono corrige **a identidade** (via correção de match) e
  **nunca edita `media_external` à mão** — o cache o segue.
- **Identidade ≠ valor editorial.** `external_ids` diz *qual* registro externo é a
  entidade; `media_external` continua sendo a *camada base editorial* do resolver
  (título, sinopse…). São eixos distintos. A mesma lógica de identidade vale para
  `person`/`company` (têm `external_ids`, sem cache dedicado).

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
