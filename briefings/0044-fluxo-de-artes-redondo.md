# 0044 — Fluxo de artes redondo (pôster/fanart/still: exibição + gestão)

> Garante que todo tipo (série, temporada, episódio, filme) tenha a arte que lhe
> cabe, **exibida corretamente** e **gerenciável** (adicionar/preferir/deletar).
> Corrige a fanart exibida deformada, dá ao episódio uma área de gestão da sua
> arte (o still), passa toda arte pelo proxy e faz a temporada herdar a fanart da
> série quando não tem a sua.

| | |
|---|---|
| **ID** | 0044 |
| **Tipo** | fix/feature |
| **Status** | done |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (arte) |
| **Docs relacionadas** | [0015](0015-enriquecimento-import-proxy-imagem.md), [0017](0017-arte-custom-upload-ui.md), [0031](0031-series-fundacao-modelo-enriquecimento.md), [0042](0042-still-de-episodio-lazy-proxy.md), [0043](0043-re-enrich-atualiza-arvore-serie.md) |
| **Depende de** | 0017, 0042 |

---

## 1. Objetivo

O fluxo de artes não está redondo. Três problemas concretos, evidenciados no
código:

1. **Fanart exibida errada.** Na galeria de arte (`art-gallery.tsx`) toda miniatura
   é renderizada com `aspect-[2/3]` fixo — o pôster fica certo, mas a **fanart
   (16:9) é deformada/recortada** no frame retrato. No hero do detalhe, o `<img>`
   do backdrop não tem `onError`/placeholder (some silenciosamente se a URL falha),
   e o backdrop de fallback (sem asset) usa a **URL crua da fonte, sem passar pelo
   proxy** — bate direto no CDN (acoplamento que o 0015/0022 removeram para pôster).

2. **Episódio sem gestão de arte.** A `ArtGallery` até aparece no detalhe do
   episódio, mas é inócua: a arte do episódio é o **still**, que vive em
   `extension_data core.tv@1.still_url` (não é asset), e os kinds de upload são só
   `poster`/`fanart` (sem `still`). Resultado: a galeria mostra "nenhuma arte", e
   um upload de poster/fanart no episódio fica **invisível** (o hero do episódio só
   renderiza o `StillFrame`). Não há como adicionar/trocar/deletar o still.

3. **Lacunas de população.** A **temporada nunca tem fanart** (nenhum caminho
   baixa/expõe fundo de temporada). Isso deixa temporadas visualmente pobres,
   embora "caiba" fanart nelas.

Alinhado ao intuito de gestor de arquivos/metadados tipo tinyMediaManager
(memória de escopo): a arte é cidadã de primeira classe, exibida e gerenciável em
todo tipo onde faz sentido.

## 2. Jornada do usuário

1. **Filme/série** — no detalhe, pôster (2:3) e fanart (16:9) aparecem corretos; a
   galeria mostra cada arte com o enquadramento certo (pôster retrato, fanart
   paisagem). O dono pode enviar/preferir/remover a sua arte custom.
2. **Temporada** — mostra o seu pôster; se não tem fanart própria, exibe a fanart
   da **série pai** (herança), em vez de fundo vazio.
3. **Episódio** — o detalhe mostra o still (16:9) grande e o card mostra o
   thumbnail (já do 0042). Agora há uma **área de gestão do still**: o dono vê o
   still atual (do provider), pode **enviar um still custom** (que passa a vencer) e
   **removê-lo** (voltando ao do provider). O still do provider não é deletável (é a
   fonte), como os assets `external` de pôster/fanart.
4. **Toda arte** é servida pelo **proxy de imagem** (nada bate direto no CDN); se
   uma imagem falha, cai no placeholder (nunca some silenciosamente).

## 3. Escopo

**Dentro:**

- **A. Exibição correta (frontend):**
  - Galeria: enquadramento por `kind` — `poster` → `aspect-[2/3]`; `fanart` e
    `still` → `aspect-video` (16:9). Rótulo do kind em cada miniatura.
  - Hero: `<img>` do backdrop ganha `onError` → placeholder e `loading="lazy"`
    (paridade com o pôster).
  - Helper único `arteSrc(url)` no front: URL absoluta (`^https?://`) → passa pelo
    **image proxy**; rota local → prefixa a base. Aplicar a `posterUrl`,
    `backdropUrl` e `stillUrl` — fecha o buraco do backdrop cru não-proxyado e
    suporta still local (custom) vs still cru (provider).

