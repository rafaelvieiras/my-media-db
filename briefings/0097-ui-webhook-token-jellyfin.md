# 0097 — UI de Webhook: `/dashboard/webhook` (token de entrada Jellyfin)

> Tela de administração para gerar/copiar o token do webhook de entrada
> (Jellyfin), consumindo a API que o briefing 0093 já expõe — e reservando o
> espaço da tela para webhooks de saída futuros.

| | |
|---|---|
| **ID** | 0097 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Histórico de consumo |
| **Status** | done |
| **Docs relacionadas** | [briefing 0093](0093-webhook-jellyfin-historico-consumo.md), [briefing 0094](0094-ui-historico-e-estado-assistido.md), [ADR-0022](../docs/adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md), [docs/00 — H2](../docs/00-agenda-de-decisoes.md) |
| **Depende de** | 0093 (feito) |

---

## 1. Objetivo

O backend do webhook de entrada do Jellyfin (0093) já tem endpoints prontos
para gerar/ler o token (`GET`/`POST /api/v1/webhooks/jellyfin/token`), mas
**nenhuma UI os consome** — hoje o dono só consegue configurar o Jellyfin
apontando pra esse webhook via curl manual. Tanto o 0093 quanto o 0094
previram essa tela e cada um empurrou a responsabilidade pro outro (0093 §3/§7,
0094 §escopo-fora); o resultado é que ela nunca foi construída. Este briefing
fecha esse buraco: uma tela dedicada em `/dashboard/webhook` pra gerar o
token, copiar a URL pronta pra colar no Jellyfin, e reservar o espaço visual
onde webhooks de **saída** (eventos disparados pelo MyMediaDB pra serviços
externos, ainda não implementados — ver H2/ADR-0022, capacidade futura
`TrackerProvider`) vão aparecer quando existirem.

## 2. Jornada do usuário

1. O dono abre `/dashboard` e vê um novo item **"Webhook"** na sidebar
   (`DashboardShell`), entre "Configurações" e "Saúde".
2. `/dashboard/webhook` mostra a seção **"Webhook de entrada — Jellyfin"**:
   - Se nenhum token foi gerado ainda (`GET` 404): mensagem "Nenhum token
     gerado ainda" + botão **"Gerar token"**.
   - Se já existe token: campo mascarado (tipo senha) com o token, botão
     "Mostrar"/"Ocultar", botão **"Copiar URL do webhook"** (monta
     `${origin}/api/v1/webhooks/jellyfin?token=${token}`) e botão **"Copiar
     token"**. Abaixo, instrução curta de como colar essa URL no plugin de
     Webhooks do Jellyfin.
   - Botão **"Gerar novo token"** sempre visível — ao clicar, pede
     confirmação (`AlertDialog` ou `window.confirm`, mesmo padrão já usado no
     projeto) porque invalida o token atual (o Jellyfin vai parar de
     autenticar até reconfigurar a URL nova).
3. Uma segunda seção, **"Webhooks de saída"**, aparece desabilitada/vazia com
   texto explicando que ainda não existem eventos de saída configuráveis
   (placeholder consciente — não é uma feature real ainda, só reserva o
   espaço da tela pra quando `TrackerProvider`/H2 virar implementação).
4. Estados de loading/erro cobertos nos dois níveis.

## 3. Escopo

**Dentro:**
- Rota `/dashboard/webhook`, registrada em `router.tsx` como filha de
  `dashboardRoute`.
- Item "Webhook" na sidebar do `DashboardShell`.
- Wrappers de API (`getJellyfinWebhookToken`, `regenerateJellyfinWebhookToken`)
  em `web/src/lib/api.ts` + `queryOptions` em `web/src/lib/queries.ts` — os
  tipos (`JellyfinWebhookToken`) já existem em `api-types.ts` (gerados a
  partir do `openapi.yaml` já existente do 0093); **não é necessário mudar o
  OpenAPI nem rodar `gen:types`**.
- Botões "copiar token"/"copiar URL" via `navigator.clipboard.writeText`
  (não existe nenhum padrão de clipboard no projeto ainda — este briefing
  introduz o primeiro uso).
- Seção "Webhooks de saída" como placeholder estático (sem API, sem estado).
- Testes de componente (Vitest + Testing Library) cobrindo os critérios de
  aceite; pelo menos 1 spec Playwright (E2E) do fluxo (gerar → copiar →
  regenerar), seguindo o padrão de specs recentes — necessário para não
  regredir a cobertura de navegador da catraca.

**Fora (explicitamente):**
- Qualquer webhook de saída real (eventos, `TrackerProvider`, sync com
  Trakt/etc.) — só o placeholder visual.
- Suporte a outros players de entrada (Plex/Emby) — mesmo padrão, outro
  briefing, quando o parser existir (0093 §7).
- Botão de "testar webhook" (disparar um evento sintético) — não existe
  endpoint de teste no backend.
- Qualquer alteração no backend/OpenAPI — o contrato do 0093 já é suficiente.

## 4. Contrato técnico

### API

