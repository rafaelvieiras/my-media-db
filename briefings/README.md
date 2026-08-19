# Briefings

Um **briefing** é uma **ordem de serviço autocontida** para uma única unidade de
trabalho: uma feature, uma correção ou uma tarefa técnica. Ele reúne tudo o que
é preciso para implementar aquilo **sem depender de contexto externo** — o que
fazer, por quê, a jornada do usuário, o contrato técnico e como saber que ficou
pronto.

O desenvolvimento do MyMediaDB é **dirigido por briefings**: nada é implementado
sem um briefing correspondente. As `docs/` descrevem *como a aplicação é*; os
briefings descrevem *o que construir agora e como*.

## Princípios

1. **Autocontido.** Quem pega o briefing não deveria precisar caçar informação
   em conversas ou na cabeça de ninguém. Se algo é necessário, está no briefing
   (ou linkado explicitamente numa doc).
2. **Uma unidade de trabalho.** Um briefing = uma coisa entregável e testável.
   Se está grande demais para um critério de aceite claro, quebre em dois.
3. **Rastreável ao roadmap.** Todo briefing referencia a fase do roadmap
   ([`docs/12`](../docs/12-roadmap.md)) e as docs que fundamentam suas decisões.
4. **Jornada antes de código.** O briefing parte da **experiência do usuário** e
   só então desce ao contrato técnico. O código serve à jornada, não o contrário.
5. **Decisões ficam registradas.** Escolhas técnicas tomadas durante o briefing
   viram texto no próprio briefing; decisões estruturais sobem para as `docs/`.

## Nomenclatura

```
briefings/NNNN-slug-curto.md
```

- `NNNN` — número sequencial de 4 dígitos, na ordem de criação (`0001`, `0002`…).
- `slug-curto` — kebab-case descritivo (`0003-scan-de-datasource`).
- `_template.md` — o modelo; **não** é um briefing (prefixo `_`).

## Estados

Cada briefing carrega um `status` no cabeçalho:

| Status | Significado |
|---|---|
| `draft` | Em escrita/discussão; ainda não pronto para implementar |
| `ready` | Aprovado e completo; pode ser implementado |
| `in-progress` | Implementação em andamento |
| `done` | Implementado e aceito (critérios de aceite satisfeitos) |
| `blocked` | Travado por dependência (indicar qual) |
| `dropped` | Abandonado (manter o arquivo com o motivo, para histórico) |

## Como usar

1. Copie [`_template.md`](_template.md) para `NNNN-slug.md`.
2. Preencha começando pela **jornada do usuário** e o **objetivo**; o contrato
   técnico vem depois.
3. Discuta/refine enquanto `draft`. Ao fechar, marque `ready`.
4. Implemente guiado pelo briefing; atualize o `status` conforme avança.
5. Se uma decisão estrutural surgir, atualize a `docs/` correspondente e linke.

## Índice

<!-- Atualizar conforme briefings forem criados -->

### Fase 1 — Esqueleto do container único

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0001](0001-backend-esqueleto-servidor.md) | Backend Go: esqueleto do servidor HTTP | feature | done | — |
| [0002](0002-frontend-scaffold-integracao-api.md) | Frontend Vite + React: scaffold e integração com a API | feature | done | — |
| [0003](0003-empacotamento-dockerfile-multistage.md) | Empacotamento: Dockerfile multi-stage (imagem única) | chore | done | 0001, 0002 |
| [0004](0004-ambiente-de-desenvolvimento-hot-reload.md) | Ambiente de desenvolvimento: compose com hot reload | chore | done | 0001, 0002 |
| [0005](0005-quality-gate-catraca-unificada.md) | Quality gate: catraca unificada (ratchet) Go + TS | chore | done | 0001, 0002 |

