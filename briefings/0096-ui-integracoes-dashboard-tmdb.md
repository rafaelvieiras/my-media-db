# 0096 — UI de Integrações: `/dashboard/integrations` + subpágina TMDB

> Tela de administração para configurar credenciais de provider por formulário
> dinâmico, substituindo a edição manual de `.env` — consumindo a API do 0095.

| | |
|---|---|
| **ID** | 0096 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Integrações configuráveis via UI |
| **Status** | done |
| **Docs relacionadas** | [ADR-0023](../docs/adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md), [03-arquitetura](../docs/03-arquitetura.md) |
| **Depende de** | 0095 |

---

## 1. Objetivo

Dar ao dono uma tela para configurar credenciais de provider (TMDB primeiro)
sem editar `.env`/redeployar — consumindo a API do 0095. Segunda metade do
épico de Integrações (ADR-0023); primeira vitrine visual da tela que futuros
plugins vão usar para pedir suas próprias credenciais.

## 2. Jornada do usuário

1. O dono abre `/dashboard` e vê um novo item **"Integrações"** na sidebar
   (`DashboardShell`, entre "Configurações" e "Saúde").
2. `/dashboard/integrations` lista as integrações registradas em cards/linha:
   nome, descrição curta, badge **"Configurada"** (verde) ou **"Não
   configurada"** (neutro) — hoje só o card do TMDB.
3. Clica no card do TMDB → `/dashboard/integrations/tmdb`: formulário
   gerado a partir do schema (`GET /integrations/tmdb`) — dois campos
   `token`/`api_key`, ambos tipo senha (toggle mostrar/ocultar), cada um
   mostrando por baixo do label uma legenda conforme a `origem` efetiva:
   - `origem:"env"` → *"Definido pela variável de ambiente
     MYMEDIADB_TMDB_TOKEN — salve aqui para sobrepor."*
   - `origem:"banco"` → *"Definido no painel."* + botão "Limpar" (grava
     `null`, volta a herdar da env var).
   - `origem:"nenhum"` → sem legenda, campo vazio.
4. Preenche o token, clica **Salvar** → `PUT /integrations/tmdb`; toast de
   sucesso: *"Salvo. Reinicie o servidor (`docker restart`) para aplicar."*
   — sem hot-reload nesta entrega (0095 §6).
5. Volta pra `/dashboard/integrations`: o card do TMDB já reflete
   `configurada:true` (a API recalcula na hora; não depende do restart pra
   ATUALIZAR A TELA, só pro provider em si usar o novo valor).
6. Estado vazio: se o Registry não tiver nenhuma integração (não deveria
   acontecer com o núcleo, mas é o caso de plugins removidos), a lista mostra
   "Nenhuma integração registrada."

## 3. Escopo

**Dentro:**
- Rotas `/dashboard/integrations` (lista) e `/dashboard/integrations/$slug`
  (formulário), registradas em `router.tsx` como filhas de `dashboardRoute`.
- Item "Integrações" na sidebar do `DashboardShell`.
- Componente de lista (cards/tabela) + componente de formulário dinâmico
  reaproveitando o padrão de "form gerado por descritor" do 0030
  (`SchemaForm`/TanStack Form + Zod) — adaptado ao vocabulário de
  `integration.Campo` (inclui o widget `secret`, que o `SchemaForm` de
  entidade não tem).
- Loading/erro/vazio nos dois níveis (lista e formulário), mesmo padrão de
  `ErrorState`/`LoadingState` já usado em `settings.tsx`.
- Testes de componente (Vitest + Testing Library) cobrindo os critérios de
  aceite; pelo menos 1 spec Playwright (E2E) do fluxo completo (ver
  `e2e/tests/`, seguindo o padrão de `resetBiblioteca`/fake provider já
  usado em specs recentes — necessário para não regredir a cobertura de
  navegador da catraca, como aconteceu no epic de histórico/0092-0094).

**Fora (explicitamente):**
- Botão "Testar conexão" (capacidade `Testador` não existe no 0095).
- Qualquer coisa além do TMDB no Registry (TVDB/fanart.tv ficam com env var
  direto até serem migrados, fora deste briefing).
- Aviso/telemetria de "servidor precisa reiniciar" além do toast estático
  (nada de polling pra saber se o restart já rolou).

## 4. Contrato técnico

### API

Nenhuma nova — consome integralmente a API do 0095
(`GET /integrations`, `GET/PUT /integrations/{slug}`).

### Frontend (Vite/React)

