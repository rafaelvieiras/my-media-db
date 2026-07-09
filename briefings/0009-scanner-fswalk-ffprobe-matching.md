# 0009 — Scanner: fs walk + ffprobe + matching Kodi/Jellyfin

> O coração da Fase 2: varrer um datasource, extrair fatos técnicos (ffprobe),
> associar cada arquivo a uma `Entity` pela convenção Kodi/Jellyfin, descobrir a
> arte local — tudo em background, não-destrutivo, com o ciclo de vida `missing`.

| | |
|---|---|
| **ID** | 0009 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 2 — UI de biblioteca |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md), [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md), [ADR-0014](../docs/adr/0014-stack-de-implementacao.md) |
| **Status** | done |
| **Depende de** | [0007](0007-schema-nucleo-minimo.md), [0008](0008-datasources-cadastro-api-library.md) |

---

## 1. Objetivo

Transformar pastas apontadas em **catálogo**. O scanner varre um `library_source`,
para cada arquivo de vídeo calcula um `content_hash` estável, extrai os **fatos
técnicos** via **ffprobe** ([ADR-0014](../docs/adr/0014-stack-de-implementacao.md)),
lê o **nome pela convenção Kodi/Jellyfin** ([ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md))
para associar o arquivo à `Entity` certa (grão 1:N plano), e descobre a **arte
local** (poster/folder) — **sem enriquecimento externo**. Roda em background, escreve
em lotes curtos pela conexão única ([ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md))
e honra o ciclo de vida não-destrutivo ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):
sumiço vira `missing`, datasource offline não marca nada.

## 2. Jornada do usuário

1. O dono clica "Escanear" num datasource (ou o scan dispara ao criá-lo).
2. Um **job em background** começa; a UI mostra progresso (arquivos vistos/total, estado).
3. À medida que avança, **itens detectados aparecem** — cada `Entity` com seus
   `media_file` (qualidades/edições empilhadas), fatos técnicos e, quando há, poster local.
4. Arquivo com **id no nome** (`[tmdbid-…]`) casa com 100% de confiança; sem id,
   casa por título+ano com **score** — baixo vira **alerta de revisão**; zero → `draft`.
5. Reescanear é **idempotente**: nada duplica (dedup por `content_hash`); arquivo
   que sumiu vira **`missing`**; se o datasource inteiro está offline, o scan é
   **suspenso** e nada é marcado missing.
6. `Ctrl+C`/shutdown durante o scan não corrompe: lotes curtos já commitados persistem.

## 3. Escopo

**Dentro:**
- **Job de scan em background** por datasource: dispara via API, reporta **status/progresso**
  consultável; um scan por datasource por vez.
- **Fs walk** com filtro de extensões de vídeo (`.mp4 .mkv .avi .mov .wmv .flv .webm`
  como ponto de partida — [06](../docs/06-biblioteca-e-scan.md)).
- **`content_hash` parcial/amostrado**: `size` + hash de blocos do início/fim (estável
  a rename/move, rápido em arquivos grandes) — decisão de sessão, ver §6.
- **ffprobe** como subprocesso (JSON) → `mediainfo_json` (general/video/audio: codec,
  resolução, HDR, faixas, duração real).
- **Matching Kodi/Jellyfin** ([ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md)):
  pasta = obra = `Entity`; arquivos que começam com o nome da pasta = `media_file` da
  **mesma** entity; `edition` como rótulo; `part_no` (stacking `-cd1`/`-part1`); id
  embutido no nome → `external_ids` + confiança 100%; sem id → título+ano + **score**;
  baixo → alerta de revisão; zero → `draft` não-identificado. **Sem chamar provider**
  (só parseia o nome; a busca externa é Fase 3).
- **Arte local** (poster): descobrir `poster.jpg`/`folder.jpg`/`<nome>-poster.jpg` →
  linha em `asset` (`kind=poster, origin=local, path_or_uri`); fallback fica a cargo da UI.
- **Escrita em lotes curtos** pela conexão de escrita; **ciclo de vida**: marcar
  `missing` os `media_file` não revistos num scan completo com datasource **acessível**;
  datasource inacessível → `state=offline`, scan suspenso, nada marcado.
- Testes com uma **pasta-fixture** (arquivos pequenos/sintéticos) cobrindo: detecção,
  dedup em re-scan, id no nome vs título+ano, missing, offline, stacking/edições.

**Fora (explicitamente):**
- **Enriquecimento externo** (TMDB/etc.) e a UI de fila de revisão/órfãs — Fase 3.
- **Watching de filesystem** (inotify) e **scan incremental por mtime** — full scan
  agora; incremental é follow-up (§7).
- Extração de thumbnail por ffmpeg (a decisão de poster é **arte local**, §6).
- Música/quadrinhos e verticais fora do padrão Kodi (`extension_data`, fases futuras).

## 4. Contrato técnico

### Dados / modelo
- Escreve `entity`, `media_file` (com `state`, `last_seen`, `content_hash`, `mediainfo_json`,
  `match_confidence`, `edition`, `part_no`), `asset` (poster local), e atualiza
  `library_source.last_scan`/`state`. Grão e colunas do [0007](0007-schema-nucleo-minimo.md).

