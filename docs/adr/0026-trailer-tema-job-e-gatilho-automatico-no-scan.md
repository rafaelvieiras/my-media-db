# ADR-0026 — Trailer/tema: job periódico + gatilho automático no scan (opt-in)

- **Status:** Aceita
- **Data:** 2026-08-12
- **Agenda:** [TR3](../00-agenda-de-decisoes.md#trailer-e-música-tema--fundacional-levantada-a-partir-de-pesquisa-sobre-paridade-com-tinymediamanager) · **Spec:** [ADR-0025](0025-trailer-tema-musical-ytdlp-themerrdb.md), [07](../07-enriquecimento-providers.md)

## Contexto

O [ADR-0025](0025-trailer-tema-musical-ytdlp-themerrdb.md) (item 5) registrou
que o download de trailer/tema seria **sempre pull explícito** — "botão ou job
que o admin liga de propósito, **nunca automático a cada scan**". Depois de
0101/0102/0103 implementados (botão manual por entity funcionando em produção),
o dono pediu explicitamente: "Faltou adicionar a possibilidade de buscar
trailer e trilha faltante, além de adicionar isso às rotinas de scan/sync e aos
jobs" — ou seja, quer **detecção em massa** e **execução automática**, não só
um clique por item.

Isso parece contrariar a cláusula "nunca automático a cada scan" do ADR-0025 e
o princípio 4 do `AGENTS.md` ("nunca sync automático"). Mas investigação do
código mostrou que o projeto **já tem precedente direto** para exatamente esse
padrão, anterior ao próprio ADR-0025:

- `autoMaterializeArtworkSettings` (`internal/server/settings.go`) é um toggle
  `app_settings` que, quando ligado, já faz o scan **baixar arte
  automaticamente** para item recém-importado (`MaterializarSeAutoDownloadLigado`,
  `internal/server/midias_faltantes.go:161`) — sem clique nenhum do dono por
  item, só a decisão de ligar o toggle uma vez.
- O briefing 0089 já enriquece o filme **automaticamente no scan** (metadados,
  incluindo o candidato de tema musical via `enriquecerTemaMusical`,
  `internal/enrich/enrich.go:146-150`) — de novo, sem pull explícito por item.
- O job `midias_faltantes` (0064) já roda **agendado** (cron), materializando
  o que falta em toda a biblioteca sem intervenção manual — é opt-in só no
  sentido de que `job_definition.enabled=0` por padrão; uma vez ligado, roda
  sozinho.

Ou seja: o princípio 4 ("nunca sync automático") sempre foi sobre **não
re-buscar dado externo sem o dono pedir** (não re-consultar TMDB/TVDB sozinho
numa política própria de refresh) — nunca sobre proibir materialização
automática de um dado **já resolvido/cacheado**, desde que atrás de um opt-in
explícito (toggle ou job desligado por padrão). A cláusula "nunca automático a
cada scan" do ADR-0025 item 5 foi, em retrospecto, mais conservadora do que o
padrão que o resto do projeto já pratica.

## Decisão

**Este ADR refina o item 5 do ADR-0025** (o resto da 0025 — downloader yt-dlp,
ThemerrDB, convenção de storage — permanece intacto e não é revisto aqui):

1. **Detecção em massa**: não existe (nem é necessária) uma coluna de banco
   "tem trailer/tema local" — o estado é sempre derivado via `os.Stat` na pasta
   real, mesmo padrão já usado por `midias_faltantes` para arte. A detecção em
   massa é a própria iteração do job novo (item 2), reaproveitando
   `ListarEntitiesComMediaFilePresente` já existente.
2. **Novo job periódico opt-in** (`job_definition.enabled=0` por padrão,
   mesmo padrão de `midias_faltantes`/`saude_biblioteca`): varre a biblioteca e
   baixa trailer/tema faltante, best-effort por entity (erro numa não aborta as
   outras). Reaproveita a UI de jobs já existente (`/dashboard/jobs`) — ganha
   "rodar agora"/agendamento/histórico de graça, sem UI nova.
3. **Gatilho automático no scan, atrás de um toggle novo** (mesmo molde de
   `autoMaterializeArtworkSettings`): quando ligado, a importação/auto-match de
   um item novo dispara uma tentativa de trailer/tema **em goroutine
   best-effort** (não bloqueia a resposta do scan) — mesma razão de cautela já
   registrada no briefing 0102 §6 e no princípio geral de não travar o scan com
   I/O pesado de rede (download de vídeo via yt-dlp é ordens de magnitude mais
   lento que uma imagem).
4. **O job periódico continua sendo a base; o gatilho de scan só acelera** —
   mesmo raciocínio já usado no `AGENTS.md` para watch vs. job periódico
   (fsnotify "só acelera", nunca é fonte única de verdade): se a tentativa
   síncrona-em-goroutine do scan falhar (binário yt-dlp ainda baixando,
   YouTube com rate limit, toggle ligado depois que o item já foi importado),
   o job periódico cobre na próxima execução.
5. Os dois mecanismos (job e gatilho de scan) **compartilham o MESMO toggle**
   — não faz sentido separar "quero automático no job" de "quero automático no
   scan"; é uma única decisão do dono ("quero trailer/tema automático ou não").
   O job em si sempre existe registrado (como todo job do sistema) e pode ser
   agendado/rodado manualmente independente do toggle; o toggle controla
   apenas o gatilho *dentro do scan*.

## Consequências

- Fecha o pedido do dono sem inventar mecanismo novo — reaproveita 100% do
  padrão de job (0052/0064) e de settings toggle (0089/auto-materialize) já
  validado em produção.
- `AGENTS.md` princípio 4 permanece respeitado pela leitura correta: opt-in
  explícito (toggle desligado por padrão, job desabilitado por padrão) — nunca
  automático "de fábrica".
- Custo: um toggle de settings a mais para o dono administrar; um job novo a
  mais na lista de `/dashboard/jobs`. Ambos seguem padrão existente, sem
  superfície nova de UI a inventar.
- Download de vídeo (yt-dlp) nunca roda de forma síncrona-bloqueante dentro do
  request HTTP do scan — só via goroutine best-effort (gatilho) ou via job
  assíncrono agendado.
- Decisões relacionadas: [ADR-0025](0025-trailer-tema-musical-ytdlp-themerrdb.md)
  (spec base, não revista aqui); briefing 0064 (molde do job de mídias
  faltantes); briefing 0089 (precedente de automático-no-scan atrás de
  toggle).
