# ADR-0022 — Histórico de consumo: tabela de eventos genérica + capacidade `TrackerProvider`

- **Status:** Aceita
- **Data:** 2026-08-10
- **Agenda:** [H1–H5](../00-agenda-de-decisoes.md) · **Spec:** [04-modelo-de-dados](../04-modelo-de-dados.md), [07-enriquecimento-providers](../07-enriquecimento-providers.md)

## Contexto

O dono pediu um jeito de rastrear **consumo** (assistido/ouvido/lido/jogado) de
forma agnóstica a tipo de mídia e a cliente de origem, citando o ecossistema de
"scrobbling" (Trakt.tv, Last.fm, ListenBrainz, Open Scrobbler) e de tracking
por vertical (scrob.app, sofa.watch, Librarium, GamingGaiden, RetroAchievements,
entre outros). Pesquisa de mercado (5 frentes: filme/série, música, livros,
jogos, e levantamento do schema atual) encontrou um padrão convergente entre
**todo** concorrente self-hosted/relevante, independente da vertical:

1. **Identidade por ID externo já conhecido**, nunca schema próprio de
   catálogo — Trakt/Simkl usam `ids.{tmdb,imdb,tvdb}`; ListenBrainz usa MBID;
   BookWyrm cascateia OpenLibrary→Inventaire/Wikidata; RetroAchievements usa
   hash de ROM; Playnite cruza AppID/GOG-ID/IGDB-ID.
2. **Histórico é sequência de eventos, não estado único mutável** —
   StoryGraph/BookWyrm guardam "progress updates" ao longo do tempo; Hardcover
   trata releitura como N leituras separadas; Trakt/Simkl scrobbam
   `start/pause/stop` com `progress` 0–100 e só fecham "assistido" quando
   `stop` ocorre com `progress ≥ 80%`; RetroAchievements guarda
   `unlock{user, timestamp, modo}` por conquista.
3. **N clientes heterogêneos → 1 schema canônico, via plugin/capacidade** —
   Playnite/SuccessStory agrega Steam+GOG+Epic+RetroAchievements+manual num só
   schema de "achievement"; Maloja e Koito falam simultaneamente Last.fm **e**
   ListenBrainz pra herdar clientes de graça; scrob.app importa/exporta de
   Trakt/Simkl/MDBList ao mesmo tempo.

O único precedente hoje no projeto (migration 0019, briefing 0088) é o oposto
disso: seis colunas soltas em `entity` (`watched`, `play_count`, `user_rating`,
`last_played_at`, `resume_position_seconds`, `resume_total_seconds`),
preenchidas **passivamente** por uma releitura pontual do `.nfo` já em disco,
sem histórico (1 valor, não N eventos), sem porta de entrada em tempo real
(webhook) e sem nenhuma API de escrita. Foi uma exceção justificada (espelhar
o `.nfo`, sem equivalente em provider externo) — não é o padrão a estender
para um sistema de tracking de verdade.

## Decisão

1. **Histórico é uma tabela de eventos genérica, não colunas por vertical.**
   Nova tabela filha de `entity`, no mesmo molde de `asset`/`media_file`
   (`id TEXT PRIMARY KEY`, `entity_id TEXT NOT NULL REFERENCES entity(id) ON
   DELETE CASCADE`, `created_at`), **append-only**:
   `historico_evento(id, entity_id, kind, status, progress_percent,
   position_seconds, occurred_at, source, payload_json, created_at)`.
   `kind` (`'assistir'|'ouvir'|'ler'|'jogar'|…`), `status`
   (`'started'|'paused'|'stopped'|'completed'|'dropped'|…`) e `source`
   (`'local'|'manual'|'jellyfin-webhook'|'plex-webhook'|'emby-webhook'|…`) são
   **TEXT livre, sem CHECK fechado** — princípio 3 do `AGENTS.md` (nunca
   coluna/tabela por-tipo). `payload_json` guarda extras namespaced por
   vertical (episódio, página, faixa) sem exigir migration nova quando uma
   vertical futura precisar de um campo específico.

