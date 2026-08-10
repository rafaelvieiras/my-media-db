# 0093 — Receptor de webhook do Jellyfin para histórico de consumo (filme/série)

> Segundo briefing do épico de histórico (ADR-0022, depende do 0092). Recebe o
> webhook de reprodução do Jellyfin (plugin oficial `jellyfin-plugin-webhook`)
> e normaliza cada evento de play/pause/stop/progresso para um
> `historico_evento`, casando o item pelos IDs externos que o próprio payload
> já carrega — sem exigir nenhuma configuração extra no lado do Jellyfin além
> de um webhook apontando pro MyMediaDB.

| | |
|---|---|
| **ID** | 0093 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins |
| **Status** | done |
| **Docs relacionadas** | [ADR-0022](../docs/adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md) |
| **Depende de** | 0092 |

---

## 1. Objetivo

Pesquisa de mercado (ADR-0022) mostrou que o menor esforço / maior alavancagem
para popular histórico de consumo é receber o webhook que media servers já
disparam nativamente — nenhum concorrente self-hosted sério (scrob.app,
sofa.watch) inventa protocolo próprio nesse ponto, todos consomem o webhook
que Jellyfin/Plex/Emby já emitem. Este briefing cobre o primeiro (Jellyfin: o
mais simples de documentar publicamente, sem exigir licença paga, ao
contrário do Plex que exige Plex Pass para webhooks nativos).

O resultado prático: o dono configura UMA vez um webhook no Jellyfin apontando
pro MyMediaDB, e todo play/pause/stop de um filme ou episódio já catalogado
vira automaticamente um `historico_evento`, sem nenhuma ação manual.

## 2. Jornada do usuário

1. O dono instala o plugin `Webhook` no Jellyfin (fora do MyMediaDB — passo
   de configuração no próprio Jellyfin) e cadastra um destino apontando para
   `POST https://<host-do-mymediadb>/api/v1/webhooks/jellyfin?token=<token>`,
   marcando os eventos `PlaybackStart`/`PlaybackProgress`/`PlaybackStop`.
2. O dono gera o `<token>` numa tela/endpoint de configuração do MyMediaDB
   (mesmo padrão de "chave secreta copiável", sem sistema de usuário/sessão
   completo — esse é o escopo, ver §6).
3. Ao assistir um episódio já catalogado, o Jellyfin dispara os webhooks; o
   MyMediaDB casa o item pelos IDs externos que o payload já carrega
   (`Provider_tmdb`/`Provider_imdb`/`Provider_tvdb`) e grava um
   `historico_evento` por chamada.
4. Se o item do Jellyfin não bate com nenhuma entity conhecida (ainda não
   catalogado, ou catalogado sem os mesmos IDs externos), o evento é
   descartado com log — nunca cria uma entity nova a partir de um webhook
   (evita poluir o catálogo com metadado pobre vindo de um payload de
   playback).
