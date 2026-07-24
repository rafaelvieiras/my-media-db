# 0082 — Hero imersivo (backdrop full-bleed, logo/banner/disco, trailer e tema musical)

> O dono viu o hero do 0079 e achou que ficou compacto demais — o pedido real
> era um hero MAIOR e IMERSIVO, no estilo do próprio detalhe do TMDB (pôster +
> sinopse dentro do card) combinado com o hero do Jellyfin (backdrop full-
> bleed com degradê escuro por trás, LOGO da obra sobreposto). Este briefing
> substitui a disposição do 0079 por uma versão maior: backdrop ocupando a
> largura toda da tela com overlay escurecendo, pôster maior, sinopse DENTRO
> do hero, barra de ações ACIMA do hero, e traz três elementos que hoje não
> aparecem nele: logo, banner e disco (arte), a opção de assistir ao trailer,
> e um player do tema musical (quando o dono tiver um `theme.mp3` na pasta).

| | |
|---|---|
| **ID** | 0082 |
| **Tipo** | feature (substitui o layout do 0079) |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | ready |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [09-nfo-e-export](../docs/09-nfo-e-export.md) |
| **Depende de** | 0079, 0048, 0049, 0062 |

---

## 1. Objetivo

O dono anexou duas referências: o hero do TMDB ("O Gato Félix") e a tela de
detalhe do Jellyfin ("The Lawnmower Man"). O pedido, com as próprias
palavras do dono:

> "Ele deve pegar a tela de fora a fora, o backdrop deve ficar no fundo com
> um overlay escurecendo ele, a sinopse deve ir para o hero também e a capa
> deve ficar um pouco maior, deixando o hero (...) em torno de 40% da tela.
> Os botões de ação podem ficar em uma barra acima do hero (...). Traga para
> o hero o logo, banner e o disco (com uma pequena animação). Traga também a
> opção de ver o trailer e (...) para quem tem o theme.mp3, traga um player
> do tema."

Hoje (pós-0079) o hero é um card com cantos arredondados, backdrop 16:9
FINO no topo do card (não full-bleed), pôster 2:3 de largura fixa (`w-32
sm:w-40`) ao lado do texto, sinopse como seção SEPARADA logo abaixo do card
(heading "Sinopse" fora do hero), e ações dentro do bloco de texto
(`rich-detail-view.tsx:1087`, logo depois do slogan). Não existe logo, banner,
disco ou player de tema em lugar nenhum da tela hoje.

## 2. O que já existe no backend e pode ser reaproveitado (sem mudar contrato)

Investigação confirmou que boa parte do pedido NÃO exige fundação nova:

- **Logo, banner e disco já são `kind`s de asset válidos** desde o 0048
  (`kindsValidos` em `internal/server/artwork_select.go:96`: `poster, fanart,
  still, logo, clearart, disc, banner, thumb, characterart`) e já têm um
  mecanismo de "preferida" por `(entity, kind)` (`marcarPreferida`/
  `metaPreferred`, `internal/server/assets.go`). O dono já consegue, hoje,
  selecionar uma logo/banner/disco pela galeria de arte (0049) e marcá-la
  como preferida — só não há NENHUM lugar na tela que a exiba.
- **`GET /api/v1/entities/{id}/assets` já devolve `{id, kind, origin,
  preferred, url}` por asset** (`internal/server/assets.go:58-66`,
  `listAssets`). O frontend já tem um cliente para essa rota (usado por
  `art-gallery.tsx`). O hero pode simplesmente pedir essa lista e filtrar
  `kind === 'logo' | 'banner' | 'disc' && preferred` — **sem precisar de
  nenhum campo novo no DTO de entity/external detail**.
- **Trailer**: `ExternalVideo` já tem `type` (`"Trailer"`, etc., convenção
  TMDB) e `site`/`key` para embutir via YouTube (`ehYouTube`,
  `rich-detail-view.tsx:233`). O hero só precisa achar o primeiro vídeo com
  `type === 'Trailer'` e abrir o MESMO player que a `VideosRail` já usa
  (`VideoCard`) — sem exigir dado novo do backend.

Ou seja: reposicionar backdrop/sinopse/ações/pôster, e exibir logo/banner/
disco/trailer no hero são **mudanças de FRONTEND** (mais, no máximo, a nova
rota de assets já existente sendo chamada de um lugar novo).