2. **"Marco"/conquista é CATÁLOGO + evento — nunca vira um evento sozinho.**
   Revisão da decisão inicial (que tratava conquista só como
   `kind='conquista'` dentro de `historico_evento`): a definição de uma
   conquista (título, descrição, ícone, pontos) é dado de **catálogo** — tem
   que existir e ser navegável mesmo ANTES do dono desbloqueá-la (é assim que
   RetroAchievements e Steam expõem: a lista inteira de conquistas possíveis
   de um jogo, com as ainda não obtidas visíveis). Um evento não modela isso.
   Decisão:
   - A definição vira uma **`entity` própria** (`type='conquista'`), ligada
     ao item-pai (jogo, álbum, o que for) pelo `entity_relation` **genérico
     já existente** (`kind='conquista'`, `parent_id`=item-pai,
     `child_id`=conquista) — o MESMO mecanismo que já liga série→temporada→
     episódio e coleção→filme, sem tabela nova. Título/descrição passam pelo
     resolver normal (`override ⟵ externo`); o ícone é um `asset` comum
     (mesmo pipeline de pôster/fanart, sem `kind` novo obrigatório além do já
     livre).
   - O **desbloqueio** (quando o dono alcançou aquilo) é que vira
     `historico_evento` — com `entity_id` apontando para a **conquista**
     (não para o item-pai), `kind='conquista'`, `status='unlocked'`. Reusa o
     schema do item 1 sem alteração nenhuma.
   - Dado de condição de desbloqueio específico da fonte (ex.: a fórmula de
     endereço de memória que o RetroAchievements usa para saber que algo
     aconteceu) é **opaco ao núcleo** — guardado namespaced em
     `extension_data` da própria conquista, nunca interpretado pelo
     MyMediaDB; só quem sabe o formato daquela fonte (um bridge externo
     rodando junto do emulador, por exemplo) o interpreta.
   - Importar o catálogo de conquistas de uma fonte externa é trabalho de uma
     nova capacidade opcional `MarcoProvider` (mesmo molde de
     `SimilarProvider`: dado o id externo do item-pai, devolve a lista de
     conquistas para importar como entities filhas). **Fora desta entrega** —
     desenhada, sem provider concreto.
   - Nenhuma tabela nova em nenhum dos pontos acima: `entity`,
     `entity_relation`, `asset`, `extension_data` e `historico_evento` já
     bastam — reforça o item 1 em vez de o contradizer.

3. **Identidade de verticais baseadas em hash de arquivo (ex. ROM) reaproveita
   `media_file.content_hash`.** Nenhuma coluna nova: o scanner já calcula um
   hash de conteúdo por arquivo (hoje usado para detectar mudança de
   arquivo); para uma vertical estilo RetroAchievements, esse mesmo hash é o
   "documento de identidade" do jogo. Uma ROM de fan-translation/hack (hash
   diferente do oficial) é modelada como **mais um `media_file` da mesma
   `entity`** — o padrão já normativo "1 obra = N arquivos/edições"
   ([ADR-0008](0008-identidade-e-matching-kodi-jellyfin.md)/MD1), sem
   conceito novo. Um hash que nenhum provider reconhece degrada
   graciosamente (item permanece catalogado, só sem match automático daquela
   fonte — princípio 6); a correção é a mesma já usada para match de
   filme/série (MD1: "correção do dono é autoritativa" — o dono liga o
   arquivo/hash ao item certo na mão). Se o deslocamento de memória de um
   patch exigir uma condição de desbloqueio diferente da oficial só para
   aquele hash, o dono sobrescreve **apenas aquele campo** pelo mecanismo de
   override **já existente e universal** (`media_override`,
   [R6](../00-agenda-de-decisoes.md) — presença da chave = override ativo) —
   não é mecanismo novo, é o override universal do projeto aplicado a mais um
   campo.

4. **"Estado atual" é projeção derivada do histórico, não uma segunda fonte de
   verdade mutável.** O que hoje `watched`/`play_count` tentam representar em
   `entity` passa a ser calculável a partir do último evento por
   `(entity_id, kind)` — evita duas fontes de verdade divergentes.

5. **A migration 0019 não é migrada nem unificada agora.** Continua sendo o
   espelho pontual do `.nfo` (leitura passiva, best-effort, exceção já
   documentada). Tensão conhecida e aceita: os dois mecanismos convivem sem
   sincronização automática entre si nesta entrega — unificar fica como
   evolução, só se a prática mostrar necessidade real (ex.: um job que deriva
   `watched`/`play_count` a partir do histórico nada exportar de volta ao
   `.nfo`).

