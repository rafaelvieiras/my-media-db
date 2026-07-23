# 0075 — Navegação responsiva em telas de celular

> Abaixo de ~768px, a barra de navegação hoje não colapsa: ela transborda e
> arrasta a página inteira para scroll horizontal.

| | |
|---|---|
| **ID** | 0075 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | ready |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md) |
| **Depende de** | — |

---

## 1. Objetivo

A auditoria de UX redimensionou o viewport para 390×844 (iPhone padrão) na
tela de detalhe de um filme. `AppHeader` (`web/src/components/app-header.tsx`)
hoje renderiza todos os links de navegação numa única linha flex sem nenhum
breakpoint (Biblioteca / Buscar / Bibliotecas / Revisão / Órfãs /
Administração — Configurações/Jobs / Saúde / seletor de idioma) — não há
menu-hambúrguer nem colapso, os itens simplesmente transbordam.

Medido: `document.body.scrollWidth` de 1043px contra `window.innerWidth` de
390px — não é só a barra que rola de lado, é a **página inteira** (o
`scrollWidth` do body reflete o filho mais largo, que é a nav). Isso não é um
ajuste fino de espaçamento: é a tela mais visitada do app (detalhe de
item) ficando de fato quebrada em qualquer celular. Um catálogo de mídia é um
caso de uso natural para consulta rápida no sofá, no celular.

## 2. Jornada do usuário

1. O dono abre o app no celular (ou redimensiona a janela abaixo de ~768px).
2. **Hoje**: a barra de navegação transborda a largura da tela; a página
   inteira ganha scroll horizontal (o conteúdo abaixo da nav também é
   arrastado para o lado ao rolar).
3. **Depois deste briefing**: abaixo do breakpoint, a nav colapsa — os links
   secundários (Órfãs, Administração/Configurações/Jobs, Saúde) ficam atrás de
   um menu (ex.: ícone de menu com um painel/`Sheet`), mantendo visíveis só o
   essencial (logo, Biblioteca, Buscar, Bibliotecas, Revisão) ou um único
   botão de menu. A página nunca ganha scroll horizontal por causa da nav.

## 3. Escopo

**Dentro:**
- `AppHeader`: breakpoint responsivo (Tailwind `sm:`/`md:`, consistente com o
  resto do app) que colapsa os links secundários atrás de um menu abaixo de
  ~768px.
- Garantir que a nav nunca force `scrollWidth` do body além do viewport em
  larguras pequenas (390px como caso de teste mínimo).

**Fora (explicitamente):**
- Qualquer outra tela/componente que também não seja responsiva (a auditoria
  cobriu especificamente a tela de detalhe; se outras telas tiverem overflow
  próprio não relacionado à nav, é achado separado).
- Reorganizar a ordem/agrupamento dos links em si (Administração continua
  como grupo "Configurações + Jobs") — só o MECANISMO de colapso é novo, a
  informação exibida é a mesma.
- Gestos de navegação mobile (swipe, bottom nav) — fora de escopo, é só o
  transbordo que está sendo corrigido.

## 4. Contrato técnico

### Frontend (Vite/React)

- `web/src/components/app-header.tsx`:
  - Abaixo do breakpoint escolhido (sugestão: `md` do Tailwind, 768px, para
    casar com o texto do achado), esconder os `NavLink`s secundários (Órfãs,
    o grupo Administração, Saúde, e possivelmente o `LanguageSelector`) atrás
    de um botão de menu — reaproveitar um componente já existente no design
    system se houver (`Sheet`/`Popover`/`DropdownMenu` do shadcn, já usados em
    outras telas do app) em vez de introduzir um padrão novo de menu.
  - Os links primários (Biblioteca, Buscar, Bibliotecas, Revisão) continuam
    visíveis mesmo no colapso, OU todos os links (inclusive esses) migram
    para dentro do menu no mobile — decisão de implementação; o critério de
    aceite é "nenhum overflow horizontal", não uma composição exata de quais
    links ficam fora do menu.
  - Testar explicitamente em 390px de largura (o caso medido na auditoria) e
    confirmar `document.documentElement.scrollWidth <=
    document.documentElement.clientWidth` (ou o body, dependendo de onde o
    overflow nasce) nesse viewport.

### Arquivos afetados

- `web/src/components/app-header.tsx` (+ `.test.tsx`).

## 5. Critérios de aceite

- [ ] Em viewport de 390×844, a página de detalhe de um item não tem scroll
      horizontal (`scrollWidth` do documento/body igual ao `innerWidth`, sem
      margem de transbordo da nav).
- [ ] Todos os links de navegação hoje visíveis continuam alcançáveis no
      mobile — atrás de um menu, se colapsados — sem perder nenhuma rota.
- [ ] Acima do breakpoint (desktop), a nav continua exatamente como está hoje
      (nenhuma regressão visual/funcional em telas largas).
- [ ] `make e2e-cov` e `make gate-local` passam (incluindo qualquer teste de
      viewport mobile já existente na suíte de componentes/E2E).

## 6. Notas de implementação / decisões

- Este é o achado de UX que o próprio briefing 0071 já havia identificado e
  explicitamente colocado FORA de escopo ("Corrigir a navbar não colapsar em
  mobile — achado da auditoria, mas é problema de nível de aplicação (layout
  global), não desta tela") — este briefing existe para fechar esse
  follow-up, já que agora ele está confirmado por uma segunda auditoria
  independente (na tela de item) como o mesmo problema de nível de
  aplicação.
