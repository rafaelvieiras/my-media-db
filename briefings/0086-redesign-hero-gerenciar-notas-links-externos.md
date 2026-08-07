# 0086 — Redesign do detalhe do item: hero com "Gerenciar", notas por fonte, links externos

> A tela de detalhe do item (`/entities/{id}`) ganha uma disposição inspirada
> num app de referência (print anexado pelo dono, estilo Seerr/Overseerr):
> ações compactadas num ÚNICO dropdown "Gerenciar" dentro do hero, uma fileira
> de NOTAS de fontes diferentes (hoje só TMDB, extensível) e uma fileira
> HORIZONTAL de links para provedores externos com o LOGO de cada site,
> logo abaixo do hero.

| | |
|---|---|
| **ID** | 0086 |
| **Tipo** | feature/redesign |
| **Fase (roadmap)** | Fase 3 — polimento de UI |
| **Status** | done |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0082 (hero imersivo full-bleed), 0079 (medalhão de nota local), 0029 (MD6 elenco/equipe/produtoras) |

---

## 1. Objetivo

O dono pediu um redesign da tela de detalhe, mostrando um print de
referência (Seerr/Overseerr: hero full-bleed + pôster + título + fileira de
notas de fontes diferentes no topo + ações de topo + fileira de logos de
provedores). O pedido tem 3 partes explícitas, com um desvio DELIBERADO do
print nas duas últimas (texto do dono vence a imagem onde divergem):

1. Trazer a DISPOSIÇÃO geral do print (hero rico, ações e notas integradas ao
   hero, item em destaque).
2. Trazer duas informações que a tela não mostra hoje: **notas de fontes
   diferentes** (hoje o hero já mostra UMA nota "da fonte" — vira uma fileira
   com uma nota por fonte que a modelar, ao lado da nota local do dono) e
   **links para provedores externos com o logo do site** (hoje existem como
   texto/ícone genérico no fim da página).
3. **Compactar toda ação em um único botão dropdown "Gerenciar"**, dentro do
   hero (não numa barra separada acima dele como hoje) — desvio explícito do
   print, que usa 2-3 botões soltos.
4. Cada link externo **abaixo do hero**, em **linha horizontal**, com logo —
   desvio explícito do print, que os mostra numa sidebar lateral (este
   projeto não tem sidebar de metadados hoje, e não é objetivo deste
   briefing criar uma — ver §3 "Fora").

## 2. Jornada do usuário

1. O dono abre um item importado (`/entities/{id}`). O hero mostra pôster,
   título, badges, tagline, sinopse — como hoje — mas agora também: (a) uma
   fileira de notas (sua nota, quando avaliou + a nota de cada fonte externa
   que a modela) e (b) um único botão **"Gerenciar"** perto do título/trailer,
   no lugar da fileira de botões que hoje fica acima do hero.
2. Ao clicar "Gerenciar", vê um menu com: Editar · Baixar informações
   faltando (só quando aplicável) · Baixar arte faltando (só quando
   aplicável) · Salvar informações (NFO) · — e, só para coleção — Excluir
   coleção (destrutivo, com confirmação).
3. Logo abaixo do hero, antes de qualquer outra seção, uma fileira horizontal
   mostra o logo de cada provedor com link público do item (TMDB sempre que
   há id; IMDb e TVDB quando o item tem o id correspondente) — clicar abre a
   página do provedor numa aba nova.
4. Ao abrir o item do EXTERNO (antes de importar, mesma `RichDetailView`), o
   comportamento é o MESMO onde faz sentido: fileira de links continua
   aparecendo (com o que a fonte tiver); a fileira de notas mostra só a nota
   da própria fonte sendo visualizada (não há "fontes cruzadas" antes de
   importar — só existe UMA fonte na tela do externo); não há "Gerenciar"
   (o externo continua com seu único botão "Importar", sem mudança).

## 3. Escopo

**Dentro:**
- Novo componente de menu dropdown "Gerenciar" (radix-ui `DropdownMenu`, já
  é dependência via o pacote unificado `radix-ui` — **sem novo pacote npm**),
  substituindo a fileira de botões soltos hoje em `entity-action-buttons.tsx`
  (Editar/Baixar informações faltando/Baixar arte faltando/Salvar NFO) E o
  "Excluir coleção" hoje solto no fim da página
  (`entity-detail.tsx::ExcluirColecao`) — os 3-5 viram itens de UM menu.