### Fase 2 — UI de biblioteca

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0006](0006-fundacao-persistencia-sqlite.md) | Fundação de persistência SQLite (WAL, pools, migrations, sqlc) | feature | done | 0001 |
| [0007](0007-schema-nucleo-minimo.md) | Schema do núcleo (mínimo da Fase 2) | feature | done | 0006 |
| [0008](0008-datasources-cadastro-api-library.md) | Datasources: cadastro e API `/api/v1/library` | feature | done | 0007 |
| [0009](0009-scanner-fswalk-ffprobe-matching.md) | Scanner: fs walk + ffprobe + matching Kodi/Jellyfin | feature | done | 0007, 0008 |
| [0010](0010-api-leitura-resolver-minimo.md) | API de leitura + resolver mínimo (camada arquivo) | feature | done | 0007, 0009 |
| [0011](0011-frontend-grid-lista-detalhe.md) | Frontend: grid de posters, lista e detalhe com mediainfo | feature | done | 0008, 0009, 0010 |

### Fase 3 — Enriquecimento + override

Compõe `override ⟵ externo ⟵ arquivo` em toda leitura e dá **autoridade ao dono**.
Breakdown definido no [roadmap](../docs/12-roadmap.md#fase-3--enriquecimento--override).
Os briefings `draft` são detalhados quando cada predecessor aterrissa (como na Fase 2).

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0012](0012-schema-fase3-camadas-externo-override.md) | Schema da Fase 3: materializar `media_external` e `media_override` | feature | done | 0007 |
| [0013](0013-resolver-tres-camadas.md) | Resolver de 3 camadas (`override ⟵ externo ⟵ arquivo`) | feature | done | 0012 |
| [0014](0014-provider-tmdb-busca-local-first.md) | Provider TMDB + busca local-first (`/api/v1/search`) | feature | done | 0012, 0013 |
| [0015](0015-enriquecimento-import-proxy-imagem.md) | Enriquecimento (import explícito) + proxy/cache de imagem | feature | done | 0014 |
| [0019](0019-fundacao-multi-idioma-settings-resolver.md) | Fundação multi-idioma: settings + resolução por idioma | feature | done | 0013 |
| [0020](0020-provider-multi-idioma-traducao-tags.md) | Provider multi-idioma + tradução de tags | feature | done | 0019, 0015 |
| [0021](0021-ui-idiomas-admin-seletor.md) | UI de idiomas: admin de config + seletor de leitura | feature | done | 0019, 0020 |
| [0016](0016-editor-override-ui.md) | Editor de override (`/api/v1/entities/{id}/override`) + UI (por idioma) | feature | done | 0013, 0015, 0019, 0021 |
| [0017](0017-arte-custom-upload-ui.md) | Arte custom: upload (`/api/v1/entities/{id}/assets`) + UI | feature | done | 0015 |
| [0018](0018-fila-revisao-orfas-correcao-match.md) | Fila de revisão + órfãs + correção de match | feature | done | 0013, 0015, 0016 |
| [0022](0022-detalhe-externo-rico-persistente.md) | Detalhe externo rico + cache persistente (provider) | feature | done | 0014, 0015, 0020 |
| [0023](0023-tela-detalhe-externo-ui.md) | Tela rica de detalhe do item externo (UI) | feature | done | 0022, 0021 |
| [0024](0024-paridade-detalhe-entity-rico.md) | Paridade rica entity↔externo: `ResolvedEntity` rico + `RichDetailView` compartilhada | feature | done | 0022, 0023 |
| [0025](0025-colecao-entity-interna.md) | Coleção como entity interna: importável + criável/editável manual | feature | done | 0024 |
| [0026](0026-centralizacao-interno-first.md) | Centralização interno-first: priorizar o que já está na biblioteca | feature/fix | done | 0024, 0025 |
| [0027](0027-similar-search-ids-item-manual.md) | Interno-first em similares/busca + editar `external_ids` + item manual | feature | done | 0025, 0026 |
| [0028](0028-testes-e2e.md) | Testes end-to-end: validar o fluxo inteiro (UI + API) | infra/qualidade | in-progress | 0015, 0025, 0026, 0027 |
| [0029](0029-md6-elenco-equipe-person-credit-company.md) | MD6: elenco, equipe e produtoras (`person`/`credit`/`company`) | feature | done | 0012, 0013, 0022, 0023, 0024 |
| [0030](0030-crud-por-rota-formulario-dirigido-por-schema.md) | CRUD por rota + formulário dirigido por schema (TanStack Form + Zod) | refactor/feature | done | 0016, 0025, 0027 |
| [0031](0031-series-fundacao-modelo-enriquecimento.md) | Séries: fundação do modelo + enriquecimento (TheTVDB + registry multi-provider) | feature | done | 0022, 0024, 0025, 0026 |
| [0032](0032-usabilidade-arvore-series-home-busca-breadcrumb.md) | Usabilidade da árvore de séries: home/busca só de topo + breadcrumb | fix/feature | done | 0031 |
| [0033](0033-busca-filtro-subnos-secoes-separadas.md) | Busca: filtro de sub-nós (temporadas/episódios) default-off em seções separadas | fix/feature | done | 0032 |
| [0034](0034-scanner-de-series-arquivo-episodio.md) | Scanner de séries: naming TV + auto-import + N:M `media_file_entity` | feature | done | 0031, 0009 |
| [0035](0035-apagar-datasource-com-arquivos.md) | Apagar datasource com arquivos (corrige 500 da FK) | fix | done | — |
| [0036](0036-posse-completude-no-card-da-temporada.md) | Posse e completude no card da temporada | fix/feature | done | 0034 |
| [0037](0037-completude-no-card-da-serie-home.md) | Completude no card da série (home): "X/Y temporadas" | fix/feature | done | 0036 |
| [0038](0038-badge-de-posse-no-topo-direito-da-capa.md) | Badge de posse no topo-direito da capa (sem apagar ausentes) | fix | done | 0037 |
| [0039](0039-numbering-real-no-card-do-episodio.md) | Numbering real no card do episódio (SxxEyy/absoluto, não posição ordinal) | fix/feature | done | 0031 |
| [0040](0040-seletor-de-ordenacao-de-episodios.md) | Seletor de ordenação de episódios (official/dvd/absoluto) reagrupado na leitura | feature | done | 0039 |
| [0041](0041-i18n-de-episodio-media-external.md) | i18n de episódio (título/overview por idioma via `media_external`) | feature | done | 0031, 0020 |
| [0042](0042-still-de-episodio-lazy-proxy.md) | Still de episódio (lazy via image proxy): URL no import, thumbnail 16:9 no card + detalhe | feature | done | 0031, 0039 |
| [0043](0043-re-enrich-atualiza-arvore-serie.md) | Re-enrich atualiza a árvore de série (temporada+episódio) preservando override | fix/feature | done | 0042 |
| [0044](0044-fluxo-de-artes-redondo.md) | Fluxo de artes redondo (pôster/fanart/still: exibição correta + gestão em todo tipo) | fix/feature | done | 0017, 0042 |
| [0045](0045-metadados-de-serie-paridade-com-filme.md) | Metadados de série: paridade com filme (slogan, gênero, classificação, tags, situação, relacionados) | feature | done | 0044, 0031 |
| [0046](0046-independencia-de-provider-serie-sem-tvdb.md) | Independência de provider: série sem TVDB (TMDB como provedor de árvore) | feature/fix | done | 0031, 0045 |
| [0047](0047-robustez-de-fonte-default-filme-e-serie.md) | Robustez de fonte-default: não assumir "tmdb" quando ele não está registrado | fix | done | — |
| [0048](0048-fundacao-providers-de-enriquecimento-arte-trailer-tema.md) | Provider fanart.tv (ArteProvider) + taxonomia de kinds + inventário lazy | feature | done | 0044, 0045, 0046 |
| [0049](0049-galeria-de-artes-linhas-lightbox-disponibilidade.md) | Galeria de artes: linhas por tipo + lightbox/zoom + selos de disponibilidade | feature | done | 0048 |
| [0050](0050-auto-match-fila-de-revisao.md) | Auto-match: busca e sincronização automática da fila de revisão | feature | done | 0009, 0014, 0015, 0018, 0026 |
| [0051](0051-fundir-duplicata-conflito-external-id.md) | Fundir duplicata por conflito de external_id (import + corrigir) | fix | done | 0015, 0018, 0027 |
| [0054](0054-auto-match-no-import-manual-e-job-de-saude.md) | Fechar o gap do auto-match: import manual e job de saúde | feature | done | 0050, 0051, 0053, 0026 |
| [0062](0062-midias-locais-nfo-e-artes-na-pasta.md) | Mídias locais: NFO + artes na pasta da biblioteca (padrão Kodi/Emby, arte "principal") | feature | done | 0044, 0048, 0049, 0059 |
| [0072](0072-galeria-de-artes-dedupe-inventario-e-lightbox-por-id.md) | Galeria de artes: de-duplicar inventário materializado + lightbox reancorado por asset | fix | done | 0048, 0049, 0062 |
| [0073](0073-guard-sair-sem-salvar-edicao-item.md) | Guard de "sair sem salvar" na edição de item (3 blocos de salvamento independentes) | fix | done | 0016, 0027, 0029, 0030 |
| [0074](0074-busca-rica-na-correcao-da-fila-de-revisao.md) | Busca rica (pôster/sinopse) na correção da fila de Revisão | fix | done | 0014, 0018, 0026 |
| [0075](0075-navegacao-responsiva-mobile.md) | Navegação responsiva em telas de celular (nav não colapsa, transborda) | fix | done | — |
| [0076](0076-creditos-reordenar-em-lote-e-fotos-de-equipe.md) | Créditos: reordenar em lote (elenco/equipe grandes) + foto para papéis-chave de equipe | feature/fix | done | 0029, 0073 |
| [0077](0077-descoberta-acionavel-completude-e-novo-item.md) | Descoberta acionável: banner de completude com ação por item + cross-link "Novo item" ↔ Busca | fix | done | 0030, 0057, 0058 |
| [0078](0078-polimentos-status-uid-dev-e-retry-do-navegador.md) | Polimentos: badge de status traduzido, paridade de uid no dev e retry do navegador de pastas | fix/chore | done | 0004, 0071 |
| [0079](0079-hero-compacto-estilo-tmdb.md) | Hero do detalhe mais compacto, no padrão visual do TMDB | fix/chore | done | 0022, 0023, 0044, 0045, 0048, 0049 |
| [0080](0080-subrotas-edicao-item-estilo-tmdb.md) | Subrotas na edição de item, com sidebar no padrão do TMDB | feature | done | 0030, 0073 |
| [0082](0082-hero-imersivo-estilo-jellyfin.md) | Hero imersivo (backdrop full-bleed, logo/banner/disco, trailer e player de tema musical) | feature | done | 0079, 0048, 0049, 0062 |
| [0083](0083-area-administrativa-dashboard-com-sidebar.md) | Área administrativa unificada em `/dashboard`, com sidebar (Bibliotecas/Revisão/Órfãs/Jobs/Configurações/Saúde) | feature | done | 0071, 0075, 0080 |
| [0084](0084-idioma-preferido-por-biblioteca.md) | Idioma preferido por biblioteca (busca + exibição) + match contra títulos alternativos/traduzidos | feature | done | 0019, 0050, 0056, 0060 |
| [0085](0085-throttle-configuravel-enriquecimento-retroativo.md) | Throttle configurável do enriquecimento retroativo: valor global via painel (default 15 dias) + override por biblioteca | feature | done | 0019, 0056, 0084 |
| [0086](0086-redesign-hero-gerenciar-notas-links-externos.md) | Redesign do detalhe do item: dropdown "Gerenciar" no hero, notas por fonte, fileira horizontal de links externos com logo | feature/redesign | done | 0082, 0079, 0029 |
| [0087](0087-fix-inventario-fanarttv-fora-do-import-explicito.md) | Corrige o inventário fanart.tv ausente fora do import explícito (filme via auto-match/retroativo + série sem TVDB) | fix | done | 0046, 0048 |
| [0088](0088-acoes-forcadas-menu-gerenciar.md) | Três ações forçadas no menu Gerenciar: reconferir arquivos/assets/campos locais de reprodução, baixar metadados ignorando cache, baixar artes faltando via fanart.tv | feature | done | 0048, 0056, 0062, 0064, 0086, 0087 |
| [0089](0089-enriquecimento-automatico-no-scan-e-idioma-nas-artes.md) | Auto-match imediato de filme no scan (metadados + inventário de artes, sem depender de job) + materialização automática opcional + prioridade de idioma (preferido → textless → inglês → qualquer) na escolha de arte | feature | done | 0046, 0048, 0056, 0064, 0084, 0087 |
| [0090](0090-fix-tvdb-id-numerico-derruba-mapeamento-tmdb.md) | Corrige `tvdb_id` numérico do TMDB derrubando o mapeamento canônico inteiro (título/sinopse/elenco/tagline) em silêncio; adiciona log no descarte de fonte | fix | done | 0020, 0022, 0087 |
| [0091](0091-similares-tmdb-recommendations-filtro-qualidade.md) | Similares: troca `/similar` por `/recommendations` do TMDB, filtra adulto/nota irrelevante, ordena por afinidade de gênero, abre capacidade `SimilarProvider` | fix/feature | done | 0022, 0023, 0027 |
| [0067](0067-arvore-de-serie-em-todo-caminho-de-identidade-externa.md) | Árvore de série (temporada/episódio) em TODO caminho de identidade externa (Corrigir, confirmar candidato, auto-match, retroativo) | fix | done | 0031, 0043 |
| [0069](0069-recasar-arquivos-pendentes-ao-sincronizar-arvore-serie.md) | Recasar arquivos pendentes de episódio ao sincronizar a árvore da série | fix | done | 0034, 0043, 0054, 0067 |
| [0070](0070-editar-path-datasource-cadastrado.md) | Editar path de um datasource já cadastrado (recasca em cascata media_file/asset/nfo) | feature | done | 0008, 0062, 0064 |
| [0071](0071-navegador-de-arquivos-e-rotas-dedicadas-datasource.md) | Navegador de arquivos (estilo Jellyfin) + rotas dedicadas `/library/new`/`/library/:id/edit` | feature | done | 0008, 0064, 0070 |

