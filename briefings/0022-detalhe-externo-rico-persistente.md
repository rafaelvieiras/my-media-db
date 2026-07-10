# 0022 — Detalhe externo rico + cache persistente (provider)

> Antes de importar, o dono quer **ver o item externo por inteiro** — como no TMDB:
> sinopse, gêneros e palavras-chave, duração, classificação indicativa, slogan,
> pôster e fundo, vídeos, coleção, similares, nota e links. Este briefing entrega a
> **camada de dados**: expandir o provider para puxar tudo isso e **persistir na
> primeira consulta** (cache re-componível), servindo um detalhe externo rico.

| | |
|---|---|
| **ID** | 0022 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (extensão pós-revisão) |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md), [ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md), [ADR-0020](../docs/adr/0020-traducao-de-tags-genres-keywords.md) |
| **Status** | draft |
| **Depende de** | [0014](0014-provider-tmdb-busca-local-first.md), [0015](0015-enriquecimento-import-proxy-imagem.md), [0020](0020-provider-multi-idioma-traducao-tags.md) |

---

## 1. Objetivo

Hoje o resultado externo da busca ([0014](0014-provider-tmdb-busca-local-first.md))
traz só o mínimo (título, ano, sinopse curta, pôster). Para **decidir importar**, o
dono precisa ver o item **como o TMDB mostra** — o conjunto rico de metadados. Este
briefing entrega o **lado dos dados** dessa tela (a UI é o [0023](0023-tela-detalhe-externo-ui.md)):
puxar o detalhe completo do provider, mapeá-lo ao canônico multi-idioma e **persistir
na primeira consulta** para não repetir a ida à fonte (cache re-componível,
[ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md)).

Fora, por decisão explícita com o dono (registrado na revisão da Fase 3): **elenco/
equipe (créditos, MD6)** e **avaliação local** — cada um vira briefing próprio.

## 2. Jornada do usuário

1. O dono abre um resultado externo (da busca) e pede "ver detalhes".
2. O sistema busca o detalhe completo na fonte **uma vez** e o **guarda**; das
   próximas vezes serve do cache (rápido, sem bater na fonte).
3. Ele vê o item rico (a tela é do [0023](0023-tela-detalhe-externo-ui.md)) e decide
   **importar** — o import reusa o detalhe já guardado, sem nova ida à fonte.

*(Esta é a camada de dados; os passos 1–3 são a experiência que o [0023](0023-tela-detalhe-externo-ui.md) monta sobre este contrato.)*

## 3. Escopo

**Dentro:**
- Expandir o provider TMDB (`BuscarDetalhe`/`MapearDetalhe`) para puxar e mapear o
  conjunto rico: duração, slogan (tagline, **traduzível**), palavras-chave
  (keywords, **traduzíveis** — distintas de gênero), classificação indicativa,
  backdrop, **vídeos** (trailer/clip), **coleção** (`belongs_to_collection`),
  **similares**, nota externa (`vote_average`), links externos (`external_ids`:
  imdb etc.). Tudo numa ida (`append_to_response`).
- **Cache persistente do detalhe externo** desacoplado de `entity` (novo, ver §4):
  a primeira consulta grava; import e re-visita reusam.
- Endpoint **GET do detalhe externo** rico (persistindo na 1ª chamada), resolvido no
  idioma ativo (multi-idioma [0020](0020-provider-multi-idioma-traducao-tags.md)).
- Import reusa o detalhe cacheado (sem nova ida à fonte quando fresco).

**Fora (explicitamente):**
- **Elenco/equipe (MD6)** — `person`/`credit`/`company` e diretor/roteirista/elenco.
  Briefing próprio (o roadmap já o adia por complexidade).
- **Avaliação/nota local** do dono — não existe no modelo; briefing próprio.
- A **tela** (componentes, layout, player de vídeo) — é o [0023](0023-tela-detalhe-externo-ui.md).
- Coleção como **entidade navegável** e "similares" como grafo persistido — aqui são
  só campos do detalhe (referências), não novas tabelas de relação.

## 4. Contrato técnico

### Dados / modelo
- **Novo cache `external_detail`** (migração goose, pt-BR), chaveado por `(source,
  source_id)` — **não** por `entity`: ver o externo **não** cataloga (viewing ≠
  cataloging). Colunas mínimas: `source`, `source_id`, `type`, `payload_json` (o
  envelope aumentado do provider, como em [0020](0020-provider-multi-idioma-traducao-tags.md)),
  `fetched_at`. PK `(source, source_id)`. É cache re-componível
  ([ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md)):
  pode ser apagado/re-puxado sem perda de dado do dono.
  - *Decisão a ratificar:* cache próprio (recomendado, mantém `media_external` só
    para itens catalogados) **vs.** `media_external` com `entity_id` nulo. O cache
    próprio evita linhas órfãs e o acoplamento do FK.
- No **import**, o payload do `external_detail` **semeia** o `media_external` da
  entity criada (find-or-create do [0015](0015-enriquecimento-import-proxy-imagem.md)),
  evitando a segunda ida à fonte quando o cache está fresco.