## 3. O que exige fundação nova: tema musical (theme.mp3)

O briefing 0048 já havia identificado isso e propositalmente adiou:
"Trailer e tema musical (...) binário de vídeo/áudio precisa de modelagem
própria (asset `kind=trailer/theme`)" — **não existe hoje nenhum conceito de
tema musical no schema, em disco ou servido por rota alguma.**

Diferente de logo/banner/disco (baixados de um provider real, fanart.tv),
**não há fonte de provider confiável e já integrada para tema musical** —
fanart.tv não tem endpoint de áudio. Seguindo o princípio de
[[independencia-de-provider]] e o padrão já usado no épico de mídias locais
(0062-0064, "pasta real, one-way"), a fundação proposta aqui é **local-only**:
o dono (ou um script dele) coloca um `theme.mp3` na pasta do item (raiz da
série ou pasta do filme, mesmo critério de `DiretorioDestino` já usado pelo
job de mídias faltantes/0081); o backend DETECTA esse arquivo (sem baixar de
lugar nenhum) e serve o binário; o frontend renderiza um player `<audio>`
compacto quando existir.

Isso é NOVO trabalho de backend (detecção + rota de serving), mas pequeno e
sem ambiguidade de fonte externa.

## 4. Escopo

**Dentro:**

### 4.1 Layout do hero (frontend, reaproveitando dados existentes)
- Backdrop **full-bleed**: ocupa a largura TOTAL do container da tela (não
  mais só a largura do card), com um overlay/gradiente escurecendo por trás
  do conteúdo (texto sempre legível em cima, tema claro e escuro — critério
  de contraste AA já usado no 0079 continua valendo, agora sobre o overlay
  em vez do `bg-card` sólido).
- Hero ocupando por volta de **40% da altura da viewport** (`min-h-[40vh]`
  ou equivalente responsivo) — não é mais um card de altura livre baseada no
  conteúdo; em telas pequenas, altura mínima pode reduzir (decisão de
  implementação, sem quebrar em mobile).
- **Pôster maior** dentro do hero (aumentar a largura fixa atual, ex. de
  `w-32 sm:w-40` para algo como `w-40 sm:w-56`, decisão de implementação —
  sem virar responsivo a ponto de dominar telas pequenas).
- **Sinopse move para DENTRO do hero** (abaixo do slogan, sobre o overlay) —
  a seção `<h2>Sinopse</h2>` separada logo após o card (linhas ~1093-1098)
  deixa de existir como bloco à parte; o texto da sinopse passa a ser parte
  do conteúdo do hero. Mantém heading "Sinopse" OU vira só um parágrafo
  proeminente sem heading (decisão de implementação — se remover o heading,
  ajustar o teste e2e `rich-detail.spec.ts` que hoje verifica
  `getByRole('heading', {name: 'Sinopse'})`).
- **Barra de ações ACIMA do hero** (não mais dentro do bloco de texto) — os
  MESMOS botões de hoje (`EntityActionButtons`/`actions` prop), só
  reposicionados para uma barra no topo da tela, antes do backdrop.
- Episódio (still no lugar do backdrop) e faixa de status do pôster
  (`FaixaStatusPoster`, 0079) continuam existindo, adaptados ao novo
  tamanho — sem regressão nos tipos que não têm backdrop/pôster (episódio
  sem pôster já é tratado hoje).

### 4.2 Logo, banner e disco (frontend consome rota já existente)
- **Logo**: sobreposta ao backdrop (canto ou centro-esquerda, sobre o
  overlay, no estilo Jellyfin) — quando existir asset `kind=logo` preferido;
  sem ela, cai no título em texto (comportamento atual, sem regressão).
- **Banner**: exibido como uma faixa adicional (posição a critério da
  implementação — ex. acima do título, ou substituindo o uso do backdrop
  quando não houver backdrop mas houver banner) — só quando existir asset
  `kind=banner` preferido; sem gating novo por tipo além de "existe ou não".