5. Não há UI para visualizar isso ainda (briefing 0094) — a jornada aqui
   termina em "o evento existe no banco, consultável via `GET
   /entities/{id}/historico`" (0092).

## 3. Escopo

**Dentro:**
- Endpoint `POST /api/v1/webhooks/jellyfin?token=` — autenticado por token
  simples (comparação constante-tempo), configurado via `app_settings`.
- Parser do payload do plugin `jellyfin-plugin-webhook` (schema documentado
  publicamente: `NotificationType`, `ItemType` (`Movie`/`Episode`),
  `Provider_tmdb`/`Provider_imdb`/`Provider_tvdb`, `PlaybackPositionTicks`,
  `RunTimeTicks`, `PlaybackPercentage` — os nomes exatos de campo variam por
  versão do plugin; implementação deve tolerar ausência de qualquer campo
  individual sem 500).
- Mapeamento `NotificationType` → `historico_evento.status`:
  `PlaybackStart`→`started`, `PlaybackProgress`→`started` (com
  `progress_percent` atualizado), `PlaybackStop`→`stopped` se
  `progress_percent < 80`, `completed` se `>= 80` (mesmo limiar que Trakt/
  Simkl usam — ADR-0022 §H3). `PlaybackPause`→`paused` (se o plugin emitir
  esse tipo).
- Casamento do item Jellyfin → `entity`: por `external_id_tmdb`/
  `external_id_imdb`/`external_id_tvdb` (colunas geradas já existentes),
  tentando cada ID presente no payload em ordem até achar; sem match, evento
  é descartado e logado (não é erro 500 — sempre 200/204 pro Jellyfin, para
  não fazer o plugin retentar indefinidamente).
- `source` do evento criado = `"jellyfin-webhook"`.
- `kind` do evento = `"assistir"` (só filme/série nesta entrega — item type
  `Movie`/`Episode`; qualquer outro `ItemType` do payload é ignorado).
- Endpoint de configuração do token: gerar/ler/regenerar (reaproveita
  `app_settings`, chave própria, ex. `webhook_jellyfin_token`).
- Testes: payload válido casando por cada ID externo isoladamente, payload
  sem match (nenhum ID bate) descartado sem erro, token inválido → 401,
  token ausente → 401, `PlaybackStop` com progresso ≥80% vira `completed`
  e <80% vira `stopped`, tolerância a payload com campos faltando.

**Fora (explicitamente):**
- Plex e Emby — ficam para um briefing seguinte reaproveitando a mesma
  tabela/endpoint pattern (o parser é o único ponto específico por server;
  o resto — casamento por ID externo, `historico_evento` — já é genérico).
- UI de configuração do webhook/token (tela com botão "copiar token",
  instruções de setup) — briefing 0094 ou um briefing de UI dedicado; esta
  entrega expõe só o endpoint de configuração via API.
- Séries: casar episódio individual (não só a série) — depende de o payload
  do Jellyfin trazer o ID do episódio de forma casável com a `entity type=
  'episode'` do MyMediaDB; se a pesquisa de implementação mostrar que o
  payload só identifica a série (sem o episódio específico), o evento cai na
  entity da SÉRIE como fallback — decisão final registrada na
  implementação, não travada aqui.
- Qualquer autenticação robusta (usuário/sessão, múltiplos tokens por
  instalação) — um único token global via `app_settings` é suficiente para
  uma instalação pessoal; sistema de usuários/permissões é Fase 4 do
  roadmap, não deste briefing.
- Música/livro/jogo — fora de escopo, esse webhook é só de media server de
  vídeo.

## 4. Contrato técnico

### Dados / modelo
- Sem migration nova além da 0092. Reaproveita `historico_evento` e
  `app_settings` (chave nova `webhook_jellyfin_token`, valor
  `{"token": "..."}`).

### API
- `POST /api/v1/webhooks/jellyfin?token=<token>` — corpo = payload do
  Jellyfin (JSON, forma variável por versão do plugin, parseado
  best-effort). Sempre 204 se o token bater (mesmo em caso de payload não
  reconhecível/sem match — para não fazer o Jellyfin retentar); 401 se o
  token não bater ou faltar.
- `POST /api/v1/webhooks/jellyfin/token` — (re)gera um token novo,
  substitui o anterior, devolve `{"token": "..."}`. Autenticado como
  qualquer outro endpoint interno da API (sem token próprio — é o painel do
  dono operando).
- `GET /api/v1/webhooks/jellyfin/token` — devolve o token atual (ou 404 se
  nunca gerado).

### Backend (Go)
- Novo pacote `internal/webhook` (ou `internal/server/webhook_jellyfin.go`,
  a critério de quem implementar): parser do payload +
  `NotificationType`→`status` + casamento por ID externo.
- Reaproveita as queries de `historico_evento` do 0092 para persistir.
- Comparação de token: `crypto/subtle.ConstantTimeCompare`, nunca `==` puro
  (evita timing attack, mesmo sendo um endpoint de baixo risco).

### Arquivos afetados
- `internal/webhook/jellyfin.go` (novo, parser + mapeamento).
- `internal/server/webhook_jellyfin.go` (novo, handlers + roteamento).
- `internal/server/webhook_jellyfin_test.go` (novo).
- `openapi.yaml` (+ `gen:types`).

## 5. Critérios de aceite

- [ ] Payload de exemplo real do `jellyfin-plugin-webhook` (capturado ou
      reproduzido da documentação pública do plugin) com `Provider_tmdb`
      casando uma entity existente gera um `historico_evento` correto.
- [ ] Mesmo teste usando `Provider_imdb`/`Provider_tvdb` isoladamente (sem
      `tmdb` no payload) também casa.
- [ ] Payload sem nenhum ID batendo entity nenhuma → 204, nenhum
      `historico_evento` criado, log registrado.
- [ ] `PlaybackStop` com `PlaybackPercentage >= 80` grava `status=completed`;
      `< 80` grava `status=stopped`.
- [ ] Requisição sem `token` ou com `token` errado → 401, nenhum evento
      criado.
- [ ] `POST /webhooks/jellyfin/token` gera um token; requisição subsequente
      ao webhook com o token antigo (se um existia) deixa de funcionar.
- [ ] Payload com campos ausentes/formato inesperado não derruba o handler
      (sem 500) — degrada para "descartado e logado".
- [ ] `go build ./... && go vet ./... && go test ./... -count=1` verde.
- [ ] `make gate-local` passa.

## 6. Notas de implementação / decisões

- **Por que token via `app_settings` em vez de esperar o sistema de
  usuários/permissões (Fase 4)?** Bloquear um ganho de alavancagem imediata
  (webhook de graça) numa dependência de fundação muito maior e ainda não
  planejada em detalhe não se justifica — um token único, comparado em
  tempo constante, é proporcional ao risco real (instalação pessoal,
  webhook read-mostly). Migrar para o sistema de usuários fica como
  evolução natural quando aquele existir.
- **Por que Jellyfin primeiro, não Plex?** Webhook nativo do Plex exige
  Plex Pass (assinatura paga); Jellyfin é 100% open source com plugin de
  webhook oficial documentado publicamente, sem paywall — menor fricção
  para o dono validar o conceito.
- **Por que nunca criar entity nova a partir do webhook?** Um payload de
  playback não tem metadado suficiente (sinopse, elenco, artes) para um
  catálogo de qualidade — auto-criar geraria entities pobres. Preferir
  descartar e logar; o dono cataloga pela via normal (busca/import) e o
  histórico retroativo pode ser preenchido depois via API manual (0092) se
  o dono quiser.
- **Por que sempre responder 200/204 mesmo sem match?** Comportamento
  padrão de webhook: responder erro faz o emissor retentar (o
  `jellyfin-plugin-webhook`, como a maioria, tem retry); "sem match" não é
  uma falha do MyMediaDB, é esperado (item ainda não catalogado) — não deve
  gerar retry nem alarme.

## 7. Fora deste briefing / follow-ups

- Webhook do Plex (quando o dono tiver Plex Pass) e do Emby, reaproveitando
  o mesmo `historico_evento`/casamento por ID externo — só o parser muda.
- UI de configuração do webhook (copiar token, passo a passo).
- Casamento fino por episódio individual, se a pesquisa de implementação
  mostrar que o payload do Jellyfin permite.
