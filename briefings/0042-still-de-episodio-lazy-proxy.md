# 0042 — Still de episódio (lazy via image proxy)

> O episódio ganha still (frame 16:9). No import só se grava a URL (barato); a imagem
> é buscada e cacheada **sob demanda** pelo image proxy quando o episódio é visto —
> zero download no import (uma série tem ~1195 episódios). Aparece no card da lista
> (thumbnail) e no detalhe (grande).

| | |
|---|---|
| **ID** | 0042 |
| **Tipo** | feature |
| **Status** | done |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Docs relacionadas** | [0015](0015-enriquecimento-import-proxy-imagem.md), [0031](0031-series-fundacao-modelo-enriquecimento.md), [0039](0039-numbering-real-no-card-do-episodio.md), [0041](0041-i18n-de-episodio-media-external.md) |
| **Depende de** | 0031, 0039 |

---

## 1. Objetivo

O still (frame do episódio, ~16:9) **não é baixado** no import — decisão da Fase 1
(`enrich/serie.go:203-207`: baixar um still por episódio síncrono é lento e hostil ao
CDN; Looney Tunes ~1195 eps). O provider já fornece a URL (`EpisodioExterno.StillURL`,
preenchida pelo TheTVDB em `tvdb_serie.go`), mas o enrich a descarta. Este briefing dá
ao episódio o still **sem custo de import**: grava só a **URL** e serve a imagem **sob
demanda** pelo **image proxy** (busca+cacheia por hash na primeira visualização, como já
faz para as telas de detalhe externo — briefing 0015). Só os stills realmente abertos
viram bytes em disco.

## 2. Jornada do usuário

1. Ao abrir uma **temporada** (ou uma ordem no seletor 0040), cada card de episódio
   mostra um **thumbnail** do still (quando o provider tem um); episódio sem still cai
   no placeholder atual ("Sem arte").
2. Ao abrir o **detalhe** do episódio, o still aparece **grande** no topo.
3. A primeira exibição de cada still dispara o proxy (busca+cacheia); as próximas vêm do
   cache. O import continua instantâneo — nada de still é baixado nele.

## 3. Escopo

**Dentro:**
- **Persistir a URL do still** por episódio no import, barato (string), sem baixar a
  imagem. Home natural: `extension_data` `core.tv@1.still_url` (o still é artwork
  **neutro de idioma**, como o `numbering` — não precisa de i18n nem de `media_external`).
  Parar de descartar `ep.StillURL` em `enrich/serie.go`.
- **Expor `still_url`** (a URL crua do provider) no **card de episódio** (`entityCardDTO`,
  no /members e no endpoint de episódios do 0040) e no **detalhe** do episódio
  (`ResolvedEntity`). Anotação no server a partir do `extension_data` (mesma fronteira do
  `numbering` 0039 — reusar `internal/tvext`), `omitempty`, só para episódio com still.
- **Servir via image proxy**: o frontend renderiza o still passando a URL crua pelo
  **proxy de imagem existente** (o mesmo que as telas externas usam — verificar o contrato
  em `internal/server/image_proxy.go` e o helper do front em `web/src/lib/api.ts`). O
  proxy busca+cacheia por hash na 1ª vez. **Confirmar** que o host do TheTVDB é aceito
  pelo allowlist do proxy; se houver allowlist e faltar o host, adicioná-lo.
- **Frontend**:
  - Card de episódio (`CardNumerado`/`PosterCard` no caminho de episódio): mostrar o
    still como thumbnail. O still é **paisagem 16:9** — divergente do frame de pôster
    2:3. Usar um **enquadramento adequado ao episódio** (thumb 16:9) em vez de forçar o
    still no frame 2:3 (evitar corte feio). Sem still → placeholder atual.
  - Detalhe do episódio: still grande no topo (16:9), com fallback ao placeholder.

**Fora (explicitamente):**
- **Baixar o still no import** (rejeitado: volume/CDN) — só a URL é persistida.
- **Novo asset kind `still`** / upload manual de still — não é necessário para o lazy via
  proxy; o still vem sempre do provider. (Se um dia se quiser still custom por upload, é
  outro briefing.)
- **Still por idioma** — o still é neutro de idioma; sem i18n.
- **Arte de temporada** — a temporada **já baixa poster** no import (feito na Fase 1);
  fora daqui.
- Escolher entre múltiplos stills / galeria — pega-se o still principal do provider.

## 4. Contrato técnico

### Backend (Go)
- **`internal/enrich/serie.go`** (`idDoEpisodio`/`extensaoEpisodio`): incluir
  `still_url` no `extension_data` `core.tv@1` do episódio quando `ep.StillURL != ""`.
  Idempotente no reuse (o namespace é reescrito no re-import; garantir que o still entra
  no mesmo envelope do `numbering`/`absolute_number`). Não baixar nada.
- **`internal/server/entities.go`** (+ `episodes.go` do 0040): estender o DTO de card de
  episódio e o detalhe com `StillURL *string json:"still_url,omitempty"`, anotado a
  partir do `core.tv@1.still_url` via `internal/tvext` (mesma anotação server-side do
  `numbering`; reusar o walk já existente onde possível para não multiplicar leituras).
- **Image proxy** (`internal/server/image_proxy.go`): confirmar o contrato (formato da
  URL do proxy, allowlist de host). Se houver allowlist e o host de artwork do TheTVDB
  não estiver nela, incluí-lo. Nenhum novo endpoint se o proxy genérico já serve.

