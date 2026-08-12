# 00 — Agenda de decisões (documento vivo)

> **O que é:** o backlog das decisões de **fundação** que ainda precisam ser
> tomadas ou documentadas. Não é especificação — é a lista de trabalho. Cada item
> aqui, quando decidido, vira texto nas docs de spec (`04`, `03`, …) ou num ADR.
>
> **Como trabalhamos:** um item por vez. Para cada item, o fluxo é:
> 1. Contexto do problema + resumo para leigos.
> 2. Perguntas objetivas (com opções sugeridas) até a decisão fechar.
> 3. A decisão é registrada na coluna **Decisão** abaixo e promovida ao documento
>    de destino.
>
> **Status possíveis:** `aberto` · `em discussão` · `decidido` · `documentado`.

---

## Foco atual: especificação do resolver

O resolver compõe o valor final de cada entidade a partir de três camadas
(`override ⟵ externo ⟵ arquivo`). É o coração do sistema e hoje está subespecificado.
Estas são as decisões que fecham a spec dele.

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **R1** | As camadas competem pelo mesmo campo ou o espaço de campos é particionado por origem? | **documentado** | Particionar por origem. A origem é **metadado do campo**, não hard-code: núcleo declara no schema do núcleo; extensões/plugins declaram no JSON Schema do próprio namespace. Resolver é motor **genérico** (não conhece campo por nome). A anotação declara só as **fontes-base** (`arquivo`/`externo`/`nenhuma`); **override é universal** (nunca declarado). Campos de sistema (`id`, `created_at`, `content_hash`) ficam fora do resolver. | `04` (+ nova seção resolver) |
| **R2** | Com duas fontes externas (TMDB+TVDB) no mesmo campo, quem vence? | **documentado** | **Lista de prioridade global configurável** de fontes (ex.: `[tmdb, tvdb, …]`); por campo, vence a primeira fonte da lista que tiver valor — antes do override. Default MVP: `[tmdb]`. Alternativas (prioridade por campo, fonte primária por entidade) registradas como evolução pós-MVP. | `04` / ADR |
| **R3** | Override em campos-lista (tags, gêneros, elenco): substitui a lista inteira ou faz merge? | **documentado** | **Merge por operações** (não substituição). O override guarda um conjunto de **adições** e um de **remoções por valor**; a base externa segue viva por baixo (re-scrape evolui o resto). Ordem = externa, com remoções aplicadas e adições ao final; dedup ao compor. Vale **só para listas de valores simples** (`tags`, `genres`). Elenco/equipe (relações) → ver MD6. | `04` |
| **R4** | Armazenamento do override: EAV `(entity_id, field, value)` vs blob JSON esparso. **(mexe na tabela da doc 04)** | **documentado** | **Blob JSON esparso**: `media_override(entity_id, override_json, updated_at)` — uma linha por entidade, JSON espelhando o formato da entity, só com campos mudados. Validável contra o mesmo schema (núcleo+plugins, R1); comporta o `{add,remove}` do R3 e override dentro de `extension_data` (R5). Resolver = base composta + aplica patch. **Substitui o esboço EAV na doc 04.** | `04` / ADR |
| **R5** | `extension_data` também passa pelo resolver (composição uniforme)? | **documentado** | **Sim, composição uniforme.** Núcleo e extensão passam pelo mesmo resolver; `override ⟵ externo ⟵ arquivo` vale campo a campo, inclusive dentro dos namespaces de `extension_data`. Coerente com R1 (motor genérico) e R4 (blob espelha a entity inteira). | `04` |
| **R6** | Como o dono "apaga" um valor — distinguir "não fiz override" de "override para vazio"? | **documentado** | **Presença da chave = override ativo**, mesmo com `null`/vazio (apaga o valor externo). Chave ausente = herda da base. A UI precisa separar "limpar override" (remove a chave) de "definir vazio" (chave com `null`). **Proveniência por campo não é exposta na API por ora** — a UI de edição consulta o override separadamente; expor proveniência fica como possível evolução (Fase 4). | `04` |
| **R7** | Fatos técnicos (mediainfo) são overridáveis pelo dono? | **documentado** | **Sim**, pelo mesmo mecanismo universal (R1) — sem regra especial. O dado técnico detectado permanece em `media_file` (não-destrutivo); o override entra por cima na composição. Uso raro (corrigir detecção errada do ffprobe), mas permitido. | `04` |

---