Nenhuma nova — consome a API já existente do 0093:
- `GET /api/v1/webhooks/jellyfin/token` → 200 `JellyfinWebhookToken`
  (`{token: string}`) ou 404 se nunca gerado.
- `POST /api/v1/webhooks/jellyfin/token` → 200 `JellyfinWebhookToken`
  (gera/substitui).

### Frontend (Vite/React)

- `web/src/routes/webhook.tsx` — `WebhookScreen`: segue o padrão estrutural
  de `settings.tsx` (seções empilhadas, `useQuery`/`useMutation` por seção,
  `LoadingState`/`ErrorState`/`FormSaveFooter` de `@/components/query-states.tsx`),
  **não** o `SchemaForm` do 0030 (esse padrão é para CRUD de entidade, não se
  aplica aqui).
  - `useQuery(jellyfinWebhookTokenQueryOptions)`: tratar 404 como dado válido
    (`{token: null}`), não como `isError` — a tela não deve mostrar
    `ErrorState` na primeira visita sem token gerado.
  - `useMutation` para `regenerateJellyfinWebhookToken`, invalidando a query
    acima no `onSuccess`.
  - Toggle mostrar/ocultar token: estado local (`useState`), sem persistir.
  - Copiar: `navigator.clipboard.writeText`, com feedback visual (toast ou
    texto temporário "Copiado!").
- `web/src/lib/api.ts`: `JellyfinWebhookToken` (tipo de
  `components['schemas']['JellyfinWebhookToken']`), `getJellyfinWebhookToken()`
  (GET, tratando 404 → retorna `{token: null}` em vez de lançar),
  `regenerateJellyfinWebhookToken()` (POST sem body).
- `web/src/lib/queries.ts`: `jellyfinWebhookTokenQueryOptions` (`queryKey:
  ['webhooks', 'jellyfin', 'token']`).
- `web/src/components/dashboard-shell.tsx`: novo item `{ to:
  '/dashboard/webhook', label: 'Webhook', icon: Webhook }` (ícone
  `Webhook` de `lucide-react`) no array `ITENS`, entre "Configurações" e
  "Saúde"; estender o union type de `to`.
- `web/src/router.tsx`: `dashboardWebhookRoute` (`path: '/webhook'`,
  `component: WebhookScreen`, import de `@/routes/webhook`), somada em
  `dashboardRoute.addChildren([...])`.

### Arquivos afetados

- `web/src/routes/webhook.tsx` (+ teste) (novo)
- `web/src/components/dashboard-shell.tsx` (+ teste, novo item)
- `web/src/router.tsx` (1 rota nova)
- `web/src/lib/api.ts`, `web/src/lib/queries.ts`
- `e2e/tests/webhook.spec.ts` (novo)

## 5. Critérios de aceite

- [x] `/dashboard/webhook` sem token gerado mostra o estado vazio + botão
      "Gerar token"; ao clicar, mostra o token novo — desmascarado por padrão
      (decisão tomada na implementação: um token recém-gerado precisa ser
      copiável sem um clique extra em "Mostrar"; um token pré-existente
      carregado do backend continua mascarado por padrão).
- [x] Com token já existente, a tela mostra o campo mascarado, toggle
      mostrar/ocultar funciona, e os botões "Copiar token"/"Copiar URL do
      webhook" colocam o valor correto na área de transferência (URL no
      formato `<origin>/api/v1/webhooks/jellyfin?token=<token>`).
- [x] "Gerar novo token" pede confirmação antes de substituir o token
      existente.
- [x] Seção "Webhooks de saída" aparece como placeholder, sem chamada de
      API.
- [x] Sidebar do `DashboardShell` mostra "Webhook" e marca
      `aria-current="page"` quando ativa.
- [x] Estado de erro real (API fora do ar, não 404) e de loading cobertos.
- [x] `tsc -b` zero-tolerância; `npm run lint`; `npm run test:coverage` sem
      regressão; spec Playwright verde e contribuindo pra cobertura de
      navegador da catraca (não regredir `e2e_front_coverage_lines`).

## 6. Notas de implementação / decisões

- **Reuso do padrão de `settings.tsx`**, não do `SchemaForm`/0030 — a tela
  não é um formulário de campos dinâmicos, é uma ação (gerar/copiar/
  regenerar) com um único valor derivado do backend.
- **404 tratado como estado válido**, não erro — decisão explícita pra evitar
  que a tela pisque `ErrorState` toda vez que o dono ainda não gerou o
  token (comportamento normal na primeira visita).
- **Seção de saída é só placeholder** — evita a tela nascer prometendo uma
  feature que não existe (H2/ADR-0022 já decidiu que saída é capacidade de
  provider futura, não um botão de configuração hoje).

## 7. Fora deste briefing / follow-ups

- Webhooks de saída reais, quando `TrackerProvider` (ADR-0022) ganhar
  implementação.
- Webhook de entrada para Plex/Emby (mesmo padrão de tela, outro parser).
- Botão "testar webhook" (dispara evento sintético), se um endpoint de teste
  for criado no backend.