- **B. Episódio: still gerenciável (backend + frontend):**
  - Backend: aceitar o kind **`still`** no upload (`parseKind`, `assets.go`), com as
    mesmas validações (magic bytes, 20 MiB, origin=`user`). Rota de servir
    `GET /entities/{id}/assets/still` (kind=still, via `escolherAssetServido`).
  - Resolver: `StillURL` do episódio passa a ter **precedência** — still `user`
    (rota local `/assets/still`) **vence** o `still_url` do provider
    (`extension_data`, cru→proxyado). Sem asset custom, mantém o do provider (0042).
  - Frontend `ArtGallery`: kinds por tipo — episódio → `['still']`; demais →
    `['poster','fanart']`. Para episódio, renderizar o **still atual do provider**
    (recebido por prop a partir do detalhe) como card "provider" (não deletável)
    quando não há still `user`; permitir enviar/remover o still custom.

- **C. Temporada herda fanart da série (backend, display):**
  - Ao resolver/anotar uma **temporada** sem fanart própria (sem asset `fanart`
    servível), preencher `BackdropURL` com a **fanart da série pai** (rota
    `/entities/{serieId}/assets/fanart`) quando a série tem uma. Pós-processo no
    server (como a anotação de still/numbering), fora do core do resolver, cuidando
    do gocyclo (≤10). Sem baixar nada novo — reusa o asset já baixado da série.

**Fora (explicitamente):**

- **Baixar fanart de temporada do provider** — nenhum provider entrega; a herança
  da série cobre o display. (Se um dia houver fonte de fanart de temporada, é outro
  briefing.)
- **Fanart de série a partir do TVDB** (o TVDB não emite backdrop no detalhe atual;
  buscar artwork de fundo na API v4 do TVDB) — **follow-up** (§7), fora daqui para
  não abrir buraco de provider. Séries via TMDB já ganham fanart.
- **Baixar o still no import** — continua lazy via proxy (decisão do 0042); só o
  still **custom** (upload do dono) vira arquivo local.
- **Novos kinds** (logo/banner/clearart/disc) — fora; só `poster`/`fanart`/`still`.
- **Materializar o still do provider como asset `external`** — mantido como URL em
  `extension_data` (lazy); o still custom é o único still que vira asset.
- **Card com fanart** — cards seguem com pôster/still; sem fundo na grade.

## 4. Contrato técnico

### Dados / modelo
- Sem migration. Tabela `asset` inalterada (o still custom é `kind='still'`,
  `origin='user'`, arquivo baixado como poster/fanart do dono).

### API
- `POST /entities/{id}/assets` passa a aceitar `kind=still` (além de
  poster/fanart). Demais rotas de asset (`GET /assets`, `.../prefer`, `DELETE`)
  inalteradas — já são genéricas por entity id.
- `GET /entities/{id}/assets/still` — servir o still custom (novo, espelha
  `getEntityPoster`/`getEntityFanart`).
- `ResolvedEntity.still_url` passa a poder ser **rota local** (quando há still
  custom) **ou** URL crua do provider (quando não há). O front decide proxy vs base
  pelo `arteSrc`.

### Backend (Go)
- `internal/server/assets.go`: `parseKind` aceita `still`; constante `kindStill`.
- `internal/server/entities.go`: rota/handler `getEntityStill` (kind=still);
  `StillURL` no resolve/anotação passa a preferir o asset `still` servível antes do
  `extension_data`; anotação de **fanart herdada** para temporada (lookup do pai via
  relação existente — `ListarRelacoesPais`/equivalente — e checagem de asset fanart
  servível da série). Extrair helpers para não estourar gocyclo.
- `internal/resolver/resolver.go`: se a precedência do still ficar melhor no
  resolver, ajustar lá; senão manter a anotação no server (escolha da implementação,
  mantendo o resolver puro). Sem tocar `media_override`/`status`/`tags`.
- `internal/server/image_proxy.go`: allowlist já cobre `image.tmdb.org` e
  `artworks.thetvdb.com` — confirmar; nada novo esperado.

### Frontend (Vite/React)
- `web/src/lib/api.ts`: helper `arteSrc(url)` (proxy p/ absoluta, base p/ local);
  `KINDS_ARTE` por tipo (episódio → `['still']`); tipar `still_url` como já está.