## Modelo de dados (fora do resolver, mas de fundação)

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **MD1** | Identidade e matching: como um arquivo vira/associa a uma `Entity` e a um id externo? Um filme com múltiplos arquivos (edições/qualidades) — relação 1:N normativa. | **documentado** | Adota a **convenção Kodi/Jellyfin** (padrão de-facto): **1:N plano** (1 obra = 1 `Entity`; qualidades/edições/partes = `media_file`), **edição = rótulo no arquivo**, **id externo embutido no nome = chave determinística**, *stacking* de partes. Match carrega **score de confiança**: alta casa sozinho, baixa **casa mesmo assim + alerta de revisão** (*"mapeado com 60% de certeza. Remover?"*), zero resultados vira `draft` não-identificado; correção do dono é autoritativa (override). Verticais fora do padrão (videoclipe→IMVDB, quadrinhos→ComicInfo) trazem identidade própria via `extension_data`, sem tocar no núcleo. | `06` (+ `04`) |
| **MD2** | `status` (intenção do dono) vs posse (existe `media_file`?) estão conflados na doc 04. Separar em eixos ortogonais. | **documentado** | **Dois eixos ortogonais.** **Posse = fato derivado** (existe `media_file`?), não editável, não mente. **`status` = intenção do dono**, um estado por vez: `cataloged` (default), `wanted`, `unreleased`, `draft`. Removido o `owned` (era posse disfarçada). Combinam livre (ex.: `wanted`+posse=sim = quero upgrade tendo cópia). Transições entre estados → MD5. | `04` |
| **MD3** | Concorrência no SQLite: API sempre no ar enquanto o scan escreve pesado (single-writer). Estratégia (WAL + escritor único). | **documentado** | Receita em 3 camadas. **(1) WAL + pragmas**: `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON` — vários leitores + 1 escritor sem bloqueio mútuo. **(2) Dois pools**: pool de N leitores + **1 conexão de escrita** (escritor único). **(3) Escritas curtas, dono na frente**: scan comita em lotes curtos; escrita interativa (override/import) tem prioridade sobre o scan. Evolução: fila/goroutine escritora se precisar priorização explícita. | `03` |
| **MD4** | Identidade externa duplicada em `entity.external_ids` e `media_external.(source, source_id)`. Qual é a autoritativa? | **documentado** | **`entity.external_ids` é o mapa de identidade autoritativo** (todos os ids conhecidos, buscados ou não; é onde o match do MD1 grava e o lookup lê). **`media_external` é cache de payload**, `source_id` subordinado (invariante: bate com `external_ids[source]`). **Conflito → identidade vence**: linha divergente é cache obsoleto → re-busca/descarta; o dono corrige a identidade, nunca edita `media_external` à mão. Identidade ≠ valor editorial (media_external segue base do resolver). Mesma lógica p/ `person`/`company`. | `04` |
| **MD5** | Ciclo de vida / deleção: arquivo some do disco — a entity vira `wanted`? órfã? Comportamento não-destrutivo definido. | **documentado** | **Evento de arquivo mexe só na posse; nunca no `status` nem no trabalho do dono.** Arquivo some (datasource acessível) → `media_file` vira **`missing`** (guarda `last_seen`), entity preservada, posse→não, status **intacto**. **Datasource inteiro offline** (HD desmontado) → nada vira `missing`, datasource sinalizado offline. Arquivo aparece p/ `wanted` → posse=sim, segue `wanted`. **Órfã** = entity auto-criada, arquivo sumiu, **zero investimento do dono** → fila de limpeza, **nunca apagada sozinha**. **Purga** é a única operação destrutiva, sempre explícita. | `04` (+ `06`) |
| **MD6** | Override de **relações** (elenco/equipe): merge por `person_id`, edição de papel/ordem. Separado do R3 por ser bem mais complexo (tabelas `person`/`credit`). | **documentado** | **R3 generalizado para créditos estruturados.** Override guarda **operações** (add/remove/edit de papel/`ord`); base externa (`credit` do payload) segue viva e evolui em re-scrape. **Chave = `person_id` + papel/departamento** (distingue a mesma pessoa em 2 papéis), com `credit_id` do provider como desempate. Pessoa adicionada pelo dono vira `person` **local** (`external_ids` vazio, ok por MD4). Resolver compõe crédito a crédito (R5); ops vivem no `media_override` (R4). | `04` |

---

## Idiomas (multi-idioma) — fundacional, levantado na Fase 3

O acervo é multi-idioma: o gestor define principal + fallback + permitidos, todo
valor carrega a flag do idioma, e o dono acrescenta traduções. Reformula o modelo
das três camadas antes de o override (0016) aterrissar.

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **L1** | Como o idioma entra no modelo das três camadas? Onde vive a config (principal/fallback/permitidos)? | **documentado** | **Dimensão de idioma = CHAVE dentro de cada camada** (externo tagueia por idioma no payload `/translations`; override fica `campo→idioma→valor`; núcleo é o fallback neutro), sem tabela nova. Campos traduzíveis (`title`/`description`/`tags`) vs neutros (`original_title`/`original_language`/`release_date`/`external_ids`/técnicos). Nova coluna `entity.original_language`. **Config em settings no banco** (`app_settings`, chave `languages`) com UI de admin — não em env. | [ADR-0018](adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md) |
| **L2** | Qual idioma a API serve e o que expõe sobre isso? | **documentado** | **Resolve para UM idioma** por requisição (`?lang=`, default = principal), shape **plano** preservado; **cadeia de fallback** `pedido → fallback → original → arquivo`; **autoridade do dono por idioma** (R6 por idioma; `null` no pedido interrompe o fallback); **proveniência de idioma** exposta (`resolved_languages`). | [ADR-0019](adr/0019-resolucao-por-idioma-fallback-proveniencia.md) |
| **L3** | Como traduzir tags (o TMDB não traduz keywords)? | **documentado** | **Tags viram lista por idioma**, merge de operações (R3) **dentro de cada idioma**; **gêneros** pela lista localizada do TMDB por idioma (cacheável); **keywords** guardadas no idioma obtido, tradução **manual** pelo dono (sem tradução automática). | [ADR-0020](adr/0020-traducao-de-tags-genres-keywords.md) |