- Novo componente `AlertDialog` (radix-ui, mesmo pacote) para a confirmação
  de "Excluir coleção" (substitui o toggle de 2 botões inline por um diálogo
  modal — dropdown fecha ao clicar no item, então a confirmação não pode
  mais ser inline dentro do próprio menu).
- Hero (`HeroImersivo`) passa a renderizar a barra de ações (trailer +
  Gerenciar) DENTRO do próprio hero (não mais numa `BarraAcoesHero` acima
  dele) — `RichDetailView` para de renderizar essa barra separada.
- `Entidade.Ratings` novo (backend): notas por FONTE (hoje só TMDB modela
  `vote_average`; extensível a qualquer provider que também modelar,
  sem tocar no núcleo — princípio de capacidade, AGENTS.md §6). A fileira no
  hero substitui o badge único "nota da fonte" por N badges (um por fonte
  presente), mantendo a nota LOCAL do dono como está hoje (medalhão âmbar,
  sempre em destaque quando existe).
- `richfields.LinksExternos` ganha IMDb e TVDB construídos a partir dos ids
  já presentes em `entity.external_ids_json` (mesma fonte que já alimenta
  `external_id_imdb`/`external_id_tvdb`), além do TMDB que já existe —
  fallback para o que a fonte já mandar em `campos["external_links"]`
  quando presente (não sobrescreve).
- Fileira horizontal de links com LOGO por provedor (`ExternalLinksRow`,
  substitui a seção `LinksExternos` de hoje) movida para logo ABAIXO do
  hero (era a ÚLTIMA seção da página, depois de Produtoras).
- Logos novos, **inline SVG** (sem dependência de rede/CDN, sem asset
  binário): TMDB, IMDb, TVDB — monocromáticos/simples, mesmo espírito de
  `web/public/icons.svg` já usado no projeto para ícones sociais. Fonte
  desconhecida (provider sem entrada no registro) degrada para um ícone
  genérico (`Link`/`ExternalLink` do lucide) + o nome em maiúsculas — nunca
  quebra, nunca esconde o link.

**Fora (explicitamente):**
- Sidebar de metadados estilo Seerr (Status/Datas de lançamento/Budget/
  Revenue/Estúdio/"Currently Streaming On") — esses dados **não são
  capturados hoje** por nenhum provider registrado (grep confirmou: sem
  `budget`/`revenue` no richfields nem no provider TMDB); layout continua
  single-column (`max-w-4xl`), sem uma segunda coluna lateral. Se o dono
  quiser isso, é um briefing próprio (mapear os campos no provider TMDB
  primeiro).
- Notas de IMDb/Rotten Tomatoes reais — não há provider para isso hoje (só
  OMDB teria essa informação, integração não pedida). A fileira de notas é
  **genérica por fonte já registrada**; no dia em que outro provider modelar
  `vote_average`, ele aparece sozinho, sem mudança de código na tela.
- Links de fanart.tv/IMVDb na fileira de provedores — são fontes só-de-arte/
  tema (0048/`fanarttv-arte-provider`), sem uma "página pública do item" que
  valha a pena linkar aqui; registro de logos fica só em tmdb/imdb/tvdb por
  ora, mas o registro (`PROVIDER_META`) é uma tabela extensível — adicionar
  uma fonte nova no futuro é uma entrada, não um redesign.
- Ações por-asset da galeria (definir principal, salvar na pasta, baixar pro
  cache, remover, enviar arte) CONTINUAM onde estão (dentro do lightbox da
  `ArtGallery`) — não entram no dropdown "Gerenciar" (são ações por-item da
  galeria, não ações do item como um todo).
- Reordenar/redesenhar as seções ABAIXO da fileira de links (Palavras-chave,
  Vídeos, Coleção, Similares, Elenco, Equipe, Produtoras, slots da entity)
  — permanecem exatamente onde estão, na mesma ordem.
- Breakpoints/responsividade novos — reusa os mesmos padrões `sm:` já
  usados no hero; sem trabalho de mobile dedicado além disso.

## 4. Contrato técnico

### Backend (Go)

**`internal/resolver/resolver.go`**
- Novo tipo:
  ```go
  // RatingFonte é a nota (vote_average) de UMA fonte específica — ao contrário
  // de VoteAverage (mesclado por prioridade, 1 valor só), aqui é 1 entrada POR
  // fonte que modela o campo, para a UI mostrar lado a lado (briefing 0086).
  type RatingFonte struct {
      Source string  `json:"source"`
      Value  float64 `json:"value"`
  }
  ```