- Campos novos do canônico (do provider) — traduzíveis vs neutros seguindo
  [ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md):
  - **Traduzíveis** (map por idioma): `tagline`, `keywords`.
  - **Neutros** (escalares/listas): `runtime`, `certification` (por região),
    `backdrop_url`, `videos[]`, `collection` (`{id,name,poster}`), `similar[]`
    (`{source_id,title,year,poster}`), `vote_average`, `external_links` (`{imdb,…}`).

### API (`/api/v1`)
- **GET `/external/{source}/{type}/{id}`** — detalhe externo rico. `type`
  (`movie|tv`) porque a fonte exige. Puxa+persiste na 1ª chamada; serve do cache
  (com refresh por TTL/força) nas seguintes. `?lang=` resolve os traduzíveis no
  idioma ativo (valida contra permitidos, como `/search` do fix da revisão). `200`
  com o detalhe canônico resolvido; `404` id inexistente na fonte; `502` fonte
  falhou; `503` sem provider. Nunca cria `entity`.
- **POST `/import`** ([0015](0015-enriquecimento-import-proxy-imagem.md)) — inalterado
  no contrato; internamente passa a **reusar** o `external_detail` cacheado quando
  fresco (otimização, não muda a resposta).

### Backend (Go)
- `internal/provider/tmdb`: `BuscarDetalhe` amplia o `append_to_response`
  (`translations,videos,keywords,release_dates,external_ids` + `belongs_to_collection`
  e `similar`/`recommendations` do próprio detalhe); `MapearDetalhe` mapeia os campos
  novos ao canônico (traduzíveis como map por idioma). **Sem** `credits` (MD6 fora).
- `internal/enrich` (ou um novo `internal/externaldetail`): serviço que resolve o
  detalhe externo — cache-first no `external_detail`, refresh por TTL, persistência
  na 1ª chamada; reusado pelo import para semear `media_external`.
- `internal/server`: handler `getExternalDetail` (o GET acima), reusando `parseLang`
  e o mapeamento canônico; a resposta usa o mesmo resolver de campos traduzíveis por
  idioma do [0019](0019-fundacao-multi-idioma-settings-resolver.md)/[0020](0020-provider-multi-idioma-traducao-tags.md).
- Queries sqlc novas (ASCII, colunas explícitas): upsert/obter `external_detail`.

### Arquivos afetados
- Criar: migração `external_detail`, queries + regen sqlc, handler do GET + testes,
  (talvez) `internal/externaldetail`. Alterar: `provider/tmdb` (append + mapping),
  `enrich` (reuso do cache no import), `openapi.yaml` (+ tipos), `server.go` (rota).

## 5. Critérios de aceite

- [ ] `GET /external/{source}/{type}/{id}` devolve o detalhe rico canônico
  (duração, tagline, keywords, classificação, backdrop, vídeos, coleção, similares,
  nota, links) resolvido no `?lang=`; `404`/`502`/`503` nas bordas.
- [ ] A **1ª chamada persiste** no `external_detail`; a 2ª (fresca) **não** bate na
  fonte (testado por contagem de idas ao provider).
- [ ] Ver o externo **não cria `entity`** (viewing ≠ cataloging) — testado.
- [ ] O **import reusa** o detalhe cacheado quando fresco (sem 2ª ida) e produz a
  mesma entity resolvida de hoje.
- [ ] Traduzíveis (tagline/keywords) respeitam o idioma ativo e a cadeia de fallback;
  neutros vêm uma vez. **Sem** créditos no payload. Catraca verde (Go + type-check web).

## 6. Notas de implementação / decisões

- **Persistir na 1ª chamada** ([ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md)):
  o detalhe externo é cache re-componível, então guardá-lo cedo é barato e evita
  martelar a fonte — decisão do dono na revisão da Fase 3.
- **Cache desacoplado de `entity`**: ver o externo não deve poluir o catálogo. Um
  `external_detail` por `(source,source_id)` mantém "ver ≠ catalogar" e alimenta o
  import quando ele acontece.
- **Sem créditos (MD6) nem nota local**: decisão explícita para manter a 1ª versão
  entregável; ambos viram briefings próprios (ver §7).
- **Vídeos "locais em destaque"**: no **externo** não há arquivo local, então os
  vídeos são os da fonte (YT/TMDB). A regra "locais primeiro, depois YT/outros" do
  dono se aplica ao **detalhe da entity já importada** (que tem `media_file`) — a ser
  tratado no [0023](0023-tela-detalhe-externo-ui.md)/follow-up. *Ponto aberto a
  confirmar.*
- **Classificação indicativa** vem de `release_dates` (movie)/`content_ratings` (tv),
  por região — escolher a região do idioma ativo, com fallback. *Detalhe de mapeamento.*

## 7. Fora deste briefing / follow-ups

- **Elenco/equipe (MD6)** — `person`/`credit`/`company`, diretor/roteirista/elenco:
  briefing próprio (o mais complexo, já adiado pelo roadmap).
- **Avaliação/nota local** do dono: modelo novo + UI — briefing próprio.
- **Coleção navegável** e **similares como grafo** persistido; **player de vídeo** e
  ordenação "locais em destaque" no detalhe da entity importada.