---

## Séries (TV) — hierarquia, ordenações e providers — fundacional, levantado pós-0030

Séries são o salto além de filmes (item único): hierarquia série→temporada→episódio
e as múltiplas ordenações de temporada (aired/DVD/absolute). Fundadas por **pesquisa
empírica** dos dois providers (TMDB + TheTVDB) e das convenções de media servers.

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **TV1** | Como representar as múltiplas ordenações de temporada sem duplicar episódios? | **documentado** | **Episódio = grão fino, entity canônica única** (ancorada no `episode.id`); **ordenação (season type) = projeção de numeração, nunca muda granularidade** (confirmado: união de todas as ordens = superconjunto `official`, zero ids próprios nas alternativas; nenhum provider tem "episódio combinado"). Numeração multi-ordem no `extension_data core.tv@1`. | [ADR-0021](adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md) |
| **TV2** | Temporada é entity (com artwork/override próprios) para toda ordenação ou só a default? | **documentado** | **Enxuto: temporada-entity só para a ordenação _default_ da série** (configurável). Ordens alternativas = numeração derivada, agrupada na leitura, sem entity. (Uniforme — ~103 entities p/ Looney Tunes — descartado.) | [ADR-0021](adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md) |
| **TV3** | Como ligar um arquivo que empacota vários episódios (rip de streaming) ao catálogo? | **documentado** | **Vínculo arquivo↔episódio N:M, ancorado no id** (junção `media_file_episode(…, ordinal)`). O "combinado" é propriedade do **arquivo/release**, não do catálogo: 1 arquivo→N episódios; 1 episódio→N arquivos (multi-part, herdado de filmes). Ancorar no id (não no número) imuniza contra re-scrape. | [ADR-0021](adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md) |
| **TV4** | Duas fontes externas (TMDB+TheTVDB) para séries — como convivem? | **documentado** | **Executa R2**: registry `nome→Provider` + lista global `[tmdb, tvdb]` (substitui o singular e o `["tmdb"]` hardcoded). TheTVDB fornece ordenações + artwork exclusivo; séries só-TheTVDB (ex. Looney Tunes, sem `remoteId` TMDB) usam identidade primária do TheTVDB. Interface opcional `SerieProvider` (análoga a `ColecaoProvider`). | [ADR-0021](adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md) · [ADR-0002](adr/0002-prioridade-entre-fontes-externas.md) |
| **TV5** | Como fatiar o épico? | **documentado** | **Enriquecimento primeiro** (Fase 1, briefing [0031](../briefings/0031-series-fundacao-modelo-enriquecimento.md)): schema + provider TheTVDB + registry + buscar/importar/exibir a árvore dos providers. Fase 2 = scanner + naming de TV + ligação N:M do disco (conferir convenção Kodi/Jellyfin). Fase 3 = seletor de ordenação, artwork de temporada, i18n. | briefings 0031+ |

---

## Melhorias de base (estrutura da documentação)

| ID | Item | Status | Decisão | Destino |
|---|---|---|---|---|
| **ME1** | Adotar ADRs (um arquivo por decisão) em vez da lista achatada da doc 11. | **documentado** | **Sim, formato leve.** Criado `docs/adr/` com README (índice + prática), `template.md` (Contexto·Decisão·Consequências) e **13 ADRs** migrados das decisões fechadas (R1–R7 → 0001–0007; MD1–MD6 → 0008–0013). ADR é **imutável**: nova decisão que supera outra cria novo ADR marcando `Substituída por`. | `docs/adr/` |
| **ME2** | Diagrama ER (mermaid) do modelo de dados. | **documentado** | Diagrama `erDiagram` (mermaid) na doc 04, após o esboço de tabelas: entity + as 3 camadas do resolver (`media_file`/`media_external`/`media_override`) + `credit`/`person`/`asset`/`entity_relation`/`library_source`. `company` sem aresta rígida (join deixado ao briefing de schema). | `04` |
| **ME3** | Criar `LICENSE` e `CONTRIBUTING.md` (inclinação MIT/Apache-2.0). | **documentado** | **MIT.** `LICENSE` (MIT, © 2026 Rafael Vieiras e contribuidores) + `CONTRIBUTING.md` na raiz (princípios, fluxo decisão→ADR→spec, commits em pt-BR, segredos nunca no código). | raiz |
| **ME4** | Glossário: termos de precedência do resolver e "posse" vs "status". | **documentado** | Doc 02 atualizada: `status` corrigido (intenção, sem `owned`), **Posse** adicionada (fato derivado), + termos Precedência do resolver, Override universal, Merge por operações, Proveniência, `missing`, Órfã, Purga, `external_ids`/`media_external`. | `02` |