### Frontend (Vite/React)
- **`web/src/lib/api.ts`**: tipar `still_url` no `EntityCard` e no detalhe (via
  `gen:types` do OpenAPI). Helper para montar a URL do proxy a partir da URL crua (reusar
  o helper existente de proxy de imagem; ver como as telas externas montam).
- **Card de episódio**: quando `type==='episode'` e há `still_url`, renderizar um
  **thumbnail 16:9** (via proxy) no lugar do frame de pôster; manter número (E05, 0039) e
  título. Sem still → placeholder atual. Escolher o caminho de menor refator (ex.: um
  slot de arte no `CardNumerado`/uma variante de card de episódio) sem quebrar os outros
  tipos.
- **Detalhe do episódio**: still grande 16:9 no topo (via proxy), com fallback.
- **Loading/erro de imagem**: `loading="lazy"`, `onError` → placeholder (como o
  `PosterCard` já faz).

### OpenAPI
- `openapi.yaml`: adicionar `still_url` ao `EntityCard` e ao `ResolvedEntity` (string
  URL, nullable/omitempty). Rodar `gen:types`; gerado já formatado (Prettier da raiz).
  `tsc -b` zero.

### Arquivos afetados (previsão)
- `internal/enrich/serie.go` (still_url no extension_data), `internal/server/entities.go`
  + `internal/server/episodes.go` (DTO + anotação), possível ajuste no
  `internal/server/image_proxy.go` (allowlist), `openapi.yaml` + tipos gerados,
  `web/src/lib/api.ts`, componentes de card/detale de episódio, testes (Go + web).
  **Sem migration.**

## 5. Critérios de aceite

- [ ] Importar/re-importar uma série grava `core.tv@1.still_url` nos episódios que têm
      still, **sem baixar imagem** (import continua rápido).
- [ ] `GET /entities/{id}/members` e `/episodes` trazem `still_url` no card de episódio;
      o detalhe do episódio também. Ausente para episódio sem still e para outros tipos.
- [ ] O still é servido pelo image proxy (busca+cacheia sob demanda); a 1ª visualização
      popula o cache, as próximas vêm dele. Nenhum still é baixado no import.
- [ ] Card de episódio mostra o thumbnail 16:9 (sem corte feio); detalhe mostra o still
      grande; sem still → placeholder. Número E05 (0039) intacto.
- [ ] Catraca completa verde (Go cov, e2e, tsc 0, web cov, jscpd, gocyclo ≤10).
- [ ] Smoke real: re-import de uma série; abrir uma temporada → thumbnails de still
      aparecem (proxy cacheia); abrir um episódio → still grande; episódio sem still →
      placeholder.

## 6. Notas de implementação / decisões

- **Lazy via proxy** (decisão do dono): import só grava a URL; a imagem é buscada e
  cacheada por hash na 1ª exibição, reusando o proxy do 0015. Custo de import ~zero,
  só baixa o que se vê — o oposto de baixar 1195 stills no import.
- **URL do still no `extension_data core.tv@1`** (neutro de idioma), ao lado do
  `numbering` — não em `media_external` (que é o eixo per-idioma do 0041) nem em asset
  (que é download). Anotado no server via `tvext`, como o `numbering` (0039).
- **Card + detalhe** (decisão do dono): thumbnail na lista e still grande no detalhe. O
  still é 16:9 — o card de episódio usa enquadramento próprio, não o frame 2:3 de pôster.
- **Sem novo asset kind**: o still vem sempre do provider e é servido pelo proxy; não há
  download nem upload custom aqui.

## 7. Fora deste briefing / follow-ups

- **Still via FFmpeg do arquivo local** quando o provider não tem thumb (gerar o frame a
  partir do vídeo já presente na biblioteca — o pipeline já tem ffprobe/ffmpeg). É o que
  o tinyMediaManager faz como fallback de thumb de episódio.
- **Variante escalada (thumbnail) no image proxy** para o card da lista: como o still
  aparece no card (N imagens ao abrir uma temporada), servir uma miniatura escalada por
  parâmetro de tamanho reduz banda/latência. Hoje o proxy cacheia full-res (o TMM mantém
  um `ImageCache` com versões escaladas via Scalr para a grade). **Deixado para depois**
  (não adicionar o parâmetro de tamanho ao proxy neste briefing).
- **Gestão do still em disco (como o TMM)**: o MyMediaDB pretende, na direção geral, ser
  um gestor de arquivos/metadados completo (baixar imagens, legendas, trailers e arte
  extra para o disco, à la tinyMediaManager). O lazy-via-proxy aqui é o **MVP do still**;
  a materialização do still como arquivo gerenciado (sidecar/cache em disco) entra quando
  a camada de gestão de arquivos chegar.
- **Re-enrich que ATUALIZA a árvore existente** (importante): hoje o re-import reusa o
  episódio existente sem reescrever o `extension_data` (mesmo comportamento do `numbering`
  0039), então séries **já importadas** só ganham `still_url` ao serem recriadas. Uma
  "re-varredura que atualiza metadados/arte" (still, numbering, título/sinopse, pôster —
  a árvore toda) é a peça que falta e casa com o intuito de gestor de arquivos tipo TMM
  (re-scrape atualiza). Merece briefing próprio (afeta filme e série).
- Still custom por upload (novo asset kind `still`) — se algum dia desejado.
- Galeria de stills / múltiplas imagens por episódio.
- Prefetch em background dos stills da temporada aberta (otimização de UX) — hoje é
  puramente sob demanda.
- **Fecha a Fase 3** do épico de séries (A ordenação + C i18n + B still).
