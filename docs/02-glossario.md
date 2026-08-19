# 02 — Glossário

Vocabulário canônico do projeto. Termos aqui têm significado fixo nas demais docs
e nos briefings.

| Termo | Definição |
|---|---|
| **Entity (entidade)** | Unidade central do modelo: qualquer mídia catalogada. Tem um `type` (`movie`, `tv`, `episode`, `collection`, `artista`, `album`, `faixa`, …). Ver [04](04-modelo-de-dados.md). |
| **Camada de override** | Registro esparso das edições do dono. Vence sempre sobre o dado externo e o do arquivo. É o que dá autoridade final ao usuário. |
| **Override universal** | Propriedade do override: alcança **qualquer** campo (núcleo, técnico e de extensão), sem campo blindado. Ver [04](04-modelo-de-dados.md). |
| **Precedência do resolver** | A ordem fixa de composição `override ⟵ externo ⟵ arquivo`: o override vence o externo, que vence o do arquivo. Lida como "puxa de" (⟵). |
| **Merge por operações** | Como o override edita listas/relações sem substituí-las em bloco: guarda **adições/remoções** (tags, gêneros) ou **add/remove/edit** de créditos, com a base externa viva por baixo. Ver [04](04-modelo-de-dados.md). |
| **Proveniência (de campo)** | De qual camada (`override`/`externo`/`arquivo`) veio o valor final de um campo. **Não exposta na API** por ora. |
| **Dado externo** | Payload puxado de uma fonte (TMDB etc.), guardado cru e identificado pela fonte, como *base* editorial. Cacheado localmente. |
| **Dado do arquivo (mediainfo)** | Fatos técnicos extraídos do arquivo de mídia (codec, HDR, faixas, duração). Não editável. |
| **Resolver** | Componente que compõe o valor final de uma entidade: `override ⟵ externo ⟵ arquivo`. Toda API serve do resolver, não do dado bruto. |
| **`extension_data`** | Blob JSON *namespaced* com campos específicos de uma vertical. Não altera o schema do núcleo. Validado por JSON Schema. |
| **Namespace (de extensão)** | Chave no formato `<owner>.<slug>@<major>` (ex.: `core.tv@1`, futuramente `plugin.games@1`). |
| **Vertical** | Uma categoria de mídia com campos próprios (filmes, séries, games, livros…). No MVP as verticais são internas (`core.*`). |
| **Provider** | Cliente de uma fonte externa de metadados (TMDB, TVDB, IMVDB, MusicBrainz…). Usado sob demanda. |
| **Datasource / Library source** | Pasta do servidor apontada pelo dono para varredura. |
| **Scan** | Varredura não-destrutiva de um datasource: descobre arquivos e extrai mediainfo. |
| **Matching** | Associação de um arquivo/entrada a uma entidade (e, opcionalmente, a um id externo). |
| **`content_hash`** | Identidade estável de um arquivo por conteúdo, para não duplicar registros ao renomear. |
| **Asset** | Mídia auxiliar de uma entidade (poster, fanart, banner, logo…). Origem `external` ou `user`. Binário em disco; metadado no banco. |
| **`status`** | **Intenção** do dono sobre a entidade, um estado por vez: `cataloged` (default), `wanted`, `unreleased`, `draft`. É eixo **ortogonal à posse**; **não** existe `owned` (era posse disfarçada). Sustenta "o banco é maior que a biblioteca". Ver [04](04-modelo-de-dados.md). |
| **Posse** | **Fato derivado** (não campo editável): a entidade tem ao menos um `media_file` **presente**? Responde *"está na biblioteca"* vs *"só no catálogo"*. Ortogonal ao `status`. |
| **`missing`** | Estado de um `media_file` cujo arquivo sumiu do disco (datasource acessível): registro **preservado** com `last_seen`, não conta para a posse, reversível se reaparecer. Ver [06](06-biblioteca-e-scan.md). |
| **Órfã** | Entity **auto-criada** cujo arquivo sumiu e que **não tem investimento do dono** (sem override/asset/coleção, `status` no default). Vai para fila de limpeza; **nunca apagada sozinha**. |
| **Purga** | A **única** operação destrutiva, sempre **explícita** do dono: remove a entity e derivados (`media_external`, `media_override`, assets). |
| **`external_ids` (identidade autoritativa)** | Mapa de todos os ids externos conhecidos da entidade (buscados ou não). Fonte de verdade da identidade; o matching grava e o lookup lê aqui. |
| **`media_external` (cache)** | Payload cru por fonte efetivamente buscada. `source_id` **subordinado** a `external_ids`; em conflito, a identidade vence. Base editorial do resolver. |
| **`/api/tmdb`** | Camada da API **compatível com o TMDB**, para apontar clientes existentes (Jellyfin/Kodi). |
| **`/api/v1`** | API **nativa**, para tudo que o TMDB não modela (override, biblioteca, mediainfo, usuários). |
| **`VITE_API_BASE`** | Variável que define a base da API para o frontend: vazia = mesmo origin (binário Go); preenchida = API externa/remota. |
| **Plugin** | Extensão de terceiros que adiciona verticais/integrações. **Alvo do design, fora do MVP** — ver [11](11-decisoes-abertas.md). |
| **Artista / Álbum / Faixa** | Hierarquia da vertical música (`entity type='artista'\|'album'\|'faixa'`). Faixa é o grão fino canônico (ancorada em MBID); álbum existe só para artwork/override/agrupamento; artista é entity de 1ª classe (não reaproveita `person`). "Various Artists" é um artista comum, com o MBID canônico da MusicBrainz. Ver [ADR-0027](adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md). |
| **`core.music@1`** | Namespace de `extension_data` da vertical música: `track_number`, `disc_number`, `disc_total`, `isrc`. Lido por `internal/musicext` (molde do `internal/tvext` de série). |
| **Briefing** | Ordem de serviço autocontida para uma unidade de trabalho. Ver [`briefings/`](../briefings/README.md). |