---

## Histórico de consumo (tracking/scrobbling) — fundacional, levantado a partir de pesquisa de mercado

O dono pediu um sistema de tracking/marcos agnóstico a tipo de mídia e a
cliente de origem, inspirado no ecossistema de scrobbling (Trakt.tv, Last.fm,
ListenBrainz, Open Scrobbler) e de tracking por vertical (scrob.app,
sofa.watch, Librarium, GamingGaiden, RetroAchievements e concorrentes).
Pesquisa de mercado em 5 frentes (filme/série, música, livros, jogos, schema
atual) fundamenta as decisões abaixo.

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **H1** | Histórico é coluna(s) de estado (como a migration 0019) ou eventos? | **documentado** | **Tabela de eventos genérica, append-only** (`historico_evento`, filha de `entity`, `kind`/`status`/`source` livres). Todo concorrente relevante modela histórico como sequência de eventos (progress updates, múltiplas leituras/plays), nunca 1 valor mutável. "Estado atual" vira projeção derivada do último evento, não segunda fonte de verdade. Migration 0019 não é migrada/unificada agora (tensão conhecida e aceita). | [ADR-0022](adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md) |
| **H2** | Entrada de eventos: webhook de player, API própria, capacidade de provider, ou os três? | **documentado** | **Webhook (adapter próprio, não é `provider.Provider`) + API manual primeiro**; capacidade de provider (`TrackerProvider`) fica reservada para SAÍDA/sincronização com serviço externo (Trakt), desenhada mas sem implementação concreta nesta entrega. Primeira integração de webhook: só Jellyfin (payload mais simples/documentado; Plex exige Plex Pass). | [ADR-0022](adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md) |
| **H3** | Vocabulário de campos (progresso, threshold de "assistido", identidade externa) — inventar formato próprio ou convergir com o mercado? | **documentado** | **Convergir**: identidade por `ids.{tmdb,imdb,tvdb}` (já é `entity.external_ids_json`), `progress_percent` 0–100, limiar de **80%** pra fechar `completed` — mesmo vocabulário que Trakt e Simkl já convergiram, minimiza fricção de um `TrackerProvider` futuro. | [ADR-0022](adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md) |
| **H4** | Como fatiar o épico — tudo de uma vez (schema+webhook+UI+`TrackerProvider`, todas as verticais) ou incremental? | **documentado** | **Incremental, só filme/série na primeira entrega** (schema já nasce agnóstico de vertical). Ordem: schema+API mínima (0092) → webhook Jellyfin (0093) → UI de estado/histórico (0094). `TrackerProvider` concreto, demais verticais (música/livro/jogo) e a implementação de "marcos"/conquistas (modelo já desenhado em H5) ficam para depois — nenhum pede migration nova quando chegar a vez. | briefings 0092–0094 |
| **H5** | "Marco"/conquista (RetroAchievements/Steam-style) — evento único, ou catálogo navegável (título/descrição/ícone mesmo sem desbloquear) + evento de desbloqueio? | **documentado** | **Catálogo + evento**, revisão da decisão inicial (que tratava conquista só como evento). Definição da conquista = `entity` própria (`type='conquista'`), ligada ao item-pai pelo `entity_relation` genérico já existente; ícone = `asset` comum. Desbloqueio = `historico_evento` apontando pra conquista. Condição de desbloqueio específica da fonte (ex. endereço de memória do RetroAchievements) fica opaca em `extension_data`, nunca interpretada pelo núcleo. Nova capacidade `MarcoProvider` (import do catálogo) segue o molde de `SimilarProvider`. Identidade de jogos por hash de ROM reaproveita `media_file.content_hash` já existente; fan-translation (hash diferente) = mais um `media_file` da mesma entity (MD1); hash não reconhecido degrada sem travar, correção é manual (MD1); condição de desbloqueio divergente por hash usa o override universal já existente (R6) — nenhum mecanismo novo em nenhum dos casos. | [ADR-0022](adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md) |

---

## Integrações configuráveis (UI) — fundacional, levantada a partir de pedido do dono

