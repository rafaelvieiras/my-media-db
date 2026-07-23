# 0072 — Galeria de artes: de-duplicar inventário materializado + lightbox reancorado por asset

> Uma arte do fanart.tv baixada pro cache não deve continuar aparecendo como um
> segundo item na galeria, e o lightbox não deve trocar de imagem sozinho depois
> de uma ação sobre o item que está aberto.

| | |
|---|---|
| **ID** | 0072 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | ready |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0048, 0049, 0062 |

---

## 1. Objetivo

Uma auditoria de UX (navegação real pela tela de detalhe, não só leitura de
código) encontrou dois bugs concretos na galeria de artes (`ArtGallery` +
`ArtLightbox`), confirmados via API e não só por observação visual:

1. **Duplicação visual.** Ao "baixar pro cache" um candidato do inventário da
   fanart.tv (`POST /artwork/select`), a mesma imagem passa a existir **duas
   vezes** na resposta de `GET /entities/{id}/artwork`: uma vez como candidato
   de inventário (`source: "fanarttv"`, `is_local: false`) e outra como o asset
   recém-materializado (`source: "external"`, `is_local: true`). O backend já
   guarda a URL de origem de todo asset materializado (`meta_json.source_url`,
   ver `metaComSourceURL`/`metaSourceURL` em `artwork_select.go`) — só não usa
   essa informação para não repetir a mesma arte na montagem da resposta.
2. **Índice do lightbox não acompanha o item.** Depois de qualquer mutação que
   reordena a lista (ex.: "Definir como principal" sobe o item ao topo), o
   lightbox continua mostrando o índice numérico antigo — que agora aponta para
   OUTRO item, sem nenhum aviso. Testado ao vivo: enviar uma arte própria,
   torná-la principal, e ver o lightbox (ainda na mesma legenda "Pôster 2 de
   2") trocar silenciosamente para uma imagem diferente. Com uma galeria de
   dezenas de pôsteres, um "Remover" na sequência apagaria a arte errada.

Um terceiro achado, menor, mora nos mesmos arquivos: os botões desabilitados
("Definir como principal"/"Salvar na pasta" quando o item não tem arquivo na
biblioteca) explicam o motivo só via atributo `title` nativo do HTML — sem
tooltip do design system, sem suporte a toque. Como já está no mesmo
componente (`art-lightbox.tsx`) e é uma troca pequena, entra aqui para não
abrir um briefing só para isso.

## 2. Jornada do usuário

1. O dono abre a galeria de um filme com dezenas de pôsteres candidatos do
   fanart.tv (ex.: 32 no achado original).
2. Abre um candidato no lightbox e clica "Baixar pro cache". **Hoje**: a
   galeria ganha 33 itens (a mesma arte duas vezes). **Depois deste
   briefing**: a galeria continua com 32 — o candidato vira a versão
   materializada NO MESMO lugar da lista.
3. Com a galeria aberta no lightbox num item qualquer, o dono clica "Definir
   como principal" (ou qualquer outra ação que reordene a lista: baixar,
   remover). **Hoje**: o lightbox pode passar a mostrar outra arte, com a
   mesma legenda de posição. **Depois**: o lightbox continua mostrando A
   MESMA arte que estava aberta (agora talvez em outra posição/legenda), ou
   fecha explicitamente se o item sumiu (removido).
4. Em um item só de catálogo (sem arquivo), o dono passa o mouse sobre
   "Definir como principal" (desabilitado). **Hoje**: só aparece dica após
   1-2s de hover parado (tooltip nativo do navegador), nada em toque.
   **Depois**: usa o `Tooltip` do design system (shadcn), consistente com o
   resto do app e utilizável em touch.

## 3. Escopo

**Dentro:**
- Backend: `montarGruposArtwork` (`internal/server/artwork.go`) não repete uma
  arte do inventário da fanart.tv que já foi materializada como asset
  (`origin='external'`) do mesmo `kind` com o mesmo `source_url`.
- Frontend: `ArtGallery`/`ArtLightbox` reancoram o item aberto do lightbox por
  identidade estável (não índice numérico puro) após qualquer mutação que
  invalida `['artwork', id]`.
- Frontend: troca do `title` nativo pelo `Tooltip` do design system nos dois
  botões desabilitados de `art-lightbox.tsx`.

**Fora (explicitamente):**
- Qualquer mudança na ordenação da galeria (`ordenarGruposArtwork` — `is_principal`
  desc, `is_local_in_library` desc — continua igual).
- Drag-and-drop ou reordenação manual de artes (não pedido, não é o problema
  aqui).
- Mudanças no fluxo de upload custom ou no `SelectArtworkRequest` em si (o
  contrato do endpoint não muda, só a montagem de leitura).

## 4. Contrato técnico

