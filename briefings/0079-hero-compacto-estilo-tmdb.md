# 0079 — Hero do detalhe mais compacto, no padrão visual do TMDB

> O dono gosta do fluxo/visual do TMDB e pediu um card de topo parecido: pôster
> à esquerda com uma faixa de status, metadados compactos ao lado (classificação,
> gêneros, nota — num "medalhão" circular — e duração numa linha só), slogan em
> itálico, e a sinopse abaixo do card. Este briefing reorganiza o LAYOUT do hero
> já existente (`RichDetailView`) — não adiciona conceito/dado novo que o app
> não tem (sem lista/favorito/"vibe check"/"assista agora": não existem no
> produto).

| | |
|---|---|
| **ID** | 0079 |
| **Tipo** | fix/chore (refactor visual) |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | ready |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md) |
| **Depende de** | 0022, 0023, 0044, 0045, 0048, 0049 |

---

## 1. Objetivo

O dono anexou uma captura de tela do TMDB (detalhe de "O Gato Félix") como
referência de layout e pediu para o card de topo do detalhe (`RichDetailView`,
usado tanto por `/entities/$id` quanto por `/external/...`) ficar "nesse
formato, com informações similares". Hoje (`web/src/components/rich-detail-
view.tsx`, função `RichDetailView`, por volta das linhas 900-999) o hero já
tem quase TODOS os dados que aparecem na referência — título+ano, tipo,
classificação (`certification`), situação (`airing_status`), duração,
nota local do dono (`local_rating`) e nota da fonte (`vote_average`), idioma
original, slogan (`tagline`), gêneros (`genres`), sinopse (`description`) — só
que dispostos em blocos empilhados verticalmente (metadados numa linha corrida,
gêneros como seção própria abaixo com título "Gêneros"), não no arranjo
compacto do TMDB (metadados logo abaixo do título, gêneros como texto corrido
próximo ao topo, nota num círculo/medalhão, pôster com uma faixa de status).

Não existe no my-media-db (e este briefing NÃO adiciona): lista/watchlist,
favorito, "qual é a sua vibe?", "assistir créditos de abertura" como ação
dedicada, "assista agora" (não há player de arquivo local embutido no
detalhe — só trailers externos, via `VideosRail`). O layout deve refletir o
que o produto realmente tem, não replicar pixel-a-pixel widgets que não
existem — o pedido explícito foi "informações similares", não paridade de
feature.

## 2. Jornada do usuário

1. O dono abre `/entities/$id` (ou `/external/...`) de um filme/série qualquer.
2. **Hoje**: o hero mostra pôster + bloco de texto com título, ano, badges de
   tipo/classificação/situação, duração e notas todos numa mesma linha corrida
   de badges/spans; gêneros aparecem como uma SEÇÃO própria (título "Gêneros" +
   badges) abaixo da sinopse.
3. **Depois deste briefing**: o pôster ganha uma faixa de status compacta na
   base (usa a MESMA `situacao` que já existe, ex. "No Ar"/"Finalizada" —
   sem inventar uma ação de "assistir", já que não há player de arquivo local
   aqui). Ao lado, título+ano, badges de tipo/classificação numa linha,
   gêneros como texto/badges compactos logo abaixo do título (não mais uma
   seção própria com heading "Gêneros" separada por scroll), a nota do dono
   (`local_rating`) — quando existir — como um medalhão circular (percentual
   ou nota/10, visual mais parecido com o círculo do TMDB do que o
   ícone-de-estrela atual), slogan em itálico, e a sinopse com heading "Sinopse"
   logo abaixo do card do hero (como já é hoje).

## 3. Escopo

**Dentro:**
- Reorganizar o LAYOUT do hero em `RichDetailView` (`web/src/components/rich-
  detail-view.tsx`): metadados compactados perto do título, gêneros movidos
  para logo abaixo do título/badges (texto corrido ou badges compactos, sem
  heading "Gêneros" própria — a seção com heading pode ser removida ou
  mantida como fallback para quando há MUITOS gêneros; decisão de
  implementação).
- Um "medalhão" circular para a nota local do dono (`local_rating`), no lugar
  do ícone de estrela + número atual — visual mais próximo do círculo de
  "avaliação" do TMDB. Sem nota local, cai no comportamento atual (nota da
  fonte como está, ou nenhum indicador).
- Faixa de status compacta na base do pôster, reaproveitando `situacao`
  (`rotuloSituacao(detail.airingStatus)`, já existe) — só para os tipos que já
  têm essa informação (série/temporada); filme/coleção sem `airing_status`
  continuam sem essa faixa (comportamento atual, sem gating novo por tipo).
- Reposicionar os botões de ação (`actions`/`EntityActionButtons`) se fizer
  sentido no novo arranjo (ex.: mais perto do topo, como no TMDB) — sem mudar
  QUAIS botões existem.

**Fora (explicitamente):**
- Qualquer feature nova que o TMDB tem e o my-media-db não tem: lista/
  watchlist, favorito, "qual é a sua vibe?", player de arquivo local embutido
  no hero, ação "assistir créditos de abertura" dedicada.
- Um "score de completude" numérico tipo o "71%" do editor do TMDB — isso
  exigiria uma MÉTRICA NOVA no backend (hoje `CatalogCompleteness` só tem
  `complete: boolean` + `missing: string[]`, sem percentual); fora de escopo
  aqui, é tema de outro briefing se o dono quiser essa métrica.
- Mudar QUAIS dados aparecem no hero (nenhum campo novo, nenhum campo
  removido) — só a DISPOSIÇÃO visual.
- Mudar a tela de EDIÇÃO (isso é o [0080](0080-subrotas-edicao-item-estilo-
  tmdb.md)).
- Mudar `ElencoGrid`/`EquipeGrupos`/`ProdutorasBand`/`VideosRail`/
  `SimilaresRail`/`ColecaoBand` (seções abaixo do hero, inalteradas).

## 4. Contrato técnico

### Frontend (Vite/React)

- `web/src/components/rich-detail-view.tsx`:
  - Reorganizar o bloco `<div className="flex min-w-0 flex-col gap-2">`
    (linhas ~926-997) — mover os gêneros para essa área (perto do título),
    substituir a seção `{detail.genres.length > 0 ? (...)}` com heading
    "Gêneros" (linhas ~1009-1021) por uma exibição compacta ao lado dos
    demais metadados do hero (ou manter a seção como estava SE a
    implementação achar que gêneros longos não cabem bem compactados — nesse
    caso, documentar a decisão).
  - Adicionar um componente `NotaMedalhao` (ou nome equivalente) que
    renderiza `local_rating` num círculo (SVG simples ou `Progress`
    circular do design system, se existir algo reaproveitável — senão um SVG
    básico com `stroke-dasharray`, sem dependência nova). Mantém o
    comportamento de fallback para `vote_average` que já existe hoje quando
    não há `local_rating`.
  - Adicionar uma faixa de status compacta associada ao `PosterImg` (só
    quando `situacao` existir) — um `<div>` sobreposto/anexado à base do
    pôster com o rótulo da situação.
  - `PosterImg`/`StillFrame` (já existentes) não mudam de assinatura — a
    faixa de status é um wrapper visual em volta, não uma prop nova neles
    necessariamente (decisão de implementação).

### Arquivos afetados

- `web/src/components/rich-detail-view.tsx` (+ `.test.tsx`).

## 5. Critérios de aceite

- [ ] O hero mostra os MESMOS dados de hoje (nenhum campo novo, nenhum
      removido), só reorganizados: gêneros perto do título, nota local em
      medalhão circular, faixa de status na base do pôster (quando aplicável).
- [ ] Sem `local_rating`, o comportamento de nota (mostrar `vote_average` ou
      nada) continua o mesmo de hoje, só que no componente de medalhão.
- [ ] Sem `airing_status` (filme, coleção), o pôster não ganha faixa de status
      — sem regressão nem gating novo por tipo além do que já existe.
- [ ] `/external/...` (detalhe do externo, que reusa a MESMA `RichDetailView`)
      recebe o mesmo layout novo automaticamente — não é um componente
      paralelo, é o mesmo compartilhado (0022/0023 continuam valendo).
- [ ] `make e2e-cov` e `make gate-local` passam.

## 6. Notas de implementação / decisões

- **Por que não copiar os ícones de lista/favorito/vibe do TMDB?** O
  my-media-db é um gestor de biblioteca pessoal, não um serviço de streaming
  com conta de usuário e watchlist social — essas ações não têm um dado por
  trás no produto. O pedido do dono foi "informações similares", que este
  briefing interpreta como: mesma DENSIDADE/organização visual dos dados que
  JÁ EXISTEM, não replicar cada widget do TMDB.
- Este é um refactor VISUAL — nenhuma rota, endpoint ou contrato de API muda.