O dono pediu uma tela `/dashboard/integrations` (subpágina por integração,
formulário dinâmico) para configurar credenciais de provider (TMDB primeiro)
que hoje só existem como variável de ambiente lida uma vez no boot — e que
sirva de ponto de extensão para plugins futuros registrarem suas próprias
integrações.

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **I1** | Onde a config de integração mora e qual a precedência frente à variável de ambiente hoje usada para credenciais? | **documentado** | **Banco tem precedência; env var vira bootstrap/fallback** — mesmo formato override-vence do resolver (R1–R7) e mesmo precedente do L1 (idiomas em banco+UI, não env). Atualiza a letra da regra de segredos de AGENTS.md/CONTRIBUTING.md (texto novo abaixo). | [ADR-0023](adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md) |
| **I2** | Como uma integração (núcleo hoje, plugin amanhã) declara seus campos de config sem o framework conhecer cada fonte por nome? | **documentado** | Interface `integration.Descriptor` (`Slug/Nome/Descricao/Campos/Configurada`) + `integration.Registry`, mesmo molde do `provider.Registry` (capacidade-opcional, ordem de registro preservada). Cada provider dono de credencial contribui seu próprio adapter. | [ADR-0023](adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md) |
| **I3** | Como a API expõe isso pro front renderizar formulário dinâmico, sem vazar segredo cru? | **documentado** | `GET /integrations`, `GET/PUT /integrations/{slug}`, mesmo vocabulário de descritor do form-por-schema (briefing 0030: `name/label/widget/...`). Campo `secret` nunca devolve valor cru (só `definido`+`origem`). PUT é parcial e esparso (R6: `null` remove o override daquele campo). | [ADR-0023](adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md) |
| **I4** | Hot-reload da credencial ao salvar, ou restart? Como fatiar o épico? | **documentado** | **Sem hot-reload no MVP** (salvar grava no banco; efeito só no próximo restart do processo — já um ganho sobre editar `.env`+redeploy). Hot-reload e "testar conexão" ficam de follow-up explícito. Dois briefings: **0095** (fundação: schema+registry+API+migra TMDB) → **0096** (UI `/dashboard/integrations`). | briefings 0095–0096 |

---

## Sobrevivência offline do catálogo — fundacional, levantada a partir da migração de 12/08/2026

Na migração de produção para o Batocera (12/08/2026), o ambiente novo subiu sem
nenhuma credencial de provider configurada. Sintoma: itens já catalogados há
meses, com `media_external`/`external_detail` intactos (copiados na migração),
apareceram SEM sinopse/elenco/equipe/tags/similares — não por perda de dado, mas
porque o código de leitura (`internal/server/entities.go:montarExterno`/
`camadasDaFonte`) usa o MESMO gate (`provider.Registry`) tanto para "pode buscar
ao vivo" (precisa de chave, correto) quanto para "pode interpretar o payload já
salvo" (função pura, não deveria precisar de chave nenhuma). Investigação completa
e decisão em [ADR-0024](adr/0024-cache-externo-sobrevive-sem-provider-vivo.md).

| ID | Questão | Status | Decisão | Destino |
|---|---|---|---|---|
| **PR1** | Leitura de `media_external`/`external_detail` já cacheado deve exigir provider com credencial viva, ou só a BUSCA nova deve exigir? | **em implementação** | **Separar as duas capacidades**: parse de payload já salvo (`MapearDetalhe`/`Similares`/richfields) fica disponível para qualquer `source` conhecida do binário, credencial presente ou não; só `BuscarDetalhe` (busca nova) continua gated por credencial. `externaldetail.PayloadFresco` passa a servir o payload stale quando a re-busca falha, em vez de descartar o dado. | [ADR-0024](adr/0024-cache-externo-sobrevive-sem-provider-vivo.md) / [briefing 0100](../briefings/0100-cache-externo-sobrevive-sem-provider-vivo.md) |

---

## Histórico de decisões fechadas

<!-- Conforme cada item fecha, mover o resumo da decisão para cá com data. -->

- **I1–I4 — Integrações configuráveis via UI: banco com precedência sobre env**
  (2026-08-10). Substitui o hardcode de credencial (TMDB/TVDB/fanart.tv só
  como variável de ambiente lida no boot) por uma tela `/dashboard/
  integrations` com subpágina por integração. Decisão: **banco (via UI) vence,
  env var vira bootstrap/fallback** — o mesmo formato override-vence do
  resolver (R1–R7), com precedente já aberto pelo L1 (idiomas em banco+UI).
  Novo pacote `internal/integration` (`Descriptor`+`Registry`, mesmo molde do
  `provider.Registry`) é o primeiro ponto de extensão concreto para o futuro
  sistema de plugins (docs/11) — sem implementar o maquinário completo de
  plugins agora. Segredo nunca volta cru pela API (só `definido`+`origem`);
  **sem hot-reload no MVP** (restart aplica a troca); "testar conexão" fica de
  follow-up. Atualiza a letra da regra de segredos em AGENTS.md/
  CONTRIBUTING.md. Registrado no [ADR-0023](adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md),
  fatiado em briefings **0095** (fundação) e **0096** (UI).