- `Entidade` ganha `Ratings []RatingFonte \`json:"ratings"\`` (sempre `[]`,
  nunca `null`).
- Novo helper, mesmo arquivo, ao lado de `camposExternosMesclados`:
  ```go
  // ratingsPorFonte (0086) coleta a nota de CADA camada externa que modela
  // vote_average, em vez do valor único mesclado por prioridade — permite a UI
  // mostrar notas de fontes diferentes lado a lado. Fonte que não modela o
  // campo simplesmente não aparece (degradação silenciosa, AGENTS.md §6).
  func ratingsPorFonte(externo []CamadaExterna) []RatingFonte {
      out := []RatingFonte{}
      for _, cam := range externo {
          if v := richfields.FloatPtrDe(cam.Campos, "vote_average"); v != nil {
              out = append(out, RatingFonte{Source: cam.Fonte, Value: *v})
          }
      }
      return out
  }
  ```
  Chamado em `montarEntidade` (ou equivalente, ~linha 477-509): acrescentar
  `Ratings: ratingsPorFonte(c.Externo),` ao literal de `Entidade{...}`.
- `richfields.LinksExternos` (assinatura muda — atualizar o ÚNICO outro
  call site, `external_detail.go`): de
  `LinksExternos(campos map[string]any, tmdbTipo, tmdbID string)` para
  `LinksExternos(campos map[string]any, tmdbTipo, tmdbID, imdbID, tvdbID string) map[string]string`.
  Nova lógica (`internal/richfields/richfields.go`, função já existe na
  linha ~211):
  ```go
  func LinksExternos(campos map[string]any, tmdbTipo, tmdbID, imdbID, tvdbID string) map[string]string {
  	out := map[string]string{}
  	if m, ok := campos["external_links"].(map[string]any); ok {
  		for k, v := range m {
  			if s, ok := v.(string); ok && s != "" {
  				out[k] = s
  			}
  		}
  	}
  	if tmdbID != "" && tmdbTipo != "" {
  		out["tmdb"] = "https://www.themoviedb.org/" + tmdbTipo + "/" + tmdbID
  	}
  	if imdbID != "" {
  		if _, ok := out["imdb"]; !ok {
  			out["imdb"] = "https://www.imdb.com/title/" + imdbID + "/"
  		}
  	}
  	if tvdbID != "" {
  		if _, ok := out["tvdb"]; !ok {
  			out["tvdb"] = "https://www.thetvdb.com/dereferrer/series/" + tvdbID
  		}
  	}
  	return out
  }
  ```
  Precedência: o que a FONTE já mandou em `external_links` vence (ids
  crus só preenchem o que falta) — evita sobrescrever uma URL mais
  específica que o provider já tenha montado.
- **`internal/resolver/resolver.go`**, call site (linha ~505): trocar
  `richfields.LinksExternos(rico, c.Entity.Type, externalIDs["tmdb"])` por
  `richfields.LinksExternos(rico, c.Entity.Type, externalIDs["tmdb"], externalIDs["imdb"], externalIDs["tvdb"])`
  (`externalIDs` já é `parseExternalIDs(c.Entity.ExternalIdsJson)`, linha
  462 — já teria tvdb pois `external_id_tvdb` existe desde 0007-series;
  confirmar que `parseExternalIDs` lê a chave `tvdb` do JSON, senão
  acrescentar).
- **`internal/server/external_detail.go`**, call site (linha ~219): trocar
  `richfields.LinksExternos(campos, tmdbTipo, tmdbID)` por
  `richfields.LinksExternos(campos, tmdbTipo, tmdbID, "", "")` — o detalhe
  externo (ainda não importado) não tem `externalIDs` cross-fonte
  resolvidos; comportamento inalterado ali (só ganha o parâmetro extra,
  vazio).
- Testes: `internal/resolver/resolver_rico_test.go` ganha um teste cobrindo
  `Ratings` com 2 camadas mockadas (ambas com `vote_average`, ex. tmdb=7.5 e
  uma fonte fake=8.0) → `Ratings` tem 2 entradas, na ORDEM das camadas
  (prioridade); e um teste com NENHUMA camada modelando o campo → `Ratings:
  []` (não nil). `internal/richfields/richfields_test.go` (se existir; senão
  criar) cobre `LinksExternos` com imdb/tvdb novos e o caso de
  `external_links` já ter `imdb` (não sobrescreve).

### API (`openapi.yaml`)

