# 0103 — Música-tema automática via ThemerrDB (+ yt-dlp)

> Adiciona uma fonte automatizada (opcional, best-effort) para o `theme.mp3`
> que o briefing 0082 já serve — sem mudar o contrato do endpoint existente.

| | |
|---|---|
| **ID** | 0103 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Trailer e música-tema |
| **Status** | done |
| **Docs relacionadas** | [ADR-0025](../docs/adr/0025-trailer-tema-musical-ytdlp-themerrdb.md), briefing [0082](0082-hero-imersivo-estilo-jellyfin.md) (tema local-only original) |
| **Depende de** | 0101 |

---

## 1. Objetivo

Desde o briefing 0082, `GET /entities/{id}/theme` serve um `theme.{mp3,ogg,flac}`
que o **dono coloca manualmente** na pasta do item — documentado ali como
"fundação mínima e LOCAL-ONLY: sem provider" (`internal/server/localmedia_handlers.go:614-621`),
por não existir, na época, fonte confiável de tema musical. A pesquisa que
fundamenta o ADR-0025 achou uma: **ThemerrDB** (LizardByte), JSON estático
público sem API key, indexado por id TMDB/IMDb, que alimenta os plugins
oficiais Themerr-plex/-jellyfin/-kodi. Este briefing conecta essa fonte,
mantendo o comportamento manual existente como precedência mais alta (arquivo
do dono sempre vence).

## 2. Jornada do usuário

1. Item já catalogado, com `external_ids.tmdb` conhecido, sem `theme.mp3` na
   pasta. Na tela de detalhe, aparece "Tema musical disponível" (ThemerrDB
   achou um candidato) com um botão "Baixar tema" — só quando o yt-dlp está
   disponível (0101); sem candidato ou sem yt-dlp, nada aparece (mesmo
   silêncio de sempre).
2. Clique dispara o download; sucesso grava `theme.mp3` na raiz da pasta do
   item — o MESMO path que `GET /entities/{id}/theme` já serve hoje. Nenhuma
   mudança de contrato desse endpoint.
3. Item com `theme.mp3` já colocado manualmente pelo dono: o candidato do
   ThemerrDB (se houver) NUNCA é oferecido para substituir — o endpoint já
   serve o arquivo do dono e este briefing não toca nisso.
4. Item sem match no ThemerrDB (a maioria, dado que a cobertura é curada pela
   comunidade): nenhuma oferta aparece — sem erro, sem "não encontrado"
   ruidoso na tela.

## 3. Escopo

**Dentro:**
- `internal/provider/provider.go`: nova interface `TemaMusicalProvider`
  (mesmo estilo/comentário de `ArteProvider`, `provider.go:197-212`):
  ```go
  // TemaMusicalProvider é uma CAPACIDADE OPCIONAL (briefing 0103): busca um
  // candidato de música-tema (link de vídeo, nunca binário) pelos ids
  // externos que o item tiver. É opcional porque nem toda fonte modela tema
  // musical (só ThemerrDB hoje); o chamador faz type-assert e degrada em
  // silêncio (sem candidato) quando ausente ou sem match.
  type TemaMusicalProvider interface {
      // BuscarTemaMusical procura um tema pelos ids externos (`ids`), filtrando
      // por `tipo` ("movie"|"tv"). Sem id utilizável ou sem match: (TemaMusical{}, false, nil)
      // — graça, não erro. Erro real (rede/status) propaga.
      BuscarTemaMusical(ctx context.Context, ids ExternalIDs, tipo string) (TemaMusical, bool, error)
  }

  // TemaMusical é um candidato de tema musical (ThemerrDB): a URL do vídeo na
  // fonte (YouTube) — NUNCA o binário; download real é lazy via yt-dlp (0101).
  type TemaMusical struct {
      URL string
  }
  ```
- Novo pacote `internal/provider/themerrdb`: `New()` sem credencial (cliente
  HTTP simples, sem chave — sempre pode ser registrado). Consulta
  `https://app.lizardbyte.dev/ThemerrDB/{movies|tv_shows}/{tmdb|imdb}/{id}.json`
  (filme: tenta `tmdb`, senão `imdb`; série: só `tmdb`), extrai
  `youtube_theme_url`. 404 da API = sem match (não é erro).
- `cmd/mymediadb/main.go` (`provedoresReais`): registra `themerrdb.New()`
  **sempre**, sem gate de credencial (não precisa de chave) — mesmo padrão de
  registro incondicional de uma capacidade opcional, análogo (mas mais
  simples, sem `Configurada()`) ao que `fanarttv` faz quando tem chave.
- Cache do candidato: mesma técnica do inventário fanart.tv
  (`provider.ChaveEnvelopeInventarioArtes`, `provider.go:284-290`) — grava o
  resultado (ou "sem match") em `media_external` sob `source="themerrdb"`,
  numa chave de envelope nova (`ChaveEnvelopeTemaMusical`), pra não bater na
  API do ThemerrDB de novo a cada `GET /entities/{id}` — só refresca no
  ciclo normal de re-enriquecimento (mesmo gatilho pull do resto do enrich).
- `GET /api/v1/entities/{id}/theme/candidato` — expõe se há um candidato do
  ThemerrDB disponível para download (sem servir binário; só metadado) e se
  já existe `theme.mp3` local (reaproveita `encontrarArquivoTema`,
  `localmedia_handlers.go:630-639`).