- **H1–H5 — Histórico de consumo: eventos genéricos + catálogo de marcos +
  `TrackerProvider`** (2026-08-10). Pesquisa de mercado em 5 frentes
  (filme/série: Trakt, scrob.app, sofa.watch, Simkl, Watcharr; música:
  Last.fm, ListenBrainz, Maloja, Koito; livros: Librarium, BookWyrm,
  Hardcover, StoryGraph; jogos: RetroAchievements, Playnite/SuccessStory,
  GamingGaiden) encontrou um padrão convergente: identidade por ID externo já
  conhecido, histórico como sequência de eventos (nunca estado único), e N
  clientes heterogêneos agregados por um schema canônico via plugin/
  capacidade. Decisão: tabela `historico_evento` genérica (append-only,
  `kind` livre), entrada por webhook de player (Jellyfin primeiro) + API
  manual, saída/sync como nova capacidade opcional `TrackerProvider`
  (desenhada, sem provider concreto ainda). **Revisão H5** (mesma sessão, a
  partir de pergunta direta do dono sobre RetroAchievements/Steam): "marco"/
  conquista NÃO é um evento sozinho — é catálogo (`entity`+`entity_relation`+
  `asset`, navegável mesmo sem desbloquear) + evento de desbloqueio; condição
  de desbloqueio específica de fonte fica opaca em `extension_data`; hash de
  ROM reaproveita `media_file.content_hash` já existente; fan-translation e
  correção de condição por hash reaproveitam mecanismos já existentes (MD1 +
  R6), sem nada novo. Épico fatiado em 3 briefings (0092–0094), só
  filme/série na primeira entrega.

- **TV1–TV5 — Fundação de séries (hierarquia, ordenações, providers)** (2026-07-11).
  Fecha o modelo de séries antes de qualquer código, fundado em **pesquisa empírica**
  das duas APIs (Simpsons, Looney Tunes 72514, One Piece) e das convenções de media
  servers (Jellyfin/Kodi/Plex/Sonarr). Achado decisivo: **os dois providers
  convergem** — o **episódio é a entidade canônica de grão fino** e trocar de
  ordenação (season type / episode group) é **pura renumeração, nunca muda a
  granularidade** (união de todas as ordens = superconjunto do `official`, zero ids
  próprios nas alternativas; não existe "episódio combinado" em nenhum provider).
  **TV1:** numeração multi-ordem no `extension_data core.tv@1` sobre o episódio fino.
  **TV2:** temporada-entity só para a ordenação _default_ (enxuto); alternativas são
  numeração derivada. **TV3:** o "combinado" (streaming empacota curtas) é do
  **arquivo**, não do catálogo → vínculo **arquivo↔episódio N:M ancorado no id**
  (`media_file_episode`), que ancorar no número quebraria no re-scrape (bug do
  Sonarr). **TV4:** duas fontes executam o **R2** (registry + lista `[tmdb, tvdb]`),
  com séries exclusivas do TheTVDB usando identidade primária dele. **TV5:**
  **enriquecimento primeiro** (briefing 0031), scanner e ligação de arquivos na fase
  seguinte. Registrado no [ADR-0021](adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md).
- **L1–L3 — Fundação multi-idioma** (2026-07-09). Requisito fundacional levantado na
  Fase 3 e ausente do design inicial: o acervo é multi-idioma. **L1 (ADR-0018):** a
  dimensão de idioma é a **chave dentro de cada camada** (payload externo tagueado
  por idioma via TMDB `/translations`; override `campo→idioma→valor`; núcleo como
  fallback neutro), sem tabela nova; campos traduzíveis (`title`/`description`/`tags`)
  vs neutros; nova coluna `entity.original_language`; **config em settings no banco**
  (`app_settings`) com UI de admin. **L2 (ADR-0019):** a leitura **resolve para um
  idioma** (`?lang=`, default principal) mantendo o shape plano, com **cadeia de
  fallback** (`pedido → fallback → original → arquivo`), autoridade do dono **por
  idioma** (R6 por idioma; `null` no pedido não cai para o fallback) e
  **proveniência de idioma** exposta. **L3 (ADR-0020):** **tags por idioma** com
  merge de operações dentro de cada idioma — gêneros pela lista localizada do TMDB,
  keywords no idioma obtido com tradução manual. Implementação em briefings
  0019–0021 (fundação → provider → UI), reformando 0012/0013/0015 e antecedendo o
  0016 (override por idioma).
- **Doc 11 — Stack de implementação e passthrough do `/api/tmdb`** (2026-07-08).
  Fecha as decisões maduras da doc 11. **Stack (ADR-0014):** ratificadas as
  inclinações — router **chi**, driver **`modernc.org/sqlite`** (CGO-free), dados
  via **sqlc**, mediainfo via **ffprobe**, contrato de tipos via **OpenAPI gerado**;
  tudo coerente com "binário único, CGO-free, cross-compile trivial". **`/api/tmdb`
  (ADR-0015):** o dono refinou o default — **passthrough + cache, nunca 404 por
  indisponibilidade**; um *miss* de busca só cacheia, um *miss* de detalhe pode
  virar `draft` em fila de revisão (`promote_draft`) ou ficar em cache oculto com
  TTL (`hidden_cache`), à escolha; falha da fonte serve cache stale. Promovido à doc
  05. **Licença MIT** confirmada (já em ME3). Os demais itens da doc 11 (plugins,
  verticais, auth, layout de assets, scan incremental) foram reclassificados de
  "abertos" para **adiados por fase** — não são indecisões de fundação.