- Novo schema `EntityRating`:
  ```yaml
  EntityRating:
    type: object
    required: [source, value]
    properties:
      source:
        type: string
      value:
        type: number
  ```
- `ResolvedEntity`: novo campo obrigatório
  `ratings: { type: array, items: { $ref: '#/components/schemas/EntityRating' } }`
  (sempre presente, `[]` quando vazio — mesmo padrão de `genres`/`keywords`).
- `ExternalDetail` (schema do `detalheExternoDTO`) **não muda** — decisão
  explícita (§3 "Fora"): a tela do externo já tem `vote_average` (nota única
  da própria fonte sendo vista); o front sintetiza `ratings` no adapter
  (ver Frontend abaixo), sem precisar de um campo novo na API.

### Frontend (Vite/React)

**Tipos/adapter (`web/src/lib/rich-detail.ts`)**
- `RichDetail` ganha `ratings: { source: string; value: number }[]`.
- `deResolvedEntity(e)`: `ratings: e.ratings` (direto do backend).
- `deExternalDetail(d)`: `ratings: d.vote_average != null ? [{ source: d.source, value: d.vote_average }] : []`
  (sintetizado — 1 entrada, a própria fonte sendo vista).

**Novas primitivas shadcn/ui (mesmo padrão de `sheet.tsx`/`tooltip.tsx`:
import de `{ X as XPrimitive } from 'radix-ui'`, SEM novo pacote npm)**
- `web/src/components/ui/dropdown-menu.tsx`: wrapper estilizado de
  `DropdownMenu` (radix-ui) — `DropdownMenu`, `DropdownMenuTrigger`,
  `DropdownMenuContent`, `DropdownMenuItem` (variant `destructive` via
  `data-variant`, mesmo padrão do resto do design system), `DropdownMenuSeparator`.
- `web/src/components/ui/alert-dialog.tsx`: wrapper estilizado de
  `AlertDialog` (radix-ui) — `AlertDialog`, `AlertDialogTrigger` (não usado
  aqui — abertura é via `open`/`onOpenChange` controlado), `AlertDialogContent`,
  `AlertDialogHeader`, `AlertDialogTitle`, `AlertDialogDescription`,
  `AlertDialogFooter`, `AlertDialogAction` (destructive), `AlertDialogCancel`.

**Logos de provedor + registro (`web/src/components/provider-logos.tsx` novo)**
- `TmdbLogo`, `ImdbLogo`, `TvdbLogo`: componentes SVG inline, simples,
  monocromáticos (`currentColor` onde possível), ~16-20px de altura,
  `aria-hidden` (o texto acessível vive no link/badge pai).
- `web/src/lib/provider-meta.ts` novo:
  ```ts
  export const PROVIDER_META: Record<string, { label: string; Logo?: FC<{ className?: string }> }> = {
    tmdb: { label: 'TMDB', Logo: TmdbLogo },
    imdb: { label: 'IMDb', Logo: ImdbLogo },
    tvdb: { label: 'TVDB', Logo: TvdbLogo },
  }
  export function metaDaFonte(source: string) {
    return PROVIDER_META[source] ?? { label: source.toUpperCase(), Logo: undefined }
  }
  ```
  Reusado tanto pela fileira de NOTAS quanto pela fileira de LINKS — fonte
  sem entrada cai no fallback (texto maiúsculo + ícone genérico), nunca
  quebra.

**`web/src/components/rich-detail-view.tsx`**
- Remove `BarraAcoesHero` (a barra ACIMA do hero some).
- `HeroImersivo` ganha um novo prop `actions?: ReactNode`; internamente
  passa a renderizar, logo após a linha de título/ano/badges (antes dos
  gêneros): `<div className="flex flex-wrap items-center gap-2"><TrailerButton videos={detail.videos} />{actions}</div>`
  (só renderiza a linha quando há trailer OU actions — mesmo guard de hoje,
  agora dentro do próprio componente).
- `RichDetailView` passa `actions` para `HeroImersivo` em vez de renderizar
  `BarraAcoesHero` separadamente.
- Nota (medalhão): troca o bloco atual (linha ~1300-1317, par local/fonte
  único) por uma fileira: `NotaMedalhao` local (inalterado, quando
  `detail.localRating != null`) + um badge compacto por entrada de
  `detail.ratings` (logo/label via `metaDaFonte` + valor `toFixed(1)`) —
  reaproveita o estilo visual de `NotaMedalhao` (contorno neutro) mas troca
  o conteúdo interno para incluir a logo do provedor quando existir.