- **Disco (`kind=disc`)**: elemento decorativo pequeno (ex. próximo ao
  pôster ou na barra de ações) com uma animação sutil de rotação contínua
  lenta (CSS puro, `animate-spin` customizado com duração longa — ex.
  8-12s/volta — sem trava de acessibilidade: respeitar
  `prefers-reduced-motion` desligando a rotação). Só aparece quando existir
  asset `kind=disc` preferido.
- Todos os três buscam via a MESMA chamada a `GET .../assets` que a galeria
  já usa — uma função utilitária pode extrair `{logo, banner, disc}`
  preferidos da lista.
- Só a tela da ENTITY (`/entities/$id`) tem acesso a `/assets` hoje (é uma
  rota autenticada por id interno) — `/external/...` (antes de importar) NÃO
  tem assets próprios (nada foi baixado ainda), então logo/banner/disco só
  aparecem no hero da entity, não no externo. Isso é esperado e não é uma
  regressão de paridade (equivalente ao gating de `availableArtwork`, que já
  só a entity recebe hoje).

### 4.3 Trailer (frontend reaproveita `videos` já carregado)
- Um botão "Assistir trailer" na barra de ações (4.1) — visível quando
  `detail.videos` tiver ao menos um vídeo com `type === 'Trailer'`
  reproduzível (`ehYouTube`). Ao clicar, abre o MESMO player embutido que a
  `VideosRail`/`VideoCard` já usa hoje (reaproveitar o componente, não
  duplicar). Sem vídeo do tipo Trailer, o botão não aparece (sem fallback
  para Teaser/Clip — só trailer de verdade).
- A `VideosRail` abaixo do hero continua existindo (lista completa de
  vídeos) — o botão do hero é um atalho para o mais relevante, não uma
  substituição.

### 4.4 Tema musical — fundação nova (backend + frontend)
- **Detecção**: reaproveitar a MESMA lógica de resolução de diretório-raiz do
  0081 (`DiretorioDestino`, `internal/localmedia/naming.go`) para achar a
  pasta-raiz do item (filme ou série) e checar se existe um `theme.mp3` (ou
  `theme.<ext>` — decisão de implementação sobre extensões aceitas: pelo
  menos mp3; opcionalmente ogg/flac) na raiz. Sem o arquivo, nenhuma
  informação nova aparece (degradação graciosa, sem erro).
- **Rota de serving**: um novo endpoint (ex. `GET
  /api/v1/entities/{id}/theme`) que serve o binário do `theme.mp3`
  encontrado — mesmo padrão de segurança já usado para proxy de arquivos
  locais (sem aceitar path arbitrário do cliente; o caminho vem SÓ da
  resolução server-side). 404 quando não existe.
- **Frontend**: um player `<audio>` compacto no hero (ex. ícone de
  nota musical com play/pause) quando a rota devolver 200 — sem autoplay
  (navegadores bloqueiam áudio autoplay com som; então é sempre uma ação do
  usuário), com loop ligado enquanto tocando.
- Isso é a fundação MÍNIMA — não inclui download automático de tema musical
  de nenhum provider (não há fonte confiável integrada), nem edição/upload
  de tema pela UI (o dono coloca o arquivo manualmente, como o Kodi já
  espera hoje).

**Fora (explicitamente):**
- Download automático de tema musical de qualquer provider (sem fonte
  confiável integrada — só detecção de arquivo local).
- Upload de tema musical pela UI (fica para outro briefing, se o dono
  quiser).
- Auto-seleção de logo/banner/disco sem a ação do dono — continua
  dependendo do fluxo de seleção já existente (0048/0049); este briefing só
  EXIBE o que já está marcado como preferido.
- Qualquer mudança na galeria de arte (`art-gallery.tsx`) em si — só uma
  nova função utilitária/hook que ela (ou o hero) pode compartilhar para ler
  os preferidos por kind.
- Mudar `/external/...` para ganhar logo/banner/disco antes de importar (sem
  assets próprios antes da importação — ver 4.2).

## 5. Contrato técnico

### Backend (Go)
- Novo endpoint `GET /api/v1/entities/{id}/theme`: resolve a pasta-raiz do
  item (reusar `DiretorioDestino`/equivalente do 0081), procura
  `theme.{mp3,ogg,flac}` (lista de extensões aceitas configurável no código,
  não exposta como opção), serve o binário com `Content-Type` correto; 404
  se ausente ou se o item não tiver arquivo local associado (mesma condição
  de "sem storage local" que outras rotas de mídia local já tratam); 503 se
  assets/storage local indisponível (mesmo padrão das demais rotas de
  mídia).