- **ME1–ME4 — Arrumação de base da documentação** (2026-07-08). Fecha o backlog de
  fundação. **ME1:** adotados **ADRs em formato leve** — `docs/adr/` com README
  (índice + prática), `template.md` e **13 ADRs** migrados das decisões já fechadas
  (R1–R7 = ADR-0001…0007; MD1–MD6 = ADR-0008…0013); ADR é imutável e uma decisão que
  supere outra vira novo ADR. **ME2:** **diagrama ER** (mermaid) na doc 04, com as
  três camadas do resolver penduradas da `entity`. **ME3:** licença **MIT** —
  `LICENSE` + `CONTRIBUTING.md` na raiz (fluxo decisão→ADR→spec, pt-BR, segredos via
  env). **ME4:** **glossário** (doc 02) atualizado — `status` corrigido (intenção,
  sem `owned`), **Posse** como fato derivado e os termos de precedência do resolver.
  Com isto, toda a fundação (R1–R7, MD1–MD6, ME1–ME4) está fechada e documentada.
- **MD3 — Concorrência SQLite: WAL + escritor único + escritas curtas** (2026-07-08).
  Resolve "API sempre no ar × scan escrevendo pesado" contra o limite de um escritor
  do SQLite. Receita em três camadas: **(1)** abrir em **WAL** com `busy_timeout=5000`,
  `synchronous=NORMAL` e `foreign_keys=ON` (vários leitores + um escritor sem bloqueio
  mútuo, sem `database is locked`); **(2)** **dois pools** — leitura com N conexões e
  **uma** conexão de escrita (escritor único); **(3)** scan em **lotes curtos** com
  escrita **interativa priorizada** sobre a do scan. Evolui para uma fila/goroutine
  escritora se a priorização precisar ser explícita. Promovido à doc 03 (Concorrência
  e persistência).
- **MD6 — Override de relações (elenco/equipe) por merge de operações** (2026-07-08).
  Fecha o que o R3 adiou: elenco/equipe seguem a mesma filosofia (merge de operações,
  base externa viva por baixo), mas sobre **créditos estruturados**. O override guarda
  **add/remove/edit** (papel, personagem, `ord` de billing); a **chave do crédito** é
  `person_id` + papel/departamento (a mesma pessoa em dois papéis = dois créditos),
  com o `credit_id` do provider como desempate. Pessoa adicionada pelo dono vira um
  `person` **local** sem id externo (coerente com MD4). O resolver compõe crédito a
  crédito (R5) e as operações vivem no `media_override` (R4). Promovido à doc 04
  (Override de relações).
- **MD5 — Ciclo de vida não-destrutivo; evento de arquivo só mexe a posse**
  (2026-07-08). Fecha a pergunta "arquivo some → vira wanted?": **não** — eventos de
  filesystem mexem apenas no eixo derivado da posse (MD2), nunca no `status`
  (intenção) nem no trabalho do dono. Arquivo sumido com datasource acessível vira
  `media_file` **`missing`** (soft, com `last_seen`), reversível se reaparecer;
  **datasource inteiro inacessível** não marca nada (evita falso sumiço em massa,
  sinaliza o datasource offline). Entity **auto-criada sem nenhum investimento do
  dono** cujo arquivo sumiu vira **órfã** numa fila de limpeza, nunca apagada
  automaticamente. **Purga** é a única operação destrutiva e é sempre explícita
  (remove entity + `media_external`/`media_override`/assets). Promovido à doc 04
  (Ciclo de vida e deleção) e doc 06 (missing/offline).
- **MD2 — Posse e `status` viram dois eixos ortogonais** (2026-07-07). Um campo só
  conflava "tenho o arquivo" (fato) com "o que pretendo" (intenção). Separado:
  **posse é derivada** da existência de `media_file` (não é campo editável, não
  diverge do disco, pode virar coluna gerada); **`status` é só intenção**, um estado
  por vez — `cataloged` (default), `wanted`, `unreleased`, `draft`. O valor `owned`
  sai (era posse disfarçada). Os eixos combinam livremente (`wanted`+posse=sim =
  tenho cópia mas quero upgrade), sustentando o "banco maior que a biblioteca". As
  transições de estado (arquivo aparece/some) ficam para o MD5. Promovido à doc 04.
