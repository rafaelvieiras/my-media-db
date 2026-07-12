# 0048 — Provider fanart.tv (ArteProvider) + taxonomia de kinds + inventário lazy

> Adicionar a fanart.tv como provider **dedicado a arte** (poster, fanart, logo,
> clearart, disc, banner, thumb, characterart + artes de temporada), enriquecendo
> qualquer filme/série por id externo, sem baixar binários no import.

| | |
|---|---|
| **ID** | 0048 |
| **Tipo** | feature (backend) |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | ready |
| **Docs relacionadas** | [09/07-providers](../docs/07-enriquecimento-providers.md), [08-artwork](../docs/08-artwork-e-assets.md), [AGENTS.md §6](../AGENTS.md) |
| **Depende de** | 0044 (artes), 0045, 0046 |

---

## 1. Objetivo

A fanart.tv é uma fonte **exclusiva de arte** de alta qualidade (logos, clearart,
disc, character art) que o TMDB/TVDB não cobrem bem. A auditoria (briefing 0048
original) confirmou que a fundação aguenta um provider por **capacidade** sem
reforma. Este briefing entrega o backend: a interface `ArteProvider`, o provider
`fanarttv`, a **taxonomia de kinds expandida** e a persistência do **inventário
de artes disponíveis** de forma **lazy** (sem baixar binários no import).

Decisões do dono (travadas nesta sessão):
- **Listar no import, baixar só a selecionada.** No import/enrich faz **1 chamada**
  (barata, `cache-control: max-age=600`) que devolve TODA a lista de artes do
  item; povoa selos de disponibilidade e a galeria via **proxy de imagem** (0
  download). Baixa pro disco só a arte definida como preferida ou num clique.
- **Todos os tipos** de arte de uma vez.
- **Chave pessoal como `api_key`** (autentica sozinha — validado, HTTP 200) via env
  var; idioma: principal das settings → `en` → textless (`""`); fanart prefere
  textless.

## 2. Realidade da API (pesquisada, 2 chamadas reais)

- **Endpoints** (base `https://webservice.fanart.tv/v3`): `/movies/{id}` (id =
  **TMDB** ou **IMDB**, ex. `tt0137523`), `/tv/{id}` (id = **TheTVDB**). Ids que
  **já temos** após enriquecimento (0045/0046).
- **Resposta**: objeto com uma **array por tipo** (+ um `<tipo>_count`) e
  `name`/`tmdb_id`/`imdb_id`/`thetvdb_id`. Cada imagem: `id`, `url`, `lang`
  (2 letras ou `""`=textless), `likes`. Disc adiciona `disc` (nº) + `disc_type`
  (`bluray`/`dvd`/`cd`/`3d`); arte de temporada adiciona `season` (nº, `all` ou `0`).
- **Sem rate-limit prático** nas chamadas de metadados; `429`+`Retry-After` raro.
  A chave pessoal *bypassa* o limite da chave de projeto e dá preview de 2 dias.
- **Tipos** (JSON key → nosso `kind`):

| filme | série | `kind` | notas |
|---|---|---|---|
| `movieposter` | `tvposter` | `poster` | 2:3 |
| `moviebackground` | `showbackground` | `fanart` | 16:9, textless |
| `hdmovielogo`,`movielogo` | `hdtvlogo`,`clearlogo` | `logo` | transparente |
| `hdmovieclearart`,`movieart` | `hdclearart`,`clearart` | `clearart` | transparente |
| `moviedisc` | — | `disc` | `disc_type`+`disc` no meta |
| `moviebanner` | `tvbanner` | `banner` | ~1000×185 |
| `moviethumb` | `tvthumb` | `thumb` | 16:9 promocional |
| — | `characterart` | `characterart` | 512×512 transparente |
| — | `seasonposter`/`seasonthumb`/`seasonbanner` | `poster`/`thumb`/`banner` **na temporada** | por `season` |

## 3. Escopo

**Dentro:**
- Interface `provider.ArteProvider` + const de kinds; provider `fanarttv`.
- **Taxonomia de kinds expandida**: abrir `parseKind` (hoje 3 fixos) para um
  **registro** de kinds válidos {poster, fanart, still, logo, clearart, disc,
  banner, thumb, characterart}; kind desconhecido ainda → 422.
- **Inventário lazy**: no import/enrich, se fanart.tv configurado e id externo
  disponível, buscar a lista e **persistir o inventário** (sem baixar binários).
- Endpoint que devolve a **arte disponível agrupada por kind** (inventário externo
  + assets locais) para a galeria e para os selos do card do topo (consumido pelo
  0049).
- Mapear artes de temporada (`season`) para as entities de temporada.
- Idioma: preferência principal→en→textless na escolha da arte servida por padrão;
  o inventário guarda `lang` de todas para o filtro do 0049.

**Fora (explicitamente):**
- UI da galeria/lightbox/selos → **briefing 0049**.
- Download real pro disco em massa (só o selecionado; o pipeline de download já
  existe em `cachearArte`, `origin=external`).
- Trailer e tema musical (seguem como follow-up; a interface `ArteProvider` abre o
  caminho, mas binário de vídeo/áudio é outra modelagem — ver §7).
- Novos esquemas (videoclipe/música) via plugin.

## 4. Contrato técnico