### Fase 5+ — Histórico de consumo (tracking/scrobbling)

Épico fundado em pesquisa de mercado (Trakt, Last.fm, ListenBrainz,
RetroAchievements e concorrentes — ver [ADR-0022](../docs/adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md)
e agenda [H1–H4](../docs/00-agenda-de-decisoes.md#histórico-de-consumo-trackingscrobbling--fundacional-levantado-a-partir-de-pesquisa-de-mercado)):
tabela de eventos genérica (`kind` livre, cobre qualquer vertical futura sem
migration nova), entrada por webhook de media server + API manual, saída/sync
externo (`TrackerProvider`) desenhada para depois. Primeira entrega só
filme/série. Os 3 briefings estão implementados e validados; `TrackerProvider`
concreto e demais verticais (música/livro/jogo) ficam para depois.

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0092](0092-fundacao-historico-de-consumo-schema-e-api.md) | Fundação do histórico de consumo: schema genérico `historico_evento` + API mínima (criar/listar/estado derivado/apagar evento) | feature | done | — |
| [0093](0093-webhook-jellyfin-historico-consumo.md) | Receptor de webhook do Jellyfin: normaliza play/pause/stop para `historico_evento`, casando por ID externo (TMDB/IMDB/TVDB) | feature | done | 0092 |
| [0094](0094-ui-historico-e-estado-assistido.md) | UI do histórico: selo de assistido no card/hero + timeline no detalhe + ação manual | feature | done | 0092, 0093 |
| [0097](0097-ui-webhook-token-jellyfin.md) | UI de Webhook: `/dashboard/webhook` para gerar/copiar o token do webhook Jellyfin (0093 nunca ganhou tela) + placeholder de webhooks de saída | feature | done | 0093 |
| [0098](0098-guard-esvaziar-external-ids.md) | Guard de confirmação no editor de IDs externos antes de salvar um mapa vazio (evita zerar a identidade de uma entity por engano) | fix | done | — |
| [0099](0099-eventos-orfaos-historico-backfill.md) | Eventos de histórico sem match viram registro órfão (em vez de descartados) e são vinculados retroativamente quando a entity ganhar o ID externo | feature | done | 0092, 0093 |

### Fase 5+ — Integrações configuráveis via UI

Substitui o hardcode de credencial de provider (env var lida só no boot) por
um registro dinâmico gravável em banco (precedência sobre a variável de
ambiente, mesmo formato override-vence do resolver) — ver
[ADR-0023](../docs/adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md)
e agenda [I1–I4](../docs/00-agenda-de-decisoes.md#integrações-configuráveis-ui--fundacional-levantada-a-partir-de-pedido-do-dono).
Primeira integração: TMDB. `internal/integration.Registry` é o primeiro ponto
de extensão concreto que um futuro sistema de plugins vai reusar.

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0095](0095-fundacao-integracoes-schema-registry-api.md) | Fundação de Integrações: schema `integration_config` + registry Go + API (`GET/PUT`) + migra o TMDB para ler do registro | feature | draft | — |
| [0096](0096-ui-integracoes-dashboard-tmdb.md) | UI de Integrações: `/dashboard/integrations` (lista) + subpágina TMDB com formulário dinâmico | feature | draft | 0095 |

### Fase 5+ — Trailer e música-tema

Paridade com o tinyMediaManager: materializa trailer (TMDB/TVDB `/videos`) e
música-tema (ThemerrDB) na pasta real da biblioteca, convenção Kodi/Jellyfin
(`<nome>-trailer.<ext>` / `theme.mp3`), usando yt-dlp como downloader opcional
de YouTube — ver [ADR-0025](../docs/adr/0025-trailer-tema-musical-ytdlp-themerrdb.md)
e agenda [TR1–TR2](../docs/00-agenda-de-decisoes.md#trailer-e-música-tema--fundacional-levantada-a-partir-de-pesquisa-sobre-paridade-com-tinymediamanager).

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0101](0101-fundacao-ytdlp-binario-dinamico.md) | Fundação: binário yt-dlp (download dinâmico em runtime + path customizável) | feature | done | — |
| [0102](0102-trailer-materializacao-disco.md) | Trailer: materialização em disco (TMDB/TVDB `/videos` + yt-dlp) | feature | done | 0101 |
| [0103](0103-tema-musical-themerrdb.md) | Música-tema automática via ThemerrDB (+ yt-dlp) | feature | done | 0101 |
| [0104](0104-trailer-tema-job-e-scan-automatico.md) | Trailer/tema: detecção em massa, job periódico e gatilho automático no scan | feature | done | 0101, 0102, 0103 |

### Fase 5+ — Música (artista/álbum/faixa)

Primeira vertical nova de fato desde o núcleo: hierarquia artista→álbum→faixa,
provider base MusicBrainz + Cover Art Archive (sem chave). `entity`/
`entity_relation` genéricos cobrem toda a estrutura — ver
[ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md)
e agenda [MU1–MU4](../docs/00-agenda-de-decisoes.md#modelo-de-música--fundacional-levantada-a-partir-de-pedido-do-dono-pra-implementar-o-schema).
Quebra por camada (mesmo raciocínio de escala do [AGENTS.md](../AGENTS.md)):
0105 fecha a fundação de dados; provider MusicBrainz, scanner/matching de
arquivo e UI ficam para briefings seguintes, a numerar depois de 0105 validado.

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0105](0105-fundacao-schema-musica.md) | Fundação do schema de música: tipos de entity (artista/álbum/faixa), `extension_data core.music@1`, coluna gerada `external_id_mbid`, kinds de `entity_relation` | feature | done | — |
| [0106](0106-provider-musicbrainz-cover-art-archive.md) | Provider MusicBrainz + Cover Art Archive: busca/import de artista→álbum→faixa + arte de capa | feature | done | 0105 |
| [0107](0107-scanner-matching-arquivo-faixa.md) | Scanner: matching de arquivo↔faixa (tags de áudio + MusicBrainz) | feature | done | 0105, 0106 |
| [0108](0108-fila-execucao-unificada-scan-jobs.md) | Fila de execução unificada (scan + jobs, ADR-0028) + contador de progresso real por arquivo | feature | done | 0107 |
| [0109](0109-home-adicionados-recentemente.md) | Home: "Adicionados recentemente" em carrosséis por schema | feature | done | 0107 |
| [0110](0110-navbar-biblioteca-dropdown-schema.md) | Navbar: "Biblioteca" com dropdown por schema | feature | done | 0107 |
| [0111](0111-entity-view-musica-album-artista.md) | Variação de view de entidade para música (tracklist de álbum, discografia de artista), não exclusiva | feature | done | 0107 |

### Transversal — Infraestrutura operacional

Não pertence a nenhuma fase de UI do roadmap; é infraestrutura de execução que
resolve uma dor operacional já existente (backup sem depender de cron do host)
e serve, explicitamente, de base técnica para o futuro sistema de plugins
(Fase 5+ — ver [12-roadmap](../docs/12-roadmap.md#fase-5--verticais-e-plugins-visão)).

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0052](0052-fundacao-sistema-de-jobs.md) | Fundação do sistema de jobs: registro dinâmico + agendador embutido | feature | done | — |
| [0053](0053-job-saude-biblioteca.md) | Job de saúde da biblioteca: re-scan de todas as fontes + resumo de revisão/órfãs | feature | done | 0052, 0018 |
| [0061](0061-catraca-sensivel-ao-diff-e-log-sob-demanda.md) | Catraca sensível ao diff (filtro Go/Web) + `gate-fast` + log completo de falha em `reports/` | chore | done | — |
| [0063](0063-storage-diferenciado-saude-biblioteca.md) | Storage diferenciado na saúde: banco vs. mídia gerenciada vs. cache da app vs. não identificado | feature | done | 0053, 0055, 0062 |
| [0064](0064-job-midias-faltantes-watch-scan-incremental.md) | Job "baixar mídias faltantes" + scan incremental (mtime/size) + watch de filesystem opcional | feature | done | 0009, 0052, 0053, 0062 |
| [0065](0065-preservacao-generica-de-elementos-desconhecidos-no-nfo.md) | Preservação genérica de elementos desconhecidos no `.nfo` (paridade com "Write clean NFO" do TMM) | fix | done | 0062 |
| [0066](0066-nfo-thumb-local-e-rating-padrao-kodi.md) | NFO: `<thumb>` aponta para arquivo local/URL direta (nunca API interna) e `<rating>` no formato atual do Kodi | fix | done | 0062, 0064 |
| [0081](0081-fix-job-midias-faltantes-para-arvore-de-serie.md) | Corrige o job de mídias faltantes para a árvore de série (temporada/episódio nunca alcançados + raiz da série resolvida errada) | fix | done | 0044, 0062, 0063, 0064 |
| [0068](0068-e2e-fase3-msw-jsdom-e-catraca-de-cobertura-navegador.md) | E2E Fase 3 (MSW+jsdom) + travar cobertura de navegador (Camada 2) na catraca | infra/qualidade | done | 0028, 0061 |
| [0100](0100-cache-externo-sobrevive-sem-provider-vivo.md) | Cache externo sobrevive sem provider vivo: separa capacidade de busca ao vivo (exige credencial) de capacidade de parse de payload já cacheado (sempre disponível) | fix | done | — |