- `LinksExternos` (função atual, linha ~544) vira `ExternalLinksRow`: cada
  link é um chip (`<a target="_blank" rel="noreferrer noopener">`) com
  `Logo` (via `metaDaFonte`, fallback `ExternalLink` do lucide) + `label`,
  em `flex flex-wrap gap-2` — chamada MOVE de depois de `ProdutorasBand`
  (fim da lista de seções) para logo depois de `<HeroImersivo />` e antes da
  seção de Palavras-chave.

**`web/src/components/entity-action-buttons.tsx` → renomeado/reescrito**
- Renomear o export para `EntityManageMenu` (só 1 call site,
  `entity-detail.tsx`) — mantém toda a lógica de mutation existente
  (`enrich-missing`/`download-missing-artwork`/`write-nfo`), agora
  disparada por `DropdownMenuItem.onSelect` em vez de `Button.onClick`.
- Novo prop `isCollection?: boolean` (default `false`): quando `true`,
  acrescenta `DropdownMenuSeparator` + `DropdownMenuItem` destrutivo
  "Excluir coleção", que abre o `AlertDialog` (estado local
  `alertaAberto`) em vez de mutar direto — a mutation `purgeEntity` (hoje em
  `ExcluirColecao`, `entity-detail.tsx`) migra para dentro deste componente.
- Mensagens de resultado/erro das 3 mutations não-destrutivas continuam
  como texto inline (role="status"/"alert"), agora renderizadas ABAIXO do
  trigger "Gerenciar" (mesmo componente, só mudou o lugar do botão que as
  dispara — nenhuma mudança de UX na mensagem em si).
- Trigger: `Button variant="secondary" size="sm"` com ícone `Settings`
  (lucide) + texto "Gerenciar" + `ChevronDown`.

**`web/src/routes/entity-detail.tsx`**
- Remove a função `ExcluirColecao` inteira (migrou para
  `EntityManageMenu`) e sua renderização (linha ~383) — a seção de coleção
  fica só com `<CollectionMembers id={id} editable />`.
- `actions={<EntityActionButtons ... />}` vira
  `actions={<EntityManageMenu id={id} catalogCompleteness={...} owned={...} isCollection={data.type === 'collection'} />}`.
- Import renomeado (`EntityManageMenu` em vez de `EntityActionButtons`).

**`web/src/lib/api-types.ts`**: regenerar via `npm run gen:types`
(`EntityRating`, `ResolvedEntity.ratings`).

### Arquivos afetados
- `openapi.yaml`
- `internal/richfields/richfields.go`
- `internal/resolver/resolver.go`
- `internal/server/external_detail.go`
- `internal/resolver/resolver_rico_test.go`
- `internal/richfields/richfields_test.go` (novo, se não existir)
- `web/src/lib/rich-detail.ts`
- `web/src/lib/api-types.ts` (gerado)
- `web/src/lib/provider-meta.ts` (novo)
- `web/src/components/provider-logos.tsx` (novo)
- `web/src/components/ui/dropdown-menu.tsx` (novo)
- `web/src/components/ui/alert-dialog.tsx` (novo)
- `web/src/components/rich-detail-view.tsx`
- `web/src/components/entity-action-buttons.tsx` (reescrito, considerar
  renomear arquivo para `entity-manage-menu.tsx`)
- `web/src/routes/entity-detail.tsx`
- Testes: `web/src/components/entity-action-buttons.test.tsx` (ou novo nome),
  `rich-detail-view.test.tsx`, `entity-detail.test.tsx` — estender para o
  dropdown, o alert-dialog de exclusão, a fileira de notas e a fileira de
  links.

## 5. Critérios de aceite

- [ ] `GET /entities/{id}` de um item com match TMDB devolve
      `ratings: [{"source":"tmdb","value":<n>}]` (não `null`, não ausente).
- [ ] Item sem NENHUM match externo devolve `ratings: []`.
- [ ] `GET /entities/{id}` de uma série com `external_id_tvdb` preenchido
      devolve um link `tvdb` em `external_links` apontando para
      `https://www.thetvdb.com/dereferrer/series/{id}`.
- [ ] `GET /external/{source}/{type}/{id}` continua funcionando sem
      regressão (assinatura de `LinksExternos` mudou, comportamento não).
- [ ] Na tela do item importado: UM botão "Gerenciar" no hero abre um menu
      com Editar sempre presente; "Baixar informações faltando"/"Baixar arte
      faltando" só quando `catalog_completeness` indicar; "Salvar
      informações (NFO)" sempre (desabilitado sem posse).