6. **Ingestão (entrada) e sincronização (saída) são mecanismos distintos:**
   - **Entrada via adapters de webhook**, registro próprio (não é uma
     capacidade de `provider.Provider` — não busca/mapeia catálogo, só
     recebe e normaliza um payload de player). Cada media server
     (Jellyfin/Plex/Emby) ganha um adapter que traduz seu payload
     proprietário para um `historico_evento`. Primeira entrega: só Jellyfin
     (webhook mais simples e documentado publicamente; Plex exige Plex
     Pass) — ver briefing 0093.
   - **Saída/sincronização via nova capacidade opcional `TrackerProvider`**
     em `internal/provider/provider.go`, seguindo EXATAMENTE o molde de
     `SimilarProvider`/`ArteProvider` (interface pequena, type-assert pelo
     chamador, degrada em silêncio quando ausente). Permite importar/exportar
     histórico de/para um serviço externo (Trakt como primeiro candidato)
     quando um item já está casado com `external_ids`. **Fora desta
     entrega** — capacidade fica desenhada, sem implementação concreta ainda
     (nenhum provider a implementa nesta fase).

7. **Vocabulário de interoperabilidade alinhado ao que já convergiu no
   mercado**, para minimizar fricção de um futuro `TrackerProvider`:
   identidade por `ids.{tmdb,imdb,tvdb}` (já é `entity.external_ids_json`),
   `progress_percent` 0–100, e o mesmo limiar de **80%** que Trakt e Simkl
   usam para fechar um evento como `completed`.

8. **Fatiamento do épico** (briefings 0092–0094): schema genérico + API
   mínima primeiro (0092); webhook de player em tempo real depois, cobrindo
   só filme/série na primeira integração concreta (0093); UI de
   estado/histórico no detalhe do item por último (0094). `TrackerProvider`
   concreto (Trakt) e as verticais de música/livro/jogo reaproveitam o MESMO
   schema em briefings futuros, sem migration nova.

## Consequências

- Ganha-se histórico real (N eventos por item, progresso ao longo do tempo,
  múltiplas sessões) sem tocar na migration 0019 nem no export de `.nfo`.
- Qualquer vertical futura de CONSUMO CONTÍNUO (música/livro/jogo) entra
  usando a MESMA tabela — `kind` novo, zero migration — coerente com o
  princípio 3 (universal e extensível sem tocar no núcleo).
- "Marco"/conquista (catálogo navegável + desbloqueio) também não pede tabela
  nova, mesmo sendo estruturalmente mais rico que um evento simples —
  `entity`/`entity_relation`/`asset` já cobrem a definição, `historico_evento`
  já cobre o desbloqueio. Prova de que o modelo genérico aguenta um caso mais
  complexo (RetroAchievements/Steam) sem precisar crescer.
- Identidade por hash de arquivo (ROM) reaproveita `media_file.content_hash`
  já calculado pelo scanner — zero coluna nova; variantes de hash
  (fan-translation) e correção de condição por hash reaproveitam mecanismos
  já existentes (MD1 correção manual de match + R6 override universal), sem
  mecanismo novo específico de jogos.
- Webhook do Jellyfin é o menor esforço de maior alavancagem: funciona sem o
  dono precisar de conta em nenhum serviço externo.
- `TrackerProvider` fica pronto como ponto de extensão real (não decorativo)
  desde já, mesmo sem implementação concreta — o dia que Trakt/ListenBrainz
  forem implementados, plugam sem mudar quem chama (mesmo padrão pago por
  `ColecaoProvider`/`ArteProvider`/`SimilarProvider`).
- Custo aceito: dois mecanismos de "estado de reprodução" convivem por ora
  (migration 0019 passiva via NFO + `historico_evento` ativo via
  webhook/API) — não há hoje sincronização entre eles; revisitar se isso
  causar confusão prática visível ao dono.
- Decisões relacionadas: [ADR-0011](0011-external-ids-autoritativo-media-external-cache.md)
  (identidade externa autoritativa, base para `TrackerProvider`),
  [ADR-0008](0008-identidade-e-matching-kodi-jellyfin.md) (1 obra = N
  arquivos/edições; correção de match é autoridade do dono — base do
  tratamento de hash de ROM/fan-translation), [ADR-0005](0005-composicao-uniforme-extension-data.md)
  (`extension_data` namespaced, base do dado opaco de condição de
  desbloqueio), [princípio 3 e 6 do `AGENTS.md`](../../AGENTS.md)
  (extensibilidade sem tocar núcleo; independência de provider + capacidade
  opcional).