- `POST /api/v1/entities/{id}/theme/download` — baixa o candidato via
  `s.ytdlp.Baixar` (0101) pro path `theme.mp3` (raiz da pasta, via
  `s.resolverDestino`) — **só quando `encontrarArquivoTema` não achou nada**
  (nunca sobrescreve arquivo do dono; sem `force` nesta entrega, ao contrário
  do trailer — tema é 1 por pasta, não faz sentido re-escolher).
- Frontend: afordance pequena perto de onde o tema já toca (se houver player
  visível — checar `rich-detail-view.tsx` na implementação) ou, na ausência de
  UI de tema hoje, um bloco simples na tela de detalhe.

**Fora (explicitamente):**
- Qualquer UI de "procurar tema manualmente"/busca — só o candidato automático
  do ThemerrDB.
- Suporte a séries por IMDb (ThemerrDB só indexa série por TMDB — filme aceita
  os dois).
- Refresh automático do candidato fora do ciclo de re-enriquecimento normal.
- Alterar o contrato de `GET /entities/{id}/theme` (continua servindo só o
  binário local, como hoje).

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration — `media_external` já é o cache genérico por
  `(entity_id, source)`; `source="themerrdb"` é uma linha nova por entidade
  enriquecida, mesmo mecanismo de qualquer outro provider.

### API
- `GET /api/v1/entities/{id}/theme/candidato` → `{"disponivel": bool, "ja_tem_local": bool}` (sem vazar a URL do YouTube pro cliente sem necessidade — o download acontece server-side).
- `POST /api/v1/entities/{id}/theme/download` → `{"local_path": string}` (200) | 409 (já existe `theme.*` local) | 422 (sem candidato) | 503 (sem yt-dlp).
- Atualizar `openapi.yaml`.

### Backend (Go)
- `internal/provider/provider.go`: `TemaMusicalProvider`, `TemaMusical`, `ChaveEnvelopeTemaMusical`.
- Novo pacote `internal/provider/themerrdb/themerrdb.go` (+ teste).
- `cmd/mymediadb/main.go`: registro incondicional em `provedoresReais`.
- Ponto de enriquecimento (mesmo lugar que hoje chama `ArteProvider.BuscarArtes` pro fanart.tv — localizar em `internal/server` o fluxo de enrich/import) ganha uma chamada equivalente para `TemaMusicalProvider`, gravando o envelope em `media_external`.
- `internal/server/localmedia_handlers.go`: `getEntityThemeCandidato`, `postEntityThemeDownload`.

### Frontend (Vite/React)
- Pequeno componente na tela de detalhe (mesmo arquivo `rich-detail-view.tsx` do trailer, se fizer sentido agrupar visualmente com ele).

### Arquivos afetados
- `internal/provider/provider.go`, `internal/provider/themerrdb/` (novo), `cmd/mymediadb/main.go`, `internal/server/localmedia_handlers.go`, ponto de enrich existente, `openapi.yaml`, `web/src/components/rich-detail-view.tsx`.

## 5. Critérios de aceite

- [x] `themerrdb.New().BuscarTemaMusical` com um `tmdb` id conhecido no ThemerrDB devolve a URL; com id desconhecido devolve `(_, false, nil)` sem erro. (`internal/provider/themerrdb/themerrdb_test.go`)
- [x] Enriquecimento de um item grava o envelope em `media_external` (source=themerrdb) sem quebrar quando não há match. (`internal/enrich/tema_musical_test.go`)
- [x] `POST /theme/download` com `theme.mp3` já presente (do dono) → 409, arquivo original intacto byte-a-byte. (`internal/server/theme_test.go: TestPostThemeDownloadJaExisteLocalSemForce409`)
- [x] `POST /theme/download` sem candidato → 422; sem yt-dlp → 503. (`TestPostThemeDownloadSemCandidato422`/`TestPostThemeDownloadSemYtdlp503`)
- [x] `GET /entities/{id}/theme` (endpoint existente, briefing 0082) continua servindo exatamente como antes — nenhuma regressão de contrato. (`TestGetEntityThemeContratoInalteradoAposDownloadAutomatico`, mais a suíte pré-existente de `localmedia_handlers_test.go` intacta)
- [x] `go build ./... && go vet ./... && go test ./... -count=1`; `tsc -b`/lint/`test:coverage` verdes. (Go: 552 testes web + suíte Go completa OK; `make gate-fast` passou com cobertura em alta em ambos os lados)

## 6. Notas de implementação / decisões

- **Por que não expor a URL do YouTube no `GET /candidato`?** Minimiza
  superfície de ToS (o cliente não linka pro YouTube diretamente a partir daí;
  o "assistir" já existe via trailer/embed quando aplicável) — decisão
  conservadora, reavaliar se o produto quiser mostrar preview antes de baixar.
- **Sem `force` no download de tema** (ao contrário do trailer): tema é
  singular por pasta (um `theme.mp3`), então re-escolher exige o dono apagar o
  arquivo manualmente primeiro — evita a UI de "trocar tema" virar decisão de
  produto não pedida por ninguém ainda.
- Cobertura do ThemerrDB é comunitária — a maioria dos itens não terá match;
  isso é esperado e não é bug (mesma postura best-effort do resto do projeto).

## 7. Fora deste briefing / follow-ups

- UI de busca manual de tema.
- Refresh fora do ciclo de re-enriquecimento.
- Suporte a séries via IMDb (ThemerrDB não modela isso).