### Provider (Go)
- `internal/provider/provider.go`: nova interface opcional
  `ArteProvider { BuscarArtes(ctx, ids ExternalIDs, tipo string, langs []string) ([]Arte, error) }`
  onde `Arte{ Kind, URL, Lang, Likes int, DiscType string, Disc int, Season string }`.
  `ExternalIDs` carrega tmdb/imdb/tvdb (o que o item tiver).
- `internal/provider/fanarttv/` (novo): `Client` implementa o **core `Provider`**
  com `Buscar`/`BuscarDetalhe` **no-op** (não é fonte de busca — nunca polui
  resultados) **e** `ArteProvider`. `Nome()="fanarttv"`. `BuscarArtes` chama
  `/movies/{id}` ou `/tv/{id}`, mapeia todos os tipos → `[]Arte` (normalizando os
  kinds da tabela §2; disc/season no struct). Registrado em
  `cmd/mymediadb/main.go` se `MYMEDIADB_FANARTTV_API_KEY` presente (env-only,
  **nunca** commitar a chave).

### Enrich (Go)
- `internal/enrich`: após o import/enrich primário, iterar providers que
  implementem `ArteProvider` e chamar `BuscarArtes` com os ids externos do item
  (best-effort: erro/sem-match degrada em silêncio). Persistir o **inventário**.
- Arte de temporada (`season`) → resolver a entity de temporada correspondente e
  anexar lá.

### Dados / persistência
- **Inventário como camada externa**, não como centenas de assets baixados:
  persistir a lista fanart.tv como `media_external source="fanarttv"` (envelope
  com as arrays por kind, incluindo `lang`/`likes`/`disc_type`/`season`/`url`).
  Idempotente (UNIQUE(entity_id, source)). **Nenhum binário baixado aqui.**
- `asset` só é materializado quando o dono **define preferida** ou **baixa** uma
  arte (reusa `cachearArte`, `origin=external`; e o mecanismo de "preferida" do
  0017/0044). Precedência atual (preferida→user→external→base) já compõe.
- `kind` no banco continua TEXT livre; a validação de entrada (`parseKind`) passa a
  aceitar o conjunto expandido.

### API
- `GET /api/v1/entities/{id}/artwork` → `{ [kind]: [{ url(proxied), lang, likes,
  source, disc_type?, disc?, season?, is_preferred, is_local }], ... }` +
  `available: { [kind]: count }` (para os selos). Mescla inventário externo
  fanart.tv + TMDB (poster/backdrop já existentes) + assets locais do dono.
- Servir imagens **sempre via proxy** (`arteSrc`), inclusive as da fanart.tv
  (host `assets.fanart.tv` na allowlist do proxy).

### Arquivos afetados (previsão)
- `internal/provider/provider.go`, `internal/provider/fanarttv/*.go` (novo)
- `internal/enrich/*.go` (chamada do ArteProvider + persistência do inventário)
- `internal/server/assets.go` (registro de kinds / `parseKind`)
- `internal/server/artwork.go` (novo — endpoint agrupado) + rota em `server.go`
- proxy allowlist (`assets.fanart.tv`)
- migrations: nenhuma (asset.kind livre; inventário em media_external)
- testes: provider (mapeamento dos tipos, disc/season/lang), enrich (inventário
  persistido sem download), server (endpoint agrupado + parseKind expandido).

## 5. Critérios de aceite

- [ ] Com `MYMEDIADB_FANARTTV_API_KEY` setada, importar um filme (id TMDB) persiste
      o inventário fanart.tv **sem baixar binários**; `GET .../artwork` devolve as
      artes agrupadas por kind com `count` por kind.
- [ ] Série: artes de show no nó série; artes de temporada anexadas à temporada
      certa via `season`.
- [ ] `parseKind` aceita {poster,fanart,still,logo,clearart,disc,banner,thumb,
      characterart} e rejeita kind fora com 422.
- [ ] Sem a chave configurada, tudo funciona como hoje (fanart.tv omitido em
      silêncio) — regressão zero.
- [ ] Idioma: a arte servida por padrão respeita principal→en→textless; o
      inventário preserva `lang` de todas.
- [ ] `go build/vet/test -count=1` verdes; catraca local passa; funções novas
      gocyclo ≤10.

## 6. Notas / decisões

- fanart.tv **não é fonte de busca** — implementa o core `Provider` com Buscar
  no-op só para entrar no Registry e ser descoberto por type-assert a
  `ArteProvider` (mesmo padrão de `SerieRicoProvider`). Ver [[independencia-de-provider]].
- **Inventário lazy** (camada externa) em vez de explodir assets: casa com a
  decisão "baixar só o selecionado" e com o modelo lazy-proxy (0042/0044). Baixar
  em massa fica fora.
- Chave: só a pessoal como `api_key` (validado). Se um dia quiser o preview
  "oficial" de 2 dias, dá para passar também como `client_key` — não é necessário.

## 7. Follow-ups

- **0049** — galeria em linhas por tipo + lightbox/zoom + selos de disponibilidade.
- Download em massa opcional / prune de inventário ao re-enrich.
- **Trailer** e **tema musical** (download pro disco): interface análoga, mas
  binário de vídeo/áudio precisa de modelagem própria (asset `kind=trailer/theme`
  vs `media_file`) — briefing dedicado.