- **MD4 — `external_ids` é a identidade autoritativa; `media_external` é cache**
  (2026-07-07). O id externo mora em dois lugares, então elege-se a autoridade:
  `entity.external_ids` é o **mapa de identidade** (todos os ids conhecidos, buscados
  ou não — é onde o match determinístico do MD1 grava e por onde o lookup busca).
  `media_external` é **cache do payload** por fonte buscada; seu `source_id` é
  subordinado, com o invariante `media_external.source_id == external_ids[source]`.
  Em conflito, a **identidade vence**: a linha de cache divergente é obsoleta
  (re-busca/descarta); o dono corrige a identidade e nunca edita o cache à mão.
  Identidade e valor editorial são eixos distintos — `media_external` permanece a
  camada base do resolver. Vale igual para `person`/`company`. Promovido à doc 04
  (seção "Identidade externa").
- **MD1 — Identidade e matching pela convenção Kodi/Jellyfin** (2026-07-07). Em vez
  de inventar regra própria, adota-se o padrão de-facto do ecossistema (Kodi/Jellyfin;
  Emby/Plex/\*arr na mesma família). Consequências: **grão 1:N plano** (1 obra = 1
  `Entity`, com N `media_file` para qualidades/edições/partes); **edição vira rótulo**
  no `media_file` (não duplica a obra); **partes** são empilhadas (*stacking*); a
  **chave de match** é o **id externo embutido no nome** (`[tmdbid-…]`/`{tmdb-…}`) →
  match determinístico, e sem id cai para título+ano. Todo match tem **score de
  confiança**: alta casa sem alerta; baixa casa com o melhor palpite **+ alerta de
  revisão** que o dono confirma/corrige; zero resultados → `draft` não-identificado.
  Música usa o mesmo princípio via MusicBrainz+tags. Verticais não cobertas
  (videoclipe→IMVDB, quadrinhos→ComicInfo) trazem identidade e provider próprios por
  `extension_data`, sem tocar no núcleo — validando R1/R5. Promovido para a doc 06
  (seção "Identidade e matching") e refletido no `media_file` da doc 04.
- **R1 — Espaço de campos particionado por origem** (2026-07-06). O resolver é um
  motor genérico que compõe camadas guiado por metadado por campo, não por uma
  tabela fixa no código. Origem declarada no schema (núcleo) ou no JSON Schema do
  namespace (extensão/plugin); só fontes-base são declaradas; override é universal.
  Consequência: adicionar vertical/plugin não toca o núcleo nem o resolver.
- **R2 — Prioridade entre fontes externas por lista global configurável**
  (2026-07-06). Ordem de preferência de fontes (default `[tmdb]`); por campo, a
  primeira fonte da lista que tiver valor forma a base externa, sobre a qual o
  override entra. Configurável pelo dono. Evoluções (por campo / fonte primária
  por entidade) ficam pós-MVP sem quebrar o modelo.
- **R3 — Override de listas por merge de operações** (2026-07-06). Para listas de
  valores simples (`tags`, `genres`), o override guarda adições e remoções por
  valor; a base externa continua viva e evolui em re-scrape. Só valores simples;
  override de relações (elenco/equipe) foi separado no item **MD6**.
- **R4 — Override como blob JSON esparso** (2026-07-06). `media_override` passa a
  ser uma linha por entidade com `override_json` espelhando o formato da entity,
  só campos mudados. Escolhido por casar com R1 (validação por schema), R3 (merge
  de listas) e R5 (override em extension_data). Substitui o esboço EAV da doc 04.
- **R5 — Composição uniforme, incluindo extension_data** (2026-07-06). O resolver
  não distingue núcleo de extensão: a mesma precedência vale campo a campo dentro
  dos namespaces. Decorre de R1 e R4.
- **R6 — Presença da chave sinaliza override** (2026-07-06). No blob esparso, chave
  presente (mesmo `null`) = override que vence; chave ausente = herda da base. UI
  distingue "limpar override" de "definir vazio". Proveniência por campo não é
  exposta na API por ora.
- **R7 — Fatos técnicos são overridáveis** (2026-07-06). Override universal alcança
  também mediainfo (corrigir detecção errada); o detectado original fica preservado
  em `media_file`. Sem exceção ao princípio de autoridade.
- **QG1 — Quality gate: catraca unificada Go + TS** (2026-07-08). Uma catraca
  (ratchet) única com um `baseline.json` cobrindo backend Go **e** frontend TS
  (Opção A entre unificado / dois gates / estender o boatman). Runner em **Node
  partindo do boatman** (gera a metade JS; estende-se o script para coletar métricas
  Go: golangci-lint, `go test -cover`, govulncheck, gofmt, gocyclo). Hooks locais com
  **lefthook** (poliglota) em vez de husky. Roda no `pre-push` local e no PR (bloqueia
  regressão + comenta). Promover os coletores Go ao boatman upstream fica como
  follow-up. Documentado em [ADR-0016](adr/0016-quality-gate-catraca-unificada.md) e
  no briefing [0005](../briefings/0005-quality-gate-catraca-unificada.md).