- Sem migration nova — não há coluna/tabela de tema (é detecção pura por
  presença de arquivo, análogo ao `still` custom em disco, sem materializar
  asset).

### Frontend (Vite/React)
- `web/src/components/rich-detail-view.tsx`: reestruturação do hero
  (backdrop full-bleed + overlay, pôster maior, sinopse interna, barra de
  ações movida para cima do hero).
- Novo componente/hook para ler `{logo, banner, disc}` preferidos via
  `GET .../assets` (reaproveitar cliente já usado por `art-gallery.tsx`,
  `web/src/lib/api.ts`).
- Novo componente de disco com animação de rotação (CSS, respeita
  `prefers-reduced-motion`).
- Botão de trailer no hero, reaproveitando `VideoCard`/lógica de
  `ehYouTube` já existente.
- Novo componente de player de tema (`<audio>`), consumindo a nova rota
  `.../theme` (chamada condicional — se 404, não renderiza nada; sem
  polling, uma checagem por carga da tela).
- Ajustar `rich-detail.spec.ts` e demais testes que hoje esperam a sinopse
  como seção separada após o card do hero.

### Arquivos afetados
- `internal/server/` (novo handler de tema, registro de rota).
- `internal/localmedia/naming.go` (reaproveitar resolução de raiz; sem
  mudança de assinatura esperada, só uso).
- `web/src/components/rich-detail-view.tsx` (+ `.test.tsx`).
- `web/src/lib/api.ts` (+ `.test.ts`, se precisar de uma função nova para
  a rota de tema ou para extrair preferidos por kind).
- `e2e/tests/rich-detail.spec.ts` (ajuste de asserts pela nova disposição).
- `openapi.yaml` (nova rota `.../theme`).

## 6. Critérios de aceite

- [ ] O backdrop ocupa a largura total da tela (full-bleed), com overlay
      escurecendo o suficiente para o texto continuar legível (AA) em claro
      e escuro.
- [ ] A sinopse aparece DENTRO do hero (não mais como seção separada abaixo).
- [ ] O pôster é visivelmente maior que antes do 0079/0082.
- [ ] A barra de ações aparece ACIMA do hero (mesmos botões de hoje).
- [ ] Com asset `kind=logo` preferido, a logo aparece sobreposta ao
      backdrop; sem ela, o título em texto continua aparecendo normalmente.
- [ ] Com asset `kind=banner` preferido, o banner aparece; sem ele, nenhuma
      regressão.
- [ ] Com asset `kind=disc` preferido, o disco aparece com animação de
      rotação contínua (desligada quando `prefers-reduced-motion: reduce`).
- [ ] Com um vídeo `type === 'Trailer'` reproduzível, um botão de trailer
      aparece na barra de ações e abre o player; sem trailer, o botão não
      aparece.
- [ ] Com um `theme.mp3` (ou extensão aceita) na pasta-raiz do item, um
      player de áudio aparece no hero e toca sob ação do usuário (sem
      autoplay); sem o arquivo, nada aparece (sem erro, sem 500).
- [ ] `/external/...` continua funcionando sem logo/banner/disco/tema (sem
      assets/arquivo local antes de importar) — sem regressão, sem crash.
- [ ] `make e2e-cov` e `make gate-local` passam.

## 7. Notas de implementação / decisões

- Este briefing SUBSTITUI a disposição visual do 0079 (que já foi ao ar) —
  não é um "0079 v2" incremental pequeno, é uma reestruturação relevante do
  mesmo componente compartilhado (`RichDetailView`), então vale tratar como
  uma reescrita da seção do hero, não um patch.
- A decisão de tema musical ser **local-only** (sem provider) é para não
  inventar uma integração externa sem fonte confiável — se o dono quiser um
  provider de tema no futuro, é outro briefing (fundação de download,
  semelhante ao 0048 para logo/banner/disco).
- `SelosArtes`/`availableArtwork` (0049, contagem de artes por kind) segue
  existindo — o disco/logo/banner exibidos aqui são a arte JÁ selecionada
  como preferida, não uma lista nova.