- `web/src/lib/rich-detail.ts`: `posterUrl`/`backdropUrl`/`stillUrl` via `arteSrc`.
- `web/src/components/rich-detail-view.tsx`: `onError`/placeholder + `loading` no
  `<img>` do backdrop.
- `web/src/components/art-gallery.tsx`: aspect por kind; kinds por tipo; card do
  still do provider (prop) para episódio; rótulo do kind.
- `web/src/routes/entity-detail.tsx`: passar `type` e `stillUrl` do detalhe para a
  `ArtGallery`.

### OpenAPI
- `still_url` já existe em `ResolvedEntity`/`EntityCard` (0042). Documentar que pode
  ser rota local. Sem novo campo. Rodar `gen:types` se algo mudar; gerado já
  formatado; `tsc -b` zero.

### Arquivos afetados (previsão)
- Go: `internal/server/assets.go`, `internal/server/entities.go`,
  `internal/server/server.go` (rota still), possivelmente
  `internal/resolver/resolver.go`, testes (`assets_test.go`, `entities_test.go`).
- Web: `web/src/lib/api.ts`, `web/src/lib/rich-detail.ts`,
  `web/src/components/art-gallery.tsx`, `web/src/components/rich-detail-view.tsx`,
  `web/src/routes/entity-detail.tsx`, testes web. **Sem migration.**

## 5. Critérios de aceite

- [x] Galeria: pôster em 2:3, fanart e still em 16:9 — sem deformar. Rótulo do kind
      visível.
- [x] Hero: se a URL do backdrop falha, cai no placeholder (não some). Backdrop
      passa pelo proxy (não bate direto no CDN).
- [x] Episódio: a galeria mostra o still atual; enviar um still custom passa a
      vencer na exibição (card + detalhe); remover volta ao still do provider; o
      still do provider não é deletável.
- [x] Upload rejeita kind fora de {poster, fanart, still} (422).
- [x] Temporada sem fanart própria exibe a fanart da série pai; temporada com
      fanart própria usa a sua.
- [x] Filme/série: pôster e fanart continuam corretos (sem regressão).
- [x] Catraca completa verde (Go cov, e2e, tsc 0, web cov, jscpd, gocyclo ≤10).
- [x] Smoke real: still de episódio (upload custom vence → `/assets/still` 200 →
      delete reverte ao provider); kind inválido 422; temporada herda a fanart da
      série pai (rota `/entities/{serie}/assets/fanart`) e reverte quando removida.

> **Nota (população de fanart de série):** como a série vem só do TVDB (sem
> backdrop), a fanart de série/temporada só é *populada* quando o **0045** traz o
> backdrop do TMDB para o nó série. O 0044 deixa a exibição/gestão/herança prontas;
> o dado chega no 0045. Fanart de **filme** (TMDB) já popula e agora exibe correta.

## 6. Notas de implementação / decisões

- **`arteSrc` unificado** (proxy p/ absoluta, base p/ local) elimina três
  tratamentos divergentes de URL de imagem (poster local, backdrop cru/local, still
  cru) e fecha o backdrop não-proxyado.
- **Still custom como asset `kind='still'`**, mantendo o still do provider lazy em
  `extension_data` (0042). Precedência `user still` ⟶ `provider still` espelha a
  precedência de pôster/fanart (preferida→user→external). O dono nunca perde a
  edição num re-import (0043 não toca asset `user` nem `still_url` além de refresh).
- **Herança de fanart de temporada** é decisão de **display** (como o TMM, que
  herda a fanart do show): não inventa dado, reusa o asset da série. Feita no
  server (anotação), fora do resolver puro.
- **Sem materializar still do provider como asset**: manter o modelo lazy do 0042 —
  só o still custom vira arquivo. A galeria mostra o still do provider por prop
  (URL do detalhe), não por `GET /assets`.

## 7. Fora deste briefing / follow-ups

- **Fanart de série via TVDB** (buscar artwork de fundo na API v4 do TVDB para
  séries sem TMDB) — hoje só TMDB dá backdrop. Merece briefing próprio de provider.
- **Fanart de temporada real** (fonte de fundo específico de temporada) — hoje
  herda da série.
- **Novos kinds de arte** (logo/banner/clearart/disc/thumb de coleção) rumo à
  paridade TMM plena.
- **Still via FFmpeg do arquivo local** quando o provider não tem thumb (follow-up
  do 0042).
- **Variante escalada (thumbnail) no image proxy** para os cards da lista
  (follow-up do 0042).