### Backend (Go)

- `internal/server/artwork.go`:
  - `coletarArtesLocais` já devolve `kindsExternos map[string]bool` (kinds que
    têm um asset `origin='external'` local). Trocar por (ou adicionar) um
    `map[string]map[string]bool` — `kind → conjunto de source_url já
    materializadas` — lendo `metaSourceURL(a.MetaJson)` para cada asset
    `origin='external'` (a função já existe em `artwork_select.go`, mesmo
    pacote `server`).
  - `coletarInventarioFanart`: antes de `append`, pular a arte (`a.URL`) cujo
    valor já está no conjunto de `source_url` materializadas daquele `kind`
    (mesma comparação usada pela idempotência de `arteExternaPorURL`, aqui só
    de LEITURA, sem tocar o banco de novo — reusa o resultado já obtido por
    `coletarArtesLocais`).
  - Sem migration, sem mudança de schema — o dado (`meta_json.source_url`) já
    existe desde o 0048.

### Frontend (Vite/React)

- `web/src/components/art-gallery.tsx`:
  - O estado `aberto: {kind, index} | null` passa a rastrear também uma
    identidade estável do item aberto — `asset_id` quando existir, senão
    `src_url` (candidato de inventário ainda não materializado tem `asset_id`
    ausente mas `src_url` preenchido; ver `ArtworkItem` no `openapi.yaml`).
  - Um `useEffect` (ou lógica equivalente no `onSuccess` das mutações
    existentes — `preferir`/`baixar`/`remover`, já centralizadas nesse
    arquivo) recalcula `aberto.index` procurando a NOVA posição do item com
    aquela identidade em `linhaAberta.items` depois que `['artwork', id]`
    revalida. Se o item não for mais encontrado (removido), fecha o lightbox
    (`setAberto(null)`) em vez de mostrar um índice arbitrário.
  - `garantirAssetId` (já existe, resolve `asset_id` de um item) pode ser
    reaproveitada ou vizinha de uma nova `identidadeItem(item): string` que
    devolve `item.asset_id ?? item.src_url ?? ''`.
- `web/src/components/art-lightbox.tsx`:
  - Trocar os dois `title={... ? undefined : TOOLTIP_SEM_PASTA}` (linhas ~286
    e ~299) pelo componente `Tooltip`/`TooltipTrigger`/`TooltipContent` do
    design system (`@/components/ui/tooltip`, já usado em outras telas do
    app) envolvendo o `Button` desabilitado. `TOOLTIP_SEM_PASTA` continua
    sendo o texto.

### Arquivos afetados

- `internal/server/artwork.go` (+ `internal/server/artwork_test.go` ou
  equivalente já existente).
- `web/src/components/art-gallery.tsx` (+ `.test.tsx`).
- `web/src/components/art-lightbox.tsx` (+ `.test.tsx`).

## 5. Critérios de aceite

- [ ] Materializar (`POST /artwork/select`) um candidato do inventário
      fanart.tv não aumenta a contagem total de itens do `kind` na resposta de
      `GET /entities/{id}/artwork` — o candidato "vira" o asset, não soma mais
      um.
- [ ] Teste de backend cobrindo o caso: inventário com uma arte cujo `url`
      coincide com o `source_url` de um asset `origin='external'` já
      materializado daquele `kind` → aparece só uma vez (a versão local).
- [ ] Abrir o lightbox num item, disparar uma ação que reordena a lista
      (preferir/baixar/remover outro item, ou o próprio) mantém o lightbox
      mostrando O MESMO item (por identidade), não o que sobrou naquela
      posição numérica.
- [ ] Remover o item que está aberto no lightbox fecha o lightbox (não mostra
      um item arbitrário no lugar).
- [ ] Os botões desabilitados de "Definir como principal"/"Salvar na pasta"
      mostram a explicação via `Tooltip` do design system (acessível por
      teclado/toque), não mais `title` nativo.
- [ ] `make e2e-cov` e `make gate-local` passam.

## 6. Notas de implementação / decisões

- **Por que resolver o dedupe no backend, não escondendo no frontend?** O
  front já reflete fielmente o que a API devolve — filtrar duplicata no
  cliente seria tratar sintoma, e qualquer outro consumidor da API (outro
  cliente, um script) veria a mesma duplicação. O dado para de-duplicar
  (`source_url`) já existe e já é usado para outra finalidade (idempotência
  do próprio select) — é extensão natural, não feature nova.
- **Por que identidade por `asset_id ?? src_url` e não um id sintético
  próprio?** Ambos os campos já saem da API hoje (`ArtworkItem.asset_id` para
  materializado, `ArtworkItem.src_url` para candidato) — não precisa de
  contrato novo nem migração de schema para ter uma chave estável.