### API
- **`POST /api/v1/library/{id}/scan`** — enfileira/dispara o scan; `202` com um
  identificador de job; `409` se já houver scan em andamento; `409/422` se offline.
- **`GET /api/v1/library/{id}/scan`** (ou `/scan/status`) — estado atual:
  `{ running, files_seen, files_total?, started_at, last_error? }`.

### Backend (Go)
- Pacote `internal/scanner` (walk, hash, orquestração de lotes) + `internal/mediainfo`
  (wrapper ffprobe) + `internal/naming` (parser Kodi/Jellyfin: título, ano, id,
  edição, parte). Serviço `scanner` da [03](../docs/03-arquitetura.md).
- **Concorrência:** walk/hash/ffprobe podem paralelizar (I/O e subprocessos); **toda
  escrita** serializa na conexão única, em **lotes curtos** ([ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md)).
- **Job/estado:** um registro de progresso em memória por datasource (mecanismo de
  jobs simples: goroutine + contexto cancelável); persistir `last_scan`. Fila robusta
  é evolução (§7).
- ffprobe é **dependência de runtime** — checar presença no boot; ausência degrada
  (mediainfo vazio + aviso), não derruba o scan.

### Arquivos afetados
- Criar: `internal/scanner/*.go`, `internal/mediainfo/*.go`, `internal/naming/*.go`,
  handlers de scan em `/api/v1/library/{id}/scan`, fixtures de teste.
- Alterar: rotas do servidor; queries sqlc (upsert por `content_hash`, marcar missing);
  Dockerfile (garantir **ffprobe/ffmpeg** na imagem final — hoje `alpine:3.22`).

## 5. Critérios de aceite

- [ ] Escanear a pasta-fixture cria as `Entity` esperadas com seus `media_file` (qualidades/edições na **mesma** entity) e `mediainfo_json` preenchido pelo ffprobe.
- [ ] **Re-scan é idempotente**: nenhum registro duplica (dedup por `content_hash`); renomear um arquivo **não** cria outro.
- [ ] Arquivo com `[tmdbid-N]` no nome → `external_ids.tmdb=N` e `match_confidence=100`; sem id → casa por título+ano com score < 100; sem match → `entity.status=draft`.
- [ ] Arquivo removido do disco (datasource acessível) → `media_file.state=missing` com `last_seen`; **entity preservada**; posse passa a falso.
- [ ] Datasource inacessível → `library_source.state=offline`, scan **suspenso**, **nenhum** `media_file` marcado missing.
- [ ] Poster local (`poster.jpg`/`folder.jpg`) vira `asset(kind=poster,origin=local)`.
- [ ] Escritas ocorrem em lotes curtos pela conexão única; interromper no meio não corrompe; catraca verde.

## 6. Notas de implementação / decisões

- **`content_hash` parcial/amostrado** (decisão de sessão, 2026-07-09): `size` +
  hash de blocos do início/fim. Rápido em bibliotecas de TB e **estável a rename/move**
  (a doc 06 exige dedup por conteúdo); colisão real é astronômica. Full-SHA descartado
  (I/O proibitivo); path+mtime descartado (quebra no move).
- **Poster = arte local** (decisão de sessão, 2026-07-09): descobre `poster.jpg`/
  `folder.jpg`/`<nome>-poster.jpg` (convenção Kodi/Jellyfin). Sem chamadas externas,
  não-destrutivo. Thumbnail por ffmpeg descartado (frame-grab feio). Placeholder é da UI.
- **Parser de nome é leitor, nunca escritor** ([ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md)):
  o scan não renomeia nada. Regex/thresholds de confiança são **tuning** — começar
  conservador e documentar; o limiar exato do alerta não é modelo.
- **ffprobe, não mediainfo-cli** ([ADR-0014](../docs/adr/0014-stack-de-implementacao.md));
  garantir o binário na imagem de produção (ajuste no Dockerfile do [0003](0003-empacotamento-dockerfile-multistage.md)).
- **Mecanismo de jobs mínimo** (goroutine + estado em memória): a [03](../docs/03-arquitetura.md)
  admite evoluir para "fila/goroutine escritora dedicada" sem mudar o resto — fica §7.
- **Escopo de matching MVP:** focar **filmes** (pasta=filme) com base sólida; TV
  (série↔temporada↔episódio via `entity_relation`) pode ser um recorte menor de
  follow-up se inflar este briefing.

## 7. Fora deste briefing / follow-ups

- **Scan incremental** (mtime/hash) e **watching** (inotify) — hoje é full scan.
- **Fila de jobs** robusta (persistente, com retry/agenda) e priorização explícita.
- **Fila de revisão** (matches de baixa confiança) e **fila de órfãs** — UI/superfície na Fase 3.
- **TV/hierarquia** (`entity_relation`) e verticais fora do padrão Kodi.
- **Enriquecimento externo** (TMDB) que resolve título+ano de verdade — Fase 3.