- `web/src/routes/integrations.tsx` — `IntegrationsListScreen`: `useQuery`
  em `integrationsQueryOptions` (nova em `web/src/lib/queries.ts`), grid de
  cards com nome/descrição/badge, `Link to="/dashboard/integrations/$slug"`.
- `web/src/routes/integration-detail.tsx` — `IntegrationDetailScreen`:
  `useQuery` em `integrationDetailQueryOptions(slug)`, monta o formulário via
  um novo `IntegrationForm` (`web/src/components/integration-form.tsx`) —
  TanStack Form, um campo por `Campo` do schema, `type="password"` +
  toggle para `tipo:"secret"`, legenda de origem, botão "Limpar" por campo
  quando `origem==="banco"`. Submit → `useMutation` chamando
  `putIntegration(slug, valores)` (novo em `web/src/lib/api.ts`), invalida a
  query da lista (badge `configurada` atualiza) e mostra o toast de restart.
- `web/src/lib/api.ts`: `listIntegrations()`, `getIntegration(slug)`,
  `putIntegration(slug, valores)` + tipos derivados de `api-types.ts`.
- `web/src/lib/queries.ts`: `integrationsQueryOptions`,
  `integrationDetailQueryOptions(slug)`.
- `web/src/components/dashboard-shell.tsx`: novo item `{ to:
  '/dashboard/integrations', label: 'Integrações', icon: Plug }` (ou ícone
  equivalente do `lucide-react`) na lista `ITENS`.
- `web/src/router.tsx`: `dashboardIntegrationsRoute` e
  `dashboardIntegrationDetailRoute` (com `params: { slug }`), somadas em
  `dashboardRoute.addChildren([...])`.

### Arquivos afetados

- `web/src/routes/integrations.tsx` (+ teste) (novo)
- `web/src/routes/integration-detail.tsx` (+ teste) (novo)
- `web/src/components/integration-form.tsx` (+ teste) (novo)
- `web/src/components/dashboard-shell.tsx` (+ teste, novo item)
- `web/src/router.tsx` (2 rotas novas)
- `web/src/lib/api.ts`, `web/src/lib/queries.ts`
- `e2e/tests/integrations.spec.ts` (novo)

## 5. Critérios de aceite

- [x] `/dashboard/integrations` lista o card do TMDB com o badge correto
      (`configurada` reflete o efetivo do backend, sem lógica própria no
      front).
- [x] Abrir `/dashboard/integrations/tmdb` mostra os 2 campos com a legenda
      de origem correta em cada cenário (env/banco/nenhum).
- [x] Salvar um valor por campo `secret` nunca ecoa o valor de volta no
      formulário — o campo volta a mostrar mascarado/vazio com
      "Definido no painel" (nunca reidrata com o texto digitado a partir da
      resposta do PUT).
- [x] Botão "Limpar" num campo com `origem:"banco"` envia `null` e o campo
      volta a refletir `origem:"env"`/`"nenhum"`.
- [x] Sidebar do `DashboardShell` mostra "Integrações" e marca
      `aria-current="page"` quando ativa (mesmo mecanismo do TanStack Router
      já usado nos outros itens).
- [x] Estado de erro (API fora) e de loading cobertos nas duas telas.
- [x] `tsc -b` zero-tolerância; `npm run lint`; `npm run test:coverage` sem
      regressão; spec Playwright verde e contribuindo pra cobertura de
      navegador da catraca (não regredir `e2e_front_coverage_lines`).

## 6. Notas de implementação / decisões

- **Reuso deliberado do vocabulário do 0030** (`SchemaForm`/descritor por
  campo) em vez de inventar um segundo padrão de formulário dinâmico — só
  estende com o widget `secret`, ausente no formulário de entidade.
- **Toast de restart é estático**, não um indicador de saúde em tempo real —
  simplicidade de MVP (ADR-0023 aceita "sem hot-reload"); reavaliar se algum
  dia o hot-reload virar realidade (aí o toast morre sozinho).
- **Nenhum campo secreto reidrata do servidor.** O formulário sempre parte
  vazio/mascarado nesses campos, mesmo logo após salvar — o valor só existe
  no submit local (estado do form), nunca é lido de volta da API.

## 7. Fora deste briefing / follow-ups

- Botão "Testar conexão" quando a capacidade `Testador` existir no backend.
- UI para TVDB/fanart.tv, quando migrados ao registro (0095 §7).
- UI para o token do webhook Jellyfin (0093), se/quando migrado ao mecanismo
  genérico.
- Indicador de "restart pendente" mais rico que um toast estático.