- [ ] Nenhum botão de ação aparece FORA do dropdown (exceto "Assistir
      trailer", que continua como botão primário ao lado).
- [ ] Abrir uma COLEÇÃO mostra "Excluir coleção" no menu (destrutivo); um
      filme/série NÃO mostra esse item.
- [ ] Clicar "Excluir coleção" abre um diálogo de confirmação (não apaga
      direto); confirmar apaga e volta para `/`; cancelar fecha sem apagar.
- [ ] O hero mostra a nota LOCAL (quando existir) + uma nota por fonte em
      `ratings`, cada uma com o logo/rótulo do provedor.
- [ ] Logo abaixo do hero (antes de Palavras-chave), uma fileira horizontal
      mostra um chip por link em `external_links`, cada um com logo (tmdb/
      imdb/tvdb) ou fallback texto+ícone genérico para fonte desconhecida;
      clicar abre em nova aba.
- [ ] Tela do item do EXTERNO (antes de importar): sem regressão visual —
      continua sem "Gerenciar" (só "Importar"), fileira de links/notas
      funciona com o que a fonte tiver.
- [ ] `tsc -b` zero erros; `npm run lint` limpo; suíte de testes web cobre
      o novo dropdown, o alert-dialog e as duas fileiras novas.
- [ ] Testado visualmente no navegador (dev server) antes de reportar
      concluído — item com trailer+ambas ações condicionais+coleção, para
      exercitar o menu completo.

## 6. Notas de implementação / decisões

- **Por que radix-ui `DropdownMenu`/`AlertDialog` em vez de construir na
  mão**: o projeto já depende do pacote unificado `radix-ui` (usado por
  `Sheet`/`Tooltip`/`Switch`) — usar os mesmos primitivos mantém consistência
  de acessibilidade (foco, ESC, aria) sem NENHUMA dependência nova, evitando
  o gotcha de `npm install` exigir restart do container de dev.
- **Por que "notas de fontes diferentes" não inclui IMDb/Rotten Tomatoes de
  verdade**: o projeto não tem provider nenhum para essas fontes hoje (só
  TMDB modela `vote_average`); a fileira foi desenhada para ser GENÉRICA por
  fonte registrada (independência de provider, AGENTS.md §6) — no dia em que
  outro provider (ou um futuro provider dedicado a ratings) existir, ele
  aparece sozinho, sem mexer na tela de novo. Isso é mais correto que
  hard-codar 4 badges como o print mostra, dos quais hoje só 1
  (TMDB) teria dado real.
- **Por que não replicar a sidebar do print**: os campos que ela mostra
  (Status/Datas separadas por tipo de release/Budget/Revenue/Estúdio/
  Currently Streaming On) não existem no schema nem são mapeados por nenhum
  provider hoje — replicar o VISUAL sem o DADO real seria enfeite vazio.
  Trazer esses campos é um briefing de captura de dados primeiro (mapear no
  provider TMDB), não um briefing de layout.
- **Por que o "Excluir coleção" precisa de `AlertDialog` em vez do toggle
  inline de hoje**: um item de dropdown fecha o menu ao ser clicado (Radix,
  comportamento padrão e correto de acessibilidade) — a confirmação
  "2 cliques com o botão trocando de texto" que existe hoje dentro da MESMA
  área não cabe mais dentro do menu; um modal separado é o padrão
  estabelecido para confirmação destrutiva pós-fechamento de menu.
- **Por que fanart.tv/IMVDb ficam fora da fileira de links**: essas fontes
  (0048/`fanarttv-arte-provider`) não têm uma "página do item" pública que
  faça sentido linkar aqui (são fontes de ARTE/tema, consumidas via
  download, não navegação) — diferente de TMDB/IMDb/TVDB, que são páginas de
  conteúdo que o dono efetivamente quer visitar.

## 7. Fora deste briefing / follow-ups

- Sidebar de metadados (budget/revenue/status/datas/streaming) — precisa de
  captura de dados nova no provider TMDB primeiro; briefing próprio se
  houver demanda.
- Provider dedicado de ratings (ex.: OMDB para IMDb/Rotten Tomatoes reais)
  — a fileira de notas já está pronta para recebê-lo sem mudança de tela.
- Logos de mais provedores na fileira de links (fanart.tv, IMVDb, Trakt
  etc.) — `PROVIDER_META` é uma tabela extensível; adicionar é trivial
  quando/se fizer sentido.
