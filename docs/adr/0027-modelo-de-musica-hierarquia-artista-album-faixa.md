# ADR-0027 — Modelo de música: hierarquia artista→álbum→faixa via MusicBrainz

- **Status:** Aceita
- **Data:** 2026-08-19
- **Agenda:** [MU1–MU4](../00-agenda-de-decisoes.md#modelo-de-música--fundacional-levantada-a-partir-de-pedido-do-dono-pra-implementar-o-schema) · **Spec:** [04-modelo-de-dados](../04-modelo-de-dados.md), [07-enriquecimento-providers](../07-enriquecimento-providers.md), [12-roadmap](../12-roadmap.md)

## Contexto

Música é a próxima vertical prevista ([12-roadmap](../12-roadmap.md), Fase 5+) e o
teste real da promessa "vertical nova sem migration do core"
([AGENTS.md](../../AGENTS.md) §3). Diferente de série (hierarquia rígida com
múltiplas ordenações concorrentes, resolvida no [ADR-0021](0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md)),
música tem seu próprio ponto espinhoso: o artista de uma **faixa** pode divergir
do artista do **álbum** (feat./colaboração), compilações/trilhas sonoras usam
"Various Artists" como agrupador não-artístico, e a identidade de um álbum
depende de um conjunto de campos — nome + artista + total de discos + selo +
código de barras — onde variar qualquer um **fragmenta o álbum** em players
reais. Este último ponto não é hipótese: é achado empírico documentado no
próprio código dos scripts pessoais de organização de música do dono
(`~/ai-workspace/scripts/music-organizer`), que já resolvem isso em produção
via convenção de tags e pasta, cruzada com a documentação oficial de
Jellyfin/Navidrome/Koel/Kodi.

Pesquisa: o modelo de séries (ADR-0021) serve de precedente direto — entidade
canônica de grão fino ancorada em id externo, entity intermediária só para
artwork/override, numeração em `extension_data`. Os scripts pessoais serviram
de fonte empírica de convenções reais (estrutura de pasta, tags usadas,
tratamento de compilação/multi-disco). **MusicBrainz + Cover Art Archive**
foi escolhido como provider base — já cotado na visão do [doc 07](../07-enriquecimento-providers.md),
sem exigir chave de API, papel equivalente ao que o TheTVDB tem hoje para
séries.

## Decisão

1. **Faixa é a entidade canônica de grão fino.** `entity type='faixa'` por
   gravação, ancorada no **MBID de recording** (nunca em número/posição).
   Numeração (`track_number`, `disc_number`, `disc_total`) vive no
   `extension_data` namespace **`core.music@1`**, espelhando a numeração de
   episódio do `core.tv@1` ([ADR-0021](0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md) item 1).

2. **Álbum é entity só para dar casa a artwork/override e agrupamento**, ligada
   à faixa por `entity_relation(kind='music_album', sort_index=track_number)`.
   Mesmo raciocínio da temporada: não é estrutura obrigatória, é onde o dono
   edita capa/título/override do álbum como um todo.

3. **Artista é entity de 1ª classe** — **não** reaproveita `person`/`credit`
   (MD6): banda não é pessoa, precisa de artwork/bio própria como item
   catalogável, igual filme/série. "Various Artists" (agrupador de
   compilação/trilha sonora) é um artista comum, com o **MBID canônico fixo**
   que a própria MusicBrainz já usa (`89ad4ac3-39f7-470e-963a-56509c546377`) —
   sem caso especial no código/schema.

4. **Atribuição artista↔faixa/álbum via `entity_relation`, sem join table
   nova.** `kind='music_artista_principal'` (billing/álbum) vs.
   `kind='music_artista_feat'` (colaboração pontual na faixa) distingue o
   artista do álbum do artista real da faixa quando divergem — mesmo padrão
   genérico do [AGENTS.md](../../AGENTS.md) §3 ("nunca colunas ou tabelas de
   junção por-tipo"). `sort_index` dá a ordem de billing.

5. **Vínculo arquivo↔faixa é 1:1** (`media_file.entity_id` direto, mesmo
   padrão já usado por filme) — **não** N:M como episódio. Cobre o caso real
   dominante (rip = 1 arquivo = 1 faixa, o que os próprios scripts pessoais do
   dono produzem). Evolução para N:M (`media_file_faixa`, medley/DJ-mix/
   multi-qualidade) fica para quando aparecer necessidade real, sem quebrar o
   modelo — mesmo raciocínio de evolução aberta do [03-arquitetura](../03-arquitetura.md#concorrência-e-persistência-sqlite)
   ("escritor único pode virar fila").

6. **MusicBrainz + Cover Art Archive é o provider base do MVP**, sem chave de
   API. Nova capacidade opcional de árvore (molde do `SerieProvider`) devolve
   artista→álbuns→faixas. Identidade primária por **MBID** — artista, álbum
   (`release`) e faixa (`recording`) compartilham o mesmo esquema de id da
   fonte. **ISRC** entra em `extension_data` como identificador auxiliar de
   faixa (matching/dedup em código de aplicação); **não** vira coluna gerada
   no MVP — evolução se um caso de uso justificar índice dedicado.

7. **Gênero vive no nível de álbum/artista** em `extension_data` — não
   duplicado por faixa. Espelha a convenção observada nos scripts pessoais
   (agrupar por álbum evita reconciliar gênero por faixa vinda de fontes
   diferentes).

## Consequências

- Confirma a promessa do [AGENTS.md](../../AGENTS.md) §3: a vertical inteira
  entra com **zero tabela nova**. `entity`/`entity_relation` genéricos já
  bastam para artista/álbum/faixa e toda a atribuição entre eles. Único touch
  de schema: uma coluna gerada `external_id_mbid`
  (`json_extract(external_ids_json, '$.mbid')`) + índice, espelhando
  `external_id_tmdb`/`_imdb`/`_tvdb`, para lookup rápido por id MusicBrainz.
- `extension_data core.music@1` segue o **mesmo estado real** de
  `core.tv@1`: sem maquinário formal de validação por JSON Schema (ainda
  inexistente, confirmado no código — `internal/tvext`); leitura via um
  parser dedicado (`internal/musicext`, molde de `internal/tvext`), não
  validação declarativa. Aceitar isso agora é o mesmo trade-off já tomado
  pelo precedente de séries.
- Artista/álbum/faixa herdam resolver, override por-idioma, busca FTS, posse
  por `media_file` e artwork **sem tocar o motor** — mesmo ganho que o
  ADR-0021 registrou para séries.
- Um único provider (MusicBrainz) já cobre a vertical inteira sozinha
  (independência de provider, [AGENTS.md](../../AGENTS.md) §6). Spotify/
  Deezer/Discogs/Last.fm — já usados pelos scripts pessoais do dono para
  identificação e download, mas fora do espírito self-hosted/sem-chave do
  projeto — ficam como enriquecimento cruzado **futuro**, nunca dependência
  de base.
- 1:1 arquivo↔faixa é uma aposta deliberada de escopo, não uma decisão
  fechada para sempre: se medley/DJ-mix virar necessidade real, a migração
  para N:M segue o precedente já aberto por `media_file_episode`
  ([ADR-0021](0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md) item 4).
- Decisões relacionadas: [ADR-0021](0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md)
  (modelo de séries, precedente direto), [ADR-0005](0005-composicao-uniforme-extension-data.md)
  (composição uniforme de `extension_data`), [ADR-0011](0011-external-ids-autoritativo-media-external-cache.md)
  (identidade externa autoritativa), [AGENTS.md](../../AGENTS.md) §3 (universal
  e extensível sem tocar o núcleo) e §6 (independência de provider).
